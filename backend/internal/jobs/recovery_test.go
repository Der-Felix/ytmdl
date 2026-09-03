package jobs

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
)

// recoveryStore is an in-memory Store that records what the recovery asked of
// it. Only the calls recover() makes are implemented; everything else would be
// a bug in the test.
type recoveryStore struct {
	Store

	mu         sync.Mutex
	unfinished []Job
	withItems  map[string]bool

	resetItems int
	resetJobs  int
	order      []string
}

func (s *recoveryStore) ResetInFlightItems(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetItems++
	s.order = append(s.order, "reset-items")
	return 3, nil
}

func (s *recoveryStore) ResetInterruptedJobs(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetJobs++
	s.order = append(s.order, "reset-jobs")
	return 2, nil
}

func (s *recoveryStore) ListUnfinished(context.Context) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "list-unfinished")
	return s.unfinished, nil
}

func (s *recoveryStore) HasItems(_ context.Context, jobID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withItems[jobID], nil
}

func newRecoveryManager(store Store) *Manager {
	m := &Manager{
		store:        store,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		resolveQueue: make(chan string, resolveQueueSize),
		wake:         make(chan struct{}, 1),
	}
	m.ctx, m.stop = context.WithCancel(context.Background())
	return m
}

// TestRecoverResetsBeforeItReadsTheQueue pins the order the recovery depends
// on: the interrupted work is put back into a startable state first, and only
// then is the queue read. Reading first would hand the dispatcher jobs whose
// items are still recorded as running.
func TestRecoverResetsBeforeItReadsTheQueue(t *testing.T) {
	store := &recoveryStore{withItems: map[string]bool{}}
	manager := newRecoveryManager(store)
	defer manager.stop()

	if err := manager.recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if store.resetItems != 1 || store.resetJobs != 1 {
		t.Fatalf("resets = %d items / %d jobs, want 1 / 1", store.resetItems, store.resetJobs)
	}
	want := []string{"reset-items", "reset-jobs", "list-unfinished"}
	if len(store.order) != len(want) {
		t.Fatalf("call order = %v, want %v", store.order, want)
	}
	for i, call := range want {
		if store.order[i] != call {
			t.Fatalf("call order = %v, want %v", store.order, want)
		}
	}
}

// TestRecoverRequeuesOnlyJobsWithoutItems covers the split the recovery makes:
// a job whose track list was never written has to be resolved again, while a
// job that already has items is left to the dispatcher so that no track is
// resolved — or downloaded — twice.
func TestRecoverRequeuesOnlyJobsWithoutItems(t *testing.T) {
	store := &recoveryStore{
		unfinished: []Job{
			{ID: "unresolved", Status: StatusQueued, Type: TypeArtist},
			{ID: "resolved", Status: StatusQueued, Type: TypeArtist, Total: 12},
		},
		withItems: map[string]bool{"resolved": true},
	}
	manager := newRecoveryManager(store)
	defer manager.stop()

	if err := manager.recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	var queued []string
	for {
		select {
		case id := <-manager.resolveQueue:
			queued = append(queued, id)
			continue
		default:
		}
		break
	}

	if len(queued) != 1 || queued[0] != "unresolved" {
		t.Fatalf("resolver queue = %v, want [unresolved]", queued)
	}

	// Both jobs must have a cancellation handle again, so that a cancel request
	// after the restart still reaches them.
	for _, id := range []string{"unresolved", "resolved"} {
		if _, ok := manager.runs.Load(id); !ok {
			t.Fatalf("job %q has no run handle after the recovery", id)
		}
	}

	// The dispatcher must have been woken, or the resumed job would wait for
	// the next tick of the safety net ticker.
	select {
	case <-manager.wake:
	default:
		t.Fatal("the dispatcher was not signalled after the recovery")
	}
}
