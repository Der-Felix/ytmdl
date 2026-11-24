package repository_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

// setupMixLibrary creates two users and a library of 36 Rock tracks (six
// artists of six), 24 Jazz tracks (six artists of four) and 9 Folk tracks
// (three artists of three). Every file was added 100 days ago.
func setupMixLibrary(t *testing.T) (*database.DB, string, string, map[string][]string) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []string{"mix_a", "mix_b"} {
		exec(`INSERT INTO users(id,username,password_hash,role,enabled,created_at,updated_at) VALUES($1,$1,'hash','user',true,now(),now())`, u)
	}
	byGenre := map[string][]string{}
	for _, g := range []struct {
		name             string
		artists, perArts int
	}{{"Rock", 6, 6}, {"Jazz", 6, 4}, {"Folk", 3, 3}} {
		for a := range g.artists {
			artist := fmt.Sprintf("mix-%s-%d", g.name, a)
			exec(`INSERT INTO artists(id,name,provider,source_id,genres_json,created_at,updated_at) VALUES($1,$1,'fixture',$1,jsonb_build_array($2::text),now(),now())`, artist, g.name)
			exec(`INSERT INTO releases(id,artist_id,title,provider,source_id,created_at,updated_at) VALUES($1,$2,$1,'fixture',$1,now(),now())`, "rel-"+artist, artist)
			for n := range g.perArts {
				id := fmt.Sprintf("%s-t%d", artist, n)
				exec(`INSERT INTO tracks(id,release_id,artist_id,title,identity_key,created_at,updated_at) VALUES($1,$2,$3,$4,$1,now(),now())`, id, "rel-"+artist, artist, fmt.Sprintf("Song %d", n))
				exec(`INSERT INTO files(id,track_id,path,created_at,updated_at) VALUES($1,$2,$3,now()-interval '100 days',now())`, "file-"+id, id, id+".flac")
				byGenre[g.name] = append(byGenre[g.name], id)
			}
		}
	}
	return db, "mix_a", "mix_b", byGenre
}

