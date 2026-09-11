package downloader

import (
	"bytes"
	"context"
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

// requireMediaTools skips when ffmpeg or ffprobe is missing; CI installs both.
func requireMediaTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

// makeMedia renders a short synthetic file with ffmpeg's built-in encoders.
func makeMedia(t *testing.T, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), out)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, b)
	}
	return out
}

func muxedFixture(t *testing.T) string {
	return makeMedia(t, "muxed.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "3", "-c:v", "mpeg4", "-c:a", "aac", "-b:a", "96k")
}

// audioWithCoverFixture is an audio only file that carries an embedded cover:
// a video stream with the attached picture disposition, not a video.
func audioWithCoverFixture(t *testing.T) string {
	audio := makeMedia(t, "audio.m4a", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:a", "aac")
	cover := makeMedia(t, "cover.png", "-f", "lavfi", "-i", "color=c=red:size=64x64", "-frames:v", "1")
	return makeMedia(t, "covered.m4a", "-i", audio, "-i", cover, "-map", "0", "-map", "1",
		"-c:a", "copy", "-c:v", "mjpeg", "-disposition:v:0", "attached_pic")
}

// downloadFile runs the real Download with a yt-dlp stub that delivers file as
// the downloaded stream and records the format selector it was asked for.
func downloadFile(t *testing.T, file, ext string, formats []provider.AudioFormat) (*Result, error, string) {
	t.Helper()
	selectorLog := filepath.Join(t.TempDir(), "selector")
	stub := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
 case "$1" in
  -f) shift; printf '%s' "$1" > '` + selectorLog + `';;
  -o) shift; cp '` + file + `' "${1%source.*}source.` + ext + `";;
 esac
 shift
done
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	d, err := New(Options{
		YTDLP:  ytdlp.New(ytdlp.Options{Binary: stub}),
		FFmpeg: ffmpeg.New("ffmpeg", time.Minute),
		Prober: NewProber(ProberOptions{Binary: "ffprobe", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}),
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	res, err := d.Download(context.Background(), provider.MediaSource{
		Provider: "youtube", ID: "fixtureVid1", URL: "https://www.youtube.com/watch?v=fixtureVid1",
		DurationMS: 3000, Formats: formats,
	}, filepath.Join(work, "track.opus"), nil)
	selector, _ := os.ReadFile(selectorLog)
	if err != nil {
		if entries, _ := os.ReadDir(work); len(entries) != 0 {
			t.Fatalf("rejected stream left files behind: %v", entries)
		}
		if !strings.Contains(logs.String(), "unexpected_video_stream") && strings.Contains(err.Error(), "video") {
			t.Fatalf("rejection was not logged: %s", logs.String())
		}
	}
	return res, err, string(selector)
}

func streamTypes(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type:stream_disposition=attached_pic",
		"-of", "csv=p=0", "--", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// A combined audio/video stream must never be filed away as an audio file.
// The generic selector used to end in "best", which yt-dlp answers with a
// combined stream when no audio only stream is left; the AAC plan kept such a
// file unchanged, video track included, under an .m4a name.
func TestCombinedStreamIsNotFiledAsAudio(t *testing.T) {
	requireMediaTools(t)
	res, err, _ := downloadFile(t, muxedFixture(t), "mp4", nil)
	if err == nil {
		t.Fatalf("combined stream stored as %s with streams %q", filepath.Base(res.Path), streamTypes(t, res.Path))
	}
	if apperr.CodeOf(err) != apperr.CodeInvalidAudio || apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("err = %v, want a candidate-scoped INVALID_AUDIO", err)
	}
}

func TestEmbeddedCoverIsNotMistakenForVideo(t *testing.T) {
	requireMediaTools(t)
	res, err, _ := downloadFile(t, audioWithCoverFixture(t), "m4a", []provider.AudioFormat{{ID: "140", Codec: "mp4a.40.2", BitrateKbps: 128}})
	if err != nil {
		t.Fatalf("audio with cover art was rejected: %v", err)
	}
	if res.Info.Codec != "aac" || filepath.Ext(res.Path) != ".m4a" {
		t.Fatalf("result %+v", res)
	}
}

// The download never falls back to "whatever the platform offers".
func TestSelectorNeverFallsBackToCombinedStreams(t *testing.T) {
	for _, formats := range [][]provider.AudioFormat{
		nil,
		{{ID: "251", Codec: "opus", BitrateKbps: 130}},
		{{ID: "234"}},
	} {
		sel := FormatSelector(formats)
		for _, alt := range strings.Split(sel, "/") {
			if alt == "best" || alt == "b" || strings.HasPrefix(alt, "best[") || alt == "best*" {
				t.Fatalf("selector %q may pick a combined stream", sel)
			}
		}
	}
}

// An audio rendition whose codec yt-dlp did not name is requested explicitly
// when it is the only audio the resolver accepted; ffprobe settles its codec.
func TestRenditionWithoutCodecIsSelectedExplicitly(t *testing.T) {
	got, ok := SelectFormat([]provider.AudioFormat{{ID: "233"}, {ID: "234", BitrateKbps: 128}})
	if !ok || got.ID != "234" {
		t.Fatalf("selected %+v %v", got, ok)
	}
	if sel := FormatSelector([]provider.AudioFormat{{ID: "234"}}); !strings.HasPrefix(sel, "234/") {
		t.Fatalf("selector %q", sel)
	}
	// A named codec still wins over an unnamed one.
	got, _ = SelectFormat([]provider.AudioFormat{{ID: "234", BitrateKbps: 256}, {ID: "140", Codec: "mp4a.40.2", BitrateKbps: 128}})
	if got.ID != "140" {
		t.Fatalf("unnamed codec outranked a named one: %+v", got)
	}
	if _, ok := SelectFormat([]provider.AudioFormat{{ID: "x", Codec: "none"}}); ok {
		t.Fatal("a format without audio was selected")
	}
}

func TestRenditionWithoutCodecDownloadsAndIsProbed(t *testing.T) {
	requireMediaTools(t)
	audio := makeMedia(t, "rendition.mp4", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:a", "aac")
	res, err, selector := downloadFile(t, audio, "mp4", []provider.AudioFormat{{ID: "234", Container: "mp4"}})
	if err != nil {
		t.Fatalf("rendition download failed: %v", err)
	}
	if !strings.HasPrefix(selector, "234/") {
		t.Fatalf("selector %q did not request the accepted rendition", selector)
	}
	if res.Info.Codec != "aac" || res.Plan != PlanKeep || filepath.Ext(res.Path) != ".m4a" {
		t.Fatalf("result %+v", res)
	}
}
