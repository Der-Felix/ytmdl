package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// oneReleaseResolver lists two recordings for every release.
type oneReleaseResolver struct{}

func (oneReleaseResolver) ResolveRelease(_ context.Context, _, releaseID string) (*discography.Result, error) {
	return &discography.Result{Groups: []discography.Group{
		{Track: music.Track{Title: "Held", SourceID: "held", ReleaseID: releaseID}},
		{Track: music.Track{Title: "Missing", SourceID: "missing", ReleaseID: releaseID}},
	}}, nil
}

// copyPlannerStub copies "missing" and holds everything else.
type copyPlannerStub struct{ queued []string }

func (p *copyPlannerStub) CheckCopy(_ context.Context, track music.Track) (jobs.CopyCheck, error) {
	if track.SourceID == "missing" {
		return jobs.CopyCheck{Known: true, SizeBytes: 1234}, nil
	}
	return jobs.CopyCheck{Known: true, Held: true}, nil
}

func (p *copyPlannerStub) EnqueueCopies(_ context.Context, provider, releaseID, _ string) (bool, error) {
	p.queued = append(p.queued, provider+"/"+releaseID)
	return true, nil
}

func (p *copyPlannerStub) CompleteReleases() bool { return true }

func setupCompleteReleasesHandlers(t *testing.T) (http.Handler, *copyPlannerStub) {
	t.Helper()
	lib, err := storage.NewLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := newMockLibCatalog()
	catalog.releases["r1"] = music.Release{ID: "r1", Title: "Arrival", AlbumArtist: "ABBA", Provider: "deezer", SourceID: "arrival"}
	planner := &copyPlannerStub{}
	libSvc, err := library.NewService(library.ServiceOptions{
		Library:  lib,
		Catalog:  catalog,
		Files:    newMockLibFiles(),
		Releases: oneReleaseResolver{},
		Copies:   planner,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(libSvc.Stop)

	h := &Handlers{deps: Deps{LibraryService: libSvc}}
	r := chi.NewRouter()
	r.Route("/api/v1/library", func(libRouter chi.Router) {
		libRouter.Get("/complete-releases", h.GetCompleteReleases)
		libRouter.Post("/complete-releases/scan", h.StartCompleteReleasesScan)
		libRouter.Delete("/complete-releases/scan", h.CancelCompleteReleasesScan)
		libRouter.Post("/complete-releases/apply", h.ApplyCompleteReleases)
	})
	return r, planner
}

func serveCompletion(router http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestCompleteReleasesEndpoints(t *testing.T) {
	router, planner := setupCompleteReleasesHandlers(t)
	const base = "/api/v1/library/complete-releases"

	rec := serveCompletion(router, http.MethodGet, base)
	var idle testEnvelope[library.CompletionStatus]
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &idle) != nil || idle.Data.State != library.CompletionIdle || !idle.Data.SettingEnabled {
		t.Fatalf("GET before a scan = %d %s", rec.Code, rec.Body.String())
	}

	rec = serveCompletion(router, http.MethodPost, base+"/apply")
	var refused testErrorEnvelope
	if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &refused) != nil || refused.Error.Code != string(apperr.CodeInvalidRequest) {
		t.Fatalf("apply without a scan = %d %s", rec.Code, rec.Body.String())
	}

	rec = serveCompletion(router, http.MethodDelete, base+"/scan")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel without a scan = %d %s", rec.Code, rec.Body.String())
	}

	rec = serveCompletion(router, http.MethodPost, base+"/scan")
	var started testEnvelope[library.CompletionStatus]
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &started) != nil || started.Data.State != library.CompletionScanning {
		t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}

	var scanned testEnvelope[library.CompletionStatus]
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec = serveCompletion(router, http.MethodGet, base)
		if err := json.Unmarshal(rec.Body.Bytes(), &scanned); err != nil {
			t.Fatal(err)
		}
		if scanned.Data.State == library.CompletionScanned {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	result := scanned.Data.Result
	if scanned.Data.State != library.CompletionScanned || result == nil ||
		result.Copies != 1 || result.Bytes != 1234 || result.ReleasesIncomplete != 1 || len(result.Releases) != 1 {
		t.Fatalf("scan result = %s", rec.Body.String())
	}

	rec = serveCompletion(router, http.MethodPost, base+"/apply")
	var applied testEnvelope[library.CompletionApply]
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &applied) != nil ||
		applied.Data.QueuedJobs != 1 || applied.Data.Copies != 1 || applied.Data.Bytes != 1234 {
		t.Fatalf("apply = %d %s", rec.Code, rec.Body.String())
	}
	if len(planner.queued) != 1 || planner.queued[0] != "deezer/arrival" {
		t.Fatalf("queued %v", planner.queued)
	}

	rec = serveCompletion(router, http.MethodPost, base+"/apply")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second apply = %d %s", rec.Code, rec.Body.String())
	}
}
