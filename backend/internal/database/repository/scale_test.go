package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/music"
)

func TestScale1k(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	files := NewFiles(db)
	ctx := context.Background()

	// Seed 20 Artists, 100 Releases, 1,000 Tracks
	artistIDs := make([]string, 20)
	for i := 0; i < 20; i++ {
		art, err := catalog.UpsertArtist(ctx, music.Artist{
			Name:     fmt.Sprintf("Scale Artist %02d", i+1),
			Provider: "test",
			SourceID: fmt.Sprintf("art-%02d", i+1),
		})
		if err != nil {
			t.Fatal(err)
		}
		artistIDs[i] = art.ID
	}

	releaseIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		artID := artistIDs[i%20]
		rel, err := catalog.UpsertRelease(ctx, music.Release{
			Title:       fmt.Sprintf("Scale Album %03d", i+1),
			AlbumArtist: fmt.Sprintf("Scale Artist %02d", (i%20)+1),
			Artists:     []string{fmt.Sprintf("Scale Artist %02d", (i%20)+1)},
			ReleaseType: music.ReleaseAlbum,
			Year:        2000 + (i % 25),
			Provider:    "test",
			SourceID:    fmt.Sprintf("rel-%03d", i+1),
		}, artID)
		if err != nil {
			t.Fatal(err)
		}
		releaseIDs[i] = rel.ID
	}

	for i := 0; i < 1000; i++ {
		relID := releaseIDs[i%100]
		artID := artistIDs[(i%100)%20]
		lyricsState := music.LyricsAvailableSynced
		if i%3 == 1 {
			lyricsState = music.LyricsAvailablePlain
		} else if i%3 == 2 {
			lyricsState = music.LyricsNotFound
		}

		trk, err := catalog.UpsertTrack(ctx, music.Track{
			Title:       fmt.Sprintf("Track %04d Special Track", i+1),
			Album:       fmt.Sprintf("Scale Album %03d", (i%100)+1),
			AlbumArtist: fmt.Sprintf("Scale Artist %02d", ((i%100)%20)+1),
			Artists:     []string{fmt.Sprintf("Scale Artist %02d", ((i%100)%20)+1)},
			TrackNumber: (i % 10) + 1,
			TrackTotal:  10,
			DiscNumber:  1,
			DiscTotal:   1,
			DurationMS:  180000 + (i * 100),
			Year:        2000 + ((i % 100) % 25),
			LyricsState: lyricsState,
		}, relID, artID, 0)
		if err != nil {
			t.Fatal(err)
		}

		_, err = files.Upsert(ctx, music.File{
			TrackID:     trk.ID,
			Path:        fmt.Sprintf("Artist/Album/Track_%04d.opus", i+1),
			SizeBytes:   2500000,
			Codec:       "opus",
			BitrateKbps: 160,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// 1. Check total count
	tracks, total, err := catalog.ListTracksFiltered(ctx, TrackListFilter{
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1000 {
		t.Fatalf("expected total 1000, got %d", total)
	}
	if len(tracks) != 50 {
		t.Fatalf("expected page size 50, got %d", len(tracks))
	}

	// 2. Filter by lyrics_state
	_, syncedTotal, err := catalog.ListTracksFiltered(ctx, TrackListFilter{
		LyricsState: "available_synced",
		Limit:       50,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedSynced := 334 // 0, 3, 6, ... up to 999
	if syncedTotal != expectedSynced {
		t.Fatalf("expected %d synced tracks, got %d", expectedSynced, syncedTotal)
	}

	// 3. Search query
	searchTracks, searchTotal, err := catalog.ListTracksFiltered(ctx, TrackListFilter{
		Query: "Track 0500",
		Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if searchTotal != 1 || len(searchTracks) != 1 {
		t.Fatalf("expected 1 match for Track 0500, got total=%d len=%d", searchTotal, len(searchTracks))
	}
}

func TestScale10kAndILIKE(t *testing.T) {
	db := openTestDB(t)
	catalog := NewCatalog(db)
	ctx := context.Background()

	// Seed an artist and release for bulk tracks
	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Benchmark Artist",
		Provider: "bench",
		SourceID: "art-bench",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Benchmark Release",
		AlbumArtist: "Benchmark Artist",
		Artists:     []string{"Benchmark Artist"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2024,
		Provider:    "bench",
		SourceID:    "rel-bench",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Batch insert 10,000 synthetic tracks into PostgreSQL
	t.Log("Inserting 10,000 tracks via bulk insert...")
	startInsert := time.Now()
	const batchSize = 500
	for b := 0; b < 20; b++ {
		var (
			values []string
			args   []any
		)
		for i := 0; i < batchSize; i++ {
			idx := b*batchSize + i + 1
			state := "available_synced"
			if idx%3 == 1 {
				state = "available_plain"
			} else if idx%3 == 2 {
				state = "not_found"
			}
			paramBase := i * 18
			values = append(values, fmt.Sprintf(
				"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				paramBase+1, paramBase+2, paramBase+3, paramBase+4, paramBase+5, paramBase+6, paramBase+7, paramBase+8,
				paramBase+9, paramBase+10, paramBase+11, paramBase+12, paramBase+13, paramBase+14, paramBase+15, paramBase+16, paramBase+17, paramBase+18,
			))
			now := time.Now().UTC()
			args = append(args,
				fmt.Sprintf("bench-track-%05d", idx),
				rel.ID,
				art.ID,
				fmt.Sprintf("Electronic Symphony Track #%05d in D Minor", idx),
				`["Benchmark Artist"]`,
				"Benchmark Release",
				"Benchmark Artist",
				(idx%12)+1,
				12,
				1,
				1,
				210000+(idx%60000),
				2024,
				fmt.Sprintf("USBM124%05d", idx),
				fmt.Sprintf("ident-key-%05d", idx),
				state,
				now,
				now,
			)
		}

		query := fmt.Sprintf(`
			INSERT INTO tracks (
				id, release_id, artist_id, title, artists_json, album, album_artist,
				track_number, track_total, disc_number, disc_total, duration_ms, year,
				isrc, identity_key, lyrics_state, created_at, updated_at
			) VALUES %s`, strings.Join(values, ", "))

		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("bulk insert batch %d failed: %v", b, err)
		}
	}
	t.Logf("Bulk insert of 10,000 tracks completed in %v", time.Since(startInsert))

	// Verify total count
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM tracks WHERE id LIKE 'bench-track-%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10000 {
		t.Fatalf("expected 10,000 tracks, got %d", count)
	}

	// 1. EXPLAIN ANALYZE: Title Search (ILIKE '%Symphony%')
	runExplain(t, ctx, db, "Title ILIKE '%Symphony%'", `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT t.id, t.title FROM tracks t
		WHERE t.title ILIKE '%Symphony%'
		ORDER BY t.created_at DESC LIMIT 50;
	`)

	// 2. EXPLAIN ANALYZE: Artist Search (ILIKE '%Benchmark%')
	runExplain(t, ctx, db, "Album Artist ILIKE '%Benchmark%'", `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT t.id, t.title FROM tracks t
		WHERE t.album_artist ILIKE '%Benchmark%'
		ORDER BY t.created_at DESC LIMIT 50;
	`)

	// 3. EXPLAIN ANALYZE: Album Search (ILIKE '%Release%')
	runExplain(t, ctx, db, "Album ILIKE '%Release%'", `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT t.id, t.title FROM tracks t
		WHERE t.album ILIKE '%Release%'
		ORDER BY t.created_at DESC LIMIT 50;
	`)

	// 4. EXPLAIN ANALYZE: Lyrics Filter (lyrics_state = 'available_synced')
	runExplain(t, ctx, db, "Lyrics State = 'available_synced'", `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT t.id, t.title FROM tracks t
		WHERE t.lyrics_state = 'available_synced'
		ORDER BY t.created_at DESC LIMIT 50;
	`)

	// 5. EXPLAIN ANALYZE: Recent Sort (ORDER BY created_at DESC LIMIT 50)
	runExplain(t, ctx, db, "Recent Sort (idx_tracks_created_at)", `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT t.id, t.title FROM tracks t
		ORDER BY t.created_at DESC LIMIT 50;
	`)

	// 6. Test functional ListTracksFiltered on 10k tracks
	t0 := time.Now()
	tracks, total, err := catalog.ListTracksFiltered(ctx, TrackListFilter{
		Query: "Track #05000",
		Limit: 50,
	})
	dur := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(tracks) != 1 {
		t.Fatalf("expected 1 match for Track #05000 in 10k, got total=%d len=%d", total, len(tracks))
	}
	t.Logf("ListTracksFiltered with search in 10,000 tracks returned in %v (total=%d)", dur, total)
}

func runExplain(t *testing.T, ctx context.Context, db *database.DB, label, sqlQuery string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, sqlQuery)
	if err != nil {
		t.Fatalf("explain %s failed: %v", label, err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	t.Logf("--- EXPLAIN ANALYZE: %s ---\n%s\n", label, strings.Join(plan, "\n"))
}
