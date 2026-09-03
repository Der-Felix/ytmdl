// Package youtube resolves audio sources on YouTube and YouTube Music through
// yt-dlp. The same implementation serves both platforms; they differ only in
// how a search is addressed, which is what SearchMode selects.
package youtube

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// ProviderName is the identifier of the plain YouTube media provider.
const ProviderName = "youtube"

// SearchMode selects how candidates are looked up.
type SearchMode string

const (
	// SearchVideos uses YouTube's general video search.
	SearchVideos SearchMode = "videos"
	// SearchMusic uses the YouTube Music search page, which returns songs
	// rather than videos.
	SearchMusic SearchMode = "music"
)

// allowedHosts are the only hosts a media URL may point at.
var allowedHosts = []string{"youtube.com", "youtu.be", "music.youtube.com", "youtube-nocookie.com"}

// musicSongsFilter restricts a YouTube Music search to songs. It is the
// parameter the web client itself sends for the "Songs" tab.
const musicSongsFilter = "EgWKAQIIAWoKEAoQCRADEAQQBQ%3D%3D"

// Config configures a media provider instance.
type Config struct {
	// Name is the provider identifier reported to the rest of the backend.
	Name string
	// Mode selects the search backend.
	Mode SearchMode
	// Client is the yt-dlp adapter.
	Client *ytdlp.Client
	// Limit bounds how many candidates a search returns.
	Limit int
	// EnrichLimit bounds how many candidates without a runtime are looked up
	// in full. Without a runtime the matching engine cannot judge a candidate,
	// so a small number of extra lookups is worth it.
	EnrichLimit int
	// MusicService marks candidates as coming from a dedicated music
	// catalogue, which the matcher uses to break ties.
	MusicService bool
}

// MediaProvider finds and resolves audio sources through yt-dlp.
type MediaProvider struct {
	name         string
	mode         SearchMode
	client       *ytdlp.Client
	limit        int
	enrichLimit  int
	musicService bool
}

var (
	_ provider.MediaProvider = (*MediaProvider)(nil)
	_ provider.Availability  = (*MediaProvider)(nil)
)

// New builds a media provider.
func New(cfg Config) (*MediaProvider, error) {
	if cfg.Client == nil {
		return nil, apperr.New(apperr.CodeInternal, "The media provider needs a yt-dlp client.")
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = ProviderName
	}
	mode := cfg.Mode
	if mode == "" {
		mode = SearchVideos
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = 10
	}
	enrich := cfg.EnrichLimit
	if enrich < 0 {
		enrich = 0
	}
	if cfg.EnrichLimit == 0 {
		enrich = 3
	}
	return &MediaProvider{
		name:         name,
		mode:         mode,
		client:       cfg.Client,
		limit:        limit,
		enrichLimit:  enrich,
		musicService: cfg.MusicService,
	}, nil
}

// Name returns the provider identifier.
func (p *MediaProvider) Name() string { return p.name }

var youtubeVideoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func isYouTubeVideoID(id string) bool {
	return youtubeVideoIDRe.MatchString(strings.TrimSpace(id))
}

func isChannelOrPlaylistURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "/channel/") ||
		strings.Contains(lower, "/c/") ||
		strings.Contains(lower, "/@") ||
		strings.Contains(lower, "/user/") ||
		strings.Contains(lower, "/playlist") ||
		strings.Contains(lower, "list=")
}

func isWatchURL(rawURL string) bool {
	if ValidateMediaURL(rawURL) != nil {
		return false
	}
	lower := strings.ToLower(rawURL)
	return (strings.Contains(lower, "watch?v=") || strings.Contains(lower, "youtu.be/")) &&
		!isChannelOrPlaylistURL(rawURL)
}

// Available reports whether yt-dlp can be executed.
func (p *MediaProvider) Available(ctx context.Context) error {
	return p.client.Available(ctx)
}

