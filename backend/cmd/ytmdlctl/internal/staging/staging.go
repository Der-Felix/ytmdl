// Package staging implements target image resolution verification, pre-pulling, and digest comparison.
package staging

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/redact"
)

var (
	// ErrSourceBuildUnsupported is returned when attempting managed image staging on source-build deployments.
	ErrSourceBuildUnsupported = errors.New("managed image staging is unsupported for source-build deployments (compose.yaml)")
	// ErrInternalRegistryUnsupported is returned when attempting managed image staging on internal private registry compose files.
	ErrInternalRegistryUnsupported = errors.New("managed update with public manifest is not supported for internal private registry compose (compose.registry.yaml)")
)

// StageOptions configures target image staging.
type StageOptions struct {
	ProjectDir  string
	ComposeFile string
	Manifest    *manifest.Manifest
}

// StagingResult records verified target images and digests ready for Stage 4 execution.
type StagingResult struct {
	TargetVersion          string
	BackendImage           string
	BackendDigest          string
	FrontendImage          string
	FrontendDigest         string
	TargetSchema           int
	RollbackClassification manifest.RollbackClassification
}

type composeConfigServices struct {
	Services map[string]struct {
		Image string `yaml:"image"`
	} `yaml:"services"`
}

// StageTargetImages verifies target Compose image resolution, pre-pulls backend/frontend images,
// and verifies that actual pulled digests match release-manifest.json exactly.
func StageTargetImages(ctx context.Context, eng engine.Engine, opts StageOptions) (*StagingResult, error) {
	if eng == nil {
		return nil, errors.New("cannot stage images: container engine is not initialized")
	}
	if opts.Manifest == nil {
		return nil, errors.New("manifest is required for image staging")
	}

	baseFile := filepath.Base(opts.ComposeFile)
	if baseFile == "compose.yaml" {
		return nil, ErrSourceBuildUnsupported
	}
	if baseFile == "compose.registry.yaml" {
		return nil, ErrInternalRegistryUnsupported
	}

	targetVersion := opts.Manifest.ReleaseVersion
	expectedBackend := opts.Manifest.Images.Backend.Repository + ":" + targetVersion
	expectedFrontend := opts.Manifest.Images.Frontend.Repository + ":" + targetVersion

	// 1. Supply-Chain Pre-Pull Gate: Resolve what Compose intends to use with YTMDL_VERSION=<target>
	configRes, err := eng.Config(ctx, opts.ProjectDir, opts.ComposeFile, map[string]string{
		"YTMDL_VERSION": targetVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("failed executing compose config for target version: %w", err)
	}
	if configRes.ExitCode != 0 {
		return nil, fmt.Errorf("compose config failed (exit %d): %s", configRes.ExitCode, redact.String(string(configRes.Stderr)))
	}

	var parsedConfig composeConfigServices
	if err := yaml.Unmarshal(configRes.Stdout, &parsedConfig); err != nil {
		return nil, fmt.Errorf("failed parsing compose config output: %w", err)
	}

	backendSvc, ok := parsedConfig.Services["backend"]
	if !ok || backendSvc.Image == "" {
		return nil, errors.New("compose config does not declare backend service or image")
	}
	if backendSvc.Image != expectedBackend {
		return nil, fmt.Errorf("target image resolution failed: backend image reference mismatch: expected %q, compose resolved %q", expectedBackend, backendSvc.Image)
	}

	frontendSvc, ok := parsedConfig.Services["frontend"]
	if !ok || frontendSvc.Image == "" {
		return nil, errors.New("compose config does not declare frontend service or image")
	}
	if frontendSvc.Image != expectedFrontend {
		return nil, fmt.Errorf("target image resolution failed: frontend image reference mismatch: expected %q, compose resolved %q", expectedFrontend, frontendSvc.Image)
	}

	// 2. Pre-Pull target images (ONLY backend and frontend)
	pullRes, err := eng.Pull(ctx, opts.ProjectDir, opts.ComposeFile, map[string]string{
		"YTMDL_VERSION": targetVersion,
	}, "backend", "frontend")
	if err != nil {
		return nil, fmt.Errorf("target image pre-pull failed: %w", err)
	}
	if pullRes.ExitCode != 0 {
		return nil, fmt.Errorf("target image pre-pull failed (exit %d): %s", pullRes.ExitCode, redact.String(string(pullRes.Stderr)))
	}

	// 3. Verify actual pulled image digests from engine (order-independent set verification)
	if err := eng.VerifyImageDigest(ctx, expectedBackend, opts.Manifest.Images.Backend.Digest); err != nil {
		return nil, fmt.Errorf("backend image digest mismatch: %w", err)
	}
	if err := eng.VerifyImageDigest(ctx, expectedFrontend, opts.Manifest.Images.Frontend.Digest); err != nil {
		return nil, fmt.Errorf("frontend image digest mismatch: %w", err)
	}

	return &StagingResult{
		TargetVersion:          targetVersion,
		BackendImage:           expectedBackend,
		BackendDigest:          strings.ToLower(strings.TrimSpace(opts.Manifest.Images.Backend.Digest)),
		FrontendImage:          expectedFrontend,
		FrontendDigest:         strings.ToLower(strings.TrimSpace(opts.Manifest.Images.Frontend.Digest)),
		TargetSchema:           opts.Manifest.TargetSchema,
		RollbackClassification: opts.Manifest.RollbackClassification,
	}, nil
}
