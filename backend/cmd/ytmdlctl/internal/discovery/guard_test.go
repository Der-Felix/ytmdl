package discovery_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
	"ytdm/backend/cmd/ytmdlctl/internal/engine"
	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestVerifyStorageGuardHostPathAloneCannotVerify(t *testing.T) {
	tmpDir := t.TempDir()
	musicDir := filepath.Join(tmpDir, "music")
	_ = os.MkdirAll(musicDir, 0755)

	guardID := "secret-guard-uuid-12345"
	markerFile := filepath.Join(musicDir, ".ytmdl-storage-id")

	// Host path with valid marker alone must NEVER produce GuardStatusVerified
	_ = os.WriteFile(markerFile, []byte("ytmdl-storage:secret-guard-uuid-12345\n"), 0644)
	status, err := discovery.VerifyStorageGuard(context.Background(), nil, "", "", musicDir, guardID)
	if status != discovery.GuardStatusUnavailable {
		t.Errorf("status = %s, want unavailable (host path cannot verify without container probe)", status)
	}

	// Disabled if expected guardID is empty
	status, err = discovery.VerifyStorageGuard(context.Background(), nil, "", "", musicDir, "")
	if status != discovery.GuardStatusDisabled || err != nil {
		t.Errorf("status = %s, want disabled (err: %v)", status, err)
	}
}

func TestVerifyStorageGuardContainerProbePrivacy(t *testing.T) {
	fake := runner.NewFake()
	guardID := "sensitive-guard-id-xyz"

	// Register fake container probe call
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "backend",
		"sh", "-c", discovery.StaticStorageGuardScript,
	}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte(""),
	}, nil)

	eng := engine.NewDocker(fake)
	status, err := discovery.VerifyStorageGuardViaContainer(context.Background(), eng, ".", "compose.yaml", guardID)
	if status != discovery.GuardStatusVerified || err != nil {
		t.Fatalf("container probe failed: status = %s, err = %v", status, err)
	}

	// Verify privacy: guard ID must NOT appear in recorded call arguments or executable
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	for _, arg := range calls[0].Args {
		if strings.Contains(arg, guardID) {
			t.Errorf("SECURITY FAILURE: guardID was passed in argv: %q", arg)
		}
	}
}

func TestStaticScriptDoesNotInterpolateUserInputs(t *testing.T) {
	// The static script must be a constant
	script := discovery.StaticStorageGuardScript
	if strings.Contains(script, "%s") || strings.Contains(script, "%v") {
		t.Error("StaticStorageGuardScript contains formatting verbs!")
	}
	if !strings.Contains(script, "read -r EXPECTED") {
		t.Error("StaticStorageGuardScript must read EXPECTED from stdin")
	}
}

func TestContainerProbeAuthoritativeOverHostPath(t *testing.T) {
	tmpDir := t.TempDir()
	musicDir := filepath.Join(tmpDir, "music")
	_ = os.MkdirAll(musicDir, 0755)
	guardID := "test-guard-uuid"

	// Create valid marker on host
	_ = os.WriteFile(filepath.Join(musicDir, ".ytmdl-storage-id"), []byte("ytmdl-storage:"+guardID+"\n"), 0644)

	// Case 1: Host path exists, but container probe returns missing (exit code 2)
	fake := runner.NewFake()
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "backend",
		"sh", "-c", discovery.StaticStorageGuardScript,
	}, &runner.RunResult{ExitCode: 2, Stderr: []byte("marker missing inside container")}, nil)

	eng := engine.NewDocker(fake)
	status, err := discovery.VerifyStorageGuard(context.Background(), eng, ".", "compose.yaml", musicDir, guardID)
	if status != discovery.GuardStatusMissing {
		t.Fatalf("expected GuardStatusMissing despite valid host path, got: %s (err: %v)", status, err)
	}

	// Case 2: Host path exists, but container probe returns mismatch (exit code 3)
	fake = runner.NewFake()
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "backend",
		"sh", "-c", discovery.StaticStorageGuardScript,
	}, &runner.RunResult{ExitCode: 3, Stderr: []byte("marker mismatch inside container")}, nil)

	eng = engine.NewDocker(fake)
	status, err = discovery.VerifyStorageGuard(context.Background(), eng, ".", "compose.yaml", musicDir, guardID)
	if status != discovery.GuardStatusMismatch {
		t.Fatalf("expected GuardStatusMismatch despite valid host path, got: %s (err: %v)", status, err)
	}

	// Case 3: Host path unavailable on macOS/Podman, but container probe verifies (exit code 0)
	fake = runner.NewFake()
	fake.Register("docker", []string{
		"compose", "-f", "compose.yaml", "exec", "-T", "backend",
		"sh", "-c", discovery.StaticStorageGuardScript,
	}, &runner.RunResult{ExitCode: 0}, nil)

	eng = engine.NewDocker(fake)
	nonExistentHostPath := filepath.Join(tmpDir, "non-existent-share")
	status, err = discovery.VerifyStorageGuard(context.Background(), eng, ".", "compose.yaml", nonExistentHostPath, guardID)
	if status != discovery.GuardStatusVerified || err != nil {
		t.Fatalf("expected GuardStatusVerified when container verifies despite missing host path, got: %s (err: %v)", status, err)
	}
}

func TestHostileGuardValuesSafety(t *testing.T) {
	hostileValues := []string{
		"$(id)",
		"; touch /tmp/pwned",
		"`whoami`",
		"test'; rm -rf /; '",
		"white space and \n newlines \r\n",
		"\"quoted$value\"",
	}

	for _, hostile := range hostileValues {
		fake := runner.NewFake()
		fake.Register("docker", []string{
			"compose", "-f", "compose.yaml", "exec", "-T", "backend",
			"sh", "-c", discovery.StaticStorageGuardScript,
		}, &runner.RunResult{ExitCode: 0}, nil)

		eng := engine.NewDocker(fake)
		status, err := discovery.VerifyStorageGuardViaContainer(context.Background(), eng, ".", "compose.yaml", hostile)
		if status != discovery.GuardStatusVerified || err != nil {
			t.Errorf("hostile value %q failed: status = %s, err = %v", hostile, status, err)
		}

		// Ensure hostile string NEVER appears in arguments
		for _, call := range fake.Calls() {
			for _, arg := range call.Args {
				if strings.Contains(arg, hostile) {
					t.Errorf("SECURITY LEAK: hostile guard value appeared in argv: %q", arg)
				}
			}
		}
	}
}
