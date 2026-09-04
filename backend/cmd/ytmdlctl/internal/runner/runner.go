// Package runner provides safe subprocess execution using vector arguments only.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// MaxStderrBytes is the maximum number of stderr bytes retained in memory.
const MaxStderrBytes = 64 * 1024

// RunRequest encapsulates command execution parameters.
type RunRequest struct {
	Executable   string
	Args         []string
	Dir          string
	Env          []string
	Stdin        io.Reader
	StdoutWriter io.Writer // optional: stream stdout directly to Writer without accumulating in memory
	Timeout      time.Duration
}

// RunResult contains the captured result of execution.
type RunResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// ProcessRunner abstracts process execution.
type ProcessRunner interface {
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
}

// RealRunner executes commands using os/exec.CommandContext.
type RealRunner struct{}

// New creates a RealRunner.
func New() *RealRunner {
	return &RealRunner{}
}

type boundedBuffer struct {
	max int
	buf bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (n int, err error) {
	if b.buf.Len() >= b.max {
		return len(p), nil
	}
	remaining := b.max - b.buf.Len()
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

// Run executes a command safely without shell intervention.
func (r *RealRunner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if req.Executable == "" {
		return nil, errors.New("runner: executable cannot be empty")
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, req.Executable, req.Args...)
	cmd.Dir = req.Dir

	// Build environment with overrides without modifying parent process environment
	if len(req.Env) > 0 {
		baseEnv := os.Environ()
		cmd.Env = MergeEnv(baseEnv, req.Env)
	}

	var stdoutBuf bytes.Buffer
	if req.StdoutWriter != nil {
		cmd.Stdout = req.StdoutWriter
	} else {
		cmd.Stdout = &stdoutBuf
	}

	stderrBounded := &boundedBuffer{max: MaxStderrBytes}
	cmd.Stderr = stderrBounded

	if req.Stdin != nil {
		cmd.Stdin = req.Stdin
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	var stdoutBytes []byte
	if req.StdoutWriter == nil {
		stdoutBytes = stdoutBuf.Bytes()
	}

	res := &RunResult{
		ExitCode: exitCode,
		Stdout:   stdoutBytes,
		Stderr:   stderrBounded.Bytes(),
	}

	if err != nil {
		// Provide context in error while keeping stderr in result
		return res, fmt.Errorf("command %q failed (exit %d): %w", req.Executable, exitCode, err)
	}

	return res, nil
}

// MergeEnv merges overrides into parent environment deterministically.
// Duplicate keys in overrides replace parent values, and duplicate keys within
// either slice resolve to the latest occurrence.
func MergeEnv(parent []string, overrides []string) []string {
	envMap := make(map[string]string, len(parent)+len(overrides))
	order := make([]string, 0, len(parent)+len(overrides))

	for _, entry := range parent {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := envMap[k]; !exists {
				order = append(order, k)
			}
			envMap[k] = v
		}
	}

	for _, entry := range overrides {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := envMap[k]; !exists {
				order = append(order, k)
			}
			envMap[k] = v
		}
	}

	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+envMap[k])
	}
	return out
}

// FakeCall records an invocation made to FakeProcessRunner.
type FakeCall struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	StdinBytes []byte
}

// FakeProcessRunner provides deterministic testing without invoking real binaries.
type FakeProcessRunner struct {
	mu        sync.Mutex
	calls     []FakeCall
	responses map[string]*fakeResponse
}

type fakeResponse struct {
	result *RunResult
	err    error
}

// NewFake creates a FakeProcessRunner.
func NewFake() *FakeProcessRunner {
	return &FakeProcessRunner{
		responses: make(map[string]*fakeResponse),
	}
}

// Key builds a lookup key from executable and args.
func Key(executable string, args ...string) string {
	return executable + " " + strings.Join(args, " ")
}

// Register stubs a response for an exact command invocation.
func (f *FakeProcessRunner) Register(executable string, args []string, res *RunResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[Key(executable, args...)] = &fakeResponse{
		result: res,
		err:    err,
	}
}

// Run looks up registered response or returns an error.
func (f *FakeProcessRunner) Run(_ context.Context, req RunRequest) (*RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var stdinBytes []byte
	if req.Stdin != nil {
		stdinBytes, _ = io.ReadAll(req.Stdin)
	}

	f.calls = append(f.calls, FakeCall{
		Executable: req.Executable,
		Args:       req.Args,
		Dir:        req.Dir,
		Env:        req.Env,
		StdinBytes: stdinBytes,
	})

	key := Key(req.Executable, req.Args...)
	resp, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("fake runner: unexpected call %q", key)
	}

	if resp.result != nil && req.StdoutWriter != nil && len(resp.result.Stdout) > 0 {
		_, _ = req.StdoutWriter.Write(resp.result.Stdout)
	}

	if resp.err != nil {
		return resp.result, resp.err
	}
	return resp.result, nil
}

// Calls returns all recorded calls.
func (f *FakeProcessRunner) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := make([]FakeCall, len(f.calls))
	copy(copied, f.calls)
	return copied
}
