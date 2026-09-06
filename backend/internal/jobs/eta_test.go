package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/music"
)

func TestCalculateETA(t *testing.T) {
	t.Run("storage unhealthy yields waiting_for_storage", func(t *testing.T) {
		etaSec, conf, text, _ := CalculateETA(100, 0, 0, 0, 50, 200, false)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "waiting_for_storage" {
			t.Errorf("expected waiting_for_storage, got %s", conf)
		}
		if text != "Auf Speicher warten" {
			t.Errorf("expected 'Auf Speicher warten', got %s", text)
		}
	})

	t.Run("empty queue with no paused jobs yields idle", func(t *testing.T) {
		etaSec, conf, text, _ := CalculateETA(0, 0, 0, 0, 50, 200, true)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "idle" {
			t.Errorf("expected idle, got %s", conf)
		}
		if text != "Keine ausstehenden Downloads" {
			t.Errorf("expected 'Keine ausstehenden Downloads', got %s", text)
		}
	})

	t.Run("empty queue with paused jobs yields paused", func(t *testing.T) {
		etaSec, conf, text, _ := CalculateETA(0, 0, 0, 32, 50, 200, true)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "paused" {
			t.Errorf("expected paused, got %s", conf)
		}
		if text != "Queue pausiert" {
			t.Errorf("expected 'Queue pausiert', got %s", text)
		}
	})

	t.Run("entire queue waiting for retry yields retry_wait state", func(t *testing.T) {
		etaSec, conf, text, _ := CalculateETA(15, 15, 0, 0, 50, 200, true)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "retry_wait" {
			t.Errorf("expected retry_wait, got %s", conf)
		}
		if text != "Wartet auf erneuten Versuch" {
			t.Errorf("expected 'Wartet auf erneuten Versuch', got %s", text)
		}
	})

	t.Run("zero history yields calculating fallback", func(t *testing.T) {
		etaSec, conf, text, _ := CalculateETA(500, 0, 2, 32, 0, 0, true)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "none" {
			t.Errorf("expected none, got %s", conf)
		}
		if text != "Berechnung läuft …" {
			t.Errorf("expected 'Berechnung läuft …', got %s", text)
		}
	})

	t.Run("low history under threshold yields calculating fallback", func(t *testing.T) {
		// 4 items in 1h (< 5) and 8 items in 6h (< 10)
		etaSec, conf, text, _ := CalculateETA(500, 0, 2, 32, 4, 8, true)
		if etaSec != nil {
			t.Errorf("expected nil etaSec, got %v", *etaSec)
		}
		if conf != "none" {
			t.Errorf("expected none, got %s", conf)
		}
		if text != "Berechnung läuft …" {
			t.Errorf("expected 'Berechnung läuft …', got %s", text)
		}
	})

	t.Run("low confidence from 5 to 19 items", func(t *testing.T) {
		// completed1h = 10 (5 <= 10 < 20 -> low confidence)
		etaSec, conf, text, rate := CalculateETA(20, 0, 1, 0, 10, 40, true)
		if etaSec == nil {
			t.Fatal("expected non-nil etaSec")
		}
		if conf != "low" {
			t.Errorf("expected low confidence, got %s", conf)
		}
		if rate != 10.0 {
			t.Errorf("expected rate 10.0, got %f", rate)
		}
		// 20 items / 10 items/hr = 2 hours = 7200s
		if *etaSec != 7200 {
			t.Errorf("expected 7200 sec, got %d", *etaSec)
		}
		if text != "ca. 2 Std." {
			t.Errorf("expected 'ca. 2 Std.', got %s", text)
		}
	})

	t.Run("medium confidence from 20 to 99 items", func(t *testing.T) {
		// completed6h = 60 (20 <= 60 < 100 -> medium confidence, rate = 10/hr)
		etaSec, conf, text, rate := CalculateETA(100, 0, 2, 0, 2, 60, true)
		if etaSec == nil {
			t.Fatal("expected non-nil etaSec")
		}
		if conf != "medium" {
			t.Errorf("expected medium confidence, got %s", conf)
		}
		if rate != 10.0 {
			t.Errorf("expected rate 10.0, got %f", rate)
		}
		// 100 items / 10 items/hr = 10 hours = 36000 sec
		if *etaSec != 36000 {
			t.Errorf("expected 36000 sec, got %d", *etaSec)
		}
		if text != "ca. 10 Std." {
			t.Errorf("expected 'ca. 10 Std.', got %s", text)
		}
	})

	t.Run("high confidence from >= 100 items", func(t *testing.T) {
		// completed1h = 100 (>= 100 -> high confidence)
		etaSec, conf, text, rate := CalculateETA(150, 0, 2, 0, 100, 300, true)
		if etaSec == nil {
			t.Fatal("expected non-nil etaSec")
		}
		if conf != "high" {
			t.Errorf("expected high confidence, got %s", conf)
		}
		if rate != 100.0 {
			t.Errorf("expected rate 100.0, got %f", rate)
		}
		// 150 items / 100 items/hr = 1.5 hours = 5400 sec (1h 30m)
		if *etaSec != 5400 {
			t.Errorf("expected 5400 sec, got %d", *etaSec)
		}
		if text != "ca. 1 Std. 30 Min." {
			t.Errorf("expected 'ca. 1 Std. 30 Min.', got %s", text)
		}
	})

	t.Run("large queue calculation", func(t *testing.T) {
		// 7,200 items, throughput 200 items/hr
		etaSec, conf, text, rate := CalculateETA(7200, 0, 2, 32, 200, 1000, true)
		if etaSec == nil {
			t.Fatal("expected non-nil etaSec")
		}
		if conf != "high" {
			t.Errorf("expected high, got %s", conf)
		}
		if rate != 200.0 {
			t.Errorf("expected rate 200.0, got %f", rate)
		}
		// 7200 / 200 = 36 hours = 129600s (~2 days)
		if *etaSec != 129600 {
			t.Errorf("expected 129600, got %d", *etaSec)
		}
		if text != "ca. 2 Tage" {
			t.Errorf("expected 'ca. 2 Tage', got %s", text)
		}
	})
}

