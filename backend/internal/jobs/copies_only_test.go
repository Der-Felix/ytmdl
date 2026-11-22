package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// refusingOrchestrator and refusingDownloader stand for the media side of the
// pipeline. A copies-only job must never reach either of them.
type refusingOrchestrator struct{ calls int }

func (o *refusingOrchestrator) ResolveMedia(context.Context, string, music.Track, int) (*orchestrator.ResolvedMedia, error) {
	o.calls++
	return nil, apperr.New(apperr.CodeInternal, "a copies-only item resolved media")
}
func (o *refusingOrchestrator) ResolveCookiePath(string) string                      { return "" }
func (o *refusingOrchestrator) RecordDownloadOutcome(context.Context, string, error) {}

type refusingDownloader struct{ calls int }

func (d *refusingDownloader) Download(context.Context, provider.MediaSource, string, downloader.ProgressCallback) (*downloader.Result, error) {
	d.calls++
	return nil, apperr.New(apperr.CodeInternal, "a copies-only item downloaded")
}

// copiesOnly is the copy harness with the media side replaced by fakes that
// count every call.
func copiesOnly(t *testing.T) (*copyHarness, *refusingOrchestrator, *refusingDownloader) {
	t.Helper()
	h := newCopyHarness(t)
	orch := &refusingOrchestrator{}
	dl := &refusingDownloader{}
	h.manager.orchestrator = orch
	h.manager.downloader = dl
	return h, orch, dl
}

// runCopiesOnly processes one item the way a backfill job does.
func (h *copyHarness) runCopiesOnly(t *testing.T, track music.Track) Item {
	t.Helper()
	item := Item{ID: "item-1", JobID: "job-1", Track: track, Label: track.Label(), MaxAttempts: 3}
	h.store.items[item.ID] = item
	job := Job{ID: "job-1", Type: TypeRelease, Options: Options{CopiesOnly: true, SkipExisting: true}}
	(&worker{manager: h.manager}).process(context.Background(), job, item)
	return h.store.items[item.ID]
}

func assertNoMedia(t *testing.T, orch *refusingOrchestrator, dl *refusingDownloader) {
	t.Helper()
	if orch.calls != 0 || dl.calls != 0 {
		t.Fatalf("a copies-only item reached the media side: %d resolutions, %d downloads", orch.calls, dl.calls)
	}
}

func TestCopiesOnlySkipsARecordingTheLibraryDoesNotHold(t *testing.T) {
	h, orch, dl := copiesOnly(t)
	h.manager.catalog = &fakeCatalog{}

	item := h.runCopiesOnly(t, boxSetTrack())

	if item.Status != ItemSkipped || item.ErrorCode != string(apperr.CodeTrackNotFound) {
		t.Fatalf("item = %s / %s, want skipped with TRACK_NOT_FOUND", item.Status, item.ErrorCode)
	}
	if !strings.Contains(item.ErrorMessage, "not in the library") {
		t.Fatalf("message = %q", item.ErrorMessage)
	}
	if item.Attempts != 0 || item.NextRetryAt != nil {
		t.Fatalf("the skip spent an attempt or scheduled a retry: attempts %d, next %v", item.Attempts, item.NextRetryAt)
	}
	assertNoMedia(t, orch, dl)
	h.assertNoCopy(t)
}

func TestCopiesOnlySkipsARecordingWhoseFileIsGone(t *testing.T) {
	h, orch, dl := copiesOnly(t)
	if err := os.Remove(filepath.Join(h.root, filepath.FromSlash(homeRel))); err != nil {
		t.Fatal(err)
	}

	item := h.runCopiesOnly(t, boxSetTrack())

	if item.Status != ItemSkipped || item.ErrorCode != string(apperr.CodeTrackNotFound) {
		t.Fatalf("item = %s / %s, want skipped with TRACK_NOT_FOUND", item.Status, item.ErrorCode)
	}
	assertNoMedia(t, orch, dl)
}

