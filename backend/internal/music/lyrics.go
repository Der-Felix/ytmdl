package music

import "strings"

// LyricsState is what the catalogue remembers about the lyrics of one track.
//
// It records the *outcome of a lookup*, never the lyrics themselves: the
// sidecar file next to the audio holds the text, because that file is what
// Plex, Jellyfin and Emby read. A second copy in the database could only ever
// disagree with it.
//
// The state machine has one deliberate gap: there is no "error" state. A
// transient failure — a timeout, a 429, a 5xx, a broken response — leaves the
// previous state and the previous check timestamp untouched, so it can never
// be mistaken for a lookup that positively found nothing. Only a definitive
// provider answer moves a track into LyricsNotFound.
//
//	unknown ──lookup succeeds, lyrics found──▶ available_synced | available_plain
//	unknown ──provider says "instrumental"──▶ instrumental
//	unknown ──every provider answered, none had it──▶ not_found
//	<any>   ──transient failure──▶ <unchanged>
type LyricsState string

const (
	// LyricsUnknown means no provider has been asked yet.
	LyricsUnknown LyricsState = "unknown"
	// LyricsAvailableSynced means timed lyrics were written as a .lrc sidecar.
	LyricsAvailableSynced LyricsState = "available_synced"
	// LyricsAvailablePlain means untimed lyrics were written as a .txt sidecar.
	LyricsAvailablePlain LyricsState = "available_plain"
	// LyricsInstrumental means a provider positively reported that the track
	// has no lyrics. It is an answer, not a miss, and it ends the search.
	LyricsInstrumental LyricsState = "instrumental"
	// LyricsNotFound means every provider was asked and none had an entry.
	LyricsNotFound LyricsState = "not_found"
)

// AllLyricsStates lists every state the catalogue may hold.
func AllLyricsStates() []LyricsState {
	return []LyricsState{
		LyricsUnknown, LyricsAvailableSynced, LyricsAvailablePlain,
		LyricsInstrumental, LyricsNotFound,
	}
}

// ValidLyricsState reports whether s is a known state.
func ValidLyricsState(s string) bool {
	for _, known := range AllLyricsStates() {
		if string(known) == s {
			return true
		}
	}
	return false
}

// HasSidecar reports whether a track in this state is expected to have a
// lyrics file next to its audio. It is what lets the library detect drift
// between the catalogue and the filesystem.
func (s LyricsState) HasSidecar() bool {
	return s == LyricsAvailableSynced || s == LyricsAvailablePlain
}

// Lyrics is a provider result normalised into the one shape the backend works
// with. Synced and plain are not alternatives: a synchronised result always
// carries the plain text as well, so a reader that cannot render timestamps
// still has something to show.
type Lyrics struct {
	Provider     string `json:"provider"`
	SourceID     string `json:"source_id,omitempty"`
	Synced       bool   `json:"synced"`
	Instrumental bool   `json:"instrumental"`
	PlainText    string `json:"plain_text,omitempty"`
	LRC          string `json:"lrc,omitempty"`
}

// State classifies the result. A Synced flag without an LRC body is downgraded
// to plain rather than trusted, because the sidecar extension follows the
// state and a .lrc file without timestamps would be a lie to the media server.
func (l Lyrics) State() LyricsState {
	switch {
	case l.Instrumental:
		return LyricsInstrumental
	case l.Synced && strings.TrimSpace(l.LRC) != "":
		return LyricsAvailableSynced
	case strings.TrimSpace(l.PlainText) != "":
		return LyricsAvailablePlain
	default:
		return LyricsNotFound
	}
}

// Extension returns the sidecar extension for this result, or "" when no
// sidecar is written.
//
// Plain lyrics deliberately never go into a .lrc file: Plex documents .lrc as
// the timed format and .txt as the untimed one, and writing the documented
// form for each costs nothing.
func (l Lyrics) Extension() string {
	switch l.State() {
	case LyricsAvailableSynced:
		return ".lrc"
	case LyricsAvailablePlain:
		return ".txt"
	default:
		return ""
	}
}

// Body returns the text that belongs in the sidecar file.
func (l Lyrics) Body() string {
	if l.State() == LyricsAvailableSynced {
		return l.LRC
	}
	return l.PlainText
}
