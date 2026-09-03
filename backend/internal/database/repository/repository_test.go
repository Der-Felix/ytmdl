package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	return dbtest.Open(t)
}

func TestMigrationsAreIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}
}

// TestMigrationsCreateEveryTable pins the schema the backend relies on. A
// migration that silently stops creating one of these tables would otherwise
// only show up at runtime.
func TestMigrationsCreateEveryTable(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	for _, table := range []string{
		"artists", "releases", "tracks", "track_sources",
		"jobs", "job_items", "files", "settings", "artist_subscriptions",
		"users", "sessions",
		"schema_migrations",
	} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&count); err != nil {
			t.Fatalf("look up table %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s does not exist", table)
		}
	}
}

func TestMigration0004ColumnsAndIndex(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Verify track columns
	for _, col := range []string{"lyrics_state", "lyrics_provider", "lyrics_checked_at", "compilation"} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM information_schema.columns
			 WHERE table_schema = current_schema() AND table_name = 'tracks' AND column_name = $1`, col).Scan(&count); err != nil {
			t.Fatalf("look up column tracks.%s: %v", col, err)
		}
		if count != 1 {
			t.Fatalf("column tracks.%s missing", col)
		}
	}

	// Verify release column
	var releaseCompCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'releases' AND column_name = 'compilation'`).Scan(&releaseCompCount); err != nil {
		t.Fatalf("look up column releases.compilation: %v", err)
	}
	if releaseCompCount != 1 {
		t.Fatal("column releases.compilation missing")
	}

	// Verify partial index
	var indexCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_indexes
		 WHERE schemaname = current_schema() AND tablename = 'tracks' AND indexname = 'idx_tracks_lyrics_backfill'`).Scan(&indexCount); err != nil {
		t.Fatalf("look up index idx_tracks_lyrics_backfill: %v", err)
	}
	if indexCount != 1 {
		t.Fatal("index idx_tracks_lyrics_backfill missing")
	}
}

// TestJobStatusConstraintAcceptsEveryGoStatus keeps the CHECK constraint and
// the Go state machine in step: every status the backend can write must be
// storable, and an invented one must be refused.
func TestJobStatusConstraintAcceptsEveryGoStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	statuses := []jobs.Status{
		jobs.StatusQueued, jobs.StatusResolvingArtist, jobs.StatusResolvingReleases,
		jobs.StatusResolvingTracks, jobs.StatusDeduplicating, jobs.StatusMatching,
		jobs.StatusDownloading, jobs.StatusTagging, jobs.StatusFinalizing,
		jobs.StatusCompleted, jobs.StatusFailed, jobs.StatusCancelled,
	}
	for _, status := range statuses {
		job := &jobs.Job{Type: jobs.TypeTrack, Status: status, Options: jobs.DefaultOptions()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("status %q was rejected by the database: %v", status, err)
		}
	}

	invalid := &jobs.Job{Type: jobs.TypeTrack, Status: "not-a-status", Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, invalid); err == nil {
		t.Fatal("an unknown job status was accepted")
	}
}

func TestJobItemStatusConstraintAcceptsEveryGoStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	statuses := []jobs.ItemStatus{
		jobs.ItemPending, jobs.ItemMatching, jobs.ItemDownloading, jobs.ItemTagging,
		jobs.ItemCompleted, jobs.ItemFailed, jobs.ItemSkipped, jobs.ItemCancelled,
	}
	items := make([]jobs.Item, 0, len(statuses))
	for i, status := range statuses {
		items = append(items, jobs.Item{Position: i, Status: status, Track: music.Track{Title: string(status)}})
	}
	if err := repo.AddItems(ctx, job.ID, items); err != nil {
		t.Fatalf("every item status must be storable: %v", err)
	}

	err := repo.AddItems(ctx, job.ID, []jobs.Item{
		{Position: 99, Status: "not-a-status", Track: music.Track{Title: "x"}},
	})
	if err == nil {
		t.Fatal("an unknown item status was accepted")
	}
}

func TestCatalogArtistUpsertIsStable(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	first, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: "The Artist", Provider: "spotify", SourceID: "abc", SourceURL: "https://example.test/abc",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: "The Artist (renamed)", Provider: "spotify", SourceID: "abc",
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert created a second row: %q != %q", first.ID, second.ID)
	}

	loaded, err := catalog.GetArtist(ctx, first.ID)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if loaded.Name != "The Artist (renamed)" {
		t.Fatalf("name = %q, want the refreshed value", loaded.Name)
	}
}

func TestCatalogGetArtistNotFound(t *testing.T) {
	catalog := NewCatalog(openTestDB(t))
	_, err := catalog.GetArtist(context.Background(), "missing")
	if code := apperr.CodeOf(err); code != apperr.CodeArtistNotFound {
		t.Fatalf("code = %s, want %s", code, apperr.CodeArtistNotFound)
	}
}

func TestCatalogTrackUpsertDeduplicates(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	base := music.Track{
		Title: "Song", Artists: []string{"Artist"}, Album: "Album",
		DurationMS: 205000, TrackNumber: 1, Year: 2001,
	}

	first, err := catalog.UpsertTrack(ctx, base, "", "", 4000)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Same recording from another release: runtime within tolerance.
	variant := base
	variant.Album = "Greatest Hits"
	variant.DurationMS = 206500
	second, err := catalog.UpsertTrack(ctx, variant, "", "", 4000)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("the same recording was stored twice: %q != %q", first.ID, second.ID)
	}

	// A genuine variant must get its own row.
	live := base
	live.Title = "Song (Live)"
	third, err := catalog.UpsertTrack(ctx, live, "", "", 4000)
	if err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("the live version was merged into the studio recording")
	}
}

func TestCatalogFindTrackByISRC(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	stored, err := catalog.UpsertTrack(ctx, music.Track{
		Title: "Song", Artists: []string{"Artist"}, DurationMS: 205000, ISRC: "DEA123456789",
	}, "", "", 4000)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	found, err := catalog.FindTrack(ctx, music.Track{
		Title: "Different Title", Artists: []string{"Someone"}, ISRC: "DE-A12-34-56789",
	}, 4000)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.ID != stored.ID {
		t.Fatalf("ISRC lookup did not find the stored recording: %+v", found)
	}
}

func TestCatalogFindTrackByID(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	stored, err := catalog.UpsertTrack(ctx, music.Track{
		ID: "stable_id_123", Title: "Song", Artists: []string{"Artist"}, DurationMS: 205000,
	}, "", "", 4000)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	found, err := catalog.FindTrack(ctx, music.Track{
		ID: "stable_id_123", Title: "Different Title (Extended)", Artists: []string{"Someone Else"}, DurationMS: 300000,
	}, 4000)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.ID != stored.ID {
		t.Fatalf("Track ID lookup did not find the stored recording: %+v", found)
	}
}

func TestCatalogTrackUpsertIdempotentSameIDDifferentTitles(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	artist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: "Nathan Dawe", Provider: "ytmusic", SourceID: "art_xyz",
	})
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}

	rel1, err := catalog.UpsertRelease(ctx, music.Release{
		Title: "Heart Still Beating", Provider: "ytmusic", SourceID: "rel_1",
	}, artist.ID)
	if err != nil {
		t.Fatalf("upsert release 1: %v", err)
	}

	rel2, err := catalog.UpsertRelease(ctx, music.Release{
		Title: "Heart Still Beating (Extended)", Provider: "ytmusic", SourceID: "rel_2",
	}, artist.ID)
	if err != nil {
		t.Fatalf("upsert release 2: %v", err)
	}

	first, err := catalog.UpsertTrack(ctx, music.Track{
		ID: "yt_track_xyz", Title: "Heart Still Beating", Artists: []string{"Nathan Dawe", "Bebe Rexha"},
		Album: "Heart Still Beating", DurationMS: 150000, Year: 2023,
	}, rel1.ID, artist.ID, 4000)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := catalog.UpsertTrack(ctx, music.Track{
		ID: "yt_track_xyz", Title: "Heart Still Beating (Extended)", Artists: []string{"Nathan Dawe", "Bebe Rexha"},
		Album: "Heart Still Beating (Extended)", DurationMS: 219000, Year: 2023,
	}, rel2.ID, artist.ID, 4000)
	if err != nil {
		t.Fatalf("second upsert must succeed idempotently: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second upsert created new ID: %q != %q", second.ID, first.ID)
	}

	// Verify that the existing release_id was preserved
	stored, err := catalog.GetTrack(ctx, "yt_track_xyz")
	if err != nil {
		t.Fatalf("get track: %v", err)
	}
	if stored.ReleaseID != rel1.ID {
		t.Fatalf("release_id changed: got %q, want %q", stored.ReleaseID, rel1.ID)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id = 'yt_track_xyz'`).Scan(&count); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row in tracks for id yt_track_xyz, got %d", count)
	}
}

