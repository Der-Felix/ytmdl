package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/lock"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
	"ytdm/backend/internal/database/dbtest"
)

func TestCLI_ReconcileArtists_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"reconcile-artists", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
}

func TestCLI_ReconcileArtists_MaintenanceAlias(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"maintenance", "reconcile-artists", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
}

func TestCLI_ReconcileArtists_DryRunAndApplyMutuallyExclusive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"reconcile-artists", "--dry-run", "--apply"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "cannot specify both --dry-run and --apply") {
		t.Errorf("stderr missing error message: %s", stderr.String())
	}
}

func TestCLI_ReconcileArtists_InterruptedStateBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	st := &state.State{
		Status: state.StatusRollbackInProgress,
	}
	if err := st.Save(tmpDir); err != nil {
		t.Fatalf("failed saving state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"reconcile-artists", "--project-dir", tmpDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "interrupted update transaction detected") {
		t.Errorf("stderr missing interrupted message: %s", stderr.String())
	}
}

func TestCLI_ReconcileArtists_DryRunWithFakeEngine(t *testing.T) {
	tmpDir := t.TempDir()

	// Write mock compose file
	composeContent := `services:
  db:
    image: postgres:18-alpine
  backend:
    image: ytmdl-backend:0.16.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed writing compose: %v", err)
	}

	fakeRunner := runner.NewFake()

	// 1. compose version check
	fakeRunner.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.24.0\n"),
	}, nil)

	// 2. compose ps check
	fakeRunner.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "{{.Service}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("db\nbackend\n"),
	}, nil)

	// 3. candidate query via psql in db container (return Alan Walker duplicate fixture)
	candidateJSON := `[
		{
			"id": "art_1",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "12345",
			"image_url": "https://img/walker.jpg",
			"created_at": "2026-09-04T08:00:00Z",
			"release_count": 3,
			"track_count": 8,
			"has_sub": true
		},
		{
			"id": "art_2",
			"name": "Alan Walker",
			"provider": "deezer",
			"source_id": "artist:alan-walker",
			"image_url": "",
			"created_at": "2026-09-04T09:00:00Z",
			"release_count": 1,
			"track_count": 2,
			"has_sub": false
		}
	]`

	querySQL := `SELECT COALESCE(json_agg(t), '[]'::json) FROM (
		SELECT
			a.id,
			a.name,
			a.provider,
			a.source_id,
			COALESCE(a.image_url, '') AS image_url,
			a.created_at,
			(SELECT COUNT(*) FROM releases r WHERE r.artist_id = a.id) AS release_count,
			(SELECT COUNT(*) FROM tracks t WHERE t.artist_id = a.id) AS track_count,
			EXISTS(
				SELECT 1 FROM artist_subscriptions s
				WHERE s.provider = a.provider AND s.artist_source_id = a.source_id
			) AS has_sub
		FROM artists a
		WHERE LOWER(a.name) IN (
			SELECT LOWER(name)
			FROM artists
			GROUP BY LOWER(name)
			HAVING COUNT(*) > 1
		)
		ORDER BY LOWER(a.name), a.provider, a.created_at ASC
	) t;`

	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", querySQL,
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(candidateJSON + "\n"),
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--file", "compose.yaml",
		"--engine", "docker",
		"-v",
	}, &stdout, &stderr, CLIDependencies{
		Runner: fakeRunner,
	})

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "DRY RUN (read-only preview, 0 writes)") {
		t.Errorf("stdout missing DRY RUN mode: %s", out)
	}
	if !strings.Contains(out, "Proved duplicate:     1 clusters (1 duplicate rows)") {
		t.Errorf("stdout missing proved duplicate count: %s", out)
	}
	if !strings.Contains(out, "Releases:             1 to be repointed") {
		t.Errorf("stdout missing planned releases: %s", out)
	}
	if !strings.Contains(out, "Tracks:               2 to be repointed") {
		t.Errorf("stdout missing planned tracks: %s", out)
	}
	if !strings.Contains(out, "PREVIEW COMPLETE") {
		t.Errorf("stdout missing PREVIEW COMPLETE: %s", out)
	}
}

func TestCLI_ReconcileArtists_LockContentionBlocksMutating(t *testing.T) {
	tmpDir := t.TempDir()

	// Acquire lock first
	fl, err := lock.Acquire(tmpDir)
	if err != nil {
		t.Fatalf("failed acquiring lock: %v", err)
	}
	defer fl.Release()

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--apply",
		"-y",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "failed acquiring update lock") {
		t.Errorf("stderr missing lock error: %s", stderr.String())
	}
}

func TestCLI_ReconcileArtists_RealPostgres_E2E(t *testing.T) {
	_, testURL := dbtest.OpenWithURL(t)
	tmpDir := t.TempDir()

	// 1. Dry run via CLI
	var stdoutDry, stderrDry bytes.Buffer
	code := runCLI(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"-v",
	}, &stdoutDry, &stderrDry)

	if code != 0 {
		t.Fatalf("dry run failed with code %d; stderr: %s", code, stderrDry.String())
	}
	if !strings.Contains(stdoutDry.String(), "DRY RUN (read-only preview, 0 writes)") {
		t.Errorf("missing dry run banner: %s", stdoutDry.String())
	}

	// 2. Mutating run via CLI with auto-confirm (-y)
	var stdoutApply, stderrApply bytes.Buffer
	code = runCLIWithDeps(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
		"-v",
	}, &stdoutApply, &stderrApply, CLIDependencies{AllowDirectDBMutate: true})

	if code != 0 {
		t.Fatalf("apply failed with code %d; stderr: %s", code, stderrApply.String())
	}
	if !strings.Contains(stdoutApply.String(), "SUCCESS") {
		t.Errorf("missing SUCCESS in output: %s", stdoutApply.String())
	}
}

func TestCLI_ReconcileArtists_DirectDBMutateBlockedEvenWithEnvVar(t *testing.T) {
	_, testURL := dbtest.OpenWithURL(t)
	tmpDir := t.TempDir()

	// Verify that even if someone sets the legacy environment variable, runCLI blocks it.
	t.Setenv("YTMDL_TEST_ALLOW_DIRECT_DB_MUTATE", "1")

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--db-url", testURL,
		"--apply",
		"-y",
	}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mutating maintenance via --db-url is not permitted") {
		t.Errorf("stderr missing expected error: %s", stderr.String())
	}
}

func TestCLI_ReconcileArtists_ConfirmationRefusal(t *testing.T) {
	_, testURL := dbtest.OpenWithURL(t)
	tmpDir := t.TempDir()

	stdin := strings.NewReader("n\n")
	var stdout, stderr bytes.Buffer

	code := runReconcileArtists(context.Background(), &stdout, &stderr, stdin, tmpDir, "", "", "", []string{
		"--db-url", testURL,
		"--apply",
	}, CLIDependencies{AllowDirectDBMutate: true})

	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Reconciliation cancelled by user") {
		t.Errorf("stdout missing cancellation notice: %s", stdout.String())
	}
}

func TestCLI_ReconcileArtists_BackupFailureAborts(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `services:
  db:
    image: postgres:18-alpine
  backend:
    image: ytmdl-backend:0.16.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte(composeContent), 0644); err != nil {
		t.Fatalf("failed writing compose: %v", err)
	}

	fakeRunner := runner.NewFake()

	// compose version check
	fakeRunner.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.24.0\n"),
	}, nil)

	// compose ps check
	fakeRunner.Register("docker", []string{"compose", "-f", "compose.yaml", "ps", "--format", "{{.Service}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("db\nbackend\n"),
	}, nil)

	// Queue check
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c",
		"SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');",
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("0\n"),
	}, nil)

	// Stop backend (Quiescent Model A)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "stop", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	// Up backend (deferred cleanup on abort)
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "up", "-d", "--no-deps", "backend",
	}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	// Make pg_dump fail
	fakeRunner.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "db",
		"pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc",
	}, &runner.RunResult{
		ExitCode: 1,
		Stderr:   []byte("pg_dump: connection lost\n"),
	}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{
		"reconcile-artists",
		"--project-dir", tmpDir,
		"--file", "compose.yaml",
		"--engine", "docker",
		"--apply",
		"-y",
	}, &stdout, &stderr, CLIDependencies{
		Runner: fakeRunner,
	})

	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout: %s, stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "PRE-RECONCILIATION BACKUP FAILED") {
		t.Errorf("stderr missing backup failure notice: %s", stderr.String())
	}
}
