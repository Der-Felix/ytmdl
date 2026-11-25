package repository

import (
	"context"
	"database/sql"
	"strings"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// LocalRadio uses only locally available songs and this user's preferences.
//
// Without a seed it is a personal radio (optionally limited to one genre):
// genre/artist affinity is balanced with variety, a penalty for recent plays
// and a penalty for tracks the user skipped (see SkipPenalty).
//
// With a seed (the web player's autoplay and song radio, the Apple app's
// radio) the result stays near the seed; see seedRadioSQL for the tiers.
func (c *Catalog) LocalRadio(ctx context.Context, user, seed, genre, nonce string) ([]music.LibraryTrack, error) {
	seed, genre = strings.TrimSpace(seed), strings.TrimSpace(genre)
	if len(seed) > 200 || len(nonce) > 100 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Radio-Auswahl.")
	}
	if _, err := NormalizeGenres([]string{genre}); err != nil {
		return nil, err
	}
	query := generalRadioSQL
	if seed != "" {
		var found string
		err := c.db.QueryRowContext(ctx, `SELECT id FROM tracks WHERE id=$1 AND EXISTS(SELECT 1 FROM files WHERE track_id=$1)`, seed).Scan(&found)
		if err == sql.ErrNoRows {
			return nil, apperr.New(apperr.CodeTrackNotFound, "Ausgangstitel ist nicht mehr verfügbar.")
		}
		if err != nil {
			return nil, err
		}
		query = seedRadioSQL
	}
	rows, err := c.db.QueryContext(ctx, query, user, seed, genre, nonce)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return c.TracksByIDs(ctx, ids)
}

// radioLimit is how many tracks one radio request returns at most, and
// radioArtistCap how many of them come from one artist before the tracks of
// other artists of the same tier have had their turn.
const (
	radioLimit       = "50"
	radioArtistCap   = "3"
	radioArtistMax   = "5"
	radioCandidates  = "100"
	radioSeedRelease = "300"
)

// generalRadioSQL is the radio without a seed. Parameters: $1 user, $2 seed
// (always empty here), $3 genre filter, $4 nonce.
const generalRadioSQL = `WITH favorite_artists AS (SELECT DISTINCT t.artist_id FROM favorite_tracks f JOIN tracks t ON t.id=f.track_id WHERE f.user_id=$1), heard_artists AS (SELECT DISTINCT t.artist_id FROM listening_history h JOIN tracks t ON t.id=h.track_id WHERE h.user_id=$1), seed AS (SELECT t.artist_id,a.genres_json FROM tracks t LEFT JOIN artists a ON a.id=t.artist_id WHERE t.id=$2), scored AS (
 SELECT t.id,COALESCE(t.artist_id,'') AS artist_id,lower(btrim(t.title)) AS title_key,
 (CASE WHEN t.artist_id IN(SELECT artist_id FROM seed) THEN 8 ELSE 0 END+
 CASE WHEN EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(a.genres_json,'[]')) g JOIN seed s ON EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(s.genres_json,'[]')) sg WHERE lower(sg)=lower(g))) THEN 6 ELSE 0 END+
 CASE WHEN t.artist_id IN(SELECT artist_id FROM favorite_artists) THEN 4 ELSE 0 END+
 CASE WHEN t.artist_id IN(SELECT artist_id FROM heard_artists) THEN 2 ELSE 0 END+
 CASE WHEN EXISTS(SELECT 1 FROM favorite_tracks f WHERE f.user_id=$1 AND f.track_id=t.id) THEN 2 ELSE 0 END-
 CASE WHEN h.last_played_at>now()-interval '1 day' THEN 5 ELSE 0 END-
 ` + skipPenaltySQL + `) AS score,
 encode(sha256(convert_to(jsonb_build_array($4::text,t.id)::text,'UTF8')),'hex') AS shuffle
 FROM tracks t LEFT JOIN artists a ON a.id=t.artist_id LEFT JOIN listening_history h ON h.track_id=t.id AND h.user_id=$1 LEFT JOIN track_skips s ON s.track_id=t.id AND s.user_id=$1
 WHERE t.id<>$2 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) AND($3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(a.genres_json,'[]')) g WHERE lower(g COLLATE "pg_c_utf8")=lower($3 COLLATE "pg_c_utf8")))
 ), unique_recordings AS(SELECT *,row_number() OVER(PARTITION BY artist_id,title_key ORDER BY score DESC,shuffle) recording FROM scored), varied AS(SELECT *,row_number() OVER(PARTITION BY artist_id ORDER BY score DESC,shuffle) artist_rank FROM unique_recordings WHERE recording=1)
 SELECT id FROM varied ORDER BY CASE WHEN artist_rank<=3 THEN 0 ELSE 1 END,score DESC,shuffle LIMIT ` + radioLimit

