package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// StagingMeta records local staged artifact state for crash recovery.
type StagingMeta struct {
	ItemID       string    `json:"item_id"`
	StagedRel    string    `json:"staged_rel"`
	StagedSize   int64     `json:"staged_size"`
	StagedSHA256 string    `json:"staged_sha256"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StagingManager manages persistent per-item staging directories and quotas.
type StagingManager struct {
	root         string
	minFreeBytes int64
	maxBytes     int64
}

// NewStagingManager creates a StagingManager rooted at path.
func NewStagingManager(root string, minFreeBytes int64, maxBytes int64) (*StagingManager, error) {
	clean := filepath.Clean(strings.TrimSpace(root))
	if clean == "" || clean == "." {
		clean = "/data/staging"
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return nil, apperr.Wrapf(apperr.CodeInternal, err, "Failed to create staging root directory %s", clean)
	}

	return &StagingManager{
		root:         clean,
		minFreeBytes: minFreeBytes,
		maxBytes:     maxBytes,
	}, nil
}

// Root returns the root staging directory.
func (s *StagingManager) Root() string {
	return s.root
}

// MinFreeBytes returns the minimum free space required in staging.
func (s *StagingManager) MinFreeBytes() int64 {
	return s.minFreeBytes
}

// MaxBytes returns the maximum quota allowed for staging artifacts.
func (s *StagingManager) MaxBytes() int64 {
	return s.maxBytes
}

// RelPath returns path relative to the staging root, or base if outside.
func (s *StagingManager) RelPath(path string) string {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(path)
	}
	return rel
}

// ItemDir returns the absolute path to the stable per-item staging directory.
func (s *StagingManager) ItemDir(itemID string) (string, error) {
	if strings.TrimSpace(itemID) == "" || strings.Contains(itemID, "..") || strings.ContainsAny(itemID, "/\\") {
		return "", apperr.New(apperr.CodeInvalidRequest, "Invalid item ID for staging directory.")
	}

	itemDir := filepath.Join(s.root, itemID)
	// Ensure path confinement
	rel, err := filepath.Rel(s.root, itemDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", apperr.New(apperr.CodeInvalidRequest, "Staging directory path escapes root.")
	}

	return itemDir, nil
}

// EnsureItemDir creates the item staging directory if not present and returns its path.
func (s *StagingManager) EnsureItemDir(itemID string) (string, error) {
	dir, err := s.ItemDir(itemID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", apperr.Wrapf(apperr.CodeInternal, err, "Failed to create item staging directory %s", dir)
	}
	return dir, nil
}

// CheckSpace verifies that staging free disk space and quota limits are maintained.
func (s *StagingManager) CheckSpace() error {
	// Query filesystem space
	_, _, _, avail, err := queryFS(s.root)
	if err == nil && s.minFreeBytes > 0 && int64(avail) < s.minFreeBytes {
		return apperr.Newf(apperr.CodeStagingLowSpace,
			"Staging filesystem free space (%d bytes) is below minimum threshold (%d bytes)", avail, s.minFreeBytes)
	}

	// Check staging quota if configured
	if s.maxBytes > 0 {
		used, err := s.UsedBytes()
		if err == nil && used > s.maxBytes {
			return apperr.Newf(apperr.CodeStagingLowSpace,
				"Staging directory used space (%d bytes) exceeds quota limit (%d bytes)", used, s.maxBytes)
		}
	}

	return nil
}

// UsedBytes calculates the total byte size of all files inside the staging root.
func (s *StagingManager) UsedBytes() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip unreadable entries
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// CountPartials counts items that currently have .part or unfinished download files.
func (s *StagingManager) CountPartials() (int, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, err
	}

	var count int
	for _, entry := range entries {
		if entry.IsDir() {
			itemPath := filepath.Join(s.root, entry.Name())
			if hasPartials(itemPath) {
				count++
			}
		}
	}
	return count, nil
}

func hasPartials(dir string) bool {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".part") || strings.HasSuffix(f.Name(), ".ytdl") {
			return true
		}
	}
	return false
}

// ComputeChecksum calculates SHA-256 and byte size of a file.
func ComputeChecksum(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// SaveMeta writes staging metadata file inside itemDir.
func (s *StagingManager) SaveMeta(itemID string, meta StagingMeta) error {
	dir, err := s.EnsureItemDir(itemID)
	if err != nil {
		return err
	}

	meta.UpdatedAt = time.Now().UTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = meta.UpdatedAt
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	metaPath := filepath.Join(dir, "meta.json")
	return os.WriteFile(metaPath, data, 0o644)
}

// LoadMeta reads staging metadata file from itemDir if present.
func (s *StagingManager) LoadMeta(itemID string) (*StagingMeta, error) {
	dir, err := s.ItemDir(itemID)
	if err != nil {
		return nil, err
	}

	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var meta StagingMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// CleanupItem removes the item's staging directory.
func (s *StagingManager) CleanupItem(itemID string) error {
	dir, err := s.ItemDir(itemID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// ResetCorruptedAudio removes any complete audio files in the staging dir on verification failure
// while keeping other auxiliary files if needed.
func (s *StagingManager) ResetCorruptedAudio(itemID string) error {
	dir, err := s.ItemDir(itemID)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && (strings.HasSuffix(name, ".opus") || strings.HasSuffix(name, ".m4a") ||
			strings.HasSuffix(name, ".mp3") || strings.HasSuffix(name, ".flac") || strings.HasSuffix(name, ".ogg")) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}
