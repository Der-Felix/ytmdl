// Package jobs owns the download pipeline: job and item state, the persistent
// queue, the worker pool and the event stream.
package jobs

import (
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
)

// Type names what a job was created for.
type Type string

const (
	TypeArtist  Type = "artist"
	TypeRelease Type = "release"
	TypeTrack   Type = "track"
)

// Valid reports whether t is a known job type.
func (t Type) Valid() bool {
	switch t {
	case TypeArtist, TypeRelease, TypeTrack:
		return true
	default:
		return false
	}
}

// Status is the state of a whole job.
type Status string

const (
	StatusQueued            Status = "queued"
	StatusResolvingArtist   Status = "resolving_artist"
	StatusResolvingReleases Status = "resolving_releases"
	StatusResolvingTracks   Status = "resolving_tracks"
	StatusDeduplicating     Status = "deduplicating"
	StatusMatching          Status = "matching"
	StatusDownloading       Status = "downloading"
	StatusTagging           Status = "tagging"
	StatusFinalizing        Status = "finalizing"
	StatusRetryWait         Status = "retry_wait"
	StatusWaitingStorage    Status = "waiting_for_storage"
	StatusWaitingSpace      Status = "waiting_for_space"
	StatusCompleted         Status = "completed"
	StatusFailed            Status = "failed"
	StatusCancelled         Status = "cancelled"
)

// pipelineRank orders the non terminal job states. A job may only move
// forward through them.
var pipelineRank = map[Status]int{
	StatusQueued:            0,
	StatusResolvingArtist:   1,
	StatusResolvingReleases: 2,
	StatusResolvingTracks:   3,
	StatusDeduplicating:     4,
	StatusMatching:          5,
	StatusDownloading:       6,
	StatusTagging:           7,
	StatusFinalizing:        8,
	StatusRetryWait:         9,
	StatusWaitingStorage:    10,
	StatusWaitingSpace:      11,
}

// processingBand holds the states a job alternates between while its items are
// worked on concurrently. Within the band any order is legitimate, because one
// worker may still be downloading while another is already tagging.
var processingBand = map[Status]struct{}{
	StatusMatching:       {},
	StatusDownloading:    {},
	StatusTagging:        {},
	StatusFinalizing:     {},
	StatusRetryWait:      {},
	StatusWaitingStorage: {},
	StatusWaitingSpace:   {},
}

// resolvingBand holds the states of the catalogue resolution phase. They form
// their own band so that a job whose resolution was interrupted by a restart
// can run through the phase again.
var resolvingBand = map[Status]struct{}{
	StatusResolvingArtist:   {},
	StatusResolvingReleases: {},
	StatusResolvingTracks:   {},
	StatusDeduplicating:     {},
}

// Terminal reports whether the job has reached its final state.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a known job status.
func (s Status) Valid() bool {
	if s.Terminal() {
		return true
	}
	_, ok := pipelineRank[s]
	return ok
}

// CanTransitionTo reports whether the job may move from s to next. A job never
// leaves a terminal state and never moves backwards through the pipeline; the
// concurrent processing states are the documented exception.
func (s Status) CanTransitionTo(next Status) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	if s.Terminal() {
		return false
	}
	if next.Terminal() {
		return true
	}
	if sameBand(processingBand, s, next) || sameBand(resolvingBand, s, next) {
		return true
	}
	return pipelineRank[next] > pipelineRank[s]
}

// sameBand reports whether both states belong to the given band.
func sameBand(band map[Status]struct{}, a, b Status) bool {
	_, first := band[a]
	_, second := band[b]
	return first && second
}

// ItemStatus is the state of a single track inside a job.
type ItemStatus string

const (
	ItemPending        ItemStatus = "pending"
	ItemMatching       ItemStatus = "matching"
	ItemDownloading    ItemStatus = "downloading"
	ItemTagging        ItemStatus = "tagging"
	ItemFinalizing     ItemStatus = "finalizing"
	ItemRetryWait      ItemStatus = "retry_wait"
	ItemWaitingStorage ItemStatus = "waiting_for_storage"
	ItemWaitingSpace   ItemStatus = "waiting_for_space"
	ItemCompleted      ItemStatus = "completed"
	ItemFailed         ItemStatus = "failed"
	ItemSkipped        ItemStatus = "skipped"
	ItemCancelled      ItemStatus = "cancelled"
)

var itemRank = map[ItemStatus]int{
	ItemPending:        0,
	ItemMatching:       1,
	ItemDownloading:    2,
	ItemTagging:        3,
	ItemFinalizing:     4,
	ItemRetryWait:      5,
	ItemWaitingStorage: 6,
	ItemWaitingSpace:   7,
}

