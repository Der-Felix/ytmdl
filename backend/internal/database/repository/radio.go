package repository

import (
	"context"
	"database/sql"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// Radio seed kinds.
const (
	RadioSeedTrack    = "track"
	RadioSeedArtist   = "artist"
	RadioSeedRelease  = "release"
	RadioSeedGenre    = "genre"
	RadioSeedPersonal = "personal"
)

const (
	// RadioDefaultLimit is how many tracks one radio request returns unless
	// asked for fewer or more.
	RadioDefaultLimit = 50
	// RadioMaxLimit bounds one radio computation; a station is at most this
	// long.
	RadioMaxLimit = 500
	// RadioMaxExclude bounds the track ids a client may exclude per request.
	RadioMaxExclude = 500
)

// RadioRequest selects a radio. At most one of TrackID, ArtistID and
// ReleaseID is the seed; Genre alone makes a genre radio and, with a seed,
// filters it. Without any of them the radio is personal.
type RadioRequest struct {
	User      string
	TrackID   string
	ArtistID  string
	ReleaseID string
	Genre     string
	// Nonce varies the shuffle; the same nonce gives the same order.
	Nonce string
	// Limit is the number of tracks (1–RadioMaxLimit, default 50).
	Limit int
	// Exclude lists track ids that must not be returned (already played or
	// queued by the client).
	Exclude []string
}

// RadioSeed describes what a radio started from.
type RadioSeed struct {
	Kind string `json:"kind"`
	// ID is the track, artist or release id; empty for genre and personal.
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// RadioResult is a computed radio.
type RadioResult struct {
	Title    string
	Seed     RadioSeed
	TrackIDs []string
}

// LocalRadio uses only locally available songs and this user's preferences.
//
// Without a seed it is a personal radio (optionally limited to one genre):
// genre/artist affinity is balanced with variety, a penalty for recent plays
// and a penalty for tracks the user skipped (see SkipPenalty).
//
// With a seed (the web player's autoplay and song radio, the Apple app's
// radio) the result stays near the seed; see seedRadioSQL for the tiers.
func (c *Catalog) LocalRadio(ctx context.Context, user, seed, genre, nonce string) ([]music.LibraryTrack, error) {
	res, err := c.Radio(ctx, RadioRequest{User: user, TrackID: seed, Genre: genre, Nonce: nonce})
	if err != nil {
		return nil, err
	}
	return c.TracksByIDs(ctx, res.TrackIDs)
}

// Radio computes a radio: its title, its seed and the ordered track ids.
// Excluded artists, genres and recordings (see listening_exclusions) never
// appear, whatever the seed.
func (c *Catalog) Radio(ctx context.Context, req RadioRequest) (*RadioResult, error) {
	req.TrackID, req.ArtistID, req.ReleaseID = strings.TrimSpace(req.TrackID), strings.TrimSpace(req.ArtistID), strings.TrimSpace(req.ReleaseID)
	req.Genre = strings.TrimSpace(req.Genre)
	if len(req.TrackID) > 200 || len(req.ArtistID) > 200 || len(req.ReleaseID) > 200 || len(req.Nonce) > 100 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Radio-Auswahl.")
	}
	seeds := 0
	for _, s := range []string{req.TrackID, req.ArtistID, req.ReleaseID} {
		if s != "" {
			seeds++
		}
	}
	if seeds > 1 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Bitte nur einen Ausgangspunkt für das Radio angeben.")
	}
	if _, err := NormalizeGenres([]string{req.Genre}); err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = RadioDefaultLimit
	}
	req.Limit = min(req.Limit, RadioMaxLimit)
	if len(req.Exclude) > RadioMaxExclude {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Zu viele ausgeschlossene Titel (höchstens 500).")
	}
	exclude := make([]string, 0, len(req.Exclude))
	for _, id := range req.Exclude {
		if id = strings.TrimSpace(id); id != "" && len(id) <= 200 {
			exclude = append(exclude, id)
		}
	}

	res := &RadioResult{}
	query, seedID := generalRadioSQL, ""
	switch {
	case req.TrackID != "":
		var title string
		err := c.db.QueryRowContext(ctx, `SELECT title FROM tracks WHERE id=$1 AND EXISTS(SELECT 1 FROM files WHERE track_id=$1)`, req.TrackID).Scan(&title)
		if err == sql.ErrNoRows {
			return nil, apperr.New(apperr.CodeTrackNotFound, "Ausgangstitel ist nicht mehr verfügbar.")
		}
		if err != nil {
			return nil, err
		}
		res.Seed = RadioSeed{Kind: RadioSeedTrack, ID: req.TrackID, Name: title}
		query, seedID = seedRadioQuery(trackSeedSQL), req.TrackID
	case req.ArtistID != "":
		var name string
		err := c.db.QueryRowContext(ctx, `SELECT name FROM artists WHERE id=$1`, req.ArtistID).Scan(&name)
		if err == sql.ErrNoRows {
			return nil, apperr.New(apperr.CodeArtistNotFound, "Künstler nicht gefunden.")
		}
		if err != nil {
			return nil, err
		}
		res.Seed = RadioSeed{Kind: RadioSeedArtist, ID: req.ArtistID, Name: name}
		query, seedID = seedRadioQuery(artistSeedSQL), req.ArtistID
	case req.ReleaseID != "":
		var title string
		err := c.db.QueryRowContext(ctx, `SELECT title FROM releases WHERE id=$1`, req.ReleaseID).Scan(&title)
		if err == sql.ErrNoRows {
			return nil, apperr.New(apperr.CodeReleaseNotFound, "Album nicht gefunden.")
		}
		if err != nil {
			return nil, err
		}
		res.Seed = RadioSeed{Kind: RadioSeedRelease, ID: req.ReleaseID, Name: title}
		query, seedID = seedRadioQuery(releaseSeedSQL), req.ReleaseID
	case req.Genre != "":
		res.Seed = RadioSeed{Kind: RadioSeedGenre, Name: req.Genre}
	default:
		res.Seed = RadioSeed{Kind: RadioSeedPersonal, Name: "Dein Radio"}
	}
	res.Title = "Radio: " + res.Seed.Name
	if res.Seed.Kind == RadioSeedPersonal {
		res.Title = res.Seed.Name
	}

	rows, err := c.db.QueryContext(ctx, query, req.User, seedID, req.Genre, req.Nonce, exclude, req.Limit)
	if err != nil {
		return nil, wrapDB("radio", err)
	}
	defer rows.Close()
	res.TrackIDs = []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		res.TrackIDs = append(res.TrackIDs, id)
	}
	return res, rows.Err()
}

