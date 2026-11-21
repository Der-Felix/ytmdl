package repository

import (
	"context"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// seedHomeDownload stores "Dancing Queen" on its home release "Arrival" with
// one file, the way a finished download does.
func seedHomeDownload(t *testing.T, catalog *Catalog) music.StoredEntry {
	t.Helper()
	stored, err := catalog.PersistDownload(context.Background(), music.LibraryEntry{
		Artist: &music.Artist{Name: "ABBA", Provider: "deezer", SourceID: "artist:abba"},
		Release: &music.Release{
			Title: "Arrival", AlbumArtist: "ABBA", ReleaseType: music.ReleaseAlbum,
			Provider: "deezer", SourceID: "rel-arrival",
		},
		Track: music.Track{
			Title: "Dancing Queen", Artists: []string{"ABBA"}, AlbumArtist: "ABBA",
			Album: "Arrival", TrackNumber: 2, DurationMS: 230000, ISRC: "SEAYD7601020",
			SourceProvider: "deezer", SourceID: "tr-dq",
		},
		File: music.File{Path: "ABBA/1976 - Arrival/02 - Dancing Queen.opus", Codec: "opus", SizeBytes: 4096, DurationMS: 230000},
	}, 4000)
	if err != nil {
		t.Fatalf("persist home download: %v", err)
	}
	return stored
}

func boxSetEntry(trackID, fileID string) music.PlacementEntry {
	return music.PlacementEntry{
		Artist: &music.Artist{Name: "ABBA", Provider: "deezer", SourceID: "artist:abba"},
		Release: music.Release{
			Title: "Thank You For The Music", AlbumArtist: "ABBA", ReleaseType: music.ReleaseCompilation,
			Provider: "deezer", SourceID: "rel-tyftm",
		},
		Placement: music.Placement{
			TrackID: trackID, Path: "ABBA/Thank You For The Music (Compilation)/1-07 - Dancing Queen.opus",
			SizeBytes: 4096, SHA256: "abc", DiscNumber: 1, TrackNumber: 7, Title: "Dancing Queen",
			SourceFileID: fileID,
		},
	}
}

func TestPlacementsPersistCreatesTheReleaseAndReadsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	catalog := NewCatalog(db)
	placements := NewPlacements(db)
	home := seedHomeDownload(t, catalog)

	stored, err := placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID))
	if err != nil {
		t.Fatalf("persist placement: %v", err)
	}
	if stored.ID == "" || stored.ReleaseID == "" || stored.ReleaseID == home.ReleaseID {
		t.Fatalf("placement = %+v, want its own release", stored)
	}
	release, err := catalog.FindReleaseBySource(ctx, "deezer", "rel-tyftm")
	if err != nil || release == nil || release.ID != stored.ReleaseID {
		t.Fatalf("box set release = %+v, %v; want the placement's release %s", release, err, stored.ReleaseID)
	}

	byPath, err := placements.FindByPath(ctx, stored.Path)
	if err != nil || byPath == nil || byPath.ID != stored.ID {
		t.Fatalf("FindByPath = %+v, %v", byPath, err)
	}
	if byPath.ReleaseProvider != "deezer" || byPath.ReleaseSourceID != "rel-tyftm" ||
		byPath.SourceFileID != home.File.ID || byPath.TrackNumber != 7 || byPath.DiscNumber != 1 {
		t.Fatalf("read back %+v", byPath)
	}
	if missing, err := placements.FindByPath(ctx, "ABBA/nothing.opus"); err != nil || missing != nil {
		t.Fatalf("FindByPath(unknown) = %+v, %v; want nil", missing, err)
	}

	for name, list := range map[string]func() ([]music.Placement, error){
		"by track":   func() ([]music.Placement, error) { return placements.ListByTrack(ctx, home.TrackID) },
		"by release": func() ([]music.Placement, error) { return placements.ListByRelease(ctx, stored.ReleaseID) },
		"by track and release": func() ([]music.Placement, error) {
			return placements.FindByTrackAndRelease(ctx, home.TrackID, stored.ReleaseID)
		},
		"all": func() ([]music.Placement, error) { return placements.ListAll(ctx) },
	} {
		got, err := list()
		if err != nil || len(got) != 1 || got[0].ID != stored.ID {
			t.Fatalf("list %s = %+v, %v; want the one placement", name, got, err)
		}
	}
	if got, err := placements.FindByTrackAndRelease(ctx, home.TrackID, home.ReleaseID); err != nil || len(got) != 0 {
		t.Fatalf("placements on the home release = %+v, %v; want none", got, err)
	}

	homeRelease, err := placements.HomeRelease(ctx, home.TrackID)
	if err != nil || homeRelease == nil || homeRelease.ID != home.ReleaseID || homeRelease.SourceID != "rel-arrival" {
		t.Fatalf("HomeRelease = %+v, %v; want Arrival", homeRelease, err)
	}
	// The home release and the file are untouched by the placement.
	track, err := catalog.GetTrack(ctx, home.TrackID)
	if err != nil || track.ReleaseID != home.ReleaseID {
		t.Fatalf("track after placement = %+v, %v", track, err)
	}
	files, err := NewFiles(db).ListByTrack(ctx, home.TrackID)
	if err != nil || len(files) != 1 {
		t.Fatalf("files after placement = %+v, %v; want only the home file", files, err)
	}
}

