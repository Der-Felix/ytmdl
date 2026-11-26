package repository

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"time"
)

// Personal mixes are picked in two steps: SQL gathers a bounded pool of
// candidate tracks with this user's signals, and the pure functions in this
// file score, cap and order them. Keeping the second step free of the
// database makes the rules unit-testable.

const (
	// MixTrackLimit is the most tracks a mix holds.
	MixTrackLimit = 50
	// MixMinTracks is the fewest tracks a mix needs to be offered at all.
	MixMinTracks = 10
	// mixArtistCap keeps a mix varied: at most this many tracks per artist.
	mixArtistCap = 3
	// genreLikedShare of a genre mix are tracks the user demonstrably likes;
	// the rest are tracks of that genre heard rarely or never.
	genreLikedShare = 30
	// skipExcludePenalty drops a track from every mix: two skips in the last
	// week, or four within the decay window.
	skipExcludePenalty = 10
)

const (
	skipWindow = 90 * 24 * time.Hour
	skipRecent = 7 * 24 * time.Hour
)

// skipPenaltySQL is SkipPenalty for a query that joins track_skips as s.
const skipPenaltySQL = `CASE WHEN s.last_skipped_at>now()-interval '90 days' THEN 3*LEAST(s.skip_count,4)+CASE WHEN s.last_skipped_at>now()-interval '7 days' THEN 4 ELSE 0 END ELSE 0 END`

// SkipPenalty weighs how strongly skips speak against a track. Skips decay:
// once the last one is older than 90 days they no longer count. Each skip
// adds 3 (counted up to four), and a skip within the last week adds 4 more.
// One fresh skip therefore costs 7, repeated fresh skips 10 to 16.
func SkipPenalty(count int64, lastSkipped, now time.Time) float64 {
	if count <= 0 || lastSkipped.IsZero() || now.Sub(lastSkipped) >= skipWindow {
		return 0
	}
	p := 3 * float64(min(count, 4))
	if now.Sub(lastSkipped) < skipRecent {
		p += 4
	}
	return p
}

// MixCandidate is one track that may enter a mix, with this user's signals.
type MixCandidate struct {
	ID        string
	ArtistID  string
	TitleKey  string
	ReleaseID string
	Plays     int64
	// LastPlayed and LastSkipped are zero when the user never played or
	// skipped the track.
	LastPlayed  time.Time
	Favorite    bool
	Skips       int64
	LastSkipped time.Time
	// ArtistAffinity sums the user's plays of the artist plus 3 per favorite.
	ArtistAffinity float64
	// Weight is a kind-specific signal: plays in the last 30 days for
	// "on repeat", the age in days for "new in the library".
	Weight float64
	// Shuffle is a hex sha256 seeded with user, day and mix, so the pick is
	// stable for a day and changes the next.
	Shuffle string
}

func (c MixCandidate) skipPenalty(now time.Time) float64 {
	return SkipPenalty(c.Skips, c.LastSkipped, now)
}

func (c MixCandidate) excluded(now time.Time) bool {
	return !c.Favorite && c.skipPenalty(now) >= skipExcludePenalty
}

// Liked reports whether the user demonstrably likes the track: a favorite,
// played repeatedly, or played and never skipped.
func (c MixCandidate) Liked() bool {
	return c.Favorite || c.Plays >= 2 || (c.Plays >= 1 && c.Skips == 0)
}

// shuffleUnit maps the hex hash to [0,1).
func shuffleUnit(hash string) float64 {
	if len(hash) > 13 {
		hash = hash[:13]
	}
	v, err := strconv.ParseUint(hash, 16, 64)
	if err != nil {
		return 0
	}
	return float64(v) / float64(uint64(1)<<(4*len(hash)))
}

func recentlyPlayed(c MixCandidate, now time.Time) bool {
	return !c.LastPlayed.IsZero() && now.Sub(c.LastPlayed) < 24*time.Hour
}

func favoriteBonus(c MixCandidate, bonus float64) float64 {
	if c.Favorite {
		return bonus
	}
	return 0
}

