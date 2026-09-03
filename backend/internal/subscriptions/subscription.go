// Package subscriptions keeps track of the artists whose catalogue should be
// watched, and synchronises that catalogue against the library.
//
// Nothing in here resolves a provider, deduplicates a track or downloads a
// file itself: the discography service, the catalogue repository and the job
// manager already do those things, and the sync composes them.
package subscriptions

import (
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// SyncStatus is the outcome of the last synchronisation.
//
// There is deliberately no "running" value. A run only exists inside the
// process that started it, so a crash would leave a persisted "running" behind
// that nothing ever clears; whether a sync is active right now is answered by
// the service, not by the database.
type SyncStatus string

const (
	// StatusPending marks a subscription that has never been synchronised.
	StatusPending SyncStatus = "pending"
	// StatusSuccess marks a run in which every release could be read.
	StatusSuccess SyncStatus = "success"
	// StatusPartial marks a run that finished but had to skip a release.
	StatusPartial SyncStatus = "partial"
	// StatusFailed marks a run that did not finish.
	StatusFailed SyncStatus = "failed"
)

// Valid reports whether s is a known sync status.
func (s SyncStatus) Valid() bool {
	switch s {
	case StatusPending, StatusSuccess, StatusPartial, StatusFailed:
		return true
	default:
		return false
	}
}

// Subscription is a watched artist.
//
// Provider and ArtistSourceID are the same identity tuple the artists table
// uses, but they are stored here rather than referenced: a subscription must
// be possible for an artist the library does not own yet, and forcing an
// artists row for that would make an artist with no music appear in the
// library listing.
type Subscription struct {
	ID string `json:"id"`

	Provider       string `json:"provider"`
	ArtistSourceID string `json:"artist_source_id"`
	ArtistName     string `json:"artist_name"`
	ArtistImageURL string `json:"artist_image_url,omitempty"`

	Enabled      bool `json:"enabled"`
	AutoDownload bool `json:"auto_download"`

	ReleaseFilter    music.ReleaseFilter `json:"release_filter"`
	DownloadPriority jobs.Priority       `json:"download_priority"`

	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
	NextSyncAt     time.Time  `json:"next_sync_at"`
	LastSyncStatus SyncStatus `json:"last_sync_status"`
	LastError      string     `json:"last_error,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Syncing reports whether this process is running a sync for the
	// subscription right now. It is filled in by the service and never stored.
	Syncing bool `json:"syncing"`
}

// DisplayName returns the artist name with the same fallback the rest of the
// backend uses.
func (s Subscription) DisplayName() string {
	if name := strings.TrimSpace(s.ArtistName); name != "" {
		return name
	}
	return music.UnknownArtist
}

// NewSubscription is the request to watch an artist.
type NewSubscription struct {
	Provider         string
	ArtistSourceID   string
	ArtistName       string
	ArtistImageURL   string
	Enabled          *bool
	AutoDownload     bool
	ReleaseFilter    *music.ReleaseFilter
	DownloadPriority *jobs.Priority
}

// Validate rejects a request that cannot identify an artist.
func (n NewSubscription) Validate() error {
	if strings.TrimSpace(n.Provider) == "" {
		return apperr.New(apperr.CodeInvalidRequest, "A metadata provider is required.")
	}
	if strings.TrimSpace(n.ArtistSourceID) == "" {
		return apperr.New(apperr.CodeInvalidRequest, "An artist id is required.")
	}
	if n.DownloadPriority != nil && !n.DownloadPriority.Valid() {
		return apperr.New(apperr.CodeInvalidRequest, "Invalid download priority.")
	}
	return nil
}

const (
	// ExportFormatName is the identifier for the JSON export format.
	ExportFormatName = "ytmdl-subscriptions"
	// ExportFormatVersion is the schema version of the export format.
	ExportFormatVersion = 1
	// MaxImportItems is the maximum number of subscriptions that may be processed in a single import.
	MaxImportItems = 5000
)

// ExportSubscription is the portable serialization format for a single subscription.
type ExportSubscription struct {
	ArtistName       string              `json:"artist_name"`
	Provider         string              `json:"provider"`
	ArtistSourceID   string              `json:"artist_source_id"`
	ArtistImageURL   string              `json:"artist_image_url,omitempty"`
	Enabled          bool                `json:"enabled"`
	AutoDownload     bool                `json:"auto_download"`
	ReleaseFilter    music.ReleaseFilter `json:"release_filter"`
	DownloadPriority jobs.Priority       `json:"download_priority"`
}

// ExportPayload represents the versioned export file structure.
type ExportPayload struct {
	Format        string               `json:"format"`
	Version       int                  `json:"version"`
	ExportedAt    time.Time            `json:"exported_at"`
	Subscriptions []ExportSubscription `json:"subscriptions"`
}

// ImportItemStatus classifies what would happen to an imported item.
type ImportItemStatus string

const (
	ImportStatusNew         ImportItemStatus = "new"
	ImportStatusWouldUpdate ImportItemStatus = "would_update"
	ImportStatusUnchanged   ImportItemStatus = "unchanged"
	ImportStatusInvalid     ImportItemStatus = "invalid"
	ImportStatusDuplicate   ImportItemStatus = "duplicate"
)

// ImportPreviewItem describes the evaluation of a single imported subscription.
type ImportPreviewItem struct {
	Index            int                 `json:"index"`
	ArtistName       string              `json:"artist_name"`
	Provider         string              `json:"provider"`
	ArtistSourceID   string              `json:"artist_source_id"`
	ArtistImageURL   string              `json:"artist_image_url,omitempty"`
	Enabled          bool                `json:"enabled"`
	AutoDownload     bool                `json:"auto_download"`
	ReleaseFilter    music.ReleaseFilter `json:"release_filter"`
	DownloadPriority jobs.Priority       `json:"download_priority"`
	Status           ImportItemStatus    `json:"status"`
	ExistingID       string              `json:"existing_id,omitempty"`
	Changes          []string            `json:"changes,omitempty"`
	Error            string              `json:"error,omitempty"`
}

// ImportPreview contains summary statistics and item breakdown for an import before application.
type ImportPreview struct {
	Total       int                 `json:"total"`
	New         int                 `json:"new"`
	Existing    int                 `json:"existing"`
	WouldUpdate int                 `json:"would_update"`
	Unchanged   int                 `json:"unchanged"`
	Invalid     int                 `json:"invalid"`
	Duplicates  int                 `json:"duplicates"`
	Warnings    []string            `json:"warnings,omitempty"`
	Items       []ImportPreviewItem `json:"items"`
}

// ImportError records an error for a specific imported subscription.
type ImportError struct {
	Index          int    `json:"index"`
	ArtistName     string `json:"artist_name,omitempty"`
	Provider       string `json:"provider,omitempty"`
	ArtistSourceID string `json:"artist_source_id,omitempty"`
	Error          string `json:"error"`
}

// ImportResult describes the outcome of applying an import.
type ImportResult struct {
	Created   int           `json:"created"`
	Updated   int           `json:"updated"`
	Unchanged int           `json:"unchanged"`
	Failed    int           `json:"failed"`
	Errors    []ImportError `json:"errors,omitempty"`
}

// ImportUpdate carries the fields needed to update an existing subscription from an import.
type ImportUpdate struct {
	ID               string
	ArtistName       string
	ArtistImageURL   string
	Enabled          bool
	AutoDownload     bool
	ReleaseFilter    music.ReleaseFilter
	DownloadPriority jobs.Priority
}

// Update carries the fields a PATCH may change. A nil field is left as it is.
type Update struct {
	Enabled          *bool
	AutoDownload     *bool
	ReleaseFilter    *music.ReleaseFilter
	DownloadPriority *jobs.Priority
}

// Empty reports whether the update would change nothing.
func (u Update) Empty() bool {
	return u.Enabled == nil && u.AutoDownload == nil && u.ReleaseFilter == nil && u.DownloadPriority == nil
}

// ListFilter narrows a subscription listing. Provider and ArtistSourceID
// together answer the artist page's question — "is this artist watched?" —
// without a second endpoint for it.
type ListFilter struct {
	Provider       string
	ArtistSourceID string
	Limit          int
	Offset         int
}

// SyncResult is the report of one synchronisation.
//
// It carries counts and warnings only. The resolved catalogue itself stays
// where it belongs — a sync answer must not grow with the size of a
// discography.
type SyncResult struct {
	SubscriptionID string `json:"subscription_id"`
	Artist         string `json:"artist"`

	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
	Status     SyncStatus `json:"status"`

	ReleasesSeen int `json:"releases_seen"`
	NewReleases  int `json:"new_releases"`

	// TracksSeen counts the distinct recordings the deduplication produced,
	// which is the set the classification below was run against.
	TracksSeen int `json:"tracks_seen"`
	NewTracks  int `json:"new_tracks"`

	// QueuedTracks counts the recordings handed to the download queue, and
	// SkippedTracks the ones the library already holds as a file.
	QueuedTracks  int `json:"queued_tracks"`
	SkippedTracks int `json:"skipped_tracks"`

	Warnings []string `json:"warnings,omitempty"`
}

// Duration returns how long the run took.
func (r SyncResult) Duration() time.Duration { return r.FinishedAt.Sub(r.StartedAt) }
