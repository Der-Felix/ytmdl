package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReleaseNotesScript(t *testing.T) {
	// Find root directory containing scripts/generate-release-notes.sh
	repoRoot, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	scriptPath := filepath.Join(repoRoot, "scripts", "generate-release-notes.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("script not found at %s: %v", scriptPath, err)
	}

	t.Run("generates valid release notes for current version", func(t *testing.T) {
		cmd := exec.Command(scriptPath, "--version", "0.18.1")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\nOutput: %s", err, string(out))
		}

		content := string(out)
		if !strings.Contains(content, "# YTMDL v0.18.1") {
			t.Errorf("expected title '# YTMDL v0.18.1', got:\n%s", content)
		}
		if !strings.Contains(content, "## Highlights") {
			t.Errorf("expected '## Highlights', got:\n%s", content)
		}
		if !strings.Contains(content, "## Changes") {
			t.Errorf("expected '## Changes', got:\n%s", content)
		}
		if !strings.Contains(content, "## Update") {
			t.Errorf("expected '## Update', got:\n%s", content)
		}
		if !strings.Contains(content, "ytmdlctl update") {
			t.Errorf("expected 'ytmdlctl update', got:\n%s", content)
		}
		if !strings.Contains(content, "No database migration is required.") {
			t.Errorf("expected migration notice, got:\n%s", content)
		}
		if !strings.Contains(content, "**Full Changelog:**") {
			t.Errorf("expected Full Changelog link, got:\n%s", content)
		}
	})

	t.Run("validate passes on properly structured notes", func(t *testing.T) {
		tmpDir := t.TempDir()
		outFile := filepath.Join(tmpDir, "RELEASE_NOTES.md")

		genCmd := exec.Command(scriptPath, "--version", "0.18.1", "--output", outFile)
		genCmd.Dir = repoRoot
		if out, err := genCmd.CombinedOutput(); err != nil {
			t.Fatalf("gen failed: %v\nOutput: %s", err, string(out))
		}

		valCmd := exec.Command(scriptPath, "--validate", outFile)
		valCmd.Dir = repoRoot
		if out, err := valCmd.CombinedOutput(); err != nil {
			t.Fatalf("validation failed unexpectedly: %v\nOutput: %s", err, string(out))
		}
	})

	t.Run("validate rejects sparse notes containing only compare link", func(t *testing.T) {
		tmpDir := t.TempDir()
		sparseFile := filepath.Join(tmpDir, "SPARSE_NOTES.md")
		if err := os.WriteFile(sparseFile, []byte("**Full Changelog**: https://github.com/Der-Felix/ytmdl/compare/v0.17.4...v0.18.0\n"), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		valCmd := exec.Command(scriptPath, "--validate", sparseFile)
		valCmd.Dir = repoRoot
		out, err := valCmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected validation to fail on sparse notes, but it passed!\nOutput: %s", string(out))
		}
	})

	t.Run("validate rejects empty release notes", func(t *testing.T) {
		tmpDir := t.TempDir()
		emptyFile := filepath.Join(tmpDir, "EMPTY_NOTES.md")
		if err := os.WriteFile(emptyFile, []byte("   \n\n  "), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		valCmd := exec.Command(scriptPath, "--validate", emptyFile)
		valCmd.Dir = repoRoot
		if err := valCmd.Run(); err == nil {
			t.Fatalf("expected validation to fail on empty notes, but it passed!")
		}
	})

	t.Run("validate rejects hollow notes lacking bullet points", func(t *testing.T) {
		tmpDir := t.TempDir()
		hollowFile := filepath.Join(tmpDir, "HOLLOW_NOTES.md")
		content := "# YTMDL v0.18.1\n\n## Highlights\n\n**Full Changelog:** https://example.com\n"
		if err := os.WriteFile(hollowFile, []byte(content), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		valCmd := exec.Command(scriptPath, "--validate", hollowFile)
		valCmd.Dir = repoRoot
		if err := valCmd.Run(); err == nil {
			t.Fatalf("expected validation to fail on notes without bullet points, but it passed!")
		}
	})
}
