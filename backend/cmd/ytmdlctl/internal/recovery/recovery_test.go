package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/recovery"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

func TestRecoveryStatus_Idle(t *testing.T) {
	tmpDir := t.TempDir()
	info, err := recovery.Status(context.Background(), nil, tmpDir, "", "")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if info.StateStatus != state.StatusIdle {
		t.Errorf("got status %q, want idle", info.StateStatus)
	}
	if !strings.Contains(info.SuggestedAction, "No recovery required") {
		t.Errorf("got suggested action %q", info.SuggestedAction)
	}
}

func TestRecoveryStatus_RecoveryRequired(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRunner := runner.NewFake()
	eng := engine.NewDocker(fakeRunner)

	// Mock DB schema query returning 9
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;",
	}, &runner.RunResult{
		Stdout:   []byte("9\n"),
		ExitCode: 0,
	}, nil)

	// Mock backend PS check (exited / not running)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "ps", "--format", "{{.Service}}",
	}, &runner.RunResult{
		Stdout:   []byte("db\n"),
		ExitCode: 0,
	}, nil)

	// Mock image inspections
	fakeRunner.Register("docker", []string{
		"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.15.0",
	}, &runner.RunResult{
		Stdout:   []byte(`[{"Id": "sha256:b111"}]`),
		ExitCode: 0,
	}, nil)
	fakeRunner.Register("docker", []string{
		"image", "inspect", "ghcr.io/der-felix/ytmdl-frontend:0.15.0",
	}, &runner.RunResult{
		Stdout:   []byte(`[{"Id": "sha256:f111"}]`),
		ExitCode: 0,
	}, nil)

	// Create backup file
	backupsDir := filepath.Join(tmpDir, "backups")
	_ = os.MkdirAll(backupsDir, 0700)
	backupPath := filepath.Join(backupsDir, "test.dump")
	_ = os.WriteFile(backupPath, []byte("valid backup"), 0600)

	// Write RECOVERY_REQUIRED state
	st := &state.State{
		StateVersion:          2,
		OperationID:           "op_test",
		Status:                state.StatusRecoveryRequired,
		CurrentVersion:        "0.15.0",
		TargetVersion:         "0.17.0",
		SchemaBefore:          8,
		TargetSchema:          9,
		BackupPath:            "backups/test.dump",
		PreviousBackendImage:  "ghcr.io/der-felix/ytmdl-backend:0.15.0",
		PreviousFrontendImage: "ghcr.io/der-felix/ytmdl-frontend:0.15.0",
		LastError:             "backend crashed after migration",
	}
	_ = st.Save(tmpDir)

	info, err := recovery.Status(context.Background(), eng, tmpDir, "compose.yaml", "")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if info.StateStatus != state.StatusRecoveryRequired {
		t.Errorf("got status %q, want recovery_required", info.StateStatus)
	}
	if info.ActualSchema != 9 {
		t.Errorf("got actual schema %d, want 9", info.ActualSchema)
	}
	if !info.BackupExists {
		t.Error("expected BackupExists to be true")
	}
	if !info.PreviousImagesAvailable {
		t.Error("expected PreviousImagesAvailable to be true")
	}
	if !strings.Contains(info.SuggestedAction, "recover resume") {
		t.Errorf("expected suggestion to include 'recover resume', got: %s", info.SuggestedAction)
	}
}

func TestRecoveryResume_TargetSchemaMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRunner := runner.NewFake()
	eng := engine.NewDocker(fakeRunner)

	// Mock DB schema returning 8 (not 9)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;",
	}, &runner.RunResult{
		Stdout:   []byte("8\n"),
		ExitCode: 0,
	}, nil)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
	}
	_ = st.Save(tmpDir)

	_, err := recovery.Resume(context.Background(), eng, recovery.ResumeOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		AutoConfirm: true,
	})
	if err == nil || !errors.Is(err, recovery.ErrTargetSchemaNotReached) {
		t.Fatalf("expected ErrTargetSchemaNotReached, got: %v", err)
	}
}

func TestRecoveryRestore_ConfirmationRefusal(t *testing.T) {
	tmpDir := t.TempDir()
	backupPath := filepath.Join(tmpDir, "backup.dump")
	_ = os.WriteFile(backupPath, []byte("dump"), 0600)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backup.dump",
	}
	_ = st.Save(tmpDir)

	stdin := strings.NewReader("n\n")
	var stdout, stderr bytes.Buffer

	_, err := recovery.Restore(context.Background(), nil, recovery.RestoreOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		AutoConfirm: false,
		Stdin:       stdin,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err == nil || !errors.Is(err, recovery.ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got: %v", err)
	}

	// State must not have changed
	loaded, _ := state.Load(tmpDir)
	if loaded.Status != state.StatusRecoveryRequired {
		t.Errorf("status was altered: %s", loaded.Status)
	}
}