func TestFormatETA(t *testing.T) {
	tests := []struct {
		sec      int64
		expected string
	}{
		{0, "< 1 Minute"},
		{45, "< 1 Minute"},
		{60, "< 1 Minute"},
		{120, "ca. 2 Min."},
		{480, "ca. 8 Min."},
		{3540, "ca. 59 Min."},
		{3600, "ca. 1 Std."},
		{4800, "ca. 1 Std. 20 Min."},
		{7200, "ca. 2 Std."},
		{21600, "ca. 6 Std."},
		{86400, "ca. 1 Tag"},
		{172800, "ca. 2 Tage"},
		{600000, "ca. 7 Tage"},
	}

	for _, tc := range tests {
		got := FormatETA(tc.sec, "high")
		if got != tc.expected {
			t.Errorf("FormatETA(%d) = %q, want %q", tc.sec, got, tc.expected)
		}
	}
}

func TestWorkerTracker(t *testing.T) {
	wt := NewWorkerTracker()

	job := Job{
		ID:    "job-1",
		Label: "Album Title",
	}
	item := Item{
		ID:       "item-1",
		JobID:    "job-1",
		Position: 2,
		Label:    "Artist Name - Track Name",
		Track: music.Track{
			Title:       "Track Name",
			Artists:     []string{"Artist Name"},
			Album:       "Album Title",
			TrackNumber: 3,
		},
	}

	wt.RecordProgress(job, item, ItemDownloading, 45.5)

	workers := wt.List()
	if len(workers) != 1 {
		t.Fatalf("expected 1 active worker, got %d", len(workers))
	}
	w := workers[0]
	if w.JobID != "job-1" || w.ItemID != "item-1" {
		t.Errorf("mismatched ids: %+v", w)
	}
	if w.Artist != "Artist Name" || w.Release != "Album Title" || w.Track != "Track Name" {
		t.Errorf("mismatched names: %+v", w)
	}
	if w.TrackNumber != 3 {
		t.Errorf("expected track number 3, got %d", w.TrackNumber)
	}
	if w.Phase != ItemDownloading || w.ProgressPercent != 45.5 {
		t.Errorf("mismatched phase/progress: %+v", w)
	}

	// Update phase to tagging with 0 percent (progress preserved or updated)
	wt.RecordProgress(job, item, ItemTagging, 0)
	workers = wt.List()
	if len(workers) != 1 {
		t.Fatalf("expected 1 active worker, got %d", len(workers))
	}
	if workers[0].Phase != ItemTagging {
		t.Errorf("expected Phase ItemTagging, got %v", workers[0].Phase)
	}

	// Concurrency test
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			it := Item{ID: "item-concurrent", Position: idx, Label: "Test"}
			wt.RecordProgress(job, it, ItemMatching, float64(idx))
		}(i)
		go func() {
			defer wg.Done()
			_ = wt.List()
		}()
	}
	wg.Wait()

	// Clear
	wt.Clear("item-1")
	wt.Clear("item-concurrent")
	if len(wt.List()) != 0 {
		t.Errorf("expected 0 workers after clear, got %d", len(wt.List()))
	}
}

