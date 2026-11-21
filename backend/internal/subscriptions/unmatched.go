package subscriptions

import (
	"context"
	"time"

	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
)

// MatchHistory answers how the last download of a release ended for the
// tracks no media was found for.
type MatchHistory interface {
	ReleaseMatchFailures(ctx context.Context, releaseID string) (jobs.MatchFailures, error)
}

// maxUnmatchedWait caps the distance between two attempts on a track no media
// was found for.
const maxUnmatchedWait = 14 * 24 * time.Hour

// unmatchedWaitSlack keeps the waits in step with a daily sync. A job finishes
// some time after the sync that queued it, so a wait of exactly one day would
// still be running when the next day's sync starts and would cost a second
// day.
const unmatchedWaitSlack = 6 * time.Hour

// unmatchedWait is how long a sync leaves a track alone after the given number
// of attempts ended without a media match: one day, then two, four and eight,
// and two weeks from the fifth attempt on.
//
// The first waits are short on purpose. A track that came out today is often
// missing from the media providers for a few days, and that is the case worth
// coming back for quickly. A track that still has no match after a week is
// unlikely to get one tomorrow.
func unmatchedWait(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	wait := maxUnmatchedWait
	if attempts < 5 {
		wait = time.Duration(1<<(attempts-1)) * 24 * time.Hour
	}
	return wait - unmatchedWaitSlack
}

// unmatchedLookup answers, for one sync, whether a missing track is still
// waiting after an attempt that found no media for it. The history is read
// once per release.
type unmatchedLookup struct {
	service *Service
	now     time.Time
	waiting map[string]map[string]struct{} // release id -> track source ids
}

func (s *Service) newUnmatchedLookup() *unmatchedLookup {
	return &unmatchedLookup{
		service: s,
		now:     time.Now().UTC(),
		waiting: make(map[string]map[string]struct{}),
	}
}

// waits reports whether the track should be left alone in this run.
//
// A history that cannot be read never holds a track back: the worst outcome
// of asking again is one more failed job, while a wrong "wait" would hide a
// track that could have been fetched.
func (l *unmatchedLookup) waits(ctx context.Context, track music.Track) bool {
	if l.service.matchHistory == nil || track.ReleaseID == "" || track.SourceID == "" {
		return false
	}

	ids, known := l.waiting[track.ReleaseID]
	if !known {
		ids = nil
		failures, err := l.service.matchHistory.ReleaseMatchFailures(ctx, track.ReleaseID)
		switch {
		case err != nil:
			l.service.logger.Warn("the match history of a release could not be read",
				logging.KeyRelease, track.ReleaseID, logging.KeyError, err.Error())
		case failures.Attempts > 0 && l.now.Before(failures.LastAt.Add(unmatchedWait(failures.Attempts))):
			ids = make(map[string]struct{}, len(failures.TrackSourceIDs))
			for _, id := range failures.TrackSourceIDs {
				ids[id] = struct{}{}
			}
		}
		l.waiting[track.ReleaseID] = ids
	}

	_, waiting := ids[track.SourceID]
	return waiting
}
