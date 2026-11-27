package jobs

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// testClock is a settable clock for pause tests.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestPauses(clock *testClock) (*providerPauses, *MediaCooldownManager) {
	cooldown := NewMediaCooldownManager()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newProviderPauses(clock.Now, func() *slog.Logger { return logger }, func() *MediaCooldownManager { return cooldown }), cooldown
}

func TestBotChallengeHoldsTheItemWithoutSpendingAnAttempt(t *testing.T) {
	prov := newMockFallbackMediaProvider("youtube", nil)
	prov.searchErr = apperr.New(apperr.CodeSessionBotChallenge, "The media session encountered a bot challenge: Sign in to confirm you're not a bot")
	mgr, store := setupTestFallbackEnvironment(t, prov)
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	mgr.nowFunc = clock.Now
	w := &worker{manager: mgr}
	item := store.items["item-1"]
	item.Attempts = 2
	store.items["item-1"] = item

	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("status = %s, want retry_wait", updated.Status)
	}
	if updated.Attempts != 2 {
		t.Fatalf("attempts = %d, want the 2 it had before the block", updated.Attempts)
	}
	if updated.ErrorCode != string(apperr.CodeSessionBotChallenge) {
		t.Fatalf("error code = %s", updated.ErrorCode)
	}
	wait := updated.NextRetryAt.Sub(clock.Now())
	if wait < 5*time.Minute || wait > 5*time.Minute+30*time.Second {
		t.Fatalf("item waits %v, want the five minute pause plus a small spread", wait)
	}
	if phase := mgr.providerPauses().Phase("ytmusic"); phase != PausePhasePaused {
		t.Fatalf("youtube family phase = %q, want paused", phase)
	}
	if remaining, cooling := mgr.cooldown.Remaining("youtube"); !cooling || remaining < 4*time.Minute {
		t.Fatalf("family cooldown = %v (%v), want the pause, so running attempts stop too", remaining, cooling)
	}
}

func TestAuthDemandPausesButStillSpendsTheAttempt(t *testing.T) {
	prov := newMockFallbackMediaProvider("youtube", nil)
	prov.searchErr = apperr.New(apperr.CodeSessionAuthFailed, "The media session requires authentication: login required")
	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	item := store.items["item-1"]

	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait || updated.Attempts != 1 {
		t.Fatalf("status=%s attempts=%d, want retry_wait with one attempt spent", updated.Status, updated.Attempts)
	}
	if mgr.providerPauses().Phase("youtube") != PausePhasePaused {
		t.Fatal("a sign-in demand did not pause the family")
	}

	// While the pause runs, the attempt does not even reach the provider.
	// Once it does again and is refused on its last attempt, the item fails,
	// as before.
	mgr.cooldown.Clear("youtube")
	item = store.items["item-1"]
	item.Attempts = 4
	store.items["item-1"] = item
	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, item)
	if updated := store.items["item-1"]; updated.Status != ItemFailed {
		t.Fatalf("status = %s after the last attempt, want failed", updated.Status)
	}
}

// rotatingOrchestrator refuses every item with a bot challenge but reports
// that the session pool still has another usable session.
type rotatingOrchestrator struct{ usable bool }

func (o *rotatingOrchestrator) ResolveMedia(context.Context, string, music.Track, int) (*orchestrator.ResolvedMedia, error) {
	return nil, apperr.New(apperr.CodeSessionBotChallenge, "bot challenge")
}
func (o *rotatingOrchestrator) ResolveCookiePath(string) string                      { return "" }
func (o *rotatingOrchestrator) RecordDownloadOutcome(context.Context, string, error) {}
func (o *rotatingOrchestrator) HasUsableSession() bool                               { return o.usable }

func TestRefusedSessionWithAHealthyAlternativeDoesNotPauseTheFamily(t *testing.T) {
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", nil))
	mgr.orchestrator = &rotatingOrchestrator{usable: true}
	now := time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)
	mgr.nowFunc = func() time.Time { return now }
	w := &worker{manager: mgr}

	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait || updated.Attempts != 0 {
		t.Fatalf("status=%s attempts=%d, want retry_wait without a spent attempt", updated.Status, updated.Attempts)
	}
	if wait := updated.NextRetryAt.Sub(now); wait < sessionRotationDelay || wait > sessionRotationDelay+5*time.Second {
		t.Fatalf("item waits %v, want a short wait for the next session", wait)
	}
	if phase := mgr.providerPauses().Phase("youtube"); phase != PausePhaseOpen {
		t.Fatalf("family phase = %q, want open while another session is usable", phase)
	}

	// Without a usable session left, the same refusal pauses the family.
	mgr.orchestrator = &rotatingOrchestrator{usable: false}
	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])
	if phase := mgr.providerPauses().Phase("youtube"); phase != PausePhasePaused {
		t.Fatalf("family phase = %q, want paused once no session is left", phase)
	}
}

