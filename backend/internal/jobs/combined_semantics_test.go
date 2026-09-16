package jobs

import (
	"context"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// The YouTube resolver's rejection with the combined fallback switched off,
// with the code and wording it had in v0.27.2-rc.2.
func noAudioOnlyStream(id string) error {
	return apperr.Newf(apperr.CodeDownloadFailed,
		"The media item %q offers no audio only stream (formats: 5 total, 1 muxed, 0 video only, "+
			"0 video with unknown audio, 4 images, 0 unknown, 0 other; muxed up to 182 kbps total, 0 kbps audio).", id)
}

// With the fallback switched off, an item whose candidates offer no audio only
// stream ends as it did in v0.27.2-rc.2: failed at once, TRACK_NOT_FOUND, no
// retry scheduled and no provider pause.
func TestWorker_NoAudioOnlyStreamWithFallbackOff_KeepsTheRc2Result(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["c1"] = noAudioOnlyStream("c1")
	prov.resolveErrors["c2"] = noAudioOnlyStream("c2")
	mgr, store := setupTestFallbackEnvironment(t, prov)

	(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemFailed || updated.NextRetryAt != nil {
		t.Fatalf("status = %v, next retry = %v, want failed without retry", updated.Status, updated.NextRetryAt)
	}
	if updated.ErrorCode != string(apperr.CodeTrackNotFound) {
		t.Fatalf("error code = %s, want %s", updated.ErrorCode, apperr.CodeTrackNotFound)
	}
	if !strings.HasPrefix(updated.ErrorMessage, "Keine der 2 passenden Quellen konnte aufgelöst werden.") {
		t.Fatalf("error message = %q", updated.ErrorMessage)
	}
	if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
		t.Fatal("the media provider was paused")
	}
	if prov.resolveCalls["c1"] != 1 || prov.resolveCalls["c2"] != 1 {
		t.Fatalf("resolve calls = %v, want each candidate once", prov.resolveCalls)
	}
}

// A combined transfer stopped by the local budget ends the item without a
// retry - the next attempt would spend the same budget on the same stream -
// and without pausing the provider family.
func TestWorker_TransferBudgetStop_EndsWithoutRetryOrPause(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
	for _, stop := range []error{
		apperr.New(apperr.CodeTransferBudgetExceeded, "The combined stream exceeded the transfer budget of 1024 bytes and was stopped."),
		apperr.New(apperr.CodeTransferBudgetExceeded, "The combined stream did not finish within the transfer time budget and was stopped."),
	} {
		prov := newMockFallbackMediaProvider("youtube", candidates)
		mgr, store := setupTestFallbackEnvironment(t, prov)
		mgr.downloader = &failingDownloader{err: stop}

		(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

		updated := store.items["item-1"]
		if updated.Status != ItemFailed || updated.NextRetryAt != nil {
			t.Fatalf("status = %v, next retry = %v, want failed without retry", updated.Status, updated.NextRetryAt)
		}
		if updated.ErrorCode != string(apperr.CodeTransferBudgetExceeded) {
			t.Fatalf("error code = %s", updated.ErrorCode)
		}
		if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
			t.Fatal("a local budget stop paused the media provider")
		}
	}
}
