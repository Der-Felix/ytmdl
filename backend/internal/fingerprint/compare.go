// Package fingerprint compares local Chromaprint measurements. A match is a
// review candidate, never authorization to delete or reject a download.
package fingerprint

import (
	"math/bits"
	"sort"
	"strconv"
	"strings"
)

// Buckets uses byte bands over adjacent words to shortlist similar recordings.
// Comparisons still require full alignment and duration checks.
func Buckets(words []uint32) string {
	seen := map[uint32]bool{}
	for i := 0; i+3 < len(words); i++ {
		for band := uint(0); band < 4; band++ {
			h := uint32(2166136261)
			for j := 0; j < 4; j++ {
				h = (h ^ ((words[i+j] >> (band * 8)) & 255)) * 16777619
			}
			seen[h&0x7fffffff] = true
		}
	}
	values := make([]uint32, 0, len(seen))
	for v := range seen {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if len(values) > 128 {
		values = values[:128]
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strconv.FormatUint(uint64(v), 10)
	}
	return "{" + strings.Join(out, ",") + "}"
}
func Usable(words []uint32) bool {
	if len(words) < 120 || len(words) > 1200 {
		return false
	}
	unique := map[uint32]bool{}
	for _, v := range words {
		unique[v] = true
	}
	return len(unique) > 20
}

// Similarity tolerates small leading shifts and codec differences. Silence and
// incomplete measurements are inconclusive, including valid short songs.
func Similarity(a, b []uint32) float64 {
	if !Usable(a) || !Usable(b) {
		return 0
	}
	best := 0.0
	for shift := -40; shift <= 40; shift++ {
		ia, ib := max(0, shift), max(0, -shift)
		n := min(len(a)-ia, len(b)-ib)
		if n < 120 || float64(n) < 0.85*float64(max(len(a), len(b))) {
			continue
		}
		distance := 0
		for j := 0; j < n; j++ {
			distance += bits.OnesCount32(a[ia+j] ^ b[ib+j])
		}
		score := 1 - float64(distance)/float64(n*32)
		if score > best {
			best = score
		}
	}
	return best
}
