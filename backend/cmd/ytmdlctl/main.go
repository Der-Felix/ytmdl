// Command ytmdlctl is the host-side update and lifecycle management CLI for YTMDL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"database/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/compose"
	"ytdm/backend/cmd/ytmdlctl/internal/config"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/lock"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/orchestrator"
	"ytdm/backend/cmd/ytmdlctl/internal/reconcile"
	"ytdm/backend/cmd/ytmdlctl/internal/recovery"
	"ytdm/backend/cmd/ytmdlctl/internal/release"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/staging"
	"ytdm/backend/cmd/ytmdlctl/internal/state"
	"ytdm/backend/internal/update"
)

// Build metadata injected via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// CLIDependencies allows injecting mocks for runners and HTTP clients.
type CLIDependencies struct {
	Runner              runner.ProcessRunner
	HTTPClient          *http.Client
	GitHubURL           string
	Repository          string
	AllowDirectDBMutate bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code := runCLI(ctx, os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runCLIWithDeps(ctx, args, stdout, stderr, CLIDependencies{})
}

func runCLIWithDeps(ctx context.Context, args []string, stdout, stderr io.Writer, deps CLIDependencies) int {
	globalFlags := flag.NewFlagSet("ytmdlctl", flag.ContinueOnError)
	globalFlags.SetOutput(stderr)
	globalFlags.Usage = func() {
		printUsage(stdout)
	}

	var (
		projectDir   string
		explicitFile string
		targetEngine string
		baseURL      string
		verbose      bool
	)

	globalFlags.StringVar(&projectDir, "project-dir", ".", "project root directory")
	globalFlags.StringVar(&explicitFile, "file", "", "compose file to use")
	globalFlags.StringVar(&explicitFile, "f", "", "compose file to use (shorthand)")
	globalFlags.StringVar(&targetEngine, "engine", "", "container engine to use (docker or podman)")
	globalFlags.StringVar(&baseURL, "base-url", "", "frontend base URL")
	globalFlags.BoolVar(&verbose, "verbose", false, "enable verbose diagnostic output")

	if err := globalFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	remaining := globalFlags.Args()
	if len(remaining) == 0 {
		printUsage(stderr)
		return 2
	}

	subcommand := remaining[0]
	subArgs := remaining[1:]

	if deps.Runner == nil {
		deps.Runner = runner.New()
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if deps.GitHubURL == "" {
		if testURL := os.Getenv("YTMDL_TEST_GITHUB_URL"); testURL != "" {
			deps.GitHubURL = testURL
		} else {
			deps.GitHubURL = "https://api.github.com"
		}
	}
	if deps.Repository == "" {
		deps.Repository = "Der-Felix/ytmdl"
	}

	switch subcommand {
	case "version":
		return runVersion(stdout, subArgs)
	case "status":
		return runStatus(ctx, stdout, stderr, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "check":
		return runCheck(ctx, stdout, stderr, projectDir, baseURL, subArgs, deps)
	case "update":
		return runUpdate(ctx, stdout, stderr, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "backup":
		return runBackup(ctx, stdout, stderr, projectDir, explicitFile, targetEngine, subArgs, deps)
	case "rollback":
		return runRollback(ctx, stdout, stderr, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "manifest-gen":
		return runManifestGen(stdout, stderr, subArgs)
	case "reconcile-artists":
		return runReconcileArtists(ctx, stdout, stderr, os.Stdin, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "merge-artists":
		return runMergeArtists(ctx, stdout, stderr, os.Stdin, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "recover", "recovery":
		return runRecover(ctx, stdout, stderr, os.Stdin, projectDir, explicitFile, targetEngine, baseURL, subArgs, deps)
	case "maintenance":
		if len(subArgs) > 0 {
			switch subArgs[0] {
			case "reconcile-artists":
				return runReconcileArtists(ctx, stdout, stderr, os.Stdin, projectDir, explicitFile, targetEngine, baseURL, subArgs[1:], deps)
			case "merge-artists":
				return runMergeArtists(ctx, stdout, stderr, os.Stdin, projectDir, explicitFile, targetEngine, baseURL, subArgs[1:], deps)
			}
		}
		fmt.Fprintf(stderr, "ytmdlctl maintenance: unknown subcommand. Supported: 'reconcile-artists', 'merge-artists'\n")
		return 2
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "ytmdlctl: unknown command %q. Run 'ytmdlctl --help' for usage.\n", subcommand)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `ytmdlctl - YTMDL Host Lifecycle & Update Management CLI

Usage:
  ytmdlctl [global flags] <command> [command flags]

Commands:
  version            Display ytmdlctl version and runtime platform
  status             Inspect local deployment status and configuration
  check              Check for available releases (Stage 2)
  update             Safely update the YTMDL deployment (use --dry-run in Stage 2)
  backup             Create and validate a database backup (Stage 3)
  rollback           Revert containers to the previous working state (Stage 4)
  recover            Inspect and recover from failed or interrupted schema updates
  reconcile-artists  Safely preview and reconcile proved duplicate artist entities
  merge-artists      Manually merge duplicate artist entities into a canonical artist
  manifest-gen       Generate and validate release-manifest.json (Stage 5)

Global Flags:
  --project-dir <path>   Project directory (default: .)
  -f, --file <file>      Compose file name
  --engine <name>        Container engine (docker or podman)
  --base-url <url>       Frontend base URL
  --verbose              Verbose output
`)
}

func runVersion(w io.Writer, args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			fmt.Fprintf(w, "Usage: ytmdlctl version\n\nDisplay ytmdlctl version, build metadata, and runtime platform.\n")
			return 0
		}
	}
	fmt.Fprintf(w, "ytmdlctl version: %s\n", version)
	fmt.Fprintf(w, "git commit:      %s\n", commit)
	fmt.Fprintf(w, "build date:      %s\n", date)
	fmt.Fprintf(w, "platform:        %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return 0
}

func runStatus(ctx context.Context, stdout, stderr io.Writer, projDir, explicitFile, explicitEngine, cliBaseURL string, args []string, deps CLIDependencies) int {
	statusFlags := flag.NewFlagSet("status", flag.ContinueOnError)
	statusFlags.SetOutput(stdout)
	statusFlags.Usage = func() {
		fmt.Fprintf(stdout, `Usage: ytmdlctl status [flags]

Inspect local deployment status, compose files, engine, containers, and health.

Flags:
  --save    Persist current resolved compose file and engine to .ytmdl/config.json
`)
	}
	saveConfig := statusFlags.Bool("save", false, "persist resolved settings to .ytmdl/config.json")
	if err := statusFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	// 1. Load .env
	envVars, _ := dotenv.ParseFile(filepath.Join(projDir, ".env"))

	// 2. Load persisted config
	loadedCfg, _ := config.Load(projDir)
	persistedFile := ""
	persistedEngine := ""
	persistedBaseURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedBaseURL = loadedCfg.BaseURL
	}

	// 3. Resolve Compose
	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    false,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// 4. Resolve Engine (only when compose file is unambiguous)
	var eng engine.Engine
	engineAmbiguous := false
	engineName := "unavailable"
	engineVersion := "unknown"

	if !composeRes.IsAmbiguous && composeRes.SelectedFile != "" {
		resolvedEng, eErr := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
			ProjectDir:      projDir,
			ComposeFile:     composeRes.SelectedFile,
			ExplicitEngine:  explicitEngine,
			PersistedEngine: persistedEngine,
			IsMutating:      false,
		})
		engineAmbiguous = errors.Is(eErr, engine.ErrAmbiguousEngine)
		if resolvedEng != nil {
			eng = resolvedEng
			engineName = eng.Name()
			if ver, vErr := eng.ComposeVersion(ctx); vErr == nil {
				engineVersion = ver
			}
		}
	}

	// 5. Base URL resolution
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  cliBaseURL,
		PersistedURL: persistedBaseURL,
		Engine:       eng,
		ProjectDir:   projDir,
		ComposeFile:  composeRes.SelectedFile,
		EnvVars:      envVars,
	})

	// 6. Inspect services if compose & engine are unique
	var services map[string]discovery.ServiceStatus
	if !composeRes.IsAmbiguous && composeRes.SelectedFile != "" && eng != nil {
		services, _ = discovery.InspectServices(ctx, eng, projDir, composeRes.SelectedFile)
	}

	// 7. Check backend health via HTTP
	var backendHealth *discovery.BackendHealth
	if resolvedURL != "" {
		hClient := discovery.NewHealthClient(resolvedURL, version, deps.HTTPClient)
		backendHealth, _ = hClient.Check(ctx)
	}

	// 8. DB schema & queue inspection
	dbUser := envVars["POSTGRES_USER"]
	dbName := envVars["POSTGRES_DB"]
	dbSchema := 0
	var queueStatus *discovery.QueueStatus
	if eng != nil && composeRes.SelectedFile != "" && !composeRes.IsAmbiguous {
		if dbS, sErr := discovery.QueryDBSchema(ctx, eng, projDir, composeRes.SelectedFile, dbUser, dbName); sErr == nil {
			dbSchema = dbS
		}
		if q, qErr := discovery.QueryQueueStatus(ctx, eng, projDir, composeRes.SelectedFile, dbUser, dbName); qErr == nil {
			queueStatus = q
		}
	}

	// 9. Storage Guard inspection
	musicPath := envVars["YTMDL_MUSIC_PATH"]
	if musicPath == "" {
		musicPath = filepath.Join(projDir, "music")
	}
	guardID := envVars["YTMDL_STORAGE_GUARD_ID"]
	if guardID == "" {
		guardID = envVars["MUSICDL_STORAGE_GUARD_ID"]
	}
	guardStatus, _ := discovery.VerifyStorageGuard(ctx, eng, projDir, composeRes.SelectedFile, musicPath, guardID)

	// 10. Update state & lock
	st, _ := state.Load(projDir)
	lockHeld, lockInfo, _ := lock.CheckContention(projDir)

	// Output Status
	fmt.Fprintf(stdout, "YTMDL STATUS\n")
	fmt.Fprintf(stdout, "============\n")
	fmt.Fprintf(stdout, "Project:          %s\n", projDir)

	if composeRes.IsAmbiguous {
		fmt.Fprintf(stdout, "Compose:          [ambiguous]\n")
		fmt.Fprintf(stdout, "  [!] Multiple candidate compose files detected: %s\n", strings.Join(composeRes.Candidates, ", "))
		fmt.Fprintf(stdout, "      (Inspection requires explicit --file or status --save)\n")
	} else if composeRes.SelectedFile != "" {
		fmt.Fprintf(stdout, "Compose:          %s\n", composeRes.SelectedFile)
	} else {
		fmt.Fprintf(stdout, "Compose:          not found\n")
	}

	if engineAmbiguous {
		fmt.Fprintf(stdout, "Engine:           [ambiguous]\n")
		fmt.Fprintf(stdout, "  [!] Multiple container engines detected (docker, podman)\n")
		fmt.Fprintf(stdout, "      (Inspection requires explicit --engine or status --save)\n")
	} else if eng != nil {
		fmt.Fprintf(stdout, "Engine:           %s (%s)\n", engineName, engineVersion)
	} else {
		fmt.Fprintf(stdout, "Engine:           unavailable\n")
	}

	// Application version
	configuredVersion := envVars["YTMDL_VERSION"]
	if configuredVersion == "" {
		configuredVersion = "unknown"
	}
	if backendHealth != nil && backendHealth.Version != "" {
		if backendHealth.CheckVersionMismatch(configuredVersion) {
			fmt.Fprintf(stdout, "Application:      %s (VERSION MISMATCH: configured is %s)\n", backendHealth.Version, configuredVersion)
		} else {
			fmt.Fprintf(stdout, "Application:      %s\n", backendHealth.Version)
		}
	} else {
		fmt.Fprintf(stdout, "Application:      %s (configured, backend unreachable)\n", configuredVersion)
	}

	if dbSchema > 0 {
		fmt.Fprintf(stdout, "Database schema:  %d\n", dbSchema)
	} else {
		fmt.Fprintf(stdout, "Database schema:  unknown\n")
	}

	// Services status
	printServiceSummary(stdout, "Backend", "backend", services, backendHealth)
	printFrontendSummary(stdout, services)
	printDBSummary(stdout, services, backendHealth)

	// Storage & Storage Guard
	fmt.Fprintf(stdout, "Storage:          available\n")
	fmt.Fprintf(stdout, "Storage Guard:    %s\n", guardStatus)

	// Queue
	if queueStatus != nil {
		fmt.Fprintf(stdout, "Queue:            responsive (%d active, %d pending)\n", queueStatus.ActiveJobs, queueStatus.TotalPending)
	} else {
		fmt.Fprintf(stdout, "Queue:            unknown (database unreachable)\n")
	}

	// Update state & lock
	if lockHeld && lockInfo != nil {
		fmt.Fprintf(stdout, "Update lock:      held by PID %d since %s\n", lockInfo.PID, lockInfo.StartedAt.Format("15:04:05 UTC"))
	}
	if st != nil {
		fmt.Fprintf(stdout, "Update state:     %s (updated: %s)\n", st.Status, st.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
		if st.Status == state.StatusRecoveryRequired {
			fmt.Fprintf(stdout, "  [!] RECOVERY REQUIRED: %s\n", st.LastError)
			fmt.Fprintf(stdout, "      Run 'ytmdlctl recover status' to inspect recovery options.\n")
		} else if st.IsInterrupted() {
			fmt.Fprintf(stdout, "  [!] Interrupted update transaction detected! Status: %s\n", st.Status)
		}
	} else {
		fmt.Fprintf(stdout, "Update state:     idle\n")
	}

	// Save if requested (requires unambiguous, fully resolved settings)
	if *saveConfig {
		if composeRes.IsAmbiguous || composeRes.SelectedFile == "" {
			fmt.Fprintf(stderr, "error: cannot save configuration with ambiguous or missing compose file\n")
			return 1
		}
		if engineAmbiguous || eng == nil {
			fmt.Fprintf(stderr, "error: cannot save configuration with ambiguous or unavailable engine\n")
			return 1
		}
		if resolvedURL == "" {
			fmt.Fprintf(stderr, "error: cannot save configuration without a resolvable base URL\n")
			return 1
		}
		cfgToSave := &config.Config{
			ConfigVersion: config.CurrentConfigVersion,
			ComposeFile:   composeRes.SelectedFile,
			Engine:        engineName,
			BaseURL:       resolvedURL,
		}
		if sErr := cfgToSave.Save(projDir); sErr != nil {
			fmt.Fprintf(stderr, "error saving config: %v\n", sErr)
			return 1
		}
		fmt.Fprintf(stdout, "Configuration saved to .ytmdl/config.json\n")
	}

	return 0
}

func printServiceSummary(w io.Writer, label, serviceName string, services map[string]discovery.ServiceStatus, bh *discovery.BackendHealth) {
	if services == nil {
		fmt.Fprintf(w, "%-18s unknown\n", label+":")
		return
	}
	svc, ok := services[serviceName]
	if !ok {
		fmt.Fprintf(w, "%-18s not found\n", label+":")
		return
	}
	health := svc.Health
	if bh != nil && bh.Status != "" {
		health = bh.Status
	}
	if health != "none" && health != "" {
		fmt.Fprintf(w, "%-18s %s (%s)\n", label+":", svc.State, health)
	} else {
		fmt.Fprintf(w, "%-18s %s\n", label+":", svc.State)
	}
}

func printFrontendSummary(w io.Writer, services map[string]discovery.ServiceStatus) {
	if services == nil {
		fmt.Fprintf(w, "%-18s unknown\n", "Frontend:")
		return
	}
	svc, ok := services["frontend"]
	if !ok {
		fmt.Fprintf(w, "%-18s not found\n", "Frontend:")
		return
	}
	if svc.Health != "none" && svc.Health != "" {
		fmt.Fprintf(w, "%-18s %s (%s)\n", "Frontend:", svc.State, svc.Health)
	} else {
		fmt.Fprintf(w, "%-18s %s\n", "Frontend:", svc.State)
	}
}

func printDBSummary(w io.Writer, services map[string]discovery.ServiceStatus, bh *discovery.BackendHealth) {
	if services == nil {
		fmt.Fprintf(w, "%-18s unknown\n", "Database:")
		return
	}
	svc, ok := services["db"]
	if !ok {
		fmt.Fprintf(w, "%-18s not found\n", "Database:")
		return
	}
	health := svc.Health
	if bh != nil && !bh.DatabaseHealthy {
		health = "unhealthy"
	}
	if health != "none" && health != "" {
		fmt.Fprintf(w, "%-18s %s (%s)\n", "Database:", svc.State, health)
	} else {
		fmt.Fprintf(w, "%-18s %s\n", "Database:", svc.State)
	}
}

func runCheck(ctx context.Context, stdout, stderr io.Writer, projDir, baseURL string, args []string, deps CLIDependencies) int {
	checkFlags := flag.NewFlagSet("check", flag.ContinueOnError)
	checkFlags.SetOutput(stdout)
	checkFlags.Usage = func() {
		fmt.Fprintf(stdout, `Usage: ytmdlctl check [flags]

Check for available public releases on GitHub.
`)
	}
	if err := checkFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	// 1. Determine current version (independent of Docker/engine)
	currentVersion := ""
	if baseURL != "" {
		hClient := discovery.NewHealthClient(baseURL, version, deps.HTTPClient)
		if bh, err := hClient.Check(ctx); err == nil && bh.Version != "" {
			currentVersion = bh.Version
		}
	}
	if currentVersion == "" {
		envVars, _ := dotenv.ParseFile(filepath.Join(projDir, ".env"))
		currentVersion = envVars["YTMDL_VERSION"]
	}
	if currentVersion == "" {
		currentVersion = "0.15.0"
	}

	currentSemVer, err := update.ParseSemVer(currentVersion)
	if err != nil {
		fmt.Fprintf(stderr, "warning: current version %q is not valid semver\n", currentVersion)
	}

	// 2. Fetch latest release from GitHub
	relClient := release.NewClient(deps.GitHubURL, deps.Repository, version, deps.HTTPClient)
	rel, err := relClient.FetchLatest(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "error checking releases: %v\n", err)
		return 1
	}

	latestSemVer, err := update.ParseSemVer(rel.Version)
	if err != nil {
		fmt.Fprintf(stderr, "error: latest release version %q is invalid semver\n", rel.Version)
		return 1
	}

	// 3. Check for release-manifest.json
	hasManifest := false
	for _, a := range rel.Assets {
		if a.Name == "release-manifest.json" {
			hasManifest = true
			break
		}
	}

	manifestStatus := "unavailable (no release-manifest.json)"
	if hasManifest {
		if _, mErr := relClient.DownloadManifest(ctx, rel); mErr == nil {
			manifestStatus = "available (manifest v1 verified)"
		} else {
			manifestStatus = fmt.Sprintf("invalid (%v)", mErr)
		}
	}

	// 4. Compare versions
	stateStr := "up to date"
	if currentSemVer.Compare(latestSemVer) < 0 {
		stateStr = "update available"
	}

	fmt.Fprintf(stdout, "YTMDL UPDATE CHECK\n")
	fmt.Fprintf(stdout, "==================\n")
	fmt.Fprintf(stdout, "Current version:           %s\n", currentVersion)
	fmt.Fprintf(stdout, "Latest public release:     %s\n", rel.Version)
	fmt.Fprintf(stdout, "State:                     %s\n", stateStr)
	fmt.Fprintf(stdout, "Managed update metadata:   %s\n", manifestStatus)
	if rel.HTMLURL != "" {
		fmt.Fprintf(stdout, "Release URL:               %s\n", rel.HTMLURL)
	}

	return 0
}

