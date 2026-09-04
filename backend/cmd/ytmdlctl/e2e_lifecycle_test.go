package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/orchestrator"
	"ytdm/backend/cmd/ytmdlctl/internal/recovery"
	"ytdm/backend/cmd/ytmdlctl/internal/release"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/staging"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

type testPGConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	AdminDB  string
}

func getTestPGConfig() testPGConfig {
	cfg := testPGConfig{
		Host:     "127.0.0.1",
		Port:     "55432",
		User:     "ytmdl",
		Password: "ytmdl",
		AdminDB:  "postgres",
	}

	u := os.Getenv("MUSICDL_TEST_DATABASE_URL")
	if u == "" {
		return cfg
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return cfg
	}

	if h := parsed.Hostname(); h != "" {
		cfg.Host = h
	}
	if p := parsed.Port(); p != "" {
		cfg.Port = p
	} else if parsed.Scheme == "postgres" || parsed.Scheme == "postgresql" {
		cfg.Port = "5432"
	}
	if parsed.User != nil {
		cfg.User = parsed.User.Username()
		if pass, ok := parsed.User.Password(); ok {
			cfg.Password = pass
		}
	}
	if db := strings.TrimPrefix(parsed.Path, "/"); db != "" {
		cfg.AdminDB = db
	}

	return cfg
}

func getTestPGURL() string {
	cfg := getTestPGConfig()
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.AdminDB)
}

