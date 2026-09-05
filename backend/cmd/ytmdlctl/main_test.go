package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

const sampleManifestJSON = `{
  "manifest_version": 1,
  "release_version": "0.16.0",
  "release_tag": "v0.16.0",
  "target_schema": 8,
  "rollback_classification": "schema_neutral",
  "min_upgrade_from": "0.15.0",
  "images": {
    "backend": {
      "repository": "ghcr.io/der-felix/ytmdl-backend",
      "tag": "0.16.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    "frontend": {
      "repository": "ghcr.io/der-felix/ytmdl-frontend",
      "tag": "0.16.0",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    }
  },
  "required_env": ["POSTGRES_PASSWORD"]
}`

func setupMockGitHub(t *testing.T, releaseTag, version string, includeManifest bool) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Der-Felix/ytmdl/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			assetsJSON := "[]"
			if includeManifest {
				assetsJSON = fmt.Sprintf(`[{"name": "release-manifest.json", "browser_download_url": "%s/download/manifest.json"}]`, server.URL)
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{
				"tag_name": "%s",
				"name": "YTMDL %s",
				"draft": false,
				"prerelease": false,
				"published_at": "2026-09-03T18:00:00Z",
				"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/%s",
				"body": "Release notes",
				"assets": %s
			}`, releaseTag, version, releaseTag, assetsJSON)))
		case "/download/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleManifestJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func setupMockBackendHealth(t *testing.T, status, version string, dbHealthy bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{
				"data": {
					"status": "%s",
					"version": "%s",
					"uptime_seconds": 3600,
					"checks": {
						"database": {"ok": %t}
					}
				}
			}`, status, version, dbHealthy)))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("version code = %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "ytmdlctl version") {
		t.Errorf("expected version output, got: %s", out)
	}
	if !strings.Contains(out, "platform:") {
		t.Errorf("expected platform output, got: %s", out)
	}
}

func TestRollbackNoActiveTransactionFails(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\n"), 0600)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "--engine", "docker", "rollback"}, &stdout, &stderr, CLIDependencies{Runner: fake})
	if code != 1 {
		t.Fatalf("rollback exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no active or reversible transaction found") {
		t.Errorf("stderr = %q, want 'no active or reversible transaction found'", stderr.String())
	}
}

func TestUpdateUnsupportedComposeFileFails(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\n"), 0600)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "--engine", "docker", "--file", "compose.yaml", "update"}, &stdout, &stderr, CLIDependencies{Runner: fake})
	if code != 1 {
		t.Fatalf("update with compose.yaml code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "managed updates are supported only for compose.ghcr.yaml") {
		t.Errorf("stderr = %q, want ErrUnsupportedCompose message", stderr.String())
	}
}

func TestStatusAmbiguousComposeReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)

	var stdout, stderr bytes.Buffer
	fake := runner.NewFake()
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "status"}, &stdout, &stderr, CLIDependencies{Runner: fake})
	if code != 0 {
		t.Fatalf("status code = %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Multiple candidate compose files detected") {
		t.Errorf("expected ambiguity warning in status output, got: %s", out)
	}
}

func TestStatusExplicitSave(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{Runner: fake}
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "--file", "compose.ghcr.yaml", "--engine", "docker", "status", "--save"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("status --save code = %d, want 0; stderr: %s", code, stderr.String())
	}

	// Verify config.json was created
	cfgPath := filepath.Join(tmpDir, ".ytmdl", "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config.json to exist after status --save: %v", err)
	}
}

func TestStatusSaveAmbiguityFails(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)

	fake := runner.NewFake()
	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{Runner: fake}
	// Attempt status --save with ambiguous compose file
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "status", "--save"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected code 1 when attempting to save ambiguous configuration, got %d", code)
	}

	// Verify config.json was NOT created
	cfgPath := filepath.Join(tmpDir, ".ytmdl", "config.json")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("CRITICAL: status --save wrote config.json despite ambiguous compose!")
	}
}

func TestStatusWithoutSaveDoesNotWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

	var stdout, stderr bytes.Buffer
	code := runCLIWithDeps(context.Background(), []string{"--project-dir", tmpDir, "--file", "compose.ghcr.yaml", "--engine", "docker", "status"}, &stdout, &stderr, CLIDependencies{Runner: fake})
	if code != 0 {
		t.Fatalf("status code = %d, want 0; stderr: %s", code, stderr.String())
	}

	// Verify config.json was NOT created
	cfgPath := filepath.Join(tmpDir, ".ytmdl", "config.json")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("CRITICAL: status without --save implicitly created %s!", cfgPath)
	}
}

func TestCheckCommand(t *testing.T) {
	ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", true)
	defer ghServer.Close()

	backendServer := setupMockBackendHealth(t, "ok", "0.15.0", true)
	defer backendServer.Close()

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		GitHubURL:  ghServer.URL,
		HTTPClient: ghServer.Client(),
	}

	code := runCLIWithDeps(context.Background(), []string{"--base-url", backendServer.URL, "check"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("check exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Current version:           0.15.0") {
		t.Errorf("expected Current version 0.15.0, got:\n%s", out)
	}
	if !strings.Contains(out, "Latest public release:     0.16.0") {
		t.Errorf("expected Latest public release 0.16.0, got:\n%s", out)
	}
	if !strings.Contains(out, "State:                     update available") {
		t.Errorf("expected update available, got:\n%s", out)
	}
	if !strings.Contains(out, "Managed update metadata:   available (manifest v1 verified)") {
		t.Errorf("expected manifest verified, got:\n%s", out)
	}
}

func TestCheckCommandWorksWithoutEngine(t *testing.T) {
	// check must succeed even when Docker/Podman is completely absent
	ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", true)
	defer ghServer.Close()

	var stdout, stderr bytes.Buffer
	fakeEmptyRunner := runner.NewFake() // no docker/podman registered
	deps := CLIDependencies{
		Runner:     fakeEmptyRunner,
		GitHubURL:  ghServer.URL,
		HTTPClient: ghServer.Client(),
	}

	code := runCLIWithDeps(context.Background(), []string{"check"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("check exit code = %d without engine, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Latest public release:     0.16.0") {
		t.Errorf("expected Latest public release 0.16.0, got:\n%s", out)
	}
}

func TestUpdateDryRunReadyScenario(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\nPOSTGRES_PASSWORD=secret\n"), 0600)

	ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", true)
	defer ghServer.Close()

	backendServer := setupMockBackendHealth(t, "ok", "0.15.0", true)
	defer backendServer.Close()

	fake := runner.NewFake()
	// Compose version
	fake.Register("podman", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Podman Compose v1.0.6\n")}, nil)
	// Inspect services
	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "ps", "--format", "json"}, &runner.RunResult{
		Stdout: []byte(`[{"Service":"backend","State":"running","Health":"healthy"},{"Service":"frontend","State":"running","Health":"healthy"},{"Service":"db","State":"running","Health":"healthy"}]`),
	}, nil)
	// Query schema
	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;"}, &runner.RunResult{
		Stdout: []byte("8\n"),
	}, nil)
	// Query queue
	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');"}, &runner.RunResult{
		Stdout: []byte("0\n"),
	}, nil)
	fake.Register("podman", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT count(*) FROM jobs WHERE status NOT IN ('completed', 'failed', 'cancelled');"}, &runner.RunResult{
		Stdout: []byte("0\n"),
	}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner:     fake,
		GitHubURL:  ghServer.URL,
		HTTPClient: ghServer.Client(),
	}

	code := runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "podman",
		"--file", "compose.yaml",
		"--base-url", backendServer.URL,
		"update", "--dry-run",
	}, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("dry-run exit code = %d, want 0; stderr: %s\nstdout:\n%s", code, stderr.String(), stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "RESULT:\nREADY") {
		t.Errorf("expected RESULT: READY, got:\n%s", out)
	}

	// Verify ZERO mutation calls were registered
	for _, call := range fake.Calls() {
		for _, forbidden := range []string{"pull", "up", "restart", "stop", "down", "create"} {
			for _, arg := range call.Args {
				if arg == forbidden {
					t.Fatalf("MUTATION DETECTED: dry-run executed forbidden command: %s %v", call.Executable, call.Args)
				}
			}
		}
	}
}

