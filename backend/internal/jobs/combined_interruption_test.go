package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
	"ytdm/backend/internal/ytdlp"
)

// The tests below drive the real worker with the real downloader, an offline
// yt-dlp stand-in and an exclusive session slot. They pin how the track time
// limit, the combined transfer budget, the slot wait, cancellation, shutdown,
// the attempt directories and the staging lifecycle work together.

// sessionSlot is an exclusive execution slot that counts grants and releases.
type sessionSlot struct {
	sem chan struct{}

	mu       sync.Mutex
	granted  int
	released int
}

func newSessionSlot() *sessionSlot {
	s := &sessionSlot{sem: make(chan struct{}, 1)}
	s.sem <- struct{}{}
	return s
}

func (s *sessionSlot) Acquire(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.sem:
	}
	s.mu.Lock()
	s.granted++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.released++
			s.mu.Unlock()
			s.sem <- struct{}{}
		})
	}, nil
}

// occupy holds the slot for another download for d.
func (s *sessionSlot) occupy(t *testing.T, d time.Duration) {
	t.Helper()
	release, err := s.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(d, release)
}

// requireFree fails unless every grant was given back exactly once.
func (s *sessionSlot) requireFree(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	granted, released := s.granted, s.released
	s.mu.Unlock()
	if granted != released {
		t.Fatalf("slot granted %d times, released %d times", granted, released)
	}
}

// withSession gives every source the session the slot belongs to; the test
// orchestrator has no session pool of its own.
type withSession struct{ next downloader.Downloader }

func (d withSession) Download(ctx context.Context, source provider.MediaSource, destination string, progress downloader.ProgressCallback) (*downloader.Result, error) {
	source.SessionID = "sess-1"
	return d.next.Download(ctx, source, destination, progress)
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

// muxedFixture renders a three second combined audio/video file.
func muxedFixture(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "muxed.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "3", "-c:v", "mpeg4", "-c:a", "aac", "-b:a", "96k", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, b)
	}
	return out
}

type combinedEnv struct {
	mgr    *Manager
	store  *fakeStore
	item   Item
	slot   *sessionSlot
	marker string
}