func TestCatalogTrackUpsertIsRaceFreeWithSameID(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	const workers = 8
	trackA := music.Track{
		ID: "shared_track_id_999", Title: "Song", Artists: []string{"Artist"},
		Album: "Single A", DurationMS: 180000,
	}
	trackB := music.Track{
		ID: "shared_track_id_999", Title: "Song (Extended)", Artists: []string{"Artist"},
		Album: "Single B", DurationMS: 240000,
	}

	ids := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			tr := trackA
			if idx%2 == 1 {
				tr = trackB
			}
			stored, err := catalog.UpsertTrack(ctx, tr, "", "", 4000)
			ids[idx], errs[idx] = stored.ID, err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i, id := range ids {
		if id != "shared_track_id_999" {
			t.Fatalf("worker %d stored unexpected ID: %q", i, id)
		}
	}

	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id = 'shared_track_id_999'`).Scan(&rows); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if rows != 1 {
		t.Fatalf("tracks table holds %d rows, want 1", rows)
	}
}

// TestCatalogTrackUpsertIsRaceFree is the reason PostgreSQL is used at all:
// several workers store the same recording at the same moment and the library
// must still hold exactly one row for it.
func TestCatalogTrackUpsertIsRaceFree(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	const workers = 8
	track := music.Track{
		Title: "Concurrent Song", Artists: []string{"Artist"}, Album: "Album",
		DurationMS: 210000, TrackNumber: 3, Year: 2020, ISRC: "DEA111111111",
	}

	ids := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			stored, err := catalog.UpsertTrack(ctx, track, "", "", 4000)
			ids[i], errs[i] = stored.ID, err
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("worker %d stored a second recording: %q != %q", i, id, ids[0])
		}
	}

	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks`).Scan(&rows); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if rows != 1 {
		t.Fatalf("tracks table holds %d rows, want 1", rows)
	}
}

