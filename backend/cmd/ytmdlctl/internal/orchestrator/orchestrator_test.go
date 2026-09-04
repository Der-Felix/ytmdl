package orchestrator_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/orchestrator"
	"ytdm/backend/cmd/ytmdlctl/internal/release"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/staging"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

func setupTestEnv(t *testing.T, version string) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := fmt.Sprintf("# Test configuration\nYTMDL_VERSION=%s\nPOSTGRES_USER=ytmdl\nPOSTGRES_DB=ytmdl\nPOSTGRES_PASSWORD=secret123\nYTMDL_STORAGE_GUARD_ID=test-guard-123\n", version)
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile .env failed: %v", err)
	}
	return tmpDir, filepath.Join(tmpDir, "compose.ghcr.yaml")
}

func setupHappyFakeRunner(composeFile, prevBackendDigest, prevFrontendDigest, targetBackendDigest, targetFrontendDigest string) *runner.FakeProcessRunner {
	fake := runner.NewFake()

	// Engine compose version
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose v2.24.0\n"),
	}, nil)

	// Inspect previous images
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.15.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(fmt.Sprintf(`[{"Id": "sha256:id_prev_backend", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@%s"]}]`, prevBackendDigest)),
	}, nil)
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-frontend:0.15.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(fmt.Sprintf(`[{"Id": "sha256:id_prev_frontend", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@%s"]}]`, prevFrontendDigest)),
	}, nil)

	// Inspect target images
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.16.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(fmt.Sprintf(`[{"Id": "sha256:id_target_backend", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@%s"]}]`, targetBackendDigest)),
	}, nil)
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-frontend:0.16.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(fmt.Sprintf(`[{"Id": "sha256:id_target_frontend", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@%s"]}]`, targetFrontendDigest)),
	}, nil)

	// Compose up services
	fake.Register("docker", []string{"compose", "-f", composeFile, "up", "-d", "--no-deps", "backend"}, &runner.RunResult{ExitCode: 0}, nil)
	fake.Register("docker", []string{"compose", "-f", composeFile, "up", "-d", "--no-deps", "frontend"}, &runner.RunResult{ExitCode: 0}, nil)
	fake.Register("docker", []string{"compose", "-f", composeFile, "up", "-d", "--no-deps", "backend", "frontend"}, &runner.RunResult{ExitCode: 0}, nil)

	// Compose ps -q
	fake.Register("docker", []string{"compose", "-f", composeFile, "ps", "-q", "backend"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("backend_c123\n"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", composeFile, "ps", "-q", "frontend"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("frontend_c456\n"),
	}, nil)

	// Container inspect
	fake.Register("docker", []string{"inspect", "backend_c123"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_target_backend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-backend:0.16.0"}}]`),
	}, nil)
	fake.Register("docker", []string{"inspect", "frontend_c456"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_target_frontend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-frontend:0.16.0"}}]`),
	}, nil)

	return fake
}

func defaultMockDeps(targetDigestBackend, targetDigestFrontend string) orchestrator.Dependencies {
	return orchestrator.Dependencies{
		ReleaseResolver: func(ctx context.Context, tag string) (*release.ReleaseInfo, error) {
			return &release.ReleaseInfo{
				TagName: "v0.16.0",
			}, nil
		},
		ManifestFetcher: func(ctx context.Context, rel *release.ReleaseInfo) (*manifest.Manifest, error) {
			m := &manifest.Manifest{
				ManifestVersion:        1,
				ReleaseVersion:         "0.16.0",
				ReleaseTag:             "v0.16.0",
				TargetSchema:           8,
				RollbackClassification: manifest.RollbackSchemaNeutral,
				MinUpgradeFrom:         "0.15.0",
			}
			m.Images.Backend.Repository = "ghcr.io/der-felix/ytmdl-backend"
			m.Images.Backend.Digest = targetDigestBackend
			m.Images.Frontend.Repository = "ghcr.io/der-felix/ytmdl-frontend"
			m.Images.Frontend.Digest = targetDigestFrontend
			return m, nil
		},
		StagingVerifier: func(ctx context.Context, eng engine.Engine, opts staging.StageOptions) (*staging.StagingResult, error) {
			return &staging.StagingResult{
				TargetVersion:  "0.16.0",
				BackendImage:   "ghcr.io/der-felix/ytmdl-backend:0.16.0",
				BackendDigest:  targetDigestBackend,
				FrontendImage:  "ghcr.io/der-felix/ytmdl-frontend:0.16.0",
				FrontendDigest: targetDigestFrontend,
			}, nil
		},
		BackupCreator: func(ctx context.Context, eng engine.Engine, opts backup.BackupOptions) (*backup.BackupResult, error) {
			return &backup.BackupResult{
				BackupPath:   filepath.Join(opts.ProjectDir, "backups", "test.dump"),
				RelativePath: "backups/test.dump",
				SizeBytes:    1024,
			}, nil
		},
		HealthChecker: func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
			return &discovery.BackendHealth{
				Status:  "ok",
				Version: "0.15.0",
			}, nil
		},
		SchemaChecker: func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error) {
			return 8, nil
		},
		GuardChecker: func(ctx context.Context, eng engine.Engine, projectDir, composeFile, localMusicPath, expectedGuardID string) (discovery.GuardStatus, error) {
			return discovery.GuardStatusVerified, nil
		},
		QueueChecker: func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error) {
			return 0, nil
		},
		FrontendChecker: func(ctx context.Context, baseURL string) error {
			return nil
		},
	}
}

