package jobs

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// resolveQueueSize bounds how many jobs may wait for catalogue resolution.
const resolveQueueSize = 128

// ManagerOptions configures the job manager.
type ManagerOptions struct {
	Store       Store
	Catalog     Catalog
	Files       FileStore
	Library     *storage.Library
	Staging     *storage.StagingManager
	Registry    *provider.Registry
	Discography *discography.Service
	Matcher     *matcher.Matcher
	Downloader  Downloader
	Tagger      Tagger
	Artwork     ArtworkFetcher
	Lyrics      LyricsResolver
	Cooldown    *MediaCooldownManager
	Broker      *Broker
	Logger      *slog.Logger

	Concurrency         int
	MaxRetries          int
	RetryBackoff        time.Duration
	TrackTimeout        time.Duration
	DurationToleranceMS int
	TempDir             string
	EmbedCover          bool
	WriteCoverFile      bool
	SkipExisting        bool
	LyricsEnabled       bool
	LyricsWriteSidecar  bool
	AllowOfflineStaging bool
}

// LyricsResolver looks up the lyrics of a track. It is optional: a manager
// without one simply never writes lyrics.
type LyricsResolver interface {
	Resolve(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error)
}

// jobRun holds the cancellation handle of a running job.
type jobRun struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Manager owns the download pipeline. HTTP handlers only ever talk to this
// type; they never touch the queue, the workers or the providers directly.
type Manager struct {
	store       Store
	catalog     Catalog
	files       FileStore
	library     *storage.Library
	staging     *storage.StagingManager
	registry    *provider.Registry
	discography *discography.Service
	matcher     *matcher.Matcher
	downloader  Downloader
	tagger      Tagger
	artwork     ArtworkFetcher
	lyrics      LyricsResolver
	cooldown    *MediaCooldownManager
	broker      *Broker
	logger      *slog.Logger

	concurrency  int
	maxRetries   int
	retryBackoff time.Duration
	trackTimeout time.Duration
	toleranceMS  int
	tempDir      string

	// The following settings can be changed while the server runs, which is
	// why they are held atomically.
	embedCover          atomic.Bool
	writeCoverFile      atomic.Bool
	lyricsEnabled       atomic.Bool
	lyricsWriteSidecar  atomic.Bool
	defaultSkipExisting atomic.Bool
	allowOfflineStaging atomic.Bool
	queuePaused         atomic.Bool
	accepting           atomic.Bool
	stopping            atomic.Bool
	admissionMu         sync.RWMutex

	maxWorkers       atomic.Int32
	activeWorkers    atomic.Int32
	rateLimit        atomic.Pointer[string]
	scheduleEnabled  atomic.Bool
	scheduleStart    atomic.Pointer[string]
	scheduleEnd      atomic.Pointer[string]
	scheduleTimezone atomic.Pointer[string]
	nowFunc          func() time.Time

	resolveQueue chan string
	wake         chan struct{}
	semaphore    chan struct{}
	finalizerSem chan struct{}

	inFlight sync.Map // item id -> struct{}
	runs     sync.Map // job id -> *jobRun

	wg        sync.WaitGroup
	ctx       context.Context
	stop      context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewManager builds the job manager.
func NewManager(opts ManagerOptions) (*Manager, error) {
	missing := func(name string) error {
		return apperr.Newf(apperr.CodeInternal, "The job manager needs a %s.", name)
	}
	switch {
	case opts.Store == nil:
		return nil, missing("job store")
	case opts.Catalog == nil:
		return nil, missing("catalogue")
	case opts.Files == nil:
		return nil, missing("file store")
	case opts.Library == nil:
		return nil, missing("library")
	case opts.Registry == nil:
		return nil, missing("provider registry")
	case opts.Discography == nil:
		return nil, missing("discography service")
	case opts.Matcher == nil:
		return nil, missing("matcher")
	case opts.Downloader == nil:
		return nil, missing("downloader")
	case opts.Tagger == nil:
		return nil, missing("tagger")
	case opts.Artwork == nil:
		return nil, missing("artwork fetcher")
	case opts.Broker == nil:
		return nil, missing("event broker")
	}

	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 2
	}
	if concurrency > 4 {
		concurrency = 4
	}
	retryBackoff := opts.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 15 * time.Second
	}
	trackTimeout := opts.TrackTimeout
	if trackTimeout <= 0 {
		trackTimeout = 30 * time.Minute
	}
	tolerance := opts.DurationToleranceMS
	if tolerance <= 0 {
		tolerance = discography.DefaultDurationToleranceMS
	}
	tempDir := strings.TrimSpace(opts.TempDir)
	if tempDir != "" {
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "The temporary directory could not be created.", err)
		}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		store:        opts.Store,
		catalog:      opts.Catalog,
		files:        opts.Files,
		library:      opts.Library,
		staging:      opts.Staging,
		registry:     opts.Registry,
		discography:  opts.Discography,
		matcher:      opts.Matcher,
		downloader:   opts.Downloader,
		tagger:       opts.Tagger,
		artwork:      opts.Artwork,
		lyrics:       opts.Lyrics,
		cooldown:     opts.Cooldown,
		broker:       opts.Broker,
		logger:       logger,
		concurrency:  concurrency,
		maxRetries:   opts.MaxRetries,
		retryBackoff: retryBackoff,
		trackTimeout: trackTimeout,
		toleranceMS:  tolerance,
		tempDir:      tempDir,
		resolveQueue: make(chan string, resolveQueueSize),
		wake:         make(chan struct{}, 1),
		semaphore:    make(chan struct{}, concurrency),
		finalizerSem: make(chan struct{}, 1), // single bounded finalization slot for network FS protection
	}
	if m.cooldown == nil {
		m.cooldown = NewMediaCooldownManager()
	}
	m.maxWorkers.Store(int32(concurrency))
	defStart := "22:00"
	defEnd := "06:00"
	emptyStr := ""
	m.scheduleStart.Store(&defStart)
	m.scheduleEnd.Store(&defEnd)
	m.scheduleTimezone.Store(&emptyStr)
	m.rateLimit.Store(&emptyStr)

	m.embedCover.Store(opts.EmbedCover)
	m.writeCoverFile.Store(opts.WriteCoverFile)
	m.lyricsEnabled.Store(opts.LyricsEnabled)
	m.lyricsWriteSidecar.Store(opts.LyricsWriteSidecar)
	m.defaultSkipExisting.Store(opts.SkipExisting)
	m.allowOfflineStaging.Store(opts.AllowOfflineStaging)
	m.accepting.Store(true)
	return m, nil
}