// newCombinedEnv prepares one item whose only source is a combined stream,
// downloaded through the real downloader by a stub running script.
func newCombinedEnv(t *testing.T, script string, budget time.Duration) *combinedEnv {
	t.Helper()
	requireFFmpeg(t)
	marker := filepath.Join(t.TempDir(), "started")
	stub := filepath.Join(t.TempDir(), "yt-dlp")
	body := "#!/bin/sh\ntouch '" + marker + "'\nOUT=\"\"\nprev=\"\"\nfor arg do\n if [ \"$prev\" = \"-o\" ]; then OUT=\"${arg%/source.*}\"; fi\n prev=\"$arg\"\ndone\n" + script + "\n"
	if err := os.WriteFile(stub, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	slot := newSessionSlot()
	real, err := downloader.New(downloader.Options{
		YTDLP:                 ytdlp.New(ytdlp.Options{Binary: stub}),
		FFmpeg:                ffmpeg.New("ffmpeg", time.Minute),
		Prober:                downloader.NewProber(downloader.ProberOptions{Binary: "ffprobe", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
		CombinedAudioFallback: true,
		CombinedTimeout:       budget,
		ExecutionGateResolver: func(string) ytdlp.ExecutionGate { return slot },
		Logger:                slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	prov := newMockFallbackMediaProvider("youtube", timeoutCandidates())
	prov.resolveResults["c1"] = &provider.MediaSource{
		Provider: "youtube", ID: "c1", URL: "https://www.youtube.com/watch?v=c1", DurationMS: 3000,
		Formats: []provider.AudioFormat{{
			ID: "18", Codec: "mp4a.40.2", Combined: true, VideoCodec: "avc1.42001E",
			BitrateKbps: 96, TransferBitrateKbps: 500,
		}},
	}
	mgr, store := setupTestFallbackEnvironment(t, prov)
	mgr.downloader = withSession{next: real}

	item := store.items["item-1"]
	item.ID = music.NewID()
	delete(store.items, "item-1")
	store.items[item.ID] = item
	return &combinedEnv{mgr: mgr, store: store, item: item, slot: slot, marker: marker}
}

func (e *combinedEnv) started() bool {
	_, err := os.Stat(e.marker)
	return err == nil
}

func (e *combinedEnv) waitStarted(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !e.started() {
		if time.Now().After(deadline) {
			t.Fatal("yt-dlp did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *combinedEnv) itemDir(t *testing.T) string {
	t.Helper()
	dir, err := e.mgr.staging.ItemDir(e.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// stagingState reports whether the item's staging directory exists and
// whether it still holds an attempt directory.
func (e *combinedEnv) stagingState(t *testing.T) (exists bool, attempts []string) {
	t.Helper()
	entries, err := os.ReadDir(e.itemDir(t))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), storage.DownloadAttemptPrefix) {
			attempts = append(attempts, entry.Name())
		}
	}
	return true, attempts
}

func (e *combinedEnv) requireNoPause(t *testing.T) {
	t.Helper()
	if _, cooling := e.mgr.cooldown.Remaining("youtube"); cooling {
		t.Fatal("the YouTube family was paused")
	}
}

const stallingTransfer = "printf 'x' > \"$OUT/source.mp4.part\"\nsleep 30"

// The track time limit passes while the combined download waits for a busy
// slot, before its own transfer budget could even start: the item waits for a
// bounded retry, yt-dlp never ran, the staging is kept without an attempt
// directory, and nothing is paused.
func TestCombined_TrackTimeoutWhileWaitingForTheSlot(t *testing.T) {
	env := newCombinedEnv(t, "exit 0", 50*time.Millisecond)
	env.slot.occupy(t, 3*time.Second)

	runThroughStartWorker(t, env.mgr, env.item, 400*time.Millisecond, nil)

	got := env.store.items[env.item.ID]
	if got.Status != ItemRetryWait || got.ErrorCode != string(apperr.CodeTrackTimeout) || got.NextRetryAt == nil {
		t.Fatalf("status = %v, code = %s, next = %v; want a TRACK_TIMEOUT retry", got.Status, got.ErrorCode, got.NextRetryAt)
	}
	if env.started() {
		t.Fatal("yt-dlp ran without the slot")
	}
	if exists, attempts := env.stagingState(t); !exists || len(attempts) != 0 {
		t.Fatalf("staging exists = %v, attempt dirs = %v; want kept and clean", exists, attempts)
	}
	env.requireNoPause(t)
	time.Sleep(3 * time.Second) // the other holder gives the slot back
	env.slot.requireFree(t)
}

// The track time limit passes during a running combined transfer whose own
// budget is longer: it is the track timeout, not a budget stop.
func TestCombined_TrackTimeoutDuringTheTransfer(t *testing.T) {
	env := newCombinedEnv(t, stallingTransfer, time.Minute)

	runThroughStartWorker(t, env.mgr, env.item, 500*time.Millisecond, nil)

	got := env.store.items[env.item.ID]
	if got.Status != ItemRetryWait || got.ErrorCode != string(apperr.CodeTrackTimeout) {
		t.Fatalf("status = %v, code = %s; want a TRACK_TIMEOUT retry", got.Status, got.ErrorCode)
	}
	if exists, attempts := env.stagingState(t); !exists || len(attempts) != 0 {
		t.Fatalf("staging exists = %v, attempt dirs = %v; want kept and clean", exists, attempts)
	}
	env.requireNoPause(t)
	env.slot.requireFree(t)
}

// The combined budget ends a stalled transfer before the track time limit:
// a permanent budget stop, the staging removed, nothing paused.
func TestCombined_BudgetStopBeforeTheTrackTimeout(t *testing.T) {
	env := newCombinedEnv(t, stallingTransfer, 300*time.Millisecond)

	runThroughStartWorker(t, env.mgr, env.item, time.Minute, nil)

	got := env.store.items[env.item.ID]
	if got.Status != ItemFailed || got.ErrorCode != string(apperr.CodeTransferBudgetExceeded) || got.NextRetryAt != nil {
		t.Fatalf("status = %v, code = %s, next = %v; want a final budget stop", got.Status, got.ErrorCode, got.NextRetryAt)
	}
	if exists, _ := env.stagingState(t); exists {
		t.Fatal("the staging of a finally failed item was kept")
	}
	env.requireNoPause(t)
	env.slot.requireFree(t)
}

// Waiting for the slot longer than the combined budget, but within the track
// time limit, still completes: the budget starts with the slot.
func TestCombined_LongSlotWaitWithinTheTrackTimeoutCompletes(t *testing.T) {
	env := newCombinedEnv(t, "cp '"+muxedFixture(t)+"' \"$OUT/source.mp4\"", 400*time.Millisecond)
	env.slot.occupy(t, 1200*time.Millisecond)

	runThroughStartWorker(t, env.mgr, env.item, time.Minute, nil)

	got := env.store.items[env.item.ID]
	if got.Status != ItemCompleted {
		t.Fatalf("status = %v, code = %s, message = %s", got.Status, got.ErrorCode, got.ErrorMessage)
	}
	if exists, _ := env.stagingState(t); exists {
		t.Fatal("the staging of a completed item was kept")
	}
	env.slot.requireFree(t)
}

// A user cancellation during the transfer: cancelled, not retried, staging
// removed, partial download counted only while it runs, nothing paused.
func TestCombined_UserCancelDuringTheTransfer(t *testing.T) {
	env := newCombinedEnv(t, stallingTransfer, time.Minute)

	var partialsWhileRunning int
	runThroughStartWorker(t, env.mgr, env.item, time.Minute, func() {
		env.waitStarted(t)
		time.Sleep(100 * time.Millisecond)
		partialsWhileRunning, _ = env.mgr.staging.CountPartials()
		env.mgr.cancelRun("job-1")
	})

	got := env.store.items[env.item.ID]
	if got.Status != ItemCancelled || got.NextRetryAt != nil {
		t.Fatalf("status = %v, next = %v; want cancelled", got.Status, got.NextRetryAt)
	}
	if partialsWhileRunning != 1 {
		t.Fatalf("partials while running = %d, want 1", partialsWhileRunning)
	}
	if n, _ := env.mgr.staging.CountPartials(); n != 0 {
		t.Fatalf("partials after cancellation = %d, want 0", n)
	}
	if exists, _ := env.stagingState(t); exists {
		t.Fatal("the staging of a cancelled item was kept")
	}
	env.requireNoPause(t)
	env.slot.requireFree(t)
}

// A service shutdown during the transfer is neither a cancellation nor a
// timeout: the item stays in its working state, its staging stays without an
// attempt directory, and after recovery the start-up pruning keeps it.
func TestCombined_ShutdownDuringTheTransfer(t *testing.T) {
	env := newCombinedEnv(t, stallingTransfer, time.Minute)
	env.mgr.ctx, env.mgr.stop = context.WithCancel(context.Background())

	runThroughStartWorker(t, env.mgr, env.item, time.Minute, func() {
		env.waitStarted(t)
		env.mgr.BeginShutdown()
		env.mgr.stopping.Store(true)
		env.mgr.stop()
	})

	got := env.store.items[env.item.ID]
	if got.Status != ItemDownloading || got.NextRetryAt != nil {
		t.Fatalf("status = %v, next = %v; want the working state kept for recovery", got.Status, got.NextRetryAt)
	}
	if exists, attempts := env.stagingState(t); !exists || len(attempts) != 0 {
		t.Fatalf("staging exists = %v, attempt dirs = %v; want kept and clean", exists, attempts)
	}
	env.slot.requireFree(t)

	// The next process recovers the item and prunes staging before workers.
	store := &pruneStore{statuses: map[string]ItemStatus{env.item.ID: ItemPending}}
	next := newPruneManager(store, env.mgr.staging)
	defer next.stop()
	next.pruneStaging(context.Background())
	if exists, _ := env.stagingState(t); !exists {
		t.Fatal("the pruning removed the staging of recovered work")
	}
}

// A track time limit on the last allowed attempt fails the item for good and
// removes its staging: the retry stays bounded.
func TestCombined_TrackTimeoutOnTheLastAttempt(t *testing.T) {
	env := newCombinedEnv(t, stallingTransfer, time.Minute)
	env.item.Attempts = env.item.MaxAttempts - 1
	env.store.items[env.item.ID] = env.item

	runThroughStartWorker(t, env.mgr, env.item, 400*time.Millisecond, nil)

	got := env.store.items[env.item.ID]
	if got.Status != ItemFailed || got.ErrorCode != string(apperr.CodeTrackTimeout) || got.NextRetryAt != nil {
		t.Fatalf("status = %v, code = %s, next = %v; want a final TRACK_TIMEOUT", got.Status, got.ErrorCode, got.NextRetryAt)
	}
	if exists, _ := env.stagingState(t); exists {
		t.Fatal("the staging of a finally failed item was kept")
	}
	env.slot.requireFree(t)
}

// An older audio file in the item's staging and a clean yt-dlp exit without
// output never complete the item.
func TestCombined_OldStagedAudioIsNotAdopted(t *testing.T) {
	env := newCombinedEnv(t, "exit 0", time.Minute)
	dir, err := env.mgr.staging.EnsureItemDir(env.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture := muxedFixture(t)
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source.mp4", "The Visitors.m4a"} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	runThroughStartWorker(t, env.mgr, env.item, time.Minute, nil)

	got := env.store.items[env.item.ID]
	if got.Status == ItemCompleted {
		t.Fatal("an older staged file completed the item")
	}
	if got.ErrorCode != string(apperr.CodeDownloadFailed) {
		t.Fatalf("code = %s, want DOWNLOAD_FAILED", got.ErrorCode)
	}
	env.slot.requireFree(t)
}

// Staging quota and partial counting see what a running attempt holds, and
// the start-up pruning treats attempt directories as part of their item: gone
// with a finished item, kept with one that is still retryable.
func TestCombined_AttemptDirectoriesInQuotaAndPruning(t *testing.T) {
	root := t.TempDir()
	stg, err := storage.NewStagingManager(root, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	finished, retryable := music.NewID(), music.NewID()
	for _, id := range []string{finished, retryable} {
		dir, err := stg.EnsureItemDir(id)
		if err != nil {
			t.Fatal(err)
		}
		attempt := filepath.Join(dir, storage.DownloadAttemptPrefix+"interrupted")
		if err := os.MkdirAll(attempt, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(attempt, "source.mp4.part"), make([]byte, 800), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if used, _ := stg.UsedBytes(); used != 1600 {
		t.Fatalf("used bytes = %d, want the attempt files counted", used)
	}
	if err := stg.CheckSpace(); apperr.CodeOf(err) != apperr.CodeStagingLowSpace {
		t.Fatalf("CheckSpace = %v, want the quota enforced over attempt files", err)
	}
	if n, _ := stg.CountPartials(); n != 2 {
		t.Fatalf("partials = %d, want 2", n)
	}

	store := &pruneStore{statuses: map[string]ItemStatus{finished: ItemFailed, retryable: ItemRetryWait}}
	m := newPruneManager(store, stg)
	defer m.stop()
	m.pruneStaging(context.Background())

	if _, err := os.Lstat(filepath.Join(root, finished)); !os.IsNotExist(err) {
		t.Fatalf("the finished item's staging and attempt directory were kept: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, retryable, storage.DownloadAttemptPrefix+"interrupted")); err != nil {
		t.Fatalf("the retryable item's staging was pruned: %v", err)
	}
	if used, _ := stg.UsedBytes(); used != 800 {
		t.Fatalf("used bytes after pruning = %d, want 800", used)
	}
	if err := stg.CheckSpace(); err != nil {
		t.Fatalf("CheckSpace after pruning = %v", err)
	}
}
