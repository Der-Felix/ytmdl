package library

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
)

// ReleaseResolver reads the track list of a release from its metadata
// provider. It is the resolution a release download runs, so a scan counts
// exactly the items a copies-only job for the release would get.
type ReleaseResolver interface {
	ResolveRelease(ctx context.Context, providerName, releaseID string) (*discography.Result, error)
}

// CopyPlanner is the job manager's side of completing releases: the decision
// a copies-only item makes, the job that makes it, and the setting for new
// downloads.
type CopyPlanner interface {
	CheckCopy(ctx context.Context, track music.Track) (jobs.CopyCheck, error)
	EnqueueCopies(ctx context.Context, metadataProvider, releaseID, label string) (bool, error)
	CompleteReleases() bool
}

// CompletionState is the lifecycle state of the scan for incomplete releases.
type CompletionState string

const (
	CompletionIdle      CompletionState = "idle"
	CompletionScanning  CompletionState = "scanning"
	CompletionScanned   CompletionState = "scanned"
	CompletionFailed    CompletionState = "failed"
	CompletionCancelled CompletionState = "cancelled"
)

// CompletionListLimit caps the releases a scan lists for display. The counts
// always cover every release, and applying queues all of them.
const CompletionListLimit = 200

// CompletionMaxAge is how long a finished scan may be applied. An older
// preview could misstate what the copies cost.
const CompletionMaxAge = 24 * time.Hour

// CompletionProgress counts the releases a running scan has looked up.
type CompletionProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// CompletionRelease is one release a backfill would add copies to.
type CompletionRelease struct {
	ReleaseID string `json:"release_id"`
	Artist    string `json:"artist"`
	Title     string `json:"title"`
	// Total is the number of distinct recordings the provider lists; Held,
	// Copies and Unknown add up to it.
	Total   int   `json:"total"`
	Held    int   `json:"held"`
	Copies  int   `json:"copies"`
	Unknown int   `json:"unknown"`
	Bytes   int64 `json:"bytes"`
}

// CompletionScan is the result of a finished scan.
type CompletionScan struct {
	// ReleasesExamined counts the releases whose track list was read.
	ReleasesExamined int `json:"releases_examined"`
	// ReleasesIncomplete counts the releases a backfill adds at least one
	// copy to. A release that only lacks recordings the library does not
	// have is not among them: copies cannot help it.
	ReleasesIncomplete int `json:"releases_incomplete"`
	// ReleasesWithoutSource counts the releases without a provider identity,
	// which cannot be looked up and are left out.
	ReleasesWithoutSource int `json:"releases_without_source"`
	// ReleasesFailed counts the releases whose lookup failed, by error code
	// in FailedByCode.
	ReleasesFailed int            `json:"releases_failed"`
	FailedByCode   map[string]int `json:"failed_by_code"`
	Copies         int            `json:"copies"`
	// Bytes is the size of the files the copies are made from. A copy is
	// retagged, so it differs from its source by the size of the tags.
	Bytes int64 `json:"bytes"`
	// HeldTracks and UnknownTracks count the listed recordings the releases
	// already hold, and those the library does not hold at all.
	HeldTracks    int `json:"held_tracks"`
	UnknownTracks int `json:"unknown_tracks"`
	// FreeBytes is the space available on the library volume when the scan
	// finished, if the storage could tell.
	FreeBytes         *int64              `json:"free_bytes,omitempty"`
	Releases          []CompletionRelease `json:"releases"`
	ReleasesTruncated bool                `json:"releases_truncated"`
}

// CompletionStatus is the state of completing releases as the API shows it.
type CompletionStatus struct {
	State CompletionState `json:"state"`
	// SettingEnabled is the complete_releases setting for new downloads. An
	// explicit backfill copies whatever it says.
	SettingEnabled bool               `json:"setting_enabled"`
	Progress       CompletionProgress `json:"progress"`
	StartedAt      *time.Time         `json:"started_at,omitempty"`
	FinishedAt     *time.Time         `json:"finished_at,omitempty"`
	ExpiresAt      *time.Time         `json:"expires_at,omitempty"`
	AppliedAt      *time.Time         `json:"applied_at,omitempty"`
	ErrorCode      string             `json:"error_code,omitempty"`
	Error          string             `json:"error,omitempty"`
	Result         *CompletionScan    `json:"result,omitempty"`
}

