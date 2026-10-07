package matcher

import (
	"testing"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func TestKnownRuntimeCannotBeOutweighedByCreditsOrISRC(t *testing.T) {
	m := New(Options{MinScore: 1})
	for _, tc := range []struct {
		name       string
		want, have int
	}{
		{"complete movement instead of section", 222000, 863001},
		{"wrong live performance", 351000, 275561},
		{"short preview", 200000, 30000},
		{"longer sonata", 258000, 402021},
	} {
		t.Run(tc.name, func(t *testing.T) {
			track := music.Track{Title: "Same Title", Artists: []string{"Same Artist"}, Album: "Same Album", DurationMS: tc.want, ISRC: "DEABC1234567"}
			cand := provider.MediaCandidate{ID: "same", Title: track.Title, Artists: track.Artists, Album: track.Album, DurationMS: tc.have, ISRC: track.ISRC}
			result := m.Score(track, cand)
			if result.Score != 0 || !result.Breakdown.DurationMismatch {
				t.Fatalf("unsafe recording accepted: %+v", result)
			}
			if len(m.Acceptable(track, []provider.MediaCandidate{cand}, 5)) != 0 {
				t.Fatal("unsafe recording reached resolution")
			}
		})
	}
}

func TestRuntimeBoundaryAndUnknownMetadataRemainEligible(t *testing.T) {
	m := New(Options{MinScore: 70})
	track := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}
	for _, duration := range []int{185000, 215000, 0} {
		result := m.Score(track, provider.MediaCandidate{Title: track.Title, Artists: track.Artists, DurationMS: duration})
		if result.Breakdown.DurationMismatch || result.Score < 70 {
			t.Fatalf("duration %d rejected: %+v", duration, result)
		}
	}
}
