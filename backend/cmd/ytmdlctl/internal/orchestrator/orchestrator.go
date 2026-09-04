// Package orchestrator coordinates transactional managed updates and schema-neutral rollbacks.
package orchestrator

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
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/redact"
	"ytdm/backend/cmd/ytmdlctl/internal/release"
	"ytdm/backend/cmd/ytmdlctl/internal/staging"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
)

var (
	// ErrCancelled is returned when the operator cancels the update at confirmation.
	ErrCancelled = errors.New("update cancelled by operator")
	// ErrUnsupportedCompose is returned when update is attempted on unsupported compose layouts.
	ErrUnsupportedCompose = errors.New("managed updates are supported only for compose.ghcr.yaml")
	// ErrRecoveryRequired is returned when an operation encounters an unsafe schema drift or failed rollback.
	ErrRecoveryRequired = errors.New("system requires manual recovery")
	// ErrRolledBack is returned when an update failed and was successfully rolled back.
	ErrRolledBack = errors.New("update failed and was rolled back to previous version")
)

type (
	ReleaseResolverFunc func(ctx context.Context, tag string) (*release.ReleaseInfo, error)
	ManifestFetcherFunc func(ctx context.Context, rel *release.ReleaseInfo) (*manifest.Manifest, error)
	StagingVerifierFunc func(ctx context.Context, eng engine.Engine, opts staging.StageOptions) (*staging.StagingResult, error)
	BackupCreatorFunc   func(ctx context.Context, eng engine.Engine, opts backup.BackupOptions) (*backup.BackupResult, error)
	HealthCheckerFunc   func(ctx context.Context, baseURL string) (*discovery.BackendHealth, error)
	SchemaCheckerFunc   func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error)
	GuardCheckerFunc    func(ctx context.Context, eng engine.Engine, projectDir, composeFile, localMusicPath, expectedGuardID string) (discovery.GuardStatus, error)
	QueueCheckerFunc    func(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (int, error)
	FrontendCheckerFunc func(ctx context.Context, baseURL string) error
)

// Dependencies allows injecting sub-components for testing without altering production logic.
type Dependencies struct {
	ReleaseResolver ReleaseResolverFunc
	ManifestFetcher ManifestFetcherFunc
	StagingVerifier StagingVerifierFunc
	BackupCreator   BackupCreatorFunc
	HealthChecker   HealthCheckerFunc
	SchemaChecker   SchemaCheckerFunc
	GuardChecker    GuardCheckerFunc
	QueueChecker    QueueCheckerFunc
	FrontendChecker FrontendCheckerFunc
}

