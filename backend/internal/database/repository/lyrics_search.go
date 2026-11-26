package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"ytdm/backend/internal/music"
)

// Lyrics search works on track_lyrics_search, a derived index of the lyrics
// sidecars (see migration 0024). Text is folded in Go, the same way for the
// index and the query: lowercase, diacritics removed, ß as ss and a few
// ligatures spelled out, so "Mädchen", "MADCHEN" and "madchen" match. The
// 'simple' text search configuration then splits it into words without
// stemming or stop words, which suits German and English alike.

const (
	// lyricsMinQueryRunes is the shortest query that searches lyrics; a
	// shorter one would match most of the library.
	lyricsMinQueryRunes = 3
	// lyricsMaxQueryWords bounds the words of one lyrics query.
	lyricsMaxQueryWords = 8
	// lyricsMatchPool bounds the matches ranked for one query; a very common
	// word ranks only the first matches the index returns.
	lyricsMatchPool = 1000
	// MaxLyricsHits bounds the lyric hits of one search.
	MaxLyricsHits = 20
	// maxIndexedLyricsBytes bounds the text kept per track.
	maxIndexedLyricsBytes = 64 << 10
	// lyricsSnippetRunes is the context kept on each side of a match.
	lyricsSnippetRunes = 45
)

var foldSpecial = map[rune]string{
	'ß': "ss", 'ẞ': "ss", 'æ': "ae", 'Æ': "ae", 'œ': "oe", 'Œ': "oe",
	'ø': "o", 'Ø': "o", 'ł': "l", 'Ł': "l", 'đ': "d", 'Đ': "d", 'þ': "th", 'Þ': "th", 'ı': "i",
	'’': "'", '‘': "'",
}

// foldRune lowercases r and strips its diacritics.
func foldRune(r rune) string {
	if s, ok := foldSpecial[r]; ok {
		return s
	}
	if r < utf8.RuneSelf {
		return string(unicode.ToLower(r))
	}
	var b strings.Builder
	for _, d := range norm.NFD.String(string(r)) {
		if unicode.Is(unicode.Mn, d) {
			continue
		}
		b.WriteRune(unicode.ToLower(d))
	}
	return b.String()
}

// FoldSearchText lowercases s and removes diacritics.
func FoldSearchText(s string) string {
	folded, _ := foldWithOffsets(s)
	return folded
}

// foldWithOffsets folds s and returns, for every byte of the folded text,
// the byte offset in s of the rune it came from.
func foldWithOffsets(s string) (string, []int) {
	var b strings.Builder
	offsets := make([]int, 0, len(s))
	for i, r := range s {
		piece := foldRune(r)
		b.WriteString(piece)
		for range len(piece) {
			offsets = append(offsets, i)
		}
	}
	return b.String(), offsets
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// lyricsQueryWords folds a query into its words.
func lyricsQueryWords(q string) []string {
	words := strings.FieldsFunc(FoldSearchText(q), func(r rune) bool { return !isWordRune(r) })
	if len(words) > lyricsMaxQueryWords {
		words = words[:lyricsMaxQueryWords]
	}
	return words
}

// lyricsTSQuery builds the to_tsquery text: every word must occur, the last
// one as a prefix (it may still be typed). phrase joins them as a phrase
// instead, which ranks hits with the words in this order first.
func lyricsTSQuery(words []string) (all, phrase string) {
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = w
	}
	parts[len(parts)-1] += ":*"
	return strings.Join(parts, " & "), strings.Join(parts, " <-> ")
}

// LyricsMatch is a track found by its lyrics.
type LyricsMatch = music.LyricsSearchHit