func TestProviderPauseEscalatesAndResetsAfterASuccess(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	pauses, cooldown := newTestPauses(clock)

	until := pauses.Block("youtube", apperr.CodeSessionBotChallenge, "item-a")
	if got := until.Sub(clock.Now()); got != 5*time.Minute {
		t.Fatalf("first pause %v, want 5m", got)
	}
	if _, cooling := cooldown.Remaining("ytmusic"); !cooling {
		t.Fatal("the pause did not hold the family cooldown")
	}
	// Workers that were already running report the same block: no escalation.
	for _, id := range []string{"item-b", "item-c", "item-d"} {
		if again := pauses.Block("ytmusic", apperr.CodeSessionBotChallenge, id); !again.Equal(until) {
			t.Fatalf("a concurrent report moved the pause to %v", again)
		}
	}

	// Every probe that meets the block again escalates: 15, 30, 60, 60 minutes.
	for i, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 60 * time.Minute} {
		clock.Advance(until.Sub(clock.Now()))
		if !pauses.Admit("youtube", "probe", false) {
			t.Fatalf("step %d: the probe was not admitted", i)
		}
		until = pauses.Block("youtube", apperr.CodeSessionBotChallenge, "probe")
		if got := until.Sub(clock.Now()); got != want {
			t.Fatalf("step %d: pause %v, want %v", i, got, want)
		}
	}

	// The probe after the last pause downloads: the family reopens.
	clock.Advance(until.Sub(clock.Now()))
	if !pauses.Admit("youtube", "probe", false) {
		t.Fatal("the probe was not admitted")
	}
	pauses.Succeeded("ytmusic")
	if phase := pauses.Phase("youtube"); phase != PausePhaseOpen {
		t.Fatalf("phase = %q after the probe succeeded, want open", phase)
	}
	// The escalation was reset: the next block starts at the first step again.
	if got := pauses.Block("youtube", apperr.CodeSessionBotChallenge, "x").Sub(clock.Now()); got != 5*time.Minute {
		t.Fatalf("pause after reset %v, want 5m", got)
	}
}

func TestRateLimitPausesStayShort(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	pauses, _ := newTestPauses(clock)
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		until := pauses.Block("youtube", apperr.CodeProviderRateLimited, "probe")
		if got := until.Sub(clock.Now()); got != want {
			t.Fatalf("step %d: pause %v, want %v", i, got, want)
		}
		clock.Advance(want)
	}
}

func TestASuccessDuringARunningPauseDoesNotReopen(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	pauses, _ := newTestPauses(clock)
	pauses.Block("youtube", apperr.CodeSessionBotChallenge, "a")
	// A download that was already running when the block hit finishes.
	pauses.Succeeded("youtube")
	if phase := pauses.Phase("youtube"); phase != PausePhasePaused {
		t.Fatalf("phase = %q, want the pause to keep running", phase)
	}
}

func TestOnlyOneProbeRunsAndAnInconclusiveProbeHandsOver(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	pauses, _ := newTestPauses(clock)
	until := pauses.Block("youtube", apperr.CodeSessionBotChallenge, "a")

	if pauses.Admit("youtube", "p1", false) {
		t.Fatal("an item started during the pause")
	}
	if !pauses.Admit("youtube", "fallback", true) {
		t.Fatal("an item that may fall back to an independent provider was held")
	}
	if !pauses.Admit("soundcloud", "sc", false) {
		t.Fatal("another provider family was held")
	}

	clock.Advance(until.Sub(clock.Now()))
	if !pauses.Admit("youtube", "p1", false) {
		t.Fatal("the probe was not admitted after the pause")
	}
	if pauses.Admit("youtube", "p2", false) || pauses.Admit("youtube", "p3", true) {
		t.Fatal("a second item started while the probe runs")
	}
	if phase := pauses.Phase("youtube"); phase != PausePhaseProbing {
		t.Fatalf("phase = %q, want probing", phase)
	}

	// The probe found no match at all: no verdict, the next item probes.
	pauses.Finished("youtube", "p1")
	if !pauses.Admit("youtube", "p2", false) {
		t.Fatal("the probe slot was not handed over")
	}
	// Finishing an item that is not the probe changes nothing.
	pauses.Finished("youtube", "p1")
	if pauses.Admit("youtube", "p3", false) {
		t.Fatal("a finished non-probe freed the probe slot")
	}
}

