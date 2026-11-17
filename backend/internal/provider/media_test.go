package provider

import (
	"testing"

	"ytdm/backend/internal/music"
)

func TestSearchQueryAsksForTheLiveRecordingOfALiveRelease(t *testing.T) {
	cases := []struct {
		name  string
		track music.Track
		want  string
	}{
		{"studio track", music.Track{Title: "Song", Artists: []string{"Artist"}, ReleaseType: music.ReleaseAlbum}, "Artist Song"},
		{"compilation track", music.Track{Title: "Song", Artists: []string{"Artist"}, ReleaseType: music.ReleaseCompilation}, "Artist Song"},
		{"plain track of a live release", music.Track{Title: "Song", Artists: []string{"Artist"}, ReleaseType: music.ReleaseLive}, "Artist Song live"},
		{"live titled track of a live release", music.Track{Title: "Song (Live)", Artists: []string{"Artist"}, ReleaseType: music.ReleaseLive}, "Artist Song (Live)"},
		{"live release without a title", music.Track{Artists: []string{"Artist"}, ReleaseType: music.ReleaseLive}, "Artist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SearchQuery(tc.track); got != tc.want {
				t.Errorf("SearchQuery = %q, want %q", got, tc.want)
			}
		})
	}
}
