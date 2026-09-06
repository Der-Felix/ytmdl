package subscriptions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// DefaultSyncInterval is how long a subscription waits between two runs when
// the configuration does not say otherwise.
const DefaultSyncInterval = 24 * time.Hour

// DefaultRetryInterval is when a run that failed is attempted again. It is
// deliberately far away from the failure: a provider that is down does not
// recover within seconds, and a tight retry loop would only turn one outage
// into a stream of requests.
const DefaultRetryInterval = time.Hour

// DefaultSyncTimeout bounds one run. A provider that stops answering must not
// be able to hold a subscription's guard forever.
const DefaultSyncTimeout = 30 * time.Minute

// Options configures the subscription service.
type Options struct {
	Store       Store
	Catalog     Catalog
	Files       FileStore
	Discography *discography.Service
	Registry    *provider.Registry

	// Downloader is optional. Without one, auto download is reported as
	// unavailable instead of silently doing nothing.
	Downloader Downloader
	// Broker is optional; without one the sync simply publishes no events.
	Broker *jobs.Broker

	SyncInterval        time.Duration
	RetryInterval       time.Duration
	SyncTimeout         time.Duration
	DurationToleranceMS int

	Logger *slog.Logger
}

// Service manages the watched artists and synchronises their catalogue.
type Service struct {
	store       Store
	catalog     Catalog
	files       FileStore
	discography *discography.Service
	registry    *provider.Registry
	downloader  Downloader
	broker      *jobs.Broker
	logger      *slog.Logger

	syncInterval  time.Duration
	retryInterval time.Duration
	syncTimeout   time.Duration
	toleranceMS   int

	// accepting closes when the service shuts down, so that a request arriving
	// during the drain is refused rather than started and then killed.
	accepting atomic.Bool
	ctx       context.Context
	stop      context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once

	// lastResults keeps the most recent report per subscription. A report is
	// a transient summary of one run, not a record — the durable state is the
	// subscription's own status, which is why this is not a table.
	lastResults sync.Map // subscription id -> *SyncResult

	// active holds the subscriptions this process is syncing right now. It is
	// the whole of the "one sync per subscription" rule: keyed per
	// subscription, so two different artists never wait for each other, and
	// process local, so a crash cannot leave a subscription marked as busy
	// forever.
	active sync.Map // subscription id -> struct{}
}

// New builds the subscription service.
func New(opts Options) (*Service, error) {
	missing := func(name string) error {
		return apperr.Newf(apperr.CodeInternal, "The subscription service needs a %s.", name)
	}
	switch {
	case opts.Store == nil:
		return nil, missing("subscription store")
	case opts.Catalog == nil:
		return nil, missing("catalogue")
	case opts.Files == nil:
		return nil, missing("file store")
	case opts.Discography == nil:
		return nil, missing("discography service")
	}

	syncInterval := opts.SyncInterval
	if syncInterval <= 0 {
		syncInterval = DefaultSyncInterval
	}
	retryInterval := opts.RetryInterval
	if retryInterval <= 0 {
		retryInterval = DefaultRetryInterval
	}
	syncTimeout := opts.SyncTimeout
	if syncTimeout <= 0 {
		syncTimeout = DefaultSyncTimeout
	}
	tolerance := opts.DurationToleranceMS
	if tolerance <= 0 {
		tolerance = discography.DefaultDurationToleranceMS
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		store:         opts.Store,
		catalog:       opts.Catalog,
		files:         opts.Files,
		discography:   opts.Discography,
		registry:      opts.Registry,
		downloader:    opts.Downloader,
		broker:        opts.Broker,
		logger:        logger,
		syncInterval:  syncInterval,
		retryInterval: retryInterval,
		syncTimeout:   syncTimeout,
		toleranceMS:   tolerance,
	}
	service.accepting.Store(true)
	service.ctx = context.Background()
	return service, nil
}

// Start gives the background runs a lifetime of their own, so that a sync
// started by an HTTP request outlives the request that asked for it.
func (s *Service) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		s.ctx, s.stop = context.WithCancel(context.WithoutCancel(ctx))
		s.accepting.Store(true)
	})
	return nil
}

