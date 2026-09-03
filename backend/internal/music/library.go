package music

import "time"

// LibraryTrack represents a track in the library along with its physical file details.
type LibraryTrack struct {
	Track
	FilePath      string    `json:"file_path,omitempty"`
	FileSizeBytes int64     `json:"file_size_bytes,omitempty"`
	Codec         string    `json:"codec,omitempty"`
	BitrateKbps   float64   `json:"bitrate_kbps,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// LibraryTrackDetail represents a single track with full file, album, and lyrics information.
type LibraryTrackDetail struct {
	Track      Track    `json:"track"`
	File       *File    `json:"file,omitempty"`
	Release    *Release `json:"release,omitempty"`
	Artist     *Artist  `json:"artist,omitempty"`
	LyricsPath string   `json:"lyrics_path,omitempty"`
}

// LibraryRelease represents an album, EP, or single with library-level summary stats.
type LibraryRelease struct {
	Release
	TrackCountInLibrary int       `json:"track_count_in_library"`
	TotalSizeBytes      int64     `json:"total_size_bytes"`
	CreatedAt           time.Time `json:"created_at"`
}

// LibraryReleaseDetail represents a release with its tracks and artist information.
type LibraryReleaseDetail struct {
	Release        Release        `json:"release"`
	Artist         *Artist        `json:"artist,omitempty"`
	Tracks         []LibraryTrack `json:"tracks"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
}

// LibraryArtist represents an artist in the library with summary counts.
type LibraryArtist struct {
	Artist
	ReleaseCount   int       `json:"release_count"`
	TrackCount     int       `json:"track_count"`
	TotalSizeBytes int64     `json:"total_size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

// LibraryArtistDetail represents an artist with their local releases and tracks.
type LibraryArtistDetail struct {
	Artist         Artist           `json:"artist"`
	Releases       []LibraryRelease `json:"releases"`
	Tracks         []LibraryTrack   `json:"tracks"`
	ReleaseCount   int              `json:"release_count"`
	TrackCount     int              `json:"track_count"`
	TotalSizeBytes int64            `json:"total_size_bytes"`
	Subscribed     bool             `json:"subscribed"`
	SubscriptionID string           `json:"subscription_id,omitempty"`
}

// LibrarySearchResults groups the top search results for the library search.
type LibrarySearchResults struct {
	Artists  []LibraryArtist  `json:"artists"`
	Releases []LibraryRelease `json:"releases"`
	Tracks   []LibraryTrack   `json:"tracks"`
}
