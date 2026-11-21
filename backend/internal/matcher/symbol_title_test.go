package matcher

import (
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// A title written in symbols only has no normalised base. It must still match
// the upload that carries the same symbols, and only that one.
func TestSymbolOnlyTitleMatchesTheSameSymbols(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := music.Track{
		Title: "@#!", Album: "@#!", Artists: []string{"LACAZETTE"},
		AlbumArtist: "LACAZETTE", DurationMS: 150000,
	}

	same := m.Score(track, provider.MediaCandidate{
		ID: "same", Title: "@#!", Album: "@#!",
		Artists: []string{"LACAZETTE", "JR."}, DurationMS: 150000,
	})
	if same.Score < 95 {
		t.Fatalf("identical symbol title scored %.1f, want >= 95 (%+v)", same.Score, same.Breakdown)
	}
	if same.Breakdown.Title != 1 || same.Breakdown.Album != 1 {
		t.Errorf("title %.2f album %.2f, want both 1", same.Breakdown.Title, same.Breakdown.Album)
	}

	// The same symbols without an album on the candidate must still pass.
	bare := m.Score(track, provider.MediaCandidate{
		ID: "bare", Title: "@#! (Official Video)", Artists: []string{"LACAZETTE"}, DurationMS: 152000,
	})
	if bare.Score < m.MinScore() {
		t.Errorf("symbol title with decoration scored %.1f, below threshold (%+v)", bare.Score, bare.Breakdown)
	}

	for _, other := range []string{"///", "$$$", "TTT", "ABC", "@#"} {
		res := m.Score(track, provider.MediaCandidate{
			ID: other, Title: other, Artists: []string{"LACAZETTE"}, DurationMS: 150000,
		})
		if res.Breakdown.Title != 0 {
			t.Errorf("%q against %q: title similarity %.2f, want 0", track.Title, other, res.Breakdown.Title)
		}
		if res.Score >= m.MinScore() {
			t.Errorf("%q against %q scored %.1f, must stay below the threshold", track.Title, other, res.Score)
		}
	}
}

// A worded title must never match a symbol title, in either direction.
func TestSymbolTitleNeverMatchesAWordedTitle(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	worded := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}
	res := m.Score(worded, provider.MediaCandidate{
		ID: "sym", Title: "!!!", Artists: []string{"Artist"}, DurationMS: 200000,
	})
	if res.Breakdown.Title != 0 || res.Score >= m.MinScore() {
		t.Errorf("worded title against symbols: title %.2f score %.1f", res.Breakdown.Title, res.Score)
	}
}

func TestTitleSimilarityOfEmptyTitlesStaysZero(t *testing.T) {
	if got := titleSimilarity(Analyze(""), Analyze("")); got != 0 {
		t.Errorf("two empty titles scored %v, want 0", got)
	}
	if got := titleSimilarity(Analyze("(Official Video)"), Analyze("[HD]")); got != 0 {
		t.Errorf("two titles of decoration only scored %v, want 0", got)
	}
	if got := titleSimilarity(Analyze("$ $ $"), Analyze("$$$")); got != 1 {
		t.Errorf("spacing inside a symbol title changed the result: %v, want 1", got)
	}
}

// An album name that is decoration on one side only says nothing and must not
// count against an otherwise exact candidate.
func TestAlbumWithoutComparableTextOnOneSideIsNotScored(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := music.Track{Title: "Song", Album: "$$$", Artists: []string{"Artist"}, DurationMS: 200000}
	res := m.Score(track, provider.MediaCandidate{
		ID: "a", Title: "Song", Album: "Greatest Hits", Artists: []string{"Artist"}, DurationMS: 200000,
	})
	if res.Score < 95 {
		t.Errorf("scored %.1f, want >= 95 (%+v)", res.Score, res.Breakdown)
	}
}
