package repository

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFoldSearchTextIgnoresCaseAndDiacritics(t *testing.T) {
	for in, want := range map[string]string{
		"Über den WOLKEN":  "uber den wolken",
		"Straße":           "strasse",
		"Mädchen, Café":    "madchen, cafe",
		"Œuvre Ærø Łódź":   "oeuvre aero lodz",
		"Don’t stop":       "don't stop",
		"naïve résumé 123": "naive resume 123",
	} {
		if got := FoldSearchText(in); got != want {
			t.Errorf("fold %q = %q, want %q", in, got, want)
		}
	}
}

func TestLyricsQueryWordsAndTSQuery(t *testing.T) {
	words := lyricsQueryWords("  Über   den Wolken!  ")
	if strings.Join(words, ",") != "uber,den,wolken" {
		t.Fatal(words)
	}
	all, phrase := lyricsTSQuery(words)
	if all != "uber & den & wolken:*" || phrase != "uber <-> den <-> wolken:*" {
		t.Fatal(all, phrase)
	}
	// Operators of the tsquery syntax never reach PostgreSQL.
	if got := lyricsQueryWords(`a & b | !c <-> 'd':*`); strings.Join(got, ",") != "a,b,c,d" {
		t.Fatal(got)
	}
	if got := lyricsQueryWords(strings.Repeat("wort ", 20)); len(got) != lyricsMaxQueryWords {
		t.Fatal(len(got))
	}
}

func TestLyricsSnippet(t *testing.T) {
	body := "Wir sind die Roboter\nÜber den Wolken muss die Freiheit wohl grenzenlos sein\nAlle Ängste, alle Sorgen, sagt man"
	snippet, match := LyricsSnippet(body, lyricsQueryWords("uber den wolk"))
	if match != "Über den Wolken" {
		t.Fatalf("match %q", match)
	}
	if !strings.Contains(snippet, match) || !strings.Contains(snippet, "Roboter / Über den Wolken muss") {
		t.Fatalf("snippet %q", snippet)
	}
	if strings.Contains(snippet, "\n") {
		t.Fatalf("line break in snippet %q", snippet)
	}

	// A phrase across a line break; the match keeps the separator.
	snippet, match = LyricsSnippet(body, lyricsQueryWords("grenzenlos sein alle"))
	if match != "grenzenlos sein / Alle" || !strings.Contains(snippet, match) {
		t.Fatalf("snippet %q match %q", snippet, match)
	}

	// Words not in this order fall back to the first word found.
	snippet, match = LyricsSnippet(body, lyricsQueryWords("sorgen angste"))
	if match != "Ängste" || !strings.Contains(snippet, match) {
		t.Fatalf("fallback snippet %q match %q", snippet, match)
	}

	// Long text is cut at word boundaries and marked.
	long := strings.Repeat("la ", 60) + "Zauberwort " + strings.Repeat("da ", 60)
	snippet, match = LyricsSnippet(long, lyricsQueryWords("zauber"))
	if match != "Zauberwort" || !strings.HasPrefix(snippet, "… la") || !strings.HasSuffix(snippet, "da …") || len([]rune(snippet)) > 2*lyricsSnippetRunes+20 {
		t.Fatalf("long snippet %q", snippet)
	}
	if s, m := LyricsSnippet(body, lyricsQueryWords("nirgends")); s != "" || m != "" {
		t.Fatalf("no match gave %q %q", s, m)
	}
}

func TestWeaveSpreadsTheSmallerList(t *testing.T) {
	a := []MixCandidate{candidate("a1", "x", "1"), candidate("a2", "x", "2"), candidate("a3", "x", "3"), candidate("a4", "x", "4")}
	b := []MixCandidate{candidate("b1", "y", "1"), candidate("b2", "y", "2")}
	got := []string{}
	for _, c := range weave(a, b) {
		got = append(got, c.ID)
	}
	if strings.Join(got, ",") != "a1,b1,a2,a3,b2,a4" {
		t.Fatal(got)
	}
	if len(weave(nil, b)) != 2 || len(weave(a, nil)) != 4 {
		t.Fatal("one empty side")
	}
}

func TestComposeArtistMixMixesArtistAndRelated(t *testing.T) {
	now := time.Now()
	cands := []MixCandidate{}
	for i := range 40 {
		c := candidate(fmt.Sprintf("seed-%d", i), "star", fmt.Sprintf("s%d", i))
		c.Weight = 1
		cands = append(cands, c)
	}
	for a := range 12 {
		for i := range 5 {
			cands = append(cands, candidate(fmt.Sprintf("rel-%d-%d", a, i), fmt.Sprintf("other-%d", a), fmt.Sprintf("r%d", i)))
		}
	}
	mix := composeArtistMix(cands, "star", now)
	perArtist := map[string]int{}
	for _, c := range mix {
		perArtist[c.ArtistID]++
	}
	if len(mix) != MixTrackLimit || perArtist["star"] != artistMixSeedShare {
		t.Fatalf("mix of %d with %d by the artist", len(mix), perArtist["star"])
	}
	for a, n := range perArtist {
		if a != "star" && n > mixArtistCap {
			t.Fatalf("related artist %s has %d", a, n)
		}
	}
	if mix[0].ArtistID != "star" {
		t.Fatal("the artist does not open its mix")
	}
	// Without related artists the artist fills in, up to its cap.
	alone := composeArtistMix(cands[:40], "star", now)
	if len(alone) != artistMixSeedCap {
		t.Fatalf("artist alone gave %d", len(alone))
	}
}

