package deezer

import (
	"reflect"
	"testing"
)

// TestOwnArtistIDs pins what Deezer says about who a release belongs to: the
// album's own artist, plus the main contributors a joined album artist names.
// Deezer marks a guest such as Bebe Rexha on David Guetta's "I'm Good (Blue)"
// as a main contributor too, so the contributor list alone is not ownership.
func TestOwnArtistIDs(t *testing.T) {
	tests := []struct {
		name         string
		artist       *apiArtistRef
		contributors []apiContributor
		want         []string
	}{
		{
			name:   "artist album listing without an artist",
			artist: nil,
			want:   nil,
		},
		{
			name:   "guest marked as main contributor",
			artist: &apiArtistRef{ID: 542, Name: "David Guetta"},
			contributors: []apiContributor{
				{ID: 542, Name: "David Guetta", Role: "Main"},
				{ID: 4492087, Name: "Bebe Rexha", Role: "Main"},
			},
			want: []string{"542"},
		},
		{
			name:   "joined album artist",
			artist: &apiArtistRef{ID: 900, Name: "LACAZETTE & Bushido"},
			contributors: []apiContributor{
				{ID: 901, Name: "LACAZETTE", Role: "Main"},
				{ID: 902, Name: "Bushido", Role: "Main"},
				{ID: 903, Name: "Guest", Role: "Featured"},
			},
			want: []string{"900", "901", "902"},
		},
		{
			name:   "band name with an ampersand and no matching contributors",
			artist: &apiArtistRef{ID: 77, Name: "Simon & Garfunkel"},
			contributors: []apiContributor{
				{ID: 77, Name: "Simon & Garfunkel", Role: "Main"},
			},
			want: []string{"77"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownArtistIDs(tc.artist, tc.contributors); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ownArtistIDs = %v, want %v", got, tc.want)
			}
		})
	}
}
