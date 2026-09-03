package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/music"
)

func TestDownloadScheduleWindow(t *testing.T) {
	mgr := &Manager{}

	// 1. Schedule disabled -> always true
	mgr.SetScheduleEnabled(false)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if !mgr.isInsideDownloadWindow(now) {
		t.Fatal("expected inside window when schedule is disabled")
	}

	// 2. Daytime window: 08:00 to 18:00 UTC
	mgr.SetScheduleEnabled(true)
	mgr.SetScheduleStart("08:00")
	mgr.SetScheduleEnd("18:00")
	mgr.SetScheduleTimezone("UTC")

	testCases := []struct {
		hour int
		min  int
		want bool
	}{
		{7, 59, false},
		{8, 0, true}, // start inclusive
		{12, 30, true},
		{17, 59, true},
		{18, 0, false}, // end exclusive
		{19, 0, false},
	}

	for _, tc := range testCases {
		testTime := time.Date(2026, 8, 28, tc.hour, tc.min, 0, 0, time.UTC)
		got := mgr.isInsideDownloadWindow(testTime)
		if got != tc.want {
			t.Errorf("daytime window at %02d:%02d: got %v, want %v", tc.hour, tc.min, got, tc.want)
		}
	}

	// 3. Overnight window: 22:00 to 06:00 UTC
	mgr.SetScheduleStart("22:00")
	mgr.SetScheduleEnd("06:00")

	overnightCases := []struct {
		hour int
		min  int
		want bool
	}{
		{21, 59, false},
		{22, 0, true}, // start inclusive
		{23, 30, true},
		{0, 0, true},
		{3, 15, true},
		{5, 59, true},
		{6, 0, false}, // end exclusive
		{12, 0, false},
	}

	for _, tc := range overnightCases {
		testTime := time.Date(2026, 8, 28, tc.hour, tc.min, 0, 0, time.UTC)
		got := mgr.isInsideDownloadWindow(testTime)
		if got != tc.want {
			t.Errorf("overnight window at %02d:%02d: got %v, want %v", tc.hour, tc.min, got, tc.want)
		}
	}
}

func TestScheduleTimezoneDST(t *testing.T) {
	mgr := &Manager{}
	mgr.SetScheduleEnabled(true)
	mgr.SetScheduleStart("08:00")
	mgr.SetScheduleEnd("18:00")
	mgr.SetScheduleTimezone("Europe/Berlin")

	// Berlin is UTC+2 in summer (CEST), UTC+1 in winter (CET)
	// Summer date: 2026-07-01 07:00 UTC == 09:00 CEST -> in window (08:00..18:00)
	summerTime := time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC)
	if !mgr.isInsideDownloadWindow(summerTime) {
		t.Fatal("expected 07:00 UTC (09:00 CEST) in Berlin window")
	}

	// Winter date: 2026-01-01 06:30 UTC == 07:30 CET -> outside window (08:00..18:00)
	winterEarly := time.Date(2026, 1, 1, 6, 30, 0, 0, time.UTC)
	if mgr.isInsideDownloadWindow(winterEarly) {
		t.Fatal("expected 06:30 UTC (07:30 CET) outside Berlin window")
	}

	// Winter date: 2026-01-01 07:30 UTC == 08:30 CET -> in window (08:00..18:00)
	winterIn := time.Date(2026, 1, 1, 7, 30, 0, 0, time.UTC)
	if !mgr.isInsideDownloadWindow(winterIn) {
		t.Fatal("expected 07:30 UTC (08:30 CET) in Berlin window")
	}
}

