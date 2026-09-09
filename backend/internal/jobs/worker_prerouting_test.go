package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

type mockOrchestratorForOrigin struct {
	lastOrigin orchestrator.Origin
	returnErr  error
}

func (m *mockOrchestratorForOrigin) ResolveMedia(ctx context.Context, _ string, _ music.Track, _ int) (*orchestrator.ResolvedMedia, error) {
	m.lastOrigin = orchestrator.OriginFromContext(ctx)
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return nil, apperr.New(apperr.CodeTrackNotFound, "not found")
}

func (m *mockOrchestratorForOrigin) ResolveCookiePath(_ string) string { return "" }

func (m *mockOrchestratorForOrigin) RecordDownloadOutcome(_ context.Context, _ string, _ error) {}

func TestWorker_OriginPropagation_Manual(t *testing.T) {
	root := t.TempDir()
	library, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}

	stagingDir := t.TempDir()
	stagingMgr, err := storage.NewStagingManager(stagingDir, 0, 0)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	store := &fakeStore{
		items: map[string]Item{
			"item-1": {
				ID:          "item-1",
				JobID:       "job-1",
				Status:      ItemPending,
				Attempts:    0,
				MaxAttempts: 5,
				Track:       aWorkerTrack(),
				Label:       "Artist - Song",
			},
		},
	}

	mockOrch := &mockOrchestratorForOrigin{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := NewBroker(logger)
	m := &Manager{
		store:        store,
		library:      library,
		staging:      stagingMgr,
		broker:       broker,
		cooldown:     NewMediaCooldownManager(),
		logger:       logger,
		finalizerSem: make(chan struct{}, 1),
		orchestrator: mockOrch,
	}

	w := &worker{manager: m}
	job := Job{
		ID:            "job-1",
		MediaProvider: "ytmusic",
		Options: Options{
			Origin: OriginManual,
		},
	}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	if mockOrch.lastOrigin != orchestrator.OriginManual {
		t.Fatalf("expected orchestrator origin = %q, got %q", orchestrator.OriginManual, mockOrch.lastOrigin)
	}
}

