// Package artistidentity provides the single authoritative resolver and classifier
// for canonical artist identity across YTMDL.
//
// Invariants enforced by this package:
//  1. One actual artist = one canonical YTMDL artist entity.
//  2. Name is NOT identity: two artists sharing the same name but with distinct
//     real provider IDs (e.g. John Williams 1158 vs 8740 on Deezer) are AMBIGUOUS
//     and MUST NEVER be merged automatically.
//  3. Real external provider IDs must NEVER be lost.
//  4. Synthetic 'artist:<name>' keys are weak candidate evidence, not proof.
package artistidentity

import (
	"strings"
	"time"

	"ytdm/backend/internal/music"
)

// EvidenceLevel defines the classification confidence for artist identity matches.
type EvidenceLevel int

const (
	// LevelAmbiguous indicates candidates cannot be proved identical and must not be merged.
	LevelAmbiguous EvidenceLevel = iota
	// LevelStrongCandidate indicates strong candidate signals (e.g. normalized names + identical release catalog).
	LevelStrongCandidate
	// LevelSubscriptionProven indicates identity corroborated by active subscription provenance.
	LevelSubscriptionProven
	// LevelProvenProvenance indicates explicit pipeline or operator provenance link.
	LevelProvenProvenance
	// LevelExactSource indicates identical provider namespace and real source ID.
	LevelExactSource
)

// String returns human-readable evidence level name.
func (e EvidenceLevel) String() string {
	switch e {
	case LevelExactSource:
		return "EXACT_SOURCE"
	case LevelProvenProvenance:
		return "PROVEN_PROVENANCE"
	case LevelSubscriptionProven:
		return "SUBSCRIPTION_PROVEN"
	case LevelStrongCandidate:
		return "STRONG_CANDIDATE"
	default:
		return "AMBIGUOUS"
	}
}

// Candidate represents an artist row evaluated for identity resolution or merge.
type Candidate struct {
	ID           string
	Name         string
	Provider     string
	SourceID     string
	SourceKind   music.ArtistSourceKind
	ImageURL     string
	CreatedAt    time.Time
	ReleaseCount int
	TrackCount   int
	HasSub       bool
}

// TotalItems returns total releases plus tracks associated with this candidate.
func (c Candidate) TotalItems() int {
	return c.ReleaseCount + c.TrackCount
}

// IsSynthetic returns true if the candidate lacks a real upstream provider ID.
func (c Candidate) IsSynthetic() bool {
	if c.SourceKind == music.SourceKindLegacySynthetic {
		return true
	}
	return IsSyntheticSourceID(c.SourceID)
}

// IsSyntheticSourceID checks if a source ID is a name-derived synthetic key.
func IsSyntheticSourceID(sourceID string) bool {
	trimmed := strings.TrimSpace(sourceID)
	return trimmed == "" || strings.HasPrefix(trimmed, "artist:")
}

// ClassifyCandidatePair evaluates two artist candidates under strict identity rules.
func ClassifyCandidatePair(a, b Candidate) EvidenceLevel {
	if a.ID != "" && a.ID == b.ID {
		return LevelExactSource
	}

	// Rule: Same provider with distinct real provider IDs is NEVER the same artist.
	// Negative test: John Williams (Deezer 1158) vs John Williams (Deezer 8740).
	if a.Provider != "" && a.Provider == b.Provider {
		if !a.IsSynthetic() && !b.IsSynthetic() {
			if a.SourceID == b.SourceID {
				return LevelExactSource
			}
			return LevelAmbiguous
		}
	}

	// Cross-provider without provenance:
	// If both have distinct real external IDs and different providers (e.g. Deezer vs Spotify),
	// without an explicit cross-provider link or subscription match, it is ambiguous.
	if a.Provider != b.Provider && !a.IsSynthetic() && !b.IsSynthetic() {
		// Cannot be automatically assumed identical solely based on name.
		return LevelAmbiguous
	}

	// Subscription-proven match:
	// A synthetic candidate matching an existing subscription or real row on the same provider
	if a.Provider == b.Provider && (a.IsSynthetic() != b.IsSynthetic()) {
		if a.HasSub || b.HasSub {
			return LevelSubscriptionProven
		}
		return LevelStrongCandidate
	}

	// Both synthetic on the same provider
	if a.Provider == b.Provider && a.IsSynthetic() && b.IsSynthetic() {
		if a.SourceID != "" && a.SourceID == b.SourceID {
			if a.HasSub || b.HasSub {
				return LevelSubscriptionProven
			}
			return LevelStrongCandidate
		}
	}

	return LevelAmbiguous
}

// IsBetterCandidate returns true if candidate a should be preferred over candidate b
// as the canonical winner when merging proved duplicates.
//
// Deterministic selection priority:
// 1. Has active subscription
// 2. Real provider ID over synthetic key
// 3. Non-empty artwork
// 4. Most associated catalog items (releases + tracks)
// 5. Oldest creation date
// 6. Stable ID lexicographical tie-breaker
func IsBetterCandidate(a, b Candidate) bool {
	// 1. Active subscription
	if a.HasSub != b.HasSub {
		return a.HasSub
	}
	// 2. Real provider ID over synthetic
	aReal := !a.IsSynthetic()
	bReal := !b.IsSynthetic()
	if aReal != bReal {
		return aReal
	}
	// 3. Non-empty artwork
	aHasImg := strings.TrimSpace(a.ImageURL) != ""
	bHasImg := strings.TrimSpace(b.ImageURL) != ""
	if aHasImg != bHasImg {
		return aHasImg
	}
	// 4. Most associated items
	if a.TotalItems() != b.TotalItems() {
		return a.TotalItems() > b.TotalItems()
	}
	// 5. Oldest creation date
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	// 6. Lexicographical tie-breaker for perfect determinism
	return a.ID < b.ID
}

// ChooseWinner selects the canonical winner from a slice of candidates and returns
// the remaining candidates as duplicates to be merged.
func ChooseWinner(candidates []Candidate) (winner Candidate, duplicates []Candidate, ok bool) {
	if len(candidates) <= 1 {
		return Candidate{}, nil, false
	}

	winnerIdx := 0
	for i := 1; i < len(candidates); i++ {
		if IsBetterCandidate(candidates[i], candidates[winnerIdx]) {
			winnerIdx = i
		}
	}

	winner = candidates[winnerIdx]
	duplicates = make([]Candidate, 0, len(candidates)-1)
	for i, c := range candidates {
		if i != winnerIdx {
			duplicates = append(duplicates, c)
		}
	}
	return winner, duplicates, true
}