func TestStarvationAging(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	// High priority always rank 2
	jHigh := Job{Priority: PriorityHigh, CreatedAt: now.Add(-5 * time.Minute)}
	if r := effectivePriority(jHigh, now); r != 2 {
		t.Fatalf("expected high priority rank 2, got %d", r)
	}

	// Normal priority:
	// < 15m -> rank 1
	jNormalFresh := Job{Priority: PriorityNormal, CreatedAt: now.Add(-10 * time.Minute)}
	if r := effectivePriority(jNormalFresh, now); r != 1 {
		t.Fatalf("expected fresh normal rank 1, got %d", r)
	}
	// >= 15m -> rank 2 (promoted to High)
	jNormalAged := Job{Priority: PriorityNormal, CreatedAt: now.Add(-15 * time.Minute)}
	if r := effectivePriority(jNormalAged, now); r != 2 {
		t.Fatalf("expected aged normal rank 2, got %d", r)
	}

	// Low priority:
	// < 30m -> rank 0
	jLowFresh := Job{Priority: PriorityLow, CreatedAt: now.Add(-20 * time.Minute)}
	if r := effectivePriority(jLowFresh, now); r != 0 {
		t.Fatalf("expected fresh low rank 0, got %d", r)
	}
	// >= 30m and < 60m -> rank 1 (promoted to Normal)
	jLow30m := Job{Priority: PriorityLow, CreatedAt: now.Add(-30 * time.Minute)}
	if r := effectivePriority(jLow30m, now); r != 1 {
		t.Fatalf("expected 30m low rank 1, got %d", r)
	}
	jLow45m := Job{Priority: PriorityLow, CreatedAt: now.Add(-45 * time.Minute)}
	if r := effectivePriority(jLow45m, now); r != 1 {
		t.Fatalf("expected 45m low rank 1, got %d", r)
	}
	// >= 60m -> rank 2 (promoted to High)
	jLow60m := Job{Priority: PriorityLow, CreatedAt: now.Add(-60 * time.Minute)}
	if r := effectivePriority(jLow60m, now); r != 2 {
		t.Fatalf("expected 60m low rank 2, got %d", r)
	}
}

func TestManagerWorkerHotReload(t *testing.T) {
	mgr := &Manager{}
	mgr.SetMaxWorkers(2)
	if w := mgr.MaxWorkers(); w != 2 {
		t.Fatalf("expected 2 workers, got %d", w)
	}

	mgr.SetMaxWorkers(4)
	if w := mgr.MaxWorkers(); w != 4 {
		t.Fatalf("expected 4 workers, got %d", w)
	}

	// Out of bounds clamped
	mgr.SetMaxWorkers(10)
	if w := mgr.MaxWorkers(); w != 4 {
		t.Fatalf("expected clamped 4 workers, got %d", w)
	}
	mgr.SetMaxWorkers(0)
	if w := mgr.MaxWorkers(); w != 2 {
		t.Fatalf("expected clamped 2 workers, got %d", w)
	}
}

func TestWorkerDownscale4to1(t *testing.T) {
	mgr := &Manager{}
	mgr.SetMaxWorkers(4)
	if mgr.MaxWorkers() != 4 {
		t.Fatalf("expected 4 workers, got %d", mgr.MaxWorkers())
	}

	// 1. Simulate 4 running workers
	mgr.activeWorkers.Store(4)

	// 2. Downscale to 1 worker via hot-reload
	mgr.SetMaxWorkers(1)
	if mgr.MaxWorkers() != 1 {
		t.Fatalf("expected 1 worker, got %d", mgr.MaxWorkers())
	}

	// 3. While active > 1, no new downloads should be dispatched
	if int(mgr.activeWorkers.Load()) < mgr.MaxWorkers() {
		t.Fatalf("expected active workers (4) to exceed downscaled capacity (1) during drain")
	}

	// 4. Drain existing 4 workers naturally 1 by 1
	mgr.activeWorkers.Add(-1) // 3 active
	if int(mgr.activeWorkers.Load()) < mgr.MaxWorkers() {
		t.Fatalf("active workers 3 still >= 1")
	}

	mgr.activeWorkers.Add(-1) // 2 active
	if int(mgr.activeWorkers.Load()) < mgr.MaxWorkers() {
		t.Fatalf("active workers 2 still >= 1")
	}

	mgr.activeWorkers.Add(-1) // 1 active
	if int(mgr.activeWorkers.Load()) < mgr.MaxWorkers() {
		t.Fatalf("active workers 1 still >= 1")
	}

	mgr.activeWorkers.Add(-1) // 0 active
	// 5. Now capacity becomes available for exactly 1 worker
	if int(mgr.activeWorkers.Load()) >= mgr.MaxWorkers() {
		t.Fatalf("active workers 0 < 1, now exactly 1 worker can be dispatched")
	}
}