func TestUpdateDryRunZeroWriteFilesystemInvariant(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "compose.yaml")
	envPath := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(composePath, []byte("services: {}"), 0644)
	_ = os.WriteFile(envPath, []byte("YTMDL_VERSION=0.15.0\nPOSTGRES_PASSWORD=secret\n"), 0600)

	ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", true)
	defer ghServer.Close()

	backendServer := setupMockBackendHealth(t, "ok", "0.15.0", true)
	defer backendServer.Close()

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

	// Snapshot directory state before run
	beforeEntries := listDirRecursive(t, tmpDir)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner:     fake,
		GitHubURL:  ghServer.URL,
		HTTPClient: ghServer.Client(),
	}

	_ = runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "docker",
		"--file", "compose.yaml",
		"--base-url", backendServer.URL,
		"update", "--dry-run",
	}, &stdout, &stderr, deps)

	// Snapshot directory state after run
	afterEntries := listDirRecursive(t, tmpDir)

	if len(beforeEntries) != len(afterEntries) {
		t.Fatalf("ZERO-WRITE VIOLATION: file count changed: before %d, after %d; entries: %v", len(beforeEntries), len(afterEntries), afterEntries)
	}
	for path, content := range beforeEntries {
		afterContent, ok := afterEntries[path]
		if !ok || afterContent != content {
			t.Fatalf("ZERO-WRITE VIOLATION: file %s modified or missing", path)
		}
	}
}

func TestUpdateDryRunWarningActiveJobs(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\nPOSTGRES_PASSWORD=secret\n"), 0600)

	ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", true)
	defer ghServer.Close()

	backendServer := setupMockBackendHealth(t, "ok", "0.15.0", true)
	defer backendServer.Close()

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT count(*) FROM jobs WHERE status IN ('downloading', 'tagging', 'finalizing', 'matching', 'resolving_artist', 'resolving_releases', 'resolving_tracks', 'deduplicating');"}, &runner.RunResult{
		Stdout: []byte("3\n"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-t", "-A", "-c", "SELECT count(*) FROM jobs WHERE status NOT IN ('completed', 'failed', 'cancelled');"}, &runner.RunResult{
		Stdout: []byte("3\n"),
	}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner:     fake,
		GitHubURL:  ghServer.URL,
		HTTPClient: ghServer.Client(),
	}

	code := runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "docker",
		"--file", "compose.yaml",
		"--base-url", backendServer.URL,
		"update", "--dry-run",
	}, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("expected code 0 for WARNING, got %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "RESULT:\nWARNING") {
		t.Errorf("expected RESULT: WARNING, got:\n%s", out)
	}
	if !strings.Contains(out, "3 active download jobs in progress") {
		t.Errorf("expected 3 active jobs warning, got:\n%s", out)
	}
}

func TestUpdateDryRunBlockedScenarios(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server)
		expectedBlock string
	}{
		{
			name: "interrupted state",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				st := &state.State{
					StateVersion: state.CurrentStateVersion,
					Status:       state.StatusMutating,
				}
				_ = st.Save(tmpDir)
			},
			expectedBlock: "interrupted update transaction detected",
		},
		{
			name: "unhealthy backend",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				// Backend returns unavailable
			},
			expectedBlock: "backend health is unavailable",
		},
		{
			name: "missing release manifest",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				// ghServer doesn't have manifest
			},
			expectedBlock: "release-manifest.json",
		},
		{
			name: "version mismatch between running backend and configured env",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.14.0\nPOSTGRES_PASSWORD=secret\n"), 0600)
			},
			expectedBlock: "version mismatch",
		},
		{
			name: "development running version blocks managed update",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=dev\nPOSTGRES_PASSWORD=secret\n"), 0600)
			},
			expectedBlock: "not a valid semver release",
		},
		{
			name: "missing required environment variable",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\n"), 0600)
			},
			expectedBlock: "missing required configuration: POSTGRES_PASSWORD",
		},
		{
			name: "empty required environment variable",
			setup: func(t *testing.T, tmpDir string, fake *runner.FakeProcessRunner, ghServer, backendServer *httptest.Server) {
				_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\nPOSTGRES_PASSWORD=\n"), 0600)
			},
			expectedBlock: "missing required configuration: POSTGRES_PASSWORD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)
			_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_VERSION=0.15.0\nPOSTGRES_PASSWORD=secret\n"), 0600)

			includeManifest := tc.name != "missing release manifest"
			ghServer := setupMockGitHub(t, "v0.16.0", "0.16.0", includeManifest)
			defer ghServer.Close()

			backendHealthy := tc.name != "unhealthy backend"
			status := "ok"
			version := "0.15.0"
			if tc.name == "development running version blocks managed update" {
				version = "dev"
			}
			if !backendHealthy {
				status = "unavailable"
			}
			backendServer := setupMockBackendHealth(t, status, version, backendHealthy)
			defer backendServer.Close()

			fake := runner.NewFake()
			fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)

			tc.setup(t, tmpDir, fake, ghServer, backendServer)

			var stdout, stderr bytes.Buffer
			deps := CLIDependencies{
				Runner:     fake,
				GitHubURL:  ghServer.URL,
				HTTPClient: ghServer.Client(),
			}

			code := runCLIWithDeps(context.Background(), []string{
				"--project-dir", tmpDir,
				"--engine", "docker",
				"--file", "compose.yaml",
				"--base-url", backendServer.URL,
				"update", "--dry-run",
			}, &stdout, &stderr, deps)

			if code != 1 {
				t.Fatalf("expected code 1 for BLOCKED, got %d", code)
			}

			out := stdout.String()
			if !strings.Contains(out, "RESULT:\nBLOCKED") {
				t.Errorf("expected RESULT: BLOCKED, got:\n%s", out)
			}
			if !strings.Contains(out, tc.expectedBlock) {
				t.Errorf("expected block reason %q in output, got:\n%s", tc.expectedBlock, out)
			}
		})
	}
}

