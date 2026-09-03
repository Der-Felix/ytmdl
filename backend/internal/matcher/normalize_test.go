package matcher

import "testing"

func TestAnalyzeBaseTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Song", "song"},
		{"case and space", "  SoNg   Title ", "song title"},
		{"punctuation", "Don't Stop Me Now!", "don t stop me now"},
		{"diacritics", "Über den Wolken", "uber den wolken"},
		{"typographic quotes", "It’s My Life", "it s my life"},
		{"feat bracket", "Song (feat. Other Artist)", "song"},
		{"feat inline", "Song feat. Other Artist", "song"},
		{"ft inline", "Song ft. Other", "song"},
		{"official video noise", "Song (Official Music Video)", "song"},
		{"remaster noise", "Song - 2011 Remaster", "song"},
		{"remaster bracket", "Song (Remastered 2009)", "song"},
		{"album version noise", "Song (Album Version)", "song"},
		{"keeps meaningful bracket", "Song (Part 2)", "song part 2"},
		{"keeps dash title", "Song - Other Words", "song other words"},
		{"ampersand", "Rock & Roll", "rock roll"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Analyze(tc.in).Base; got != tc.want {
				t.Fatalf("Analyze(%q).Base = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAnalyzeVersions(t *testing.T) {
	tests := []struct {
		in       string
		wantBase string
		wantVer  string
	}{
		{"Song", "song", ""},
		{"Song (Live)", "song", "live"},
		{"Song - Live at Wembley", "song", "live"},
		{"Song (Instrumental)", "song", "instrumental"},
		{"Song (Acoustic Version)", "song", "acoustic"},
		{"Song (Tiesto Remix)", "song", "remix"},
		{"Song Tiesto Remix", "song", "remix"},
		{"Song sped up", "song", "sped_up"},
		{"Song (Sped Up)", "song", "sped_up"},
		{"Song slowed + reverb", "song", "slowed"},
		{"Song (Karaoke Version)", "song", "karaoke"},
		{"Song (Radio Edit)", "song", "radio_edit"},
		{"Song (Live Acoustic)", "song", "live+acoustic"},
		// A version word inside the actual title must not be stripped.
		{"Long Live", "long live", ""},
		{"Live Forever", "live forever", ""},
		{"The Remix Artist", "the remix artist", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := Analyze(tc.in)
			if got.Base != tc.wantBase {
				t.Errorf("base = %q, want %q", got.Base, tc.wantBase)
			}
			if got.Versions.String() != tc.wantVer {
				t.Errorf("versions = %q, want %q", got.Versions.String(), tc.wantVer)
			}
		})
	}
}

func TestAnalyzeFeatured(t *testing.T) {
	got := Analyze("Song (feat. Alice & Bob)")
	if got.Base != "song" {
		t.Fatalf("base = %q, want %q", got.Base, "song")
	}
	want := []string{"alice", "bob"}
	if len(got.Featured) != len(want) {
		t.Fatalf("featured = %v, want %v", got.Featured, want)
	}
	for i := range want {
		if got.Featured[i] != want[i] {
			t.Fatalf("featured = %v, want %v", got.Featured, want)
		}
	}
}

func TestNormalizeArtist(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Beatles", "beatles"},
		{"BEATLES", "beatles"},
		{"Mötley Crüe", "motley crue"},
		{"AC/DC", "ac dc"},
		{"  Sigur Rós ", "sigur ros"},
	}
	for _, tc := range tests {
		if got := NormalizeArtist(tc.in); got != tc.want {
			t.Errorf("NormalizeArtist(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeArtistsIsOrderIndependent(t *testing.T) {
	a := NormalizeArtists([]string{"Alice", "Bob"})
	b := NormalizeArtists([]string{"Bob", "Alice"})
	if a != b {
		t.Fatalf("order changed the key: %q != %q", a, b)
	}
	if a == "" {
		t.Fatal("key must not be empty")
	}
}

func TestSimilarity(t *testing.T) {
	if got := Similarity("song", "song"); got != 1 {
		t.Errorf("identical strings scored %v", got)
	}
	if got := Similarity("", ""); got != 0 {
		t.Errorf("empty strings scored %v, want 0", got)
	}
	near := Similarity("dont stop me now", "don t stop me now")
	if near < 0.8 {
		t.Errorf("near identical strings scored %v, want >= 0.8", near)
	}
	far := Similarity("song", "completely different title")
	if far > 0.3 {
		t.Errorf("unrelated strings scored %v, want <= 0.3", far)
	}
}
