package mediasession

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLimiter_FastDeterministicPacing(t *testing.T) {
	// Fake clock
	simulatedTime := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	var sleepDelays []time.Duration

	l := NewLimiter(2.0, 2) // 2 req/s, burst 2 -> interval = 500ms
	l.now = func() time.Time {
		return simulatedTime
	}
	l.sleep = func(ctx context.Context, d time.Duration) error {
		sleepDelays = append(sleepDelays, d)
		simulatedTime = simulatedTime.Add(d)
		return nil
	}

	ctx := context.Background()

	// 1st request: burst token available immediately
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("1st request failed: %v", err)
	}
	if len(sleepDelays) != 0 {
		t.Errorf("1st request should not sleep, got %v", sleepDelays)
	}

	// 2nd request: 2nd burst token available immediately
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("2nd request failed: %v", err)
	}
	if len(sleepDelays) != 0 {
		t.Errorf("2nd request should not sleep, got %v", sleepDelays)
	}

	// 3rd request: burst exhausted, must wait interval (500ms)
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("3rd request failed: %v", err)
	}
	if len(sleepDelays) != 1 || sleepDelays[0] != 500*time.Millisecond {
		t.Errorf("3rd request sleep delay = %v, want [500ms]", sleepDelays)
	}

	// 4th request: must wait another 500ms
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("4th request failed: %v", err)
	}
	if len(sleepDelays) != 2 || sleepDelays[1] != 500*time.Millisecond {
		t.Errorf("4th request sleep delay = %v, want 500ms", sleepDelays)
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	l := NewLimiter(1.0, 1) // 1 req/s
	ctx, cancel := context.WithCancel(context.Background())

	// Exhaust burst
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("1st request failed: %v", err)
	}

	// Cancel context immediately
	cancel()
	err := l.Wait(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestLimiter_IndependentPerSession(t *testing.T) {
	// Two distinct session limiters
	l1 := NewLimiter(1.0, 1)
	l2 := NewLimiter(1.0, 1)

	ctx := context.Background()

	// Exhaust l1 burst
	if err := l1.Wait(ctx); err != nil {
		t.Fatalf("l1 failed: %v", err)
	}

	// l2 burst must remain available immediately
	now := time.Now()
	if err := l2.Wait(ctx); err != nil {
		t.Fatalf("l2 failed: %v", err)
	}
	if time.Since(now) > 50*time.Millisecond {
		t.Error("l2 was blocked by l1 burst consumption")
	}
}

func TestLimiter_DualLimiterOrderNoDeadlock(t *testing.T) {
	globalLimiter := NewLimiter(100.0, 10)
	sessionLimiter := NewLimiter(50.0, 5)

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Enforced consistent order: Global -> Session
			if err := globalLimiter.Wait(ctx); err != nil {
				t.Errorf("global wait error: %v", err)
			}
			if err := sessionLimiter.Wait(ctx); err != nil {
				t.Errorf("session wait error: %v", err)
			}
		}()
	}

	wg.Wait()
}