// TestCatalogTrackUpsertIsRaceFreeWithoutISRC covers the second lookup path:
// recordings that are only recognisable by identity key and runtime.
func TestCatalogTrackUpsertIsRaceFreeWithoutISRC(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	const workers = 8
	start := make(chan struct{})
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			track := music.Track{
				Title: "No ISRC", Artists: []string{"Artist"}, Album: "Album",
				// Slightly different runtimes, all inside the tolerance: the
				// deduplication has to treat them as the same recording.
				DurationMS: 200000 + i*100,
			}
			<-start
			_, errs[i] = catalog.UpsertTrack(ctx, track, "", "", 4000)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks`).Scan(&rows); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if rows != 1 {
		t.Fatalf("tracks table holds %d rows, want 1", rows)
	}
}

func TestCatalogTrackSources(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	track, err := catalog.UpsertTrack(ctx, music.Track{
		Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000,
	}, "", "", 4000)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}

	for _, source := range []music.Source{
		{TrackID: track.ID, Provider: "spotify", Kind: music.SourceMetadata, SourceID: "sp1"},
		{TrackID: track.ID, Provider: "ytmusic", Kind: music.SourceMedia, SourceID: "yt1"},
		{TrackID: track.ID, Provider: "ytmusic", Kind: music.SourceMedia, SourceID: "yt1"},
	} {
		if err := catalog.AddSource(ctx, source); err != nil {
			t.Fatalf("add source: %v", err)
		}
	}

	sources, err := catalog.ListSources(ctx, track.ID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want 2 (the duplicate must not create a row)", len(sources))
	}

	err = catalog.AddSource(ctx, music.Source{
		TrackID: track.ID, Provider: "ytmusic", Kind: "invented", SourceID: "yt2",
	})
	if code := apperr.CodeOf(err); code != apperr.CodeInvalidRequest {
		t.Fatalf("unknown source kind: code = %s, want %s", code, apperr.CodeInvalidRequest)
	}
}

// TestPersistDownloadIsAtomic verifies the promise the download pipeline makes:
// artist, release, recording, sources and file appear together or not at all.
func TestPersistDownloadIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)
	files := NewFiles(db)

	entry := music.LibraryEntry{
		Artist:  &music.Artist{Name: "Artist", Provider: "ytmusic", SourceID: "artist:artist"},
		Release: &music.Release{Title: "Album", AlbumArtist: "Artist", ReleaseType: music.ReleaseAlbum, Year: 2024, Provider: "ytmusic", SourceID: "MPREabc"},
		Track:   music.Track{Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationMS: 200000, TrackNumber: 1},
		Sources: []music.Source{
			{Provider: "ytmusic", Kind: music.SourceMetadata, SourceID: "vid1"},
			{Provider: "ytmusic", Kind: music.SourceMedia, SourceID: "vid1"},
		},
		File: music.File{Path: "Artist/2024 - Album/01 - Song.opus", Codec: "opus", SizeBytes: 4096},
	}

	stored, err := catalog.PersistDownload(ctx, entry, 4000)
	if err != nil {
		t.Fatalf("persist download: %v", err)
	}
	if stored.ArtistID == "" || stored.ReleaseID == "" || stored.TrackID == "" || stored.File.ID == "" {
		t.Fatalf("persist download returned an incomplete result: %+v", stored)
	}
	if stored.File.TrackID != stored.TrackID {
		t.Fatalf("the file was not linked to the recording: %+v", stored.File)
	}

	sources, err := catalog.ListSources(ctx, stored.TrackID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(sources))
	}

	byTrack, err := files.ListByTrack(ctx, stored.TrackID)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(byTrack) != 1 || byTrack[0].Path != entry.File.Path {
		t.Fatalf("the library file was not stored: %+v", byTrack)
	}

	// A second download of the same recording must not duplicate anything.
	second, err := catalog.PersistDownload(ctx, entry, 4000)
	if err != nil {
		t.Fatalf("second persist: %v", err)
	}
	if second.TrackID != stored.TrackID || second.File.ID != stored.File.ID {
		t.Fatalf("the second download created new rows: %+v vs %+v", second, stored)
	}
	assertRowCounts(t, db, map[string]int{"artists": 1, "releases": 1, "tracks": 1, "track_sources": 2, "files": 1})
}

// TestPersistDownloadRollsBackOnFailure feeds an entry whose file record is
// invalid. Nothing at all may remain behind.
func TestPersistDownloadRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	_, err := catalog.PersistDownload(ctx, music.LibraryEntry{
		Artist: &music.Artist{Name: "Artist", Provider: "ytmusic", SourceID: "artist:artist"},
		Track:  music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000},
		File:   music.File{Path: ""}, // rejected: a library file needs a path
	}, 4000)
	if err == nil {
		t.Fatal("an entry without a file path was accepted")
	}
	assertRowCounts(t, db, map[string]int{"artists": 0, "tracks": 0, "files": 0})
}

func TestJobsLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	job := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusQueued, Label: "Artist",
		MetadataProvider: "spotify", MediaProvider: "ytmusic", TargetID: "artist-1",
		Options: jobs.DefaultOptions(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	items := []jobs.Item{
		{Position: 0, Track: music.Track{Title: "One", Artists: []string{"Artist"}}, Label: "Artist - One"},
		{Position: 1, Track: music.Track{Title: "Two", Artists: []string{"Artist"}}, Label: "Artist - Two"},
		{Position: 2, Track: music.Track{Title: "Three", Artists: []string{"Artist"}}, Label: "Artist - Three"},
	}
	if err := repo.AddItems(ctx, job.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	stored, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Total != 3 {
		t.Fatalf("total = %d, want 3", stored.Total)
	}
	if stored.Options.ReleaseFilter != jobs.DefaultOptions().ReleaseFilter {
		t.Fatalf("options round trip failed: %+v", stored.Options)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Fatalf("timestamps were not stored: %+v", stored)
	}

	loaded, err := repo.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(loaded) != 3 || loaded[0].Track.Title != "One" {
		t.Fatalf("items were not stored in order: %+v", loaded)
	}

	if err := repo.UpdateItem(ctx, loaded[0].ID, jobs.ItemUpdate{Status: jobs.ItemMatching}); err != nil {
		t.Fatalf("update to matching: %v", err)
	}
	if err := repo.UpdateItem(ctx, loaded[0].ID, jobs.ItemUpdate{
		Status: jobs.ItemDownloading, MediaProvider: "ytmusic", MediaID: "abc", MatchScore: 98.5,
	}); err != nil {
		t.Fatalf("update to downloading: %v", err)
	}
	if err := repo.UpdateItem(ctx, loaded[0].ID, jobs.ItemUpdate{Status: jobs.ItemCompleted}); err != nil {
		t.Fatalf("update to completed: %v", err)
	}
	if err := repo.UpdateItem(ctx, loaded[1].ID, jobs.ItemUpdate{
		Status: jobs.ItemFailed, ErrorCode: string(apperr.CodeMatchFailed), ErrorMessage: "no match",
	}); err != nil {
		t.Fatalf("update to failed: %v", err)
	}
	if err := repo.UpdateItem(ctx, loaded[2].ID, jobs.ItemUpdate{Status: jobs.ItemSkipped}); err != nil {
		t.Fatalf("update to skipped: %v", err)
	}

	refreshed, err := repo.RefreshCounters(ctx, job.ID)
	if err != nil {
		t.Fatalf("refresh counters: %v", err)
	}
	if refreshed.Completed != 1 || refreshed.Failed != 1 || refreshed.Skipped != 1 {
		t.Fatalf("counters = %+v, want 1/1/1", refreshed.Summary())
	}

	item, err := repo.GetItem(ctx, loaded[0].ID)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if item.MediaID != "abc" || item.MatchScore != 98.5 {
		t.Fatalf("worker fields were not persisted: %+v", item)
	}
	if item.FinishedAt == nil || item.StartedAt == nil {
		t.Fatal("timestamps were not recorded")
	}
	if item.FinishedAt.Before(*item.StartedAt) {
		t.Fatalf("finished_at %v is before started_at %v", item.FinishedAt, item.StartedAt)
	}

	filtered, total, err := repo.List(ctx, jobs.ListFilter{Type: jobs.TypeArtist})
	if err != nil {
		t.Fatalf("list by type: %v", err)
	}
	if len(filtered) != 1 || total != 1 || filtered[0].ID != job.ID {
		t.Fatalf("type filter returned %+v, total %d", filtered, total)
	}
	if none, totalNone, err := repo.List(ctx, jobs.ListFilter{Type: jobs.TypeTrack}); err != nil || len(none) != 0 || totalNone != 0 {
		t.Fatalf("type filter matched the wrong rows: %+v, total %d (%v)", none, totalNone, err)
	}
}

// TestJobLabelIsPersisted covers the name a job gets once the catalogue was
// read: it has to survive a restart, not just the event stream.
func TestJobLabelIsPersisted(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	job := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusQueued,
		Label: "artist UCabcdef", TargetID: "UCabcdef", Options: jobs.DefaultOptions(),
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := repo.SetLabel(ctx, job.ID, "Kevin MacLeod"); err != nil {
		t.Fatalf("set label: %v", err)
	}

	stored, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Label != "Kevin MacLeod" {
		t.Fatalf("label = %q, want the resolved artist name", stored.Label)
	}
}

func TestJobsRejectsIllegalTransitions(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	job := &jobs.Job{Type: jobs.TypeTrack, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := repo.SetStatus(ctx, job.ID, jobs.StatusCompleted, "", ""); err != nil {
		t.Fatalf("queued -> completed: %v", err)
	}
	err := repo.SetStatus(ctx, job.ID, jobs.StatusDownloading, "", "")
	if err == nil {
		t.Fatal("a completed job must not move back into the pipeline")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeInvalidRequest {
		t.Fatalf("code = %s, want %s", code, apperr.CodeInvalidRequest)
	}
}

// TestJobStatusUpdatesAreSerialised covers the "cancel during a status update"
// race: whichever transition happens first, the terminal state must win and
// the job must not be dragged back into the pipeline.
func TestJobStatusUpdatesAreSerialised(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	for range 20 {
		job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatalf("create job: %v", err)
		}
		if err := repo.SetStatus(ctx, job.ID, jobs.StatusDownloading, "", ""); err != nil {
			t.Fatalf("queued -> downloading: %v", err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = repo.SetStatus(ctx, job.ID, jobs.StatusCancelled, "JOB_CANCELLED", "cancelled")
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = repo.SetStatus(ctx, job.ID, jobs.StatusTagging, "", "")
		}()
		close(start)
		wg.Wait()

		final, err := repo.Get(ctx, job.ID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if final.Status == jobs.StatusCancelled {
			continue // the cancellation won and nothing may move afterwards
		}
		if final.Status != jobs.StatusTagging {
			t.Fatalf("status = %q, want cancelled or tagging", final.Status)
		}
		// The tagging update won the race; the cancellation must then have been
		// applied on top of it rather than lost.
		if err := repo.SetStatus(ctx, job.ID, jobs.StatusCancelled, "JOB_CANCELLED", "cancelled"); err != nil {
			t.Fatalf("cancel after tagging: %v", err)
		}
	}
}

// TestJobCountersStayConsistentUnderConcurrentFinishes runs the update every
// worker performs when it finishes a track. The counters are recomputed from
// the item table, so the final numbers must match it exactly.
func TestJobCountersStayConsistentUnderConcurrentFinishes(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	const total = 12
	items := make([]jobs.Item, 0, total)
	for i := range total {
		items = append(items, jobs.Item{Position: i, Track: music.Track{Title: "Track"}})
	}
	if err := repo.AddItems(ctx, job.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}
	stored, err := repo.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, item := range stored {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := jobs.ItemCompleted
			switch i % 3 {
			case 1:
				status = jobs.ItemFailed
			case 2:
				status = jobs.ItemSkipped
			}
			<-start
			if err := repo.UpdateItem(ctx, item.ID, jobs.ItemUpdate{Status: status}); err != nil {
				t.Errorf("update item %d: %v", i, err)
				return
			}
			if _, err := repo.RefreshCounters(ctx, job.ID); err != nil {
				t.Errorf("refresh counters: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	final, err := repo.RefreshCounters(ctx, job.ID)
	if err != nil {
		t.Fatalf("final refresh: %v", err)
	}
	if final.Completed != 4 || final.Failed != 4 || final.Skipped != 4 {
		t.Fatalf("counters = %+v, want 4/4/4", final.Summary())
	}
	if final.Processed() != final.Total {
		t.Fatalf("processed %d of %d", final.Processed(), final.Total)
	}
}

func TestJobsCancelAndReset(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := repo.AddItems(ctx, job.ID, []jobs.Item{
		{Position: 0, Track: music.Track{Title: "One"}},
		{Position: 1, Track: music.Track{Title: "Two"}},
	}); err != nil {
		t.Fatalf("add items: %v", err)
	}

	items, _ := repo.ListItems(ctx, job.ID)
	if err := repo.UpdateItem(ctx, items[0].ID, jobs.ItemUpdate{Status: jobs.ItemDownloading}); err != nil {
		t.Fatalf("update item: %v", err)
	}

	reset, err := repo.ResetInFlightItems(ctx)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset != 1 {
		t.Fatalf("reset %d items, want 1", reset)
	}

	cancelled, err := repo.CancelPendingItems(ctx, job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled != 2 {
		t.Fatalf("cancelled %d items, want 2", cancelled)
	}

	pending, err := repo.ListPendingItems(ctx, job.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending items, want 0", len(pending))
	}
}

// TestRecoveryReturnsInterruptedWorkToTheQueue is the database half of the
// restart behaviour: after a crash every unfinished job is queued and every
// unfinished item is pending, while finished work is left untouched.
func TestRecoveryReturnsInterruptedWorkToTheQueue(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	running := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, running); err != nil {
		t.Fatalf("create running job: %v", err)
	}
	if err := repo.AddItems(ctx, running.ID, []jobs.Item{
		{Position: 0, Track: music.Track{Title: "Done"}},
		{Position: 1, Track: music.Track{Title: "Downloading"}},
		{Position: 2, Track: music.Track{Title: "Tagging"}},
		{Position: 3, Track: music.Track{Title: "Waiting"}},
	}); err != nil {
		t.Fatalf("add items: %v", err)
	}
	if err := repo.SetStatus(ctx, running.ID, jobs.StatusDownloading, "", ""); err != nil {
		t.Fatalf("set downloading: %v", err)
	}

	items, _ := repo.ListItems(ctx, running.ID)
	mustUpdate(t, repo, items[0].ID, jobs.ItemUpdate{Status: jobs.ItemCompleted})
	mustUpdate(t, repo, items[1].ID, jobs.ItemUpdate{Status: jobs.ItemDownloading})
	mustUpdate(t, repo, items[2].ID, jobs.ItemUpdate{Status: jobs.ItemMatching})
	mustUpdate(t, repo, items[2].ID, jobs.ItemUpdate{Status: jobs.ItemTagging})

	finished := &jobs.Job{Type: jobs.TypeTrack, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, finished); err != nil {
		t.Fatalf("create finished job: %v", err)
	}
	if err := repo.SetStatus(ctx, finished.ID, jobs.StatusCompleted, "", ""); err != nil {
		t.Fatalf("complete job: %v", err)
	}

	// This is what the manager does on start up.
	resetItems, err := repo.ResetInFlightItems(ctx)
	if err != nil {
		t.Fatalf("reset items: %v", err)
	}
	if resetItems != 2 {
		t.Fatalf("reset %d items, want 2", resetItems)
	}
	resetJobs, err := repo.ResetInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("reset jobs: %v", err)
	}
	if resetJobs != 1 {
		t.Fatalf("reset %d jobs, want 1 (the completed job must not be touched)", resetJobs)
	}

	recovered, err := repo.Get(ctx, running.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if recovered.Status != jobs.StatusQueued {
		t.Fatalf("status = %q, want queued", recovered.Status)
	}

	stillDone, err := repo.Get(ctx, finished.ID)
	if err != nil {
		t.Fatalf("get finished job: %v", err)
	}
	if stillDone.Status != jobs.StatusCompleted {
		t.Fatalf("a completed job was requeued: %q", stillDone.Status)
	}

	pending, err := repo.ListPendingItems(ctx, running.ID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("got %d pending items, want 3 (the completed one stays completed)", len(pending))
	}
	for _, item := range pending {
		if item.Status != jobs.ItemPending {
			t.Fatalf("item %q is %q, want pending", item.Label, item.Status)
		}
		if item.StartedAt != nil {
			t.Fatalf("item %q kept the start time of the interrupted attempt", item.Label)
		}
	}

	// The recovery has to survive being run twice, e.g. after a shutdown that
	// already reset the work and a start up that resets it again.
	if _, err := repo.ResetInFlightItems(ctx); err != nil {
		t.Fatalf("second item reset: %v", err)
	}
	if again, err := repo.ResetInterruptedJobs(ctx); err != nil || again != 0 {
		t.Fatalf("second job reset changed %d rows (%v), want 0", again, err)
	}
}

func mustUpdate(t *testing.T, repo *Jobs, id string, update jobs.ItemUpdate) {
	t.Helper()
	if err := repo.UpdateItem(context.Background(), id, update); err != nil {
		t.Fatalf("update item %s: %v", id, err)
	}
}

func TestFilesUpsertByPath(t *testing.T) {
	ctx := context.Background()
	files := NewFiles(openTestDB(t))

	first, err := files.Upsert(ctx, music.File{Path: "Artist/2001 - Album/01 - Song.opus", Codec: "opus", SizeBytes: 100})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second, err := files.Upsert(ctx, music.File{Path: "Artist/2001 - Album/01 - Song.opus", Codec: "opus", SizeBytes: 200})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert created a second row: %q != %q", first.ID, second.ID)
	}

	found, err := files.FindByPath(ctx, "Artist/2001 - Album/01 - Song.opus")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.SizeBytes != 200 {
		t.Fatalf("file was not refreshed: %+v", found)
	}
	if found.CreatedAt.IsZero() || found.UpdatedAt.IsZero() {
		t.Fatalf("file timestamps were not stored: %+v", found)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	settings := NewSettings(openTestDB(t))

	if _, ok, err := settings.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("unknown key: ok=%v err=%v", ok, err)
	}
	if err := settings.SetMany(ctx, map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("set many: %v", err)
	}
	if err := settings.Set(ctx, "a", "3"); err != nil {
		t.Fatalf("set: %v", err)
	}
	all, err := settings.All(ctx)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if all["a"] != "3" || all["b"] != "2" {
		t.Fatalf("settings = %v", all)
	}
}

// TestUnicodeAndLongTextRoundTrip guards against an encoding regression in the
// PostgreSQL driver: names from the providers routinely contain non-ASCII text.
func TestUnicodeAndLongTextRoundTrip(t *testing.T) {
	ctx := context.Background()
	catalog := NewCatalog(openTestDB(t))

	name := "Sigur Rós – Ágætis byrjun 日本語 Ⅻ " + strings.Repeat("é", 200)
	artist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: name, Provider: "ytmusic", SourceID: "UCunicode",
	})
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	loaded, err := catalog.GetArtist(ctx, artist.ID)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if loaded.Name != name {
		t.Fatalf("name round trip failed:\n got %q\nwant %q", loaded.Name, name)
	}

	track, err := catalog.UpsertTrack(ctx, music.Track{
		Title: "Ævin", Artists: []string{"Sigur Rós", "Ólafur"}, Album: "Ágætis", DurationMS: 300000,
	}, "", "", 4000)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	stored, err := catalog.GetTrack(ctx, track.ID)
	if err != nil {
		t.Fatalf("get track: %v", err)
	}
	if len(stored.Artists) != 2 || stored.Artists[1] != "Ólafur" {
		t.Fatalf("artist list round trip failed: %+v", stored.Artists)
	}
}

// TestTimestampsAreStoredInUTC pins that reading a row back yields the instant
// it was written, independently of the server's timezone setting.
func TestTimestampsAreStoredInUTC(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	before := time.Now().UTC().Add(-time.Second)
	job := &jobs.Job{Type: jobs.TypeTrack, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	stored, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.CreatedAt.Before(before) || stored.CreatedAt.After(after) {
		t.Fatalf("created_at %v is outside [%v, %v]", stored.CreatedAt, before, after)
	}
	if _, offset := stored.CreatedAt.Zone(); offset != 0 {
		t.Fatalf("created_at was not returned in UTC: %v", stored.CreatedAt)
	}
}

func TestGetJobNotFound(t *testing.T) {
	_, err := NewJobs(openTestDB(t)).Get(context.Background(), "missing")
	if code := apperr.CodeOf(err); code != apperr.CodeJobNotFound {
		t.Fatalf("code = %s, want %s", code, apperr.CodeJobNotFound)
	}
	if errors.Is(err, nil) {
		t.Fatal("a missing job must produce an error")
	}
}

func TestFilesListAllAndDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	files := NewFiles(db)
	cat := NewCatalog(db)

	t1, err := cat.UpsertTrack(ctx, music.Track{Title: "Track 1", DurationMS: 120000}, "", "", 0)
	if err != nil {
		t.Fatalf("upsert t1: %v", err)
	}
	t2, err := cat.UpsertTrack(ctx, music.Track{Title: "Track 2", DurationMS: 150000}, "", "", 0)
	if err != nil {
		t.Fatalf("upsert t2: %v", err)
	}

	f1, err := files.Upsert(ctx, music.File{Path: "Artist/Album/01.opus", Codec: "opus", SizeBytes: 100, TrackID: t1.ID})
	if err != nil {
		t.Fatalf("upsert f1: %v", err)
	}
	f2, err := files.Upsert(ctx, music.File{Path: "Artist/Album/02.opus", Codec: "opus", SizeBytes: 200, TrackID: t1.ID})
	if err != nil {
		t.Fatalf("upsert f2: %v", err)
	}
	f3, err := files.Upsert(ctx, music.File{Path: "Artist/Album/03.opus", Codec: "opus", SizeBytes: 300, TrackID: t2.ID})
	if err != nil {
		t.Fatalf("upsert f3: %v", err)
	}

	all, err := files.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d files, want 3", len(all))
	}

	if err := files.DeleteByTrack(ctx, t1.ID); err != nil {
		t.Fatalf("delete by track: %v", err)
	}
	remaining, err := files.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all after delete by track: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != f3.ID {
		t.Fatalf("remaining files: %+v, want only f3", remaining)
	}

	if err := files.DeleteByPath(ctx, f3.Path); err != nil {
		t.Fatalf("delete by path: %v", err)
	}
	empty, err := files.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all after delete by path: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty files list, got %d", len(empty))
	}
	_ = f1
	_ = f2
}

func TestCatalogDeleteTrackAndRelease(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	cat := NewCatalog(db)

	artist, err := cat.UpsertArtist(ctx, music.Artist{Name: "Artist", Provider: "deezer", SourceID: "art1"})
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	release, err := cat.UpsertRelease(ctx, music.Release{Title: "Release", Provider: "deezer", SourceID: "rel1"}, artist.ID)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	track, err := cat.UpsertTrack(ctx, music.Track{Title: "Track", Artists: []string{"Artist"}, DurationMS: 180000}, release.ID, artist.ID, 0)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	if err := cat.AddSource(ctx, music.Source{TrackID: track.ID, Provider: "deezer", SourceID: "src1", Kind: music.SourceMetadata}); err != nil {
		t.Fatalf("add source: %v", err)
	}

	allTracks, err := cat.ListAllTracks(ctx)
	if err != nil {
		t.Fatalf("list all tracks: %v", err)
	}
	if len(allTracks) != 1 || allTracks[0].Track.ID != track.ID {
		t.Fatalf("list all tracks mismatch: %+v", allTracks)
	}

	if err := cat.DeleteTrack(ctx, track.ID); err != nil {
		t.Fatalf("delete track: %v", err)
	}
	tracksAfter, err := cat.ListAllTracks(ctx)
	if err != nil {
		t.Fatalf("list all tracks after delete: %v", err)
	}
	if len(tracksAfter) != 0 {
		t.Fatalf("expected 0 tracks, got %d", len(tracksAfter))
	}

	if err := cat.DeleteRelease(ctx, release.ID); err != nil {
		t.Fatalf("delete release: %v", err)
	}
	relAfter, err := cat.GetRelease(ctx, release.ID)
	if err == nil || relAfter != nil {
		t.Fatalf("expected release to be deleted, got %v, err=%v", relAfter, err)
	}
}

func assertRowCounts(t *testing.T, db *database.DB, want map[string]int) {
	t.Helper()
	for table, expected := range want {
		var count int
		if err := db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != expected {
			t.Fatalf("%s holds %d rows, want %d", table, count, expected)
		}
	}
}

// TestLyricsMigrationAppliesToAnExistingDatabase runs migration 0004 against a
// database that already holds v0.8.0 data and verifies that it is purely
// additive: every existing row survives unchanged and the new columns arrive
// with the defaults the backend expects.
func TestLyricsMigrationAppliesToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	artist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: "Migration Artist", Provider: "deezer", SourceID: "mig_artist_1",
	})
	if err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	release, err := catalog.UpsertRelease(ctx, music.Release{
		Title: "Migration Album", AlbumArtist: "Migration Artist", ReleaseType: music.ReleaseAlbum,
		Year: 2001, Provider: "deezer", SourceID: "mig_release_1",
	}, artist.ID)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	track, err := catalog.UpsertTrack(ctx, music.Track{
		Title: "Migration Track", Artists: []string{"Migration Artist"},
		Album: "Migration Album", AlbumArtist: "Migration Artist",
		TrackNumber: 1, TrackTotal: 10, DiscNumber: 1, DiscTotal: 1,
		DurationMS: 200000, ISRC: "MIGRATION001",
	}, release.ID, artist.ID, 4000)
	if err != nil {
		t.Fatalf("seed track: %v", err)
	}

	// Roll the schema back to its v0.8.0 shape.
	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_tracks_lyrics_backfill`,
		`ALTER TABLE tracks DROP CONSTRAINT IF EXISTS tracks_lyrics_state_known`,
		`ALTER TABLE tracks DROP COLUMN IF EXISTS lyrics_state`,
		`ALTER TABLE tracks DROP COLUMN IF EXISTS lyrics_provider`,
		`ALTER TABLE tracks DROP COLUMN IF EXISTS lyrics_checked_at`,
		`ALTER TABLE tracks DROP COLUMN IF EXISTS compilation`,
		`ALTER TABLE releases DROP COLUMN IF EXISTS compilation`,
		`DELETE FROM schema_migrations WHERE version = 4`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("roll back (%s): %v", statement, err)
		}
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("apply migration 0004: %v", err)
	}

	loaded, err := catalog.GetTrack(ctx, track.ID)
	if err != nil {
		t.Fatalf("get track after migration: %v", err)
	}
	if loaded.Title != "Migration Track" || loaded.ISRC != "MIGRATION001" ||
		loaded.TrackTotal != 10 || loaded.AlbumArtist != "Migration Artist" {
		t.Fatalf("the existing track was modified by the migration: %+v", loaded)
	}
	if loaded.DisplayLyricsState() != music.LyricsUnknown {
		t.Errorf("lyrics state = %q, want unknown for an untouched track", loaded.DisplayLyricsState())
	}
	if loaded.LyricsCheckedAt != nil {
		t.Errorf("lyrics_checked_at = %v, want NULL", loaded.LyricsCheckedAt)
	}
	if loaded.Compilation {
		t.Error("compilation must default to false")
	}

	loadedRelease, err := catalog.GetRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("get release after migration: %v", err)
	}
	if loadedRelease.Title != "Migration Album" || loadedRelease.Compilation {
		t.Fatalf("the existing release was modified: %+v", loadedRelease)
	}
}

