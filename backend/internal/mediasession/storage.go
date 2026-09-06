package mediasession

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

const (
	// CookieRefPrefix identifies managed cookie files.
	CookieRefPrefix = "managed://cookies/"

	cookieDirPerm  = 0700
	cookieFilePerm = 0600
)

var validIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// CookieStorage securely manages server-stored media session cookie files.
// Files are stored with restrictive permissions (0600) in a dedicated directory (0700).
// Writes and replacements are atomic via temporary files and fsync.
// Secret cookie contents are never logged or exposed in error messages.
type CookieStorage struct {
	baseDir string
	legacy  *LegacyAdapter
}

// NewCookieStorage initializes storage in baseDir. If baseDir does not exist,
// it is created with 0700 permissions.
func NewCookieStorage(baseDir string, legacy *LegacyAdapter) (*CookieStorage, error) {
	clean := filepath.Clean(strings.TrimSpace(baseDir))
	if clean == "" || clean == "." {
		return nil, apperr.New(apperr.CodeInvalidRequest, "cookie storage directory must be specified")
	}

	if err := os.MkdirAll(clean, cookieDirPerm); err != nil {
		return nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to create cookie storage directory", err)
	}

	// Canonicalize baseDir to resolve any symlinks (e.g. /var on macOS)
	canonicalBase, err := filepath.EvalSymlinks(clean)
	if err != nil {
		canonicalBase = clean
	}

	return &CookieStorage{
		baseDir: canonicalBase,
		legacy:  legacy,
	}, nil
}

// BaseDir returns the canonical base directory for cookie storage.
func (s *CookieStorage) BaseDir() string {
	return s.baseDir
}

// ValidateID checks that sessionID is non-empty, contains only safe characters,
// and cannot be used for directory traversal or path injection.
func ValidateID(sessionID string) error {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return apperr.New(apperr.CodeInvalidRequest, "session ID cannot be empty")
	}
	if len(trimmed) > 128 {
		return apperr.New(apperr.CodeInvalidRequest, "session ID exceeds maximum length")
	}
	if !validIDRegex.MatchString(trimmed) {
		return apperr.New(apperr.CodeInvalidRequest, "session ID contains invalid characters; must be alphanumeric, hyphen, or underscore")
	}
	return nil
}

func (s *CookieStorage) resolveFilePath(sessionID string) (string, error) {
	if err := ValidateID(sessionID); err != nil {
		return "", err
	}
	filename := sessionID + ".cookies.txt"
	target := filepath.Join(s.baseDir, filename)

	// Containment verification
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(s.baseDir, cleanTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", apperr.New(apperr.CodeInvalidRequest, "path traversal detected in cookie reference")
	}

	// Check for symlink escape if target already exists
	if fi, err := os.Lstat(cleanTarget); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", apperr.New(apperr.CodeInvalidRequest, "symlink escape detected in cookie storage")
		}
	}

	return cleanTarget, nil
}

// Store writes cookie data for sessionID atomically and returns the opaque cookie reference.
// The file is created with 0600 permissions.
func (s *CookieStorage) Store(sessionID string, content []byte) (string, error) {
	targetPath, err := s.resolveFilePath(sessionID)
	if err != nil {
		return "", err
	}

	if err := s.writeAtomic(targetPath, sessionID, content); err != nil {
		return "", err
	}

	return CookieRefPrefix + sessionID, nil
}

// Replace atomically overwrites the cookie file for sessionID.
func (s *CookieStorage) Replace(sessionID string, content []byte) error {
	targetPath, err := s.resolveFilePath(sessionID)
	if err != nil {
		return err
	}

	return s.writeAtomic(targetPath, sessionID, content)
}

// writeAtomic writes data to a temporary file in the same directory, syncs,
// sets 0600 mode, and atomically renames it onto targetPath.
func (s *CookieStorage) writeAtomic(targetPath, sessionID string, content []byte) error {
	tempName := fmt.Sprintf(".%s.tmp.%d", sessionID, time.Now().UnixNano())
	tempPath := filepath.Join(s.baseDir, tempName)

	f, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, cookieFilePerm)
	if err != nil {
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to create temporary cookie file", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to write cookie data", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to sync cookie data to disk", err)
	}

	if err := f.Close(); err != nil {
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to close temporary cookie file", err)
	}

	_ = os.Chmod(tempPath, cookieFilePerm)

	if err := os.Rename(tempPath, targetPath); err != nil {
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to commit cookie file", err)
	}

	cleanup = false
	_ = os.Chmod(targetPath, cookieFilePerm)
	return nil
}