func listDirRecursive(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, rErr := os.ReadFile(path)
		if rErr != nil {
			return rErr
		}
		entries[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}
	return entries
}

func TestBackupCommandCLI(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services:\n  db:\n    image: postgres:18-alpine\n"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("POSTGRES_USER=ytmdl\nPOSTGRES_DB=ytmdl\n"), 0600)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("PGDUMP_DUMP_VALID_DATA"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_restore", "--list"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("; TOC list\n"),
	}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner: fake,
	}

	code := runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "docker",
		"--file", "compose.yaml",
		"backup",
	}, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Backup created: backups/ytmdl_v0.15.0_manual_") {
		t.Errorf("output missing Backup created line, got:\n%s", out)
	}
	if !strings.Contains(out, "Validation:     PASS") {
		t.Errorf("output missing Validation: PASS, got:\n%s", out)
	}

	// Verify backup file exists in default backups dir
	files, err := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected 1 file in backups dir, got %v (err: %v)", files, err)
	}
	if !strings.HasSuffix(files[0].Name(), ".dump") {
		t.Errorf("expected .dump extension, got %s", files[0].Name())
	}
}

func TestBackupCommandCustomBackupDir(t *testing.T) {
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "custom_backups")
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services:\n  db:\n    image: postgres:18-alpine\n"), 0644)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("VALID_DATA"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_restore", "--list"}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner: fake,
	}

	code := runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "docker",
		"--file", "compose.yaml",
		"backup",
		"--backup-dir", customDir,
	}, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	files, err := os.ReadDir(customDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("expected 1 file in customDir, got %v (err: %v)", files, err)
	}
}

func TestBackupCommandSucceedsEvenIfStorageGuardFails(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services:\n  db:\n    image: postgres:18-alpine\n"), 0644)
	// Even with Guard ID configured and storage path missing, backup succeeds!
	_ = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("YTMDL_STORAGE_GUARD_ID=nonexistent-id\nYTMDL_MUSIC_PATH=/nonexistent/music\n"), 0600)

	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{Stdout: []byte("Docker Compose v2.24.0\n")}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("VALID_DATA"),
	}, nil)
	fake.Register("docker", []string{"compose", "-f", "compose.yaml", "exec", "-T", "db", "pg_restore", "--list"}, &runner.RunResult{
		ExitCode: 0,
	}, nil)

	var stdout, stderr bytes.Buffer
	deps := CLIDependencies{
		Runner: fake,
	}

	code := runCLIWithDeps(context.Background(), []string{
		"--project-dir", tmpDir,
		"--engine", "docker",
		"--file", "compose.yaml",
		"backup",
	}, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("expected exit code 0 even if storage guard fails, got %d. stderr: %s", code, stderr.String())
	}
}

