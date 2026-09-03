package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

type mockProv struct {
	releases map[string]music.Release
}

func (m *mockProv) Name() string { return "ytmusic" }
func (m *mockProv) SearchArtists(_ context.Context, _ string) ([]music.Artist, error) {
	return nil, nil
}
func (m *mockProv) GetArtist(_ context.Context, _ string) (*music.Artist, error) {
	return nil, nil
}
func (m *mockProv) GetDiscography(_ context.Context, _ string) ([]music.Release, error) {
	return nil, nil
}
func (m *mockProv) GetRelease(_ context.Context, id string) (*music.Release, error) {
	r, ok := m.releases[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *mockProv) GetReleaseTracks(_ context.Context, _ string) ([]music.Track, error) {
	return nil, nil
}

func setupArtworkTestEnv(t *testing.T) (*Handlers, *mockLibCatalog, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockLibCatalog()
	files := newMockLibFiles()

	reg := provider.NewRegistry()
	reg.RegisterMetadata(&mockProv{
		releases: map[string]music.Release{
			"src-1": {
				ID:       "rel-1",
				SourceID: "src-1",
				Title:    "Album 1",
				CoverURL: "https://img.test/cover1.jpg",
			},
		},
	})

	libSvc, err := library.NewService(library.ServiceOptions{
		Library:   lib,
		Catalog:   catalog,
		Files:     files,
		Providers: reg,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &Handlers{
		deps: Deps{
			LibraryService: libSvc,
			Catalog:        &repository.Catalog{},
		},
	}

	rel := music.Release{
		ID:       "rel-1",
		Provider: "ytmusic",
		SourceID: "src-1",
		Title:    "Album 1",
		CoverURL: "https://img.test/avatar.jpg",
	}
	catalog.releases[rel.ID] = rel

	return h, catalog, root
}

func TestHandler_PreviewReleaseArtwork(t *testing.T) {
	h, _, _ := setupArtworkTestEnv(t)

	r := chi.NewRouter()
	r.Post("/releases/{id}/artwork/preview", h.PreviewReleaseArtwork)

	req := httptest.NewRequest(http.MethodPost, "/releases/rel-1/artwork/preview", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Data library.ArtworkRepairPreview `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Data.ReleaseID != "rel-1" {
		t.Errorf("release_id = %q, want rel-1", resp.Data.ReleaseID)
	}
	if resp.Data.Status != library.RepairStatusNeedsRefresh {
		t.Errorf("status = %s, want %s", resp.Data.Status, library.RepairStatusNeedsRefresh)
	}
	if resp.Data.NewCoverURL != "https://img.test/cover1.jpg" {
		t.Errorf("new_cover_url = %q, want https://img.test/cover1.jpg", resp.Data.NewCoverURL)
	}
}

func TestHandler_PreviewBulkArtwork(t *testing.T) {
	h, _, _ := setupArtworkTestEnv(t)

	r := chi.NewRouter()
	r.Post("/artwork/preview", h.PreviewBulkArtwork)

	body, _ := json.Marshal(BulkArtworkRequest{
		ReleaseIDs: []string{"rel-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/artwork/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Data []*library.ArtworkRepairPreview `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(resp.Data))
	}
	if resp.Data[0].ReleaseID != "rel-1" {
		t.Errorf("release_id = %q, want rel-1", resp.Data[0].ReleaseID)
	}
}

func TestHandler_RefreshReleaseArtwork(t *testing.T) {
	h, _, _ := setupArtworkTestEnv(t)

	r := chi.NewRouter()
	r.Post("/releases/{id}/artwork/refresh", h.RefreshReleaseArtwork)

	req := httptest.NewRequest(http.MethodPost, "/releases/rel-1/artwork/refresh", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Data library.ArtworkRepairResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.ReleaseID != "rel-1" {
		t.Errorf("release_id = %q, want rel-1", resp.Data.ReleaseID)
	}
}

func TestHandler_RefreshBulkArtwork(t *testing.T) {
	h, _, _ := setupArtworkTestEnv(t)

	r := chi.NewRouter()
	r.Post("/artwork/refresh", h.RefreshBulkArtwork)

	body, _ := json.Marshal(BulkArtworkRequest{
		ReleaseIDs: []string{"rel-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/artwork/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Data []*library.ArtworkRepairResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Data))
	}
	if resp.Data[0].ReleaseID != "rel-1" {
		t.Errorf("release_id = %q, want rel-1", resp.Data[0].ReleaseID)
	}
}
