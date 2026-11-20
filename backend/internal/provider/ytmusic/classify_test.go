package ytmusic

import (
	"testing"

	"ytdm/backend/internal/music"
)

// TestClassifyRelease pins the order every metadata provider shares: a
// declared compilation first, then live and remix markers in the title, then
// the type the subtitle names.
func TestClassifyRelease(t *testing.T) {
	tests := []struct {
		subtitle, title string
		want            music.ReleaseType
	}{
		{"Album • 2013", "Random Access Memories", music.ReleaseAlbum},
		{"Single • 2022", "I'm Good (Blue)", music.ReleaseSingle},
		{"Single • 2022", "I'm Good (Blue) (Oliver Heldens Remix)", music.ReleaseRemix},
		{"EP • 2023", "Say My Name (Remixes)", music.ReleaseRemix},
		{"Single • 2018", "Meant To Be (Live From CMA Fest 2018)", music.ReleaseLive},
		{"Single • 2019", "Heart Still Beating (Extended)", music.ReleaseSingle},
		{"Single • 2010", "Premixed Feelings", music.ReleaseSingle},
		{"Album • 2001", "Long Live", music.ReleaseAlbum},
		{"Compilation • 2020", "Dance Hits (The Remixes)", music.ReleaseCompilation},
		{"2020", "Night Moves EP", music.ReleaseEP},
	}
	for _, tc := range tests {
		if got := classifyRelease(tc.subtitle, tc.title); got != tc.want {
			t.Errorf("classifyRelease(%q, %q) = %q, want %q", tc.subtitle, tc.title, got, tc.want)
		}
	}
}
