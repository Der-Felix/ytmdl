package ytdlp

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

// delayedGate grants its slot after a fixed wait, the way a session busy with
// another download does.
type delayedGate struct {
	wait     time.Duration
	released chan struct{}
}

func (g *delayedGate) Acquire(ctx context.Context) (func(), error) {
	select {
	case <-time.After(g.wait):
		return func() { close(g.released) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// writeOutput is a stub body that writes source.m4a into the -o directory.
const writeOutput = `prev=""
for arg do
 if [ "$prev" = "-o" ]; then printf 'audio' > "${arg%/source.*}/source.m4a"; fi
 prev="$arg"
done
`

func download(t *testing.T, ctx context.Context, client *Client, timeout time.Duration) (string, error) {
	t.Helper()
	return client.Download(ctx, DownloadRequest{
		URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Dir: t.TempDir(), ProcessTimeout: timeout,
	}, nil)
}

// The process timeout starts once the slot is granted: a wait longer than the
// timeout does not stop a process that itself finishes in time.
func TestProcessTimeoutStartsAfterTheSlotIsGranted(t *testing.T) {
	gate := &delayedGate{wait: 600 * time.Millisecond, released: make(chan struct{})}
	client := New(Options{Binary: fakeBinary(t, writeOutput)}).WithExecutionGate(gate)

	path, err := download(t, context.Background(), client, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if path == "" {
		t.Fatal("no output path")
	}
	select {
	case <-gate.released:
	default:
		t.Fatal("the slot was not released")
	}
}

// A process that outruns its timeout while the caller is still waiting is a
// budget stop, not a cancellation, and the slot is given back.
func TestProcessTimeoutIsABudgetStop(t *testing.T) {
	gate := &delayedGate{released: make(chan struct{})}
	client := New(Options{Binary: fakeBinary(t, "sleep 5\n"+writeOutput)}).WithExecutionGate(gate)

	_, err := download(t, context.Background(), client, 200*time.Millisecond)
	if apperr.CodeOf(err) != apperr.CodeTransferBudgetExceeded || !errors.Is(err, ErrProcessTimeout) {
		t.Fatalf("err = %v, want a process timeout", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("the budget stop carries a context error: %v", err)
	}
	select {
	case <-gate.released:
	default:
		t.Fatal("the slot was not released")
	}
}

// Giving up while waiting for the slot is reported as the caller's own
// cancellation or deadline, and the tool never starts.
func TestGivingUpWhileWaitingForTheSlot(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"cancelled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(100*time.Millisecond, cancel)
			return ctx, cancel
		}, context.Canceled},
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 100*time.Millisecond)
		}, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := fakeBinary(t, writeOutput)
			gate := &delayedGate{wait: time.Hour, released: make(chan struct{})}
			client := New(Options{Binary: binary}).WithExecutionGate(gate)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := download(t, ctx, client, time.Minute)
			if apperr.CodeOf(err) != apperr.CodeJobCancelled || !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want JOB_CANCELLED carrying %v", err, tc.want)
			}
			if starts := processStarts(t, binary); starts != 0 {
				t.Fatalf("yt-dlp started %d times without a slot", starts)
			}
		})
	}
}

// yt-dlp refuses an oversized stream on standard output and still exits 0.
// That is a budget stop, not a success and not an unexplained failure.
func TestMaxFilesizeRefusalIsRecognised(t *testing.T) {
	client := New(Options{Binary: fakeBinary(t,
		"echo '[download] File is larger than max-filesize (9999 bytes > 1024 bytes). Aborting.'\nexit 0\n")})
	_, err := download(t, context.Background(), client, 0)
	if apperr.CodeOf(err) != apperr.CodeTransferBudgetExceeded || !errors.Is(err, ErrMaxFilesize) {
		t.Fatalf("err = %v, want the size refusal", err)
	}
}

// A clean exit without any output file is no success.
func TestCleanExitWithoutOutputIsAFailure(t *testing.T) {
	client := New(Options{Binary: fakeBinary(t, "exit 0\n")})
	path, err := download(t, context.Background(), client, 0)
	if path != "" || apperr.CodeOf(err) != apperr.CodeDownloadFailed {
		t.Fatalf("path = %q, err = %v", path, err)
	}
}
