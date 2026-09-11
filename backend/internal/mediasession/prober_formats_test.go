package mediasession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/ytdlp"
)

func probeWithFormats(t *testing.T, formats string) *ProbeResult {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\necho '{\"id\":\"fixtureVid1\",\"title\":\"Fixture\",\"formats\":" + formats + "}'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	res, _ := NewYTDLPProber(ytdlp.New(ytdlp.Options{Binary: binary}), "").Probe(context.Background(), "", "")
	if res == nil {
		t.Fatal("no probe result")
	}
	return res
}

// yt-dlp emits HLS audio renditions with vcodec "none" and no acodec. A probe
// that only counted named audio codecs reported "no usable audio formats" for
// such a session and would have refused its replacement cookies.
func TestProbeCountsAudioRenditionWithoutCodec(t *testing.T) {
	res := probeWithFormats(t, `[{"format_id":"234","vcodec":"none"},{"format_id":"232","vcodec":"avc1.4D401F","acodec":"none"}]`)
	if !res.UsableAudioFormats || res.Status != HealthHealthy {
		t.Fatalf("probe result %+v", res)
	}
}

func TestProbeStillRejectsFormatsWithoutAudio(t *testing.T) {
	res := probeWithFormats(t, `[{"format_id":"sb0","vcodec":"none","acodec":"none"},{"format_id":"232","vcodec":"avc1.4D401F","acodec":"none"},{"format_id":"x"}]`)
	if res.UsableAudioFormats || res.FailureCategory != "NO_USABLE_AUDIO_FORMATS" {
		t.Fatalf("probe result %+v", res)
	}
}
