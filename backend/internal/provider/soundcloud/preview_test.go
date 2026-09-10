package soundcloud

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// Minimal, URL-free metadata from the two isolated 2026-09-10 reproductions.
// Full track runtimes were 195/212s; resolved metadata advertised 30s previews.
func TestResolveRejectsSoundCloudPreviews(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		duration float64
		formats  []ytdlp.Format
		wantErr  bool
	}{
		{"observed_254407911", "254407911", 30, []ytdlp.Format{{FormatID: "hls_mp3_1_0_preview", ACodec: "mp3", VCodec: "none", ABR: 128}, {FormatID: "http_mp3_1_0_preview", ACodec: "mp3", VCodec: "none", ABR: 128}}, true},
		{"observed_254407916", "254407916", 30, []ytdlp.Format{{FormatID: "hls_mp3_1_0_preview", ACodec: "mp3", VCodec: "none", ABR: 128}, {FormatID: "http_mp3_1_0_preview", ACodec: "mp3", VCodec: "none", ABR: 128}}, true},
		{"preview_claims_full_duration", "254407911", 195, []ytdlp.Format{{FormatID: "http_mp3_1_0_preview", ACodec: "mp3", VCodec: "none"}}, true},
		{"no_formats", "missing", 30, nil, true},
		{"mixed_full_and_preview", "mixed", 30, []ytdlp.Format{{FormatID: "hls_opus_0_0_preview", ACodec: "opus", VCodec: "none"}, {FormatID: "http_mp3_128", ACodec: "mp3", VCodec: "none"}}, false},
		{"full_short_track", "short", 30, []ytdlp.Format{{FormatID: "http_mp3_128", ACodec: "mp3", VCodec: "none"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := ytdlp.Info{ID: tc.id, Duration: tc.duration, Formats: tc.formats}
			data, err := json.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(t.TempDir(), "yt-dlp")
			if err := os.WriteFile(binary, append([]byte("#!/bin/sh\ncat <<'JSON'\n"), append(data, []byte("\nJSON\n")...)...), 0700); err != nil {
				t.Fatal(err)
			}
			p, err := New(Config{Client: ytdlp.New(ytdlp.Options{Binary: binary})})
			if err != nil {
				t.Fatal(err)
			}
			src, err := p.Resolve(context.Background(), provider.MediaCandidate{ID: tc.id, URL: "https://soundcloud.com/test/track"})
			if tc.wantErr {
				if src != nil || apperr.CodeOf(err) != apperr.CodeTrackNotFound || !apperr.AllowsCandidateFallback(err) {
					t.Fatalf("source=%+v error=%v; want candidate-scoped TRACK_NOT_FOUND", src, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(src.Formats) != 1 || src.Formats[0].ID != "http_mp3_128" {
					t.Fatalf("unexpected formats: %+v", src.Formats)
				}
				if src.DurationMS != 30000 {
					t.Fatalf("short full track duration changed: %d", src.DurationMS)
				}
			}
		})
	}
}