// BeginShutdown stops admitting new runs while the ones in flight finish.
func (s *Service) BeginShutdown() { s.accepting.Store(false) }

// Stop refuses further runs, cancels the ones in flight and waits for them.
// A cancelled run still records its outcome: recordFailure writes on a context
// the cancellation does not reach.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		s.BeginShutdown()
		if s.stop != nil {
			s.stop()
		}
		s.wg.Wait()
	})
}

// SyncInterval returns the configured gap between two scheduled runs.
func (s *Service) SyncInterval() time.Duration { return s.syncInterval }

/* ------------------------------------------------------------------- CRUD */

// Create starts watching an artist. Subscribing to an artist that is already
// watched returns the existing subscription rather than failing, so a repeated
// request is harmless.
func (s *Service) Create(ctx context.Context, req NewSubscription) (*Subscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	// The subscription keeps the provider the artist was found with; nothing
	// here converts an artist onto a different provider.
	name, err := s.providerName(req.Provider)
	if err != nil {
		return nil, err
	}
	req.Provider = name
	req.ArtistName = strings.TrimSpace(req.ArtistName)

	sub, err := s.store.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	s.logger.Info("artist subscribed",
		logging.KeyProvider, sub.Provider,
		logging.KeyArtist, sub.DisplayName(),
		"subscription_id", sub.ID,
		"auto_download", sub.AutoDownload)

	return s.decorate(sub), nil
}

// providerName resolves the metadata provider, so that a subscription can
// never be stored against a provider the backend does not have.
func (s *Service) providerName(name string) (string, error) {
	if s.registry == nil {
		return name, nil
	}
	metadata, err := s.registry.Metadata(name)
	if err != nil {
		return "", err
	}
	return metadata.Name(), nil
}

// Get returns one subscription.
func (s *Service) Get(ctx context.Context, id string) (*Subscription, error) {
	sub, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.decorate(sub), nil
}

// FindBySource returns the subscription of an artist, or nil when the artist
// is not watched.
func (s *Service) FindBySource(ctx context.Context, providerName, artistSourceID string) (*Subscription, error) {
	name, err := s.providerName(providerName)
	if err != nil {
		return nil, err
	}
	sub, err := s.store.FindBySource(ctx, name, artistSourceID)
	if err != nil || sub == nil {
		return nil, err
	}
	return s.decorate(sub), nil
}

// List returns the subscriptions.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]Subscription, error) {
	list, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Syncing = s.isSyncing(list[i].ID)
	}
	return list, nil
}

// Update changes the flags of a subscription.
func (s *Service) Update(ctx context.Context, id string, update Update) (*Subscription, error) {
	if update.Empty() {
		return nil, apperr.New(apperr.CodeInvalidRequest,
			"The request changes nothing; set enabled or auto_download.")
	}
	if update.DownloadPriority != nil {
		if !update.DownloadPriority.Valid() {
			return nil, apperr.Newf(apperr.CodeInvalidRequest, "Invalid download priority %q.", *update.DownloadPriority)
		}
		c := update.DownloadPriority.Canonical()
		update.DownloadPriority = &c
	}
	sub, err := s.store.Update(ctx, id, update)
	if err != nil {
		return nil, err
	}
	return s.decorate(sub), nil
}

// Delete stops watching an artist. Nothing that was already downloaded is
// touched: a subscription describes what to watch, not what to keep.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.lastResults.Delete(id)
	return nil
}

// Export produces a portable versioned snapshot of all artist subscriptions.
// No sensitive credentials, internal database IDs, or machine-specific paths are included.
func (s *Service) Export(ctx context.Context) (*ExportPayload, error) {
	subs, err := s.store.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	exported := make([]ExportSubscription, len(subs))
	for i, sub := range subs {
		filter := sub.ReleaseFilter
		if !filter.Any() {
			filter = music.DefaultReleaseFilter()
		}
		priority := sub.DownloadPriority
		if !priority.Valid() {
			priority = jobs.PriorityLow
		} else {
			priority = priority.Canonical()
		}

		exported[i] = ExportSubscription{
			ArtistName:       sub.DisplayName(),
			Provider:         sub.Provider,
			ArtistSourceID:   sub.ArtistSourceID,
			ArtistImageURL:   sub.ArtistImageURL,
			Enabled:          sub.Enabled,
			AutoDownload:     sub.AutoDownload,
			ReleaseFilter:    filter,
			DownloadPriority: priority,
		}
	}

	return &ExportPayload{
		Format:        ExportFormatName,
		Version:       ExportFormatVersion,
		ExportedAt:    time.Now().UTC(),
		Subscriptions: exported,
	}, nil
}

