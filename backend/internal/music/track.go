package music

import (
	"strings"
	"time"
)

// Track is a normalised track. It carries everything needed for matching,
// tagging and library placement.
type Track struct {
	ID string `json:"id"`

	Title   string   `json:"title"`
	Artists []string `json:"artists"`

	Album       string `json:"album"`
	AlbumArtist string `json:"album_artist"`

	TrackNumber int `json:"track_number"`
	TrackTotal  int `json:"track_total"`

	DiscNumber int `json:"disc_number"`
	DiscTotal  int `json:"disc_total"`

	DurationMS int `json:"duration_ms"`
	Year       int `json:"year"`

	ISRC string `json:"isrc,omitempty"`

	// Compilation mirrors the release flag onto the recording, because the
	// tagger only ever sees a track.
	Compilation bool `json:"compilation,omitempty"`

	// The lyrics *state* belongs to the recording; the lyrics *text* does not.
	// It lives in the sidecar file next to the audio, which is what Plex,
	// Jellyfin and Emby read.
	LyricsState     LyricsState `json:"lyrics_state,omitempty"`
	LyricsProvider  string      `json:"lyrics_provider,omitempty"`
	LyricsCheckedAt *time.Time  `json:"lyrics_checked_at,omitempty"`

	CoverURL string `json:"cover_url,omitempty"`

	SourceProvider string `json:"source_provider"`
	SourceID       string `json:"source_id"`
	SourceURL      string `json:"source_url"`

	// ReleaseID links the track back to the release it was resolved from.
	ReleaseID   string      `json:"release_id,omitempty"`
	ReleaseType ReleaseType `json:"release_type,omitempty"`
}

// DisplayTitle returns the trimmed title with a safe fallback.
func (t Track) DisplayTitle() string {
	if s := strings.TrimSpace(t.Title); s != "" {
		return s
	}
	return "Unknown Title"
}

// DisplayArtist returns the primary credited artist.
func (t Track) DisplayArtist() string {
	return PrimaryArtist(t.Artists)
}

// DisplayAlbumArtist returns the album artist with a fallback to the primary
// track artist.
func (t Track) DisplayAlbumArtist() string {
	if a := strings.TrimSpace(t.AlbumArtist); a != "" {
		return a
	}
	return t.DisplayArtist()
}

// Label renders "Artist - Title", the form used in logs and SSE events.
func (t Track) Label() string {
	return t.DisplayArtist() + " - " + t.DisplayTitle()
}

// DurationSeconds returns the duration rounded to whole seconds.
func (t Track) DurationSeconds() int {
	return (t.DurationMS + 500) / 1000
}

// Source describes one external origin of an internal track. A single track
// can be known to several providers at once.
type Source struct {
	ID        string `json:"id"`
	TrackID   string `json:"track_id"`
	Provider  string `json:"provider"`
	SourceID  string `json:"source_id"`
	SourceURL string `json:"source_url,omitempty"`
	// Kind separates metadata origins from media origins.
	Kind SourceKind `json:"kind"`
}

// SourceKind distinguishes the role a source plays for a track.
type SourceKind string

const (
	// SourceMetadata is a provider the track's metadata came from.
	SourceMetadata SourceKind = "metadata"
	// SourceMedia is a provider an audio stream was resolved from.
	SourceMedia SourceKind = "media"
)

// DisplayLyricsState returns the recorded state, normalising an unset or
// unrecognised value to LyricsUnknown so callers never have to guard it.
func (t Track) DisplayLyricsState() LyricsState {
	if ValidLyricsState(string(t.LyricsState)) {
		return t.LyricsState
	}
	return LyricsUnknown
}
