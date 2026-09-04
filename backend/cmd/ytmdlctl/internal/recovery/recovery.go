// Package recovery implements operator recovery workflows for schema-migrated deployments.
package recovery

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/redact"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

var identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func isValidIdentifier(name string) bool {
	return identifierRegex.MatchString(name)
}

func quoteIdentifier(name string) (string, error) {
	if !isValidIdentifier(name) {
		return "", fmt.Errorf("invalid postgres identifier %q", name)
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

func quoteLiteral(val string) string {
	return `'` + strings.ReplaceAll(val, `'`, `''`) + `'`
}

var (
	// ErrCancelled is returned when the operator declines confirmation.
	ErrCancelled = errors.New("recovery operation cancelled by operator")
	// ErrNoRecoveryRequired is returned when the deployment is not in a recoverable state.
	ErrNoRecoveryRequired = errors.New("deployment is not in a state requiring recovery")
	// ErrTargetSchemaNotReached is returned when resume is attempted but DB schema is not at target.
	ErrTargetSchemaNotReached = errors.New("cannot resume: database schema is not at target version")
	// ErrBackupNotFound is returned when the pre-migration backup is missing.
	ErrBackupNotFound = errors.New("pre-migration database backup file not found")
)

// RecoveryStatusInfo describes the state and options for an interrupted/failed deployment.
type RecoveryStatusInfo struct {
	StateStatus              state.Status
	OperationID              string
	CurrentVersion           string
	TargetVersion            string
	SchemaBefore             int
	TargetSchema             int
	ActualSchema             int
	BackupPath               string
	BackupExists             bool
	BackupSizeBytes          int64
	RecoverySafetyBackupPath string
	QuarantineDBName         string
	BackendContainerRunning  bool
	PreviousImagesAvailable  bool
	LastError                string
	SuggestedAction          string
}

// Status inspects the deployment in a strictly read-only manner and returns recovery metadata.
func Status(ctx context.Context, eng engine.Engine, projectDir, composeFile, baseURL string) (*RecoveryStatusInfo, error) {
	st, err := state.Load(projectDir)
	if err != nil {
		return nil, fmt.Errorf("failed loading update state: %w", err)
	}
	if st == nil || st.Status == state.StatusIdle {
		return &RecoveryStatusInfo{
			StateStatus:     state.StatusIdle,
			SuggestedAction: "Deployment is idle. No recovery required.",
		}, nil
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	dbUser := envVars["POSTGRES_USER"]
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := envVars["POSTGRES_DB"]
	if dbName == "" {
		dbName = "ytmdl"
	}

	actualSchema := 0
	if eng != nil && composeFile != "" {
		if s, sErr := discovery.QueryDBSchema(ctx, eng, projectDir, composeFile, dbUser, dbName); sErr == nil {
			actualSchema = s
		}
	}

	backendRunning := false
	if eng != nil && composeFile != "" {
		running, _ := eng.IsServiceRunning(ctx, projectDir, composeFile, "backend")
		backendRunning = running
	}

	backupExists := false
	var backupSize int64
	if st.BackupPath != "" {
		fullBackupPath := st.BackupPath
		if !filepath.IsAbs(fullBackupPath) {
			fullBackupPath = filepath.Join(projectDir, fullBackupPath)
		}
		if fi, sErr := os.Stat(fullBackupPath); sErr == nil {
			backupExists = true
			backupSize = fi.Size()
		}
	}

	prevImgsOk := false
	if eng != nil && st.PreviousBackendImage != "" && st.PreviousFrontendImage != "" {
		_, bErr := eng.InspectImageID(ctx, st.PreviousBackendImage)
		_, fErr := eng.InspectImageID(ctx, st.PreviousFrontendImage)
		prevImgsOk = (bErr == nil && fErr == nil)
	}

	var suggestion string
	switch {
	case st.Status == state.StatusSuccess || st.Status == state.StatusRolledBack || st.Status == state.StatusRecovered:
		suggestion = "Deployment is stable. No recovery required."
	case actualSchema == st.TargetSchema && st.TargetSchema > st.SchemaBefore:
		suggestion = "Database schema was successfully migrated to " + strconv.Itoa(actualSchema) + ". Run 'ytmdlctl recover resume' to complete target deployment."
	case actualSchema == st.SchemaBefore:
		suggestion = "Database schema is unchanged (" + strconv.Itoa(actualSchema) + "). Run 'ytmdlctl rollback' to restore previous application."
	case backupExists:
		suggestion = "Database schema (" + strconv.Itoa(actualSchema) + ") requires manual resolution or explicit database restore. Run 'ytmdlctl recover restore' to restore Schema " + strconv.Itoa(st.SchemaBefore) + " from backup."
	default:
		suggestion = "Database requires manual administrative recovery. Pre-migration backup is missing or unverified."
	}

	return &RecoveryStatusInfo{
		StateStatus:              st.Status,
		OperationID:              st.OperationID,
		CurrentVersion:           st.CurrentVersion,
		TargetVersion:            st.TargetVersion,
		SchemaBefore:             st.SchemaBefore,
		TargetSchema:             st.TargetSchema,
		ActualSchema:             actualSchema,
		BackupPath:               st.BackupPath,
		BackupExists:             backupExists,
		BackupSizeBytes:          backupSize,
		RecoverySafetyBackupPath: st.RecoverySafetyBackupPath,
		QuarantineDBName:         st.QuarantineDBName,
		BackendContainerRunning:  backendRunning,
		PreviousImagesAvailable:  prevImgsOk,
		LastError:                st.LastError,
		SuggestedAction:          suggestion,
	}, nil
}

// ResumeOptions configures the resume workflow.
type ResumeOptions struct {
	ProjectDir  string
	ComposeFile string
	BaseURL     string
	AutoConfirm bool
	Stdout      io.Writer
	Stderr      io.Writer
	Stdin       io.Reader
}

// ResumeResult describes outcome of resuming a target deployment.
type ResumeResult struct {
	TargetVersion string
	TargetSchema  int
}

// Resume attempts to finish deployment of the target version when actual DB schema is already at target schema.
func Resume(ctx context.Context, eng engine.Engine, opts ResumeOptions) (*ResumeResult, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	st, err := state.Load(opts.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("failed loading state: %w", err)
	}
	if st == nil || (st.Status != state.StatusRecoveryRequired && st.Status != state.StatusVerifying && st.Status != state.StatusMigrating) {
		return nil, ErrNoRecoveryRequired
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(opts.ProjectDir, ".env"))
	dbUser := envVars["POSTGRES_USER"]
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := envVars["POSTGRES_DB"]
	if dbName == "" {
		dbName = "ytmdl"
	}

	actualSchema, err := discovery.QueryDBSchema(ctx, eng, opts.ProjectDir, opts.ComposeFile, dbUser, dbName)
	if err != nil {
		return nil, fmt.Errorf("failed querying database schema: %w", err)
	}
	if actualSchema != st.TargetSchema {
		return nil, fmt.Errorf("%w: actual schema is %d, expected target schema %d", ErrTargetSchemaNotReached, actualSchema, st.TargetSchema)
	}

	fmt.Fprintf(stdout, "Resuming deployment for target version %s (schema %d)...\n", st.TargetVersion, actualSchema)

	if err := st.TransitionTo(state.StatusRecoveryInProgress); err != nil {
		return nil, err
	}
	_ = st.Save(opts.ProjectDir)

	upEnv := map[string]string{"YTMDL_VERSION": st.TargetVersion}

	// 1. Start target backend
	fmt.Fprintf(stdout, "Starting target backend container...\n")
	upRes, err := eng.UpServices(ctx, opts.ProjectDir, opts.ComposeFile, upEnv, "backend")
	if err != nil || upRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed starting target backend during resume")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed starting target backend: %v", err)
	}

	// 2. Poll backend health
	deadline := time.Now().Add(60 * time.Second)
	hc := discovery.NewHealthClient(opts.BaseURL, "resume", nil)
	var lastHealthErr error
	var h *discovery.BackendHealth
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		h, lastHealthErr = hc.Check(ctx)
		if lastHealthErr == nil && h != nil && h.Status == "ok" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastHealthErr != nil || h == nil || h.Status != "ok" {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("target backend health unverified: %v", lastHealthErr))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("target backend health unverified: %v", lastHealthErr)
	}

	// 3. Verify target backend image digest
	cid, err := eng.GetServiceContainerID(ctx, opts.ProjectDir, opts.ComposeFile, "backend")
	if err == nil {
		imgRef, _, _ := eng.InspectContainerImage(ctx, cid)
		if st.TargetBackendDigest != "" {
			if err := eng.VerifyImageDigest(ctx, imgRef, st.TargetBackendDigest); err != nil {
				st.Status = state.StatusRecoveryRequired
				st.LastError = redact.String("running backend image digest mismatch")
				_ = st.Save(opts.ProjectDir)
				return nil, fmt.Errorf("running backend image digest mismatch: %w", err)
			}
		}
	}

	// 4. Start target frontend
	fmt.Fprintf(stdout, "Starting target frontend container...\n")
	upRes, err = eng.UpServices(ctx, opts.ProjectDir, opts.ComposeFile, upEnv, "frontend")
	if err != nil || upRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed starting target frontend during resume")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed starting target frontend: %v", err)
	}

	// 5. Verify frontend reachability
	fDeadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 5 * time.Second}
	var fErr error
	for time.Now().Before(fDeadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, opts.BaseURL, nil)
		if rErr == nil {
			resp, doErr := client.Do(req)
			if doErr == nil {
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					fErr = nil
					break
				}
				fErr = fmt.Errorf("frontend status %d", resp.StatusCode)
			} else {
				fErr = doErr
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if fErr != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("frontend unverified: %v", fErr))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("frontend reachability failed: %w", fErr)
	}

	// 6. Transition to SUCCESS
	if err := st.TransitionTo(state.StatusSuccess); err != nil {
		return nil, err
	}
	_ = st.Save(opts.ProjectDir)

	fmt.Fprintf(stdout, "\nResume successful! Target deployment active on version %s (schema %d)\n", st.TargetVersion, actualSchema)
	return &ResumeResult{
		TargetVersion: st.TargetVersion,
		TargetSchema:  actualSchema,
	}, nil
}