func TestProbeNowEndsThePauseButKeepsTheEscalation(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	pauses, cooldown := newTestPauses(clock)
	if pauses.ProbeNow("youtube") {
		t.Fatal("ProbeNow reported a pause that did not exist")
	}
	pauses.Block("youtube", apperr.CodeSessionBotChallenge, "a")

	if !pauses.ProbeNow("youtube") {
		t.Fatal("ProbeNow did not end the pause")
	}
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("the family cooldown is still active")
	}
	if !pauses.Admit("youtube", "probe", false) || pauses.Admit("youtube", "other", false) {
		t.Fatal("after ProbeNow exactly one probe must start")
	}
	// The probe meets the block again: the next, longer step follows.
	if got := pauses.Block("youtube", apperr.CodeSessionBotChallenge, "probe").Sub(clock.Now()); got != 15*time.Minute {
		t.Fatalf("pause after a failed manual probe %v, want 15m", got)
	}
}

func TestDispatcherHoldsAPausedFamilyAndLetsOneProbeThrough(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	due := clock.Now().Add(-time.Second)
	store := newMemorySchedulerStore()
	subscription := Options{Origin: OriginSubscription}
	store.addJob(Job{ID: "yt", Status: StatusMatching, MediaProvider: "ytmusic", Options: subscription, CreatedAt: clock.Now()}, []Item{
		{ID: "yt-1", JobID: "yt", Status: ItemPending},
		{ID: "yt-2", JobID: "yt", Status: ItemPending},
		{ID: "yt-3", JobID: "yt", Status: ItemRetryWait, NextRetryAt: &due},
		{ID: "yt-staged", JobID: "yt", Status: ItemWaitingStorage, StagedSHA256: "abc"},
	})
	store.addJob(Job{ID: "sc", Status: StatusMatching, MediaProvider: "soundcloud", Options: subscription, CreatedAt: clock.Now().Add(time.Second)}, []Item{
		{ID: "sc-1", JobID: "sc", Status: ItemPending},
	})
	orch := &precheckOrchestrator{blocked: map[string]bool{}, calls: map[string]int{}}
	mgr := holdTestManager(store, orch, clock.Now())
	mgr.nowFunc = clock.Now

	until := mgr.providerPauses().Block("youtube", apperr.CodeSessionBotChallenge, "yt-0")

	ready := readyIDs(mgr.collectCandidates(context.Background()))
	if ready["yt-1"] || ready["yt-2"] || ready["yt-3"] {
		t.Fatalf("items of the paused family were made ready: %v", ready)
	}
	if !ready["sc-1"] {
		t.Fatalf("another provider's item was held: %v", ready)
	}
	if !ready["yt-staged"] {
		t.Fatalf("a staged result that only needs the library was held: %v", ready)
	}

	// The pause is over: the items are offered, but only one is admitted.
	clock.Advance(until.Sub(clock.Now()))
	candidates := mgr.collectCandidates(context.Background())
	admitted := 0
	for i := range candidates {
		c := &candidates[i]
		if c.job.ID != "yt" {
			continue
		}
		for _, it := range c.ready {
			if it.StagedSHA256 == "" && mgr.admit(c, it) {
				admitted++
			}
		}
	}
	if admitted != 1 {
		t.Fatalf("%d items admitted after the pause, want exactly one probe", admitted)
	}
	// While the probe runs nothing else of the family is even offered.
	ready = readyIDs(mgr.collectCandidates(context.Background()))
	if ready["yt-1"] || ready["yt-2"] || ready["yt-3"] {
		t.Fatalf("items offered while the probe runs: %v", ready)
	}
}

