package repository_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestCatalogLibrarySearchRanking(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// Setup artists: exact, prefix, substring
	// 1. "Beat" (exact match for query "Beat")
	// 2. "Beatles" (prefix match for query "Beat")
	// 3. "The Heartbeat" (substring match for query "Beat")
	artExact, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Beat",
		Provider: "test",
		SourceID: "art_exact",
	})
	if err != nil {
		t.Fatal(err)
	}
	artPrefix, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Beatles",
		Provider: "test",
		SourceID: "art_prefix",
	})
	if err != nil {
		t.Fatal(err)
	}
	artSub, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "The Heartbeat",
		Provider: "test",
		SourceID: "art_sub",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test SearchArtists ranking: exact > prefix > substring
	artists, err := catalog.SearchArtists(ctx, "Beat", 10)
	if err != nil {
		t.Fatalf("SearchArtists failed: %v", err)
	}
	if len(artists) != 3 {
		t.Fatalf("expected 3 artists, got %d", len(artists))
	}
	if artists[0].ID != artExact.ID {
		t.Errorf("expected exact match %q first, got %q", artExact.Name, artists[0].Name)
	}
	if artists[1].ID != artPrefix.ID {
		t.Errorf("expected prefix match %q second, got %q", artPrefix.Name, artists[1].Name)
	}
	if artists[2].ID != artSub.ID {
		t.Errorf("expected substring match %q third, got %q", artSub.Name, artists[2].Name)
	}

	// Setup releases
	relExact, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Discovery",
		AlbumArtist: "Daft Punk",
		Artists:     []string{"Daft Punk"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2001,
		Provider:    "test",
		SourceID:    "rel_exact",
	}, artExact.ID)
	if err != nil {
		t.Fatal(err)
	}
	relPrefix, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Discovery (Deluxe)",
		AlbumArtist: "Daft Punk",
		Artists:     []string{"Daft Punk"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2003,
		Provider:    "test",
		SourceID:    "rel_prefix",
	}, artExact.ID)
	if err != nil {
		t.Fatal(err)
	}
	relSub, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Rediscovery",
		AlbumArtist: "Other Artist",
		Artists:     []string{"Other Artist"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2010,
		Provider:    "test",
		SourceID:    "rel_sub",
	}, artPrefix.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Test SearchReleases ranking: exact > prefix > substring
	releases, err := catalog.SearchReleases(ctx, "Discovery", 10)
	if err != nil {
		t.Fatalf("SearchReleases failed: %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("expected 3 releases, got %d", len(releases))
	}
	if releases[0].ID != relExact.ID {
		t.Errorf("expected exact release %q first, got %q", relExact.Title, releases[0].Title)
	}
	if releases[1].ID != relPrefix.ID {
		t.Errorf("expected prefix release %q second, got %q", relPrefix.Title, releases[1].Title)
	}
	if releases[2].ID != relSub.ID {
		t.Errorf("expected substring release %q third, got %q", relSub.Title, releases[2].Title)
	}

	// Setup tracks
	trkExact, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Echoes",
		Album:       "Meddle",
		AlbumArtist: "Pink Floyd",
		Artists:     []string{"Pink Floyd"},
		Year:        1971,
		ISRC:        "GBAYE7100010",
	}, relExact.ID, artExact.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	trkPrefix, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Echoes of Silence",
		Album:       "Trilogy",
		AlbumArtist: "The Weeknd",
		Artists:     []string{"The Weeknd"},
		Year:        2011,
	}, relPrefix.ID, artPrefix.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	trkSub, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Distant Echoes",
		Album:       "Ambient Works",
		AlbumArtist: "Aphex Twin",
		Artists:     []string{"Aphex Twin"},
		Year:        1992,
	}, relSub.ID, artSub.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Test SearchTracks ranking: exact > prefix > substring
	tracks, err := catalog.SearchTracks(ctx, "Echoes", 10)
	if err != nil {
		t.Fatalf("SearchTracks failed: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(tracks))
	}
	if tracks[0].ID != trkExact.ID {
		t.Errorf("expected exact track %q first, got %q", trkExact.Title, tracks[0].Title)
	}
	if tracks[1].ID != trkPrefix.ID {
		t.Errorf("expected prefix track %q second, got %q", trkPrefix.Title, tracks[1].Title)
	}
	if tracks[2].ID != trkSub.ID {
		t.Errorf("expected substring track %q third, got %q", trkSub.Title, tracks[2].Title)
	}

	// 4. Test SearchLibrary combining all three
	res, err := catalog.SearchLibrary(ctx, "Echoes", 5)
	if err != nil {
		t.Fatalf("SearchLibrary failed: %v", err)
	}
	if len(res.Tracks) != 3 {
		t.Errorf("expected 3 tracks in omni-search, got %d", len(res.Tracks))
	}
	if res.Tracks[0].ID != trkExact.ID {
		t.Errorf("omni-search track order incorrect, expected %q first", trkExact.Title)
	}

	// 5. Test ListTracksFiltered with sort="relevance"
	filteredTracks, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		Query: "Echoes",
		Sort:  "relevance",
		Order: "asc",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListTracksFiltered relevance sort failed: %v", err)
	}
	if total != 3 || len(filteredTracks) != 3 {
		t.Fatalf("expected total=3 len=3, got total=%d len=%d", total, len(filteredTracks))
	}
	if filteredTracks[0].ID != trkExact.ID {
		t.Errorf("expected exact track %q first in relevance sort, got %q", trkExact.Title, filteredTracks[0].Title)
	}
	if filteredTracks[1].ID != trkPrefix.ID {
		t.Errorf("expected prefix track %q second in relevance sort, got %q", trkPrefix.Title, filteredTracks[1].Title)
	}

	_ = now
}

