package deezer

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fixedClock lets the reservation arithmetic be tested without sleeping.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestLimiter(rate float64, burst int) (*limiter, *fixedClock) {
	clock := &fixedClock{now: time.Unix(0, 0)}
	l := newLimiter(rate, burst)
	l.now = clock.Now
	return l, clock
}

func TestLimiterLetsTheBurstThroughImmediately(t *testing.T) {
	l, _ := newTestLimiter(10, 4)

	for i := range 4 {
		if delay := l.reserve(); delay != 0 {
			t.Fatalf("request %d inside the burst was delayed by %s", i, delay)
		}
	}
}

// Past the burst the caller has to wait one interval per request, and the
// waits accumulate: two callers in a row must not be given the same slot.
func TestLimiterSpacesRequestsBeyondTheBurst(t *testing.T) {
	l, _ := newTestLimiter(10, 1) // one request per 100ms

	if delay := l.reserve(); delay != 0 {
		t.Fatalf("the first request was delayed by %s", delay)
	}

	first := l.reserve()
	if first < 90*time.Millisecond || first > 110*time.Millisecond {
		t.Fatalf("the second request should wait about one interval, waited %s", first)
	}

	second := l.reserve()
	if second < first+90*time.Millisecond {
		t.Fatalf("the third request queued behind the second: %s should exceed %s by an interval",
			second, first)
	}
}

// Time passing refills the bucket, and the refill is capped at the burst so
// that an idle period cannot buy an unbounded burst afterwards.
func TestLimiterRefillsOverTimeAndIsCappedAtTheBurst(t *testing.T) {
	l, clock := newTestLimiter(10, 3)

	for range 3 {
		l.reserve()
	}
	if delay := l.reserve(); delay == 0 {
		t.Fatal("the burst was not exhausted")
	}

	// Idle far longer than it takes to refill the whole bucket.
	clock.advance(10 * time.Second)

	for i := range 3 {
		if delay := l.reserve(); delay != 0 {
			t.Fatalf("request %d after the idle period was delayed by %s", i, delay)
		}
	}
	if delay := l.reserve(); delay == 0 {
		t.Fatal("the refill was not capped at the burst")
	}
}

// Several goroutines sharing one limiter must share one budget: this is what
// keeps two parallel subscription syncs from each running at the full rate.
func TestLimiterIsSharedAcrossGoroutines(t *testing.T) {
	l, _ := newTestLimiter(10, 1)

	const callers = 8
	delays := make([]time.Duration, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delays[i] = l.reserve()
		}()
	}
	wg.Wait()

	// One caller goes free and the other seven are spread over seven
	// intervals; no two may be handed the same slot.
	seen := make(map[time.Duration]int, callers)
	for _, d := range delays {
		seen[d]++
	}
	for delay, count := range seen {
		if count > 1 {
			t.Fatalf("%d callers were given the same slot (%s)", count, delay)
		}
	}

	var longest time.Duration
	for _, d := range delays {
		if d > longest {
			longest = d
		}
	}
	if longest < 600*time.Millisecond {
		t.Fatalf("eight callers at 10/s should span about 700ms, longest wait was %s", longest)
	}
}

func TestLimiterWaitRespectsContextCancellation(t *testing.T) {
	// A rate low enough that the second call would wait a long time.
	l := newLimiter(1, 1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("the first wait failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := l.Wait(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a cancelled wait reported success")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("the cancelled wait did not return promptly: %s", elapsed)
	}
}

func TestLimiterWaitReturnsAtOnceForAnAlreadyCancelledContext(t *testing.T) {
	l := newLimiter(1000, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.Wait(ctx); err == nil {
		t.Fatal("an already cancelled wait reported success")
	}
}

// A non-positive rate switches pacing off entirely, which is what the tests of
// the other endpoints rely on to stay fast.
func TestLimiterIsDisabledForANonPositiveRate(t *testing.T) {
	for _, rate := range []float64{0, -1} {
		l := newLimiter(rate, 0)
		for range 100 {
			if delay := l.reserve(); delay != 0 {
				t.Fatalf("rate %v should disable pacing, got a delay of %s", rate, delay)
			}
		}
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("rate %v: %v", rate, err)
		}
	}
}

// A burst below one would stall every request; it is corrected rather than
// honoured.
func TestLimiterForcesAUsableBurst(t *testing.T) {
	l, _ := newTestLimiter(10, 0)
	if delay := l.reserve(); delay != 0 {
		t.Fatalf("the first request was delayed by %s", delay)
	}
}
