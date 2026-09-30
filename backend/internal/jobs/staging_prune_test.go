package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// pruneStore is a recovery store that also answers item states, and records
// the order in which the start-up asked it.
type pruneStore struct {
	recoveryStore

	statuses  map[string]ItemStatus
	statusErr error
	inFlight  map[string]bool
}

func (s *pruneStore) ResetInFlightItems(ctx context.Context) (int, error) {
	n, err := s.recoveryStore.ResetInFlightItems(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.inFlight {
		s.statuses[id] = ItemPending
	}
	return n, err
}

func (s *pruneStore) ItemStatuses(_ context.Context, ids []string) (map[string]ItemStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "item-statuses")
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	out := make(map[string]ItemStatus)
	for _, id := range ids {
		if st, ok := s.statuses[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

type stagingFixture struct {
	root    string
	staging *storage.StagingManager
	ids     map[string]string // label -> item id
}

// newStagingFixture creates one item directory with a file per label.
func newStagingFixture(t *testing.T, labels ...string) *stagingFixture {
	t.Helper()
	root := t.TempDir()
	stg, err := storage.NewStagingManager(root, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := &stagingFixture{root: root, staging: stg, ids: map[string]string{}}
	for _, label := range labels {
		id := music.NewID()
		dir, err := stg.EnsureItemDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "source.webm.part"), []byte(label), 0o644); err != nil {
			t.Fatal(err)
		}
		f.ids[label] = id
	}
	return f
}

func (f *stagingFixture) exists(t *testing.T, name string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(f.root, name))
	return err == nil
}

func newPruneManager(store Store, stg *storage.StagingManager) *Manager {
	m := newRecoveryManager(store)
	m.staging = stg
	return m
}

// Only the staging of items in a final state is removed. Active, waiting and
// retryable items keep theirs, and so does every entry whose item is unknown
// or that is not an item directory at all.
func TestPruneStagingRemovesOnlyFinishedItems(t *testing.T) {
	f := newStagingFixture(t, "completed", "failed", "skipped", "cancelled",
		"pending", "retry_wait", "downloading", "waiting_space", "unknown")
	statuses := map[string]ItemStatus{}
	for label, id := range f.ids {
		if label != "unknown" {
			statuses[id] = ItemStatus(label)
		}
	}
	statuses[f.ids["waiting_space"]] = ItemWaitingSpace

	// Entries that are not item directories of a known shape.
	outside := t.TempDir()
	finishedLinkID := music.NewID()
	statuses[finishedLinkID] = ItemFailed
	if err := os.Symlink(outside, filepath.Join(f.root, finishedLinkID)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.root, "lost+found"), 0o755); err != nil {
		t.Fatal(err)
	}
	upperID := "ABCDEF0123456789ABCDEF0123456789"
	statuses[upperID] = ItemFailed
	if err := os.MkdirAll(filepath.Join(f.root, upperID), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newPruneManager(&pruneStore{statuses: statuses}, f.staging)
	defer m.stop()
	m.pruneStaging(context.Background())

	for label, id := range f.ids {
		gone := !f.exists(t, id)
		want := ItemStatus(label).Terminal()
		if gone != want {
			t.Errorf("%s: removed = %v, want %v", label, gone, want)
		}
	}
	for _, kept := range []string{finishedLinkID, "lost+found", upperID} {
		if !f.exists(t, kept) {
			t.Errorf("%s was removed", kept)
		}
	}
}

// If the item states cannot be read for every candidate, nothing is removed.
// A store that cannot answer at all leaves staging alone as well.
func TestPruneStagingRemovesNothingWhenTheStateIsUnclear(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store Store
	}{
		{"query fails", &pruneStore{statusErr: errors.New("connection reset")}},
		{"store cannot answer", &recoveryStore{withItems: map[string]bool{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStagingFixture(t, "completed", "failed")
			if ps, ok := tc.store.(*pruneStore); ok {
				ps.statuses = map[string]ItemStatus{
					f.ids["completed"]: ItemCompleted, f.ids["failed"]: ItemFailed,
				}
			}
			m := newPruneManager(tc.store, f.staging)
			defer m.stop()
			m.pruneStaging(context.Background())
			for label, id := range f.ids {
				if !f.exists(t, id) {
					t.Errorf("%s was removed although the state was unclear", label)
				}
			}
		})
	}
}

// Many directories are judged in bounded batches; a failure in a later batch
// still removes nothing.
func TestPruneStagingAllOrNothingAcrossBatches(t *testing.T) {
	labels := make([]string, stagingPruneBatch+3)
	for i := range labels {
		labels[i] = "item-" + time.Duration(i).String()
	}
	f := newStagingFixture(t, labels...)
	statuses := map[string]ItemStatus{}
	for _, id := range f.ids {
		statuses[id] = ItemFailed
	}
	store := &failingSecondBatch{pruneStore: pruneStore{statuses: statuses}}
	m := newPruneManager(store, f.staging)
	defer m.stop()
	m.pruneStaging(context.Background())
	for _, id := range f.ids {
		if !f.exists(t, id) {
			t.Fatal("a directory was removed although a later batch failed")
		}
	}
	if store.calls != 2 {
		t.Fatalf("status queries = %d, want 2", store.calls)
	}
}

type failingSecondBatch struct {
	pruneStore
	calls int
}

func (s *failingSecondBatch) ItemStatuses(ctx context.Context, ids []string) (map[string]ItemStatus, error) {
	s.calls++
	if len(ids) > stagingPruneBatch {
		return nil, errors.New("batch too large")
	}
	if s.calls == 2 {
		return nil, errors.New("connection reset")
	}
	return s.pruneStore.ItemStatuses(ctx, ids)
}

// Start prunes only after recovery has returned interrupted work to the
// queue, and before any worker runs: an item a crashed process left
// "downloading" is pending again by then and keeps its partial download.
func TestStartPrunesAfterRecoveryAndBeforeWorkers(t *testing.T) {
	f := newStagingFixture(t, "interrupted", "finished")
	store := &pruneStore{
		recoveryStore: recoveryStore{withItems: map[string]bool{}},
		statuses: map[string]ItemStatus{
			f.ids["interrupted"]: ItemDownloading,
			f.ids["finished"]:    ItemFailed,
		},
		inFlight: map[string]bool{f.ids["interrupted"]: true},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Manager{
		store:        store,
		staging:      f.staging,
		logger:       logger,
		broker:       NewBroker(logger),
		resolveQueue: make(chan string, resolveQueueSize),
		wake:         make(chan struct{}, 1),
		concurrency:  1,
		trackTimeout: time.Minute,
	}
	m.queuePaused.Store(true) // no dispatching; only the start-up order is under test
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.stopping.Store(true)
	m.stop()
	m.wg.Wait()

	store.mu.Lock()
	order := append([]string(nil), store.order...)
	store.mu.Unlock()
	want := []string{"reset-items", "reset-jobs", "list-unfinished", "item-statuses"}
	if len(order) < len(want) {
		t.Fatalf("order = %v", order)
	}
	for i, step := range want {
		if order[i] != step {
			t.Fatalf("order = %v, want it to start with %v", order, want)
		}
	}
	if !f.exists(t, f.ids["interrupted"]) {
		t.Fatal("the staging of recovered work was removed")
	}
	if f.exists(t, f.ids["finished"]) {
		t.Fatal("the staging of a finally failed item was kept")
	}
}

// failingStore cannot store a final item state; every other update works.
type failingStore struct {
	*fakeStore
}

func (s *failingStore) UpdateItem(ctx context.Context, id string, update ItemUpdate) error {
	if update.Status.Terminal() {
		return errors.New("database unavailable")
	}
	return s.fakeStore.UpdateItem(ctx, id, update)
}

// The end of an item decides about its staging: a final failure and a
// cancellation remove it, a scheduled retry keeps it - the next attempt may
// continue the partial download - and a final state that could not be stored
// keeps it for the next process to judge.
func TestWorkerStagingFollowsTheStoredOutcome(t *testing.T) {
	permanent := apperr.New(apperr.CodeTrackNotFound, "gone")
	retryable := apperr.New(apperr.CodeDownloadFailed, "connection dropped")
	for _, tc := range []struct {
		name      string
		err       error
		attempts  int
		breakDB   bool
		wantState ItemStatus
		wantKept  bool
	}{
		{"final failure", permanent, 0, false, ItemFailed, false},
		{"retries exhausted", retryable, 4, false, ItemFailed, false},
		{"retry scheduled", retryable, 0, false, ItemRetryWait, true},
		{"final failure not stored", permanent, 0, true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", timeoutCandidates()))
			mgr.downloader = &partialThenFail{staging: mgr.staging, err: tc.err}
			item := store.items["item-1"]
			item.ID = music.NewID()
			item.Attempts = tc.attempts
			delete(store.items, "item-1")
			store.items[item.ID] = item
			if tc.breakDB {
				mgr.store = &failingStore{fakeStore: store}
			}

			(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, item)

			if tc.wantState != "" && store.items[item.ID].Status != tc.wantState {
				t.Fatalf("status = %v, want %v", store.items[item.ID].Status, tc.wantState)
			}
			dir, _ := mgr.staging.ItemDir(item.ID)
			_, err := os.Stat(filepath.Join(dir, "source.webm.part"))
			if kept := err == nil; kept != tc.wantKept {
				t.Fatalf("staging kept = %v, want %v", kept, tc.wantKept)
			}
		})
	}
}

// A cancelled item's staging is removed as well.
func TestWorkerStagingIsRemovedOnCancellation(t *testing.T) {
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", timeoutCandidates()))
	dl := &stallingDownloader{}
	mgr.downloader = &partialThen{staging: mgr.staging, next: dl}
	item := store.items["item-1"]
	item.ID = music.NewID()
	delete(store.items, "item-1")
	store.items[item.ID] = item

	runThroughStartWorker(t, mgr, item, time.Minute, func() {
		deadline := time.Now().Add(5 * time.Second)
		for dl.calls.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		mgr.cancelRun("job-1")
	})

	if store.items[item.ID].Status != ItemCancelled {
		t.Fatalf("status = %v", store.items[item.ID].Status)
	}
	dir, _ := mgr.staging.ItemDir(item.ID)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the staging of a cancelled item was kept: %v", err)
	}
}

// partialThenFail writes a partial download into the item's staging and then
// fails with err.
type partialThenFail struct {
	staging *storage.StagingManager
	err     error
}

func (d *partialThenFail) Download(_ context.Context, _ provider.MediaSource, destination string, _ downloader.ProgressCallback) (*downloader.Result, error) {
	if err := os.WriteFile(filepath.Join(filepath.Dir(destination), "source.webm.part"), []byte("partial"), 0o644); err != nil {
		return nil, err
	}
	return nil, d.err
}

// partialThen writes a partial download and hands over to next.
type partialThen struct {
	staging *storage.StagingManager
	next    downloader.Downloader
}

func (d *partialThen) Download(ctx context.Context, source provider.MediaSource, destination string, progress downloader.ProgressCallback) (*downloader.Result, error) {
	if err := os.WriteFile(filepath.Join(filepath.Dir(destination), "source.webm.part"), []byte("partial"), 0o644); err != nil {
		return nil, err
	}
	return d.next.Download(ctx, source, destination, progress)
}