// UpdateOptions configures the managed update orchestrator.
type UpdateOptions struct {
	ProjectDir     string
	ComposeFile    string
	ExplicitEngine string
	BaseURL        string
	TargetVersion  string
	BackupDir      string
	Verbose        bool
	AutoConfirm    bool
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

// RollbackOptions configures the rollback orchestrator.
type RollbackOptions struct {
	ProjectDir     string
	ComposeFile    string
	ExplicitEngine string
	BaseURL        string
	Verbose        bool
	AutoConfirm    bool
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

// UpdateResult holds outcome details for a successful update.
type UpdateResult struct {
	PreviousVersion string
	CurrentVersion  string
	TargetSchema    int
	BackupPath      string
}

// RollbackResult holds outcome details for a successful rollback.
type RollbackResult struct {
	RestoredVersion string
	Schema          int
}

// Update executes the complete transactional managed update flow.
func Update(ctx context.Context, eng engine.Engine, deps Dependencies, opts UpdateOptions) (*UpdateResult, error) {
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

	// 1. Ambient host environment check
	if val, ok := os.LookupEnv("YTMDL_VERSION"); ok && strings.TrimSpace(val) != "" {
		return nil, fmt.Errorf("managed update blocked: YTMDL_VERSION is set in host process environment (%q); unset the host override before updating", strings.TrimSpace(val))
	}

	// 2. Compose mode check
	composeBase := filepath.Base(opts.ComposeFile)
	if composeBase != "compose.ghcr.yaml" {
		return nil, fmt.Errorf("%w: current compose file is %q", ErrUnsupportedCompose, composeBase)
	}

	// 2b. Check Podman Compose provider compatibility
	if err := engine.CheckPodmanProviderCompatibility(ctx, eng); err != nil {
		return nil, fmt.Errorf("preflight engine compatibility check failed: %w", err)
	}

	// 3. Existing transaction state check
	st, err := state.Load(opts.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("failed checking update state: %w", err)
	}
	if st != nil {
		if st.IsInterrupted() {
			return nil, fmt.Errorf("an interrupted update transaction is present (status %s, operation %s); run 'ytmdlctl rollback' or check 'ytmdlctl status' before starting a new update", st.Status, st.OperationID)
		}
		if st.Status == state.StatusRecoveryRequired {
			return nil, fmt.Errorf("system is in RECOVERY_REQUIRED state (%s); manual database recovery is required before updates can proceed", st.LastError)
		}
	}

	// 4. Validate .env
	dotEnvPath := filepath.Join(opts.ProjectDir, ".env")
	configuredVersion, err := dotenv.ValidateForUpdate(dotEnvPath)
	if err != nil {
		return nil, fmt.Errorf("preflight configuration check failed: %w", err)
	}

	// 5. Preflight read-only discovery
	envVars, _ := dotenv.ParseFile(dotEnvPath)
	dbUser := envVars["POSTGRES_USER"]
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := envVars["POSTGRES_DB"]
	if dbName == "" {
		dbName = "ytmdl"
	}

	schemaChecker := deps.SchemaChecker
	if schemaChecker == nil {
		schemaChecker = func(ctx context.Context, e engine.Engine, pDir, cFile string) (int, error) {
			return discovery.QueryDBSchema(ctx, e, pDir, cFile, dbUser, dbName)
		}
	}
	schemaBefore, err := schemaChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile)
	if err != nil {
		return nil, fmt.Errorf("preflight database schema discovery failed: %w", err)
	}

	healthChecker := deps.HealthChecker
	if healthChecker == nil {
		healthChecker = func(ctx context.Context, u string) (*discovery.BackendHealth, error) {
			hc := discovery.NewHealthClient(u, "update", nil)
			return hc.Check(ctx)
		}
	}
	runningHealth, err := healthChecker(ctx, opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("preflight backend health check failed: %w", err)
	}
	if runningHealth.CheckVersionMismatch(configuredVersion) {
		return nil, fmt.Errorf("version mismatch between running backend (%q) and configured .env (%q); update blocked", runningHealth.Version, configuredVersion)
	}

	guardChecker := deps.GuardChecker
	if guardChecker == nil {
		guardChecker = discovery.VerifyStorageGuard
	}
	guardID := envVars["YTMDL_STORAGE_GUARD_ID"]
	musicPath := envVars["YTMDL_MUSIC_PATH"]
	if musicPath == "" {
		musicPath = "/music"
	}
	guardStatus, err := guardChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile, musicPath, guardID)
	if err != nil || guardStatus != discovery.GuardStatusVerified {
		return nil, fmt.Errorf("preflight Storage Guard verification failed: status=%s (err: %v)", guardStatus, err)
	}

	queueChecker := deps.QueueChecker
	if queueChecker == nil {
		queueChecker = func(ctx context.Context, e engine.Engine, pDir, cFile string) (int, error) {
			qStatus, qErr := discovery.QueryQueueStatus(ctx, e, pDir, cFile, dbUser, dbName)
			if qErr != nil {
				return 0, qErr
			}
			return qStatus.ActiveJobs, nil
		}
	}
	activeJobs, _ := queueChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile)

	// 6. Target release resolution & strict manifest
	releaseResolver := deps.ReleaseResolver
	if releaseResolver == nil {
		releaseResolver = func(ctx context.Context, tag string) (*release.ReleaseInfo, error) {
			client := release.NewClient("", "", "update", nil)
			if tag != "" {
				return client.FetchTag(ctx, tag)
			}
			return client.FetchLatest(ctx)
		}
	}
	targetRel, err := releaseResolver(ctx, opts.TargetVersion)
	if err != nil {
		return nil, fmt.Errorf("target release resolution failed: %w", err)
	}
	targetVersion := strings.TrimPrefix(targetRel.TagName, "v")
	if !isSemverGreater(targetVersion, configuredVersion) {
		return nil, fmt.Errorf("target version %q is not newer than current version %q; downgrades are not supported", targetVersion, configuredVersion)
	}

	manifestFetcher := deps.ManifestFetcher
	if manifestFetcher == nil {
		manifestFetcher = func(ctx context.Context, rel *release.ReleaseInfo) (*manifest.Manifest, error) {
			client := release.NewClient("", "", "update", nil)
			return client.DownloadManifest(ctx, rel)
		}
	}
	targetManifest, err := manifestFetcher(ctx, targetRel)
	if err != nil {
		return nil, fmt.Errorf("release manifest validation failed: %w", err)
	}

	// Schema compatibility validation
	if err := targetManifest.ValidateSchemaCompatibility(schemaBefore); err != nil {
		return nil, fmt.Errorf("managed update blocked: %w", err)
	}
	isSchemaForward := targetManifest.IsSchemaForward()

	// Verify required environment variables
	for _, reqKey := range targetManifest.RequiredEnv {
		if val, ok := envVars[reqKey]; !ok || strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("managed update blocked: missing required environment variable %q in .env", reqKey)
		}
	}

	// 7. User confirmation prompt
	fmt.Fprintf(stdout, "\nYTMDL Update Preflight Summary\n")
	fmt.Fprintf(stdout, "==============================\n")
	fmt.Fprintf(stdout, "Current version: %s\n", configuredVersion)
	fmt.Fprintf(stdout, "Target version:  %s\n", targetVersion)
	fmt.Fprintf(stdout, "Database schema: %d -> %d (%s)\n", schemaBefore, targetManifest.TargetSchema, targetManifest.RollbackClassification)
	fmt.Fprintf(stdout, "Storage Guard:   VERIFIED\n")
	fmt.Fprintf(stdout, "Active jobs:     %d\n", activeJobs)
	fmt.Fprintf(stdout, "Database backup: enabled\n")
	if isSchemaForward {
		fmt.Fprintf(stdout, "\nWARNING: SCHEMA-FORWARD UPDATE (%d -> %d)\n", schemaBefore, targetManifest.TargetSchema)
		fmt.Fprintf(stdout, "- Backend service will be stopped prior to migration to ensure DB quiescence.\n")
		fmt.Fprintf(stdout, "- Once database schema reaches %d, automatic application rollback is disabled.\n", targetManifest.TargetSchema)
		fmt.Fprintf(stdout, "- If post-migration issues arise, explicit 'ytmdlctl recover' workflows are provided.\n")
	}
	fmt.Fprintf(stdout, "\n")

	if !opts.AutoConfirm {
		fmt.Fprintf(stdout, "Proceed with update? [y/N]: ")
		scanner := bufio.NewScanner(stdin)
		if !scanner.Scan() {
			fmt.Fprintf(stdout, "\nUpdate cancelled.\n")
			return nil, ErrCancelled
		}
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans != "y" && ans != "yes" {
			fmt.Fprintf(stdout, "Update cancelled.\n")
			return nil, ErrCancelled
		}
	}

	// 8. Capture PRE-UPDATE runtime snapshot
	prevBackendImg := targetManifest.Images.Backend.Repository + ":" + configuredVersion
	prevFrontendImg := targetManifest.Images.Frontend.Repository + ":" + configuredVersion

	prevBackendID, err := eng.InspectImageID(ctx, prevBackendImg)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting current backend image ID (%s): %w (previous image must remain locally available)", prevBackendImg, err)
	}
	prevBackendDigests, err := eng.InspectImageRepoDigests(ctx, prevBackendImg)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting current backend image digests (%s): %w (previous image must remain locally available)", prevBackendImg, err)
	}

	prevFrontendID, err := eng.InspectImageID(ctx, prevFrontendImg)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting current frontend image ID (%s): %w (previous image must remain locally available)", prevFrontendImg, err)
	}
	prevFrontendDigests, err := eng.InspectImageRepoDigests(ctx, prevFrontendImg)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting current frontend image digests (%s): %w (previous image must remain locally available)", prevFrontendImg, err)
	}

	// 9. Target Image Staging & Digest Verification
	stagingVerifier := deps.StagingVerifier
	if stagingVerifier == nil {
		stagingVerifier = staging.StageTargetImages
	}
	stageRes, err := stagingVerifier(ctx, eng, staging.StageOptions{
		ProjectDir:  opts.ProjectDir,
		ComposeFile: opts.ComposeFile,
		Manifest:    targetManifest,
	})
	if err != nil {
		return nil, fmt.Errorf("target image staging failed: %w", err)
	}

	// 10. Database Backup & Quiescence Preparation
	opID := generateOperationID()
	now := time.Now().UTC()
	st = &state.State{
		StateVersion:            state.CurrentStateVersion,
		OperationID:             opID,
		StartedAt:               now,
		UpdatedAt:               now,
		CurrentVersion:          configuredVersion,
		TargetVersion:           targetVersion,
		ComposeFile:             opts.ComposeFile,
		Engine:                  eng.Name(),
		BaseURL:                 opts.BaseURL,
		SchemaBefore:            schemaBefore,
		TargetSchema:            targetManifest.TargetSchema,
		PreviousBackendImage:    prevBackendImg,
		PreviousBackendImageID:  prevBackendID,
		PreviousBackendDigest:   primaryDigest(prevBackendDigests),
		PreviousBackendDigests:  prevBackendDigests,
		PreviousFrontendImage:   prevFrontendImg,
		PreviousFrontendImageID: prevFrontendID,
		PreviousFrontendDigest:  primaryDigest(prevFrontendDigests),
		PreviousFrontendDigests: prevFrontendDigests,
		TargetBackendImage:      stageRes.BackendImage,
		TargetBackendDigest:     stageRes.BackendDigest,
		TargetFrontendImage:     stageRes.FrontendImage,
		TargetFrontendDigest:    stageRes.FrontendDigest,
		RollbackClassification:  string(targetManifest.RollbackClassification),
	}

	backupCreator := deps.BackupCreator
	if backupCreator == nil {
		backupCreator = backup.CreateBackup
	}

	if isSchemaForward {
		// 10a. Transition to QUIESCING
		st.Status = state.StatusQuiescing
		if err := st.Save(opts.ProjectDir); err != nil {
			return nil, fmt.Errorf("failed persisting QUIESCING state: %w", err)
		}

		// Stop backend to prevent active database writes
		fmt.Fprintf(stdout, "Stopping backend service to achieve database writer quiescence...\n")
		stopRes, stopErr := eng.StopServices(ctx, opts.ProjectDir, opts.ComposeFile, "backend")
		if stopErr != nil || (stopRes != nil && stopRes.ExitCode != 0) {
			return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("failed stopping backend service: %v", stopErr))
		}

		// Verify database quiescence
		if qErr := discovery.VerifyDBQuiescence(ctx, eng, opts.ProjectDir, opts.ComposeFile, dbUser, dbName); qErr != nil {
			return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("pre-migration database quiescence check failed: %w", qErr))
		}

		// Take mandatory pre-migration backup
		backupRes, err := backupCreator(ctx, eng, backup.BackupOptions{
			ProjectDir:     opts.ProjectDir,
			ComposeFile:    opts.ComposeFile,
			BackupDir:      opts.BackupDir,
			CurrentVersion: configuredVersion,
			TargetVersion:  targetVersion,
			DBUser:         dbUser,
			DBName:         dbName,
			SkipLock:       true,
		})
		if err != nil {
			return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("pre-migration database backup failed: %w", err))
		}
		st.BackupPath = backupRes.RelativePath

		// Transition to PREPARED
		if err := st.TransitionTo(state.StatusPrepared); err != nil {
			return nil, err
		}
		if err := st.Save(opts.ProjectDir); err != nil {
			return nil, fmt.Errorf("failed persisting PREPARED state: %w", err)
		}

		// Transition to MIGRATING
		if err := st.TransitionTo(state.StatusMigrating); err != nil {
			return nil, err
		}
		if err := st.Save(opts.ProjectDir); err != nil {
			return nil, fmt.Errorf("failed persisting MIGRATING state: %w", err)
		}
	} else {
		// Schema-neutral path
		backupRes, err := backupCreator(ctx, eng, backup.BackupOptions{
			ProjectDir:     opts.ProjectDir,
			ComposeFile:    opts.ComposeFile,
			BackupDir:      opts.BackupDir,
			CurrentVersion: configuredVersion,
			TargetVersion:  targetVersion,
			DBUser:         dbUser,
			DBName:         dbName,
			SkipLock:       true,
		})
		if err != nil {
			return nil, fmt.Errorf("pre-update database backup failed: %w", err)
		}
		st.BackupPath = backupRes.RelativePath

		// Persist PREPARED state
		st.Status = state.StatusPrepared
		if err := st.Save(opts.ProjectDir); err != nil {
			return nil, fmt.Errorf("failed persisting PREPARED state: %w", err)
		}

		// Transition to MUTATING (durable before .env write!)
		if err := st.TransitionTo(state.StatusMutating); err != nil {
			return nil, err
		}
		if err := st.Save(opts.ProjectDir); err != nil {
			return nil, fmt.Errorf("failed persisting MUTATING state: %w", err)
		}
	}

	// 13. Surgically update .env
	if err := dotenv.UpdateVersion(dotEnvPath, targetVersion); err != nil {
		return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("failed updating .env: %w", err))
	}

	// 14. Recreate backend only
	upEnv := map[string]string{"YTMDL_VERSION": targetVersion}
	upRes, err := eng.UpServices(ctx, opts.ProjectDir, opts.ComposeFile, upEnv, "backend")
	if err != nil || upRes.ExitCode != 0 {
		var errMsg string
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = redact.String(string(upRes.Stderr))
		}
		return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("failed recreating backend: %s", errMsg))
	}

	// 15. Transition to VERIFYING
	if err := st.TransitionTo(state.StatusVerifying); err != nil {
		return nil, handleMutationFailure(eng, deps, opts, st, err)
	}
	_ = st.Save(opts.ProjectDir)

	// 16. Verify backend acceptance
	if err := verifyBackendAcceptance(ctx, eng, deps, opts, targetVersion, stageRes.BackendDigest, targetManifest.TargetSchema, guardID, musicPath); err != nil {
		return nil, handleMutationFailure(eng, deps, opts, st, err)
	}

	// 17. Recreate frontend only
	upRes, err = eng.UpServices(ctx, opts.ProjectDir, opts.ComposeFile, upEnv, "frontend")
	if err != nil || upRes.ExitCode != 0 {
		var errMsg string
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = redact.String(string(upRes.Stderr))
		}
		return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("failed recreating frontend: %s", errMsg))
	}

	// 18. Verify frontend acceptance
	if err := verifyFrontendAcceptance(ctx, eng, deps, opts, stageRes.FrontendDigest); err != nil {
		return nil, handleMutationFailure(eng, deps, opts, st, err)
	}

	// 19. Final checks: Storage Guard & Queue readability
	guardStatus, err = guardChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile, musicPath, guardID)
	if err != nil || guardStatus != discovery.GuardStatusVerified {
		return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("final Storage Guard check failed: %v", err))
	}
	if _, err := queueChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile); err != nil {
		return nil, handleMutationFailure(eng, deps, opts, st, fmt.Errorf("final queue readability check failed: %w", err))
	}

	// 20. Transition to SUCCESS
	if err := st.TransitionTo(state.StatusSuccess); err != nil {
		return nil, err
	}
	if err := st.Save(opts.ProjectDir); err != nil {
		return nil, fmt.Errorf("failed persisting SUCCESS state: %w", err)
	}

	fmt.Fprintf(stdout, "\nYTMDL update complete!\n")
	fmt.Fprintf(stdout, "Previous version: %s\n", configuredVersion)
	fmt.Fprintf(stdout, "Current version:  %s\n", targetVersion)
	fmt.Fprintf(stdout, "Database schema:  %d\n", targetManifest.TargetSchema)
	fmt.Fprintf(stdout, "Backup retained:  %s\n", st.BackupPath)

	return &UpdateResult{
		PreviousVersion: configuredVersion,
		CurrentVersion:  targetVersion,
		TargetSchema:    targetManifest.TargetSchema,
		BackupPath:      st.BackupPath,
	}, nil
}

