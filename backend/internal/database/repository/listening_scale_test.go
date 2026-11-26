package repository_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

// TestListeningAtLibraryScale builds a library of 150,000 tracks (3,000
// artists, 15,000 releases, a file each), a listening history of 5,000
// tracks with 30,000 timestamped plays and a lyrics index of 50,000 tracks,
// then times every listening query with a genre and an artist excluded.
// The bounds are generous (the CI runs with the race detector); the timings
// are logged with -v.
func TestListeningAtLibraryScale(t *testing.T) {
	if testing.Short() {
		t.Skip("library scale test")
	}
	db := dbtest.Open(t)
	ctx := context.Background()
	start := time.Now()
	for _, q := range []string{
		`INSERT INTO users(id,username,password_hash,role,enabled,created_at,updated_at) VALUES('scale_u','scale_u','hash','user',true,now(),now())`,
		`INSERT INTO artists(id,name,provider,source_id,genres_json,created_at,updated_at)
		 SELECT 'sa'||i,'Artist '||i,'fixture','sa'||i,
		  jsonb_build_array((ARRAY['Rock','Pop','Jazz','Schlager','Hip-Hop','Electronic','Folk','Metal','Klassik','Indie'])[1+i%10],
		                    (ARRAY['Pop','Rock','Soul','Dance','Rap','House','Country','Punk','Oper','Alternative'])[1+(i/10)%10]),now(),now()
		 FROM generate_series(1,3000) i`,
		`INSERT INTO releases(id,artist_id,title,provider,source_id,year,created_at,updated_at)
		 SELECT 'sr'||i,'sa'||(1+i%3000),'Album '||i,'fixture','sr'||i,1960+i%60,now(),now() FROM generate_series(1,15000) i`,
		`INSERT INTO tracks(id,release_id,artist_id,title,identity_key,duration_ms,lyrics_state,created_at,updated_at)
		 SELECT 'st'||i,'sr'||(1+i%15000),'sa'||(1+(1+i%15000)%3000),'Song '||i,'st'||i,200000,
		  CASE WHEN i%3=0 THEN 'available_plain' ELSE 'unknown' END,now(),now() FROM generate_series(1,150000) i`,
		`INSERT INTO files(id,track_id,path,created_at,updated_at) SELECT 'sf'||i,'st'||i,'s/'||i||'.flac',now()-interval '100 days',now() FROM generate_series(1,150000) i`,
		`INSERT INTO listening_history(user_id,track_id,play_count,last_played_at) SELECT 'scale_u','st'||(i*30),1+i%7,now()-(i%200)*interval '1 day' FROM generate_series(1,5000) i`,
		`INSERT INTO playback_events(user_id,event_id,track_id,played_at)
		 SELECT 'scale_u','se'||i,'st'||(30*(1+i%5000)),now()-(i%365)*interval '1 day'-(i%24)*interval '1 hour' FROM generate_series(1,30000) i`,
		`INSERT INTO favorite_tracks(user_id,track_id,created_at) SELECT 'scale_u','st'||(i*60),now()-interval '100 days' FROM generate_series(1,1000) i`,
		`INSERT INTO track_skips(user_id,track_id,skip_count,last_skipped_at) SELECT 'scale_u','st'||(i*90),2,now()-interval '3 days' FROM generate_series(1,500) i`,
		`INSERT INTO track_lyrics_search(track_id,body,document)
		 SELECT 'st'||i,b,to_tsvector('simple',lower(b)) FROM generate_series(3,150000,3) i,
		 LATERAL (SELECT 'zeile '||md5(i::text)||E'\n'||CASE WHEN i%150=0 THEN 'über den wolken muss die freiheit ' ELSE 'la la '||substr(md5((i+1)::text),1,8)||' ' END||md5((i+2)::text) AS b) x`,
		`ANALYZE`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	t.Logf("fixture built in %v", time.Since(start))

	c := repository.NewCatalog(db)
	for _, e := range [][2]string{{"genre", "Schlager"}, {"artist", "sa7"}, {"track", "st300"}} {
		if _, err := c.AddListeningExclusion(ctx, "scale_u", e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	day := time.Now().In(berlin)
	var mixes []repository.PersonalMix
	timings := []string{}
	measure := func(name string, limit time.Duration, fn func() error) {
		t.Helper()
		t0 := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		d := time.Since(t0)
		timings = append(timings, fmt.Sprintf("%s=%dms", name, d.Milliseconds()))
		if d > limit {
			t.Errorf("%s took %v (limit %v)", name, d, limit)
		}
	}
	measure("mixes", 10*time.Second, func() (err error) {
		mixes, err = c.PersonalMixesOn(ctx, "scale_u", day)
		if err == nil && len(mixes) < 5 {
			err = fmt.Errorf("only %d mixes", len(mixes))
		}
		return err
	})
	kinds := []string{}
	for _, m := range mixes {
		kinds = append(kinds, m.ID)
	}
	t.Logf("mixes: %s", strings.Join(kinds, " "))
	if len(mixes) > 0 {
		measure("mix_detail", 3*time.Second, func() error { _, err := c.PersonalMixOn(ctx, "scale_u", mixes[0].ID, day); return err })
	}
	radio := func(name string, req repository.RadioRequest) {
		measure(name, 5*time.Second, func() error {
			res, err := c.Radio(ctx, req)
			if err == nil && len(res.TrackIDs) == 0 {
				err = fmt.Errorf("empty radio")
			}
			return err
		})
	}
	radio("radio_personal", repository.RadioRequest{User: "scale_u", Nonce: "n"})
	radio("radio_genre_500", repository.RadioRequest{User: "scale_u", Genre: "Rock", Nonce: "n", Limit: 500})
	radio("radio_track", repository.RadioRequest{User: "scale_u", TrackID: "st31", Nonce: "n"})
	radio("radio_artist_500", repository.RadioRequest{User: "scale_u", ArtistID: "sa31", Nonce: "n", Limit: 500})
	radio("radio_release", repository.RadioRequest{User: "scale_u", ReleaseID: "sr31", Nonce: "n"})
	for _, p := range []string{"week", "year", "all"} {
		measure("stats_"+p, 3*time.Second, func() error { _, err := c.ListeningStatsAt(ctx, "scale_u", p, 10, time.Now(), berlin); return err })
	}
	measure("lyrics_phrase", 2*time.Second, func() error {
		hits, err := c.SearchLyrics(ctx, "über den wolken", 20)
		if err == nil && len(hits) != 20 {
			err = fmt.Errorf("%d hits", len(hits))
		}
		return err
	})
	measure("lyrics_common_prefix", 2*time.Second, func() error { _, err := c.SearchLyrics(ctx, "zei", 20); return err })
	measure("library_search", 3*time.Second, func() error { _, err := c.SearchLibrary(ctx, "Song 4711", 5); return err })
	measure("lyrics_index_scan", 2*time.Second, func() error { _, err := c.ListTracksNeedingLyricsIndex(ctx, 200); return err })
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("st%d", i+1)
	}
	measure("filter_excluded", time.Second, func() error { _, err := c.FilterExcludedTracks(ctx, "scale_u", ids); return err })
	t.Logf("timings: %s", strings.Join(timings, " "))
	// TEMPORARY: report the timings from CI.
	t.Errorf("timings: %s; mixes: %s", strings.Join(timings, " "), strings.Join(kinds, " "))
}
