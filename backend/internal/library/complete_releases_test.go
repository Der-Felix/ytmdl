package library

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// releaseCatalog lists the stored releases of a completion scan.
type releaseCatalog struct {
	CatalogStore
	releases []music.Release
}

func (c *releaseCatalog) ListAllReleases(context.Context) ([]music.Release, error) {
	return c.releases, nil
}

// fakeReleaseResolver answers track lists by release source id. A release
// listed in block waits until the channel is closed.
type fakeReleaseResolver struct {
	mu     sync.Mutex
	tracks map[string][]music.Track
	errs   map[string]error
	block  map[string]chan struct{}
	asked  []string
}

func (r *fakeReleaseResolver) ResolveRelease(ctx context.Context, providerName, releaseID string) (*discography.Result, error) {
	r.mu.Lock()
	r.asked = append(r.asked, providerName+"/"+releaseID)
	wait := r.block[releaseID]
	r.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := r.errs[releaseID]; err != nil {
		return nil, err
	}
	result := &discography.Result{}
	for _, track := range r.tracks[releaseID] {
		result.Groups = append(result.Groups, discography.Group{Track: track})
	}
	return result, nil
}

// fakeCopyPlanner answers the copy decision by track source id: "unknown",
// "held" or a size in bytes for a copy.
type fakeCopyPlanner struct {
	mu       sync.Mutex
	decision map[string]jobs.CopyCheck
	enqueued []string
	busy     map[string]bool
	setting  bool
}

func (p *fakeCopyPlanner) CheckCopy(_ context.Context, track music.Track) (jobs.CopyCheck, error) {
	return p.decision[track.SourceID], nil
}

func (p *fakeCopyPlanner) EnqueueCopies(_ context.Context, provider, releaseID, _ string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy[releaseID] {
		return false, nil
	}
	p.enqueued = append(p.enqueued, provider+"/"+releaseID)
	return true, nil
}

func (p *fakeCopyPlanner) CompleteReleases() bool { return p.setting }

func tracksOf(releaseID string, ids ...string) []music.Track {
	out := make([]music.Track, 0, len(ids))
	for _, id := range ids {
		out = append(out, music.Track{Title: id, SourceProvider: "deezer", SourceID: id, ReleaseID: releaseID})
	}
	return out
}

type completionHarness struct {
	svc      *Service
	resolver *fakeReleaseResolver
	planner  *fakeCopyPlanner
}

