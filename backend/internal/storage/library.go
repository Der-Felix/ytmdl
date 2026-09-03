package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// dirPerm and filePerm are the permissions new library entries are created
// with. They keep the library readable for the media server group while
// staying writable only for the service account.
const (
	dirPerm  os.FileMode = 0o755
	filePerm os.FileMode = 0o644
)

// maxLyricsBytes bounds a lyrics sidecar. Lyrics are a few kilobytes of text;
// anything larger is a provider defect, not a song.
const maxLyricsBytes = 256 << 10

// Library is the on disk music collection.
type Library struct {
	layout *Layout
	guard  *StorageGuard
}

// NewLibrary opens the library rooted at path, creating the root if needed.
func NewLibrary(root string) (*Library, error) {
	layout, err := NewLayout(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(layout.Root(), dirPerm); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The library directory could not be created.", err)
	}
	return &Library{
		layout: layout,
		guard:  NewStorageGuard(layout.Root(), "", 0),
	}, nil
}

// SetGuard configures the storage guard for the library.
func (l *Library) SetGuard(guard *StorageGuard) {
	l.guard = guard
}

// Guard returns the storage guard.
func (l *Library) Guard() *StorageGuard {
	return l.guard
}

// Layout exposes the path layout.
func (l *Library) Layout() *Layout { return l.layout }

// Root returns the library root directory.
func (l *Library) Root() string { return l.layout.Root() }

// EnsureReleaseDir creates and returns the directory of a release.
func (l *Library) EnsureReleaseDir(release music.Release) (string, error) {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return "", err
		}
	}
	dir, err := l.layout.ReleaseDir(release)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The release directory could not be created.", err)
	}
	return dir, nil
}

// TrackPath returns the absolute target path of a track.
func (l *Library) TrackPath(release music.Release, track music.Track, ext string) (string, error) {
	return l.layout.TrackPath(release, track, ext)
}

// Exists reports whether a regular file exists at path.
func (l *Library) Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (l *Library) RelPath(abs string) string {
	rel, err := filepath.Rel(l.layout.Root(), abs)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	canonicalRoot, _ := filepath.EvalSymlinks(l.layout.Root())
	if canonicalRoot == "" {
		canonicalRoot = l.layout.Root()
	}
	canonicalAbs, _ := filepath.EvalSymlinks(abs)
	if canonicalAbs == "" {
		canonicalAbs = abs
	}
	relCan, errCan := filepath.Rel(canonicalRoot, canonicalAbs)
	if errCan == nil && !strings.HasPrefix(relCan, "..") {
		return relCan
	}
	return abs
}

// CommitStaged safely moves a staged audio artifact from local staging to destination on target storage.
// It verifies Guard identity, copies to a target temporary file on the destination filesystem,
// verifies target size and SHA-256 hash, and atomically commits without overwriting different files.
// If the destination already exists with the exact expected hash, it succeeds idempotently (recovering from a previous crash).
func (l *Library) CommitStaged(source, destination, expectedSHA256 string, expectedSize int64) (alreadyCommitted bool, err error) {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return false, err
		}
	}

	safe, err := l.layout.contain(destination)
	if err != nil {
		return false, err
	}

	releaseDir := filepath.Dir(safe)
	if err := os.MkdirAll(releaseDir, dirPerm); err != nil {
		return false, apperr.Wrap(apperr.CodeInternal, "The release directory could not be created.", err)
	}

	// 1. Check if destination already exists
	if l.Exists(safe) {
		destHash, destSize, err := ComputeChecksum(safe)
		if err == nil && expectedSHA256 != "" && destSize == expectedSize && destHash == expectedSHA256 {
			// Idempotent recovery: this file was already successfully placed
			return true, nil
		}
		// A different file already occupies destination -> PATH_CONFLICT
		return false, conflict(l.RelPath(safe))
	}

	// 2. Create target temp directory inside library root
	finalizeDir := filepath.Join(l.Root(), ".ytmdl-finalize")
	if err := os.MkdirAll(finalizeDir, dirPerm); err != nil {
		return false, apperr.Wrap(apperr.CodeInternal, "The finalization temp directory could not be created.", err)
	}

	tempTarget := filepath.Join(finalizeDir, fmt.Sprintf(".ytmdl-%s-%d.tmp", filepath.Base(safe), time.Now().UnixNano()))
	defer os.Remove(tempTarget)

	out, err := os.OpenFile(tempTarget, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return false, apperr.Wrap(apperr.CodeInternal, "The target temporary file could not be created.", err)
	}

	if err := copyContents(source, out, tempTarget); err != nil {
		return false, err
	}

	// 3. Verify target temp file hash and size
	tempHash, tempSize, err := ComputeChecksum(tempTarget)
	if err != nil {
		return false, apperr.Wrap(apperr.CodeInternal, "Failed to verify target temporary file.", err)
	}
	if (expectedSize > 0 && tempSize != expectedSize) || (expectedSHA256 != "" && tempHash != expectedSHA256) {
		return false, apperr.Newf(apperr.CodeInternal, "Target verification checksum mismatch: %s != %s", tempHash, expectedSHA256)
	}

	// 4. Atomic No-Replace Commit (immune to TOCTOU races)
	if err := renameNoReplace(tempTarget, safe); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Target was created concurrently. Check if it matches expected hash for crash recovery.
			destHash, destSize, checkErr := ComputeChecksum(safe)
			if checkErr == nil && expectedSHA256 != "" && destSize == expectedSize && destHash == expectedSHA256 {
				return true, nil
			}
			return false, conflict(l.RelPath(safe))
		}
		return false, apperr.Wrap(apperr.CodeInternal, "Failed to commit target file into place.", err)
	}
	_ = os.Chmod(safe, filePerm)

	return false, nil
}