// RestoreOptions configures explicit destructive database restoration.
type RestoreOptions struct {
	ProjectDir  string
	ComposeFile string
	BaseURL     string
	BackupDir   string
	AutoConfirm bool
	Stdout      io.Writer
	Stderr      io.Writer
	Stdin       io.Reader
}

// RestoreResult describes outcome of restoring a previous database schema and application.
type RestoreResult struct {
	RestoredVersion          string
	RestoredSchema           int
	PreUpgradeBackupPath     string
	RecoverySafetyBackupPath string
	QuarantineDBName         string
}

// Restore executes an explicit, destructive database restore into a temporary database, validates it,
// swaps databases in PostgreSQL, and restores the previous application version.
func Restore(ctx context.Context, eng engine.Engine, opts RestoreOptions) (*RestoreResult, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}

	st, err := state.Load(opts.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("failed loading state: %w", err)
	}
	if st == nil {
		return nil, errors.New("no update state found")
	}

	if st.BackupPath == "" {
		return nil, ErrBackupNotFound
	}
	fullBackupPath := st.BackupPath
	if !filepath.IsAbs(fullBackupPath) {
		fullBackupPath = filepath.Join(opts.ProjectDir, fullBackupPath)
	}
	if _, err := os.Stat(fullBackupPath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBackupNotFound, fullBackupPath)
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(opts.ProjectDir, ".env"))
	dbUser := envVars["POSTGRES_USER"]
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := envVars["POSTGRES_DB"]
	if dbName == "" {
		dbName = "ytmdl"
	}

	if !isValidIdentifier(dbUser) {
		return nil, fmt.Errorf("invalid database user %q", dbUser)
	}
	if !isValidIdentifier(dbName) {
		return nil, fmt.Errorf("invalid database name %q", dbName)
	}

	// Operator confirmation
	if !opts.AutoConfirm {
		fmt.Fprintf(stdout, "\nWARNING: DESTRUCTIVE ACTION - DATABASE RESTORE\n")
		fmt.Fprintf(stdout, "=============================================\n")
		fmt.Fprintf(stdout, "Target database %q will be restored from:\n  %s\n", dbName, st.BackupPath)
		fmt.Fprintf(stdout, "Target schema will be restored to Schema %d.\n", st.SchemaBefore)
		fmt.Fprintf(stdout, "Application version will be restored to %s.\n", st.CurrentVersion)
		fmt.Fprintf(stdout, "A recovery safety backup of the current database will be created first.\n\n")
		fmt.Fprintf(stdout, "Are you sure you want to proceed? [y/N]: ")

		scanner := bufio.NewScanner(stdin)
		if !scanner.Scan() {
			return nil, ErrCancelled
		}
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans != "y" && ans != "yes" {
			return nil, ErrCancelled
		}
		fmt.Fprintf(stdout, "\n")
	}

	// Transition to RECOVERY_IN_PROGRESS
	if err := st.TransitionTo(state.StatusRecoveryInProgress); err != nil {
		return nil, err
	}
	_ = st.Save(opts.ProjectDir)

	// Step 1: Quiesce writers: Stop target backend
	fmt.Fprintf(stdout, "Stopping backend service to ensure database quiescence...\n")
	stopRes, stopErr := eng.StopServices(ctx, opts.ProjectDir, opts.ComposeFile, "backend")
	if stopErr != nil || (stopRes != nil && stopRes.ExitCode != 0) {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed stopping backend before restore")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed stopping backend service: %v", stopErr)
	}

	// Verify DB writer quiescence
	if qErr := discovery.VerifyDBQuiescence(ctx, eng, opts.ProjectDir, opts.ComposeFile, dbUser, dbName); qErr != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("database quiescence check failed: %v", qErr))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("database quiescence failed: %w", qErr)
	}

	// Step 2: Create recovery safety backup of current database state
	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(opts.ProjectDir, "backups")
	}
	fmt.Fprintf(stdout, "Creating recovery safety backup of current database state...\n")
	safetyBackupRes, err := backup.CreateBackup(ctx, eng, backup.BackupOptions{
		ProjectDir:     opts.ProjectDir,
		ComposeFile:    opts.ComposeFile,
		BackupDir:      backupDir,
		CurrentVersion: st.TargetVersion,
		TargetVersion:  "recovery_safety",
		DBUser:         dbUser,
		DBName:         dbName,
		SkipLock:       true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not create current-state safety backup: %v\n", err)
	} else if safetyBackupRes != nil {
		st.RecoverySafetyBackupPath = safetyBackupRes.RelativePath
		_ = st.Save(opts.ProjectDir)
		fmt.Fprintf(stdout, "Recovery safety backup created: %s (%s, PASS)\n", safetyBackupRes.RelativePath, formatSize(safetyBackupRes.SizeBytes))
	}

	// Step 3: Restore pre-migration backup into a NEW temporary database
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	tempDBName := fmt.Sprintf("ytmdl_rec_tmp_%s", hex.EncodeToString(randBytes))

	quotedTempDBName, err := quoteIdentifier(tempDBName)
	if err != nil {
		return nil, err
	}
	quotedDBUser, err := quoteIdentifier(dbUser)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(stdout, "Creating temporary restoration database %q...\n", tempDBName)
	createSQL := fmt.Sprintf("CREATE DATABASE %s OWNER %s;", quotedTempDBName, quotedDBUser)
	createRes, err := eng.Exec(ctx, opts.ProjectDir, opts.ComposeFile, "db", nil,
		"psql", "-U", dbUser, "-d", "postgres", "-c", createSQL)
	if err != nil || createRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed creating temporary recovery database")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed creating temporary database: %v", err)
	}

	// Cleanup temporary DB on failure
	restoreSuccess := false
	defer func() {
		if !restoreSuccess {
			dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", quotedTempDBName)
			_, _ = eng.Exec(context.Background(), opts.ProjectDir, opts.ComposeFile, "db", nil,
				"psql", "-U", dbUser, "-d", "postgres", "-c", dropSQL)
		}
	}()

	fmt.Fprintf(stdout, "Restoring pre-migration backup into %q...\n", tempDBName)
	backupFile, err := os.Open(fullBackupPath)
	if err != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed opening backup file for restore")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed opening backup file: %w", err)
	}

	restoreRes, err := eng.ExecStream(ctx, opts.ProjectDir, opts.ComposeFile, "db", backupFile, io.Discard,
		"pg_restore", "-U", dbUser, "-d", tempDBName)
	_ = backupFile.Close()
	if err != nil || restoreRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("pg_restore into temporary database failed")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("pg_restore failed: %v", err)
	}

	// Step 4: Validate restored temporary database
	fmt.Fprintf(stdout, "Validating restored temporary database...\n")
	schemaCheckSQL := "SELECT COALESCE(MAX(version), 0) FROM schema_migrations;"
	sRes, err := eng.Exec(ctx, opts.ProjectDir, opts.ComposeFile, "db", nil,
		"psql", "-U", dbUser, "-d", tempDBName, "-t", "-A", "-c", schemaCheckSQL)
	if err != nil || sRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed checking schema in restored temp database")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed checking schema in temp DB: %v", err)
	}
	tempSchema, err := strconv.Atoi(strings.TrimSpace(string(sRes.Stdout)))
	if err != nil || tempSchema != st.SchemaBefore {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("restored schema %d does not match expected schema_before %d", tempSchema, st.SchemaBefore))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("restored database schema is %d, expected %d", tempSchema, st.SchemaBefore)
	}

	// Step 5: Controlled database swap
	quarantineDB := fmt.Sprintf("ytmdl_quar_%s", time.Now().UTC().Format("20060102_150405"))
	fmt.Fprintf(stdout, "Performing controlled database swap (%q -> %q, %q -> %q)...\n", dbName, quarantineDB, tempDBName, dbName)

	quotedDBName, err := quoteIdentifier(dbName)
	if err != nil {
		return nil, err
	}
	quotedQuarantineDB, err := quoteIdentifier(quarantineDB)
	if err != nil {
		return nil, err
	}
	literalDBName := quoteLiteral(dbName)

	swapCommands := fmt.Sprintf(`
		SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();
		ALTER DATABASE %s RENAME TO %s;
		ALTER DATABASE %s RENAME TO %s;
	`, literalDBName, quotedDBName, quotedQuarantineDB, quotedTempDBName, quotedDBName)

	swapRes, err := eng.Exec(ctx, opts.ProjectDir, opts.ComposeFile, "db", nil,
		"psql", "-U", dbUser, "-d", "postgres", "-c", swapCommands)
	if err != nil || swapRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("database swap failed")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("database swap failed: %v", err)
	}

	restoreSuccess = true
	st.QuarantineDBName = quarantineDB

	// Verify restored active DB schema is now schemaBefore
	activeSchema, sErr := discovery.QueryDBSchema(ctx, eng, opts.ProjectDir, opts.ComposeFile, dbUser, dbName)
	if sErr != nil || activeSchema != st.SchemaBefore {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("active DB schema is %d after swap, expected %d", activeSchema, st.SchemaBefore))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("active database schema verification failed (got %d, want %d)", activeSchema, st.SchemaBefore)
	}

	// Step 6: Restore .env and application services
	fmt.Fprintf(stdout, "Reverting .env to version %s...\n", st.CurrentVersion)
	dotEnvPath := filepath.Join(opts.ProjectDir, ".env")
	if err := dotenv.UpdateVersion(dotEnvPath, st.CurrentVersion); err != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("failed reverting .env: %v", err))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed reverting .env: %w", err)
	}

	fmt.Fprintf(stdout, "Restoring previous application containers (%s)...\n", st.CurrentVersion)
	rbEnv := map[string]string{"YTMDL_VERSION": st.CurrentVersion}
	upRes, err := eng.UpServices(ctx, opts.ProjectDir, opts.ComposeFile, rbEnv, "backend", "frontend")
	if err != nil || upRes.ExitCode != 0 {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String("failed starting previous containers after database restore")
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("failed starting previous containers: %v", err)
	}

	// Step 7: Verify previous backend health
	deadline := time.Now().Add(60 * time.Second)
	hc := discovery.NewHealthClient(opts.BaseURL, "restore", nil)
	var lastHealthErr error
	var h *discovery.BackendHealth
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		h, lastHealthErr = hc.Check(ctx)
		if lastHealthErr == nil && h != nil && h.Status == "ok" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastHealthErr != nil || h == nil || h.Status != "ok" {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("restored backend health unverified: %v", lastHealthErr))
		_ = st.Save(opts.ProjectDir)
		return nil, fmt.Errorf("restored backend health unverified: %v", lastHealthErr)
	}

	// Step 8: Transition to RECOVERED
	if err := st.TransitionTo(state.StatusRecovered); err != nil {
		return nil, err
	}
	_ = st.Save(opts.ProjectDir)

	fmt.Fprintf(stdout, "\nDATABASE RECOVERY COMPLETE!\n")
	fmt.Fprintf(stdout, "Restored application version: %s\n", st.CurrentVersion)
	fmt.Fprintf(stdout, "Database schema:              %d\n", activeSchema)
	fmt.Fprintf(stdout, "Pre-upgrade backup used:      %s\n", st.BackupPath)
	if st.RecoverySafetyBackupPath != "" {
		fmt.Fprintf(stdout, "Recovery safety backup:       %s\n", st.RecoverySafetyBackupPath)
	}
	fmt.Fprintf(stdout, "Failed database quarantined:  %s\n", quarantineDB)

	return &RestoreResult{
		RestoredVersion:          st.CurrentVersion,
		RestoredSchema:           activeSchema,
		PreUpgradeBackupPath:     st.BackupPath,
		RecoverySafetyBackupPath: st.RecoverySafetyBackupPath,
		QuarantineDBName:         quarantineDB,
	}, nil
}

func formatSize(b int64) string {
	if b >= 1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	}
	if b >= 1024 {
		return fmt.Sprintf("%.2f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%d B", b)
}
