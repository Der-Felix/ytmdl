// Package engine defines the container engine abstraction and detection logic.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

var (
	// ErrNoEngineFound is returned when neither Docker nor Podman Compose is available.
	ErrNoEngineFound = errors.New("neither docker compose nor podman compose is available in PATH")
	// ErrAmbiguousEngine is returned when both engines are present and neither unambiguously owns the project.
	ErrAmbiguousEngine = errors.New("both docker compose and podman compose are installed; explicit --engine is required")
)

var digestRegex = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Engine abstracts container engine operations without Go SDKs.
type Engine interface {
	Name() string
	ComposeVersion(ctx context.Context) (string, error)
	IsServiceRunning(ctx context.Context, projectDir, composeFile, service string) (bool, error)
	Port(ctx context.Context, projectDir, composeFile, service string, containerPort int) (string, error)
	InspectImageDigest(ctx context.Context, imageRef string) (string, error)
	PS(ctx context.Context, projectDir, composeFile string, args ...string) (*runner.RunResult, error)
	Exec(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, command ...string) (*runner.RunResult, error)
	ExecStream(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, stdout io.Writer, command ...string) (*runner.RunResult, error)
	Config(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string) (*runner.RunResult, error)
	Pull(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error)
	UpServices(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error)
	StopServices(ctx context.Context, projectDir, composeFile string, services ...string) (*runner.RunResult, error)
	GetServiceContainerID(ctx context.Context, projectDir, composeFile, service string) (string, error)
	InspectContainerImage(ctx context.Context, containerID string) (imageRef, imageID string, err error)
	VerifyImageDigest(ctx context.Context, imageRef, expectedDigest string) error
	InspectImageRepoDigests(ctx context.Context, imageRef string) ([]string, error)
	InspectImageID(ctx context.Context, imageRef string) (string, error)
}

// BaseEngine implements Engine using a ProcessRunner.
type BaseEngine struct {
	binary string
	runner runner.ProcessRunner
}

// Name returns the engine binary name ("docker" or "podman").
func (e *BaseEngine) Name() string {
	return e.binary
}

// ComposeVersion queries the engine's compose version.
func (e *BaseEngine) ComposeVersion(ctx context.Context) (string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"compose", "version"},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// IsServiceRunning inspects if a service is actively running for the given compose project.
func (e *BaseEngine) IsServiceRunning(ctx context.Context, projectDir, composeFile, service string) (bool, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"compose", "-f", composeFile, "ps", "--format", "{{.Service}}"},
		Dir:        projectDir,
	})
	if err != nil {
		return false, err
	}
	services := strings.Split(string(res.Stdout), "\n")
	for _, s := range services {
		if strings.TrimSpace(s) == service {
			return true, nil
		}
	}
	return false, nil
}

// Port queries the host-forwarded port for a service's container port.
func (e *BaseEngine) Port(ctx context.Context, projectDir, composeFile, service string, containerPort int) (string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"compose", "-f", composeFile, "port", service, strconv.Itoa(containerPort)},
		Dir:        projectDir,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

type imageInspectEntry struct {
	ID          string   `json:"Id"`
	Digest      string   `json:"Digest"`
	RepoDigests []string `json:"RepoDigests"`
}

// VerifyImageDigest verifies that imageRef contains expectedDigest for its repository.
// This check is exact-digest driven and completely order-independent.
func (e *BaseEngine) VerifyImageDigest(ctx context.Context, imageRef, expectedDigest string) error {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"image", "inspect", imageRef},
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("inspect image %s failed (exit %d): %s", imageRef, res.ExitCode, res.Stderr)
	}
	return VerifyExpectedDigest(res.Stdout, imageRef, expectedDigest)
}

// InspectImageRepoDigests returns all valid repository digests for imageRef belonging to its repository.
func (e *BaseEngine) InspectImageRepoDigests(ctx context.Context, imageRef string) ([]string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"image", "inspect", imageRef},
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("inspect image %s failed (exit %d): %s", imageRef, res.ExitCode, res.Stderr)
	}
	return RepoDigestsFromInspect(res.Stdout, imageRef)
}

