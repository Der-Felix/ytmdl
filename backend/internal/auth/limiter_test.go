package auth

import (
	"testing"
	"time"
)

func TestLimiterSlidingWindow(t *testing.T) {
	limiter := NewLimiter(3, 100*time.Millisecond)
	defer limiter.Close()

	key := "127.0.0.1:user1"

	// 1st attempt: allowed
	allowed, _ := limiter.Allow(key)
	if !allowed {
		t.Fatal("1st attempt should be allowed")
	}
	limiter.RecordFailure(key)

	// 2nd attempt: allowed
	allowed, _ = limiter.Allow(key)
	if !allowed {
		t.Fatal("2nd attempt should be allowed")
	}
	limiter.RecordFailure(key)

	// 3rd attempt: allowed
	allowed, _ = limiter.Allow(key)
	if !allowed {
		t.Fatal("3rd attempt should be allowed")
	}
	limiter.RecordFailure(key)

	// 4th attempt: rate limited
	allowed, retryAfter := limiter.Allow(key)
	if allowed {
		t.Fatal("4th attempt should be blocked")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retryAfter)
	}

	// Different key should not be blocked
	otherAllowed, _ := limiter.Allow("127.0.0.1:user2")
	if !otherAllowed {
		t.Fatal("unrelated key should be allowed")
	}

	// After window expires, should be allowed again
	time.Sleep(120 * time.Millisecond)
	allowed, _ = limiter.Allow(key)
	if !allowed {
		t.Fatal("attempt after window expiry should be allowed")
	}
}

func TestLimiterSuccessClears(t *testing.T) {
	limiter := NewLimiter(2, 5*time.Minute)
	defer limiter.Close()

	key := "10.0.0.1:alice"
	limiter.RecordFailure(key)

	// Success clears record
	limiter.RecordSuccess(key)

	// Now can record 2 failures without hitting limit
	limiter.RecordFailure(key)
	allowed, _ := limiter.Allow(key)
	if !allowed {
		t.Fatal("should be allowed after success cleared earlier failures")
	}
}