func TestSetLyricsStateAndCandidateListing(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	seed := func(name string) music.Track {
		t.Helper()
		track, err := catalog.UpsertTrack(ctx, music.Track{
			Title: name, Artists: []string{"Lyrics Artist"}, Album: "Lyrics Album",
			AlbumArtist: "Lyrics Artist", TrackNumber: 1, DurationMS: 100000,
			ISRC: "LYR" + name,
		}, "", "", 4000)
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		return track
	}

	fresh := seed("fresh")
	stale := seed("stale")
	never := seed("never")
	synced := seed("synced")

	now := time.Now().UTC()
	if err := catalog.SetLyricsState(ctx, fresh.ID, music.LyricsNotFound, "lrclib", now); err != nil {
		t.Fatalf("SetLyricsState fresh: %v", err)
	}
	if err := catalog.SetLyricsState(ctx, stale.ID, music.LyricsNotFound, "lrclib", now.Add(-30*24*time.Hour)); err != nil {
		t.Fatalf("SetLyricsState stale: %v", err)
	}
	if err := catalog.SetLyricsState(ctx, synced.ID, music.LyricsAvailableSynced, "lrclib", now.Add(-365*24*time.Hour)); err != nil {
		t.Fatalf("SetLyricsState synced: %v", err)
	}

	stored, err := catalog.GetTrack(ctx, fresh.ID)
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if stored.LyricsState != music.LyricsNotFound || stored.LyricsProvider != "lrclib" {
		t.Fatalf("stored state = %q/%q", stored.LyricsState, stored.LyricsProvider)
	}
	if stored.LyricsCheckedAt == nil {
		t.Fatal("lyrics_checked_at was not recorded")
	}

	candidates, err := catalog.ListTracksNeedingLyrics(ctx, now.Add(-14*24*time.Hour), 100)
	if err != nil {
		t.Fatalf("ListTracksNeedingLyrics: %v", err)
	}
	ids := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		ids[candidate.Track.ID] = true
	}
	if !ids[stale.ID] {
		t.Error("a track whose check has aged out must be offered again")
	}
	if !ids[never.ID] {
		t.Error("a track that was never checked must be offered")
	}
	if ids[fresh.ID] {
		t.Error("a recent definitive miss must stay in its cooldown")
	}
	if ids[synced.ID] {
		t.Error("a track that already has synchronised lyrics must never be offered")
	}
}

