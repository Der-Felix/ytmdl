package music_test

import (
	"reflect"
	"testing"

	"ytdm/backend/internal/music"
)

func TestApplyReleaseContextFillsTheTrackContext(t *testing.T) {
	release := music.Release{
		SourceID: "rel-1", ReleaseType: music.ReleaseAlbum, Year: 2001,
		CoverURL: "https://example.test/c.jpg", Title: "Discovery",
		AlbumArtist: "Daft Punk", Artists: []string{"Daft Punk"}, TrackCount: 14,
	}
	tracks := []music.Track{
		{Title: "One More Time", Artists: []string{"Daft Punk"}, TrackNumber: 1},
		{Title: "Aerodynamic", Artists: []string{"Daft Punk"}, TrackNumber: 2},
	}

	music.ApplyReleaseContext(&release, tracks, "Daft Punk")

	for i, track := range tracks {
		if track.ReleaseID != "rel-1" || track.ReleaseType != music.ReleaseAlbum {
			t.Errorf("track %d: release context missing: %+v", i, track)
		}
		if track.Year != 2001 || track.CoverURL != "https://example.test/c.jpg" {
			t.Errorf("track %d: year/cover not inherited: %+v", i, track)
		}
		if track.Album != "Discovery" {
			t.Errorf("track %d: album = %q", i, track.Album)
		}
		if track.AlbumArtist != "Daft Punk" {
			t.Errorf("track %d: album artist = %q", i, track.AlbumArtist)
		}
		if track.DiscNumber != 1 || track.DiscTotal != 1 {
			t.Errorf("track %d: disc = %d/%d, want 1/1", i, track.DiscNumber, track.DiscTotal)
		}
		// The provider reported 14 tracks; that count is authoritative even
		// though only two were resolved here.
		if track.TrackTotal != 14 {
			t.Errorf("track %d: TrackTotal = %d, want 14", i, track.TrackTotal)
		}
	}
}

func TestApplyReleaseContextDerivesTotalsOnlyFromRealData(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseAlbum, Title: "B",
		AlbumArtist: "X", Artists: []string{"X"}}
	tracks := []music.Track{
		{Title: "1", Artists: []string{"X"}, TrackNumber: 1},
		{Title: "2", Artists: []string{"X"}, TrackNumber: 2},
		{Title: "3", Artists: []string{"X"}, TrackNumber: 3},
	}
	music.ApplyReleaseContext(&release, tracks, "X")
	for i, track := range tracks {
		if track.TrackTotal != 3 {
			t.Errorf("track %d: TrackTotal = %d, want 3 (derived from the resolved list)", i, track.TrackTotal)
		}
	}

	// Nothing known and nothing resolved: the total stays unset rather than
	// being invented.
	empty := music.Release{SourceID: "r2", ReleaseType: music.ReleaseAlbum, Title: "C", AlbumArtist: "X"}
	var none []music.Track
	music.ApplyReleaseContext(&empty, none, "X")
}

func TestApplyReleaseContextMultiDiscTotalsArePerDisc(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseAlbum, Title: "The Wall",
		AlbumArtist: "Pink Floyd", Artists: []string{"Pink Floyd"}, TrackCount: 26}
	tracks := []music.Track{
		{Title: "A", Artists: []string{"Pink Floyd"}, TrackNumber: 1, DiscNumber: 1},
		{Title: "B", Artists: []string{"Pink Floyd"}, TrackNumber: 2, DiscNumber: 1},
		{Title: "C", Artists: []string{"Pink Floyd"}, TrackNumber: 1, DiscNumber: 2},
	}
	music.ApplyReleaseContext(&release, tracks, "Pink Floyd")

	if tracks[0].DiscTotal != 2 || tracks[2].DiscTotal != 2 {
		t.Fatalf("DiscTotal = %d/%d, want 2", tracks[0].DiscTotal, tracks[2].DiscTotal)
	}
	if tracks[0].TrackTotal != 2 {
		t.Errorf("disc 1 TrackTotal = %d, want 2", tracks[0].TrackTotal)
	}
	if tracks[2].TrackTotal != 1 {
		t.Errorf("disc 2 TrackTotal = %d, want 1", tracks[2].TrackTotal)
	}
}

func TestApplyReleaseContextReducesACorroboratedJoinedCredit(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseSingle, Title: "CCN",
		Artists: []string{"LACAZETTE", "Bushido"}, AlbumArtist: "LACAZETTE & Bushido"}
	tracks := []music.Track{{Title: "CCN", Artists: []string{"LACAZETTE", "Bushido"}, TrackNumber: 1}}

	music.ApplyReleaseContext(&release, tracks, "LACAZETTE")

	if release.AlbumArtist != "LACAZETTE" {
		t.Errorf("release album artist = %q, want LACAZETTE", release.AlbumArtist)
	}
	if tracks[0].AlbumArtist != "LACAZETTE" {
		t.Errorf("track album artist = %q, want LACAZETTE", tracks[0].AlbumArtist)
	}
	want := []string{"LACAZETTE", "Bushido"}
	if !reflect.DeepEqual(tracks[0].Artists, want) {
		t.Errorf("track artists = %v, want %v — the feature must stay in ARTIST", tracks[0].Artists, want)
	}
}

