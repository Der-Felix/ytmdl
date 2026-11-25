package music

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLibraryTrackJSONCarriesArtistID(t *testing.T) {
	track := LibraryTrack{Track: Track{ID: "t1", Title: "Song", ReleaseID: "r1"}, ArtistID: "a1"}
	raw, err := json.Marshal(track)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["artist_id"] != "a1" {
		t.Fatalf("artist_id = %v, want a1 (json %s)", got["artist_id"], raw)
	}
	if got["release_id"] != "r1" || got["id"] != "t1" {
		t.Fatalf("embedded track fields missing: %s", raw)
	}
}

func TestLibraryTrackJSONOmitsEmptyArtistID(t *testing.T) {
	raw, err := json.Marshal(LibraryTrack{Track: Track{ID: "t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "artist_id") {
		t.Fatalf("empty artist id must be left out: %s", raw)
	}
}
