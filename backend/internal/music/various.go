package music

import "strings"

// variousArtistsNames are the placeholder names providers use for a release
// that has no single artist.
//
// This list exists because the release *type* is not a reliable signal. Deezer
// reports genuine Various-Artists soundtracks with record_type "album" — for
// example "The Greatest Showman (Original Motion Picture Soundtrack)", whose
// album artist is Deezer's own placeholder entity rather than a performer.
// The placeholder name is therefore the strongest evidence a provider gives,
// and it is localised, so the spellings of the languages the backend requests
// are listed alongside the English one.
var variousArtistsNames = map[string]struct{}{
	"various artists":          {},
	"various":                  {},
	"va":                       {},
	"verschiedene interpreten": {},
	"verschiedene künstler":    {},
	"diverse interpreten":      {},
	"artistes divers":          {},
	"multi-interprètes":        {},
	"varios artistas":          {},
	"vari artisti":             {},
	"diverse artiesten":        {},
	"vários intérpretes":       {},
	"olika artister":           {},
	"różni wykonawcy":          {},
	"çeşitli sanatçılar":       {},
	"soundtrack":               {},
	"original soundtrack":      {},
}

// IsVariousArtistsName reports whether a name is a provider's placeholder for
// "this release has no single artist" rather than the name of a performer.
func IsVariousArtistsName(name string) bool {
	_, ok := variousArtistsNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}