// Place moves a finished file into the library without ever overwriting
// another file.
func (l *Library) Place(source, destination string) error {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return err
		}
	}
	return l.move(source, destination, false)
}

// Replace moves a file into the library and overwrites the destination.
func (l *Library) Replace(source, destination string) error {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return err
		}
	}
	return l.move(source, destination, true)
}

// move implements Place and Replace.
func (l *Library) move(source, destination string, overwrite bool) error {
	safe, err := l.layout.contain(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(safe), dirPerm); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The target directory could not be created.", err)
	}

	if overwrite {
		return l.overwriteInto(source, safe)
	}

	// os.Link fails with EEXIST when the destination is taken. That makes the
	// existence check and the move one atomic step, so two workers racing for
	// the same target cannot both believe they won.
	switch err := os.Link(source, safe); {
	case err == nil:
		if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apperr.Wrap(apperr.CodeInternal, "The temporary file could not be removed.", err)
		}
		if err := chmodFunc(safe, filePerm); err != nil && !isChmodUnsupported(safe, err) {
			return apperr.Wrap(apperr.CodeInternal, "The file permissions could not be set.", err)
		}
		return nil

	case errors.Is(err, os.ErrExist):
		// The destination exists. It is only acceptable when it is literally
		// the file we were asked to move — a retry of a move that already
		// succeeded.
		if sameFile(source, safe) {
			_ = os.Remove(source)
			return nil
		}
		if !overwrite {
			return conflict(l.RelPath(safe))
		}
		return l.overwriteInto(source, safe)

	default:
		return apperr.Wrap(apperr.CodeInternal, "The file could not be moved into the library.", err)
	}
}

// copyInto copies source to an exclusively created destination and removes the
// source afterwards. O_EXCL is what keeps this from overwriting.
func (l *Library) copyInto(source, destination string) error {
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if errors.Is(err, os.ErrExist) {
		if sameFile(source, destination) {
			_ = os.Remove(source)
			return nil
		}
		return conflict(l.RelPath(destination))
	}
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The target file could not be created.", err)
	}
	if err := copyContents(source, out, destination); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, "The temporary file could not be removed.", err)
	}
	return nil
}

// overwriteInto replaces the destination with source.
func (l *Library) overwriteInto(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		if err := chmodFunc(destination, filePerm); err != nil && !isChmodUnsupported(destination, err) {
			return apperr.Wrap(apperr.CodeInternal, "The file permissions could not be set.", err)
		}
		return nil
	} else if !isCrossDevice(err) {
		return apperr.Wrap(apperr.CodeInternal, "The file could not be moved into the library.", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(destination), ".ytdm-place-*")
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The temporary file could not be created.", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := copyContents(source, temp, tempPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The file could not replace the existing one.", err)
	}
	if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, "The temporary file could not be removed.", err)
	}
	if err := chmodFunc(destination, filePerm); err != nil && !isChmodUnsupported(destination, err) {
		return apperr.Wrap(apperr.CodeInternal, "The file permissions could not be set.", err)
	}
	return nil
}

