package discography

import (
	"context"
	"fmt"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// titles builds a track list from plain titles.
func titles(names ...string) []music.Track {
	out := make([]music.Track, 0, len(names))
	for i, name := range names {
		out = append(out, music.Track{Title: name, Artists: []string{"Artist"}, TrackNumber: i + 1, DurationMS: 200000, SourceProvider: "deezer"})
	}
	return out
}

func numbered(n int, format string) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf(format, i))
	}
	return out
}

func TestInferLiveRelease(t *testing.T) {
	cases := []struct {
		name    string
		release music.Release
		tracks  []music.Track
		want    music.ReleaseType
	}{
		{
			// A concert album named after the venue: the titles carry the
			// marker, the intro and one song do not.
			name:    "live album without a title marker",
			release: music.Release{Title: "Rock in Rio", ReleaseType: music.ReleaseAlbum},
			tracks: titles("Intro", "The Wicker Man - Live", "Ghost of the Navigator - Live",
				"Brave New World - Live", "Wrathchild - Live", "2 Minutes to Midnight - Live",
				"Blood Brothers - Live", "Sign of the Cross", "The Mercenary - Live",
				"The Trooper - Live", "Dream of Mirrors - Live", "The Clansman - Live"),
			want: music.ReleaseLive,
		},
		{
			name:    "every track marked in brackets",
			release: music.Release{Title: "Wembley 1986", ReleaseType: music.ReleaseAlbum},
			tracks:  titles(numbered(8, "Song %d (Live at Wembley Stadium)")...),
			want:    music.ReleaseLive,
		},
		{
			name:    "studio album",
			release: music.Release{Title: "Brave New World", ReleaseType: music.ReleaseAlbum},
			tracks:  titles("The Wicker Man", "Ghost of the Navigator", "Brave New World", "Blood Brothers"),
			want:    music.ReleaseAlbum,
		},
		{
			// Twelve studio tracks and a twelve track concert as a bonus disc:
			// the studio tracks must keep their studio identity.
			name:    "deluxe edition with a live bonus disc",
			release: music.Release{Title: "Album (Deluxe Edition)", ReleaseType: music.ReleaseAlbum},
			tracks:  titles(append(numbered(12, "Song %d"), numbered(12, "Song %d - Live")...)...),
			want:    music.ReleaseAlbum,
		},
		{
			name:    "best-of typed as a compilation with mostly live tracks",
			release: music.Release{Title: "Greatest Hits", ReleaseType: music.ReleaseCompilation},
			tracks:  titles(append(numbered(9, "Song %d (Live)"), "Song 10")...),
			want:    music.ReleaseCompilation,
		},
		{
			name:    "too few marked tracks to tell",
			release: music.Release{Title: "Two Songs", ReleaseType: music.ReleaseAlbum},
			tracks:  titles("Song (Live)", "Other Song (Live)"),
			want:    music.ReleaseAlbum,
		},
		{
			// "Live Forever" is a song title, not a marker.
			name:    "titles that only contain the word",
			release: music.Release{Title: "Definitely Maybe", ReleaseType: music.ReleaseAlbum},
			tracks:  titles("Live Forever", "Live and Let Die", "Long Live", "Alive"),
			want:    music.ReleaseAlbum,
		},
		{
			name:    "single keeps its type",
			release: music.Release{Title: "Song", ReleaseType: music.ReleaseSingle},
			tracks:  titles("Song (Live)", "Song - Live", "Song (Live at X)"),
			want:    music.ReleaseSingle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			release := tc.release
			changed := InferLiveRelease(&release, tc.tracks)
			if release.ReleaseType != tc.want {
				t.Fatalf("release type = %q, want %q", release.ReleaseType, tc.want)
			}
			if changed != (tc.release.ReleaseType != tc.want) {
				t.Errorf("InferLiveRelease reported %v", changed)
			}
		})
	}
}

