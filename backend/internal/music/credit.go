package music

import (
	"regexp"
	"strings"
)

// VariousArtists is the literal album artist Plex documents for compilations.
// Emby accepts any unique value and Jellyfin reads it from the tag, so this one
// string satisfies all three media servers.
const VariousArtists = "Various Artists"

// featuringSeparators matches the markers that explicitly announce a guest
// credit. They are not ambiguous: an artist name does not contain " feat. ",
// so splitting on them needs no corroboration.
var featuringSeparators = regexp.MustCompile(
	`(?i)\s+(?:feat\.?|ft\.?|featuring|w/)\s+`)

// ambiguousSeparators matches the characters and words that *may* join two
// artists — and that also occur inside perfectly ordinary names such as
// "Simon & Garfunkel", "Earth, Wind & Fire" or "AC/DC". Splitting on them is
// only ever done when structured provider data confirms the result.
var ambiguousSeparators = regexp.MustCompile(
	`(?i)\s*(?:,|&|;|\+|/|\bx\b|\band\b|\bund\b|\bwith\b|\bvs\.?\b)\s*`)

// SplitFeaturing splits a credit string at its explicit featuring markers.
//
// This is the one split that is safe on unstructured text: "Artist A feat.
// Artist B" states a guest credit, it does not name a band. A string without a
// marker is returned unchanged as a single element.
func SplitFeaturing(credit string) []string {
	return cleanCredits(featuringSeparators.Split(strings.TrimSpace(credit), -1))
}

// SplitCredit splits a credit string at every known separator.
//
// It is a last resort for genuinely unstructured display text and must never
// be the primary source of artist identities: it cannot tell "LACAZETTE &
// Bushido" (two artists) from "Simon & Garfunkel" (one). Callers use it only
// where structured provider data — artist runs with channel ids, contributor
// lists, release artists — is absent, or to confirm a split that structured
// data already established.
func SplitCredit(credit string) []string {
	trimmed := strings.TrimSpace(credit)
	if trimmed == "" {
		return nil
	}
	var out []string
	for _, part := range SplitFeaturing(trimmed) {
		out = append(out, ambiguousSeparators.Split(part, -1)...)
	}
	return cleanCredits(out)
}

// NormalizeCredits returns the artist list that becomes the ARTIST tag.
//
// The provider's structured credits are kept as they are — one entry per
// artist the provider actually identified. The only transformation is that an
// entry still carrying an explicit "feat." marker is separated, because that
// marker is a statement about credits rather than part of a name.
func NormalizeCredits(credits []string) []string {
	out := make([]string, 0, len(credits))
	seen := make(map[string]struct{}, len(credits))
	for _, credit := range credits {
		for _, name := range SplitFeaturing(credit) {
			key := strings.ToLower(name)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolveAlbumArtist returns the single name that becomes the ALBUMARTIST tag
// and, with it, the artist directory in the library.
//
// Structured provider data wins. The album artist a provider reported as one
// name is used exactly as it came — a name is never taken apart merely because
// it contains "&", "," or "/". The one case in which the provider's string is
// reduced is when the provider itself contradicts it: it delivered a joined
// display string *and* a structured credit list, and splitting the string
// reproduces that list. Then, and only then, the first credit wins.
//
// providerAlbumArtist is the provider's own release artist (Deezer's
// album.artist, Spotify's album.artists[0], the first artist run of a YouTube
// Music release header). credits are the structured credits of the release.
func ResolveAlbumArtist(providerAlbumArtist string, credits []string) string {
	normalised := NormalizeCredits(credits)
	provider := strings.TrimSpace(providerAlbumArtist)

	if provider == "" {
		if len(normalised) > 0 {
			return normalised[0]
		}
		return UnknownArtist
	}

	// The provider's album artist is one of the credits it also listed
	// separately: structured, corroborated, used verbatim. This is what keeps
	// "Simon & Garfunkel" intact.
	for _, credit := range normalised {
		if strings.EqualFold(credit, provider) {
			return credit
		}
	}

	// The provider gave a joined display string next to a structured credit
	// list. Splitting is only accepted when it reproduces that very list.
	if len(normalised) > 1 && creditsMatch(SplitCredit(provider), normalised) {
		return normalised[0]
	}

	// An explicit featuring marker is unambiguous even without corroboration.
	if parts := SplitFeaturing(provider); len(parts) > 1 {
		return parts[0]
	}

	return provider
}

// creditsMatch reports whether two credit lists name the same artists in the
// same order, ignoring case.
func creditsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// cleanCredits trims, drops empties and removes case insensitive duplicates
// while preserving order.
func cleanCredits(parts []string) []string {
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
