package subscriptions

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
)

// DefaultCheckInterval is how often the scheduler looks for due
// subscriptions. It is not the sync interval: a subscription is checked every
// 24 hours by default, and this is only how finely that deadline is observed.
const DefaultCheckInterval = 15 * time.Minute

// defaultBatchSize bounds how many subscriptions one tick works through, so
// that a large backlog is spread over several ticks instead of hammering a
// metadata provider in one burst.
const defaultBatchSize = 25

// SchedulerOptions configures the periodic sync.
type SchedulerOptions struct {
	Service *Service
	// Interval is how often due subscriptions are looked for.
	Interval time.Duration
	// BatchSize bounds one tick.
	BatchSize int
	Logger    *slog.Logger
}

// Scheduler runs the due subscriptions periodically.
//
// It contains no synchronisation logic of its own: it selects what is due and
// hands each subscription to the same Service.Sync a manual "check now" uses.
type Scheduler struct {
	service   *Service
	interval  time.Duration
	batchSize int
	logger    *slog.Logger

	wg        sync.WaitGroup
	stop      context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewScheduler builds the scheduler.
func NewScheduler(opts SchedulerOptions) (*Scheduler, error) {
	if opts.Service == nil {
		return nil, apperr.New(apperr.CodeInternal, "The subscription scheduler needs a service.")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultCheckInterval
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		service: opts.Service, interval: interval,
		batchSize: batchSize, logger: logger,
	}, nil
}

// Start begins the periodic check. The scheduler outlives the context it is
// started with, exactly like the job manager, so that a request scoped context
// cannot bring the background work down with it.
func (s *Scheduler) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		var runCtx context.Context
		runCtx, s.stop = context.WithCancel(context.WithoutCancel(ctx))

		s.logger.Info("subscription scheduler started",
			"check_interval", s.interval.String(),
			"sync_interval", s.service.SyncInterval().String())

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.run(runCtx)
		}()
	})
	return nil
}

// Stop ends the periodic check and waits for the run in flight.
//
// The context of the running sync is cancelled, so an HTTP call to a metadata
// provider is abandoned rather than held onto; whatever the run had already
// established is still written back, because the outcome is recorded on a
// context that the cancellation does not reach.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		if s.stop != nil {
			s.stop()
		}
		s.wg.Wait()
		s.logger.Info("subscription scheduler stopped")
	})
}

// run is the periodic loop.
func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// The first check happens at once rather than one interval later, so that
	// a restart picks up whatever became due while the process was down.
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick synchronises the subscriptions that are due and returns how many were
// attempted.
//
// The runs are sequential on purpose. The download queue resolves its jobs one
// at a time for the same reason: it keeps the load on the metadata providers
// predictable and their rate limits intact.
func (s *Scheduler) tick(ctx context.Context) int {
	if ctx.Err() != nil {
		return 0
	}

	due, err := s.service.DueForSync(ctx, time.Now().UTC(), s.batchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("due subscriptions could not be listed", logging.KeyError, err.Error())
		}
		return 0
	}
	if len(due) == 0 {
		return 0
	}

	var attempted int
	for _, sub := range due {
		// A shutdown must not start another sync; the one already running is
		// cancelled through its own context.
		if ctx.Err() != nil {
			break
		}
		attempted++

		result, err := s.service.Sync(ctx, sub.ID)
		switch {
		case err == nil:
			s.logger.Info("scheduled sync finished",
				"subscription_id", sub.ID,
				logging.KeyArtist, sub.DisplayName(),
				"status", string(result.Status),
				"new_releases", result.NewReleases,
				"new_tracks", result.NewTracks,
				"queued_tracks", result.QueuedTracks)

		case apperr.CodeOf(err) == apperr.CodeAlreadyExists:
			// A manual check is already running for this artist; the schedule
			// simply waits for the next tick.
			s.logger.Debug("scheduled sync skipped, a sync is already running",
				"subscription_id", sub.ID)

		default:
			// One artist that cannot be synchronised must not stop the ones
			// behind it. The failure is already recorded on the subscription
			// and retried on its own schedule.
			s.logger.Warn("scheduled sync failed",
				"subscription_id", sub.ID,
				logging.KeyArtist, sub.DisplayName(),
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
		}
	}
	return attempted
}