// Terminal reports whether the item has reached its final state.
func (s ItemStatus) Terminal() bool {
	switch s {
	case ItemCompleted, ItemFailed, ItemSkipped, ItemCancelled:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a known item status.
func (s ItemStatus) Valid() bool {
	if s.Terminal() {
		return true
	}
	_, ok := itemRank[s]
	return ok
}

// CanTransitionTo reports whether an item may move from s to next. Items are
// processed by workers, and can transition through matching, downloading,
// tagging, finalizing, retry_wait, waiting_for_storage, waiting_for_space, and terminal states.
func (s ItemStatus) CanTransitionTo(next ItemStatus) bool {
	if !s.Valid() || !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	if s.Terminal() {
		return false
	}
	if next.Terminal() {
		return true
	}
	// Active non-terminal states can transition between each other on retries/waits/steps
	return true
}

// Priority defines the scheduling importance of a job.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityVeryHigh Priority = "very_high"
	PriorityUrgent   Priority = "urgent" // alias for very_high
)

// Valid reports whether p is a known priority level.
func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityVeryHigh, PriorityUrgent:
		return true
	default:
		return false
	}
}

// Canonical returns the canonical Priority value.
func (p Priority) Canonical() Priority {
	if p == PriorityUrgent {
		return PriorityVeryHigh
	}
	return p
}

// Rank maps the priority to an integer rank for database indexing and fast comparison:
// 0 = low, 1 = normal, 2 = high, 3 = very_high.
func (p Priority) Rank() int {
	switch p {
	case PriorityLow:
		return 0
	case PriorityNormal:
		return 1
	case PriorityHigh:
		return 2
	case PriorityVeryHigh, PriorityUrgent:
		return 3
	default:
		return 1
	}
}

// PriorityFromRank maps an integer rank (0, 1, 2, 3) back to a Priority.
func PriorityFromRank(r int) Priority {
	switch r {
	case 0:
		return PriorityLow
	case 1:
		return PriorityNormal
	case 2:
		return PriorityHigh
	case 3:
		return PriorityVeryHigh
	default:
		return PriorityNormal
	}
}

// Origin defines how a download was initiated.
type Origin string

const (
	OriginManual       Origin = "manual"
	OriginSubscription Origin = "subscription"
)

// Valid reports whether o is a known origin.
func (o Origin) Valid() bool {
	switch o {
	case OriginManual, OriginSubscription:
		return true
	default:
		return false
	}
}

// ResolveOrigin reports the effective trigger origin of a job, including legacy
// jobs persisted before v0.26 whose options carry no explicit origin.
//
// Subscription automation has only ever queued release jobs: every background
// enqueue path runs through Manager.EnqueueReleaseWithPriority, which creates
// TypeRelease exclusively. Artist and track jobs can therefore only have been
// created by an operator request, and an untagged one is provably manual.
//
// An untagged release job is genuinely ambiguous - it may be either a manual
// release download or a pre-upgrade subscription enqueue - so it stays on the
// conservative subscription policy. That keeps a legacy background backlog from
// gaining automatic SoundCloud substitution after an upgrade, at the cost of
// old manual release jobs losing manual-only resilience until they are requeued.
func (j *Job) ResolveOrigin() Origin {
	if j == nil {
		return OriginSubscription
	}
	if j.Options.Origin.Valid() {
		return j.Options.Origin
	}
	switch j.Type {
	case TypeArtist, TypeTrack:
		return OriginManual
	default:
		return OriginSubscription
	}
}

// resolutionOrigin maps a job's effective origin onto the media resolution
// policy origin. Only work proven to be operator-initiated gets the manual
// policy, which allows independent providers to substitute for an unavailable
// preferred provider.
func resolutionOrigin(job Job) orchestrator.Origin {
	if job.ResolveOrigin() == OriginManual {
		return orchestrator.OriginManual
	}
	return orchestrator.OriginSubscription
}

// Options are the per job settings taken from the API request.
type Options struct {
	ReleaseFilter music.ReleaseFilter `json:"release_filter"`
	SkipExisting  bool                `json:"skip_existing"`
	// ReleaseID narrows a track job to the release the track belongs to. It is
	// needed for metadata providers that cannot resolve a single track id.
	ReleaseID string `json:"release_id,omitempty"`

	// Origin records whether the download was triggered manually by a user or by a subscription.
	Origin Origin `json:"origin,omitempty"`

	// Canonical provenance fields (Schema 9+).
	// When set, items generated by this job will attach their downloads
	// to this canonical artist rather than synthesizing a new artist entity.
	CanonicalArtistID string `json:"canonical_artist_id,omitempty"`
	ArtistSourceID    string `json:"artist_source_id,omitempty"`
	ArtistName        string `json:"artist_name,omitempty"`
}

// DefaultOptions returns the options used when a request omits them.
func DefaultOptions() Options {
	return Options{ReleaseFilter: music.DefaultReleaseFilter(), SkipExisting: true}
}

