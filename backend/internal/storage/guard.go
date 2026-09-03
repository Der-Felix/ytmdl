package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
)

// MarkerFileName is the required marker file on the library root when Guard is enabled.
const MarkerFileName = ".ytmdl-storage-id"

// MarkerPrefix is the required prefix inside the marker file.
const MarkerPrefix = "ytmdl-storage:"

// GuardStatus describes the result of identity verification.
type GuardStatus string

const (
	GuardDisabled GuardStatus = "disabled"
	GuardVerified GuardStatus = "verified"
	GuardMissing  GuardStatus = "missing"
	GuardMismatch GuardStatus = "mismatch"
)

// HealthStatus describes the overall operational health of the storage.
type HealthStatus string

const (
	HealthHealthy       HealthStatus = "healthy"
	HealthDegraded      HealthStatus = "degraded"
	HealthUnavailable   HealthStatus = "unavailable"
	HealthGuardMissing  HealthStatus = "guard_missing"
	HealthGuardMismatch HealthStatus = "guard_mismatch"
	HealthReadOnly      HealthStatus = "read_only"
	HealthLowSpace      HealthStatus = "low_space"
	HealthUnknown       HealthStatus = "unknown"
)

// StorageHealth holds snapshot diagnostics about library storage.
type StorageHealth struct {
	Path           string       `json:"path"`
	Status         HealthStatus `json:"status"`
	Filesystem     string       `json:"filesystem"`
	GuardStatus    GuardStatus  `json:"guard_status"`
	Writable       bool         `json:"writable"`
	TotalBytes     uint64       `json:"total_bytes"`
	FreeBytes      uint64       `json:"free_bytes"`
	AvailableBytes uint64       `json:"available_bytes"`
	MinFreeBytes   int64        `json:"min_free_bytes"`
	LastChecked    time.Time    `json:"last_checked"`
	LastError      string       `json:"last_error,omitempty"`
}

// StorageGuard protects the library root from unmounted/missing network shares.
type StorageGuard struct {
	root         string
	guardID      string
	minFreeBytes int64
	cacheTTL     time.Duration

	mu           sync.Mutex
	probeActive  bool
	cachedHealth StorageHealth
}

// NewStorageGuard creates a StorageGuard for root.
func NewStorageGuard(root string, guardID string, minFreeBytes int64) *StorageGuard {
	return &StorageGuard{
		root:         root,
		guardID:      strings.TrimSpace(guardID),
		minFreeBytes: minFreeBytes,
		cacheTTL:     30 * time.Second,
	}
}

// Root returns the configured root path.
func (g *StorageGuard) Root() string {
	return g.root
}

// GuardID returns the configured guard ID.
func (g *StorageGuard) GuardID() string {
	return g.guardID
}

// MinFreeBytes returns the configured minimum free space threshold.
func (g *StorageGuard) MinFreeBytes() int64 {
	return g.minFreeBytes
}

// ValidateIdentity strictly verifies the storage marker file.
// It is read-only and never writes or creates files/directories.
func (g *StorageGuard) ValidateIdentity() (GuardStatus, error) {
	if g.guardID == "" {
		return GuardDisabled, nil
	}

	markerPath := filepath.Join(g.root, MarkerFileName)
	fi, err := os.Lstat(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return GuardMissing, apperr.Newf(apperr.CodeStorageGuardMismatch,
				"Storage guard marker %s is missing from library root %s", MarkerFileName, g.root)
		}
		return GuardMissing, apperr.Wrapf(apperr.CodeStorageUnavailable, err,
			"Failed to inspect storage guard marker %s", markerPath)
	}

	// The marker must be a regular file (never a symlink, directory, FIFO, socket, device)
	if !fi.Mode().IsRegular() {
		return GuardMismatch, apperr.Newf(apperr.CodeStorageGuardMismatch,
			"Storage guard marker %s must be a regular file", markerPath)
	}

	// Reject empty or oversized marker files
	if fi.Size() == 0 {
		return GuardMismatch, apperr.Newf(apperr.CodeStorageGuardMismatch,
			"Storage guard marker %s is empty", markerPath)
	}
	if fi.Size() > 4096 {
		return GuardMismatch, apperr.Newf(apperr.CodeStorageGuardMismatch,
			"Storage guard marker %s exceeds maximum allowed size", markerPath)
	}

	f, err := os.Open(markerPath)
	if err != nil {
		return GuardMismatch, apperr.Wrapf(apperr.CodeStorageUnavailable, err,
			"Failed to open storage guard marker %s", markerPath)
	}
	defer f.Close()

	var buf [512]byte
	n, err := f.Read(buf[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return GuardMismatch, apperr.Wrapf(apperr.CodeStorageUnavailable, err,
			"Failed to read storage guard marker %s", markerPath)
	}

	content := strings.TrimSpace(string(buf[:n]))
	expected := g.guardID
	if !strings.HasPrefix(expected, MarkerPrefix) {
		expected = MarkerPrefix + expected
	}

	if content != expected && content != g.guardID {
		return GuardMismatch, apperr.Newf(apperr.CodeStorageGuardMismatch,
			"Storage guard marker content mismatch at %s", markerPath)
	}

	return GuardVerified, nil
}

