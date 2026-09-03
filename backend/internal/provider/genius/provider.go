package genius

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

const (
	ProviderName       = "genius"
	DefaultBaseURL     = "https://api.genius.com"
	DefaultTimeout     = 12 * time.Second
	MinMatchConfidence = 0.85

	// Rate limiting & circuit breaker
	circuitBreakerThreshold = 3
	circuitBreakerDuration  = 5 * time.Minute
	requestPacingDuration   = 500 * time.Millisecond
)

type searchResponse struct {
	Meta struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"meta"`
	Response struct {
		Hits []struct {
			Type   string     `json:"type"`
			Result SongResult `json:"result"`
		} `json:"hits"`
		Sections []struct {
			Type string `json:"type"`
			Hits []struct {
				Type   string     `json:"type"`
				Result SongResult `json:"result"`
			} `json:"hits"`
		} `json:"sections"`
	} `json:"response"`
}

type SongResult struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Path          string `json:"path"`
	PrimaryArtist struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"primary_artist"`
}

var _ provider.LyricsProvider = (*LyricsProvider)(nil)

// LyricsProvider implements provider.LyricsProvider for Genius.
type LyricsProvider struct {
	enabled bool
	token   string
	baseURL string
	client  *http.Client
	timeout time.Duration
	logger  *slog.Logger

	// Rate limiting & serialization (1 request at a time)
	reqMu   sync.Mutex
	lastReq time.Time

	// Circuit breaker state
	cbMu          sync.Mutex
	failCount     int
	cooldownUntil time.Time
}

// NewLyricsProvider constructs a Genius lyrics provider.
func NewLyricsProvider(cfg Config) *LyricsProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &LyricsProvider{
		enabled: cfg.Enabled,
		token:   strings.TrimSpace(cfg.AccessToken),
		baseURL: baseURL,
		client:  client,
		timeout: timeout,
		logger:  logger,
	}
}

// Name returns the provider name.
func (p *LyricsProvider) Name() string {
	return ProviderName
}

// IsEnabled reports whether the provider is enabled.
func (p *LyricsProvider) IsEnabled() bool {
	return p.enabled
}

// HasToken reports whether an API token is configured.
func (p *LyricsProvider) HasToken() bool {
	return p.token != ""
}

// SetEnabled dynamically updates the enabled state.
func (p *LyricsProvider) SetEnabled(enabled bool) {
	p.enabled = enabled
}

