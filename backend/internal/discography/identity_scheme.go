package discography

import (
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
)

// Identity schemes. Every stored track row records the scheme its identity
// key was built under (tracks.identity_scheme), so that a change to how the
// key is built can be carried over to the stored rows by the identity
// migration instead of by lookups that know every older form for ever.
const (
	// IdentitySchemeUnrecorded covers every row written before schemes were
	// recorded (up to 1.5.0). Such a row carries one of the forms those
	// versions wrote: the current one, or the earlier reading of an
	// unbracketed remix title (up to 1.3.0).
	IdentitySchemeUnrecorded = 1
	// RemixReadingScheme is the first scheme whose rows are known to carry
	// the current reading of an unbracketed remix title. Rows below it may
	// still carry OlderSchemeKeys.
	RemixReadingScheme = 2
	// IdentityScheme is the scheme new rows are written under. Raising it
	// needs a registered identity migration step that brings the rows of the
	// previous scheme over.
	IdentityScheme = RemixReadingScheme
)

// KeyVerdict is what a scheme step concludes about one stored key.
type KeyVerdict int

const (
	// KeyCurrent means the stored key is already what the newer scheme
	// writes; only the row's scheme is raised.
	KeyCurrent KeyVerdict = iota
	// KeyRewrite means the stored key is exactly what the older scheme built
	// from the row and the newer scheme builds a different one.
	KeyRewrite
	// KeyNotDerivable means the stored key is neither: it was built from
	// fields the row no longer has (a title or artist credit updated later).
	// Recomputing it could change what the row is recognised as, so it is
	// left alone.
	KeyNotDerivable
)

// RereadRemixTitleKey is the step from IdentitySchemeUnrecorded to
// RemixReadingScheme for one stored row: t carries the row's stored title,
// artists and release type, stored its identity key. It returns the key the
// row should carry and the verdict.
//
// The live marker a release adds is deliberately not touched: a row written
// before the release counted keeps its key without it, exactly as before, and
// the lookup still tells such rows apart by their release (see
// Identity.LegacyLiveKeys). Only the title reading changes.
func RereadRemixTitleKey(t music.Track, stored string) (string, KeyVerdict) {
	titles := []string{t.Title}
	if t.Title == unknownTitle {
		// A track without a title is stored under the display fallback; its
		// key was built from the empty title.
		titles = append(titles, "")
	}
	for _, title := range titles {
		row := t
		row.Title = title
		current := matcher.Analyze(title)
		if stored == identityKey(row, current.Base, current.Versions) ||
			stored == identityKey(row, current.Base, current.Versions|matcher.VersionLive) {
			return stored, KeyCurrent
		}
	}
	for _, title := range titles {
		row := t
		row.Title = title
		if stored == olderSchemeKey(row) {
			current := matcher.Analyze(title)
			return identityKey(row, current.Base, current.Versions), KeyRewrite
		}
	}
	return stored, KeyNotDerivable
}

// unknownTitle is what music.Track.DisplayTitle stores for a track without a
// title.
var unknownTitle = music.Track{}.DisplayTitle()
