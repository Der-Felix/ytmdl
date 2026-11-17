package storage

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// CoverFileName is the default name of the cover image written next to a
// release. "cover" is accepted as an album's primary image by Plex, Jellyfin
// and Emby alike.
const CoverFileName = "cover.jpg"

// Layout builds library paths below a fixed root.
type Layout struct {
	root string
}

// NewLayout returns a Layout rooted at an absolute, cleaned directory.
func NewLayout(root string) (*Layout, error) {
	if strings.TrimSpace(root) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The library path must not be empty.")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The library path could not be resolved.", err)
	}
	return &Layout{root: filepath.Clean(abs)}, nil
}

// Root returns the library root.
func (l *Layout) Root() string { return l.root }

// NamingScheme identifies one version of the rules that turn catalog metadata
// into library paths. A library written under an older scheme is moved to the
// current one by the library service's naming migration.
type NamingScheme int

const (
	// NamingSchemeLegacy is the scheme up to v1.4: refused characters became
	// "-" or "_", and disc 2, track 2 of a multi disc release was "202 - Title".
	NamingSchemeLegacy NamingScheme = 1
	// NamingSchemeFullwidth replaces refused characters with their fullwidth
	// look-alikes and numbers multi disc tracks "2-02 - Title".
	NamingSchemeFullwidth NamingScheme = 2

	// CurrentNamingScheme is the scheme new files are written with.
	CurrentNamingScheme = NamingSchemeFullwidth
)

// ArtistDir returns the directory of an artist.
func (l *Layout) ArtistDir(artist string) (string, error) {
	return l.join(SanitizeComponent(fallback(artist, music.UnknownArtist)))
}

// ReleaseDirName renders the directory name of a release: "2000 - Album" for
// albums, with the release type appended for everything else so that singles
// and compilations stay distinguishable.
func ReleaseDirName(release music.Release) string {
	return releaseDirName(release, CurrentNamingScheme)
}

func releaseDirName(release music.Release, scheme NamingScheme) string {
	title := fallback(release.Title, "Unknown Album")
	name := title
	if release.Year > 0 {
		name = fmt.Sprintf("%04d - %s", release.Year, title)
	}
	if suffix := releaseTypeSuffix(release.ReleaseType); suffix != "" {
		name += " " + suffix
	}
	return sanitizeComponent(name, scheme)
}

func releaseTypeSuffix(t music.ReleaseType) string {
	switch t {
	case music.ReleaseSingle:
		return "[Single]"
	case music.ReleaseEP:
		return "[EP]"
	case music.ReleaseLive:
		return "[Live]"
	case music.ReleaseCompilation:
		return "[Compilation]"
	case music.ReleaseRemix:
		return "[Remix]"
	default:
		return ""
	}
}

// ReleaseDirRel returns the relative directory a release is stored in within the library:
// "<Artist>/<ReleaseDirName>".
func ReleaseDirRel(release music.Release) string {
	return ReleaseDirRelFor(CurrentNamingScheme, release)
}

// ReleaseDirRelFor is ReleaseDirRel under a given naming scheme.
func ReleaseDirRelFor(scheme NamingScheme, release music.Release) string {
	artist := sanitizeComponent(fallback(release.DisplayAlbumArtist(), music.UnknownArtist), scheme)
	return filepath.Join(artist, releaseDirName(release, scheme))
}

// TrackRelPathFor returns the library relative path of a track under a given
// naming scheme: "<Artist>/<Release>/<TrackFileName>".
func TrackRelPathFor(scheme NamingScheme, release music.Release, track music.Track, ext string) string {
	return filepath.Join(ReleaseDirRelFor(scheme, release), TrackFileNameFor(scheme, track, ext))
}

// ReleaseDir returns the directory a release is stored in.
func (l *Layout) ReleaseDir(release music.Release) (string, error) {
	return l.join(ReleaseDirRel(release))
}

// TrackFileName renders the file name of a track including its extension.
//
// A multi disc release prepends the disc number and a hyphen to the track
// number: "3-02 - Track.ext" is disc three, track two, and track 105 on disc
// two is "2-105 - Track.ext". Unlike the run-together "302" form it cannot be
// misread (disc 1 track 102 and disc 11 track 2 were both "1102"). Every track
// of a release stays in one directory, which is what Jellyfin requires and
// what Emby needs in order to store local artwork next to an album. All three
// media servers take disc and track from the tags, which are written
// independently of this name.
func TrackFileName(track music.Track, ext string) string {
	return TrackFileNameFor(CurrentNamingScheme, track, ext)
}

// TrackFileNameFor is TrackFileName under a given naming scheme.
func TrackFileNameFor(scheme NamingScheme, track music.Track, ext string) string {
	title := fallback(track.Title, "Unknown Title")

	var prefix string
	switch {
	case track.DiscTotal > 1 && track.TrackNumber > 0:
		disc := track.DiscNumber
		if disc <= 0 {
			disc = 1
		}
		if scheme == NamingSchemeLegacy {
			prefix = fmt.Sprintf("%d%02d - ", disc, track.TrackNumber)
		} else {
			prefix = fmt.Sprintf("%d-%02d - ", disc, track.TrackNumber)
		}
	case track.TrackNumber > 0:
		prefix = fmt.Sprintf("%02d - ", track.TrackNumber)
	}
	return sanitizeFilename(prefix+title, ext, scheme)
}

