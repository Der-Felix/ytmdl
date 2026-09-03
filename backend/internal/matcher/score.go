package matcher

import (
	"sort"
	"strings"
)

// levenshtein returns the edit distance between two rune slices.
func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			curr[j] = best
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// editRatio is the Levenshtein similarity of two strings in [0,1].
func editRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	maxLen := len(ra)
	if len(rb) > maxLen {
		maxLen = len(rb)
	}
	if maxLen == 0 {
		return 1
	}
	dist := levenshtein(ra, rb)
	return 1 - float64(dist)/float64(maxLen)
}

// tokenRatio compares two strings word by word. Every word of one side is
// matched against its most similar word on the other side and the two
// directions are averaged. Compared to a plain set intersection this tolerates
// spelling differences and words that were split apart ("dont" vs "don t")
// without treating a longer title as equal to a shorter one.
func tokenRatio(a, b string) float64 {
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1
	}
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	return (bestMatchAverage(ta, tb) + bestMatchAverage(tb, ta)) / 2
}

// bestMatchAverage averages, over every token in from, the similarity to its
// closest token in to.
func bestMatchAverage(from, to []string) float64 {
	var total float64
	for _, f := range from {
		var best float64
		for _, t := range to {
			if r := editRatio(f, t); r > best {
				best = r
			}
		}
		total += best
	}
	return total / float64(len(from))
}

// Similarity combines edit distance and token overlap into a single score in
// [0,1]. Both inputs are expected to be normalised keys.
func Similarity(a, b string) float64 {
	if a == b {
		if a == "" {
			return 0
		}
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	return 0.6*clamp01(editRatio(a, b)) + 0.4*tokenRatio(a, b)
}

// artistSimilarity compares two artist credit lists. A candidate that credits
// at least the primary wanted artist scores high even when it lists additional
// collaborators, because media platforms frequently add them.
func artistSimilarity(wanted, candidate []string) float64 {
	wantKeys := artistKeys(wanted)
	candKeys := artistKeys(candidate)
	if len(wantKeys) == 0 || len(candKeys) == 0 {
		return 0
	}

	// Best pairwise similarity for every wanted artist.
	var total float64
	for _, w := range wantKeys {
		var best float64
		for _, c := range candKeys {
			if s := Similarity(w, c); s > best {
				best = s
			}
		}
		total += best
	}
	coverage := total / float64(len(wantKeys))

	// A candidate crediting far more artists than wanted is slightly less
	// certain, but never below the coverage of the primary artist.
	primary := 0.0
	for _, c := range candKeys {
		if s := Similarity(wantKeys[0], c); s > primary {
			primary = s
		}
	}
	if primary > coverage {
		coverage = 0.5*coverage + 0.5*primary
	}

	// Platforms disagree about where a credit ends: "Simon & Garfunkel" may
	// arrive as one name or as two. Comparing the whole credit as a single
	// string catches that case, and the better of the two readings wins.
	if joined := Similarity(joinKeys(wantKeys), joinKeys(candKeys)); joined > coverage {
		coverage = joined
	}
	return clamp01(coverage)
}

// joinKeys renders an artist list as one comparable string. The names are
// sorted so that a different credit order does not change the result.
func joinKeys(keys []string) string {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

func artistKeys(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if k := NormalizeArtist(n); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// durationFalloffMS is the additional difference beyond the tolerance after
// which the duration component reaches zero.
const durationFalloffMS = 20000

// durationScore rates how closely two durations agree.
func durationScore(wantedMS, candidateMS, toleranceMS int) float64 {
	diff := wantedMS - candidateMS
	if diff < 0 {
		diff = -diff
	}
	if diff <= toleranceMS {
		return 1
	}
	over := float64(diff - toleranceMS)
	return clamp01(1 - over/durationFalloffMS)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
