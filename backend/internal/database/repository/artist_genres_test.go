package repository_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestArtistGenreBackfillCandidatesAndNeverOverwriteManual(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// A download stores its artist under a synthetic source; only the
	// release carries the Deezer album id.
	for _, a := range []struct{ id, name, provider, source, genres string }{
		{"g-album", "Daft Punk", "deezer", "artist:daft punk", "[]"},
		{"g-manual", "Manual", "deezer", "artist:manual", "[]"},
		{"g-has", "Has Genres", "deezer", "artist:has", `["Rock"]`},
		{"g-direct", "Direct", "deezer", "1424821", "[]"},
		{"g-none", "Nowhere", "local", "artist:nowhere", "[]"},
		{"g-sub", "Subscribed", "deezer", "artist:subscribed", "[]"},
		{"g-empty", "No Tracks", "deezer", "555", "[]"},
	} {
		exec(`INSERT INTO artists(id,name,provider,source_id,genres_json,created_at,updated_at) VALUES($1,$2,$3,$4,$5::jsonb,now(),now())`, a.id, a.name, a.provider, a.source, a.genres)
		exec(`INSERT INTO artist_sources(id,artist_id,provider,source_kind,source_id,created_at,updated_at) VALUES($1,$1,$2,CASE WHEN $3 LIKE 'artist:%' THEN 'legacy_synthetic' ELSE 'external' END,$3,now(),now())`, a.id, a.provider, a.source)
		if a.id != "g-empty" {
			exec(`INSERT INTO tracks(id,artist_id,title,identity_key,created_at,updated_at) VALUES($1,$1,'Song',$1,now(),now())`, a.id)
		}
	}
	exec(`UPDATE artists SET genres_manual=true WHERE id='g-manual'`)
	exec(`INSERT INTO releases(id,artist_id,title,provider,source_id,created_at,updated_at) VALUES('g-rel','g-album','Discovery','deezer','302127',now(),now()),('g-rel2','g-manual','X','deezer','1',now(),now())`)
	exec(`INSERT INTO artist_subscriptions(id,provider,artist_source_id,artist_name,next_sync_at,created_at,updated_at) VALUES('g-subscription','deezer','4050205','subscribed',now(),now(),now())`)

	got, err := c.ArtistsMissingGenres(ctx, "deezer", time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.GenreCandidate{
		{ArtistID: "g-album", Name: "Daft Punk", SourceAlbumID: "302127"},
		{ArtistID: "g-direct", Name: "Direct", SourceArtistID: "1424821"},
		{ArtistID: "g-sub", Name: "Subscribed", SourceArtistID: "4050205"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates %+v", got)
	}
	if got, _ = c.ArtistsMissingGenres(ctx, "deezer", time.Hour, 1); len(got) != 1 || got[0].ArtistID != "g-album" {
		t.Fatalf("limit ignored: %+v", got)
	}

	// Manual and existing genres are never overwritten, by the backfill or
	// by a provider artist arriving through a download.
	for _, id := range []string{"g-manual", "g-has"} {
		if err := c.StoreArtistGenreLookup(ctx, id, []string{"Dance"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.UpsertArtist(ctx, music.Artist{ID: "g-manual", Name: "Manual", Provider: "deezer", SourceID: "artist:manual", Genres: []string{"Pop"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.StoreArtistGenreLookup(ctx, "g-album", []string{"Dance", " dance ", "Electro"}); err != nil {
		t.Fatal(err)
	}
	if err := c.StoreArtistGenreLookup(ctx, "g-sub", nil); err != nil {
		t.Fatal(err)
	}
	for id, genres := range map[string][]string{"g-manual": {}, "g-has": {"Rock"}, "g-album": {"Dance", "Electro"}, "g-sub": {}} {
		a, err := c.GetArtist(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Genres) != len(genres) || (len(genres) > 0 && !reflect.DeepEqual(a.Genres, genres)) {
			t.Fatalf("%s genres %v, want %v", id, a.Genres, genres)
		}
	}

	// A recorded lookup is not repeated within the recheck window; an
	// artist without provider genres is asked again after it.
	if got, _ = c.ArtistsMissingGenres(ctx, "deezer", time.Hour, 10); len(got) != 1 || got[0].ArtistID != "g-direct" {
		t.Fatalf("after lookups: %+v", got)
	}
	exec(`UPDATE artist_genre_lookups SET checked_at=now()-interval '2 hours' WHERE artist_id='g-sub'`)
	if got, _ = c.ArtistsMissingGenres(ctx, "deezer", time.Hour, 10); len(got) != 2 || got[1].ArtistID != "g-sub" {
		t.Fatalf("recheck: %+v", got)
	}

	// A subscription sync finds its library artist by source or by name.
	if id, err := c.ArtistMissingGenresBySource(ctx, "deezer", "1424821", ""); err != nil || id != "g-direct" {
		t.Fatalf("by source: %q %v", id, err)
	}
	if id, err := c.ArtistMissingGenresBySource(ctx, "deezer", "4050205", "SUBSCRIBED"); err != nil || id != "g-sub" {
		t.Fatalf("by name: %q %v", id, err)
	}
	if id, err := c.ArtistMissingGenresBySource(ctx, "deezer", "302127", "Daft Punk"); err != nil || id != "" {
		t.Fatalf("artist with genres returned: %q %v", id, err)
	}
}
