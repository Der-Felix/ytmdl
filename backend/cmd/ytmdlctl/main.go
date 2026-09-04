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
	"strings"
	"syscall"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/backup"
	"ytdm/backend/cmd/ytmdlctl/internal/compose"
	"ytdm/backend/cmd/ytmdlctl/internal/config"
	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/lock"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/orchestrator"
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
	Runner     runner.ProcessRunner
	HTTPClient *http.Client
	GitHubURL  string
	Repository string
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
  version       Display ytmdlctl version and runtime platform
  status        Inspect local deployment status and configuration
  check         Check for available releases (Stage 2)
  update        Safely update the YTMDL deployment (use --dry-run in Stage 2)
  backup        Create and validate a database backup (Stage 3)
  rollback      Revert containers to the previous working state (Stage 4)
  manifest-gen  Generate and validate release-manifest.json (Stage 5)

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
		if st.IsInterrupted() {
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
		rollbackClass = string(targetManifest.RollbackClassification)
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

func runManifestGen(stdout, stderr io.Writer, args []string) int {
	manifestFlags := flag.NewFlagSet("manifest-gen", flag.ContinueOnError)
	manifestFlags.SetOutput(stdout)
	manifestFlags.Usage = func() {
		fmt.Fprintf(stdout, `Usage: ytmdlctl manifest-gen [flags]

Generate and validate release-manifest.json for a release.

Flags:
  --version <ver>         Release version (e.g. 0.16.0)
  --tag <tag>             Release git tag (e.g. v0.16.0, optional)
  --schema <num>          Target database schema (default: 8)
  --classification <cls>  Rollback classification (default: schema_neutral)
  --min-upgrade <ver>     Minimum upgradeable version (default: 0.15.0)
  --backend-digest <d>    sha256 digest of pushed backend image
  --frontend-digest <d>   sha256 digest of pushed frontend image
  --required-env <keys>   Comma-separated list of required environment variables
  -o, --output <path>     Output file path (default: release-manifest.json, - for stdout)
`)
	}

	version := manifestFlags.String("version", "", "release version without leading 'v'")
	tag := manifestFlags.String("tag", "", "release git tag (optional)")
	schema := manifestFlags.Int("schema", 8, "target database schema")
	classification := manifestFlags.String("classification", "schema_neutral", "rollback classification")
	minUpgrade := manifestFlags.String("min-upgrade", "0.15.0", "minimum upgradeable version")
	backendDigest := manifestFlags.String("backend-digest", "", "sha256 digest of pushed backend image")
	frontendDigest := manifestFlags.String("frontend-digest", "", "sha256 digest of pushed frontend image")
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

	var envList []string
	if *requiredEnv != "" {
		for _, e := range strings.Split(*requiredEnv, ",") {
			tr := strings.TrimSpace(e)
			if tr != "" {
				envList = append(envList, tr)
			}
		}
	}

	data, err := manifest.Generate(manifest.GeneratorOptions{
		ReleaseVersion:         *version,
		ReleaseTag:             *tag,
		TargetSchema:           *schema,
		RollbackClassification: manifest.RollbackClassification(*classification),
		MinUpgradeFrom:         *minUpgrade,
		BackendDigest:          *backendDigest,
		FrontendDigest:         *frontendDigest,
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
