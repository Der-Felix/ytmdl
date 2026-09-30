package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func testTool(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyReasonsAndUnchangedTolerance(t *testing.T) {
	for _, tc := range []struct {
		name                string
		info                *AudioInfo
		expected, tolerance int
		reason              string
	}{
		{"unprobed", nil, 30000, 15000, "ffprobe_error"},
		{"missing_stream", &AudioInfo{DurationMS: 30000, SizeBytes: 10}, 30000, 15000, "missing_audio_stream"},
		{"invalid_duration", &AudioInfo{Codec: "mp3", SizeBytes: 10}, 30000, 15000, "invalid_duration"},
		{"empty", &AudioInfo{Codec: "mp3", DurationMS: 30000}, 30000, 15000, "empty_file"},
		{"observed_254407911", &AudioInfo{Codec: "mp3", Container: "mp3", DurationMS: 195653, SizeBytes: 476472}, 30000, 15000, "duration_mismatch"},
		{"observed_254407916", &AudioInfo{Codec: "mp3", Container: "mp3", DurationMS: 212040, SizeBytes: 476472}, 30000, 15000, "duration_mismatch"},
		{"shorter", &AudioInfo{Codec: "mp3", DurationMS: 14999, SizeBytes: 10}, 30000, 15000, "duration_mismatch"},
		{"full_short_track", &AudioInfo{Codec: "mp3", Container: "mp3", DurationMS: 30000, SizeBytes: 476472}, 30000, 15000, ""},
		{"boundary", &AudioInfo{Codec: "mp3", DurationMS: 45000, SizeBytes: 10}, 30000, 15000, ""},
		{"outside_default", &AudioInfo{Codec: "mp3", DurationMS: 45001, SizeBytes: 10}, 30000, 0, "duration_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Verify(tc.info, tc.expected, tc.tolerance)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *verificationFailure
			if !errors.As(err, &failure) || failure.reason != tc.reason || apperr.CodeOf(err) != apperr.CodeInvalidAudio {
				t.Fatalf("unexpected verification error: %v", err)
			}
		})
	}
}

func TestProbeFailureDiagnosticsExcludeToolOutput(t *testing.T) {
	const secret = "https://example.invalid/audio?signature=SECRET Cookie: SENSITIVE"
	for _, tc := range []struct {
		name, script, reason string
		empty                bool
	}{
		{"stderr", "echo '" + secret + "' >&2\nexit 1", "ffprobe_error", false},
		{"invalid_json", "echo '" + secret + "'", "ffprobe_error", false},
		{"no_stream", `echo '{"streams":[],"format":{"format_name":"mp3","duration":"30","size":"4"}}'`, "missing_audio_stream", false},
		{"empty", "exit 1", "empty_file", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private-file.mp3")
			data := []byte("test")
			if tc.empty {
				data = nil
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			info, err := NewProber(ProberOptions{Binary: testTool(t, tc.script)}).Probe(context.Background(), path)
			if err == nil {
				t.Fatal("expected failure")
			}
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			logVerificationFailure(logger, "raw", info, 30000, 15000, err)
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if event["verification_reason"] != tc.reason || event["size_bytes"] != float64(len(data)) {
				t.Fatalf("unexpected event: %v", event)
			}
			if tc.reason == "ffprobe_error" && event["measured_duration_ms"] != nil {
				t.Fatalf("unknown duration must be null: %v", event)
			}
			for _, forbidden := range []string{"SECRET", "SENSITIVE", "https://", "private-file"} {
				if strings.Contains(output.String()+err.Error(), forbidden) {
					t.Fatalf("leaked %s", forbidden)
				}
			}
		})
	}
}

