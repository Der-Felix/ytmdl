package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
	"ytdm/backend/internal/music"
)

// LRCLibName is the stable provider identifier.
const LRCLibName = "lrclib"

// DefaultLRCLibBaseURL is the public LRCLIB instance. It needs no account, no
// API key and no registration.
const DefaultLRCLibBaseURL = "https://lrclib.net"

// maxResponseBytes bounds one LRCLIB answer.
const maxResponseBytes = 4 << 20

// LRCLibConfig configures the LRCLIB client.
type LRCLibConfig struct {
	BaseURL string
	// UserAgent identifies this client. LRCLIB requires an application name,
	// a version and a project link or an email address.
	UserAgent string
	Client    *http.Client
	Timeout   time.Duration
}

// LRCLib is the LRCLIB lyrics provider.
type LRCLib struct {
	baseURL   string
	userAgent string
	client    *http.Client
}

// NewLRCLib builds the provider on top of an SSRF protected client.
func NewLRCLib(cfg LRCLibConfig) *LRCLib {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultLRCLibBaseURL
	}
	client := cfg.Client
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = httpx.New(timeout)
	}
	agent := strings.TrimSpace(cfg.UserAgent)
	if agent == "" {
		agent = "YTMDL (https://github.com/)"
	}
	return &LRCLib{baseURL: base, userAgent: agent, client: client}
}

// Name returns the provider identifier.
func (l *LRCLib) Name() string { return LRCLibName }

// Lyrics looks a track up. A miss returns (nil, nil).
//
// LRCLIB matches on the track title and the artist name and narrows the result
// with the album name and the duration; it accepts a duration difference of at
// most two seconds. It has no ISRC lookup, so the recording identifier the
// catalogue carries cannot be used here.
//
// Only the primary artist is sent. LRCLIB indexes by it, and a joined credit
// string such as "A; B" reliably misses.
func (l *LRCLib) Lyrics(ctx context.Context, track music.Track, _ string) (*music.Lyrics, error) {
	title := strings.TrimSpace(track.Title)
	artist := music.PrimaryArtist(track.Artists)
	if title == "" || artist == music.UnknownArtist {
		return nil, nil
	}

	album := strings.TrimSpace(track.Album)
	result, err := l.get(ctx, title, artist, album, track.DurationSeconds())
	if err != nil {
		return nil, err
	}
	// LRCLIB records frequently carry a different album string than the
	// metadata provider does — "Discovery" is filed there as
	// "(2001) Daft Punk - Discovery". Dropping the optional field is the one
	// retry worth making; anything beyond that would be guessing at variants.
	if result == nil && album != "" {
		return l.get(ctx, title, artist, "", track.DurationSeconds())
	}
	return result, nil
}

func (l *LRCLib) get(ctx context.Context, title, artist, album string, durationSeconds int) (*music.Lyrics, error) {
	query := url.Values{}
	query.Set("track_name", title)
	query.Set("artist_name", artist)
	if album != "" {
		query.Set("album_name", album)
	}
	if durationSeconds >= 1 && durationSeconds <= 3600 {
		query.Set("duration", strconv.Itoa(durationSeconds))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		l.baseURL+"/api/get?"+query.Encode(), nil)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The lyrics request could not be built.", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", l.userAgent)

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "LRCLIB could not be reached.", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	case http.StatusTooManyRequests:
		return nil, NewRateLimitError(LRCLibName, parseRetryAfter(resp.Header.Get("Retry-After")))
	default:
		return nil, apperr.Newf(apperr.CodeProviderUnavailable,
			"LRCLIB answered with status %d.", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The LRCLIB response could not be read.", err)
	}

	var payload struct {
		ID           int64   `json:"id"`
		Instrumental bool    `json:"instrumental"`
		PlainLyrics  *string `json:"plainLyrics"`
		SyncedLyrics *string `json:"syncedLyrics"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The LRCLIB response could not be decoded.", err)
	}

	result := music.Lyrics{
		Provider:     LRCLibName,
		SourceID:     strconv.FormatInt(payload.ID, 10),
		Instrumental: payload.Instrumental,
	}
	if payload.PlainLyrics != nil {
		result.PlainText = strings.TrimSpace(*payload.PlainLyrics)
	}
	if payload.SyncedLyrics != nil && strings.TrimSpace(*payload.SyncedLyrics) != "" {
		result.LRC = strings.TrimSpace(*payload.SyncedLyrics)
		result.Synced = true
		if result.PlainText == "" {
			result.PlainText = StripTimestamps(result.LRC)
		}
	}
	if result.State() == music.LyricsNotFound {
		return nil, nil
	}
	return &result, nil
}

// RateLimitError carries the Retry-After a provider asked for. LRCLIB's
// documentation is explicit that ignoring it can get a client banned, so the
// duration travels with the error instead of being guessed at by the caller.
type RateLimitError struct {
	Provider   string
	RetryAfter time.Duration
	err        error
}

// NewRateLimitError builds the error a provider returns when it rate limits.
func NewRateLimitError(providerName string, retryAfter time.Duration) *RateLimitError {
	return &RateLimitError{
		Provider:   providerName,
		RetryAfter: retryAfter,
		err: apperr.Newf(apperr.CodeProviderRateLimited,
			"%s is rate limiting the backend; the next attempt is allowed in %s.",
			providerName, retryAfter.Round(time.Second)),
	}
}

func (e *RateLimitError) Error() string { return e.err.Error() }
func (e *RateLimitError) Unwrap() error { return e.err }

// RetryAfter reports how long a caller must wait after a rate limit error.
func RetryAfter(err error) (time.Duration, bool) {
	var limited *RateLimitError
	if errors.As(err, &limited) {
		return limited.RetryAfter, true
	}
	return 0, false
}

// parseRetryAfter reads the Retry-After header, clamped to a sane range.
func parseRetryAfter(header string) time.Duration {
	const fallback = 30 * time.Second
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds <= 0 {
		return fallback
	}
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

// StripTimestamps turns an LRC body into plain text, so a synchronised result
// always carries a readable fallback for clients that cannot render timings.
func StripTimestamps(lrc string) string {
	var b strings.Builder
	for _, line := range strings.Split(lrc, "\n") {
		text := strings.TrimSpace(line)
		for strings.HasPrefix(text, "[") {
			end := strings.Index(text, "]")
			if end < 0 {
				break
			}
			text = strings.TrimSpace(text[end+1:])
		}
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
