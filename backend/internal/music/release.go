package music

import "strings"

// ReleaseType classifies a release. Providers use their own vocabulary; it is
// mapped onto these values by NormaliseReleaseType.
type ReleaseType string

const (
	ReleaseAlbum       ReleaseType = "album"
	ReleaseSingle      ReleaseType = "single"
	ReleaseEP          ReleaseType = "ep"
	ReleaseLive        ReleaseType = "live"
	ReleaseCompilation ReleaseType = "compilation"
	ReleaseRemix       ReleaseType = "remix"
)

// AllReleaseTypes lists every supported release type.
func AllReleaseTypes() []ReleaseType {
	return []ReleaseType{
		ReleaseAlbum, ReleaseSingle, ReleaseEP,
		ReleaseLive, ReleaseCompilation, ReleaseRemix,
	}
}

// Valid reports whether t is a known release type.
func (t ReleaseType) Valid() bool {
	switch t {
	case ReleaseAlbum, ReleaseSingle, ReleaseEP, ReleaseLive, ReleaseCompilation, ReleaseRemix:
		return true
	default:
		return false
	}
}

// Release is a normalised album, single, EP or other release.
type Release struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Artists     []string `json:"artists"`
	AlbumArtist string   `json:"album_artist"`

	ReleaseType ReleaseType `json:"release_type"`

	// Compilation marks a release whose tracks are by different artists and
	// that therefore belongs under "Various Artists" in a media server.
	Compilation bool `json:"compilation,omitempty"`

	Year       int `json:"year"`
	TrackCount int `json:"track_count"`

	CoverURL string `json:"cover_url,omitempty"`

	Provider  string `json:"provider"`
	SourceID  string `json:"source_id"`
	SourceURL string `json:"source_url"`

	// ReleaseDate keeps the provider's full date string when available
	// (YYYY, YYYY-MM or YYYY-MM-DD). Year is derived from it.
	ReleaseDate string `json:"release_date,omitempty"`
}

// DisplayTitle returns the trimmed release title with a safe fallback.
func (r Release) DisplayTitle() string {
	if t := strings.TrimSpace(r.Title); t != "" {
		return t
	}
	return "Unknown Album"
}

// DisplayAlbumArtist returns the album artist, falling back to the first
// credited artist.
func (r Release) DisplayAlbumArtist() string {
	if a := strings.TrimSpace(r.AlbumArtist); a != "" {
		return a
	}
	return PrimaryArtist(r.Artists)
}

// ReleaseFilter selects which release types a discography request covers.
type ReleaseFilter struct {
	Albums       bool `json:"albums"`
	Singles      bool `json:"singles"`
	EPs          bool `json:"eps"`
	Live         bool `json:"live"`
	Compilations bool `json:"compilations"`
	Remixes      bool `json:"remixes"`
}

// DefaultReleaseFilter is the filter used when a request does not specify one:
// the primary catalogue without secondary or derivative releases.
func DefaultReleaseFilter() ReleaseFilter {
	return ReleaseFilter{Albums: true, Singles: true, EPs: true}
}

// Allows reports whether the filter includes the given release type.
func (f ReleaseFilter) Allows(t ReleaseType) bool {
	switch t {
	case ReleaseAlbum:
		return f.Albums
	case ReleaseSingle:
		return f.Singles
	case ReleaseEP:
		return f.EPs
	case ReleaseLive:
		return f.Live
	case ReleaseCompilation:
		return f.Compilations
	case ReleaseRemix:
		return f.Remixes
	default:
		return false
	}
}

// Any reports whether the filter selects at least one release type.
func (f ReleaseFilter) Any() bool {
	for _, t := range AllReleaseTypes() {
		if f.Allows(t) {
			return true
		}
	}
	return false
}

// FilterReleases returns the releases whose type the filter allows.
func FilterReleases(releases []Release, filter ReleaseFilter) []Release {
	out := make([]Release, 0, len(releases))
	for _, r := range releases {
		if filter.Allows(r.ReleaseType) {
			out = append(out, r)
		}
	}
	return out
}
