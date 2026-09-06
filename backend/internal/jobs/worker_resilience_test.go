package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/storage"
)

type fakeStore struct {
	Store
	items map[string]Item
}

func (s *fakeStore) UpdateItem(_ context.Context, id string, update ItemUpdate) error {
	it := s.items[id]
	if update.Status != "" {
		it.Status = update.Status
	}
	if update.Attempts != nil {
		it.Attempts = *update.Attempts
	}
	if update.MaxAttempts != nil {
		it.MaxAttempts = *update.MaxAttempts
	}
	if update.NextRetryAt != nil {
		it.NextRetryAt = update.NextRetryAt
	}
	if update.ClearNextRetry {
		it.NextRetryAt = nil
	}
	if update.ErrorCode != "" {
		it.ErrorCode = update.ErrorCode
	}
	if update.ErrorMessage != "" {
		it.ErrorMessage = update.ErrorMessage
	}
	if update.MediaID != "" {
		it.MediaID = update.MediaID
	}
	if update.MediaProvider != "" {
		it.MediaProvider = update.MediaProvider
	}
	if update.MediaURL != "" {
		it.MediaURL = update.MediaURL
	}
	if update.MatchScore != 0 {
		it.MatchScore = update.MatchScore
	}
	s.items[id] = it
	return nil
}

func TestWorker_StorageGuardWaitTransitionsWithoutRetryPenalty(t *testing.T) {
	root := t.TempDir()
	library, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}

	// Set required storage guard with non-existent marker
	guard := storage.NewStorageGuard(root, "storage-uuid-12345", 0)
	library.SetGuard(guard)

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
	}

	w := &worker{manager: m}
	job := Job{ID: "job-1", MediaProvider: "ytmusic"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemWaitingStorage {
		t.Fatalf("expected status %v, got %v", ItemWaitingStorage, updated.Status)
	}
	if updated.Attempts != 0 {
		t.Fatalf("expected 0 attempts spent on storage wait, got %d", updated.Attempts)
	}
	if updated.ErrorCode != string(apperr.CodeStorageGuardMismatch) {
		t.Fatalf("expected error code %v, got %v", apperr.CodeStorageGuardMismatch, updated.ErrorCode)
	}
}

func TestManager_QueuePausedControl(t *testing.T) {
	m := &Manager{}
	if m.QueuePaused() {
		t.Fatal("expected queue not paused by default")
	}

	m.SetQueuePaused(true)
	if !m.QueuePaused() {
		t.Fatal("expected queue paused after SetQueuePaused(true)")
	}

	m.SetQueuePaused(false)
	if !m.QueuePaused() == false {
		t.Fatal("expected queue resumed after SetQueuePaused(false)")
	}
}

func TestWorker_BackoffCalculation(t *testing.T) {
	for attempt := 1; attempt <= 5; attempt++ {
		backoff := calculateBackoff(attempt)
		if backoff <= 0 {
			t.Fatalf("attempt %d: invalid backoff %v", attempt, backoff)
		}
		if attempt == 1 && (backoff < 3*time.Second || backoff > 8*time.Second) {
			t.Fatalf("attempt 1 backoff out of expected range: %v", backoff)
		}
	}
}
