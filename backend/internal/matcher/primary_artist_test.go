package matcher

import (
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func TestComposerAloneCannotEstablishRequestedPerformance(t *testing.T) {
	track := music.Track{Title: "Soirées musicales, Op. 6: No. 4, Ballade", AlbumArtist: "Clara Isabella Siegle", Artists: []string{"Clara Isabella Siegle & Clara Schumann"}, DurationMS: 388000}
	matcher := New(Options{MinScore: 1})
	for _, candidate := range []provider.MediaCandidate{
		{Title: track.Title, Artists: []string{"Clara Schumann"}, Uploader: "Samantha Brule", DurationMS: 382000},
		{Title: track.Title, Artists: []string{"Another Pianist", "Clara Schumann"}, DurationMS: 388000},
		{Title: track.Title, DurationMS: 388000},
	} {
		result := matcher.Score(track, candidate)
		if result.Score != 0 || !result.Breakdown.PrimaryArtistUnconfirmed || len(matcher.Acceptable(track, []provider.MediaCandidate{candidate}, 5)) != 0 {
			t.Fatalf("unconfirmed performer accepted: %+v", result)
		}
	}
	for _, candidate := range []provider.MediaCandidate{
		{Title: track.Title, Artists: []string{"Clara Isabella Siegle & Clara Schumann"}, DurationMS: 388000},
		{Title: track.Title, Artists: []string{"Clara Schumann"}, Uploader: "Clara Isabella Siegle - Topic", DurationMS: 388000},
		{Title: "Clara Isabella Siegle: " + track.Title, DurationMS: 388000},
	} {
		if result := matcher.Score(track, candidate); result.Breakdown.PrimaryArtistUnconfirmed || result.Score == 0 {
			t.Fatalf("explicit performer evidence rejected: %+v", result)
		}
	}
	track.ISRC = "DEABC2600001"
	if result := matcher.Score(track, provider.MediaCandidate{ISRC: track.ISRC, DurationMS: track.DurationMS}); result.Score != 100 {
		t.Fatalf("exact recording identity lost: %+v", result)
	}
	track.ISRC = ""
	for _, generic := range []string{"", "Various Artists", "V.A.", "Verschiedene Interpreten"} {
		track.AlbumArtist = generic
		if result := matcher.Score(track, provider.MediaCandidate{Title: track.Title, Artists: []string{"Clara Schumann"}, DurationMS: track.DurationMS}); result.Breakdown.PrimaryArtistUnconfirmed {
			t.Fatalf("generic compilation credit treated as a performer: %s", generic)
		}
	}
}