// CompletionApply reports what applying a scan queued.
type CompletionApply struct {
	QueuedJobs int `json:"queued_jobs"`
	// AlreadyQueued counts releases that already had an unfinished release
	// job; that job decides about their recordings.
	AlreadyQueued int   `json:"already_queued"`
	FailedJobs    int   `json:"failed_jobs"`
	Copies        int   `json:"copies"`
	Bytes         int64 `json:"bytes"`
}

// completionTarget is a release a finished scan found incomplete, with what
// applying needs to queue it.
type completionTarget struct {
	provider string
	sourceID string
	label    string
	copies   int
	bytes    int64
}

// completionState holds the one scan the service allows at a time and the
// last result. It lives in memory only: after a restart the state is idle
// and a new scan is needed, which is cheap enough not to persist.
type completionState struct {
	mu      sync.Mutex
	run     int
	status  CompletionStatus
	cancel  context.CancelFunc
	targets []completionTarget
}

// StartCompleteReleasesScan starts the read-only scan for releases a backfill
// could complete. It looks up every stored release with a provider identity
// at its metadata provider, so it runs in the background.
func (s *Service) StartCompleteReleasesScan() (*CompletionStatus, error) {
	if s.releases == nil || s.copies == nil {
		return nil, apperr.New(apperr.CodeInternal, "Completing releases is not available.")
	}
	if s.ctx.Err() != nil {
		return nil, apperr.New(apperr.CodeShuttingDown, "The library service is shutting down.")
	}
	// An unreachable library looks like one whose files are all gone; the
	// scan would report every recording as unknown.
	if guard := s.library.Guard(); guard != nil {
		if _, err := guard.ValidateIdentity(); err != nil {
			return nil, err
		}
	}

	c := &s.completion
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.State == CompletionScanning {
		return nil, apperr.New(apperr.CodeAlreadyExists, "A scan for incomplete releases is already running.")
	}

	ctx, cancel := context.WithCancel(s.ctx)
	started := time.Now().UTC()
	c.run++
	c.status = CompletionStatus{State: CompletionScanning, StartedAt: &started}
	c.cancel = cancel
	c.targets = nil

	run := c.run
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.runCompletionScan(ctx, run)
	}()

	status := s.completionSnapshot()
	return &status, nil
}

// CancelCompleteReleasesScan stops a running scan. It is recorded as
// cancelled at once; the lookups still in flight are discarded.
func (s *Service) CancelCompleteReleasesScan() (*CompletionStatus, error) {
	c := &s.completion
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.State != CompletionScanning {
		return nil, apperr.New(apperr.CodeInvalidRequest, "No scan for incomplete releases is running.")
	}
	c.cancel()
	// A later scan must not see the writes of this one.
	c.run++
	finished := time.Now().UTC()
	c.status.State = CompletionCancelled
	c.status.FinishedAt = &finished
	c.status.ErrorCode = string(apperr.CodeJobCancelled)
	c.status.Error = "The scan was cancelled."
	status := s.completionSnapshot()
	return &status, nil
}

// CompleteReleasesStatus returns the running scan or the last one.
func (s *Service) CompleteReleasesStatus() CompletionStatus {
	s.completion.mu.Lock()
	defer s.completion.mu.Unlock()
	return s.completionSnapshot()
}

// completionSnapshot copies the status. The caller holds the lock. A
// result is never changed once it is stored, so it is shared.
func (s *Service) completionSnapshot() CompletionStatus {
	status := s.completion.status
	if status.State == "" {
		status.State = CompletionIdle
	}
	if s.copies != nil {
		status.SettingEnabled = s.copies.CompleteReleases()
	}
	if status.State == CompletionScanned && status.FinishedAt != nil && status.AppliedAt == nil {
		expires := status.FinishedAt.Add(CompletionMaxAge)
		status.ExpiresAt = &expires
	}
	return status
}

