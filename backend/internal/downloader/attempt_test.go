package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// slotGate is an exclusive execution slot like a media session's: one holder
// at a time, and every grant has to be given back exactly once.
type slotGate struct {
	sem chan struct{}

	mu       sync.Mutex
	granted  int
	released int
}

func newSlotGate() *slotGate {
	g := &slotGate{sem: make(chan struct{}, 1)}
	g.sem <- struct{}{}
	return g
}

func (g *slotGate) Acquire(ctx context.Context) (func(), error) {
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

// occupy holds the slot on behalf of another download for d.
func (g *slotGate) occupy(t *testing.T, d time.Duration) {
	t.Helper()
	release, err := g.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(d, release)
}

// requireFree fails unless every grant was given back and the slot can be
// taken right away.
func (g *slotGate) requireFree(t *testing.T) {
	t.Helper()
	g.mu.Lock()
	granted, released := g.granted, g.released
	g.mu.Unlock()
	if granted != released {
		t.Fatalf("slot granted %d times but released %d times", granted, released)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := g.Acquire(ctx)
	if err != nil {
		t.Fatalf("the slot is still held after the download returned: %v", err)
	}
	release()
}

type attemptCase struct {
	script   string // yt-dlp stub body; OUT is the -o template directory
	formats  []provider.AudioFormat
	fallback bool
	maxBytes int64
	timeout  time.Duration
	gate     *slotGate
	ctx      context.Context
	// preExisting files are copied into the work directory before the attempt.
	preExisting map[string]string
}

type attemptOutcome struct {
	result  *Result
	err     error
	workDir string
	started bool
	elapsed time.Duration
}

// runAttempt drives the real Download against an offline yt-dlp stub. The
// stub sees the output directory as $OUT and records that it ran.
func runAttempt(t *testing.T, tc attemptCase) attemptOutcome {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	stub := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\ntouch '" + marker + "'\n" + `
OUT=""
prev=""
for arg do
 if [ "$prev" = "-o" ]; then OUT="${arg%/source.*}"; fi
 prev="$arg"
done
` + tc.script + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		YTDLP:                 ytdlp.New(ytdlp.Options{Binary: stub}),
		FFmpeg:                ffmpeg.New("ffmpeg", time.Minute),
		Prober:                NewProber(ProberOptions{Binary: "ffprobe", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
		CombinedAudioFallback: tc.fallback,
		CombinedMaxBytes:      tc.maxBytes,
		CombinedTimeout:       tc.timeout,
		Logger:                slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	if tc.gate != nil {
		gate := tc.gate
		opts.ExecutionGateResolver = func(string) ytdlp.ExecutionGate { return gate }
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for name, from := range tc.preExisting {
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := tc.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	began := time.Now()
	res, err := d.Download(ctx, provider.MediaSource{
		Provider: "youtube", ID: "attemptVid", URL: "https://www.youtube.com/watch?v=attemptVid",
		DurationMS: 3000, Formats: tc.formats, SessionID: "sess-1",
	}, filepath.Join(work, "track.opus"), nil)
	_, statErr := os.Stat(marker)
	return attemptOutcome{result: res, err: err, workDir: work, started: statErr == nil, elapsed: time.Since(began)}
}

func audioOnlyFormat() []provider.AudioFormat {
	return []provider.AudioFormat{{ID: "140", Codec: "mp4a.40.2", BitrateKbps: 128}}
}

func oldAudio(t *testing.T) string {
	return makeMedia(t, "old.m4a",
		"-f", "lavfi", "-i", "sine=frequency=220:sample_rate=44100", "-t", "3", "-c:a", "aac")
}

// copyTo writes fixture as OUT/source.<ext> the way yt-dlp names its output.
func copyTo(fixture, ext string) string {
	return "cp '" + fixture + "' \"$OUT/source." + ext + "\""
}

// requireOnly fails unless the work directory holds exactly these names.
func requireOnly(t *testing.T, dir string, want ...string) {
	t.Helper()
	got := remaining(t, dir)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("work directory holds %v, want %v", got, want)
	}
}

// Waiting for a session slot that another download holds is bounded by the
// caller's context only; the transfer budget starts once the slot is granted.
func TestWaitingForTheSessionSlotDoesNotConsumeTheTransferBudget(t *testing.T) {
	requireMediaTools(t)
	gate := newSlotGate()
	gate.occupy(t, 900*time.Millisecond)
	out := runAttempt(t, attemptCase{
		script:   copyTo(muxedAAC(t), "mp4"),
		formats:  []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
		fallback: true, timeout: 400 * time.Millisecond, gate: gate,
	})
	if out.err != nil {
		t.Fatalf("a download that waited longer than its budget for the slot was stopped: %v", out.err)
	}
	if out.elapsed < 900*time.Millisecond {
		t.Fatalf("elapsed %v: the download did not wait for the slot", out.elapsed)
	}
	if out.result.FormatKind != "combined" || out.result.Plan != PlanExtractAudio {
		t.Fatalf("result = %+v", out.result)
	}
	gate.requireFree(t)
}

// Once the slot is held, the budget is binding.
func TestTransferBudgetRunsFromTheGrantedSlot(t *testing.T) {
	requireMediaTools(t)
	gate := newSlotGate()
	out := runAttempt(t, attemptCase{
		script:   "sleep 5\n" + copyTo(muxedAAC(t), "mp4"),
		formats:  []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
		fallback: true, timeout: 300 * time.Millisecond, gate: gate,
	})
	requireLocalBudgetStop(t, out.err)
	if out.elapsed > 4*time.Second {
		t.Fatalf("elapsed %v: the budget did not stop the transfer", out.elapsed)
	}
	requireOnly(t, out.workDir)
	gate.requireFree(t)
}

// A caller who gives up while the download waits for the slot gets its own
// cancellation or deadline back - never a budget stop - and the tool never
// starts.
func TestGivingUpWhileWaitingForTheSlotIsNotABudgetStop(t *testing.T) {
	requireMediaTools(t)
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"cancelled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(150*time.Millisecond, cancel)
			return ctx, cancel
		}, context.Canceled},
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 150*time.Millisecond)
		}, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := newSlotGate()
			gate.occupy(t, 2*time.Second)
			ctx, cancel := tc.ctx()
			defer cancel()
			out := runAttempt(t, attemptCase{
				script:   copyTo(muxedAAC(t), "mp4"),
				formats:  []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
				fallback: true, timeout: 10 * time.Second, gate: gate, ctx: ctx,
			})
			if apperr.CodeOf(out.err) != apperr.CodeJobCancelled || !errors.Is(out.err, tc.want) {
				t.Fatalf("err = %v, want JOB_CANCELLED carrying %v", out.err, tc.want)
			}
			if out.started {
				t.Fatal("yt-dlp started although the slot was never granted")
			}
			requireOnly(t, out.workDir)
			time.Sleep(2100 * time.Millisecond) // the other holder gives the slot back
			gate.requireFree(t)
		})
	}
}

// A clean exit without new output is no success, and an older audio file in
// the work directory - even one named like the target or like yt-dlp's own
// output - is never taken as this candidate's result. It stays untouched.
func TestOldAudioAndCleanExitWithoutOutputIsNotASuccess(t *testing.T) {
	requireMediaTools(t)
	old := oldAudio(t)
	for _, tc := range []struct {
		name     string
		formats  []provider.AudioFormat
		fallback bool
		script   string
		code     apperr.Code
	}{
		{"audio only", audioOnlyFormat(), false, "exit 0", apperr.CodeDownloadFailed},
		{"combined", []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)}, true, "exit 0", apperr.CodeDownloadFailed},
		{"combined refused by size", []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)}, true,
			"printf '[download] File is larger than max-filesize (9999 bytes > 1024 bytes). Aborting.\\n'\nexit 0",
			apperr.CodeTransferBudgetExceeded},
		{"only a partial file", audioOnlyFormat(), false, "printf 'x' > \"$OUT/source.m4a.part\"\nexit 0", apperr.CodeDownloadFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := newSlotGate()
			out := runAttempt(t, attemptCase{
				script: tc.script, formats: tc.formats, fallback: tc.fallback, maxBytes: 1024, gate: gate,
				preExisting: map[string]string{"track.m4a": old, "source.m4a": old},
			})
			if out.result != nil {
				t.Fatalf("an old file was accepted as this attempt's result: %s", out.result.Path)
			}
			if apperr.CodeOf(out.err) != tc.code {
				t.Fatalf("err = %v, want %s", out.err, tc.code)
			}
			if tc.code == apperr.CodeTransferBudgetExceeded {
				requireLocalBudgetStop(t, out.err)
			}
			requireOnly(t, out.workDir, "source.m4a", "track.m4a")
			want, _ := os.ReadFile(old)
			for _, name := range []string{"source.m4a", "track.m4a"} {
				got, err := os.ReadFile(filepath.Join(out.workDir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("%s was modified by the failed attempt", name)
				}
			}
			gate.requireFree(t)
		})
	}
}

// A successful attempt replaces an older file of the same name, and an attempt
// directory an interrupted process left behind is removed.
func TestSuccessfulAttemptReplacesOldFilesAndClearsStaleAttempts(t *testing.T) {
	requireMediaTools(t)
	fresh := makeMedia(t, "fresh.m4a",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=44100", "-t", "3", "-c:a", "aac")
	out := runAttempt(t, attemptCase{
		script:      copyTo(fresh, "m4a"),
		formats:     audioOnlyFormat(),
		gate:        newSlotGate(),
		preExisting: map[string]string{"track.m4a": oldAudio(t)},
	})
	if out.err != nil {
		t.Fatal(out.err)
	}
	if filepath.Base(out.result.Path) != "track.m4a" {
		t.Fatalf("path = %s", out.result.Path)
	}
	if audioStreamMD5(t, out.result.Path) != audioStreamMD5(t, fresh) {
		t.Fatal("the stored file is not the one this attempt downloaded")
	}
	requireOnly(t, out.workDir, "track.m4a")

	stale := filepath.Join(out.workDir, attemptDirPrefix+"interrupted")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "source.webm.part"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeStaleAttempts(out.workDir)
	requireOnly(t, out.workDir, "track.m4a")
}

// Every way a download can end gives the session slot back exactly once.
func TestSessionSlotIsReleasedOnEveryOutcome(t *testing.T) {
	requireMediaTools(t)
	combined := []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)}
	muxed := muxedAAC(t)
	for _, tc := range []struct {
		name     string
		script   string
		formats  []provider.AudioFormat
		fallback bool
		maxBytes int64
		timeout  time.Duration
		cancelIn time.Duration
		wantErr  bool
	}{
		{name: "success", script: copyTo(muxed, "mp4"), formats: combined, fallback: true},
		{name: "tool failure", script: "echo 'ERROR: something broke' >&2\nexit 1", formats: combined, fallback: true, wantErr: true},
		{name: "clean exit without output", script: "exit 0", formats: audioOnlyFormat(), wantErr: true},
		{name: "size refusal", script: "echo 'File is larger than max-filesize'\nexit 0", formats: combined, fallback: true, maxBytes: 1024, wantErr: true},
		{name: "byte budget while running", script: "printf '@YTDM-PROGRESS@4096|8192|NA|1000|1\\n'\nsleep 5",
			formats: combined, fallback: true, maxBytes: 1024, wantErr: true},
		{name: "byte budget after arrival", script: copyTo(muxed, "mp4"), formats: combined, fallback: true, maxBytes: 1024, wantErr: true},
		{name: "time budget", script: "sleep 5", formats: combined, fallback: true, timeout: 200 * time.Millisecond, wantErr: true},
		{name: "caller cancels while running", script: "sleep 5", formats: combined, fallback: true, cancelIn: 200 * time.Millisecond, wantErr: true},
		{name: "verification failure", script: copyTo(muxed, "mp4"), formats: audioOnlyFormat(), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelIn > 0 {
				time.AfterFunc(tc.cancelIn, cancel)
			}
			gate := newSlotGate()
			out := runAttempt(t, attemptCase{
				script: tc.script, formats: tc.formats, fallback: tc.fallback,
				maxBytes: tc.maxBytes, timeout: tc.timeout, gate: gate, ctx: ctx,
			})
			if (out.err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", out.err, tc.wantErr)
			}
			if !out.started {
				t.Fatal("the stub never ran; the case does not test a held slot")
			}
			gate.requireFree(t)
			if tc.wantErr {
				requireOnly(t, out.workDir)
			}
		})
	}
}
