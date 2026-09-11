package jobs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/throughput"
)

// precheckOrchestrator answers the pre-attempt availability question from a
// fixed table. It must never be asked to resolve anything in these tests.
type precheckOrchestrator struct {
	mu      sync.Mutex
	blocked map[string]bool
	calls   map[string]int
}

func (o *precheckOrchestrator) Precheck(ctx context.Context, preferred string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	key := preferred + "/" + string(orchestrator.OriginFromContext(ctx))
	o.calls[key]++
	if o.blocked[preferred] {
		return apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "cooling", time.Minute)
	}
	return nil
}

func (o *precheckOrchestrator) ResolveMedia(context.Context, string, music.Track, int) (*orchestrator.ResolvedMedia, error) {
	panic("the dispatcher must not resolve media")
}
func (o *precheckOrchestrator) ResolveCookiePath(string) string                      { return "" }
func (o *precheckOrchestrator) RecordDownloadOutcome(context.Context, string, error) {}

func holdTestManager(store Store, orch MediaOrchestrator, now time.Time) *Manager {
	mgr := &Manager{
		store:        store,
		nowFunc:      func() time.Time { return now },
		semaphore:    make(chan struct{}, 2),
		orchestrator: orch,
		throughput:   throughput.New(),
	}
	mgr.maxWorkers.Store(2)
	start, end, empty := "00:00", "23:59", ""
	mgr.scheduleStart.Store(&start)
	mgr.scheduleEnd.Store(&end)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)
	return mgr
}

func readyIDs(candidates []jobCandidate) map[string]bool {
	out := map[string]bool{}
	for _, c := range candidates {
		for _, it := range c.ready {
			out[it.ID] = true
		}
	}
	return out
}

func TestDispatcherHoldsDueRetriesWhileTheirProviderIsBlocked(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	store := newMemorySchedulerStore()
	subscription := Options{Origin: OriginSubscription}
	store.addJob(Job{ID: "yt", Status: StatusRetryWait, MediaProvider: "ytmusic", Options: subscription, CreatedAt: now}, []Item{
		{ID: "yt-retry-1", JobID: "yt", Status: ItemRetryWait, NextRetryAt: &due, ErrorCode: string(apperr.CodeSessionUnavailable)},
		{ID: "yt-retry-2", JobID: "yt", Status: ItemRetryWait, NextRetryAt: &due, ErrorCode: string(apperr.CodeProviderRateLimited)},
		{ID: "yt-pending", JobID: "yt", Status: ItemPending},
	})
	store.addJob(Job{ID: "sc", Status: StatusRetryWait, MediaProvider: "soundcloud", Options: subscription, CreatedAt: now.Add(time.Second)}, []Item{
		{ID: "sc-retry", JobID: "sc", Status: ItemRetryWait, NextRetryAt: &due},
	})
	orch := &precheckOrchestrator{blocked: map[string]bool{"ytmusic": true}, calls: map[string]int{}}
	mgr := holdTestManager(store, orch, now)

	ready := readyIDs(mgr.collectCandidates(context.Background()))
	if ready["yt-retry-1"] || ready["yt-retry-2"] {
		t.Fatalf("due retries of a blocked provider were dispatched: %v", ready)
	}
	// Never-attempted work still gets its single attempt, so its wait reason
	// is recorded, and other providers' jobs are not held up at all.
	if !ready["yt-pending"] || !ready["sc-retry"] {
		t.Fatalf("unblocked work was held: %v", ready)
	}
	if orch.calls["ytmusic/subscription"] != 1 {
		t.Fatalf("precheck calls per pass: %v", orch.calls)
	}
	w := mgr.throughput.Drain()
	if w.GaugeLast["items.held_for_provider"] != 2 {
		t.Fatalf("held gauge = %d", w.GaugeLast["items.held_for_provider"])
	}

	// The provider becomes available: the held retries are dispatched again,
	// and their persisted state was never touched.
	orch.mu.Lock()
	orch.blocked["ytmusic"] = false
	orch.mu.Unlock()
	ready = readyIDs(mgr.collectCandidates(context.Background()))
	if !ready["yt-retry-1"] || !ready["yt-retry-2"] {
		t.Fatalf("retries stayed held after the provider recovered: %v", ready)
	}
	if it, _ := store.GetItem(context.Background(), "yt-retry-2"); it.Status != ItemRetryWait || it.ErrorCode != string(apperr.CodeProviderRateLimited) {
		t.Fatalf("held item was modified: %+v", it)
	}
}

