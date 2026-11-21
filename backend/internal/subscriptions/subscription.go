// Package subscriptions keeps track of the artists whose catalogue should be
// watched, and synchronises that catalogue against the library.
//
// Nothing in here resolves a provider, deduplicates a track or downloads a
// file itself: the discography service, the catalogue repository and the job
// manager already do those things, and the sync composes them.
package subscriptions

import (
	"encoding/json"
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

	// IncludeAppearances is this subscription's own choice about the releases
	// whose primary artist is someone else — a single the artist is featured
	// on, another artist's remix package. Nil follows the server-wide setting
	// (settings: subscription_include_appearances); true or false is an
	// exception for this artist. When the effective value is false, a sync
	// considers only the artist's own releases (joint releases included); a
	// release whose primary artist cannot be determined is still kept.
	IncludeAppearances *bool `json:"include_appearances"`
	// EffectiveIncludeAppearances is what applies right now: the subscription's
	// own value when it has one, otherwise the server-wide setting. It is
	// filled in by the service and never stored.
	EffectiveIncludeAppearances bool `json:"effective_include_appearances"`

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
	// IncludeAppearances is the subscription's own appearance choice; nil
	// follows the server-wide setting.
	IncludeAppearances *bool
}

// ResolveIncludeAppearances returns the appearance switch that applies to a
// subscription: its own value when set, otherwise the server-wide one.
func ResolveIncludeAppearances(own *bool, global bool) bool {
	if own != nil {
		return *own
	}
	return global
}

// NullableBool is a JSON field that tells three states apart: absent (Set is
// false), explicit null (Set is true, Value is nil) and true or false. A PATCH
// uses it to put a per-subscription choice back to "follow the setting".
type NullableBool struct {
	Set   bool
	Value *bool
}

// UnmarshalJSON records that the field was present; a JSON null leaves Value
// nil. encoding/json calls it for null too because NullableBool is not a
// pointer.
func (n *NullableBool) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
		n.Value = nil
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	n.Value = &value
	return nil
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
	// IncludeAppearances is written only when the subscription has its own
	// choice. Absent or null on import means "follow the server-wide setting",
	// which is also how a file written before the field existed reads.
	IncludeAppearances *bool `json:"include_appearances,omitempty"`
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
	Index              int                 `json:"index"`
	ArtistName         string              `json:"artist_name"`
	Provider           string              `json:"provider"`
	ArtistSourceID     string              `json:"artist_source_id"`
	ArtistImageURL     string              `json:"artist_image_url,omitempty"`
	Enabled            bool                `json:"enabled"`
	AutoDownload       bool                `json:"auto_download"`
	ReleaseFilter      music.ReleaseFilter `json:"release_filter"`
	DownloadPriority   jobs.Priority       `json:"download_priority"`
	IncludeAppearances *bool               `json:"include_appearances"`
	Status             ImportItemStatus    `json:"status"`
	ExistingID         string              `json:"existing_id,omitempty"`
	Changes            []string            `json:"changes,omitempty"`
	Error              string              `json:"error,omitempty"`
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
	// IncludeAppearances replaces the stored choice; nil follows the setting.
	IncludeAppearances *bool
}

// Update carries the fields a PATCH may change. A nil field is left as it is.
type Update struct {
	Enabled          *bool
	AutoDownload     *bool
	ReleaseFilter    *music.ReleaseFilter
	DownloadPriority *jobs.Priority
	// IncludeAppearances is left as it is unless Set; Set with a nil Value
	// puts the subscription back to following the server-wide setting.
	IncludeAppearances NullableBool
}

// Empty reports whether the update would change nothing.
func (u Update) Empty() bool {
	return u.Enabled == nil && u.AutoDownload == nil && u.ReleaseFilter == nil &&
		u.DownloadPriority == nil && !u.IncludeAppearances.Set
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
	// AppearancesSkipped counts the releases left out because the
	// subscription excludes appearances.
	AppearancesSkipped int `json:"appearances_skipped,omitempty"`

	// TracksSeen counts the distinct recordings the deduplication produced,
	// which is the set the classification below was run against.
	TracksSeen int `json:"tracks_seen"`
	NewTracks  int `json:"new_tracks"`

	// UnmatchedTracks counts the recordings left alone in this run because
	// the last download found no media for them and the wait before the next
	// attempt has not passed. They are neither new nor queued.
	UnmatchedTracks int `json:"unmatched_tracks,omitempty"`

	// QueuedTracks counts the recordings handed to the download queue, and
	// SkippedTracks the ones the library already holds as a file.
	QueuedTracks  int `json:"queued_tracks"`
	SkippedTracks int `json:"skipped_tracks"`

	// CopiesQueued counts the appearances of recordings the library already
	// holds (and counts under SkippedTracks) on releases the library does not
	// hold yet. Their release jobs place a copy of the existing file instead
	// of downloading, so they are neither new nor in QueuedTracks.
	CopiesQueued int `json:"copies_queued,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// Duration returns how long the run took.
func (r SyncResult) Duration() time.Duration { return r.FinishedAt.Sub(r.StartedAt) }