// NewManagerForTest creates a lightweight Manager instance for unit/handler testing.
func NewManagerForTest(store Store, broker *Broker) *Manager {
	if broker == nil {
		broker = NewBroker(slog.Default())
	}
	m := &Manager{
		store:        store,
		broker:       broker,
		logger:       slog.Default(),
		wake:         make(chan struct{}, 1),
		resolveQueue: make(chan string, 10),
		semaphore:    make(chan struct{}, 2),
		finalizerSem: make(chan struct{}, 1),
	}
	m.maxWorkers.Store(2)
	defStart := "22:00"
	defEnd := "06:00"
	emptyStr := ""
	m.scheduleStart.Store(&defStart)
	m.scheduleEnd.Store(&defEnd)
	m.scheduleTimezone.Store(&emptyStr)
	m.rateLimit.Store(&emptyStr)
	m.accepting.Store(true)
	return m
}

// BeginShutdown closes admission for new jobs while allowing already active
// HTTP requests to finish. It is safe to call more than once.
func (m *Manager) BeginShutdown() {
	m.admissionMu.Lock()
	m.accepting.Store(false)
	m.admissionMu.Unlock()
}

// Stopping reports whether worker contexts are being cancelled because the
// service is shutting down. Explicitly cancelled jobs remain distinguishable
// and are still persisted as cancelled.
func (m *Manager) Stopping() bool { return m.stopping.Load() }

