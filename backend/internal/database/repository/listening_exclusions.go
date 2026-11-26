package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// Exclusion kinds: what a user does not want chosen for them in mixes and
// the radio. Explicit playback of an excluded item stays possible.
const (
	ExclusionArtist = "artist"
	ExclusionGenre  = "genre"
	// ExclusionTrack is "weniger davon": stronger than a skip, it keeps every
	// version of the recording (same artist, same normalized title) out.
	ExclusionTrack = "track"
)

// maxExclusions bounds the exclusions of one user.
const maxExclusions = 1000

// ListeningExclusion is one steering preference of a user.
type ListeningExclusion struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Target is the artist id, the track id or the genre as entered.
	Target string `json:"target"`
	// Name is the artist's name, the track's title or the genre.
	Name string `json:"name"`
	// ArtistName is the track's artist, for track exclusions.
	ArtistName string    `json:"artist_name,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

const exclusionSelectSQL = `SELECT e.id,e.kind,COALESCE(e.artist_id,e.track_id,e.genre),
 COALESCE(CASE e.kind WHEN 'artist' THEN a.name WHEN 'track' THEN t.title ELSE e.genre END,''),
 COALESCE(CASE WHEN e.kind='track' THEN ta.name END,''),e.created_at
 FROM listening_exclusions e LEFT JOIN artists a ON a.id=e.artist_id LEFT JOIN tracks t ON t.id=e.track_id LEFT JOIN artists ta ON ta.id=t.artist_id
 WHERE e.user_id=$1`

// exclusionKeySQL matches the unique key of listening_exclusions for kind $2
// and target $3.
const exclusionKeySQL = ` AND e.kind=$2 AND COALESCE(e.artist_id,e.track_id,lower(btrim(e.genre) COLLATE "pg_c_utf8"))=CASE WHEN $2='genre' THEN lower(btrim($3::text) COLLATE "pg_c_utf8") ELSE $3::text END`

// ListeningExclusions lists the user's exclusions, newest first.
func (c *Catalog) ListeningExclusions(ctx context.Context, userID string) ([]ListeningExclusion, error) {
	return c.queryExclusions(ctx, exclusionSelectSQL+` ORDER BY e.created_at DESC,e.id`, userID)
}

func (c *Catalog) queryExclusions(ctx context.Context, query string, args ...any) ([]ListeningExclusion, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDB("listening exclusions", err)
	}
	defer rows.Close()
	out := []ListeningExclusion{}
	for rows.Next() {
		var e ListeningExclusion
		if err := rows.Scan(&e.ID, &e.Kind, &e.Target, &e.Name, &e.ArtistName, &e.CreatedAt); err != nil {
			return nil, wrapDB("scan listening exclusion", err)
		}
		e.CreatedAt = e.CreatedAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// normalizeExclusion validates kind and target.
func normalizeExclusion(kind, target string) (string, string, error) {
	kind, target = strings.TrimSpace(kind), strings.TrimSpace(target)
	switch kind {
	case ExclusionArtist, ExclusionTrack:
		if target == "" || len(target) > 200 {
			return "", "", apperr.New(apperr.CodeInvalidRequest, "Bitte einen Künstler oder Titel angeben.")
		}
	case ExclusionGenre:
		if target == "" || utf8.RuneCountInString(target) > 80 || strings.ContainsFunc(target, unicode.IsControl) {
			return "", "", apperr.New(apperr.CodeInvalidRequest, "Genres dürfen höchstens 80 Zeichen und keine Steuerzeichen enthalten.")
		}
	default:
		return "", "", apperr.New(apperr.CodeInvalidRequest, "Unbekannte Art der Einstellung (artist, genre oder track).")
	}
	return kind, target, nil
}

// AddListeningExclusion stores an exclusion and returns it. Adding one that
// exists already returns the existing one.
func (c *Catalog) AddListeningExclusion(ctx context.Context, userID, kind, target string) (*ListeningExclusion, error) {
	kind, target, err := normalizeExclusion(kind, target)
	if err != nil {
		return nil, err
	}
	switch kind {
	case ExclusionArtist:
		if err := c.requireRow(ctx, `SELECT EXISTS(SELECT 1 FROM artists WHERE id=$1)`, target, apperr.CodeArtistNotFound, "Künstler nicht gefunden."); err != nil {
			return nil, err
		}
	case ExclusionTrack:
		if err := c.requireRow(ctx, `SELECT EXISTS(SELECT 1 FROM tracks WHERE id=$1)`, target, apperr.CodeTrackNotFound, "Titel nicht gefunden."); err != nil {
			return nil, err
		}
	}
	var count int
	if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM listening_exclusions WHERE user_id=$1`, userID).Scan(&count); err != nil {
		return nil, wrapDB("count listening exclusions", err)
	}
	existing, err := c.queryExclusions(ctx, exclusionSelectSQL+exclusionKeySQL, userID, kind, target)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return &existing[0], nil
	}
	if count >= maxExclusions {
		return nil, apperr.New(apperr.CodeInvalidRequest, fmt.Sprintf("Höchstens %d Ausschlüsse sind möglich.", maxExclusions))
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO listening_exclusions(id,user_id,kind,artist_id,track_id,genre)
 VALUES($1,$2,$3,CASE WHEN $3='artist' THEN $4::text END,CASE WHEN $3='track' THEN $4::text END,CASE WHEN $3='genre' THEN $4::text END)
 ON CONFLICT DO NOTHING`, music.NewID(), userID, kind, target)
	if err != nil {
		return nil, wrapDB("add listening exclusion", err)
	}
	existing, err = c.queryExclusions(ctx, exclusionSelectSQL+exclusionKeySQL, userID, kind, target)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		// The artist or track was deleted in between.
		return nil, apperr.New(apperr.CodeInvalidRequest, "Die Einstellung konnte nicht gespeichert werden.")
	}
	return &existing[0], nil
}

func (c *Catalog) requireRow(ctx context.Context, query, id string, code apperr.Code, message string) error {
	var ok bool
	if err := c.db.QueryRowContext(ctx, query, id).Scan(&ok); err != nil {
		return wrapDB("check exclusion target", err)
	}
	if !ok {
		return apperr.New(code, message)
	}
	return nil
}

// RemoveListeningExclusion deletes one of the user's exclusions by id. An id
// that does not exist or belongs to someone else is PREFERENCE_NOT_FOUND.
func (c *Catalog) RemoveListeningExclusion(ctx context.Context, userID, id string) error {
	res, err := c.db.ExecContext(ctx, `DELETE FROM listening_exclusions WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return wrapDB("remove listening exclusion", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.New(apperr.CodePreferenceNotFound, "Einstellung nicht gefunden.")
	}
	return nil
}

