package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// combinedFormat is what the resolver hands over for a muxed stream.
func combinedFormat(id, codec string, size int64) provider.AudioFormat {
	return provider.AudioFormat{
		ID: id, Codec: codec, Combined: true, VideoCodec: "avc1.42001E",
		BitrateKbps: 96, TransferBitrateKbps: 500, Filesize: size,
	}
}

type combinedCase struct {
	file       string // fixture copied in as the "downloaded" stream
	ext        string // extension the stub writes it under
	formats    []provider.AudioFormat
	fallback   bool
	maxBytes   int64
	timeout    time.Duration
	durationMS int
	// stubPrelude runs inside the yt-dlp stub before it writes the file, so a
	// test can make the transfer slow or noisy.
	stubPrelude string
	// preExisting are files placed in the work directory before the attempt,
	// standing in for staging work the attempt must not touch.
	preExisting []string
}

type combinedOutcome struct {
	result   *Result
	err      error
	selector string
	workDir  string
	logs     string
}

// runCombined drives the real Download against an offline yt-dlp stub.
func runCombined(t *testing.T, tc combinedCase) combinedOutcome {
	t.Helper()
	selectorLog := filepath.Join(t.TempDir(), "selector")
	stub := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\n" + tc.stubPrelude + `
while [ "$#" -gt 0 ]; do
 case "$1" in
  -f) shift; printf '%s' "$1" > '` + selectorLog + `';;
  -o) shift; cp '` + tc.file + `' "${1%source.*}source.` + tc.ext + `";;
 esac
 shift
