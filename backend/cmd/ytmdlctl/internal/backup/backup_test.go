package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/lock"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

func setupTestBackupEngine(fake *runner.FakeProcessRunner, user, db string, dumpContent []byte, dumpErr, restoreErr error) {
	if user == "" {
		user = "ytmdl"
	}
	if db == "" {
		db = "ytmdl"
	}

	dumpRes := &runner.RunResult{ExitCode: 0, Stdout: dumpContent}
	if dumpErr != nil {
		dumpRes = &runner.RunResult{ExitCode: 1, Stderr: []byte("pg_dump: error: failed")}
	}
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "exec", "-T", "db", "pg_dump", "-U", user, "-d", db, "-Fc"}, dumpRes, dumpErr)

	restoreRes := &runner.RunResult{ExitCode: 0, Stdout: []byte("; TOC list\n1; 123 TABLE DATA table ytmdl\n")}
	if restoreErr != nil {
		restoreRes = &runner.RunResult{ExitCode: 1, Stderr: []byte("pg_restore: error: corrupt file")}
	}
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "exec", "-T", "db", "pg_restore", "--list"}, restoreRes, restoreErr)
}

func TestBackupSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("VALID_PGDUMP_CUSTOM_BYTES"), nil, nil)
	eng := engine.NewDocker(fake)

	res, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
		DBUser:         "ytmdl",
		DBName:         "ytmdl",
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if !res.Validated {
		t.Error("expected backup to be validated")
	}
	if res.SizeBytes != int64(len("VALID_PGDUMP_CUSTOM_BYTES")) {
		t.Errorf("SizeBytes = %d, want %d", res.SizeBytes, len("VALID_PGDUMP_CUSTOM_BYTES"))
	}

	// Verify file exists on disk
	fi, err := os.Stat(res.BackupPath)
	if err != nil {
		t.Fatalf("final backup file does not exist: %v", err)
	}

	// Verify permissions (0600 on unix)
	perm := fi.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	// Verify temp file does not remain
	tempPath := res.BackupPath + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists!", tempPath)
	}

	// Verify file name format: ytmdl_v0.15.0_manual_*.dump
	base := filepath.Base(res.BackupPath)
	if !strings.HasPrefix(base, "ytmdl_v0.15.0_manual_") || !strings.HasSuffix(base, ".dump") {
		t.Errorf("unexpected filename format: %s", base)
	}
}

func TestBackupTargetVersionNaming(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("DUMP_CONTENT"), nil, nil)
	eng := engine.NewDocker(fake)

	res, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.16.0",
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	base := filepath.Base(res.BackupPath)
	if !strings.HasPrefix(base, "ytmdl_v0.15.0_pre_v0.16.0_") || !strings.HasSuffix(base, ".dump") {
		t.Errorf("unexpected filename format for target version: %s", base)
	}
}

func TestBackupZeroByteDumpFailsAndCleansUp(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte(""), nil, nil)
	eng := engine.NewDocker(fake)

	_, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty dump error, got: %v", err)
	}

	// Verify no .dump or .dump.tmp exists in backups directory
	files, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".dump") || strings.HasSuffix(f.Name(), ".tmp") {
			t.Errorf("leftover file found after empty dump error: %s", f.Name())
		}
	}
}

func TestBackupPgDumpFailureCleansUp(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", nil, errors.New("dump failed"), nil)
	eng := engine.NewDocker(fake)

	_, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err == nil {
		t.Fatal("expected error on pg_dump failure, got nil")
	}

	files, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".dump") || strings.HasSuffix(f.Name(), ".tmp") {
			t.Errorf("leftover file found: %s", f.Name())
		}
	}
}

func TestBackupPgRestoreValidationFailureCleansUp(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("CORRUPT_BYTES"), nil, errors.New("restore failed"))
	eng := engine.NewDocker(fake)

	_, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("expected validation failure error, got: %v", err)
	}

	files, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".dump") || strings.HasSuffix(f.Name(), ".tmp") {
			t.Errorf("leftover file found: %s", f.Name())
		}
	}
}

func TestBackupLockContentionFails(t *testing.T) {
	tmpDir := t.TempDir()
	// Acquire lock first
	heldLock, err := lock.Acquire(tmpDir)
	if err != nil {
		t.Fatalf("failed acquiring initial lock: %v", err)
	}
	defer heldLock.Release()

	fake := runner.NewFake()
	eng := engine.NewDocker(fake)

	_, err = backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err == nil || (!errors.Is(err, lock.ErrAlreadyLocked) && !strings.Contains(err.Error(), "already in progress")) {
		t.Fatalf("expected lock contention error, got: %v", err)
	}
}

