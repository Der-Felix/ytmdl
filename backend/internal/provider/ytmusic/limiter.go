package ytmusic

import (
	"context"
	"sync"
	"time"
)

// requestLimiter is a cancellation-aware token bucket used immediately before
// every InnerTube HTTP request. It intentionally has no background goroutine.
type requestLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    float64
	tokens   float64
	last     time.Time
	serial   chan struct{}
}

func newRequestLimiter(requestsPerSecond float64, burst int) *requestLimiter {
	if requestsPerSecond <= 0 {
		requestsPerSecond = DefaultInnerTubeRequestsPerSecond
	}
	if burst <= 0 {
		burst = DefaultInnerTubeBurst
	}
	return &requestLimiter{
		interval: time.Duration(float64(time.Second) / requestsPerSecond),
		burst:    float64(burst),
		tokens:   float64(burst),
		serial:   make(chan struct{}, 1),
	}
}

func (l *requestLimiter) admit(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case l.serial <- struct{}{}:
	}
	if err := l.wait(ctx); err != nil {
		<-l.serial
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { <-l.serial }) }, nil
}

func (l *requestLimiter) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := l.reserve()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *requestLimiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.last.IsZero() {
		l.last = now
	}
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
