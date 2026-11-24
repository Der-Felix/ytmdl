package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Provider genres for artists already in the library. A download stores its
// artist under a synthetic source without genres, so genres come from a
// separate lookup at the metadata provider (see package artistgenres).

// seedArtistGenres stores provider genres unless the artist already has
// genres or an administrator set them by hand (genres_manual, even an empty
// list). It reports whether the row changed.
func seedArtistGenres(ctx context.Context, exec executor, artistID string, genres []string) (bool, error) {
	// Provider genre metadata is optional; invalid entries are dropped rather
	// than failing the caller. Manual writes are validated strictly instead.
	normalized, _ := NormalizeGenres(genres[:min(len(genres), 20)])
	if len(normalized) == 0 {
		return false, nil
	}
	result, err := exec.ExecContext(ctx, `UPDATE artists SET genres_json = $1::jsonb WHERE id = $2 AND NOT genres_manual AND genres_json = '[]'::jsonb`, encodeStrings(normalized), artistID)
	if err != nil {
		return false, wrapDB("seed artist genres", err)
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// GenreCandidate is a library artist without genres and the provider ids
// that may lead to them: the provider's artist id, or one of the artist's
// albums at that provider when only those are known.
type GenreCandidate struct {
	ArtistID       string
	Name           string
	SourceArtistID string
	SourceAlbumID  string
}

// GenreRecheck says when a recorded lookup is due again: NoGenres after the
// provider answered without genres, Failed after it answered with an error.
type GenreRecheck struct {
	NoGenres time.Duration
	Failed   time.Duration
}

// genreLookupDue is the SQL condition for an artist whose lookup (l) is due;
// the two placeholders are the NoGenres and Failed rechecks in seconds.
const genreLookupDue = `(l.artist_id IS NULL OR (NOT l.failed AND l.checked_at < now()-make_interval(secs => %s)) OR (l.failed AND l.checked_at < now()-make_interval(secs => %s)))`

// ArtistsMissingGenres lists library artists with tracks, no genres and no
// manual choice that have an artist or album id at provider, and that were
// not looked up within the recheck. Ordered by id, so successive batches advance.
func (c *Catalog) ArtistsMissingGenres(ctx context.Context, provider string, recheck GenreRecheck, limit int) ([]GenreCandidate, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id,name,artist_source,album_source FROM (
 SELECT a.id,a.name,
  COALESCE(
   (SELECT s.source_id FROM artist_sources s WHERE s.artist_id=a.id AND s.provider=$1 AND s.source_id ~ '^[0-9]{1,20}$' ORDER BY s.is_primary DESC,s.source_id LIMIT 1),
   CASE WHEN a.provider=$1 AND a.source_id ~ '^[0-9]{1,20}$' THEN a.source_id END,
   (SELECT sub.artist_source_id FROM artist_subscriptions sub WHERE sub.provider=$1 AND lower(sub.artist_name)=lower(a.name) AND sub.artist_source_id ~ '^[0-9]{1,20}$' ORDER BY sub.artist_source_id LIMIT 1),
   '') AS artist_source,
  COALESCE((SELECT r.source_id FROM releases r WHERE r.artist_id=a.id AND r.provider=$1 AND r.source_id ~ '^[0-9]{1,20}$' ORDER BY r.created_at,r.id LIMIT 1),'') AS album_source
 FROM artists a LEFT JOIN artist_genre_lookups l ON l.artist_id=a.id
 WHERE a.genres_json='[]'::jsonb AND NOT a.genres_manual
  AND `+fmt.Sprintf(genreLookupDue, "$2", "$4")+`
  AND EXISTS(SELECT 1 FROM tracks t WHERE t.artist_id=a.id)
 ) x WHERE artist_source<>'' OR album_source<>'' ORDER BY id LIMIT $3`, provider, recheck.NoGenres.Seconds(), limit, recheck.Failed.Seconds())
	if err != nil {
		return nil, wrapDB("artists missing genres", err)
	}
	defer rows.Close()
	out := []GenreCandidate{}
	for rows.Next() {
		var g GenreCandidate
		if err := rows.Scan(&g.ArtistID, &g.Name, &g.SourceArtistID, &g.SourceAlbumID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// StoreArtistGenreLookup records a lookup and stores the genres found, in
// one transaction. Manual or existing genres are never overwritten.
func (c *Catalog) StoreArtistGenreLookup(ctx context.Context, artistID string, genres []string) error {
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := seedArtistGenres(ctx, tx, artistID, genres); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO artist_genre_lookups(artist_id,checked_at,found,failed) SELECT id,now(),$2,false FROM artists WHERE id=$1
 ON CONFLICT(artist_id) DO UPDATE SET checked_at=now(),found=EXCLUDED.found,failed=false`, artistID, len(genres) > 0)
		if err != nil {
			return wrapDB("record artist genre lookup", err)
		}
		return nil
	})
}

// RecordArtistGenreFailure records that the provider failed on an artist, so
// it is asked again only after the Failed recheck. A found lookup is kept.
func (c *Catalog) RecordArtistGenreFailure(ctx context.Context, artistID string) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO artist_genre_lookups(artist_id,checked_at,found,failed) SELECT id,now(),false,true FROM artists WHERE id=$1
 ON CONFLICT(artist_id) DO UPDATE SET checked_at=now(),failed=true WHERE NOT artist_genre_lookups.found`, artistID)
	if err != nil {
		return wrapDB("record artist genre failure", err)
	}
	return nil
}

// ArtistMissingGenresBySource finds the library artist a provider artist
// stands for, when that artist still needs genres: by its provider source,
// or by a subscription of that provider id whose name it carries (downloads
// store artists under a synthetic source named after the artist). "" when
// there is none, it already has genres, or its last lookup is not due
// yet (the same recheck as ArtistsMissingGenres).
func (c *Catalog) ArtistMissingGenresBySource(ctx context.Context, provider, sourceID, name string, recheck GenreRecheck) (string, error) {
	var id string
	err := c.db.QueryRowContext(ctx, `SELECT a.id FROM artists a LEFT JOIN artist_genre_lookups l ON l.artist_id=a.id
 WHERE a.genres_json='[]'::jsonb AND NOT a.genres_manual AND `+fmt.Sprintf(genreLookupDue, "$4", "$5")+` AND (
  EXISTS(SELECT 1 FROM artist_sources s WHERE s.artist_id=a.id AND s.provider=$1 AND s.source_id=$2)
  OR (a.provider=$1 AND a.source_id=$2)
  OR ($3<>'' AND lower(a.name)=lower($3) AND EXISTS(SELECT 1 FROM artist_sources s WHERE s.artist_id=a.id AND s.provider=$1)))
 ORDER BY a.id LIMIT 1`, provider, sourceID, strings.TrimSpace(name), recheck.NoGenres.Seconds(), recheck.Failed.Seconds()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", wrapDB("artist by provider source", err)
	}
	return id, nil
}
