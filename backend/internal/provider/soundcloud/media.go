// Package soundcloud resolves audio sources on SoundCloud through yt-dlp.
package soundcloud

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// ProviderName is the identifier of the SoundCloud media provider.
const ProviderName = "soundcloud"

// Config configures a SoundCloud media provider instance.
type Config struct {
	// Client is the yt-dlp adapter.
	Client *ytdlp.Client
	// Limit bounds how many candidates a search returns.
	Limit int
	// RequestsPerSecond paces search and resolve operations. Zero or negative disables pacing.
	RequestsPerSecond float64
	// Burst is the token bucket burst capacity.
	Burst int
}

// limiter paces requests across concurrent workers.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    float64
	tokens   float64
	last     time.Time
}

func newLimiter(ratePerSecond float64, burst int) *limiter {
	if ratePerSecond <= 0 {
		return nil
	}
	interval := time.Duration(float64(time.Second) / ratePerSecond)
	if interval <= 0 {
		interval = time.Nanosecond
	}
	b := float64(max(burst, 1))
	return &limiter{
		interval: interval,
		burst:    b,
		tokens:   b,
	}
}

func (l *limiter) Wait(ctx context.Context) error {
	if l == nil || l.interval <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := l.reserve()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if l.last.IsZero() {
		l.last = now
	}
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = min(l.burst, l.tokens+float64(elapsed)/float64(l.interval))
		l.last = now
	}
	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	return time.Duration(-l.tokens * float64(l.interval))
}

// MediaProvider finds and resolves audio sources on SoundCloud through yt-dlp.
type MediaProvider struct {
	client  *ytdlp.Client
	limit   int
	limiter *limiter
}

var (
	_ provider.MediaProvider  = (*MediaProvider)(nil)
	_ provider.FamilyProvider = (*MediaProvider)(nil)
	_ provider.Availability   = (*MediaProvider)(nil)
)

// New builds a SoundCloud media provider.
func New(cfg Config) (*MediaProvider, error) {
	if cfg.Client == nil {
		return nil, apperr.New(apperr.CodeInternal, "The SoundCloud media provider needs a yt-dlp client.")
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = 10
	}
	p := &MediaProvider{
		client:  cfg.Client,
		limit:   limit,
		limiter: newLimiter(cfg.RequestsPerSecond, cfg.Burst),
	}
	if p.limiter != nil {
		// Pacing applies to real process starts only; a search or resolution
		// answered from the yt-dlp query cache sends no request.
		p.client = p.client.WithPacer(p.limiter)
	}
	return p, nil
}

// Name returns the provider identifier.
func (p *MediaProvider) Name() string { return ProviderName }

// Family returns the platform family (SoundCloud platform family).
func (p *MediaProvider) Family() provider.Family { return provider.FamilySoundCloud }

// Available reports whether yt-dlp can be executed.
func (p *MediaProvider) Available(ctx context.Context) error {
	return p.client.Available(ctx)
}

// Search returns the candidates that might carry the wanted track.
func (p *MediaProvider) Search(ctx context.Context, track music.Track) ([]provider.MediaCandidate, error) {
	query := provider.SearchQuery(track)
	if strings.TrimSpace(query) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The track has neither an artist nor a title to search for.")
	}

	target := fmt.Sprintf("scsearch%d:%s", p.limit, query)
	results, err := p.client.Query(ctx, target, "--flat-playlist")
	if err != nil {
		return nil, err
	}

	candidates := make([]provider.MediaCandidate, 0, len(results))
	for _, info := range results {
		if c, ok := p.toCandidate(info); ok {
			candidates = append(candidates, c)
		}
	}
	return candidates, nil
}