func TestUpdateHappyPath(t *testing.T) {
	prevBackendDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	prevFrontendDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	targetBackendDigest := "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	targetFrontendDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"

	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := setupHappyFakeRunner(composeFile, prevBackendDigest, prevFrontendDigest, targetBackendDigest, targetFrontendDigest)
	eng := engine.NewDocker(fake)

	deps := defaultMockDeps(targetBackendDigest, targetFrontendDigest)
	callCount := 0
	deps.HealthChecker = func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
		callCount++
		if callCount == 1 {
			return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
		}
		return &discovery.BackendHealth{Status: "ok", Version: "0.16.0"}, nil
	}

	var stdout, stderr bytes.Buffer
	res, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("Update failed: %v\nstderr: %s", err, stderr.String())
	}

	if res.PreviousVersion != "0.15.0" || res.CurrentVersion != "0.16.0" {
		t.Errorf("unexpected UpdateResult: %+v", res)
	}

	// Verify .env was updated to 0.16.0
	dotEnvData, _ := os.ReadFile(filepath.Join(projectDir, ".env"))
	if !strings.Contains(string(dotEnvData), "YTMDL_VERSION=0.16.0") {
		t.Errorf(".env does not contain 0.16.0:\n%s", string(dotEnvData))
	}

	// Verify state is SUCCESS
	st, err := state.Load(projectDir)
	if err != nil || st == nil {
		t.Fatalf("failed loading state: %v", err)
	}
	if st.Status != state.StatusSuccess {
		t.Errorf("state.Status = %q, want %q", st.Status, state.StatusSuccess)
	}
	if st.CurrentVersion != "0.15.0" || st.TargetVersion != "0.16.0" {
		t.Errorf("state versions = %s -> %s, want 0.15.0 -> 0.16.0", st.CurrentVersion, st.TargetVersion)
	}
}

func TestUpdateAmbientVersionBlocked(t *testing.T) {
	t.Setenv("YTMDL_VERSION", "0.16.0")

	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := runner.NewFake()
	eng := engine.NewDocker(fake)
	deps := defaultMockDeps("sha256:1111", "sha256:2222")

	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
	})

	if err == nil || !strings.Contains(err.Error(), "YTMDL_VERSION is set in host process environment") {
		t.Fatalf("expected ambient environment error, got: %v", err)
	}
}

func TestUpdateUnsupportedComposeBlocked(t *testing.T) {
	projectDir, _ := setupTestEnv(t, "0.15.0")
	fake := runner.NewFake()
	eng := engine.NewDocker(fake)
	deps := defaultMockDeps("sha256:1111", "sha256:2222")

	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: filepath.Join(projectDir, "compose.yaml"),
	})

	if !errors.Is(err, orchestrator.ErrUnsupportedCompose) {
		t.Fatalf("expected ErrUnsupportedCompose, got: %v", err)
	}
}

func TestUpdateConfirmationCancelled(t *testing.T) {
	targetBackendDigest := "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	targetFrontendDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := setupHappyFakeRunner(composeFile, "sha256:1111", "sha256:2222", targetBackendDigest, targetFrontendDigest)
	eng := engine.NewDocker(fake)
	deps := defaultMockDeps(targetBackendDigest, targetFrontendDigest)

	// Provide 'n' as input
	stdin := strings.NewReader("n\n")
	var stdout, stderr bytes.Buffer

	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: false,
		Stdin:       stdin,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if !errors.Is(err, orchestrator.ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got: %v", err)
	}

	// Verify .env was NOT modified
	dotEnvData, _ := os.ReadFile(filepath.Join(projectDir, ".env"))
	if !strings.Contains(string(dotEnvData), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env was modified despite cancellation:\n%s", string(dotEnvData))
	}

	// Verify NO state was written
	st, _ := state.Load(projectDir)
	if st != nil {
		t.Errorf("expected nil state after cancel, got: %+v", st)
	}
}

