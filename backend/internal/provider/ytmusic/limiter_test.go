package ytmusic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestInnerTubeConcurrentCallsArePacedAtHTTPBoundary(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	var active, maximum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		starts = append(starts, time.Now())
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer server.Close()

	api := &innerTube{
		httpClient: server.Client(), baseURL: server.URL, language: "en", region: "US",
		limiter: newRequestLimiter(20, 1),
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			switch i % 3 {
			case 0:
				_, err = api.search(context.Background(), "test", "")
			case 1:
				_, err = api.browse(context.Background(), "browse", "continuation")
			default:
				_, err = api.next(context.Background(), "video")
			}
			if err != nil {
				t.Errorf("call %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	mu.Lock()
	got := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(got) != 5 {
		t.Fatalf("HTTP starts = %d, want 5", len(got))
	}
	if maximum != 1 {
		t.Fatalf("concurrent InnerTube requests = %d, want 1", maximum)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Before(got[j]) })
	for i := 1; i < len(got); i++ {
		if gap := got[i].Sub(got[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("request burst escaped pacing: gap %v", gap)
		}
	}
}

func TestInnerTubeAdmissionHonorsConfiguredBurst(t *testing.T) {
	limiter := newRequestLimiter(10, 2)
	if delay := limiter.reserve(); delay != 0 {
		t.Fatalf("first token delayed by %v", delay)
	}
	if delay := limiter.reserve(); delay != 0 {
		t.Fatalf("second burst token delayed by %v", delay)
	}
	if delay := limiter.reserve(); delay < 90*time.Millisecond {
		t.Fatalf("third token escaped configured burst: delay %v", delay)
	}
}

func TestInnerTubeAdmissionCancellationReturnsWithoutLeakingWaiter(t *testing.T) {
	limiter := newRequestLimiter(0.1, 1)
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- limiter.wait(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wait error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled admission waiter leaked")
	}
}