func TestWorker_OriginPropagation_Subscription(t *testing.T) {
	root := t.TempDir()
	library, _ := storage.NewLibrary(root)
	stagingDir := t.TempDir()
	stagingMgr, _ := storage.NewStagingManager(stagingDir, 0, 0)

	store := &fakeStore{
		items: map[string]Item{
			"item-1": {
				ID:          "item-1",
				JobID:       "job-1",
				Status:      ItemPending,
				Attempts:    0,
				MaxAttempts: 5,
				Track:       aWorkerTrack(),
				Label:       "Artist - Song",
			},
		},
	}

	mockOrch := &mockOrchestratorForOrigin{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := NewBroker(logger)
	m := &Manager{
		store:        store,
		library:      library,
		staging:      stagingMgr,
		broker:       broker,
		cooldown:     NewMediaCooldownManager(),
		logger:       logger,
		finalizerSem: make(chan struct{}, 1),
		orchestrator: mockOrch,
	}

	w := &worker{manager: m}
	job := Job{
		ID:            "job-1",
		MediaProvider: "ytmusic",
		Options: Options{
			Origin: OriginSubscription,
		},
	}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	if mockOrch.lastOrigin != orchestrator.OriginSubscription {
		t.Fatalf("expected orchestrator origin = %q, got %q", orchestrator.OriginSubscription, mockOrch.lastOrigin)
	}
}

func TestWorker_OriginPropagation_EmptyDefaultsToSubscription(t *testing.T) {
	root := t.TempDir()
	library, _ := storage.NewLibrary(root)
	stagingDir := t.TempDir()
	stagingMgr, _ := storage.NewStagingManager(stagingDir, 0, 0)

	store := &fakeStore{
		items: map[string]Item{
			"item-1": {
				ID:          "item-1",
				JobID:       "job-1",
				Status:      ItemPending,
				Attempts:    0,
				MaxAttempts: 5,
				Track:       aWorkerTrack(),
				Label:       "Artist - Song",
			},
		},
	}

	mockOrch := &mockOrchestratorForOrigin{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := NewBroker(logger)
	m := &Manager{
		store:        store,
		library:      library,
		staging:      stagingMgr,
		broker:       broker,
		cooldown:     NewMediaCooldownManager(),
		logger:       logger,
		finalizerSem: make(chan struct{}, 1),
		orchestrator: mockOrch,
	}

	w := &worker{manager: m}
	job := Job{
		ID:            "job-1",
		MediaProvider: "ytmusic",
		Options: Options{
			Origin: "", // Legacy job without origin
		},
	}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	if mockOrch.lastOrigin != orchestrator.OriginSubscription {
		t.Fatalf("expected empty origin to default to %q, got %q", orchestrator.OriginSubscription, mockOrch.lastOrigin)
	}
}

func TestWorker_SessionUnavailable_RetryWait_PreservesAttempts(t *testing.T) {
	root := t.TempDir()
	library, _ := storage.NewLibrary(root)
	stagingDir := t.TempDir()
	stagingMgr, _ := storage.NewStagingManager(stagingDir, 0, 0)

	store := &fakeStore{
		items: map[string]Item{
			"item-1": {
				ID:          "item-1",
				JobID:       "job-1",
				Status:      ItemPending,
				Attempts:    0,
				MaxAttempts: 5,
				Track:       aWorkerTrack(),
				Label:       "Artist - Song",
			},
		},
	}

	sessionErr := apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "waiting for session", 15*time.Minute)
	mockOrch := &mockOrchestratorForOrigin{returnErr: sessionErr}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := NewBroker(logger)
	m := &Manager{
		store:        store,
		library:      library,
		staging:      stagingMgr,
		broker:       broker,
		cooldown:     NewMediaCooldownManager(),
		logger:       logger,
		finalizerSem: make(chan struct{}, 1),
		orchestrator: mockOrch,
	}

	w := &worker{manager: m}
	job := Job{
		ID:            "job-1",
		MediaProvider: "ytmusic",
		Options: Options{
			Origin: OriginSubscription,
		},
	}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("expected status ItemRetryWait, got %s", updated.Status)
	}
	if updated.Attempts != 0 {
		t.Fatalf("expected attempts to remain 0 (preserved retry budget), got %d", updated.Attempts)
	}
	if updated.NextRetryAt == nil {
		t.Fatal("expected NextRetryAt to be set")
	}
}

func TestManager_EnqueueOriginPersistence(t *testing.T) {
	store := &atomicEnqueueStore{}
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

	// Manual request with OriginManual
	manualOrigin := OriginManual
	jobManual, err := m.Enqueue(context.Background(), Request{
		Type:             TypeArtist,
		MetadataProvider: "ytmusic",
		TargetID:         "artist-123",
		Options: RequestOptions{
			Origin: &manualOrigin,
		},
	})
	if err != nil {
		t.Fatalf("Enqueue manual failed: %v", err)
	}
	if jobManual.Options.Origin != OriginManual {
		t.Errorf("jobManual.Options.Origin = %q, want %q", jobManual.Options.Origin, OriginManual)
	}

	// Subscription enqueue
	queued, err := m.EnqueueReleaseWithPriority(context.Background(), "ytmusic", "release-sub-1", "Artist", PriorityNormal)
	if err != nil || !queued {
		t.Fatalf("EnqueueReleaseWithPriority failed: queued=%v, err=%v", queued, err)
	}

	store.mu.Lock()
	subJob := store.jobs[len(store.jobs)-1]
	store.mu.Unlock()

	if subJob.Options.Origin != OriginSubscription {
		t.Errorf("subJob.Options.Origin = %q, want %q", subJob.Options.Origin, OriginSubscription)
	}
}