func TestCatalogLibrarySearchEscapingAndUnicode(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	// Special characters: %, _, \, quotes, and umlauts
	artSpecial, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Mötley Crüe 100%_Real",
		Provider: "test",
		SourceID: "art_special",
	})
	if err != nil {
		t.Fatal(err)
	}

	relSpecial, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Björk's 50% & \\_Mix",
		AlbumArtist: "Mötley Crüe 100%_Real",
		Artists:     []string{"Mötley Crüe 100%_Real"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1995,
		Provider:    "test",
		SourceID:    "rel_special",
	}, artSpecial.ID)
	if err != nil {
		t.Fatal(err)
	}

	trkSpecial, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Track 100%_Pure & Real",
		Album:       "Björk's 50% & \\_Mix",
		AlbumArtist: "Mötley Crüe 100%_Real",
		Artists:     []string{"Mötley Crüe 100%_Real"},
		Year:        1995,
	}, relSpecial.ID, artSpecial.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Search literal "%" should find the release/track with "%", not match everything wildcard style
	trks, err := catalog.SearchTracks(ctx, "100%", 10)
	if err != nil {
		t.Fatalf("SearchTracks literal %% failed: %v", err)
	}
	if len(trks) != 1 || trks[0].ID != trkSpecial.ID {
		t.Fatalf("expected exactly 1 track for '100%%', got %d", len(trks))
	}

	// 2. Search literal "_" should find the track with "_", not treat as single-char wildcard
	trks, err = catalog.SearchTracks(ctx, "%_Pure", 10)
	if err != nil {
		t.Fatalf("SearchTracks literal %%_ failed: %v", err)
	}
	if len(trks) != 1 || trks[0].ID != trkSpecial.ID {
		t.Fatalf("expected exactly 1 track for '%%_Pure', got %d", len(trks))
	}

	// 3. Search with umlaut / accents
	arts, err := catalog.SearchArtists(ctx, "Mötley", 10)
	if err != nil {
		t.Fatalf("SearchArtists umlaut failed: %v", err)
	}
	if len(arts) != 1 || arts[0].ID != artSpecial.ID {
		t.Fatalf("expected 1 artist for 'Mötley', got %d", len(arts))
	}

	// Case-insensitivity on umlaut
	arts, err = catalog.SearchArtists(ctx, "mötley", 10)
	if err != nil {
		t.Fatalf("SearchArtists lower umlaut failed: %v", err)
	}
	if len(arts) != 1 || arts[0].ID != artSpecial.ID {
		t.Fatalf("expected 1 artist for 'mötley', got %d", len(arts))
	}

	// 4. Apostrophe search
	rels, err := catalog.SearchReleases(ctx, "Björk's", 10)
	if err != nil {
		t.Fatalf("SearchReleases apostrophe failed: %v", err)
	}
	if len(rels) != 1 || rels[0].ID != relSpecial.ID {
		t.Fatalf("expected 1 release for Björk's, got %d", len(rels))
	}
}