done
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	d, err := New(Options{
		YTDLP:                 ytdlp.New(ytdlp.Options{Binary: stub}),
		FFmpeg:                ffmpeg.New("ffmpeg", time.Minute),
		Prober:                NewProber(ProberOptions{Binary: "ffprobe", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
		CombinedAudioFallback: tc.fallback,
		CombinedMaxBytes:      tc.maxBytes,
		CombinedTimeout:       tc.timeout,
		Logger:                slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for _, name := range tc.preExisting {
		if err := os.WriteFile(filepath.Join(work, name), []byte("other work"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	duration := tc.durationMS
	if duration == 0 {
		duration = 3000
	}
	res, err := d.Download(context.Background(), provider.MediaSource{
		Provider: "youtube", ID: "combinedVid", URL: "https://www.youtube.com/watch?v=combinedVid",
		DurationMS: duration, Formats: tc.formats, SessionID: "sess-1",
	}, filepath.Join(work, "track.opus"), nil)
	selector, _ := os.ReadFile(selectorLog)
	return combinedOutcome{result: res, err: err, selector: string(selector), workDir: work, logs: logs.String()}
}

// remaining lists the files left in the work directory.
func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// audioStreamMD5 is the checksum of the audio packets alone, taken through a
// stream copy. Two files whose audio was copied rather than re-encoded share
// it even when their containers differ.
func audioStreamMD5(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-map", "0:a:0", "-c:a", "copy", "-f", "md5", "-").Output()
	if err != nil {
		t.Fatalf("ffmpeg md5 of %s: %v", filepath.Base(path), err)
	}
	return strings.TrimSpace(string(out))
}

func muxedAAC(t *testing.T) string {
	return makeMedia(t, "muxed_aac.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "3", "-c:v", "mpeg4", "-c:a", "aac", "-b:a", "96k")
}

func muxedOpus(t *testing.T) string {
	return makeMedia(t, "muxed_opus.webm",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "3", "-c:v", "libvpx", "-c:a", "libopus", "-b:a", "96k")
}

// The measured production shape: no audio only stream, one combined stream.
// Its audio is copied out and stored; the video never reaches the library.
func TestCombinedStreamYieldsExtractedAudio(t *testing.T) {
	requireMediaTools(t)
	source := muxedAAC(t)
	out := runCombined(t, combinedCase{
		file: source, ext: "mp4", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if out.err != nil {
		t.Fatalf("combined acquisition failed: %v", out.err)
	}
	if out.result.Plan != PlanExtractAudio || out.result.FormatKind != "combined" {
		t.Fatalf("result = %+v", out.result)
	}
	if filepath.Ext(out.result.Path) != ".m4a" || out.result.Info.Codec != "aac" {
		t.Fatalf("stored %s as %s", out.result.Info.Codec, filepath.Base(out.result.Path))
	}
	if out.result.Info.VideoStreams != 0 {
		t.Fatalf("a video stream survived into the library file: %q", streamTypes(t, out.result.Path))
	}
	if !strings.HasPrefix(out.selector, "18/") {
		t.Fatalf("selector %q did not address the judged format by id", out.selector)
	}
	// Nothing but the finished audio may remain; the combined stream itself is
	// never published as a music file.
	if names := remaining(t, out.workDir); len(names) != 1 || names[0] != filepath.Base(out.result.Path) {
		t.Fatalf("work directory holds %v", names)
	}
	if out.result.TransferredBytes <= 0 {
		t.Fatal("the transferred size was not measured")
	}
}

// Stream copy means the audio packets are moved, not re-encoded. The container
// changes; the packets do not.
func TestExtractedAudioPacketsAreUnchanged(t *testing.T) {
	requireMediaTools(t)
	source := muxedAAC(t)
	want := audioStreamMD5(t, source)
	out := runCombined(t, combinedCase{
		file: source, ext: "mp4", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if out.err != nil {
		t.Fatalf("combined acquisition failed: %v", out.err)
	}
	if got := audioStreamMD5(t, out.result.Path); got != want {
		t.Fatalf("audio packets changed: %s != %s", got, want)
	}
}

// Opus goes into an Ogg container, not into the MP4 the combined stream used.
// A codec is never renamed into a container that cannot hold it.
func TestExtractedOpusGetsACompatibleContainer(t *testing.T) {
	requireMediaTools(t)
	source := muxedOpus(t)
	want := audioStreamMD5(t, source)
	out := runCombined(t, combinedCase{
		file: source, ext: "webm", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("43", "opus", 0)},
	})
	if out.err != nil {
		t.Fatalf("combined acquisition failed: %v", out.err)
	}
	if filepath.Ext(out.result.Path) != ".opus" || out.result.Info.Codec != "opus" {
		t.Fatalf("stored %s as %s", out.result.Info.Codec, filepath.Base(out.result.Path))
	}
	if out.result.Info.Container != "ogg" {
		t.Fatalf("container = %q, want ogg", out.result.Info.Container)
	}
	if got := audioStreamMD5(t, out.result.Path); got != want {
		t.Fatalf("audio packets changed: %s != %s", got, want)
	}
	if !out.result.NativeOpus {
		t.Fatal("extracted native Opus was not reported as native")
	}
}

// An audio codec this backend cannot store without re-encoding is rejected
// rather than re-encoded or renamed.
func TestCombinedStreamWithUncopyableCodecIsRejected(t *testing.T) {
	requireMediaTools(t)
	source := makeMedia(t, "muxed_ac3.mkv",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "3", "-c:v", "mpeg4", "-c:a", "ac3", "-b:a", "128k")
	out := runCombined(t, combinedCase{
		file: source, ext: "mkv", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("x", "ac-3", 0)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", out.err)
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("rejected attempt left %v behind", names)
	}
}

// With the fallback switched off nothing changes: a stream that turns out to
// carry video is still rejected and removed.
func TestDisabledFallbackKeepsTheCombinedStreamRejected(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: false,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeInvalidAudio || apperr.ScopeOf(out.err) != apperr.ScopeCandidate {
		t.Fatalf("err = %v, want the unchanged candidate-scoped rejection", out.err)
	}
	if !strings.Contains(out.logs, "unexpected_video_stream") {
		t.Fatalf("rejection was not logged: %s", out.logs)
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("rejected stream left %v behind", names)
	}
}

// An embedded cover is an attached picture, not a video track, and survives
// the check that no video reaches the library.
func TestCoverArtIsNotMistakenForAVideoTrackAfterExtraction(t *testing.T) {
	requireMediaTools(t)
	audio := makeMedia(t, "audio.m4a", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:a", "aac")
	cover := makeMedia(t, "cover.png", "-f", "lavfi", "-i", "color=c=red:size=64x64", "-frames:v", "1")
	covered := makeMedia(t, "covered.m4a", "-i", audio, "-i", cover, "-map", "0", "-map", "1",
		"-c:a", "copy", "-c:v", "mjpeg", "-disposition:v:0", "attached_pic")

	// The same file is accepted on the audio only path with the fallback on:
	// the cover must not be counted as the video that would reject it.
	out := runCombined(t, combinedCase{
		file: covered, ext: "m4a", fallback: true,
		formats: []provider.AudioFormat{{ID: "140", Codec: "mp4a.40.2", BitrateKbps: 128}},
	})
	if out.err != nil {
		t.Fatalf("audio with an embedded cover was rejected: %v", out.err)
	}
	if out.result.FormatKind != "audio_only" || out.result.Plan == PlanExtractAudio {
		t.Fatalf("an audio only stream took the extraction path: %+v", out.result)
	}
	if out.result.Info.VideoStreams != 0 {
		t.Fatalf("cover counted as video: %q", streamTypes(t, out.result.Path))
	}
}

// A file whose runtime does not match the catalogue is rejected on the
// combined path exactly as on the audio only one.
func TestExtractedAudioStillFacesDurationVerification(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true,
		formats:    []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
		durationMS: 240000,
	})
	if apperr.CodeOf(out.err) != apperr.CodeMediaVerifyFailed {
		t.Fatalf("err = %v", out.err)
	}
	if !strings.Contains(out.logs, "duration_mismatch") {
		t.Fatalf("verification reason missing: %s", out.logs)
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("rejected attempt left %v behind", names)
	}
}

// A stream that cannot be inspected at all fails before anything is stored.
func TestCorruptedCombinedStreamFailsVerification(t *testing.T) {
	requireMediaTools(t)
	broken := filepath.Join(t.TempDir(), "broken.mp4")
	if err := os.WriteFile(broken, bytes.Repeat([]byte{0x00, 0x42}, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runCombined(t, combinedCase{
		file: broken, ext: "mp4", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if out.err == nil {
		t.Fatalf("a corrupted stream was stored as %s", filepath.Base(out.result.Path))
	}
	if apperr.CodeOf(out.err) != apperr.CodeInvalidAudio {
		t.Fatalf("err = %v", out.err)
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("rejected attempt left %v behind", names)
	}
}

// A size the platform announces above the budget is refused before a byte
// moves: the stub must never run.
func TestAnnouncedSizeAboveBudgetIsRefusedBeforeTransfer(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true, maxBytes: 1 << 20,
		formats: []provider.AudioFormat{combinedFormat("22", "mp4a.40.2", 64<<20)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", out.err)
	}
	if out.selector != "" {
		t.Fatal("the transfer was started despite an announced size above the budget")
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("refused attempt left %v behind", names)
	}
}

// A stream that announces no size at all - a segmented one, typically - is
// bounded by what actually arrived, not by a metadata estimate.
func TestTransferredSizeAboveBudgetIsRejectedAfterTheFact(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true, maxBytes: 1024,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", out.err)
	}
	if !strings.Contains(apperr.MessageOf(out.err), "transfer budget") {
		t.Fatalf("message = %q", apperr.MessageOf(out.err))
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("over-budget attempt left %v behind", names)
	}
}

// A transfer that outruns its time budget is stopped and reported as a
// provider condition, not as a cancelled job.
func TestTransferAboveTimeBudgetIsStopped(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true,
		timeout:     300 * time.Millisecond,
		stubPrelude: "sleep 5",
		formats:     []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeProviderUnavailable {
		t.Fatalf("err = %v", out.err)
	}
	if names := remaining(t, out.workDir); len(names) != 0 {
		t.Fatalf("stopped attempt left %v behind", names)
	}
}

// A transfer is stopped while it runs as soon as the bytes it reports pass the
// budget, so the remaining bytes are never fetched.
func TestRunningTransferIsStoppedWhenItReportsPassingTheBudget(t *testing.T) {
	requireMediaTools(t)
	// The stub announces progress past the budget and then stalls; only the
	// budget watchdog can end it.
	prelude := "printf '@YTDM-PROGRESS@4096|8192|NA|1000|1\\n'\nsleep 5"
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true,
		maxBytes: 1024, timeout: 10 * time.Second, stubPrelude: prelude,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if apperr.CodeOf(out.err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", out.err)
	}
	if !strings.Contains(apperr.MessageOf(out.err), "transfer budget") {
		t.Fatalf("message = %q", apperr.MessageOf(out.err))
	}
}

// Cleanup removes what the attempt created and nothing else: a staging
// directory may hold work of other steps.
func TestCleanupRemovesOnlyTheFilesOfThisAttempt(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true, maxBytes: 1024,
		formats:     []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
		preExisting: []string{"meta.json", "cover.jpg"},
	})
	if out.err == nil {
		t.Fatal("the over-budget transfer was accepted")
	}
	names := remaining(t, out.workDir)
	if len(names) != 2 {
		t.Fatalf("work directory holds %v, want only the two pre-existing files", names)
	}
	for _, name := range names {
		if name != "meta.json" && name != "cover.jpg" {
			t.Fatalf("cleanup touched foreign files: %v", names)
		}
	}
}

// The diagnostics of a combined acquisition name the kind of stream, the
// format, the codecs, what was transferred and how long it took - and carry no
// address, cookie or token.
func TestCombinedAcquisitionIsDiagnosable(t *testing.T) {
	requireMediaTools(t)
	out := runCombined(t, combinedCase{
		file: muxedAAC(t), ext: "mp4", fallback: true,
		formats: []provider.AudioFormat{combinedFormat("18", "mp4a.40.2", 0)},
	})
	if out.err != nil {
		t.Fatalf("combined acquisition failed: %v", out.err)
	}
	var event map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.logs), "\n") {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(line), &candidate); err == nil && candidate["msg"] == "download finished" {
			event = candidate
		}
	}
	if event == nil {
		t.Fatalf("no completion event: %s", out.logs)
	}
	for _, key := range []string{"format_kind", "format_id", "provider", "media_id", "source_codec",
		"source_video_codec", "codec", "transferred_bytes", "stored_bytes", "download_ms", "extract_ms",
		"verification", "session_lease_ms"} {
		if _, ok := event[key]; !ok {
			t.Errorf("diagnostic field %s missing", key)
		}
	}
	if event["format_kind"] != "combined" || event["format_id"] != "18" {
		t.Errorf("format diagnostics = %v / %v", event["format_kind"], event["format_id"])
	}
	if event["source_video_codec"] != "avc1.42001E" {
		t.Errorf("source_video_codec = %v", event["source_video_codec"])
	}
	if event["extract_ms"] == float64(0) {
		t.Error("the extraction duration was not measured")
	}
	for _, forbidden := range []string{"http", "youtube.com", "sess-1", "cookie", "token"} {
		if strings.Contains(strings.ToLower(out.logs), forbidden) {
			t.Errorf("diagnostics leak %q: %s", forbidden, out.logs)
		}
	}
}