// PreviewImport analyzes an incoming subscription export file against the current database
// state without performing any modifications.
func (s *Service) PreviewImport(ctx context.Context, payload ExportPayload) (*ImportPreview, error) {
	if payload.Format != ExportFormatName {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "Unsupported format %q; expected %q.", payload.Format, ExportFormatName)
	}
	if payload.Version != ExportFormatVersion {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "Unsupported format version %d; expected %d.", payload.Version, ExportFormatVersion)
	}
	if len(payload.Subscriptions) > MaxImportItems {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "Import contains %d subscriptions, exceeding the maximum limit of %d.", len(payload.Subscriptions), MaxImportItems)
	}

	existingList, err := s.store.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	existingMap := make(map[string]Subscription, len(existingList))
	for _, sub := range existingList {
		key := strings.ToLower(sub.Provider) + ":" + strings.TrimSpace(sub.ArtistSourceID)
		existingMap[key] = sub
	}

	seenInFile := make(map[string]int, len(payload.Subscriptions))
	preview := &ImportPreview{
		Total: len(payload.Subscriptions),
		Items: make([]ImportPreviewItem, 0, len(payload.Subscriptions)),
	}

	for i, item := range payload.Subscriptions {
		rawProv := strings.TrimSpace(item.Provider)
		sourceID := strings.TrimSpace(item.ArtistSourceID)
		name := strings.TrimSpace(item.ArtistName)
		if name == "" {
			name = music.UnknownArtist
		}

		filter := item.ReleaseFilter
		if !filter.Any() {
			filter = music.DefaultReleaseFilter()
		}
		priority := item.DownloadPriority
		if !priority.Valid() {
			priority = jobs.PriorityLow
		} else {
			priority = priority.Canonical()
		}

		pItem := ImportPreviewItem{
			Index:            i,
			ArtistName:       name,
			Provider:         rawProv,
			ArtistSourceID:   sourceID,
			ArtistImageURL:   strings.TrimSpace(item.ArtistImageURL),
			Enabled:          item.Enabled,
			AutoDownload:     item.AutoDownload,
			ReleaseFilter:    filter,
			DownloadPriority: priority,
		}

		if rawProv == "" || sourceID == "" {
			pItem.Status = ImportStatusInvalid
			pItem.Error = "Provider and artist source ID are required."
			preview.Invalid++
			preview.Items = append(preview.Items, pItem)
			continue
		}

		normProv, err := s.providerName(rawProv)
		if err != nil {
			pItem.Status = ImportStatusInvalid
			pItem.Error = fmt.Sprintf("Invalid provider %q: %s", rawProv, apperr.MessageOf(err))
			preview.Invalid++
			preview.Items = append(preview.Items, pItem)
			continue
		}
		pItem.Provider = normProv

		fileKey := strings.ToLower(normProv) + ":" + sourceID
		if prevIdx, seen := seenInFile[fileKey]; seen {
			pItem.Status = ImportStatusDuplicate
			pItem.Error = fmt.Sprintf("Duplicate of subscription #%d in this import file.", prevIdx+1)
			preview.Duplicates++
			preview.Items = append(preview.Items, pItem)
			continue
		}
		seenInFile[fileKey] = i

		if existing, exists := existingMap[fileKey]; exists {
			pItem.ExistingID = existing.ID
			preview.Existing++

			changes := make([]string, 0, 4)
			if existing.Enabled != item.Enabled {
				changes = append(changes, fmt.Sprintf("enabled: %v -> %v", existing.Enabled, item.Enabled))
			}
			if existing.AutoDownload != item.AutoDownload {
				changes = append(changes, fmt.Sprintf("auto_download: %v -> %v", existing.AutoDownload, item.AutoDownload))
			}
			if existing.DownloadPriority != priority {
				changes = append(changes, fmt.Sprintf("priority: %s -> %s", existing.DownloadPriority, priority))
			}
			if existing.ReleaseFilter != filter {
				changes = append(changes, "release_filter modified")
			}
			if item.ArtistName != "" && existing.ArtistName != name && name != music.UnknownArtist {
				changes = append(changes, fmt.Sprintf("name: %s -> %s", existing.ArtistName, name))
			}

			if len(changes) > 0 {
				pItem.Status = ImportStatusWouldUpdate
				pItem.Changes = changes
				preview.WouldUpdate++
			} else {
				pItem.Status = ImportStatusUnchanged
				preview.Unchanged++
			}
		} else {
			pItem.Status = ImportStatusNew
			preview.New++
		}

		preview.Items = append(preview.Items, pItem)
	}

	return preview, nil
}

