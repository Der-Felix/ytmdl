package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("validation script not found at %s: %v", scriptPath, err)
	}
	return repoRoot
}

// Case A: .release-version says 0.18.1, target release is 0.19.0 -> EXPECT qualification FAIL
func TestReleaseConsistency_CaseA_VersionMismatch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")

	// Pass an explicit mismatched version (e.g. 0.18.1 when repo is 0.19.0)
	cmd := exec.Command(scriptPath, "--version", "0.18.1")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected Case A (version mismatch) to FAIL, but it passed.\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Target version (0.18.1) does not match .release-version") &&
		!strings.Contains(outStr, "RELEASE CONSISTENCY ERROR") {
		t.Errorf("expected version mismatch error in output, got:\n%s", outStr)
	}
}

// Case B: release artifact generator uses Schema 9, latest target is Schema 10 -> EXPECT qualification FAIL
func TestReleaseConsistency_CaseB_SchemaMismatch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")

	// Pass an explicit stale schema (Schema 9 when latest DB migration is 10)
	cmd := exec.Command(scriptPath, "--schema", "9")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected Case B (schema mismatch) to FAIL, but it passed.\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "does not match latest DB migration schema (10)") &&
		!strings.Contains(outStr, "RELEASE CONSISTENCY ERROR") {
		t.Errorf("expected schema mismatch error in output, got:\n%s", outStr)
	}
}

// Case C: all version/schema metadata consistent -> EXPECT qualification PASS
func TestReleaseConsistency_CaseC_AllConsistent(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")

	cmd := exec.Command(scriptPath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("expected Case C (all consistent) to PASS, but failed with: %v\nOutput:\n%s", err, string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "Release metadata validation PASSED") {
		t.Errorf("expected validation PASSED confirmation, got:\n%s", outStr)
	}
}

// Case D: verify_only must perform zero publication mutation even when qualification fails
func TestReleaseConsistency_CaseD_ZeroPublicationMutation(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Record tags before test
	tagsBeforeCmd := exec.Command("git", "tag", "-l")
	tagsBeforeCmd.Dir = repoRoot
	tagsBeforeOut, err := tagsBeforeCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to list git tags before test: %v", err)
	}

	// 1. Run validation that fails
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")
	failCmd := exec.Command(scriptPath, "--version", "0.18.1")
	failCmd.Dir = repoRoot
	_ = failCmd.Run() // Expected to fail

	// 2. Run local artifact generation in temporary dir
	tmpDir := t.TempDir()
	buildScript := filepath.Join(repoRoot, "scripts", "build-release-artifacts.sh")
	buildCmd := exec.Command(buildScript)
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(),
		"OUTPUT_DIR="+tmpDir,
		"VERSION=0.19.0",
		"GENERATE_MANIFEST=true",
	)
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("artifact build script failed: %v\nOutput:\n%s", err, string(buildOut))
	}

	// Record tags after test
	tagsAfterCmd := exec.Command("git", "tag", "-l")
	tagsAfterCmd.Dir = repoRoot
	tagsAfterOut, err := tagsAfterCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to list git tags after test: %v", err)
	}

	// Verify no new tags were created
	if string(tagsBeforeOut) != string(tagsAfterOut) {
		t.Errorf("git tags mutated during dry run! Before: %q, After: %q", string(tagsBeforeOut), string(tagsAfterOut))
	}

	// Verify v0.19.0 tag does NOT exist
	v19Cmd := exec.Command("git", "tag", "-l", "v0.19.0")
	v19Cmd.Dir = repoRoot
	v19Out, _ := v19Cmd.CombinedOutput()
	if strings.TrimSpace(string(v19Out)) != "" {
		t.Errorf("v0.19.0 tag was unexpectedly created: %s", string(v19Out))
	}
}
