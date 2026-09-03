package genius

import (
	"strings"

	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
)

// MatchScore records the evaluation of a candidate Genius song result against a wanted track.
type MatchScore struct {
	Candidate  SongResult
	Confidence float64
	Reason     string
	Accepted   bool
}

// MatchCandidate evaluates a Genius search result against the wanted track.
// It enforces conservative artist/title similarity and strict version compatibility.
func MatchCandidate(wanted music.Track, cand SongResult, threshold float64) MatchScore {
	score := MatchScore{
		Candidate: cand,
	}

	if strings.TrimSpace(cand.Title) == "" || strings.TrimSpace(cand.PrimaryArtist.Name) == "" {
		score.Reason = "missing candidate metadata"
		return score
	}

	// 1. Title analysis & Version Extraction
	wantTitle := matcher.Analyze(wanted.Title)
	candTitle := matcher.Analyze(cand.Title)

	// Strict variant safety: Live, Remix, Instrumental, Acoustic, Demo must not conflict.
	if wantTitle.Versions.Has(matcher.VersionLive) != candTitle.Versions.Has(matcher.VersionLive) {
		score.Reason = "variant mismatch (live)"
		return score
	}
	if wantTitle.Versions.Has(matcher.VersionRemix) != candTitle.Versions.Has(matcher.VersionRemix) {
		score.Reason = "variant mismatch (remix)"
		return score
	}
	if wantTitle.Versions.Has(matcher.VersionInstrumental) != candTitle.Versions.Has(matcher.VersionInstrumental) {
		score.Reason = "variant mismatch (instrumental)"
		return score
	}
	if wantTitle.Versions.Has(matcher.VersionAcoustic) != candTitle.Versions.Has(matcher.VersionAcoustic) {
		score.Reason = "variant mismatch (acoustic)"
		return score
	}
	if wantTitle.Versions.Has(matcher.VersionDemo) != candTitle.Versions.Has(matcher.VersionDemo) {
		score.Reason = "variant mismatch (demo)"
		return score
	}

	// Version penalty for minor differing markers (e.g. radio edit, extended)
	versionPenalty := 0.0
	if wantTitle.Versions != candTitle.Versions {
		versionPenalty = 0.20
	}

	// 2. Title similarity
	titleSim := matcher.Similarity(wantTitle.Base, candTitle.Base)
	if titleSim < 0.70 {
		score.Confidence = titleSim
		score.Reason = "title similarity too low"
		return score
	}

	// 3. Artist similarity
	// Build collection of wanted artist names
	var wantArtists []string
	if len(wanted.Artists) > 0 {
		wantArtists = append(wantArtists, wanted.Artists...)
	}
	if wanted.AlbumArtist != "" {
		wantArtists = append(wantArtists, wanted.AlbumArtist)
	}
	if len(wantTitle.Featured) > 0 {
		wantArtists = append(wantArtists, wantTitle.Featured...)
	}
	if len(wantArtists) == 0 {
		wantArtists = []string{wanted.Title} // fallback if artist was blank
	}

	candArtistNorm := matcher.NormalizeArtist(cand.PrimaryArtist.Name)
	bestArtistSim := 0.0

	for _, wa := range wantArtists {
		waNorm := matcher.NormalizeArtist(wa)
		if waNorm == "" {
			continue
		}
		if waNorm == candArtistNorm {
			bestArtistSim = 1.0
			break
		}
		sim := matcher.Similarity(waNorm, candArtistNorm)
		if sim > bestArtistSim {
			bestArtistSim = sim
		}
	}

	// Also check if candidate title carries featured artist that matches
	for _, cf := range candTitle.Featured {
		cfNorm := matcher.NormalizeArtist(cf)
		for _, wa := range wantArtists {
			sim := matcher.Similarity(matcher.NormalizeArtist(wa), cfNorm)
			if sim > bestArtistSim {
				bestArtistSim = sim
			}
		}
	}

	if bestArtistSim < 0.70 {
		score.Confidence = (titleSim + bestArtistSim) / 2
		score.Reason = "artist similarity too low"
		return score
	}

	// Combined weighted score
	combined := (0.50 * titleSim) + (0.50 * bestArtistSim) - versionPenalty
	if combined < 0 {
		combined = 0
	}

	score.Confidence = combined
	if combined >= threshold {
		score.Accepted = true
		score.Reason = "match confirmed"
	} else {
		score.Reason = "confidence below threshold"
	}

	return score
}