// ApplyImport executes the import in a transactional database batch.
// Crucially, this operation ONLY persists subscription records and DOES NOT
// create download jobs, trigger background syncs, or enqueue tracks.
func (s *Service) ApplyImport(ctx context.Context, payload ExportPayload) (*ImportResult, error) {
	preview, err := s.PreviewImport(ctx, payload)
	if err != nil {
		return nil, err
	}

	newSubs := make([]NewSubscription, 0, preview.New)
	updates := make([]ImportUpdate, 0, preview.WouldUpdate)
	errors := make([]ImportError, 0, preview.Invalid+preview.Duplicates)

	for _, item := range preview.Items {
		switch item.Status {
		case ImportStatusNew:
			newSubs = append(newSubs, NewSubscription{
				Provider:         item.Provider,
				ArtistSourceID:   item.ArtistSourceID,
				ArtistName:       item.ArtistName,
				ArtistImageURL:   item.ArtistImageURL,
				Enabled:          &item.Enabled,
				AutoDownload:     item.AutoDownload,
				ReleaseFilter:    &item.ReleaseFilter,
				DownloadPriority: &item.DownloadPriority,
			})
		case ImportStatusWouldUpdate:
			updates = append(updates, ImportUpdate{
				ID:               item.ExistingID,
				ArtistName:       item.ArtistName,
				ArtistImageURL:   item.ArtistImageURL,
				Enabled:          item.Enabled,
				AutoDownload:     item.AutoDownload,
				ReleaseFilter:    item.ReleaseFilter,
				DownloadPriority: item.DownloadPriority,
			})
		case ImportStatusInvalid, ImportStatusDuplicate:
			errors = append(errors, ImportError{
				Index:          item.Index,
				ArtistName:     item.ArtistName,
				Provider:       item.Provider,
				ArtistSourceID: item.ArtistSourceID,
				Error:          item.Error,
			})
		}
	}

	var repoResult *ImportResult
	if len(newSubs) > 0 || len(updates) > 0 {
		var err error
		repoResult, err = s.store.ApplyImport(ctx, newSubs, updates)
		if err != nil {
			return nil, err
		}
	} else {
		repoResult = &ImportResult{}
	}

	result := &ImportResult{
		Created:   repoResult.Created,
		Updated:   repoResult.Updated,
		Unchanged: preview.Unchanged,
		Failed:    len(errors) + repoResult.Failed,
		Errors:    append(errors, repoResult.Errors...),
	}

	s.logger.Info("subscriptions imported",
		"created", result.Created,
		"updated", result.Updated,
		"unchanged", result.Unchanged,
		"failed", result.Failed)

	return result, nil
}

// decorate fills in the process state a stored subscription cannot carry.
func (s *Service) decorate(sub *Subscription) *Subscription {
	sub.Syncing = s.isSyncing(sub.ID)
	return sub
}

func (s *Service) isSyncing(id string) bool {
	_, running := s.active.Load(id)
	return running
}

/* ------------------------------------------------------------------- sync */

