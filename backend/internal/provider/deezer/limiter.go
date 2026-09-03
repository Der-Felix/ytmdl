package deezer

import (
	"context"
	"sync"
	"time"
)

// limiter paces the requests one Deezer client sends.
//
// Deezer allows roughly 50 requests per five seconds per address and answers
// anything beyond that with a quota error rather than with data. The client
// used to issue requests as fast as the network allowed — measured at 18 to 26
// per second — which cost a large artist most of its catalogue.
//
// It is a token bucket: the burst absorbs the short flurries an interactive
// request makes, and the refill rate is the sustained ceiling. One limiter
// belongs to one client, so everything that talks to Deezer through that
// client — subscription syncs, artist pages, searches — draws on the same
// budget instead of each running at the full rate.
type limiter struct {
	mu sync.Mutex

	// interval is the time one token takes to refill; burst is the bucket's
	// capacity. A non-positive rate disables pacing entirely.
	interval time.Duration
	burst    float64

	// tokens may go negative: a caller that finds the bucket empty still takes
	// its token and waits for the deficit, which is what makes concurrent
	// callers queue behind one another instead of all waking at once.
	tokens float64
	last   time.Time

	// now is injectable so the reservation arithmetic can be tested without
	// sleeping.
	now func() time.Time
}

// newLimiter builds a limiter for the given sustained rate in requests per
// second. A rate of zero or less disables pacing.
func newLimiter(ratePerSecond float64, burst int) *limiter {
	l := &limiter{now: time.Now}
	if ratePerSecond <= 0 {
		return l
	}
	l.interval = time.Duration(float64(time.Second) / ratePerSecond)
	if l.interval <= 0 {
		l.interval = time.Nanosecond
	}
	// A burst below one would stall every single request.
	l.burst = float64(max(burst, 1))
	l.tokens = l.burst
	return l
}

// enabled reports whether this limiter paces anything.
func (l *limiter) enabled() bool { return l.interval > 0 }

// reserve takes one token and returns how long the caller must wait before
// using it.
func (l *limiter) reserve() time.Duration {
	if !l.enabled() {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.last.IsZero() {
		l.last = now
	}

	// Refill for the time that passed, capped at the burst so that an idle
	// period cannot buy an unbounded burst afterwards.
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = min(l.burst, l.tokens+float64(elapsed)/float64(l.interval))
		l.last = now
	}

	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	return time.Duration(-l.tokens * float64(l.interval))
}

// Wait blocks until this caller may send its request. A cancelled context ends
// the wait immediately and returns its error, so a shutdown never has to sit
// out a backoff.
func (l *limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := l.reserve()
	if delay <= 0 {
		return nil
	}
	return sleep(ctx, delay)
}

// sleep waits for d, or returns early when the context ends. The timer is
// always stopped, so a cancelled wait leaves nothing behind.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
