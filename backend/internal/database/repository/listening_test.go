package repository_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func TestListeningExclusionsCRUDAndOwnership(t *testing.T) {
	db, user, other, byGenre := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()

	a, err := c.AddListeningExclusion(ctx, user, "artist", "mix-Jazz-0")
	if err != nil || a.Kind != repository.ExclusionArtist || a.Target != "mix-Jazz-0" || a.Name != "mix-Jazz-0" || a.ID == "" {
		t.Fatalf("artist exclusion %+v %v", a, err)
	}
	again, err := c.AddListeningExclusion(ctx, user, "artist", "mix-Jazz-0")
	if err != nil || again.ID != a.ID {
		t.Fatal("adding twice is not idempotent", again, err)
	}
	g, err := c.AddListeningExclusion(ctx, user, "genre", " rock ")
	if err != nil || g.Target != "rock" {
		t.Fatal(g, err)
	}
	if g2, err := c.AddListeningExclusion(ctx, user, "genre", "ROCK"); err != nil || g2.ID != g.ID {
		t.Fatal("genre exclusion is not case-insensitive", g2, err)
	}
	folk := byGenre["Folk"][0]
	tr, err := c.AddListeningExclusion(ctx, user, "track", folk)
	if err != nil || tr.Name != "Song 0" || tr.ArtistName != "mix-Folk-0" {
		t.Fatalf("track exclusion %+v %v", tr, err)
	}
	for _, bad := range []struct {
		kind, target string
		code         apperr.Code
	}{
		{"artist", "missing", apperr.CodeArtistNotFound},
		{"track", "missing", apperr.CodeTrackNotFound},
		{"mood", "calm", apperr.CodeInvalidRequest},
		{"genre", "", apperr.CodeInvalidRequest},
	} {
		if _, err := c.AddListeningExclusion(ctx, user, bad.kind, bad.target); apperr.CodeOf(err) != bad.code {
			t.Fatalf("%s %q: %v", bad.kind, bad.target, err)
		}
	}

	list, err := c.ListeningExclusions(ctx, user)
	if err != nil || len(list) != 3 {
		t.Fatalf("list %+v %v", list, err)
	}
	if list, err := c.ListeningExclusions(ctx, other); err != nil || list == nil || len(list) != 0 {
		t.Fatal("another user's exclusions listed", list, err)
	}
	if err := c.RemoveListeningExclusion(ctx, other, a.ID); apperr.CodeOf(err) != apperr.CodePreferenceNotFound {
		t.Fatal("another user removed an exclusion", err)
	}
	if err := c.RemoveListeningExclusion(ctx, user, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveListeningExclusion(ctx, user, a.ID); apperr.CodeOf(err) != apperr.CodePreferenceNotFound {
		t.Fatal("removed twice", err)
	}
	if err := c.RemoveListeningExclusionByTarget(ctx, user, "genre", "Rock"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveListeningExclusionByTarget(ctx, user, "genre", "Rock"); err != nil {
		t.Fatal("removing a missing exclusion by target must succeed", err)
	}
	if list, _ := c.ListeningExclusions(ctx, user); len(list) != 1 || list[0].ID != tr.ID {
		t.Fatalf("left %+v", list)
	}
	// The exclusion goes with its track.
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id=$1`, folk); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.ListeningExclusions(ctx, user); len(list) != 0 {
		t.Fatalf("exclusion of a deleted track kept: %+v", list)
	}
}

func allMixTracks(t *testing.T, c *repository.Catalog, user string, day time.Time) (map[string][]string, []repository.PersonalMix) {
	t.Helper()
	mixes, err := c.PersonalMixesOn(context.Background(), user, day)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, m := range mixes {
		out[m.ID] = mixTracks(t, c, user, m.ID, day)
	}
	return out, mixes
}

func TestExclusionsKeepMixesAndRadioClean(t *testing.T) {
	db, user, other, byGenre := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	day := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// A second version of Jazz-1's "Song 0".
	if _, err := db.ExecContext(ctx, `INSERT INTO tracks(id,release_id,artist_id,title,identity_key,created_at,updated_at) VALUES('jazz-dup','rel-mix-Jazz-1','mix-Jazz-1',' song 0','jazz-dup',now(),now());
 INSERT INTO files(id,track_id,path,created_at,updated_at) VALUES('file-jazz-dup','jazz-dup','jazz-dup.flac',now()-interval '100 days',now())`); err != nil {
		t.Fatal(err)
	}
	for _, e := range [][2]string{{"genre", "Rock"}, {"artist", "mix-Jazz-0"}, {"track", byGenre["Jazz"][4]}} {
		if _, err := c.AddListeningExclusion(ctx, user, e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	banned := map[string]bool{"jazz-dup": true, byGenre["Jazz"][4]: true}
	for _, id := range byGenre["Rock"] {
		banned[id] = true
	}
	for _, id := range byGenre["Jazz"][:4] {
		banned[id] = true
	}

	tracks, mixes := allMixTracks(t, c, user, day)
	if len(mixByKind(mixes, repository.MixKindGenre)) != 1 || mixByKind(mixes, repository.MixKindGenre)[0].Genre != "Jazz" {
		t.Fatalf("excluded genre still has a mix: %+v", mixes)
	}
	for id, ids := range tracks {
		for _, tr := range ids {
			if banned[tr] {
				t.Fatalf("excluded track %s in mix %s", tr, id)
			}
		}
	}
	// The other user is not affected.
	if _, otherMixes := allMixTracks(t, c, other, day); len(mixByKind(otherMixes, repository.MixKindGenre)) != 2 {
		t.Fatalf("exclusions leaked to another user: %+v", otherMixes)
	}

	for _, req := range []repository.RadioRequest{
		{User: user, Nonce: "a", Limit: 500},
		{User: user, TrackID: byGenre["Jazz"][5], Nonce: "b", Limit: 500},
		{User: user, ArtistID: "mix-Jazz-2", Nonce: "c", Limit: 500},
	} {
		res, err := c.Radio(ctx, req)
		if err != nil || len(res.TrackIDs) == 0 {
			t.Fatal("radio", req, res, err)
		}
		for _, id := range res.TrackIDs {
			if banned[id] {
				t.Fatalf("excluded track %s in radio %+v", id, req)
			}
		}
	}
	if res, err := c.Radio(ctx, repository.RadioRequest{User: user, Genre: "rock"}); err != nil || len(res.TrackIDs) != 0 {
		t.Fatal("a radio of an excluded genre played", res, err)
	}
	if res, err := c.Radio(ctx, repository.RadioRequest{User: other, Genre: "rock"}); err != nil || len(res.TrackIDs) != 36 {
		t.Fatal("another user's radio lost tracks", res, err)
	}
	kept, err := c.FilterExcludedTracks(ctx, user, []string{byGenre["Jazz"][8], byGenre["Rock"][0], "jazz-dup", byGenre["Folk"][0]})
	if err != nil || !reflect.DeepEqual(kept, []string{byGenre["Jazz"][8], byGenre["Folk"][0]}) {
		t.Fatal("filter", kept, err)
	}
	// Explicit playback stays possible.
	if got, err := c.TracksByIDs(ctx, []string{byGenre["Rock"][0]}); err != nil || len(got) != 1 {
		t.Fatal("an excluded track can no longer be loaded", err)
	}
}

func TestNewMixKinds(t *testing.T) {
	db, user, other, _ := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE tracks SET year=1994 WHERE artist_id LIKE 'mix-Rock-%'`)
	exec(`UPDATE releases SET year=1962 WHERE artist_id LIKE 'mix-Jazz-%'`)
	exec(`INSERT INTO listening_history(user_id,track_id,play_count,last_played_at) SELECT $1,id,3,now()-interval '100 days' FROM tracks WHERE artist_id LIKE 'mix-Rock-%'`, user)
	exec(`INSERT INTO listening_history(user_id,track_id,play_count,last_played_at) SELECT $1,id,1,now()-interval '2 days' FROM tracks WHERE artist_id IN('mix-Jazz-0','mix-Jazz-1','mix-Jazz-2','mix-Jazz-3')`, user)
	exec(`INSERT INTO favorite_tracks(user_id,track_id,created_at) SELECT $1,id,now()-interval '60 days' FROM tracks WHERE artist_id IN('mix-Rock-0','mix-Rock-1','mix-Rock-2','mix-Rock-3')`, user)
	// Mornings (09:00 UTC) three and four days ago: Rock-2 to Rock-5.
	for _, back := range []int{3, 4} {
		d := time.Now().UTC().AddDate(0, 0, -back)
		at := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, time.UTC)
		exec(`INSERT INTO playback_events(user_id,event_id,track_id,played_at) SELECT $1::text,'morning-'||$2::text||'-'||id,id,$3::timestamptz FROM tracks WHERE artist_id IN('mix-Rock-2','mix-Rock-3','mix-Rock-4','mix-Rock-5')`, user, fmt.Sprint(back), at)
	}
	day := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	mixes, err := c.PersonalMixesOn(ctx, user, day)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		repository.MixKindDaypart: 1, repository.MixKindArtist: 3, repository.MixKindDecade: 2,
		repository.MixKindFavorites: 1, repository.MixKindForgottenFavorites: 1, repository.MixKindRediscover: 1, repository.MixKindOnRepeat: 1,
	}
	for kind, n := range want {
		if got := mixByKind(mixes, kind); len(got) != n {
			t.Fatalf("%d %s mixes, want %d: %+v", len(got), kind, n, mixes)
		}
	}
	daypart := mixByKind(mixes, repository.MixKindDaypart)[0]
	if daypart.ID != "daypart-morning" || daypart.Daypart != "morning" || daypart.Title != "Dein Morgen-Mix" || daypart.Group != repository.MixGroupMadeForYou {
		t.Fatalf("daypart mix %+v", daypart)
	}
	if _, err := c.PersonalMixOn(ctx, user, "daypart-evening", day); apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatal("an evening mix without evening plays", err)
	}
	artists := mixByKind(mixes, repository.MixKindArtist)
	if artists[0].Title != "Mix: mix-Rock-0" || artists[0].ArtistID != "mix-Rock-0" || !strings.HasPrefix(artists[0].ID, "artist-") {
		t.Fatalf("artist mix %+v", artists[0])
	}
	decades := mixByKind(mixes, repository.MixKindDecade)
	if decades[0].ID != "decade-1990" || decades[0].Title != "Deine 90er" || decades[1].Title != "Deine 60er" || decades[1].Decade != 1960 || decades[0].Group != repository.MixGroupRewind {
		t.Fatalf("decade mixes %+v", decades)
	}
	if f := mixByKind(mixes, repository.MixKindFavorites)[0]; f.Title != "Favoriten-Mix" || f.ID != "favorites" {
		t.Fatalf("favorites mix %+v", f)
	}
	if f := mixByKind(mixes, repository.MixKindForgottenFavorites)[0]; f.Title != "Vergessene Favoriten" || f.Group != repository.MixGroupRewind {
		t.Fatalf("forgotten favorites %+v", f)
	}

	ids := map[string]bool{}
	for _, m := range mixes {
		if m.Group == "" || ids[m.ID] {
			t.Fatalf("mix without group or duplicate id: %+v", m)
		}
		ids[m.ID] = true
		d, err := c.PersonalMixOn(ctx, user, m.ID, day)
		if err != nil {
			t.Fatalf("detail of %s: %v", m.ID, err)
		}
		if d.Title != m.Title || d.Kind != m.Kind || d.Group != m.Group || d.TrackCount != m.TrackCount || !reflect.DeepEqual(d.CoverTrackIDs, m.CoverTrackIDs) {
			t.Fatalf("detail %+v differs from list entry %+v", d.PersonalMix, m)
		}
		switch m.Kind {
		case repository.MixKindDecade:
			for _, tr := range d.Tracks {
				if m.Decade == 1990 && !strings.HasPrefix(tr.ArtistID, "mix-Rock-") || m.Decade == 1960 && !strings.HasPrefix(tr.ArtistID, "mix-Jazz-") {
					t.Fatalf("track %s (%s) in %s", tr.ID, tr.ArtistID, m.Title)
				}
			}
		case repository.MixKindDaypart:
			for _, tr := range d.Tracks {
				if !slices.Contains([]string{"mix-Rock-2", "mix-Rock-3", "mix-Rock-4", "mix-Rock-5"}, tr.ArtistID) {
					t.Fatalf("track %s of %s in the morning mix", tr.ID, tr.ArtistID)
				}
			}
		case repository.MixKindArtist:
			own := 0
			for _, tr := range d.Tracks {
				if tr.ArtistID == m.ArtistID {
					own++
				} else if !strings.HasPrefix(tr.ArtistID, "mix-Rock-") {
					t.Fatalf("unrelated artist %s in %s", tr.ArtistID, m.Title)
				}
			}
			if own != 6 {
				t.Fatalf("%s holds %d of the artist's six tracks", m.Title, own)
			}
		}
	}
	// Another user with no history gets none of these.
	otherMixes, err := c.PersonalMixesOn(ctx, other, day)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range otherMixes {
		if m.Kind != repository.MixKindGenre {
			t.Fatalf("another user got %+v", m)
		}
	}
	// Excluding the artist drops its mix.
	if _, err := c.AddListeningExclusion(ctx, user, "artist", "mix-Rock-0"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PersonalMixOn(ctx, user, artists[0].ID, day); apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatal("mix of an excluded artist served", err)
	}
}