// InspectImageID returns the local immutable image/config ID (e.g. sha256:abc...) of imageRef.
func (e *BaseEngine) InspectImageID(ctx context.Context, imageRef string) (string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"image", "inspect", imageRef},
	})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("inspect image %s failed (exit %d): %s", imageRef, res.ExitCode, res.Stderr)
	}
	return ParseImageIDFromInspect(res.Stdout)
}

// InspectImageDigest queries the engine for the image's repository digest.
func (e *BaseEngine) InspectImageDigest(ctx context.Context, imageRef string) (string, error) {
	digests, err := e.InspectImageRepoDigests(ctx, imageRef)
	if err != nil {
		return "", err
	}
	if len(digests) == 0 {
		return "", fmt.Errorf("no valid repository digest found for image %q in RepoDigests", imageRef)
	}
	return digests[0], nil
}

func parseInspectEntries(data []byte) ([]imageInspectEntry, error) {
	var entries []imageInspectEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		var single imageInspectEntry
		if sErr := json.Unmarshal(data, &single); sErr != nil {
			return nil, fmt.Errorf("failed parsing image inspect JSON: %w", err)
		}
		entries = []imageInspectEntry{single}
	}
	if len(entries) == 0 {
		return nil, errors.New("image inspect returned empty data")
	}
	return entries, nil
}

func normalizeExpectedRepo(imageRef string) string {
	repo := imageRef
	if idx := strings.Index(repo, ":"); idx != -1 {
		repo = repo[:idx]
	}
	if idx := strings.Index(repo, "@"); idx != -1 {
		repo = repo[:idx]
	}
	return strings.TrimSpace(repo)
}

// RepoDigestsFromInspect extracts all unique, valid sha256 repository digests
// belonging to expectedRepo from the image inspect JSON.
// Any digests under unrelated repositories are strictly excluded.
func RepoDigestsFromInspect(data []byte, expectedRepo string) ([]string, error) {
	entries, err := parseInspectEntries(data)
	if err != nil {
		return nil, err
	}

	normExpected := normalizeExpectedRepo(expectedRepo)
	entry := entries[0]

	var matchingDigests []string
	seen := make(map[string]bool)
	for _, rd := range entry.RepoDigests {
		rd = strings.TrimSpace(rd)
		repo, digest, ok := strings.Cut(rd, "@")
		if !ok {
			continue
		}
		repo = strings.TrimSpace(repo)
		if repo == normExpected || strings.HasSuffix(repo, "/"+normExpected) || strings.HasSuffix(normExpected, "/"+repo) {
			digest = strings.ToLower(strings.TrimSpace(digest))
			if digestRegex.MatchString(digest) && !seen[digest] {
				seen[digest] = true
				matchingDigests = append(matchingDigests, digest)
			}
		}
	}

	return matchingDigests, nil
}

// VerifyExpectedDigest verifies that expectedDigest (from release manifest) is present
// in the image's repository digests for expectedRepo.
// This verification is set-based and independent of array ordering in inspect output.
func VerifyExpectedDigest(data []byte, expectedRepo, expectedDigest string) error {
	cleanExpected := strings.ToLower(strings.TrimSpace(expectedDigest))
	if !digestRegex.MatchString(cleanExpected) {
		return fmt.Errorf("invalid expected digest format %q", expectedDigest)
	}

	digests, err := RepoDigestsFromInspect(data, expectedRepo)
	if err != nil {
		return err
	}
	if len(digests) == 0 {
		return fmt.Errorf("no repository digests found for repository %q in inspect data", expectedRepo)
	}

	for _, d := range digests {
		if d == cleanExpected {
			return nil
		}
	}

	return fmt.Errorf("image digest mismatch for repository %q: expected %s, found %v", expectedRepo, expectedDigest, digests)
}

// ParseImageIDFromInspect extracts the local immutable image/config ID from inspect JSON.
func ParseImageIDFromInspect(data []byte) (string, error) {
	entries, err := parseInspectEntries(data)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(entries[0].ID)
	if id == "" {
		return "", errors.New("image inspect returned empty image ID")
	}
	return id, nil
}