// verifyBackendAcceptance waits for and checks health, version, running digest, schema, and storage guard.
func verifyBackendAcceptance(ctx context.Context, eng engine.Engine, deps Dependencies, opts UpdateOptions, expectedVersion, expectedDigest string, expectedSchema int, guardID, musicPath string) error {
	healthChecker := deps.HealthChecker
	if healthChecker == nil {
		healthChecker = func(ctx context.Context, u string) (*discovery.BackendHealth, error) {
			hc := discovery.NewHealthClient(u, "update", nil)
			return hc.Check(ctx)
		}
	}

	// Poll health endpoint with timeout
	deadline := time.Now().Add(60 * time.Second)
	var lastHealthErr error
	var h *discovery.BackendHealth
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, lastHealthErr = healthChecker(ctx, opts.BaseURL)
		if lastHealthErr == nil && h != nil && h.Status == "ok" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastHealthErr != nil || h == nil || h.Status != "ok" {
		return fmt.Errorf("backend health verification failed: %v", lastHealthErr)
	}

	cleanReported := strings.TrimPrefix(strings.TrimSpace(h.Version), "v")
	cleanExpected := strings.TrimPrefix(strings.TrimSpace(expectedVersion), "v")
	if cleanReported != cleanExpected {
		return fmt.Errorf("backend reported version mismatch: expected %q, got %q", cleanExpected, cleanReported)
	}

	// Inspect running container image digest
	cid, err := eng.GetServiceContainerID(ctx, opts.ProjectDir, opts.ComposeFile, "backend")
	if err != nil {
		return fmt.Errorf("failed obtaining backend container ID: %w", err)
	}
	imgRef, _, err := eng.InspectContainerImage(ctx, cid)
	if err != nil {
		return fmt.Errorf("failed inspecting backend container image: %w", err)
	}
	if err := eng.VerifyImageDigest(ctx, imgRef, expectedDigest); err != nil {
		return fmt.Errorf("running backend image digest verification failed: %w", err)
	}

	// DB Schema verification
	schemaChecker := deps.SchemaChecker
	if schemaChecker == nil {
		envVars, _ := dotenv.ParseFile(filepath.Join(opts.ProjectDir, ".env"))
		schemaChecker = func(ctx context.Context, e engine.Engine, pDir, cFile string) (int, error) {
			return discovery.QueryDBSchema(ctx, e, pDir, cFile, envVars["POSTGRES_USER"], envVars["POSTGRES_DB"])
		}
	}
	actualSchema, err := schemaChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile)
	if err != nil {
		return fmt.Errorf("failed verifying database schema: %w", err)
	}
	if actualSchema != expectedSchema {
		// Schema drift error has special formatting
		return fmt.Errorf("CRITICAL_SCHEMA_DRIFT: expected schema %d, but database schema changed to %d", expectedSchema, actualSchema)
	}

	// Storage guard
	guardChecker := deps.GuardChecker
	if guardChecker == nil {
		guardChecker = discovery.VerifyStorageGuard
	}
	guardStatus, err := guardChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile, musicPath, guardID)
	if err != nil || guardStatus != discovery.GuardStatusVerified {
		return fmt.Errorf("Storage Guard verification failed: status=%s (err: %v)", guardStatus, err)
	}

	return nil
}

