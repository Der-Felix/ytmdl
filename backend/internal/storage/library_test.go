package storage_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func newLibrary(t *testing.T) (*storage.Library, string) {
	t.Helper()
	root := t.TempDir()
	library, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}
	return library, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestPlaceNeverOverwritesAnotherFile is the regression test for the silent
// overwrite: two different recordings that normalise onto the same target must
// not cost the library a track.
func TestPlaceNeverOverwritesAnotherFile(t *testing.T) {
	library, root := newLibrary(t)

	destination := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	writeFile(t, destination, "the first recording")

	source := filepath.Join(t.TempDir(), "download.opus")
	writeFile(t, source, "a different recording")

	err := library.Place(source, destination)
	if err == nil {
		t.Fatal("Place overwrote an existing file")
	}
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
	if got := readFile(t, destination); got != "the first recording" {
		t.Fatalf("the existing file was modified: %q", got)
	}
	if got := readFile(t, source); got != "a different recording" {
		t.Fatalf("the source was consumed although the move failed: %q", got)
	}
}

func TestPlaceIntoAFreeDestinationSucceeds(t *testing.T) {
	library, root := newLibrary(t)

	source := filepath.Join(t.TempDir(), "download.opus")
	writeFile(t, source, "audio")
	destination := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")

	if err := library.Place(source, destination); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if got := readFile(t, destination); got != "audio" {
		t.Fatalf("destination = %q", got)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Error("the source file was not removed")
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("permissions = %o, want 644", perm)
	}
}

// TestPlaceIsIdempotentForTheSameFile covers a retried move: the destination
// exists because we put it there.
func TestPlaceIsIdempotentForTheSameFile(t *testing.T) {
	library, root := newLibrary(t)

	source := filepath.Join(root, "tmp", "download.opus")
	writeFile(t, source, "audio")
	destination := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, destination); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	if err := library.Place(source, destination); err != nil {
		t.Fatalf("Place on the same file must be idempotent: %v", err)
	}
	if got := readFile(t, destination); got != "audio" {
		t.Fatalf("destination = %q", got)
	}
}

func TestReplaceOverwritesDeliberately(t *testing.T) {
	library, root := newLibrary(t)

	destination := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	writeFile(t, destination, "old")
	source := filepath.Join(t.TempDir(), "download.opus")
	writeFile(t, source, "new")

	if err := library.Replace(source, destination); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got := readFile(t, destination); got != "new" {
		t.Fatalf("destination = %q, want new", got)
	}
}

func TestPlaceRefusesToLeaveTheLibrary(t *testing.T) {
	library, root := newLibrary(t)
	source := filepath.Join(t.TempDir(), "download.opus")
	writeFile(t, source, "audio")

	if err := library.Place(source, filepath.Join(root, "..", "escape.opus")); err == nil {
		t.Fatal("a path outside the library must be refused")
	}
}

func TestWriteLyricsWritesTheMatchingSidecarAndRemovesTheOther(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "2001 - B", "01 - C.opus")
	writeFile(t, audio, "audio")

	txt, err := library.WriteLyrics(audio, music.Lyrics{Provider: "ytmusic", PlainText: "line"})
	if err != nil {
		t.Fatalf("WriteLyrics plain: %v", err)
	}
	if filepath.Base(txt) != "01 - C.txt" {
		t.Fatalf("plain sidecar = %q", txt)
	}

	lrc, err := library.WriteLyrics(audio, music.Lyrics{
		Provider: "lrclib", Synced: true, LRC: "[00:01.00]line", PlainText: "line",
	})
	if err != nil {
		t.Fatalf("WriteLyrics synced: %v", err)
	}
	if filepath.Base(lrc) != "01 - C.lrc" {
		t.Fatalf("synced sidecar = %q", lrc)
	}
	if got := readFile(t, lrc); got != "[00:01.00]line" {
		t.Fatalf("sidecar body = %q", got)
	}
	if _, err := os.Stat(txt); !os.IsNotExist(err) {
		t.Error("the stale .txt sidecar was not removed")
	}
}

func TestWriteLyricsWithoutContentRemovesEverySidecar(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, audio, "audio")
	writeFile(t, filepath.Join(root, "A", "01 - C.lrc"), "[00:01.00]a")

	path, err := library.WriteLyrics(audio, music.Lyrics{Provider: "lrclib", Instrumental: true})
	if err != nil {
		t.Fatalf("WriteLyrics: %v", err)
	}
	if path != "" {
		t.Fatalf("an instrumental result must write no sidecar, got %q", path)
	}
	if _, err := os.Stat(filepath.Join(root, "A", "01 - C.lrc")); !os.IsNotExist(err) {
		t.Error("the sidecar survived")
	}
}