func runUpdate(ctx context.Context, stdout, stderr io.Writer, projDir, explicitFile, explicitEngine, baseURL string, args []string, deps CLIDependencies) int {
	updateFlags := flag.NewFlagSet("update", flag.ContinueOnError)
	updateFlags.SetOutput(stdout)
	dryRun := updateFlags.Bool("dry-run", false, "perform read-only preflight readiness validation without making changes")
	autoConfirm := updateFlags.Bool("yes", false, "automatically confirm prompt without asking")
	autoConfirmShort := updateFlags.Bool("y", false, "automatically confirm prompt without asking")
	targetVersion := updateFlags.String("target", "", "target version to update to (defaults to latest stable release)")
	backupDir := updateFlags.String("backup-dir", "", "directory to store pre-update backup (defaults to backups/)")

	if err := updateFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *dryRun {
		// Execute STRICT READ-ONLY Dry Run
		return runUpdateDryRun(ctx, stdout, stderr, projDir, explicitFile, explicitEngine, baseURL, deps)
	}

	// Managed Update execution
	projectDir, err := filepath.Abs(projDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed resolving project directory: %v\n", err)
		return 1
	}

	fl, err := lock.Acquire(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed acquiring lock: %v\n", err)
		return 1
	}
	defer fl.Release()

	loadedCfg, _ := config.Load(projectDir)
	persistedFile := ""
	persistedEngine := ""
	persistedURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedURL = loadedCfg.BaseURL
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}
	composeFile := composeRes.SelectedFile

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projectDir,
		ComposeFile:     composeFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  baseURL,
		PersistedURL: persistedURL,
		Engine:       eng,
		ProjectDir:   projectDir,
		ComposeFile:  composeFile,
		EnvVars:      envVars,
	})

	confirm := *autoConfirm || *autoConfirmShort
	orchDeps := orchestrator.Dependencies{
		ReleaseResolver: func(ctx context.Context, tag string) (*release.ReleaseInfo, error) {
			client := release.NewClient(deps.GitHubURL, deps.Repository, version, deps.HTTPClient)
			if tag != "" {
				return client.FetchTag(ctx, tag)
			}
			return client.FetchLatest(ctx)
		},
		ManifestFetcher: func(ctx context.Context, rel *release.ReleaseInfo) (*manifest.Manifest, error) {
			client := release.NewClient(deps.GitHubURL, deps.Repository, version, deps.HTTPClient)
			return client.DownloadManifest(ctx, rel)
		},
		StagingVerifier: staging.StageTargetImages,
		BackupCreator:   backup.CreateBackup,
		HealthChecker: func(ctx context.Context, u string) (*discovery.BackendHealth, error) {
			hc := discovery.NewHealthClient(u, version, deps.HTTPClient)
			return hc.Check(ctx)
		},
	}

	_, err = orchestrator.Update(ctx, eng, orchDeps, orchestrator.UpdateOptions{
		ProjectDir:     projectDir,
		ComposeFile:    composeFile,
		ExplicitEngine: explicitEngine,
		BaseURL:        resolvedURL,
		TargetVersion:  *targetVersion,
		BackupDir:      *backupDir,
		AutoConfirm:    confirm,
		Stdout:         stdout,
		Stderr:         stderr,
		Stdin:          os.Stdin,
	})

	if err != nil {
		if errors.Is(err, orchestrator.ErrCancelled) {
			return 0
		}
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}

	return 0
}

