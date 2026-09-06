package provider

import (
	"context"
	"strings"

	"ytdm/backend/internal/music"
)

// MediaCandidate is one possible audio source for a wanted track. Candidates
// are scored by the matching engine before anything is downloaded.
type MediaCandidate struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`

	Title   string   `json:"title"`
	Artists []string `json:"artists,omitempty"`
	Album   string   `json:"album,omitempty"`

	DurationMS int    `json:"duration_ms"`
	ISRC       string `json:"isrc,omitempty"`

	// Uploader is the channel or account that published the item. It is used
	// as a fallback artist when no structured credit is available.
	Uploader string `json:"uploader,omitempty"`

	// IsMusicService reports whether the candidate comes from a dedicated
	// music catalogue rather than a general video platform.
	IsMusicService bool `json:"is_music_service"`
}

// Label renders a candidate for logs and API responses.
func (c MediaCandidate) Label() string {
	artist := music.PrimaryArtist(c.Artists)
	if artist == music.UnknownArtist && c.Uploader != "" {
		artist = c.Uploader
	}
	return artist + " - " + c.Title
}

// AudioFormat describes one audio stream offered by a media source.
type AudioFormat struct {
	ID          string  `json:"id"`
	Codec       string  `json:"codec"`
	Container   string  `json:"container"`
	BitrateKbps float64 `json:"bitrate_kbps"`
	SampleRate  int     `json:"sample_rate"`
	Channels    int     `json:"channels"`
	Filesize    int64   `json:"filesize"`
}

// IsOpus reports whether the format carries a native Opus stream.
func (f AudioFormat) IsOpus() bool {
	return strings.HasPrefix(strings.ToLower(f.Codec), "opus")
}

// MediaSource is a resolved, downloadable audio source.
type MediaSource struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`

	Title      string `json:"title"`
	Uploader   string `json:"uploader,omitempty"`
	DurationMS int    `json:"duration_ms"`

	// Formats lists the audio formats the source offers. It may be empty when
	// the provider does not enumerate formats up front; the downloader then
	// falls back to its own format selection.
	Formats []AudioFormat `json:"formats,omitempty"`

	// SessionID is the opaque identifier of the session used to resolve this source,
	// allowing the downloader to use the affine session without holding a control-plane lease.
	SessionID string `json:"session_id,omitempty"`
}

// MediaProvider finds and resolves audio sources for a wanted track.
type MediaProvider interface {
	// Name returns the stable provider identifier, e.g. "ytmusic".
	Name() string

	// Search returns candidates that might carry the wanted track.
	Search(ctx context.Context, track music.Track) ([]MediaCandidate, error)

	// Resolve turns a candidate into a concrete, downloadable source.
	Resolve(ctx context.Context, candidate MediaCandidate) (*MediaSource, error)
}

// SearchQuery builds the free text query used to look for a track on a media
// platform.
func SearchQuery(track music.Track) string {
	parts := make([]string, 0, 2)
	if artist := music.PrimaryArtist(track.Artists); artist != music.UnknownArtist {
		parts = append(parts, artist)
	}
	if title := strings.TrimSpace(track.Title); title != "" {
		parts = append(parts, title)
	}
	return strings.Join(parts, " ")
}