// radioArtistCap is how many tracks come from one artist before the tracks
// of other artists of the same tier have had their turn; radioArtistMax is
// the most one artist gets in a seeded radio.
const (
	radioArtistCap   = "3"
	radioArtistMax   = "5"
	radioCandidates  = "100"
	radioSeedRelease = "300"
)

// generalRadioSQL is the radio without a seed. Parameters: $1 user, $2 seed
// (always empty here), $3 genre filter, $4 nonce, $5 excluded track ids, $6
// limit.
var generalRadioSQL = `WITH ` + exclusionCTE + `, favorite_artists AS (SELECT DISTINCT t.artist_id FROM favorite_tracks f JOIN tracks t ON t.id=f.track_id WHERE f.user_id=$1), heard_artists AS (SELECT DISTINCT t.artist_id FROM listening_history h JOIN tracks t ON t.id=h.track_id WHERE h.user_id=$1), seed AS (SELECT t.artist_id,a.genres_json FROM tracks t LEFT JOIN artists a ON a.id=t.artist_id WHERE t.id=$2), scored AS (
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
 WHERE t.id<>$2 AND t.id<>ALL($5::text[]) AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) AND($3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(a.genres_json,'[]')) g WHERE lower(g COLLATE "pg_c_utf8")=lower($3 COLLATE "pg_c_utf8")))
 AND ` + exclusionFilter("t") + `
 ), unique_recordings AS(SELECT *,row_number() OVER(PARTITION BY artist_id,title_key ORDER BY score DESC,shuffle) recording FROM scored), varied AS(SELECT *,row_number() OVER(PARTITION BY artist_id ORDER BY score DESC,shuffle) artist_rank FROM unique_recordings WHERE recording=1)
 SELECT id FROM varied ORDER BY CASE WHEN artist_rank<=3 THEN 0 ELSE 1 END,score DESC,shuffle LIMIT $6`