func TestWriteLyricsLeavesNoTemporaryFiles(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, audio, "audio")

	if _, err := library.WriteLyrics(audio, music.Lyrics{Provider: "lrclib", PlainText: "line"}); err != nil {
		t.Fatalf("WriteLyrics: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "A"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ytdm-") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestWriteLyricsRefusesAnOversizedBody(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, audio, "audio")

	huge := strings.Repeat("x", (256<<10)+1)
	if _, err := library.WriteLyrics(audio, music.Lyrics{Provider: "lrclib", PlainText: huge}); err == nil {
		t.Fatal("an oversized lyrics body must be refused")
	}
}

func TestReadLyricsPrefersTheSyncedSidecar(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, audio, "audio")
	writeFile(t, filepath.Join(root, "A", "01 - C.lrc"), "[00:01.00]a")
	writeFile(t, filepath.Join(root, "A", "01 - C.txt"), "a")

	path, body, err := library.ReadLyrics(audio)
	if err != nil {
		t.Fatalf("ReadLyrics: %v", err)
	}
	if filepath.Ext(path) != ".lrc" || body != "[00:01.00]a" {
		t.Fatalf("ReadLyrics = %q, %q", path, body)
	}
}

func TestReadLyricsWithoutASidecarIsNotAnError(t *testing.T) {
	library, root := newLibrary(t)
	audio := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, audio, "audio")

	path, body, err := library.ReadLyrics(audio)
	if err != nil {
		t.Fatalf("ReadLyrics: %v", err)
	}
	if path != "" || body != "" {
		t.Fatalf("ReadLyrics = %q, %q, want empty", path, body)
	}
}

func TestMoveTrackMovesAudioAndSidecarTogether(t *testing.T) {
	library, root := newLibrary(t)
	source := filepath.Join(root, "A & B", "2025 - C [Single]", "01 - C.opus")
	writeFile(t, source, "audio")
	writeFile(t, storage.SidecarPathFor(source, ".lrc"), "[00:01.00]a")

	destination := filepath.Join(root, "A", "2025 - C [Single]", "01 - C.opus")
	if err := library.MoveTrack(source, destination); err != nil {
		t.Fatalf("MoveTrack: %v", err)
	}
	if got := readFile(t, destination); got != "audio" {
		t.Fatalf("audio = %q", got)
	}
	if got := readFile(t, storage.SidecarPathFor(destination, ".lrc")); got != "[00:01.00]a" {
		t.Fatalf("sidecar = %q", got)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Error("the source audio survived")
	}
	if _, err := os.Stat(storage.SidecarPathFor(source, ".lrc")); !os.IsNotExist(err) {
		t.Error("the source sidecar survived")
	}
}

// TestMoveTrackAbortsWholeMoveOnSidecarConflict is the "no half move" rule: a
// conflict on the sidecar must leave the audio file exactly where it was.
func TestMoveTrackAbortsWholeMoveOnSidecarConflict(t *testing.T) {
	library, root := newLibrary(t)
	source := filepath.Join(root, "A & B", "2025 - C [Single]", "01 - C.opus")
	writeFile(t, source, "audio")
	writeFile(t, storage.SidecarPathFor(source, ".lrc"), "source lyrics")

	destination := filepath.Join(root, "A", "2025 - C [Single]", "01 - C.opus")
	writeFile(t, storage.SidecarPathFor(destination, ".lrc"), "other lyrics")

	err := library.MoveTrack(source, destination)
	if err == nil {
		t.Fatal("a taken sidecar destination must abort the move")
	}
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
	if got := readFile(t, source); got != "audio" {
		t.Fatalf("the audio file did not stay put: %q", got)
	}
	if got := readFile(t, storage.SidecarPathFor(source, ".lrc")); got != "source lyrics" {
		t.Fatalf("the source sidecar changed: %q", got)
	}
	if got := readFile(t, storage.SidecarPathFor(destination, ".lrc")); got != "other lyrics" {
		t.Fatalf("the foreign sidecar was overwritten: %q", got)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Error("the audio file was moved even though the move aborted")
	}
}

