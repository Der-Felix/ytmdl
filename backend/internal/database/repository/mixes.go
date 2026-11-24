package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// Mix kinds.
const (
	MixKindGenre      = "genre"
	MixKindOnRepeat   = "on_repeat"
	MixKindRediscover = "rediscover"
	MixKindUnheard    = "unheard"
	MixKindNew        = "new"
)

// maxGenreMixes is the number of "Dein Mix" mixes offered.
const maxGenreMixes = 4

// PersonalMix describes one automatic mix of the current user.
type PersonalMix struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Genre is set for genre mixes.
	Genre      string `json:"genre,omitempty"`
	TrackCount int    `json:"track_count"`
	// CoverTrackIDs (up to four, different releases first) and
	// CoverReleaseIDs feed /library/{tracks|releases}/{id}/artwork.
	CoverTrackIDs   []string `json:"cover_track_ids"`
	CoverReleaseIDs []string `json:"cover_release_ids"`
	trackIDs        []string
}

// PersonalMixDetail is a mix with its tracks.
type PersonalMixDetail struct {
	PersonalMix
	Tracks []music.LibraryTrack `json:"tracks"`
}

// PersonalMixes computes the current user's mixes. They are stable for a
// calendar day and change the next.
func (c *Catalog) PersonalMixes(ctx context.Context, userID string) ([]PersonalMix, error) {
	return c.PersonalMixesOn(ctx, userID, time.Now())
}

// PersonalMixesOn computes the mixes as seeded for the calendar day of day.
// Time windows (recent plays, skips, new files) always use the current time;
// day only seeds the daily shuffle.
func (c *Catalog) PersonalMixesOn(ctx context.Context, userID string, day time.Time) ([]PersonalMix, error) {
	b := mixBuilder{c: c, user: userID, day: day.Format(time.DateOnly), now: time.Now()}
	mixes, err := b.genreMixes(ctx)
	if err != nil {
		return nil, err
	}
	for _, kind := range []string{MixKindOnRepeat, MixKindRediscover, MixKindUnheard, MixKindNew} {
		m, err := b.kindMix(ctx, kind)
		if err != nil {
			return nil, err
		}
		if m != nil {
			mixes = append(mixes, *m)
		}
	}
	return mixes, nil
}

// PersonalMix returns one mix with its tracks.
func (c *Catalog) PersonalMix(ctx context.Context, userID, id string) (*PersonalMixDetail, error) {
	return c.PersonalMixOn(ctx, userID, id, time.Now())
}

// PersonalMixOn is PersonalMix as seeded for the calendar day of day.
func (c *Catalog) PersonalMixOn(ctx context.Context, userID, id string, day time.Time) (*PersonalMixDetail, error) {
	if len(id) > 100 {
		return nil, mixNotFound()
	}
	b := mixBuilder{c: c, user: userID, day: day.Format(time.DateOnly), now: time.Now()}
	var found *PersonalMix
	if strings.HasPrefix(id, MixKindGenre+"-") {
		mixes, err := b.genreMixes(ctx)
		if err != nil {
			return nil, err
		}
		for i := range mixes {
			if mixes[i].ID == id {
				found = &mixes[i]
			}
		}
	} else {
		switch id {
		case MixKindOnRepeat, MixKindRediscover, MixKindUnheard, MixKindNew:
			m, err := b.kindMix(ctx, id)
			if err != nil {
				return nil, err
			}
			found = m
		}
	}
	if found == nil {
		return nil, mixNotFound()
	}
	tracks, err := c.TracksByIDs(ctx, found.trackIDs)
	if err != nil {
		return nil, err
	}
	found.TrackCount = len(tracks)
	return &PersonalMixDetail{PersonalMix: *found, Tracks: tracks}, nil
}

func mixNotFound() error {
	return apperr.New(apperr.CodePlaylistNotFound, "Mix nicht gefunden. Die Auswahl ändert sich täglich; bitte die Übersicht neu laden.")
}

type mixBuilder struct {
	c    *Catalog
	user string
	day  string
	now  time.Time
}

func (b mixBuilder) seed(mixID string) string {
	return b.user + "|" + b.day + "|" + mixID
}

func genreMixID(genre string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(genre))))
	return MixKindGenre + "-" + hex.EncodeToString(sum[:6])
}

func newMix(id, kind, title, description string, picked []MixCandidate) *PersonalMix {
	if len(picked) < MixMinTracks {
		return nil
	}
	m := &PersonalMix{ID: id, Kind: kind, Title: title, Description: description, TrackCount: len(picked)}
	for _, p := range picked {
		m.trackIDs = append(m.trackIDs, p.ID)
	}
	m.CoverTrackIDs, m.CoverReleaseIDs = mixCovers(picked)
	return m
}