// SkipExisting reports whether new jobs skip recordings the library already
// holds.
func (m *Manager) SkipExisting() bool { return m.defaultSkipExisting.Load() }

// SetSkipExisting changes the default for new jobs. Running jobs keep the
// value they were created with.
func (m *Manager) SetSkipExisting(value bool) { m.defaultSkipExisting.Store(value) }

// EmbedCover reports whether covers are embedded into the audio file.
func (m *Manager) EmbedCover() bool { return m.embedCover.Load() }

// SetEmbedCover switches cover embedding on or off.
func (m *Manager) SetEmbedCover(value bool) { m.embedCover.Store(value) }

// WriteCoverFile reports whether a cover.jpg is written next to a release.
func (m *Manager) WriteCoverFile() bool { return m.writeCoverFile.Load() }

// SetWriteCoverFile switches the cover file on or off.
func (m *Manager) SetWriteCoverFile(value bool) { m.writeCoverFile.Store(value) }

// LyricsEnabled reports whether finished downloads get lyrics.
func (m *Manager) LyricsEnabled() bool { return m.lyricsEnabled.Load() }

// SetLyricsEnabled switches the lyrics lookup on or off.
func (m *Manager) SetLyricsEnabled(value bool) { m.lyricsEnabled.Store(value) }

// LyricsWriteSidecar reports whether a lyrics sidecar file (.lrc/.txt) is written.
func (m *Manager) LyricsWriteSidecar() bool { return m.lyricsWriteSidecar.Load() }

// SetLyricsWriteSidecar switches the lyrics sidecar file writing on or off.
func (m *Manager) SetLyricsWriteSidecar(value bool) { m.lyricsWriteSidecar.Store(value) }

// Matcher exposes the matching engine so that its threshold can be adjusted.
func (m *Manager) Matcher() *matcher.Matcher { return m.matcher }

// Staging returns the local persistent staging manager.
func (m *Manager) Staging() *storage.StagingManager { return m.staging }

// Cooldown returns the media cooldown manager.
func (m *Manager) Cooldown() *MediaCooldownManager { return m.cooldown }

// AllowOfflineStaging reports whether downloads can proceed to local staging even if library is offline.
func (m *Manager) AllowOfflineStaging() bool { return m.allowOfflineStaging.Load() }

// SetAllowOfflineStaging toggles offline staging.
func (m *Manager) SetAllowOfflineStaging(value bool) { m.allowOfflineStaging.Store(value) }

// QueuePaused reports whether queue dispatching is currently paused.
func (m *Manager) QueuePaused() bool { return m.queuePaused.Load() }

// SetQueuePaused pauses or resumes the download queue dispatcher.
func (m *Manager) SetQueuePaused(value bool) {
	m.queuePaused.Store(value)
	if !value {
		m.signal()
	}
}

// Concurrency returns how many tracks are downloaded in parallel.
func (m *Manager) Concurrency() int { return m.concurrency }

// Request describes a download order coming from the API.
type Request struct {
	Type             Type
	MetadataProvider string
	MediaProvider    string
	TargetID         string
	ReleaseID        string
	Label            string
	Options          RequestOptions
}

// RequestOptions are the optional per request settings. A nil field means the
// server default applies.
type RequestOptions struct {
	ReleaseFilter *music.ReleaseFilter
	SkipExisting  *bool
	Priority      *Priority
}