func TestMoveTrackRefusesATakenAudioDestination(t *testing.T) {
	library, root := newLibrary(t)
	source := filepath.Join(root, "A & B", "01 - C.opus")
	writeFile(t, source, "audio")
	destination := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, destination, "another recording")

	err := library.MoveTrack(source, destination)
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
	if got := readFile(t, destination); got != "another recording" {
		t.Fatalf("the destination was overwritten: %q", got)
	}
}

func TestMoveTrackToItselfIsANoOp(t *testing.T) {
	library, root := newLibrary(t)
	path := filepath.Join(root, "A", "01 - C.opus")
	writeFile(t, path, "audio")

	if err := library.MoveTrack(path, path); err != nil {
		t.Fatalf("MoveTrack: %v", err)
	}
	if got := readFile(t, path); got != "audio" {
		t.Fatalf("file = %q", got)
	}
}

func TestWriteCoverUsesTheExtensionOfTheImage(t *testing.T) {
	library, root := newLibrary(t)
	dir := filepath.Join(root, "A", "2001 - B")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	png, err := library.WriteCover(dir, ".png", []byte("png-bytes"))
	if err != nil {
		t.Fatalf("WriteCover png: %v", err)
	}
	if filepath.Base(png) != "cover.png" {
		t.Fatalf("cover path = %q, want cover.png", png)
	}

	jpg, err := library.WriteCover(dir, ".jpg", []byte("jpg-bytes"))
	if err != nil {
		t.Fatalf("WriteCover jpg: %v", err)
	}
	if filepath.Base(jpg) != "cover.jpg" {
		t.Fatalf("cover path = %q, want cover.jpg", jpg)
	}
}

func TestCommitStaged_FreshSuccess(t *testing.T) {
	library, root := newLibrary(t)
	stagingDir := t.TempDir()
	source := filepath.Join(stagingDir, "audio.opus")
	content := "staged-audio-content-12345"
	writeFile(t, source, content)

	hash, size, err := storage.ComputeChecksum(source)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	destination := filepath.Join(root, "Artist", "2024 - Album", "01 - Song.opus")
	alreadyCommitted, err := library.CommitStaged(source, destination, hash, size)
	if err != nil {
		t.Fatalf("CommitStaged failed: %v", err)
	}
	if alreadyCommitted {
		t.Errorf("expected alreadyCommitted=false for fresh commit")
	}

	if got := readFile(t, destination); got != content {
		t.Fatalf("destination content = %q, want %q", got, content)
	}
}

func TestCommitStaged_IdempotentCrashRecovery(t *testing.T) {
	library, root := newLibrary(t)
	stagingDir := t.TempDir()
	source := filepath.Join(stagingDir, "audio.opus")
	content := "staged-audio-content-12345"
	writeFile(t, source, content)

	hash, size, err := storage.ComputeChecksum(source)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	destination := filepath.Join(root, "Artist", "2024 - Album", "01 - Song.opus")
	// Pre-place the exact file at destination to simulate post-commit crash before DB update
	writeFile(t, destination, content)

	alreadyCommitted, err := library.CommitStaged(source, destination, hash, size)
	if err != nil {
		t.Fatalf("CommitStaged failed on idempotent recovery: %v", err)
	}
	if !alreadyCommitted {
		t.Errorf("expected alreadyCommitted=true on matching destination hash")
	}
}

