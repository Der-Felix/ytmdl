package repository

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// Listening statistics periods. Week, month and year are rolling windows
// ending now (7, 30 and 365 days); all covers the whole history.
const (
	StatsPeriodWeek  = "week"
	StatsPeriodMonth = "month"
	StatsPeriodYear  = "year"
	StatsPeriodAll   = "all"
)

// Sources of listening statistics.
const (
	// StatsSourceEvents counts timestamped plays (playback_events).
	StatsSourceEvents = "events"
	// StatsSourceAggregate counts the per-track play counters
	// (listening_history), which also hold plays without a timestamp.
	StatsSourceAggregate = "aggregate"
)

// MaxStatsLimit bounds every top list.
const MaxStatsLimit = 50

// ListeningStats summarizes what a user listened to in a period.
type ListeningStats struct {
	Period string `json:"period"`
	// From is the start of the period; nil for all.
	From *time.Time `json:"from"`
	To   time.Time  `json:"to"`
	// Since is the user's first timestamped play; nil without any.
	Since *time.Time `json:"since"`
	// Complete reports that timestamped plays cover the whole period, so
	// every figure is exact. For all it reports that no play lacks a
	// timestamp (then the activity series adds up to the totals).
	Complete bool   `json:"complete"`
	Source   string `json:"source"`
	TimeZone string `json:"time_zone"`

	Totals      ListeningTotals `json:"totals"`
	TopTracks   []StatsTrack    `json:"top_tracks"`
	TopArtists  []StatsArtist   `json:"top_artists"`
	TopReleases []StatsRelease  `json:"top_releases"`
	TopGenres   []StatsGenre    `json:"top_genres"`
	Activity    StatsActivity   `json:"activity"`
}

// ListeningTotals are the period's totals.
type ListeningTotals struct {
	Plays           int64 `json:"plays"`
	DistinctTracks  int64 `json:"distinct_tracks"`
	DistinctArtists int64 `json:"distinct_artists"`
	// ListeningMS is estimated: the duration of every played track, as if
	// each play ran to the end.
	ListeningMS int64 `json:"listening_ms"`
	// UntimedPlays (all only) are plays in the counters without a timestamp,
	// so they are missing from the activity series.
	UntimedPlays int64 `json:"untimed_plays"`
}

// StatsTrack is a top track.
type StatsTrack struct {
	Track music.LibraryTrack `json:"track"`
	Plays int64              `json:"plays"`
}

// StatsArtist is a top artist.
type StatsArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Plays          int64  `json:"plays"`
	DistinctTracks int64  `json:"distinct_tracks"`
}

// StatsRelease is a top release.
type StatsRelease struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistID   string `json:"artist_id,omitempty"`
	ArtistName string `json:"artist_name"`
	Plays      int64  `json:"plays"`
}

// StatsGenre is a top genre. A play counts for every genre of its artist.
type StatsGenre struct {
	Name  string `json:"name"`
	Plays int64  `json:"plays"`
}

// StatsActivity is the period's plays per day (week, month) or per month
// (year, all), in the requested time zone, from timestamped plays only.
type StatsActivity struct {
	Bucket string       `json:"bucket"`
	Points []StatsPoint `json:"points"`
}

// StatsPoint is one day or month; Start is its first day as YYYY-MM-DD.
type StatsPoint struct {
	Start       string `json:"start"`
	Plays       int64  `json:"plays"`
	ListeningMS int64  `json:"listening_ms"`
}