// SourceSuffix is the stable marker appended to a file name when a different
// recording already holds the conventional name: " [source-<16 hex>]", derived
// from the media provider and its ID. An empty provider or ID yields "".
func SourceSuffix(provider, id string) string {
	provider, id = strings.TrimSpace(provider), strings.TrimSpace(id)
	if provider == "" || id == "" {
		return ""
	}
	key := sha256.Sum256([]byte(provider + "\x00" + id))
	return fmt.Sprintf(" [source-%x]", key[:8])
}

// sourceSuffixPattern recognises the marker SourceSuffix produces at the end
// of a file name without its extension.
var sourceSuffixPattern = regexp.MustCompile(` \[source-[0-9a-f]{16}\]$`)

// WithSourceSuffix inserts suffix in front of the extension of a file name,
// shortening the name part so the whole stays within MaxComponentLength bytes.
// The cut never splits a UTF-8 sequence.
func WithSourceSuffix(fileName, suffix string) string {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	for len(base)+len(suffix)+len(ext) > MaxComponentLength && base != "" {
		_, width := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-width]
	}
	return base + suffix + ext
}

// SplitSourceSuffix reports the source marker a file name carries, if any.
func SplitSourceSuffix(fileName string) (suffix string, ok bool) {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	loc := sourceSuffixPattern.FindStringIndex(base)
	if loc == nil {
		return "", false
	}
	return base[loc[0]:], true
}

// LyricsExtensions lists every sidecar extension the backend writes, most
// specific first. Plex documents .lrc as the timed format and .txt as the
// untimed one; Jellyfin accepts both. Removing lyrics has to clear all of
// them, because a track can change from plain to synchronised.
func LyricsExtensions() []string { return []string{".lrc", ".txt"} }

// SidecarPathFor replaces the extension of an audio path with ext.
//
// It is the only way a sidecar path is ever built. All three media servers
// require the lyrics file to differ from its track in nothing but the
// extension, so deriving it from the audio path — rather than rendering a name
// a second time — is what keeps the two from drifting apart when the naming
// rules change.
func SidecarPathFor(audioPath, ext string) string {
	return strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ext
}

// LyricsPath returns the sidecar path of a track inside the library.
func (l *Layout) LyricsPath(release music.Release, track music.Track, audioExt, lyricsExt string) (string, error) {
	audio, err := l.TrackPath(release, track, audioExt)
	if err != nil {
		return "", err
	}
	return l.contain(SidecarPathFor(audio, lyricsExt))
}

// CoverFileNameFor returns the cover file name for an image extension. The
// name has to match the actual image format: a PNG stored as "cover.jpg" is a
// file whose name lies about its contents.
func CoverFileNameFor(ext string) string {
	if strings.EqualFold(strings.TrimSpace(ext), ".png") {
		return "cover.png"
	}
	return CoverFileName
}

// TrackPath returns the absolute path of a track file inside the library.
func (l *Layout) TrackPath(release music.Release, track music.Track, ext string) (string, error) {
	dir, err := l.ReleaseDir(release)
	if err != nil {
		return "", err
	}
	return l.contain(filepath.Join(dir, TrackFileName(track, ext)))
}

// CoverPath returns the path of the cover file of a release directory for an
// image of the given extension.
func (l *Layout) CoverPath(releaseDir, ext string) (string, error) {
	return l.contain(filepath.Join(releaseDir, CoverFileNameFor(ext)))
}

// CoverPaths returns every cover file name a release directory may hold, so
// callers can find or remove an existing cover regardless of its format.
func (l *Layout) CoverPaths(releaseDir string) ([]string, error) {
	out := make([]string, 0, 2)
	for _, ext := range []string{".jpg", ".png"} {
		path, err := l.CoverPath(releaseDir, ext)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

// join builds a path from already sanitised components and verifies that the
// result stays inside the library root.
func (l *Layout) join(components ...string) (string, error) {
	parts := make([]string, 0, len(components)+1)
	parts = append(parts, l.root)
	for _, c := range components {
		if c == "" {
			return "", apperr.New(apperr.CodeInternal, "An empty path component was produced.")
		}
		parts = append(parts, c)
	}
	return l.contain(filepath.Join(parts...))
}

func (l *Layout) contain(path string) (string, error) {
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(l.root, clean)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return clean, nil
	}

	// Fallback to canonical comparison for symlinked roots (e.g. /var vs /private/var on macOS)
	canonicalRoot, _ := filepath.EvalSymlinks(l.root)
	if canonicalRoot == "" {
		canonicalRoot = l.root
	}
	canonicalPath, _ := filepath.EvalSymlinks(clean)
	if canonicalPath == "" {
		ancestor := clean
		var parts []string
		for {
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				break
			}
			parts = append([]string{filepath.Base(ancestor)}, parts...)
			ancestor = parent
			if resolvedParent, err := filepath.EvalSymlinks(ancestor); err == nil {
				canonicalPath = filepath.Join(append([]string{resolvedParent}, parts...)...)
				break
			}
		}
	}
	if canonicalPath != "" && canonicalRoot != "" {
		relCan, errCan := filepath.Rel(canonicalRoot, canonicalPath)
		if errCan == nil && relCan != ".." && !strings.HasPrefix(relCan, ".."+string(filepath.Separator)) && !filepath.IsAbs(relCan) {
			return clean, nil
		}
	}

	if err != nil {
		return "", apperr.Wrap(apperr.CodeInvalidRequest, "The resulting path is outside the library.", err)
	}
	return "", apperr.Newf(apperr.CodeInvalidRequest,
		"The resulting path %q is outside the library.", rel)
}

func fallback(value, def string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return def
}