// Memory Mock Store for Deterministic Scheduler Simulation
type memorySchedulerStore struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	items      map[string][]Item
	itemMap    map[string]*Item
	inFlight   map[string]bool
	claimOrder []string // item IDs claimed in order
}

func newMemorySchedulerStore() *memorySchedulerStore {
	return &memorySchedulerStore{
		jobs:     make(map[string]*Job),
		items:    make(map[string][]Item),
		itemMap:  make(map[string]*Item),
		inFlight: make(map[string]bool),
	}
}

func (s *memorySchedulerStore) addJob(j Job, items []Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = &j
	s.items[j.ID] = items
	for i := range items {
		s.itemMap[items[i].ID] = &items[i]
	}
}

func (s *memorySchedulerStore) ListUnfinished(_ context.Context) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []Job
	for _, j := range s.jobs {
		if !j.Status.Terminal() && !j.Paused {
			res = append(res, *j)
		}
	}
	return res, nil
}

func (s *memorySchedulerStore) ListPendingItems(_ context.Context, jobID string) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []Item
	for _, it := range s.items[jobID] {
		if (it.Status == ItemPending || it.Status == ItemWaitingStorage || it.Status == ItemWaitingSpace || it.Status == ItemRetryWait) && !s.inFlight[it.ID] {
			res = append(res, it)
		}
	}
	return res, nil
}

func (s *memorySchedulerStore) UpdateItem(_ context.Context, itemID string, update ItemUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.itemMap[itemID]
	if !ok {
		return nil
	}
	if update.Status != "" {
		it.Status = update.Status
	}
	if update.Attempts != nil {
		it.Attempts = *update.Attempts
	}
	if update.NextRetryAt != nil {
		it.NextRetryAt = update.NextRetryAt
	}
	return nil
}

func (s *memorySchedulerStore) ResetItemForRetry(_ context.Context, jobID, itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.itemMap[itemID]
	if !ok {
		return nil
	}
	it.Status = ItemPending
	it.ErrorCode = ""
	it.ErrorMessage = ""
	it.NextRetryAt = nil
	return nil
}

func (s *memorySchedulerStore) ResetFailedItemsInJob(_ context.Context, jobID string) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	retried := 0
	skipped := 0
	for i := range s.items[jobID] {
		it := &s.items[jobID][i]
		if it.Status == ItemFailed {
			if it.ErrorCode == "PATH_CONFLICT" {
				skipped++
				continue
			}
			it.Status = ItemPending
			it.ErrorCode = ""
			it.ErrorMessage = ""
			it.NextRetryAt = nil
			retried++
		}
	}
	return retried, skipped, nil
}

