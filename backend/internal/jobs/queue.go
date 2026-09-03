package jobs

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// dispatchInterval is the safety net of the dispatcher. Normal operation is
// driven by the wake signal; the ticker only catches a missed signal.
const dispatchInterval = 5 * time.Second

// storageMonitorInterval is how often the storage health is probed in the background.
const storageMonitorInterval = 30 * time.Second

// Start brings the queue up: work left behind by a previous process is
// recovered, then the resolver, storage monitor and the dispatcher begin.
func (m *Manager) Start(ctx context.Context) error {
	var err error
	m.startOnce.Do(func() {
		m.ctx, m.stop = context.WithCancel(context.WithoutCancel(ctx))
		err = m.recover(m.ctx)
		if err != nil {
			m.stop()
			return
		}

		m.wg.Add(3)
		go func() {
			defer m.wg.Done()
			m.runResolver(m.ctx)
		}()
		go func() {
			defer m.wg.Done()
			m.runStorageMonitor(m.ctx)
		}()
		go func() {
			defer m.wg.Done()
			m.runDispatcher(m.ctx)
		}()
	})
	return err
}

// Stop interrupts every running worker and waits for it to finish. Running
// yt-dlp processes are terminated through the job contexts, while persisted
// work is reset for recovery by the next process.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		m.BeginShutdown()
		m.stopping.Store(true)
		if m.stop != nil {
			m.stop()
		}
		m.runs.Range(func(_, value any) bool {
			if run, ok := value.(*jobRun); ok {
				run.cancel()
			}
			return true
		})
		m.wg.Wait()

		// A service shutdown is not a user cancellation. Put interrupted work
		// back into the persistent queue so the next process resumes it, and so
		// that nothing is left recorded as "downloading" while nothing runs.
		resetCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.resetInterrupted(resetCtx, "shutdown")
	})
}

// resetInterrupted returns every job and item that a process left in an active
// working state to a state the queue can start from again.
func (m *Manager) resetInterrupted(ctx context.Context, phase string) {
	items, err := m.store.ResetInFlightItems(ctx)
	if err != nil {
		m.logger.Error("interrupted items could not be reset",
			"phase", phase, logging.KeyError, err.Error())
	} else if items > 0 {
		m.logger.Info("interrupted items reset", "phase", phase, "items", items)
	}

	jobs, err := m.store.ResetInterruptedJobs(ctx)
	if err != nil {
		m.logger.Error("interrupted jobs could not be reset",
			"phase", phase, logging.KeyError, err.Error())
	} else if jobs > 0 {
		m.logger.Info("interrupted jobs requeued", "phase", phase, "jobs", jobs)
	}
}

// recover picks the work of a previous process back up.
func (m *Manager) recover(ctx context.Context) error {
	m.resetInterrupted(ctx, "startup")

	unfinished, err := m.store.ListUnfinished(ctx)
	if err != nil {
		return err
	}
	for _, job := range unfinished {
		m.registerRun(job.ID)

		hasItems, err := m.store.HasItems(ctx, job.ID)
		if err != nil {
			return err
		}
		if hasItems {
			m.logger.Info("job resumed", logging.KeyJobID, job.ID, "status", string(job.Status))
			continue
		}
		m.queueForResolve(job.ID)
	}
	if len(unfinished) > 0 {
		m.signal()
	}
	return nil
}

// runResolver pulls newly enqueued jobs and discovers their tracks.

func (m *Manager) runResolver(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case jobID := <-m.resolveQueue:
			m.resolve(ctx, jobID)
		}
	}
}

