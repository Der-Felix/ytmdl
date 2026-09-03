// Package storage turns domain objects into safe library paths and answers
// questions about what the library already contains.
package storage

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxComponentLength caps a single path component. The limit leaves room for
// the library root and the file extension on filesystems with a 255 byte
// limit per component.
const MaxComponentLength = 120

// placeholder is used when sanitising leaves nothing behind.
const placeholder = "_"

// reservedNames are refused as bare file or directory names by Windows and by
// SMB shares exported from it.
var reservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// SanitizeComponent turns arbitrary provider supplied text into a single safe
// path component. It never returns an empty string, a string containing a path
// separator, or a relative path element such as "." or "..".
func SanitizeComponent(name string) string {
	var b strings.Builder
	b.Grow(len(name))

	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			// A separator would create a new directory level and is the core
			// of any traversal attempt.
			b.WriteRune('-')
		case r == ':':
			b.WriteRune('-')
		case r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('_')
		case r == 0:
			// Dropped: a NUL byte truncates the path in every syscall.
		case unicode.IsControl(r):
			b.WriteRune(' ')
		case r == '\ufeff' || r == '\u200b' || r == '\u200e' || r == '\u200f' || r == '\u202e':
			// Zero width and bidirectional overrides can disguise a name.
		case r == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}

	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, " .")
	out = truncateRunes(out, MaxComponentLength)
	out = strings.Trim(out, " .")

	if out == "" {
		return placeholder
	}
	if _, reserved := reservedNames[strings.ToLower(baseName(out))]; reserved {
		return placeholder + out
	}
	return out
}

// SanitizeFilename sanitises a file name while preserving the extension. ext
// must include the leading dot and is itself sanitised.
func SanitizeFilename(name, ext string) string {
	ext = sanitizeExtension(ext)
	limit := MaxComponentLength - utf8.RuneCountInString(ext)
	if limit < 1 {
		limit = 1
	}

	base := SanitizeComponent(name)
	base = truncateRunes(base, limit)
	base = strings.Trim(base, " .")
	if base == "" {
		base = placeholder
	}
	if _, reserved := reservedNames[strings.ToLower(base)]; reserved {
		base = placeholder + base
	}
	return base + ext
}

// sanitizeExtension keeps only a leading dot followed by alphanumerics.
func sanitizeExtension(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	ext = strings.TrimLeft(ext, ".")
	var b strings.Builder
	for _, r := range ext {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "." + truncateRunes(b.String(), 16)
}

// baseName returns the part of name before the last dot, used for the reserved
// name check.
func baseName(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

// truncateRunes shortens s to at most n runes without splitting a rune.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
