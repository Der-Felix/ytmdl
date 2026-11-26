package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// Mix kinds.
const (
	MixKindGenre              = "genre"
	MixKindOnRepeat           = "on_repeat"
	MixKindRediscover         = "rediscover"
	MixKindUnheard            = "unheard"
	MixKindNew                = "new"
	MixKindArtist             = "artist"
	MixKindDecade             = "decade"
	MixKindFavorites          = "favorites"
	MixKindForgottenFavorites = "forgotten_favorites"
	MixKindDaypart            = "daypart"
)

// Mix groups let a client lay the mixes out in sections.
const (
	MixGroupMadeForYou = "made_for_you"
	MixGroupDiscover   = "discover"
	MixGroupRewind     = "rewind"
)

// mixGroups assigns every kind to its section.
var mixGroups = map[string]string{
	MixKindGenre:              MixGroupMadeForYou,
	MixKindDaypart:            MixGroupMadeForYou,
	MixKindArtist:             MixGroupMadeForYou,
	MixKindFavorites:          MixGroupMadeForYou,
	MixKindOnRepeat:           MixGroupMadeForYou,
	MixKindUnheard:            MixGroupDiscover,
	MixKindNew:                MixGroupDiscover,
	MixKindRediscover:         MixGroupRewind,
	MixKindForgottenFavorites: MixGroupRewind,
	MixKindDecade:             MixGroupRewind,
}

const (
	// maxGenreMixes is the number of "Dein Mix" mixes offered.
	maxGenreMixes = 6
	// maxArtistMixes is the number of "Mix: <artist>" mixes offered.
	maxArtistMixes = 3
	// maxDecadeMixes is the number of decade mixes offered.
	maxDecadeMixes = 2
)

