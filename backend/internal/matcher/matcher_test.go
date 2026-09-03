package matcher

import (
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func wantedTrack() music.Track {
	return music.Track{
		Title:      "Song",
		Artists:    []string{"Artist"},
		Album:      "Album",
		DurationMS: 205000, // 03:25
	}
}

func TestScoreExactMatchIsConfident(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	res := m.Score(wantedTrack(), provider.MediaCandidate{
		ID: "a", Title: "Song", Artists: []string{"Artist"},
		Album: "Album", DurationMS: 205000,
	})
	if res.Score < 95 {
		t.Fatalf("exact match scored %.1f, want >= 95 (%+v)", res.Score, res.Breakdown)
	}
}

func TestScoreRanksSpecExample(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()

	candidates := []provider.MediaCandidate{
		{ID: "exact", Title: "Song", Artists: []string{"Artist"}, DurationMS: 205000},
		{ID: "live", Title: "Song (Live)", Artists: []string{"Artist"}, DurationMS: 209000},
		{ID: "spedup", Title: "Song Sped Up", Uploader: "Random Channel", DurationMS: 171000},
	}

	ranked := m.Rank(track, candidates)
	if len(ranked) != 3 {
		t.Fatalf("got %d results, want 3", len(ranked))
	}
	if ranked[0].Candidate.ID != "exact" || ranked[1].Candidate.ID != "live" || ranked[2].Candidate.ID != "spedup" {
		t.Fatalf("unexpected order: %s, %s, %s",
			ranked[0].Candidate.ID, ranked[1].Candidate.ID, ranked[2].Candidate.ID)
	}
	if ranked[0].Score < m.MinScore() {
		t.Errorf("exact candidate scored %.1f, below threshold", ranked[0].Score)
	}
	if ranked[1].Score >= m.MinScore() {
		t.Errorf("live candidate scored %.1f, must stay below threshold %.1f",
			ranked[1].Score, m.MinScore())
	}
	if ranked[2].Score >= ranked[1].Score {
		t.Errorf("sped up candidate must rank last")
	}
}

func TestScoreISRCMatchWins(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()
	track.ISRC = "DEA123456789"

	res := m.Score(track, provider.MediaCandidate{
		ID: "isrc", Title: "Completely Different Title",
		Artists: []string{"Someone Else"}, DurationMS: 1000,
		ISRC: "de-a12-34-56789",
	})
	if res.Score != 100 {
		t.Fatalf("ISRC match scored %.1f, want 100", res.Score)
	}
	if !res.Breakdown.ISRCMatch {
		t.Fatal("breakdown should record the ISRC match")
	}
}

func TestScoreISRCMismatchIsPenalised(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()
	track.ISRC = "DEA123456789"

	res := m.Score(track, provider.MediaCandidate{
		ID: "other", Title: "Song", Artists: []string{"Artist"},
		Album: "Album", DurationMS: 205000, ISRC: "USB987654321",
	})
	if !res.Breakdown.ISRCMismatch {
		t.Fatal("breakdown should record the ISRC mismatch")
	}
	if res.Score >= 60 {
		t.Fatalf("mismatching ISRC scored %.1f, want a clear penalty", res.Score)
	}
}

func TestBestFailsBelowThreshold(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	_, err := m.Best(wantedTrack(), []provider.MediaCandidate{
		{ID: "wrong", Title: "Something Else", Artists: []string{"Nobody"}, DurationMS: 60000},
	})
	if err == nil {
		t.Fatal("expected MATCH_FAILED")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeMatchFailed {
		t.Fatalf("error code = %s, want %s", code, apperr.CodeMatchFailed)
	}
}

func TestBestFailsWithoutCandidates(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	if _, err := m.Best(wantedTrack(), nil); apperr.CodeOf(err) != apperr.CodeMatchFailed {
		t.Fatalf("error code = %s, want %s", apperr.CodeOf(err), apperr.CodeMatchFailed)
	}
}

func TestScoreAcceptsTopicChannelAsArtist(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	res := m.Score(wantedTrack(), provider.MediaCandidate{
		ID: "topic", Title: "Song", Uploader: "Artist - Topic", DurationMS: 205000,
	})
	if res.Score < m.MinScore() {
		t.Fatalf("topic channel candidate scored %.1f, want >= %.1f (%+v)",
			res.Score, m.MinScore(), res.Breakdown)
	}
}

func TestScoreWantedLiveVersionPrefersLiveCandidate(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()
	track.Title = "Song (Live)"

	studio := m.Score(track, provider.MediaCandidate{
		ID: "studio", Title: "Song", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	live := m.Score(track, provider.MediaCandidate{
		ID: "live", Title: "Song (Live at Wembley)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if live.Score <= studio.Score {
		t.Fatalf("live candidate %.1f must beat studio candidate %.1f", live.Score, studio.Score)
	}
}

func TestScoreMultipleArtists(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := music.Track{
		Title:      "Get Lucky",
		Artists:    []string{"Daft Punk", "Pharrell Williams"},
		Album:      "Random Access Memories",
		DurationMS: 248000,
	}

	candidate := provider.MediaCandidate{
		ID:         "yt1",
		Title:      "Get Lucky (feat. Pharrell Williams)",
		Artists:    []string{"Daft Punk"},
		Album:      "Random Access Memories",
		DurationMS: 248000,
	}

	res := m.Score(track, candidate)
	if res.Score < 85 {
		t.Fatalf("multiple artists match scored %.1f, want >= 85 (%+v)", res.Score, res.Breakdown)
	}
}

func TestScoreSpecialCharacters(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := music.Track{
		Title:      "Harder, Better, Faster, Stronger",
		Artists:    []string{"Daft Punk"},
		Album:      "Discovery",
		DurationMS: 224000,
	}

	candidate := provider.MediaCandidate{
		ID:         "yt2",
		Title:      "Harder Better Faster Stronger",
		Artists:    []string{"Daft Punk"},
		Album:      "Discovery",
		DurationMS: 224000,
	}

	res := m.Score(track, candidate)
	if res.Score < 90 {
		t.Fatalf("special characters match scored %.1f, want >= 90 (%+v)", res.Score, res.Breakdown)
	}
}

func TestScoreRejectsKaraokeAndCover(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack() // "Song" by "Artist"

	karaoke := m.Score(track, provider.MediaCandidate{
		ID: "karaoke", Title: "Song (Karaoke Version)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if karaoke.Score >= m.MinScore() {
		t.Fatalf("karaoke score %.1f must stay below threshold %.1f", karaoke.Score, m.MinScore())
	}

	cover := m.Score(track, provider.MediaCandidate{
		ID: "cover", Title: "Song (Cover)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if cover.Score >= m.MinScore() {
		t.Fatalf("cover score %.1f must stay below threshold %.1f", cover.Score, m.MinScore())
	}
}

func TestScoreRejectsRemixWhenOriginalWanted(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack() // "Song" by "Artist"

	remix := m.Score(track, provider.MediaCandidate{
		ID: "remix", Title: "Song (Remix)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if remix.Score >= m.MinScore() {
		t.Fatalf("remix score %.1f must stay below threshold %.1f", remix.Score, m.MinScore())
	}
}

func TestScoreRejectsAcousticWhenOriginalWanted(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()

	acoustic := m.Score(track, provider.MediaCandidate{
		ID: "acoustic", Title: "Song (Acoustic)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if acoustic.Score >= m.MinScore() {
		t.Fatalf("acoustic score %.1f must stay below threshold %.1f", acoustic.Score, m.MinScore())
	}
}

func TestScoreRejectsExtendedWhenOriginalWanted(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	track := wantedTrack()

	extended := m.Score(track, provider.MediaCandidate{
		ID: "extended", Title: "Song (Extended Mix)", Artists: []string{"Artist"}, DurationMS: 205000,
	})
	if extended.Score >= m.MinScore() {
		t.Fatalf("extended score %.1f must stay below threshold %.1f", extended.Score, m.MinScore())
	}
}
