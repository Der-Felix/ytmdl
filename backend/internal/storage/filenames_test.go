package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
)

func TestSanitizeComponentRemovesDangerousCharacters(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"forward slash", "AC/DC", "AC-DC"},
		{"backslash", `AC\DC`, "AC-DC"},
		{"colon", "Album: The Return", "Album- The Return"},
		{"asterisk", "N*E*R*D", "N_E_R_D"},
		{"question mark", "Who?", "Who_"},
		{"quote", `Say "Hello"`, "Say _Hello_"},
		{"angle brackets", "<script>", "_script_"},
		{"pipe", "A|B", "A_B"},
		{"control characters", "Song\x07Name", "Song Name"},
		{"newline", "Song\nName", "Song Name"},
		{"tab", "Song\tName", "Song Name"},
		{"nul byte", "Song\x00Name", "SongName"},
		{"collapsed whitespace", "  Song    Name  ", "Song Name"},
		{"trailing dot", "Album.", "Album"},
		{"leading dot", ".hidden", "hidden"},
		{"unicode kept", "Björk – Jóga", "Björk – Jóga"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeComponent(tc.in); got != tc.want {
				t.Fatalf("SanitizeComponent(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeComponentBlocksTraversal(t *testing.T) {
	for _, in := range []string{
		"..", ".", "../..", "../../etc/passwd", "/etc/passwd",
		`..\..\windows`, "....//....//", "~/.ssh/authorized_keys",
	} {
		got := SanitizeComponent(in)
		if got == "" {
			t.Fatalf("SanitizeComponent(%q) returned an empty component", in)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Fatalf("SanitizeComponent(%q) = %q still contains a separator", in, got)
		}
		if got == "." || got == ".." {
			t.Fatalf("SanitizeComponent(%q) = %q is a relative path element", in, got)
		}
	}
}

func TestSanitizeComponentNeverEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "...", "///", "\x00", "​"} {
		if got := SanitizeComponent(in); got == "" {
			t.Fatalf("SanitizeComponent(%q) returned an empty string", in)
		}
	}
}

func TestSanitizeComponentReservedNames(t *testing.T) {
	for _, in := range []string{"CON", "nul", "Com1", "LPT9"} {
		got := SanitizeComponent(in)
		if strings.EqualFold(got, in) {
			t.Fatalf("SanitizeComponent(%q) = %q, reserved name was not escaped", in, got)
		}
	}
}

func TestSanitizeComponentLength(t *testing.T) {
	long := strings.Repeat("ä", 500)
	got := SanitizeComponent(long)
	if n := len([]rune(got)); n > MaxComponentLength {
		t.Fatalf("component has %d runes, want at most %d", n, MaxComponentLength)
	}
}

func TestSanitizeFilenameKeepsExtension(t *testing.T) {
	got := SanitizeFilename("01 - Song/Name", ".opus")
	if got != "01 - Song-Name.opus" {
		t.Fatalf("got %q", got)
	}

	long := SanitizeFilename(strings.Repeat("x", 400), ".opus")
	if n := len([]rune(long)); n > MaxComponentLength {
		t.Fatalf("file name has %d runes, want at most %d", n, MaxComponentLength)
	}
	if !strings.HasSuffix(long, ".opus") {
		t.Fatalf("extension lost: %q", long)
	}
}

