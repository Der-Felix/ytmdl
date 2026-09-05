package staging_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
	"ytdm/backend/cmd/ytmdlctl/internal/staging"
)

func validTestManifest() *manifest.Manifest {
	m := &manifest.Manifest{
		ManifestVersion:        manifest.ManifestVersion2,
		ReleaseVersion:         "0.16.0",
		ReleaseTag:             "v0.16.0",
		TargetSchema:           9,
		RollbackClassification: manifest.RollbackSchemaNeutral,
		MinUpgradeFrom:         "0.15.0",
	}
	m.Images.Backend = manifest.ImageSpec{
		Repository: "ghcr.io/der-felix/ytmdl-backend",
		Tag:        "0.16.0",
		Digest:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}
	m.Images.Frontend = manifest.ImageSpec{
		Repository: "ghcr.io/der-felix/ytmdl-frontend",
		Tag:        "0.16.0",
		Digest:     "sha256:2222222222222222222222222222222222222222222222222222222222222222",
	}
	return m
}

func setupTestStagingEngine(fake *runner.FakeProcessRunner, resolvedBackend, resolvedFrontend string, pullErr error, backendInspectJSON, frontendInspectJSON string) {
	configYAML := "services:\n  backend:\n    image: " + resolvedBackend + "\n  frontend:\n    image: " + resolvedFrontend + "\n"
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "config"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(configYAML),
	}, nil)

	pullRes := &runner.RunResult{ExitCode: 0}
	if pullErr != nil {
		pullRes = &runner.RunResult{ExitCode: 1, Stderr: []byte("pull failed: network timeout")}
	}
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "pull", "backend", "frontend"}, pullRes, pullErr)

	fake.Register("docker", []string{"image", "inspect", resolvedBackend}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(backendInspectJSON),
	}, nil)

	fake.Register("docker", []string{"image", "inspect", resolvedFrontend}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(frontendInspectJSON),
	}, nil)
}

func TestStageTargetImagesSuccess(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	backendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"]}]`
	frontendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	res, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err != nil {
		t.Fatalf("StageTargetImages failed: %v", err)
	}

	if res.TargetVersion != "0.16.0" {
		t.Errorf("TargetVersion = %q, want 0.16.0", res.TargetVersion)
	}
	if res.BackendDigest != m.Images.Backend.Digest {
		t.Errorf("BackendDigest = %q, want %q", res.BackendDigest, m.Images.Backend.Digest)
	}
	if res.FrontendDigest != m.Images.Frontend.Digest {
		t.Errorf("FrontendDigest = %q, want %q", res.FrontendDigest, m.Images.Frontend.Digest)
	}
}

func TestStageTargetImagesSourceBuildUnsupported(t *testing.T) {
	fake := runner.NewFake()
	eng := engine.NewDocker(fake)
	m := validTestManifest()

	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.yaml",
		Manifest:    m,
	})
	if !errors.Is(err, staging.ErrSourceBuildUnsupported) {
		t.Fatalf("got %v, want ErrSourceBuildUnsupported", err)
	}
}

func TestStageTargetImagesInternalRegistryUnsupported(t *testing.T) {
	fake := runner.NewFake()
	eng := engine.NewDocker(fake)
	m := validTestManifest()

	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.registry.yaml",
		Manifest:    m,
	})
	if !errors.Is(err, staging.ErrInternalRegistryUnsupported) {
		t.Fatalf("got %v, want ErrInternalRegistryUnsupported", err)
	}
}

func TestStageTargetImagesResolvedImageMismatchRejectedBeforePull(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	// Compose resolves to latest instead of 0.16.0
	configYAML := "services:\n  backend:\n    image: ghcr.io/der-felix/ytmdl-backend:latest\n  frontend:\n    image: ghcr.io/der-felix/ytmdl-frontend:0.16.0\n"
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "config"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(configYAML),
	}, nil)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil || !strings.Contains(err.Error(), "image reference mismatch") {
		t.Fatalf("expected resolution mismatch error, got: %v", err)
	}

	// Verify pull was NEVER called
	for _, call := range fake.Calls() {
		for _, arg := range call.Args {
			if arg == "pull" {
				t.Fatal("SECURITY VIOLATION: compose pull was called despite image resolution mismatch!")
			}
		}
	}
}

