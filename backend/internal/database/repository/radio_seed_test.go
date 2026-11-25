package repository_test

import (
	"context"
	"testing"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

// radioFixture adds artists, releases and tracks with local files for the
// seeded radio tests.
type radioFixture struct {
	t        *testing.T
	db       *database.DB
	artistOf map[string]string
}

func (f *radioFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(context.Background(), query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *radioFixture) artist(id, genres string) {
	f.t.Helper()
	f.exec(`INSERT INTO artists(id,name,provider,source_id,genres_json,created_at,updated_at) VALUES($1,$1,'fixture',$1,$2::jsonb,now(),now())`, id, genres)
}

func (f *radioFixture) release(id, artist string, year int) {
	f.t.Helper()
	f.exec(`INSERT INTO releases(id,artist_id,title,provider,source_id,year,created_at,updated_at) VALUES($1,$2,$1,'fixture',$1,$3,now(),now())`, id, artist, year)
}

func (f *radioFixture) track(id, release, artist, title string, year int) {
	f.t.Helper()
	f.exec(`INSERT INTO tracks(id,release_id,artist_id,title,identity_key,year,created_at,updated_at) VALUES($1,$2,$3,$4,$1,$5,now(),now())`, id, release, artist, title, year)
	f.exec(`INSERT INTO files(id,track_id,path,created_at,updated_at) VALUES($1,$2,$3,now(),now())`, "file-"+id, id, "radio/"+id+".opus")
	f.artistOf[id] = artist
}

func radioIDs(tracks []music.LibraryTrack) []string {
	out := make([]string, len(tracks))
	for i, tr := range tracks {
		out[i] = tr.ID
	}
	return out
}

// TestLocalRadioStaysNearTheSeed pins the tiers of the seeded radio: a party
// schlager seed listed as "Schlager, Pop" must not drift to an artist that
// only shares "Pop".
func TestLocalRadioStaysNearTheSeed(t *testing.T) {
	db, _, user, _, _ := setupPlaylistTest(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	f := &radioFixture{t: t, db: db, artistOf: map[string]string{}}

	f.artist("seed-artist", `["Schlager","Pop"]`)
	f.artist("same-main", `["schlager"]`)
	f.artist("secondary", `["Pop","Schlager"]`)
	f.artist("pop-only", `["Pop"]`)
	f.artist("dance-pop", `["Dance","Pop"]`)
	f.artist("compilation-mate", `[]`)
	f.artist("stranger", `[]`)

	f.release("seed-album", "seed-artist", 2020)
	f.release("party-hits", "seed-artist", 2021)
	f.release("same-main-album", "same-main", 2019)
	f.release("secondary-album", "secondary", 2019)
	f.release("pop-album", "pop-only", 1976)
	f.release("dance-album", "dance-pop", 2020)
	f.release("stranger-album", "stranger", 2020)

	f.track("seed", "seed-album", "seed-artist", "Seed Song", 2020)
	for _, id := range []string{"s2", "s3", "s4", "s5", "s6", "s7"} {
		f.track(id, "seed-album", "seed-artist", "Song "+id, 2020)
	}
	// Another version of the seed itself is not played right after it.
	f.track("seed-live", "party-hits", "seed-artist", " seed song ", 2021)
	// A compilation: an artist without genres that shares it is close.
	f.track("s8", "party-hits", "seed-artist", "Song s8", 2021)
	f.track("mate", "party-hits", "compilation-mate", "Mate Song", 2021)
	f.track("era-near", "same-main-album", "same-main", "Near", 2019)
	f.track("era-far", "same-main-album", "same-main", "Far", 1985)
	f.track("sec1", "secondary-album", "secondary", "Sec 1", 2019)
	f.track("sec2", "secondary-album", "secondary", "Sec 2", 2019)
	f.track("abba", "pop-album", "pop-only", "Pop Song", 1976)
	f.track("dance", "dance-album", "dance-pop", "Dance Song", 2020)
	f.track("strange", "stranger-album", "stranger", "Strange Song", 2020)

	for _, nonce := range []string{"a", "b", "c", "d", "e"} {
		radio, err := c.LocalRadio(ctx, user, "seed", "", nonce)
		if err != nil {
			t.Fatal(err)
		}
		pos := map[string]int{}
		perArtist := map[string]int{}
		for i, tr := range radio {
			pos[tr.ID] = i
			perArtist[f.artistOf[tr.ID]]++
		}
		for _, banned := range []string{"seed", "seed-live", "abba", "dance", "strange", "trk_pl_1"} {
			if _, ok := pos[banned]; ok {
				t.Fatalf("nonce %s: %s is not near the seed: %v", nonce, banned, radioIDs(radio))
			}
		}
		// Tier 1: five of the seed artist's seven other songs, both songs of
		// the artist with the same main genre and the compilation mate.
		// Tier 2: the artist that lists the seed's main genre second.
		if len(radio) != 10 || perArtist["seed-artist"] != 5 || perArtist["same-main"] != 2 || perArtist["compilation-mate"] != 1 || perArtist["secondary"] != 2 {
			t.Fatalf("nonce %s: unexpected radio %v", nonce, radioIDs(radio))
		}
		if pos["sec1"] < 8 || pos["sec2"] < 8 {
			t.Fatalf("nonce %s: related tier played before the close tier was used up: %v", nonce, radioIDs(radio))
		}
		if pos["era-near"] > pos["era-far"] {
			t.Fatalf("nonce %s: the seed's era did not rank first: %v", nonce, radioIDs(radio))
		}
		seedArtistFirst := 0
		for _, tr := range radio[:6] {
			if f.artistOf[tr.ID] == "seed-artist" {
				seedArtistFirst++
			}
		}
		if seedArtistFirst != 3 {
			t.Fatalf("nonce %s: per-artist cap not applied before other artists: %v", nonce, radioIDs(radio))
		}
	}

	// A seed whose artist has no genres keeps to the artist and the artists
	// it shares releases with instead of the whole library.
	f.track("mate-2", "party-hits", "compilation-mate", "Mate Two", 2021)
	radio, err := c.LocalRadio(ctx, user, "mate", "", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(radio) == 0 {
		t.Fatal("genre-less seed found nothing")
	}
	for _, tr := range radio {
		if a := f.artistOf[tr.ID]; a != "compilation-mate" && a != "seed-artist" {
			t.Fatalf("genre-less seed drifted to %q: %v", a, radioIDs(radio))
		}
	}

	// The genre filter still applies to a seeded radio.
	filtered, err := c.LocalRadio(ctx, user, "seed", "SCHLAGER", "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) == 0 {
		t.Fatal("genre filter removed everything")
	}
	for _, tr := range filtered {
		if a := f.artistOf[tr.ID]; a == "compilation-mate" || a == "" {
			t.Fatalf("genre filter ignored: %v", radioIDs(filtered))
		}
	}
}
