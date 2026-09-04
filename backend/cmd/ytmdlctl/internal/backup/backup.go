// Package backup implements transactional, validated PostgreSQL streaming backups.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/lock"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

var validVersionCharRegex = regexp.MustCompile(`[^a-zA-Z0-9.-]`)

// BackupOptions configures database backup execution.
type BackupOptions struct {
	ProjectDir     string
	ComposeFile    string
	BackupDir      string // if empty, defaults to <ProjectDir>/backups
	CurrentVersion string // e.g. "0.15.0"
	TargetVersion  string // optional target version for Stage 4 pre-update backups
	DBUser         string // defaults to "ytmdl"
	DBName         string // defaults to "ytmdl"
	SkipLock       bool   // true if caller already holds exclusive lock
}

// BackupResult describes a successfully created and validated backup file.
type BackupResult struct {
	BackupPath   string
	RelativePath string
	SizeBytes    int64
	Duration     time.Duration
	Validated    bool
}

// SanitizeVersion cleans a version string for safe inclusion in filenames.
func SanitizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = validVersionCharRegex.ReplaceAllString(v, "")
	if v == "" {
		v = "unknown"
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// GenerateBackupBaseFilename creates the base name for a backup file.
func GenerateBackupBaseFilename(currentVersion, targetVersion string, timestamp time.Time) string {
	cleanCurrent := SanitizeVersion(currentVersion)
	ts := timestamp.UTC().Format("20060102_150405")
	if targetVersion != "" {
		cleanTarget := SanitizeVersion(targetVersion)
		return fmt.Sprintf("ytmdl_%s_pre_%s_%s", cleanCurrent, cleanTarget, ts)
	}
	return fmt.Sprintf("ytmdl_%s_manual_%s", cleanCurrent, ts)
}

// CreateBackup streams a PostgreSQL custom-format dump from the DB container,
// verifies its structural integrity with pg_restore --list, and atomically finalizes it.
func CreateBackup(ctx context.Context, eng engine.Engine, opts BackupOptions) (*BackupResult, error) {
	if eng == nil {
		return nil, errors.New("cannot create backup: container engine is not initialized")
	}
	if opts.ProjectDir == "" {
		return nil, errors.New("project directory is required")
	}
	if opts.ComposeFile == "" {
		return nil, errors.New("compose file is required")
	}

	// 1. Acquire host update/maintenance lock to prevent concurrent update races (unless caller holds it)
	if !opts.SkipLock {
		lk, err := lock.Acquire(opts.ProjectDir)
		if err != nil {
			return nil, fmt.Errorf("cannot perform backup: %w", err)
		}
		defer lk.Release()
	}

	// 2. Reject backup if an interrupted update transaction is active
	st, err := state.Load(opts.ProjectDir)
	if err == nil && st != nil && st.IsInterrupted() {
		return nil, fmt.Errorf("cannot perform backup: interrupted update transaction detected (status: %s); recovery required", st.Status)
	}

	// 3. Resolve backup directory
	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(opts.ProjectDir, "backups")
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return nil, fmt.Errorf("failed creating backup directory %q: %w", backupDir, err)
	}

	// 4. Resolve safe non-colliding file names
	now := time.Now()
	baseName := GenerateBackupBaseFilename(opts.CurrentVersion, opts.TargetVersion, now)

	finalPath, tempPath, err := resolveNonCollidingPaths(backupDir, baseName)
	if err != nil {
		return nil, err
	}

	// 5. Create temporary file with O_CREATE | O_EXCL and 0600 permissions
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary backup file: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	user := opts.DBUser
	if user == "" {
		user = "ytmdl"
	}
	db := opts.DBName
	if db == "" {
		db = "ytmdl"
	}

	// 6. Stream pg_dump -Fc directly into temp file without loading into RAM
	dumpStart := time.Now()
	res, err := eng.ExecStream(ctx, opts.ProjectDir, opts.ComposeFile, "db", nil, tempFile,
		"pg_dump", "-U", user, "-d", db, "-Fc")
	if err != nil {
		_ = tempFile.Close()
		return nil, fmt.Errorf("database backup execution failed: %w", err)
	}
	if res.ExitCode != 0 {
		_ = tempFile.Close()
		errMsg := strings.TrimSpace(string(res.Stderr))
		if errMsg == "" {
			errMsg = fmt.Sprintf("exit code %d", res.ExitCode)
		}
		return nil, fmt.Errorf("database backup failed: %s", errMsg)
	}

	// 7. Fsync and close temp file
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return nil, fmt.Errorf("failed syncing backup file to disk: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("failed closing backup file: %w", err)
	}

	// 8. Assert file size > 0
	fi, err := os.Stat(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed stating backup temp file: %w", err)
	}
	if fi.Size() == 0 {
		return nil, errors.New("backup dump is empty (0 bytes)")
	}

	// 9. Structurally validate backup with pg_restore --list streamed from host
	dumpReader, err := os.Open(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed opening backup file for validation: %w", err)
	}
	restoreRes, err := eng.ExecStream(ctx, opts.ProjectDir, opts.ComposeFile, "db", dumpReader, io.Discard,
		"pg_restore", "--list")
	_ = dumpReader.Close()
	if err != nil {
		return nil, fmt.Errorf("backup validation process failed: %w", err)
	}
	if restoreRes.ExitCode != 0 {
		errMsg := strings.TrimSpace(string(restoreRes.Stderr))
		if errMsg == "" {
			errMsg = fmt.Sprintf("exit code %d", restoreRes.ExitCode)
		}
		return nil, fmt.Errorf("backup structural validation failed (pg_restore): %s", errMsg)
	}

	// 10. Atomically rename .dump.tmp to .dump and sync parent directory
	if err := os.Rename(tempPath, finalPath); err != nil {
		return nil, fmt.Errorf("failed finalizing backup file: %w", err)
	}
	if dirFile, err := os.Open(backupDir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	success = true
	duration := time.Since(dumpStart)

	relPath, err := filepath.Rel(opts.ProjectDir, finalPath)
	if err != nil {
		relPath = finalPath
	}

	return &BackupResult{
		BackupPath:   finalPath,
		RelativePath: relPath,
		SizeBytes:    fi.Size(),
		Duration:     duration,
		Validated:    true,
	}, nil
}

func resolveNonCollidingPaths(backupDir, baseName string) (string, string, error) {
	for i := 0; i < 1000; i++ {
		candidateBase := baseName
		if i > 0 {
			candidateBase = fmt.Sprintf("%s_%d", baseName, i)
		}
		finalPath := filepath.Join(backupDir, candidateBase+".dump")
		tempPath := filepath.Join(backupDir, candidateBase+".dump.tmp")

		if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
			continue
		}
		return finalPath, tempPath, nil
	}
	return "", "", errors.New("failed finding non-colliding backup filename after 1000 attempts")
}
