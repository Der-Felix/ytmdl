package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/music"
)

func TestMigration0005Indexes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	expectedIndexes := []struct {
		table string
		index string
	}{
		{"tracks", "idx_tracks_created_at"},
		{"releases", "idx_releases_created_at"},
		{"releases", "idx_releases_year"},
		{"tracks", "idx_tracks_lyrics_state"},
	}

	for _, exp := range expectedIndexes {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM pg_indexes
			 WHERE schemaname = current_schema() AND tablename = $1 AND indexname = $2`,
			exp.table, exp.index).Scan(&count); err != nil {
			t.Fatalf("look up index %s on %s: %v", exp.index, exp.table, err)
		}
		if count != 1 {
			t.Fatalf("expected index %s on %s to exist, found count=%d", exp.index, exp.table, count)
		}
	}
}

func TestCatalogLibraryFilteringAndSearch(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	files := NewFiles(db)
	ctx := context.Background()

	// 1. Create Artists
	artistA, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Daft Punk",
		Provider: "deezer",
		SourceID: "27",
	})
	if err != nil {
		t.Fatal(err)
	}

	artistB, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "LACAZETTE",
		Provider: "ytmusic",
		SourceID: "UC123",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create Releases
	releaseA, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Discovery",
		AlbumArtist: "Daft Punk",
		Artists:     []string{"Daft Punk"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2001,
		Provider:    "deezer",
		SourceID:    "rel-1",
	}, artistA.ID)
	if err != nil {
		t.Fatal(err)
	}

	releaseB, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "LID",
		AlbumArtist: "LACAZETTE",
		Artists:     []string{"LACAZETTE"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2024,
		Provider:    "ytmusic",
		SourceID:    "rel-2",
	}, artistB.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Create Tracks
	track1, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "One More Time",
		Album:       "Discovery",
		AlbumArtist: "Daft Punk",
		Artists:     []string{"Daft Punk"},
		TrackNumber: 1,
		TrackTotal:  14,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  320000,
		Year:        2001,
		ISRC:        "FRZ010100001",
	}, releaseA.ID, artistA.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	track2, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Harder, Better, Faster, Stronger",
		Album:       "Discovery",
		AlbumArtist: "Daft Punk",
		Artists:     []string{"Daft Punk"},
		TrackNumber: 4,
		TrackTotal:  14,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  224000,
		Year:        2001,
		ISRC:        "FRZ010100004",
	}, releaseA.ID, artistA.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	track3, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "LID",
		Album:       "LID",
		AlbumArtist: "LACAZETTE",
		Artists:     []string{"LACAZETTE"},
		TrackNumber: 1,
		TrackTotal:  14,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  142000,
		Year:        2024,
	}, releaseB.ID, artistB.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Attach files
	_, err = files.Upsert(ctx, music.File{
		TrackID:     track1.ID,
		Path:        "Daft Punk/2001 - Discovery/01 - One More Time.opus",
		SizeBytes:   4000000,
		Codec:       "opus",
		BitrateKbps: 160,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = files.Upsert(ctx, music.File{
		TrackID:     track2.ID,
		Path:        "Daft Punk/2001 - Discovery/04 - Harder, Better, Faster, Stronger.opus",
		SizeBytes:   3500000,
		Codec:       "opus",
		BitrateKbps: 160,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set lyrics state
	now := time.Now().UTC()
	if err := catalog.SetLyricsState(ctx, track1.ID, music.LyricsAvailableSynced, "lrclib", now); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetLyricsState(ctx, track2.ID, music.LyricsAvailableSynced, "lrclib", now); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetLyricsState(ctx, track3.ID, music.LyricsNotFound, "lrclib", now); err != nil {
		t.Fatal(err)
	}

	// Test 1: ListTracksFiltered with search
	tracks, total, err := catalog.ListTracksFiltered(ctx, TrackListFilter{
		Query: "harder",
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListTracksFiltered search error: %v", err)
	}
	if total != 1 || len(tracks) != 1 {
		t.Fatalf("expected 1 result for 'harder', got total=%d len=%d", total, len(tracks))
	}
	if tracks[0].Title != "Harder, Better, Faster, Stronger" {
		t.Fatalf("unexpected track title: %s", tracks[0].Title)
	}
	if tracks[0].FilePath == "" || tracks[0].Codec != "opus" {
		t.Fatalf("expected joined file info, got path=%q codec=%q", tracks[0].FilePath, tracks[0].Codec)
	}

	// Test 2: Search by exact ISRC
	tracks, total, err = catalog.ListTracksFiltered(ctx, TrackListFilter{
		Query: "frz010100001",
		Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || tracks[0].Title != "One More Time" {
		t.Fatalf("ISRC search failed, got total=%d", total)
	}

	// Test 3: Filter by lyrics_state
	tracks, total, err = catalog.ListTracksFiltered(ctx, TrackListFilter{
		LyricsState: "not_found",
		Limit:       50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || tracks[0].Title != "LID" {
		t.Fatalf("LyricsState filter failed, got total=%d", total)
	}

	// Test 4: Invalid sort rejected
	_, _, err = catalog.ListTracksFiltered(ctx, TrackListFilter{
		Sort: "malicious_column; DROP TABLE tracks;",
	})
	if err == nil {
		t.Fatal("expected error on invalid sort, got nil")
	}

	// Test 5: ListReleasesFiltered with type and sort
	releases, totalR, err := catalog.ListReleasesFiltered(ctx, ReleaseListFilter{
		ReleaseType: "album",
		Sort:        "year",
		Order:       "desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if totalR != 2 || len(releases) != 2 {
		t.Fatalf("expected 2 albums, got total=%d len=%d", totalR, len(releases))
	}
	if releases[0].Year != 2024 || releases[1].Year != 2001 {
		t.Fatalf("expected year desc sort, got %d, %d", releases[0].Year, releases[1].Year)
	}

	// Test 6: ListArtistsFiltered with release_count sort
	artists, totalA, err := catalog.ListArtistsFiltered(ctx, ArtistListFilter{
		Sort:  "name",
		Order: "asc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if totalA != 2 || len(artists) != 2 {
		t.Fatalf("expected 2 artists, got total=%d len=%d", totalA, len(artists))
	}
	if artists[0].Name != "Daft Punk" {
		t.Fatalf("expected Daft Punk first, got %s", artists[0].Name)
	}

	// Test 7: GetLibraryTrackDetail
	td, err := catalog.GetLibraryTrackDetail(ctx, track1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if td.Track.Title != "One More Time" || td.File == nil || !strings.HasSuffix(td.LyricsPath, ".lrc") {
		t.Fatalf("unexpected track detail: %+v", td)
	}

	// Test 8: GetLibraryReleaseDetail
	rd, err := catalog.GetLibraryReleaseDetail(ctx, releaseA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rd.Tracks) != 2 || rd.TotalSizeBytes != 7500000 {
		t.Fatalf("unexpected release detail: tracks=%d size=%d", len(rd.Tracks), rd.TotalSizeBytes)
	}

	// Test 9: GetLibraryArtistDetail
	ad, err := catalog.GetLibraryArtistDetail(ctx, artistA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ad.ReleaseCount != 1 || ad.TrackCount != 2 {
		t.Fatalf("unexpected artist detail: rels=%d tracks=%d", ad.ReleaseCount, ad.TrackCount)
	}

	// Test 10: Omni-search
	res, err := catalog.SearchLibrary(ctx, "daft", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artists) != 1 || len(res.Releases) != 1 || len(res.Tracks) != 2 {
		t.Fatalf("omni search failed: %+v", res)
	}

	// Test 11: Empty omni-search returns empty slices
	resEmpty, err := catalog.SearchLibrary(ctx, "   ", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(resEmpty.Artists) != 0 || len(resEmpty.Releases) != 0 || len(resEmpty.Tracks) != 0 {
		t.Fatalf("expected empty omni search result, got %+v", resEmpty)
	}

	// Test 12: GetLibraryAggregates
	artCount, relCount, trkCount, fileCount, totBytes, lyricsCov, codecs, err := catalog.GetLibraryAggregates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if artCount != 2 || relCount != 2 || trkCount != 3 || fileCount != 2 || totBytes != 7500000 {
		t.Fatalf("aggregates mismatch: art=%d rel=%d trk=%d files=%d bytes=%d", artCount, relCount, trkCount, fileCount, totBytes)
	}
	if lyricsCov[music.LyricsAvailableSynced] != 2 || lyricsCov[music.LyricsNotFound] != 1 {
		t.Fatalf("lyrics coverage mismatch: %+v", lyricsCov)
	}
	if codecs["opus"] != 2 {
		t.Fatalf("codecs mismatch: %+v", codecs)
	}
}