func TestRadioSeedsTitlesLimitAndExclude(t *testing.T) {
	db, user, _, byGenre := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	inGenre := func(ids []string, genre string) bool {
		for _, id := range ids {
			if !slices.Contains(byGenre[genre], id) {
				return false
			}
		}
		return true
	}

	res, err := c.Radio(ctx, repository.RadioRequest{User: user, ArtistID: "mix-Jazz-0", Nonce: "n"})
	if err != nil || res.Title != "Radio: mix-Jazz-0" || res.Seed.Kind != repository.RadioSeedArtist || res.Seed.ID != "mix-Jazz-0" ||
		len(res.TrackIDs) != 24 || !inGenre(res.TrackIDs, "Jazz") {
		t.Fatalf("artist radio %+v %v", res, err)
	}
	res, err = c.Radio(ctx, repository.RadioRequest{User: user, ReleaseID: "rel-mix-Folk-0", Nonce: "n"})
	if err != nil || res.Title != "Radio: rel-mix-Folk-0" || res.Seed.Kind != repository.RadioSeedRelease || len(res.TrackIDs) != 9 || !inGenre(res.TrackIDs, "Folk") {
		t.Fatalf("release radio %+v %v", res, err)
	}
	res, err = c.Radio(ctx, repository.RadioRequest{User: user, Genre: "Folk", Nonce: "n"})
	if err != nil || res.Title != "Radio: Folk" || res.Seed.Kind != repository.RadioSeedGenre || len(res.TrackIDs) != 9 || !inGenre(res.TrackIDs, "Folk") {
		t.Fatalf("genre radio %+v %v", res, err)
	}
	res, err = c.Radio(ctx, repository.RadioRequest{User: user, TrackID: byGenre["Folk"][0], Nonce: "n"})
	if err != nil || res.Title != "Radio: Song 0" || res.Seed.Kind != repository.RadioSeedTrack || slices.Contains(res.TrackIDs, byGenre["Folk"][0]) {
		t.Fatalf("track radio %+v %v", res, err)
	}
	if res, err = c.Radio(ctx, repository.RadioRequest{User: user}); err != nil || res.Title != "Dein Radio" || len(res.TrackIDs) != repository.RadioDefaultLimit {
		t.Fatalf("personal radio %+v %v", res, err)
	}
	if res, err = c.Radio(ctx, repository.RadioRequest{User: user, Genre: "Rock", Limit: 5}); err != nil || len(res.TrackIDs) != 5 {
		t.Fatal("limit ignored", res, err)
	}
	whole, err := c.Radio(ctx, repository.RadioRequest{User: user, Genre: "Rock", Nonce: "x", Limit: 500})
	if err != nil || len(whole.TrackIDs) != 36 {
		t.Fatal("whole station", whole, err)
	}
	seen := map[string]bool{}
	for _, id := range whole.TrackIDs {
		if seen[id] {
			t.Fatal("a station repeats", id)
		}
		seen[id] = true
	}
	skip := byGenre["Folk"][:3]
	if res, err = c.Radio(ctx, repository.RadioRequest{User: user, Genre: "Folk", Exclude: skip}); err != nil || len(res.TrackIDs) != 6 {
		t.Fatal("exclude ignored", res, err)
	}
	for _, id := range res.TrackIDs {
		if slices.Contains(skip, id) {
			t.Fatal("excluded id returned", id)
		}
	}
	for _, bad := range []struct {
		req  repository.RadioRequest
		code apperr.Code
	}{
		{repository.RadioRequest{User: user, ArtistID: "mix-Jazz-0", ReleaseID: "rel-mix-Jazz-0"}, apperr.CodeInvalidRequest},
		{repository.RadioRequest{User: user, ArtistID: "missing"}, apperr.CodeArtistNotFound},
		{repository.RadioRequest{User: user, ReleaseID: "missing"}, apperr.CodeReleaseNotFound},
		{repository.RadioRequest{User: user, TrackID: "missing"}, apperr.CodeTrackNotFound},
		{repository.RadioRequest{User: user, Exclude: make([]string, 501)}, apperr.CodeInvalidRequest},
	} {
		if _, err := c.Radio(ctx, bad.req); apperr.CodeOf(err) != bad.code {
			t.Fatalf("%+v: %v", bad.req, err)
		}
	}
}