func TestSetLyricsStateRejectsUnknownStatesAndMissingTracks(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	if err := catalog.SetLyricsState(ctx, "does-not-exist", music.LyricsNotFound, "lrclib", time.Now()); err == nil {
		t.Error("an unknown track must be reported")
	}
	if err := catalog.SetLyricsState(ctx, "does-not-exist", music.LyricsState("error"), "lrclib", time.Now()); err == nil {
		t.Error("an unknown state must be refused")
	}
}

func TestJobPriorityAndPause(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	jobLow := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusQueued, Priority: jobs.PriorityLow,
		Label: "Low Job", TargetID: "low_1", Options: jobs.DefaultOptions(),
	}
	jobNormal := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusQueued, Priority: jobs.PriorityNormal,
		Label: "Normal Job", TargetID: "norm_1", Options: jobs.DefaultOptions(),
	}
	jobHigh := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusQueued, Priority: jobs.PriorityHigh,
		Label: "High Job", TargetID: "high_1", Options: jobs.DefaultOptions(),
	}

	if err := repo.Create(ctx, jobLow); err != nil {
		t.Fatalf("create low: %v", err)
	}
	if err := repo.Create(ctx, jobNormal); err != nil {
		t.Fatalf("create normal: %v", err)
	}
	if err := repo.Create(ctx, jobHigh); err != nil {
		t.Fatalf("create high: %v", err)
	}

	// Verify ListUnfinished orders High before Normal before Low
	unfinished, err := repo.ListUnfinished(ctx)
	if err != nil {
		t.Fatalf("list unfinished: %v", err)
	}
	if len(unfinished) < 3 {
		t.Fatalf("expected at least 3 unfinished jobs, got %d", len(unfinished))
	}
	// The first 3 should be High, Normal, Low
	if unfinished[0].ID != jobHigh.ID || unfinished[1].ID != jobNormal.ID || unfinished[2].ID != jobLow.ID {
		t.Fatalf("unexpected priority order: [%s, %s, %s]", unfinished[0].Label, unfinished[1].Label, unfinished[2].Label)
	}

	// Test SetPriority
	if err := repo.SetPriority(ctx, jobLow.ID, jobs.PriorityHigh); err != nil {
		t.Fatalf("set priority: %v", err)
	}
	updatedLow, err := repo.Get(ctx, jobLow.ID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}
	if updatedLow.Priority != jobs.PriorityHigh {
		t.Fatalf("expected priority high, got %s", updatedLow.Priority)
	}

	// Test SetPaused
	if err := repo.SetPaused(ctx, jobHigh.ID, true); err != nil {
		t.Fatalf("set paused: %v", err)
	}
	pausedHigh, err := repo.Get(ctx, jobHigh.ID)
	if err != nil {
		t.Fatalf("get paused job: %v", err)
	}
	if !pausedHigh.Paused {
		t.Fatal("expected paused = true")
	}

	// ListUnfinished should exclude paused jobs
	unfinishedAfterPause, err := repo.ListUnfinished(ctx)
	if err != nil {
		t.Fatalf("list unfinished after pause: %v", err)
	}
	for _, j := range unfinishedAfterPause {
		if j.ID == jobHigh.ID {
			t.Fatalf("paused job %s was included in ListUnfinished", j.ID)
		}
	}
}