// SearchLyrics finds up to limit tracks whose stored lyrics contain every
// word of q (the last as a prefix). Hits with the words as a phrase rank
// first, then by text search rank. Only tracks with a file whose lyrics are
// marked available count. A query shorter than three characters finds
// nothing.
func (c *Catalog) SearchLyrics(ctx context.Context, q string, limit int) ([]LyricsMatch, error) {
	out := []LyricsMatch{}
	words := lyricsQueryWords(q)
	if len(words) == 0 || utf8.RuneCountInString(strings.Join(words, "")) < lyricsMinQueryRunes {
		return out, nil
	}
	limit = min(max(limit, 1), MaxLyricsHits)
	all, phrase := lyricsTSQuery(words)
	rows, err := c.db.QueryContext(ctx, `WITH q AS (SELECT to_tsquery('simple',$1) AS q,to_tsquery('simple',$2) AS p),
 m AS (SELECT s.track_id,s.body,s.document FROM track_lyrics_search s,q WHERE s.document @@ q.q LIMIT `+strconv.Itoa(lyricsMatchPool)+`)
 SELECT m.track_id,m.body FROM m CROSS JOIN q JOIN tracks t ON t.id=m.track_id
 WHERE t.lyrics_state IN('available_synced','available_plain') AND EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
 ORDER BY (m.document @@ q.p) DESC,ts_rank_cd(m.document,q.q) DESC,m.track_id LIMIT $3`, all, phrase, limit)
	if err != nil {
		return nil, wrapDB("search lyrics", err)
	}
	ids, bodies := []string{}, map[string]string{}
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		bodies[id] = body
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tracks, err := c.TracksByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range tracks {
		snippet, match := LyricsSnippet(bodies[t.ID], words)
		out = append(out, LyricsMatch{Track: t, Snippet: snippet, Match: match})
	}
	return out, nil
}

// foldedWord is a word of folded text with its byte range.
type foldedWord struct {
	text       string
	start, end int
}

func foldedWords(folded string) []foldedWord {
	out := []foldedWord{}
	start := -1
	for i, r := range folded {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, foldedWord{folded[start:i], start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, foldedWord{folded[start:], start, len(folded)})
	}
	return out
}

// findLyricsMatch returns the byte range in folded of the first place where
// the words occur in order (the last as a prefix), else of the first word of
// folded that starts with any query word; ok is false when there is none.
func findLyricsMatch(folded string, words []string) (from, to int, ok bool) {
	text := foldedWords(folded)
	matches := func(w foldedWord, k int) bool {
		if k == len(words)-1 {
			return strings.HasPrefix(w.text, words[k])
		}
		return w.text == words[k]
	}
	for i := 0; i+len(words) <= len(text); i++ {
		all := true
		for k := range words {
			if !matches(text[i+k], k) {
				all = false
				break
			}
		}
		if all {
			return text[i].start, text[i+len(words)-1].end, true
		}
	}
	for _, w := range text {
		for _, q := range words {
			if strings.HasPrefix(w.text, q) {
				return w.start, w.end, true
			}
		}
	}
	return 0, 0, false
}

// normalizeSnippet turns line breaks (with the blanks around them) into
// " / " and runs of blanks into one space.
func normalizeSnippet(s string) string {
	lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	joined := strings.Join(out, " / ")
	// A break right at the start or end of s still separates it from the
	// text the caller puts next to it.
	if strings.TrimSpace(s) != "" {
		trimmed := strings.TrimLeft(s, " \t")
		if strings.HasPrefix(trimmed, "\n") || strings.HasPrefix(trimmed, "\r") {
			joined = "/ " + joined
		}
		trimmed = strings.TrimRight(s, " \t")
		if strings.HasSuffix(trimmed, "\n") || strings.HasSuffix(trimmed, "\r") {
			joined += " /"
		}
	}
	return joined
}

