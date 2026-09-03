// Package lyrics resolves the lyrics of a track from external providers and
// normalises them into music.Lyrics.
//
// The text is never stored in the catalogue: it is written next to the audio
// file as a .lrc or .txt sidecar, because that file is what Plex, Jellyfin and
// Emby read.
package lyrics

import (
	"context"
	"sync"
	"time"
)

// Limiter paces the requests the lyrics providers send.
//
// LRCLIB is a free service without API keys and asks its clients explicitly to
// send requests one at a time with a short delay in between, especially when
// scanning a whole library. Exceeding that answers 429 with a Retry-After that
// must be honoured; ignoring it can get the client banned. One limiter is
// shared by every lookup, so a bulk backfill and a single refresh draw on the
// same budget instead of each running at the full rate.
//
// It is a token bucket: tokens may go negative, so a caller that finds the
// bucket empty still takes its token and waits for the deficit. That is what
// makes concurrent callers queue behind one another rather than all waking at
// the same moment.
type Limiter struct {
	mu sync.Mutex

	interval time.Duration
	burst    float64
	tokens   float64
	last     time.Time

	// now and sleep are injectable so the reservation arithmetic can be
	// tested without waiting.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewLimiter builds a limiter for a sustained rate in requests per second. A
// rate of zero or less disables pacing.
func NewLimiter(ratePerSecond float64, burst int) *Limiter {
	l := &Limiter{now: time.Now, sleep: sleepContext}
	if ratePerSecond <= 0 {
		return l
	}
	l.interval = time.Duration(float64(time.Second) / ratePerSecond)
	if l.interval <= 0 {
		l.interval = time.Nanosecond
	}
	if burst < 1 {
		burst = 1
	}
	l.burst = float64(burst)
	l.tokens = l.burst
	l.last = l.now()
	return l
}

// Enabled reports whether this limiter paces anything.
func (l *Limiter) Enabled() bool { return l.interval > 0 }

// Wait blocks until the caller may send its request, or until ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := l.reserve()
	if delay <= 0 {
		return nil
	}
	return l.sleep(ctx, delay)
}

// reserve takes one token and reports how long the caller has to wait for it.
func (l *Limiter) reserve() time.Duration {
	if !l.Enabled() {
		return 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.last.IsZero() {
		l.last = now
	}
	refill := float64(now.Sub(l.last)) / float64(l.interval)
	if refill > 0 {
		l.tokens += refill
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
	}

	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	return time.Duration(-l.tokens * float64(l.interval))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