// Sync compares the artist's catalogue at the provider with the library and
// waits for the answer. The scheduler uses it; the HTTP API does not, because
// a full discography takes longer than a request may.
//
// A disabled subscription can still be synced by hand: disabling stops the
// scheduler, it does not take the button away from someone who explicitly
// asks for a check.
func (s *Service) Sync(ctx context.Context, id string) (*SyncResult, error) {
	sub, release, err := s.claim(ctx, id)
	if err != nil {
		return nil, err
	}
	defer release()

	return s.run(ctx, sub)
}

// StartSync begins a run in the background and returns at once.
//
// The guard is taken before returning, so a second request is refused straight
// away rather than racing the goroutine. Progress and completion travel on the
// event stream; the report is kept for LastResult.
func (s *Service) StartSync(ctx context.Context, id string) (*Subscription, error) {
	sub, release, err := s.claim(ctx, id)
	if err != nil {
		return nil, err
	}

	// The goroutine and the caller must not share the subscription: the
	// handler serialises the value it gets back while the run is already
	// reading its own copy.
	running := *sub
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer release()

		// The run belongs to the service, not to the request that asked for
		// it: the client is long gone by the time a discography is walked.
		runCtx, cancel := context.WithTimeout(s.ctx, s.syncTimeout)
		defer cancel()

		if _, err := s.run(runCtx, &running); err != nil {
			return
		}
	}()

	answer := *sub
	answer.Syncing = true
	return &answer, nil
}

// DueForSync returns the enabled subscriptions whose next run is due. It is
// what the scheduler selects on, and it lives here so that the scheduler has
// no reason to reach past the service into the store.
func (s *Service) DueForSync(ctx context.Context, now time.Time, limit int) ([]Subscription, error) {
	return s.store.ListDueForSync(ctx, now, limit)
}

// LastResult returns the report of the most recent run of this process, or nil
// when the subscription has not been synced since the backend started.
func (s *Service) LastResult(id string) *SyncResult {
	value, ok := s.lastResults.Load(id)
	if !ok {
		return nil
	}
	result, _ := value.(*SyncResult)
	return result
}

// claim loads a subscription and takes its sync guard. The returned function
// releases the guard and must always be called.
func (s *Service) claim(ctx context.Context, id string) (*Subscription, func(), error) {
	if !s.accepting.Load() {
		return nil, nil, apperr.New(apperr.CodeShuttingDown,
			"The service is shutting down and is not starting new synchronisations.")
	}

	sub, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if _, running := s.active.LoadOrStore(sub.ID, struct{}{}); running {
		return nil, nil, apperr.Newf(apperr.CodeAlreadyExists,
			"A synchronisation for %q is already running.", sub.DisplayName())
	}
	var once sync.Once
	return sub, func() { once.Do(func() { s.active.Delete(sub.ID) }) }, nil
}