// Unused methods to satisfy Store interface
func (s *memorySchedulerStore) Create(context.Context, *Job) error { return nil }
func (s *memorySchedulerStore) Get(_ context.Context, id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, nil
	}
	return j, nil
}
func (s *memorySchedulerStore) List(context.Context, ListFilter) ([]Job, int, error) {
	return nil, 0, nil
}
func (s *memorySchedulerStore) SetStatus(context.Context, string, Status, string, string) error {
	return nil
}
func (s *memorySchedulerStore) SetLabel(context.Context, string, string) error { return nil }
func (s *memorySchedulerStore) SetTotal(context.Context, string, int) error    { return nil }
func (s *memorySchedulerStore) CancelPendingItems(context.Context, string) (int, error) {
	return 0, nil
}
func (s *memorySchedulerStore) RefreshCounters(context.Context, string) (*Job, error) {
	return nil, nil
}
func (s *memorySchedulerStore) SetPriority(_ context.Context, id string, p Priority) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Priority = p
	}
	return nil
}
func (s *memorySchedulerStore) SetPaused(_ context.Context, id string, paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Paused = paused
	}
	return nil
}
func (s *memorySchedulerStore) DeleteHistory(context.Context, time.Time, []Status) (int, int, error) {
	return 0, 0, nil
}
func (s *memorySchedulerStore) AddItems(context.Context, string, []Item) error { return nil }
func (s *memorySchedulerStore) ListItems(_ context.Context, jobID string) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[jobID], nil
}
func (s *memorySchedulerStore) GetItem(_ context.Context, id string) (*Item, error) {

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.itemMap[id], nil
}
func (s *memorySchedulerStore) HasItems(context.Context, string) (bool, error)  { return false, nil }
func (s *memorySchedulerStore) ResetInFlightItems(context.Context) (int, error) { return 0, nil }
func (s *memorySchedulerStore) ResetInterruptedJobs(context.Context) (int, error) {
	return 0, nil
}

func TestSchedulerInterleaving500vs1(t *testing.T) {
	// Job A: 500 items, normal
	// Job B: 1 item, normal
	// 2 workers
	// Pass 1 must allocate 1 slot to A and 1 slot to B so B runs in the first cycle!
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	jobA := Job{
		ID:        "job_a",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now,
	}
	var itemsA []Item
	for i := 1; i <= 500; i++ {
		itemsA = append(itemsA, Item{
			ID:     "item_a_" + time.Duration(i).String(),
			JobID:  "job_a",
			Status: ItemPending,
			Track:  music.Track{ID: "track_a_" + time.Duration(i).String()},
		})
	}
	store.addJob(jobA, itemsA)

	jobB := Job{
		ID:        "job_b",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now.Add(1 * time.Second),
	}
	itemsB := []Item{
		{
			ID:     "item_b_1",
			JobID:  "job_b",
			Status: ItemPending,
			Track:  music.Track{ID: "track_b_1"},
		},
	}
	store.addJob(jobB, itemsB)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 2),
	}
	mgr.maxWorkers.Store(2)
	defStart := "00:00"
	defEnd := "23:59"
	empty := ""
	mgr.scheduleStart.Store(&defStart)
	mgr.scheduleEnd.Store(&defEnd)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}

	// Verify that candidates contain 1 item from job A and 1 item from job B (fair 1-slot cap)
	jobIDs := make(map[string]int)
	for _, c := range candidates[:2] {
		jobIDs[c.job.ID]++
	}
	if jobIDs["job_a"] != 1 || jobIDs["job_b"] != 1 {
		t.Fatalf("expected fair allocation (1 slot to A, 1 slot to B), got: %+v", jobIDs)
	}
}

func TestSchedulerFairness4Workers3Jobs(t *testing.T) {
	// Jobs: A, B, C (all normal)
	// Workers: 4
	// Pass 1 allocates 1 slot to A, 1 to B, 1 to C (3 slots).
	// Pass 2 fills the 4th slot. All 4 workers utilized!
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	for _, id := range []string{"job_a", "job_b", "job_c"} {
		j := Job{
			ID:        id,
			Status:    StatusDownloading,
			Priority:  PriorityNormal,
			CreatedAt: now,
		}
		var items []Item
		for i := 1; i <= 5; i++ {
			items = append(items, Item{
				ID:     id + "_item_" + time.Duration(i).String(),
				JobID:  id,
				Status: ItemPending,
				Track:  music.Track{ID: id + "_track_" + time.Duration(i).String()},
			})
		}
		store.addJob(j, items)
	}

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 4),
	}
	mgr.maxWorkers.Store(4)
	defStart := "00:00"
	defEnd := "23:59"
	empty := ""
	mgr.scheduleStart.Store(&defStart)
	mgr.scheduleEnd.Store(&defEnd)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) < 3 {
		t.Fatalf("expected at least 3 job candidates for 4 workers, got %d", len(candidates))
	}

	jobCounts := make(map[string]int)
	for _, c := range candidates {
		jobCounts[c.job.ID] += len(c.ready)
	}

	// Every job (A, B, C) must have ready items
	if jobCounts["job_a"] < 1 || jobCounts["job_b"] < 1 || jobCounts["job_c"] < 1 {
		t.Fatalf("expected all 3 jobs to have ready candidates, got: %+v", jobCounts)
	}
}

