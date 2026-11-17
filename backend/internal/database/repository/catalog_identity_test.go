package repository

import (
	"context"
	"testing"

	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/music"
)

// A live album usually lists its tracks under the plain song title. Such a
// track must not make the studio recording count as already owned, whether
// its row was written with the current identity key or with the one earlier
// versions used.
func TestFindTrackKeepsStudioAndLiveRecordingsApart(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	artist, err := c.UpsertArtist(ctx, music.Artist{Name: "Artist", Provider: "ytmusic", SourceID: "artist:identity"})
	if err != nil {
		t.Fatal(err)
	}
	liveRelease, err := c.UpsertRelease(ctx, music.Release{
		Title: "Live in Paris", Provider: "ytmusic", SourceID: "release:live", ReleaseType: music.ReleaseLive,
	}, artist.ID)
	if err != nil {
		t.Fatal(err)
	}

	liveTrack := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000, ReleaseType: music.ReleaseLive}
	studioTrack := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 201000, ReleaseType: music.ReleaseAlbum}

	storedLive, err := c.UpsertTrack(ctx, liveTrack, liveRelease.ID, artist.ID, 4000)
	if err != nil {
		t.Fatal(err)
	}

	check := func(stage string) {
		t.Helper()
		found, err := c.FindTrack(ctx, studioTrack, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if found != nil {
			t.Errorf("%s: the studio recording was found as track %s, the live one; it must count as missing", stage, found.ID)
		}
		found, err = c.FindTrack(ctx, liveTrack, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if found == nil || found.ID != storedLive.ID {
			t.Errorf("%s: the live recording was not found again (got %v)", stage, found)
		}
	}
	check("current key")

	// The same row as an earlier version wrote it: under the key without the
	// live marker, which is also the studio recording's key.
	legacy := discography.IdentityOf(liveTrack).LegacyLiveKeys
	if len(legacy) == 0 {
		t.Fatal("a plainly titled track of a live release has a legacy key")
	}
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET identity_key = $2 WHERE id = $1`, storedLive.ID, legacy[0]); err != nil {
		t.Fatal(err)
	}
	check("legacy key")
}

// A concert album whose title carries no marker was catalogued as an album,
// its plainly titled songs under the studio key. Once the track list makes it
// a live release, those rows must still be found by its own tracks, and must
// no longer stand in for the studio recording — also while the stored release
// is still typed "album", and whether or not the release row is updated.
func TestFindTrackRecognisesAnAlbumThatTurnedOutLive(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	artist, err := c.UpsertArtist(ctx, music.Artist{Name: "Artist", Provider: "deezer", SourceID: "artist:inferred"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := c.UpsertRelease(ctx, music.Release{
		Title: "Rock in Rio", Provider: "deezer", SourceID: "rock-in-rio", ReleaseType: music.ReleaseAlbum,
	}, artist.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The library as an earlier version wrote it: the album's tracks under
	// the keys their titles gave them, the release typed "album".
	asAlbum := func(title string, duration int) music.Track {
		return music.Track{Title: title, Artists: []string{"Artist"}, DurationMS: duration,
			ReleaseType: music.ReleaseAlbum, SourceProvider: "deezer", ReleaseID: "rock-in-rio"}
	}
	var plainID string
	for i, title := range []string{"Wicker Man - Live", "Ghost - Live", "Brave New World - Live", "Sign of the Cross"} {
		row, err := c.UpsertTrack(ctx, asAlbum(title, 200000+i*30000), stored.ID, artist.ID, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if title == "Sign of the Cross" {
			plainID = row.ID
		}
	}

	// The same song resolved now: the release is live.
	liveSong := asAlbum("Sign of the Cross", 290000)
	liveSong.ReleaseType = music.ReleaseLive
	studioSong := music.Track{Title: "Sign of the Cross", Artists: []string{"Artist"}, DurationMS: 291000,
		ReleaseType: music.ReleaseAlbum, SourceProvider: "deezer", ReleaseID: "the-x-factor"}
	// The same song from a different provider release that knows nothing of
	// the stored one: only the stored track share can tell.
	liveElsewhere := liveSong
	liveElsewhere.SourceProvider, liveElsewhere.ReleaseID = "spotify", "other"

	cases := []struct {
		name    string
		track   music.Track
		wantRow string
	}{
		{"the live album's own track finds its row", liveSong, plainID},
		{"the live track finds the row through the stored release", liveElsewhere, plainID},
		{"the studio recording is not the live row", studioSong, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := c.FindTrack(ctx, tc.track, 4000)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if found != nil {
				got = found.ID
			}
			if got != tc.wantRow {
				t.Errorf("found %q, want %q", got, tc.wantRow)
			}
		})
	}

	// A studio album with one live bonus track stays a studio album: its
	// plain rows still are the studio recordings.
	studioRelease, err := c.UpsertRelease(ctx, music.Release{
		Title: "Studio", Provider: "deezer", SourceID: "studio", ReleaseType: music.ReleaseAlbum,
	}, artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	var studioRow string
	for i, title := range []string{"One", "Two", "Three", "Three - Live"} {
		track := asAlbum(title, 180000+i*30000)
		track.ReleaseID = "studio"
		row, err := c.UpsertTrack(ctx, track, studioRelease.ID, artist.ID, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if title == "One" {
			studioRow = row.ID
		}
	}
	other := asAlbum("One", 180000)
	other.ReleaseID = "best-of"
	other.ReleaseType = music.ReleaseCompilation
	found, err := c.FindTrack(ctx, other, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != studioRow {
		t.Errorf("the studio recording on a compilation was not found (got %v, want %s)", found, studioRow)
	}
}
