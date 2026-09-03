package jobs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"ytdm/backend/internal/apperr"
)

func TestManagerRejectsEnqueueAfterBeginShutdown(t *testing.T) {
	manager := &Manager{}
	manager.accepting.Store(true)

	manager.BeginShutdown()
	manager.BeginShutdown() // Shutdown admission is idempotent.

	_, err := manager.Enqueue(context.Background(), Request{})
	if code := apperr.CodeOf(err); code != apperr.CodeShuttingDown {
		t.Fatalf("Enqueue error code = %q, want %q", code, apperr.CodeShuttingDown)
	}
	if status := apperr.HTTPStatus(apperr.CodeOf(err)); status != http.StatusServiceUnavailable {
		t.Fatalf("Enqueue HTTP status = %d, want %d", status, http.StatusServiceUnavailable)
	}
}

type shutdownStore struct {
	Store
	resetItemCalls int
	resetJobCalls  int
}

func (s *shutdownStore) ResetInFlightItems(context.Context) (int, error) {
	s.resetItemCalls++
	return 2, nil
}

func (s *shutdownStore) ResetInterruptedJobs(context.Context) (int, error) {
	s.resetJobCalls++
	return 1, nil
}

func TestManagerStopPreservesInterruptedWorkForRestart(t *testing.T) {
	store := &shutdownStore{}
	manager := &Manager{
		store:  store,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	manager.accepting.Store(true)

	manager.Stop()
	manager.Stop() // Worker shutdown and queue reset are idempotent.

	if store.resetItemCalls != 1 {
		t.Fatalf("ResetInFlightItems calls = %d, want 1", store.resetItemCalls)
	}
	if store.resetJobCalls != 1 {
		t.Fatalf("ResetInterruptedJobs calls = %d, want 1", store.resetJobCalls)
	}
	if !manager.Stopping() {
		t.Fatal("manager is not marked as stopping")
	}
	if manager.accepting.Load() {
		t.Fatal("manager still accepts jobs after Stop")
	}
}
