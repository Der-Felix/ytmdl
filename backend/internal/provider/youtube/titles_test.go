package youtube

import (
	"strings"
	"testing"
)

func TestSplitArtists(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Artist", []string{"Artist"}},
		{"Artist, Other", []string{"Artist", "Other"}},
		// "&" is part of many band names and is therefore left alone; the
		// matcher compares the whole credit as well as the single names.
		{"Artist & Other", []string{"Artist & Other"}},
		{"Artist feat. Guest", []string{"Artist", "Guest"}},
		{"Artist ft. Guest", []string{"Artist", "Guest"}},
		{"Artist · Other", []string{"Artist", "Other"}},
		{"  ", nil},
		{"", nil},
	}
	for _, tc := range tests {
		got := splitArtists(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitArtists(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitArtists(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestSplitArtistTitle(t *testing.T) {
	tests := []struct {
		in         string
		wantArtist string
		wantTitle  string
		wantOK     bool
	}{
		{"Artist - Song", "Artist", "Song", true},
		{"Artist – Song", "Artist", "Song", true},
		{"Artist - Song - Live", "Artist", "Song - Live", true},
		{"Artist | Song", "Artist", "Song", true},
		{"JustATitle", "", "JustATitle", false},
		{"- Song", "", "- Song", false},
	}
	for _, tc := range tests {
		artist, title, ok := splitArtistTitle(tc.in)
		if ok != tc.wantOK || artist != tc.wantArtist || title != tc.wantTitle {
			t.Errorf("splitArtistTitle(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, artist, title, ok, tc.wantArtist, tc.wantTitle, tc.wantOK)
		}
	}
}

func TestStripTopicSuffix(t *testing.T) {
	if got := StripTopicSuffix("Artist - Topic"); got != "Artist" {
		t.Errorf("got %q, want %q", got, "Artist")
	}
	if got := StripTopicSuffix("Artist"); got != "Artist" {
		t.Errorf("got %q, want %q", got, "Artist")
	}
}

func TestValidateMediaURL(t *testing.T) {
	valid := []string{
		"https://www.youtube.com/watch?v=abc",
		"https://music.youtube.com/watch?v=abc",
		"https://youtu.be/abc",
		"https://youtube-nocookie.com/embed/abc",
	}
	for _, raw := range valid {
		if err := ValidateMediaURL(raw); err != nil {
			t.Errorf("ValidateMediaURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{
		"", "not a url", "ftp://youtube.com/x",
		"https://evil.test/watch?v=abc",
		"https://127.0.0.1/watch?v=abc",
		"https://youtube.com.evil.test/watch?v=abc",
		"file:///etc/passwd",
	}
	for _, raw := range invalid {
		if err := ValidateMediaURL(raw); err == nil {
			t.Errorf("ValidateMediaURL(%q) was accepted", raw)
		}
	}
}

func TestMusicSearchURLEscapesQuery(t *testing.T) {
	got := musicSearchURL("artist & song / test")
	if strings.Contains(got, " ") {
		t.Errorf("query was not escaped: %q", got)
	}
	if !strings.HasPrefix(got, "https://music.youtube.com/search?q=") {
		t.Errorf("unexpected search URL: %q", got)
	}
	if err := ValidateMediaURL(got); err != nil {
		t.Errorf("the search URL is not accepted by the validator: %v", err)
	}
}

func TestNewRequiresClient(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected an error without a yt-dlp client")
	}
}

func TestIsYouTubeVideoID(t *testing.T) {
	valid := []string{
		"Jhm1qqF79E0",
		"Ii6cQsbBpnU",
		"luxCEnkpTmI",
		"dQw4w9WgXcQ",
		"abc_123-XYZ",
	}
	for _, id := range valid {
		if !isYouTubeVideoID(id) {
			t.Errorf("isYouTubeVideoID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",
		"short",
		"toolong123456789",
		"UCt5yj95-pWrcRIkay4kOh8A", // channel ID
		"MPREb_DU7ozAp3WFt",        // browse ID
		"invalid!@#$",
		" ",
	}
	for _, id := range invalid {
		if isYouTubeVideoID(id) {
			t.Errorf("isYouTubeVideoID(%q) = true, want false", id)
		}
	}
}

func TestIsChannelOrPlaylistURL(t *testing.T) {
	channelURLs := []string{
		"https://www.youtube.com/channel/UCt5yj95-pWrcRIkay4kOh8A",
		"https://music.youtube.com/channel/UCt5yj95-pWrcRIkay4kOh8A",
		"https://www.youtube.com/@LACAZETTE",
		"https://www.youtube.com/c/ArtistName",
		"https://www.youtube.com/user/ArtistName",
		"https://www.youtube.com/playlist?list=PL1234567890",
		"https://music.youtube.com/playlist?list=OLAK5uy_k3",
		"https://www.youtube.com/watch?v=Jhm1qqF79E0&list=RDJhm1qqF79E0",
	}
	for _, u := range channelURLs {
		if !isChannelOrPlaylistURL(u) {
			t.Errorf("isChannelOrPlaylistURL(%q) = false, want true", u)
		}
	}

	trackURLs := []string{
		"https://www.youtube.com/watch?v=Jhm1qqF79E0",
		"https://music.youtube.com/watch?v=Jhm1qqF79E0",
		"https://youtu.be/Jhm1qqF79E0",
	}
	for _, u := range trackURLs {
		if isChannelOrPlaylistURL(u) {
			t.Errorf("isChannelOrPlaylistURL(%q) = true, want false", u)
		}
	}
}

func TestIsWatchURL(t *testing.T) {
	validWatch := []string{
		"https://www.youtube.com/watch?v=Jhm1qqF79E0",
		"https://music.youtube.com/watch?v=Jhm1qqF79E0",
		"https://youtu.be/Jhm1qqF79E0",
	}
	for _, u := range validWatch {
		if !isWatchURL(u) {
			t.Errorf("isWatchURL(%q) = false, want true", u)
		}
	}

	invalidWatch := []string{
		"https://www.youtube.com/channel/UCt5yj95-pWrcRIkay4kOh8A",
		"https://music.youtube.com/channel/UCt5yj95-pWrcRIkay4kOh8A",
		"https://www.youtube.com/playlist?list=PL1234567890",
		"https://music.youtube.com/search?q=query",
		"https://evil.test/watch?v=abc",
	}
	for _, u := range invalidWatch {
		if isWatchURL(u) {
			t.Errorf("isWatchURL(%q) = true, want false", u)
		}
	}
}