// Enqueue creates a job and hands it to the resolver. The HTTP request returns
// as soon as the job is persisted; nothing is downloaded on the request path.
func (m *Manager) Enqueue(ctx context.Context, req Request) (*Job, error) {
	m.admissionMu.RLock()
	defer m.admissionMu.RUnlock()
	if !m.accepting.Load() {
		return nil, apperr.New(apperr.CodeShuttingDown,
			"The service is shutting down and is not accepting new jobs.")
	}
	if !req.Type.Valid() {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "%q is not a valid job type.", req.Type)
	}
	if strings.TrimSpace(req.TargetID) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "A target id is required.")
	}

	metadataProvider, err := m.registry.Metadata(req.MetadataProvider)
	if err != nil {
		return nil, err
	}
	if req.MediaProvider != "" {
		if _, err := m.registry.Media(req.MediaProvider); err != nil {
			return nil, err
		}
	}

	options := Options{
		ReleaseFilter: music.DefaultReleaseFilter(),
		SkipExisting:  m.defaultSkipExisting.Load(),
		ReleaseID:     strings.TrimSpace(req.ReleaseID),
	}
	if req.Options.ReleaseFilter != nil && req.Options.ReleaseFilter.Any() {
		options.ReleaseFilter = *req.Options.ReleaseFilter
	}
	if req.Options.SkipExisting != nil {
		options.SkipExisting = *req.Options.SkipExisting
	}

	mediaProvider := req.MediaProvider
	if mediaProvider == "" {
		mediaProvider = m.registry.DefaultMediaName()
	}

	jobPriority := PriorityNormal
	if req.Options.Priority != nil && req.Options.Priority.Valid() {
		jobPriority = *req.Options.Priority
	}

	job := &Job{
		Type:             req.Type,
		Status:           StatusQueued,
		Priority:         jobPriority,
		Label:            strings.TrimSpace(req.Label),
		MetadataProvider: metadataProvider.Name(),
		MediaProvider:    mediaProvider,
		TargetID:         strings.TrimSpace(req.TargetID),
		Options:          options,
	}

	if job.Label == "" {
		job.Label = string(req.Type) + " " + job.TargetID
	}

	if err := m.store.Create(ctx, job); err != nil {
		return nil, err
	}

	m.broker.Publish(Event{
		Type: EventJobCreated, JobID: job.ID, Status: job.Status, Label: job.Label,
	})
	m.logger.Info("job created",
		logging.KeyJobID, job.ID, "type", string(job.Type),
		logging.KeyProvider, job.MetadataProvider, "target_id", job.TargetID)

	m.registerRun(job.ID)
	m.queueForResolve(job.ID)
	return job, nil
}

// Get returns a job.
func (m *Manager) Get(ctx context.Context, id string) (*Job, error) {
	return m.store.Get(ctx, id)
}

// List returns jobs newest first along with total count.
func (m *Manager) List(ctx context.Context, filter ListFilter) ([]Job, int, error) {
	return m.store.List(ctx, filter)
}

// HasUnfinishedJob reports whether a job of this type for this target has not
// reached a terminal state yet.
//
// It is what keeps a repeated request — a subscription sync coming back after
// a partial run — from putting a second job on work that is already under way.
// Only unfinished jobs are examined, and there are few of those.
func (m *Manager) HasUnfinishedJob(ctx context.Context, jobType Type, targetID string) (bool, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return false, nil
	}
	unfinished, err := m.store.ListUnfinished(ctx)
	if err != nil {
		return false, err
	}
	for _, job := range unfinished {
		if job.Type == jobType && job.TargetID == targetID {
			return true, nil
		}
	}
	return false, nil
}

// Items returns the items of a job.
func (m *Manager) Items(ctx context.Context, jobID string) ([]Item, error) {
	if _, err := m.store.Get(ctx, jobID); err != nil {
		return nil, err
	}
	return m.store.ListItems(ctx, jobID)
}