// seedRadioSQL is the radio around a seed track. Parameters: $1 user, $2 seed
// track, $3 optional genre filter, $4 nonce.
//
// Genres live on the artist (at most three from Deezer, most frequent first,
// or as the user set them), so the first genre is the artist's main genre.
// Sharing any one genre is far too loose: a party schlager artist listed as
// "Schlager, Pop" shares "Pop" with half the library. Candidates are
// therefore artists in two tiers, and a tier-2 track only plays once tier 1
// is used up:
//
//  1. close: the seed's artist; artists whose main genre is the seed artist's
//     main genre; artists that share a release with the seed's artist (a
//     compilation, a feature, a placement) unless both have genres and the
//     other one lacks the seed's main genre.
//  2. related: artists that list the seed's main genre, but not as their main
//     genre.
//
// Artists that share only a secondary genre, and everything else, are never
// played. A small library therefore yields fewer tracks rather than unrelated
// ones. When the seed's artist has no genres, only tier 1 by artist and
// shared releases remains.
//
// Within a tier, more shared genres, a shared release, a recording from the
// seed's era (track or release year within 5, softer within 10 years) and the
// user's own affinity rank higher; recent plays and skips rank lower. At most
// three tracks per artist come before any artist's fourth, and no artist gets
// more than five. The library carries no language data, so language is not a
// signal.
//
// Cost: the artists table is read once (it is far smaller than tracks); at
// most 100 candidate artists are expanded through idx_tracks_artist, so the
// number of tracks scored does not grow with the library.
const seedRadioSQL = `WITH sd AS (
 SELECT t.artist_id,lower(btrim(t.title)) AS title_key,
  COALESCE(NULLIF(t.year,0),r.year,0) AS year,
  COALESCE(a.genres_json,'[]'::jsonb) AS genres,
  lower(btrim(COALESCE(a.genres_json->>0,''))) AS primary_genre
 FROM tracks t LEFT JOIN releases r ON r.id=t.release_id LEFT JOIN artists a ON a.id=t.artist_id WHERE t.id=$2
), seed_genres AS (
 SELECT DISTINCT lower(btrim(g)) AS genre FROM sd CROSS JOIN jsonb_array_elements_text(sd.genres) g
), seed_releases AS (
 SELECT release_id FROM (
  SELECT t.release_id FROM tracks t JOIN sd ON t.artist_id=sd.artist_id WHERE t.release_id IS NOT NULL
  UNION
  SELECT p.release_id FROM tracks t JOIN sd ON t.artist_id=sd.artist_id JOIN track_placements p ON p.track_id=t.id
 ) x LIMIT ` + radioSeedRelease + `
), co_artists AS (
 SELECT t.artist_id FROM seed_releases sr JOIN tracks t ON t.release_id=sr.release_id WHERE t.artist_id IS NOT NULL
 UNION
 SELECT t.artist_id FROM seed_releases sr JOIN track_placements p ON p.release_id=sr.release_id JOIN tracks t ON t.id=p.track_id WHERE t.artist_id IS NOT NULL
), favorite_artists AS (
 SELECT DISTINCT t.artist_id FROM favorite_tracks f JOIN tracks t ON t.id=f.track_id WHERE f.user_id=$1
), heard_artists AS (
 SELECT DISTINCT t.artist_id FROM listening_history h JOIN tracks t ON t.id=h.track_id WHERE h.user_id=$1
), matched AS (
 SELECT a.id,
  COALESCE(a.id=sd.artist_id,false) AS same_artist,
  EXISTS(SELECT 1 FROM co_artists ca WHERE ca.artist_id=a.id) AS co_release,
  jsonb_array_length(a.genres_json)=0 AS no_genres,
  sd.primary_genre AS seed_primary,
  sd.primary_genre<>'' AND lower(btrim(COALESCE(a.genres_json->>0,'')))=sd.primary_genre AS same_primary,
  sd.primary_genre<>'' AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(a.genres_json) g WHERE lower(btrim(g))=sd.primary_genre) AS has_primary,
  (SELECT count(*) FROM jsonb_array_elements_text(a.genres_json) g WHERE lower(btrim(g)) IN(SELECT genre FROM seed_genres)) AS shared
 FROM artists a CROSS JOIN sd
 WHERE $3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(a.genres_json) g WHERE lower(g COLLATE "pg_c_utf8")=lower($3 COLLATE "pg_c_utf8"))
), tiered AS (
 SELECT id,
  CASE WHEN same_artist OR same_primary OR (co_release AND (no_genres OR seed_primary='' OR has_primary)) THEN 1
   WHEN has_primary THEN 2 END AS tier,
  shared*2+CASE WHEN co_release THEN 3 ELSE 0 END+CASE WHEN same_artist THEN 2 ELSE 0 END+
  CASE WHEN id IN(SELECT artist_id FROM favorite_artists) THEN 2 ELSE 0 END+
  CASE WHEN id IN(SELECT artist_id FROM heard_artists) THEN 1 ELSE 0 END AS artist_score
 FROM matched
), candidates AS (
 SELECT id,tier,artist_score FROM tiered
 WHERE tier IS NOT NULL AND EXISTS(SELECT 1 FROM tracks t JOIN files f ON f.track_id=t.id WHERE t.artist_id=tiered.id AND t.id<>$2)
 ORDER BY tier,artist_score DESC,encode(sha256(convert_to(jsonb_build_array($4::text,'artist',id)::text,'UTF8')),'hex')
 LIMIT ` + radioCandidates + `
)
SELECT p.id FROM candidates c CROSS JOIN LATERAL (
 SELECT u.id,u.score,u.shuffle,row_number() OVER(ORDER BY u.score DESC,u.shuffle) AS artist_rank FROM (
  SELECT DISTINCT ON(k.title_key) k.id,k.score,k.shuffle FROM (
   SELECT t.id,lower(btrim(t.title)) AS title_key,
    (CASE WHEN sd.year>0 AND y.year>0 AND abs(y.year-sd.year)<=5 THEN 3 WHEN sd.year>0 AND y.year>0 AND abs(y.year-sd.year)<=10 THEN 1 ELSE 0 END+
    CASE WHEN EXISTS(SELECT 1 FROM favorite_tracks ft WHERE ft.user_id=$1 AND ft.track_id=t.id) THEN 1 ELSE 0 END-
    CASE WHEN h.last_played_at>now()-interval '1 day' THEN 5 ELSE 0 END-
    ` + skipPenaltySQL + `) AS score,
    encode(sha256(convert_to(jsonb_build_array($4::text,t.id)::text,'UTF8')),'hex') AS shuffle
   FROM tracks t CROSS JOIN sd
   LEFT JOIN releases r ON r.id=t.release_id
   CROSS JOIN LATERAL (SELECT COALESCE(NULLIF(t.year,0),r.year,0) AS year) y
   LEFT JOIN listening_history h ON h.track_id=t.id AND h.user_id=$1
   LEFT JOIN track_skips s ON s.track_id=t.id AND s.user_id=$1
   WHERE t.artist_id=c.id AND t.id<>$2 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
    AND NOT (t.artist_id IS NOT DISTINCT FROM sd.artist_id AND lower(btrim(t.title))=sd.title_key)
  ) k ORDER BY k.title_key,k.score DESC,k.shuffle
 ) u ORDER BY u.score DESC,u.shuffle LIMIT ` + radioArtistMax + `
) p
ORDER BY c.tier,CASE WHEN p.artist_rank<=` + radioArtistCap + ` THEN 0 ELSE 1 END,c.artist_score+p.score DESC,p.shuffle LIMIT ` + radioLimit