// LyricsSnippet cuts a short excerpt of body around the first place where
// the query words occur (see findLyricsMatch), with about 45 characters of
// context on each side cut at word boundaries. Line breaks become " / ", and
// "…" marks cut text. match is the matched text exactly as it appears in
// snippet; both are empty when nothing matches.
func LyricsSnippet(body string, words []string) (snippet, match string) {
	if len(words) == 0 || body == "" {
		return "", ""
	}
	folded, offsets := foldWithOffsets(body)
	from, to, ok := findLyricsMatch(folded, words)
	if !ok {
		return "", ""
	}
	start, end := offsets[from], len(body)
	if to < len(offsets) {
		end = offsets[to]
	}

	before, after := body[:start], body[end:]
	lead, trail := "", ""
	if r := []rune(before); len(r) > lyricsSnippetRunes {
		before = string(r[len(r)-lyricsSnippetRunes:])
		if i := strings.IndexFunc(before, unicode.IsSpace); i >= 0 {
			before = before[i:]
		}
		lead = "… "
	}
	if r := []rune(after); len(r) > lyricsSnippetRunes {
		after = string(r[:lyricsSnippetRunes])
		if i := strings.LastIndexFunc(after, unicode.IsSpace); i >= 0 {
			after = after[:i+1]
		}
		trail = " …"
	}
	match = normalizeSnippet(body[start:end])
	pre, post := normalizeSnippet(before), normalizeSnippet(after)
	if pre != "" && unicode.IsSpace(lastRune(before)) {
		pre += " "
	}
	if post != "" && unicode.IsSpace(firstRune(after)) {
		post = " " + post
	}
	if pre == "" {
		lead = strings.TrimSpace(lead)
		if lead != "" {
			lead += " "
		}
	}
	return strings.TrimSpace(lead + pre + match + post + trail), match
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// LyricsIndexCandidate is a track whose lyrics need (re)indexing.
type LyricsIndexCandidate struct {
	TrackID   string
	CheckedAt *time.Time
}

// ListTracksNeedingLyricsIndex lists up to limit tracks whose lyrics are
// marked available but whose index row is missing or was built from an
// older lyrics check.
func (c *Catalog) ListTracksNeedingLyricsIndex(ctx context.Context, limit int) ([]LyricsIndexCandidate, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT t.id,t.lyrics_checked_at FROM tracks t LEFT JOIN track_lyrics_search s ON s.track_id=t.id
 WHERE t.lyrics_state IN('available_synced','available_plain') AND (s.track_id IS NULL OR s.source_checked_at IS DISTINCT FROM t.lyrics_checked_at)
 ORDER BY t.id LIMIT $1`, limit)
	if err != nil {
		return nil, wrapDB("lyrics index candidates", err)
	}
	defer rows.Close()
	out := []LyricsIndexCandidate{}
	for rows.Next() {
		var cand LyricsIndexCandidate
		var checked sql.NullTime
		if err := rows.Scan(&cand.TrackID, &checked); err != nil {
			return nil, err
		}
		if checked.Valid {
			t := checked.Time
			cand.CheckedAt = &t
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}

// UpsertLyricsIndex stores the plain lyrics text of a track in the search
// index. checkedAt is the track's lyrics_checked_at the text belongs to. An
// empty body (no readable sidecar) is stored too, so the track is not read
// again until its lyrics change.
func (c *Catalog) UpsertLyricsIndex(ctx context.Context, trackID, body string, checkedAt *time.Time) error {
	body = strings.ToValidUTF8(body, "")
	if len(body) > maxIndexedLyricsBytes {
		body = body[:maxIndexedLyricsBytes]
		body = strings.ToValidUTF8(body, "")
	}
	var checked any
	if checkedAt != nil {
		checked = *checkedAt
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO track_lyrics_search(track_id,body,document,source_checked_at,indexed_at)
 SELECT id,$2::text,to_tsvector('simple',$3::text),$4::timestamptz,now() FROM tracks WHERE id=$1
 ON CONFLICT(track_id) DO UPDATE SET body=EXCLUDED.body,document=EXCLUDED.document,source_checked_at=EXCLUDED.source_checked_at,indexed_at=now()`,
		trackID, body, FoldSearchText(body), checked)
	if err != nil {
		return wrapDB("index lyrics", err)
	}
	return nil
}

// DeleteLyricsIndex removes a track from the lyrics search index.
func (c *Catalog) DeleteLyricsIndex(ctx context.Context, trackID string) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM track_lyrics_search WHERE track_id=$1`, trackID); err != nil {
		return wrapDB("remove lyrics index", err)
	}
	return nil
}

// PruneLyricsIndex removes index rows of tracks whose lyrics are no longer
// marked available.
func (c *Catalog) PruneLyricsIndex(ctx context.Context) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM track_lyrics_search s USING tracks t WHERE t.id=s.track_id AND t.lyrics_state NOT IN('available_synced','available_plain')`); err != nil {
		return wrapDB("prune lyrics index", err)
	}
	return nil
}