// Lyrics resolves lyrics for a track using Genius as fallback.
func (p *LyricsProvider) Lyrics(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error) {
	if !p.enabled {
		return nil, nil
	}

	// 1. Check circuit breaker
	if p.isTripped() {
		p.logger.Warn("genius lyrics skipped: circuit breaker active after repeated rate limits/challenges",
			logging.KeyProvider, ProviderName)
		return nil, nil
	}

	// 2. Bound execution by timeout
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// 3. Serialize request and enforce minimum pacing
	p.acquireRateSlot(ctx)

	// 4. Build search query
	artist := ""
	if len(track.Artists) > 0 {
		artist = track.Artists[0]
	} else if track.AlbumArtist != "" {
		artist = track.AlbumArtist
	}
	query := strings.TrimSpace(artist + " " + track.Title)
	if query == "" {
		return nil, nil
	}

	var searchURL string
	if p.token != "" {
		// Official API endpoint requiring Bearer authentication.
		searchURL = fmt.Sprintf("%s/search?q=%s", p.baseURL, url.QueryEscape(query))
	} else if p.baseURL != DefaultBaseURL {
		// Testing override endpoint.
		searchURL = fmt.Sprintf("%s/api/search/song?q=%s", p.baseURL, url.QueryEscape(query))
	} else {
		// Best-effort fallback: unauthenticated public web search endpoint used by genius.com.
		// Undocumented and without API SLA; failure (403, 429, challenge, schema change) cleanly trips
		// the circuit breaker and yields CodeProviderUnavailable without impacting audio downloads.
		searchURL = fmt.Sprintf("https://genius.com/api/search/song?q=%s", url.QueryEscape(query))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	p.logger.Debug("searching genius lyrics",
		logging.KeyProvider, ProviderName,
		logging.KeyTrack, track.Label(),
		"has_token", p.token != "",
		"query", query)

	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p.recordFailure()
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "genius search network error", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		p.recordFailure()
		p.logger.Warn("genius api rate limited or forbidden",
			logging.KeyProvider, ProviderName,
			"status", resp.StatusCode)
		return nil, apperr.Newf(apperr.CodeProviderUnavailable, "genius rate limit/forbidden status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		p.recordFailure()
		return nil, apperr.Newf(apperr.CodeProviderUnavailable, "genius search unexpected status %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		p.recordFailure()
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to decode genius search response", err)
	}

	// 5. Evaluate candidate hits
	var bestCandidate *SongResult
	var bestConfidence float64

	var candidates []SongResult
	for _, hit := range sr.Response.Hits {
		if hit.Type == "song" {
			candidates = append(candidates, hit.Result)
		}
	}
	for _, sec := range sr.Response.Sections {
		if sec.Type == "song" || sec.Type == "top_hit" {
			for _, hit := range sec.Hits {
				if hit.Type == "song" {
					candidates = append(candidates, hit.Result)
				}
			}
		}
	}

	for _, cand := range candidates {
		match := MatchCandidate(track, cand, MinMatchConfidence)
		if match.Accepted && match.Confidence > bestConfidence {
			bestConfidence = match.Confidence
			chosen := cand
			bestCandidate = &chosen
		}
	}

	if bestCandidate == nil {
		p.recordSuccess() // Successful API query that simply had no match
		p.logger.Debug("no genius candidate met match threshold",
			logging.KeyProvider, ProviderName,
			logging.KeyTrack, track.Label())
		return nil, nil
	}

	p.logger.Info("genius candidate matched",
		logging.KeyProvider, ProviderName,
		logging.KeyTrack, track.Label(),
		"candidate_id", bestCandidate.ID,
		"candidate_title", bestCandidate.Title,
		"confidence", bestConfidence)

	// 6. Fetch song page and parse lyrics
	p.acquireRateSlot(ctx)

	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, bestCandidate.URL, nil)
	if err != nil {
		return nil, err
	}
	pageReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	pageResp, err := p.client.Do(pageReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p.recordFailure()
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "genius song page fetch failed", err)
	}
	defer pageResp.Body.Close()

	if pageResp.StatusCode == http.StatusTooManyRequests || pageResp.StatusCode == http.StatusForbidden {
		p.recordFailure()
		return nil, apperr.Newf(apperr.CodeProviderUnavailable, "genius song page rate limit/forbidden status %d", pageResp.StatusCode)
	}
	if pageResp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if pageResp.StatusCode != http.StatusOK {
		p.recordFailure()
		return nil, apperr.Newf(apperr.CodeProviderUnavailable, "genius song page returned status %d", pageResp.StatusCode)
	}

	plainText, err := ParseLyrics(pageResp.Body)
	if err != nil {
		if errors.Is(err, ErrChallengePage) {
			p.recordFailure()
			p.logger.Warn("genius returned bot challenge/captcha page",
				logging.KeyProvider, ProviderName)
			return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "genius challenge page encountered", err)
		}
		if errors.Is(err, ErrNoLyricsFound) || errors.Is(err, ErrInvalidContent) {
			// Page had no lyrics or invalid text (e.g. instrumental)
			p.logger.Debug("genius page contained no extractable lyrics",
				logging.KeyProvider, ProviderName,
				"reason", err.Error())
			return nil, nil
		}
		p.recordFailure()
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to parse genius lyrics", err)
	}

	// 7. Successful resolution!
	p.recordSuccess()

	return &music.Lyrics{
		Provider:     ProviderName,
		SourceID:     strconv.FormatInt(bestCandidate.ID, 10),
		Synced:       false,
		Instrumental: false,
		PlainText:    plainText,
		LRC:          "",
	}, nil
}

// acquireRateSlot ensures strictly serialized requests with pacing delay.
func (p *LyricsProvider) acquireRateSlot(ctx context.Context) {
	p.reqMu.Lock()
	defer p.reqMu.Unlock()

	since := time.Since(p.lastReq)
	if since < requestPacingDuration {
		select {
		case <-ctx.Done():
		case <-time.After(requestPacingDuration - since):
		}
	}
	p.lastReq = time.Now()
}

// isTripped checks if the circuit breaker is currently active.
func (p *LyricsProvider) isTripped() bool {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()

	if p.failCount >= circuitBreakerThreshold {
		if time.Now().Before(p.cooldownUntil) {
			return true
		}
		// Cooldown elapsed, reset for probe
		p.failCount = 0
	}
	return false
}

func (p *LyricsProvider) recordFailure() {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()

	p.failCount++
	if p.failCount >= circuitBreakerThreshold {
		p.cooldownUntil = time.Now().Add(circuitBreakerDuration)
		p.logger.Warn("genius provider tripped circuit breaker after consecutive failures",
			logging.KeyProvider, ProviderName,
			"cooldown_minutes", circuitBreakerDuration.Minutes())
	}
}

func (p *LyricsProvider) recordSuccess() {
	p.cbMu.Lock()
	defer p.cbMu.Unlock()
	p.failCount = 0
}
