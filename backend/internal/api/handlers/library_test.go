package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

type mockLibCatalog struct {
	tracks   map[string]music.Track
	releases map[string]music.Release
	artists  map[string]music.Artist
}

func newMockLibCatalog() *mockLibCatalog {
	return &mockLibCatalog{
		tracks:   make(map[string]music.Track),
		releases: make(map[string]music.Release),
		artists:  make(map[string]music.Artist),
	}
}

func (m *mockLibCatalog) GetTrack(_ context.Context, id string) (*music.Track, error) {
	t, ok := m.tracks[id]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func (m *mockLibCatalog) ListAllTracks(_ context.Context) ([]repository.StoredTrack, error) {
	var res []repository.StoredTrack
	for _, t := range m.tracks {
		res = append(res, repository.StoredTrack{Track: t, IdentityKey: t.ID})
	}
	return res, nil
}

func (m *mockLibCatalog) GetRelease(_ context.Context, id string) (*music.Release, error) {
	r, ok := m.releases[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (m *mockLibCatalog) ListTracks(_ context.Context, releaseID string, limit, offset int) ([]music.Track, error) {
	var res []music.Track
	for _, t := range m.tracks {
		if releaseID == "" || t.ReleaseID == releaseID {
			res = append(res, t)
		}
	}
	return res, nil
}

func (m *mockLibCatalog) DeleteTrack(_ context.Context, id string) error {
	delete(m.tracks, id)
	return nil
}

func (m *mockLibCatalog) DeleteRelease(_ context.Context, id string) error {
	delete(m.releases, id)
	return nil
}

func (m *mockLibCatalog) UpdateReleaseCover(_ context.Context, releaseID string, coverURL string) error {
	r, ok := m.releases[releaseID]
	if !ok {
		return nil
	}
	r.CoverURL = coverURL
	m.releases[releaseID] = r
	for id, t := range m.tracks {
		if t.ReleaseID == releaseID {
			t.CoverURL = coverURL
			m.tracks[id] = t
		}
	}
	return nil
}

func (m *mockLibCatalog) ListSources(_ context.Context, _ string) ([]music.Source, error) {
	return nil, nil
}

func (m *mockLibCatalog) SetLyricsState(_ context.Context, trackID string, state music.LyricsState, provider string, checkedAt time.Time) error {
	t, ok := m.tracks[trackID]
	if !ok {
		return nil
	}
	t.LyricsState = state
	t.LyricsProvider = provider
	if !checkedAt.IsZero() {
		t.LyricsCheckedAt = &checkedAt
	}
	m.tracks[trackID] = t
	return nil
}

func (m *mockLibCatalog) ListTracksNeedingLyrics(_ context.Context, before time.Time, limit int) ([]repository.StoredTrack, error) {
	var res []repository.StoredTrack
	for _, t := range m.tracks {
		if t.LyricsState == music.LyricsAvailableSynced {
			continue
		}
		if t.LyricsCheckedAt == nil || t.LyricsCheckedAt.Before(before) {
			res = append(res, repository.StoredTrack{Track: t, IdentityKey: t.ID})
			if limit > 0 && len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}

func (m *mockLibCatalog) LyricsStats(_ context.Context, cutoff time.Time) (repository.LyricsStats, error) {
	var stats repository.LyricsStats
	stats.TracksScanned = len(m.tracks)
	for _, t := range m.tracks {
		switch t.LyricsState {
		case music.LyricsAvailableSynced:
			stats.AlreadyLRC++
		case music.LyricsAvailablePlain:
			stats.AlreadyTXT++
		case music.LyricsInstrumental:
			stats.Instrumental++
		default:
			stats.Missing++
			if t.LyricsCheckedAt == nil || t.LyricsCheckedAt.Before(cutoff) {
				stats.Eligible++
			}
		}
	}
	return stats, nil
}

func (m *mockLibCatalog) GetLibraryAggregates(_ context.Context) (
	artistCount, releaseCount, trackCount, fileCount int,
	totalBytes int64,
	lyricsCoverage map[music.LyricsState]int,
	codecBreakdown map[string]int,
	err error,
) {
	lyricsCoverage = make(map[music.LyricsState]int)
	codecBreakdown = make(map[string]int)
	for _, t := range m.tracks {
		lyricsCoverage[t.LyricsState]++
	}
	return len(m.artists), len(m.releases), len(m.tracks), len(m.tracks), 2048, lyricsCoverage, codecBreakdown, nil
}

func (m *mockLibCatalog) ListArtists(_ context.Context, limit, offset int) ([]music.Artist, error) {
	var res []music.Artist
	for _, a := range m.artists {
		res = append(res, a)
	}
	return res, nil
}

func (m *mockLibCatalog) ListReleases(_ context.Context, artistID string, limit, offset int) ([]music.Release, error) {
	var res []music.Release
	for _, r := range m.releases {
		if artistID == "" || r.ID == artistID {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockLibCatalog) ListAllReleases(_ context.Context) ([]music.Release, error) {
	var res []music.Release
	for _, r := range m.releases {
		res = append(res, r)
	}
	return res, nil
}

func (m *mockLibCatalog) UpsertArtist(_ context.Context, artist music.Artist) (music.Artist, error) {
	m.artists[artist.ID] = artist
	return artist, nil
}

func (m *mockLibCatalog) UpsertRelease(_ context.Context, release music.Release, _ string) (music.Release, error) {
	m.releases[release.ID] = release
	return release, nil
}

func (m *mockLibCatalog) UpsertTrack(_ context.Context, track music.Track, _, _ string, _ int) (music.Track, error) {
	m.tracks[track.ID] = track
	return track, nil
}

type mockLibFiles struct {
	files map[string]music.File
}

func newMockLibFiles() *mockLibFiles {
	return &mockLibFiles{files: make(map[string]music.File)}
}

func (m *mockLibFiles) ListAll(_ context.Context) ([]music.File, error) {
	var res []music.File
	for _, f := range m.files {
		res = append(res, f)
	}
	return res, nil
}

func (m *mockLibFiles) FindByID(_ context.Context, id string) (*music.File, error) {
	for _, f := range m.files {
		if f.ID == id {
			return &f, nil
		}
	}
	return nil, nil
}

func (m *mockLibFiles) FindByPath(_ context.Context, path string) (*music.File, error) {
	f, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	return &f, nil
}

func (m *mockLibFiles) ListByTrack(_ context.Context, trackID string) ([]music.File, error) {
	var res []music.File
	for _, f := range m.files {
		if f.TrackID == trackID {
			res = append(res, f)
		}
	}
	return res, nil
}

func (m *mockLibFiles) Delete(_ context.Context, id string) error {
	for path, f := range m.files {
		if f.ID == id {
			delete(m.files, path)
			break
		}
	}
	return nil
}

func (m *mockLibFiles) DeleteByTrack(_ context.Context, trackID string) error {
	for path, f := range m.files {
		if f.TrackID == trackID {
			delete(m.files, path)
		}
	}
	return nil
}

func (m *mockLibFiles) DeleteByPath(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}

func (m *mockLibFiles) Upsert(_ context.Context, file music.File) (music.File, error) {
	if file.ID == "" {
		file.ID = music.NewID()
	}
	m.files[file.Path] = file
	return file, nil
}

func setupLibraryHandlersWithMocks(t *testing.T) (*Handlers, *library.Service, *mockLibCatalog, *mockLibFiles, *storage.Library, http.Handler) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockLibCatalog()
	mockFiles := newMockLibFiles()
	catRepo := repository.NewCatalog(nil)
	filesRepo := repository.NewFiles(nil)

	// Build real library service with mocks
	libSvc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Catalog: catalog,
		Files:   mockFiles,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		libSvc.Stop()
	})

	h := &Handlers{
		deps: Deps{
			Catalog:        catRepo,
			Files:          filesRepo,
			LibraryService: libSvc,
		},
	}

	r := chi.NewRouter()
	r.Route("/api/v1/library", func(libRouter chi.Router) {
		libRouter.Get("/stats", h.LibraryStats)
		libRouter.Post("/scan", h.StartLibraryScan)
		libRouter.Get("/scan", h.GetLibraryScan)
		libRouter.Post("/tracks/{id}/redownload", h.RedownloadLibraryTrack)
		libRouter.Post("/tracks/{id}/retag", h.RetagLibraryTrack)
		libRouter.Delete("/tracks/{id}", h.DeleteLibraryTrack)
		libRouter.Delete("/releases/{id}", h.DeleteLibraryRelease)
		libRouter.Delete("/scan/issues/{id}", h.DeleteLibraryOrphanIssue)
		libRouter.Get("/tracks/{id}/lyrics", h.TrackLyrics)
		libRouter.Post("/tracks/{id}/lyrics/refresh", h.RefreshTrackLyrics)
		libRouter.Delete("/tracks/{id}/lyrics", h.DeleteTrackLyrics)
		libRouter.Post("/lyrics/backfill", h.BackfillLyrics)
		libRouter.Get("/lyrics/backfill", h.BackfillLyricsStatus)
		libRouter.Get("/lyrics/backfill/preview", h.PreviewBackfillLyrics)
		libRouter.Get("/compatibility", h.CompatibilityReport)
		libRouter.Post("/reorganize", h.Reorganize)
	})

	return h, libSvc, catalog, mockFiles, lib, r
}

func setupLibraryHandlers(t *testing.T) (*Handlers, *library.Service, http.Handler) {
	h, libSvc, _, _, _, r := setupLibraryHandlersWithMocks(t)
	return h, libSvc, r
}

func TestLibraryStatsAndScanEndpoints(t *testing.T) {
	_, _, router := setupLibraryHandlers(t)

	// GET /api/v1/library/stats
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/stats", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats returned status %d: %s", rec.Code, rec.Body.String())
	}

	// POST /api/v1/library/scan
	reqScan := httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", nil)
	recScan := httptest.NewRecorder()
	router.ServeHTTP(recScan, reqScan)

	if recScan.Code != http.StatusOK && recScan.Code != http.StatusAccepted {
		t.Fatalf("POST /scan returned status %d: %s", recScan.Code, recScan.Body.String())
	}

	// GET /api/v1/library/scan
	reqGetScan := httptest.NewRequest(http.MethodGet, "/api/v1/library/scan", nil)
	recGetScan := httptest.NewRecorder()
	router.ServeHTTP(recGetScan, reqGetScan)

	if recGetScan.Code != http.StatusOK {
		t.Fatalf("GET /scan returned status %d: %s", recGetScan.Code, recGetScan.Body.String())
	}
}

func TestLibraryDeleteTrackEndpoint(t *testing.T) {
	_, _, router := setupLibraryHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/library/tracks/non-existent", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Deleting a non-existent track safely returns 200 or 204 or 404
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent && rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /tracks/non-existent returned unexpected status %d", rec.Code)
	}
}

func TestLibraryDeleteOrphanIssueEndpoint(t *testing.T) {
	_, _, router := setupLibraryHandlers(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/library/scan/issues/invalid-id", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE /scan/issues/invalid-id returned status %d, want 404/400", rec.Code)
	}
}

