package matcher

import (
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// The audio for a track has to be the same kind of recording as the track:
// a plainly titled track of a live album wants a live upload, a studio track
// a studio one. The release of the wanted track and the album an upload names
// carry that information where the titles do not.
func TestScorePicksTheRecordingKindOfTheRelease(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})

	liveTrack := music.Track{
		Title: "Sign of the Cross", Artists: []string{"Iron Maiden"}, Album: "Rock in Rio",
		DurationMS: 660000, ReleaseType: music.ReleaseLive,
	}
	studioTrack := music.Track{
		Title: "Sign of the Cross", Artists: []string{"Iron Maiden"}, Album: "The X Factor",
		DurationMS: 660000, ReleaseType: music.ReleaseAlbum,
	}
	// A studio track on a compilation is still the studio recording.
	compilationTrack := studioTrack
	compilationTrack.Album = "Best Of"
	compilationTrack.ReleaseType = music.ReleaseCompilation

	studioUpload := provider.MediaCandidate{
		ID: "studio", Title: "Sign of the Cross", Artists: []string{"Iron Maiden"},
		Album: "The X Factor", DurationMS: 661000, IsMusicService: true,
	}
	studioVideo := provider.MediaCandidate{
		ID: "video", Title: "Sign of the Cross", Uploader: "Iron Maiden", DurationMS: 659000,
	}
	liveTitled := provider.MediaCandidate{
		ID: "live-title", Title: "Sign of the Cross (Live in Rio)", Uploader: "Iron Maiden", DurationMS: 662000,
	}
	// The YouTube Music upload of the very album track: plain title, the album
	// names the release.
	sameAlbumUpload := provider.MediaCandidate{
		ID: "same-album", Title: "Sign of the Cross", Artists: []string{"Iron Maiden"},
		Album: "Rock in Rio", DurationMS: 660000, IsMusicService: true,
	}
	liveAlbumUpload := provider.MediaCandidate{
		ID: "live-album", Title: "Sign of the Cross", Artists: []string{"Iron Maiden"},
		Album: "Live at Donington", DurationMS: 662000, IsMusicService: true,
	}

	cases := []struct {
		name       string
		track      music.Track
		candidates []provider.MediaCandidate
		want       string
	}{
		{"plain live track prefers a live titled upload", liveTrack,
			[]provider.MediaCandidate{studioUpload, studioVideo, liveTitled}, "live-title"},
		{"plain live track prefers the upload of its own album", liveTrack,
			[]provider.MediaCandidate{studioUpload, sameAlbumUpload}, "same-album"},
		{"plain live track prefers another live album over the studio one", liveTrack,
			[]provider.MediaCandidate{studioUpload, liveAlbumUpload}, "live-album"},
		{"studio track prefers the studio upload over a live titled one", studioTrack,
			[]provider.MediaCandidate{liveTitled, studioUpload}, "studio"},
		{"studio track prefers the studio upload over a plain titled live album upload", studioTrack,
			[]provider.MediaCandidate{liveAlbumUpload, studioUpload}, "studio"},
		{"compilation track prefers the studio upload", compilationTrack,
			[]provider.MediaCandidate{liveAlbumUpload, studioVideo}, "video"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ranked := m.Rank(tc.track, tc.candidates)
			if got := ranked[0].Candidate.ID; got != tc.want {
				t.Fatalf("best = %q (%.1f, %s), want %q; ranking: %+v",
					got, ranked[0].Score, ranked[0].Reason(), tc.want, ranked)
			}
			if ranked[0].Score < m.MinScore() {
				t.Errorf("best candidate scored %.1f, below the threshold", ranked[0].Score)
			}
		})
	}
}

// A studio track must not accept an upload that its album shows to be live,
// and a live track must not accept a plain studio upload.
func TestScoreRejectsTheOtherRecordingKind(t *testing.T) {
	m := New(Options{MinScore: 70, DurationToleranceMS: 4000})
	studio := music.Track{Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationMS: 205000, ReleaseType: music.ReleaseAlbum}
	live := music.Track{Title: "Song", Artists: []string{"Artist"}, Album: "Paris 2012", DurationMS: 205000, ReleaseType: music.ReleaseLive}

	cases := []struct {
		name      string
		track     music.Track
		candidate provider.MediaCandidate
	}{
		{"studio track, upload from a live album", studio, provider.MediaCandidate{
			ID: "a", Title: "Song", Artists: []string{"Artist"}, Album: "Live in Paris", DurationMS: 205000}},
		{"studio track, upload from an unplugged album", studio, provider.MediaCandidate{
			ID: "b", Title: "Song", Artists: []string{"Artist"}, Album: "MTV Unplugged", DurationMS: 205000}},
		{"live track, plain studio upload", live, provider.MediaCandidate{
			ID: "c", Title: "Song", Artists: []string{"Artist"}, Album: "Album", DurationMS: 205000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := m.Score(tc.track, tc.candidate)
			if res.Score >= m.MinScore() {
				t.Fatalf("scored %.1f (%+v), want below %.1f", res.Score, res.Breakdown, m.MinScore())
			}
			if res.Breakdown.VersionPenalty == 0 {
				t.Errorf("no version penalty: %+v", res.Breakdown)
			}
		})
	}
}

func TestAlbumNamesLiveRelease(t *testing.T) {
	cases := map[string]bool{
		"Live at Wembley '86":          true,
		"Rock in Rio (Live)":           true,
		"MTV Unplugged in New York":    true,
		"Alive in Athens - Live":       true,
		"Live in Paris":                true,
		"Long Live":                    false,
		"Live Forever":                 false,
		"Alive":                        false,
		"Rock in Rio":                  false,
		"":                             false,
		"Greatest Hits (Remastered)":   false,
		"The Live Sessions Collection": false,
	}
	for title, want := range cases {
		if got := AlbumNamesLiveRelease(title); got != want {
			t.Errorf("AlbumNamesLiveRelease(%q) = %v, want %v", title, got, want)
		}
	}
}