func TestListeningStatsPeriods(t *testing.T) {
	db, user, other, byGenre := setupMixLibrary(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET duration_ms=200000`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rock, jazz := byGenre["Rock"], byGenre["Jazz"]
	event := 0
	play := func(u, id string, at time.Time, n int) {
		for range n {
			event++
			if _, err := db.ExecContext(ctx, `INSERT INTO playback_events(user_id,event_id,track_id,played_at) VALUES($1,$2,$3,$4)`, u, fmt.Sprintf("stats-event-%06d", event), id, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	play(user, rock[0], now.Add(-24*time.Hour), 3)
	play(user, rock[1], now.Add(-10*24*time.Hour), 1)
	play(user, jazz[0], now.Add(-100*24*time.Hour), 2)
	play(user, jazz[1], now.Add(-500*24*time.Hour), 1)
	play(other, rock[2], now.Add(-24*time.Hour), 1)
	// The counters hold four plays of jazz[1] that have no timestamp.
	for id, n := range map[string]int{rock[0]: 3, rock[1]: 1, jazz[0]: 2, jazz[1]: 5} {
		if _, err := db.ExecContext(ctx, `INSERT INTO listening_history(user_id,track_id,play_count,last_played_at) VALUES($1,$2,$3,now())`, user, id, n); err != nil {
			t.Fatal(err)
		}
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	stats := func(u, period string) *repository.ListeningStats {
		t.Helper()
		s, err := c.ListeningStatsAt(ctx, u, period, 10, now, berlin)
		if err != nil {
			t.Fatal(period, err)
		}
		return s
	}
	sum := func(s *repository.ListeningStats) int64 {
		var n int64
		for _, p := range s.Activity.Points {
			n += p.Plays
		}
		return n
	}

	week := stats(user, "week")
	if week.Totals.Plays != 3 || week.Totals.DistinctTracks != 1 || week.Totals.DistinctArtists != 1 || week.Totals.ListeningMS != 600000 ||
		week.Source != repository.StatsSourceEvents || !week.Complete || week.From == nil || week.Since == nil || week.TimeZone != "Europe/Berlin" {
		t.Fatalf("week %+v", week)
	}
	if len(week.TopTracks) != 1 || week.TopTracks[0].Track.ID != rock[0] || week.TopTracks[0].Plays != 3 ||
		week.TopArtists[0].ID != "mix-Rock-0" || week.TopReleases[0].ID != "rel-mix-Rock-0" || week.TopReleases[0].ArtistName != "mix-Rock-0" ||
		week.TopGenres[0].Name != "Rock" || week.TopGenres[0].Plays != 3 {
		t.Fatalf("week tops %+v", week)
	}
	if week.Activity.Bucket != "day" || len(week.Activity.Points) != 8 || sum(week) != 3 {
		t.Fatalf("week activity %+v", week.Activity)
	}
	month := stats(user, "month")
	if month.Totals.Plays != 4 || month.Totals.DistinctTracks != 2 || len(month.Activity.Points) != 31 || sum(month) != 4 {
		t.Fatalf("month %+v", month)
	}
	year := stats(user, "year")
	if year.Totals.Plays != 6 || year.Totals.DistinctArtists != 2 || year.Activity.Bucket != "month" || len(year.Activity.Points) < 12 || len(year.Activity.Points) > 13 || sum(year) != 6 {
		t.Fatalf("year %+v", year)
	}
	if len(year.TopGenres) != 2 || year.TopGenres[0].Name != "Rock" || year.TopGenres[0].Plays != 4 || year.TopGenres[1].Plays != 2 {
		t.Fatalf("year genres %+v", year.TopGenres)
	}
	all := stats(user, "all")
	if all.Source != repository.StatsSourceAggregate || all.From != nil || all.Totals.Plays != 11 || all.Totals.UntimedPlays != 4 || all.Complete ||
		all.TopTracks[0].Track.ID != jazz[1] || all.TopTracks[0].Plays != 5 || sum(all) != 7 {
		t.Fatalf("all %+v", all)
	}

	// Another user's plays are private, and a history that starts within
	// the period is marked incomplete.
	ow := stats(other, "week")
	if ow.Totals.Plays != 1 || ow.Complete || ow.TopTracks[0].Track.ID != rock[2] {
		t.Fatalf("other week %+v", ow)
	}
	if empty := stats("mix_nobody", "month"); empty.Totals.Plays != 0 || empty.Since != nil || !empty.Complete || empty.TopTracks == nil {
		t.Fatalf("empty stats %+v", empty)
	}
	if _, err := c.ListeningStatsAt(ctx, user, "decade", 10, now, berlin); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatal("unknown period accepted", err)
	}
}

func TestSearchLyricsIndexAndSnippets(t *testing.T) {
	db := dbtest.Open(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO artists(id,name,provider,source_id,created_at,updated_at) VALUES('lyr-a','Lyric Artist','fixture','lyr-a',now(),now())`)
	exec(`INSERT INTO releases(id,artist_id,title,provider,source_id,created_at,updated_at) VALUES('lyr-r','lyr-a','Lyrics','fixture','lyr-r',now(),now())`)
	for _, tr := range []struct {
		id, state string
		file      bool
	}{{"lyr-de", "available_plain", true}, {"lyr-en", "available_synced", true}, {"lyr-gone", "not_found", true}, {"lyr-nofile", "available_plain", false}} {
		exec(`INSERT INTO tracks(id,release_id,artist_id,title,identity_key,lyrics_state,lyrics_checked_at,created_at,updated_at) VALUES($1,'lyr-r','lyr-a',$1,$1,$2,now()-interval '1 day',now(),now())`, tr.id, tr.state)
		if tr.file {
			exec(`INSERT INTO files(id,track_id,path,created_at,updated_at) VALUES($1,$2,$3,now(),now())`, "file-"+tr.id, tr.id, tr.id+".flac")
		}
	}
	need, err := c.ListTracksNeedingLyricsIndex(ctx, 10)
	if err != nil || len(need) != 3 || need[0].CheckedAt == nil {
		t.Fatalf("candidates %+v %v", need, err)
	}
	bodies := map[string]string{
		"lyr-de":     "Wir fahren auf der Autobahn\nÜber den Wolken muss die Freiheit wohl grenzenlos sein",
		"lyr-en":     "Hello darkness, my old friend\nI've come to talk with you again",
		"lyr-nofile": "Über den Wolken ohne Datei",
	}
	for _, n := range need {
		if err := c.UpsertLyricsIndex(ctx, n.TrackID, bodies[n.TrackID], n.CheckedAt); err != nil {
			t.Fatal(err)
		}
	}
	// A stale row of a track whose lyrics are gone.
	if err := c.UpsertLyricsIndex(ctx, "lyr-gone", "Über den Wolken, veraltet", nil); err != nil {
		t.Fatal(err)
	}
	if need, err = c.ListTracksNeedingLyricsIndex(ctx, 10); err != nil || len(need) != 0 {
		t.Fatalf("index not caught up: %+v %v", need, err)
	}

	search := func(q string) []repository.LyricsMatch {
		t.Helper()
		hits, err := c.SearchLyrics(ctx, q, 10)
		if err != nil {
			t.Fatal(q, err)
		}
		return hits
	}
	for _, q := range []string{"über den wolken", "UBER DEN WOLK", "freiheit wolken", "grenzenlos"} {
		hits := search(q)
		if len(hits) != 1 || hits[0].Track.ID != "lyr-de" || hits[0].Snippet == "" || !strings.Contains(hits[0].Snippet, hits[0].Match) {
			t.Fatalf("%q: %+v", q, hits)
		}
	}
	if hits := search("darkness my old"); len(hits) != 1 || hits[0].Track.ID != "lyr-en" || hits[0].Match != "darkness, my old" {
		t.Fatalf("english: %+v", hits)
	}
	for _, q := range []string{"ab", "nirgendwo", "", "!!!"} {
		if hits := search(q); hits == nil || len(hits) != 0 {
			t.Fatalf("%q found %+v", q, hits)
		}
	}

	res, err := c.SearchLibrary(ctx, "wolken", 5)
	if err != nil || len(res.Lyrics) != 1 || res.Lyrics[0].Track.ID != "lyr-de" {
		t.Fatalf("library search lyrics group %+v %v", res, err)
	}
	if res, err = c.SearchLibraryWithLyrics(ctx, "wolken", 5, false); err != nil || res.Lyrics == nil || len(res.Lyrics) != 0 {
		t.Fatal("lyrics=false", res, err)
	}

	// New lyrics: the track needs indexing again; lyrics gone: pruned.
	exec(`UPDATE tracks SET lyrics_checked_at=now() WHERE id='lyr-en'`)
	if need, _ = c.ListTracksNeedingLyricsIndex(ctx, 10); len(need) != 1 || need[0].TrackID != "lyr-en" {
		t.Fatalf("rewritten lyrics not picked up: %+v", need)
	}
	exec(`UPDATE tracks SET lyrics_state='not_found' WHERE id='lyr-de'`)
	if err := c.PruneLyricsIndex(ctx); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM track_lyrics_search WHERE track_id IN('lyr-de','lyr-gone')`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("stale rows kept", rows, err)
	}
	if err := c.DeleteLyricsIndex(ctx, "lyr-en"); err != nil {
		t.Fatal(err)
	}
	if hits := search("darkness"); len(hits) != 0 {
		t.Fatal("deleted row still found")
	}
}