func TestUpdateBackendFailureAutomaticRollback(t *testing.T) {
	prevBackendDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	prevFrontendDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	targetBackendDigest := "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	targetFrontendDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"

	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := setupHappyFakeRunner(composeFile, prevBackendDigest, prevFrontendDigest, targetBackendDigest, targetFrontendDigest)
	// Restored containers in rollback return previous image
	fake.Register("docker", []string{"inspect", "backend_c123"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_prev_backend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-backend:0.15.0"}}]`),
	}, nil)
	fake.Register("docker", []string{"inspect", "frontend_c456"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_prev_frontend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-frontend:0.15.0"}}]`),
	}, nil)
	eng := engine.NewDocker(fake)

	deps := defaultMockDeps(targetBackendDigest, targetFrontendDigest)
	callCount := 0
	deps.HealthChecker = func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
		callCount++
		if callCount == 1 {
			// Preflight check
			return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
		}
		if callCount == 2 {
			// Target acceptance verification: fails!
			return nil, errors.New("backend failed to start: connection refused")
		}
		// Restored backend verification during rollback: succeeds!
		return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
	}

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if !errors.Is(err, orchestrator.ErrRolledBack) {
		t.Fatalf("expected ErrRolledBack, got: %v", err)
	}

	// Verify .env was restored to 0.15.0
	dotEnvData, _ := os.ReadFile(filepath.Join(projectDir, ".env"))
	if !strings.Contains(string(dotEnvData), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env was not restored to 0.15.0:\n%s", string(dotEnvData))
	}

	// Verify state is ROLLED_BACK
	st, err := state.Load(projectDir)
	if err != nil || st == nil {
		t.Fatalf("failed loading state: %v", err)
	}
	if st.Status != state.StatusRolledBack {
		t.Errorf("state.Status = %q, want %q", st.Status, state.StatusRolledBack)
	}
}

func TestUpdateUnexpectedSchemaDriftRecoveryRequired(t *testing.T) {
	prevBackendDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	prevFrontendDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	targetBackendDigest := "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	targetFrontendDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"

	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := setupHappyFakeRunner(composeFile, prevBackendDigest, prevFrontendDigest, targetBackendDigest, targetFrontendDigest)
	eng := engine.NewDocker(fake)

	deps := defaultMockDeps(targetBackendDigest, targetFrontendDigest)
	callCount := 0
	deps.HealthChecker = func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
		callCount++
		if callCount == 1 {
			return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
		}
		return &discovery.BackendHealth{Status: "ok", Version: "0.16.0"}, nil
	}

	// Schema drift: returns schema 8 initially, then schema 9 after target backend runs!
	schemaCallCount := 0
	deps.SchemaChecker = func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error) {
		schemaCallCount++
		if schemaCallCount == 1 {
			return 8, nil
		}
		return 9, nil // unexpected schema drift!
	}

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if !errors.Is(err, orchestrator.ErrRecoveryRequired) {
		t.Fatalf("expected ErrRecoveryRequired on schema drift, got: %v", err)
	}

	// Verify state is RECOVERY_REQUIRED
	st, err := state.Load(projectDir)
	if err != nil || st == nil {
		t.Fatalf("failed loading state: %v", err)
	}
	if st.Status != state.StatusRecoveryRequired {
		t.Errorf("state.Status = %q, want %q", st.Status, state.StatusRecoveryRequired)
	}
	// Backup MUST be preserved in state
	if st.BackupPath == "" {
		t.Error("expected BackupPath to be preserved in state")
	}
}

