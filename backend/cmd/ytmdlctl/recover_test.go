package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

func TestCLI_Recover_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"recover", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0 for help, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "ytmdlctl recover <action>") {
		t.Errorf("expected usage text, got: %s", out)
	}
}

func TestCLI_Recover_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"recover", "invalid-subcmd"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected code 2 for invalid subcommand, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown action") {
		t.Errorf("expected unknown action error, got: %s", stderr.String())
	}
}

func TestCLI_Recover_Status_Idle(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"--project-dir", tmpDir, "recover", "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0, got %d. stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "State status:            idle") {
		t.Errorf("expected state status idle, got: %s", out)
	}
	if !strings.Contains(out, "No recovery required") {
		t.Errorf("expected suggestion 'No recovery required', got: %s", out)
	}
}

func TestCLI_Recover_Status_RecoveryRequired(t *testing.T) {
	tmpDir := t.TempDir()
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_rec_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/test.dump",
		LastError:      "target backend crashed after DB migration",
	}
	_ = st.Save(tmpDir)

	var stdout, stderr bytes.Buffer
	fake := runner.NewFake()
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "recover", "status"}, &stdout, &stderr, CLIDependencies{
		Runner: fake,
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d. stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "recovery_required") {
		t.Errorf("expected recovery_required in output, got: %s", out)
	}
	if !strings.Contains(out, "op_rec_test") {
		t.Errorf("expected op_rec_test in output, got: %s", out)
	}
	if !strings.Contains(out, "target backend crashed after DB migration") {
		t.Errorf("expected last error in output, got: %s", out)
	}
}

func TestCLI_Status_Reports_RecoveryRequired(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.17.0\n"), 0600)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}\n"), 0600)
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_rec_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		LastError:      "target backend crashed after DB migration",
	}
	_ = st.Save(tmpDir)

	var stdout, stderr bytes.Buffer
	fake := runner.NewFake()
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "status"}, &stdout, &stderr, CLIDependencies{
		Runner: fake,
	})
	// status may return non-zero if engine or services unreachable, but stdout should contain recovery warning
	out := stdout.String()
	if !strings.Contains(out, "RECOVERY REQUIRED") {
		t.Errorf("expected 'RECOVERY REQUIRED' in status output, got: %s", out)
	}
	if !strings.Contains(out, "ytmdlctl recover status") {
		t.Errorf("expected recommendation to run 'ytmdlctl recover status', got: %s", out)
	}
	_ = code
}
