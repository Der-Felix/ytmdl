package library

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// mockMetadataProvider implements provider.MetadataProvider for testing
type mockMetadataProvider struct {
	releases map[string]music.Release
	err      error
}

func (m *mockMetadataProvider) Name() string { return "ytmusic" }
func (m *mockMetadataProvider) SearchArtists(_ context.Context, _ string) ([]music.Artist, error) {
	return nil, nil
}
func (m *mockMetadataProvider) GetArtist(_ context.Context, _ string) (*music.Artist, error) {
	return nil, nil
}
func (m *mockMetadataProvider) GetDiscography(_ context.Context, _ string) ([]music.Release, error) {
	return nil, nil
}
func (m *mockMetadataProvider) GetRelease(_ context.Context, id string) (*music.Release, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.releases[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *mockMetadataProvider) GetReleaseTracks(_ context.Context, _ string) ([]music.Track, error) {
	return nil, nil
}

func loopbackURL(raw string) string {
	return strings.Replace(raw, "127.0.0.1", "localhost", 1)
}

// generateJPEG generates a valid JPEG image byte slice
func generateJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestArtworkRepair_PreviewAndApply(t *testing.T) {
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockCatalog()
	files := newMockFiles()
	tagger := &mockTagger{}

	// Setup mock HTTP server for cover images
	authenticCoverJPEG := generateJPEG(t, 800, 800)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(authenticCoverJPEG)
	}))
	defer server.Close()

	avatarURL := "https://img.test/artist_avatar.jpg"
	authenticCoverURL := loopbackURL(server.URL) + "/release_cover_800.jpg"

	// Mock Provider
	mockProv := &mockMetadataProvider{
		releases: map[string]music.Release{
			"source-rel-1": {
				ID:          "rel-1",
				SourceID:    "source-rel-1",
				Title:       "Hybrid Theory",
				Artists:     []string{"Linkin Park"},
				CoverURL:    authenticCoverURL,
				ReleaseType: music.ReleaseAlbum,
				Year:        2000,
			},
		},
	}
	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockProv)

	svc, err := NewService(ServiceOptions{
		Library:        lib,
		Catalog:        catalog,
		Files:          files,
		Tagger:         tagger,
		Providers:      reg,
		ArtworkFetcher: metadata.NewArtworkFetcher(server.Client()),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create release with artist avatar URL (Bug 1 state)
	rel := music.Release{
		ID:          "rel-1",
		Provider:    "ytmusic",
		SourceID:    "source-rel-1",
		Title:       "Hybrid Theory",
		Artists:     []string{"Linkin Park"},
		CoverURL:    avatarURL,
		ReleaseType: music.ReleaseAlbum,
		Year:        2000,
	}
	catalog.releases[rel.ID] = rel

	// Create track and file
	track := music.Track{
		ID:          "trk-1",
		ReleaseID:   "rel-1",
		Title:       "Papercut",
		Artists:     []string{"Linkin Park"},
		Album:       "Hybrid Theory",
		TrackNumber: 1,
		Year:        2000,
		CoverURL:    avatarURL,
	}
	catalog.tracks[track.ID] = track

	relDir := filepath.Join("Linkin Park", "2000 - Hybrid Theory")
	trackRelPath := filepath.Join(relDir, "01 - Papercut.opus")
	trackAbsPath := filepath.Join(root, trackRelPath)
	_ = os.MkdirAll(filepath.Dir(trackAbsPath), 0o755)
	if err := os.WriteFile(trackAbsPath, []byte("fake-opus-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	files.files[trackRelPath] = music.File{
		ID:        "f-1",
		TrackID:   track.ID,
		Path:      trackRelPath,
		Container: "opus",
		Codec:     "opus",
		SizeBytes: 14,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	ctx := context.Background()

	// 1. Preview: Must detect NEEDS_REFRESH
	preview, err := svc.PreviewReleaseArtwork(ctx, rel.ID)
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if preview.Status != RepairStatusNeedsRefresh {
		t.Fatalf("preview status = %s, want %s", preview.Status, RepairStatusNeedsRefresh)
	}
	if preview.CurrentCoverURL != avatarURL {
		t.Fatalf("current cover url = %q, want %q", preview.CurrentCoverURL, avatarURL)
	}
	if preview.NewCoverURL != authenticCoverURL {
		t.Fatalf("new cover url = %q, want %q", preview.NewCoverURL, authenticCoverURL)
	}
	if preview.CoverExists {
		t.Fatal("cover.jpg should not exist yet")
	}
	if preview.TracksAffected != 1 {
		t.Fatalf("tracks affected = %d, want 1", preview.TracksAffected)
	}

	// 2. Apply: Must write cover.jpg, update embedded tagger, update catalog
	res, err := svc.ApplyReleaseArtwork(ctx, rel.ID, preview)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if res.Status != RepairStatusApplied {
		t.Fatalf("apply result status = %s (msg: %s), want %s", res.Status, res.Message, RepairStatusApplied)
	}
	if !res.CoverWritten {
		t.Fatal("cover.jpg was not written")
	}
	if res.TracksRepaired != 1 || res.TracksFailed != 0 {
		t.Fatalf("tracks repaired = %d, failed = %d, want 1, 0", res.TracksRepaired, res.TracksFailed)
	}

	// Check that cover.jpg exists on disk
	coverAbsPath := filepath.Join(root, preview.CoverPath)
	if _, err := os.Stat(coverAbsPath); os.IsNotExist(err) {
		t.Fatalf("cover.jpg does not exist at %s", coverAbsPath)
	}

	// Check that catalog was updated
	updatedRel, _ := catalog.GetRelease(ctx, rel.ID)
	if updatedRel.CoverURL != authenticCoverURL {
		t.Fatalf("release cover_url in DB = %q, want %q", updatedRel.CoverURL, authenticCoverURL)
	}
	updatedTrack, _ := catalog.GetTrack(ctx, track.ID)
	if updatedTrack.CoverURL != authenticCoverURL {
		t.Fatalf("track cover_url in DB = %q, want %q", updatedTrack.CoverURL, authenticCoverURL)
	}

	// 3. Second Preview: Must now be ALREADY_CORRECT (Idempotency)
	preview2, err := svc.PreviewReleaseArtwork(ctx, rel.ID)
	if err != nil {
		t.Fatalf("second preview failed: %v", err)
	}
	if preview2.Status != RepairStatusAlreadyCorrect {
		t.Fatalf("second preview status = %s, want %s", preview2.Status, RepairStatusAlreadyCorrect)
	}

	// 4. Second Apply: Must be a no-op returning ALREADY_CORRECT
	taggerAppliedCountBefore := len(tagger.applied)
	res2, err := svc.ApplyReleaseArtwork(ctx, rel.ID, preview2)
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if res2.Status != RepairStatusAlreadyCorrect {
		t.Fatalf("second apply status = %s, want %s", res2.Status, RepairStatusAlreadyCorrect)
	}
	if len(tagger.applied) != taggerAppliedCountBefore {
		t.Fatal("second apply should not have called tagger")
	}
}

func TestArtworkRepair_ImageInvalid_NoDBMutation(t *testing.T) {
	root := t.TempDir()
	lib, _ := storage.NewLibrary(root)
	catalog := newMockCatalog()
	files := newMockFiles()
	tagger := &mockTagger{}

	// Mock server returning HTML 404 instead of an image
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>Not Found</html>"))
	}))
	defer server.Close()

	avatarURL := "https://img.test/artist_avatar.jpg"
	badCoverURL := loopbackURL(server.URL) + "/bad_cover.jpg"

	mockProv := &mockMetadataProvider{
		releases: map[string]music.Release{
			"source-rel-2": {
				ID:       "rel-2",
				SourceID: "source-rel-2",
				Title:    "Meteora",
				CoverURL: badCoverURL,
			},
		},
	}
	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockProv)

	svc, _ := NewService(ServiceOptions{
		Library:        lib,
		Catalog:        catalog,
		Files:          files,
		Tagger:         tagger,
		Providers:      reg,
		ArtworkFetcher: metadata.NewArtworkFetcher(server.Client()),
	})

	rel := music.Release{
		ID:       "rel-2",
		Provider: "ytmusic",
		SourceID: "source-rel-2",
		Title:    "Meteora",
		CoverURL: avatarURL,
	}
	catalog.releases[rel.ID] = rel

	ctx := context.Background()
	res, err := svc.ApplyReleaseArtwork(ctx, rel.ID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Status != RepairStatusImageInvalid {
		t.Fatalf("status = %s, want %s", res.Status, RepairStatusImageInvalid)
	}

	// Verify DB release was NOT mutated
	relAfter, _ := catalog.GetRelease(ctx, rel.ID)
	if relAfter.CoverURL != avatarURL {
		t.Fatalf("DB release cover_url was mutated to %q", relAfter.CoverURL)
	}
}

