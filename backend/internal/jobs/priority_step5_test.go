package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func newTestManagerForPriority(store Store) *Manager {
	reg := provider.NewRegistry()
	reg.RegisterMetadata(enqueueMetadata{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Manager{
		store:        store,
		registry:     reg,
		broker:       NewBroker(nil),
		logger:       logger,
		resolveQueue: make(chan string, 64),
		wake:         make(chan struct{}, 1),
	}
	m.accepting.Store(true)
	return m
}

// 1. manual request with no explicit priority -> PriorityHigh
func TestStep5_1_ManualRequest_DefaultPriorityHigh(t *testing.T) {
	store := &atomicEnqueueStore{}
	m := newTestManagerForPriority(store)

	manualOrigin := OriginManual
	job, err := m.Enqueue(context.Background(), Request{
		Type:             TypeTrack,
		MetadataProvider: "ytmusic",
		TargetID:         "track-1",
		Options: RequestOptions{
			Origin: &manualOrigin,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if job.Priority != PriorityHigh {
		t.Errorf("job.Priority = %v, want %v", job.Priority, PriorityHigh)
	}
	if job.Priority.Rank() != 2 {
		t.Errorf("job.Priority.Rank() = %d, want 2", job.Priority.Rank())
	}
	if job.Options.Origin != OriginManual {
		t.Errorf("job.Options.Origin = %v, want %v", job.Options.Origin, OriginManual)
	}
}

// 2. subscription request -> PriorityLow (and background/unspecified default is Low)
func TestStep5_2_SubscriptionRequest_DefaultPriorityLow(t *testing.T) {
	store := &atomicEnqueueStore{}
	m := newTestManagerForPriority(store)

	subOrigin := OriginSubscription
	jobSub, err := m.Enqueue(context.Background(), Request{
		Type:             TypeRelease,
		MetadataProvider: "ytmusic",
		TargetID:         "release-sub",
		Options: RequestOptions{
			Origin: &subOrigin,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue subscription failed: %v", err)
	}
	if jobSub.Priority != PriorityLow {
		t.Errorf("jobSub.Priority = %v, want %v", jobSub.Priority, PriorityLow)
	}
	if jobSub.Priority.Rank() != 0 {
		t.Errorf("jobSub.Priority.Rank() = %d, want 0", jobSub.Priority.Rank())
	}

	// Unspecified/empty origin also defaults to PriorityLow
	jobEmpty, err := m.Enqueue(context.Background(), Request{
		Type:             TypeRelease,
		MetadataProvider: "ytmusic",
		TargetID:         "release-empty",
		Options:          RequestOptions{},
	})
	if err != nil {
		t.Fatalf("Enqueue empty origin failed: %v", err)
	}
	if jobEmpty.Priority != PriorityLow {
		t.Errorf("jobEmpty.Priority = %v, want %v", jobEmpty.Priority, PriorityLow)
	}
}

// 3. explicit manual priority override, if supported -> preserved
func TestStep5_3_ExplicitManualPriorityOverride_Preserved(t *testing.T) {
	cases := []struct {
		inputPriority Priority
		wantPriority  Priority
		wantRank      int
	}{
		{PriorityLow, PriorityLow, 0},
		{PriorityNormal, PriorityNormal, 1},
		{PriorityHigh, PriorityHigh, 2},
		{PriorityVeryHigh, PriorityVeryHigh, 3},
		{PriorityUrgent, PriorityVeryHigh, 3}, // Canonicalized
	}

	for _, tc := range cases {
		t.Run(string(tc.inputPriority), func(t *testing.T) {
			store := &atomicEnqueueStore{}
			m := newTestManagerForPriority(store)

			manualOrigin := OriginManual
			pri := tc.inputPriority
			job, err := m.Enqueue(context.Background(), Request{
				Type:             TypeTrack,
				MetadataProvider: "ytmusic",
				TargetID:         "track-" + string(tc.inputPriority),
				Options: RequestOptions{
					Origin:   &manualOrigin,
					Priority: &pri,
				},
			})
			if err != nil {
				t.Fatalf("Enqueue failed: %v", err)
			}
			if job.Priority != tc.wantPriority {
				t.Errorf("job.Priority = %v, want %v", job.Priority, tc.wantPriority)
			}
			if job.Priority.Rank() != tc.wantRank {
				t.Errorf("job.Priority.Rank() = %d, want %d", job.Priority.Rank(), tc.wantRank)
			}
		})
	}
}

// 4. explicit subscription priority override, if supported -> preserve existing intended semantics
func TestStep5_4_ExplicitSubscriptionPriorityOverride_Preserved(t *testing.T) {
	cases := []struct {
		inputPriority Priority
		wantPriority  Priority
		wantRank      int
	}{
		{PriorityNormal, PriorityNormal, 1},
		{PriorityHigh, PriorityHigh, 2},
		{PriorityVeryHigh, PriorityVeryHigh, 3},
	}

	for _, tc := range cases {
		t.Run(string(tc.inputPriority), func(t *testing.T) {
			store := &atomicEnqueueStore{}
			m := newTestManagerForPriority(store)

			subOrigin := OriginSubscription
			pri := tc.inputPriority
			job, err := m.Enqueue(context.Background(), Request{
				Type:             TypeRelease,
				MetadataProvider: "ytmusic",
				TargetID:         "release-" + string(tc.inputPriority),
				Options: RequestOptions{
					Origin:   &subOrigin,
					Priority: &pri,
				},
			})
			if err != nil {
				t.Fatalf("Enqueue failed: %v", err)
			}
			if job.Priority != tc.wantPriority {
				t.Errorf("job.Priority = %v, want %v", job.Priority, tc.wantPriority)
			}
			if job.Priority.Rank() != tc.wantRank {
				t.Errorf("job.Priority.Rank() = %d, want %d", job.Priority.Rank(), tc.wantRank)
			}
		})
	}
}

// 5. dispatcher ordering: manual High is selected before otherwise equivalent Normal/Low work
func TestStep5_5_DispatcherOrdering_ManualHighBeforeEquivalentNormalAndLow(t *testing.T) {
	store := newMemorySchedulerStore()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	// Low priority (subscription) created at T0
	jobLow := Job{
		ID:        "job_sub_low",
		Status:    StatusDownloading,
		Priority:  PriorityLow,
		CreatedAt: now,
		Options:   Options{Origin: OriginSubscription},
	}
	itemsLow := []Item{
		{ID: "item_low_1", JobID: "job_sub_low", Status: ItemPending, Track: music.Track{ID: "t_low"}},
	}
	store.addJob(jobLow, itemsLow)

	// Normal priority created at T0 + 10s
	jobNormal := Job{
		ID:        "job_normal",
		Status:    StatusDownloading,
		Priority:  PriorityNormal,
		CreatedAt: now.Add(10 * time.Second),
	}
	itemsNormal := []Item{
		{ID: "item_normal_1", JobID: "job_normal", Status: ItemPending, Track: music.Track{ID: "t_normal"}},
	}
	store.addJob(jobNormal, itemsNormal)

	// Manual High priority created at T0 + 20s (most recently created!)
	jobManualHigh := Job{
		ID:        "job_manual_high",
		Status:    StatusDownloading,
		Priority:  PriorityHigh,
		CreatedAt: now.Add(20 * time.Second),
		Options:   Options{Origin: OriginManual},
	}
	itemsHigh := []Item{
		{ID: "item_high_1", JobID: "job_manual_high", Status: ItemPending, Track: music.Track{ID: "t_high"}},
	}
	store.addJob(jobManualHigh, itemsHigh)

	mgr := &Manager{
		store:     store,
		nowFunc:   func() time.Time { return now.Add(30 * time.Second) },
		semaphore: make(chan struct{}, 3),
	}
	mgr.maxWorkers.Store(3)
	mgr.activeWorkers.Store(0)

	candidates := mgr.collectCandidates(context.Background())
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}

	// High priority MUST be first candidate, Normal second, Low third
	if candidates[0].job.ID != "job_manual_high" {
		t.Errorf("candidate[0] = %s (priority %v), want job_manual_high", candidates[0].job.ID, candidates[0].job.Priority)
	}
	if candidates[1].job.ID != "job_normal" {
		t.Errorf("candidate[1] = %s (priority %v), want job_normal", candidates[1].job.ID, candidates[1].job.Priority)
	}
	if candidates[2].job.ID != "job_sub_low" {
		t.Errorf("candidate[2] = %s (priority %v), want job_sub_low", candidates[2].job.ID, candidates[2].job.Priority)
	}
}

// 6. provider unavailable: manual High still waits -> no circuit bypass
func TestStep5_6_ProviderUnavailable_ManualHighStillWaits_NoCircuitBypass(t *testing.T) {
	prov := newMockFallbackMediaProvider("youtube", nil)
	prov.searchErr = apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "sessions cooling", 2*time.Minute)
	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	manualOrigin := OriginManual
	job := Job{
		ID:            "job-manual-1",
		MediaProvider: "youtube",
		Priority:      PriorityHigh,
		Options:       Options{Origin: manualOrigin},
	}
	item := store.items["item-1"]
	item.Attempts = 0

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("status = %v, want retry_wait (no circuit bypass)", updated.Status)
	}
	if updated.ErrorCode != string(apperr.CodeSessionUnavailable) {
		t.Fatalf("errorCode = %v, want SESSION_UNAVAILABLE", updated.ErrorCode)
	}
	if updated.Attempts != 0 {
		t.Fatalf("attempts = %d, want preserved 0 (retry budget preserved)", updated.Attempts)
	}
	if updated.NextRetryAt == nil || updated.NextRetryAt.Before(time.Now().Add(time.Minute)) {
		t.Fatalf("NextRetryAt = %v, want >= T+1m", updated.NextRetryAt)
	}
}

// 7. Step 3: manual pre-routing behavior unchanged
func TestStep5_7_Step3_ManualPreRoutingBehaviorUnchanged(t *testing.T) {
	store := &atomicEnqueueStore{}
	m := newTestManagerForPriority(store)

	manualOrigin := OriginManual
	job, err := m.Enqueue(context.Background(), Request{
		Type:             TypeTrack,
		MetadataProvider: "ytmusic",
		TargetID:         "manual-track-preroute",
		Options: RequestOptions{
			Origin: &manualOrigin,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Must have OriginManual and PriorityHigh
	if job.Options.Origin != OriginManual {
		t.Errorf("Origin = %v, want %v", job.Options.Origin, OriginManual)
	}
	if job.Priority != PriorityHigh {
		t.Errorf("Priority = %v, want %v", job.Priority, PriorityHigh)
	}
}

// 8. Step 4: SESSION_UNAVAILABLE still renders/propagates unchanged
func TestStep5_8_Step4_SessionUnavailablePropagationUnchanged(t *testing.T) {
	items := []Item{
		{
			ID:           "item-1",
			Status:       ItemRetryWait,
			ErrorCode:    string(apperr.CodeSessionUnavailable),
			ErrorMessage: "Provider vorübergehend nicht verfügbar",
		},
	}

	status, errCode, errMsg := DeriveParentStatusDetails(items)
	if status != StatusRetryWait {
		t.Errorf("status = %v, want %v", status, StatusRetryWait)
	}
	if errCode != string(apperr.CodeSessionUnavailable) {
		t.Errorf("errCode = %v, want %v", errCode, apperr.CodeSessionUnavailable)
	}
	if errMsg != "Provider vorübergehend nicht verfügbar" {
		t.Errorf("errMsg = %v, want Provider vorübergehend nicht verfügbar", errMsg)
	}
}

// 9. existing priority enum/rank mapping unchanged
func TestStep5_9_PriorityEnumRankMappingUnchanged(t *testing.T) {
	if PriorityLow.Rank() != 0 {
		t.Errorf("PriorityLow.Rank() = %d, want 0", PriorityLow.Rank())
	}
	if PriorityNormal.Rank() != 1 {
		t.Errorf("PriorityNormal.Rank() = %d, want 1", PriorityNormal.Rank())
	}
	if PriorityHigh.Rank() != 2 {
		t.Errorf("PriorityHigh.Rank() = %d, want 2", PriorityHigh.Rank())
	}
	if PriorityVeryHigh.Rank() != 3 {
		t.Errorf("PriorityVeryHigh.Rank() = %d, want 3", PriorityVeryHigh.Rank())
	}
	if PriorityUrgent.Rank() != 3 {
		t.Errorf("PriorityUrgent.Rank() = %d, want 3", PriorityUrgent.Rank())
	}

	if PriorityFromRank(0) != PriorityLow {
		t.Errorf("PriorityFromRank(0) = %v, want PriorityLow", PriorityFromRank(0))
	}
	if PriorityFromRank(1) != PriorityNormal {
		t.Errorf("PriorityFromRank(1) = %v, want PriorityNormal", PriorityFromRank(1))
	}
	if PriorityFromRank(2) != PriorityHigh {
		t.Errorf("PriorityFromRank(2) = %v, want PriorityHigh", PriorityFromRank(2))
	}
	if PriorityFromRank(3) != PriorityVeryHigh {
		t.Errorf("PriorityFromRank(3) = %v, want PriorityVeryHigh", PriorityFromRank(3))
	}
}

// 10. no migration: schema remains 12 -> 12
func TestStep5_10_NoMigration(t *testing.T) {
	migrationsDir := filepath.Join("..", "database", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir migrations failed: %v", err)
	}

	maxMigration := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			parts := strings.Split(entry.Name(), "_")
			if len(parts) > 0 {
				var num int
				for _, ch := range parts[0] {
					if ch >= '0' && ch <= '9' {
						num = num*10 + int(ch-'0')
					}
				}
				if num > maxMigration {
					maxMigration = num
				}
			}
		}
	}

	if maxMigration != 12 {
		t.Errorf("latest migration = %d, want 12 (no new migration allowed)", maxMigration)
	}
}