// RemoveListeningExclusionByTarget deletes the user's exclusion of kind and
// target, if there is one.
func (c *Catalog) RemoveListeningExclusionByTarget(ctx context.Context, userID, kind, target string) error {
	kind, target, err := normalizeExclusion(kind, target)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `DELETE FROM listening_exclusions e WHERE e.user_id=$1`+exclusionKeySQL, userID, kind, target)
	if err != nil {
		return wrapDB("remove listening exclusion", err)
	}
	return nil
}

// exclusionCTE defines, for the user $1, excluded_artists(artist_id) (artists
// excluded directly or through one of their genres) and
// excluded_recordings(artist_id,title_key) (every version of an excluded
// track). Both are tiny for a user with few exclusions; the artists scan for
// genres only runs when the user excluded a genre.
const exclusionCTE = `excluded_artists AS (
 SELECT artist_id FROM listening_exclusions WHERE user_id=$1 AND kind='artist'
 UNION
 SELECT xa.id FROM artists xa WHERE EXISTS(SELECT 1 FROM listening_exclusions WHERE user_id=$1 AND kind='genre')
  AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(xa.genres_json) xg JOIN listening_exclusions xe ON xe.user_id=$1 AND xe.kind='genre'
   AND lower(btrim(xe.genre) COLLATE "pg_c_utf8")=lower(btrim(xg) COLLATE "pg_c_utf8"))
), excluded_recordings AS (
 SELECT DISTINCT COALESCE(xt.artist_id,'') AS artist_id,lower(btrim(xt.title)) AS title_key
 FROM listening_exclusions xe JOIN tracks xt ON xt.id=xe.track_id WHERE xe.user_id=$1 AND xe.kind='track'
)`

// exclusionFilter is the condition that keeps the track alias t out when it
// is excluded; the query must define exclusionCTE.
func exclusionFilter(t string) string {
	return fmt.Sprintf(`NOT EXISTS(SELECT 1 FROM excluded_artists xa WHERE xa.artist_id=%[1]s.artist_id) AND NOT EXISTS(SELECT 1 FROM excluded_recordings xr WHERE xr.artist_id=COALESCE(%[1]s.artist_id,'') AND xr.title_key=lower(btrim(%[1]s.title)))`, t)
}

// userExclusions holds a user's excluded artists and genres for the steps
// that pick artists or genres before any track query.
type userExclusions struct {
	artists map[string]bool
	// genres holds lowercased genre names.
	genres map[string]bool
}

func (c *Catalog) loadExclusions(ctx context.Context, userID string) (userExclusions, error) {
	out := userExclusions{artists: map[string]bool{}, genres: map[string]bool{}}
	rows, err := c.db.QueryContext(ctx, `SELECT kind,COALESCE(artist_id,''),COALESCE(lower(btrim(genre) COLLATE "pg_c_utf8"),'') FROM listening_exclusions WHERE user_id=$1 AND kind IN('artist','genre')`, userID)
	if err != nil {
		return out, wrapDB("load listening exclusions", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, artist, genre string
		if err := rows.Scan(&kind, &artist, &genre); err != nil {
			return out, err
		}
		if kind == ExclusionArtist {
			out.artists[artist] = true
		} else {
			out.genres[genre] = true
		}
	}
	return out, rows.Err()
}

// FilterExcludedTracks returns ids without the tracks the user's exclusions
// cover, in the given order.
func (c *Catalog) FilterExcludedTracks(ctx context.Context, userID string, ids []string) ([]string, error) {
	out := []string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.db.QueryContext(ctx, `WITH `+exclusionCTE+`
 SELECT t.id FROM unnest($2::text[]) WITH ORDINALITY AS u(id,n) JOIN tracks t ON t.id=u.id
 WHERE `+exclusionFilter("t")+` ORDER BY u.n`, userID, ids)
	if err != nil {
		return nil, wrapDB("filter excluded tracks", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