// WriteCover writes the cover image of a release directory under the name that
// matches the image's actual format.
func (l *Library) WriteCover(releaseDir, ext string, data []byte) (string, error) {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return "", err
		}
	}
	path, err := l.layout.CoverPath(releaseDir, ext)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The release directory could not be created.", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes a file from the library. Paths outside the library are
// refused.
func (l *Library) Remove(path string) error {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return err
		}
	}
	safe, err := l.layout.contain(path)
	if err != nil {
		return err
	}
	if err := os.Remove(safe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, "The file could not be removed.", err)
	}
	return nil
}

// WriteLyrics writes the sidecar belonging to an audio file and removes the
// sidecar of the other kind, so exactly one lyrics file exists per track.
//
// The new file is written completely before the old one is deleted, and it is
// written through a temporary file that is renamed into place, so a crash can
// never leave half a lyrics file next to a track. An empty result removes
// every sidecar and returns an empty path.
func (l *Library) WriteLyrics(audioPath string, lyrics music.Lyrics) (string, error) {
	if l.guard != nil {
		if err := l.guard.RequireWritable(); err != nil {
			return "", err
		}
	}
	safeAudio, err := l.layout.contain(audioPath)
	if err != nil {
		return "", err
	}

	ext := lyrics.Extension()
	if ext == "" {
		return "", l.RemoveLyrics(safeAudio)
	}

	body := lyrics.Body()
	if len(body) > maxLyricsBytes {
		return "", apperr.Newf(apperr.CodeUnsupportedMediaType,
			"The lyrics are larger than the allowed %d bytes.", maxLyricsBytes)
	}

	target := SidecarPathFor(safeAudio, ext)
	if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The lyrics directory could not be created.", err)
	}
	if err := writeFileAtomic(target, []byte(body)); err != nil {
		return "", err
	}

	// A track can change from plain to synchronised and back. Leaving the old
	// sidecar behind would give the media servers two contradictory answers.
	// It is removed only now, after the new one is safely on disk.
	for _, other := range LyricsExtensions() {
		if other == ext {
			continue
		}
		if err := os.Remove(SidecarPathFor(safeAudio, other)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", apperr.Wrap(apperr.CodeInternal, "The stale lyrics file could not be removed.", err)
		}
	}
	return target, nil
}

// RemoveLyrics deletes every sidecar of an audio file. The audio itself is
// never touched.
func (l *Library) RemoveLyrics(audioPath string) error {
	safeAudio, err := l.layout.contain(audioPath)
	if err != nil {
		return err
	}
	for _, ext := range LyricsExtensions() {
		if err := os.Remove(SidecarPathFor(safeAudio, ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return apperr.Wrap(apperr.CodeInternal, "The lyrics file could not be removed.", err)
		}
	}
	return nil
}

// ReadLyrics returns the sidecar of an audio file. A track without one yields
// empty results and no error: a missing sidecar is a normal state, and the
// catalogue may well claim otherwise.
func (l *Library) ReadLyrics(audioPath string) (string, string, error) {
	safeAudio, err := l.layout.contain(audioPath)
	if err != nil {
		return "", "", err
	}
	// .lrc first: a synchronised sidecar is the better answer if both exist.
	for _, ext := range LyricsExtensions() {
		path := SidecarPathFor(safeAudio, ext)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", "", apperr.Wrap(apperr.CodeInternal, "The lyrics file could not be read.", err)
		}
		return path, string(data), nil
	}
	return "", "", nil
}

