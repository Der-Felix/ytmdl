package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// fakePlacementStore lists the placements of the library tests.
type fakePlacementStore struct{ placed []music.Placement }

func (f *fakePlacementStore) ListAll(context.Context) ([]music.Placement, error) {
	return f.placed, nil
}

func (f *fakePlacementStore) ListByTrack(_ context.Context, trackID string) ([]music.Placement, error) {
	var out []music.Placement
	for _, p := range f.placed {
		if p.TrackID == trackID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePlacementStore) ListByRelease(_ context.Context, releaseID string) ([]music.Placement, error) {
	var out []music.Placement
	for _, p := range f.placed {
		if p.ReleaseID == releaseID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePlacementStore) FindByPath(_ context.Context, path string) (*music.Placement, error) {
	for _, p := range f.placed {
		if p.Path == path {
			found := p
			return &found, nil
		}
	}
	return nil, nil
}

var (
	placementHomeRel = filepath.Join("ABBA", "1976 - Arrival", "02 - Dancing Queen.opus")
	placementCopyRel = filepath.Join("ABBA", "1994 - Thank You For The Music [Compilation]", "1-07 - Dancing Queen.opus")
)

// seedPlacement stores "Dancing Queen" at home on Arrival and as a placement,
// with a lyrics sidecar, on the box set; a second box set track lives there at
// home.
func seedPlacement(t *testing.T, svc *Service, root string, catalog *mockCatalog, files *mockFiles) *fakePlacementStore {
	t.Helper()
	catalog.releases["rel-arrival"] = music.Release{ID: "rel-arrival", Title: "Arrival", AlbumArtist: "ABBA", Year: 1976}
	catalog.releases["rel-box"] = music.Release{ID: "rel-box", Title: "Thank You For The Music", AlbumArtist: "ABBA", Year: 1994, ReleaseType: music.ReleaseCompilation}
	catalog.tracks["t-dq"] = music.Track{ID: "t-dq", ReleaseID: "rel-arrival", Title: "Dancing Queen", Artists: []string{"ABBA"}}
	catalog.tracks["t-box"] = music.Track{ID: "t-box", ReleaseID: "rel-box", Title: "Only On The Box", Artists: []string{"ABBA"}}
	boxRel := filepath.Join(filepath.Dir(placementCopyRel), "1-08 - Only On The Box.opus")

	for rel, content := range map[string]string{placementHomeRel: "home", placementCopyRel: "copy", boxRel: "box"} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(storage.SidecarPathFor(filepath.Join(root, placementCopyRel), ".lrc"), []byte("[00:01.00]x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files.files[placementHomeRel] = music.File{ID: "f-dq", TrackID: "t-dq", Path: placementHomeRel}
	files.files[boxRel] = music.File{ID: "f-box", TrackID: "t-box", Path: boxRel}

	store := &fakePlacementStore{placed: []music.Placement{{
		ID: "p-1", TrackID: "t-dq", ReleaseID: "rel-box", Path: placementCopyRel, SourceFileID: "f-dq",
	}}}
	svc.placements = store
	return store
}

func exists(t *testing.T, root, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func TestDeleteTrackRemovesItsPlacementFiles(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	seedPlacement(t, svc, root, catalog, files)

	if err := svc.DeleteTrack(context.Background(), "t-dq"); err != nil {
		t.Fatalf("DeleteTrack: %v", err)
	}
	if exists(t, root, placementHomeRel) || exists(t, root, placementCopyRel) {
		t.Fatal("the home file or the placed copy stayed on disk")
	}
	if _, err := os.Stat(storage.SidecarPathFor(filepath.Join(root, placementCopyRel), ".lrc")); !os.IsNotExist(err) {
		t.Fatalf("the copy's lyrics sidecar stayed: %v", err)
	}
	if _, ok := catalog.tracks["t-box"]; !ok {
		t.Fatal("another track of the box set was deleted")
	}
}

func TestDeleteReleaseRemovesItsPlacementsAndKeepsRecordingsFromElsewhere(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	seedPlacement(t, svc, root, catalog, files)

	if err := svc.DeleteRelease(context.Background(), "rel-box"); err != nil {
		t.Fatalf("DeleteRelease: %v", err)
	}
	if exists(t, root, placementCopyRel) {
		t.Fatal("the placed copy on the deleted release stayed on disk")
	}
	if !exists(t, root, placementHomeRel) {
		t.Fatal("the recording's home file on another release was deleted")
	}
	if _, ok := catalog.tracks["t-dq"]; !ok {
		t.Fatal("a recording whose home is another release was deleted")
	}
	if _, ok := catalog.tracks["t-box"]; ok {
		t.Fatal("the box set's own track was kept")
	}
}

func TestDeleteReleaseRemovesThePlacementsOfItsRecordingsElsewhere(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	seedPlacement(t, svc, root, catalog, files)

	// Deleting Arrival deletes "Dancing Queen", whose placement row would
	// cascade away; its copy on the box set must not stay behind untracked.
	if err := svc.DeleteRelease(context.Background(), "rel-arrival"); err != nil {
		t.Fatalf("DeleteRelease: %v", err)
	}
	if exists(t, root, placementHomeRel) || exists(t, root, placementCopyRel) {
		t.Fatal("a file of the deleted recording stayed on disk")
	}
	if !exists(t, root, filepath.Join(filepath.Dir(placementCopyRel), "1-08 - Only On The Box.opus")) {
		t.Fatal("the box set's own file was deleted")
	}
}

func TestReconcileDoesNotReportPlacementsAsOrphans(t *testing.T) {
	svc, root, catalog, files, _, prober, _ := setupTestService(t)
	seedPlacement(t, svc, root, catalog, files)
	prober.probeFn = func(context.Context, string) (*downloader.AudioInfo, error) {
		return &downloader.AudioInfo{Codec: "opus", DurationMS: 180000, Channels: 2, SampleRate: 48000, SizeBytes: 1024}, nil
	}
	orphan := filepath.Join("ABBA", "1976 - Arrival", "99 - Stray.opus")
	if err := os.WriteFile(filepath.Join(root, orphan), []byte("stray"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var orphans []string
	for _, issue := range result.Issues {
		if issue.Status == StatusOrphanFile {
			orphans = append(orphans, issue.Path)
		}
		if issue.Path == placementCopyRel {
			t.Fatalf("the placement was reported: %+v", issue)
		}
	}
	if len(orphans) != 1 || orphans[0] != orphan {
		t.Fatalf("orphans = %v, want only %s", orphans, orphan)
	}
}

func TestPlacementsCountAsRegisteredForQuarantine(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	seedPlacement(t, svc, root, catalog, files)

	if !svc.registeredInCatalog(context.Background(), placementCopyRel) {
		t.Fatal("a placement file counted as unregistered")
	}
	if svc.registeredInCatalog(context.Background(), filepath.Join("ABBA", "nothing.opus")) {
		t.Fatal("an unknown path counted as registered")
	}
}
