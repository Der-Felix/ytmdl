// Package matcher normalises titles and artist names and scores media
// candidates against a wanted track.
package matcher

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// VersionSet is a bit set of version markers found in a title. Two recordings
// only describe the same thing when their version sets are equal.
type VersionSet uint32

// Version markers that make a recording a genuinely different rendition.
const (
	VersionLive VersionSet = 1 << iota
	VersionRemix
	VersionInstrumental
	VersionAcoustic
	VersionSpedUp
	VersionSlowed
	VersionCover
	VersionKaraoke
	VersionRadioEdit
	VersionExtended
	VersionDemo
)

var versionNames = []struct {
	bit  VersionSet
	name string
}{
	{VersionLive, "live"},
	{VersionRemix, "remix"},
	{VersionInstrumental, "instrumental"},
	{VersionAcoustic, "acoustic"},
	{VersionSpedUp, "sped_up"},
	{VersionSlowed, "slowed"},
	{VersionCover, "cover"},
	{VersionKaraoke, "karaoke"},
	{VersionRadioEdit, "radio_edit"},
	{VersionExtended, "extended"},
	{VersionDemo, "demo"},
}

// Has reports whether the set contains the given marker.
func (v VersionSet) Has(other VersionSet) bool { return v&other != 0 }

// Names returns the stable names of all markers in the set, sorted.
func (v VersionSet) Names() []string {
	out := make([]string, 0, len(versionNames))
	for _, entry := range versionNames {
		if v.Has(entry.bit) {
			out = append(out, entry.name)
		}
	}
	return out
}

// String renders the set as a stable, comparable key.
func (v VersionSet) String() string { return strings.Join(v.Names(), "+") }

// TitleInfo is the result of analysing a raw title.
type TitleInfo struct {
	// Original is the untouched input.
	Original string
	// Base is the normalised title with featuring credits, decorative noise
	// and version markers removed.
	Base string
	// Versions holds the version markers that were recognised.
	Versions VersionSet
	// Featured lists the normalised names of featured artists.
	Featured []string
}

// versionPatterns maps a marker onto the expressions that identify it. They are
// matched against a single bracketed or dash separated segment, not against the
// whole title, so that a song simply named "Live Forever" is not misread.
var versionPatterns = []struct {
	bit VersionSet
	re  *regexp.Regexp
}{
	{VersionLive, regexp.MustCompile(`(?i)\b(live|en vivo|unplugged|ao vivo|in concert)\b`)},
	{VersionRemix, regexp.MustCompile(`(?i)\b(remix|rmx|bootleg|mashup|flip|rework|vip mix)\b`)},
	{VersionInstrumental, regexp.MustCompile(`(?i)\b(instrumental)\b`)},
	{VersionAcoustic, regexp.MustCompile(`(?i)\b(acoustic|akustik|acustico)\b`)},
	{VersionSpedUp, regexp.MustCompile(`(?i)\b(sped ?up|speed ?up|nightcore)\b`)},
	{VersionSlowed, regexp.MustCompile(`(?i)\b(slowed|daycore)\b`)},
	{VersionCover, regexp.MustCompile(`(?i)\b(cover|covered by)\b`)},
	{VersionKaraoke, regexp.MustCompile(`(?i)\b(karaoke|backing track|sing ?along)\b`)},
	{VersionRadioEdit, regexp.MustCompile(`(?i)\b(radio (edit|version|mix))\b`)},
	{VersionExtended, regexp.MustCompile(`(?i)\b(extended|long version|full version)\b`)},
	{VersionDemo, regexp.MustCompile(`(?i)\b(demo|rough mix)\b`)},
}