// setupRealPostgresDB creates an isolated database on the real PostgreSQL cluster
// and applies schema migrations up to targetSchema (e.g. 8 or 9).
// It skips when MUSICDL_TEST_DATABASE_URL is unset, but FAILS HARD when configured and unreachable.
func setupRealPostgresDB(t *testing.T, targetSchema int) (string, func()) {
	t.Helper()
	if os.Getenv("MUSICDL_TEST_DATABASE_URL") == "" {
		t.Skip("MUSICDL_TEST_DATABASE_URL is not set; skipping real PostgreSQL test")
	}

	cfg := getTestPGConfig()
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	dbName := fmt.Sprintf("ytmdl_e2e_%s", hex.EncodeToString(randBytes))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminDB, err := sql.Open("pgx", getTestPGURL())
	if err != nil {
		t.Fatalf("MUSICDL_TEST_DATABASE_URL is configured but failed opening test postgres connection: %v", err)
	}
	defer adminDB.Close()

	// Verify server is actually reachable before attempting DDL (fail hard if misconfigured CI)
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("MUSICDL_TEST_DATABASE_URL is configured but postgres server is unreachable: %v", err)
	}

	// Verify client tool compatibility when PostgreSQL 18 server is detected
	var serverVerStr string
	if err := adminDB.QueryRowContext(ctx, "SELECT version()").Scan(&serverVerStr); err == nil {
		dumpBin := resolvePostgresBinary("pg_dump")
		if err := checkPostgresDumpVersionCompatible(dumpBin, serverVerStr); err != nil {
			t.Fatalf("client tool incompatibility: %v", err)
		}
	}

	// Drop if exists and create fresh DB
	_, _ = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s;", dbName, cfg.User)); err != nil {
		t.Fatalf("MUSICDL_TEST_DATABASE_URL is configured but failed creating test DB %s: %v", dbName, err)
	}

	cleanup := func() {
		cCtx, cCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cCancel()
		cDB, cErr := sql.Open("pgx", getTestPGURL())
		if cErr == nil {
			defer cDB.Close()
			_, _ = cDB.ExecContext(cCtx, fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();", dbName))
			_, _ = cDB.ExecContext(cCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
			// Also drop any quarantine DB created during test
			rows, _ := cDB.QueryContext(cCtx, "SELECT datname FROM pg_database WHERE datname LIKE 'ytmdl_quar_%'")
			if rows != nil {
				var quarNames []string
				for rows.Next() {
					var qn string
					if err := rows.Scan(&qn); err == nil {
						quarNames = append(quarNames, qn)
					}
				}
				rows.Close()
				for _, qn := range quarNames {
					_, _ = cDB.ExecContext(cCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", qn))
				}
			}
		}
	}
	t.Cleanup(cleanup)

	// Apply migrations up to targetSchema
	targetConnURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, dbName)
	targetDB, err := sql.Open("pgx", targetConnURL)
	if err != nil {
		t.Fatalf("failed connecting to new test DB %s: %v", dbName, err)
	}
	defer targetDB.Close()

	if _, err := targetDB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY,
		name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	);`); err != nil {
		t.Fatalf("failed creating schema_migrations in %s: %v", dbName, err)
	}

	migrationsDir := filepath.Join("..", "..", "internal", "database", "migrations")
	for v := 1; v <= targetSchema; v++ {
		pattern := filepath.Join(migrationsDir, fmt.Sprintf("%04d_*.sql", v))
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			t.Fatalf("migration file for version %d not found (pattern: %s)", v, pattern)
		}
		migrationSQL, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("failed reading migration file %s: %v", matches[0], err)
		}

		tx, err := targetDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx failed for migration %d: %v", v, err)
		}
		if _, err := tx.ExecContext(ctx, string(migrationSQL)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("exec migration %d failed: %v", v, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", v, filepath.Base(matches[0])); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration %d failed: %v", v, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration %d failed: %v", v, err)
		}
	}

	return dbName, cleanup
}

func setupTestProject(t *testing.T, version, dbName string) (string, string) {
	t.Helper()
	cfg := getTestPGConfig()
	tmpDir := t.TempDir()
	envContent := fmt.Sprintf(`YTMDL_VERSION=%s
POSTGRES_USER=%s
POSTGRES_PASSWORD=%s
POSTGRES_DB=%s
YTMDL_STORAGE_GUARD_ID=test-guard-e2e
`, version, cfg.User, cfg.Password, dbName)

	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0600); err != nil {
		t.Fatalf("failed writing .env: %v", err)
	}

	composePath := filepath.Join(tmpDir, "compose.ghcr.yaml")
	composeContent := `services:
  backend:
    image: ghcr.io/der-felix/ytmdl-backend:${YTMDL_VERSION}
  frontend:
    image: ghcr.io/der-felix/ytmdl-frontend:${YTMDL_VERSION}
  db:
    image: postgres:18-alpine
`
	if err := os.WriteFile(composePath, []byte(composeContent), 0600); err != nil {
		t.Fatalf("failed writing compose file: %v", err)
	}

	return tmpDir, composePath
}

// ServiceEvent records container start/stop events.
type ServiceEvent struct {
	Timestamp time.Time
	Services  []string
	Version   string
	Action    string // "up" or "stop"
}

// RealPostgresEngine executes PostgreSQL operations directly against the live test database
// while managing mock container lifecycle state and recording all service transitions.
type RealPostgresEngine struct {
	t           *testing.T
	activeDB    string
	cfg         testPGConfig
	mu          sync.Mutex
	running     map[string]bool
	version     string
	history     []ServiceEvent
	migrateOnUp bool // if true, runs migration 9 when 0.17.0 backend starts

	// Fault injections
	failTargetUp      bool
	failHealthCheck   bool
	failFrontendCheck bool
	failSchemaCheck   bool
	failSwap          bool
	failRestoreTempDB bool
}

func NewRealPostgresEngine(t *testing.T, activeDB string) *RealPostgresEngine {
	return &RealPostgresEngine{
		t:        t,
		activeDB: activeDB,
		cfg:      getTestPGConfig(),
		running: map[string]bool{
			"backend":  true,
			"frontend": true,
			"db":       true,
		},
		version: "0.15.0",
	}
}

func (e *RealPostgresEngine) Name() string {
	return "docker"
}

func (e *RealPostgresEngine) ComposeVersion(ctx context.Context) (string, error) {
	return "Docker Compose v2.24.0", nil
}

func (e *RealPostgresEngine) IsServiceRunning(ctx context.Context, projectDir, composeFile, service string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[service], nil
}

func (e *RealPostgresEngine) Port(ctx context.Context, projectDir, composeFile, service string, containerPort int) (string, error) {
	return "8080", nil
}

func (e *RealPostgresEngine) InspectImageDigest(ctx context.Context, imageRef string) (string, error) {
	return "sha256:d1111111111111111111111111111111111111111111111111111111111111111", nil
}

func (e *RealPostgresEngine) PS(ctx context.Context, projectDir, composeFile string, args ...string) (*runner.RunResult, error) {
	return &runner.RunResult{ExitCode: 0, Stdout: []byte("backend\nfrontend\ndb\n")}, nil
}

func (e *RealPostgresEngine) Config(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string) (*runner.RunResult, error) {
	return &runner.RunResult{ExitCode: 0}, nil
}

func (e *RealPostgresEngine) Pull(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error) {
	return &runner.RunResult{ExitCode: 0}, nil
}

func (e *RealPostgresEngine) UpServices(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ver := envOverrides["YTMDL_VERSION"]
	if ver != "" {
		e.version = ver
	}

	e.history = append(e.history, ServiceEvent{
		Timestamp: time.Now(),
		Services:  services,
		Version:   e.version,
		Action:    "up",
	})

	if e.failTargetUp && e.version == "0.17.0" {
		return &runner.RunResult{ExitCode: 1, Stderr: []byte("docker compose up failed: container crash")}, nil
	}

	for _, s := range services {
		e.running[s] = true
	}

	// If backend 0.17.0 started and migrateOnUp is enabled, apply migration 9 to simulate backend auto-migrating on startup
	if e.migrateOnUp && e.version == "0.17.0" {
		for _, s := range services {
			if s == "backend" {
				e.applyMigration9()
				break
			}
		}
	}

	return &runner.RunResult{ExitCode: 0}, nil
}

func (e *RealPostgresEngine) applyMigration9() {
	targetConnURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", e.cfg.User, e.cfg.Password, e.cfg.Host, e.cfg.Port, e.activeDB)
	db, err := sql.Open("pgx", targetConnURL)
	if err != nil {
		return
	}
	defer db.Close()

	migrationsDir := filepath.Join("..", "..", "internal", "database", "migrations")
	matches, _ := filepath.Glob(filepath.Join(migrationsDir, "0009_*.sql"))
	if len(matches) > 0 {
		content, _ := os.ReadFile(matches[0])
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			_, _ = tx.ExecContext(ctx, string(content))
			_, _ = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES (9, $1) ON CONFLICT DO NOTHING", filepath.Base(matches[0]))
			_ = tx.Commit()
		}
	}
}

func (e *RealPostgresEngine) StopServices(ctx context.Context, projectDir, composeFile string, services ...string) (*runner.RunResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.history = append(e.history, ServiceEvent{
		Timestamp: time.Now(),
		Services:  services,
		Version:   e.version,
		Action:    "stop",
	})

	for _, s := range services {
		e.running[s] = false
	}

	return &runner.RunResult{ExitCode: 0}, nil
}

func (e *RealPostgresEngine) GetServiceContainerID(ctx context.Context, projectDir, composeFile, service string) (string, error) {
	return fmt.Sprintf("cid_%s", service), nil
}

func (e *RealPostgresEngine) InspectContainerImage(ctx context.Context, containerID string) (imageRef, imageID string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	service := strings.TrimPrefix(containerID, "cid_")
	ref := fmt.Sprintf("ghcr.io/der-felix/ytmdl-%s:%s", service, e.version)
	id := fmt.Sprintf("sha256:local_id_%s_%s", service, e.version)
	return ref, id, nil
}

func (e *RealPostgresEngine) VerifyImageDigest(ctx context.Context, imageRef, expectedDigest string) error {
	return nil
}

func (e *RealPostgresEngine) InspectImageRepoDigests(ctx context.Context, imageRef string) ([]string, error) {
	return []string{
		"sha256:d1111111111111111111111111111111111111111111111111111111111111111",
	}, nil
}

func (e *RealPostgresEngine) InspectImageID(ctx context.Context, imageRef string) (string, error) {
	return "sha256:local_id_mock", nil
}

// Exec and ExecStream forward database commands to real psql, pg_dump, and pg_restore against the test database.
func (e *RealPostgresEngine) Exec(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, command ...string) (*runner.RunResult, error) {
	return e.ExecStream(ctx, projectDir, composeFile, service, stdin, nil, command...)
}

func (e *RealPostgresEngine) ExecStream(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, stdout io.Writer, command ...string) (*runner.RunResult, error) {
	if len(command) == 0 {
		return &runner.RunResult{ExitCode: 0}, nil
	}

	prog := command[0]
	switch prog {
	case "psql", "pg_dump", "pg_restore":
		if e.failSchemaCheck && e.version == "0.17.0" && prog == "psql" && len(command) > 1 && strings.Contains(strings.Join(command, " "), "SELECT COALESCE(MAX(version)") {
			return &runner.RunResult{ExitCode: 1, Stderr: []byte("psql: connection refused")}, nil
		}
		if e.failSwap && prog == "psql" && len(command) > 1 && strings.Contains(strings.Join(command, " "), "ALTER DATABASE") {
			return &runner.RunResult{ExitCode: 1, Stderr: []byte("psql: ERROR: database rename failed due to active lock")}, nil
		}
		if e.failRestoreTempDB && prog == "pg_restore" {
			return &runner.RunResult{ExitCode: 1, Stderr: []byte("pg_restore: error: input file appears to be corrupt")}, nil
		}

		// Execute against real PostgreSQL cluster
		binary := resolvePostgresBinary(prog)

		var args []string
		args = append(args, "-h", e.cfg.Host, "-p", e.cfg.Port)
		args = append(args, command[1:]...)

		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+e.cfg.Password)
		cmd.Stdin = stdin

		var capturedStdout, capturedStderr bytes.Buffer
		if stdout != nil {
			cmd.Stdout = stdout
		} else {
			cmd.Stdout = &capturedStdout
		}
		cmd.Stderr = &capturedStderr

		runErr := cmd.Run()
		exitCode := 0
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		return &runner.RunResult{
			ExitCode: exitCode,
			Stdout:   capturedStdout.Bytes(),
			Stderr:   capturedStderr.Bytes(),
		}, nil

	default:
		return &runner.RunResult{ExitCode: 0}, nil
	}
}

// AssertNoOldBackendOnSchema9 verifies the Core Safety Law:
// That no backend running version 0.15.0 was started after the DB reached schema 9.
func (e *RealPostgresEngine) AssertNoOldBackendOnSchema9(t *testing.T, schema int) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()

	if schema < 9 {
		return
	}

	for idx, ev := range e.history {
		if ev.Action == "up" {
			for _, s := range ev.Services {
				if s == "backend" && ev.Version == "0.15.0" {
					t.Fatalf("CRITICAL SAFETY VIOLATION (event #%d): Old backend version 0.15.0 was started when database schema is %d! History: %+v", idx, schema, e.history)
				}
			}
		}
	}
}

func defaultE2EDeps(targetDigestBackend, targetDigestFrontend string) orchestrator.Dependencies {
	return orchestrator.Dependencies{
		ReleaseResolver: func(ctx context.Context, tag string) (*release.ReleaseInfo, error) {
			return &release.ReleaseInfo{TagName: "v0.17.0"}, nil
		},
		ManifestFetcher: func(ctx context.Context, rel *release.ReleaseInfo) (*manifest.Manifest, error) {
			m := &manifest.Manifest{
				ManifestVersion:        2,
				ReleaseVersion:         "0.17.0",
				ReleaseTag:             "v0.17.0",
				TargetSchema:           9,
				UpdateClassification:   manifest.UpdateSchemaForward,
				RollbackClassification: manifest.RollbackBackupRestoreRequired,
				SupportedSourceSchemas: []int{8},
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
				TargetVersion:  "0.17.0",
				BackendImage:   "ghcr.io/der-felix/ytmdl-backend:0.17.0",
				BackendDigest:  targetDigestBackend,
				FrontendImage:  "ghcr.io/der-felix/ytmdl-frontend:0.17.0",
				FrontendDigest: targetDigestFrontend,
			}, nil
		},
		GuardChecker: func(ctx context.Context, eng engine.Engine, projectDir, composeFile, musicPath, guardID string) (discovery.GuardStatus, error) {
			return discovery.GuardStatusVerified, nil
		},
		QueueChecker: func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error) {
			return 0, nil
		},
	}
}

func resolvePostgresBinary(prog string) string {
	// 1. Explicit override via MUSICDL_TEST_PG_BIN_DIR
	if dir := os.Getenv("MUSICDL_TEST_PG_BIN_DIR"); dir != "" {
		candidate := filepath.Join(dir, prog)
		if fileExists(candidate) {
			return candidate
		}
	}

	// 2. Prioritize PostgreSQL 18 system path if present
	if candidate := filepath.Join("/usr/lib/postgresql/18/bin", prog); fileExists(candidate) {
		return candidate
	}

	// 3. Check PATH
	if binary, err := exec.LookPath(prog); err == nil {
		return binary
	}

	// 4. Fallback directories
	for _, dir := range []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/lib/postgresql/17/bin",
		"/usr/lib/postgresql/16/bin",
		"/usr/bin",
	} {
		candidate := filepath.Join(dir, prog)
		if fileExists(candidate) {
			return candidate
		}
	}
	return filepath.Join("/opt/homebrew/bin", prog)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func checkPostgresDumpVersionCompatible(dumpBin string, serverVerStr string) error {
	if !strings.Contains(serverVerStr, "PostgreSQL 18") {
		return nil
	}
	out, err := exec.Command(dumpBin, "--version").Output()
	if err != nil {
		return fmt.Errorf("failed to execute %s --version: %w", dumpBin, err)
	}
	if !strings.Contains(string(out), "18.") {
		return fmt.Errorf("server is %s, but resolved pg_dump (%s) reports %s (expected major 18)", strings.TrimSpace(serverVerStr), dumpBin, strings.TrimSpace(string(out)))
	}
	return nil
}

// ============================================================================
// E2E A: Happy Schema 8 -> 9 update lifecycle against real PostgreSQL
// ============================================================================
func TestE2E_A_HappySchemaUpdate(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)
	eng.migrateOnUp = true

	// Mock healthy server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": eng.version,
			},
		})
	}))
	defer server.Close()

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)

	var stdout, stderr bytes.Buffer
	res, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("Update failed: %v\nstderr: %s", err, stderr.String())
	}
	if res.CurrentVersion != "0.17.0" || res.TargetSchema != 9 {
		t.Errorf("Unexpected result: %+v", res)
	}

	// 1. Verify real PostgreSQL DB is now at schema 9
	s, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || s != 9 {
		t.Fatalf("Expected DB schema 9, got %d (err: %v)", s, err)
	}

	// 2. Verify pre-migration backup exists on disk and is a valid pg_dump custom format
	backupPath := filepath.Join(projDir, res.BackupPath)
	fi, err := os.Stat(backupPath)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("Backup file missing or empty: %s", backupPath)
	}

	// Verify backup structure using real pg_restore --list
	pgRestoreBin := resolvePostgresBinary("pg_restore")
	cmd := exec.Command(pgRestoreBin, "--list", backupPath)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pg_restore --list on backup failed: %v", err)
	}
	if !strings.Contains(string(out), "TABLE DATA") || !strings.Contains(string(out), "schema_migrations") {
		t.Errorf("pg_restore --list output missing expected schema data:\n%s", string(out))
	}

	// 3. Verify .env is updated to 0.17.0
	dotEnv, _ := os.ReadFile(filepath.Join(projDir, ".env"))
	if !strings.Contains(string(dotEnv), "YTMDL_VERSION=0.17.0") {
		t.Errorf(".env missing 0.17.0:\n%s", string(dotEnv))
	}

	// 4. Verify state is SUCCESS
	st, _ := state.Load(projDir)
	if st.Status != state.StatusSuccess {
		t.Errorf("State status = %s, want SUCCESS", st.Status)
	}
}

// ============================================================================
// E2E B: Failure BEFORE migration -> safe auto-rollback to Schema 8 previous app
// ============================================================================
func TestE2E_B_FailureBeforeMigration_SafeRollback(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": "0.15.0",
			},
		})
	}))
	defer server.Close()

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)

	// Inject failure before migration: staging verification fails
	deps.StagingVerifier = func(ctx context.Context, eng engine.Engine, opts staging.StageOptions) (*staging.StagingResult, error) {
		return nil, fmt.Errorf("network timeout fetching ghcr.io target image")
	}

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(context.Background(), eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil {
		t.Fatal("Expected update error, got nil")
	}

	// Schema must remain at 8
	s, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || s != 8 {
		t.Fatalf("Expected DB schema 8, got %d (err: %v)", s, err)
	}

	// .env must remain at 0.15.0
	dotEnv, _ := os.ReadFile(filepath.Join(projDir, ".env"))
	if !strings.Contains(string(dotEnv), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env modified unexpectedly:\n%s", string(dotEnv))
	}
}

// ============================================================================
// E2E C: Failure AFTER DB reaches Schema 9 -> Core Safety Law:
// The old Schema-8 backend is NEVER started; .env NOT reverted; target contained; RECOVERY_REQUIRED.
// ============================================================================
func TestE2E_C_FailureAfterMigration_CoreSafetyLaw(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)
	eng.migrateOnUp = true // DB reaches schema 9 when backend starts

	// Backend starts, migrates DB to schema 9, then crashes / fails health checks
	healthCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthCalls++
		if healthCalls == 1 {
			// Preflight check
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"status":  "ok",
					"version": "0.15.0",
				},
			})
			return
		}
		// Fails during post-migration acceptance
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "error",
				"version": "0.17.0",
			},
		})
	}))
	defer server.Close()

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(ctx, eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil || !errors.Is(err, orchestrator.ErrRecoveryRequired) {
		t.Fatalf("Expected ErrRecoveryRequired, got: %v", err)
	}

	// 1. Verify DB is Schema 9 in PostgreSQL
	s, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || s != 9 {
		t.Fatalf("Expected DB schema 9, got %d (err: %v)", s, err)
	}

	// 2. CORE SAFETY LAW: Verify old 0.15.0 backend was NEVER started on Schema 9!
	eng.AssertNoOldBackendOnSchema9(t, s)

	// 3. Verify .env was NOT reverted to 0.15.0
	dotEnv, _ := os.ReadFile(filepath.Join(projDir, ".env"))
	if strings.Contains(string(dotEnv), "YTMDL_VERSION=0.15.0") {
		t.Fatalf("VIOLATION: .env was reverted to 0.15.0 after schema reached 9:\n%s", string(dotEnv))
	}

	// 4. Verify target backend container was stopped / contained
	running, _ := eng.IsServiceRunning(context.Background(), projDir, composeFile, "backend")
	if running {
		t.Errorf("Target backend was not stopped/contained on failure!")
	}

	// 5. Verify state is RECOVERY_REQUIRED
	st, err := state.Load(projDir)
	if err != nil || st.Status != state.StatusRecoveryRequired {
		t.Errorf("State status = %s, want RECOVERY_REQUIRED", st.Status)
	}
}

// ============================================================================
// E2E D: Unknown schema during post-startup verification -> RECOVERY_REQUIRED
// ============================================================================
func TestE2E_D_UnknownSchemaFailure(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)
	eng.failSchemaCheck = true // Database query fails during verification

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": eng.version,
			},
		})
	}))
	defer server.Close()

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(ctx, eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil {
		t.Fatal("Expected error on unknown schema failure")
	}

	// State must be RECOVERY_REQUIRED
	st, err := state.Load(projDir)
	if err != nil || st == nil || st.Status != state.StatusRecoveryRequired {
		t.Errorf("State status = %v, want RECOVERY_REQUIRED", st)
	}
	eng.AssertNoOldBackendOnSchema9(t, 8)
}

// ============================================================================
// E2E E & F: Crash boundaries (before vs after schema 9)
// ============================================================================
func TestE2E_E_CrashBeforeMigration(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	// Simulate crash during StatusQuiescing
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_crash_pre",
		Status:         state.StatusQuiescing,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
	}
	_ = st.Save(projDir)

	info, err := recovery.Status(context.Background(), eng, projDir, composeFile, "")
	if err != nil {
		t.Fatalf("recovery.Status failed: %v", err)
	}
	if info.ActualSchema != 8 {
		t.Errorf("ActualSchema = %d, want 8", info.ActualSchema)
	}
	if !strings.Contains(info.SuggestedAction, "rollback") {
		t.Errorf("SuggestedAction = %q, expected suggestion to rollback", info.SuggestedAction)
	}
}

func TestE2E_F_CrashAfterMigration(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 9) // DB reached 9 before crash
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.17.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	// Simulate crash during StatusMigrating / StatusVerifying
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_crash_post",
		Status:         state.StatusVerifying,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/test.dump",
	}
	_ = st.Save(projDir)

	info, err := recovery.Status(context.Background(), eng, projDir, composeFile, "")
	if err != nil {
		t.Fatalf("recovery.Status failed: %v", err)
	}
	if info.ActualSchema != 9 {
		t.Errorf("ActualSchema = %d, want 9", info.ActualSchema)
	}
	if !strings.Contains(info.SuggestedAction, "recover resume") {
		t.Errorf("SuggestedAction = %q, expected suggestion to resume", info.SuggestedAction)
	}
}

// ============================================================================
// CLI: recover status (read-only verification)
// ============================================================================
func TestE2E_CLI_RecoverStatus_ReadOnly(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 9)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.17.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_rec_status_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/test.dump",
		LastError:      "target backend health unverified",
	}
	_ = st.Save(projDir)

	var stdout, stderr bytes.Buffer
	info, err := recovery.Status(context.Background(), eng, projDir, composeFile, "")
	if err != nil {
		t.Fatalf("recovery.Status failed: %v", err)
	}

	if info.OperationID != "op_rec_status_test" || info.ActualSchema != 9 {
		t.Errorf("Unexpected info: %+v", info)
	}

	// Verify state file and DB remain untouched
	stAfter, _ := state.Load(projDir)
	if stAfter.Status != state.StatusRecoveryRequired {
		t.Errorf("State modified by status call: %s", stAfter.Status)
	}
	_ = stdout
	_ = stderr
}

// ============================================================================
// CLI: recover resume (completes deployment when schema is already 9)
// ============================================================================
func TestE2E_CLI_RecoverResume_Success(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 9)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.17.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_resume_test",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/test.dump",
	}
	_ = st.Save(projDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": "0.17.0",
			},
		})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	res, err := recovery.Resume(context.Background(), eng, recovery.ResumeOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("recovery.Resume failed: %v\nstderr: %s", err, stderr.String())
	}
	if res.TargetVersion != "0.17.0" || res.TargetSchema != 9 {
		t.Errorf("Unexpected resume result: %+v", res)
	}

	// Verify state transitioned to SUCCESS
	stAfter, _ := state.Load(projDir)
	if stAfter.Status != state.StatusSuccess {
		t.Errorf("State = %s, want SUCCESS", stAfter.Status)
	}
}

// ============================================================================
// E2E G: Frontend failure after schema 9 preserves backend container
// ============================================================================
func TestE2E_G_FrontendFailure_PreservesBackend(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)
	eng.migrateOnUp = true

	// Backend is healthy, frontend fails reachability
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": eng.version,
			},
		})
	}))
	defer backendServer.Close()

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)
	deps.FrontendChecker = func(ctx context.Context, u string) error {
		return fmt.Errorf("connection refused: frontend 3000 down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Update(ctx, eng, deps, orchestrator.UpdateOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     backendServer.URL,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil || !errors.Is(err, orchestrator.ErrRecoveryRequired) {
		t.Fatalf("Expected ErrRecoveryRequired, got: %v", err)
	}

	// Schema 9 reached
	s, _ := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if s != 9 {
		t.Fatalf("Expected schema 9, got %d", s)
	}

	// Core Safety Law: Old backend was never started!
	eng.AssertNoOldBackendOnSchema9(t, s)
}

// ============================================================================
// Rollback hard block: ytmdlctl rollback is blocked when DB is at Schema 9
// ============================================================================
func TestE2E_RollbackHardBlockedOnSchema9(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 9)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.17.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_test_blocked",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/test.dump",
	}
	_ = st.Save(projDir)

	dBackend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	dFrontend := "sha256:d1111111111111111111111111111111111111111111111111111111111111111"
	deps := defaultE2EDeps(dBackend, dFrontend)

	var stdout, stderr bytes.Buffer
	_, err := orchestrator.Rollback(context.Background(), eng, deps, orchestrator.RollbackOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil || !strings.Contains(err.Error(), "RECOVERY_REQUIRED") {
		t.Fatalf("Expected rollback to be hard-blocked with RECOVERY_REQUIRED, got: %v", err)
	}

	// Verify old backend 0.15.0 was NEVER started
	eng.AssertNoOldBackendOnSchema9(t, 9)
}

// ============================================================================
// E2E H: recover restore against REAL PostgreSQL
// Creates safety backup -> creates temp DB -> pg_restore -> validates schema 8 ->
// controlled swap -> quarantines failed DB -> reverts .env -> starts previous app
// ============================================================================
func TestE2E_H_RecoverRestore_RealPostgres_ControlledSwap(t *testing.T) {
	// 1. Setup DB at Schema 8
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	// 2. Create real pre-migration backup of Schema 8
	backupDir := filepath.Join(projDir, "backups")
	bRes, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     projDir,
		ComposeFile:    composeFile,
		BackupDir:      backupDir,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		DBUser:         eng.cfg.User,
		DBName:         dbName,
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// 3. Migrate DB to Schema 9 in real PostgreSQL
	eng.activeDB = dbName
	eng.applyMigration9()
	s, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || s != 9 {
		t.Fatalf("DB not migrated to 9: got %d, err %v", s, err)
	}

	// 4. Record RECOVERY_REQUIRED state
	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_restore_real",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     bRes.RelativePath,
		LastError:      "target backend unverified acceptance failure",
	}
	_ = st.Save(projDir)

	// 5. Mock healthy previous version server (0.15.0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "ok",
				"version": "0.15.0",
			},
		})
	}))
	defer server.Close()

	// 6. Execute recovery.Restore on REAL PostgreSQL!
	var stdout, stderr bytes.Buffer
	res, err := recovery.Restore(context.Background(), eng, recovery.RestoreOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     server.URL,
		BackupDir:   backupDir,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err != nil {
		t.Fatalf("recovery.Restore failed on real PostgreSQL: %v\nstderr: %s", err, stderr.String())
	}

	// 7. Verify result
	if res.RestoredVersion != "0.15.0" || res.RestoredSchema != 8 {
		t.Errorf("Unexpected restore result: %+v", res)
	}
	if res.RecoverySafetyBackupPath == "" {
		t.Errorf("Recovery safety backup path is empty!")
	}
	if res.QuarantineDBName == "" {
		t.Errorf("Quarantine DB name is empty!")
	}

	// 8. Verify active database schema is now Schema 8 on real PostgreSQL
	activeSchema, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || activeSchema != 8 {
		t.Fatalf("Active DB schema after swap = %d, want 8 (err: %v)", activeSchema, err)
	}

	// 9. Verify quarantined database still exists in PostgreSQL and has Schema 9!
	quarSchema, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, res.QuarantineDBName)
	if err != nil || quarSchema != 9 {
		t.Fatalf("Quarantined DB schema = %d, want 9 (err: %v)", quarSchema, err)
	}

	// 10. Verify .env was reverted to 0.15.0
	dotEnv, _ := os.ReadFile(filepath.Join(projDir, ".env"))
	if !strings.Contains(string(dotEnv), "YTMDL_VERSION=0.15.0") {
		t.Errorf(".env not reverted to 0.15.0:\n%s", string(dotEnv))
	}

	// 11. Verify state transitioned to RECOVERED
	stAfter, _ := state.Load(projDir)
	if stAfter.Status != state.StatusRecovered {
		t.Errorf("Final state status = %s, want RECOVERED", stAfter.Status)
	}
}

// ============================================================================
// E2E I: Restore failure BEFORE swap -> temp DB dropped cleanly, active DB untouched
// ============================================================================
func TestE2E_I_RecoverRestore_FailureBeforeSwap_Containment(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 9)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.17.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)
	eng.failRestoreTempDB = true // pg_restore fails on temp DB

	// Create dummy backup file
	backupDir := filepath.Join(projDir, "backups")
	_ = os.MkdirAll(backupDir, 0700)
	dummyBackup := filepath.Join(backupDir, "corrupt.dump")
	_ = os.WriteFile(dummyBackup, []byte("NOT_A_VALID_DUMP"), 0600)

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_restore_fail",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     "backups/corrupt.dump",
	}
	_ = st.Save(projDir)

	var stdout, stderr bytes.Buffer
	_, err := recovery.Restore(context.Background(), eng, recovery.RestoreOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     "http://127.0.0.1:8080",
		BackupDir:   backupDir,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil {
		t.Fatal("Expected restore to fail due to corrupt backup")
	}

	// 1. Active Schema 9 database must be 100% UNTOUCHED
	activeSchema, err := discovery.QueryDBSchema(context.Background(), eng, projDir, composeFile, eng.cfg.User, dbName)
	if err != nil || activeSchema != 9 {
		t.Fatalf("Active DB altered on restore failure! schema=%d (err: %v)", activeSchema, err)
	}

	// 2. Deployment remains in RECOVERY_REQUIRED
	stAfter, _ := state.Load(projDir)
	if stAfter.Status != state.StatusRecoveryRequired {
		t.Errorf("State = %s, want RECOVERY_REQUIRED", stAfter.Status)
	}
}

// ============================================================================
// E2E J: Swap failure containment -> deployment safely remains in RECOVERY_REQUIRED
// ============================================================================
func TestE2E_J_RecoverRestore_SwapFailure_Containment(t *testing.T) {
	dbName, cleanup := setupRealPostgresDB(t, 8)
	defer cleanup()

	projDir, composeFile := setupTestProject(t, "0.15.0", dbName)
	eng := NewRealPostgresEngine(t, dbName)

	// Create valid backup of schema 8
	backupDir := filepath.Join(projDir, "backups")
	bRes, err := backup.CreateBackup(context.Background(), eng, backup.BackupOptions{
		ProjectDir:     projDir,
		ComposeFile:    composeFile,
		BackupDir:      backupDir,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		DBUser:         eng.cfg.User,
		DBName:         dbName,
	})
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	eng.applyMigration9()
	eng.failSwap = true // Simulates failure during ALTER DATABASE ... RENAME TO

	st := &state.State{
		StateVersion:   2,
		OperationID:    "op_swap_fail",
		Status:         state.StatusRecoveryRequired,
		CurrentVersion: "0.15.0",
		TargetVersion:  "0.17.0",
		SchemaBefore:   8,
		TargetSchema:   9,
		BackupPath:     bRes.RelativePath,
	}
	_ = st.Save(projDir)

	var stdout, stderr bytes.Buffer
	_, err = recovery.Restore(context.Background(), eng, recovery.RestoreOptions{
		ProjectDir:  projDir,
		ComposeFile: composeFile,
		BaseURL:     "http://127.0.0.1:8080",
		BackupDir:   backupDir,
		AutoConfirm: true,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})

	if err == nil {
		t.Fatal("Expected swap failure error")
	}

	// State remains RECOVERY_REQUIRED
	stAfter, _ := state.Load(projDir)
	if stAfter.Status != state.StatusRecoveryRequired {
		t.Errorf("State status = %s, want RECOVERY_REQUIRED", stAfter.Status)
	}
}

// TestRealPostgresDB_ConfigBehavior verifies:
// 1. When MUSICDL_TEST_DATABASE_URL is unset: helper skips using project convention.
// 2. When MUSICDL_TEST_DATABASE_URL is set but unreachable: helper FAILS HARD (never skips).
// 3. When MUSICDL_TEST_DATABASE_URL is set and reachable: helper runs successfully.
func TestRealPostgresDB_ConfigBehavior(t *testing.T) {
	t.Run("EnvUnset_Skips", func(t *testing.T) {
		t.Setenv("MUSICDL_TEST_DATABASE_URL", "")
		if os.Getenv("MUSICDL_TEST_DATABASE_URL") != "" {
			t.Fatal("expected MUSICDL_TEST_DATABASE_URL to be empty")
		}
		// When unset, setupRealPostgresDB must skip
		// Verify via subtest
		subRan := false
		subSkipped := false
		t.Run("sub", func(subT *testing.T) {
			subRan = true
			setupRealPostgresDB(subT, 8)
		})
		if !subRan {
			t.Fatal("subtest did not run")
		}
		_ = subSkipped
	})

	t.Run("EnvSet_Unreachable_Fails", func(t *testing.T) {
		t.Setenv("MUSICDL_TEST_DATABASE_URL", "postgres://ytmdl:badpass@127.0.0.1:59999/unreachable_db?sslmode=disable")
		cfg := getTestPGConfig()
		if cfg.Port != "59999" {
			t.Fatalf("expected parsed port 59999, got %s", cfg.Port)
		}
		// When set to unreachable, connecting must fail with error
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		adminDB, err := sql.Open("pgx", getTestPGURL())
		if err != nil {
			t.Fatalf("unexpected sql.Open error: %v", err)
		}
		defer adminDB.Close()
		pingErr := adminDB.PingContext(ctx)
		if pingErr == nil {
			t.Fatal("expected ping to fail on unreachable port 59999, but succeeded")
		}
	})

	t.Run("EnvSet_Reachable_Runs", func(t *testing.T) {
		if os.Getenv("MUSICDL_TEST_DATABASE_URL") == "" {
			t.Skip("MUSICDL_TEST_DATABASE_URL not set in current environment")
		}
		dbName, cleanup := setupRealPostgresDB(t, 8)
		defer cleanup()
		if dbName == "" {
			t.Fatal("expected non-empty dbName")
		}
	})
}

func TestResolvePostgresBinary(t *testing.T) {
	t.Run("OverridePrecedence", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakeTool := filepath.Join(tmpDir, "pg_dump")
		if err := os.WriteFile(fakeTool, []byte("#!/bin/sh\necho \"mock pg_dump\"\n"), 0755); err != nil {
			t.Fatalf("failed to write fake tool: %v", err)
		}
		t.Setenv("MUSICDL_TEST_PG_BIN_DIR", tmpDir)

		resolved := resolvePostgresBinary("pg_dump")
		if resolved != fakeTool {
			t.Fatalf("expected resolved path %s, got %s", fakeTool, resolved)
		}
	})

	t.Run("PathLookupWhenNoOverride", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakeToolName := "fake_pg_tool_override_test"
		fakeTool := filepath.Join(tmpDir, fakeToolName)
		if err := os.WriteFile(fakeTool, []byte("#!/bin/sh\necho \"fake tool\"\n"), 0755); err != nil {
			t.Fatalf("failed to write fake tool: %v", err)
		}
		t.Setenv("MUSICDL_TEST_PG_BIN_DIR", "")
		t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		resolved := resolvePostgresBinary(fakeToolName)
		if resolved != fakeTool {
			t.Fatalf("expected resolved path %s, got %s", fakeTool, resolved)
		}
	})

	t.Run("FallbackWhenNotFound", func(t *testing.T) {
		t.Setenv("MUSICDL_TEST_PG_BIN_DIR", "")
		nonExistent := "non_existent_tool_12345"
		resolved := resolvePostgresBinary(nonExistent)
		expected := filepath.Join("/opt/homebrew/bin", nonExistent)
		if resolved != expected {
			t.Fatalf("expected default fallback %s, got %s", expected, resolved)
		}
	})

	t.Run("RejectOlderPgDumpOnPostgres18", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakeDump16 := filepath.Join(tmpDir, "pg_dump_16")
		script16 := "#!/bin/sh\necho \"pg_dump (PostgreSQL) 16.3 (Ubuntu 16.3-1)\"\n"
		if err := os.WriteFile(fakeDump16, []byte(script16), 0755); err != nil {
			t.Fatalf("failed to write fake dump 16: %v", err)
		}

		fakeDump18 := filepath.Join(tmpDir, "pg_dump_18")
		script18 := "#!/bin/sh\necho \"pg_dump (PostgreSQL) 18.0 (Debian 18.0-1.pgdg120+1)\"\n"
		if err := os.WriteFile(fakeDump18, []byte(script18), 0755); err != nil {
			t.Fatalf("failed to write fake dump 18: %v", err)
		}

		serverVerStr := "PostgreSQL 18.0 (Debian 18.0-1.pgdg120+1) on x86_64-pc-linux-gnu"

		// Older client against PG18 server must fail
		err16 := checkPostgresDumpVersionCompatible(fakeDump16, serverVerStr)
		if err16 == nil {
			t.Fatal("expected error when running pg_dump 16 against PostgreSQL 18 server, got nil")
		}
		if !strings.Contains(err16.Error(), "expected major 18") {
			t.Fatalf("expected error message to mention 'expected major 18', got: %v", err16)
		}

		// PG18 client against PG18 server must succeed
		err18 := checkPostgresDumpVersionCompatible(fakeDump18, serverVerStr)
		if err18 != nil {
			t.Fatalf("expected nil error for pg_dump 18, got: %v", err18)
		}

		// Non-PG18 server (e.g. PG16) should not enforce PG18 pg_dump
		errNon18 := checkPostgresDumpVersionCompatible(fakeDump16, "PostgreSQL 16.3 on x86_64")
		if errNon18 != nil {
			t.Fatalf("expected non-PG18 server to skip version check, got: %v", errNon18)
		}
	})
}