func TestRecoveryRestore_BackupNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "nonexistent.dump",
	}
	_ = st.Save(tmpDir)

	_, err := recovery.Restore(context.Background(), nil, recovery.RestoreOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		AutoConfirm: true,
	})
	if err == nil || !errors.Is(err, recovery.ErrBackupNotFound) {
		t.Fatalf("expected ErrBackupNotFound, got: %v", err)
	}
}

func TestRecoveryResume_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRunner := runner.NewFake()
	eng := engine.NewDocker(fakeRunner)

	targetDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// Mock DB schema returning 9
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;",
	}, &runner.RunResult{
		Stdout:   []byte("9\n"),
		ExitCode: 0,
	}, nil)

	// Mock up backend
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend",
	}, &runner.RunResult{ExitCode: 0}, nil)

	// Mock get container id for backend
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "ps", "-q", "backend",
	}, &runner.RunResult{Stdout: []byte("c_backend\n"), ExitCode: 0}, nil)

	// Mock inspect container image
	fakeRunner.Register("docker", []string{
		"inspect", "c_backend",
	}, &runner.RunResult{
		Stdout:   []byte(`[{"Image":"sha256:target_be_id","Config":{"Image":"ghcr.io/der-felix/ytmdl-backend:0.17.0"}}]`),
		ExitCode: 0,
	}, nil)

	// Mock inspect image repo digests
	fakeRunner.Register("docker", []string{
		"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.17.0",
	}, &runner.RunResult{
		Stdout:   []byte(fmt.Sprintf(`[{"Id":"sha256:target_be_id","RepoDigests":["ghcr.io/der-felix/ytmdl-backend@%s"]}]`, targetDigest)),
		ExitCode: 0,
	}, nil)

	// Mock up frontend
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "frontend",
	}, &runner.RunResult{ExitCode: 0}, nil)

	// Start test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"status":"ok","version":"0.17.0"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	st := &state.State{
		StateVersion:        2,
		OperationID:         "op_test",
		Status:              state.StatusRecoveryRequired,
		CurrentVersion:      "0.15.0",
		TargetVersion:       "0.17.0",
		SchemaBefore:        8,
		TargetSchema:        9,
		TargetBackendDigest: targetDigest,
	}
	_ = st.Save(tmpDir)

	var stdout, stderr bytes.Buffer
	res, err := recovery.Resume(context.Background(), eng, recovery.ResumeOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		BaseURL:     ts.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatalf("Resume failed: %v\nstderr: %s", err, stderr.String())
	}

	if res.TargetVersion != "0.17.0" {
		t.Errorf("got target version %q, want 0.17.0", res.TargetVersion)
	}
	if res.TargetSchema != 9 {
		t.Errorf("got target schema %d, want 9", res.TargetSchema)
	}

	loaded, _ := state.Load(tmpDir)
	if loaded.Status != state.StatusSuccess {
		t.Errorf("got final state %q, want %q", loaded.Status, state.StatusSuccess)
	}
}

func TestRecoveryRestore_RestoredSchemaMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRunner := runner.NewFake()
	eng := engine.NewDocker(fakeRunner)

	backupPath := filepath.Join(tmpDir, "backup.dump")
	_ = os.WriteFile(backupPath, []byte("dump_content"), 0600)

	// Mock stop backend
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "stop", "backend",
	}, &runner.RunResult{ExitCode: 0}, nil)

	// Mock DB quiescence check (0 active writers)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM pg_stat_activity WHERE datname = 'ytmdl' AND pid <> pg_backend_pid() AND state = 'active' AND application_name NOT IN ('ytmdlctl', 'pg_dump', 'psql');",
	}, &runner.RunResult{Stdout: []byte("0\n"), ExitCode: 0}, nil)

	// Mock safety backup creation (pg_dump + pg_restore --list)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc",
	}, &runner.RunResult{Stdout: []byte("SAFETY_DUMP_BYTES"), ExitCode: 0}, nil)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_restore", "--list",
	}, &runner.RunResult{Stdout: []byte("; TOC list\n"), ExitCode: 0}, nil)

	// Mock create temp DB
	fakeRunner.RegisterPrefix("docker", `compose -f compose.yaml exec -T db psql -U ytmdl -d postgres -c CREATE DATABASE "ytmdl_rec_tmp_`, &runner.RunResult{ExitCode: 0}, nil)

	// Mock pg_restore into temp DB
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db pg_restore -U ytmdl -d ytmdl_rec_tmp_", &runner.RunResult{ExitCode: 0}, nil)

	// Mock schema check in temp DB returning 9 (mismatch! Expected schema_before 8)
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d ytmdl_rec_tmp_", &runner.RunResult{
		Stdout:   []byte("9\n"),
		ExitCode: 0,
	}, nil)

	// Mock defer DROP DATABASE cleanup
	fakeRunner.RegisterPrefix("docker", `compose -f compose.yaml exec -T db psql -U ytmdl -d postgres -c DROP DATABASE IF EXISTS "ytmdl_rec_tmp_`, &runner.RunResult{ExitCode: 0}, nil)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backup.dump",
	}
	_ = st.Save(tmpDir)

	_, err := recovery.Restore(context.Background(), eng, recovery.RestoreOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		AutoConfirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "restored database schema is 9, expected 8") {
		t.Fatalf("expected restored database schema mismatch error, got: %v", err)
	}

	// State must stay recovery_required
	loaded, _ := state.Load(tmpDir)
	if loaded.Status != state.StatusRecoveryRequired {
		t.Errorf("got status %s, want %s", loaded.Status, state.StatusRecoveryRequired)
	}
}

