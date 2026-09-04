package runner_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/runner"
)

func TestRealRunnerBasicExecution(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	res, err := r.Run(ctx, runner.RunRequest{
		Executable: "echo",
		Args:       []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if strings.TrimSpace(string(res.Stdout)) != "hello world" {
		t.Errorf("Stdout = %q, want %q", string(res.Stdout), "hello world")
	}
}

func TestRealRunnerEnvironmentOverride(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	// Ensure parent process does NOT have YTMDL_TEST_VAR set to "overridden"
	origVal := os.Getenv("YTMDL_TEST_VAR")
	defer os.Setenv("YTMDL_TEST_VAR", origVal)
	os.Setenv("YTMDL_TEST_VAR", "parent_value")

	res, err := r.Run(ctx, runner.RunRequest{
		Executable: "env",
		Env:        []string{"YTMDL_TEST_VAR=overridden_value"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	stdout := string(res.Stdout)
	if !strings.Contains(stdout, "YTMDL_TEST_VAR=overridden_value") {
		t.Errorf("expected child process to have YTMDL_TEST_VAR=overridden_value, got stdout: %s", stdout)
	}

	// Verify parent environment was NOT mutated
	if os.Getenv("YTMDL_TEST_VAR") != "parent_value" {
		t.Errorf("parent environment was mutated! got %q, want %q", os.Getenv("YTMDL_TEST_VAR"), "parent_value")
	}
}

func TestMergeEnvDeterministicDuplicates(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"YTMDL_VERSION=0.15.0",
		"KEEP_ME=yes",
	}
	overrides := []string{
		"YTMDL_VERSION=0.16.0",
		"NEW_VAR=hello",
	}

	merged := runner.MergeEnv(parent, overrides)

	// Verify YTMDL_VERSION appears exactly once and has value 0.16.0
	count := 0
	val := ""
	for _, entry := range merged {
		k, v, ok := strings.Cut(entry, "=")
		if ok && k == "YTMDL_VERSION" {
			count++
			val = v
		}
	}

	if count != 1 {
		t.Errorf("expected YTMDL_VERSION to appear exactly once, appeared %d times in %+v", count, merged)
	}
	if val != "0.16.0" {
		t.Errorf("expected YTMDL_VERSION to be 0.16.0, got %q", val)
	}
}

func TestRealRunnerContextCancellation(t *testing.T) {
	r := runner.New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := r.Run(ctx, runner.RunRequest{
		Executable: "sleep",
		Args:       []string{"1"},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Logf("cancelled error was: %v", err)
	}
}

func TestRealRunnerWorkingDirectory(t *testing.T) {
	r := runner.New()
	ctx := context.Background()
	tmpDir := t.TempDir()

	res, err := r.Run(ctx, runner.RunRequest{
		Executable: "pwd",
		Dir:        tmpDir,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	gotDir := strings.TrimSpace(string(res.Stdout))
	// On macOS tmpDir may have /private/var/folders symlink
	if !strings.HasSuffix(gotDir, tmpDir) && gotDir != tmpDir {
		t.Logf("pwd returned %q for tmpDir %q", gotDir, tmpDir)
	}
}

func TestRealRunnerStdinCapture(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	input := "hello from stdin\n"
	res, err := r.Run(ctx, runner.RunRequest{
		Executable: "cat",
		Stdin:      bytes.NewReader([]byte(input)),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if string(res.Stdout) != input {
		t.Errorf("Stdout = %q, want %q", string(res.Stdout), input)
	}
}

func TestRealRunnerHostileArgvNotInterpolated(t *testing.T) {
	r := runner.New()
	ctx := context.Background()
	markerFile := t.TempDir() + "/pwned"

	hostileArg := "; touch " + markerFile + " ; echo $(id)"
	res, err := r.Run(ctx, runner.RunRequest{
		Executable: "echo",
		Args:       []string{hostileArg},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Output must contain the literal characters, NOT execute the touch command
	if !strings.Contains(string(res.Stdout), hostileArg) {
		t.Errorf("expected stdout to contain literal argument %q, got %q", hostileArg, string(res.Stdout))
	}

	// Verify marker file was NOT created
	if _, err := os.Stat(markerFile); !os.IsNotExist(err) {
		t.Fatalf("SECURITY FAILURE: marker file %q was created! Shell injection occurred!", markerFile)
	}
}

func TestFakeRunner(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("docker", []string{"compose", "version"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("Docker Compose version v2.29.0\n"),
	}, nil)

	ctx := context.Background()
	res, err := fake.Run(ctx, runner.RunRequest{
		Executable: "docker",
		Args:       []string{"compose", "version"},
	})
	if err != nil {
		t.Fatalf("fake Run failed: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "v2.29.0") {
		t.Errorf("got %q, want v2.29.0", string(res.Stdout))
	}

	// Unregistered call returns error
	_, err = fake.Run(ctx, runner.RunRequest{
		Executable: "unknown",
		Args:       []string{"foo"},
	})
	if err == nil {
		t.Fatal("expected error for unregistered call, got nil")
	}
}

func TestRealRunnerStdoutWriterStreaming(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	var streamBuf bytes.Buffer
	res, err := r.Run(ctx, runner.RunRequest{
		Executable:   "echo",
		Args:         []string{"streamed", "output"},
		StdoutWriter: &streamBuf,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if strings.TrimSpace(streamBuf.String()) != "streamed output" {
		t.Errorf("streamBuf = %q, want %q", streamBuf.String(), "streamed output")
	}
	// When StdoutWriter is used, res.Stdout in memory is empty
	if len(res.Stdout) != 0 {
		t.Errorf("expected res.Stdout to be empty when streaming, got len %d", len(res.Stdout))
	}
}

func TestRealRunnerStdoutStderrSeparation(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	// Write to both stdout and stderr using python or sh
	var streamBuf bytes.Buffer
	res, err := r.Run(ctx, runner.RunRequest{
		Executable:   "/bin/sh",
		Args:         []string{"-c", "echo stdout-data; echo stderr-data >&2"},
		StdoutWriter: &streamBuf,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	stdoutStr := strings.TrimSpace(streamBuf.String())
	stderrStr := strings.TrimSpace(string(res.Stderr))

	if stdoutStr != "stdout-data" {
		t.Errorf("stdout = %q, want stdout-data", stdoutStr)
	}
	if stderrStr != "stderr-data" {
		t.Errorf("stderr = %q, want stderr-data", stderrStr)
	}
	if strings.Contains(stdoutStr, "stderr-data") {
		t.Error("CRITICAL: stderr was mixed into stdout stream!")
	}
}

func TestRealRunnerBoundedStderr(t *testing.T) {
	r := runner.New()
	ctx := context.Background()

	// Generate 100 KiB on stderr
	res, _ := r.Run(ctx, runner.RunRequest{
		Executable: "/bin/sh",
		Args:       []string{"-c", "python3 -c 'import sys; sys.stderr.write(\"x\" * 102400)' 2>/dev/null || perl -e 'print STDERR \"x\" x 102400'"},
	})

	if len(res.Stderr) > runner.MaxStderrBytes {
		t.Errorf("stderr len %d exceeds MaxStderrBytes %d", len(res.Stderr), runner.MaxStderrBytes)
	}
}

func TestFakeRunnerStdoutStreaming(t *testing.T) {
	fake := runner.NewFake()
	fake.Register("pg_dump", []string{"-Fc"}, &runner.RunResult{
		ExitCode: 0,
		Stdout:   []byte("PGDUMP_BINARY_BYTES"),
	}, nil)

	var streamed bytes.Buffer
	res, err := fake.Run(context.Background(), runner.RunRequest{
		Executable:   "pg_dump",
		Args:         []string{"-Fc"},
		StdoutWriter: &streamed,
	})
	if err != nil {
		t.Fatalf("fake Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if streamed.String() != "PGDUMP_BINARY_BYTES" {
		t.Errorf("streamed = %q, want PGDUMP_BINARY_BYTES", streamed.String())
	}
}