// verifyFrontendAcceptance checks running container and HTTP reachability.
func verifyFrontendAcceptance(ctx context.Context, eng engine.Engine, deps Dependencies, opts UpdateOptions, expectedDigest string) error {
	frontendChecker := deps.FrontendChecker
	if frontendChecker == nil {
		frontendChecker = func(ctx context.Context, u string) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return err
			}
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 400 {
				return fmt.Errorf("frontend HTTP status %d", resp.StatusCode)
			}
			return nil
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = frontendChecker(ctx, opts.BaseURL)
		if lastErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("frontend HTTP reachability check failed: %w", lastErr)
	}

	cid, err := eng.GetServiceContainerID(ctx, opts.ProjectDir, opts.ComposeFile, "frontend")
	if err != nil {
		return fmt.Errorf("failed obtaining frontend container ID: %w", err)
	}
	imgRef, _, err := eng.InspectContainerImage(ctx, cid)
	if err != nil {
		return fmt.Errorf("failed inspecting frontend container image: %w", err)
	}
	if err := eng.VerifyImageDigest(ctx, imgRef, expectedDigest); err != nil {
		return fmt.Errorf("running frontend image digest verification failed: %w", err)
	}

	return nil
}

// handleMutationFailure analyzes failures during MUTATING/VERIFYING and triggers rollback or RECOVERY_REQUIRED.
func handleMutationFailure(eng engine.Engine, deps Dependencies, opts UpdateOptions, st *state.State, triggerErr error) error {
	// Probe actual database schema to determine if schema-neutral rollback is safe
	schemaChecker := deps.SchemaChecker
	if schemaChecker == nil {
		envVars, _ := dotenv.ParseFile(filepath.Join(opts.ProjectDir, ".env"))
		schemaChecker = func(ctx context.Context, e engine.Engine, pDir, cFile string) (int, error) {
			return discovery.QueryDBSchema(ctx, e, pDir, cFile, envVars["POSTGRES_USER"], envVars["POSTGRES_DB"])
		}
	}
	// Use fresh bounded background context for schema check
	probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	currentSchema, err := schemaChecker(probeCtx, eng, opts.ProjectDir, opts.ComposeFile)
	if err != nil || currentSchema != st.SchemaBefore {
		// Stop any errant target backend container to prevent crash loops / unbounded writes
		if eng != nil {
			_, _ = eng.StopServices(context.Background(), opts.ProjectDir, opts.ComposeFile, "backend")
		}
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("update failed (%v); database schema cannot be rolled back automatically (actual: %d, schema_before: %d, err: %v)", triggerErr, currentSchema, st.SchemaBefore, err))
		_ = st.Save(opts.ProjectDir)
		return fmt.Errorf("%w: update failed (%v) and database schema is %d (expected %d). Automatic rollback refused to protect database integrity. Run 'ytmdlctl recover status' for options. Database backup is preserved at %s", ErrRecoveryRequired, triggerErr, currentSchema, st.SchemaBefore, st.BackupPath)
	}

	// Schema is proven unchanged: execute automatic schema-neutral rollback
	fmt.Fprintf(opts.Stderr, "Update failed: %v\nTriggering automatic schema-neutral rollback to %s (database schema unchanged at %d)...\n", triggerErr, st.CurrentVersion, currentSchema)
	rbErr := executeRollback(context.Background(), eng, deps, opts.ProjectDir, opts.ComposeFile, opts.BaseURL, st, opts.Stdout, opts.Stderr)
	if rbErr != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("automatic rollback failed: %v", rbErr))
		_ = st.Save(opts.ProjectDir)
		return fmt.Errorf("%w: update failed (%v) and automatic rollback failed (%v). Run 'ytmdlctl recover status' for options. Backup preserved at %s", ErrRecoveryRequired, triggerErr, rbErr, st.BackupPath)
	}

	return fmt.Errorf("%w: update failed (%v); deployment restored to %s", ErrRolledBack, triggerErr, st.CurrentVersion)
}

