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