// noisePatterns match segments that carry no information about the recording
// itself and are therefore removed without changing the track identity.
var noisePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*official\b.*$`),
	regexp.MustCompile(`(?i)\b(official (music )?(video|audio|visualizer)|lyrics?( video)?|visuali[sz]er)\b`),
	regexp.MustCompile(`(?i)^\s*(hd|hq|4k|8k|mv|m/v|audio|video|full audio|explicit|clean|stereo|mono)\s*$`),
	regexp.MustCompile(`(?i)\b((19|20)\d{2} )?(digital(ly)? )?remaster(ed)?( (19|20)\d{2})?( version)?\b`),
	regexp.MustCompile(`(?i)\b(album version|single version|original mix|original version|main version|standard version)\b`),
	regexp.MustCompile(`(?i)\b(bonus track|deluxe( edition)?|expanded edition|anniversary edition|special edition|reissue)\b`),
	regexp.MustCompile(`(?i)\b(free download|out now|premiere|audio only|visualiser)\b`),
}

// trailingVersionPatterns catch version markers that appear without brackets at
// the very end of a title, e.g. "Song sped up". Only markers that practically
// never form part of a real song title are listed here, so that titles such as
// "Long Live" or "Live Forever" keep their meaning.
var trailingVersionPatterns = []struct {
	bit VersionSet
	re  *regexp.Regexp
}{
	{VersionSpedUp, regexp.MustCompile(`(?i)\s+(sped ?up|speed ?up|nightcore)( version)?\s*$`)},
	{VersionSlowed, regexp.MustCompile(`(?i)\s+slowed( down)?( (\+|and) ?reverb)?\s*$`)},
	{VersionInstrumental, regexp.MustCompile(`(?i)\s+instrumental( version)?\s*$`)},
	{VersionKaraoke, regexp.MustCompile(`(?i)\s+karaoke( version)?\s*$`)},
	{VersionRemix, regexp.MustCompile(`(?i)\s+([\w'&.-]+\s+)+remix\s*$`)},
}

// stripTrailingVersions removes unbracketed trailing version markers and
// reports which ones were found.
func stripTrailingVersions(s string) (string, VersionSet) {
	var bits VersionSet
	for changed := true; changed; {
		changed = false
		for _, p := range trailingVersionPatterns {
			if loc := p.re.FindStringIndex(s); loc != nil {
				s = s[:loc[0]]
				bits |= p.bit
				changed = true
			}
		}
	}
	return s, bits
}

var (
	featPrefixRe = regexp.MustCompile(`(?i)^\s*(feat\.?|ft\.?|featuring|with|w/|con)\s+(.+)$`)
	inlineFeatRe = regexp.MustCompile(`(?i)\s+(feat\.?|ft\.?|featuring)\s+.*$`)
	splitFeatRe  = regexp.MustCompile(`(?i)\s*(,|&|\band\b|\bx\b|\bund\b|/|\+)\s*`)
	spaceRe      = regexp.MustCompile(`\s+`)
)

// Analyze decomposes a raw title into a base title, version markers and
// featured artists.
func Analyze(raw string) TitleInfo {
	info := TitleInfo{Original: raw}

	work := foldASCII(raw)
	work = normalisePunctuation(work)

	var segments []string
	work, segments = extractBracketed(work)

	// Trailing dash separated segments carry the same kind of information as
	// bracketed ones ("Song - Live at Wembley").
	head, dashSegments := splitDashSuffixes(work)
	work = head
	segments = append(segments, dashSegments...)

	kept := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if m := featPrefixRe.FindStringSubmatch(seg); m != nil {
			info.Featured = append(info.Featured, splitArtistList(m[2])...)
			continue
		}
		if bits, ok := classifyVersion(seg); ok {
			info.Versions |= bits
			continue
		}
		if isNoise(seg) {
			continue
		}
		kept = append(kept, seg)
	}

	var trailingBits VersionSet
	work, trailingBits = stripTrailingVersions(work)
	info.Versions |= trailingBits

	// An inline "feat. X" without brackets belongs to the credits, not to the
	// title.
	if m := inlineFeatRe.FindString(work); m != "" {
		trimmed := strings.TrimSpace(m)
		if sub := featPrefixRe.FindStringSubmatch(trimmed); sub != nil {
			info.Featured = append(info.Featured, splitArtistList(sub[2])...)
		}
		work = inlineFeatRe.ReplaceAllString(work, "")
	}

	base := strings.Join(append([]string{work}, kept...), " ")
	base = stripNoiseWords(base)
	info.Base = cleanKey(base)
	info.Featured = dedupeStrings(info.Featured)
	return info
}

// classifyVersion reports the version markers a segment describes. A segment
// only counts when the marker words dominate it, so that "(Part 2 of the Live
// Sessions Story)" is not silently swallowed.
func classifyVersion(segment string) (VersionSet, bool) {
	var bits VersionSet
	for _, p := range versionPatterns {
		if p.re.MatchString(segment) {
			bits |= p.bit
		}
	}
	if bits == 0 {
		return 0, false
	}
	return bits, true
}

func isNoise(segment string) bool {
	for _, re := range noisePatterns {
		if re.MatchString(segment) {
			// The segment is noise only when nothing meaningful remains.
			if cleanKey(re.ReplaceAllString(segment, " ")) == "" {
				return true
			}
		}
	}
	return false
}

func stripNoiseWords(s string) string {
	for _, re := range noisePatterns {
		s = re.ReplaceAllString(s, " ")
	}
	return s
}