func TestRollbackFromPrepared(t *testing.T) {
	projectDir, composeFile := setupTestEnv(t, "0.15.0")
	fake := runner.NewFake()
	eng := engine.NewDocker(fake)
	deps := defaultMockDeps("sha256:1111", "sha256:2222")

	// Create PREPARED state with zero mutations
	st := &state.State{
		StateVersion:   state.CurrentStateVersion,
		OperationID:    "op_test",
		Status:         state.StatusPrepared,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.16.0",
		SchemaBefore:   8,
	}
	_ = st.Save(projectDir)

	var stdout, stderr bytes.Buffer
	res, err := orchestrator.Rollback(context.Background(), eng, deps, orchestrator.RollbackOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if res.RestoredVersion != "0.15.0" {
		t.Errorf("RestoredVersion = %q, want 0.15.0", res.RestoredVersion)
	}

	// State should become rolled_back
	st, _ = state.Load(projectDir)
	if st.Status != state.StatusRolledBack {
		t.Errorf("Status = %q, want %q", st.Status, state.StatusRolledBack)
	}
}

func TestRollbackFromSuccess(t *testing.T) {
	prevBackendDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	prevFrontendDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	projectDir, composeFile := setupTestEnv(t, "0.16.0")
	fake := setupHappyFakeRunner(composeFile, prevBackendDigest, prevFrontendDigest, "sha256:3333", "sha256:4444")
	// Restored containers in rollback return previous image
	fake.Register("docker", []string{"inspect", "backend_c123"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_prev_backend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-backend:0.15.0"}}]`),
	}, nil)
	fake.Register("docker", []string{"inspect", "frontend_c456"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:id_prev_frontend", "Config": {"Image": "ghcr.io/der-felix/ytmdl-frontend:0.15.0"}}]`),
	}, nil)
	eng := engine.NewDocker(fake)

	deps := defaultMockDeps("sha256:3333", "sha256:4444")
	deps.HealthChecker = func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
		return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
	}

	// State is currently SUCCESS (0.15.0 -> 0.16.0)
	st := &state.State{
		StateVersion:            state.CurrentStateVersion,
		OperationID:             "op_test",
		Status:                  state.StatusSuccess,
		CurrentVersion:          "0.15.0",
		TargetVersion:           "0.16.0",
		SchemaBefore:            8,
		PreviousBackendImage:    "ghcr.io/der-felix/ytmdl-backend:0.15.0",
		PreviousBackendImageID:  "sha256:id_prev_backend",
		PreviousBackendDigest:   prevBackendDigest,
		PreviousBackendDigests:  []string{prevBackendDigest},
		PreviousFrontendImage:   "ghcr.io/der-felix/ytmdl-frontend:0.15.0",
		PreviousFrontendImageID: "sha256:id_prev_frontend",
		PreviousFrontendDigest:  prevFrontendDigest,
		PreviousFrontendDigests: []string{prevFrontendDigest},
	}
	_ = st.Save(projectDir)

	var stdout, stderr bytes.Buffer
	res, err := orchestrator.Rollback(context.Background(), eng, deps, orchestrator.RollbackOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if res.RestoredVersion != "0.15.0" {
		t.Errorf("RestoredVersion = %q, want 0.15.0", res.RestoredVersion)
	}

	// Verify .env was restored to 0.15.0
	dotEnvData, _ := os.ReadFile(filepath.Join(projectDir, ".env"))
	if !strings.Contains(string(dotEnvData), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env does not contain 0.15.0:\n%s", string(dotEnvData))
	}

	// State is now ROLLED_BACK
	st, _ = state.Load(projectDir)
	if st.Status != state.StatusRolledBack {
		t.Errorf("Status = %q, want %q", st.Status, state.StatusRolledBack)
	}
}

func TestRollbackImageIdentityReversedRepoDigests(t *testing.T) {
	d1 := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	d2 := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	projectDir, composeFile := setupTestEnv(t, "0.16.0")
	fake := setupHappyFakeRunner(composeFile, d1, d2, "sha256:3333", "sha256:4444")

	// Running restored image returns repo digests in reversed order compared to snapshot
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.15.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Id": "sha256:different_local_id", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@` + d2 + `", "ghcr.io/der-felix/ytmdl-backend@` + d1 + `"]}]`),
	}, nil)
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-frontend:0.15.0"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Id": "sha256:different_local_id", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@` + d2 + `", "ghcr.io/der-felix/ytmdl-frontend@` + d1 + `"]}]`),
	}, nil)

	fake.Register("docker", []string{"inspect", "backend_c123"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:different_local_id", "Config": {"Image": "ghcr.io/der-felix/ytmdl-backend:0.15.0"}}]`),
	}, nil)
	fake.Register("docker", []string{"inspect", "frontend_c456"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`[{"Image": "sha256:different_local_id", "Config": {"Image": "ghcr.io/der-felix/ytmdl-frontend:0.15.0"}}]`),
	}, nil)

	eng := engine.NewDocker(fake)
	deps := defaultMockDeps("sha256:3333", "sha256:4444")
	deps.HealthChecker = func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error) {
		return &discovery.BackendHealth{Status: "ok", Version: "0.15.0"}, nil
	}

	st := &state.State{
		StateVersion:            state.CurrentStateVersion,
		OperationID:             "op_test_rev",
		Status:                  state.StatusSuccess,
		CurrentVersion:          "0.15.0",
		TargetVersion:           "0.16.0",
		SchemaBefore:            8,
		PreviousBackendImage:    "ghcr.io/der-felix/ytmdl-backend:0.15.0",
		PreviousBackendDigests:  []string{d1, d2}, // snapshot had d1 first
		PreviousFrontendImage:   "ghcr.io/der-felix/ytmdl-frontend:0.15.0",
		PreviousFrontendDigests: []string{d1, d2},
	}
	_ = st.Save(projectDir)

	var stdout, stderr bytes.Buffer
	res, err := orchestrator.Rollback(context.Background(), eng, deps, orchestrator.RollbackOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("Rollback failed with reversed digests: %v", err)
	}
	if res.RestoredVersion != "0.15.0" {
		t.Errorf("RestoredVersion = %q, want 0.15.0", res.RestoredVersion)
	}
}