func TestPlacementsPersistTwiceUpdatesTheSamePosition(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	placements := NewPlacements(db)
	home := seedHomeDownload(t, NewCatalog(db))

	first, err := placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID))
	if err != nil {
		t.Fatal(err)
	}
	again := boxSetEntry(home.TrackID, home.File.ID)
	again.Placement.Path = "ABBA/Thank You For The Music (Compilation)/1-07 - Dancing Queen [source-x].opus"
	again.Placement.SHA256 = "def"
	second, err := placements.Persist(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("second persist created %s, want the row %s updated", second.ID, first.ID)
	}
	all, err := placements.ListAll(ctx)
	if err != nil || len(all) != 1 || all[0].Path != again.Placement.Path || all[0].SHA256 != "def" {
		t.Fatalf("placements = %+v, %v", all, err)
	}
}

func TestPlacementsPathIsUnique(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	placements := NewPlacements(db)
	home := seedHomeDownload(t, NewCatalog(db))

	if _, err := placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID)); err != nil {
		t.Fatal(err)
	}
	other := boxSetEntry(home.TrackID, home.File.ID)
	other.Placement.TrackNumber = 8
	_, err := placements.Persist(ctx, other)
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("persist at a taken path = %v, want PATH_CONFLICT", err)
	}
}

func TestPlacementsFollowTheirTrackAndRelease(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	catalog := NewCatalog(db)
	placements := NewPlacements(db)

	home := seedHomeDownload(t, catalog)
	stored, err := placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := placements.Delete(ctx, stored.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := placements.ListAll(ctx); len(got) != 0 {
		t.Fatalf("after Delete: %+v", got)
	}

	// Deleting the release a placement lives on removes the placement and
	// leaves the recording, whose home is elsewhere, alone.
	stored, err = placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.DeleteRelease(ctx, stored.ReleaseID); err != nil {
		t.Fatal(err)
	}
	if got, _ := placements.ListAll(ctx); len(got) != 0 {
		t.Fatalf("after deleting the release: %+v", got)
	}
	if _, err := catalog.GetTrack(ctx, home.TrackID); err != nil {
		t.Fatalf("recording gone with its placement's release: %v", err)
	}

	// Deleting the recording removes its placements.
	if _, err := placements.Persist(ctx, boxSetEntry(home.TrackID, home.File.ID)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.DeleteTrack(ctx, home.TrackID); err != nil {
		t.Fatal(err)
	}
	if got, _ := placements.ListAll(ctx); len(got) != 0 {
		t.Fatalf("after deleting the track: %+v", got)
	}
}

func TestPlacementsHomeReleaseOfATrackWithoutOne(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	stored, err := NewCatalog(db).PersistDownload(ctx, music.LibraryEntry{
		Track: music.Track{Title: "Loose", Artists: []string{"Someone"}, DurationMS: 100000},
		File:  music.File{Path: "Someone/Loose.opus"},
	}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	release, err := NewPlacements(db).HomeRelease(ctx, stored.TrackID)
	if err != nil || release != nil {
		t.Fatalf("HomeRelease = %+v, %v; want nil", release, err)
	}
}
