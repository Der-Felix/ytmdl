package music

import "time"

// File is an audio file that exists in the library. It records what was
// actually written to disk, including the verified stream properties.
type File struct {
	ID      string `json:"id"`
	TrackID string `json:"track_id,omitempty"`

	// Path is relative to the library root.
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`

	Codec       string  `json:"codec"`
	Container   string  `json:"container"`
	BitrateKbps float64 `json:"bitrate_kbps"`
	SampleRate  int     `json:"sample_rate"`
	Channels    int     `json:"channels"`
	DurationMS  int     `json:"duration_ms"`

	SourceProvider string `json:"source_provider,omitempty"`
	SourceID       string `json:"source_id,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LibraryEntry bundles everything one finished download adds to the
// catalogue: the artist and release it belongs to, the recording itself, the
// external origins it was resolved from and the file that was written. It
// exists so that all of it can be persisted as one unit.
type LibraryEntry struct {
	// Artist and Release are optional; a track that carries no provider
	// identity for them is stored without the link.
	Artist  *Artist
	Release *Release
	Track   Track
	Sources []Source
	File    File
}

// StoredEntry reports the internal identifiers a LibraryEntry was written
// under, after deduplication against the existing catalogue.
type StoredEntry struct {
	ArtistID  string
	ReleaseID string
	TrackID   string
	File      File
}
