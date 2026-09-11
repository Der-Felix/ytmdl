package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func ageRestrictedCandidate(id string) error {
	return ytdlp.ClassifyError("ERROR: [youtube] "+id+": Sorry, this content is age-restricted", errors.New("exit status 1"))
}

// An item whose candidates are all age restricted is finished with the
// existing "no candidate could be resolved" failure: no retry, no pause of the
// media provider, every candidate resolved exactly once. Before the fix the
// item went back to retry_wait with PROVIDER_UNAVAILABLE and resolved the same
// candidate again about a minute later.
func TestWorker_AllCandidatesAgeRestricted_FinishWithoutRetryLoop(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	for _, c := range candidates {
		prov.resolveErrors[c.ID] = ageRestrictedCandidate(c.ID)
	}
	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}

	w.process(context.Background(), job, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemFailed {
		t.Fatalf("status = %v, want failed", updated.Status)
	}
	if updated.NextRetryAt != nil {
		t.Fatalf("a retry was scheduled: %v", updated.NextRetryAt)
	}
	if !strings.Contains(updated.ErrorMessage, "Keine der 2 passenden Quellen konnte aufgelöst werden.") {
		t.Fatalf("error message = %q", updated.ErrorMessage)
	}
	if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
		t.Fatal("the media provider was paused")
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.resolveCalls["c1"] != 1 || prov.resolveCalls["c2"] != 1 {
		t.Fatalf("resolve calls = %v, want each candidate once", prov.resolveCalls)
	}
}

// A restricted candidate is skipped and the next suitable one is downloaded.
func TestWorker_AgeRestrictedCandidate_NextCandidateCompletes(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["c1"] = ageRestrictedCandidate("c1")
	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}

	w.process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, store.items["item-1"])

	updated := store.items["item-1"]
	if updated.Status != ItemCompleted || updated.MediaID != "c2" {
		t.Fatalf("status = %v, media = %q (error %q), want completed with c2", updated.Status, updated.MediaID, updated.ErrorMessage)
	}
	if _, cooling := mgr.cooldown.Remaining("youtube"); cooling {
		t.Fatal("the media provider was paused")
	}
}