// newCompletionHarness holds four releases: "complete" holds all its
// recordings, "arrival" lacks two the library holds elsewhere and one it does
// not hold at all, "broken" cannot be read from its provider, and "local" has
// no provider identity.
func newCompletionHarness(t *testing.T) *completionHarness {
	t.Helper()
	lib, err := storage.NewLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &releaseCatalog{releases: []music.Release{
		{ID: "r-complete", Title: "Complete", AlbumArtist: "ABBA", Provider: "deezer", SourceID: "complete"},
		{ID: "r-arrival", Title: "Arrival", AlbumArtist: "ABBA", Provider: "deezer", SourceID: "arrival"},
		{ID: "r-broken", Title: "Broken", AlbumArtist: "ABBA", Provider: "gone", SourceID: "broken"},
		{ID: "r-local", Title: "Local", AlbumArtist: "ABBA"},
	}}
	resolver := &fakeReleaseResolver{
		tracks: map[string][]music.Track{
			"complete": tracksOf("complete", "c1", "c2"),
			"arrival":  tracksOf("arrival", "a1", "a2", "a3", "a4", "a5"),
		},
		errs: map[string]error{
			"broken": apperr.New(apperr.CodeProviderNotFound, "The provider is not configured."),
		},
		block: map[string]chan struct{}{},
	}
	planner := &fakeCopyPlanner{decision: map[string]jobs.CopyCheck{
		"c1": {Known: true, Held: true}, "c2": {Known: true, Held: true},
		"a1": {Known: true, Held: true}, "a2": {Known: true, Held: true},
		"a3": {Known: true, SizeBytes: 3_000_000}, "a4": {Known: true, SizeBytes: 5_500_000},
		// a5 is unknown: the zero value.
	}, busy: map[string]bool{}}

	svc, err := NewService(ServiceOptions{
		Library:  lib,
		Catalog:  catalog,
		Files:    newMockFiles(),
		Releases: resolver,
		Copies:   planner,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Stop)
	return &completionHarness{svc: svc, resolver: resolver, planner: planner}
}

func (h *completionHarness) waitFor(t *testing.T, state CompletionState) CompletionStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status := h.svc.CompleteReleasesStatus(); status.State == state {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state stayed %s, want %s", h.svc.CompleteReleasesStatus().State, state)
	return CompletionStatus{}
}

func TestCompleteReleasesScanCountsWhatACopiesOnlyJobWouldDo(t *testing.T) {
	h := newCompletionHarness(t)
	h.planner.setting = true

	if status := h.svc.CompleteReleasesStatus(); status.State != CompletionIdle || status.Result != nil || !status.SettingEnabled {
		t.Fatalf("before a scan: %+v", status)
	}
	started, err := h.svc.StartCompleteReleasesScan()
	if err != nil {
		t.Fatal(err)
	}
	if started.State != CompletionScanning || started.StartedAt == nil {
		t.Fatalf("started = %+v", started)
	}

	status := h.waitFor(t, CompletionScanned)
	got := status.Result
	if got == nil {
		t.Fatal("a finished scan has no result")
	}
	if got.ReleasesExamined != 2 || got.ReleasesIncomplete != 1 || got.ReleasesWithoutSource != 1 ||
		got.ReleasesFailed != 1 || got.FailedByCode[string(apperr.CodeProviderNotFound)] != 1 {
		t.Fatalf("release counts = %+v", got)
	}
	if got.Copies != 2 || got.Bytes != 8_500_000 || got.HeldTracks != 4 || got.UnknownTracks != 1 {
		t.Fatalf("track counts = %+v", got)
	}
	want := CompletionRelease{ReleaseID: "r-arrival", Artist: "ABBA", Title: "Arrival",
		Total: 5, Held: 2, Copies: 2, Unknown: 1, Bytes: 8_500_000}
	if len(got.Releases) != 1 || got.Releases[0] != want || got.ReleasesTruncated {
		t.Fatalf("releases = %+v, want [%+v]", got.Releases, want)
	}
	if status.Progress != (CompletionProgress{Done: 3, Total: 3}) {
		t.Fatalf("progress = %+v", status.Progress)
	}
	if status.FinishedAt == nil || status.ExpiresAt == nil || !status.ExpiresAt.Equal(status.FinishedAt.Add(CompletionMaxAge)) {
		t.Fatalf("timestamps = %v / %v", status.FinishedAt, status.ExpiresAt)
	}
	for _, asked := range h.resolver.asked {
		if asked == "/" {
			t.Fatal("a release without a provider identity was looked up")
		}
	}
	if len(h.resolver.asked) != 3 {
		t.Fatalf("looked up %v, want the three releases with an identity", h.resolver.asked)
	}
	if len(h.planner.enqueued) != 0 {
		t.Fatal("a scan queued jobs")
	}
}

func TestCompleteReleasesScanRunsOnceAndCanBeCancelled(t *testing.T) {
	h := newCompletionHarness(t)
	gate := make(chan struct{})
	h.resolver.block["complete"] = gate
	defer close(gate)

	if _, err := h.svc.StartCompleteReleasesScan(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartCompleteReleasesScan(); apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("second start = %v, want ALREADY_EXISTS", err)
	}
	if _, err := h.svc.ApplyCompleteReleases(context.Background()); apperr.CodeOf(err) != apperr.CodeConflict {
		t.Fatalf("apply during a scan = %v, want STALE_REPAIR (conflict)", err)
	}

	cancelled, err := h.svc.CancelCompleteReleasesScan()
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != CompletionCancelled || cancelled.ErrorCode != string(apperr.CodeJobCancelled) || cancelled.Result != nil {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	if _, err := h.svc.CancelCompleteReleasesScan(); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("cancel without a scan = %v, want INVALID_REQUEST", err)
	}
	if _, err := h.svc.ApplyCompleteReleases(context.Background()); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("apply after a cancelled scan = %v, want INVALID_REQUEST", err)
	}

	// A new scan can start at once; the cancelled one never overwrites it.
	h.resolver.mu.Lock()
	delete(h.resolver.block, "complete")
	h.resolver.mu.Unlock()
	if _, err := h.svc.StartCompleteReleasesScan(); err != nil {
		t.Fatal(err)
	}
	if status := h.waitFor(t, CompletionScanned); status.Result == nil || status.Result.Copies != 2 {
		t.Fatalf("rescan = %+v", status)
	}
}

func TestApplyCompleteReleasesQueuesTheIncompleteReleasesOnce(t *testing.T) {
	h := newCompletionHarness(t)
	if _, err := h.svc.ApplyCompleteReleases(context.Background()); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("apply without a scan = %v, want INVALID_REQUEST", err)
	}
	if _, err := h.svc.StartCompleteReleasesScan(); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, CompletionScanned)

	applied, err := h.svc.ApplyCompleteReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *applied != (CompletionApply{QueuedJobs: 1, Copies: 2, Bytes: 8_500_000}) {
		t.Fatalf("applied = %+v", applied)
	}
	if len(h.planner.enqueued) != 1 || h.planner.enqueued[0] != "deezer/arrival" {
		t.Fatalf("enqueued %v, want only the incomplete release", h.planner.enqueued)
	}
	if status := h.svc.CompleteReleasesStatus(); status.AppliedAt == nil || status.ExpiresAt != nil {
		t.Fatalf("after apply: %+v", status)
	}
	if _, err := h.svc.ApplyCompleteReleases(context.Background()); apperr.CodeOf(err) != apperr.CodeConflict {
		t.Fatalf("second apply = %v, want a conflict", err)
	}
}

func TestApplyCompleteReleasesRefusesAStaleScan(t *testing.T) {
	h := newCompletionHarness(t)
	if _, err := h.svc.StartCompleteReleasesScan(); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, CompletionScanned)

	h.svc.completion.mu.Lock()
	old := time.Now().Add(-CompletionMaxAge - time.Minute)
	h.svc.completion.status.FinishedAt = &old
	h.svc.completion.mu.Unlock()

	if _, err := h.svc.ApplyCompleteReleases(context.Background()); apperr.CodeOf(err) != apperr.CodeConflict {
		t.Fatalf("stale apply = %v, want a conflict", err)
	}
	if len(h.planner.enqueued) != 0 {
		t.Fatal("a stale scan queued jobs")
	}
}

func TestApplyCompleteReleasesCountsAReleaseAlreadyQueued(t *testing.T) {
	h := newCompletionHarness(t)
	h.planner.busy["arrival"] = true
	if _, err := h.svc.StartCompleteReleasesScan(); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, CompletionScanned)

	applied, err := h.svc.ApplyCompleteReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *applied != (CompletionApply{AlreadyQueued: 1}) {
		t.Fatalf("applied = %+v", applied)
	}
}