// run performs one synchronisation. The caller holds the guard.
func (s *Service) run(ctx context.Context, sub *Subscription) (*SyncResult, error) {
	started := time.Now().UTC()
	logger := s.logger.With(
		"subscription_id", sub.ID,
		logging.KeyProvider, sub.Provider,
		logging.KeyArtist, sub.DisplayName(),
		logging.KeyOperation, "subscription_sync",
	)

	result := &SyncResult{
		SubscriptionID: sub.ID,
		Artist:         sub.DisplayName(),
		StartedAt:      started,
	}

	s.publish(jobs.Event{
		Type:           jobs.EventSubscriptionSyncStarted,
		SubscriptionID: sub.ID, Label: sub.DisplayName(),
	})
	logger.Info("subscription sync started")

	filter := sub.ReleaseFilter
	if !filter.Any() {
		filter = music.DefaultReleaseFilter()
	}

	catalogue, err := s.discography.ResolveArtist(ctx, discography.ArtistRequest{
		Provider: sub.Provider,
		ArtistID: sub.ArtistSourceID,
		Filter:   filter,
	}, func(_ discography.Stage, current, total int) {
		s.publish(jobs.Event{
			Type: jobs.EventSubscriptionSyncProgress, SubscriptionID: sub.ID,
			Label: sub.DisplayName(), Current: current, Total: total,
		})
	})
	if err != nil {
		return nil, s.recordFailure(ctx, sub, result, err, logger)
	}

	if catalogue.Artist.Name != "" {
		result.Artist = catalogue.Artist.DisplayName()
	}
	result.ReleasesSeen = len(catalogue.Releases)
	result.TracksSeen = len(catalogue.Groups)
	result.Warnings = append(result.Warnings, catalogue.Warnings...)

	// A release the provider could not deliver this time is worth coming back
	// for; one that is simply gone is not. The distinction decides when the
	// next run is scheduled, so it is carried rather than flattened into the
	// warning text.
	transient := catalogue.TransientWarnings

	newReleases, err := s.countNewReleases(ctx, sub.Provider, catalogue.Releases)
	if err != nil {
		return nil, s.recordFailure(ctx, sub, result, err, logger)
	}
	result.NewReleases = newReleases

	missing, err := s.classify(ctx, catalogue.Groups, result)
	if err != nil {
		return nil, s.recordFailure(ctx, sub, result, err, logger)
	}

	if sub.AutoDownload {
		// Work that never reached the queue is transient in the same sense:
		// the catalogue was read, but what it produced was not acted on.
		transient += s.queue(ctx, sub, missing, result, logger)
	}

	result.FinishedAt = time.Now().UTC()
	result.Status = StatusSuccess
	if len(result.Warnings) > 0 {
		result.Status = StatusPartial
	}

	// A partial run that lost part of the catalogue to a rate limit or an
	// outage must not be treated like a complete one: waiting a full day would
	// leave the subscription blind to everything it failed to read. A partial
	// run whose only losses are permanent keeps the normal interval, because
	// coming back sooner would find exactly the same thing.
	next := s.syncInterval
	if transient > 0 {
		// Never later than a clean run would be scheduled. Nothing stops a
		// deployment from configuring a retry interval longer than the sync
		// interval, and pushing a degraded run further out than a complete
		// one is the opposite of coming back early.
		next = min(s.retryInterval, s.syncInterval)
	}

	// A partial run is not a sync failure: the comparison finished and its
	// numbers are usable, so last_error is cleared rather than filled.
	//
	// The write does not use the run's context. A shutdown that cancels
	// between the last provider call and this line would otherwise lose a
	// result that was already complete, and the next tick would repeat a run
	// that had in fact just finished.
	if err := s.store.RecordSync(context.WithoutCancel(ctx), sub.ID, SyncOutcome{
		At:     result.FinishedAt,
		NextAt: result.FinishedAt.Add(next),
		Status: result.Status,
	}); err != nil {
		logger.Error("the sync outcome could not be stored", logging.KeyError, err.Error())
	}

	logger.Info("subscription sync finished",
		"status", string(result.Status),
		"releases_seen", result.ReleasesSeen, "new_releases", result.NewReleases,
		"tracks_seen", result.TracksSeen, "new_tracks", result.NewTracks,
		"queued_tracks", result.QueuedTracks, "skipped_tracks", result.SkippedTracks,
		"warnings", len(result.Warnings), "warnings_transient", transient,
		"next_sync_in", next.String(),
		"duration_ms", result.Duration().Milliseconds())

	s.lastResults.Store(sub.ID, result)
	s.publish(jobs.Event{
		Type: jobs.EventSubscriptionSyncCompleted, SubscriptionID: sub.ID,
		Label: result.Artist, Current: result.TracksSeen, Total: result.TracksSeen,
	})
	return result, nil
}

// countNewReleases counts the releases the library does not hold yet.
func (s *Service) countNewReleases(ctx context.Context, providerName string, releases []music.Release) (int, error) {
	var count int
	for _, release := range releases {
		if err := ctx.Err(); err != nil {
			return 0, apperr.Wrap(apperr.CodeJobCancelled, "The synchronisation was cancelled.", err)
		}
		existing, err := s.catalog.FindReleaseBySource(ctx, providerName, release.SourceID)
		if err != nil {
			return 0, err
		}
		if existing == nil {
			count++
		}
	}
	return count, nil
}