// The plainly titled song of a live album whose title has no marker must not
// share the studio recording's identity once the track list made the album a
// live release; the same studio recording on an album and on a compilation
// keeps one identity.
func TestInferredLiveReleaseSeparatesItsPlainTracksFromTheStudioRecording(t *testing.T) {
	liveRelease := music.Release{Title: "Rock in Rio", ReleaseType: music.ReleaseAlbum, SourceID: "live", Provider: "deezer"}
	liveTracks := titles("Intro - Live", "The Wicker Man - Live", "Ghost of the Navigator - Live",
		"Brave New World - Live", "Sign of the Cross")
	applyReleaseContext(&liveRelease, liveTracks, "Artist")

	studioRelease := music.Release{Title: "The X Factor", ReleaseType: music.ReleaseAlbum, SourceID: "studio", Provider: "deezer"}
	studioTracks := titles("Sign of the Cross", "Lord of the Flies")
	applyReleaseContext(&studioRelease, studioTracks, "Artist")

	bestOf := music.Release{Title: "Best Of", ReleaseType: music.ReleaseCompilation, SourceID: "best", Provider: "deezer"}
	bestTracks := titles("Sign of the Cross", "The Trooper")
	applyReleaseContext(&bestOf, bestTracks, "Artist")

	if liveRelease.ReleaseType != music.ReleaseLive || liveTracks[4].ReleaseType != music.ReleaseLive {
		t.Fatalf("the album and its tracks were not made live: %q / %q", liveRelease.ReleaseType, liveTracks[4].ReleaseType)
	}
	liveSong, studioSong, bestSong := liveTracks[4], studioTracks[0], bestTracks[0]
	if IdentityKey(liveSong) == IdentityKey(studioSong) {
		t.Error("the plainly titled live track shares the studio recording's identity")
	}
	if IdentityKey(bestSong) != IdentityKey(studioSong) {
		t.Error("the studio recording on a compilation must keep the album track's identity")
	}

	groups := Deduplicate([]music.Track{studioSong, liveSong, bestSong}, DedupOptions{})
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2: the studio recording (album + compilation) and the live one", len(groups))
	}

	// The track was stored under the studio key before; the lookup knows it,
	// and it knows the release it came from.
	id := IdentityOf(liveSong)
	if !id.Live || len(id.LegacyLiveKeys) == 0 || id.LegacyLiveKeys[0] != IdentityKey(studioSong) {
		t.Errorf("identity = %+v, want live with the studio key as legacy key", id)
	}
	if id.ReleaseProvider != "deezer" || id.ReleaseSourceID != "live" {
		t.Errorf("identity release = %q/%q, want deezer/live", id.ReleaseProvider, id.ReleaseSourceID)
	}
}

// A release that the track list shows to be live is filtered like any other
// live release: the default filter leaves it out.
func TestResolveArtistFiltersAnInferredLiveRelease(t *testing.T) {
	mockP := &mockMetadataProvider{
		name: "deezer",
		getDisco: func(ctx context.Context, artistID string) ([]music.Release, error) {
			return []music.Release{
				{ID: "studio", SourceID: "studio", Title: "Brave New World", ReleaseType: music.ReleaseAlbum, Provider: "deezer"},
				{ID: "live", SourceID: "live", Title: "Rock in Rio", ReleaseType: music.ReleaseAlbum, Provider: "deezer"},
			}, nil
		},
		getTracks: func(ctx context.Context, releaseID string) ([]music.Track, error) {
			if releaseID == "live" {
				return titles("Intro", "The Wicker Man - Live", "Ghost of the Navigator - Live", "Brave New World - Live"), nil
			}
			return titles("The Wicker Man", "Ghost of the Navigator", "Brave New World"), nil
		},
	}
	reg := provider.NewRegistry()
	reg.RegisterMetadata(mockP)
	service, err := NewService(Options{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		filter   music.ReleaseFilter
		releases int
	}{
		{"default filter", music.DefaultReleaseFilter(), 1},
		{"live releases wanted", music.ReleaseFilter{Albums: true, Live: true}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := service.ResolveArtist(context.Background(), ArtistRequest{
				Provider: "deezer", ArtistID: "a1", Filter: tc.filter,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Releases) != tc.releases {
				t.Fatalf("releases = %d, want %d", len(res.Releases), tc.releases)
			}
			for _, r := range res.Releases {
				if r.SourceID == "live" && r.ReleaseType != music.ReleaseLive {
					t.Errorf("the concert album is typed %q", r.ReleaseType)
				}
			}
		})
	}
}
