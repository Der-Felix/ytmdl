package music

import "time"

// Placement is a further appearance of a recording on a release other than
// its home release. The recording stays one track; the placement is a copy of
// one of its library files, put into that release's folder and tagged with
// that release's album, disc and track number, so a media server shows the
// release complete.
//
// Placements are not versions of the recording. They are kept apart from File
// on purpose: playback, duplicate detection and the canonical-path audit work
// on files and must not mistake a placement for a second copy.
type Placement struct {
	ID        string `json:"id"`
	TrackID   string `json:"track_id"`
	ReleaseID string `json:"release_id"`

	// Path is relative to the library root, like File.Path.
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256,omitempty"`

	DiscNumber  int `json:"disc_number"`
	TrackNumber int `json:"track_number"`
	// Title is the title as the release lists it.
	Title string `json:"title"`

	// SourceFileID is the file the copy was made from.
	SourceFileID string `json:"source_file_id,omitempty"`

	// ReleaseProvider and ReleaseSourceID identify the release; they are
	// filled when a placement is read.
	ReleaseProvider string `json:"release_provider,omitempty"`
	ReleaseSourceID string `json:"release_source_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PlacementEntry is what placing a copy adds to the catalogue: the release the
// copy is placed on, its artist, and the placement itself. They are persisted
// as one unit, so a placement never points at a release that is not stored.
type PlacementEntry struct {
	// Artist is optional, as in LibraryEntry.
	Artist    *Artist
	Release   Release
	Placement Placement
}