func mixByKind(mixes []repository.PersonalMix, kind string) []repository.PersonalMix {
	out := []repository.PersonalMix{}
	for _, m := range mixes {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

func mixTracks(t *testing.T, c *repository.Catalog, user, id string, day time.Time) []string {
	t.Helper()
	d, err := c.PersonalMixOn(context.Background(), user, id, day)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, tr := range d.Tracks {
		ids = append(ids, tr.ID)
	}
	return ids
}

func TestPersonalMixesEmptyLibrary(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,enabled,created_at,updated_at) VALUES('lonely','lonely','hash','user',true,now(),now())`); err != nil {
		t.Fatal(err)
	}
	mixes, err := repository.NewCatalog(db).PersonalMixes(ctx, "lonely")
	if err != nil || mixes == nil || len(mixes) != 0 {
		t.Fatalf("want an empty list, got %v %v", mixes, err)
	}
}

func TestPersonalMixesColdStartCapsAndMinimum(t *testing.T) {
	db, user, _, _ := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	day := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mixes, err := c.PersonalMixesOn(ctx, user, day)
	if err != nil {
		t.Fatal(err)
	}
	// Folk has nine tracks and stays out; history-based mixes are absent.
	if len(mixes) != 2 || mixes[0].Genre != "Rock" || mixes[1].Genre != "Jazz" || mixes[0].Title != "Dein Mix 1" {
		t.Fatalf("cold start mixes: %+v", mixes)
	}
	for _, m := range mixes {
		if m.TrackCount != 18 || len(m.CoverTrackIDs) != 4 || len(m.CoverReleaseIDs) != 4 {
			t.Fatalf("mix %s: %d tracks, covers %v %v", m.ID, m.TrackCount, m.CoverTrackIDs, m.CoverReleaseIDs)
		}
		d, err := c.PersonalMixOn(ctx, user, m.ID, day)
		if err != nil {
			t.Fatal(err)
		}
		perArtist := map[string]int{}
		for _, tr := range d.Tracks {
			perArtist[tr.ReleaseID]++ // one release per artist in this fixture
		}
		for a, n := range perArtist {
			if n > 3 {
				t.Fatalf("artist %s has %d tracks in %s", a, n, m.ID)
			}
		}
	}
	if _, err := c.PersonalMixOn(ctx, user, "genre-000000000000", day); err == nil {
		t.Fatal("unknown mix found")
	}
	if _, err := c.PersonalMixOn(ctx, user, repository.MixKindRediscover, day); err == nil {
		t.Fatal("mix below the minimum served")
	}
}

func TestPersonalMixesStableForADay(t *testing.T) {
	db, user, _, _ := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	day := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	mixes, err := c.PersonalMixesOn(ctx, user, day)
	if err != nil || len(mixes) == 0 {
		t.Fatal(mixes, err)
	}
	id := mixes[0].ID
	morning, evening := mixTracks(t, c, user, id, day), mixTracks(t, c, user, id, day.Add(10*time.Hour))
	if !reflect.DeepEqual(morning, evening) {
		t.Fatal("mix changed within a day")
	}
	if next := mixTracks(t, c, user, id, day.Add(24*time.Hour)); reflect.DeepEqual(morning, next) {
		t.Fatal("mix did not change the next day")
	}
}

func TestPersonalMixesFollowHistoryAndSkips(t *testing.T) {
	db, user, other, byGenre := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	day := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	before, err := c.PersonalMixesOn(ctx, user, day)
	if err != nil {
		t.Fatal(err)
	}

	// The other user loves Jazz: played long ago, played a lot this month,
	// and skipped one track twice.
	jazz := byGenre["Jazz"]
	for _, id := range jazz {
		if _, err := db.ExecContext(ctx, `INSERT INTO listening_history(user_id,track_id,play_count,last_played_at) VALUES($1,$2,5,now()-interval '100 days')`, other, id); err != nil {
			t.Fatal(err)
		}
		for e := range 2 {
			if _, err := db.ExecContext(ctx, `INSERT INTO playback_events(user_id,event_id,track_id,played_at) VALUES($1,$2,$3,now()-interval '2 days')`, other, fmt.Sprintf("event-%s-%d", id, e), id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, ev := range []string{"skip-event-0000001", "skip-event-0000002", "skip-event-0000002"} {
		if err := c.RecordSkip(ctx, other, ev, jazz[1]); err != nil {
			t.Fatal(err)
		}
	}
	var skips int
	if err := db.QueryRowContext(ctx, `SELECT skip_count FROM track_skips WHERE user_id=$1 AND track_id=$2`, other, jazz[1]).Scan(&skips); err != nil || skips != 2 {
		t.Fatal("skip events not idempotent", skips, err)
	}

	// User isolation: the first user's mixes are untouched.
	after, err := c.PersonalMixesOn(ctx, user, day)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("another user's history changed the mixes: %v", err)
	}

	mixes, err := c.PersonalMixesOn(ctx, other, day)
	if err != nil {
		t.Fatal(err)
	}
	genre := mixByKind(mixes, repository.MixKindGenre)
	if len(genre) == 0 || genre[0].Genre != "Jazz" {
		t.Fatalf("Jazz is not the first genre mix: %+v", genre)
	}
	for _, kind := range []string{repository.MixKindRediscover, repository.MixKindOnRepeat} {
		if len(mixByKind(mixes, kind)) != 1 {
			t.Fatalf("mix %s missing: %+v", kind, mixes)
		}
	}
	// Every Jazz track was played, so there is nothing unheard by those artists.
	if len(mixByKind(mixes, repository.MixKindUnheard)) != 0 || len(mixByKind(mixes, repository.MixKindNew)) != 0 {
		t.Fatalf("unexpected mixes: %+v", mixes)
	}
	for _, m := range mixes {
		for _, id := range mixTracks(t, c, other, m.ID, day) {
			if id == jazz[1] {
				t.Fatalf("twice skipped track in %s", m.ID)
			}
		}
	}

	// Recently added files form "Neu in der Mediathek".
	if _, err := db.ExecContext(ctx, `UPDATE files SET created_at=now()-interval '2 days' WHERE track_id = ANY($1)`, byGenre["Rock"]); err != nil {
		t.Fatal(err)
	}
	mixes, err = c.PersonalMixesOn(ctx, other, day)
	if err != nil || len(mixByKind(mixes, repository.MixKindNew)) != 1 {
		t.Fatalf("new mix missing: %+v %v", mixes, err)
	}

	// Clearing the history forgets skips too.
	if err := c.ClearListeningHistory(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM track_skips WHERE user_id=$1)+(SELECT count(*) FROM skip_events WHERE user_id=$1)`, other).Scan(&skips); err != nil || skips != 0 {
		t.Fatal("skips survived clearing the history", skips, err)
	}
}