// ApplyCompleteReleases queues one copies-only release job for every release
// the last finished scan found incomplete. Nothing is checked again here: the
// worker decides every item afresh, so a recording placed since the scan is
// skipped and a job queued twice copies nothing twice.
func (s *Service) ApplyCompleteReleases(ctx context.Context) (*CompletionApply, error) {
	if s.copies == nil {
		return nil, apperr.New(apperr.CodeInternal, "Completing releases is not available.")
	}
	c := &s.completion
	c.mu.Lock()
	defer c.mu.Unlock()

	switch {
	case c.status.State == CompletionScanning:
		return nil, apperr.New(apperr.CodeConflict, "A scan is still running; apply it once it has finished.")
	case c.status.State != CompletionScanned || c.status.FinishedAt == nil:
		return nil, apperr.New(apperr.CodeInvalidRequest, "There is no finished scan to apply; scan the library first.")
	case c.status.AppliedAt != nil:
		return nil, apperr.New(apperr.CodeConflict, "This scan was already applied; scan again to see what is still missing.")
	case time.Since(*c.status.FinishedAt) > CompletionMaxAge:
		return nil, apperr.New(apperr.CodeConflict, "The scan is older than 24 hours; scan again so the preview matches the library.")
	}

	out := &CompletionApply{}
	var firstErr error
	for _, target := range c.targets {
		queued, err := s.copies.EnqueueCopies(ctx, target.provider, target.sourceID, target.label)
		switch {
		case err != nil:
			out.FailedJobs++
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("copies for an incomplete release could not be queued",
				logging.KeyOperation, "complete_releases",
				logging.KeyRelease, target.label,
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
		case queued:
			out.QueuedJobs++
			out.Copies += target.copies
			out.Bytes += target.bytes
		default:
			out.AlreadyQueued++
		}
	}
	if out.FailedJobs > 0 && out.QueuedJobs == 0 && out.AlreadyQueued == 0 {
		return nil, firstErr
	}
	// A scan with failed hand-offs can be applied again: the releases already
	// queued are recognised by their unfinished job.
	if out.FailedJobs == 0 {
		applied := time.Now().UTC()
		c.status.AppliedAt = &applied
	}
	s.logger.Info("copies for incomplete releases queued",
		logging.KeyOperation, "complete_releases",
		"queued_jobs", out.QueuedJobs, "already_queued", out.AlreadyQueued,
		"failed_jobs", out.FailedJobs, "copies", out.Copies, "bytes", out.Bytes)
	return out, nil
}

// runCompletionScan looks up every stored release and records what a
// copies-only job would do with each of its recordings.
func (s *Service) runCompletionScan(ctx context.Context, run int) {
	result := &CompletionScan{FailedByCode: map[string]int{}, Releases: []CompletionRelease{}}
	var targets []completionTarget

	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("panic recovered in the scan for incomplete releases",
				logging.KeyOperation, "complete_releases", "panic", rec)
			s.finishCompletionScan(run, CompletionFailed, nil, nil,
				apperr.New(apperr.CodeInternal, fmt.Sprintf("The scan stopped unexpectedly: %v", rec)))
		}
	}()

	releases, err := s.catalog.ListAllReleases(ctx)
	if err != nil {
		s.finishCompletionScan(run, CompletionFailed, nil, nil, err)
		return
	}
	lookups := make([]music.Release, 0, len(releases))
	for _, release := range releases {
		if release.Provider == "" || release.SourceID == "" {
			result.ReleasesWithoutSource++
			continue
		}
		lookups = append(lookups, release)
	}
	s.reportCompletionProgress(run, 0, len(lookups))

	for i, release := range lookups {
		if ctx.Err() != nil {
			s.cancelledCompletionScan(run)
			return
		}
		entry, err := s.examineRelease(ctx, release)
		if ctx.Err() != nil {
			s.cancelledCompletionScan(run)
			return
		}
		if err != nil {
			code := string(apperr.CodeOf(err))
			result.ReleasesFailed++
			result.FailedByCode[code]++
			s.logger.Warn("release could not be checked for missing copies",
				logging.KeyOperation, "complete_releases",
				logging.KeyRelease, release.DisplayTitle(),
				logging.KeyProvider, release.Provider,
				logging.KeyErrorCode, code,
				logging.KeyError, err.Error())
		} else {
			result.ReleasesExamined++
			result.HeldTracks += entry.Held
			result.UnknownTracks += entry.Unknown
			if entry.Copies > 0 {
				result.ReleasesIncomplete++
				result.Copies += entry.Copies
				result.Bytes += entry.Bytes
				targets = append(targets, completionTarget{
					provider: release.Provider,
					sourceID: release.SourceID,
					label:    entry.Artist + " - " + entry.Title,
					copies:   entry.Copies,
					bytes:    entry.Bytes,
				})
				if len(result.Releases) < CompletionListLimit {
					result.Releases = append(result.Releases, entry)
				} else {
					result.ReleasesTruncated = true
				}
			}
		}
		s.reportCompletionProgress(run, i+1, len(lookups))
	}

	if guard := s.library.Guard(); guard != nil {
		// The health check is cached for a while, so asking it is cheap.
		if health := guard.CheckHealth(ctx, false); health.TotalBytes > 0 {
			free := int64(health.AvailableBytes)
			result.FreeBytes = &free
		}
	}

	s.logger.Info("scan for incomplete releases finished",
		logging.KeyOperation, "complete_releases",
		"releases_examined", result.ReleasesExamined,
		"releases_incomplete", result.ReleasesIncomplete,
		"releases_failed", result.ReleasesFailed,
		"releases_without_source", result.ReleasesWithoutSource,
		"copies", result.Copies, "bytes", result.Bytes,
		"unknown_tracks", result.UnknownTracks)
	s.finishCompletionScan(run, CompletionScanned, result, targets, nil)
}

