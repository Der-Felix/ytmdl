package compose_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/compose"
)

func TestResolveExplicitFileValid(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "compose.ghcr.yaml")
	_ = os.WriteFile(filePath, []byte("services: {}"), 0644)

	res, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:   tmpDir,
		ExplicitFile: "compose.ghcr.yaml",
		IsMutating:   true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if res.SelectedFile != "compose.ghcr.yaml" {
		t.Errorf("SelectedFile = %q, want compose.ghcr.yaml", res.SelectedFile)
	}
}

func TestResolveExplicitFileMissing(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:   tmpDir,
		ExplicitFile: "compose.missing.yaml",
		IsMutating:   true,
	})
	if !errors.Is(err, compose.ErrFileNotFound) {
		t.Fatalf("got %v, want ErrFileNotFound", err)
	}
}

func TestResolvePathEscapeRejected(t *testing.T) {
	tmpDir := t.TempDir()

	// Attempt path traversal
	_, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:   tmpDir,
		ExplicitFile: "../outside/compose.yaml",
		IsMutating:   true,
	})
	if !errors.Is(err, compose.ErrPathEscape) {
		t.Fatalf("got %v, want ErrPathEscape", err)
	}
}

func TestResolveSymlinkEscapeRejected(t *testing.T) {
	projDir := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.yaml")
	_ = os.WriteFile(outsideFile, []byte("services: {}"), 0644)

	// Create symlink inside projDir pointing outside
	symlinkPath := filepath.Join(projDir, "symlinked-compose.yaml")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skipf("symlinks not supported on this filesystem: %v", err)
	}

	_, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:   projDir,
		ExplicitFile: "symlinked-compose.yaml",
		IsMutating:   true,
	})
	if !errors.Is(err, compose.ErrPathEscape) {
		t.Fatalf("symlink escape err = %v, want ErrPathEscape", err)
	}
}

func TestResolveSingleCandidateAutoSelect(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)

	res, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir: tmpDir,
		IsMutating: true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if res.SelectedFile != "compose.ghcr.yaml" {
		t.Errorf("SelectedFile = %q, want compose.ghcr.yaml", res.SelectedFile)
	}
}

func TestResolveMultipleCandidatesMutatingFails(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)

	_, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir: tmpDir,
		IsMutating: true,
	})
	if !errors.Is(err, compose.ErrAmbiguousCompose) {
		t.Fatalf("mutating resolve = %v, want ErrAmbiguousCompose", err)
	}
}

func TestResolveMultipleCandidatesReadOnlyReports(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)

	res, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir: tmpDir,
		IsMutating: false,
	})
	if err != nil {
		t.Fatalf("read-only resolve failed: %v", err)
	}
	if !res.IsAmbiguous {
		t.Error("expected IsAmbiguous == true")
	}
	if len(res.Candidates) != 2 {
		t.Errorf("Candidates count = %d, want 2", len(res.Candidates))
	}
}

func TestResolvePersistedFileOverridesScan(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.ghcr.yaml"), []byte("services: {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "compose.yaml"), []byte("services: {}"), 0644)

	res, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir:    tmpDir,
		PersistedFile: "compose.yaml",
		IsMutating:    true,
	})
	if err != nil {
		t.Fatalf("Resolve with persisted file failed: %v", err)
	}
	if res.SelectedFile != "compose.yaml" {
		t.Errorf("SelectedFile = %q, want compose.yaml", res.SelectedFile)
	}
}

func TestResolveNoComposeFilesFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := compose.Resolve(compose.ResolveOptions{
		ProjectDir: tmpDir,
		IsMutating: false,
	})
	if !errors.Is(err, compose.ErrNoComposeFound) {
		t.Fatalf("got %v, want ErrNoComposeFound", err)
	}
}