// ResolvePath resolves an opaque cookie reference onto the local filesystem path.
// It supports managed references ("managed://cookies/<id>") and legacy references ("managed://legacy/default").
// Raw filesystem paths are never accepted.
func (s *CookieStorage) ResolvePath(cookieRef string) (string, error) {
	cookieRef = strings.TrimSpace(cookieRef)
	if cookieRef == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "cookie reference cannot be empty")
	}

	if cookieRef == LegacyCookieRef {
		if s.legacy != nil && s.legacy.IsConfigured() {
			return s.legacy.CookiePath(), nil
		}
		return "", apperr.New(apperr.CodeFileNotFound, "legacy cookie file is not configured or unavailable")
	}

	if !strings.HasPrefix(cookieRef, CookieRefPrefix) {
		return "", apperr.New(apperr.CodeInvalidRequest, "unrecognized cookie reference format")
	}

	sessionID := strings.TrimPrefix(cookieRef, CookieRefPrefix)
	targetPath, err := s.resolveFilePath(sessionID)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", apperr.New(apperr.CodeFileNotFound, "managed cookie file not found")
		}
		return "", apperr.Wrap(apperr.CodeStorageUnavailable, "failed to access managed cookie file", err)
	}
	if info.IsDir() {
		return "", apperr.New(apperr.CodeInvalidRequest, "target cookie file is a directory")
	}

	return targetPath, nil
}

// Read reads the content of a managed cookie file.
func (s *CookieStorage) Read(cookieRef string) ([]byte, error) {
	path, err := s.ResolvePath(cookieRef)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to read cookie file", err)
	}
	return data, nil
}

// Delete removes a managed cookie file. Legacy cookies are never deleted.
func (s *CookieStorage) Delete(cookieRef string) error {
	cookieRef = strings.TrimSpace(cookieRef)
	if cookieRef == LegacyCookieRef {
		// Never delete external legacy cookie files
		return nil
	}

	if !strings.HasPrefix(cookieRef, CookieRefPrefix) {
		return apperr.New(apperr.CodeInvalidRequest, "unrecognized cookie reference format")
	}

	sessionID := strings.TrimPrefix(cookieRef, CookieRefPrefix)
	targetPath, err := s.resolveFilePath(sessionID)
	if err != nil {
		return err
	}

	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.Wrap(apperr.CodeStorageUnavailable, "failed to delete cookie file", err)
	}
	return nil
}

// HasCookie reports whether a backing cookie file exists and is non-empty.
func (s *CookieStorage) HasCookie(cookieRef string) bool {
	if s == nil || strings.TrimSpace(cookieRef) == "" {
		return false
	}
	path, err := s.ResolvePath(cookieRef)
	if err != nil {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

// SaveCandidate writes a candidate cookie file for validation and probe testing.
// The file is created with 0600 mode and a cleanup function is returned.
func (s *CookieStorage) SaveCandidate(sessionID string, content []byte) (string, func(), error) {
	if err := ValidateID(sessionID); err != nil {
		return "", nil, err
	}
	candidateName := fmt.Sprintf(".%s.candidate.%d", sessionID, time.Now().UnixNano())
	candidatePath := filepath.Join(s.baseDir, candidateName)

	f, err := os.OpenFile(candidatePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, cookieFilePerm)
	if err != nil {
		return "", nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to create candidate cookie file", err)
	}

	cleanup := func() {
		_ = os.Remove(candidatePath)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to write candidate cookie file", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to sync candidate cookie file", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, apperr.Wrap(apperr.CodeStorageUnavailable, "failed to close candidate cookie file", err)
	}
	_ = os.Chmod(candidatePath, cookieFilePerm)
	return candidatePath, cleanup, nil
}

// PromoteCandidate atomically replaces the official cookie file with candidatePath.
func (s *CookieStorage) PromoteCandidate(sessionID string, candidatePath string) (string, error) {
	targetPath, err := s.resolveFilePath(sessionID)
	if err != nil {
		return "", err
	}
	_ = os.Chmod(candidatePath, cookieFilePerm)
	if err := os.Rename(candidatePath, targetPath); err != nil {
		return "", apperr.Wrap(apperr.CodeStorageUnavailable, "failed to promote candidate cookie file", err)
	}
	_ = os.Chmod(targetPath, cookieFilePerm)
	return CookieRefPrefix + sessionID, nil
}