// ParseDigestFromInspect extracts a normalized sha256 digest from inspect JSON.
// Deprecated: For target images, use VerifyExpectedDigest. For snapshots, use RepoDigestsFromInspect.
func ParseDigestFromInspect(data []byte, imageRef string) (string, error) {
	digests, err := RepoDigestsFromInspect(data, imageRef)
	if err != nil {
		return "", err
	}
	if len(digests) == 0 {
		return "", fmt.Errorf("no valid repository digest found for image %q in RepoDigests", imageRef)
	}
	return digests[0], nil
}

// CheckPodmanProviderCompatibility verifies that if engine is Podman,
// the external Compose provider is compatible with YTMDL (Compose V2 provider).
// Known incompatible providers like Python podman-compose 1.x fail with an actionable error.
func CheckPodmanProviderCompatibility(ctx context.Context, eng Engine) error {
	if eng == nil || eng.Name() != "podman" {
		return nil
	}
	ver, err := eng.ComposeVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed determining Podman Compose provider version: %w", err)
	}
	verLower := strings.ToLower(ver)
	if strings.Contains(verLower, "podman-compose") {
		return errors.New("the active Podman Compose provider (\"podman-compose\") is not compatible with YTMDL's user namespace configuration. Install a Compose V2 provider (e.g. docker-compose-v2 or docker-compose-plugin) and rerun ytmdlctl")
	}
	return nil
}

// PS executes compose ps with optional flags.
func (e *BaseEngine) PS(ctx context.Context, projectDir, composeFile string, args ...string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "ps"}
	cmdArgs = append(cmdArgs, args...)
	return e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       cmdArgs,
		Dir:        projectDir,
	})
}

// Exec executes a command inside a compose service container (using -T for non-interactive execution).
func (e *BaseEngine) Exec(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, command ...string) (*runner.RunResult, error) {
	return e.ExecStream(ctx, projectDir, composeFile, service, stdin, nil, command...)
}

// ExecStream executes a command inside a compose service container with optional streamed stdout and stdin.
func (e *BaseEngine) ExecStream(ctx context.Context, projectDir, composeFile, service string, stdin io.Reader, stdout io.Writer, command ...string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "exec", "-T", service}
	cmdArgs = append(cmdArgs, command...)
	return e.runner.Run(ctx, runner.RunRequest{
		Executable:   e.binary,
		Args:         cmdArgs,
		Dir:          projectDir,
		Stdin:        stdin,
		StdoutWriter: stdout,
	})
}

// Config runs compose config with optional environment overrides.
func (e *BaseEngine) Config(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "config"}
	var env []string
	for k, v := range envOverrides {
		env = append(env, k+"="+v)
	}
	return e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       cmdArgs,
		Dir:        projectDir,
		Env:        env,
	})
}

// Pull runs compose pull with optional environment overrides for specific services.
func (e *BaseEngine) Pull(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "pull"}
	cmdArgs = append(cmdArgs, services...)
	var env []string
	for k, v := range envOverrides {
		env = append(env, k+"="+v)
	}
	return e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       cmdArgs,
		Dir:        projectDir,
		Env:        env,
	})
}

// UpServices executes compose -f <file> up -d --no-deps <services...> with optional env overrides.
func (e *BaseEngine) UpServices(ctx context.Context, projectDir, composeFile string, envOverrides map[string]string, services ...string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "up", "-d", "--no-deps"}
	cmdArgs = append(cmdArgs, services...)
	var env []string
	for k, v := range envOverrides {
		env = append(env, k+"="+v)
	}
	return e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       cmdArgs,
		Dir:        projectDir,
		Env:        env,
	})
}

// StopServices executes compose -f <file> stop <services...>.
func (e *BaseEngine) StopServices(ctx context.Context, projectDir, composeFile string, services ...string) (*runner.RunResult, error) {
	cmdArgs := []string{"compose", "-f", composeFile, "stop"}
	cmdArgs = append(cmdArgs, services...)
	return e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       cmdArgs,
		Dir:        projectDir,
	})
}

// GetServiceContainerID queries the running container ID for a compose service.
func (e *BaseEngine) GetServiceContainerID(ctx context.Context, projectDir, composeFile, service string) (string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"compose", "-f", composeFile, "ps", "-q", service},
		Dir:        projectDir,
	})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("compose ps -q %s failed (exit %d): %s", service, res.ExitCode, res.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", fmt.Errorf("no container found for service %q", service)
	}
	return strings.TrimSpace(lines[0]), nil
}

