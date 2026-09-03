package youtube

import (
	"regexp"
	"strings"
)

// artistSeparators split a combined artist credit such as "A, B & C" into the
// individual names. YouTube Music renders credits this way.
// Words that frequently form part of a band name ("and", "x") are deliberately
// not separators: splitting them apart would misread the credit.
var artistSeparators = regexp.MustCompile(`\s*(?:,|;|·|\bfeat\b\.?|\bft\b\.?|\bfeaturing\b|\bwith\b)\s*`)

// topicSuffix marks the auto generated artist channels YouTube creates for
// music. Their name is the artist plus this suffix.
const topicSuffix = " - topic"

// splitArtists turns a combined credit into individual artist names.
func splitArtists(credit string) []string {
	trimmed := strings.TrimSpace(credit)
	if trimmed == "" {
		return nil
	}

	parts := artistSeparators.Split(trimmed, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// splitArtistTitle splits a video title of the form "Artist - Title" into its
// two halves. The split only happens on the first separator, so that a title
// containing further dashes stays intact.
func splitArtistTitle(raw string) (string, string, bool) {
	title := strings.TrimSpace(raw)
	for _, separator := range []string{" - ", " – ", " — ", " | "} {
		artist, rest, found := strings.Cut(title, separator)
		if !found {
			continue
		}
		artist = strings.TrimSpace(artist)
		rest = strings.TrimSpace(rest)
		if artist == "" || rest == "" {
			continue
		}
		return artist, rest, true
	}
	return "", title, false
}

// StripTopicSuffix removes the " - Topic" decoration from a channel name.
func StripTopicSuffix(name string) string {
	if strings.HasSuffix(strings.ToLower(name), topicSuffix) {
		return strings.TrimSpace(name[:len(name)-len(topicSuffix)])
	}
	return name
}
