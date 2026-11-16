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

// Reserve checks the limit for key and, when the attempt is allowed, counts it
// in the same step. Checking and counting under one lock is what keeps
// concurrent attempts from all passing a check that none of them has been
// counted against yet. The returned time identifies the reservation for
// Release.
func (l *Limiter) Reserve(key string) (bool, time.Duration, time.Time) {
	if allowed, retryAfter := l.Allow(key); !allowed {
		return false, retryAfter, time.Time{}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, exists := l.records[key]
	if !exists {
		rec = &attemptRecord{}
		l.records[key] = rec
	}
	if len(rec.timestamps) >= l.maxAttempts {
		// Another attempt took the last slot between the check and this lock.
		retryAfter := rec.timestamps[0].Add(l.window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, time.Time{}
	}
	rec.timestamps = append(rec.timestamps, now)
	return true, 0, now
}

// Release takes back one reservation made by Reserve, for an attempt that
// turned out not to be a failure. Other attempts recorded for key are kept.
func (l *Limiter) Release(key string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, exists := l.records[key]
	if !exists {
		return
	}
	for i, ts := range rec.timestamps {
		if ts.Equal(at) {
			rec.timestamps = append(rec.timestamps[:i], rec.timestamps[i+1:]...)
			break
		}
	}
	if len(rec.timestamps) == 0 {
		delete(l.records, key)
	}
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
