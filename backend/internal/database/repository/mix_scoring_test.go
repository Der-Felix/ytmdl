package repository

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestSkipPenaltyDecays(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		count int64
		ago   time.Duration
		want  float64
	}{
		{0, 0, 0},
		{1, time.Hour, 7},
		{2, time.Hour, 10},
		{9, time.Hour, 16},
		{1, 30 * 24 * time.Hour, 3},
		{4, 30 * 24 * time.Hour, 12},
		{5, 91 * 24 * time.Hour, 0},
	} {
		if got := SkipPenalty(tc.count, now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("SkipPenalty(%d, -%s) = %v, want %v", tc.count, tc.ago, got, tc.want)
		}
	}
	if SkipPenalty(3, time.Time{}, now) != 0 {
		t.Error("a skip without time counts")
	}
}

func candidate(id, artist, title string) MixCandidate {
	return MixCandidate{ID: id, ArtistID: artist, TitleKey: title, ReleaseID: "rel-" + artist, Shuffle: fmt.Sprintf("%064x", len(id)*7919+len(title))}
}

func TestPickMixCapsArtistsAndDeduplicatesRecordings(t *testing.T) {
	now := time.Now()
	cands := []MixCandidate{}
	for i := range 6 {
		cands = append(cands, candidate(fmt.Sprintf("a%d", i), "artist-a", fmt.Sprintf("song %d", i)))
	}
	cands = append(cands, candidate("b1", "artist-b", "same"), candidate("b2", "artist-b", "same"))
	got := composeMix(cands, discoveryScore, 1, now)
	perArtist := map[string]int{}
	for _, c := range got {
		perArtist[c.ArtistID]++
	}
	if perArtist["artist-a"] != mixArtistCap || perArtist["artist-b"] != 1 || len(got) != 4 {
		t.Fatalf("caps not applied: %v", perArtist)
	}
}

func TestSkippedTracksSinkOrDropOut(t *testing.T) {
	now := time.Now()
	a, b, c := candidate("a", "x", "a"), candidate("b", "y", "b"), candidate("c", "z", "c")
	a.Plays, b.Plays, c.Plays = 5, 5, 5
	a.Skips, a.LastSkipped = 1, now.Add(-time.Hour)
	c.Skips, c.LastSkipped = 2, now.Add(-time.Hour)
	got := composeMix([]MixCandidate{a, b, c}, rediscoverScore, 0, now)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("want b before skipped a and twice-skipped c dropped, got %v", got)
	}
	c.Favorite = true
	if got = composeMix([]MixCandidate{c}, rediscoverScore, 0, now); len(got) != 1 {
		t.Fatal("a favorite is never dropped for skips")
	}
}

func TestComposeGenreMixBalancesLikedAndNew(t *testing.T) {
	now := time.Now()
	cands := []MixCandidate{}
	for i := range 60 {
		c := candidate(fmt.Sprintf("liked%d", i), fmt.Sprintf("la%d", i), "t")
		c.Plays = 4
		cands = append(cands, c)
		cands = append(cands, candidate(fmt.Sprintf("new%d", i), fmt.Sprintf("na%d", i), "t"))
	}
	got := composeGenreMix(cands, now)
	liked := 0
	for _, c := range got {
		if c.Liked() {
			liked++
		}
	}
	if len(got) != MixTrackLimit || liked != genreLikedShare {
		t.Fatalf("want %d tracks with %d liked, got %d with %d", MixTrackLimit, genreLikedShare, len(got), liked)
	}
	if !got[0].Liked() || got[1].Liked() {
		t.Fatal("liked and new tracks are not woven together")
	}
	// Without new tracks the liked ones fill the whole mix.
	got = composeGenreMix(cands[:0:0], now)
	if len(got) != 0 {
		t.Fatal("empty pool produced tracks")
	}
	onlyLiked := []MixCandidate{}
	for _, c := range cands {
		if c.Plays > 0 {
			onlyLiked = append(onlyLiked, c)
		}
	}
	if got = composeGenreMix(onlyLiked, now); len(got) != MixTrackLimit {
		t.Fatalf("liked tracks did not fill the gap: %d", len(got))
	}
}

func TestShuffleJitterChangesOrderDeterministically(t *testing.T) {
	now := time.Now()
	mk := func(seed string) []MixCandidate {
		out := []MixCandidate{}
		for i := range 20 {
			c := candidate(fmt.Sprintf("t%d", i), fmt.Sprintf("a%d", i), "x")
			c.Shuffle = fmt.Sprintf("%x", sha256.Sum256([]byte(seed+c.ID)))
			out = append(out, c)
		}
		return out
	}
	a1, a2, b := composeMix(mk("aa"), discoveryScore, 4, now), composeMix(mk("aa"), discoveryScore, 4, now), composeMix(mk("bb"), discoveryScore, 4, now)
	if fmt.Sprint(a1) != fmt.Sprint(a2) {
		t.Fatal("same seed, different order")
	}
	if u := shuffleUnit("ffffffffffffff"); u < 0 || u >= 1 {
		t.Fatalf("shuffle unit out of range: %v", u)
	}
	if fmt.Sprint(a1) == fmt.Sprint(b) {
		t.Fatal("another day, same order")
	}
}

func TestMixCoversPreferDifferentReleases(t *testing.T) {
	tracks := []MixCandidate{
		{ID: "1", ReleaseID: "r1"}, {ID: "2", ReleaseID: "r1"}, {ID: "3", ReleaseID: "r2"}, {ID: "4", ReleaseID: ""}, {ID: "5", ReleaseID: "r3"},
	}
	ids, releases := mixCovers(tracks)
	if fmt.Sprint(ids) != "[1 3 5 2]" || fmt.Sprint(releases) != "[r1 r2 r3]" {
		t.Fatalf("covers %v %v", ids, releases)
	}
}

func TestMixGenreOrderLeavesOutDislikedGenres(t *testing.T) {
	library := []weightedGenre{{"Rock", 36}, {"Jazz", 24}, {"Folk", 9}}
	// A new user tries the library's genres; a genre the user mostly skips
	// (negative affinity) is not filled in from the library.
	if got := mixGenreOrder(nil, library); fmt.Sprint(got) != "[Rock Jazz Folk]" {
		t.Fatalf("cold start order %v", got)
	}
	user := []weightedGenre{{"Jazz", 12}, {" rock ", -6}}
	if got := mixGenreOrder(user, library); fmt.Sprint(got) != "[Jazz Folk]" {
		t.Fatalf("order with a disliked genre %v", got)
	}
	many := []weightedGenre{}
	for i := range 10 {
		many = append(many, weightedGenre{fmt.Sprintf("G%d", i), float64(20 - i)})
	}
	if got := mixGenreOrder(many, nil); len(got) != maxUserGenres || got[0] != "G0" {
		t.Fatalf("user genres not capped: %v", got)
	}
}

func TestGenreMixDescriptionWithoutFavorites(t *testing.T) {
	fresh := []MixCandidate{candidate("a", "x", "t"), candidate("b", "y", "u")}
	if d := genreMixDescription("Jazz", fresh); d != "Eine Auswahl aus Jazz in deiner Mediathek" {
		t.Fatalf("cold-start description %q", d)
	}
	fresh[1].Favorite = true
	if d := genreMixDescription("Jazz", fresh); d != "Deine Favoriten aus Jazz und Titel, die du kaum kennst" {
		t.Fatalf("description with favorites %q", d)
	}
}