// CheckHealth performs a non-blocking cached health check with single-flight probing.
func (g *StorageGuard) CheckHealth(ctx context.Context, forceProbe bool) StorageHealth {
	g.mu.Lock()
	now := time.Now().UTC()
	if !forceProbe && !g.cachedHealth.LastChecked.IsZero() && now.Sub(g.cachedHealth.LastChecked) < g.cacheTTL {
		cached := g.cachedHealth
		g.mu.Unlock()
		return cached
	}

	if g.probeActive {
		// Another probe is currently active; return last known health immediately without blocking
		cached := g.cachedHealth
		g.mu.Unlock()
		return cached
	}

	g.probeActive = true
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		g.probeActive = false
		g.mu.Unlock()
	}()

	health := g.runProbe(ctx)

	g.mu.Lock()
	g.cachedHealth = health
	g.mu.Unlock()

	return health
}

// runProbe executes the identity validation, statfs queries, and safe writability test.
func (g *StorageGuard) runProbe(ctx context.Context) StorageHealth {
	now := time.Now().UTC()
	h := StorageHealth{
		Path:         g.root,
		Status:       HealthUnknown,
		MinFreeBytes: g.minFreeBytes,
		LastChecked:  now,
	}

	// 1. Identity Check (Read-Only)
	guardStatus, err := g.ValidateIdentity()
	h.GuardStatus = guardStatus
	if err != nil {
		if guardStatus == GuardMissing {
			h.Status = HealthGuardMissing
		} else {
			h.Status = HealthGuardMismatch
		}
		h.LastError = err.Error()
		return h
	}

	// 2. Query Filesystem & Space
	fsType, total, free, avail, err := queryFS(g.root)
	if err != nil {
		h.Status = HealthUnavailable
		h.LastError = fmt.Sprintf("Filesystem query failed: %v", err)
		return h
	}
	h.Filesystem = fsType
	h.TotalBytes = total
	h.FreeBytes = free
	h.AvailableBytes = avail

	// 3. Space Threshold Check
	if g.minFreeBytes > 0 && int64(avail) < g.minFreeBytes {
		h.Status = HealthLowSpace
		h.LastError = fmt.Sprintf("Available space (%d bytes) is below minimum threshold (%d bytes)",
			avail, g.minFreeBytes)
		// Space is low, but still verify writability
	}

	// 4. Writability Probe (Executed ONLY after Guard is validated)
	writable, writeErr := g.probeWritability()
	h.Writable = writable
	if writeErr != nil {
		if os.IsPermission(writeErr) || strings.Contains(writeErr.Error(), "read-only") {
			h.Status = HealthReadOnly
		} else if h.Status != HealthLowSpace {
			h.Status = HealthDegraded
		}
		h.LastError = fmt.Sprintf("Writability probe failed: %v", writeErr)
		return h
	}

	if h.Status == HealthUnknown {
		h.Status = HealthHealthy
	}

	return h
}

// probeWritability tests if files can be created, synced, and removed in the health directory.
func (g *StorageGuard) probeWritability() (bool, error) {
	healthDir := filepath.Join(g.root, ".ytmdl-health")
	if err := os.MkdirAll(healthDir, 0o755); err != nil {
		return false, err
	}

	probeFile := filepath.Join(healthDir, fmt.Sprintf(".ytmdl-probe-%d-%s", time.Now().UnixNano(), randomHex(4)))
	f, err := os.OpenFile(probeFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}

	_, writeErr := f.Write([]byte("ytmdl-probe-ok\n"))
	syncErr := f.Sync()
	closeErr := f.Close()
	_ = os.Remove(probeFile)

	if writeErr != nil {
		return false, writeErr
	}
	if syncErr != nil {
		return false, syncErr
	}
	if closeErr != nil {
		return false, closeErr
	}

	return true, nil
}

// RequireWritable verifies that the storage is healthy and writable before an operation.
func (g *StorageGuard) RequireWritable() error {
	guardStatus, err := g.ValidateIdentity()
	if err != nil {
		return err
	}
	if guardStatus == GuardMismatch || guardStatus == GuardMissing {
		return apperr.New(apperr.CodeStorageGuardMismatch, "Storage identity guard verification failed.")
	}

	// Check space
	if g.minFreeBytes > 0 {
		_, _, _, avail, err := queryFS(g.root)
		if err == nil && int64(avail) < g.minFreeBytes {
			return apperr.Newf(apperr.CodeStorageLowSpace,
				"Storage free space (%d bytes) is below minimum threshold (%d bytes)", avail, g.minFreeBytes)
		}
	}

	return nil
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
