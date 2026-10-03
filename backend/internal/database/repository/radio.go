package repository

import (
	"context"
	"database/sql"
	"strings"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// LocalRadio uses only locally available songs and this user's preferences.
// Genre/artist affinity is balanced with variety and a penalty for recent plays.
func (c *Catalog) LocalRadio(ctx context.Context, user, seed, genre, nonce string) ([]music.LibraryTrack, error) {
	seed, genre = strings.TrimSpace(seed), strings.TrimSpace(genre)
	if len(seed) > 200 || len(nonce) > 100 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Radio-Auswahl.")
	}
	if _, err := NormalizeGenres([]string{genre}); err != nil {
		return nil, err
	}
	if seed != "" {
		var found string
		err := c.db.QueryRowContext(ctx, `SELECT id FROM tracks WHERE id=$1 AND EXISTS(SELECT 1 FROM files WHERE track_id=$1)`, seed).Scan(&found)
		if err == sql.ErrNoRows {
			return nil, apperr.New(apperr.CodeTrackNotFound, "Ausgangstitel ist nicht mehr verfügbar.")
		}
		if err != nil {
			return nil, err
		}
	}
	rows, err := c.db.QueryContext(ctx, `WITH favorite_artists AS (SELECT DISTINCT t.artist_id FROM favorite_tracks f JOIN tracks t ON t.id=f.track_id WHERE f.user_id=$1), heard_artists AS (SELECT DISTINCT t.artist_id FROM listening_history h JOIN tracks t ON t.id=h.track_id WHERE h.user_id=$1), seed AS (SELECT t.artist_id,a.genres_json FROM tracks t LEFT JOIN artists a ON a.id=t.artist_id WHERE t.id=$2), scored AS (
 SELECT t.id,COALESCE(t.artist_id,'') AS artist_id,lower(btrim(t.title)) AS title_key,
 (CASE WHEN t.artist_id IN(SELECT artist_id FROM seed) THEN 8 ELSE 0 END+
 CASE WHEN EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(a.genres_json,'[]')) g JOIN seed s ON EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(s.genres_json,'[]')) sg WHERE lower(sg)=lower(g))) THEN 6 ELSE 0 END+
 CASE WHEN t.artist_id IN(SELECT artist_id FROM favorite_artists) THEN 4 ELSE 0 END+
 CASE WHEN t.artist_id IN(SELECT artist_id FROM heard_artists) THEN 2 ELSE 0 END+
 CASE WHEN EXISTS(SELECT 1 FROM favorite_tracks f WHERE f.user_id=$1 AND f.track_id=t.id) THEN 2 ELSE 0 END-
 CASE WHEN h.last_played_at>now()-interval '1 day' THEN 5 ELSE 0 END) AS score,
 encode(sha256(convert_to(jsonb_build_array($4::text,t.id)::text,'UTF8')),'hex') AS shuffle
 FROM tracks t LEFT JOIN artists a ON a.id=t.artist_id LEFT JOIN listening_history h ON h.track_id=t.id AND h.user_id=$1
 WHERE t.id<>$2 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) AND($3='' OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(a.genres_json,'[]')) g WHERE lower(g COLLATE "pg_c_utf8")=lower($3 COLLATE "pg_c_utf8")))
 ), unique_recordings AS(SELECT *,row_number() OVER(PARTITION BY artist_id,title_key ORDER BY score DESC,shuffle) recording FROM scored), varied AS(SELECT *,row_number() OVER(PARTITION BY artist_id ORDER BY score DESC,shuffle) artist_rank FROM unique_recordings WHERE recording=1)
 SELECT id FROM varied ORDER BY CASE WHEN artist_rank<=3 THEN 0 ELSE 1 END,score DESC,shuffle LIMIT 50`, user, seed, genre, nonce)
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
