package subscriptions

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

func newScheduler(t *testing.T, h *harness, interval time.Duration) *Scheduler {
	t.Helper()
	scheduler, err := NewScheduler(SchedulerOptions{
		Service:  h.service,
		Interval: interval,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}
	return scheduler
}

func TestSchedulerSyncsADueSubscription(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, time.Hour)
	sub := subscribe(t, h, false)

	synced := scheduler.tick(context.Background())
	if synced != 1 {
		t.Fatalf("expected one subscription to be synced, got %d", synced)
	}

	loaded, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != StatusSuccess {
		t.Fatalf("the run did not happen: status %q", loaded.LastSyncStatus)
	}
	if loaded.LastSyncAt == nil {
		t.Fatal("the run left no last sync time")
	}
}

func TestSchedulerSkipsASubscriptionThatIsNotDue(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, time.Hour)
	sub := subscribe(t, h, false)

	// The first run pushes the subscription one interval into the future.
	if scheduler.tick(context.Background()) != 1 {
		t.Fatal("the first tick did not sync")
	}
	if synced := scheduler.tick(context.Background()); synced != 0 {
		t.Fatalf("a subscription that is not due was synced again: %d", synced)
	}

	loaded, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !loaded.NextSyncAt.After(time.Now().UTC()) {
		t.Fatalf("next_sync_at was not moved forward: %v", loaded.NextSyncAt)
	}
}

func TestSchedulerSkipsADisabledSubscription(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, time.Hour)
	sub := subscribe(t, h, false)

	off := false
	if _, err := h.service.Update(context.Background(), sub.ID, Update{Enabled: &off}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if synced := scheduler.tick(context.Background()); synced != 0 {
		t.Fatalf("a disabled subscription was synced: %d", synced)
	}

	loaded, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != StatusPending {
		t.Fatalf("the disabled subscription was touched: %q", loaded.LastSyncStatus)
	}
}

// A disabled subscription is only out of the scheduler's reach; someone who
// explicitly asks for a check still gets one.
func TestManualSyncStillWorksForADisabledSubscription(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	off := false
	if _, err := h.service.Update(context.Background(), sub.ID, Update{Enabled: &off}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("a manual sync of a disabled subscription was refused: %v", err)
	}
}

// A failing subscription must not stop the ones behind it in the queue.
func TestSchedulerKeepsGoingAfterAFailedSync(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)
	scheduler := newScheduler(t, h, time.Hour)

	ctx := context.Background()
	if _, err := h.service.Create(ctx, NewSubscription{
		Provider: "fake", ArtistSourceID: "27", ArtistName: "Daft Punk",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := h.service.Create(ctx, NewSubscription{
		Provider: "fake", ArtistSourceID: "28", ArtistName: "Kevin MacLeod",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The store refuses to answer for the second subscription's run.
	h.catalog.trackErr = apperr.New(apperr.CodeInternal, "The database operation failed.")

	// Both are attempted even though every one of them fails.
	if attempted := scheduler.tick(ctx); attempted != 2 {
		t.Fatalf("expected both subscriptions to be attempted, got %d", attempted)
	}
}

// A run that fails is retried later rather than on the very next tick.
func TestSchedulerDoesNotRetryAFailureImmediately(t *testing.T) {
	p := discoveryProvider()
	p.discoErr = apperr.New(apperr.CodeProviderUnavailable, "Deezer did not answer.")
	h := newHarness(t, p)
	scheduler := newScheduler(t, h, time.Hour)
	subscribe(t, h, false)

	if attempted := scheduler.tick(context.Background()); attempted != 1 {
		t.Fatalf("the first attempt did not happen: %d", attempted)
	}
	if attempted := scheduler.tick(context.Background()); attempted != 0 {
		t.Fatalf("the failure was retried immediately: %d", attempted)
	}
}

func TestSchedulerStopsWithoutStartingNewSyncs(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, 10*time.Millisecond)
	sub := subscribe(t, h, false)

	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for the first tick to have done its work.
	deadline := time.Now().Add(3 * time.Second)
	for {
		loaded, err := h.service.Get(context.Background(), sub.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if loaded.LastSyncStatus == StatusSuccess {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the scheduler never ran")
		}
		time.Sleep(5 * time.Millisecond)
	}

	scheduler.Stop()

	// After the stop nothing new may be attempted, even when a subscription
	// becomes due again.
	if err := h.store.RecordSync(context.Background(), sub.ID, SyncOutcome{
		At: time.Now().UTC().Add(-time.Hour), NextAt: time.Now().UTC().Add(-time.Minute),
		Status: StatusSuccess,
	}); err != nil {
		t.Fatalf("make it due again: %v", err)
	}
	before := len(h.store.outcomes())
	time.Sleep(60 * time.Millisecond)
	if after := len(h.store.outcomes()); after != before {
		t.Fatalf("the scheduler kept running after Stop: %d new outcomes", after-before)
	}
}

// Stop must be safe to call more than once and before Start.
func TestSchedulerStopIsIdempotent(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, time.Hour)

	scheduler.Stop()
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	scheduler.Stop()
	scheduler.Stop()
}

// A restart must not re-run a subscription whose next run is still in the
// future: the schedule lives in the database, not in the process.
func TestSchedulerRespectsNextSyncAtAcrossARestart(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	first := newScheduler(t, h, time.Hour)
	if first.tick(context.Background()) != 1 {
		t.Fatal("the first run did not happen")
	}
	first.Stop()

	before := len(h.store.outcomes())

	// A new process, the same database.
	second := newScheduler(t, h, time.Hour)
	if synced := second.tick(context.Background()); synced != 0 {
		t.Fatalf("the restart re-ran a subscription that is not due: %d", synced)
	}
	if after := len(h.store.outcomes()); after != before {
		t.Fatalf("the restart produced %d extra runs", after-before)
	}

	loaded, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != StatusSuccess {
		t.Fatalf("the stored state did not survive the restart: %q", loaded.LastSyncStatus)
	}
	if loaded.LastSyncAt == nil {
		t.Fatal("the last sync time did not survive the restart")
	}
}

// A subscription that is due again after a restart is picked back up.
func TestSchedulerPicksUpWorkThatBecameDueWhileTheProcessWasDown(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	first := newScheduler(t, h, time.Hour)
	if first.tick(context.Background()) != 1 {
		t.Fatal("the first run did not happen")
	}
	first.Stop()

	// Time passed while nothing was running.
	past := time.Now().UTC().Add(-25 * time.Hour)
	if err := h.store.RecordSync(context.Background(), sub.ID, SyncOutcome{
		At: past, NextAt: past.Add(24 * time.Hour), Status: StatusSuccess,
	}); err != nil {
		t.Fatalf("age the subscription: %v", err)
	}

	second := newScheduler(t, h, time.Hour)
	if synced := second.tick(context.Background()); synced != 1 {
		t.Fatalf("the overdue subscription was not picked up: %d", synced)
	}
}

// A cancelled context must stop the tick rather than work through the batch.
func TestSchedulerTickHonoursACancelledContext(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	scheduler := newScheduler(t, h, time.Hour)
	subscribe(t, h, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if synced := scheduler.tick(ctx); synced != 0 {
		t.Fatalf("a cancelled tick still synced %d subscriptions", synced)
	}
}

func TestSchedulerNeedsAService(t *testing.T) {
	if _, err := NewScheduler(SchedulerOptions{Interval: time.Hour}); err == nil {
		t.Fatal("a scheduler without a service was accepted")
	}
}
