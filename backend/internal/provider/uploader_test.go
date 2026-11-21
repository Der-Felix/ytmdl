package provider

import "testing"

func TestUploaderNamesArtist(t *testing.T) {
	cases := []struct {
		name, artist, title, uploader string
		wantArtist, wantTitle         string
	}{
		{"title first, own channel", "Løve", "FiLow", "FiLow", "FiLow", "Løve"},
		{"case differs", "Løve", "filow", "FiLow", "filow", "Løve"},
		{"usual order", "FiLow", "Løve", "FiLow", "FiLow", "Løve"},
		{"third party channel", "Producer", "Beat Track", "Reposter", "Producer", "Beat Track"},
		{"self titled", "FiLow", "FiLow", "FiLow", "FiLow", "FiLow"},
		{"no uploader", "Løve", "FiLow", "", "Løve", "FiLow"},
	}
	for _, tc := range cases {
		artist, title := UploaderNamesArtist(tc.artist, tc.title, tc.uploader)
		if artist != tc.wantArtist || title != tc.wantTitle {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, artist, title, tc.wantArtist, tc.wantTitle)
		}
	}
}