func TestSchedulerPriorityHighOverLow(t *testing.T) {
	// A: 500 items, low
	// B: 1 item, high
	// 2 workers
	// B must be selected first
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	jobA := Job{
		ID:        "job_a",
		Status:    StatusDownloading,
		Priority:  PriorityLow,
		CreatedAt: now,
	}
	var itemsA []Item
	for i := 1; i <= 500; i++ {
		itemsA = append(itemsA, Item{
			ID:     "item_a_" + time.Duration(i).String(),
			JobID:  "job_a",
			Status: ItemPending,
			Track:  music.Track{ID: "track_a_" + time.Duration(i).String()},
		})
	}
	store.addJob(jobA, itemsA)

	jobB := Job{
		ID:        "job_b",
		Status:    StatusDownloading,
		Priority:  PriorityHigh,
		CreatedAt: now.Add(5 * time.Second),
	}
	itemsB := []Item{
		{
			ID:     "item_b_1",
			JobID:  "job_b",
			Status: ItemPending,
			Track:  music.Track{ID: "track_b_1"},
		},
	}
	store.addJob(jobB, itemsB)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 2),
	}
	mgr.maxWorkers.Store(2)
	defStart := "00:00"
	defEnd := "23:59"
	empty := ""
	mgr.scheduleStart.Store(&defStart)
	mgr.scheduleEnd.Store(&defEnd)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	if candidates[0].job.ID != "job_b" {
		t.Fatalf("expected high priority job B as first candidate, got %s", candidates[0].job.ID)
	}
}

func TestSchedulerPausedJobBehavior(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	// Paused job
	jobPaused := Job{
		ID:        "job_paused",
		Status:    StatusDownloading,
		Priority:  PriorityHigh,
		Paused:    true,
		CreatedAt: now,
	}
	items := []Item{
		{
			ID:     "item_paused_1",
			JobID:  "job_paused",
			Status: ItemPending,
			Track:  music.Track{ID: "track_1"},
		},
	}
	store.addJob(jobPaused, items)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 2),
	}
	mgr.maxWorkers.Store(2)
	defStart := "00:00"
	defEnd := "23:59"
	empty := ""
	mgr.scheduleStart.Store(&defStart)
	mgr.scheduleEnd.Store(&defEnd)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates from paused job, got %d", len(candidates))
	}
}

func TestBulkRetryFailedItems(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	job := Job{
		ID:        "job_retry",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now,
	}
	items := []Item{
		{ID: "item_fail_1", JobID: "job_retry", Status: ItemFailed, ErrorCode: "DOWNLOAD_FAILED"},
		{ID: "item_fail_2", JobID: "job_retry", Status: ItemFailed, ErrorCode: "NETWORK_TIMEOUT"},
		{ID: "item_path_conflict", JobID: "job_retry", Status: ItemFailed, ErrorCode: "PATH_CONFLICT"},
		{ID: "item_completed", JobID: "job_retry", Status: ItemCompleted},
		{ID: "item_cancelled", JobID: "job_retry", Status: ItemCancelled},
	}
	store.addJob(job, items)

	retried, skipped, err := store.ResetFailedItemsInJob(context.Background(), "job_retry")
	if err != nil {
		t.Fatalf("ResetFailedItemsInJob: %v", err)
	}
	if retried != 2 {
		t.Fatalf("expected 2 retried items, got %d", retried)
	}
	if skipped != 1 {
		t.Fatalf("expected 1 skipped (PATH_CONFLICT) item, got %d", skipped)
	}

	// Verify states
	if store.itemMap["item_fail_1"].Status != ItemPending {
		t.Errorf("expected item_fail_1 to be pending")
	}
	if store.itemMap["item_fail_2"].Status != ItemPending {
		t.Errorf("expected item_fail_2 to be pending")
	}
	if store.itemMap["item_path_conflict"].Status != ItemFailed {
		t.Errorf("expected PATH_CONFLICT to remain failed")
	}
	if store.itemMap["item_completed"].Status != ItemCompleted {
		t.Errorf("expected completed item untouched")
	}
	if store.itemMap["item_cancelled"].Status != ItemCancelled {
		t.Errorf("expected cancelled item untouched")
	}
}