// runStorageMonitor periodically re-checks the storage status and wakes the
// queue if a previously broken storage became healthy again.
func (m *Manager) runStorageMonitor(ctx context.Context) {
	if m.library == nil || m.library.Guard() == nil {
		return
	}

	ticker := time.NewTicker(storageMonitorInterval)
	defer ticker.Stop()

	var lastHealth storage.HealthStatus = storage.HealthHealthy

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			health := m.library.Guard().CheckHealth(ctx, true)
			if health.Status != lastHealth {
				m.logger.Info("storage health state changed",
					"previous", string(lastHealth),
					"current", string(health.Status),
					"error", health.LastError,
				)
				lastHealth = health.Status
				if health.Status == storage.HealthHealthy {
					// Storage is healthy; wake any items waiting for storage or space
					m.signal()
				}
			}
		}
	}
}

// runDispatcher hands ready items to the worker pool.
func (m *Manager) runDispatcher(ctx context.Context) {
	ticker := time.NewTicker(dispatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
		m.dispatch(ctx)
	}
}

type jobCandidate struct {
	job   Job
	ready []Item
}

// effectivePriority calculates priority rank with starvation protection aging.
func effectivePriority(j Job, now time.Time) int {
	baseRank := j.Priority.Rank()
	age := now.Sub(j.CreatedAt)
	if baseRank == 1 && age >= 15*time.Minute {
		return 2 // normal -> high
	}
	if baseRank == 0 && age >= 60*time.Minute {
		return 2 // low -> high
	}
	if baseRank == 0 && age >= 30*time.Minute {
		return 1 // low -> normal
	}
	return baseRank
}

// dispatch starts a worker for every ready item that is not already running.
func (m *Manager) dispatch(ctx context.Context) {
	if m.queuePaused.Load() {
		return
	}

	maxWorkers := m.MaxWorkers()
	if int(m.activeWorkers.Load()) >= maxWorkers {
		return
	}

	candidates := m.collectCandidates(ctx)
	if len(candidates) == 0 {
		return
	}

	activePerJob := make(map[string]int)

	m.inFlight.Range(func(key, value any) bool {
		if jobID, ok := value.(string); ok {
			activePerJob[jobID]++
		}
		return true
	})

	// Pass 1: Fair interleaving (cap at 1 worker per job if multiple runnable jobs exist)
	for i := range candidates {
		if ctx.Err() != nil || int(m.activeWorkers.Load()) >= maxWorkers {
			return
		}
		c := &candidates[i]
		if len(candidates) > 1 && activePerJob[c.job.ID] >= 1 {
			continue
		}
		for idx, item := range c.ready {
			if _, running := m.inFlight.LoadOrStore(item.ID, c.job.ID); !running {
				m.activeWorkers.Add(1)
				activePerJob[c.job.ID]++
				c.ready = append(c.ready[:idx], c.ready[idx+1:]...)
				m.startWorker(c.job, item)
				break // 1 slot allocated for this job in pass 1
			}
		}
	}

	// Pass 2: Allocate remaining worker capacity in priority order
	for i := range candidates {
		if ctx.Err() != nil || int(m.activeWorkers.Load()) >= maxWorkers {
			return
		}
		c := &candidates[i]
		for idx := 0; idx < len(c.ready); {
			if int(m.activeWorkers.Load()) >= maxWorkers {
				return
			}
			item := c.ready[idx]
			if _, running := m.inFlight.LoadOrStore(item.ID, c.job.ID); !running {
				m.activeWorkers.Add(1)
				activePerJob[c.job.ID]++
				c.ready = append(c.ready[:idx], c.ready[idx+1:]...)
				m.startWorker(c.job, item)
			} else {
				idx++
			}
		}
	}
}

