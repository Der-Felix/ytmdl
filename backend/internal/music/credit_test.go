package music_test

import (
	"reflect"
	"testing"

	"ytdm/backend/internal/music"
)

// TestSplitCreditNeverInventsArtists guards the property that matters: the
// splitter is only ever consulted for unstructured text, and its output is
// only ever used when structured data confirms it.
func TestSplitCredit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"LACAZETTE & Bushido", []string{"LACAZETTE", "Bushido"}},
		{"LACAZETTE, Samra & Venti", []string{"LACAZETTE", "Samra", "Venti"}},
		{"Artist A feat. Artist B", []string{"Artist A", "Artist B"}},
		{"Artist A ft. Artist B", []string{"Artist A", "Artist B"}},
		{"Artist A featuring Artist B", []string{"Artist A", "Artist B"}},
		{"Daft Punk", []string{"Daft Punk"}},
		{"", nil},
		{"   ", nil},
	}
	for _, c := range cases {
		if got := music.SplitCredit(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitCredit(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSplitFeaturingLeavesAmbiguousSeparatorsAlone(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Simon & Garfunkel", []string{"Simon & Garfunkel"}},
		{"Earth, Wind & Fire", []string{"Earth, Wind & Fire"}},
		{"Hall & Oates", []string{"Hall & Oates"}},
		{"AC/DC", []string{"AC/DC"}},
		{"Tyler, The Creator", []string{"Tyler, The Creator"}},
		{"Calvin Harris feat. Rihanna", []string{"Calvin Harris", "Rihanna"}},
		{"Calvin Harris ft. Rihanna", []string{"Calvin Harris", "Rihanna"}},
	}
	for _, c := range cases {
		if got := music.SplitFeaturing(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitFeaturing(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestResolveAlbumArtistKeepsLegitimateNames is the regression test for the
// band names that contain a separator. None of them may ever be reduced.
func TestResolveAlbumArtistKeepsLegitimateNames(t *testing.T) {
	names := []string{
		"Simon & Garfunkel",
		"Earth, Wind & Fire",
		"Hall & Oates",
		"AC/DC",
		"Tyler, The Creator",
		"Emerson, Lake & Palmer",
		"Crosby, Stills & Nash",
		"Above & Beyond",
		"Florence + the Machine",
	}
	for _, name := range names {
		// A provider that reports the name both as the release artist and as
		// the single structured credit — the normal, structured case.
		if got := music.ResolveAlbumArtist(name, []string{name}); got != name {
			t.Errorf("ResolveAlbumArtist(%q, [%q]) = %q, want the name unchanged", name, name, got)
		}
		// And with no credit list at all, so nothing can corroborate a split.
		if got := music.ResolveAlbumArtist(name, nil); got != name {
			t.Errorf("ResolveAlbumArtist(%q, nil) = %q, want the name unchanged", name, got)
		}
	}
}

func TestResolveAlbumArtistPrefersStructuredProviderData(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		credits  []string
		want     string
	}{
		{
			name:     "structured release artist wins over the credit order",
			provider: "LACAZETTE",
			credits:  []string{"LACAZETTE", "Gangsta Ralph", "Sido"},
			want:     "LACAZETTE",
		},
		{
			name:     "structured release artist that is credited second still wins",
			provider: "LACAZETTE",
			credits:  []string{"AVIE", "LACAZETTE"},
			want:     "LACAZETTE",
		},
		{
			name:     "joined display string corroborated by the credit list is reduced",
			provider: "LACAZETTE & Bushido",
			credits:  []string{"LACAZETTE", "Bushido"},
			want:     "LACAZETTE",
		},
		{
			name:     "joined display string that the credits do not corroborate stays whole",
			provider: "Simon & Garfunkel",
			credits:  []string{"Simon & Garfunkel", "Art Garfunkel"},
			want:     "Simon & Garfunkel",
		},
		{
			name:     "explicit featuring marker is unambiguous without corroboration",
			provider: "Calvin Harris feat. Rihanna",
			credits:  nil,
			want:     "Calvin Harris",
		},
		{
			name:     "no release artist falls back to the first structured credit",
			provider: "",
			credits:  []string{"Blue Swede", "Raspberries"},
			want:     "Blue Swede",
		},
		{
			name:     "nothing at all",
			provider: "",
			credits:  nil,
			want:     music.UnknownArtist,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := music.ResolveAlbumArtist(c.provider, c.credits); got != c.want {
				t.Errorf("ResolveAlbumArtist(%q, %v) = %q, want %q", c.provider, c.credits, got, c.want)
			}
		})
	}
}

// TestResolveAlbumArtistFeaturedArtistNeverWins is the case from the brief:
// the track credits Rihanna, the release does not.
func TestResolveAlbumArtistFeaturedArtistNeverWins(t *testing.T) {
	got := music.ResolveAlbumArtist("Calvin Harris", []string{"Calvin Harris", "Rihanna"})
	if got != "Calvin Harris" {
		t.Fatalf("ResolveAlbumArtist = %q, want Calvin Harris", got)
	}
}

func TestNormalizeCredits(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"Calvin Harris", "Rihanna"}, []string{"Calvin Harris", "Rihanna"}},
		{[]string{"Calvin Harris feat. Rihanna"}, []string{"Calvin Harris", "Rihanna"}},
		{[]string{"Simon & Garfunkel"}, []string{"Simon & Garfunkel"}},
		{[]string{"A", "a", " A "}, []string{"A"}},
		{[]string{"", "   "}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		if got := music.NormalizeCredits(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizeCredits(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