func TestCatalogLibrarySearchFilterAndFavorites(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// Insert test users
	user1 := "u_search_test_1"
	user2 := "u_search_test_2"
	for _, u := range []string{user1, user2} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
			VALUES ($1, $2, 'hash', 'user', true, $3, $3)
			ON CONFLICT (id) DO NOTHING
		`, u, u, now)
		if err != nil {
			t.Fatalf("insert user %s: %v", u, err)
		}
	}

	art1, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Artist Alpha",
		Provider: "test",
		SourceID: "art_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	art2, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Artist Beta",
		Provider: "test",
		SourceID: "art_2",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel1, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Album 1999",
		AlbumArtist: "Artist Alpha",
		Artists:     []string{"Artist Alpha"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1999,
		Provider:    "test",
		SourceID:    "rel_1",
	}, art1.ID)
	if err != nil {
		t.Fatal(err)
	}
	rel2, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Album 2024",
		AlbumArtist: "Artist Beta",
		Artists:     []string{"Artist Beta"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2024,
		Provider:    "test",
		SourceID:    "rel_2",
	}, art2.ID)
	if err != nil {
		t.Fatal(err)
	}

	trk1, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Song Alpha One",
		Album:       "Album 1999",
		AlbumArtist: "Artist Alpha",
		Artists:     []string{"Artist Alpha"},
		Year:        1999,
	}, rel1.ID, art1.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalog.UpsertTrack(ctx, music.Track{
		Title:       "Song Alpha Two",
		Album:       "Album 1999",
		AlbumArtist: "Artist Alpha",
		Artists:     []string{"Artist Alpha"},
		Year:        1999,
	}, rel1.ID, art1.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalog.UpsertTrack(ctx, music.Track{
		Title:       "Song Beta One",
		Album:       "Album 2024",
		AlbumArtist: "Artist Beta",
		Artists:     []string{"Artist Beta"},
		Year:        2024,
	}, rel2.ID, art2.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Favorite trk1 for user1
	_, err = db.ExecContext(ctx, `
		INSERT INTO favorite_tracks (user_id, track_id, created_at)
		VALUES ($1, $2, $3)
	`, user1, trk1.ID, now)
	if err != nil {
		t.Fatalf("insert favorite: %v", err)
	}

	// 1. Filter by Year
	trks, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		Year:  1999,
		Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(trks) != 2 {
		t.Fatalf("expected 2 tracks for year 1999, got total=%d len=%d", total, len(trks))
	}

	// 2. Filter by Year + Artist
	trks, total, err = catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		ArtistID: art1.ID,
		Year:     1999,
		Limit:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("expected 2 tracks for art1 + 1999, got %d", total)
	}

	// 3. Filter by Favorite for user1 (should return trk1 only)
	trks, total, err = catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		FavoriteOnly: true,
		UserID:       user1,
		Limit:        50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(trks) != 1 || trks[0].ID != trk1.ID {
		t.Fatalf("expected 1 favorite track for user1 (trk1), got total=%d", total)
	}

	// 4. Filter by Favorite for user2 (should return 0)
	trks, total, err = catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		FavoriteOnly: true,
		UserID:       user2,
		Limit:        50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(trks) != 0 {
		t.Fatalf("expected 0 favorite tracks for user2, got total=%d len=%d", total, len(trks))
	}

	// 5. Filter by Favorite with empty UserID returns 0 tracks defensively
	trks, total, err = catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		FavoriteOnly: true,
		UserID:       "",
		Limit:        50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(trks) != 0 {
		t.Fatalf("expected 0 tracks for FavoriteOnly without UserID, got %d", total)
	}

	// 6. Max query length validation (>200 chars)
	longQuery := strings.Repeat("a", 201)
	_, _, err = catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		Query: longQuery,
	})
	if err == nil {
		t.Fatal("expected error for query > 200 chars, got nil")
	}

	_, err = catalog.SearchLibrary(ctx, longQuery, 10)
	if err == nil {
		t.Fatal("expected error for SearchLibrary query > 200 chars, got nil")
	}
}

func TestCatalogLibrarySearchAccentSemantics(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Beyoncé",
		Provider: "test",
		SourceID: "art_beyonce",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Exact query: "Beyoncé" -> MUST MATCH
	arts, err := catalog.SearchArtists(ctx, "Beyoncé", 10)
	if err != nil {
		t.Fatalf("SearchArtists 'Beyoncé': %v", err)
	}
	if len(arts) != 1 || arts[0].ID != art.ID {
		t.Fatalf("expected 'Beyoncé' to match stored 'Beyoncé', got %d results", len(arts))
	}

	// 2. Uppercase query: "BEYONCÉ" -> MUST MATCH (case-insensitive Unicode)
	arts, err = catalog.SearchArtists(ctx, "BEYONCÉ", 10)
	if err != nil {
		t.Fatalf("SearchArtists 'BEYONCÉ': %v", err)
	}
	if len(arts) != 1 || arts[0].ID != art.ID {
		t.Fatalf("expected 'BEYONCÉ' to match stored 'Beyoncé', got %d results", len(arts))
	}

	// 3. Unaccented query: "Beyonce" -> MUST NOT MATCH (accent folding is NOT enabled/claimed)
	arts, err = catalog.SearchArtists(ctx, "Beyonce", 10)
	if err != nil {
		t.Fatalf("SearchArtists 'Beyonce': %v", err)
	}
	if len(arts) != 0 {
		t.Fatalf("expected 'Beyonce' to NOT match stored 'Beyoncé' without accent-folding, got %d results", len(arts))
	}

	// 4. Extended Unicode case-folding matrix:
	// ärzte / ÄRZTE, österreich / ÖSTERREICH, über / ÜBER, été / ÉTÉ
	unicodePairs := []struct {
		stored string
		query  string
		source string
	}{
		{stored: "ärzte", query: "ÄRZTE", source: "art_aerzte"},
		{stored: "österreich", query: "ÖSTERREICH", source: "art_oesterreich"},
		{stored: "über", query: "ÜBER", source: "art_ueber"},
		{stored: "été", query: "ÉTÉ", source: "art_ete"},
	}

	for _, pair := range unicodePairs {
		storedArt, err := catalog.UpsertArtist(ctx, music.Artist{
			Name:     pair.stored,
			Provider: "test",
			SourceID: pair.source,
		})
		if err != nil {
			t.Fatalf("UpsertArtist '%s': %v", pair.stored, err)
		}

		res, err := catalog.SearchArtists(ctx, pair.query, 10)
		if err != nil {
			t.Fatalf("SearchArtists query '%s': %v", pair.query, err)
		}
		if len(res) != 1 || res[0].ID != storedArt.ID {
			t.Fatalf("expected query '%s' to match stored '%s', got %d results", pair.query, pair.stored, len(res))
		}
	}
}

func TestCatalogLibrarySearchLiteralEscapesFull(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	testLiterals := []string{
		"%",
		"_",
		"\\",
		"%%",
		"__",
		"100%",
		"a_b",
		"back\\slash",
		"'",
		"\"",
		");",
		"' OR 1=1 --",
	}

	for i, lit := range testLiterals {
		art, err := catalog.UpsertArtist(ctx, music.Artist{
			Name:     "Artist " + lit,
			Provider: "test",
			SourceID: fmt.Sprintf("art_lit_%d", i),
		})
		if err != nil {
			t.Fatalf("failed inserting artist for literal %q: %v", lit, err)
		}

		rel, err := catalog.UpsertRelease(ctx, music.Release{
			Title:       "Release " + lit,
			AlbumArtist: art.Name,
			Artists:     []string{art.Name},
			ReleaseType: music.ReleaseAlbum,
			Year:        2020,
			Provider:    "test",
			SourceID:    fmt.Sprintf("rel_lit_%d", i),
		}, art.ID)
		if err != nil {
			t.Fatalf("failed inserting release for literal %q: %v", lit, err)
		}

		trk, err := catalog.UpsertTrack(ctx, music.Track{
			Title:       "Track " + lit,
			Album:       rel.Title,
			AlbumArtist: art.Name,
			Artists:     []string{art.Name},
			Year:        2020,
		}, rel.ID, art.ID, 0)
		if err != nil {
			t.Fatalf("failed inserting track for literal %q: %v", lit, err)
		}

		// Exact search by the literal should match ONLY this track
		resTracks, err := catalog.SearchTracks(ctx, lit, 50)
		if err != nil {
			t.Fatalf("SearchTracks failed on literal %q: %v", lit, err)
		}
		found := false
		for _, tr := range resTracks {
			if tr.ID == trk.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("literal %q not found in SearchTracks results (got %d tracks)", lit, len(resTracks))
		}

		// ListTracksFiltered with query = lit
		filtered, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
			Query: lit,
			Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListTracksFiltered failed on literal %q: %v", lit, err)
		}
		if total < 1 || len(filtered) < 1 {
			t.Errorf("expected at least 1 track for query %q, got total=%d", lit, total)
		}
	}
}

func TestCatalogLibrarySearchDeterministicTieBreakers(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Common Artist",
		Provider: "test",
		SourceID: "art_tie",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Common Release",
		AlbumArtist: art.Name,
		Artists:     []string{art.Name},
		ReleaseType: music.ReleaseAlbum,
		Year:        2021,
		Provider:    "test",
		SourceID:    "rel_tie",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = rel

	// Insert 5 distinct tracks by 5 different artists with the EXACT same title
	var trackIDs []string
	for i := 1; i <= 5; i++ {
		time.Sleep(2 * time.Millisecond) // slight timestamp difference
		coverArt, err := catalog.UpsertArtist(ctx, music.Artist{
			Name:     fmt.Sprintf("Cover Artist %d", i),
			Provider: "test",
			SourceID: fmt.Sprintf("art_cover_%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		coverRel, err := catalog.UpsertRelease(ctx, music.Release{
			Title:       fmt.Sprintf("Cover Album %d", i),
			AlbumArtist: coverArt.Name,
			Artists:     []string{coverArt.Name},
			ReleaseType: music.ReleaseAlbum,
			Year:        2021,
			Provider:    "test",
			SourceID:    fmt.Sprintf("rel_cover_%d", i),
		}, coverArt.ID)
		if err != nil {
			t.Fatal(err)
		}
		trk, err := catalog.UpsertTrack(ctx, music.Track{
			Title:       "Identical Track Title",
			Album:       coverRel.Title,
			AlbumArtist: coverArt.Name,
			Artists:     []string{coverArt.Name},
			Year:        2021,
		}, coverRel.ID, coverArt.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		trackIDs = append(trackIDs, trk.ID)
	}

	// Query multiple times, assert identical ordering each time
	firstRun, err := catalog.SearchTracks(ctx, "Identical Track Title", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstRun) != 5 {
		t.Fatalf("expected 5 tracks, got %d", len(firstRun))
	}

	for run := 1; run <= 3; run++ {
		repeatRun, err := catalog.SearchTracks(ctx, "Identical Track Title", 10)
		if err != nil {
			t.Fatal(err)
		}
		for idx := range firstRun {
			if firstRun[idx].ID != repeatRun[idx].ID {
				t.Fatalf("run %d tie-breaker non-deterministic at index %d: %s vs %s",
					run, idx, firstRun[idx].ID, repeatRun[idx].ID)
			}
		}
	}
}

func TestCatalogLibraryPaginationMultiPage(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()

	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Pagination Artist",
		Provider: "test",
		SourceID: "art_pagi",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Pagination Album",
		AlbumArtist: art.Name,
		Artists:     []string{art.Name},
		ReleaseType: music.ReleaseAlbum,
		Year:        2022,
		Provider:    "test",
		SourceID:    "rel_pagi",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Insert 12 tracks for this release
	totalTracks := 12
	insertedIDs := make(map[string]bool)
	for i := 1; i <= totalTracks; i++ {
		time.Sleep(2 * time.Millisecond)
		trk, err := catalog.UpsertTrack(ctx, music.Track{
			Title:       fmt.Sprintf("PagiTrack %02d", i),
			Album:       rel.Title,
			AlbumArtist: art.Name,
			Artists:     []string{art.Name},
			Year:        2022,
		}, rel.ID, art.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		insertedIDs[trk.ID] = true
	}

	pageSize := 5
	seenIDs := make(map[string]bool)
	var allRetrieved []music.LibraryTrack

	// Page 1 (offset 0, limit 5)
	p1, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		ArtistID: art.ID,
		Sort:     "recent",
		Order:    "desc",
		Limit:    pageSize,
		Offset:   0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != totalTracks {
		t.Fatalf("expected total=%d, got %d", totalTracks, total)
	}
	if len(p1) != 5 {
		t.Fatalf("expected page 1 len=5, got %d", len(p1))
	}
	for _, tr := range p1 {
		if seenIDs[tr.ID] {
			t.Fatalf("duplicate track ID %s on page 1", tr.ID)
		}
		seenIDs[tr.ID] = true
		allRetrieved = append(allRetrieved, tr)
	}

	// Page 2 (offset 5, limit 5)
	p2, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		ArtistID: art.ID,
		Sort:     "recent",
		Order:    "desc",
		Limit:    pageSize,
		Offset:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p2) != 5 {
		t.Fatalf("expected page 2 len=5, got %d", len(p2))
	}
	for _, tr := range p2 {
		if seenIDs[tr.ID] {
			t.Fatalf("duplicate track ID %s on page 2", tr.ID)
		}
		seenIDs[tr.ID] = true
		allRetrieved = append(allRetrieved, tr)
	}

	// Page 3 (offset 10, limit 5) -> remaining 2 tracks
	p3, total, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		ArtistID: art.ID,
		Sort:     "recent",
		Order:    "desc",
		Limit:    pageSize,
		Offset:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p3) != 2 {
		t.Fatalf("expected page 3 len=2, got %d", len(p3))
	}
	for _, tr := range p3 {
		if seenIDs[tr.ID] {
			t.Fatalf("duplicate track ID %s on page 3", tr.ID)
		}
		seenIDs[tr.ID] = true
		allRetrieved = append(allRetrieved, tr)
	}

	// Page 4 (offset 15, limit 5) -> 0 tracks
	p4, _, err := catalog.ListTracksFiltered(ctx, repository.TrackListFilter{
		ArtistID: art.ID,
		Sort:     "recent",
		Order:    "desc",
		Limit:    pageSize,
		Offset:   15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p4) != 0 {
		t.Fatalf("expected page 4 len=0, got %d", len(p4))
	}

	// Ensure all 12 tracks were retrieved exactly once without gaps or duplicates
	if len(allRetrieved) != totalTracks {
		t.Fatalf("expected total %d tracks retrieved across pages, got %d", totalTracks, len(allRetrieved))
	}
	for id := range insertedIDs {
		if !seenIDs[id] {
			t.Fatalf("track %s was skipped during pagination", id)
		}
	}
}

func TestCatalogLibraryCombinedFilterMatrix(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()
	now := time.Now().UTC()

	userID := "u_matrix_user"
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES ($1, $2, 'hash', 'user', true, $3, $3)
		ON CONFLICT (id) DO NOTHING
	`, userID, userID, now)
	if err != nil {
		t.Fatal(err)
	}

	artA, _ := catalog.UpsertArtist(ctx, music.Artist{Name: "Artist Matrix A", Provider: "test", SourceID: "art_mat_a"})
	artB, _ := catalog.UpsertArtist(ctx, music.Artist{Name: "Artist Matrix B", Provider: "test", SourceID: "art_mat_b"})

	relA1, _ := catalog.UpsertRelease(ctx, music.Release{Title: "Rel Matrix 2020", AlbumArtist: artA.Name, Year: 2020, Provider: "test", SourceID: "rel_mat_a1"}, artA.ID)
	relA2, _ := catalog.UpsertRelease(ctx, music.Release{Title: "Rel Matrix 2023", AlbumArtist: artA.Name, Year: 2023, Provider: "test", SourceID: "rel_mat_a2"}, artA.ID)
	relB1, _ := catalog.UpsertRelease(ctx, music.Release{Title: "Rel Matrix Other", AlbumArtist: artB.Name, Year: 2020, Provider: "test", SourceID: "rel_mat_b1"}, artB.ID)

	// Target track: matches ALL filter criteria
	trTarget, _ := catalog.UpsertTrack(ctx, music.Track{
		Title: "Target Matrix Song", Album: relA1.Title, AlbumArtist: artA.Name, Artists: []string{artA.Name}, Year: 2020,
	}, relA1.ID, artA.ID, 0)

	// Distractor 1: matches Artist + Rel + Year, but different title
	trDist1, _ := catalog.UpsertTrack(ctx, music.Track{
		Title: "Different Song One", Album: relA1.Title, AlbumArtist: artA.Name, Artists: []string{artA.Name}, Year: 2020,
	}, relA1.ID, artA.ID, 0)

	// Distractor 2: matches Query + Artist, but different release & year
	trDist2, _ := catalog.UpsertTrack(ctx, music.Track{
		Title: "Target Matrix Echo", Album: relA2.Title, AlbumArtist: artA.Name, Artists: []string{artA.Name}, Year: 2023,
	}, relA2.ID, artA.ID, 0)

	// Distractor 3: matches Query + Year, but different artist
	trDist3, _ := catalog.UpsertTrack(ctx, music.Track{
		Title: "Target Matrix Beat", Album: relB1.Title, AlbumArtist: artB.Name, Artists: []string{artB.Name}, Year: 2020,
	}, relB1.ID, artB.ID, 0)

	// Favorite only trTarget and trDist2
	_, _ = db.ExecContext(ctx, `INSERT INTO favorite_tracks (user_id, track_id, created_at) VALUES ($1, $2, $3)`, userID, trTarget.ID, now)
	_, _ = db.ExecContext(ctx, `INSERT INTO favorite_tracks (user_id, track_id, created_at) VALUES ($1, $2, $3)`, userID, trDist2.ID, now)

	_ = trDist1
	_ = trDist3

	tests := []struct {
		name        string
		filter      repository.TrackListFilter
		expectedIDs []string
	}{
		{
			name:        "q only",
			filter:      repository.TrackListFilter{Query: "Target Matrix"},
			expectedIDs: []string{trTarget.ID, trDist2.ID, trDist3.ID},
		},
		{
			name:        "artist only",
			filter:      repository.TrackListFilter{ArtistID: artA.ID},
			expectedIDs: []string{trTarget.ID, trDist1.ID, trDist2.ID},
		},
		{
			name:        "release only",
			filter:      repository.TrackListFilter{ReleaseID: relA1.ID},
			expectedIDs: []string{trTarget.ID, trDist1.ID},
		},
		{
			name:        "year only",
			filter:      repository.TrackListFilter{Year: 2020},
			expectedIDs: []string{trTarget.ID, trDist1.ID, trDist3.ID},
		},
		{
			name:        "favorite only",
			filter:      repository.TrackListFilter{FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID, trDist2.ID},
		},
		{
			name:        "q + artist",
			filter:      repository.TrackListFilter{Query: "Target Matrix", ArtistID: artA.ID},
			expectedIDs: []string{trTarget.ID, trDist2.ID},
		},
		{
			name:        "q + release",
			filter:      repository.TrackListFilter{Query: "Target Matrix", ReleaseID: relA1.ID},
			expectedIDs: []string{trTarget.ID},
		},
		{
			name:        "q + year",
			filter:      repository.TrackListFilter{Query: "Target Matrix", Year: 2020},
			expectedIDs: []string{trTarget.ID, trDist3.ID},
		},
		{
			name:        "q + favorite",
			filter:      repository.TrackListFilter{Query: "Target Matrix", FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID, trDist2.ID},
		},
		{
			name:        "artist + release",
			filter:      repository.TrackListFilter{ArtistID: artA.ID, ReleaseID: relA1.ID},
			expectedIDs: []string{trTarget.ID, trDist1.ID},
		},
		{
			name:        "artist + favorite",
			filter:      repository.TrackListFilter{ArtistID: artA.ID, FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID, trDist2.ID},
		},
		{
			name:        "release + favorite",
			filter:      repository.TrackListFilter{ReleaseID: relA1.ID, FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID},
		},
		{
			name:        "q + artist + release",
			filter:      repository.TrackListFilter{Query: "Target Matrix", ArtistID: artA.ID, ReleaseID: relA1.ID},
			expectedIDs: []string{trTarget.ID},
		},
		{
			name:        "q + artist + favorite",
			filter:      repository.TrackListFilter{Query: "Target Matrix", ArtistID: artA.ID, FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID, trDist2.ID},
		},
		{
			name:        "q + release + favorite",
			filter:      repository.TrackListFilter{Query: "Target Matrix", ReleaseID: relA1.ID, FavoriteOnly: true, UserID: userID},
			expectedIDs: []string{trTarget.ID},
		},
		{
			name: "q + artist + release + year + favorite (all combined)",
			filter: repository.TrackListFilter{
				Query:        "Target Matrix",
				ArtistID:     artA.ID,
				ReleaseID:    relA1.ID,
				Year:         2020,
				FavoriteOnly: true,
				UserID:       userID,
			},
			expectedIDs: []string{trTarget.ID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.filter.Limit = 50
			results, total, err := catalog.ListTracksFiltered(ctx, tc.filter)
			if err != nil {
				t.Fatalf("filter failed: %v", err)
			}
			if total != len(tc.expectedIDs) || len(results) != len(tc.expectedIDs) {
				t.Fatalf("expected %d tracks, got total=%d len=%d", len(tc.expectedIDs), total, len(results))
			}
			resMap := make(map[string]bool)
			for _, r := range results {
				resMap[r.ID] = true
			}
			for _, expID := range tc.expectedIDs {
				if !resMap[expID] {
					t.Errorf("expected track %s in results, but missing", expID)
				}
			}
		})
	}
}