func TestManagerGetQueueSummary(t *testing.T) {
	store := newMemorySchedulerStore()
	m := NewManagerForTest(store, nil)

	ctx := context.Background()
	summary, err := m.GetQueueSummary(ctx)
	if err != nil {
		t.Fatalf("GetQueueSummary failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.ETAText != "Keine ausstehenden Downloads" {
		t.Errorf("expected 'Keine ausstehenden Downloads', got %s", summary.ETAText)
	}
	if summary.ETAConfidence != "idle" {
		t.Errorf("expected idle confidence, got %s", summary.ETAConfidence)
	}

	// Now register a worker progress
	job := Job{ID: "job-active", Label: "Active Album"}
	item := Item{ID: "item-active", Label: "Artist - Song"}
	m.publishProgress(job, item, 42.0)

	summary, err = m.GetQueueSummary(ctx)
	if err != nil {
		t.Fatalf("GetQueueSummary after active worker failed: %v", err)
	}
	if len(summary.Current) != 1 {
		t.Fatalf("expected 1 active worker, got %d", len(summary.Current))
	}
	if summary.Current[0].ProgressPercent != 42.0 {
		t.Errorf("expected 42.0 percent, got %v", summary.Current[0].ProgressPercent)
	}
}

func TestWorkerTracker_LifecycleCleanup(t *testing.T) {
	wt := NewWorkerTracker()
	job := Job{ID: "job-cleanup", Label: "Cleanup Album"}

	// 1. Simulate active worker
	item := Item{ID: "item-lifecycle", Label: "Artist - Song"}
	wt.RecordProgress(job, item, ItemDownloading, 50.0)
	if len(wt.List()) != 1 {
		t.Fatalf("expected 1 active worker, got %d", len(wt.List()))
	}

	// 2. Terminal outcomes clear the worker immediately
	terminalStatuses := []ItemStatus{ItemCompleted, ItemFailed, ItemSkipped, ItemCancelled}
	for _, status := range terminalStatuses {
		wt.RecordProgress(job, item, ItemDownloading, 50.0)
		if len(wt.List()) != 1 {
			t.Fatalf("setup failed for %v", status)
		}
		wt.Clear(item.ID)
		if len(wt.List()) != 0 {
			t.Errorf("worker not cleared for status %v", status)
		}
	}

	// 3. Retry wait outcome clears the worker
	wt.RecordProgress(job, item, ItemDownloading, 50.0)
	wt.Clear(item.ID) // mimics publishItem logic for ItemRetryWait
	if len(wt.List()) != 0 {
		t.Errorf("worker not cleared for retry_wait")
	}
}

func TestNextUpJobs_DispatcherParity(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Now().UTC()

	// 0. Very High priority fresh job (queued, paused=false, pri=very_high) -> Priority = 3
	jobVeryHigh := &Job{
		ID:        "job-very-high",
		Status:    StatusQueued,
		Priority:  PriorityVeryHigh,
		Paused:    false,
		CreatedAt: now.Add(-1 * time.Minute),
		Label:     "Very High Fresh Album",
	}
	store.jobs["job-very-high"] = jobVeryHigh
	store.items["job-very-high"] = []Item{
		{ID: "it-vh1", JobID: "job-very-high", Status: ItemPending},
	}

	// 1. High priority fresh job (queued, paused=false, pri=high) -> EffectivePriority = 2
	jobHigh := &Job{
		ID:        "job-high-fresh",
		Status:    StatusQueued,
		Priority:  PriorityHigh,
		Paused:    false,
		CreatedAt: now.Add(-5 * time.Minute),
		Label:     "High Fresh Album",
	}
	store.jobs["job-high-fresh"] = jobHigh
	store.items["job-high-fresh"] = []Item{
		{ID: "it-hf1", JobID: "job-high-fresh", Status: ItemPending},
	}

	// 2. Matching job with open tracks (status=matching, paused=false, pri=normal, created -10m) -> EffectivePriority = 1
	jobMatching := &Job{
		ID:        "job-matching",
		Status:    StatusMatching,
		Priority:  PriorityNormal,
		Paused:    false,
		CreatedAt: now.Add(-10 * time.Minute),
		Label:     "Matching Album",
	}
	store.jobs["job-matching"] = jobMatching
	store.items["job-matching"] = []Item{
		{ID: "it-m1", JobID: "job-matching", Status: ItemMatching},
		{ID: "it-m2", JobID: "job-matching", Status: ItemPending},
	}

	// 3. Retry-wait job with due retry items (status=retry_wait, paused=false, pri=normal, created -20m -> aged to High) -> EffectivePriority = 2
	// Note: created -20m means it was created BEFORE job-high-fresh (-5m), so effectivePriority 2 with older created_at comes first!
	jobRetryWait := &Job{
		ID:        "job-retry-wait",
		Status:    StatusRetryWait,
		Priority:  PriorityNormal,
		Paused:    false,
		CreatedAt: now.Add(-20 * time.Minute),
		Label:     "Retry Wait Album",
	}
	store.jobs["job-retry-wait"] = jobRetryWait
	store.items["job-retry-wait"] = []Item{
		{ID: "it-rw1", JobID: "job-retry-wait", Status: ItemRetryWait},
	}

	// 4. Waiting for storage job (status=waiting_for_storage, paused=false, pri=low, created -70m -> aged to High) -> EffectivePriority = 2
	// Created -70m (oldest effective priority 2)!
	jobWaitingStorage := &Job{
		ID:        "job-wait-storage",
		Status:    StatusWaitingStorage,
		Priority:  PriorityLow,
		Paused:    false,
		CreatedAt: now.Add(-70 * time.Minute),
		Label:     "Waiting Storage Album",
	}
	store.jobs["job-wait-storage"] = jobWaitingStorage
	store.items["job-wait-storage"] = []Item{
		{ID: "it-ws1", JobID: "job-wait-storage", Status: ItemWaitingStorage},
		{ID: "it-ws2", JobID: "job-wait-storage", Status: ItemWaitingSpace},
	}

	// 5. Normal priority fresh job (status=queued, paused=false, pri=normal, created -2m) -> EffectivePriority = 1
	jobNormalFresh := &Job{
		ID:        "job-norm-fresh",
		Status:    StatusQueued,
		Priority:  PriorityNormal,
		Paused:    false,
		CreatedAt: now.Add(-2 * time.Minute),
		Label:     "Normal Fresh Album",
	}
	store.jobs["job-norm-fresh"] = jobNormalFresh
	store.items["job-norm-fresh"] = []Item{
		{ID: "it-nf1", JobID: "job-norm-fresh", Status: ItemPending},
	}

	// 6. Low priority fresh job (status=queued, paused=false, pri=low, created -5m) -> EffectivePriority = 0
	jobLowFresh := &Job{
		ID:        "job-low-fresh",
		Status:    StatusQueued,
		Priority:  PriorityLow,
		Paused:    false,
		CreatedAt: now.Add(-5 * time.Minute),
		Label:     "Low Fresh Album",
	}
	store.jobs["job-low-fresh"] = jobLowFresh
	store.items["job-low-fresh"] = []Item{
		{ID: "it-lf1", JobID: "job-low-fresh", Status: ItemPending},
	}

	// --- EXCLUDED JOBS ---

	// Excluded: Queued but Paused (queued, paused=true)
	jobQueuedPaused := &Job{
		ID:        "job-queued-paused",
		Status:    StatusQueued,
		Priority:  PriorityHigh,
		Paused:    true,
		CreatedAt: now.Add(-80 * time.Minute),
		Label:     "Queued Paused Album",
	}
	store.jobs["job-queued-paused"] = jobQueuedPaused
	store.items["job-queued-paused"] = []Item{
		{ID: "it-qp1", JobID: "job-queued-paused", Status: ItemPending},
	}

	// Excluded: Completed (status=completed)
	jobCompleted := &Job{
		ID:        "job-completed",
		Status:    StatusCompleted,
		Priority:  PriorityHigh,
		Paused:    false,
		CreatedAt: now.Add(-90 * time.Minute),
		Label:     "Completed Album",
	}
	store.jobs["job-completed"] = jobCompleted
	store.items["job-completed"] = []Item{
		{ID: "it-c1", JobID: "job-completed", Status: ItemCompleted},
	}

	// Excluded: Failed (status=failed)
	jobFailed := &Job{
		ID:        "job-failed",
		Status:    StatusFailed,
		Priority:  PriorityHigh,
		Paused:    false,
		CreatedAt: now.Add(-95 * time.Minute),
		Label:     "Failed Album",
	}
	store.jobs["job-failed"] = jobFailed
	store.items["job-failed"] = []Item{
		{ID: "it-f1", JobID: "job-failed", Status: ItemFailed},
	}

	// Excluded: Cancelled (status=cancelled)
	jobCancelled := &Job{
		ID:        "job-cancelled",
		Status:    StatusCancelled,
		Priority:  PriorityHigh,
		Paused:    false,
		CreatedAt: now.Add(-100 * time.Minute),
		Label:     "Cancelled Album",
	}
	store.jobs["job-cancelled"] = jobCancelled
	store.items["job-cancelled"] = []Item{
		{ID: "it-ca1", JobID: "job-cancelled", Status: ItemCancelled},
	}

	// Excluded: In-flight job whose items are all already completed or matching (no open/runnable tracks)
	jobAllDone := &Job{
		ID:        "job-alldone",
		Status:    StatusDownloading,
		Priority:  PriorityHigh,
		Paused:    false,
		CreatedAt: now.Add(-15 * time.Minute),
		Label:     "All Items Done Album",
	}
	store.jobs["job-alldone"] = jobAllDone
	store.items["job-alldone"] = []Item{
		{ID: "it-ad1", JobID: "job-alldone", Status: ItemCompleted},
	}

	ctx := context.Background()

	// Verify LIMIT 5 enforcement
	next5, err := store.NextUpJobs(ctx, 5)
	if err != nil {
		t.Fatalf("NextUpJobs limit 5 error: %v", err)
	}
	if len(next5) != 5 {
		t.Fatalf("expected exactly 5 next up jobs for limit 5, got %d", len(next5))
	}

	// Expected Order according to Deterministic Priority Dispatcher Semantics (priority DESC, created_at ASC, id ASC):
	// 1. job-very-high    (pri=very_high base rank 3, created -1m)
	// 2. job-high-fresh   (pri=high base rank 2, created -5m)
	// 3. job-retry-wait   (pri=normal base rank 1, created -20m)
	// 4. job-matching     (pri=normal base rank 1, created -10m)
	// 5. job-norm-fresh   (pri=normal base rank 1, created -2m)
	// (job-wait-storage is #6 pri=low, job-low-fresh is #7, correctly cut off by LIMIT 5)
	expectedOrder := []string{
		"job-very-high",
		"job-high-fresh",
		"job-retry-wait",
		"job-matching",
		"job-norm-fresh",
	}

	for i, expectedID := range expectedOrder {
		if next5[i].JobID != expectedID {
			t.Errorf("at index %d: expected job ID %q, got %q", i, expectedID, next5[i].JobID)
		}
	}

	// Verify excluded jobs are not in the preview
	excludedIDs := map[string]bool{
		"job-queued-paused": true,
		"job-completed":     true,
		"job-failed":        true,
		"job-cancelled":     true,
		"job-alldone":       true,
	}
	for _, j := range next5 {
		if excludedIDs[j.JobID] {
			t.Errorf("excluded job %q appeared in NextUpJobs preview", j.JobID)
		}
	}
}