// Search returns the candidates that might carry the wanted track.
func (p *MediaProvider) Search(ctx context.Context, track music.Track) ([]provider.MediaCandidate, error) {
	// 1. Direct-ID Fast Path: If track carries a known video ID from ytmusic/youtube
	if candidate, ok := p.probeDirectID(ctx, track); ok {
		return []provider.MediaCandidate{candidate}, nil
	}

	query := provider.SearchQuery(track)
	if strings.TrimSpace(query) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The track has neither an artist nor a title to search for.")
	}

	var (
		results []ytdlp.Info
		err     error
	)
	switch p.mode {
	case SearchMusic:
		results, err = p.client.Query(ctx, musicSearchURL(query),
			"--flat-playlist", "--playlist-items", "1:"+strconv.Itoa(p.limit))
	default:
		results, err = p.client.Search(ctx, "ytsearch", query, p.limit)
	}
	if err != nil {
		return nil, err
	}

	candidates := make([]provider.MediaCandidate, 0, len(results))
	for _, info := range results {
		candidate, ok := p.toCandidate(info)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
		if len(candidates) >= p.limit {
			break
		}
	}

	p.enrichDurations(ctx, candidates)
	return candidates, nil
}

// probeDirectID attempts to directly resolve the track if its SourceID is a valid YouTube video ID.
func (p *MediaProvider) probeDirectID(ctx context.Context, track music.Track) (provider.MediaCandidate, bool) {
	sourceProvider := strings.TrimSpace(track.SourceProvider)
	if sourceProvider != "ytmusic" && sourceProvider != "youtube" {
		return provider.MediaCandidate{}, false
	}
	sourceID := strings.TrimSpace(track.SourceID)
	if !isYouTubeVideoID(sourceID) {
		return provider.MediaCandidate{}, false
	}

	target := watchURL(sourceID)
	results, err := p.client.Query(ctx, target, "--no-playlist")
	if err != nil || len(results) == 0 {
		return provider.MediaCandidate{}, false
	}

	candidate, ok := p.toCandidate(results[0])
	if !ok {
		return provider.MediaCandidate{}, false
	}

	// Validate plausibility: if track duration is known and candidate duration is known,
	// ensure they don't deviate wildly (e.g. max 15 seconds)
	if track.DurationMS > 0 && candidate.DurationMS > 0 {
		diff := track.DurationMS - candidate.DurationMS
		if diff < 0 {
			diff = -diff
		}
		if diff > 15000 {
			return provider.MediaCandidate{}, false
		}
	}

	return candidate, true
}

// Resolve turns a candidate into a downloadable source including its formats.
func (p *MediaProvider) Resolve(ctx context.Context, candidate provider.MediaCandidate) (*provider.MediaSource, error) {
	target := strings.TrimSpace(candidate.URL)
	if target == "" && candidate.ID != "" {
		target = watchURL(candidate.ID)
	}
	if err := ValidateMediaURL(target); err != nil {
		return nil, err
	}

	results, err := p.client.Query(ctx, target, "--no-playlist")
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, apperr.Newf(apperr.CodeTrackNotFound, "No media could be resolved for %q.", target)
	}
	info := results[0]

	source := &provider.MediaSource{
		Provider:   p.name,
		ID:         info.ID,
		URL:        info.PageURL(),
		Title:      info.DisplayTitle(),
		Uploader:   info.UploaderName(),
		DurationMS: info.DurationMS(),
	}
	for _, format := range info.AudioFormats() {
		source.Formats = append(source.Formats, provider.AudioFormat{
			ID:          format.FormatID,
			Codec:       format.ACodec,
			Container:   containerOf(format),
			BitrateKbps: format.Bitrate(),
			SampleRate:  format.ASR,
			Channels:    format.AudioChannels,
			Filesize:    format.Size(),
		})
	}
	if len(source.Formats) == 0 {
		return nil, apperr.Newf(apperr.CodeDownloadFailed,
			"The media item %q offers no audio only stream.", source.ID)
	}
	return source, nil
}