func TestProbeWaitsWhileTheSessionPoolStillHoldsTheFamily(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	store := newMemorySchedulerStore()
	store.addJob(Job{ID: "yt", Status: StatusMatching, MediaProvider: "ytmusic", Options: Options{Origin: OriginSubscription}, CreatedAt: clock.Now()}, []Item{
		{ID: "yt-1", JobID: "yt", Status: ItemPending},
	})
	orch := &precheckOrchestrator{blocked: map[string]bool{"ytmusic": true}, calls: map[string]int{}}
	mgr := holdTestManager(store, orch, clock.Now())
	mgr.nowFunc = clock.Now
	until := mgr.providerPauses().Block("youtube", apperr.CodeSessionBotChallenge, "yt-0")
	clock.Advance(until.Sub(clock.Now()))

	if ready := readyIDs(mgr.collectCandidates(context.Background())); ready["yt-1"] {
		t.Fatal("the probe started although every session is still cooling")
	}
	if phase := mgr.providerPauses().Phase("youtube"); phase != PausePhaseProbeReady {
		t.Fatalf("phase = %q, want the probe still pending", phase)
	}
}

func TestManualJobFallsBackDuringAPause(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	store := newMemorySchedulerStore()
	store.addJob(Job{ID: "manual", Status: StatusMatching, MediaProvider: "ytmusic", Options: Options{Origin: OriginManual}, CreatedAt: clock.Now()}, []Item{
		{ID: "m-1", JobID: "manual", Status: ItemPending},
	})
	// SoundCloud can serve the manual job; the precheck says an attempt
	// would not wait.
	orch := &precheckOrchestrator{blocked: map[string]bool{}, calls: map[string]int{}}
	mgr := holdTestManager(store, orch, clock.Now())
	mgr.nowFunc = clock.Now
	mgr.providerPauses().Block("youtube", apperr.CodeSessionBotChallenge, "x")

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) != 1 || !candidates[0].fallback {
		t.Fatalf("manual job not offered for its fallback: %+v", candidates)
	}
	if !mgr.admit(&candidates[0], candidates[0].ready[0]) {
		t.Fatal("manual job with an available fallback was not admitted")
	}
}

func TestVerifiedDownloadAfterThePauseReopensTheFamily(t *testing.T) {
	candidates := []provider.MediaCandidate{{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"}}
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", candidates))
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)}
	mgr.nowFunc = clock.Now
	until := mgr.providerPauses().Block("youtube", apperr.CodeSessionBotChallenge, "x")
	clock.Advance(until.Sub(clock.Now()))
	mgr.cooldown.Clear("youtube") // the cooldown runs on the wall clock
	if !mgr.providerPauses().Admit("youtube", "item-1", false) {
		t.Fatal("probe not admitted")
	}

	(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	if store.items["item-1"].Status != ItemCompleted {
		t.Fatalf("probe item did not complete: %+v", store.items["item-1"])
	}
	if phase := mgr.providerPauses().Phase("youtube"); phase != PausePhaseOpen {
		t.Fatalf("phase = %q after the probe downloaded, want open", phase)
	}
}

// countingStore reports fixed queue counts.
type countingStore struct {
	*memorySchedulerStore
	counts QueueCounts
}

func (s *countingStore) QueueCounts(context.Context) (QueueCounts, error) { return s.counts, nil }

func TestQueueSummaryReportsTheProviderPause(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 27, 14, 17, 0, 0, time.UTC)}
	store := &countingStore{memorySchedulerStore: newMemorySchedulerStore(), counts: QueueCounts{
		RunnableItems: 57800, ActiveItems: 0, CompletedLast1h: 0, CompletedLast6h: 40, BlockedFailedItems: 312,
	}}
	mgr := NewManagerForTest(store, nil)
	mgr.nowFunc = clock.Now
	mgr.SetScheduleTimezone("UTC")

	mgr.providerPauses().Block("youtube", apperr.CodeSessionBotChallenge, "a")
	summary, err := mgr.GetQueueSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.ProviderPauses) != 1 {
		t.Fatalf("provider pauses = %+v", summary.ProviderPauses)
	}
	pause := summary.ProviderPauses[0]
	if pause.Provider != "youtube" || pause.ReasonCode != string(apperr.CodeSessionBotChallenge) || pause.Phase != PausePhasePaused || pause.Level != 1 {
		t.Fatalf("pause = %+v", pause)
	}
	if pause.ResumesAt == nil || !pause.ResumesAt.Equal(clock.Now().Add(5*time.Minute)) {
		t.Fatalf("resumes at %v", pause.ResumesAt)
	}
	wantMessage := "YouTube verlangt eine Bot-Prüfung. Downloads pausieren bis 14:22, dann wird erneut geprüft."
	if pause.Message != wantMessage {
		t.Fatalf("message = %q, want %q", pause.Message, wantMessage)
	}
	if summary.ETAText != "YouTube pausiert bis 14:22" || summary.ETAConfidence != "provider_paused" || summary.ETASeconds != nil {
		t.Fatalf("eta = %q / %q / %v", summary.ETAText, summary.ETAConfidence, summary.ETASeconds)
	}
	if summary.BlockedFailedItems != 312 {
		t.Fatalf("blocked failed items = %d", summary.BlockedFailedItems)
	}

	// Once the pause ran out, the summary says that the next item probes.
	clock.Advance(5 * time.Minute)
	summary, _ = mgr.GetQueueSummary(context.Background())
	if summary.ProviderPauses[0].Phase != PausePhaseProbeReady || summary.ProviderPauses[0].ResumesAt != nil {
		t.Fatalf("pause after it ran out = %+v", summary.ProviderPauses[0])
	}
	if summary.ETAText != "YouTube wird erneut geprüft" {
		t.Fatalf("eta text = %q", summary.ETAText)
	}
	if !strings.Contains(summary.ProviderPauses[0].Message, "Der nächste Download prüft") {
		t.Fatalf("message = %q", summary.ProviderPauses[0].Message)
	}

	// A pause on another day names the date.
	if got := formatResume(clock.Now().Add(20*time.Hour), time.UTC, clock.Now()); got != "28.11. 10:22" {
		t.Fatalf("resume on the next day = %q", got)
	}

	// Without a pause the estimate is back.
	mgr.providerPauses().Admit("youtube", "probe", false)
	mgr.providerPauses().Succeeded("youtube")
	summary, _ = mgr.GetQueueSummary(context.Background())
	if len(summary.ProviderPauses) != 0 || summary.ETAConfidence == "provider_paused" {
		t.Fatalf("summary still reports a pause: %+v / %q", summary.ProviderPauses, summary.ETAConfidence)
	}
}