func TestDispatcherHoldRespectsPauseAndDownloadWindow(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	due := now.Add(-time.Second)
	store := newMemorySchedulerStore()
	store.addJob(Job{ID: "sc", Status: StatusRetryWait, MediaProvider: "soundcloud", CreatedAt: now}, []Item{
		{ID: "sc-retry", JobID: "sc", Status: ItemRetryWait, NextRetryAt: &due},
	})
	orch := &precheckOrchestrator{blocked: map[string]bool{}, calls: map[string]int{}}
	mgr := holdTestManager(store, orch, now)

	// Outside the download window nothing is ready, and the provider is not
	// even asked.
	start, end := "22:00", "06:00"
	mgr.scheduleStart.Store(&start)
	mgr.scheduleEnd.Store(&end)
	mgr.scheduleEnabled.Store(true)
	if ready := readyIDs(mgr.collectCandidates(context.Background())); len(ready) != 0 {
		t.Fatalf("work dispatched outside the window: %v", ready)
	}
	if len(orch.calls) != 0 {
		t.Fatalf("precheck ran outside the window: %v", orch.calls)
	}

	// A paused queue dispatches nothing regardless of provider state.
	mgr.scheduleEnabled.Store(false)
	mgr.queuePaused.Store(true)
	mgr.dispatch(context.Background())
	if mgr.activeWorkers.Load() != 0 {
		t.Fatal("paused queue started a worker")
	}
}

func TestRetryAfterRateLimitWaitsForTheKnownCooldown(t *testing.T) {
	prov := newMockFallbackMediaProvider("youtube", nil)
	prov.searchErr = apperr.New(apperr.CodeProviderRateLimited, "rate limited")
	mgr, store := setupTestFallbackEnvironment(t, prov)
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	mgr.nowFunc = func() time.Time { return now }
	w := &worker{manager: mgr}

	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait || updated.Attempts != 1 {
		t.Fatalf("status=%s attempts=%d", updated.Status, updated.Attempts)
	}
	// The first backoff step is about five seconds; the provider is known to
	// be blocked for a minute, so retrying earlier would only spend an attempt.
	wait := updated.NextRetryAt.Sub(now)
	if wait < 59*time.Second || wait > 90*time.Second {
		t.Fatalf("retry scheduled %v after the rate limit, want the cooldown plus a small spread", wait)
	}
}

func TestRetryDelayUsesBackoffWithoutCooldown(t *testing.T) {
	mgr := &Manager{cooldown: NewMediaCooldownManager()}
	for i := 0; i < 50; i++ {
		d := mgr.retryDelay(Job{MediaProvider: "youtube"}, 2, apperr.New(apperr.CodeDownloadFailed, "x"))
		if d < 12*time.Second || d > 18*time.Second {
			t.Fatalf("attempt 2 delay %v outside 15s±20%%", d)
		}
	}
	hinted := apperr.NewRetryAfter(apperr.CodeProviderUnavailable, "x", 3*time.Minute)
	if d := mgr.retryDelay(Job{}, 1, hinted); d < 3*time.Minute || d > 3*time.Minute+18*time.Second {
		t.Fatalf("hinted delay %v", d)
	}
}

func TestSessionWaitSpreadNeverRetriesEarly(t *testing.T) {
	for i := 0; i < 200; i++ {
		if d := spreadAfter(2 * time.Minute); d < 2*time.Minute || d > 2*time.Minute+12*time.Second {
			t.Fatalf("spread %v", d)
		}
		if d := spreadAfter(time.Hour); d > time.Hour+30*time.Second {
			t.Fatalf("spread exceeds its cap: %v", d)
		}
	}
}

func TestThroughputSummaryReportsOutcomesWithoutPrivateData(t *testing.T) {
	candidates := []provider.MediaCandidate{{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube", URL: "https://www.youtube.com/watch?v=cand-1"}}
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", candidates))
	var buf bytes.Buffer
	mgr.logger = slog.New(slog.NewJSONHandler(&buf, nil))
	mgr.throughput = throughput.New()
	w := &worker{manager: mgr}
	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])
	if store.items["item-1"].Status != ItemCompleted {
		t.Fatalf("item did not complete: %+v", store.items["item-1"])
	}
	mgr.throughput.Inc("items.error.TRACK_NOT_FOUND")

	buf.Reset()
	mgr.logThroughput(false)
	line := buf.String()
	for _, want := range []string{`"msg":"throughput summary"`, `"items.completed":1`, `"items.error.TRACK_NOT_FOUND":1`, `"window_seconds"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary lacks %s: %s", want, line)
		}
	}
	for _, leak := range []string{"http", "ABBA", "Visitors", "cand-1"} {
		if strings.Contains(line, leak) {
			t.Fatalf("summary leaks %q: %s", leak, line)
		}
	}
}