// genreMixes builds up to four genre mixes: the user's genres by affinity
// first, then, for a new user or too few such genres, the library's largest.
func (b mixBuilder) genreMixes(ctx context.Context) ([]PersonalMix, error) {
	genres, err := b.userGenres(ctx)
	if err != nil {
		return nil, err
	}
	if len(genres) < maxGenreMixes {
		more, err := b.libraryGenres(ctx)
		if err != nil {
			return nil, err
		}
		genres = append(genres, more...)
	}
	mixes := []PersonalMix{}
	seen := map[string]bool{}
	for _, g := range genres {
		if len(mixes) == maxGenreMixes {
			break
		}
		id := genreMixID(g)
		if seen[id] {
			continue
		}
		seen[id] = true
		cands, err := b.candidates(ctx, id, genreCandidatesSQL, g)
		if err != nil {
			return nil, err
		}
		title := fmt.Sprintf("Dein Mix %d", len(mixes)+1)
		if m := newMix(id, MixKindGenre, title, "Deine Favoriten aus "+g+" und Titel, die du kaum kennst", composeGenreMix(cands, b.now)); m != nil {
			m.Genre = g
			mixes = append(mixes, *m)
		}
	}
	return mixes, nil
}

func (b mixBuilder) kindMix(ctx context.Context, kind string) (*PersonalMix, error) {
	var (
		query, title, description string
		score                     func(MixCandidate, time.Time) float64
		jitter                    float64
	)
	switch kind {
	case MixKindOnRepeat:
		query, score, jitter = onRepeatCandidatesSQL, onRepeatScore, 1
		title, description = "In Dauerschleife", "Deine meistgespielten Titel der letzten 30 Tage"
	case MixKindRediscover:
		query, score, jitter = rediscoverCandidatesSQL, rediscoverScore, 3
		title, description = "Wiederentdecken", "Früher oft gehört, seit Wochen nicht mehr"
	case MixKindUnheard:
		query, score, jitter = unheardCandidatesSQL, discoveryScore, 4
		title, description = "Noch nicht gehört", "Unbekannte Titel deiner Künstler"
	case MixKindNew:
		query, score, jitter = newCandidatesSQL, newInLibraryScore, 3
		title, description = "Neu in der Mediathek", "In den letzten 30 Tagen hinzugekommen"
	default:
		return nil, nil
	}
	cands, err := b.candidates(ctx, kind, query)
	if err != nil {
		return nil, err
	}
	return newMix(kind, kind, title, description, composeMix(cands, score, jitter, b.now)), nil
}

// userGenres ranks genres by this user's affinity: plays (capped per track)
// and favorites count for a genre, recent skips against it.
func (b mixBuilder) userGenres(ctx context.Context) ([]string, error) {
	return b.genreList(ctx, `WITH sig AS (
 SELECT track_id,LEAST(play_count,20)::float8 AS w FROM listening_history WHERE user_id=$1
 UNION ALL SELECT track_id,5 FROM favorite_tracks WHERE user_id=$1
 UNION ALL SELECT track_id,-(3*LEAST(skip_count,4))::float8 FROM track_skips WHERE user_id=$1 AND last_skipped_at>now()-interval '90 days')
 SELECT min(btrim(g.name)),sum(sig.w)::float8 FROM sig JOIN tracks t ON t.id=sig.track_id JOIN artists a ON a.id=t.artist_id
 CROSS JOIN LATERAL jsonb_array_elements_text(a.genres_json) AS g(name)
 WHERE btrim(g.name)<>'' GROUP BY lower(btrim(g.name) COLLATE "pg_c_utf8") HAVING sum(sig.w)>0 ORDER BY 2 DESC,1 LIMIT 8`, b.user)
}

// libraryGenres ranks genres by the number of local tracks.
func (b mixBuilder) libraryGenres(ctx context.Context) ([]string, error) {
	return b.genreList(ctx, `WITH artist_tracks AS (SELECT t.artist_id,count(*) AS n FROM tracks t WHERE t.artist_id IS NOT NULL AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) GROUP BY t.artist_id)
 SELECT min(btrim(g.name)),sum(art.n)::float8 FROM artist_tracks art JOIN artists a ON a.id=art.artist_id
 CROSS JOIN LATERAL jsonb_array_elements_text(a.genres_json) AS g(name)
 WHERE btrim(g.name)<>'' GROUP BY lower(btrim(g.name) COLLATE "pg_c_utf8") ORDER BY 2 DESC,1 LIMIT 8`)
}

