package mediasession

import (
	"context"
	"sync"
	"time"
)

// Limiter implements a token bucket rate limiter for media session and
// platform-family requests. Tokens can go negative so concurrent callers
// queue deterministically behind each other.
type Limiter struct {
	mu sync.Mutex

	rate     float64
	interval time.Duration
	burst    float64
	tokens   float64
	last     time.Time

	// now and sleep are injectable so reservation arithmetic and context
	// cancellation can be tested without waiting for real wall-clock time.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewLimiter initializes a token bucket limiter for ratePerSecond and burst.
// A non-positive ratePerSecond disables pacing.
func NewLimiter(ratePerSecond float64, burst int) *Limiter {
	l := &Limiter{
		rate:  ratePerSecond,
		now:   time.Now,
		sleep: defaultSleepContext,
	}
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
	return l
}

// Enabled reports whether pacing is active.
func (l *Limiter) Enabled() bool {
	return l != nil && l.interval > 0
}

// Rate returns the configured requests per second.
func (l *Limiter) Rate() float64 {
	if l == nil {
		return 0
	}
	return l.rate
}

// Burst returns the configured burst capacity.
func (l *Limiter) Burst() int {
	if l == nil {
		return 0
	}
	return int(l.burst)
}

// Wait reserves a token and blocks until the caller is permitted to proceed,
// or until ctx is cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil || !l.Enabled() {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := l.reserve()
	if delay <= 0 {
		return nil
	}
	return l.sleep(ctx, delay)
}

// reserve deducts one token and returns the wait duration required before the request may proceed.
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

func defaultSleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