func runRollback(ctx context.Context, stdout, stderr io.Writer, projDir, explicitFile, explicitEngine, baseURL string, args []string, deps CLIDependencies) int {
	rbFlags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	rbFlags.SetOutput(stdout)
	autoConfirm := rbFlags.Bool("yes", false, "automatically confirm rollback prompt")
	autoConfirmShort := rbFlags.Bool("y", false, "automatically confirm rollback prompt")

	if err := rbFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	projectDir, err := filepath.Abs(projDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed resolving project directory: %v\n", err)
		return 1
	}

	fl, err := lock.Acquire(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed acquiring lock: %v\n", err)
		return 1
	}
	defer fl.Release()

	loadedCfg, _ := config.Load(projectDir)
	persistedFile := ""
	persistedEngine := ""
	persistedURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedURL = loadedCfg.BaseURL
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}
	composeFile := composeRes.SelectedFile

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projectDir,
		ComposeFile:     composeFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  baseURL,
		PersistedURL: persistedURL,
		Engine:       eng,
		ProjectDir:   projectDir,
		ComposeFile:  composeFile,
		EnvVars:      envVars,
	})

	confirm := *autoConfirm || *autoConfirmShort
	orchDeps := orchestrator.Dependencies{
		HealthChecker: func(ctx context.Context, u string) (*discovery.BackendHealth, error) {
			hc := discovery.NewHealthClient(u, version, deps.HTTPClient)
			return hc.Check(ctx)
		},
	}

	_, err = orchestrator.Rollback(ctx, eng, orchDeps, orchestrator.RollbackOptions{
		ProjectDir:     projectDir,
		ComposeFile:    composeFile,
		ExplicitEngine: explicitEngine,
		BaseURL:        resolvedURL,
		AutoConfirm:    confirm,
		Stdout:         stdout,
		Stderr:         stderr,
		Stdin:          os.Stdin,
	})

	if err != nil {
		if errors.Is(err, orchestrator.ErrCancelled) {
			return 0
		}
		fmt.Fprintf(stderr, "ytmdlctl: %v\n", err)
		return 1
	}

	return 0
}

func runUpdateDryRun(ctx context.Context, stdout, stderr io.Writer, projDir, explicitFile, explicitEngine, cliBaseURL string, deps CLIDependencies) int {
	var blockedReasons []string
	var warningReasons []string

	// 1. Lock contention check (Read-Only)
	if locked, info, _ := lock.CheckContention(projDir); locked && info != nil {
		blockedReasons = append(blockedReasons, fmt.Sprintf("update lock currently held by PID %d", info.PID))
	}

	// 2. Interrupted state check
	st, _ := state.Load(projDir)
	if st != nil && st.IsInterrupted() {
		blockedReasons = append(blockedReasons, fmt.Sprintf("interrupted update transaction detected (status: %s); recovery required", st.Status))
	}

	// 3. Compose & Engine resolution
	envVars, _ := dotenv.ParseFile(filepath.Join(projDir, ".env"))
	loadedCfg, _ := config.Load(projDir)
	persistedFile := ""
	persistedEngine := ""
	persistedBaseURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedBaseURL = loadedCfg.BaseURL
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    true, // Strict check for dry-run
	})
	if err != nil {
		blockedReasons = append(blockedReasons, fmt.Sprintf("compose resolution failed: %v", err))
	}

	selectedFile := ""
	if composeRes != nil {
		selectedFile = composeRes.SelectedFile
	}

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projDir,
		ComposeFile:     selectedFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      true, // Strict check
	})
	if err != nil {
		blockedReasons = append(blockedReasons, fmt.Sprintf("engine resolution failed: %v", err))
	} else if eng != nil {
		if pErr := engine.CheckPodmanProviderCompatibility(ctx, eng); pErr != nil {
			blockedReasons = append(blockedReasons, pErr.Error())
		}
	}

	// 4. Inspect Services
	var services map[string]discovery.ServiceStatus
	if eng != nil && selectedFile != "" {
		services, _ = discovery.InspectServices(ctx, eng, projDir, selectedFile)
	}

	// 5. Backend HTTP Health
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  cliBaseURL,
		PersistedURL: persistedBaseURL,
		Engine:       eng,
		ProjectDir:   projDir,
		ComposeFile:  selectedFile,
		EnvVars:      envVars,
	})

	var backendHealth *discovery.BackendHealth
	if resolvedURL != "" {
		hClient := discovery.NewHealthClient(resolvedURL, version, deps.HTTPClient)
		if bh, hErr := hClient.Check(ctx); hErr == nil {
			backendHealth = bh
			if bh.Status != "ok" {
				blockedReasons = append(blockedReasons, fmt.Sprintf("backend health is %s", bh.Status))
			}
			if !bh.DatabaseHealthy {
				blockedReasons = append(blockedReasons, "backend reports database unhealthy")
			}
		} else {
			blockedReasons = append(blockedReasons, fmt.Sprintf("backend HTTP health check failed: %v", hErr))
		}
	} else {
		blockedReasons = append(blockedReasons, "could not resolve base URL for health check")
	}

	// 6. DB Schema & Queue
	dbUser := envVars["POSTGRES_USER"]
	dbName := envVars["POSTGRES_DB"]
	dbSchema := 0
	activeJobs := 0
	if eng != nil && selectedFile != "" {
		if schema, sErr := discovery.QueryDBSchema(ctx, eng, projDir, selectedFile, dbUser, dbName); sErr == nil {
			dbSchema = schema
		}
		if q, qErr := discovery.QueryQueueStatus(ctx, eng, projDir, selectedFile, dbUser, dbName); qErr == nil {
			activeJobs = q.ActiveJobs
			if activeJobs > 0 {
				warningReasons = append(warningReasons, fmt.Sprintf("%d active download jobs in progress", activeJobs))
			}
		}
	}

	// 7. Storage Guard (Authoritative container mount namespace verification)
	musicPath := envVars["YTMDL_MUSIC_PATH"]
	if musicPath == "" {
		musicPath = filepath.Join(projDir, "music")
	}
	guardID := envVars["YTMDL_STORAGE_GUARD_ID"]
	if guardID == "" {
		guardID = envVars["MUSICDL_STORAGE_GUARD_ID"]
	}
	guardStatus, _ := discovery.VerifyStorageGuard(ctx, eng, projDir, selectedFile, musicPath, guardID)
	if guardStatus == discovery.GuardStatusMissing || guardStatus == discovery.GuardStatusMismatch {
		blockedReasons = append(blockedReasons, fmt.Sprintf("storage guard verification failed: %s", guardStatus))
	}

	// 8. Remote Release & Manifest
	currentVersion := envVars["YTMDL_VERSION"]
	if backendHealth != nil && backendHealth.Version != "" {
		currentVersion = backendHealth.Version
		// Check version mismatch between running backend and configured YTMDL_VERSION
		configuredVer := envVars["YTMDL_VERSION"]
		if configuredVer != "" && backendHealth.CheckVersionMismatch(configuredVer) {
			blockedReasons = append(blockedReasons, fmt.Sprintf("version mismatch: running backend is %s, but configured YTMDL_VERSION is %s", backendHealth.Version, configuredVer))
		}
	}
	if currentVersion == "" {
		currentVersion = "0.15.0"
	}

	// Verify running version is valid SemVer (non-dev)
	currentSemVer, curErr := update.ParseSemVer(currentVersion)
	if curErr != nil || currentVersion == "dev" || currentVersion == "development" {
		blockedReasons = append(blockedReasons, fmt.Sprintf("running backend version %q is not a valid semver release; managed update requires a release version", currentVersion))
	}

	relClient := release.NewClient(deps.GitHubURL, deps.Repository, version, deps.HTTPClient)
	rel, err := relClient.FetchLatest(ctx)
	var targetManifest *manifest.Manifest
	if err != nil {
		blockedReasons = append(blockedReasons, fmt.Sprintf("failed fetching latest GitHub release: %v", err))
	} else {
		m, mErr := relClient.DownloadManifest(ctx, rel)
		if mErr != nil {
			blockedReasons = append(blockedReasons, fmt.Sprintf("failed downloading/validating release-manifest.json: %v", mErr))
		} else {
			targetManifest = m
			if eligErr := m.CheckEligibility(currentVersion); eligErr != nil {
				blockedReasons = append(blockedReasons, fmt.Sprintf("upgrade ineligible: %v", eligErr))
			}

			// Validate target version is strictly greater than current version (no downgrade)
			if curErr == nil {
				targetSemVer, tErr := update.ParseSemVer(m.ReleaseVersion)
				if tErr == nil {
					if targetSemVer.Compare(currentSemVer) <= 0 {
						blockedReasons = append(blockedReasons, fmt.Sprintf("target version %s must be strictly greater than current version %s (downgrade not allowed)", m.ReleaseVersion, currentVersion))
					}
				}
			}

			// Schema compatibility check
			if dbSchema > 0 {
				if sCompatErr := m.ValidateSchemaCompatibility(dbSchema); sCompatErr != nil {
					blockedReasons = append(blockedReasons, fmt.Sprintf("schema compatibility error: %v", sCompatErr))
				}
			}

			// Check required_env with deterministic precedence (process env over .env; must be non-empty)
			for _, envKey := range m.RequiredEnv {
				val := getEffectiveEnv(envKey, envVars)
				if val == "" {
					blockedReasons = append(blockedReasons, fmt.Sprintf("missing required configuration: %s", envKey))
				}
			}
		}
	}

	// 9. Structured Summary Output
	fmt.Fprintf(stdout, "YTMDL UPDATE DRY RUN\n\n")

	fmt.Fprintf(stdout, "Deployment\n")
	fmt.Fprintf(stdout, "----------\n")
	if selectedFile != "" {
		fmt.Fprintf(stdout, "Compose: %s\n", selectedFile)
	} else {
		fmt.Fprintf(stdout, "Compose: [unresolved]\n")
	}
	if eng != nil {
		fmt.Fprintf(stdout, "Engine:  %s\n\n", eng.Name())
	} else {
		fmt.Fprintf(stdout, "Engine:  [unresolved]\n\n")
	}

	fmt.Fprintf(stdout, "Current\n")
	fmt.Fprintf(stdout, "-------\n")
	fmt.Fprintf(stdout, "Version:       %s\n", currentVersion)
	if dbSchema > 0 {
		fmt.Fprintf(stdout, "Schema:        %d\n", dbSchema)
	} else {
		fmt.Fprintf(stdout, "Schema:        unknown\n")
	}
	backendState := "unhealthy"
	if backendHealth != nil && backendHealth.Status == "ok" {
		backendState = "healthy"
	}
	fmt.Fprintf(stdout, "Backend:       %s\n", backendState)
	frontendState := "unknown"
	if services != nil {
		if fSvc, ok := services["frontend"]; ok && fSvc.State == "running" {
			frontendState = "healthy"
		}
	}
	fmt.Fprintf(stdout, "Frontend:      %s\n", frontendState)
	dbState := "unhealthy"
	if backendHealth != nil && backendHealth.DatabaseHealthy {
		dbState = "healthy"
	}
	fmt.Fprintf(stdout, "Database:      %s\n", dbState)
	fmt.Fprintf(stdout, "Storage Guard: %s\n", guardStatus)
	fmt.Fprintf(stdout, "Active jobs:   %d\n\n", activeJobs)

	targetVersion := "unknown"
	targetSchema := 0
	rollbackClass := "unknown"
	if targetManifest != nil {
		targetVersion = targetManifest.ReleaseVersion
		targetSchema = targetManifest.TargetSchema
		if p, pErr := targetManifest.FindUpgradePath(dbSchema); pErr == nil && p != nil {
			rollbackClass = string(p.RollbackClassification)
		} else if targetManifest.RollbackClassification != "" {
			rollbackClass = string(targetManifest.RollbackClassification)
		}
	} else if rel != nil {
		targetVersion = rel.Version
	}

	fmt.Fprintf(stdout, "Target\n")
	fmt.Fprintf(stdout, "------\n")
	fmt.Fprintf(stdout, "Version:           %s\n", targetVersion)
	if targetSchema > 0 {
		fmt.Fprintf(stdout, "Schema:            %d\n", targetSchema)
	} else {
		fmt.Fprintf(stdout, "Schema:            unknown\n")
	}
	if targetManifest != nil {
		fmt.Fprintf(stdout, "Managed metadata:  verified\n")
		fmt.Fprintf(stdout, "Rollback:          %s\n\n", strings.ReplaceAll(rollbackClass, "_", " "))
	} else {
		fmt.Fprintf(stdout, "Managed metadata:  missing or invalid\n\n")
	}

	fmt.Fprintf(stdout, "Planned later actions\n")
	fmt.Fprintf(stdout, "---------------------\n")
	fmt.Fprintf(stdout, "Database backup\n")
	fmt.Fprintf(stdout, "Pull + verify target images\n")
	fmt.Fprintf(stdout, "Update configured version\n")
	fmt.Fprintf(stdout, "Recreate application containers\n")
	fmt.Fprintf(stdout, "Verify health\n\n")

	// Print Outcome
	if len(blockedReasons) > 0 {
		fmt.Fprintf(stdout, "RESULT:\n")
		fmt.Fprintf(stdout, "BLOCKED\n")
		for _, reason := range blockedReasons {
			fmt.Fprintf(stdout, "  - [BLOCKED] %s\n", reason)
		}
		return 1
	}

	if len(warningReasons) > 0 {
		fmt.Fprintf(stdout, "RESULT:\n")
		fmt.Fprintf(stdout, "WARNING\n")
		for _, warning := range warningReasons {
			fmt.Fprintf(stdout, "  - [WARNING] %s\n", warning)
		}
		return 0
	}

	fmt.Fprintf(stdout, "RESULT:\n")
	fmt.Fprintf(stdout, "READY\n")
	return 0
}