func (m *Manager) collectCandidates(ctx context.Context) []jobCandidate {
	jobs, err := m.store.ListUnfinished(ctx)
	if err != nil {
		if ctx.Err() == nil {
			m.logger.Error("pending jobs could not be listed", logging.KeyError, err.Error())
		}
		return nil
	}

	now := m.now().UTC()
	insideWindow := m.isInsideDownloadWindow(now)

	storageHealthy := true
	if m.library != nil && m.library.Guard() != nil {
		health := m.library.Guard().CheckHealth(ctx, false)
		if health.Status != storage.HealthHealthy {
			storageHealthy = false
		}
	}

	// Apply starvation aging sort: effectivePriority DESC, CreatedAt ASC, ID ASC
	sort.SliceStable(jobs, func(i, j int) bool {
		rI := effectivePriority(jobs[i], now)
		rJ := effectivePriority(jobs[j], now)
		if rI != rJ {
			return rI > rJ
		}
		if !jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
		}
		return jobs[i].ID < jobs[j].ID
	})

	var candidates []jobCandidate

	for _, job := range jobs {
		if ctx.Err() != nil {
			return nil
		}
		if job.Total == 0 && job.Status == StatusQueued {
			continue // still waiting for the resolver
		}

		items, err := m.store.ListItems(ctx, job.ID)
		if err != nil {
			if ctx.Err() == nil {
				m.logger.Error("job items could not be listed",
					logging.KeyJobID, job.ID, logging.KeyError, err.Error())
			}
			continue
		}

		if len(items) == 0 {
			continue
		}

		derivedStatus := DeriveParentStatus(items)
		if derivedStatus != job.Status && !job.Status.Terminal() {
			m.setStatus(ctx, &job, derivedStatus)
		}

		var readyItems []Item
		allTerminal := true

		for _, it := range items {
			if !it.Status.Terminal() {
				allTerminal = false
			}

			// If item is already running, do not count as ready to be launched again
			if _, running := m.inFlight.Load(it.ID); running {
				continue
			}

			switch it.Status {
			case ItemPending:
				if insideWindow {
					readyItems = append(readyItems, it)
				}

			case ItemRetryWait:
				if insideWindow && (it.NextRetryAt == nil || !it.NextRetryAt.After(now)) {
					readyItems = append(readyItems, it)
				}

			case ItemWaitingStorage:
				// If already staged with hash, can finalize to /music even outside window
				if it.StagedSHA256 != "" {
					if storageHealthy || m.allowOfflineStaging.Load() {
						readyItems = append(readyItems, it)
					}
				} else if insideWindow && (storageHealthy || m.allowOfflineStaging.Load()) {
					readyItems = append(readyItems, it)
				}

			case ItemWaitingSpace:
				if (m.staging == nil || m.staging.CheckSpace() == nil) && insideWindow {
					readyItems = append(readyItems, it)
				}
			}
		}

		if allTerminal {
			m.finalize(ctx, job.ID)
			continue
		}

		if len(readyItems) > 0 {
			candidates = append(candidates, jobCandidate{
				job:   job,
				ready: readyItems,
			})
		}
	}

	return candidates
}

// startWorker processes one item in its own goroutine.
func (m *Manager) startWorker(job Job, item Item) {
	m.wg.Add(1)
	go func() {
		defer func() {
			m.activeWorkers.Add(-1)
			m.inFlight.Delete(item.ID)
			m.wg.Done()
			m.signal()
		}()

		run := m.registerRun(job.ID)
		itemCtx, cancel := context.WithTimeout(run.ctx, m.trackTimeout)
		defer cancel()

		(&worker{manager: m}).process(itemCtx, job, item)
	}()
}