func (b mixBuilder) genreList(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := b.c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDB("mix genres", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		var weight float64
		if err := rows.Scan(&g, &weight); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// mixShuffleSQL is the radio's deterministic shuffle, seeded with $2.
const mixShuffleSQL = `encode(sha256(convert_to(jsonb_build_array($2::text,%s)::text,'UTF8')),'hex')`

// The candidate queries define cand(id,weight); $1 is the user, $2 the
// shuffle seed. Every pool is bounded, by the user's own history or by a
// LIMIT over the daily shuffle, so the work stays small on a large library.
var (
	// Liked: the user's played or favorited tracks of the genre (the 2000
	// strongest). New: up to 400 tracks of the genre heard at most once.
	genreCandidatesSQL = `genre_artists AS (SELECT a.id FROM artists a WHERE EXISTS(SELECT 1 FROM jsonb_array_elements_text(a.genres_json) g WHERE lower(btrim(g) COLLATE "pg_c_utf8")=lower(btrim($3) COLLATE "pg_c_utf8"))),
 mine AS (SELECT track_id,sum(w) AS w FROM (SELECT track_id,play_count::float8 AS w FROM listening_history WHERE user_id=$1 UNION ALL SELECT track_id,1000 FROM favorite_tracks WHERE user_id=$1) x GROUP BY track_id),
 cand AS ((SELECT t.id,0::float8 AS weight FROM mine JOIN tracks t ON t.id=mine.track_id WHERE t.artist_id IN(SELECT id FROM genre_artists) ORDER BY mine.w DESC,t.id LIMIT 2000)
 UNION ALL (SELECT t.id,0::float8 FROM tracks t LEFT JOIN listening_history h ON h.user_id=$1 AND h.track_id=t.id
  WHERE t.artist_id IN(SELECT id FROM genre_artists) AND COALESCE(h.play_count,0)<=1 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
  ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "t.id") + ` LIMIT 400))`
	// Played at least three times, not in the last 60 days.
	rediscoverCandidatesSQL = `cand AS (SELECT track_id AS id,0::float8 AS weight FROM listening_history WHERE user_id=$1 AND play_count>=3 AND last_played_at<now()-interval '60 days')`
	// Never played, by artists the user plays or favorited (600 per day).
	unheardCandidatesSQL = `mine_artists AS (SELECT DISTINCT t.artist_id FROM (SELECT track_id FROM listening_history WHERE user_id=$1 UNION SELECT track_id FROM favorite_tracks WHERE user_id=$1) m JOIN tracks t ON t.id=m.track_id WHERE t.artist_id IS NOT NULL),
 cand AS (SELECT t.id,0::float8 AS weight FROM tracks t WHERE t.artist_id IN(SELECT artist_id FROM mine_artists)
  AND NOT EXISTS(SELECT 1 FROM listening_history h WHERE h.user_id=$1 AND h.track_id=t.id) AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
  ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "t.id") + ` LIMIT 600)`
	// Played at least twice in the last 30 days; weight is that play count.
	onRepeatCandidatesSQL = `cand AS (SELECT track_id AS id,count(*)::float8 AS weight FROM playback_events WHERE user_id=$1 AND played_at>now()-interval '30 days' GROUP BY track_id HAVING count(*)>=2)`
	// First file added in the last 30 days (600 per day); weight is its age in days.
	newCandidatesSQL = `cand AS (SELECT f.track_id AS id,(extract(epoch FROM now()-min(f.created_at))/86400)::float8 AS weight FROM files f
  WHERE f.created_at>now()-interval '30 days' AND f.track_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM files o WHERE o.track_id=f.track_id AND o.created_at<=now()-interval '30 days')
  GROUP BY f.track_id ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "f.track_id") + ` LIMIT 600)`
)

// candidates runs a candidate query and attaches this user's signals.
func (b mixBuilder) candidates(ctx context.Context, mixID, candSQL string, extra ...any) ([]MixCandidate, error) {
	query := `WITH ` + candSQL + `,
 artist_affinity AS (SELECT t.artist_id,sum(x.w) AS affinity FROM (SELECT track_id,play_count::float8 AS w FROM listening_history WHERE user_id=$1 UNION ALL SELECT track_id,3 FROM favorite_tracks WHERE user_id=$1) x JOIN tracks t ON t.id=x.track_id WHERE t.artist_id IS NOT NULL GROUP BY t.artist_id)
 SELECT t.id,COALESCE(t.artist_id,''),lower(btrim(t.title)),COALESCE(t.release_id,''),COALESCE(h.play_count,0),h.last_played_at,fav.track_id IS NOT NULL,
 COALESCE(s.skip_count,0),s.last_skipped_at,COALESCE(aa.affinity,0),c.weight,` + fmt.Sprintf(mixShuffleSQL, "t.id") + `
 FROM (SELECT id,max(weight) AS weight FROM cand GROUP BY id) c JOIN tracks t ON t.id=c.id
 LEFT JOIN listening_history h ON h.user_id=$1 AND h.track_id=t.id
 LEFT JOIN favorite_tracks fav ON fav.user_id=$1 AND fav.track_id=t.id
 LEFT JOIN track_skips s ON s.user_id=$1 AND s.track_id=t.id
 LEFT JOIN artist_affinity aa ON aa.artist_id=t.artist_id
 WHERE EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)`
	args := append([]any{b.user, b.seed(mixID)}, extra...)
	rows, err := b.c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDB("mix candidates", err)
	}
	defer rows.Close()
	out := []MixCandidate{}
	for rows.Next() {
		var m MixCandidate
		var lastPlayed, lastSkipped sql.NullTime
		if err := rows.Scan(&m.ID, &m.ArtistID, &m.TitleKey, &m.ReleaseID, &m.Plays, &lastPlayed, &m.Favorite, &m.Skips, &lastSkipped, &m.ArtistAffinity, &m.Weight, &m.Shuffle); err != nil {
			return nil, err
		}
		m.LastPlayed, m.LastSkipped = lastPlayed.Time, lastSkipped.Time
		out = append(out, m)
	}
	return out, rows.Err()
}