// getEffectiveEnv looks up key with precedence: process environment > .env. Must be non-empty.
func getEffectiveEnv(key string, envVars map[string]string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	if envVars != nil {
		if val, ok := envVars[key]; ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func runBackup(ctx context.Context, stdout, stderr io.Writer, projectDir, explicitFile, targetEngine string, args []string, deps CLIDependencies) int {
	subFlags := flag.NewFlagSet("backup", flag.ContinueOnError)
	subFlags.SetOutput(stderr)

	var (
		subFile      string
		subEngine    string
		subBackupDir string
	)
	subFlags.StringVar(&subFile, "file", "", "compose file to use")
	subFlags.StringVar(&subFile, "f", "", "compose file to use (shorthand)")
	subFlags.StringVar(&subEngine, "engine", "", "container engine to use (docker or podman)")
	subFlags.StringVar(&subBackupDir, "backup-dir", "", "custom backup destination directory")

	if err := subFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if subFile != "" {
		explicitFile = subFile
	}
	if subEngine != "" {
		targetEngine = subEngine
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed resolving project directory %q: %v\n", projectDir, err)
		return 1
	}

	loadedCfg, _ := config.Load(absProjectDir)
	persistedFile := ""
	persistedEngine := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    absProjectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    false,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: compose resolution failed: %v\n", err)
		return 1
	}

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      absProjectDir,
		ComposeFile:     composeRes.SelectedFile,
		ExplicitEngine:  targetEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      false,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: engine resolution failed: %v\n", err)
		return 1
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(absProjectDir, ".env"))
	dbUser := getEffectiveEnv("POSTGRES_USER", envVars)
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := getEffectiveEnv("POSTGRES_DB", envVars)
	if dbName == "" {
		dbName = "ytmdl"
	}

	currentVersion := getEffectiveEnv("YTMDL_VERSION", envVars)
	if currentVersion == "" {
		currentVersion = "0.15.0"
	}

	backupDir := subBackupDir
	if backupDir != "" && !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(absProjectDir, backupDir)
	}

	res, err := backup.CreateBackup(ctx, eng, backup.BackupOptions{
		ProjectDir:     absProjectDir,
		ComposeFile:    composeRes.SelectedFile,
		BackupDir:      backupDir,
		CurrentVersion: currentVersion,
		DBUser:         dbUser,
		DBName:         dbName,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: backup failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Backup created: %s\n", res.RelativePath)
	fmt.Fprintf(stdout, "Size:           %s\n", formatFileSize(res.SizeBytes))
	fmt.Fprintf(stdout, "Validation:     PASS\n")
	return 0
}

func formatFileSize(bytes int64) string {
	if bytes >= 1024*1024 {
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1024*1024))
	} else if bytes >= 1024 {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%d bytes", bytes)
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func parsePlatformDigests(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	res := make(map[string]string)
	for _, entry := range flags {
		for _, part := range strings.Split(entry, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			idx := strings.Index(part, "=")
			if idx <= 0 || idx >= len(part)-1 {
				return nil, fmt.Errorf("invalid platform format %q, expected platform=digest (e.g. linux/amd64=sha256:...)", part)
			}
			plat := strings.TrimSpace(part[:idx])
			dig := strings.TrimSpace(part[idx+1:])
			normPlat, err := manifest.NormalizePlatform(plat)
			if err != nil {
				return nil, fmt.Errorf("invalid platform %q: %w", plat, err)
			}
			res[normPlat] = dig
		}
	}
	return res, nil
}

func runManifestGen(stdout, stderr io.Writer, args []string) int {
	manifestFlags := flag.NewFlagSet("manifest-gen", flag.ContinueOnError)
	manifestFlags.SetOutput(stdout)
	manifestFlags.Usage = func() {
		fmt.Fprintf(stdout, `Usage: ytmdlctl manifest-gen [flags]

Generate and validate release-manifest.json for a release.

Flags:
  --version <ver>                 Release version (e.g. 0.17.0)
  --tag <tag>                     Release git tag (e.g. v0.17.0, optional)
  --manifest-version <num>        Manifest schema version (1, 2, or 3, default: auto)
  --schema <num>                  Target database schema (default: 8)
  --update-classification <cls>   Update classification (schema_neutral or schema_forward)
  --classification <cls>          Rollback classification (schema_neutral or backup_restore_required)
  --supported-sources <schemas>   Comma-separated list of supported source schemas (e.g. 8)
  --min-upgrade <ver>             Minimum upgradeable version (default: 0.15.0)
  --backend-digest <d>            sha256 digest of pushed backend image (or index)
  --backend-platform <p=d>        Backend platform digest (e.g. linux/amd64=sha256:..., repeatable)
  --frontend-digest <d>           sha256 digest of pushed frontend image (or index)
  --frontend-platform <p=d>       Frontend platform digest (e.g. linux/amd64=sha256:..., repeatable)
  --required-env <keys>           Comma-separated list of required environment variables
  -o, --output <path>             Output file path (default: release-manifest.json, - for stdout)
`)
	}

	version := manifestFlags.String("version", "", "release version without leading 'v'")
	tag := manifestFlags.String("tag", "", "release git tag (optional)")
	manifestVer := manifestFlags.Int("manifest-version", 0, "manifest schema version (1, 2, or 3)")
	schema := manifestFlags.Int("schema", 8, "target database schema")
	updateClassification := manifestFlags.String("update-classification", "", "update classification (schema_neutral or schema_forward)")
	classification := manifestFlags.String("classification", "", "rollback classification (schema_neutral or backup_restore_required)")
	supportedSourcesStr := manifestFlags.String("supported-sources", "", "comma-separated list of supported source schemas")
	minUpgrade := manifestFlags.String("min-upgrade", "0.15.0", "minimum upgradeable version")
	backendDigest := manifestFlags.String("backend-digest", "", "sha256 digest of pushed backend image")
	frontendDigest := manifestFlags.String("frontend-digest", "", "sha256 digest of pushed frontend image")
	var backendPlatformsFlag stringSliceFlag
	manifestFlags.Var(&backendPlatformsFlag, "backend-platform", "backend platform digest mapping (platform=digest)")
	var frontendPlatformsFlag stringSliceFlag
	manifestFlags.Var(&frontendPlatformsFlag, "frontend-platform", "frontend platform digest mapping (platform=digest)")
	requiredEnv := manifestFlags.String("required-env", "", "comma-separated list of required environment variables")
	outputFile := manifestFlags.String("output", "release-manifest.json", "output file path")
	manifestFlags.StringVar(outputFile, "o", "release-manifest.json", "output file path (shorthand)")

	if err := manifestFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *version == "" {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: --version is required\n")
		return 2
	}
	if *backendDigest == "" {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: --backend-digest is required\n")
		return 2
	}
	if *frontendDigest == "" {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: --frontend-digest is required\n")
		return 2
	}

	backendPlatforms, err := parsePlatformDigests(backendPlatformsFlag)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: %v\n", err)
		return 2
	}
	frontendPlatforms, err := parsePlatformDigests(frontendPlatformsFlag)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: %v\n", err)
		return 2
	}

	var envList []string
	if *requiredEnv != "" {
		for _, e := range strings.Split(*requiredEnv, ",") {
			tr := strings.TrimSpace(e)
			if tr != "" {
				envList = append(envList, tr)
			}
		}
	}

	var supportedSources []int
	if *supportedSourcesStr != "" {
		for _, s := range strings.Split(*supportedSourcesStr, ",") {
			tr := strings.TrimSpace(s)
			if tr != "" {
				val, err := strconv.Atoi(tr)
				if err != nil {
					fmt.Fprintf(stderr, "ytmdlctl manifest-gen: invalid supported-sources value %q: %v\n", tr, err)
					return 2
				}
				supportedSources = append(supportedSources, val)
			}
		}
	}

	data, err := manifest.Generate(manifest.GeneratorOptions{
		ManifestVersion:        *manifestVer,
		ReleaseVersion:         *version,
		ReleaseTag:             *tag,
		TargetSchema:           *schema,
		UpdateClassification:   manifest.UpdateClassification(*updateClassification),
		RollbackClassification: manifest.RollbackClassification(*classification),
		SupportedSourceSchemas: supportedSources,
		MinUpgradeFrom:         *minUpgrade,
		BackendDigest:          *backendDigest,
		BackendPlatforms:       backendPlatforms,
		FrontendDigest:         *frontendDigest,
		FrontendPlatforms:      frontendPlatforms,
		RequiredEnv:            envList,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl manifest-gen: %v\n", err)
		return 1
	}

	if *outputFile == "-" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "ytmdlctl manifest-gen: write stdout failed: %v\n", err)
			return 1
		}
	} else {
		if err := os.WriteFile(*outputFile, data, 0644); err != nil {
			fmt.Fprintf(stderr, "ytmdlctl manifest-gen: write file failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Successfully generated and validated %s\n", *outputFile)
	}
	return 0
}

