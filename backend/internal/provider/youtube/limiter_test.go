package youtube

import (
	"context"
	"testing"
	"time"
)

func TestLimiter_DisabledWhenRateZeroOrNegative(t *testing.T) {
	lZero := newLimiter(0, 5)
	if lZero != nil {
		t.Fatalf("expected nil limiter for rate 0, got %+v", lZero)
	}

	lNeg := newLimiter(-1.5, 5)
	if lNeg != nil {
		t.Fatalf("expected nil limiter for negative rate, got %+v", lNeg)
	}

	// Wait on nil limiter should return nil immediately
	if err := lZero.Wait(context.Background()); err != nil {
		t.Fatalf("Wait on nil limiter returned error: %v", err)
	}
}

func TestLimiter_BurstAbsorption(t *testing.T) {
	// Rate of 1 per second, burst of 3
	l := newLimiter(1.0, 3)
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}

	// 3 tokens should be available immediately without waiting
	for i := 0; i < 3; i++ {
		delay := l.reserve()
		if delay > 0 {
			t.Fatalf("token %d required wait %v, expected 0", i, delay)
		}
	}

	// 4th token requires waiting ~1 second
	delay := l.reserve()
	if delay < 500*time.Millisecond || delay > 1500*time.Millisecond {
		t.Fatalf("4th token required wait %v, expected ~1s", delay)
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	// Rate of 0.1 per second (10s interval), burst 1
	l := newLimiter(0.1, 1)

	// Consume the single burst token
	l.reserve()

	// Next wait with cancelled context must abort immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := l.Wait(ctx)
	if err == nil || err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
