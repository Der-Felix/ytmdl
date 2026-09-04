package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

func TestLoadEmptyState(t *testing.T) {
	tmpDir := t.TempDir()

	st, err := state.Load(tmpDir)
	if err != nil {
		t.Fatalf("Load on empty dir failed: %v", err)
	}
	if st != nil {
		t.Errorf("expected nil state for non-existent file, got %+v", st)
	}
}

func TestSaveAndLoadValidState(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	orig := &state.State{
		StateVersion:           1,
		OperationID:            "op_12345",
		Status:                 state.StatusPrepared,
		StartedAt:              now,
		UpdatedAt:              now,
		CurrentVersion:         "0.15.0",
		TargetVersion:          "0.16.0",
		ComposeFile:            "compose.ghcr.yaml",
		Engine:                 "docker",
		SchemaBefore:           8,
		BackupPath:             "backups/ytmdl_v0.15.0_pre_v0.16.0_20260903_200000.dump",
		PreviousBackendImage:   "ghcr.io/der-felix/ytmdl-backend:0.15.0",
		PreviousBackendDigest:  "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		PreviousFrontendImage:  "ghcr.io/der-felix/ytmdl-frontend:0.15.0",
		PreviousFrontendDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		LastError:              "failed to connect to postgres://ytmdl:secretpass@db:5432/ytmdl",
	}

	if err := orig.Save(tmpDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file permissions
	stPath := filepath.Join(tmpDir, ".ytmdl", "update-state.json")
	info, err := os.Stat(stPath)
	if err != nil {
		t.Fatalf("stat %s failed: %v", stPath, err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("update-state.json perm = %o, want 0600", info.Mode().Perm())
	}

	// Load back
	loaded, err := state.Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil state")
	}

	if loaded.StateVersion != 1 {
		t.Errorf("StateVersion = %d, want 1", loaded.StateVersion)
	}
	if loaded.OperationID != "op_12345" {
		t.Errorf("OperationID = %q, want op_12345", loaded.OperationID)
	}
	if loaded.Status != state.StatusPrepared {
		t.Errorf("Status = %q, want %q", loaded.Status, state.StatusPrepared)
	}
	if loaded.IsInterrupted() != true {
		t.Errorf("expected IsInterrupted() == true for StatusPrepared")
	}

	// Verify LastError was automatically redacted
	if loaded.LastError != "failed to connect to postgres://ytmdl:***REDACTED***@db:5432/ytmdl" {
		t.Errorf("LastError was not redacted: got %q", loaded.LastError)
	}
}

func TestInterruptedStatuses(t *testing.T) {
	cases := []struct {
		status      state.Status
		interrupted bool
	}{
		{state.StatusIdle, false},
		{state.StatusQuiescing, true},
		{state.StatusPrepared, true},
		{state.StatusMigrating, true},
		{state.StatusMutating, true},
		{state.StatusVerifying, true},
		{state.StatusSuccess, false},
		{state.StatusRollbackInProgress, true},
		{state.StatusRolledBack, false},
		{state.StatusRecoveryRequired, false},
		{state.StatusRecoveryInProgress, true},
		{state.StatusRecovered, false},
	}

	for _, tc := range cases {
		s := &state.State{Status: tc.status}
		if s.IsInterrupted() != tc.interrupted {
			t.Errorf("Status %s: IsInterrupted() = %v, want %v", tc.status, s.IsInterrupted(), tc.interrupted)
		}
	}
}

func TestLoadMalformedState(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	_ = os.MkdirAll(dotDir, 0700)
	_ = os.WriteFile(filepath.Join(dotDir, "update-state.json"), []byte("{broken json:"), 0600)

	_, err := state.Load(tmpDir)
	if err == nil {
		t.Fatal("expected error on malformed json, got nil")
	}
}

func TestLoadUnknownStatus(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	_ = os.MkdirAll(dotDir, 0700)
	_ = os.WriteFile(filepath.Join(dotDir, "update-state.json"), []byte(`{"state_version": 2, "status": "bogus_status"}`), 0600)

	_, err := state.Load(tmpDir)
	if !errors.Is(err, state.ErrUnknownStatus) {
		t.Fatalf("got %v, want ErrUnknownStatus", err)
	}
}

func TestLoadUnknownStateVersion(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	_ = os.MkdirAll(dotDir, 0700)
	_ = os.WriteFile(filepath.Join(dotDir, "update-state.json"), []byte(`{"state_version": 42, "status": "idle"}`), 0600)

	_, err := state.Load(tmpDir)
	if !errors.Is(err, state.ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}

func TestStateTransitions(t *testing.T) {
	legal := []struct {
		from state.Status
		to   state.Status
	}{
		{state.StatusIdle, state.StatusQuiescing},
		{state.StatusIdle, state.StatusPrepared},
		{state.StatusSuccess, state.StatusQuiescing},
		{state.StatusSuccess, state.StatusPrepared},
		{state.StatusSuccess, state.StatusRollbackInProgress},
		{state.StatusRolledBack, state.StatusQuiescing},
		{state.StatusRolledBack, state.StatusPrepared},
		{state.StatusRecovered, state.StatusQuiescing},
		{state.StatusRecovered, state.StatusPrepared},
		{state.StatusQuiescing, state.StatusPrepared},
		{state.StatusQuiescing, state.StatusRollbackInProgress},
		{state.StatusQuiescing, state.StatusRolledBack},
		{state.StatusPrepared, state.StatusMigrating},
		{state.StatusPrepared, state.StatusMutating},
		{state.StatusPrepared, state.StatusRolledBack},
		{state.StatusPrepared, state.StatusRollbackInProgress},
		{state.StatusMigrating, state.StatusVerifying},
		{state.StatusMigrating, state.StatusRollbackInProgress},
		{state.StatusMigrating, state.StatusRecoveryRequired},
		{state.StatusMutating, state.StatusVerifying},
		{state.StatusMutating, state.StatusRollbackInProgress},
		{state.StatusMutating, state.StatusRecoveryRequired},
		{state.StatusVerifying, state.StatusSuccess},
		{state.StatusVerifying, state.StatusRollbackInProgress},
		{state.StatusVerifying, state.StatusRecoveryRequired},
		{state.StatusRollbackInProgress, state.StatusRolledBack},
		{state.StatusRollbackInProgress, state.StatusRecoveryRequired},
		{state.StatusRecoveryRequired, state.StatusRecoveryInProgress},
		{state.StatusRecoveryRequired, state.StatusRollbackInProgress},
		{state.StatusRecoveryInProgress, state.StatusSuccess},
		{state.StatusRecoveryInProgress, state.StatusRecovered},
		{state.StatusRecoveryInProgress, state.StatusRecoveryRequired},
	}

	for _, tc := range legal {
		s := &state.State{Status: tc.from}
		if err := s.TransitionTo(tc.to); err != nil {
			t.Errorf("expected legal transition %q -> %q, got error: %v", tc.from, tc.to, err)
		}
		if s.Status != tc.to {
			t.Errorf("after transition, status = %q, want %q", s.Status, tc.to)
		}
	}

	illegal := []struct {
		from state.Status
		to   state.Status
	}{
		{state.StatusIdle, state.StatusMutating},
		{state.StatusIdle, state.StatusSuccess},
		{state.StatusQuiescing, state.StatusSuccess},
		{state.StatusPrepared, state.StatusSuccess},
		{state.StatusPrepared, state.StatusRecoveryRequired},
		{state.StatusMigrating, state.StatusSuccess},
		{state.StatusMutating, state.StatusSuccess},
		{state.StatusVerifying, state.StatusPrepared},
		{state.StatusSuccess, state.StatusMutating},
		{state.StatusSuccess, state.StatusRolledBack},
		{state.StatusRolledBack, state.StatusMutating},
		{state.StatusRecoveryRequired, state.StatusSuccess},
		{state.StatusRecoveryRequired, state.StatusRolledBack},
		{state.StatusRecoveryInProgress, state.StatusPrepared},
	}

	for _, tc := range illegal {
		s := &state.State{Status: tc.from}
		if err := s.TransitionTo(tc.to); err == nil {
			t.Errorf("expected illegal transition %q -> %q to fail, but got nil", tc.from, tc.to)
		} else if !errors.Is(err, state.ErrInvalidTransition) {
			t.Errorf("expected ErrInvalidTransition, got %v", err)
		}
	}
}