func runReconcileArtists(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, projectDir, explicitFile, targetEngine, baseURL string, args []string, deps CLIDependencies) int {
	recFlags := flag.NewFlagSet("reconcile-artists", flag.ContinueOnError)
	recFlags.SetOutput(stderr)

	var (
		subProjectDir    string
		subFile          string
		subEngine        string
		subBackupDir     string
		dryRun           bool
		apply            bool
		autoConfirm      bool
		autoConfirmShort bool
		verbose          bool
		verboseShort     bool
		dbURL            string
		subBaseURL       string
	)

	recFlags.StringVar(&subProjectDir, "project-dir", "", "project root directory")
	recFlags.StringVar(&subFile, "file", "", "compose file to use")
	recFlags.StringVar(&subFile, "f", "", "compose file to use (shorthand)")
	recFlags.StringVar(&subEngine, "engine", "", "container engine to use (docker or podman)")
	recFlags.StringVar(&subBaseURL, "base-url", "", "frontend base URL")
	recFlags.StringVar(&subBackupDir, "backup-dir", "", "custom backup destination directory (defaults to backups/)")
	recFlags.BoolVar(&dryRun, "dry-run", false, "perform read-only simulation without writing changes")
	recFlags.BoolVar(&apply, "apply", false, "execute proved duplicate reconciliation")
	recFlags.BoolVar(&autoConfirm, "yes", false, "automatically confirm prompt without asking")
	recFlags.BoolVar(&autoConfirmShort, "y", false, "automatically confirm prompt without asking (shorthand)")
	recFlags.BoolVar(&verbose, "verbose", false, "show detailed candidate artist information")
	recFlags.BoolVar(&verboseShort, "v", false, "show detailed candidate artist information (shorthand)")
	recFlags.StringVar(&dbURL, "db-url", "", "direct PostgreSQL connection URL (optional, bypasses container engine)")

	if err := recFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if dryRun && apply {
		fmt.Fprintf(stderr, "ytmdlctl: cannot specify both --dry-run and --apply\n")
		return 2
	}

	// Default to dry-run unless --apply is explicitly specified
	isDryRun := !apply || dryRun

	if subProjectDir != "" {
		projectDir = subProjectDir
	}
	if subFile != "" {
		explicitFile = subFile
	}
	if subEngine != "" {
		targetEngine = subEngine
	}
	if subBaseURL != "" {
		baseURL = subBaseURL
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed resolving project directory %q: %v\n", projectDir, err)
		return 1
	}

	// 1. Lock check
	if !isDryRun {
		fl, err := lock.Acquire(absProjectDir)
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: failed acquiring update lock: %v\n", err)
			return 1
		}
		defer fl.Release()
	} else {
		if locked, info, _ := lock.CheckContention(absProjectDir); locked && info != nil {
			fmt.Fprintf(stderr, "warning: update lock currently held by PID %d\n", info.PID)
		}
	}

	// 2. Reject if interrupted transaction
	st, _ := state.Load(absProjectDir)
	if st != nil && st.IsInterrupted() {
		fmt.Fprintf(stderr, "ytmdlctl: cannot reconcile: interrupted update transaction detected (%s); recovery required\n", st.Status)
		return 1
	}

	// 3. Resolve configuration and DB settings
	loadedCfg, _ := config.Load(absProjectDir)
	persistedFile := ""
	persistedEngine := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(absProjectDir, ".env"))
	dbUser := getEffectiveEnv("POSTGRES_USER", envVars)
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := getEffectiveEnv("POSTGRES_DB", envVars)
	if dbName == "" {
		dbName = "ytmdl"
	}
	currentVersion := getEffectiveEnv("YTMDL_VERSION", envVars)
	if currentVersion == "" {
		currentVersion = "0.19.3"
	}

	backupDir := subBackupDir
	if backupDir != "" && !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(absProjectDir, backupDir)
	}

	var exec reconcile.Executor
	var eng engine.Engine
	var composeFile string

	if dbURL != "" {
		if !isDryRun && !deps.AllowDirectDBMutate {
			fmt.Fprintf(stderr, "ytmdlctl: mutating maintenance via --db-url is not permitted; mutating operations require the managed container lifecycle (quiescent backend + validated backup)\n")
			return 1
		}
		// Direct PostgreSQL connection
		db, err := sql.Open("pgx", dbURL)
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: direct database connection failed: %v\n", err)
			return 1
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: database ping failed: %v\n", err)
			return 1
		}
		exec = &reconcile.SQLExecutor{DB: db}
	} else {
		// Container engine execution
		composeRes, err := compose.Resolve(compose.ResolveOptions{
			ProjectDir:    absProjectDir,
			ExplicitFile:  explicitFile,
			PersistedFile: persistedFile,
			IsMutating:    !isDryRun,
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: compose resolution failed: %v\n", err)
			return 1
		}
		composeFile = composeRes.SelectedFile

		eng, err = engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
			ProjectDir:      absProjectDir,
			ComposeFile:     composeFile,
			ExplicitEngine:  targetEngine,
			PersistedEngine: persistedEngine,
			IsMutating:      !isDryRun,
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: engine resolution failed: %v\n", err)
			return 1
		}

		// Concurrency guard: reject mutating operation if background jobs are actively running
		if !isDryRun {
			qStatus, qErr := discovery.QueryQueueStatus(ctx, eng, absProjectDir, composeFile, dbUser, dbName)
			if qErr == nil && qStatus != nil && qStatus.ActiveJobs > 0 {
				fmt.Fprintf(stderr, "ytmdlctl: cannot reconcile: %d active jobs currently running in background queue; please wait for queue to finish or pause queue\n", qStatus.ActiveJobs)
				return 1
			}
		}

		exec = &reconcile.EngineExecutor{
			Engine:      eng,
			ProjectDir:  absProjectDir,
			ComposeFile: composeFile,
			User:        dbUser,
			Database:    dbName,
		}
	}

	// 4. Confirmation (mutating mode only)
	confirm := autoConfirm || autoConfirmShort
	if !isDryRun && !confirm {
		fmt.Fprintf(stdout, "WARNING: This will merge proved duplicate artist entities in the YTMDL library.\n")
		fmt.Fprintf(stdout, "A validated database backup will be created before any modifications are applied.\n\n")
		fmt.Fprintf(stdout, "Are you sure you want to proceed? [y/N]: ")

		var response string
		if stdin != nil {
			fmt.Fscanln(stdin, &response)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Fprintf(stdout, "Reconciliation cancelled by user (0 modifications made).\n")
			return 0
		}
		fmt.Fprintf(stdout, "\n")
	}

	// 5. Quiescent Backend lifecycle (mutating mode with container engine)
	var backendStopped bool
	if !isDryRun && eng != nil {
		fmt.Fprintf(stdout, "Stopping backend service to ensure database quiescence...\n")
		stopRes, stopErr := eng.StopServices(ctx, absProjectDir, composeFile, "backend")
		if stopErr != nil || (stopRes != nil && stopRes.ExitCode != 0) {
			var errMsg string
			if stopErr != nil {
				errMsg = stopErr.Error()
			} else {
				errMsg = strings.TrimSpace(string(stopRes.Stderr))
			}
			fmt.Fprintf(stderr, "ytmdlctl: failed stopping backend service: %s\n", errMsg)
			return 1
		}
		backendStopped = true
		defer func() {
			if backendStopped {
				fmt.Fprintf(stdout, "Restoring backend service...\n")
				upEnv := map[string]string{"YTMDL_VERSION": currentVersion}
				_, _ = eng.UpServices(context.Background(), absProjectDir, composeFile, upEnv, "backend")
			}
		}()
	}

	// 6. Pre-mutation backup (mutating mode only)
	var backupRes *backup.BackupResult
	if !isDryRun && eng != nil {
		fmt.Fprintf(stdout, "Creating pre-reconciliation database backup...\n")
		bRes, err := backup.CreateBackup(ctx, eng, backup.BackupOptions{
			ProjectDir:     absProjectDir,
			ComposeFile:    composeFile,
			BackupDir:      backupDir,
			CurrentVersion: currentVersion,
			TargetVersion:  "reconcile",
			DBUser:         dbUser,
			DBName:         dbName,
			SkipLock:       true, // lock already held above
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: PRE-RECONCILIATION BACKUP FAILED: %v (0 database writes performed)\n", err)
			return 1
		}
		backupRes = bRes
		fmt.Fprintf(stdout, "Backup created: %s (%s, PASS)\n\n", backupRes.RelativePath, formatFileSize(backupRes.SizeBytes))
	}

	// 7. Execute reconciliation
	isVerbose := verbose || verboseShort
	report, err := reconcile.Run(ctx, exec, reconcile.Options{
		ProjectDir:     absProjectDir,
		ComposeFile:    composeFile,
		BackupDir:      backupDir,
		CurrentVersion: currentVersion,
		DBUser:         dbUser,
		DBName:         dbName,
		DryRun:         isDryRun,
		Apply:          !isDryRun,
		Verbose:        isVerbose,
		Stdout:         stdout,
		Stderr:         stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: reconciliation failed: %v\n", err)
		return 1
	}

	// 8. Restart backend service (if stopped)
	if backendStopped {
		fmt.Fprintf(stdout, "Restarting backend service...\n")
		upEnv := map[string]string{"YTMDL_VERSION": currentVersion}
		upRes, upErr := eng.UpServices(ctx, absProjectDir, composeFile, upEnv, "backend")
		if upErr != nil || (upRes != nil && upRes.ExitCode != 0) {
			var errMsg string
			if upErr != nil {
				errMsg = upErr.Error()
			} else {
				errMsg = strings.TrimSpace(string(upRes.Stderr))
			}
			fmt.Fprintf(stderr, "warning: failed restarting backend service: %s\n", errMsg)
		} else {
			backendStopped = false
			fmt.Fprintf(stdout, "Backend service restarted successfully.\n")

			// Health verification
			persistedBaseURL := ""
			if loadedCfg != nil {
				persistedBaseURL = loadedCfg.BaseURL
			}
			resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
				ExplicitURL:  baseURL,
				PersistedURL: persistedBaseURL,
				Engine:       eng,
				ProjectDir:   absProjectDir,
				ComposeFile:  composeFile,
				EnvVars:      envVars,
			})
			if resolvedURL != "" {
				hc := discovery.NewHealthClient(resolvedURL, "reconcile", deps.HTTPClient)
				deadline := time.Now().Add(15 * time.Second)
				var lastHealthErr error
				var h *discovery.BackendHealth
				for time.Now().Before(deadline) {
					if ctx.Err() != nil {
						break
					}
					h, lastHealthErr = hc.Check(ctx)
					if lastHealthErr == nil && h != nil && (h.Status == "ok" || h.DatabaseHealthy) {
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				if h != nil && (h.Status == "ok" || h.DatabaseHealthy) {
					fmt.Fprintf(stdout, "Backend health verification: PASS (status: %s)\n\n", h.Status)
				} else if lastHealthErr != nil {
					fmt.Fprintf(stderr, "warning: backend health check unverified: %v\n\n", lastHealthErr)
				}
			}
		}
	}

	// 9. Structured Report Output
	fmt.Fprintf(stdout, "YTMDL ARTIST RECONCILIATION REPORT\n")
	fmt.Fprintf(stdout, "==================================\n")
	if isDryRun {
		fmt.Fprintf(stdout, "Execution mode:       DRY RUN (read-only preview, 0 writes)\n")
	} else {
		fmt.Fprintf(stdout, "Execution mode:       MUTATING (applied to database)\n")
	}
	fmt.Fprintf(stdout, "Artists scanned:      %d\n", report.ClustersExamined)
	fmt.Fprintf(stdout, "Proved duplicate:     %d clusters (%d duplicate rows)\n", report.ProvedClusters, report.ProvedDups)
	fmt.Fprintf(stdout, "Ambiguous clusters:   %d clusters (%d rows preserved untouched)\n\n", report.AmbiguousClusters, report.AmbiguousDups)

	if isDryRun {
		fmt.Fprintf(stdout, "Planned merges\n")
		fmt.Fprintf(stdout, "--------------\n")
		fmt.Fprintf(stdout, "Merged clusters:      %d\n", report.ProvedClusters)
		fmt.Fprintf(stdout, "Duplicate rows:       %d to be deleted\n", report.ProvedDups)
		fmt.Fprintf(stdout, "Releases:             %d to be repointed\n", report.ReassignedReleases)
		fmt.Fprintf(stdout, "Tracks:               %d to be repointed\n\n", report.ReassignedTracks)

		fmt.Fprintf(stdout, "RESULT:\n")
		fmt.Fprintf(stdout, "PREVIEW COMPLETE\n")
		fmt.Fprintf(stdout, "[DRY RUN] No modifications made. Pass --apply (with -y to auto-confirm) to execute reconciliation.\n")
	} else {
		fmt.Fprintf(stdout, "Reconciliation Results\n")
		fmt.Fprintf(stdout, "----------------------\n")
		fmt.Fprintf(stdout, "Merged clusters:      %d\n", report.MergedGroups)
		fmt.Fprintf(stdout, "Deleted duplicate rows: %d\n", report.MergedRows)
		fmt.Fprintf(stdout, "Reassigned releases:  %d\n", report.ReassignedReleases)
		fmt.Fprintf(stdout, "Reassigned tracks:    %d\n", report.ReassignedTracks)
		if backupRes != nil {
			fmt.Fprintf(stdout, "Preserved backup:     %s (%s)\n", backupRes.RelativePath, formatFileSize(backupRes.SizeBytes))
		}
		fmt.Fprintf(stdout, "Post-integrity check: PASS (0 dangling references, 0 proved duplicates remaining)\n\n")

		fmt.Fprintf(stdout, "RESULT:\n")
		fmt.Fprintf(stdout, "SUCCESS\n")
	}

	if isVerbose {
		if len(report.ProvedDetails) > 0 {
			fmt.Fprintf(stdout, "\nProved Duplicate Details:\n")
			for i, pg := range report.ProvedDetails {
				fmt.Fprintf(stdout, "  [%d] %q (%s)\n", i+1, pg.Winner.Name, pg.Provider)
				fmt.Fprintf(stdout, "      Canonical Winner: %s (source: %s, sub: %v, items: %d)\n", pg.Winner.ID, pg.Winner.SourceID, pg.Winner.HasSub, pg.Winner.TotalItems())
				for _, d := range pg.Duplicates {
					fmt.Fprintf(stdout, "      -> Duplicate:     %s (source: %s, items: %d)\n", d.ID, d.SourceID, d.TotalItems())
				}
			}
		}
		if len(report.AmbiguousDetails) > 0 {
			fmt.Fprintf(stdout, "\nAmbiguous Cluster Details (preserved):\n")
			for i, ag := range report.AmbiguousDetails {
				fmt.Fprintf(stdout, "  [%d] %q\n", i+1, ag.ClusterName)
				fmt.Fprintf(stdout, "      Reason: %s\n", ag.Reason)
				for _, c := range ag.Candidates {
					fmt.Fprintf(stdout, "      - %s (provider: %s, source: %s, items: %d)\n", c.ID, c.Provider, c.SourceID, c.TotalItems())
				}
			}
		}
	}

	return 0
}

func runMergeArtists(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, projectDir, explicitFile, targetEngine, baseURL string, args []string, deps CLIDependencies) int {
	mergeFlags := flag.NewFlagSet("merge-artists", flag.ContinueOnError)
	mergeFlags.SetOutput(stderr)

	var (
		subProjectDir    string
		subFile          string
		subEngine        string
		subBackupDir     string
		dryRun           bool
		apply            bool
		autoConfirm      bool
		autoConfirmShort bool
		verbose          bool
		verboseShort     bool
		dbURL            string
		subBaseURL       string
	)

	mergeFlags.StringVar(&subProjectDir, "project-dir", "", "project root directory")
	mergeFlags.StringVar(&subFile, "file", "", "compose file to use")
	mergeFlags.StringVar(&subFile, "f", "", "compose file to use (shorthand)")
	mergeFlags.StringVar(&subEngine, "engine", "", "container engine to use (docker or podman)")
	mergeFlags.StringVar(&subBaseURL, "base-url", "", "frontend base URL")
	mergeFlags.StringVar(&subBackupDir, "backup-dir", "", "custom backup destination directory (defaults to backups/)")
	mergeFlags.BoolVar(&dryRun, "dry-run", false, "perform read-only simulation without writing changes")
	mergeFlags.BoolVar(&apply, "apply", false, "execute manual artist merge")
	mergeFlags.BoolVar(&autoConfirm, "yes", false, "automatically confirm prompt without asking")
	mergeFlags.BoolVar(&autoConfirmShort, "y", false, "automatically confirm prompt without asking (shorthand)")
	mergeFlags.BoolVar(&verbose, "verbose", false, "show detailed artist information")
	mergeFlags.BoolVar(&verboseShort, "v", false, "show detailed artist information (shorthand)")
	mergeFlags.StringVar(&dbURL, "db-url", "", "direct PostgreSQL connection URL (optional, bypasses container engine)")

	if err := mergeFlags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	remaining := mergeFlags.Args()
	if len(remaining) < 2 {
		fmt.Fprintf(stderr, "ytmdlctl merge-artists: requires at least 2 artist IDs: <canonical-id> <duplicate-id> [duplicate-ids...]\n")
		fmt.Fprintf(stderr, "Usage: ytmdlctl merge-artists [flags] <canonical-id> <duplicate-id> [duplicate-ids...]\n")
		return 2
	}

	if dryRun && apply {
		fmt.Fprintf(stderr, "ytmdlctl: cannot specify both --dry-run and --apply\n")
		return 2
	}

	isDryRun := !apply || dryRun

	canonicalID := strings.TrimSpace(remaining[0])
	rawDupIDs := remaining[1:]

	// Validate duplicate IDs
	seenDups := make(map[string]struct{})
	var dupIDs []string
	for _, raw := range rawDupIDs {
		d := strings.TrimSpace(raw)
		if d == "" {
			continue
		}
		if d == canonicalID {
			fmt.Fprintf(stderr, "ytmdlctl merge-artists: cannot merge canonical artist %s into itself\n", canonicalID)
			return 2
		}
		if _, exists := seenDups[d]; !exists {
			seenDups[d] = struct{}{}
			dupIDs = append(dupIDs, d)
		}
	}

	if len(dupIDs) == 0 {
		fmt.Fprintf(stderr, "ytmdlctl merge-artists: at least one unique duplicate artist ID is required\n")
		return 2
	}

	if subProjectDir != "" {
		projectDir = subProjectDir
	}
	if subFile != "" {
		explicitFile = subFile
	}
	if subEngine != "" {
		targetEngine = subEngine
	}
	if subBaseURL != "" {
		baseURL = subBaseURL
	}

	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: failed resolving project directory %q: %v\n", projectDir, err)
		return 1
	}

	// 1. Lock check
	if !isDryRun {
		fl, err := lock.Acquire(absProjectDir)
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: failed acquiring update lock: %v\n", err)
			return 1
		}
		defer fl.Release()
	} else {
		if locked, info, _ := lock.CheckContention(absProjectDir); locked && info != nil {
			fmt.Fprintf(stderr, "warning: update lock currently held by PID %d\n", info.PID)
		}
	}

	// 2. Reject if interrupted transaction
	st, _ := state.Load(absProjectDir)
	if st != nil && st.IsInterrupted() {
		fmt.Fprintf(stderr, "ytmdlctl: cannot merge: interrupted update transaction detected (%s); recovery required\n", st.Status)
		return 1
	}

	// 3. Resolve configuration and DB settings
	loadedCfg, _ := config.Load(absProjectDir)
	persistedFile := ""
	persistedEngine := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(absProjectDir, ".env"))
	dbUser := getEffectiveEnv("POSTGRES_USER", envVars)
	if dbUser == "" {
		dbUser = "ytmdl"
	}
	dbName := getEffectiveEnv("POSTGRES_DB", envVars)
	if dbName == "" {
		dbName = "ytmdl"
	}
	currentVersion := getEffectiveEnv("YTMDL_VERSION", envVars)
	if currentVersion == "" {
		currentVersion = "0.19.3"
	}

	backupDir := subBackupDir
	if backupDir != "" && !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(absProjectDir, backupDir)
	}

	var exec reconcile.Executor
	var eng engine.Engine
	var composeFile string

	if dbURL != "" {
		if !isDryRun && !deps.AllowDirectDBMutate {
			fmt.Fprintf(stderr, "ytmdlctl: mutating maintenance via --db-url is not permitted; mutating operations require the managed container lifecycle (quiescent backend + validated backup)\n")
			return 1
		}
		db, err := sql.Open("pgx", dbURL)
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: direct database connection failed: %v\n", err)
			return 1
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: database ping failed: %v\n", err)
			return 1
		}
		exec = &reconcile.SQLExecutor{DB: db}
	} else {
		composeRes, err := compose.Resolve(compose.ResolveOptions{
			ProjectDir:    absProjectDir,
			ExplicitFile:  explicitFile,
			PersistedFile: persistedFile,
			IsMutating:    !isDryRun,
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: compose resolution failed: %v\n", err)
			return 1
		}
		composeFile = composeRes.SelectedFile

		eng, err = engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
			ProjectDir:      absProjectDir,
			ComposeFile:     composeFile,
			ExplicitEngine:  targetEngine,
			PersistedEngine: persistedEngine,
			IsMutating:      !isDryRun,
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: engine resolution failed: %v\n", err)
			return 1
		}

		if !isDryRun {
			qStatus, qErr := discovery.QueryQueueStatus(ctx, eng, absProjectDir, composeFile, dbUser, dbName)
			if qErr == nil && qStatus != nil && qStatus.ActiveJobs > 0 {
				fmt.Fprintf(stderr, "ytmdlctl: cannot merge: %d active jobs currently running in background queue; please wait for queue to finish or pause queue\n", qStatus.ActiveJobs)
				return 1
			}
		}

		exec = &reconcile.EngineExecutor{
			Engine:      eng,
			ProjectDir:  absProjectDir,
			ComposeFile: composeFile,
			User:        dbUser,
			Database:    dbName,
		}
	}

	// 4. Query target artists to validate existence and safety
	allIDs := append([]string{canonicalID}, dupIDs...)
	cands, err := exec.GetArtists(ctx, allIDs)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl merge-artists: failed querying artists: %v\n", err)
		return 1
	}

	candMap := make(map[string]reconcile.Candidate)
	for _, c := range cands {
		candMap[c.ID] = c
	}

	canonicalCand, ok := candMap[canonicalID]
	if !ok {
		fmt.Fprintf(stderr, "ytmdlctl merge-artists: canonical artist %s not found in database\n", canonicalID)
		return 1
	}

	var duplicateCands []reconcile.Candidate
	for _, dID := range dupIDs {
		dCand, ok := candMap[dID]
		if !ok {
			fmt.Fprintf(stderr, "ytmdlctl merge-artists: duplicate artist %s not found in database\n", dID)
			return 1
		}
		duplicateCands = append(duplicateCands, dCand)
	}

	// Safety check 1: duplicate vs canonical distinct real IDs on same provider
	for _, d := range duplicateCands {
		if !canonicalCand.IsSynthetic() && !d.IsSynthetic() && canonicalCand.Provider == d.Provider && canonicalCand.SourceID != d.SourceID {
			fmt.Fprintf(stderr, "ytmdlctl merge-artists: safety rejection: cannot merge duplicate %s (%s:%s) into canonical %s (%s:%s); distinct real IDs on the same provider represent separate catalog entities\n", d.ID, d.Provider, d.SourceID, canonicalCand.ID, canonicalCand.Provider, canonicalCand.SourceID)
			return 1
		}
	}

	// Safety check 2: duplicate vs duplicate distinct real IDs on same provider
	provRealMap := make(map[string]map[string]string)
	for _, d := range duplicateCands {
		if !d.IsSynthetic() {
			if provRealMap[d.Provider] == nil {
				provRealMap[d.Provider] = make(map[string]string)
			}
			for prevSource, prevID := range provRealMap[d.Provider] {
				if prevSource != d.SourceID {
					fmt.Fprintf(stderr, "ytmdlctl merge-artists: safety rejection: duplicate %s (%s:%s) and %s (%s:%s) have distinct real IDs on provider %s\n", d.ID, d.Provider, d.SourceID, prevID, d.Provider, prevSource, d.Provider)
					return 1
				}
			}
			provRealMap[d.Provider][d.SourceID] = d.ID
		}
	}

	// Calculate counts and best artwork
	plannedRel := 0
	plannedTrk := 0
	bestImage := strings.TrimSpace(canonicalCand.ImageURL)
	for _, d := range duplicateCands {
		plannedRel += d.ReleaseCount
		plannedTrk += d.TrackCount
		if bestImage == "" && strings.TrimSpace(d.ImageURL) != "" {
			bestImage = strings.TrimSpace(d.ImageURL)
		}
	}

	// 5. Dry-run preview
	if isDryRun {
		fmt.Fprintf(stdout, "YTMDL ARTIST MANUAL MERGE REPORT\n")
		fmt.Fprintf(stdout, "================================\n")
		fmt.Fprintf(stdout, "Execution mode:       DRY RUN (read-only preview, 0 writes)\n")
		fmt.Fprintf(stdout, "Canonical Artist:     %s (%q, provider: %s, source: %s, items: %d)\n", canonicalCand.ID, canonicalCand.Name, canonicalCand.Provider, canonicalCand.SourceID, canonicalCand.TotalItems())
		fmt.Fprintf(stdout, "Duplicate Artists to merge (%d):\n", len(duplicateCands))
		for _, d := range duplicateCands {
			fmt.Fprintf(stdout, "  - %s (%q, provider: %s, source: %s, items: %d)\n", d.ID, d.Name, d.Provider, d.SourceID, d.TotalItems())
		}
		fmt.Fprintf(stdout, "\nPlanned moves:\n")
		fmt.Fprintf(stdout, "  Releases:           %d to be repointed\n", plannedRel)
		fmt.Fprintf(stdout, "  Tracks:             %d to be repointed\n\n", plannedTrk)
		fmt.Fprintf(stdout, "RESULT:\n")
		fmt.Fprintf(stdout, "PREVIEW COMPLETE\n")
		fmt.Fprintf(stdout, "[DRY RUN] No modifications made. Pass --apply (with -y to auto-confirm) to execute merge.\n")
		return 0
	}

	// 6. Confirmation (mutating mode only)
	confirm := autoConfirm || autoConfirmShort
	if !confirm {
		fmt.Fprintf(stdout, "WARNING: This will permanently merge %d duplicate artist entities into canonical artist %s (%q).\n", len(duplicateCands), canonicalCand.ID, canonicalCand.Name)
		fmt.Fprintf(stdout, "A validated database backup will be created before any modifications are applied.\n\n")
		fmt.Fprintf(stdout, "Are you sure you want to proceed? [y/N]: ")

		var response string
		if stdin != nil {
			fmt.Fscanln(stdin, &response)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Fprintf(stdout, "Merge cancelled by user (0 modifications made).\n")
			return 0
		}
		fmt.Fprintf(stdout, "\n")
	}

	// 7. Quiescent Backend lifecycle (mutating mode with container engine)
	var backendStopped bool
	if eng != nil {
		fmt.Fprintf(stdout, "Stopping backend service to ensure database quiescence...\n")
		stopRes, stopErr := eng.StopServices(ctx, absProjectDir, composeFile, "backend")
		if stopErr != nil || (stopRes != nil && stopRes.ExitCode != 0) {
			var errMsg string
			if stopErr != nil {
				errMsg = stopErr.Error()
			} else {
				errMsg = strings.TrimSpace(string(stopRes.Stderr))
			}
			fmt.Fprintf(stderr, "ytmdlctl: failed stopping backend service: %s\n", errMsg)
			return 1
		}
		backendStopped = true
		defer func() {
			if backendStopped {
				fmt.Fprintf(stdout, "Restoring backend service...\n")
				upEnv := map[string]string{"YTMDL_VERSION": currentVersion}
				_, _ = eng.UpServices(context.Background(), absProjectDir, composeFile, upEnv, "backend")
			}
		}()
	}

	// 8. Pre-mutation backup
	var backupRes *backup.BackupResult
	if eng != nil {
		fmt.Fprintf(stdout, "Creating pre-merge database backup...\n")
		bRes, err := backup.CreateBackup(ctx, eng, backup.BackupOptions{
			ProjectDir:     absProjectDir,
			ComposeFile:    composeFile,
			BackupDir:      backupDir,
			CurrentVersion: currentVersion,
			TargetVersion:  "merge",
			DBUser:         dbUser,
			DBName:         dbName,
			SkipLock:       true, // lock already held above
		})
		if err != nil {
			fmt.Fprintf(stderr, "ytmdlctl: PRE-MERGE BACKUP FAILED: %v (0 database writes performed)\n", err)
			return 1
		}
		backupRes = bRes
		fmt.Fprintf(stdout, "Backup created: %s (%s, PASS)\n\n", backupRes.RelativePath, formatFileSize(backupRes.SizeBytes))
	}

	// 9. Execute manual merge
	relMoved, trkMoved, err := exec.MergeGroup(ctx, canonicalCand.ID, dupIDs, bestImage)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: merge failed: %v\n", err)
		return 1
	}

	// 10. Integrity check
	if err := exec.VerifyIntegrity(ctx); err != nil {
		fmt.Fprintf(stderr, "ytmdlctl: post-merge integrity check failed: %v\n", err)
		return 1
	}

	// 11. Restart backend service
	if backendStopped {
		fmt.Fprintf(stdout, "Restarting backend service...\n")
		upEnv := map[string]string{"YTMDL_VERSION": currentVersion}
		upRes, upErr := eng.UpServices(ctx, absProjectDir, composeFile, upEnv, "backend")
		if upErr != nil || (upRes != nil && upRes.ExitCode != 0) {
			var errMsg string
			if upErr != nil {
				errMsg = upErr.Error()
			} else {
				errMsg = strings.TrimSpace(string(upRes.Stderr))
			}
			fmt.Fprintf(stderr, "warning: failed restarting backend service: %s\n", errMsg)
		} else {
			backendStopped = false
			fmt.Fprintf(stdout, "Backend service restarted successfully.\n")

			persistedBaseURL := ""
			if loadedCfg != nil {
				persistedBaseURL = loadedCfg.BaseURL
			}
			resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
				ExplicitURL:  baseURL,
				PersistedURL: persistedBaseURL,
				Engine:       eng,
				ProjectDir:   absProjectDir,
				ComposeFile:  composeFile,
				EnvVars:      envVars,
			})
			if resolvedURL != "" {
				hc := discovery.NewHealthClient(resolvedURL, "merge", deps.HTTPClient)
				deadline := time.Now().Add(15 * time.Second)
				var lastHealthErr error
				var h *discovery.BackendHealth
				for time.Now().Before(deadline) {
					if ctx.Err() != nil {
						break
					}
					h, lastHealthErr = hc.Check(ctx)
					if lastHealthErr == nil && h != nil && (h.Status == "ok" || h.DatabaseHealthy) {
						break
					}
					time.Sleep(500 * time.Millisecond)
				}
				if h != nil && (h.Status == "ok" || h.DatabaseHealthy) {
					fmt.Fprintf(stdout, "Backend health verification: PASS (status: %s)\n\n", h.Status)
				} else if lastHealthErr != nil {
					fmt.Fprintf(stderr, "warning: backend health check unverified: %v\n\n", lastHealthErr)
				}
			}
		}
	}

	// 12. Structured Report Output
	fmt.Fprintf(stdout, "YTMDL ARTIST MANUAL MERGE REPORT\n")
	fmt.Fprintf(stdout, "================================\n")
	if isDryRun {
		fmt.Fprintf(stdout, "Execution mode:       DRY RUN (read-only preview, 0 writes)\n")
	} else {
		fmt.Fprintf(stdout, "Execution mode:       MUTATING (applied to database)\n")
	}
	fmt.Fprintf(stdout, "Canonical Artist:     %s (%q)\n", canonicalCand.ID, canonicalCand.Name)
	fmt.Fprintf(stdout, "Merged Duplicates:    %d\n", len(dupIDs))
	fmt.Fprintf(stdout, "Reassigned Releases:  %d\n", relMoved)
	fmt.Fprintf(stdout, "Reassigned Tracks:    %d\n", trkMoved)
	if backupRes != nil {
		fmt.Fprintf(stdout, "Preserved Backup:     %s (%s)\n", backupRes.RelativePath, formatFileSize(backupRes.SizeBytes))
	}
	fmt.Fprintf(stdout, "Post-integrity check: PASS (0 dangling references)\n\n")
	fmt.Fprintf(stdout, "RESULT:\n")
	fmt.Fprintf(stdout, "SUCCESS\n")

	return 0
}