func TestDaypartWeights(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	// Saturday 2026-03-07, 08:30 in Berlin.
	now := time.Date(2026, 3, 7, 8, 30, 0, 0, berlin)
	morning := daypartAt(now)
	if morning.Key != "morning" || daypartAt(now.Add(15*time.Hour)).Key != "night" || daypartAt(now.Add(-5*time.Hour)).Key != "night" {
		t.Fatal("dayparts", morning.Key)
	}
	events := []playEvent{}
	add := func(id string, at time.Time, n int) {
		for range n {
			events = append(events, playEvent{TrackID: id, PlayedAt: at.UTC()})
		}
	}
	add("weekend-morning", time.Date(2026, 2, 28, 9, 0, 0, 0, berlin), 10) // a Saturday
	add("weekday-morning", time.Date(2026, 3, 4, 7, 0, 0, 0, berlin), 10)  // a Wednesday
	add("evening", time.Date(2026, 3, 4, 20, 0, 0, 0, berlin), 30)         // not a morning
	add("utc-trap", time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC), 1)      // 11:30 in Berlin: afternoon
	w := daypartWeights(events, morning, now)
	if w == nil || w["weekend-morning"] != 15 || w["weekday-morning"] != 10 || w["evening"] != 0 || w["utc-trap"] != 0 {
		t.Fatalf("weights %v", w)
	}
	if daypartWeights(events[:19], morning, now) != nil {
		t.Fatal("too few plays still made a daypart mix")
	}
	ids, ws := topWeights(w, 1)
	if len(ids) != 1 || ids[0] != "weekend-morning" || ws[0] != 15 {
		t.Fatal(ids, ws)
	}
	if _, ok := daypartByKey("evening"); !ok {
		t.Fatal("evening unknown")
	}
	if _, ok := daypartByKey("brunch"); ok {
		t.Fatal("unknown daypart accepted")
	}
}

func TestDecadeTitleAndScores(t *testing.T) {
	for decade, want := range map[int]string{1990: "Deine 90er", 1960: "Deine 60er", 2000: "Deine 2000er", 2010: "Deine 2010er", 1900: "Deine 1900er"} {
		if got := decadeTitle(decade); got != want {
			t.Errorf("decade %d: %q", decade, got)
		}
	}
	now := time.Now()
	recent, old, never := candidate("r", "a", "r"), candidate("o", "a", "o"), candidate("n", "a", "n")
	recent.LastPlayed, old.LastPlayed = now.Add(-24*time.Hour), now.Add(-200*24*time.Hour)
	if !(favoritesScore(old, now) > favoritesScore(never, now) && favoritesScore(never, now) > favoritesScore(recent, now)) {
		t.Fatal("favorites not ordered by time since the last play")
	}
	for kind, group := range mixGroups {
		if group != MixGroupMadeForYou && group != MixGroupDiscover && group != MixGroupRewind {
			t.Fatalf("kind %s in unknown group %s", kind, group)
		}
	}
}

func TestActivitySeriesFillsGaps(t *testing.T) {
	first := time.Date(2026, 1, 30, 22, 0, 0, 0, time.UTC)
	last := time.Date(2026, 2, 2, 1, 0, 0, 0, time.UTC)
	days := activitySeries(map[string]StatsPoint{"2026-01-31": {Start: "2026-01-31", Plays: 3}}, "day", first, last)
	if len(days) != 4 || days[0].Start != "2026-01-30" || days[1].Plays != 3 || days[3].Start != "2026-02-02" {
		t.Fatal(days)
	}
	months := activitySeries(nil, "month", time.Date(2025, 11, 15, 0, 0, 0, 0, time.UTC), last)
	if len(months) != 4 || months[0].Start != "2025-11-01" || months[3].Start != "2026-02-01" {
		t.Fatal(months)
	}
}

func TestNormalizeExclusion(t *testing.T) {
	if k, v, err := normalizeExclusion(" genre ", "  Schlager "); err != nil || k != "genre" || v != "Schlager" {
		t.Fatal(k, v, err)
	}
	for _, bad := range [][2]string{{"mood", "x"}, {"artist", ""}, {"genre", strings.Repeat("g", 81)}, {"track", strings.Repeat("t", 201)}} {
		if _, _, err := normalizeExclusion(bad[0], bad[1]); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}
