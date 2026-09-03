package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

var supportedAudioExtensions = map[string]struct{}{
	".opus": {},
	".ogg":  {},
	".oga":  {},
	".m4a":  {},
	".mp4":  {},
	".mp3":  {},
	".flac": {},
}

var reservedInternalDirs = map[string]struct{}{
	".ytmdl-trash":              {},
	".ytmdl-finalize":           {},
	".git":                      {},
	".stversions":               {},
	".trash":                    {},
	".trashes":                  {},
	".fseventsd":                {},
	".spotlight-v100":           {},
	"$recycle.bin":              {},
	"system volume information": {},
	"@eadir":                    {},
	".temporaryitems":           {},
}

// IsSupportedAudio reports whether ext is a recognised audio container extension.
func IsSupportedAudio(ext string) bool {
	_, ok := supportedAudioExtensions[strings.ToLower(ext)]
	return ok
}

// IsReservedDir reports whether a directory is an internal YTMDL storage directory
// or well-known filesystem metadata directory that should never be traversed.
func IsReservedDir(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	_, ok := reservedInternalDirs[name]
	return ok
}

// IsIgnoredFile reports whether name is a temporary file, hidden file, or cover image.
func IsIgnoredFile(name string) bool {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, ".") {
		return true
	}
	lower := strings.ToLower(name)
	if lower == "cover.jpg" || lower == "cover.jpeg" || lower == "cover.png" || lower == "cover.webp" {
		return true
	}
	return false
}

// DiscoveredFile is a physical audio file located within the library directory.
type DiscoveredFile struct {
	AbsPath   string
	RelPath   string
	SizeBytes int64
	ModTime   time.Time
	Extension string
}

// CanonicalRoot resolves root to its clean, symlink-evaluated absolute path.
func CanonicalRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "The library path must not be empty.")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The library path could not be resolved.", err)
	}
	if _, statErr := os.Stat(abs); statErr != nil {
		if os.IsNotExist(statErr) {
			return "", apperr.Newf(apperr.CodeStorageUnavailable, "The library root path %q does not exist.", root)
		}
		return "", apperr.Wrap(apperr.CodeInternal, "The library root path could not be accessed.", statErr)
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The library symlinks could not be resolved.", err)
	}
	return filepath.Clean(eval), nil
}

// VerifyPathConfinement validates that target lives strictly inside library root.
// For existing targets, symlinks are dereferenced.
// For missing targets (when allowMissing is true), parent directories and clean relative
// paths are validated so that missing files do not fail solely because they don't exist yet.
func VerifyPathConfinement(root, target string, allowMissing bool) (absPath string, relPath string, err error) {
	canonicalRoot, err := CanonicalRoot(root)
	if err != nil {
		return "", "", err
	}

	cleanTarget := target
	if !filepath.IsAbs(cleanTarget) {
		cleanTarget = filepath.Join(canonicalRoot, cleanTarget)
	}
	cleanTarget = filepath.Clean(cleanTarget)

	// Check if target exists
	info, statErr := os.Lstat(cleanTarget)
	if statErr == nil {
		resolved, evalErr := filepath.EvalSymlinks(cleanTarget)
		if evalErr != nil {
			return "", "", apperr.Wrap(apperr.CodeInvalidRequest, "Failed to resolve symlinks for target path.", evalErr)
		}
		rel, relErr := filepath.Rel(canonicalRoot, resolved)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", "", apperr.Newf(apperr.CodeInvalidRequest, "The path %q escapes the library directory.", target)
		}
		_ = info
		return resolved, rel, nil
	}

	if !allowMissing {
		return "", "", apperr.Wrap(apperr.CodeFileNotFound, "The specified file does not exist in the library.", statErr)
	}

	// Find the closest existing ancestor directory and canonicalize it.
	ancestor := cleanTarget
	var missingParts []string
	for {
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		missingParts = append([]string{filepath.Base(ancestor)}, missingParts...)
		ancestor = parent
		if _, err := os.Stat(ancestor); err == nil {
			break
		}
	}

	evalAncestor, evalErr := filepath.EvalSymlinks(ancestor)
	if evalErr != nil {
		evalAncestor = ancestor
	}

	canonicalTarget := filepath.Join(append([]string{evalAncestor}, missingParts...)...)
	rel, relErr := filepath.Rel(canonicalRoot, canonicalTarget)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", apperr.Newf(apperr.CodeInvalidRequest, "The missing path %q is outside the library directory.", target)
	}

	return canonicalTarget, rel, nil
}

// WalkLibraryFiles traverses the library root and returns all discovered audio files,
// strictly ignoring hidden/temp files, covers, non-audio formats, and symlinks escaping root.
func WalkLibraryFiles(root string) ([]DiscoveredFile, error) {
	canonicalRoot, err := CanonicalRoot(root)
	if err != nil {
		return nil, err
	}

	var discovered []DiscoveredFile

	err = filepath.WalkDir(canonicalRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		name := d.Name()

		if d.IsDir() {
			// Skip internal YTMDL and reserved system directories (.ytmdl-trash, .ytmdl-finalize, .git, etc.)
			if path != canonicalRoot && IsReservedDir(name) {
				return filepath.SkipDir
			}
			return nil
		}

		if IsIgnoredFile(name) {
			return nil
		}

		ext := filepath.Ext(name)
		if !IsSupportedAudio(ext) {
			return nil
		}

		// Security: verify symlink confinement for each candidate file
		resolvedAbs, rel, confErr := VerifyPathConfinement(canonicalRoot, path, false)
		if confErr != nil {
			// Ignore escaping symlinks or unreadable files
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		discovered = append(discovered, DiscoveredFile{
			AbsPath:   resolvedAbs,
			RelPath:   rel,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC(),
			Extension: strings.ToLower(ext),
		})
		return nil
	})

	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Failed to scan library filesystem.", err)
	}

	return discovered, nil
}