func runRecover(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, projDir, explicitFile, explicitEngine, cliBaseURL string, args []string, deps CLIDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		var subArgs []string
		if len(args) > 0 {
			subArgs = args[1:]
		}
		return runRecoverStatus(ctx, stdout, stderr, projDir, explicitFile, explicitEngine, cliBaseURL, subArgs, deps)
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "status":
		return runRecoverStatus(ctx, stdout, stderr, projDir, explicitFile, explicitEngine, cliBaseURL, subArgs, deps)
	case "resume":
		return runRecoverResume(ctx, stdout, stderr, stdin, projDir, explicitFile, explicitEngine, cliBaseURL, subArgs, deps)
	case "restore":
		return runRecoverRestore(ctx, stdout, stderr, stdin, projDir, explicitFile, explicitEngine, cliBaseURL, subArgs, deps)
	case "help", "-h", "--help":
		printRecoverUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "ytmdlctl recover: unknown action %q. Supported: 'status', 'resume', 'restore'\n", subcommand)
		return 2
	}
}

func printRecoverUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: ytmdlctl recover <action> [flags]

Manage recovery operations for failed or interrupted schema migrations.

Actions:
  status   Inspect current deployment state, schema, backups, and suggested action
  resume   Complete target version deployment when DB schema was successfully migrated
  restore  Destructively restore Schema 8 database from pre-migration backup