// ListeningStatsAt computes the statistics of period for the user, with now
// as the end and loc for days and months. loc must be a named zone that
// PostgreSQL knows too (the handler resolves it).
func (c *Catalog) ListeningStatsAt(ctx context.Context, userID, period string, limit int, now time.Time, loc *time.Location) (*ListeningStats, error) {
	var from time.Time
	switch period {
	case StatsPeriodWeek:
		from = now.AddDate(0, 0, -7)
	case StatsPeriodMonth:
		from = now.AddDate(0, 0, -30)
	case StatsPeriodYear:
		from = now.AddDate(0, 0, -365)
	case StatsPeriodAll:
	default:
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültiger Zeitraum (week, month, year oder all).")
	}
	limit = min(max(limit, 1), MaxStatsLimit)
	tz := loc.String()
	if tz == "Local" || tz == "" {
		tz, loc = "UTC", time.UTC
	}
	out := &ListeningStats{Period: period, To: now.UTC(), TimeZone: tz, Source: StatsSourceEvents,
		TopTracks: []StatsTrack{}, TopArtists: []StatsArtist{}, TopReleases: []StatsRelease{}, TopGenres: []StatsGenre{}}

	var since sql.NullTime
	if err := c.db.QueryRowContext(ctx, `SELECT min(played_at) FROM playback_events WHERE user_id=$1`, userID).Scan(&since); err != nil {
		return nil, wrapDB("stats since", err)
	}
	if since.Valid {
		s := since.Time.UTC()
		out.Since = &s
	}

	// plays(track_id,n) is the source every figure is computed from.
	var plays string
	args := []any{userID}
	if period == StatsPeriodAll {
		out.Source = StatsSourceAggregate
		plays = `plays AS (SELECT track_id,play_count::bigint AS n FROM listening_history WHERE user_id=$1)`
	} else {
		f := from.UTC()
		out.From = &f
		out.Complete = !since.Valid || !since.Time.After(from)
		plays = `plays AS (SELECT track_id,count(*)::bigint AS n FROM playback_events WHERE user_id=$1 AND played_at>=$2 AND played_at<=$3 GROUP BY track_id)`
		args = append(args, from, now)
	}
	with := `WITH ` + plays + ` `

	if err := c.db.QueryRowContext(ctx, with+`SELECT COALESCE(sum(p.n),0)::bigint,count(*),count(DISTINCT t.artist_id),COALESCE(sum(p.n*t.duration_ms),0)::bigint
 FROM plays p JOIN tracks t ON t.id=p.track_id`, args...).Scan(&out.Totals.Plays, &out.Totals.DistinctTracks, &out.Totals.DistinctArtists, &out.Totals.ListeningMS); err != nil {
		return nil, wrapDB("stats totals", err)
	}
	if period == StatsPeriodAll {
		var timed int64
		if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM playback_events WHERE user_id=$1`, userID).Scan(&timed); err != nil {
			return nil, wrapDB("stats timed plays", err)
		}
		out.Totals.UntimedPlays = max(out.Totals.Plays-timed, 0)
		out.Complete = out.Totals.UntimedPlays == 0
	}

	limitArg := len(args) + 1
	lim := `$` + strconv.Itoa(limitArg)
	qargs := append(append([]any{}, args...), limit)

	// Top tracks.
	rows, err := c.db.QueryContext(ctx, with+`SELECT p.track_id,p.n FROM plays p ORDER BY p.n DESC,p.track_id LIMIT `+lim, qargs...)
	if err != nil {
		return nil, wrapDB("stats top tracks", err)
	}
	ids, counts := []string{}, map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		counts[id] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tracks, err := c.TracksByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tracks {
		out.TopTracks = append(out.TopTracks, StatsTrack{Track: t, Plays: counts[t.ID]})
	}

	// Top artists.
	if err := c.scanRows(ctx, with+`SELECT t.artist_id,min(a.name),sum(p.n)::bigint,count(*) FROM plays p JOIN tracks t ON t.id=p.track_id JOIN artists a ON a.id=t.artist_id
 GROUP BY t.artist_id ORDER BY 3 DESC,1 LIMIT `+lim, qargs, func(r *sql.Rows) error {
		var a StatsArtist
		if err := r.Scan(&a.ID, &a.Name, &a.Plays, &a.DistinctTracks); err != nil {
			return err
		}
		out.TopArtists = append(out.TopArtists, a)
		return nil
	}); err != nil {
		return nil, wrapDB("stats top artists", err)
	}

	// Top releases.
	if err := c.scanRows(ctx, with+`SELECT r.id,min(r.title),COALESCE(min(r.artist_id),''),COALESCE(min(a.name),''),sum(p.n)::bigint FROM plays p JOIN tracks t ON t.id=p.track_id JOIN releases r ON r.id=t.release_id LEFT JOIN artists a ON a.id=r.artist_id
 GROUP BY r.id ORDER BY 5 DESC,1 LIMIT `+lim, qargs, func(r *sql.Rows) error {
		var rel StatsRelease
		if err := r.Scan(&rel.ID, &rel.Title, &rel.ArtistID, &rel.ArtistName, &rel.Plays); err != nil {
			return err
		}
		out.TopReleases = append(out.TopReleases, rel)
		return nil
	}); err != nil {
		return nil, wrapDB("stats top releases", err)
	}

	// Top genres.
	if err := c.scanRows(ctx, with+`SELECT min(btrim(g.name)),sum(p.n)::bigint FROM plays p JOIN tracks t ON t.id=p.track_id JOIN artists a ON a.id=t.artist_id
 CROSS JOIN LATERAL jsonb_array_elements_text(a.genres_json) AS g(name) WHERE btrim(g.name)<>''
 GROUP BY lower(btrim(g.name) COLLATE "pg_c_utf8") ORDER BY 2 DESC,1 LIMIT `+lim, qargs, func(r *sql.Rows) error {
		var g StatsGenre
		if err := r.Scan(&g.Name, &g.Plays); err != nil {
			return err
		}
		out.TopGenres = append(out.TopGenres, g)
		return nil
	}); err != nil {
		return nil, wrapDB("stats top genres", err)
	}

	// Activity from timestamped plays.
	bucket, start := "day", from
	if period == StatsPeriodYear || period == StatsPeriodAll {
		bucket = "month"
	}
	if period == StatsPeriodAll {
		start = now
		if since.Valid {
			start = since.Time
		}
	}
	points := map[string]StatsPoint{}
	if err := c.scanRows(ctx, `SELECT to_char(date_trunc('`+bucket+`',e.played_at AT TIME ZONE $2::text),'YYYY-MM-DD'),count(*),COALESCE(sum(t.duration_ms),0)::bigint
 FROM playback_events e JOIN tracks t ON t.id=e.track_id WHERE e.user_id=$1 AND e.played_at>=$3 AND e.played_at<=$4 GROUP BY 1`,
		[]any{userID, tz, start, now}, func(r *sql.Rows) error {
			var p StatsPoint
			if err := r.Scan(&p.Start, &p.Plays, &p.ListeningMS); err != nil {
				return err
			}
			points[p.Start] = p
			return nil
		}); err != nil {
		return nil, wrapDB("stats activity", err)
	}
	out.Activity = StatsActivity{Bucket: bucket, Points: activitySeries(points, bucket, start.In(loc), now.In(loc))}
	return out, nil
}

// activitySeries lists every day or month from first to last (local dates),
// with zero for the ones without plays.
func activitySeries(points map[string]StatsPoint, bucket string, first, last time.Time) []StatsPoint {
	out := []StatsPoint{}
	cur := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	if bucket == "month" {
		cur = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	for !cur.After(end) && len(out) < 1000 {
		key := cur.Format(time.DateOnly)
		p, ok := points[key]
		if !ok {
			p = StatsPoint{Start: key}
		}
		out = append(out, p)
		if bucket == "month" {
			cur = cur.AddDate(0, 1, 0)
		} else {
			cur = cur.AddDate(0, 0, 1)
		}
	}
	return out
}

func (c *Catalog) scanRows(ctx context.Context, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