func TestCommitStaged_PathConflictOnDifferentFile(t *testing.T) {
	library, root := newLibrary(t)
	stagingDir := t.TempDir()
	source := filepath.Join(stagingDir, "audio.opus")
	content := "staged-audio-content-12345"
	writeFile(t, source, content)

	hash, size, _ := storage.ComputeChecksum(source)

	destination := filepath.Join(root, "Artist", "2024 - Album", "01 - Song.opus")
	// Pre-place a DIFFERENT file at destination
	writeFile(t, destination, "foreign-different-audio-content")

	_, err := library.CommitStaged(source, destination, hash, size)
	if err == nil {
		t.Fatal("expected error on conflicting destination file, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("expected CodePathConflict, got %v", apperr.CodeOf(err))
	}
}

func TestCommitStaged_GuardMissingRejection(t *testing.T) {
	library, root := newLibrary(t)
	library.SetGuard(storage.NewStorageGuard(root, "required-guard-uuid", 0))

	stagingDir := t.TempDir()
	source := filepath.Join(stagingDir, "audio.opus")
	writeFile(t, source, "audio")
	hash, size, _ := storage.ComputeChecksum(source)

	destination := filepath.Join(root, "Artist", "2024 - Album", "01 - Song.opus")
	_, err := library.CommitStaged(source, destination, hash, size)
	if err == nil {
		t.Fatal("expected error when guard marker missing, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeStorageGuardMismatch {
		t.Fatalf("expected CodeStorageGuardMismatch, got %v", apperr.CodeOf(err))
	}
}

func TestCommitStaged_ConcurrentForeignRace_NoReplace(t *testing.T) {
	library, root := newLibrary(t)
	stagingDir := t.TempDir()

	for i := 0; i < 20; i++ {
		source := filepath.Join(stagingDir, fmt.Sprintf("staged-%d.opus", i))
		stagedContent := fmt.Sprintf("staged-track-content-%d", i)
		writeFile(t, source, stagedContent)
		stagedHash, stagedSize, err := storage.ComputeChecksum(source)
		if err != nil {
			t.Fatalf("ComputeChecksum: %v", err)
		}

		destination := filepath.Join(root, fmt.Sprintf("Artist-%d", i), "2024 - Album", "01 - Track.opus")
		foreignContent := fmt.Sprintf("CRITICAL-FOREIGN-USER-DATA-%d", i)

		var wg sync.WaitGroup
		wg.Add(2)

		var commitErr error
		var alreadyCommitted bool
		var foreignWrote bool

		// 1. Foreign writer using atomic O_EXCL creation
		go func() {
			defer wg.Done()
			_ = os.MkdirAll(filepath.Dir(destination), 0o755)
			f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err == nil {
				_, _ = f.Write([]byte(foreignContent))
				_ = f.Close()
				foreignWrote = true
			}
		}()

		// 2. CommitStaged
		go func() {
			defer wg.Done()
			alreadyCommitted, commitErr = library.CommitStaged(source, destination, stagedHash, stagedSize)
		}()

		wg.Wait()

		finalData, err := os.ReadFile(destination)
		if err != nil {
			t.Fatalf("iteration %d: read destination: %v", i, err)
		}

		if foreignWrote {
			// Foreign writer won the race: destination must contain foreign content and CommitStaged must return PATH_CONFLICT
			if string(finalData) != foreignContent {
				t.Fatalf("iteration %d: foreign file was overwritten! got %q, want %q", i, string(finalData), foreignContent)
			}
			if commitErr == nil {
				t.Fatalf("iteration %d: CommitStaged returned nil error despite foreign file present!", i)
			}
			if apperr.CodeOf(commitErr) != apperr.CodePathConflict {
				t.Fatalf("iteration %d: expected CodePathConflict, got %v", i, apperr.CodeOf(commitErr))
			}
			if alreadyCommitted {
				t.Fatalf("iteration %d: alreadyCommitted was true on foreign file!", i)
			}
		} else {
			// CommitStaged won the race: destination must contain staged content and commitErr must be nil
			if string(finalData) != stagedContent {
				t.Fatalf("iteration %d: staged content was corrupted! got %q, want %q", i, string(finalData), stagedContent)
			}
			if commitErr != nil {
				t.Fatalf("iteration %d: CommitStaged failed: %v", i, commitErr)
			}
		}
	}
}

func TestCommitStaged_TOCTOU_DeterministicForeignCollision(t *testing.T) {
	library, root := newLibrary(t)
	stagingDir := t.TempDir()

	source := filepath.Join(stagingDir, "track.opus")
	writeFile(t, source, "staged-track-data")
	stagedHash, stagedSize, err := storage.ComputeChecksum(source)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	destination := filepath.Join(root, "Artist", "2024 - Album", "01 - Track.opus")
	foreignData := "CRITICAL-FOREIGN-PRE-EXISTING-TRACK"
	writeFile(t, destination, foreignData)

	// Call CommitStaged with destination already existing
	alreadyCommitted, commitErr := library.CommitStaged(source, destination, stagedHash, stagedSize)
	if commitErr == nil {
		t.Fatal("expected CommitStaged to fail on existing foreign file")
	}
	if apperr.CodeOf(commitErr) != apperr.CodePathConflict {
		t.Fatalf("expected CodePathConflict, got %v", apperr.CodeOf(commitErr))
	}
	if alreadyCommitted {
		t.Fatal("expected alreadyCommitted=false on foreign file")
	}

	// Verify foreign file was preserved 100% byte-for-byte
	afterData := readFile(t, destination)
	if afterData != foreignData {
		t.Fatalf("foreign file corrupted! got %q, want %q", afterData, foreignData)
	}
}

func TestWriteCover_LocalFS_Succeeds(t *testing.T) {
	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	path, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err != nil {
		t.Fatalf("WriteCover failed on local filesystem: %v", err)
	}
	if filepath.Base(path) != "cover.jpg" {
		t.Fatalf("unexpected cover path: %s", path)
	}
}

func TestWriteCover_LocalFS_RejectsEPERM(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EPERM
	})
	defer restoreChmod()

	restoreFS := storage.SetQueryFSFuncForTest(func(path string) (string, uint64, uint64, uint64, error) {
		return "ext4", 1000, 1000, 1000, nil
	})
	defer restoreFS()

	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	_, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err == nil {
		t.Fatal("expected WriteCover to fail with EPERM on local filesystem (ext4)")
	}
}

