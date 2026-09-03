package auth

import (
	"sync"
	"time"
)

type attemptRecord struct {
	timestamps []time.Time
}

// Limiter implements an in-memory sliding window rate limiter.
type Limiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	records     map[string]*attemptRecord
	stopCh      chan struct{}
}

// NewLimiter creates a new rate limiter that allows maxAttempts within window.
func NewLimiter(maxAttempts int, window time.Duration) *Limiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	l := &Limiter{
		maxAttempts: maxAttempts,
		window:      window,
		records:     make(map[string]*attemptRecord),
		stopCh:      make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if an action for key is permitted under the rate limit.
// If not allowed, it returns false and the duration until the oldest attempt expires.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, exists := l.records[key]
	if !exists {
		return true, 0
	}

	// Filter out expired timestamps
	cutoff := now.Add(-l.window)
	valid := rec.timestamps[:0]
	for _, ts := range rec.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	rec.timestamps = valid

	if len(rec.timestamps) >= l.maxAttempts {
		oldest := rec.timestamps[0]
		retryAfter := oldest.Add(l.window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter
	}

	return true, 0
}

// RecordFailure records a failed attempt for key.
func (l *Limiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, exists := l.records[key]
	if !exists {
		rec = &attemptRecord{}
		l.records[key] = rec
	}
	rec.timestamps = append(rec.timestamps, now)
}

// RecordSuccess clears any failure records for key upon successful authentication.
func (l *Limiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, key)
}

func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			cutoff := now.Add(-l.window)
			for k, rec := range l.records {
				valid := rec.timestamps[:0]
				for _, ts := range rec.timestamps {
					if ts.After(cutoff) {
						valid = append(valid, ts)
					}
				}
				if len(valid) == 0 {
					delete(l.records, k)
				} else {
					rec.timestamps = valid
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Close stops the background cleanup loop.
func (l *Limiter) Close() {
	select {
	case <-l.stopCh:
	default:
		close(l.stopCh)
	}
}