func TestArtworkRepair_BulkPreviewAndApply(t *testing.T) {
	root := t.TempDir()
	lib, _ := storage.NewLibrary(root)
	catalog := newMockCatalog()
	files := newMockFiles()
	tagger := &mockTagger{}

	authenticCoverJPEG := generateJPEG(t, 600, 600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(authenticCoverJPEG)
	}))
	defer server.Close()

	authenticCoverURL := loopbackURL(server.URL) + "/cover.jpg"

	mockProv := &mockMetadataProvider{
		releases: map[string]music.Release{
			"src-1": {ID: "rel-1", SourceID: "src-1", Title: "Rel 1", CoverURL: authenticCoverURL},
			"src-2": {ID: "rel-2", SourceID: "src-2", Title: "Rel 2", CoverURL: authenticCoverURL},
		},
	}
	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockProv)

	svc, _ := NewService(ServiceOptions{
		Library:        lib,
		Catalog:        catalog,
		Files:          files,
		Tagger:         tagger,
		Providers:      reg,
		ArtworkFetcher: metadata.NewArtworkFetcher(server.Client()),
	})

	releases := []music.Release{
		{ID: "rel-1", Provider: "ytmusic", SourceID: "src-1", Title: "Rel 1", CoverURL: "https://old.url/1"},
		{ID: "rel-2", Provider: "ytmusic", SourceID: "src-2", Title: "Rel 2", CoverURL: "https://old.url/2"},
	}
	catalog.releases["rel-1"] = releases[0]
	catalog.releases["rel-2"] = releases[1]

	ctx := context.Background()

	// 1. Bulk Preview
	previews, err := svc.PreviewBulkArtwork(ctx, "", []string{"rel-1", "rel-2"})
	if err != nil {
		t.Fatalf("bulk preview failed: %v", err)
	}
	if len(previews) != 2 {
		t.Fatalf("got %d previews, want 2", len(previews))
	}
	for _, p := range previews {
		if p.Status != RepairStatusNeedsRefresh {
			t.Fatalf("expected NEEDS_REFRESH, got %s", p.Status)
		}
	}

	// 2. Bulk Apply
	results, err := svc.ApplyBulkArtwork(ctx, "", []string{"rel-1", "rel-2"})
	if err != nil {
		t.Fatalf("bulk apply failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != RepairStatusApplied {
			t.Fatalf("expected APPLIED, got %s (msg: %s)", r.Status, r.Message)
		}
		if !r.CoverWritten {
			t.Fatal("cover should have been written")
		}
	}
}