func TestBackupInterruptedStateRefused(t *testing.T) {
	tmpDir := t.TempDir()
	// Save interrupted update state
	st := &state.State{
		StateVersion: state.CurrentStateVersion,
		Status:       state.StatusMutating,
	}
	err := st.Save(tmpDir)
	if err != nil {
		t.Fatalf("failed saving state: %v", err)
	}

	fake := runner.NewFake()
	eng := engine.NewDocker(fake)

	_, err = backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted update transaction") {
		t.Fatalf("expected interrupted state refusal, got: %v", err)
	}
}

func TestBackupSpecialCharactersInDBUserAndName(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	specialUser := "user with spaces & $id"
	specialDB := "db;drop table"
	setupTestBackupEngine(fake, specialUser, specialDB, []byte("SPECIAL_DUMP"), nil, nil)
	eng := engine.NewDocker(fake)

	res, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
		DBUser:         specialUser,
		DBName:         specialDB,
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if !res.Validated {
		t.Error("expected validated backup")
	}

	// Verify exact arguments recorded in fake calls
	calls := fake.Calls()
	var pgDumpCall *runner.FakeCall
	for _, c := range calls {
		if len(c.Args) > 6 && c.Args[6] == "pg_dump" {
			pgDumpCall = &c
			break
		}
	}
	if pgDumpCall == nil {
		t.Fatal("pg_dump call not found")
	}

	// Verify user and db are separate literal elements
	foundUser := false
	foundDB := false
	for i, arg := range pgDumpCall.Args {
		if arg == "-U" && i+1 < len(pgDumpCall.Args) && pgDumpCall.Args[i+1] == specialUser {
			foundUser = true
		}
		if arg == "-d" && i+1 < len(pgDumpCall.Args) && pgDumpCall.Args[i+1] == specialDB {
			foundDB = true
		}
	}
	if !foundUser {
		t.Errorf("special user %q was not passed as literal argument", specialUser)
	}
	if !foundDB {
		t.Errorf("special db %q was not passed as literal argument", specialDB)
	}
}

func TestBackupZeroAppMutation(t *testing.T) {
	tmpDir := t.TempDir()
	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("DUMP_CONTENT"), nil, nil)
	eng := engine.NewDocker(fake)

	_, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		CurrentVersion: "0.15.0",
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Verify NO mutation commands were executed: pull, up, restart, stop, down, run
	forbidden := []string{"pull", "up", "restart", "stop", "down", "run"}
	for _, call := range fake.Calls() {
		for _, arg := range call.Args {
			for _, f := range forbidden {
				if arg == f {
					t.Fatalf("MUTATION VIOLATION: forbidden command %q called during backup!", f)
				}
			}
		}
	}
}

func TestBackupExistingDirectoryNotChmodded(t *testing.T) {
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "shared_backups")
	_ = os.MkdirAll(customDir, 0755)

	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("DUMP_CONTENT"), nil, nil)
	eng := engine.NewDocker(fake)

	_, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		BackupDir:      customDir,
		CurrentVersion: "0.15.0",
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	fi, err := os.Stat(customDir)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0755 {
		t.Errorf("expected existing directory permissions 0755 preserved, got: %o", fi.Mode().Perm())
	}
}

func TestBackupTempSymlinkCollision(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	_ = os.MkdirAll(backupDir, 0700)

	// Create a decoy target file
	decoyPath := filepath.Join(tmpDir, "decoy.txt")
	_ = os.WriteFile(decoyPath, []byte("ORIGINAL_DECOY_DATA"), 0644)

	// Symlink in backups directory pointing to decoy
	symlinkPath := filepath.Join(backupDir, "ytmdl_v0.15.0_manual_test.dump.tmp")
	_ = os.Symlink(decoyPath, symlinkPath)

	fake := runner.NewFake()
	setupTestBackupEngine(fake, "ytmdl", "ytmdl", []byte("NEW_BACKUP_DATA"), nil, nil)
	eng := engine.NewDocker(fake)

	res, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     tmpDir,
		ComposeFile:    "compose.ghcr.yaml",
		BackupDir:      backupDir,
		CurrentVersion: "0.15.0",
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Decoy data must NOT be overwritten!
	decoyData, _ := os.ReadFile(decoyPath)
	if string(decoyData) != "ORIGINAL_DECOY_DATA" {
		t.Fatalf("SECURITY VIOLATION: symlink was followed and overwritten: %s", string(decoyData))
	}

	// Verify the backup used a safe unique name
	if res.BackupPath == decoyPath {
		t.Fatalf("backup path matched decoy path!")
	}
}
