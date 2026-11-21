package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// TestQuickAuditTreatsPlacementsAsCatalogued: a placement file is neither
// untracked nor a file of its recording in the wrong place, and it counts
// towards the completeness of the release it was placed on.
func TestQuickAuditTreatsPlacementsAsCatalogued(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	catalog := repository.NewCatalog(db)
	files := repository.NewFiles(db)
	placements := repository.NewPlacements(db)
	auditRepo := repository.NewAudit(db)
	svc, err := library.NewService(library.ServiceOptions{
		Library: lib, Files: files, Catalog: catalog, Placements: placements,
		Prober: &mockProber{}, Audit: auditRepo,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	arrival := music.Release{Title: "Arrival", AlbumArtist: "ABBA", Artists: []string{"ABBA"}, ReleaseType: music.ReleaseAlbum, Year: 1976, TrackCount: 1, Provider: "deezer", SourceID: "rel-arrival"}
	dq := music.Track{Title: "Dancing Queen", Artists: []string{"ABBA"}, AlbumArtist: "ABBA", Album: "Arrival", TrackNumber: 2, DiscNumber: 1, DurationMS: 230000, Year: 1976, ISRC: "SEAYD7601020"}
	homeRel, err := filepath.Rel(root, mustTrackPath(t, lib, arrival, dq))
	if err != nil {
		t.Fatal(err)
	}
	home, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist:  &music.Artist{Name: "ABBA", Provider: "deezer", SourceID: "artist:abba"},
		Release: &arrival, Track: dq,
		File: music.File{Path: homeRel, SizeBytes: 4, DurationMS: 230000, Codec: "opus"},
	}, 4000)
	if err != nil {
		t.Fatal(err)
	}

	box := music.Release{Title: "Thank You For The Music", AlbumArtist: "ABBA", Artists: []string{"ABBA"}, ReleaseType: music.ReleaseCompilation, Year: 1994, TrackCount: 1, Provider: "deezer", SourceID: "rel-box"}
	boxTrack := dq
	boxTrack.Album, boxTrack.Year, boxTrack.TrackNumber = box.Title, 1994, 7
	copyRel, err := filepath.Rel(root, mustTrackPath(t, lib, box, boxTrack))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := placements.Persist(ctx, music.PlacementEntry{
		Release: box,
		Placement: music.Placement{TrackID: home.TrackID, Path: copyRel, DiscNumber: 1, TrackNumber: 7,
			Title: "Dancing Queen", SourceFileID: home.File.ID},
	}); err != nil {
		t.Fatal(err)
	}

	stray := filepath.Join("ABBA", "Stray", "01 - Stray.opus")
	for _, rel := range []string{homeRel, copyRel, stray} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("OggS"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := svc.WaitForAudit(ctx, run.ID, 20*time.Second)
	if err != nil || done.Status != music.AuditRunCompleted {
		t.Fatalf("audit = %+v, %v", done, err)
	}
	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var untracked []string
	for _, f := range findings {
		t.Logf("finding %s %s %s", f.FindingCode, f.RelativePath, f.ReleaseTitle)
		switch {
		case f.FindingCode == music.FindingFileUntracked:
			untracked = append(untracked, f.RelativePath)
		case f.RelativePath == copyRel:
			t.Fatalf("the placement was reported: %+v", f)
		case f.FindingCode == music.FindingReleaseIncomplete:
			t.Fatalf("a release was reported incomplete: %+v", f)
		}
	}
	if len(untracked) != 1 || untracked[0] != stray {
		t.Fatalf("untracked = %v, want only %s", untracked, stray)
	}
}

func mustTrackPath(t *testing.T, lib *storage.Library, release music.Release, track music.Track) string {
	t.Helper()
	path, err := lib.TrackPath(release, track, ".opus")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