func TestRetryBlockedFailuresRequeuesOnlyBlockedItems(t *testing.T) {
	store := newMemorySchedulerStore()
	store.addJob(Job{ID: "a", Status: StatusFailed}, []Item{
		{ID: "bot", JobID: "a", Status: ItemFailed, Attempts: 5, ErrorCode: string(apperr.CodeSessionBotChallenge)},
		{ID: "rate", JobID: "a", Status: ItemFailed, Attempts: 5, ErrorCode: string(apperr.CodeProviderRateLimited)},
		{ID: "zero", JobID: "a", Status: ItemFailed, Attempts: 1, ErrorCode: string(apperr.CodeMatchFailed),
			ErrorMessage: `No sufficiently accurate media match found for "x" (best score 0.0, required 70.0).`},
	})
	store.addJob(Job{ID: "b", Status: StatusCompleted}, []Item{
		{ID: "gone", JobID: "b", Status: ItemFailed, Attempts: 1, ErrorCode: string(apperr.CodeTrackNotFound)},
		{ID: "done", JobID: "b", Status: ItemCompleted},
	})
	mgr := NewManagerForTest(store, nil)

	result, err := mgr.RetryBlockedFailures(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Items != 2 || result.Jobs != 1 {
		t.Fatalf("result = %+v, want 2 items in 1 job", result)
	}
	for _, id := range []string{"bot", "rate"} {
		if it, _ := store.GetItem(context.Background(), id); it.Status != ItemPending || it.Attempts != 0 {
			t.Fatalf("%s not requeued: %+v", id, it)
		}
	}
	for _, id := range []string{"zero", "gone"} {
		if it, _ := store.GetItem(context.Background(), id); it.Status != ItemFailed {
			t.Fatalf("%s requeued without being asked: %+v", id, it)
		}
	}
	if j, _ := store.Get(context.Background(), "a"); j.Status != StatusQueued {
		t.Fatalf("job a status = %s, want queued", j.Status)
	}
	if j, _ := store.Get(context.Background(), "b"); j.Status != StatusCompleted {
		t.Fatalf("job b status = %s, want untouched", j.Status)
	}

	result, err = mgr.RetryBlockedFailures(context.Background(), true)
	if err != nil || result.Items != 1 {
		t.Fatalf("zero-score requeue = %+v (%v), want 1 item", result, err)
	}
	if it, _ := store.GetItem(context.Background(), "zero"); it.Status != ItemPending {
		t.Fatalf("zero-score match failure not requeued: %+v", it)
	}
}
