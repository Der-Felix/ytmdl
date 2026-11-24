package deezer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ytdm/backend/internal/apperr"
)

// Artist genres. Deezer has no genres on an artist; every album carries one
// genre_id, also in the artist's album list. An artist's genres are the most
// frequent album genres, so one or two list pages and a cached id to name
// table answer it without a request per album.

const (
	// genreLanguage pins the language of genre names (see client.attempt).
	// German matches the application's interface.
	genreLanguage = "de"
	// maxArtistGenres is how many genres an artist gets at most.
	maxArtistGenres = 3
	// artistGenrePages bounds the album list pages read per artist; 200
	// albums are plenty to tell the main genres.
	artistGenrePages = 2
)

// genreUnknown (-1) marks an album without a genre; genreAll (0) is the
// "All" entry of the genre list. Neither is a genre of an artist.
const (
	genreUnknown = -1
	genreAll     = 0
)

type apiGenre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type apiGenreList struct {
	Data []apiGenre `json:"data"`
}

// apiAlbumGenre is the part of an album list item genre derivation reads.
type apiAlbumGenre struct {
	ID      int64 `json:"id"`
	GenreID int64 `json:"genre_id"`
}

type apiAlbumGenreList struct {
	Data []apiAlbumGenre `json:"data"`
	Next string          `json:"next"`
}

// genreNames caches Deezer's id to name table for the life of the process.
type genreNames struct {
	mu    sync.Mutex
	names map[int64]string
	full  bool
}

// ArtistGenres returns up to three genres of a Deezer artist, most frequent
// first, derived from the genres of the artist's albums.
func (p *Provider) ArtistGenres(ctx context.Context, artistID string) ([]string, error) {
	if err := validateID(artistID); err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	next := fmt.Sprintf("/artist/%s/albums?limit=100", artistID)
	visited := map[string]bool{}
	for page := 0; page < artistGenrePages && next != "" && !visited[next]; page++ {
		visited[next] = true
		var list apiAlbumGenreList
		if err := p.client.get(ctx, next, &list); err != nil {
			if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
				return nil, apperr.Newf(apperr.CodeArtistNotFound, "Deezer artist %q not found", artistID)
			}
			return nil, err
		}
		for _, album := range list.Data {
			counts[album.GenreID]++
		}
		if len(list.Data) == 0 {
			break
		}
		next = list.Next
	}
	ids := deriveGenreIDs(counts)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		name, err := p.genreName(ctx, id)
		if err != nil {
			return nil, err
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// AlbumArtist returns the main artist of a Deezer album: the way to an
// artist's Deezer id when the library only knows one of its albums.
func (p *Provider) AlbumArtist(ctx context.Context, albumID string) (id, name string, err error) {
	if err := validateID(albumID); err != nil {
		return "", "", err
	}
	var album apiAlbum
	if err := p.client.get(ctx, "/album/"+albumID, &album); err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderNotFound {
			return "", "", apperr.Newf(apperr.CodeReleaseNotFound, "Deezer release %q not found", albumID)
		}
		return "", "", err
	}
	if album.Artist == nil || album.Artist.ID <= 0 {
		return "", "", nil
	}
	return strconv.FormatInt(album.Artist.ID, 10), strings.TrimSpace(album.Artist.Name), nil
}

// deriveGenreIDs ranks album genres by frequency (ties by id, so the order is
// deterministic) and keeps up to maxArtistGenres that reach a quarter of the
// most frequent one: a soundtrack or two do not make a dance act a film
// composer. Unknown (-1) and "All" (0) never count.
func deriveGenreIDs(counts map[int64]int) []int64 {
	ids := make([]int64, 0, len(counts))
	for id, n := range counts {
		if id > genreAll && n > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if counts[ids[i]] != counts[ids[j]] {
			return counts[ids[i]] > counts[ids[j]]
		}
		return ids[i] < ids[j]
	})
	out := []int64{}
	for _, id := range ids {
		if len(out) == maxArtistGenres || counts[id]*4 < counts[ids[0]] {
			break
		}
		out = append(out, id)
	}
	return out
}

// genreName resolves a genre id. The list endpoint is read once; ids it does
// not contain (Deezer uses some, such as 522 "Singer & Songwriter") are asked
// for one by one and cached, an unknown one as "".
func (p *Provider) genreName(ctx context.Context, id int64) (string, error) {
	g := &p.genres
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.names == nil {
		g.names = map[int64]string{}
	}
	if name, ok := g.names[id]; ok {
		return name, nil
	}
	if !g.full {
		var list apiGenreList
		if err := p.client.get(ctx, "/genre", &list); err != nil {
			return "", err
		}
		for _, genre := range list.Data {
			g.names[genre.ID] = strings.TrimSpace(genre.Name)
		}
		g.full = true
		if name, ok := g.names[id]; ok {
			return name, nil
		}
	}
	var one apiGenre
	if err := p.client.get(ctx, fmt.Sprintf("/genre/%d", id), &one); err != nil {
		// Deezer answers an id it does not know with an error object; the
		// genre is then left out rather than failing the artist.
		if code := apperr.CodeOf(err); code == apperr.CodeProviderNotFound || code == apperr.CodeInvalidRequest {
			g.names[id] = ""
			return "", nil
		}
		return "", err
	}
	g.names[id] = strings.TrimSpace(one.Name)
	return g.names[id], nil
}
