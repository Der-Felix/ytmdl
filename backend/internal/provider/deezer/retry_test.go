package deezer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

// retryProvider builds a provider whose pacing is off and whose backoff is
// negligible, so the retry behaviour can be tested without real waiting.
func retryProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	return retryProviderWith(t, handler, Config{
		RequestsPerSecond: -1, // pacing off
		MaxRetries:        3,
		RetryBackoff:      time.Millisecond,
		MaxRetryBackoff:   2 * time.Millisecond,
	})
}

// retryProviderWith is retryProvider with room to widen the backoff cap, which
// the Retry-After tests need: the cap deliberately wins over the header, so a
// two millisecond ceiling would hide whether the header was read at all.
func retryProviderWith(t *testing.T, handler http.HandlerFunc, cfg Config) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg.APIBaseURL = server.URL
	cfg.HTTPClient = server.Client()
	return New(cfg)
}

func quotaPayload(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"error": map[string]any{
			"type": "Exception", "message": "Quota limit exceeded", "code": 4,
		},
	})
}

func artistPayload(w http.ResponseWriter) {
	writeJSON(w, map[string]any{"id": 27, "name": "Daft Punk"})
}

func TestRetryRecoversFromHTTP429(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		artistPayload(w)
	})

	artist, err := p.GetArtist(context.Background(), "27")
	if err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if artist.Name != "Daft Punk" {
		t.Fatalf("unexpected artist: %+v", artist)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected exactly one retry, got %d attempts", got)
	}
}

// Deezer signals a breached quota with HTTP 200 and an error payload. That is
// the form the live syncs actually hit; 429 was never observed.
func TestRetryRecoversFromQuotaExceptionOnHTTP200(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			quotaPayload(w)
			return
		}
		artistPayload(w)
	})

	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected two retries, got %d attempts", got)
	}
}

func TestRetryGivesUpAfterTheConfiguredAttempts(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		quotaPayload(w)
	})

	_, err := p.GetArtist(context.Background(), "27")
	if apperr.CodeOf(err) != apperr.CodeProviderRateLimited {
		t.Fatalf("expected PROVIDER_RATE_LIMITED, got %v", err)
	}
	// One initial attempt plus MaxRetries.
	if got := calls.Load(); got != 4 {
		t.Fatalf("expected 4 attempts, got %d", got)
	}
}

func TestRetryHonoursRetryAfterSeconds(t *testing.T) {
	var calls atomic.Int32
	p := retryProviderWith(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		artistPayload(w)
	}, Config{
		RequestsPerSecond: -1,
		MaxRetries:        3,
		RetryBackoff:      time.Millisecond,
		MaxRetryBackoff:   5 * time.Second,
	})

	start := time.Now()
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	elapsed := time.Since(start)

	// The configured backoff is a millisecond, so anything near a second can
	// only come from the header having been read.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("Retry-After was ignored: waited only %s", elapsed)
	}
}

// A Retry-After far beyond the cap must not park a request for minutes.
func TestRetryCapsAnExcessiveRetryAfter(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		artistPayload(w)
	})

	start := time.Now()
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	// The configured ceiling is two milliseconds; an uncapped header would
	// have parked the request for an hour.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("an hour long Retry-After was not capped: waited %s", elapsed)
	}
}

func TestRetryRecoversFromATransientServerError(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		artistPayload(w)
	})

	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected one retry, got %d attempts", got)
	}
}

// Permanent failures must fail fast: retrying them only wastes the budget the
// transient failures need.
func TestPermanentFailuresAreNotRetried(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		code    apperr.Code
	}{
		{
			name: "404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			code: apperr.CodeArtistNotFound,
		},
		{
			name: "400",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			code: apperr.CodeInvalidRequest,
		},
		{
			name: "DataException",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"error": map[string]any{
					"type": "DataException", "message": "no data", "code": 800,
				}})
			},
			code: apperr.CodeArtistNotFound,
		},
		{
			name: "OAuthException",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"error": map[string]any{
					"type": "OAuthException", "message": "invalid token", "code": 300,
				}})
			},
			code: apperr.CodeProviderUnavailable,
		},
		{
			name: "malformed JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("{not json"))
			},
			code: apperr.CodeProviderUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			p := retryProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				tc.handler(w, r)
			})

			_, err := p.GetArtist(context.Background(), "27")
			if apperr.CodeOf(err) != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("a permanent failure was retried: %d attempts", got)
			}
		})
	}
}

// A shutdown must not have to sit out a backoff.
func TestRetryStopsWhenTheContextIsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	p := New(Config{
		APIBaseURL:      server.URL,
		HTTPClient:      server.Client(),
		MaxRetries:      5,
		RetryBackoff:    30 * time.Second,
		MaxRetryBackoff: 30 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := p.GetArtist(ctx, "27")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a cancelled request reported success")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("the backoff was not interrupted: %s", elapsed)
	}
	if code := apperr.CodeOf(err); code != apperr.CodeJobCancelled && code != apperr.CodeProviderRateLimited {
		t.Fatalf("unexpected error code %s: %v", code, err)
	}
}