// MoveTrack moves an audio file and its lyrics sidecar to a new path as one
// unit.
//
// Either everything moves or nothing does: every destination is checked before
// the first byte is moved, and a failure part way through moves what already
// arrived back. A track whose audio and lyrics ended up in different
// directories would be worse than one that was never moved at all.
func (l *Library) MoveTrack(source, destination string) error {
	safeSource, err := l.layout.contain(source)
	if err != nil {
		return err
	}
	safeDestination, err := l.layout.contain(destination)
	if err != nil {
		return err
	}
	if safeSource == safeDestination {
		return nil
	}

	type pending struct{ from, to string }
	moves := []pending{{safeSource, safeDestination}}
	for _, ext := range LyricsExtensions() {
		from := SidecarPathFor(safeSource, ext)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		moves = append(moves, pending{from, SidecarPathFor(safeDestination, ext)})
	}

	// Check every destination first. A conflict on the sidecar must not leave
	// the audio file already moved.
	for _, move := range moves {
		if _, err := os.Stat(move.to); err == nil {
			return conflict(l.RelPath(move.to))
		} else if !errors.Is(err, os.ErrNotExist) {
			return apperr.Wrap(apperr.CodeInternal, "The target path could not be inspected.", err)
		}
	}

	done := make([]pending, 0, len(moves))
	for _, move := range moves {
		if err := l.Place(move.from, move.to); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				// Best effort: put back what was already moved so the track
				// stays whole where it was.
				_ = l.Replace(done[i].to, done[i].from)
			}
			return err
		}
		done = append(done, move)
	}
	return nil
}

// conflict builds the error that stops an operation which would otherwise
// overwrite another file.
func conflict(relPath string) error {
	return apperr.Newf(apperr.CodePathConflict,
		"Another file already exists at %q. It was not overwritten.", relPath)
}

// sameFile reports whether two paths refer to the same file on disk.
func sameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

// chmodFunc is the function used to set file permissions. It defaults to os.Chmod,
// but can be overridden in tests to simulate filesystem error conditions.
var chmodFunc = os.Chmod

// queryFSFunc is used to query the filesystem type and space metrics.
// It defaults to QueryFS, but can be overridden in tests.
var queryFSFunc = QueryFS

// IsNetworkFilesystem reports whether the filesystem type corresponds to a network
// mount (CIFS/SMB) where POSIX chmod is unsupported or fixed at mount time.
func IsNetworkFilesystem(fsType string) bool {
	lower := strings.ToLower(fsType)
	return strings.Contains(lower, "cifs") ||
		strings.Contains(lower, "smb")
}

// isChmodUnsupported reports whether an error from os.Chmod indicates that the underlying
// filesystem does not support setting POSIX permissions (e.g. CIFS/SMB mounts returning
// EPERM, EOPNOTSUPP, or ENOTSUP). On local filesystems (ext4, xfs, btrfs, virtiofs, etc.),
// EPERM is treated as a genuine permission failure and is NOT tolerated.
func isChmodUnsupported(path string, err error) bool {
	if err == nil {
		return true
	}
	if !errors.Is(err, syscall.EPERM) &&
		!errors.Is(err, syscall.EOPNOTSUPP) &&
		!errors.Is(err, syscall.ENOTSUP) {
		return false
	}

	targetDir := path
	if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
		targetDir = filepath.Dir(path)
	}

	fsType, _, _, _, statErr := queryFSFunc(targetDir)
	if statErr != nil {
		return false
	}
	return IsNetworkFilesystem(fsType)
}

// writeFileAtomic writes data through a temporary file in the same directory
// and renames it into place, so a reader never sees a partial file.
func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".ytdm-write-*")
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The temporary file could not be created.", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return apperr.Wrap(apperr.CodeInternal, "The file could not be written.", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return apperr.Wrap(apperr.CodeInternal, "The file could not be flushed to disk.", err)
	}
	if err := temp.Close(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The file could not be closed.", err)
	}
	if err := chmodFunc(tempPath, filePerm); err != nil && !isChmodUnsupported(tempPath, err) {
		return apperr.Wrap(apperr.CodeInternal, "The file permissions could not be set.", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The file could not be renamed into place.", err)
	}
	return nil
}

// copyContents copies source into an already opened destination file.
func copyContents(source string, out *os.File, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		out.Close()
		os.Remove(destination)
		return apperr.Wrap(apperr.CodeInternal, "The downloaded file could not be opened.", err)
	}
	defer in.Close()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(destination)
		return apperr.Wrap(apperr.CodeInternal, "The file could not be copied into the library.", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(destination)
		return apperr.Wrap(apperr.CodeInternal, "The file could not be flushed to disk.", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(destination)
		return apperr.Wrap(apperr.CodeInternal, "The target file could not be closed.", err)
	}
	return nil
}

// isCrossDevice reports whether a link or rename failed because the two paths
// live on different filesystems.
func isCrossDevice(err error) bool {
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return strings.Contains(strings.ToLower(linkErr.Err.Error()), "cross-device")
	}
	return false
}