// examineRelease reads a release's track list and asks for every recording
// on it what a copies-only item would do.
func (s *Service) examineRelease(ctx context.Context, release music.Release) (CompletionRelease, error) {
	entry := CompletionRelease{
		ReleaseID: release.ID,
		Artist:    release.DisplayAlbumArtist(),
		Title:     release.DisplayTitle(),
	}
	resolved, err := s.releases.ResolveRelease(ctx, release.Provider, release.SourceID)
	if err != nil {
		return entry, err
	}
	for _, track := range resolved.Tracks() {
		check, err := s.copies.CheckCopy(ctx, track)
		if err != nil {
			return entry, err
		}
		entry.Total++
		switch {
		case !check.Known:
			entry.Unknown++
		case check.Held:
			entry.Held++
		default:
			entry.Copies++
			entry.Bytes += check.SizeBytes
		}
	}
	return entry, nil
}

// reportCompletionProgress records how far a scan is, unless it is no longer
// the current one.
func (s *Service) reportCompletionProgress(run, done, total int) {
	c := &s.completion
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != run {
		return
	}
	c.status.Progress = CompletionProgress{Done: done, Total: total}
}

// cancelledCompletionScan records a scan whose context ended. An explicit
// cancel has recorded it already; this covers the service shutting down.
func (s *Service) cancelledCompletionScan(run int) {
	s.finishCompletionScan(run, CompletionCancelled, nil, nil,
		apperr.New(apperr.CodeJobCancelled, "The scan was cancelled."))
}

// finishCompletionScan stores the outcome of a scan, unless it was cancelled
// or replaced meanwhile.
func (s *Service) finishCompletionScan(run int, state CompletionState, result *CompletionScan, targets []completionTarget, err error) {
	c := &s.completion
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != run {
		return
	}
	finished := time.Now().UTC()
	c.status.State = state
	c.status.FinishedAt = &finished
	c.status.Result = result
	c.targets = targets
	if err != nil {
		c.status.ErrorCode = string(apperr.CodeOf(err))
		c.status.Error = apperr.MessageOf(err)
	}
}