// Resolve turns a candidate into a concrete, downloadable source.
func (p *MediaProvider) Resolve(ctx context.Context, candidate provider.MediaCandidate) (*provider.MediaSource, error) {
	results, err := p.client.Query(ctx, candidate.URL)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, apperr.Newf(apperr.CodeTrackNotFound,
			"The media item %q could not be resolved on SoundCloud.", candidate.ID)
	}

	info := results[0]
	pageURL := soundcloudURL(info)
	if pageURL == "" {
		pageURL = candidate.URL
	}

	source := &provider.MediaSource{
		Provider:   ProviderName,
		ID:         info.ID,
		URL:        pageURL,
		Title:      info.DisplayTitle(),
		Uploader:   strings.TrimSpace(info.Uploader),
		DurationMS: info.DurationMS(),
		SessionID:  "", // Strictly empty: SoundCloud never uses YouTube sessions
	}

	for _, f := range info.Formats {
		// yt-dlp marks SoundCloud snipped streams with an _preview suffix.
		// Their source metadata and probed files can report different durations; they
		// are not full-track sources regardless of the matching score.
		if !f.IsAudioOnly() || strings.HasSuffix(strings.ToLower(f.FormatID), "_preview") {
			continue
		}
		source.Formats = append(source.Formats, provider.AudioFormat{
			ID:          f.FormatID,
			Codec:       f.ACodec,
			Container:   f.Container,
			BitrateKbps: f.Bitrate(),
			SampleRate:  f.ASR,
			Channels:    f.AudioChannels,
			Filesize:    f.Size(),
		})
	}

	if len(source.Formats) == 0 {
		return nil, apperr.New(apperr.CodeTrackNotFound, "SoundCloud offers no full-length audio format for this candidate (preview-only or unavailable).")
	}
	return source, nil
}

func soundcloudURL(info ytdlp.Info) string {
	if u := strings.TrimSpace(info.WebpageURL); u != "" && isValidSoundCloudURL(u) {
		return u
	}
	if u := strings.TrimSpace(info.URL); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		if isValidSoundCloudURL(u) {
			return u
		}
	}
	return ""
}

func isValidSoundCloudURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com")
}

func isSoundCloudTrackURL(rawURL string) bool {
	if !isValidSoundCloudURL(rawURL) {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return false
	}
	parts := strings.Split(path, "/")
	// A soundcloud track is usually /<user>/<track-slug> (2 parts), and not /sets/
	if len(parts) < 2 {
		return false
	}
	if parts[1] == "sets" {
		return false
	}
	return true
}

var artistSeparators = regexp.MustCompile(`\s*(?:,|;|·|\bfeat\b\.?|\bft\b\.?|\bfeaturing\b|\bwith\b)\s*`)

func splitArtists(credit string) []string {
	trimmed := strings.TrimSpace(credit)
	if trimmed == "" {
		return nil
	}

	parts := artistSeparators.Split(trimmed, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func splitArtistTitle(raw string) (string, string, bool) {
	title := strings.TrimSpace(raw)
	for _, separator := range []string{" - ", " – ", " — ", " | "} {
		artist, rest, found := strings.Cut(title, separator)
		if !found {
			continue
		}
		artist = strings.TrimSpace(artist)
		rest = strings.TrimSpace(rest)
		if artist == "" || rest == "" {
			continue
		}
		return artist, rest, true
	}
	return "", title, false
}

func (p *MediaProvider) toCandidate(info ytdlp.Info) (provider.MediaCandidate, bool) {
	if strings.TrimSpace(info.ID) == "" {
		return provider.MediaCandidate{}, false
	}
	if info.IsLive || info.LiveStatus == "is_live" || info.LiveStatus == "is_upcoming" {
		return provider.MediaCandidate{}, false
	}

	pageURL := soundcloudURL(info)
	if pageURL == "" || !isSoundCloudTrackURL(pageURL) {
		return provider.MediaCandidate{}, false
	}

	title := info.DisplayTitle()
	artists := splitArtists(info.DisplayArtist())
	uploader := strings.TrimSpace(info.Uploader)

	if len(artists) == 0 {
		if artist, rest, ok := splitArtistTitle(info.Title); ok {
			artists = splitArtists(artist)
			if strings.TrimSpace(info.Track) == "" {
				title = rest
			}
		}
	}
	if len(artists) == 0 && uploader != "" {
		artists = []string{uploader}
	}

	return provider.MediaCandidate{
		Provider:       ProviderName,
		ID:             info.ID,
		URL:            pageURL,
		Title:          title,
		Artists:        artists,
		Album:          strings.TrimSpace(info.Album),
		DurationMS:     info.DurationMS(),
		Uploader:       uploader,
		IsMusicService: false,
	}, true
}