// classify sorts the distinct recordings into the three states that matter and
// returns the ones that still have to be fetched: the unknown recordings and
// the ones the catalogue knows but never produced a file for.
func (s *Service) classify(ctx context.Context, groups []discography.Group, result *SyncResult) ([]music.Track, error) {
	missing := make([]music.Track, 0, len(groups))

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "The synchronisation was cancelled.", err)
		}

		// The same lookup the download pipeline uses: ISRC first, identity key
		// and runtime second. There is no second matching system here.
		existing, err := s.catalog.FindTrack(ctx, group.Track, s.toleranceMS)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			result.NewTracks++
			missing = append(missing, group.Track)
			continue
		}

		files, err := s.files.ListByTrack(ctx, existing.ID)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			result.SkippedTracks++
			continue
		}
		// Known to the catalogue but never downloaded: not new, still missing.
		missing = append(missing, group.Track)
	}
	return missing, nil
}

// queue hands the missing recordings to the existing download pipeline.
//
// The work is grouped per release rather than per track: a first sync of a
// large discography would otherwise create hundreds of jobs, while one release
// job covers the same recordings and the worker's own skip-existing check
// drops whatever the library already owns.
// It returns how many hand-offs failed for a reason worth retrying.
func (s *Service) queue(ctx context.Context, sub *Subscription, missing []music.Track, result *SyncResult, logger *slog.Logger) int {
	if len(missing) == 0 {
		return 0
	}
	if s.downloader == nil {
		result.Warnings = append(result.Warnings,
			"Auto download is not available: the download queue is not configured.")
		return 0
	}

	// Order matters for a predictable download sequence, so the releases are
	// queued in the order the deduplicated catalogue produced them.
	order := make([]string, 0, len(missing))
	counts := make(map[string]int, len(missing))
	for _, track := range missing {
		releaseID := track.ReleaseID
		if releaseID == "" {
			// Without a release the track cannot be grouped; it is reported
			// rather than silently dropped.
			result.Warnings = append(result.Warnings,
				track.Label()+": the track has no release and was not queued.")
			continue
		}
		if _, seen := counts[releaseID]; !seen {
			order = append(order, releaseID)
		}
		counts[releaseID]++
	}

	var failed int
	for _, releaseID := range order {
		queued, err := s.downloader.EnqueueReleaseWithPriority(ctx, sub.Provider, releaseID, sub.DisplayName(), sub.DownloadPriority)
		if err != nil {
			failed++
			result.Warnings = append(result.Warnings,
				"The release could not be queued: "+apperr.MessageOf(err))
			logger.Warn("release could not be queued",
				logging.KeyRelease, releaseID,
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
			continue
		}

		if !queued {
			// An earlier run already handed this release over and its job has
			// not finished. That is not a warning — the recordings are on
			// their way — so they still count towards the report.
			logger.Debug("release already queued by an earlier run",
				logging.KeyRelease, releaseID)
		}
		result.QueuedTracks += counts[releaseID]
	}
	return failed
}

// recordFailure writes a failed run back to the subscription and returns the
// error the caller should report.
func (s *Service) recordFailure(ctx context.Context, sub *Subscription, result *SyncResult, cause error, logger *slog.Logger) error {
	result.FinishedAt = time.Now().UTC()
	result.Status = StatusFailed

	code := apperr.CodeOf(cause)
	message := apperr.MessageOf(cause)

	// The subscription must record the failure even when the run was cut short
	// by the caller's context, which is why the write does not use it.
	writeCtx := ctx
	if writeCtx.Err() != nil {
		writeCtx = context.WithoutCancel(ctx)
	}
	if err := s.store.RecordSync(writeCtx, sub.ID, SyncOutcome{
		At:     result.FinishedAt,
		NextAt: result.FinishedAt.Add(s.retryInterval),
		Status: StatusFailed,
		Error:  message,
	}); err != nil {
		logger.Error("the sync failure could not be stored", logging.KeyError, err.Error())
	}

	logger.Error("subscription sync failed",
		logging.KeyErrorCode, string(code), logging.KeyError, cause.Error())

	s.publish(jobs.Event{
		Type: jobs.EventSubscriptionSyncFailed, SubscriptionID: sub.ID,
		Label: result.Artist, ErrorCode: string(code), ErrorMessage: message,
	})
	return cause
}

// publish sends an event when a broker is configured.
func (s *Service) publish(event jobs.Event) {
	if s.broker == nil {
		return
	}
	s.broker.Publish(event)
}