// resolve turns a job into the list of tracks it will download.
func (m *Manager) resolve(ctx context.Context, jobID string) {
	job, err := m.store.Get(ctx, jobID)
	if err != nil {
		m.logger.Error("job could not be loaded", logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}
	if job.Status.Terminal() {
		return
	}

	hasItems, err := m.store.HasItems(ctx, jobID)
	if err != nil {
		m.fail(ctx, job, err)
		return
	}
	if hasItems {
		m.signal()
		return
	}

	run := m.registerRun(jobID)
	logger := m.logger.With(logging.KeyJobID, jobID, "type", string(job.Type))

	label := job.Label
	tracks, err := m.resolveTracks(run.ctx, job, logger)
	if err != nil {
		if m.Stopping() {
			return
		}
		if run.ctx.Err() != nil {
			m.markCancelled(ctx, job)
			return
		}
		m.fail(ctx, job, err)
		return
	}

	items := make([]Item, 0, len(tracks))
	for i, track := range tracks {
		items = append(items, Item{
			Position:    i,
			Status:      ItemPending,
			Track:       track,
			Label:       track.Label(),
			MaxAttempts: 5,
		})
	}
	if err := m.store.AddItems(run.ctx, jobID, items); err != nil {
		if m.Stopping() {
			return
		}
		m.fail(ctx, job, err)
		return
	}
	job.Total = len(items)

	if job.Label != label {
		if err := m.store.SetLabel(ctx, jobID, job.Label); err != nil {
			logger.Warn("the job label could not be stored", logging.KeyError, err.Error())
		}
	}

	logger.Info("job resolved", "tracks", len(items))

	if len(items) == 0 {
		m.setStatus(ctx, job, StatusFinalizing)
		m.complete(ctx, jobID)
		return
	}

	m.setStatus(ctx, job, StatusMatching)
	m.broker.Publish(Event{
		Type: EventJobStatus, JobID: jobID, Status: job.Status,
		Label: job.Label, Current: 0, Total: job.Total,
	})
	m.signal()
}

// resolveTracks runs the catalogue resolution for a job.
func (m *Manager) resolveTracks(ctx context.Context, job *Job, logger *slog.Logger) ([]music.Track, error) {
	switch job.Type {
	case TypeArtist:
		result, err := m.discography.ResolveArtist(ctx, discography.ArtistRequest{
			Provider: job.MetadataProvider,
			ArtistID: job.TargetID,
			Filter:   job.Options.ReleaseFilter,
		}, func(stage discography.Stage, current, total int) {
			m.setStatus(ctx, job, statusForStage(stage))
			m.broker.Publish(Event{
				Type: EventJobStatus, JobID: job.ID, Status: job.Status,
				Label: job.Label, Current: current, Total: total,
			})
		})
		if err != nil {
			return nil, err
		}
		if job.Label == "" || job.Label == string(TypeArtist)+" "+job.TargetID {
			job.Label = result.Artist.DisplayName()
		}
		for _, warning := range result.Warnings {
			logger.Warn("release skipped during resolution", "detail", warning)
		}
		return result.Tracks(), nil

	case TypeRelease:
		m.setStatus(ctx, job, StatusResolvingReleases)
		result, err := m.discography.ResolveRelease(ctx, job.MetadataProvider, job.TargetID)
		if err != nil {
			return nil, err
		}
		if len(result.Releases) > 0 {
			job.Label = result.Releases[0].DisplayTitle()
		}
		return result.Tracks(), nil

	case TypeTrack:
		m.setStatus(ctx, job, StatusResolvingTracks)
		track, err := m.discography.ResolveTrack(ctx, job.MetadataProvider, job.TargetID, job.Options.ReleaseID)
		if err != nil {
			return nil, err
		}
		job.Label = track.Label()
		return []music.Track{*track}, nil

	default:
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "%q is not a valid job type.", job.Type)
	}
}

// statusForStage maps a resolution stage onto the job state.
func statusForStage(stage discography.Stage) Status {
	switch stage {
	case discography.StageArtist:
		return StatusResolvingArtist
	case discography.StageReleases:
		return StatusResolvingReleases
	case discography.StageTracks:
		return StatusResolvingTracks
	case discography.StageDedup:
		return StatusDeduplicating
	default:
		return StatusQueued
	}
}