func TestSanitizeFilenameSanitisesExtension(t *testing.T) {
	got := SanitizeFilename("Song", "../../evil")
	if strings.ContainsAny(got, `/\`) {
		t.Fatalf("got %q, extension was not sanitised", got)
	}
}

func TestTrackFileName(t *testing.T) {
	tests := []struct {
		name  string
		track music.Track
		want  string
	}{
		{"numbered", music.Track{Title: "Song", TrackNumber: 1}, "01 - Song.opus"},
		{"two digits", music.Track{Title: "Song", TrackNumber: 12}, "12 - Song.opus"},
		{"three digits", music.Track{Title: "Song", TrackNumber: 123}, "123 - Song.opus"},
		{"unnumbered", music.Track{Title: "Song"}, "Song.opus"},
		// A multi disc release uses the disc-prefixed form Plex documents:
		// disc three, track two is "302 - Track.ext".
		{"multi disc", music.Track{Title: "Song", TrackNumber: 3, DiscNumber: 2, DiscTotal: 2}, "203 - Song.opus"},
		{"multi disc first disc", music.Track{Title: "Song", TrackNumber: 1, DiscNumber: 1, DiscTotal: 2}, "101 - Song.opus"},
		{"multi disc third disc", music.Track{Title: "Song", TrackNumber: 2, DiscNumber: 3, DiscTotal: 3}, "302 - Song.opus"},
		{"multi disc without a disc number defaults to one", music.Track{Title: "Song", TrackNumber: 4, DiscTotal: 2}, "104 - Song.opus"},
		{"single disc keeps plain numbering", music.Track{Title: "Song", TrackNumber: 3, DiscNumber: 1, DiscTotal: 1}, "03 - Song.opus"},
		{"empty title", music.Track{Title: "", TrackNumber: 1}, "01 - Unknown Title.opus"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrackFileName(tc.track, ".opus"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReleaseDirName(t *testing.T) {
	tests := []struct {
		release music.Release
		want    string
	}{
		{music.Release{Title: "Album", Year: 2000, ReleaseType: music.ReleaseAlbum}, "2000 - Album"},
		{music.Release{Title: "Single", Year: 2026, ReleaseType: music.ReleaseSingle}, "2026 - Single [Single]"},
		{music.Release{Title: "Short", Year: 2020, ReleaseType: music.ReleaseEP}, "2020 - Short [EP]"},
		{music.Release{Title: "Album", ReleaseType: music.ReleaseAlbum}, "Album"},
		{music.Release{Title: "Best Of", Year: 1999, ReleaseType: music.ReleaseCompilation}, "1999 - Best Of [Compilation]"},
	}
	for _, tc := range tests {
		if got := ReleaseDirName(tc.release); got != tc.want {
			t.Errorf("ReleaseDirName(%+v) = %q, want %q", tc.release, got, tc.want)
		}
	}
}

func TestLayoutContainsPaths(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}

	release := music.Release{
		Title:       "../../../etc",
		AlbumArtist: "../../root",
		Year:        2001,
		ReleaseType: music.ReleaseAlbum,
	}
	dir, err := layout.ReleaseDir(release)
	if err != nil {
		t.Fatalf("ReleaseDir returned an error: %v", err)
	}
	if !strings.HasPrefix(dir, filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("release dir %q escaped the library root %q", dir, root)
	}

	path, err := layout.TrackPath(release, music.Track{Title: "../../evil", TrackNumber: 1}, ".opus")
	if err != nil {
		t.Fatalf("TrackPath returned an error: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("track path %q escaped the library root %q", path, root)
	}
	for _, element := range strings.Split(strings.TrimPrefix(path, filepath.Clean(root)), string(filepath.Separator)) {
		if element == ".." || element == "." {
			t.Fatalf("track path %q still contains a traversal element", path)
		}
	}
}

func TestLayoutRejectsEmptyRoot(t *testing.T) {
	if _, err := NewLayout("  "); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}

func TestSidecarPathForKeepsTheBasename(t *testing.T) {
	cases := []struct {
		audio string
		ext   string
		want  string
	}{
		{"/music/A/2001 - B/04 - C.opus", ".txt", "/music/A/2001 - B/04 - C.txt"},
		{"/music/A/2001 - B/201 - C.opus", ".lrc", "/music/A/2001 - B/201 - C.lrc"},
		{"/music/A/2001 - B/C.m4a", ".lrc", "/music/A/2001 - B/C.lrc"},
		{"/music/A/no-extension", ".lrc", "/music/A/no-extension.lrc"},
	}
	for _, c := range cases {
		if got := SidecarPathFor(c.audio, c.ext); got != c.want {
			t.Errorf("SidecarPathFor(%q, %q) = %q, want %q", c.audio, c.ext, got, c.want)
		}
	}
}

// TestLyricsPathAlwaysMatchesTrackPath is the guarantee that there is no second
// naming function for lyrics: whatever the track path rules produce, the
// sidecar follows.
func TestLyricsPathAlwaysMatchesTrackPath(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	release := music.Release{Title: "The Wall", AlbumArtist: "Pink Floyd", Year: 1979, ReleaseType: music.ReleaseAlbum}

	for _, track := range []music.Track{
		{Title: "Hey You", TrackNumber: 1, DiscNumber: 2, DiscTotal: 2},
		{Title: "In the Flesh", TrackNumber: 1, DiscNumber: 1, DiscTotal: 1},
		{Title: "Odd/Name: with*chars", TrackNumber: 7, DiscNumber: 1, DiscTotal: 1},
		{Title: "Unnumbered"},
	} {
		audio, err := layout.TrackPath(release, track, ".opus")
		if err != nil {
			t.Fatalf("TrackPath: %v", err)
		}
		for _, ext := range LyricsExtensions() {
			sidecar, err := layout.LyricsPath(release, track, ".opus", ext)
			if err != nil {
				t.Fatalf("LyricsPath: %v", err)
			}
			if strings.TrimSuffix(audio, ".opus") != strings.TrimSuffix(sidecar, ext) {
				t.Errorf("audio %q and sidecar %q do not share a basename", audio, sidecar)
			}
		}
	}
}

func TestCoverFileNameFor(t *testing.T) {
	cases := map[string]string{
		".png":  "cover.png",
		".PNG":  "cover.png",
		".jpg":  "cover.jpg",
		".jpeg": "cover.jpg",
		"":      "cover.jpg",
	}
	for ext, want := range cases {
		if got := CoverFileNameFor(ext); got != want {
			t.Errorf("CoverFileNameFor(%q) = %q, want %q", ext, got, want)
		}
	}
}
