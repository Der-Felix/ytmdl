package matcher

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// Weights of the individual scoring components. They sum to one; when a
// component cannot be evaluated its weight is redistributed over the others so
// that a candidate is never punished for missing optional metadata.
const (
	weightTitle    = 0.40
	weightArtist   = 0.30
	weightDuration = 0.20
	weightAlbum    = 0.10
)

// Penalties applied after the weighted components have been combined.
const (
	// firstVersionPenalty is subtracted when the wanted and the offered
	// rendition differ in any version marker at all.
	firstVersionPenalty = 35
	// extraVersionPenalty is subtracted for every further differing marker.
	extraVersionPenalty = 15
	// isrcMismatchFactor scales the score down when both sides carry an ISRC
	// and the two disagree: that is strong evidence for a different recording.
	isrcMismatchFactor = 0.5
)

// Breakdown records how a score came about. It is attached to every result so
// that matching decisions stay explainable in logs and tests.
type Breakdown struct {
	Title           float64 `json:"title"`
	Artist          float64 `json:"artist"`
	Duration        float64 `json:"duration"`
	Album           float64 `json:"album"`
	Base            float64 `json:"base"`
	VersionPenalty  float64 `json:"version_penalty"`
	ISRCMatch       bool    `json:"isrc_match"`
	ISRCMismatch    bool    `json:"isrc_mismatch"`
	WantedVersions  string  `json:"wanted_versions,omitempty"`
	OfferedVersions string  `json:"offered_versions,omitempty"`
}

// Result is a scored candidate.
type Result struct {
	Candidate provider.MediaCandidate `json:"candidate"`
	Score     float64                 `json:"score"`
	Breakdown Breakdown               `json:"breakdown"`
}

// Reason renders a short human readable explanation of the score.
func (r Result) Reason() string {
	switch {
	case r.Breakdown.ISRCMatch:
		return "ISRC match"
	case r.Breakdown.ISRCMismatch:
		return "ISRC mismatch"
	case r.Breakdown.VersionPenalty > 0:
		return fmt.Sprintf("version mismatch (wanted %q, offered %q)",
			r.Breakdown.WantedVersions, r.Breakdown.OfferedVersions)
	default:
		return "metadata similarity"
	}
}

// Matcher scores media candidates against a wanted track. The acceptance
// threshold can be changed while the server runs, so it is held atomically.
type Matcher struct {
	minScore            atomic.Uint64
	durationToleranceMS int
}

// Options configures a Matcher.
type Options struct {
	MinScore            float64
	DurationToleranceMS int
}

// New builds a Matcher. Values outside the sensible range fall back to the
// defaults so that a Matcher is always usable.
func New(opts Options) *Matcher {
	m := &Matcher{durationToleranceMS: opts.DurationToleranceMS}
	if !m.SetMinScore(opts.MinScore) {
		m.SetMinScore(DefaultMinScore)
	}
	if m.durationToleranceMS <= 0 {
		m.durationToleranceMS = 4000
	}
	return m
}

// DefaultMinScore is the acceptance threshold used when none is configured.
const DefaultMinScore = 70

// MinScore returns the current acceptance threshold.
func (m *Matcher) MinScore() float64 {
	return math.Float64frombits(m.minScore.Load())
}

// SetMinScore changes the acceptance threshold. It reports whether the value
// was accepted; values outside 0 to 100 are rejected and leave the threshold
// unchanged.
func (m *Matcher) SetMinScore(score float64) bool {
	if score <= 0 || score > 100 {
		return false
	}
	m.minScore.Store(math.Float64bits(score))
	return true
}

// DurationToleranceMS returns the configured runtime tolerance.
func (m *Matcher) DurationToleranceMS() int { return m.durationToleranceMS }

