//go:build darwin || linux

package ytdlp

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCommandCancellationTerminatesSpawnedChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := New(Options{Binary: "/bin/sh"})
	cmd := client.command(ctx, "-c", `
trap '' TERM
sleep 300 &
child=$!
printf '%s\n' "$child"
wait "$child"
`)

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create inherited stdout pipe: %v", err)
	}
	cmd.Stdout = stdoutWriter

	if err := cmd.Start(); err != nil {
		stdoutReader.Close()
		stdoutWriter.Close()
		t.Fatalf("start process-group leader: %v", err)
	}
	if err := stdoutWriter.Close(); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Process.Kill()
		stdoutReader.Close()
		t.Fatalf("close parent copy of stdout pipe: %v", err)
	}

	childPID := 0
	childTerminated := false
	t.Cleanup(func() {
		stdoutReader.Close()
		if !childTerminated && childPID > 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	})

	reader := bufio.NewReader(stdoutReader)
	type lineResult struct {
		line string
		err  error
	}
	ready := make(chan lineResult, 1)
	go func() {
		line, readErr := reader.ReadString('\n')
		ready <- lineResult{line: line, err: readErr}
	}()

	select {
	case result := <-ready:
		if result.err != nil {
			t.Fatalf("read child pid: %v", result.err)
		}
		childPID, err = strconv.Atoi(strings.TrimSpace(result.line))
		if err != nil || childPID <= 0 {
			t.Fatalf("parse child pid %q: %v", result.line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the spawned child")
	}

	childPGID, err := syscall.Getpgid(childPID)
	if err != nil {
		t.Fatalf("read child process group: %v", err)
	}
	if childPGID != cmd.Process.Pid {
		t.Fatalf("child process group = %d, want leader pid %d", childPGID, cmd.Process.Pid)
	}

	// The child inherits this pipe. It can reach EOF only after both the shell
	// and its spawned child have exited, so this does not depend on zombie reaping.
	pipeClosed := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(io.Discard, reader)
		pipeClosed <- copyErr
	}()

	select {
	case err := <-pipeClosed:
		t.Fatalf("child released inherited pipe before cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("cancelled command exited without an error")
		}
	case <-time.After(processGroupGracePeriod + 5*time.Second):
		t.Fatal("timed out waiting for the cancelled process group")
	}

	select {
	case err := <-pipeClosed:
		if err != nil {
			t.Fatalf("read inherited pipe after cancellation: %v", err)
		}
		childTerminated = true
	case <-time.After(2 * time.Second):
		t.Fatal("spawned child survived process-group cancellation")
	}

	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("context error = %v, want %v", err, context.Canceled)
	}
}