// The seed of a seeded radio as sd(artist_id,title_key,year,genres,
// primary_genre); $2 is the seed's id.
const (
	// A track: its artist, its title (other versions are left out) and its
	// year, else its release's.
	trackSeedSQL = `SELECT t.artist_id,lower(btrim(t.title)) AS title_key,
  COALESCE(NULLIF(t.year,0),r.year,0) AS year,
  COALESCE(a.genres_json,'[]'::jsonb) AS genres,
  lower(btrim(COALESCE(a.genres_json->>0,''))) AS primary_genre
 FROM tracks t LEFT JOIN releases r ON r.id=t.release_id LEFT JOIN artists a ON a.id=t.artist_id WHERE t.id=$2`
	// An artist: no title and no era, so every track of the artist may play.
	artistSeedSQL = `SELECT a.id AS artist_id,''::text AS title_key,0 AS year,
  COALESCE(a.genres_json,'[]'::jsonb) AS genres,
  lower(btrim(COALESCE(a.genres_json->>0,''))) AS primary_genre
 FROM artists a WHERE a.id=$2`
	// A release: its artist and its year; the release's own tracks and the
	// artists on it are close (see seed_releases).
	releaseSeedSQL = `SELECT r.artist_id,''::text AS title_key,COALESCE(r.year,0) AS year,
  COALESCE(a.genres_json,'[]'::jsonb) AS genres,
  lower(btrim(COALESCE(a.genres_json->>0,''))) AS primary_genre
 FROM releases r LEFT JOIN artists a ON a.id=r.artist_id WHERE r.id=$2`
)

// seedRadioQuery is the radio around a seed. Parameters: $1 user, $2 seed id,
// $3 optional genre filter, $4 nonce, $5 excluded track ids, $6 limit.
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
//     compilation, a feature, a placement), or appear on the seed release,
//     unless both have genres and the other one lacks the seed's main genre.
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
// signal. Excluded artists (directly or by genre) are no candidates, and
// excluded recordings are left out.
//
// Cost: the artists table is read once (it is far smaller than tracks); at
// most 100 candidate artists are expanded through idx_tracks_artist, so the
// number of tracks scored does not grow with the library.
func seedRadioQuery(seedSQL string) string {
	return `WITH sd AS (
 ` + seedSQL + `
), ` + exclusionCTE + `, seed_genres AS (
 SELECT DISTINCT lower(btrim(g)) AS genre FROM sd CROSS JOIN jsonb_array_elements_text(sd.genres) g
), seed_releases AS (
 SELECT release_id FROM (
  SELECT t.release_id FROM tracks t JOIN sd ON t.artist_id=sd.artist_id WHERE t.release_id IS NOT NULL
  UNION
  SELECT p.release_id FROM tracks t JOIN sd ON t.artist_id=sd.artist_id JOIN track_placements p ON p.track_id=t.id
  UNION
  SELECT id FROM releases WHERE id=$2
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
 WHERE tier IS NOT NULL AND NOT EXISTS(SELECT 1 FROM excluded_artists xa WHERE xa.artist_id=tiered.id)
  AND EXISTS(SELECT 1 FROM tracks t JOIN files f ON f.track_id=t.id WHERE t.artist_id=tiered.id AND t.id<>$2)
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
   WHERE t.artist_id=c.id AND t.id<>$2 AND t.id<>ALL($5::text[]) AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
    AND NOT (t.artist_id IS NOT DISTINCT FROM sd.artist_id AND lower(btrim(t.title))=sd.title_key)
    AND ` + exclusionFilter("t") + `
  ) k ORDER BY k.title_key,k.score DESC,k.shuffle
 ) u ORDER BY u.score DESC,u.shuffle LIMIT ` + radioArtistMax + `
) p
ORDER BY c.tier,CASE WHEN p.artist_rank<=` + radioArtistCap + ` THEN 0 ELSE 1 END,c.artist_score+p.score DESC,p.shuffle LIMIT $6`
}
