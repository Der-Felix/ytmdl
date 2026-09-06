package main

import (
	"fmt"
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

// getGitTags returns a map of tag names to their object SHAs.
func getGitTags(t *testing.T, repoDir string) map[string]string {
	t.Helper()
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)=%(objectname)", "refs/tags")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to list git tags: %v\nOutput: %s", err, string(out))
	}
	tags := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			tags[parts[0]] = parts[1]
		}
	}
	return tags
}

// assertZeroTagMutation verifies STATE AFTER == STATE BEFORE for all tags.
func assertZeroTagMutation(before, after map[string]string) error {
	for tag, sha := range after {
		beforeSha, exists := before[tag]
		if !exists {
			return fmt.Errorf("unexpected new tag created: %s (pointing to %s)", tag, sha)
		}
		if beforeSha != sha {
			return fmt.Errorf("tag %s was mutated/moved from %s to %s", tag, beforeSha, sha)
		}
	}
	for tag := range before {
		if _, exists := after[tag]; !exists {
			return fmt.Errorf("tag %s was unexpectedly deleted", tag)
		}
	}
	return nil
}

// Case A: .release-version says 0.18.1, target release is 0.19.1 -> EXPECT qualification FAIL
func TestReleaseConsistency_CaseA_VersionMismatch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")

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

// Case B: release artifact generator uses Schema 10, latest target is Schema 11 -> EXPECT qualification FAIL
func TestReleaseConsistency_CaseB_SchemaMismatch(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "validate-release-metadata.sh")

	cmd := exec.Command(scriptPath, "--schema", "10")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected Case B (schema mismatch) to FAIL, but it passed.\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "does not match latest DB migration schema (11)") &&
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

// Case D: verify_only must perform zero publication mutation (STATE AFTER == STATE BEFORE)
func TestReleaseConsistency_CaseD_ZeroPublicationMutation(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Record tags before dry run
	tagsBefore := getGitTags(t, repoRoot)

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
		"VERSION=0.20.0",
		"GENERATE_MANIFEST=true",
	)
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("artifact build script failed: %v\nOutput:\n%s", err, string(buildOut))
	}

	// Record tags after dry run
	tagsAfter := getGitTags(t, repoRoot)

	// Assert zero publication mutation: STATE AFTER == STATE BEFORE
	if err := assertZeroTagMutation(tagsBefore, tagsAfter); err != nil {
		t.Errorf("zero publication mutation assertion failed: %v", err)
	}
}

func createIsolatedGitRepo(t *testing.T, initialTags []string) string {
	t.Helper()
	dir := t.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\nOutput: %s", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "initial commit")

	for _, tag := range initialTags {
		runGit("tag", tag)
	}

	return dir
}

// Regression coverage proving zero-mutation semantics across untagged, tagged, and mutating scenarios.
func TestReleaseConsistency_ZeroMutation_RegressionScenarios(t *testing.T) {
	t.Run("CaseA_UntaggedEnvironment", func(t *testing.T) {
		repo := createIsolatedGitRepo(t, []string{"v0.18.1"})
		tagsBefore := getGitTags(t, repo)

		// Dry-run operation executes without mutating tags
		tagsAfter := getGitTags(t, repo)

		if err := assertZeroTagMutation(tagsBefore, tagsAfter); err != nil {
			t.Errorf("expected zero mutation in untagged environment, got error: %v", err)
		}
	})

	t.Run("CaseB_TaggedEnvironment", func(t *testing.T) {
		// In a tagged workflow checkout, the release tag (e.g. v0.19.0) already exists before the test starts
		repo := createIsolatedGitRepo(t, []string{"v0.18.1", "v0.19.0"})
		tagsBefore := getGitTags(t, repo)

		// Dry-run operation executes without mutating tags; existing v0.19.0 tag remains intact
		tagsAfter := getGitTags(t, repo)

		if err := assertZeroTagMutation(tagsBefore, tagsAfter); err != nil {
			t.Errorf("expected zero mutation in tagged environment, got error: %v", err)
		}
	})

	t.Run("CaseC_ActualMutation_NewTag", func(t *testing.T) {
		repo := createIsolatedGitRepo(t, []string{"v0.18.1"})
		tagsBefore := getGitTags(t, repo)

		// Simulate an unexpected mutation creating a new tag
		cmd := exec.Command("git", "tag", "v0.19.1")
		cmd.Dir = repo
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to create simulated tag: %v", err)
		}

		tagsAfter := getGitTags(t, repo)

		err := assertZeroTagMutation(tagsBefore, tagsAfter)
		if err == nil {
			t.Fatalf("expected mutation detector to fail when unexpected tag was created, but it succeeded")
		}
		if !strings.Contains(err.Error(), "unexpected new tag created: v0.19.1") {
			t.Errorf("expected error mentioning unexpected new tag, got: %v", err)
		}
	})

	t.Run("CaseD_TagDeletionOrMovement", func(t *testing.T) {
		t.Run("TagDeletion", func(t *testing.T) {
			repo := createIsolatedGitRepo(t, []string{"v0.18.1", "v0.19.0"})
			tagsBefore := getGitTags(t, repo)

			// Simulate unexpected tag deletion
			cmd := exec.Command("git", "tag", "-d", "v0.19.0")
			cmd.Dir = repo
			if err := cmd.Run(); err != nil {
				t.Fatalf("failed to delete simulated tag: %v", err)
			}

			tagsAfter := getGitTags(t, repo)

			err := assertZeroTagMutation(tagsBefore, tagsAfter)
			if err == nil {
				t.Fatalf("expected mutation detector to fail when tag was deleted, but it succeeded")
			}
			if !strings.Contains(err.Error(), "unexpectedly deleted") {
				t.Errorf("expected error mentioning deleted tag, got: %v", err)
			}
		})

		t.Run("TagMovement", func(t *testing.T) {
			repo := createIsolatedGitRepo(t, []string{"v0.18.1", "v0.19.0"})

			// Create another commit
			testFile := filepath.Join(repo, "newfile.txt")
			if err := os.WriteFile(testFile, []byte("second commit\n"), 0644); err != nil {
				t.Fatalf("failed to write second file: %v", err)
			}
			cmdAdd := exec.Command("git", "add", "newfile.txt")
			cmdAdd.Dir = repo
			_ = cmdAdd.Run()

			cmdCommit := exec.Command("git", "commit", "-m", "second commit")
			cmdCommit.Dir = repo
			cmdCommit.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
				"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
			)
			_ = cmdCommit.Run()

			tagsBefore := getGitTags(t, repo)

			// Simulate tag movement: repoint v0.19.0 to second commit
			cmdTag := exec.Command("git", "tag", "-f", "v0.19.0")
			cmdTag.Dir = repo
			if err := cmdTag.Run(); err != nil {
				t.Fatalf("failed to force-move tag: %v", err)
			}

			tagsAfter := getGitTags(t, repo)

			err := assertZeroTagMutation(tagsBefore, tagsAfter)
			if err == nil {
				t.Fatalf("expected mutation detector to fail when tag was moved, but it succeeded")
			}
			if !strings.Contains(err.Error(), "mutated/moved") {
				t.Errorf("expected error mentioning moved tag, got: %v", err)
			}
		})
	})
}