// Rollback executes explicit operator rollback.
func Rollback(ctx context.Context, eng engine.Engine, deps Dependencies, opts RollbackOptions) (*RollbackResult, error) {
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
		return nil, fmt.Errorf("failed loading update state: %w", err)
	}
	if st == nil || st.Status == state.StatusIdle || st.CurrentVersion == "" {
		return nil, errors.New("no active or reversible transaction found in update-state.json")
	}

	if st.Status == state.StatusRecoveryRequired {
		return nil, fmt.Errorf("automatic rollback blocked: system is in RECOVERY_REQUIRED state (%s). Run 'ytmdlctl recover status' for options", st.LastError)
	}

	// Rollback from PREPARED when no mutation occurred
	if st.Status == state.StatusPrepared {
		dotEnvPath := filepath.Join(opts.ProjectDir, ".env")
		configuredVersion, _ := dotenv.ValidateForUpdate(dotEnvPath)
		if configuredVersion == st.CurrentVersion {
			_ = st.TransitionTo(state.StatusRolledBack)
			_ = st.Save(opts.ProjectDir)
			fmt.Fprintf(stdout, "Transaction was in PREPARED state with zero deployment mutations. State marked rolled back.\n")
			return &RollbackResult{
				RestoredVersion: st.CurrentVersion,
				Schema:          st.SchemaBefore,
			}, nil
		}
	}

	// Verify schema safety
	schemaChecker := deps.SchemaChecker
	if schemaChecker == nil {
		envVars, _ := dotenv.ParseFile(filepath.Join(opts.ProjectDir, ".env"))
		schemaChecker = func(ctx context.Context, e engine.Engine, pDir, cFile string) (int, error) {
			return discovery.QueryDBSchema(ctx, e, pDir, cFile, envVars["POSTGRES_USER"], envVars["POSTGRES_DB"])
		}
	}
	currentSchema, err := schemaChecker(ctx, eng, opts.ProjectDir, opts.ComposeFile)
	if err != nil || currentSchema != st.SchemaBefore {
		return nil, fmt.Errorf("cannot rollback: database schema is %d, but transaction requires schema %d (err: %v); manual recovery required. Run 'ytmdlctl recover status' for options", currentSchema, st.SchemaBefore, err)
	}

	// Verify previous images are locally present
	if _, err := eng.InspectImageID(ctx, st.PreviousBackendImage); err != nil {
		return nil, fmt.Errorf("previous backend image (%s) is not available locally: %w", st.PreviousBackendImage, err)
	}
	if _, err := eng.InspectImageID(ctx, st.PreviousFrontendImage); err != nil {
		return nil, fmt.Errorf("previous frontend image (%s) is not available locally: %w", st.PreviousFrontendImage, err)
	}

	if !opts.AutoConfirm {
		fmt.Fprintf(stdout, "Rollback to version %s (schema %d)? [y/N]: ", st.CurrentVersion, st.SchemaBefore)
		scanner := bufio.NewScanner(stdin)
		if !scanner.Scan() {
			return nil, ErrCancelled
		}
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans != "y" && ans != "yes" {
			return nil, ErrCancelled
		}
	}

	if err := executeRollback(ctx, eng, deps, opts.ProjectDir, opts.ComposeFile, opts.BaseURL, st, stdout, stderr); err != nil {
		st.Status = state.StatusRecoveryRequired
		st.LastError = redact.String(fmt.Sprintf("rollback failed: %v", err))
		_ = st.Save(opts.ProjectDir)
		return nil, err
	}

	fmt.Fprintf(stdout, "\nRollback complete! Restored to version %s (schema %d)\n", st.CurrentVersion, st.SchemaBefore)
	return &RollbackResult{
		RestoredVersion: st.CurrentVersion,
		Schema:          st.SchemaBefore,
	}, nil
}