// Cancel stops a running job. Everything that has already finished stays as it
// is; running downloads are terminated through the job context.
func (m *Manager) Cancel(ctx context.Context, id string) (*Job, error) {
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status.Terminal() {
		return nil, apperr.Newf(apperr.CodeInvalidRequest,
			"The job is already %s and cannot be cancelled.", job.Status)
	}

	if err := m.store.SetStatus(ctx, id, StatusCancelled,
		string(apperr.CodeJobCancelled), "The job was cancelled."); err != nil {
		return nil, err
	}
	if _, err := m.store.CancelPendingItems(ctx, id); err != nil {
		return nil, err
	}
	m.cancelRun(id)

	updated, err := m.store.RefreshCounters(ctx, id)
	if err != nil {
		return nil, err
	}

	m.broker.Publish(Event{
		Type: EventJobCancelled, JobID: id, Status: StatusCancelled,
		Label: updated.Label, Summary: ptr(updated.Summary()),
	})
	m.logger.Info("job cancelled", logging.KeyJobID, id)
	return updated, nil
}

// Broker exposes the event broker for the event stream handler.
func (m *Manager) Broker() *Broker { return m.broker }

// setStatus moves a job forward. A rejected transition is logged and ignored:
// the state machine is a guard rail, not a reason to fail a running job.
func (m *Manager) setStatus(ctx context.Context, job *Job, status Status) {
	if job.Status == status {
		return
	}
	if err := m.store.SetStatus(ctx, job.ID, status, "", ""); err != nil {
		m.logger.Debug("job status transition rejected",
			logging.KeyJobID, job.ID, "from", string(job.Status), "to", string(status),
			logging.KeyError, err.Error())
		return
	}
	job.Status = status
	m.broker.Publish(Event{Type: EventJobStatus, JobID: job.ID, Status: status, Label: job.Label})
}

// fail marks a job as failed.
func (m *Manager) fail(ctx context.Context, job *Job, err error) {
	code := string(apperr.CodeOf(err))
	message := apperr.MessageOf(err)

	if setErr := m.store.SetStatus(ctx, job.ID, StatusFailed, code, message); setErr != nil {
		m.logger.Error("job could not be marked as failed",
			logging.KeyJobID, job.ID, logging.KeyError, setErr.Error())
	}
	if _, cancelErr := m.store.CancelPendingItems(ctx, job.ID); cancelErr != nil {
		m.logger.Error("pending items could not be cancelled",
			logging.KeyJobID, job.ID, logging.KeyError, cancelErr.Error())
	}
	m.releaseRun(job.ID)

	m.logger.Error("job failed",
		logging.KeyJobID, job.ID, logging.KeyErrorCode, code, logging.KeyError, err.Error())
	m.broker.Publish(Event{
		Type: EventJobFailed, JobID: job.ID, Status: StatusFailed, Label: job.Label,
		ErrorCode: code, ErrorMessage: message,
	})
}

// updateItem applies an item update, using a background context so that a
// cancelled job can still record its final state.
func (m *Manager) updateItem(ctx context.Context, itemID string, update ItemUpdate) error {
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}
	return m.store.UpdateItem(ctx, itemID, update)
}

// publishItem announces an item state change.
func (m *Manager) publishItem(job Job, item Item, status ItemStatus, score float64, err error) {
	event := Event{
		Type:       EventItemStatus,
		JobID:      job.ID,
		Status:     job.Status,
		Label:      job.Label,
		ItemID:     item.ID,
		ItemStatus: status,
		Track:      item.Label,
		Current:    item.Position + 1,
		Total:      job.Total,
		MatchScore: score,
	}
	if err != nil {
		event.ErrorCode = string(apperr.CodeOf(err))
		event.ErrorMessage = apperr.MessageOf(err)
	}
	m.broker.Publish(event)
}

// publishProgress announces download progress of a single track.
func (m *Manager) publishProgress(job Job, item Item, percent float64) {
	m.broker.Publish(Event{
		Type:            EventJobProgress,
		JobID:           job.ID,
		Status:          StatusDownloading,
		Label:           job.Label,
		ItemID:          item.ID,
		ItemStatus:      ItemDownloading,
		Track:           item.Label,
		Current:         item.Position + 1,
		Total:           job.Total,
		DownloadPercent: percent,
	})
}