func TestRecoveryRestore_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRunner := runner.NewFake()
	eng := engine.NewDocker(fakeRunner)

	backupPath := filepath.Join(tmpDir, "backup.dump")
	_ = os.WriteFile(backupPath, []byte("dump_content"), 0600)

	envPath := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(envPath, []byte("YTMDL_VERSION=0.17.0\nPOSTGRES_USER=ytmdl\nPOSTGRES_DB=ytmdl\n"), 0600)

	// Mock stop backend
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "stop", "backend",
	}, &runner.RunResult{ExitCode: 0}, nil)

	// Mock DB quiescence check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM pg_stat_activity WHERE datname = 'ytmdl' AND pid <> pg_backend_pid() AND state = 'active' AND application_name NOT IN ('ytmdlctl', 'pg_dump', 'psql');",
	}, &runner.RunResult{Stdout: []byte("0\n"), ExitCode: 0}, nil)

	// Mock safety backup creation
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc",
	}, &runner.RunResult{Stdout: []byte("SAFETY_DUMP_BYTES"), ExitCode: 0}, nil)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_restore", "--list",
	}, &runner.RunResult{Stdout: []byte("; TOC list\n"), ExitCode: 0}, nil)

	// Mock create temp DB
	fakeRunner.RegisterPrefix("docker", `compose -f compose.yaml exec -T db psql -U ytmdl -d postgres -c CREATE DATABASE "ytmdl_rec_tmp_`, &runner.RunResult{ExitCode: 0}, nil)

	// Mock pg_restore into temp DB
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db pg_restore -U ytmdl -d ytmdl_rec_tmp_", &runner.RunResult{ExitCode: 0}, nil)

	// Mock schema check in temp DB returning 8 (matches schema_before!)
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d ytmdl_rec_tmp_", &runner.RunResult{
		Stdout:   []byte("8\n"),
		ExitCode: 0,
	}, nil)

	// Mock DB swap
	fakeRunner.RegisterPrefix("docker", "compose -f compose.yaml exec -T db psql -U ytmdl -d postgres -c", &runner.RunResult{ExitCode: 0}, nil)

	// Mock active DB schema query after swap returning 8
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;",
	}, &runner.RunResult{
		Stdout:   []byte("8\n"),
		ExitCode: 0,
	}, nil)

	// Mock starting previous application containers
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend", "frontend",
	}, &runner.RunResult{ExitCode: 0}, nil)

	// Start test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"status":"ok","version":"0.15.0"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backup.dump",
	}
	_ = st.Save(tmpDir)

	var stdout, stderr bytes.Buffer
	res, err := recovery.Restore(context.Background(), eng, recovery.RestoreOptions{
		ProjectDir:  tmpDir,
		ComposeFile: "compose.yaml",
		BaseURL:     ts.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatalf("Restore failed: %v\nstderr: %s", err, stderr.String())
	}

	if res.RestoredVersion != "0.15.0" {
		t.Errorf("got restored version %q, want 0.15.0", res.RestoredVersion)
	}
	if res.RestoredSchema != 8 {
		t.Errorf("got restored schema %d, want 8", res.RestoredSchema)
	}
	if !strings.HasPrefix(res.QuarantineDBName, "ytmdl_quar_") {
		t.Errorf("got quarantine db %q, want ytmdl_quar_*", res.QuarantineDBName)
	}

	// Verify state transitioned to StatusRecovered
	loaded, _ := state.Load(tmpDir)
	if loaded.Status != state.StatusRecovered {
		t.Errorf("got status %q, want %q", loaded.Status, state.StatusRecovered)
	}

	// Verify .env reverted to 0.15.0
	envBytes, _ := os.ReadFile(envPath)
	if !strings.Contains(string(envBytes), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env was not reverted to 0.15.0: %s", string(envBytes))
	}
}
