package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"ytdm/backend/internal/music"
)

func TestArtistGenresPersistFilterAndManualClear(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()
	var artists []music.Artist
	for i, name := range []string{"Alpha", "Beta", "Gamma"} {
		a, err := c.UpsertArtist(ctx, music.Artist{Name: name, Provider: "ytmusic", SourceID: "artist:" + name})
		if err != nil {
			t.Fatal(err)
		}
		artists = append(artists, a)
		r, err := c.UpsertRelease(ctx, music.Release{Title: name, Provider: "ytmusic", SourceID: name, ReleaseType: music.ReleaseAlbum}, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.UpsertTrack(ctx, music.Track{Title: name, DurationMS: 120000 + i}, r.ID, a.ID, 1000)
		if err != nil {
			t.Fatal(err)
		}
	}
	genres, err := c.SetArtistGenres(ctx, artists[0].ID, []string{" Hip-Hop ", "hip-hop", "100%_Pop"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(genres, []string{"Hip-Hop", "100%_Pop"}) {
		t.Fatal(genres)
	}
	if _, err = c.SetArtistGenres(ctx, artists[1].ID, []string{"Hip-Hop"}); err != nil {
		t.Fatal(err)
	}
	a, err := c.GetArtist(ctx, artists[0].ID)
	if err != nil || !reflect.DeepEqual(a.Genres, genres) {
		t.Fatalf("genres not persisted: %v %v", a, err)
	}
	list, total, err := c.ListArtistsFiltered(ctx, ArtistListFilter{Genre: "hip-hop", Limit: 1, Offset: 1})
	if err != nil || total != 2 || len(list) != 1 || len(list[0].Genres) == 0 {
		t.Fatalf("pagination: %v %d %v", list, total, err)
	}
	_, total, err = c.ListReleasesFiltered(ctx, ReleaseListFilter{Genre: "HIP-HOP"})
	if err != nil || total != 2 {
		t.Fatalf("releases: %d %v", total, err)
	}
	_, total, err = c.ListTracksFiltered(ctx, TrackListFilter{Genre: "100%_Pop"})
	if err != nil || total != 1 {
		t.Fatalf("literal tracks: %d %v", total, err)
	}
	_, total, err = c.ListTracksFiltered(ctx, TrackListFilter{GenreMissing: true})
	if err != nil || total != 1 {
		t.Fatalf("unknown tracks: %d %v", total, err)
	}
	_, total, err = c.ListReleasesFiltered(ctx, ReleaseListFilter{GenreMissing: true})
	if err != nil || total != 1 {
		t.Fatalf("unknown releases: %d %v", total, err)
	}
	_, total, err = c.ListArtistsFiltered(ctx, ArtistListFilter{GenreMissing: true})
	if err != nil || total != 1 {
		t.Fatalf("unknown artists: %d %v", total, err)
	}
	if _, _, err = c.ListArtistsFiltered(ctx, ArtistListFilter{Genre: "Hip-Hop", GenreMissing: true}); err == nil {
		t.Fatal("contradictory filter accepted")
	}
	if _, err = c.SetArtistGenres(ctx, artists[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	seed := artists[0]
	seed.Genres = []string{"Provider Genre"}
	updated, err := c.UpsertArtist(ctx, seed)
	if err != nil || len(updated.Genres) != 0 {
		t.Fatalf("manual clear overwritten: %v %v", updated, err)
	}
	seed = artists[2]
	seed.Genres = []string{"Provider Genre"}
	updated, err = c.UpsertArtist(ctx, seed)
	if err != nil || len(updated.Genres) != 1 {
		t.Fatalf("provider genres not seeded: %v %v", updated, err)
	}
	available, err := c.ListGenres(ctx)
	if err != nil || !reflect.DeepEqual(available, []string{"Hip-Hop", "Provider Genre"}) {
		t.Fatalf("options: %v %v", available, err)
	}
	if _, err = c.SetArtistGenres(ctx, "missing", nil); err == nil {
		t.Fatal("unknown artist accepted")
	}
	if _, err = db.ExecContext(ctx, `UPDATE artists SET genres_json = '[1]' WHERE id = $1`, artists[0].ID); err == nil {
		t.Fatal("non-string genre accepted")
	}
}

func TestArtistGenreValidation(t *testing.T) {
	for _, v := range [][]string{make([]string, 21), {strings.Repeat("ä", 81)}, {"Pop\nRock"}} {
		if _, err := NormalizeGenres(v); err == nil {
			t.Fatal("invalid genres accepted")
		}
	}
}

func TestArtistMergePreservesGenres(t *testing.T) {
	c := NewCatalog(openTestDB(t))
	ctx := context.Background()
	a, err := c.UpsertArtist(ctx, music.Artist{Name: "Merged Artist", Provider: "ytmusic", SourceID: "artist:first"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.UpsertArtist(ctx, music.Artist{Name: "Duplicate Name", Provider: "ytmusic", SourceID: "artist:second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.SetArtistGenres(ctx, a.ID, []string{"Pop"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.SetArtistGenres(ctx, b.ID, []string{"pop", "Rock"}); err != nil {
		t.Fatal(err)
	}
	if err = c.MergeArtists(ctx, a.ID, []string{b.ID}); err != nil {
		t.Fatal(err)
	}
	artist, err := c.GetArtist(ctx, a.ID)
	if err != nil || !reflect.DeepEqual(artist.Genres, []string{"Pop", "Rock"}) {
		t.Fatalf("merge: %v %v", artist, err)
	}
}