// MaxWorkers returns the current worker count.
func (m *Manager) MaxWorkers() int {
	w := int(m.maxWorkers.Load())
	if w < 1 {
		return 2
	}
	if w > 4 {
		return 4
	}
	return w
}

// SetMaxWorkers updates the maximum concurrent workers.
func (m *Manager) SetMaxWorkers(w int) {
	if w < 1 {
		w = 2
	}
	if w > 4 {
		w = 4
	}
	m.maxWorkers.Store(int32(w))
	m.signal()
}

// RateLimit returns the rate limit string (e.g. "5M" or "").
func (m *Manager) RateLimit() string {
	ptr := m.rateLimit.Load()
	if ptr == nil {
		return ""
	}
	return *ptr
}

// SetRateLimit updates the download bandwidth rate limit.
func (m *Manager) SetRateLimit(limit string) {
	m.rateLimit.Store(&limit)
	if rl, ok := m.downloader.(interface{ SetRateLimit(string) }); ok {
		rl.SetRateLimit(limit)
	}
}

// ScheduleEnabled reports whether the download time window is active.
func (m *Manager) ScheduleEnabled() bool {
	return m.scheduleEnabled.Load()
}

// SetScheduleEnabled updates whether the time window is active.
func (m *Manager) SetScheduleEnabled(enabled bool) {
	m.scheduleEnabled.Store(enabled)
	m.signal()
}

// ScheduleStart returns the window start time string (e.g. "22:00").
func (m *Manager) ScheduleStart() string {
	ptr := m.scheduleStart.Load()
	if ptr == nil {
		return "22:00"
	}
	return *ptr
}

// SetScheduleStart updates the window start time string.
func (m *Manager) SetScheduleStart(start string) {
	m.scheduleStart.Store(&start)
	m.signal()
}

// ScheduleEnd returns the window end time string (e.g. "06:00").
func (m *Manager) ScheduleEnd() string {
	ptr := m.scheduleEnd.Load()
	if ptr == nil {
		return "06:00"
	}
	return *ptr
}

// SetScheduleEnd updates the window end time string.
func (m *Manager) SetScheduleEnd(end string) {
	m.scheduleEnd.Store(&end)
	m.signal()
}

// ScheduleTimezone returns the configured timezone (e.g. "Europe/Berlin" or "").
func (m *Manager) ScheduleTimezone() string {
	ptr := m.scheduleTimezone.Load()
	if ptr == nil {
		return ""
	}
	return *ptr
}

// SetScheduleTimezone updates the configured timezone.
func (m *Manager) SetScheduleTimezone(tz string) {
	m.scheduleTimezone.Store(&tz)
	m.signal()
}

// SetNowFunc overrides the clock for testing.
func (m *Manager) SetNowFunc(fn func() time.Time) {
	m.nowFunc = fn
}

func (m *Manager) now() time.Time {
	if m.nowFunc != nil {
		return m.nowFunc()
	}
	return time.Now().UTC()
}

// isInsideDownloadWindow reports whether t falls inside the configured download window.
func (m *Manager) isInsideDownloadWindow(t time.Time) bool {
	if !m.scheduleEnabled.Load() {
		return true
	}
	startStr := m.ScheduleStart()
	endStr := m.ScheduleEnd()
	if startStr == "" || endStr == "" || startStr == endStr {
		return true
	}

	loc := time.Local
	if tz := m.ScheduleTimezone(); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	localTime := t.In(loc)
	curMin := localTime.Hour()*60 + localTime.Minute()

	startParts := strings.Split(startStr, ":")
	endParts := strings.Split(endStr, ":")
	if len(startParts) != 2 || len(endParts) != 2 {
		return true
	}
	startH, _ := strconv.Atoi(startParts[0])
	startM, _ := strconv.Atoi(startParts[1])
	endH, _ := strconv.Atoi(endParts[0])
	endM, _ := strconv.Atoi(endParts[1])

	startMin := startH*60 + startM
	endMin := endH*60 + endM

	if startMin < endMin {
		// e.g. 08:00 to 18:00
		return curMin >= startMin && curMin < endMin
	}
	// Overnight: e.g. 22:00 to 06:00
	return curMin >= startMin || curMin < endMin
}

