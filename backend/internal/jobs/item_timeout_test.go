package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// stallingDownloader blocks until its context ends and then reports what the
// yt-dlp client reports: a cancellation carrying the context error while a
// transfer runs, or the bare context error while it waits for the session
// slot.
type stallingDownloader struct {
	whileWaiting bool
	calls        atomic.Int32
}

func (d *stallingDownloader) Download(ctx context.Context, _ provider.MediaSource, _ string, _ downloader.ProgressCallback) (*downloader.Result, error) {
	d.calls.Add(1)
	<-ctx.Done()
	if d.whileWaiting {
		return nil, ctx.Err()
	}
	return nil, apperr.Wrap(apperr.CodeJobCancelled, "The download was cancelled.", ctx.Err())
}

// stallingSearch is a media provider whose search blocks until its context
// ends, like a yt-dlp query waiting for the session slot.
type stallingSearch struct{ *mockFallbackMediaProvider }

func (p stallingSearch) Search(ctx context.Context, _ music.Track) ([]provider.MediaCandidate, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

const trackTimeoutCode = string(apperr.CodeTrackTimeout)

func timeoutCandidates() []provider.MediaCandidate {
	return []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
}

// runThroughStartWorker processes the item the way the dispatcher does, with
// the manager's own per-item context, and waits for the worker to finish.
func runThroughStartWorker(t *testing.T, mgr *Manager, item Item, trackTimeout time.Duration, during func()) {
	t.Helper()
	mgr.trackTimeout = trackTimeout
	mgr.startWorker(Job{ID: "job-1", MediaProvider: "youtube"}, item)
	if during != nil {
		during()
	}
	done := make(chan struct{})
	go func() { mgr.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not finish")
	}
}

// The item's own time limit passing is a local, bounded retry - whether it
// passes during a transfer, while the download waits for the session slot or
// while the search waits for it. It is neither a cancellation nor a provider
// condition.
func TestWorker_TrackTimeoutIsABoundedRetry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Manager)
		prov  func() provider.MediaProvider
	}{
		{"during the transfer", func(m *Manager) { m.downloader = &stallingDownloader{} }, nil},
		{"waiting for the slot to download", func(m *Manager) { m.downloader = &stallingDownloader{whileWaiting: true} }, nil},
		{"waiting for the slot to search", nil, func() provider.MediaProvider {
			return stallingSearch{newMockFallbackMediaProvider("youtube", timeoutCandidates())}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prov provider.MediaProvider = newMockFallbackMediaProvider("youtube", timeoutCandidates())
			if tc.prov != nil {
				prov = tc.prov()
			}
			mgr, store := setupTestFallbackEnvironment(t, prov)
			if tc.setup != nil {
				tc.setup(mgr)
			}
			started := time.Now()

			runThroughStartWorker(t, mgr, store.items["item-1"], 200*time.Millisecond, nil)

			updated := store.items["item-1"]
			if updated.Status != ItemRetryWait || updated.NextRetryAt == nil || !updated.NextRetryAt.After(started) {
				t.Fatalf("status = %v, next retry = %v, want a scheduled retry", updated.Status, updated.NextRetryAt)
			}
			if updated.Attempts != 1 {
				t.Fatalf("attempts = %d, want the timeout to consume one attempt", updated.Attempts)
			}
			if updated.ErrorCode != trackTimeoutCode {
				t.Fatalf("error code = %q, want %s", updated.ErrorCode, trackTimeoutCode)
			}
			if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
				t.Fatal("a local timeout paused the media provider")
			}
		})
	}
}

// The retry stays bounded: a timeout on the last allowed attempt fails the
// item for good.
func TestWorker_TrackTimeoutOnTheLastAttemptFails(t *testing.T) {
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", timeoutCandidates()))
	mgr.downloader = &stallingDownloader{}
	item := store.items["item-1"]
	item.Attempts = item.MaxAttempts - 1
	store.items["item-1"] = item

	runThroughStartWorker(t, mgr, item, 200*time.Millisecond, nil)

	updated := store.items["item-1"]
	if updated.Status != ItemFailed || updated.NextRetryAt != nil {
		t.Fatalf("status = %v, next retry = %v, want failed without retry", updated.Status, updated.NextRetryAt)
	}
	if updated.ErrorCode != trackTimeoutCode {
		t.Fatalf("error code = %q", updated.ErrorCode)
	}
}

// An explicit cancellation stays a cancellation, is not retried and pauses
// nothing - also when it arrives while the download waits for the slot.
func TestWorker_UserCancelStaysCancelled(t *testing.T) {
	for _, whileWaiting := range []bool{false, true} {
		mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", timeoutCandidates()))
		dl := &stallingDownloader{whileWaiting: whileWaiting}
		mgr.downloader = dl

		runThroughStartWorker(t, mgr, store.items["item-1"], time.Minute, func() {
			deadline := time.Now().Add(5 * time.Second)
			for dl.calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			mgr.cancelRun("job-1")
		})

		updated := store.items["item-1"]
		if updated.Status != ItemCancelled || updated.NextRetryAt != nil {
			t.Fatalf("waiting=%v: status = %v, next retry = %v, want cancelled", whileWaiting, updated.Status, updated.NextRetryAt)
		}
		if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
			t.Fatalf("waiting=%v: a cancellation paused the media provider", whileWaiting)
		}
	}
}

// A service shutdown is neither a cancellation nor a timeout: the item is left
// in its working state for the next process to recover, and no retry is spent.
func TestWorker_ShutdownIsNotACancellation(t *testing.T) {
	mgr, store := setupTestFallbackEnvironment(t, newMockFallbackMediaProvider("youtube", timeoutCandidates()))
	dl := &stallingDownloader{}
	mgr.downloader = dl
	mgr.ctx, mgr.stop = context.WithCancel(context.Background())

	runThroughStartWorker(t, mgr, store.items["item-1"], time.Minute, func() {
		deadline := time.Now().Add(5 * time.Second)
		for dl.calls.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		mgr.BeginShutdown()
		mgr.stopping.Store(true)
		mgr.stop()
	})

	updated := store.items["item-1"]
	switch updated.Status {
	case ItemCancelled, ItemFailed, ItemRetryWait:
		t.Fatalf("status = %v: shutdown was recorded as a final outcome or a retry", updated.Status)
	}
	if updated.NextRetryAt != nil {
		t.Fatalf("next retry = %v, want none", updated.NextRetryAt)
	}
}