Flags for 'resume' and 'restore':
  -y, --yes    Automatically confirm prompt without asking
`)
}

func runRecoverStatus(ctx context.Context, stdout, stderr io.Writer, projDir, explicitFile, explicitEngine, cliBaseURL string, args []string, deps CLIDependencies) int {
	projectDir, err := filepath.Abs(projDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover: failed resolving project directory: %v\n", err)
		return 1
	}

	loadedCfg, _ := config.Load(projectDir)
	persistedFile := ""
	persistedEngine := ""
	persistedURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedURL = loadedCfg.BaseURL
	}

	composeRes, _ := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    false,
	})
	composeFile := ""
	if composeRes != nil {
		composeFile = composeRes.SelectedFile
	}

	var eng engine.Engine
	if composeFile != "" {
		eng, _ = engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
			ProjectDir:      projectDir,
			ComposeFile:     composeFile,
			ExplicitEngine:  explicitEngine,
			PersistedEngine: persistedEngine,
			IsMutating:      false,
		})
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  cliBaseURL,
		PersistedURL: persistedURL,
		Engine:       eng,
		ProjectDir:   projectDir,
		ComposeFile:  composeFile,
		EnvVars:      envVars,
	})

	info, err := recovery.Status(ctx, eng, projectDir, composeFile, resolvedURL)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover status: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "YTMDL RECOVERY STATUS\n")
	fmt.Fprintf(stdout, "=====================\n")
	fmt.Fprintf(stdout, "State status:            %s\n", info.StateStatus)
	if info.OperationID != "" {
		fmt.Fprintf(stdout, "Operation ID:            %s\n", info.OperationID)
	}
	if info.CurrentVersion != "" {
		fmt.Fprintf(stdout, "Current version:         %s\n", info.CurrentVersion)
	}
	if info.TargetVersion != "" {
		fmt.Fprintf(stdout, "Target version:          %s\n", info.TargetVersion)
	}
	if info.SchemaBefore > 0 {
		fmt.Fprintf(stdout, "Schema before:           %d\n", info.SchemaBefore)
	}
	if info.TargetSchema > 0 {
		fmt.Fprintf(stdout, "Target schema:           %d\n", info.TargetSchema)
	}
	if info.ActualSchema > 0 {
		fmt.Fprintf(stdout, "Actual database schema:  %d\n", info.ActualSchema)
	} else {
		fmt.Fprintf(stdout, "Actual database schema:  unknown\n")
	}

	if info.BackupPath != "" {
		existsStr := "MISSING"
		if info.BackupExists {
			existsStr = fmt.Sprintf("EXISTS (%s)", formatFileSize(info.BackupSizeBytes))
		}
		fmt.Fprintf(stdout, "Pre-upgrade backup:      %s [%s]\n", info.BackupPath, existsStr)
	}
	if info.RecoverySafetyBackupPath != "" {
		fmt.Fprintf(stdout, "Recovery safety backup:  %s\n", info.RecoverySafetyBackupPath)
	}
	if info.QuarantineDBName != "" {
		fmt.Fprintf(stdout, "Quarantined database:    %s\n", info.QuarantineDBName)
	}

	backendStr := "stopped"
	if info.BackendContainerRunning {
		backendStr = "running"
	}
	fmt.Fprintf(stdout, "Backend container:       %s\n", backendStr)

	prevImgStr := "not found"
	if info.PreviousImagesAvailable {
		prevImgStr = "available"
	}
	fmt.Fprintf(stdout, "Previous images:         %s\n", prevImgStr)

	if info.LastError != "" {
		fmt.Fprintf(stdout, "Last recorded error:     %s\n", info.LastError)
	}

	fmt.Fprintf(stdout, "\nSuggested Action:\n")
	fmt.Fprintf(stdout, "  %s\n", info.SuggestedAction)

	return 0
}

func runRecoverResume(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, projDir, explicitFile, explicitEngine, cliBaseURL string, args []string, deps CLIDependencies) int {
	flags := flag.NewFlagSet("recover resume", flag.ContinueOnError)
	flags.SetOutput(stdout)
	autoConfirm := flags.Bool("yes", false, "automatically confirm prompt")
	autoConfirmShort := flags.Bool("y", false, "automatically confirm prompt")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	projectDir, err := filepath.Abs(projDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover resume: failed resolving project directory: %v\n", err)
		return 1
	}

	fl, err := lock.Acquire(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover resume: failed acquiring lock: %v\n", err)
		return 1
	}
	defer fl.Release()

	loadedCfg, _ := config.Load(projectDir)
	persistedFile := ""
	persistedEngine := ""
	persistedURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedURL = loadedCfg.BaseURL
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover resume: %v\n", err)
		return 1
	}
	composeFile := composeRes.SelectedFile

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projectDir,
		ComposeFile:     composeFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover resume: %v\n", err)
		return 1
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  cliBaseURL,
		PersistedURL: persistedURL,
		Engine:       eng,
		ProjectDir:   projectDir,
		ComposeFile:  composeFile,
		EnvVars:      envVars,
	})

	confirm := *autoConfirm || *autoConfirmShort
	res, err := recovery.Resume(ctx, eng, recovery.ResumeOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		BaseURL:     resolvedURL,
		AutoConfirm: confirm,
		Stdout:      stdout,
		Stderr:      stderr,
		Stdin:       stdin,
	})
	if err != nil {
		if errors.Is(err, recovery.ErrCancelled) {
			fmt.Fprintf(stdout, "Operation cancelled by operator.\n")
			return 0
		}
		fmt.Fprintf(stderr, "ytmdlctl recover resume: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Successfully resumed deployment of version %s (schema %d)\n", res.TargetVersion, res.TargetSchema)
	return 0
}

func runRecoverRestore(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, projDir, explicitFile, explicitEngine, cliBaseURL string, args []string, deps CLIDependencies) int {
	flags := flag.NewFlagSet("recover restore", flag.ContinueOnError)
	flags.SetOutput(stdout)
	autoConfirm := flags.Bool("yes", false, "automatically confirm destructive restore prompt")
	autoConfirmShort := flags.Bool("y", false, "automatically confirm destructive restore prompt")
	backupDir := flags.String("backup-dir", "", "backup directory (defaults to backups/)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	projectDir, err := filepath.Abs(projDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover restore: failed resolving project directory: %v\n", err)
		return 1
	}

	fl, err := lock.Acquire(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover restore: failed acquiring lock: %v\n", err)
		return 1
	}
	defer fl.Release()

	loadedCfg, _ := config.Load(projectDir)
	persistedFile := ""
	persistedEngine := ""
	persistedURL := ""
	if loadedCfg != nil {
		persistedFile = loadedCfg.ComposeFile
		persistedEngine = loadedCfg.Engine
		persistedURL = loadedCfg.BaseURL
	}

	composeRes, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    projectDir,
		ExplicitFile:  explicitFile,
		PersistedFile: persistedFile,
		IsMutating:    true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover restore: %v\n", err)
		return 1
	}
	composeFile := composeRes.SelectedFile

	eng, err := engine.Resolve(ctx, deps.Runner, engine.ResolveOptions{
		ProjectDir:      projectDir,
		ComposeFile:     composeFile,
		ExplicitEngine:  explicitEngine,
		PersistedEngine: persistedEngine,
		IsMutating:      true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ytmdlctl recover restore: %v\n", err)
		return 1
	}

	envVars, _ := dotenv.ParseFile(filepath.Join(projectDir, ".env"))
	resolvedURL, _ := discovery.ResolveBaseURL(ctx, discovery.ResolveBaseURLOptions{
		ExplicitURL:  cliBaseURL,
		PersistedURL: persistedURL,
		Engine:       eng,
		ProjectDir:   projectDir,
		ComposeFile:  composeFile,
		EnvVars:      envVars,
	})

	confirm := *autoConfirm || *autoConfirmShort
	res, err := recovery.Restore(ctx, eng, recovery.RestoreOptions{
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
		BaseURL:     resolvedURL,
		BackupDir:   *backupDir,
		AutoConfirm: confirm,
		Stdout:      stdout,
		Stderr:      stderr,
		Stdin:       stdin,
	})
	if err != nil {
		if errors.Is(err, recovery.ErrCancelled) {
			fmt.Fprintf(stdout, "Operation cancelled by operator.\n")
			return 0
		}
		fmt.Fprintf(stderr, "ytmdlctl recover restore: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Database recovery complete! Version restored: %s (schema %d)\n", res.RestoredVersion, res.RestoredSchema)
	return 0
}