func TestManifestGenCommand(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "release-manifest.json")

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"manifest-gen",
		"--version", "0.16.0",
		"--tag", "v0.16.0",
		"--schema", "8",
		"--classification", "schema_neutral",
		"--min-upgrade", "0.15.0",
		"--backend-digest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--frontend-digest", "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"--output", outPath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed reading generated manifest: %v", err)
	}

	if !strings.Contains(string(content), `"release_version": "0.16.0"`) {
		t.Errorf("manifest content missing release_version: %s", string(content))
	}

	// Test missing required flag
	var stdoutErr, stderrErr bytes.Buffer
	codeErr := runCLI(context.Background(), []string{
		"manifest-gen",
		"--version", "0.16.0",
	}, &stdoutErr, &stderrErr)

	if codeErr != 2 {
		t.Fatalf("expected exit code 2 on missing digests, got %d", codeErr)
	}
}

func TestManifestGenCommand_V2(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "release-manifest.json")

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"manifest-gen",
		"--version", "0.17.0",
		"--tag", "v0.17.0",
		"--manifest-version", "2",
		"--schema", "9",
		"--update-classification", "schema_forward",
		"--classification", "backup_restore_required",
		"--supported-sources", "8",
		"--min-upgrade", "0.15.0",
		"--backend-digest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--frontend-digest", "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"--output", outPath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed reading generated manifest: %v", err)
	}

	m, err := manifest.Decode(content)
	if err != nil {
		t.Fatalf("failed to decode generated manifest: %v", err)
	}
	if err := m.Validate("v0.17.0"); err != nil {
		t.Fatalf("generated v2 manifest validation failed: %v", err)
	}
	if m.ManifestVersion != 2 || m.TargetSchema != 9 || !m.IsSchemaForward() {
		t.Errorf("unexpected manifest fields: %+v", m)
	}

	// Also test automatic defaulting for schema 9
	outPathDefault := filepath.Join(tmpDir, "manifest-default.json")
	var stdout2, stderr2 bytes.Buffer
	code2 := runCLI(context.Background(), []string{
		"manifest-gen",
		"--version", "0.17.0",
		"--schema", "9",
		"--backend-digest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--frontend-digest", "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"--output", outPathDefault,
	}, &stdout2, &stderr2)

	if code2 != 0 {
		t.Fatalf("expected exit code 0 for defaulted schema 9, got %d. stderr: %s", code2, stderr2.String())
	}
	content2, _ := os.ReadFile(outPathDefault)
	m2, err := manifest.Decode(content2)
	if err != nil || m2.ManifestVersion != 2 || m2.TargetSchema != 9 || !m2.IsSchemaForward() {
		t.Errorf("defaulted schema 9 manifest failed: %v, %+v", err, m2)
	}
}

func TestManifestGenCommand_V3(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "release-manifest-v3.json")

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{
		"manifest-gen",
		"--version", "0.17.3",
		"--tag", "v0.17.3",
		"--manifest-version", "3",
		"--schema", "9",
		"--min-upgrade", "0.15.0",
		"--backend-digest", "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"--backend-platform", "linux/amd64=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--backend-platform", "linux/arm64=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--frontend-digest", "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"--frontend-platform", "linux/amd64=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"--frontend-platform", "linux/arm64=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"--output", outPath,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed reading generated manifest: %v", err)
	}

	m, err := manifest.Decode(content)
	if err != nil {
		t.Fatalf("failed to decode generated manifest: %v", err)
	}
	if err := m.Validate("v0.17.3"); err != nil {
		t.Fatalf("generated v3 manifest validation failed: %v", err)
	}
	if m.ManifestVersion != 3 || m.TargetSchema != 9 {
		t.Errorf("unexpected manifest fields: %+v", m)
	}
	if len(m.UpgradePaths) != 2 {
		t.Errorf("expected 2 upgrade paths, got %d", len(m.UpgradePaths))
	}
	if len(m.Images.Backend.Platforms) != 2 || len(m.Images.Frontend.Platforms) != 2 {
		t.Errorf("expected 2 platforms for backend/frontend images, got %+v, %+v", m.Images.Backend.Platforms, m.Images.Frontend.Platforms)
	}
}