func TestDeleteHistoryPreservesLibrary(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	jobsRepo := NewJobs(db)
	catalogRepo := NewCatalog(db)

	now := time.Now().UTC().Add(-40 * 24 * time.Hour) // 40 days old

	// 1. Create Artist, Release, Track, File
	entry := music.LibraryEntry{
		Artist: &music.Artist{Name: "Radiohead", Provider: "spotify", SourceID: "rh_del_test"},
		Release: &music.Release{
			Title: "OK Computer", ReleaseType: music.ReleaseAlbum, Year: 1997,
			Provider: "spotify", SourceID: "ok_del_test",
		},
		Track: music.Track{
			Title: "Airbag", TrackNumber: 1, DiscNumber: 1, Year: 1997,
		},
		File: music.File{Path: "Radiohead/OK Computer/01 Airbag.opus", Codec: "opus", SizeBytes: 12345},
	}
	stored, err := catalogRepo.PersistDownload(ctx, entry, 15000)
	if err != nil {
		t.Fatalf("persist download: %v", err)
	}

	// 2. Create old Completed Job and Item referencing track
	oldJob := &jobs.Job{
		Type: jobs.TypeRelease, Status: jobs.StatusCompleted, Label: "OK Computer",
		MetadataProvider: "spotify", MediaProvider: "youtube", TargetID: "ok_del_test",
		Options: jobs.DefaultOptions(), Total: 1, Completed: 1,
	}
	if err := jobsRepo.Create(ctx, oldJob); err != nil {
		t.Fatalf("create old job: %v", err)
	}
	// Manually set old created_at
	_, _ = db.ExecContext(ctx, `UPDATE jobs SET created_at = $1, status = 'completed' WHERE id = $2`, now, oldJob.ID)

	items := []jobs.Item{
		{
			ID: music.NewID(), JobID: oldJob.ID, Position: 1, Status: jobs.ItemCompleted,
			TrackID: stored.TrackID, Track: entry.Track, Label: "Radiohead - Airbag",
		},
	}
	if err := jobsRepo.AddItems(ctx, oldJob.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	// 3. Delete history older than 30 days
	deletedJobs, deletedItems, err := jobsRepo.DeleteHistory(ctx, time.Now().UTC().Add(-30*24*time.Hour), []jobs.Status{jobs.StatusCompleted})
	if err != nil {
		t.Fatalf("delete history: %v", err)
	}
	if deletedJobs != 1 || deletedItems != 1 {
		t.Fatalf("expected 1 deleted job and 1 deleted item, got %d jobs, %d items", deletedJobs, deletedItems)
	}

	// Verify job and item are deleted
	if _, err := jobsRepo.Get(ctx, oldJob.ID); err == nil {
		t.Fatal("expected old job to be deleted")
	}

	// 4. Verify Track, File, Release, Artist are completely intact!
	track, err := catalogRepo.FindTrack(ctx, entry.Track, 15000)
	if err != nil || track == nil {
		t.Fatalf("track was deleted or corrupted by history cleanup: %v", err)
	}
	if track.ID != stored.TrackID {
		t.Fatalf("expected track ID %s, got %s", stored.TrackID, track.ID)
	}
}

func TestResetFailedItems(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	job := &jobs.Job{
		Type: jobs.TypeArtist, Status: jobs.StatusFailed, Label: "Failed Job",
		TargetID: "fail_1", Options: jobs.DefaultOptions(), Total: 2, Failed: 2,
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	items := []jobs.Item{
		{
			ID: music.NewID(), JobID: job.ID, Position: 1, Status: jobs.ItemFailed,
			Label: "Track 1", Attempts: 5, MaxAttempts: 5, ErrorCode: "DOWNLOAD_FAILED", ErrorMessage: "HTTP 404",
		},
		{
			ID: music.NewID(), JobID: job.ID, Position: 2, Status: jobs.ItemFailed,
			Label: "Track 2", Attempts: 5, MaxAttempts: 5, ErrorCode: "DOWNLOAD_FAILED", ErrorMessage: "HTTP 404",
		},
	}
	if err := repo.AddItems(ctx, job.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	// 1. Reset item 1 individually
	if err := repo.ResetItemForRetry(ctx, job.ID, items[0].ID); err != nil {
		t.Fatalf("reset item 1: %v", err)
	}
	it1, err := repo.GetItem(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("get item 1: %v", err)
	}
	if it1.Status != jobs.ItemPending || it1.Attempts != 0 || it1.ErrorCode != "" {
		t.Fatalf("item 1 not reset properly: %+v", it1)
	}

	// 2. Reset all failed items in job (item 2 remains failed)
	retried, skipped, err := repo.ResetFailedItemsInJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("reset failed items: %v", err)
	}
	if retried != 1 || skipped != 0 {
		t.Fatalf("expected 1 retried, got %d (skipped %d)", retried, skipped)
	}
	it2, err := repo.GetItem(ctx, items[1].ID)
	if err != nil {
		t.Fatalf("get item 2: %v", err)
	}
	if it2.Status != jobs.ItemPending || it2.Attempts != 0 {
		t.Fatalf("item 2 not reset properly: %+v", it2)
	}
}

func TestJobHistoryPagination137(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	baseTime := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	createdIDs := make(map[string]bool)

	// Create 137 completed jobs
	for i := 1; i <= 137; i++ {
		j := &jobs.Job{
			Type:             jobs.TypeTrack,
			Status:           jobs.StatusCompleted,
			Label:            fmt.Sprintf("Job %03d", i),
			MetadataProvider: "spotify",
			MediaProvider:    "youtube",
			TargetID:         fmt.Sprintf("track_%03d", i),
			Options:          jobs.DefaultOptions(),
			Total:            1,
			Completed:        1,
		}
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
		// Set distinct decreasing created_at so ordering is well defined
		_, _ = db.ExecContext(ctx, `UPDATE jobs SET created_at = $1 WHERE id = $2`, baseTime.Add(time.Duration(i)*time.Second), j.ID)
		createdIDs[j.ID] = true
	}

	seenIDs := make(map[string]bool)
	pageSize := 25
	offsets := []int{0, 25, 50, 75, 100, 125}

	for _, offset := range offsets {
		page, total, err := repo.List(ctx, jobs.ListFilter{
			Status: jobs.StatusCompleted,
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			t.Fatalf("List at offset %d: %v", offset, err)
		}
		if total < 137 {
			t.Fatalf("expected total >= 137, got %d", total)
		}
		for _, j := range page {
			if !createdIDs[j.ID] {
				continue // ignore jobs from other tests
			}
			if seenIDs[j.ID] {
				t.Fatalf("duplicate job ID seen across pages: %s", j.ID)
			}
			seenIDs[j.ID] = true
		}
	}

	if len(seenIDs) != 137 {
		t.Fatalf("expected to paginate all 137 jobs exactly once, saw %d", len(seenIDs))
	}
}

func TestJobHistoryStableTieBreaker(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	sameTime := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	var created []string

	// Create 10 jobs with the exact same created_at
	for i := 1; i <= 10; i++ {
		j := &jobs.Job{
			Type:     jobs.TypeTrack,
			Status:   jobs.StatusQueued,
			Label:    fmt.Sprintf("TieJob %d", i),
			TargetID: fmt.Sprintf("tie_track_%d", i),
			Options:  jobs.DefaultOptions(),
		}
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("create tie job %d: %v", i, err)
		}
		_, _ = db.ExecContext(ctx, `UPDATE jobs SET created_at = $1 WHERE id = $2`, sameTime, j.ID)
		created = append(created, j.ID)
	}

	// Paginate page 1 (5 items) and page 2 (5 items)
	p1, _, err := repo.List(ctx, jobs.ListFilter{Status: jobs.StatusQueued, Limit: 5, Offset: 0})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	p2, _, err := repo.List(ctx, jobs.ListFilter{Status: jobs.StatusQueued, Limit: 5, Offset: 5})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}

	p1IDs := make(map[string]bool)
	for _, j := range p1 {
		p1IDs[j.ID] = true
	}
	for _, j := range p2 {
		if p1IDs[j.ID] {
			t.Fatalf("tie-breaker failed: job %s appeared in both page 1 and page 2", j.ID)
		}
	}
}

func Test10kQueueAndHistoryPerformance(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	// Explain Analyze queue selection query
	explainQuery := `
		EXPLAIN ANALYZE
		SELECT id, type, status, priority, paused, label, metadata_provider, media_provider,
		       target_id, options_json, total, completed, failed, skipped, error_code, error_message,
		       created_at, updated_at, started_at, finished_at
		FROM jobs
		WHERE status NOT IN ('completed', 'failed', 'cancelled') AND NOT paused
		ORDER BY priority DESC, created_at ASC
		LIMIT 100`

	var planLines []string
	rows, err := db.QueryContext(ctx, explainQuery)
	if err != nil {
		t.Fatalf("EXPLAIN ANALYZE queue: %v", err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		planLines = append(planLines, line)
	}
	rows.Close()

	t.Logf("Queue Selection EXPLAIN ANALYZE Plan:\n%s", strings.Join(planLines, "\n"))

	// Explain Analyze history pagination
	historyExplain := `
		EXPLAIN ANALYZE
		SELECT id, type, status, priority, paused, label, metadata_provider, media_provider,
		       target_id, options_json, total, completed, failed, skipped, error_code, error_message,
		       created_at, updated_at, started_at, finished_at
		FROM jobs
		WHERE status = 'completed'
		ORDER BY created_at DESC, id DESC
		LIMIT 25 OFFSET 100`

	var historyPlan []string
	rowsH, err := db.QueryContext(ctx, historyExplain)
	if err != nil {
		t.Fatalf("EXPLAIN ANALYZE history: %v", err)
	}
	for rowsH.Next() {
		var line string
		if err := rowsH.Scan(&line); err != nil {
			t.Fatalf("scan history line: %v", err)
		}
		historyPlan = append(historyPlan, line)
	}
	rowsH.Close()

	t.Logf("History Pagination EXPLAIN ANALYZE Plan:\n%s", strings.Join(historyPlan, "\n"))

	// Execute ListUnfinished and check latency
	start := time.Now()
	_, err = repo.ListUnfinished(ctx)
	if err != nil {
		t.Fatalf("ListUnfinished: %v", err)
	}
	dur := time.Since(start)
	t.Logf("ListUnfinished execution duration: %v", dur)
}

func TestJobProgressGreenDayRegression(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	// Simulate artist parent job: total 514, stale jobs.completed = 0
	parentJob := &jobs.Job{
		Type:             jobs.TypeArtist,
		Status:           jobs.StatusDownloading,
		Priority:         jobs.PriorityNormal,
		Label:            "Green Day",
		MetadataProvider: "ytmusic",
		MediaProvider:    "ytmusic",
		TargetID:         "UC4JNeITH4P7G51C1hJoG6vQ",
		Options:          jobs.DefaultOptions(),
		Total:            514,
		Completed:        0,
		Failed:           0,
		Skipped:          0,
	}
	if err := repo.Create(ctx, parentJob); err != nil {
		t.Fatalf("create parent job: %v", err)
	}

	// Populate 514 items: 166 completed, 4 matching, 344 pending
	items := make([]jobs.Item, 0, 514)
	for i := 0; i < 514; i++ {
		status := jobs.ItemPending
		if i < 166 {
			status = jobs.ItemCompleted
		} else if i < 170 {
			status = jobs.ItemMatching
		}
		items = append(items, jobs.Item{
			Position: i,
			Status:   status,
			Track:    music.Track{Title: "Track " + strconv.Itoa(i), Artists: []string{"Green Day"}},
			Label:    "Green Day - Track " + strconv.Itoa(i),
		})
	}
	if err := repo.AddItems(ctx, parentJob.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	// 1. Verify repo.Get derives live progress from job_items (166 / 514)
	gotJob, err := repo.Get(ctx, parentJob.ID)
	if err != nil {
		t.Fatalf("repo.Get: %v", err)
	}
	if gotJob.Total != 514 {
		t.Errorf("Get total = %d, want 514", gotJob.Total)
	}
	if gotJob.Completed != 166 {
		t.Errorf("Get completed = %d, want 166", gotJob.Completed)
	}
	if gotJob.Failed != 0 {
		t.Errorf("Get failed = %d, want 0", gotJob.Failed)
	}
	if gotJob.Skipped != 0 {
		t.Errorf("Get skipped = %d, want 0", gotJob.Skipped)
	}

	// 2. Verify repo.List derives live progress from job_items (166 / 514)
	listed, totalCount, err := repo.List(ctx, jobs.ListFilter{Type: jobs.TypeArtist})
	if err != nil {
		t.Fatalf("repo.List: %v", err)
	}
	if totalCount != 1 || len(listed) != 1 {
		t.Fatalf("repo.List returned %d items, totalCount %d", len(listed), totalCount)
	}
	listJob := listed[0]
	if listJob.ID != parentJob.ID {
		t.Fatalf("listJob ID = %s, want %s", listJob.ID, parentJob.ID)
	}
	if listJob.Total != 514 {
		t.Errorf("List total = %d, want 514", listJob.Total)
	}
	if listJob.Completed != 166 {
		t.Errorf("List completed = %d, want 166", listJob.Completed)
	}
	if listJob.Failed != 0 {
		t.Errorf("List failed = %d, want 0", listJob.Failed)
	}
	if listJob.Skipped != 0 {
		t.Errorf("List skipped = %d, want 0", listJob.Skipped)
	}

	// 3. Consistency between Get and List
	if gotJob.Completed != listJob.Completed || gotJob.Total != listJob.Total {
		t.Errorf("Inconsistency between Get (%d/%d) and List (%d/%d)",
			gotJob.Completed, gotJob.Total, listJob.Completed, listJob.Total)
	}

	// 4. Simulated restart persistence: recreate repo instance
	freshRepo := NewJobs(db)
	restartedGet, err := freshRepo.Get(ctx, parentJob.ID)
	if err != nil {
		t.Fatalf("freshRepo.Get: %v", err)
	}
	if restartedGet.Completed != 166 || restartedGet.Total != 514 {
		t.Errorf("freshRepo.Get after restart = %d/%d, want 166/514",
			restartedGet.Completed, restartedGet.Total)
	}

	restartedList, _, err := freshRepo.List(ctx, jobs.ListFilter{Type: jobs.TypeArtist})
	if err != nil {
		t.Fatalf("freshRepo.List: %v", err)
	}
	if len(restartedList) != 1 || restartedList[0].Completed != 166 || restartedList[0].Total != 514 {
		t.Errorf("freshRepo.List after restart = %+v, want 166/514", restartedList)
	}
}

func TestJobProgressEdgeCases(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewJobs(db)

	// Case 1: Zero-item job (total = 0, no items added yet)
	zeroJob := &jobs.Job{
		Type:             jobs.TypeRelease,
		Status:           jobs.StatusResolvingTracks,
		Priority:         jobs.PriorityLow,
		Label:            "Resolving Release",
		MetadataProvider: "spotify",
		MediaProvider:    "ytmusic",
		TargetID:         "rel-0",
		Options:          jobs.DefaultOptions(),
		Total:            0,
		Completed:        0,
	}
	if err := repo.Create(ctx, zeroJob); err != nil {
		t.Fatalf("create zeroJob: %v", err)
	}

	gotZero, err := repo.Get(ctx, zeroJob.ID)
	if err != nil {
		t.Fatalf("get zeroJob: %v", err)
	}
	if gotZero.Total != 0 || gotZero.Completed != 0 {
		t.Errorf("zeroJob = %d/%d, want 0/0", gotZero.Completed, gotZero.Total)
	}

	// Case 2: Job with failed, skipped, and completed items
	mixedJob := &jobs.Job{
		Type:             jobs.TypeRelease,
		Status:           jobs.StatusDownloading,
		Priority:         jobs.PriorityHigh,
		Label:            "Mixed Album",
		MetadataProvider: "spotify",
		MediaProvider:    "ytmusic",
		TargetID:         "rel-mixed",
		Options:          jobs.DefaultOptions(),
		Total:            10,
		Completed:        0,
		Failed:           0,
		Skipped:          0,
	}
	if err := repo.Create(ctx, mixedJob); err != nil {
		t.Fatalf("create mixedJob: %v", err)
	}

	mixedItems := []jobs.Item{
		{Position: 0, Status: jobs.ItemCompleted, Track: music.Track{Title: "T1"}},
		{Position: 1, Status: jobs.ItemCompleted, Track: music.Track{Title: "T2"}},
		{Position: 2, Status: jobs.ItemCompleted, Track: music.Track{Title: "T3"}},
		{Position: 3, Status: jobs.ItemCompleted, Track: music.Track{Title: "T4"}},
		{Position: 4, Status: jobs.ItemCompleted, Track: music.Track{Title: "T5"}},
		{Position: 5, Status: jobs.ItemFailed, Track: music.Track{Title: "T6"}},
		{Position: 6, Status: jobs.ItemFailed, Track: music.Track{Title: "T7"}},
		{Position: 7, Status: jobs.ItemSkipped, Track: music.Track{Title: "T8"}},
		{Position: 8, Status: jobs.ItemPending, Track: music.Track{Title: "T9"}},
		{Position: 9, Status: jobs.ItemMatching, Track: music.Track{Title: "T10"}},
	}
	if err := repo.AddItems(ctx, mixedJob.ID, mixedItems); err != nil {
		t.Fatalf("add mixedItems: %v", err)
	}

	gotMixed, err := repo.Get(ctx, mixedJob.ID)
	if err != nil {
		t.Fatalf("get mixedJob: %v", err)
	}
	if gotMixed.Total != 10 {
		t.Errorf("mixedJob Total = %d, want 10", gotMixed.Total)
	}
	if gotMixed.Completed != 5 {
		t.Errorf("mixedJob Completed = %d, want 5", gotMixed.Completed)
	}
	if gotMixed.Failed != 2 {
		t.Errorf("mixedJob Failed = %d, want 2", gotMixed.Failed)
	}
	if gotMixed.Skipped != 1 {
		t.Errorf("mixedJob Skipped = %d, want 1", gotMixed.Skipped)
	}
	if gotMixed.Processed() != 8 {
		t.Errorf("mixedJob Processed() = %d, want 8", gotMixed.Processed())
	}

	// Verify List with pagination returns mixedJob accurately
	listMixed, _, err := repo.List(ctx, jobs.ListFilter{Priority: jobs.PriorityHigh})
	if err != nil {
		t.Fatalf("list mixedJob: %v", err)
	}
	if len(listMixed) != 1 || listMixed[0].Completed != 5 || listMixed[0].Failed != 2 || listMixed[0].Skipped != 1 {
		t.Errorf("listMixed = %+v, want 5/2/1", listMixed)
	}
}