func TestCopiesOnlyPlacesACopyEvenWhenCompletingReleasesIsOff(t *testing.T) {
	h, orch, dl := copiesOnly(t)
	h.manager.SetCompleteReleases(false)

	item := h.runCopiesOnly(t, boxSetTrack())

	if item.Status != ItemCompleted {
		t.Fatalf("item = %s / %s (%s), want completed", item.Status, item.ErrorCode, item.ErrorMessage)
	}
	if got := h.read(t, boxSetRel); got != homeAudio+"|tagged:Thank You For The Music" {
		t.Fatalf("copy = %q", got)
	}
	if len(h.placements.persisted) != 1 {
		t.Fatalf("persisted %d placements, want 1", len(h.placements.persisted))
	}
	assertNoMedia(t, orch, dl)

	// Run again, the job is idempotent: the placement is found and skipped.
	assertSkipped(t, h.runCopiesOnly(t, boxSetTrack()))
	if len(h.placements.persisted) != 1 {
		t.Fatal("a second run placed a second copy")
	}
}

func TestCopiesOnlySkipsARecordingTheReleaseHolds(t *testing.T) {
	h, orch, dl := copiesOnly(t)
	track := boxSetTrack()
	track.Album, track.ReleaseType, track.Year, track.ReleaseID = "Arrival", music.ReleaseAlbum, 1976, "rel-arrival"

	assertSkipped(t, h.runCopiesOnly(t, track))
	h.assertNoCopy(t)
	assertNoMedia(t, orch, dl)
}

func TestCopiesOnlyWaitsForAnUnreachableLibrary(t *testing.T) {
	h, orch, dl := copiesOnly(t)
	// Unreachable, the library would look as if it held nothing; the item
	// must wait instead of being skipped as unknown.
	h.manager.library.SetGuard(storage.NewStorageGuard(h.root, "expected-guard-id", 0))

	item := h.runCopiesOnly(t, boxSetTrack())

	if item.Status != ItemWaitingStorage || item.Attempts != 0 {
		t.Fatalf("item = %s / %s, attempts %d; want waiting_storage without an attempt", item.Status, item.ErrorCode, item.Attempts)
	}
	assertNoMedia(t, orch, dl)
}

func TestCheckCopyAnswersLikeTheWorker(t *testing.T) {
	h := newCopyHarness(t)
	h.manager.files.(*fakeFiles).byTrack[0].SizeBytes = 4_000_000
	ctx := context.Background()

	got, err := h.manager.CheckCopy(ctx, boxSetTrack())
	if err != nil {
		t.Fatal(err)
	}
	if got != (CopyCheck{Known: true, SizeBytes: 4_000_000}) {
		t.Fatalf("further release: %+v, want a copy of 4000000 bytes", got)
	}

	home := boxSetTrack()
	home.Album, home.ReleaseType, home.Year, home.ReleaseID = "Arrival", music.ReleaseAlbum, 1976, "rel-arrival"
	if got, err := h.manager.CheckCopy(ctx, home); err != nil || got != (CopyCheck{Known: true, Held: true, SizeBytes: 4_000_000}) {
		t.Fatalf("home release: %+v, %v; want held", got, err)
	}

	h.manager.catalog = &fakeCatalog{}
	if got, err := h.manager.CheckCopy(ctx, boxSetTrack()); err != nil || got.Known {
		t.Fatalf("unknown recording: %+v, %v", got, err)
	}
	// Nothing was written while asking.
	h.assertNoCopy(t)
}

func TestEnqueueCopiesQueuesACopiesOnlyJobOncePerRelease(t *testing.T) {
	store := &atomicEnqueueStore{}
	registry := provider.NewRegistry()
	registry.RegisterMetadata(enqueueMetadata{})
	m := &Manager{
		store: store, registry: registry, broker: NewBroker(nil),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		resolveQueue: make(chan string, 4), wake: make(chan struct{}, 1),
	}
	m.accepting.Store(true)
	// The default for downloads must not matter.
	m.SetSkipExisting(false)

	queued, err := m.EnqueueCopies(context.Background(), "ytmusic", "rel-1", "ABBA - Arrival")
	if err != nil || !queued {
		t.Fatalf("first enqueue = %v, %v", queued, err)
	}
	queued, err = m.EnqueueCopies(context.Background(), "ytmusic", "rel-1", "ABBA - Arrival")
	if err != nil || queued {
		t.Fatalf("second enqueue while the first is unfinished = %v, %v; want not queued", queued, err)
	}
	if len(store.jobs) != 1 {
		t.Fatalf("stored %d jobs, want 1", len(store.jobs))
	}
	job := store.jobs[0]
	if job.Type != TypeRelease || job.TargetID != "rel-1" || !job.Options.CopiesOnly || !job.Options.SkipExisting ||
		job.Options.Origin != OriginManual || job.Priority != PriorityVeryHigh {
		t.Fatalf("job = %+v", job)
	}
}