// Scores per mix kind. Each subtracts the skip penalty; the jitter added in
// pickMix lets the order move from day to day.
func likedScore(c MixCandidate, now time.Time) float64 {
	s := 3*math.Log1p(float64(c.Plays)) + favoriteBonus(c, 5) - c.skipPenalty(now)
	if recentlyPlayed(c, now) {
		s -= 3
	}
	return s
}

func discoveryScore(c MixCandidate, now time.Time) float64 {
	return math.Min(2*math.Log1p(c.ArtistAffinity), 6) + favoriteBonus(c, 2) - c.skipPenalty(now)
}

func rediscoverScore(c MixCandidate, now time.Time) float64 {
	return 3*math.Log1p(float64(c.Plays)) + favoriteBonus(c, 3) - c.skipPenalty(now)
}

func onRepeatScore(c MixCandidate, now time.Time) float64 {
	return 2*c.Weight + favoriteBonus(c, 1) - c.skipPenalty(now)
}

func newInLibraryScore(c MixCandidate, now time.Time) float64 {
	return math.Log1p(c.ArtistAffinity) + favoriteBonus(c, 2) - c.Weight/10 - c.skipPenalty(now)
}

// mixUsage enforces the rules that hold across a whole mix: one version of a
// recording (artist and normalized title), at most mixArtistCap tracks per
// artist, each track once.
type mixUsage struct {
	ids        map[string]bool
	recordings map[string]bool
	artists    map[string]int
	// caps overrides mixArtistCap for single artists (the artist of an
	// artist mix).
	caps map[string]int
}

func newMixUsage() *mixUsage {
	return &mixUsage{ids: map[string]bool{}, recordings: map[string]bool{}, artists: map[string]int{}, caps: map[string]int{}}
}

func (u *mixUsage) take(c MixCandidate) bool {
	key := c.ArtistID + "\x00" + c.TitleKey
	limit := mixArtistCap
	if n, ok := u.caps[c.ArtistID]; ok {
		limit = n
	}
	if u.ids[c.ID] || u.recordings[key] || u.artists[c.ArtistID] >= limit {
		return false
	}
	u.ids[c.ID], u.recordings[key] = true, true
	u.artists[c.ArtistID]++
	return true
}

// pickMix orders the candidates by score plus a daily jitter and takes up to
// limit of them that the usage rules allow.
func pickMix(cands []MixCandidate, score func(MixCandidate, time.Time) float64, jitter float64, limit int, usage *mixUsage, now time.Time) []MixCandidate {
	type ranked struct {
		c   MixCandidate
		key float64
	}
	list := make([]ranked, 0, len(cands))
	for _, c := range cands {
		if c.excluded(now) {
			continue
		}
		list = append(list, ranked{c, score(c, now) + jitter*shuffleUnit(c.Shuffle)})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].key != list[j].key {
			return list[i].key > list[j].key
		}
		if list[i].c.Shuffle != list[j].c.Shuffle {
			return list[i].c.Shuffle < list[j].c.Shuffle
		}
		return list[i].c.ID < list[j].c.ID
	})
	out := []MixCandidate{}
	for _, r := range list {
		if len(out) >= limit {
			break
		}
		if usage.take(r.c) {
			out = append(out, r.c)
		}
	}
	return out
}

// composeGenreMix fills about 60 % of a genre mix with tracks the user likes
// and the rest with tracks of the genre heard at most once. When one side is
// short the other fills the gap. Both are woven together, three liked to two
// new, so the discoveries are spread over the mix.
func composeGenreMix(cands []MixCandidate, now time.Time) []MixCandidate {
	liked, fresh := []MixCandidate{}, []MixCandidate{}
	for _, c := range cands {
		if c.Liked() {
			liked = append(liked, c)
		} else if c.Plays <= 1 {
			fresh = append(fresh, c)
		}
	}
	usage := newMixUsage()
	likedPick := pickMix(liked, likedScore, 4, genreLikedShare, usage, now)
	freshPick := pickMix(fresh, discoveryScore, 6, MixTrackLimit-len(likedPick), usage, now)
	if rest := MixTrackLimit - len(likedPick) - len(freshPick); rest > 0 {
		likedPick = append(likedPick, pickMix(liked, likedScore, 4, rest, usage, now)...)
	}
	out := make([]MixCandidate, 0, len(likedPick)+len(freshPick))
	for i, l, f := 0, 0, 0; l < len(likedPick) || f < len(freshPick); i++ {
		wantLiked := i%5 == 0 || i%5 == 2 || i%5 == 4
		if (wantLiked && l < len(likedPick)) || f >= len(freshPick) {
			out = append(out, likedPick[l])
			l++
		} else {
			out = append(out, freshPick[f])
			f++
		}
	}
	return out
}