// executeRollback performs idempotent restoration of previous version using a dedicated bounded context.
func executeRollback(parentCtx context.Context, eng engine.Engine, deps Dependencies, projectDir, composeFile, baseURL string, st *state.State, stdout, stderr io.Writer) error {
	// Use fresh bounded recovery context (never aborted by cancelled parentCtx)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = st.TransitionTo(state.StatusRollbackInProgress)
	_ = st.Save(projectDir)

	dotEnvPath := filepath.Join(projectDir, ".env")
	if err := dotenv.UpdateVersion(dotEnvPath, st.CurrentVersion); err != nil {
		return fmt.Errorf("failed reverting .env to %s: %w", st.CurrentVersion, err)
	}

	rbEnv := map[string]string{"YTMDL_VERSION": st.CurrentVersion}
	upRes, err := eng.UpServices(ctx, projectDir, composeFile, rbEnv, "backend", "frontend")
	if err != nil || upRes.ExitCode != 0 {
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			msg = redact.String(string(upRes.Stderr))
		}
		return fmt.Errorf("failed recreating previous containers: %s", msg)
	}

	// Verify backend restoration
	healthChecker := deps.HealthChecker
	if healthChecker == nil {
		healthChecker = func(ctx context.Context, u string) (*discovery.BackendHealth, error) {
			hc := discovery.NewHealthClient(u, "rollback", nil)
			return hc.Check(ctx)
		}
	}
	var h *discovery.BackendHealth
	for i := 0; i < 60; i++ {
		h, err = healthChecker(ctx, baseURL)
		if err == nil && h != nil && h.Status == "ok" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil || h == nil || h.Status != "ok" {
		return fmt.Errorf("restored backend health check failed: %v", err)
	}

	cleanReported := strings.TrimPrefix(strings.TrimSpace(h.Version), "v")
	cleanExpected := strings.TrimPrefix(strings.TrimSpace(st.CurrentVersion), "v")
	if cleanReported != cleanExpected {
		return fmt.Errorf("restored backend reported version %q, expected %q", cleanReported, cleanExpected)
	}

	// Verify frontend restoration
	frontendChecker := deps.FrontendChecker
	if frontendChecker == nil {
		frontendChecker = func(ctx context.Context, u string) error {
			req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if rErr != nil {
				return rErr
			}
			client := &http.Client{Timeout: 5 * time.Second}
			resp, rErr := client.Do(req)
			if rErr != nil {
				return rErr
			}
			resp.Body.Close()
			return nil
		}
	}
	if err := frontendChecker(ctx, baseURL); err != nil {
		return fmt.Errorf("restored frontend reachability check failed: %w", err)
	}

	// Verify restored backend and frontend image identity (order-independent snapshot matching)
	if err := verifyRestoredImageIdentity(ctx, eng, projectDir, composeFile, "backend", st.PreviousBackendImageID, st.PreviousBackendDigests); err != nil {
		return fmt.Errorf("restored backend image verification failed: %w", err)
	}
	if err := verifyRestoredImageIdentity(ctx, eng, projectDir, composeFile, "frontend", st.PreviousFrontendImageID, st.PreviousFrontendDigests); err != nil {
		return fmt.Errorf("restored frontend image verification failed: %w", err)
	}

	_ = st.TransitionTo(state.StatusRolledBack)
	_ = st.Save(projectDir)

	return nil
}