func TestDownloaderLogsFinalVerificationAndCleansFile(t *testing.T) {
	// Replay measured metadata from the real preview, with no provider contact.
	const probe = `echo '{"streams":[{"codec_type":"audio","codec_name":"mp3","duration":"195.653"}],"format":{"format_name":"mp3","size":"476472"}}'`
	for _, tc := range []struct {
		name, final string
		code        apperr.Code
		reason      string
	}{
		{"duration_mismatch", probe, apperr.CodeMediaVerifyFailed, "duration_mismatch"},
		{"final_probe_error", "echo 'https://invalid/?signature=SECRET' >&2; exit 1", apperr.CodeInvalidAudio, "ffprobe_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			downloadTool := testTool(t, `while [ "$#" -gt 0 ]; do
if [ "$1" = '-o' ]; then shift; dest="${1%source.*}source.mp3"; printf 'data' > "$dest"; exit 0; fi
shift
done
exit 1`)
			probeTool := testTool(t, "for arg do path=\"$arg\"; done\ncase \"$path\" in\n *source.mp3) "+probe+";;\n *) "+tc.final+";;\nesac")
			var output bytes.Buffer
			d, err := New(Options{YTDLP: ytdlp.New(ytdlp.Options{Binary: downloadTool}), FFmpeg: ffmpeg.New("ffmpeg", time.Second), Prober: NewProber(ProberOptions{Binary: probeTool}), Logger: slog.New(slog.NewJSONHandler(&output, nil))})
			if err != nil {
				t.Fatal(err)
			}
			result, err := d.Download(context.Background(), provider.MediaSource{Provider: "soundcloud", ID: "254407911", URL: "https://example.invalid/?signature=SECRET", DurationMS: 30000, SessionID: "SESSION_SECRET", Title: "TITLE_SECRET", Uploader: "UPLOADER_SECRET"}, filepath.Join(work, "audio.mp3"), nil)
			if result != nil || apperr.CodeOf(err) != tc.code {
				t.Fatalf("result=%v error=%v", result, err)
			}
			files, err := os.ReadDir(work)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatalf("rejected media remains: %v", files)
			}
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			allowed := map[string]bool{}
			for _, key := range []string{"time", "level", "msg", "provider", "operation", "media_id", "format_kind", "format_id", "verification_stage", "verification_reason", "expected_duration_ms", "measured_duration_ms", "duration_tolerance_ms", "codec", "container", "size_bytes"} {
				allowed[key] = true
			}
			for key := range event {
				if !allowed[key] {
					t.Errorf("unreviewed diagnostic field: %s", key)
				}
			}
			for key, want := range map[string]any{"provider": "soundcloud", "media_id": "254407911", "format_kind": "audio_only", "format_id": "", "verification_reason": tc.reason, "verification_stage": "final", "expected_duration_ms": float64(30000), "duration_tolerance_ms": float64(15000)} {
				if event[key] != want {
					t.Errorf("%s=%v, want %v", key, event[key], want)
				}
			}
			if tc.reason == "duration_mismatch" {
				for key, want := range map[string]any{"measured_duration_ms": float64(195653), "codec": "mp3", "container": "mp3", "size_bytes": float64(476472)} {
					if event[key] != want {
						t.Errorf("%s=%v, want %v", key, event[key], want)
					}
				}
			}
			if strings.Contains(output.String(), "SECRET") {
				t.Fatal("signed URL leaked")
			}
		})
	}
}

func TestProbeMissingBinaryAndTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, binary string
		timeout      time.Duration
		code         apperr.Code
	}{
		{"missing", filepath.Join(t.TempDir(), "missing"), time.Second, apperr.CodeToolUnavailable},
		{"timeout", testTool(t, "exec sleep 2"), 10 * time.Millisecond, apperr.CodeInvalidAudio},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewProber(ProberOptions{Binary: tc.binary, Timeout: tc.timeout}).Probe(context.Background(), path)
			var failure *verificationFailure
			if apperr.CodeOf(err) != tc.code || !errors.As(err, &failure) || failure.reason != "ffprobe_error" || failure.info.SizeBytes != 4 {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDiagnosticTokensRejectFreeText(t *testing.T) {
	for _, s := range []string{"https://host/?signature=secret", "Cookie: secret", "mp3\nsecret", strings.Repeat("x", 129)} {
		if diagnosticToken(s) != "redacted" {
			t.Errorf("accepted unsafe token")
		}
	}
}
