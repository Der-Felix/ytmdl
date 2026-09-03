package library

import (
	"strconv"
	"strings"
	"unicode"

	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
)

// NormalizeTag prepares a metadata string for comparison by trimming whitespace,
// lowercasing, and collapsing consecutive whitespace characters.
func NormalizeTag(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
		} else {
			b.WriteRune(unicode.ToLower(r))
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// CompareMetadata checks whether embedded tags match the database track catalog record.
// It returns a list of mismatched field names. If the list is empty, tags are considered matching.
func CompareMetadata(track music.Track, tags map[string][]string) []string {
	if len(tags) == 0 {
		return []string{"tags_empty"}
	}

	var mismatches []string

	// 1. Title comparison
	if track.Title != "" {
		tagTitle := firstTag(tags, metadata.FieldTitle, "title")
		if tagTitle == "" || NormalizeTag(track.DisplayTitle()) != NormalizeTag(tagTitle) {
			mismatches = append(mismatches, "title")
		}
	}

	// 2. Artist comparison
	if len(track.Artists) > 0 {
		tagArtists := allTags(tags, metadata.FieldArtist, "artist")
		expectedArtist := NormalizeTag(music.JoinArtists(track.Artists))
		actualArtistComma := NormalizeTag(strings.Join(tagArtists, ", "))
		actualArtistSemi := NormalizeTag(strings.Join(tagArtists, "; "))
		firstArtist := NormalizeTag(firstTag(tags, metadata.FieldArtist, "artist"))
		displayArtist := NormalizeTag(track.DisplayArtist())

		match := (actualArtistComma != "" && actualArtistComma == expectedArtist) ||
			(actualArtistSemi != "" && actualArtistSemi == expectedArtist) ||
			(firstArtist != "" && (firstArtist == expectedArtist || firstArtist == displayArtist))

		if !match {
			mismatches = append(mismatches, "artist")
		}
	}

	// 3. Album comparison
	if track.Album != "" {
		tagAlbum := firstTag(tags, metadata.FieldAlbum, "album")
		if tagAlbum == "" || NormalizeTag(track.Album) != NormalizeTag(tagAlbum) {
			mismatches = append(mismatches, "album")
		}
	}

	// 4. Track Number comparison (numeric)
	if track.TrackNumber > 0 {
		tagTrack := firstTag(tags, metadata.FieldTrackNumber, "track")
		tagNum := parseTrackNumber(tagTrack)
		if tagNum <= 0 || tagNum != track.TrackNumber {
			mismatches = append(mismatches, "track_number")
		}
	}

	// 5. Disc Number comparison (numeric)
	if track.DiscNumber > 0 {
		tagDisc := firstTag(tags, metadata.FieldDiscNumber, "disc")
		tagDiscNum := parseTrackNumber(tagDisc)
		if tagDiscNum <= 0 || tagDiscNum != track.DiscNumber {
			mismatches = append(mismatches, "disc_number")
		}
	}

	// 6. Year comparison (numeric)
	if track.Year > 0 {
		tagDate := firstTag(tags, metadata.FieldDate, "date")
		tagYear := parseYear(tagDate)
		if tagYear <= 0 || tagYear != track.Year {
			mismatches = append(mismatches, "year")
		}
	}

	// 7. ISRC comparison (case-insensitive)
	if strings.TrimSpace(track.ISRC) != "" {
		tagISRC := firstTag(tags, metadata.FieldISRC, "isrc")
		if strings.ToUpper(strings.TrimSpace(track.ISRC)) != strings.ToUpper(strings.TrimSpace(tagISRC)) {
			mismatches = append(mismatches, "isrc")
		}
	}

	return mismatches
}

func firstTag(tags map[string][]string, keys ...string) string {
	for _, k := range keys {
		variants := []string{k, strings.ToUpper(k), strings.ToLower(k)}
		for _, variant := range variants {
			if vals, ok := tags[variant]; ok && len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
				return strings.TrimSpace(vals[0])
			}
		}
	}
	return ""
}

func allTags(tags map[string][]string, keys ...string) []string {
	seenKeys := make(map[string]bool)
	var res []string
	for _, k := range keys {
		for _, variant := range []string{k, strings.ToUpper(k), strings.ToLower(k)} {
			if seenKeys[variant] {
				continue
			}
			seenKeys[variant] = true
			if vals, ok := tags[variant]; ok {
				for _, v := range vals {
					if t := strings.TrimSpace(v); t != "" {
						res = append(res, t)
					}
				}
			}
		}
	}
	return res
}

func parseTrackNumber(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		s = s[:4]
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}
