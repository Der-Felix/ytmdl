package jobs

import "time"

// MatchFailures describes how the most recent finished job for a release
// ended, as far as tracks without a media match are concerned.
//
// It is zero when there is no finished job for the release, or when that job
// lost a track for any other reason: only a job whose failures are all
// MATCH_FAILED says "this was tried and nothing fits", which is the one case a
// caller may want to wait on instead of trying again at once.
type MatchFailures struct {
	// Attempts counts the finished jobs for the release that left at least
	// one track without a match.
	Attempts int
	// LastAt is when the most recent of those jobs finished.
	LastAt time.Time
	// TrackSourceIDs holds the provider identities of the tracks the most
	// recent job could not match.
	TrackSourceIDs []string
}
