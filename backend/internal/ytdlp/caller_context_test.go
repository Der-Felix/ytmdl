package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

// countingGate is an exclusive slot that records every grant and release.
type countingGate struct {
	sem chan struct{}

	mu       sync.Mutex
	granted  int
	released int
}

func newCountingGate(busyFor time.Duration) *countingGate {
	g := &countingGate{sem: make(chan struct{}, 1)}
	if busyFor > 0 {
		time.AfterFunc(busyFor, func() { g.sem <- struct{}{} })
	} else {
		g.sem <- struct{}{}
	}
	return g
}

func (g *countingGate) Acquire(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.sem:
	}
	g.mu.Lock()
	g.granted++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.released++
			g.mu.Unlock()
			g.sem <- struct{}{}
		})
	}, nil
}

func (g *countingGate) counts() (int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.granted, g.released
}

// stalledTool starts, records its process id and then never finishes.
func stalledTool(t *testing.T) (binary, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(t.TempDir(), "pid")
	return fakeBinary(t, "echo $$ > '"+pidFile+"'\nsleep 30\n"), pidFile
}

// requireProcessEnded fails unless the process the stub recorded is gone.
func requireProcessEnded(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the tool never started: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}

type callerEnd struct {
	name string
	ctx  func() (context.Context, context.CancelFunc)
	want error
}

func callerEnds(after time.Duration) []callerEnd {
	return []callerEnd{
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), after)
		}, context.DeadlineExceeded},
		{"cancelled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(after, cancel)
			return ctx, cancel
		}, context.Canceled},
	}
}

func requireCallerEnd(t *testing.T, err, want error) {
	t.Helper()
	if apperr.CodeOf(err) != apperr.CodeJobCancelled || !errors.Is(err, want) {
		t.Fatalf("err = %v, want JOB_CANCELLED carrying %v", err, want)
	}
	if apperr.ScopeOf(err) == apperr.ScopeProvider || apperr.StopsCandidateFanout(err) {
		t.Fatalf("the caller's end was classified as scope %s", apperr.ScopeOf(err))
	}
}

// The caller's deadline or cancellation while a query or a download runs ends
// the process, gives the slot back once, and is reported as the caller's end -
// never as a provider condition.
func TestCallerEndWhileRunning(t *testing.T) {
	for _, tc := range callerEnds(300 * time.Millisecond) {
		t.Run("query "+tc.name, func(t *testing.T) {
			binary, pidFile := stalledTool(t)
			gate := newCountingGate(0)
			client := New(Options{Binary: binary, Timeout: time.Minute, DisableQueryCache: true}).WithExecutionGate(gate)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := client.Query(ctx, "ytsearch1:x")
			requireCallerEnd(t, err, tc.want)
			requireProcessEnded(t, pidFile)
			if granted, released := gate.counts(); granted != 1 || released != 1 {
				t.Fatalf("slot granted %d, released %d", granted, released)
			}
		})
		t.Run("download "+tc.name, func(t *testing.T) {
			binary, pidFile := stalledTool(t)
			gate := newCountingGate(0)
			client := New(Options{Binary: binary}).WithExecutionGate(gate)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := client.Download(ctx, DownloadRequest{URL: "https://www.youtube.com/watch?v=x", Dir: t.TempDir()}, nil)
			requireCallerEnd(t, err, tc.want)
			requireProcessEnded(t, pidFile)
			if granted, released := gate.counts(); granted != 1 || released != 1 {
				t.Fatalf("slot granted %d, released %d", granted, released)
			}
		})
	}
}

// The caller's deadline or cancellation while waiting for a busy slot is the
// caller's end as well; nothing starts and nothing is left to release.
func TestCallerEndWhileWaitingForTheSlot(t *testing.T) {
	for _, tc := range callerEnds(200 * time.Millisecond) {
		t.Run("query "+tc.name, func(t *testing.T) {
			binary := fakeBinary(t, "exit 0\n")
			gate := newCountingGate(time.Hour)
			client := New(Options{Binary: binary, Timeout: time.Minute, DisableQueryCache: true}).WithExecutionGate(gate)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := client.Query(ctx, "ytsearch1:x")
			requireCallerEnd(t, err, tc.want)
			if starts := processStarts(t, binary); starts != 0 {
				t.Fatalf("started %d processes without a slot", starts)
			}
			if granted, released := gate.counts(); granted != 0 || released != 0 {
				t.Fatalf("slot granted %d, released %d", granted, released)
			}
		})
		t.Run("download "+tc.name, func(t *testing.T) {
			binary := fakeBinary(t, "exit 0\n")
			gate := newCountingGate(time.Hour)
			client := New(Options{Binary: binary}).WithExecutionGate(gate)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := client.Download(ctx, DownloadRequest{URL: "https://www.youtube.com/watch?v=x", Dir: t.TempDir()}, nil)
			requireCallerEnd(t, err, tc.want)
			if starts := processStarts(t, binary); starts != 0 {
				t.Fatalf("started %d processes without a slot", starts)
			}
		})
	}
}

// A query that outruns its own timeout while the caller is still waiting stays
// a provider condition, exactly as before; the slot is still given back.
func TestQueryTimeoutOfItsOwnStaysAProviderCondition(t *testing.T) {
	binary, pidFile := stalledTool(t)
	gate := newCountingGate(0)
	client := New(Options{Binary: binary, Timeout: 300 * time.Millisecond, DisableQueryCache: true}).WithExecutionGate(gate)

	_, err := client.Query(context.Background(), "ytsearch1:x")
	if apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("err = %v, want PROVIDER_UNAVAILABLE", err)
	}
	requireProcessEnded(t, pidFile)
	if granted, released := gate.counts(); granted != 1 || released != 1 {
		t.Fatalf("slot granted %d, released %d", granted, released)
	}
}

// A cached query shared by two callers: when one caller gives up, the other
// is not handed that caller's cancellation as its own answer.
func TestCallerEndDoesNotLeakIntoASharedQuery(t *testing.T) {
	binary := fakeBinary(t, "sleep 1\n"+videoJSON+"\n")
	client := New(Options{Binary: binary, Timeout: time.Minute})

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()
	leaderDone := make(chan error, 1)
	go func() {
		_, err := client.Query(leaderCtx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		leaderDone <- err
	}()
	waitForStarts(t, binary, 1)

	followerDone := make(chan error, 1)
	go func() {
		_, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		followerDone <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancelLeader()

	requireCallerEnd(t, <-leaderDone, context.Canceled)
	if err := <-followerDone; err != nil {
		t.Fatalf("the follower inherited the leader's end: %v", err)
	}
}