// toCandidate maps one yt-dlp result onto a candidate. Entries that cannot
// carry a normal track — live streams and items without an id — are dropped.
func (p *MediaProvider) toCandidate(info ytdlp.Info) (provider.MediaCandidate, bool) {
	if strings.TrimSpace(info.ID) == "" {
		return provider.MediaCandidate{}, false
	}
	if info.IsLive || info.LiveStatus == "is_live" || info.LiveStatus == "is_upcoming" {
		return provider.MediaCandidate{}, false
	}
	if strings.HasPrefix(strings.TrimSpace(info.ID), "UC") {
		return provider.MediaCandidate{}, false
	}

	pageURL := info.PageURL()
	if isChannelOrPlaylistURL(pageURL) || ValidateMediaURL(pageURL) != nil {
		return provider.MediaCandidate{}, false
	}

	title := info.DisplayTitle()
	artists := splitArtists(info.DisplayArtist())
	uploader := StripTopicSuffix(info.UploaderName())
	if len(artists) == 0 {
		// A general video title usually reads "Artist - Title"; splitting it
		// gives the matcher something to compare the credits against.
		if artist, rest, ok := splitArtistTitle(info.Title); ok {
			artists = splitArtists(artist)
			if strings.TrimSpace(info.Track) == "" {
				title = rest
			}
		}
	}
	if len(artists) == 0 && uploader != "" && uploader != info.UploaderName() {
		// An auto generated "<Artist> - Topic" channel names the artist
		// directly, which is more reliable than any title heuristic.
		artists = []string{uploader}
	}

	return provider.MediaCandidate{
		Provider:       p.name,
		ID:             info.ID,
		URL:            pageURL,
		Title:          title,
		Artists:        artists,
		Album:          strings.TrimSpace(info.Album),
		DurationMS:     info.DurationMS(),
		Uploader:       uploader,
		IsMusicService: p.musicService,
	}, true
}

// enrichDurations looks up the runtime of the first candidates that came back
// without one. A candidate whose runtime is unknown cannot be judged reliably,
// and the flat search result of some platforms omits it.
func (p *MediaProvider) enrichDurations(ctx context.Context, candidates []provider.MediaCandidate) {
	if p.enrichLimit <= 0 {
		return
	}

	targets := make([]string, 0, p.enrichLimit)
	index := make(map[string]int, p.enrichLimit)
	for i := range candidates {
		if candidates[i].DurationMS > 0 {
			continue
		}
		if !isWatchURL(candidates[i].URL) {
			continue
		}
		targets = append(targets, candidates[i].URL)
		index[candidates[i].ID] = i
		if len(targets) >= p.enrichLimit {
			break
		}
	}
	if len(targets) == 0 {
		return
	}

	for _, target := range targets {
		results, err := p.client.Query(ctx, target, "--no-playlist")
		if err != nil || len(results) == 0 {
			// Enrichment is best effort: a candidate that stays without a
			// runtime is simply scored more cautiously.
			continue
		}
		info := results[0]
		i, ok := index[info.ID]
		if !ok {
			continue
		}
		if duration := info.DurationMS(); duration > 0 {
			candidates[i].DurationMS = duration
		}
		if album := strings.TrimSpace(info.Album); album != "" && candidates[i].Album == "" {
			candidates[i].Album = album
		}
		if artists := splitArtists(info.DisplayArtist()); len(artists) > 0 && len(candidates[i].Artists) == 0 {
			candidates[i].Artists = artists
		}
	}
}

// ValidateMediaURL checks that a URL points at a supported platform.
func ValidateMediaURL(raw string) error {
	parsed, err := httpx.ValidateURL(raw)
	if err != nil {
		return err
	}
	if !httpx.HasHostSuffix(parsed, allowedHosts...) {
		return apperr.Newf(apperr.CodeInvalidRequest,
			"The host %q is not a supported media source.", parsed.Hostname())
	}
	return nil
}

// musicSearchURL builds the YouTube Music search page URL for a query.
func musicSearchURL(query string) string {
	return "https://music.youtube.com/search?q=" + url.QueryEscape(query) + "&sp=" + musicSongsFilter
}

func watchURL(id string) string {
	return "https://www.youtube.com/watch?v=" + url.QueryEscape(id)
}

// containerOf returns the container of a format.
func containerOf(format ytdlp.Format) string {
	if c := strings.TrimSpace(format.Container); c != "" {
		return strings.TrimSuffix(c, "_dash")
	}
	return strings.TrimSpace(format.Ext)
}