func primaryDigest(digests []string) string {
	if len(digests) > 0 {
		return digests[0]
	}
	return ""
}

func verifyRestoredImageIdentity(ctx context.Context, eng engine.Engine, projectDir, composeFile, service, expectedImageID string, expectedDigests []string) error {
	cid, err := eng.GetServiceContainerID(ctx, projectDir, composeFile, service)
	if err != nil {
		return fmt.Errorf("failed obtaining %s container ID: %w", service, err)
	}
	imgRef, imgID, err := eng.InspectContainerImage(ctx, cid)
	if err != nil {
		return fmt.Errorf("failed inspecting %s container image: %w", service, err)
	}

	// 1. If local immutable image ID matches expected snapshot ID, exact match!
	if expectedImageID != "" && imgID != "" && strings.EqualFold(imgID, expectedImageID) {
		return nil
	}

	// 2. Otherwise verify repository digests set intersection
	if len(expectedDigests) > 0 {
		runningDigests, err := eng.InspectImageRepoDigests(ctx, imgRef)
		if err == nil {
			for _, rd := range runningDigests {
				for _, ed := range expectedDigests {
					if strings.EqualFold(rd, ed) {
						return nil
					}
				}
			}
		}
	}

	if expectedImageID != "" || len(expectedDigests) > 0 {
		return fmt.Errorf("running image (ID %q) does not match expected previous image ID %q or digests %v", imgID, expectedImageID, expectedDigests)
	}
	return nil
}

func generateOperationID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "op_" + hex.EncodeToString(b)
}

func isSemverGreater(target, current string) bool {
	tClean := strings.TrimPrefix(strings.TrimSpace(target), "v")
	cClean := strings.TrimPrefix(strings.TrimSpace(current), "v")
	if tClean == cClean {
		return false
	}
	tParts := strings.Split(strings.Split(tClean, "-")[0], ".")
	cParts := strings.Split(strings.Split(cClean, "-")[0], ".")
	if len(tParts) < 3 || len(cParts) < 3 {
		return tClean > cClean
	}
	for i := 0; i < 3; i++ {
		var tNum, cNum int
		fmt.Sscanf(tParts[i], "%d", &tNum)
		fmt.Sscanf(cParts[i], "%d", &cNum)
		if tNum > cNum {
			return true
		}
		if tNum < cNum {
			return false
		}
	}
	return false
}