func TestLibraryLyricsEndpoints(t *testing.T) {
	_, _, router := setupLibraryHandlers(t)

	// GET /api/v1/library/tracks/{id}/lyrics (non-existent)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks/non-existent/lyrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("GET /tracks/non-existent/lyrics returned status %d", rec.Code)
	}

	// DELETE /api/v1/library/tracks/{id}/lyrics
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/library/tracks/non-existent/lyrics", nil)
	recDel := httptest.NewRecorder()
	router.ServeHTTP(recDel, reqDel)

	if recDel.Code != http.StatusOK && recDel.Code != http.StatusNotFound {
		t.Fatalf("DELETE /tracks/non-existent/lyrics returned status %d", recDel.Code)
	}

	// GET /api/v1/library/lyrics/backfill
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v1/library/lyrics/backfill", nil)
	recStatus := httptest.NewRecorder()
	router.ServeHTTP(recStatus, reqStatus)

	if recStatus.Code != http.StatusOK {
		t.Fatalf("GET /lyrics/backfill returned status %d", recStatus.Code)
	}

	// GET /api/v1/library/lyrics/backfill/preview
	reqPreview := httptest.NewRequest(http.MethodGet, "/api/v1/library/lyrics/backfill/preview", nil)
	recPreview := httptest.NewRecorder()
	router.ServeHTTP(recPreview, reqPreview)

	if recPreview.Code != http.StatusOK {
		t.Fatalf("GET /lyrics/backfill/preview returned status %d: %s", recPreview.Code, recPreview.Body.String())
	}
}

func TestLibraryCompatibilityEndpoints(t *testing.T) {
	_, _, router := setupLibraryHandlers(t)

	// GET /api/v1/library/compatibility
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/compatibility", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /compatibility returned status %d", rec.Code)
	}
}