// weave merges two picks so that b is spread evenly over a: at every step the
// list that is further behind its share goes next.
func weave(a, b []MixCandidate) []MixCandidate {
	out := make([]MixCandidate, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		// Compare i/len(a) with j/len(b) without dividing.
		if j >= len(b) || (i < len(a) && i*len(b) <= j*len(a)) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	return out
}

const (
	// artistMixSeedShare is how many tracks of an artist mix come from the
	// artist itself; the rest are related artists, at most mixArtistCap each.
	artistMixSeedShare = 20
	// artistMixSeedCap bounds the artist's own tracks when related artists
	// cannot fill the mix.
	artistMixSeedCap = 35
)

// artistSeedScore ranks the artist's own tracks: what the user plays and
// likes first, with room for the rest through the jitter.
func artistSeedScore(c MixCandidate, now time.Time) float64 {
	return 2*math.Log1p(float64(c.Plays)) + favoriteBonus(c, 3) - c.skipPenalty(now) - recentPenalty(c, now)
}

func recentPenalty(c MixCandidate, now time.Time) float64 {
	if recentlyPlayed(c, now) {
		return 3
	}
	return 0
}

// relatedScore ranks tracks of related artists: artists the user knows and
// tracks the user likes first.
func relatedScore(c MixCandidate, now time.Time) float64 {
	return discoveryScore(c, now) + math.Log1p(float64(c.Plays)) - recentPenalty(c, now)
}

// composeArtistMix builds "Mix: <artist>": candidates with Weight 1 are the
// artist's own tracks, the others belong to related artists. About 20 of the
// 50 tracks are the artist's; when related artists are short, more of the
// artist's own fill in (up to artistMixSeedCap).
func composeArtistMix(cands []MixCandidate, artistID string, now time.Time) []MixCandidate {
	seed, related := []MixCandidate{}, []MixCandidate{}
	for _, c := range cands {
		if c.Weight == 1 && c.ArtistID == artistID {
			seed = append(seed, c)
		} else if c.ArtistID != artistID {
			related = append(related, c)
		}
	}
	usage := newMixUsage()
	usage.caps[artistID] = artistMixSeedCap
	seedPick := pickMix(seed, artistSeedScore, 4, artistMixSeedShare, usage, now)
	relatedPick := pickMix(related, relatedScore, 5, MixTrackLimit-len(seedPick), usage, now)
	if rest := MixTrackLimit - len(seedPick) - len(relatedPick); rest > 0 {
		seedPick = append(seedPick, pickMix(seed, artistSeedScore, 4, rest, usage, now)...)
	}
	return weave(seedPick, relatedPick)
}

// favoritesScore prefers favorites the user has not played for a while.
func favoritesScore(c MixCandidate, now time.Time) float64 {
	days := 180.0
	if !c.LastPlayed.IsZero() {
		days = math.Max(now.Sub(c.LastPlayed).Hours()/24, 0)
	}
	return 2*math.Log1p(days) - c.skipPenalty(now)
}

// forgottenScore ranks favorites not heard for months: the more they were
// played back then, the higher.
func forgottenScore(c MixCandidate, now time.Time) float64 {
	return 2*math.Log1p(float64(c.Plays)) - c.skipPenalty(now)
}

// daypartScore ranks what the user plays at this time of day; Weight holds
// those plays (weighted by weekday or weekend).
func daypartScore(c MixCandidate, now time.Time) float64 {
	return 3*math.Log1p(c.Weight) + favoriteBonus(c, 1) - c.skipPenalty(now) - recentPenalty(c, now)
}

// daypart is a time of day a mix can be made for. Hours are local to the
// user's time zone; night wraps around midnight.
type daypart struct {
	Key, Title, Description string
	From, To                int
}

var dayparts = []daypart{
	{"morning", "Dein Morgen-Mix", "Was du morgens gern hörst", 5, 11},
	{"afternoon", "Dein Nachmittags-Mix", "Was du nachmittags gern hörst", 11, 17},
	{"evening", "Dein Abend-Mix", "Was du abends gern hörst", 17, 22},
	{"night", "Dein Nacht-Mix", "Was du nachts gern hörst", 22, 5},
}

func (d daypart) contains(hour int) bool {
	if d.From < d.To {
		return hour >= d.From && hour < d.To
	}
	return hour >= d.From || hour < d.To
}

// daypartAt is the daypart of t in t's location.
func daypartAt(t time.Time) daypart {
	for _, d := range dayparts {
		if d.contains(t.Hour()) {
			return d
		}
	}
	return dayparts[0]
}

func daypartByKey(key string) (daypart, bool) {
	for _, d := range dayparts {
		if d.Key == key {
			return d, true
		}
	}
	return daypart{}, false
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// playEvent is one play with its time.
type playEvent struct {
	TrackID  string
	PlayedAt time.Time
}

const (
	// daypartMinPlays is the fewest plays in a daypart before a mix is made
	// for it; below that the history says too little about the time of day.
	daypartMinPlays = 20
	// daypartPool bounds the tracks a daypart mix is picked from.
	daypartPool = 500
)

// daypartWeights counts the plays of each track that fell into d (in loc).
// A play on the same kind of day as now (weekday or weekend) counts 1.5, so
// a Saturday morning leans on Saturday and Sunday mornings. It returns nil
// when fewer than daypartMinPlays plays fell into d.
func daypartWeights(events []playEvent, d daypart, now time.Time) map[string]float64 {
	loc := now.Location()
	weekend := isWeekend(now)
	out := map[string]float64{}
	plays := 0
	for _, e := range events {
		local := e.PlayedAt.In(loc)
		if !d.contains(local.Hour()) {
			continue
		}
		plays++
		w := 1.0
		if isWeekend(local) == weekend {
			w = 1.5
		}
		out[e.TrackID] += w
	}
	if plays < daypartMinPlays {
		return nil
	}
	return out
}

// topWeights returns the ids and weights of the n heaviest entries, heaviest
// first, ties by id.
func topWeights(weights map[string]float64, n int) ([]string, []float64) {
	ids := make([]string, 0, len(weights))
	for id := range weights {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if weights[ids[i]] != weights[ids[j]] {
			return weights[ids[i]] > weights[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > n {
		ids = ids[:n]
	}
	ws := make([]float64, len(ids))
	for i, id := range ids {
		ws[i] = weights[id]
	}
	return ids, ws
}

// decadeTitle names a decade mix: "Deine 90er", "Deine 2000er".
func decadeTitle(decade int) string {
	if decade > 1900 && decade < 2000 {
		return "Deine " + strconv.Itoa(decade%100) + "er"
	}
	return "Deine " + strconv.Itoa(decade) + "er"
}

// composeMix picks a single-pool mix.
func composeMix(cands []MixCandidate, score func(MixCandidate, time.Time) float64, jitter float64, now time.Time) []MixCandidate {
	return pickMix(cands, score, jitter, MixTrackLimit, newMixUsage(), now)
}

// mixCovers picks up to four tracks for a cover collage, preferring
// different releases, and the distinct releases among them.
func mixCovers(tracks []MixCandidate) (trackIDs, releaseIDs []string) {
	trackIDs, releaseIDs = []string{}, []string{}
	seen := map[string]bool{}
	for _, t := range tracks {
		if len(trackIDs) == 4 {
			break
		}
		if t.ReleaseID != "" && !seen[t.ReleaseID] {
			seen[t.ReleaseID] = true
			trackIDs = append(trackIDs, t.ID)
			releaseIDs = append(releaseIDs, t.ReleaseID)
		}
	}
	for _, t := range tracks {
		if len(trackIDs) == 4 {
			break
		}
		if !slices.Contains(trackIDs, t.ID) {
			trackIDs = append(trackIDs, t.ID)
		}
	}
	return trackIDs, releaseIDs
}