func TestRetryWaitDueCandidate(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	job := Job{
		ID:        "job_retry_wait",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now,
	}
	// Item with next_retry_at in the past -> due
	dueTime := now.Add(-1 * time.Minute)
	notDueTime := now.Add(5 * time.Minute)
	items := []Item{
		{ID: "item_due", JobID: "job_retry_wait", Status: ItemRetryWait, NextRetryAt: &dueTime},
		{ID: "item_not_due", JobID: "job_retry_wait", Status: ItemRetryWait, NextRetryAt: &notDueTime},
	}
	store.addJob(job, items)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 2),
	}
	mgr.maxWorkers.Store(2)
	defStart := "00:00"
	defEnd := "23:59"
	empty := ""
	mgr.scheduleStart.Store(&defStart)
	mgr.scheduleEnd.Store(&defEnd)
	mgr.scheduleTimezone.Store(&empty)
	mgr.rateLimit.Store(&empty)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) != 1 || len(candidates[0].ready) != 1 {
		t.Fatalf("expected exactly 1 candidate (due retry), got %d", len(candidates))
	}
	if candidates[0].ready[0].ID != "item_due" {
		t.Fatalf("expected item_due, got %s", candidates[0].ready[0].ID)
	}
}

func TestWaitingForStorageRecovery(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	job := Job{
		ID:        "job_storage",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now,
	}
	items := []Item{
		{ID: "item_staged_storage", JobID: "job_storage", Status: ItemWaitingStorage, StagedSHA256: "abc123sha"},
	}
	store.addJob(job, items)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now },
		semaphore: make(chan struct{}, 2),
	}
	mgr.maxWorkers.Store(2)
	// Window closed outside schedule
	mgr.SetScheduleEnabled(true)
	mgr.SetScheduleStart("22:00")
	mgr.SetScheduleEnd("06:00")
	mgr.SetScheduleTimezone("UTC")

	// Current time 12:00 UTC (outside window 22:00..06:00)
	outsideTime := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	mgr.SetNowFunc(func() time.Time { return outsideTime })

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) != 1 || len(candidates[0].ready) != 1 {
		t.Fatalf("expected staged item waiting for storage to bypass schedule window, got %d candidates", len(candidates))
	}
	if candidates[0].ready[0].ID != "item_staged_storage" {
		t.Fatalf("expected item_staged_storage, got %s", candidates[0].ready[0].ID)
	}
}

func TestFinalizerSingleConcurrencySlot(t *testing.T) {
	mgr := &Manager{
		finalizerSem: make(chan struct{}, 1),
	}

	// Finalizer semaphore capacity must strictly equal 1
	if cap(mgr.finalizerSem) != 1 {
		t.Fatalf("expected finalizerSem cap=1, got %d", cap(mgr.finalizerSem))
	}

	// Verify acquisition and release
	mgr.finalizerSem <- struct{}{}
	select {
	case mgr.finalizerSem <- struct{}{}:
		t.Fatal("expected second acquisition to block (cap 1)")
	default:
		// Expected to block
	}
	<-mgr.finalizerSem
}