func TestWriteCover_CIFS_ToleratesEPERM(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EPERM
	})
	defer restoreChmod()

	restoreFS := storage.SetQueryFSFuncForTest(func(path string) (string, uint64, uint64, uint64, error) {
		return "CIFS/SMB", 1000, 1000, 1000, nil
	})
	defer restoreFS()

	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	path, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err != nil {
		t.Fatalf("WriteCover failed under simulated EPERM on CIFS: %v", err)
	}
	if filepath.Base(path) != "cover.jpg" {
		t.Fatalf("unexpected cover path: %s", path)
	}

	content := readFile(t, path)
	if content != string(coverData) {
		t.Fatalf("cover content corrupted! got %q, want %q", content, string(coverData))
	}
}

func TestWriteCover_CIFS_ToleratesEOPNOTSUPP(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EOPNOTSUPP
	})
	defer restoreChmod()

	restoreFS := storage.SetQueryFSFuncForTest(func(path string) (string, uint64, uint64, uint64, error) {
		return "CIFS/SMB", 1000, 1000, 1000, nil
	})
	defer restoreFS()

	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	path, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err != nil {
		t.Fatalf("WriteCover failed under simulated EOPNOTSUPP on CIFS: %v", err)
	}
	content := readFile(t, path)
	if content != string(coverData) {
		t.Fatalf("cover content corrupted! got %q, want %q", content, string(coverData))
	}
}

func TestWriteCover_FailsOnEACCES(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EACCES
	})
	defer restoreChmod()

	restoreFS := storage.SetQueryFSFuncForTest(func(path string) (string, uint64, uint64, uint64, error) {
		return "CIFS/SMB", 1000, 1000, 1000, nil
	})
	defer restoreFS()

	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	_, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err == nil {
		t.Fatal("expected WriteCover to fail under simulated EACCES even on CIFS")
	}

	// Verify no stray temp files left behind in release directory
	entries, _ := os.ReadDir(releaseDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ytdm-write-") {
			t.Errorf("found leftover temp file after error: %s", e.Name())
		}
	}
}

func TestWriteCover_FailsOnEROFS(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EROFS
	})
	defer restoreChmod()

	library, root := newLibrary(t)
	releaseDir := filepath.Join(root, "Artist", "2024 - Album")
	coverData := []byte("fake-jpeg-cover-data")

	_, err := library.WriteCover(releaseDir, ".jpg", coverData)
	if err == nil {
		t.Fatal("expected WriteCover to fail under simulated EROFS")
	}
}

func TestWriteLyrics_CIFS_ToleratesEPERM(t *testing.T) {
	restoreChmod := storage.SetChmodFuncForTest(func(name string, mode os.FileMode) error {
		return syscall.EPERM
	})
	defer restoreChmod()

	restoreFS := storage.SetQueryFSFuncForTest(func(path string) (string, uint64, uint64, uint64, error) {
		return "CIFS/SMB", 1000, 1000, 1000, nil
	})
	defer restoreFS()

	library, root := newLibrary(t)
	audioPath := filepath.Join(root, "Artist", "2024 - Album", "01 - Track.opus")
	writeFile(t, audioPath, "audio-data")

	lyrics := music.Lyrics{
		LRC:       "[00:01.00] Line 1\n[00:05.00] Line 2",
		PlainText: "Line 1\nLine 2",
		Synced:    true,
	}

	path, err := library.WriteLyrics(audioPath, lyrics)
	if err != nil {
		t.Fatalf("WriteLyrics failed under simulated EPERM: %v", err)
	}
	if filepath.Ext(path) != ".lrc" {
		t.Fatalf("expected .lrc extension, got: %s", path)
	}

	content := readFile(t, path)
	if content != lyrics.LRC {
		t.Fatalf("lyrics content mismatch! got %q, want %q", content, lyrics.LRC)
	}
}