func TestStageTargetImagesPullFailure(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		errors.New("pull failed"), "", "")

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil || !strings.Contains(err.Error(), "pull") {
		t.Fatalf("expected pull failure error, got: %v", err)
	}
}

func TestStageTargetImagesBackendDigestMismatchFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	// Wrong backend digest
	backendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:9999999999999999999999999999999999999999999999999999999999999999"]}]`
	frontendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil || !strings.Contains(err.Error(), "backend image digest mismatch") {
		t.Fatalf("expected backend digest mismatch error, got: %v", err)
	}
}

func TestStageTargetImagesFrontendDigestMismatchFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	backendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"]}]`
	// Wrong frontend digest
	frontendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:8888888888888888888888888888888888888888888888888888888888888888"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil || !strings.Contains(err.Error(), "frontend image digest mismatch") {
		t.Fatalf("expected frontend digest mismatch error, got: %v", err)
	}
}

func TestStageTargetImagesSecretBearingComposeConfigDoesNotLeak(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	secretValue := "super-secret-test-value"
	secretURL := "postgres://user:" + secretValue + "@db/test"

	// Compose config contains secrets in environment
	configYAML := fmt.Sprintf(`services:
  backend:
    image: ghcr.io/der-felix/ytmdl-backend:0.16.0
    environment:
      POSTGRES_PASSWORD: %s
      MUSICDL_DATABASE_URL: %s
  frontend:
    image: ghcr.io/der-felix/ytmdl-frontend:0.16.0
`, secretValue, secretURL)

	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "config"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(configYAML),
	}, nil)

	backendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"]}]`
	frontendInspect := `[{"RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222"]}]`

	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "pull", "backend", "frontend"}, &runner.RunResult{ExitCode: 0}, nil)
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-backend:0.16.0"}, &runner.RunResult{ExitCode: 0, Stdout: []byte(backendInspect)}, nil)
	fake.Register("docker", []string{"image", "inspect", "ghcr.io/der-felix/ytmdl-frontend:0.16.0"}, &runner.RunResult{ExitCode: 0, Stdout: []byte(frontendInspect)}, nil)

	eng := engine.NewDocker(fake)
	res, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err != nil {
		t.Fatalf("StageTargetImages failed: %v", err)
	}

	// Verify secrets do NOT appear anywhere in StagingResult
	resultStr := fmt.Sprintf("%+v", res)
	if strings.Contains(resultStr, secretValue) {
		t.Fatalf("SECURITY VIOLATION: secret %q leaked into StagingResult: %s", secretValue, resultStr)
	}
}

func TestStageTargetImagesSecretInComposeConfigErrorIsRedacted(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	secretURL := "postgres://user:super-secret-test-value@db/test"
	errorStderr := "Error: invalid URL format in " + secretURL

	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "config"}, &runner.RunResult{
		ExitCode: 1,
		Stderr:   []byte(errorStderr),
	}, nil)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "super-secret-test-value") {
		t.Fatalf("SECURITY VIOLATION: secret password leaked in returned error: %s", errStr)
	}
	if !strings.Contains(errStr, "***REDACTED***") {
		t.Fatalf("expected redacted marker in error, got: %s", errStr)
	}
}

func TestStageTargetImagesPlatformMismatchFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()

	// Engine target platform is explicitly registered as linux/amd64
	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "x86_64", "OSType": "linux"}`),
	}, nil)

	// Image inspect reports arm64 (mismatch)
	backendInspect := `[{"Architecture": "arm64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"]}]`
	frontendInspect := `[{"Architecture": "arm64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil {
		t.Fatal("expected error for platform mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "platform mismatch") {
		t.Errorf("expected platform mismatch error, got: %v", err)
	}
}

func TestStageTargetImagesUnsupportedPlatformFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()
	m.ManifestVersion = 3
	m.Images.Backend.Platforms = map[string]manifest.PlatformSpec{
		"linux/arm64": {Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	m.Images.Frontend.Platforms = map[string]manifest.PlatformSpec{
		"linux/arm64": {Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
	}

	// Engine target platform is linux/amd64 (not present in platforms map)
	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "x86_64", "OSType": "linux"}`),
	}, nil)

	configYAML := "services:\n  backend:\n    image: ghcr.io/der-felix/ytmdl-backend:0.16.0\n  frontend:\n    image: ghcr.io/der-felix/ytmdl-frontend:0.16.0\n"
	fake.Register("docker", []string{"compose", "-f", "compose.ghcr.yaml", "config"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(configYAML),
	}, nil)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil {
		t.Fatal("expected error for unsupported platform, got nil")
	}
	if !strings.Contains(err.Error(), "not supported by image") {
		t.Errorf("expected not supported by image error, got: %v", err)
	}
}

func TestStageTargetImagesManifestV3MultiArchSuccess(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()
	m.ManifestVersion = 3
	m.Images.Backend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"linux/arm64": {Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	m.Images.Frontend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		"linux/arm64": {Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
	}

	// Engine is linux/amd64
	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "x86_64", "OSType": "linux"}`),
	}, nil)

	// Pulled image RepoDigests has BOTH the index digest and the platform-specific digest
	backendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111", "ghcr.io/der-felix/ytmdl-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}]`
	frontendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222", "ghcr.io/der-felix/ytmdl-frontend@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	res, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err != nil {
		t.Fatalf("StageTargetImages failed: %v", err)
	}
	if res.BackendImage != "ghcr.io/der-felix/ytmdl-backend:0.16.0" {
		t.Errorf("BackendImage = %q", res.BackendImage)
	}
}

func TestStageTargetImagesManifestV3MultiArchSuccess_ARM64(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()
	m.ManifestVersion = 3
	m.Images.Backend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"linux/arm64": {Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	m.Images.Frontend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		"linux/arm64": {Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
	}

	// Engine is linux/arm64
	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "aarch64", "OSType": "linux"}`),
	}, nil)

	// Pulled image RepoDigests has BOTH the index digest and the ARM64 platform digest
	backendInspect := `[{"Architecture": "arm64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111", "ghcr.io/der-felix/ytmdl-backend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]}]`
	frontendInspect := `[{"Architecture": "arm64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222", "ghcr.io/der-felix/ytmdl-frontend@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	res, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err != nil {
		t.Fatalf("StageTargetImages failed: %v", err)
	}
	if res.BackendImage != "ghcr.io/der-felix/ytmdl-backend:0.16.0" {
		t.Errorf("BackendImage = %q", res.BackendImage)
	}
}

func TestStageTargetImagesManifestV3_MissingIndexDigestFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()
	m.ManifestVersion = 3
	m.Images.Backend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	m.Images.Frontend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
	}

	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "x86_64", "OSType": "linux"}`),
	}, nil)

	// backend only has platform digest, missing index digest
	backendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}]`
	frontendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222", "ghcr.io/der-felix/ytmdl-frontend@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil {
		t.Fatal("expected failure when index digest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dual digest verification failed") {
		t.Errorf("expected dual digest verification failed error, got: %v", err)
	}
}

func TestStageTargetImagesManifestV3_MissingPlatformDigestFails(t *testing.T) {
	fake := runner.NewFake()
	m := validTestManifest()
	m.ManifestVersion = 3
	m.Images.Backend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	m.Images.Frontend.Platforms = map[string]manifest.PlatformSpec{
		"linux/amd64": {Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
	}

	fake.Register("docker", []string{"info", "--format", "{{json .}}"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(`{"Architecture": "x86_64", "OSType": "linux"}`),
	}, nil)

	// backend only has index digest, missing platform digest
	backendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-backend@sha256:1111111111111111111111111111111111111111111111111111111111111111"]}]`
	frontendInspect := `[{"Architecture": "amd64", "Os": "linux", "RepoDigests": ["ghcr.io/der-felix/ytmdl-frontend@sha256:2222222222222222222222222222222222222222222222222222222222222222", "ghcr.io/der-felix/ytmdl-frontend@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"]}]`

	setupTestStagingEngine(fake,
		"ghcr.io/der-felix/ytmdl-backend:0.16.0",
		"ghcr.io/der-felix/ytmdl-frontend:0.16.0",
		nil, backendInspect, frontendInspect)

	eng := engine.NewDocker(fake)
	_, err := staging.StageTargetImages(context.Background(), eng, staging.StageOptions{
		ProjectDir:  ".",
		ComposeFile: "compose.ghcr.yaml",
		Manifest:    m,
	})
	if err == nil {
		t.Fatal("expected failure when platform digest is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dual digest verification failed") {
		t.Errorf("expected dual digest verification failed error, got: %v", err)
	}
}
