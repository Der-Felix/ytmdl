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

	// 1. The dry run and real update share the same read-only pre-pull gate.
	if err := VerifyTargetImageResolution(ctx, eng, opts); err != nil {
		return nil, err
	}

	// 2. Pre-Pull Platform & Digest Resolution:
	targetPlat, err := eng.TargetPlatform(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed detecting engine target platform: %w", err)
	}

	backendExpectedDigests, err := opts.Manifest.GetExpectedDigests(opts.Manifest.Images.Backend, targetPlat)
	if err != nil {
		return nil, fmt.Errorf("target image staging preflight blocked: %w", err)
	}
	frontendExpectedDigests, err := opts.Manifest.GetExpectedDigests(opts.Manifest.Images.Frontend, targetPlat)
	if err != nil {
		return nil, fmt.Errorf("target image staging preflight blocked: %w", err)
	}

	// Pre-Pull target images (ONLY backend and frontend)
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
	if opts.Manifest.ManifestVersion >= manifest.ManifestVersion3 {
		normPlat, pErr := manifest.NormalizePlatform(targetPlat)
		if pErr != nil {
			return nil, fmt.Errorf("invalid engine target platform: %w", pErr)
		}
		backendPlatSpec, ok := opts.Manifest.Images.Backend.Platforms[normPlat]
		if !ok {
			return nil, fmt.Errorf("manifest v3 missing backend platform %s", normPlat)
		}
		frontendPlatSpec, ok := opts.Manifest.Images.Frontend.Platforms[normPlat]
		if !ok {
			return nil, fmt.Errorf("manifest v3 missing frontend platform %s", normPlat)
		}

		if err := eng.VerifyImageDualDigests(ctx, expectedBackend, opts.Manifest.Images.Backend.Digest, backendPlatSpec.Digest); err != nil {
			return nil, fmt.Errorf("backend image dual digest verification failed: %w", err)
		}
		if err := eng.VerifyImageDualDigests(ctx, expectedFrontend, opts.Manifest.Images.Frontend.Digest, frontendPlatSpec.Digest); err != nil {
			return nil, fmt.Errorf("frontend image dual digest verification failed: %w", err)
		}
	} else {
		if err := eng.VerifyImageAnyDigest(ctx, expectedBackend, backendExpectedDigests); err != nil {
			return nil, fmt.Errorf("backend image digest mismatch: %w", err)
		}
		if err := eng.VerifyImageAnyDigest(ctx, expectedFrontend, frontendExpectedDigests); err != nil {
			return nil, fmt.Errorf("frontend image digest mismatch: %w", err)
		}
	}

	// 4. Verify actual pulled image architecture matches targetPlatform
	backendPlat, err := eng.InspectImagePlatform(ctx, expectedBackend)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting backend image platform: %w", err)
	}
	if backendPlat != targetPlat {
		return nil, fmt.Errorf("backend image platform mismatch: expected %s, got %s", targetPlat, backendPlat)
	}

	frontendPlat, err := eng.InspectImagePlatform(ctx, expectedFrontend)
	if err != nil {
		return nil, fmt.Errorf("failed inspecting frontend image platform: %w", err)
	}
	if frontendPlat != targetPlat {
		return nil, fmt.Errorf("frontend image platform mismatch: expected %s, got %s", targetPlat, frontendPlat)
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

// VerifyTargetImageResolution checks the effective target Compose configuration,
// including host overrides, without pulling images or changing the deployment.
// Configuration and subprocess output may contain secrets and are never returned.
func VerifyTargetImageResolution(ctx context.Context, eng engine.Engine, opts StageOptions) error {
	if eng == nil || opts.Manifest == nil {
		return errors.New("target image resolution requires a container engine and release manifest")
	}
	res, err := eng.Config(ctx, opts.ProjectDir, opts.ComposeFile, map[string]string{
		"YTMDL_VERSION": opts.Manifest.ReleaseVersion,
	})
	if err != nil || res == nil || res.ExitCode != 0 {
		return errors.New("target image resolution failed: compose configuration unavailable")
	}
	var config composeConfigServices
	if err := yaml.Unmarshal(res.Stdout, &config); err != nil {
		return errors.New("target image resolution failed: invalid compose configuration")
	}
	for _, component := range []struct{ name, repository string }{
		{"backend", opts.Manifest.Images.Backend.Repository},
		{"frontend", opts.Manifest.Images.Frontend.Repository},
	} {
		service, ok := config.Services[component.name]
		if !ok || service.Image == "" {
			return fmt.Errorf("target image resolution failed: %s service or image missing", component.name)
		}
		if service.Image != component.repository+":"+opts.Manifest.ReleaseVersion {
			return fmt.Errorf("target image resolution failed: %s image reference mismatch; check host image overrides before updating", component.name)
		}
	}
	return nil
}
