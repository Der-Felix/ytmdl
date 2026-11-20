package storage

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"ytdm/backend/internal/music"
)

// TestLookAlikesNeverBuildATraversal checks every refused character on its own
// and in the combinations a traversal needs: the replacement must never be a
// separator, a dot or an empty name.
func TestLookAlikesNeverBuildATraversal(t *testing.T) {
	for from, to := range lookAlikes {
		if to == '/' || to == '\\' || to == '.' || to < utf8.RuneSelf {
			t.Fatalf("%q maps to %q, which is ASCII", from, to)
		}
	}
	for _, in := range []string{
		"..", "../..", `..\..`, "../../etc/passwd", `..\..\windows\system32`,
		"/", `\`, ":", "..:..", "?", "*", `"`, "<>", "|", "./.", ".／.",
	} {
		got := SanitizeComponent(in)
		if got == "" || got == "." || got == ".." {
			t.Fatalf("SanitizeComponent(%q) = %q", in, got)
		}
		if strings.ContainsAny(got, "/\\:*?\"<>|") {
			t.Fatalf("SanitizeComponent(%q) = %q still holds a refused character", in, got)
		}
		if filepath.Base(got) != got || !filepath.IsLocal(got) {
			t.Fatalf("SanitizeComponent(%q) = %q is not a single local component", in, got)
		}
	}
}

// TestLookAlikesNeverBuildAReservedName: a reserved Windows name is ASCII
// only, so a fullwidth character can never complete one, and the reserved
// check still applies to the remaining characters.
func TestLookAlikesNeverBuildAReservedName(t *testing.T) {
	cases := map[string]string{
		"CON":   "_CON",
		"nul.":  "_nul",
		"CON?":  "CON？",
		"?CON":  "？CON",
		"LPT1:": "LPT1：",
		"AUX|":  "AUX｜",
	}
	for in, want := range cases {
		if got := SanitizeComponent(in); got != want {
			t.Errorf("SanitizeComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestByteCapHoldsForFullwidthCharacters: each look-alike takes three bytes,
// so a name made of them reaches the byte cap long before the rune cap. The
// cut must land on a character boundary in every alignment.
func TestByteCapHoldsForFullwidthCharacters(t *testing.T) {
	for prefix := 0; prefix < 3; prefix++ {
		in := strings.Repeat("a", prefix) + strings.Repeat("?", 200)
		for _, got := range []string{SanitizeComponent(in), SanitizeFilename(in, ".opus")} {
			if len(got) > MaxComponentBytes {
				t.Fatalf("prefix %d: %d bytes, want at most %d", prefix, len(got), MaxComponentBytes)
			}
			if utf8.RuneCountInString(got) > MaxComponentLength {
				t.Fatalf("prefix %d: %d runes, want at most %d", prefix, utf8.RuneCountInString(got), MaxComponentLength)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("prefix %d: %q is not valid UTF-8", prefix, got)
			}
		}
	}
	name := SanitizeFilename(strings.Repeat("／", 200), ".opus")
	if !strings.HasSuffix(name, ".opus") || len(name) > MaxComponentBytes {
		t.Fatalf("file name %q lost its extension or exceeds the cap", name)
	}
}

func TestLegacySchemeIsReproducedExactly(t *testing.T) {
	release := music.Release{AlbumArtist: "AC/DC", Title: "Who? Me: Live", Year: 1991, ReleaseType: music.ReleaseLive}
	track := music.Track{Title: `A*B "C" <D>|E`, TrackNumber: 2, DiscNumber: 2, DiscTotal: 2}

	legacy := TrackRelPathFor(NamingSchemeLegacy, release, track, ".opus")
	want := filepath.Join("AC-DC", "1991 - Who_ Me- Live [Live]", "202 - A_B _C_ _D__E.opus")
	if legacy != want {
		t.Fatalf("legacy path = %q, want %q", legacy, want)
	}
	current := TrackRelPathFor(CurrentNamingScheme, release, track, ".opus")
	want = filepath.Join("AC／DC", "1991 - Who？ Me： Live [Live]", "2-02 - A＊B ＂C＂ ＜D＞｜E.opus")
	if current != want {
		t.Fatalf("current path = %q, want %q", current, want)
	}
}

func TestSourceSuffixRoundTrip(t *testing.T) {
	suffix := SourceSuffix("youtube", "abc")
	if suffix == "" || SourceSuffix("", "abc") != "" || SourceSuffix("youtube", " ") != "" {
		t.Fatal("SourceSuffix must need both provider and ID")
	}
	name := WithSourceSuffix("01 - Song.opus", suffix)
	if name != "01 - Song"+suffix+".opus" {
		t.Fatalf("WithSourceSuffix = %q", name)
	}
	got, ok := SplitSourceSuffix(name)
	if !ok || got != suffix {
		t.Fatalf("SplitSourceSuffix(%q) = %q, %v", name, got, ok)
	}
	if _, ok := SplitSourceSuffix("01 - Song [source-xyz].opus"); ok {
		t.Fatal("a malformed marker was recognised")
	}

	long := WithSourceSuffix(strings.Repeat("？", 60)+".opus", suffix)
	if len(long) > MaxComponentLength || !utf8.ValidString(long) || !strings.HasSuffix(long, suffix+".opus") {
		t.Fatalf("long suffixed name %q", long)
	}
}

// TestFullwidthNamesOnDisk writes a release with every look-alike into a real
// directory, so the names are known to be accepted by the filesystem the tests
// run on.
func TestFullwidthNamesOnDisk(t *testing.T) {
	lib, err := NewLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := music.Release{AlbumArtist: `AC/DC\`, Title: `Who? *Live* "at" <Home>|`, Year: 1991}
	track := music.Track{Title: "Intro: Part 1/2", TrackNumber: 1, DiscNumber: 2, DiscTotal: 2}
	dir, err := lib.EnsureReleaseDir(release)
	if err != nil {
		t.Fatal(err)
	}
	path, err := lib.TrackPath(release, track, ".opus")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("track %q is not inside %q", path, dir)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "2-01 - Intro： Part 1／2.opus" {
		t.Fatalf("directory holds %v (%v)", entries, err)
	}
}

func TestReleaseDirLocksExcludeSharedHolders(t *testing.T) {
	lib, err := NewLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlockShared := lib.ShareReleaseDir("A/B")
	unlockShared2 := lib.ShareReleaseDir("A/B") // shared holders do not block each other

	acquired := make(chan struct{})
	released := make(chan struct{})
	go func() {
		defer close(released)
		unlock := lib.LockReleaseDirs("A/C", "A/B", "A/B")
		close(acquired)
		unlock()
	}()
	select {
	case <-acquired:
		t.Fatal("exclusive lock acquired while the directory was shared")
	case <-time.After(50 * time.Millisecond):
	}
	unlockShared()
	unlockShared2()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive lock never acquired")
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				lib.LockReleaseDirs("X", "Y")()
			} else {
				lib.LockReleaseDirs("Y", "X")()
			}
		}(i)
	}
	wg.Wait()
	// The first goroutine signals acquired before it unlocks. Without waiting
	// for it here, its two locks may still be held when the map is counted,
	// and the count would read the map while that goroutine deletes from it.
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive lock never released")
	}
	lib.dirLocks.mu.Lock()
	n := len(lib.dirLocks.locks)
	lib.dirLocks.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d idle locks retained", n)
	}
}