// extractBracketed removes (), [] and {} groups from s and returns the
// remaining text plus the extracted group contents.
func extractBracketed(s string) (string, []string) {
	var (
		out      strings.Builder
		segments []string
		current  strings.Builder
		depth    int
	)
	closers := map[rune]rune{'(': ')', '[': ']', '{': '}'}
	var want rune
	for _, r := range s {
		if depth == 0 {
			if c, ok := closers[r]; ok {
				depth = 1
				want = c
				current.Reset()
				continue
			}
			out.WriteRune(r)
			continue
		}
		if r == want {
			depth = 0
			segments = append(segments, current.String())
			out.WriteRune(' ')
			continue
		}
		current.WriteRune(r)
	}
	if depth != 0 {
		// Unbalanced bracket: treat the remainder as a normal segment.
		segments = append(segments, current.String())
	}
	return out.String(), segments
}

// splitDashSuffixes separates "Title - Suffix - Suffix" into the head and the
// suffixes. Only suffixes that look like annotations are split off; a dash
// inside the actual title is preserved because the head keeps everything up to
// the first recognised annotation.
func splitDashSuffixes(s string) (string, []string) {
	parts := strings.Split(s, " - ")
	if len(parts) < 2 {
		return s, nil
	}
	head := parts[0]
	var segments []string
	for _, p := range parts[1:] {
		trimmed := strings.TrimSpace(p)
		_, isVersion := classifyVersion(trimmed)
		switch {
		case isVersion, isNoise(trimmed), featPrefixRe.MatchString(trimmed):
			segments = append(segments, trimmed)
		default:
			// Not an annotation: it belongs to the title itself.
			head += " " + trimmed
		}
	}
	return head, segments
}

func splitArtistList(s string) []string {
	raw := splitFeatRe.Split(s, -1)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if key := cleanKey(r); key != "" {
			out = append(out, key)
		}
	}
	return out
}

// NormalizeTitle returns the comparable base form of a title.
func NormalizeTitle(s string) string { return Analyze(s).Base }

var leadingArticleRe = regexp.MustCompile(`(?i)^(the|die|der|das|les|la|le|el)\s+`)

// NormalizeArtist returns the comparable form of an artist name.
func NormalizeArtist(s string) string {
	s = foldASCII(s)
	s = normalisePunctuation(s)
	s = leadingArticleRe.ReplaceAllString(strings.TrimSpace(s), "")
	return cleanKey(s)
}

// NormalizeArtists normalises and sorts a list of artist names into a stable
// key. Order differences between providers must not create a new identity.
func NormalizeArtists(names []string) string {
	keys := make([]string, 0, len(names))
	for _, n := range names {
		for _, part := range splitArtistList(n) {
			if part != "" {
				keys = append(keys, part)
			}
		}
	}
	keys = dedupeStrings(keys)
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// cleanKey lowercases, removes every non alphanumeric rune and collapses
// whitespace.
func cleanKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(b.String(), " "))
}

// normalisePunctuation maps typographic characters onto their ASCII form so
// that quoting styles do not create different keys.
func normalisePunctuation(s string) string {
	replacer := strings.NewReplacer(
		"‘", "'", "’", "'", "‚", "'", "‛", "'",
		"“", `"`, "”", `"`, "„", `"`,
		"–", "-", "—", "-", "―", "-", "−", "-",
		"\u00a0", " ", "\u200b", "", "\ufeff", "",
		"×", "x",
	)
	return replacer.Replace(s)
}

// asciiFolding maps common accented Latin characters onto ASCII. Doing this
// here keeps the backend free of an extra Unicode transformation dependency.
var asciiFolding = map[rune]string{
	'ä': "a", 'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'æ': "ae",
	'ç': "c", 'ć': "c", 'č': "c", 'ĉ': "c",
	'ď': "d", 'đ': "d", 'ð': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g",
	'ĥ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i", 'ı': "i",
	'ĵ': "j",
	'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ő': "o",
	'œ': "oe",
	'ŕ': "r", 'ř': "r",
	'ś': "s", 'ş': "s", 'š': "s", 'ș': "s",
	'ß': "ss",
	'ţ': "t", 'ť': "t", 'ț': "t", 'þ': "th",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
}

// foldASCII replaces accented characters with their ASCII equivalent.
func foldASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		lower := unicode.ToLower(r)
		if repl, ok := asciiFolding[lower]; ok {
			if unicode.IsUpper(r) {
				b.WriteString(strings.ToUpper(repl))
			} else {
				b.WriteString(repl)
			}
			continue
		}
		if unicode.Is(unicode.Mn, r) {
			continue // combining mark left over from decomposed input
		}
		b.WriteRune(r)
	}
	return b.String()
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