func TestApplyReleaseContextFeaturedArtistNeverBecomesTheAlbumArtist(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseSingle, Title: "This Is What You Came For",
		Artists: []string{"Calvin Harris"}, AlbumArtist: "Calvin Harris"}
	tracks := []music.Track{{
		Title: "This Is What You Came For", TrackNumber: 1,
		Artists: []string{"Calvin Harris", "Rihanna"},
	}}

	music.ApplyReleaseContext(&release, tracks, "Calvin Harris")

	if release.AlbumArtist != "Calvin Harris" || tracks[0].AlbumArtist != "Calvin Harris" {
		t.Fatalf("album artist = %q / %q, want Calvin Harris", release.AlbumArtist, tracks[0].AlbumArtist)
	}
	if !reflect.DeepEqual(tracks[0].Artists, []string{"Calvin Harris", "Rihanna"}) {
		t.Errorf("track artists = %v", tracks[0].Artists)
	}
}

func TestApplyReleaseContextVariousArtistsPlaceholder(t *testing.T) {
	// Deezer files this as record_type "album" with its localised placeholder
	// artist, so the placeholder name is the only usable signal.
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseAlbum,
		Title:       "The Greatest Showman (Original Motion Picture Soundtrack)",
		AlbumArtist: "Verschiedene Interpreten", Artists: []string{"Verschiedene Interpreten"}}
	tracks := []music.Track{
		{Title: "A", Artists: []string{"Hugh Jackman"}, TrackNumber: 1},
		{Title: "B", Artists: []string{"Keala Settle"}, TrackNumber: 2},
	}

	music.ApplyReleaseContext(&release, tracks, "")

	if release.AlbumArtist != music.VariousArtists {
		t.Fatalf("album artist = %q, want %q", release.AlbumArtist, music.VariousArtists)
	}
	if !release.Compilation || !tracks[0].Compilation {
		t.Error("the compilation flag was not set")
	}
	if tracks[0].Artists[0] != "Hugh Jackman" {
		t.Errorf("track artist was overwritten: %v", tracks[0].Artists)
	}
	if tracks[0].AlbumArtist != music.VariousArtists {
		t.Errorf("track album artist = %q", tracks[0].AlbumArtist)
	}
}

func TestApplyReleaseContextTypedCompilationWithDifferentArtists(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseCompilation, Title: "Awesome Mix"}
	tracks := []music.Track{
		{Title: "A", Artists: []string{"Blue Swede"}, TrackNumber: 1},
		{Title: "B", Artists: []string{"Raspberries"}, TrackNumber: 2},
	}
	music.ApplyReleaseContext(&release, tracks, "")

	if release.AlbumArtist != music.VariousArtists || !release.Compilation {
		t.Fatalf("album artist = %q, compilation = %v", release.AlbumArtist, release.Compilation)
	}
}

func TestApplyReleaseContextSingleArtistCompilationStaysUnderThatArtist(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseCompilation, Title: "Best of X",
		AlbumArtist: "X", Artists: []string{"X"}}
	tracks := []music.Track{
		{Title: "A", Artists: []string{"X"}, TrackNumber: 1},
		{Title: "B", Artists: []string{"X"}, TrackNumber: 2},
	}
	music.ApplyReleaseContext(&release, tracks, "X")

	if release.AlbumArtist != "X" {
		t.Fatalf("album artist = %q, want X", release.AlbumArtist)
	}
	if release.Compilation {
		t.Error("a one-artist compilation must not be flagged")
	}
}

func TestApplyReleaseContextCollaborationAlbumIsNotACompilation(t *testing.T) {
	// Several credited artists alone must never produce Various Artists.
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseAlbum, Title: "Watch the Throne",
		AlbumArtist: "JAY-Z", Artists: []string{"JAY-Z", "Kanye West"}}
	tracks := []music.Track{
		{Title: "A", Artists: []string{"JAY-Z"}, TrackNumber: 1},
		{Title: "B", Artists: []string{"Kanye West"}, TrackNumber: 2},
	}
	music.ApplyReleaseContext(&release, tracks, "JAY-Z")

	if release.AlbumArtist != "JAY-Z" || release.Compilation {
		t.Fatalf("album artist = %q, compilation = %v — a collaboration album must stay one",
			release.AlbumArtist, release.Compilation)
	}
}

func TestApplyReleaseContextFallsBackToTheContextArtist(t *testing.T) {
	release := music.Release{SourceID: "r", ReleaseType: music.ReleaseSingle, Title: "X"}
	tracks := []music.Track{{Title: "X", TrackNumber: 1}}
	music.ApplyReleaseContext(&release, tracks, "Some Artist")

	if release.AlbumArtist != "Some Artist" {
		t.Fatalf("album artist = %q, want the context artist", release.AlbumArtist)
	}
	if len(tracks[0].Artists) != 0 {
		t.Errorf("track artists = %v, want the release artists (none here)", tracks[0].Artists)
	}
}

func TestApplyReleaseContextNilReleaseIsSafe(t *testing.T) {
	music.ApplyReleaseContext(nil, []music.Track{{Title: "A"}}, "X")
}