type containerInspectJSON struct {
	Image  string `json:"Image"`
	Config struct {
		Image string `json:"Image"`
	} `json:"Config"`
}

// InspectContainerImage extracts the image reference and image ID from a container inspect.
func (e *BaseEngine) InspectContainerImage(ctx context.Context, containerID string) (string, string, error) {
	res, err := e.runner.Run(ctx, runner.RunRequest{
		Executable: e.binary,
		Args:       []string{"inspect", containerID},
	})
	if err != nil {
		return "", "", err
	}
	if res.ExitCode != 0 {
		return "", "", fmt.Errorf("inspect container %s failed (exit %d): %s", containerID, res.ExitCode, res.Stderr)
	}

	var entries []containerInspectJSON
	if err := json.Unmarshal(res.Stdout, &entries); err != nil {
		return "", "", fmt.Errorf("failed parsing container inspect JSON: %w", err)
	}
	if len(entries) == 0 {
		return "", "", fmt.Errorf("empty container inspect for %s", containerID)
	}

	return entries[0].Config.Image, entries[0].Image, nil
}

// NewDocker creates an Engine for Docker Compose.
func NewDocker(r runner.ProcessRunner) *BaseEngine {
	return &BaseEngine{binary: "docker", runner: r}
}

// NewPodman creates an Engine for Podman Compose.
func NewPodman(r runner.ProcessRunner) *BaseEngine {
	return &BaseEngine{binary: "podman", runner: r}
}

// ResolveOptions controls engine discovery.
type ResolveOptions struct {
	ProjectDir      string
	ComposeFile     string
	ExplicitEngine  string
	PersistedEngine string
	IsMutating      bool
}

// Resolve identifies the container engine to use following strict rules.
func Resolve(ctx context.Context, r runner.ProcessRunner, opts ResolveOptions) (Engine, error) {
	if opts.ExplicitEngine != "" {
		norm := strings.ToLower(strings.TrimSpace(opts.ExplicitEngine))
		switch norm {
		case "docker":
			return NewDocker(r), nil
		case "podman":
			return NewPodman(r), nil
		default:
			return nil, fmt.Errorf("unsupported container engine %q (expected docker or podman)", opts.ExplicitEngine)
		}
	}

	if opts.PersistedEngine != "" {
		norm := strings.ToLower(strings.TrimSpace(opts.PersistedEngine))
		switch norm {
		case "docker":
			return NewDocker(r), nil
		case "podman":
			return NewPodman(r), nil
		}
	}

	dockerAvail := isBinaryAvailable(ctx, r, "docker")
	podmanAvail := isBinaryAvailable(ctx, r, "podman")

	if !dockerAvail && !podmanAvail {
		return nil, ErrNoEngineFound
	}

	if dockerAvail && !podmanAvail {
		return NewDocker(r), nil
	}
	if podmanAvail && !dockerAvail {
		return NewPodman(r), nil
	}

	// Both available: probe project ownership
	if opts.ComposeFile != "" {
		dockerOwns := checkEngineOwnsProject(ctx, r, "docker", opts.ProjectDir, opts.ComposeFile)
		podmanOwns := checkEngineOwnsProject(ctx, r, "podman", opts.ProjectDir, opts.ComposeFile)

		if dockerOwns && !podmanOwns {
			return NewDocker(r), nil
		}
		if podmanOwns && !dockerOwns {
			return NewPodman(r), nil
		}
	}

	// Cannot determine unique ownership
	return nil, ErrAmbiguousEngine
}

func isBinaryAvailable(ctx context.Context, r runner.ProcessRunner, name string) bool {
	res, err := r.Run(ctx, runner.RunRequest{
		Executable: name,
		Args:       []string{"compose", "version"},
	})
	return err == nil && res != nil && res.ExitCode == 0
}

func checkEngineOwnsProject(ctx context.Context, r runner.ProcessRunner, binary, projectDir, composeFile string) bool {
	res, err := r.Run(ctx, runner.RunRequest{
		Executable: binary,
		Args:       []string{"compose", "-f", composeFile, "ps", "-q"},
		Dir:        projectDir,
	})
	if err != nil || res == nil || res.ExitCode != 0 {
		return false
	}
	return strings.TrimSpace(string(res.Stdout)) != ""
}
