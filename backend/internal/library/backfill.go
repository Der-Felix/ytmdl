package library

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/music"
)

// BackfillCooldown is how long a negative lyrics result is trusted. It is what
// keeps a repeated run from asking a free public service about the same missing
// track over and over.
const BackfillCooldown = 14 * 24 * time.Hour

// BackfillLimit caps one run. A large library is worked through in several
// runs rather than in one very long request to the providers.
const BackfillLimit = 500

// BackfillStatus is the lifecycle state of a backfill run.
type BackfillStatus string

const (
	BackfillRunning   BackfillStatus = "running"
	BackfillCompleted BackfillStatus = "completed"
	BackfillFailed    BackfillStatus = "failed"
)

// BackfillResult reports what one run did.
type BackfillResult struct {
	Status       BackfillStatus `json:"status"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
	Candidates   int            `json:"candidates"`
	Processed    int            `json:"processed"`
	Written      int            `json:"written"`
	Synced       int            `json:"synced"`
	Plain        int            `json:"plain"`
	Instrumental int            `json:"instrumental"`
	Missing      int            `json:"missing"`
	Remaining    int            `json:"remaining"`
	Warnings     []string       `json:"warnings,omitempty"`
}

// backfillState holds the single run the service allows at a time.
type backfillState struct {
	mu     sync.RWMutex
	active *BackfillResult
	latest *BackfillResult
}

// StartBackfillLyrics initiates a lyrics backfill run in the background.
// It returns the initial BackfillResult and http 202 immediately.
func (s *Service) StartBackfillLyrics() (*BackfillResult, error) {
	if s.lyrics == nil {
		return nil, apperr.New(apperr.CodeInternal, "No lyrics resolver is configured.")
	}
	if s.ctx != nil && s.ctx.Err() != nil {
		return nil, apperr.New(apperr.CodeShuttingDown, "The library service is shutting down.")
	}

	s.backfill.mu.Lock()
	defer s.backfill.mu.Unlock()

	if s.backfill.active != nil && s.backfill.active.Status == BackfillRunning {
		return nil, apperr.New(apperr.CodeAlreadyExists, "A lyrics backfill is already running.")
	}

	candidates, err := s.catalog.ListTracksNeedingLyrics(s.ctx,
		time.Now().UTC().Add(-BackfillCooldown), BackfillLimit)
	if err != nil {
		return nil, err
	}

	result := &BackfillResult{
		Status:     BackfillRunning,
		StartedAt:  time.Now().UTC(),
		Candidates: len(candidates),
	}
	s.backfill.active = result

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runBackfill(s.ctx, result, candidates)
	}()

	snapshot := *result
	return &snapshot, nil
}

// BackfillLyrics runs a backfill synchronously. It is used in tests and maintenance tasks.
func (s *Service) BackfillLyrics(ctx context.Context) (*BackfillResult, error) {
	if s.lyrics == nil {
		return nil, apperr.New(apperr.CodeInternal, "No lyrics resolver is configured.")
	}
	s.backfill.mu.Lock()
	if s.backfill.active != nil && s.backfill.active.Status == BackfillRunning {
		s.backfill.mu.Unlock()
		return nil, apperr.New(apperr.CodeAlreadyExists, "A lyrics backfill is already running.")
	}

	candidates, err := s.catalog.ListTracksNeedingLyrics(ctx,
		time.Now().UTC().Add(-BackfillCooldown), BackfillLimit)
	if err != nil {
		s.backfill.mu.Unlock()
		return nil, err
	}

	result := &BackfillResult{
		Status:     BackfillRunning,
		StartedAt:  time.Now().UTC(),
		Candidates: len(candidates),
	}
	s.backfill.active = result
	s.backfill.mu.Unlock()

	s.runBackfill(ctx, result, candidates)
	return s.BackfillStatusOf(), nil
}

func (s *Service) runBackfill(ctx context.Context, result *BackfillResult, candidates []repository.StoredTrack) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("panic recovered in lyrics backfill",
				logging.KeyOperation, "lyrics_backfill",
				"panic", rec)
			s.backfill.mu.Lock()
			result.Status = BackfillFailed
			result.Warnings = append(result.Warnings, fmt.Sprintf("Panic: %v", rec))
			s.backfill.mu.Unlock()
		}
		finished := time.Now().UTC()
		s.backfill.mu.Lock()
		result.FinishedAt = &finished
		saved := *result
		if len(result.Warnings) > 0 {
			saved.Warnings = make([]string, len(result.Warnings))
			copy(saved.Warnings, result.Warnings)
		}
		s.backfill.latest = &saved
		s.backfill.active = nil
		s.backfill.mu.Unlock()
	}()

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			s.backfill.mu.Lock()
			result.Status = BackfillFailed
			result.Warnings = append(result.Warnings, "The backfill was cancelled.")
			result.Remaining = len(candidates) - result.Processed
			s.backfill.mu.Unlock()
			return
		}

		outcome, err := s.RefreshTrackLyrics(ctx, candidate.Track.ID)
		s.backfill.mu.Lock()
		result.Processed++
		if err != nil {
			if wait, limited := lyrics.RetryAfter(err); limited {
				s.logger.Warn("lyrics backfill stopped by a rate limit",
					logging.KeyOperation, "lyrics_backfill",
					"retry_after_ms", wait.Milliseconds())
				result.Status = BackfillFailed
				result.Warnings = append(result.Warnings,
					"The lyrics provider is rate limiting; the run was stopped. Try again later.")
				result.Remaining = len(candidates) - result.Processed
				s.backfill.mu.Unlock()
				return
			}
			result.Warnings = append(result.Warnings, candidate.Track.Title+": "+apperr.MessageOf(err))
			s.backfill.mu.Unlock()
			continue
		}

		switch outcome.State {
		case music.LyricsAvailableSynced:
			result.Written++
			result.Synced++
		case music.LyricsAvailablePlain:
			result.Written++
			result.Plain++
		case music.LyricsInstrumental:
			result.Instrumental++
		default:
			result.Missing++
		}
		s.backfill.mu.Unlock()
	}

	s.backfill.mu.Lock()
	result.Status = BackfillCompleted
	if len(candidates) == BackfillLimit {
		result.Remaining = -1 // unknown, but at least one more run is worthwhile
	}
	s.backfill.mu.Unlock()
}

// BackfillStatusOf returns the running run, or the last finished one.
func (s *Service) BackfillStatusOf() *BackfillResult {
	s.backfill.mu.RLock()
	defer s.backfill.mu.RUnlock()

	current := s.backfill.active
	if current == nil {
		current = s.backfill.latest
	}
	if current == nil {
		return nil
	}
	snapshot := *current
	if len(current.Warnings) > 0 {
		snapshot.Warnings = make([]string, len(current.Warnings))
		copy(snapshot.Warnings, current.Warnings)
	}
	return &snapshot
}

// PreviewBackfillLyrics calculates candidate counts for lyrics backfill without modifying anything.
func (s *Service) PreviewBackfillLyrics(ctx context.Context) (repository.LyricsStats, error) {
	cutoff := time.Now().UTC().Add(-BackfillCooldown)
	return s.catalog.LyricsStats(ctx, cutoff)
}