// Job is one download order, typically a complete artist.
type Job struct {
	ID       string   `json:"id"`
	Type     Type     `json:"type"`
	Status   Status   `json:"status"`
	Priority Priority `json:"priority"`
	Paused   bool     `json:"paused"`
	Label    string   `json:"label"`

	MetadataProvider string `json:"metadata_provider"`
	MediaProvider    string `json:"media_provider"`
	TargetID         string `json:"target_id"`

	Options Options `json:"options"`

	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Processed returns the number of items that reached a terminal state.
func (j Job) Processed() int { return j.Completed + j.Failed + j.Skipped }

// Item is one track inside a job.
type Item struct {
	ID       string     `json:"id"`
	JobID    string     `json:"job_id"`
	Position int        `json:"position"`
	Status   ItemStatus `json:"status"`

	TrackID string      `json:"track_id,omitempty"`
	Track   music.Track `json:"track"`
	Label   string      `json:"label"`

	MediaProvider string  `json:"media_provider,omitempty"`
	MediaID       string  `json:"media_id,omitempty"`
	MediaURL      string  `json:"media_url,omitempty"`
	MatchScore    float64 `json:"match_score"`

	FileID      string     `json:"file_id,omitempty"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`

	StagingRelPath string `json:"staging_relpath,omitempty"`
	StagedSize     int64  `json:"staged_size,omitempty"`
	StagedSHA256   string `json:"staged_sha256,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Summary is the closing report of a job.
type Summary struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// Summary builds the summary of a job.
func (j Job) Summary() Summary {
	return Summary{Total: j.Total, Completed: j.Completed, Failed: j.Failed, Skipped: j.Skipped}
}

// ItemUpdate carries the fields a worker changes on an item. Empty strings and
// zero values leave the stored value untouched, so a worker only has to name
// what it actually learned.
type ItemUpdate struct {
	Status         ItemStatus
	MediaProvider  string
	MediaID        string
	MediaURL       string
	MatchScore     float64
	FileID         string
	TrackID        string
	Attempts       *int
	MaxAttempts    *int
	NextRetryAt    *time.Time
	ClearNextRetry bool
	StagingRelPath *string
	StagedSize     *int64
	StagedSHA256   *string
	ErrorCode      string
	ErrorMessage   string
}

// DeriveParentStatus calculates the aggregate job status from its items.
func DeriveParentStatus(items []Item) Status {
	status, _, _ := DeriveParentStatusDetails(items)
	return status
}

// DeriveParentStatusDetails calculates the aggregate job status and any inherited
// error details from its items.
func DeriveParentStatusDetails(items []Item) (Status, string, string) {
	if len(items) == 0 {
		return StatusQueued, "", ""
	}

	var hasPending, hasMatching, hasDownloading, hasTagging, hasFinalizing bool
	var hasRetryWait, hasWaitingStorage, hasWaitingSpace bool
	var terminalCount, completedCount, failedCount, skippedCount, cancelledCount int

	for _, it := range items {
		switch it.Status {
		case ItemPending:
			hasPending = true
		case ItemMatching:
			hasMatching = true
		case ItemDownloading:
			hasDownloading = true
		case ItemTagging:
			hasTagging = true
		case ItemFinalizing:
			hasFinalizing = true
		case ItemRetryWait:
			hasRetryWait = true
		case ItemWaitingStorage:
			hasWaitingStorage = true
		case ItemWaitingSpace:
			hasWaitingSpace = true
		case ItemCompleted:
			terminalCount++
			completedCount++
		case ItemFailed:
			terminalCount++
			failedCount++
		case ItemSkipped:
			terminalCount++
			skippedCount++
		case ItemCancelled:
			terminalCount++
			cancelledCount++
		}
	}

	// If all items reached terminal states
	if terminalCount == len(items) {
		if completedCount > 0 || skippedCount == len(items) {
			return StatusCompleted, "", ""
		}
		if cancelledCount == len(items) {
			return StatusCancelled, "", ""
		}
		return StatusFailed, "", ""
	}

	// Active processing states precedence
	if hasDownloading {
		return StatusDownloading, "", ""
	}
	if hasMatching {
		return StatusMatching, "", ""
	}
	if hasTagging {
		return StatusTagging, "", ""
	}
	if hasFinalizing {
		return StatusFinalizing, "", ""
	}
	if hasWaitingStorage {
		return StatusWaitingStorage, "", ""
	}
	if hasWaitingSpace {
		return StatusWaitingSpace, "", ""
	}
	if hasRetryWait {
		// When active non-terminal items are in retry_wait, propagate SESSION_UNAVAILABLE
		// if all active items are waiting on a session/provider.
		hasSessionWait := false
		allSessionWait := true
		var errMsg string
		for _, it := range items {
			if !it.Status.Terminal() {
				if it.ErrorCode == string(apperr.CodeSessionUnavailable) {
					hasSessionWait = true
					if errMsg == "" {
						errMsg = it.ErrorMessage
					}
				} else {
					allSessionWait = false
				}
			}
		}
		if hasSessionWait && allSessionWait {
			if errMsg == "" {
				errMsg = "Provider vorübergehend nicht verfügbar"
			}
			return StatusRetryWait, string(apperr.CodeSessionUnavailable), errMsg
		}
		return StatusRetryWait, "", ""
	}
	if hasPending {
		return StatusQueued, "", ""
	}

	return StatusQueued, "", ""
}

// ListFilter narrows a job listing.
type ListFilter struct {
	Status   Status
	Type     Type
	Priority Priority
	Limit    int
	Offset   int
}
