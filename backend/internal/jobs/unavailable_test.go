package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func unavailableCandidate(id string) error {
	return ytdlp.ClassifyError("ERROR: [youtube] "+id+": This video is unavailable", errors.New("exit status 1"))
}

// All candidates unavailable: the item finishes with the existing permanent
// result, no retry and no pause.
func TestWorker_AllCandidatesUnavailable_FinishWithoutRetryLoop(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["c1"] = unavailableCandidate("c1")
	prov.resolveErrors["c2"] = unavailableCandidate("c2")
	mgr, store := setupTestFallbackEnvironment(t, prov)

	(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemFailed || updated.NextRetryAt != nil {
		t.Fatalf("status = %v, next retry = %v, want failed without retry", updated.Status, updated.NextRetryAt)
	}
	if !strings.Contains(updated.ErrorMessage, "Keine der 2 passenden Quellen konnte aufgelöst werden.") {
		t.Fatalf("error message = %q", updated.ErrorMessage)
	}
	if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
		t.Fatal("the media provider was paused")
	}
	if prov.resolveCalls["c1"] != 1 || prov.resolveCalls["c2"] != 1 {
		t.Fatalf("resolve calls = %v, want each candidate once", prov.resolveCalls)
	}
}

type failingDownloader struct{ err error }

func (d *failingDownloader) Download(context.Context, provider.MediaSource, string, downloader.ProgressCallback) (*downloader.Result, error) {
	return nil, d.err
}

// The documented boundary of this change: a restriction that only the
// download reports ends the item without another candidate selection. It is
// a permanent candidate failure - no retry loop, no pause - and keeps the
// resolved media id, which tells it apart from the resolve phase's
// "Keine der N passenden Quellen" result.
func TestWorker_RestrictionReportedByDownload_EndsWithoutReselection(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
	}
	for _, late := range []error{
		ytdlp.ClassifyError("ERROR: [youtube] c1: Sorry, this content is age-restricted", errors.New("exit status 1")),
		unavailableCandidate("c1"),
	} {
		prov := newMockFallbackMediaProvider("youtube", candidates)
		mgr, store := setupTestFallbackEnvironment(t, prov)
		mgr.downloader = &failingDownloader{err: late}

		(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

		updated := store.items["item-1"]
		if updated.Status != ItemFailed || updated.NextRetryAt != nil {
			t.Fatalf("status = %v, next retry = %v, want failed without retry", updated.Status, updated.NextRetryAt)
		}
		if updated.ErrorCode != string(apperr.CodeTrackNotFound) || updated.MediaID != "c1" {
			t.Fatalf("error code = %s, media = %q", updated.ErrorCode, updated.MediaID)
		}
		if strings.Contains(updated.ErrorMessage, "Keine der") {
			t.Fatalf("download-phase failure reads like a resolve-phase result: %q", updated.ErrorMessage)
		}
		if len(prov.resolvedOrder) != 1 {
			t.Fatalf("resolved %v: the boundary changed, another candidate was selected", prov.resolvedOrder)
		}
		if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
			t.Fatal("the media provider was paused")
		}
	}
}