// SetPriority updates a job's priority and publishes an event.
func (m *Manager) SetPriority(ctx context.Context, id string, priority Priority) (*Job, error) {
	if !priority.Valid() {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "Invalid priority %q.", priority)
	}
	if err := m.store.SetPriority(ctx, id, priority); err != nil {
		return nil, err
	}
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	m.broker.Publish(Event{
		Type: EventJobPriorityChanged, JobID: id, Status: job.Status, Label: job.Label, Priority: priority,
	})
	m.signal()
	return job, nil
}

// Pause pauses a job so no new items are claimed.
func (m *Manager) Pause(ctx context.Context, id string) (*Job, error) {
	if err := m.store.SetPaused(ctx, id, true); err != nil {
		return nil, err
	}
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	pausedVal := true
	m.broker.Publish(Event{
		Type: EventJobPaused, JobID: id, Status: job.Status, Label: job.Label, Paused: &pausedVal,
	})
	return job, nil
}

// Resume resumes a paused job.
func (m *Manager) Resume(ctx context.Context, id string) (*Job, error) {
	if err := m.store.SetPaused(ctx, id, false); err != nil {
		return nil, err
	}
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	pausedVal := false
	m.broker.Publish(Event{
		Type: EventJobResumed, JobID: id, Status: job.Status, Label: job.Label, Paused: &pausedVal,
	})
	m.signal()
	return job, nil
}

// RetryFailed requeues all failed items of a job.
func (m *Manager) RetryFailed(ctx context.Context, id string) (*Job, int, int, error) {
	retried, skipped, err := m.store.ResetFailedItemsInJob(ctx, id)
	if err != nil {
		return nil, 0, 0, err
	}
	job, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, 0, 0, err
	}
	m.broker.Publish(Event{
		Type: EventJobRetried, JobID: id, Status: job.Status, Label: job.Label,
	})
	m.signal()
	return job, retried, skipped, nil
}

// RetryItem requeues a single failed or retry_wait item.
func (m *Manager) RetryItem(ctx context.Context, jobID, itemID string) (*Item, error) {
	if err := m.store.ResetItemForRetry(ctx, jobID, itemID); err != nil {
		return nil, err
	}
	item, err := m.store.GetItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	job, err := m.store.Get(ctx, jobID)
	if err == nil {
		m.broker.Publish(Event{
			Type: EventItemStatus, JobID: jobID, ItemID: itemID, ItemStatus: ItemPending,
			Status: job.Status, Label: job.Label,
		})
	}
	m.signal()
	return item, nil
}

// DeleteHistory removes completed/cancelled jobs older than the given cutoff.
func (m *Manager) DeleteHistory(ctx context.Context, olderThanDays int, statuses []Status) (int, int, error) {
	if olderThanDays < 7 {
		return 0, 0, apperr.New(apperr.CodeInvalidRequest, "older_than_days must be at least 7.")
	}
	cutoff := m.now().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
	return m.store.DeleteHistory(ctx, cutoff, statuses)
}

// EnqueueReleaseWithPriority queues a release download with a specified priority.
func (m *Manager) EnqueueReleaseWithPriority(ctx context.Context, provider, releaseID, artistName string, priority Priority) (bool, error) {
	if exists, err := m.HasUnfinishedJob(ctx, TypeRelease, releaseID); err != nil || exists {
		return false, err
	}
	_, err := m.Enqueue(ctx, Request{
		Type:             TypeRelease,
		MetadataProvider: provider,
		TargetID:         releaseID,
		Label:            artistName + " — " + releaseID,
		Options: RequestOptions{
			Priority: &priority,
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func ptr[T any](value T) *T { return &value }