// Score rates a single candidate against the wanted track.
func (m *Matcher) Score(track music.Track, candidate provider.MediaCandidate) Result {
	wantInfo := Analyze(track.Title)
	haveInfo := Analyze(candidate.Title)

	var bd Breakdown
	bd.WantedVersions = wantInfo.Versions.String()
	bd.OfferedVersions = haveInfo.Versions.String()

	// An ISRC identifies the exact recording; nothing else can outweigh it.
	wantISRC := normaliseISRC(track.ISRC)
	haveISRC := normaliseISRC(candidate.ISRC)
	if wantISRC != "" && haveISRC != "" {
		if wantISRC == haveISRC {
			bd.ISRCMatch = true
			bd.Title, bd.Artist, bd.Duration, bd.Album = 1, 1, 1, 1
			bd.Base = 100
			return Result{Candidate: candidate, Score: 100, Breakdown: bd}
		}
		bd.ISRCMismatch = true
	}

	weights := 0.0
	total := 0.0

	bd.Title = Similarity(wantInfo.Base, haveInfo.Base)
	total += weightTitle * bd.Title
	weights += weightTitle

	bd.Artist = artistSimilarity(track.Artists, candidateArtists(candidate))
	total += weightArtist * bd.Artist
	weights += weightArtist

	if track.DurationMS > 0 && candidate.DurationMS > 0 {
		bd.Duration = durationScore(track.DurationMS, candidate.DurationMS, m.durationToleranceMS)
		total += weightDuration * bd.Duration
		weights += weightDuration
	}

	if wantedAlbum := NormalizeTitle(track.Album); wantedAlbum != "" {
		if haveAlbum := NormalizeTitle(candidate.Album); haveAlbum != "" {
			bd.Album = Similarity(wantedAlbum, haveAlbum)
			total += weightAlbum * bd.Album
			weights += weightAlbum
		}
	}

	if weights == 0 {
		return Result{Candidate: candidate, Breakdown: bd}
	}
	score := 100 * total / weights
	bd.Base = score

	if diff := wantInfo.Versions ^ haveInfo.Versions; diff != 0 {
		count := popcount(uint32(diff))
		bd.VersionPenalty = firstVersionPenalty + float64(count-1)*extraVersionPenalty
		score -= bd.VersionPenalty
	}
	if bd.ISRCMismatch {
		score *= isrcMismatchFactor
	}

	return Result{Candidate: candidate, Score: clampScore(score), Breakdown: bd}
}

// Rank scores every candidate and returns them sorted by descending score.
func (m *Matcher) Rank(track music.Track, candidates []provider.MediaCandidate) []Result {
	results := make([]Result, 0, len(candidates))
	for _, c := range candidates {
		results = append(results, m.Score(track, c))
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Prefer a dedicated music catalogue on a tie.
		if results[i].Candidate.IsMusicService != results[j].Candidate.IsMusicService {
			return results[i].Candidate.IsMusicService
		}
		return results[i].Candidate.ID < results[j].Candidate.ID
	})
	return results
}

// Best returns the highest scoring candidate that reaches the configured
// threshold. When no candidate does, MATCH_FAILED is returned rather than an
// arbitrary result.
func (m *Matcher) Best(track music.Track, candidates []provider.MediaCandidate) (Result, error) {
	ranked := m.Rank(track, candidates)
	if len(ranked) == 0 {
		return Result{}, apperr.Newf(apperr.CodeMatchFailed,
			"No media candidates found for %q.", track.Label())
	}
	best := ranked[0]
	if threshold := m.MinScore(); best.Score < threshold {
		return best, apperr.Newf(apperr.CodeMatchFailed,
			"No sufficiently accurate media match found for %q (best score %.1f, required %.1f).",
			track.Label(), best.Score, threshold)
	}
	return best, nil
}

// candidateArtists returns the artist credits of a candidate, falling back to
// the uploader when the platform delivers no structured credit.
func candidateArtists(c provider.MediaCandidate) []string {
	cleaned := make([]string, 0, len(c.Artists))
	for _, a := range c.Artists {
		if strings.TrimSpace(a) != "" {
			cleaned = append(cleaned, a)
		}
	}
	if len(cleaned) > 0 {
		return cleaned
	}
	if u := strings.TrimSpace(c.Uploader); u != "" {
		return []string{stripChannelSuffix(u)}
	}
	return nil
}

var channelSuffixes = []string{" - topic", "vevo", " official", "officialchannel"}

// stripChannelSuffix removes the decorations platforms add to channel names so
// that "Artist - Topic" compares equal to "Artist".
func stripChannelSuffix(name string) string {
	lower := strings.ToLower(name)
	for _, suffix := range channelSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(name[:len(name)-len(suffix)])
		}
	}
	return name
}

func normaliseISRC(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) != 12 {
		return ""
	}
	return out
}

func popcount(v uint32) int {
	var n int
	for v != 0 {
		v &= v - 1
		n++
	}
	return n
}
