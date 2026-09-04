// Package lock provides host-side concurrency mutual exclusion via OS flock.
package lock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var (
	// ErrAlreadyLocked is returned when another process holds the update lock.
	ErrAlreadyLocked = errors.New("update is already in progress by another process")
)

// Lock represents an acquired exclusive file lock.
type Lock struct {
	file *os.File
	path string
}

// LockInfo contains metadata read from a held lock file.
type LockInfo struct {
	PID       int
	StartedAt time.Time
}

// Acquire attempts to take an exclusive non-blocking lock on <projectDir>/.ytmdl/update.lock.
func Acquire(projectDir string) (*Lock, error) {
	dotDir := filepath.Join(projectDir, ".ytmdl")
	if err := os.MkdirAll(dotDir, 0700); err != nil {
		return nil, fmt.Errorf("lock: failed to create directory %s: %w", dotDir, err)
	}
	// Ensure directory permissions
	_ = os.Chmod(dotDir, 0700)

	lockPath := filepath.Join(dotDir, "update.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("lock: failed to open lock file %s: %w", lockPath, err)
	}

	fd := int(file.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		defer file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			info := readLockInfo(file)
			if info != nil && info.PID > 0 {
				return nil, fmt.Errorf("%w (PID %d since %s)", ErrAlreadyLocked, info.PID, info.StartedAt.Format(time.RFC3339))
			}
			return nil, ErrAlreadyLocked
		}
		return nil, fmt.Errorf("lock: flock failed: %w", err)
	}

	// Lock acquired: truncate and record PID and timestamp
	_ = file.Truncate(0)
	_, _ = file.Seek(0, io.SeekStart)
	metadata := fmt.Sprintf("pid=%d\nstarted_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	_, _ = file.WriteString(metadata)
	_ = file.Sync()

	return &Lock{
		file: file,
		path: lockPath,
	}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() {
		_ = l.file.Close()
		l.file = nil
	}()

	fd := int(l.file.Fd())
	if err := unix.Flock(fd, unix.LOCK_UN); err != nil {
		return fmt.Errorf("lock: release flock failed: %w", err)
	}
	return nil
}

// CheckContention checks if update.lock is currently locked by another process.
// It creates ZERO files, performing a read-only probe.
func CheckContention(projectDir string) (bool, *LockInfo, error) {
	lockPath := filepath.Join(projectDir, ".ytmdl", "update.lock")
	file, err := os.OpenFile(lockPath, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("lock: failed opening lock file: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			info := readLockInfo(file)
			return true, info, nil
		}
		return false, nil, fmt.Errorf("lock: check flock failed: %w", err)
	}

	// Uncontended: release immediately
	_ = unix.Flock(fd, unix.LOCK_UN)
	return false, nil, nil
}

func readLockInfo(f *os.File) *LockInfo {
	_, _ = f.Seek(0, io.SeekStart)
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return nil
	}

	lines := strings.Split(string(buf[:n]), "\n")
	info := &LockInfo{}
	for _, line := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "pid":
			if p, err := strconv.Atoi(v); err == nil {
				info.PID = p
			}
		case "started_at":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				info.StartedAt = t
			}
		}
	}
	return info
}