// PersonalMix describes one automatic mix of the current user.
type PersonalMix struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Group is the section the mix belongs to: made_for_you, discover or
	// rewind.
	Group string `json:"group,omitempty"`
	// Genre is set for genre mixes.
	Genre string `json:"genre,omitempty"`
	// ArtistID is set for artist mixes, Decade (e.g. 1990) for decade mixes
	// and Daypart (morning, afternoon, evening, night) for daypart mixes.
	ArtistID   string `json:"artist_id,omitempty"`
	Decade     int    `json:"decade,omitempty"`
	Daypart    string `json:"daypart,omitempty"`
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
// day seeds the daily shuffle, and its clock time and location pick the
// daypart mix (pass the current time in the user's time zone).
func (c *Catalog) PersonalMixesOn(ctx context.Context, userID string, day time.Time) ([]PersonalMix, error) {
	b, err := c.newMixBuilder(ctx, userID, day)
	if err != nil {
		return nil, err
	}
	one := func(m *PersonalMix, err error) ([]PersonalMix, error) {
		if err != nil || m == nil {
			return nil, err
		}
		return []PersonalMix{*m}, nil
	}
	kind := func(k string) func(context.Context) ([]PersonalMix, error) {
		return func(ctx context.Context) ([]PersonalMix, error) { return one(b.kindMix(ctx, k)) }
	}
	// The steps are independent; they run side by side on a few
	// connections and keep this order in the answer.
	steps := []func(context.Context) ([]PersonalMix, error){
		b.genreMixes,
		func(ctx context.Context) ([]PersonalMix, error) { return one(b.daypartMix(ctx, daypartAt(day))) },
		b.artistMixes,
		kind(MixKindFavorites),
		kind(MixKindOnRepeat),
		kind(MixKindRediscover),
		kind(MixKindForgottenFavorites),
		b.decadeMixes,
		kind(MixKindUnheard),
		kind(MixKindNew),
	}
	results := make([][]PersonalMix, len(steps))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(mixParallelism)
	for i, step := range steps {
		g.Go(func() error {
			ms, err := step(gctx)
			results[i] = ms
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	mixes := []PersonalMix{}
	for _, ms := range results {
		mixes = append(mixes, ms...)
	}
	return mixes, nil
}

// mixParallelism bounds the database connections one mix list uses at once.
const mixParallelism = 3

// PersonalMix returns one mix with its tracks.
func (c *Catalog) PersonalMix(ctx context.Context, userID, id string) (*PersonalMixDetail, error) {
	return c.PersonalMixOn(ctx, userID, id, time.Now())
}

// PersonalMixOn is PersonalMix as seeded for the calendar day of day.
func (c *Catalog) PersonalMixOn(ctx context.Context, userID, id string, day time.Time) (*PersonalMixDetail, error) {
	if len(id) > 100 {
		return nil, mixNotFound()
	}
	b, err := c.newMixBuilder(ctx, userID, day)
	if err != nil {
		return nil, err
	}
	var found *PersonalMix
	switch {
	case strings.HasPrefix(id, MixKindGenre+"-"):
		found, err = b.genreMixByID(ctx, id)
	case strings.HasPrefix(id, MixKindArtist+"-"):
		found, err = b.artistMixByID(ctx, id)
	case strings.HasPrefix(id, MixKindDecade+"-"):
		found, err = b.decadeMixByID(ctx, id)
	case strings.HasPrefix(id, MixKindDaypart+"-"):
		if d, ok := daypartByKey(strings.TrimPrefix(id, MixKindDaypart+"-")); ok {
			found, err = b.daypartMix(ctx, d)
		}
	default:
		switch id {
		case MixKindOnRepeat, MixKindRediscover, MixKindUnheard, MixKindNew, MixKindFavorites, MixKindForgottenFavorites:
			found, err = b.kindMix(ctx, id)
		}
	}
	if err != nil {
		return nil, err
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
	// clock is the request time in the user's time zone; it picks the
	// daypart and the weekday.
	clock time.Time
	now   time.Time
	excl  userExclusions
}

func (c *Catalog) newMixBuilder(ctx context.Context, userID string, day time.Time) (mixBuilder, error) {
	excl, err := c.loadExclusions(ctx, userID)
	if err != nil {
		return mixBuilder{}, err
	}
	return mixBuilder{c: c, user: userID, day: day.Format(time.DateOnly), clock: day, now: time.Now(), excl: excl}, nil
}

func (b mixBuilder) seed(mixID string) string {
	return b.user + "|" + b.day + "|" + mixID
}

func hashedMixID(kind, key string) string {
	sum := sha256.Sum256([]byte(key))
	return kind + "-" + hex.EncodeToString(sum[:6])
}

func genreMixID(genre string) string {
	return hashedMixID(MixKindGenre, strings.ToLower(strings.TrimSpace(genre)))
}

func artistMixID(artistID string) string {
	return hashedMixID(MixKindArtist, artistID)
}

func decadeMixID(decade int) string {
	return MixKindDecade + "-" + strconv.Itoa(decade)
}

func newMix(id, kind, title, description string, picked []MixCandidate) *PersonalMix {
	if len(picked) < MixMinTracks {
		return nil
	}
	m := &PersonalMix{ID: id, Kind: kind, Group: mixGroups[kind], Title: title, Description: description, TrackCount: len(picked)}
	for _, p := range picked {
		m.trackIDs = append(m.trackIDs, p.ID)
	}
	m.CoverTrackIDs, m.CoverReleaseIDs = mixCovers(picked)
	return m
}

// offerMixes builds the mixes of order in turn and keeps the first limit
// that reach MixMinTracks; n is the number the next offered mix gets.
func offerMixes[T any](order []T, limit int, build func(item T, n int) (*PersonalMix, error)) ([]PersonalMix, error) {
	mixes := []PersonalMix{}
	for _, item := range order {
		if len(mixes) == limit {
			break
		}
		m, err := build(item, len(mixes)+1)
		if err != nil {
			return nil, err
		}
		if m != nil {
			mixes = append(mixes, *m)
		}
	}
	return mixes, nil
}

// offeredMixByID builds the mix with this id exactly as offerMixes offers
// it, or nil when it is not offered. Its number, and whether it is offered
// at all, depend on which entries before it reach MixMinTracks, so those are
// composed (without loading their tracks); entries after it are not touched,
// and an id outside order costs no candidate query.
func offeredMixByID[T any](order []T, limit int, id string, idOf func(T) string, build func(item T, n int) (*PersonalMix, error)) (*PersonalMix, error) {
	target := slices.IndexFunc(order, func(item T) bool { return idOf(item) == id })
	if target < 0 {
		return nil, nil
	}
	offered, err := offerMixes(order[:target], limit, build)
	if err != nil || len(offered) == limit {
		return nil, err
	}
	return build(order[target], len(offered)+1)
}

// genreMixes builds up to maxGenreMixes genre mixes, trying the genres in
// genreOrder. A genre too small for a mix is passed over and does not take a
// number.
func (b mixBuilder) genreMixes(ctx context.Context) ([]PersonalMix, error) {
	order, err := b.genreOrder(ctx)
	if err != nil {
		return nil, err
	}
	return offerMixes(order, maxGenreMixes, func(g string, n int) (*PersonalMix, error) { return b.genreMix(ctx, g, n) })
}

func (b mixBuilder) genreMixByID(ctx context.Context, id string) (*PersonalMix, error) {
	order, err := b.genreOrder(ctx)
	if err != nil {
		return nil, err
	}
	return offeredMixByID(order, maxGenreMixes, id, genreMixID, func(g string, n int) (*PersonalMix, error) { return b.genreMix(ctx, g, n) })
}

// genreMix builds the mix of one genre as "Dein Mix n", or nil when the
// genre has too few tracks for a mix.
func (b mixBuilder) genreMix(ctx context.Context, genre string, n int) (*PersonalMix, error) {
	id := genreMixID(genre)
	cands, err := b.candidates(ctx, id, genreCandidatesSQL, genre)
	if err != nil {
		return nil, err
	}
	picked := composeGenreMix(cands, b.now)
	m := newMix(id, MixKindGenre, fmt.Sprintf("Dein Mix %d", n), genreMixDescription(genre, picked), picked)
	if m != nil {
		m.Genre = genre
	}
	return m, nil
}

// genreMixDescription names the user's favorites only when the mix has some;
// a cold-start mix is a plain selection from the library.
func genreMixDescription(genre string, picked []MixCandidate) string {
	if slices.ContainsFunc(picked, MixCandidate.Liked) {
		return "Deine Favoriten aus " + genre + " und Titel, die du kaum kennst"
	}
	return "Eine Auswahl aus " + genre + " in deiner Mediathek"
}

// genreOrder lists the genres genre mixes are tried in, without the genres
// the user excluded.
func (b mixBuilder) genreOrder(ctx context.Context) ([]string, error) {
	user, err := b.userGenres(ctx)
	if err != nil {
		return nil, err
	}
	var library []weightedGenre
	if countAffine(user) < maxGenreMixes {
		if library, err = b.libraryGenres(ctx); err != nil {
			return nil, err
		}
	}
	order := mixGenreOrder(user, library)
	return slices.DeleteFunc(order, func(g string) bool { return b.excl.genres[strings.ToLower(strings.TrimSpace(g))] }), nil
}

// weightedGenre is a genre with a weight: the user's affinity, or the
// library's track count.
type weightedGenre struct {
	Name   string
	Weight float64
}

// maxUserGenres bounds the user's own genres tried for mixes.
const maxUserGenres = 8

func countAffine(user []weightedGenre) int {
	n := 0
	for _, g := range user {
		if g.Weight > 0 {
			n++
		}
	}
	return min(n, maxUserGenres)
}

// mixGenreOrder orders genres for mixes: the user's genres with a positive
// affinity, strongest first (at most maxUserGenres), then the library's
// largest genres, which only fill in for a new user or too few such genres.
// A genre the user's affinity is negative for (mostly skipped) is never
// filled in from the library. Duplicates by mix id are dropped.
func mixGenreOrder(user, library []weightedGenre) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, g := range user {
		if g.Weight < 0 {
			seen[genreMixID(g.Name)] = true
		}
	}
	for _, g := range user {
		if g.Weight > 0 && len(out) < maxUserGenres && !seen[genreMixID(g.Name)] {
			seen[genreMixID(g.Name)] = true
			out = append(out, g.Name)
		}
	}
	for _, g := range library {
		if !seen[genreMixID(g.Name)] {
			seen[genreMixID(g.Name)] = true
			out = append(out, g.Name)
		}
	}
	return out
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
	case MixKindFavorites:
		query, score, jitter = favoritesCandidatesSQL, favoritesScore, 3
		title, description = "Favoriten-Mix", "Deine Favoriten, länger nicht Gehörtes zuerst"
	case MixKindForgottenFavorites:
		query, score, jitter = forgottenCandidatesSQL, forgottenScore, 3
		title, description = "Vergessene Favoriten", "Favoriten, die du seit Monaten nicht gehört hast"
	default:
		return nil, nil
	}
	cands, err := b.candidates(ctx, kind, query)
	if err != nil {
		return nil, err
	}
	return newMix(kind, kind, title, description, composeMix(cands, score, jitter, b.now)), nil
}

// topArtist is one of the user's most played artists.
type topArtist struct {
	ID, Name string
}

// maxTopArtists bounds the artists tried for artist mixes.
const maxTopArtists = 6

// topArtists ranks the user's artists by plays (capped per track) and
// favorites; an artist needs a weight of at least six (six plays or a
// favorite and a play) to count. Excluded artists are left out.
func (b mixBuilder) topArtists(ctx context.Context) ([]topArtist, error) {
	rows, err := b.c.db.QueryContext(ctx, `WITH `+exclusionCTE+`, sig AS (
 SELECT track_id,LEAST(play_count,20)::float8 AS w FROM listening_history WHERE user_id=$1
 UNION ALL SELECT track_id,5 FROM favorite_tracks WHERE user_id=$1)
 SELECT t.artist_id,min(a.name) FROM sig JOIN tracks t ON t.id=sig.track_id JOIN artists a ON a.id=t.artist_id
 WHERE NOT EXISTS(SELECT 1 FROM excluded_artists xa WHERE xa.artist_id=t.artist_id)
 GROUP BY t.artist_id HAVING sum(sig.w)>=6 ORDER BY sum(sig.w) DESC,t.artist_id LIMIT `+strconv.Itoa(maxTopArtists), b.user)
	if err != nil {
		return nil, wrapDB("mix artists", err)
	}
	defer rows.Close()
	out := []topArtist{}
	for rows.Next() {
		var a topArtist
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (b mixBuilder) artistMixes(ctx context.Context) ([]PersonalMix, error) {
	order, err := b.topArtists(ctx)
	if err != nil {
		return nil, err
	}
	return offerMixes(order, maxArtistMixes, func(a topArtist, _ int) (*PersonalMix, error) { return b.artistMix(ctx, a) })
}

func (b mixBuilder) artistMixByID(ctx context.Context, id string) (*PersonalMix, error) {
	order, err := b.topArtists(ctx)
	if err != nil {
		return nil, err
	}
	return offeredMixByID(order, maxArtistMixes, id, func(a topArtist) string { return artistMixID(a.ID) },
		func(a topArtist, _ int) (*PersonalMix, error) { return b.artistMix(ctx, a) })
}

// artistMix builds "Mix: <artist>": the artist and related artists.
func (b mixBuilder) artistMix(ctx context.Context, a topArtist) (*PersonalMix, error) {
	id := artistMixID(a.ID)
	cands, err := b.candidates(ctx, id, artistCandidatesSQL, a.ID)
	if err != nil {
		return nil, err
	}
	m := newMix(id, MixKindArtist, "Mix: "+a.Name, a.Name+" und ähnliche Künstler", composeArtistMix(cands, a.ID, b.now))
	if m != nil {
		m.ArtistID = a.ID
	}
	return m, nil
}

// userDecades lists the decades the user listens to, most played first: at
// least five different tracks of the decade were played.
func (b mixBuilder) userDecades(ctx context.Context) ([]int, error) {
	rows, err := b.c.db.QueryContext(ctx, `SELECT y.decade FROM (
 SELECT (COALESCE(NULLIF(t.year,0),r.year,0)/10)*10 AS decade,LEAST(h.play_count,20) AS w
 FROM listening_history h JOIN tracks t ON t.id=h.track_id LEFT JOIN releases r ON r.id=t.release_id WHERE h.user_id=$1) y
 WHERE y.decade BETWEEN 1900 AND 2100 GROUP BY y.decade HAVING count(*)>=5 ORDER BY sum(y.w) DESC,y.decade DESC LIMIT 4`, b.user)
	if err != nil {
		return nil, wrapDB("mix decades", err)
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (b mixBuilder) decadeMixes(ctx context.Context) ([]PersonalMix, error) {
	order, err := b.userDecades(ctx)
	if err != nil {
		return nil, err
	}
	return offerMixes(order, maxDecadeMixes, func(d int, _ int) (*PersonalMix, error) { return b.decadeMix(ctx, d) })
}

func (b mixBuilder) decadeMixByID(ctx context.Context, id string) (*PersonalMix, error) {
	order, err := b.userDecades(ctx)
	if err != nil {
		return nil, err
	}
	return offeredMixByID(order, maxDecadeMixes, id, decadeMixID, func(d int, _ int) (*PersonalMix, error) { return b.decadeMix(ctx, d) })
}

// decadeMix builds "Deine 90er": the user's tracks of the decade and tracks
// of the decade by their artists that they barely know.
func (b mixBuilder) decadeMix(ctx context.Context, decade int) (*PersonalMix, error) {
	id := decadeMixID(decade)
	cands, err := b.candidates(ctx, id, decadeCandidatesSQL, decade)
	if err != nil {
		return nil, err
	}
	description := fmt.Sprintf("Deine Titel aus den %der-Jahren und mehr aus dem Jahrzehnt", decade)
	m := newMix(id, MixKindDecade, decadeTitle(decade), description, composeGenreMix(cands, b.now))
	if m != nil {
		m.Decade = decade
	}
	return m, nil
}

// daypartMix builds the mix of what the user plays at daypart d, or nil when
// the last 120 days hold too few plays at that time of day.
func (b mixBuilder) daypartMix(ctx context.Context, d daypart) (*PersonalMix, error) {
	rows, err := b.c.db.QueryContext(ctx, `SELECT track_id,played_at FROM playback_events WHERE user_id=$1 AND played_at>now()-interval '120 days' ORDER BY played_at DESC LIMIT 20000`, b.user)
	if err != nil {
		return nil, wrapDB("daypart plays", err)
	}
	events := []playEvent{}
	for rows.Next() {
		var e playEvent
		if err := rows.Scan(&e.TrackID, &e.PlayedAt); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	weights := daypartWeights(events, d, b.clock)
	if weights == nil {
		return nil, nil
	}
	ids, ws := topWeights(weights, daypartPool)
	id := MixKindDaypart + "-" + d.Key
	cands, err := b.candidates(ctx, id, daypartCandidatesSQL, ids, ws)
	if err != nil {
		return nil, err
	}
	m := newMix(id, MixKindDaypart, d.Title, d.Description, composeMix(cands, daypartScore, 2, b.now))
	if m != nil {
		m.Daypart = d.Key
	}
	return m, nil
}

// userGenres ranks genres by this user's affinity: plays (capped per track)
// and favorites count for a genre, recent skips against it. Genres with a
// negative affinity are listed too, so the library fallback can leave them out.
func (b mixBuilder) userGenres(ctx context.Context) ([]weightedGenre, error) {
	return b.genreList(ctx, `WITH sig AS (
 SELECT track_id,LEAST(play_count,20)::float8 AS w FROM listening_history WHERE user_id=$1
 UNION ALL SELECT track_id,5 FROM favorite_tracks WHERE user_id=$1
 UNION ALL SELECT track_id,-(3*LEAST(skip_count,4))::float8 FROM track_skips WHERE user_id=$1 AND last_skipped_at>now()-interval '90 days')
 SELECT min(btrim(g.name)),sum(sig.w)::float8 FROM sig JOIN tracks t ON t.id=sig.track_id JOIN artists a ON a.id=t.artist_id
 CROSS JOIN LATERAL jsonb_array_elements_text(a.genres_json) AS g(name)
 WHERE btrim(g.name)<>'' GROUP BY lower(btrim(g.name) COLLATE "pg_c_utf8") HAVING sum(sig.w)<>0 ORDER BY 2 DESC,1`, b.user)
}

// libraryGenres ranks genres by the number of local tracks.
func (b mixBuilder) libraryGenres(ctx context.Context) ([]weightedGenre, error) {
	return b.genreList(ctx, `WITH artist_tracks AS (SELECT t.artist_id,count(*) AS n FROM tracks t WHERE t.artist_id IS NOT NULL AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) GROUP BY t.artist_id)
 SELECT min(btrim(g.name)),sum(art.n)::float8 FROM artist_tracks art JOIN artists a ON a.id=art.artist_id
 CROSS JOIN LATERAL jsonb_array_elements_text(a.genres_json) AS g(name)
 WHERE btrim(g.name)<>'' GROUP BY lower(btrim(g.name) COLLATE "pg_c_utf8") ORDER BY 2 DESC,1 LIMIT 10`)
}

func (b mixBuilder) genreList(ctx context.Context, query string, args ...any) ([]weightedGenre, error) {
	rows, err := b.c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDB("mix genres", err)
	}
	defer rows.Close()
	out := []weightedGenre{}
	for rows.Next() {
		var g weightedGenre
		if err := rows.Scan(&g.Name, &g.Weight); err != nil {
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
	// The user's favorites (2000 per day).
	favoritesCandidatesSQL = `cand AS (SELECT f.track_id AS id,0::float8 AS weight FROM favorite_tracks f WHERE f.user_id=$1 ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "f.track_id") + ` LIMIT 2000)`
	// Favorites older than 30 days, not played in the last 90 days.
	forgottenCandidatesSQL = `cand AS (SELECT f.track_id AS id,0::float8 AS weight FROM favorite_tracks f LEFT JOIN listening_history h ON h.user_id=$1 AND h.track_id=f.track_id
  WHERE f.user_id=$1 AND f.created_at<now()-interval '30 days' AND (h.last_played_at IS NULL OR h.last_played_at<now()-interval '90 days'))`
	// $3 is the artist. Its own tracks (300 per day, weight 1) and those of up
	// to 30 related artists (600 per day): artists whose genres include the
	// artist's main genre, those with it as their own main genre and those
	// the user plays first. An artist without genres has no related artists.
	artistCandidatesSQL = `seed_artist AS (SELECT a.id,lower(btrim(COALESCE(a.genres_json->>0,''))) AS primary_genre FROM artists a WHERE a.id=$3),
 played_artists AS (SELECT t.artist_id,sum(h.play_count) AS w FROM listening_history h JOIN tracks t ON t.id=h.track_id WHERE h.user_id=$1 AND t.artist_id IS NOT NULL GROUP BY t.artist_id),
 related AS (SELECT a.id FROM artists a CROSS JOIN seed_artist s LEFT JOIN played_artists p ON p.artist_id=a.id
  WHERE a.id<>s.id AND s.primary_genre<>'' AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(a.genres_json) g WHERE lower(btrim(g))=s.primary_genre)
  ORDER BY (lower(btrim(COALESCE(a.genres_json->>0,'')))=s.primary_genre) DESC,COALESCE(p.w,0) DESC,` + fmt.Sprintf(mixShuffleSQL, "a.id") + ` LIMIT 30),
 cand AS ((SELECT t.id,1::float8 AS weight FROM tracks t WHERE t.artist_id=$3 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "t.id") + ` LIMIT 300)
 UNION ALL (SELECT t.id,0::float8 FROM tracks t WHERE t.artist_id IN(SELECT id FROM related) AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "t.id") + ` LIMIT 600))`
	// $3 is the first year of the decade. Liked: the user's played or
	// favorited tracks of the decade (the 2000 strongest). New: up to 400
	// tracks of the decade heard at most once, by the user's 200 strongest
	// artists. The year is the track's, else its release's.
	decadeCandidatesSQL = `mine AS (SELECT track_id,sum(w) AS w FROM (SELECT track_id,play_count::float8 AS w FROM listening_history WHERE user_id=$1 UNION ALL SELECT track_id,1000 FROM favorite_tracks WHERE user_id=$1) x GROUP BY track_id),
 mine_artists AS (SELECT t.artist_id FROM mine JOIN tracks t ON t.id=mine.track_id WHERE t.artist_id IS NOT NULL GROUP BY t.artist_id ORDER BY sum(mine.w) DESC,t.artist_id LIMIT 200),
 cand AS ((SELECT t.id,0::float8 AS weight FROM mine JOIN tracks t ON t.id=mine.track_id LEFT JOIN releases r ON r.id=t.release_id
   WHERE COALESCE(NULLIF(t.year,0),r.year,0) BETWEEN $3::int AND $3::int+9 ORDER BY mine.w DESC,t.id LIMIT 2000)
 UNION ALL (SELECT t.id,0::float8 FROM tracks t LEFT JOIN releases r ON r.id=t.release_id LEFT JOIN listening_history h ON h.user_id=$1 AND h.track_id=t.id
  WHERE t.artist_id IN(SELECT artist_id FROM mine_artists) AND COALESCE(NULLIF(t.year,0),r.year,0) BETWEEN $3::int AND $3::int+9
   AND COALESCE(h.play_count,0)<=1 AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
  ORDER BY ` + fmt.Sprintf(mixShuffleSQL, "t.id") + ` LIMIT 400))`
	// $3 and $4 are the tracks played at this time of day and their weights.
	daypartCandidatesSQL = `cand AS (SELECT u.id,u.w AS weight FROM unnest($3::text[],$4::float8[]) AS u(id,w))`
)

// candidates runs a candidate query and attaches this user's signals.
// Excluded artists, genres and recordings never become candidates.
func (b mixBuilder) candidates(ctx context.Context, mixID, candSQL string, extra ...any) ([]MixCandidate, error) {
	query := `WITH ` + candSQL + `,
 ` + exclusionCTE + `,
 artist_affinity AS (SELECT t.artist_id,sum(x.w) AS affinity FROM (SELECT track_id,play_count::float8 AS w FROM listening_history WHERE user_id=$1 UNION ALL SELECT track_id,3 FROM favorite_tracks WHERE user_id=$1) x JOIN tracks t ON t.id=x.track_id WHERE t.artist_id IS NOT NULL GROUP BY t.artist_id)
 SELECT t.id,COALESCE(t.artist_id,''),lower(btrim(t.title)),COALESCE(t.release_id,''),COALESCE(h.play_count,0),h.last_played_at,fav.track_id IS NOT NULL,
 COALESCE(s.skip_count,0),s.last_skipped_at,COALESCE(aa.affinity,0),c.weight,` + fmt.Sprintf(mixShuffleSQL, "t.id") + `
 FROM (SELECT id,max(weight) AS weight FROM cand GROUP BY id) c JOIN tracks t ON t.id=c.id
 LEFT JOIN listening_history h ON h.user_id=$1 AND h.track_id=t.id
 LEFT JOIN favorite_tracks fav ON fav.user_id=$1 AND fav.track_id=t.id
 LEFT JOIN track_skips s ON s.user_id=$1 AND s.track_id=t.id
 LEFT JOIN artist_affinity aa ON aa.artist_id=t.artist_id
 WHERE EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id) AND ` + exclusionFilter("t")
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