// The pacing has to hold across goroutines, because that is what stops two
// parallel subscription syncs from each running at the full rate.
func TestPacingIsSharedAcrossConcurrentRequests(t *testing.T) {
	var (
		mu    sync.Mutex
		times []time.Time
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		artistPayload(w)
	}))
	t.Cleanup(server.Close)

	p := New(Config{
		APIBaseURL:        server.URL,
		HTTPClient:        server.Client(),
		RequestsPerSecond: 20, // 50ms apart
		Burst:             1,
	})

	const callers = 6
	var wg sync.WaitGroup
	start := time.Now()
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.GetArtist(context.Background(), "27"); err != nil {
				t.Errorf("request failed: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	mu.Lock()
	got := len(times)
	mu.Unlock()
	if got != callers {
		t.Fatalf("expected %d requests, saw %d", callers, got)
	}

	// One free slot plus five paced ones: about 250ms. Well under that would
	// mean the goroutines each had their own budget.
	if elapsed < 200*time.Millisecond {
		t.Fatalf("concurrent callers were not paced together: %s", elapsed)
	}
}

func TestRetryAfterAcceptsAnHTTPDate(t *testing.T) {
	var calls atomic.Int32
	p := retryProviderWith(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		artistPayload(w)
	}, Config{
		RequestsPerSecond: -1,
		MaxRetries:        3,
		RetryBackoff:      time.Millisecond,
		MaxRetryBackoff:   5 * time.Second,
	})

	start := time.Now()
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("an HTTP-date Retry-After was ignored: waited %s", elapsed)
	}
}

// A nonsense header must not defeat the retry; the configured backoff applies.
func TestRetryIgnoresAnUnparsableRetryAfter(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "soon-ish")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		artistPayload(w)
	})

	start := time.Now()
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("an unparsable Retry-After was treated as a long wait: %s", elapsed)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected one retry, got %d attempts", got)
	}
}

// Retries must not be silently unbounded: the ceiling is what keeps one dead
// release from eating the whole sync timeout.
func TestRetryBudgetIsBounded(t *testing.T) {
	var calls atomic.Int32
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	if _, err := p.GetArtist(context.Background(), "27"); err == nil {
		t.Fatal("a permanently failing endpoint reported success")
	}
	if got := calls.Load(); got > 4 {
		t.Fatalf("retries were not bounded: %d attempts", got)
	}
}

// Sanity: the JSON decoder still sees a complete body after the retry layer
// has read it to inspect the error payload.
func TestBodyIsIntactAfterTheRetryLayer(t *testing.T) {
	p := retryProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id": 27, "name": "Daft Punk", "nb_album": 38,
			"picture_xl": "https://example.test/x.jpg",
		})
	})

	artist, err := p.GetArtist(context.Background(), "27")
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if artist.Name != "Daft Punk" || artist.SourceID != strconv.Itoa(27) {
		t.Fatalf("the payload was not decoded: %+v", artist)
	}
	if artist.ImageURL == "" {
		t.Fatal("the image was lost")
	}
}

var _ = json.Marshal

/* ----------------------------------------------------- liveness probe */

// The availability probe must answer while a catalogue walk is under way.
// It is a liveness check, not catalogue data: queueing it behind three hundred
// paced requests would report Deezer as unreachable exactly when it is busy
// serving us.
func TestAvailableIsNotHeldUpByThePacing(t *testing.T) {
	p := retryProviderWith(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": []any{}})
	}, Config{
		RequestsPerSecond: 1, // one per second: a paced call would queue
		Burst:             1,
	})

	// Spend the burst, so anything paced now has to wait a full second.
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("priming call failed: %v", err)
	}

	start := time.Now()
	if err := p.Available(context.Background()); err != nil {
		t.Fatalf("available: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("the probe queued behind the pacing: %s", elapsed)
	}
}

// The probe answers with what it found on the first try. Retrying a liveness
// check only delays the answer it exists to give.
func TestAvailableDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	p := retryProviderWith(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		quotaPayload(w)
	}, Config{
		RequestsPerSecond: -1,
		MaxRetries:        3,
		RetryBackoff:      time.Millisecond,
		MaxRetryBackoff:   time.Millisecond,
	})

	if err := p.Available(context.Background()); err == nil {
		t.Fatal("a rate limited probe reported availability")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the probe retried: %d attempts", got)
	}
}

// The catalogue endpoints stay paced; only the probe is exempt.
func TestCatalogueRequestsStayPaced(t *testing.T) {
	p := retryProviderWith(t, func(w http.ResponseWriter, _ *http.Request) {
		artistPayload(w)
	}, Config{RequestsPerSecond: 20, Burst: 1})

	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("priming call failed: %v", err)
	}

	start := time.Now()
	if _, err := p.GetArtist(context.Background(), "27"); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("a catalogue request was not paced: %s", elapsed)
	}
}
