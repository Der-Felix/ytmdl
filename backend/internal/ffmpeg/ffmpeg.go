// Package ffmpeg is the technical adapter around the ffmpeg binary. It only
// runs the process; what to run is decided by the downloader and the tagger.
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// Runner executes ffmpeg.
type Runner struct {
	binary  string
	timeout time.Duration
}

// New builds a Runner. An empty binary name falls back to "ffmpeg".
func New(binary string, timeout time.Duration) *Runner {
	b := strings.TrimSpace(binary)
	if b == "" {
		b = "ffmpeg"
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Runner{binary: b, timeout: timeout}
}

// Binary returns the configured executable.
func (r *Runner) Binary() string { return r.binary }

// Available reports whether ffmpeg can be executed.
func (r *Runner) Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if err := exec.CommandContext(ctx, r.binary, "-version").Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return apperr.Wrapf(apperr.CodeToolUnavailable, err, "ffmpeg was not found at %q.", r.binary)
		}
		return apperr.Wrap(apperr.CodeToolUnavailable, "ffmpeg could not be started.", err)
	}
	return nil
}

// Run executes ffmpeg with the given arguments. The standard flags that make
// the output usable in a service are added automatically.
func (r *Runner) Run(ctx context.Context, code apperr.Code, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	full := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)
	cmd := exec.CommandContext(ctx, r.binary, full...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return apperr.Wrapf(apperr.CodeToolUnavailable, err, "ffmpeg was not found at %q.", r.binary)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return apperr.Wrap(code, "The ffmpeg call did not finish in time.", ctxErr)
		}
		return apperr.Wrapf(code, err, "ffmpeg failed: %s", firstLine(stderr.String()))
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no error output"
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	const maxLen = 400
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}
