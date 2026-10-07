package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func TestRecoveredRecordingIsPrimaryWithoutDuplicatingLibraryRows(t *testing.T) {
	db, playlists, user, _, ids := setupPlaylistTest(t)
	ctx := context.Background()
	files := repository.NewFiles(db)
	catalog := repository.NewCatalog(db)
	id := ids[0]
	if _, err := db.ExecContext(ctx, `UPDATE files SET duration_ms=210000 WHERE track_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	correct, err := files.Upsert(ctx, music.File{TrackID: id, Path: "zzz-correct.opus", DurationMS: 61000, SizeBytes: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.Upsert(ctx, music.File{TrackID: id, Path: "aaa-unknown.opus", SizeBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	allFiles, err := files.ListByTrack(ctx, id)
	if err != nil || len(allFiles) != 3 || allFiles[0].ID != correct.ID {
		t.Fatalf("stream selection must prefer verified audio while retaining originals: %+v, %v", allFiles, err)
	}
	store, err := storage.NewLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range allFiles {
		path := filepath.Join(store.Root(), file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("isolated streaming selection fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := library.NewService(library.ServiceOptions{Library: store, Catalog: catalog, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Stop)
	_, primary, err := svc.StreamTrack(ctx, id)
	if err != nil || primary.ID != correct.ID {
		t.Fatalf("track playback selected preserved wrong recording: %+v, %v", primary, err)
	}
	_, original, err := svc.StreamFile(ctx, allFiles[2].ID)
	if err != nil || original.ID != allFiles[2].ID {
		t.Fatalf("original file became inaccessible: %+v, %v", original, err)
	}
	trackDetail, err := catalog.GetLibraryTrackDetail(ctx, id)
	if err != nil || trackDetail.File == nil || trackDetail.File.ID != correct.ID {
		t.Fatalf("track detail selected old recording: %+v, %v", trackDetail, err)
	}
	tracks, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{})
	if err != nil || total != 4 || len(tracks) != 4 {
		t.Fatalf("physical copies inflated library pagination: %d, %d, %v", total, len(tracks), err)
	}
	for _, tr := range tracks {
		if tr.ID == id && tr.FilePath != correct.Path {
			t.Fatalf("library selected old recording: %+v", tr)
		}
	}
	results, err := catalog.SearchLibrary(ctx, "Computerwelt", 10)
	if err != nil || len(results.Tracks) != 1 || results.Tracks[0].FilePath != correct.Path || len(results.Artists) != 0 {
		t.Fatalf("search duplicated or selected old recording: %+v, %v", results, err)
	}
	artists, _, err := catalog.ListArtistsFiltered(ctx, repository.ArtistListFilter{})
	if err != nil || len(artists) != 1 || artists[0].TrackCount != 4 {
		t.Fatalf("artist track count included physical copies: %+v, %v", artists, err)
	}
	artistResults, err := catalog.SearchArtists(ctx, "Kraftwerk", 10)
	if err != nil || len(artistResults) != 1 || artistResults[0].TrackCount != 4 {
		t.Fatalf("artist search counted physical copies: %+v, %v", artistResults, err)
	}
	pl, err := playlists.CreatePlaylist(ctx, user, "Recovered audio", "")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := playlists.AddTrack(ctx, user, pl.ID, id)
	if err != nil || detail.TrackCount != 1 || len(detail.Tracks) != 1 || detail.Tracks[0].FilePath != correct.Path {
		t.Fatalf("playlist duplicated the repaired recording: %+v, %v", detail, err)
	}
	if err := playlists.FavoriteTrack(ctx, user, id); err != nil {
		t.Fatal(err)
	}
	favorites, err := playlists.ListFavoriteTracks(ctx, user)
	if err != nil || len(favorites) != 1 || favorites[0].FilePath != correct.Path {
		t.Fatalf("favorites duplicated the repaired recording: %+v, %v", favorites, err)
	}
	// Unknown metadata remains playable; no old file is deleted by selection.
	if err := files.Delete(ctx, correct.ID); err != nil {
		t.Fatal(err)
	}
	allFiles, err = files.ListByTrack(ctx, id)
	if err != nil || len(allFiles) != 2 || allFiles[0].Path != "aaa-unknown.opus" {
		t.Fatalf("unknown-duration fallback was lost: %+v, %v", allFiles, err)
	}
}