// finalize closes a job once every item reached a terminal state.
func (m *Manager) finalize(ctx context.Context, jobID string) {
	job, err := m.store.RefreshCounters(ctx, jobID)
	if err != nil {
		m.logger.Error("job could not be finalised", logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}
	if job.Status.Terminal() || job.Processed() < job.Total {
		return
	}

	items, err := m.store.ListItems(ctx, jobID)
	if err != nil {
		m.logger.Error("job items could not be listed during finalization", logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}

	finalStatus := DeriveParentStatus(items)
	if finalStatus.Terminal() {
		if finalStatus == StatusCompleted {
			m.complete(ctx, jobID)
		} else if finalStatus == StatusCancelled {
			m.markCancelled(ctx, job)
		} else {
			m.fail(ctx, job, apperr.New(apperr.CodeDownloadFailed, "All tracks in this job failed."))
		}
	}
}

// complete marks a job as completed and publishes its summary.
func (m *Manager) complete(ctx context.Context, jobID string) {
	job, err := m.store.RefreshCounters(ctx, jobID)
	if err != nil {
		m.logger.Error("job could not be completed", logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}
	if job.Status.Terminal() {
		return
	}
	if err := m.store.SetStatus(ctx, jobID, StatusCompleted, "", ""); err != nil {
		m.logger.Error("job could not be marked as completed",
			logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}
	m.releaseRun(jobID)

	summary := job.Summary()
	m.logger.Info("job completed",
		logging.KeyJobID, jobID,
		"total", summary.Total, "completed", summary.Completed,
		"failed", summary.Failed, "skipped", summary.Skipped)

	m.broker.Publish(Event{
		Type: EventJobCompleted, JobID: jobID, Status: StatusCompleted,
		Label: job.Label, Current: job.Processed(), Total: job.Total,
		Summary: &summary,
	})
}

// markCancelled records that a job stopped because it was cancelled.
func (m *Manager) markCancelled(ctx context.Context, job *Job) {
	if err := m.store.SetStatus(ctx, job.ID, StatusCancelled,
		string(apperr.CodeJobCancelled), "The job was cancelled."); err != nil {
		m.logger.Debug("cancelled job could not be updated",
			logging.KeyJobID, job.ID, logging.KeyError, err.Error())
	}
	if _, err := m.store.CancelPendingItems(ctx, job.ID); err != nil {
		m.logger.Error("pending items could not be cancelled",
			logging.KeyJobID, job.ID, logging.KeyError, err.Error())
	}
	m.releaseRun(job.ID)
	m.broker.Publish(Event{
		Type: EventJobCancelled, JobID: job.ID, Status: StatusCancelled, Label: job.Label,
	})
}

// queueForResolve hands a job to the resolver without blocking the caller.
func (m *Manager) queueForResolve(jobID string) {
	select {
	case m.resolveQueue <- jobID:
	default:
		go func() {
			select {
			case m.resolveQueue <- jobID:
			case <-m.resolveDone():
			}
		}()
	}
}

// resolveDone returns a channel that closes when the manager stops.
func (m *Manager) resolveDone() <-chan struct{} {
	if m.ctx == nil {
		return context.Background().Done()
	}
	return m.ctx.Done()
}

// signal wakes the dispatcher.
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// registerRun returns the cancellation handle of a job.
func (m *Manager) registerRun(jobID string) *jobRun {
	if existing, ok := m.runs.Load(jobID); ok {
		return existing.(*jobRun)
	}

	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	run := &jobRun{ctx: ctx, cancel: cancel}

	actual, loaded := m.runs.LoadOrStore(jobID, run)
	if loaded {
		cancel()
		return actual.(*jobRun)
	}
	return run
}

// cancelRun cancels a running job and forgets its handle.
func (m *Manager) cancelRun(jobID string) {
	if value, ok := m.runs.LoadAndDelete(jobID); ok {
		value.(*jobRun).cancel()
	}
}

// releaseRun forgets the handle of a finished job.
func (m *Manager) releaseRun(jobID string) {
	if value, ok := m.runs.LoadAndDelete(jobID); ok {
		value.(*jobRun).cancel()
	}
}
