package lock_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/lock"
)

func TestLockAcquisitionAndRelease(t *testing.T) {
	tmpDir := t.TempDir()

	l, err := lock.Acquire(tmpDir)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Verify .ytmdl directory exists with 0700
	dotYtmdl := filepath.Join(tmpDir, ".ytmdl")
	info, err := os.Stat(dotYtmdl)
	if err != nil {
		t.Fatalf("stat %s failed: %v", dotYtmdl, err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf(".ytmdl perm = %o, want 0700", info.Mode().Perm())
	}

	// Verify update.lock exists with 0600
	lockFile := filepath.Join(dotYtmdl, "update.lock")
	lockInfo, err := os.Stat(lockFile)
	if err != nil {
		t.Fatalf("stat %s failed: %v", lockFile, err)
	}
	if lockInfo.Mode().Perm() != 0600 {
		t.Errorf("update.lock perm = %o, want 0600", lockInfo.Mode().Perm())
	}

	// Concurrent lock attempt in same process / second instance must fail with ErrAlreadyLocked
	_, err = lock.Acquire(tmpDir)
	if !errors.Is(err, lock.ErrAlreadyLocked) {
		t.Fatalf("second Acquire = %v, want ErrAlreadyLocked", err)
	}

	// Release first lock
	if err := l.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// After release, acquiring lock again must succeed
	l2, err := lock.Acquire(tmpDir)
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}
	_ = l2.Release()
}

func TestCheckContention(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Non-existent lock file -> not locked, zero files created
	locked, info, err := lock.CheckContention(tmpDir)
	if err != nil || locked || info != nil {
		t.Fatalf("expected not locked, got locked = %v, info = %+v, err = %v", locked, info, err)
	}

	// Verify no files were created
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	if _, err := os.Stat(dotDir); !os.IsNotExist(err) {
		t.Fatalf("expected .ytmdl not to exist, got err: %v", err)
	}

	// 2. Acquired lock -> contention detected
	l, err := lock.Acquire(tmpDir)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer l.Release()

	locked, info, err = lock.CheckContention(tmpDir)
	if err != nil || !locked || info == nil {
		t.Fatalf("expected locked = true, got locked = %v, info = %+v, err = %v", locked, info, err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("info.PID = %d, want %d", info.PID, os.Getpid())
	}
}
