package youtube

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// ytdlpCase is one entry of testdata/hls_formats.json: the formats yt-dlp
// itself produced for a synthetic manifest (see testdata/hls_formats.py) and
// the format its own bestaudio selector picked.
type ytdlpCase struct {
	Case      string            `json:"case"`
	Version   string            `json:"yt_dlp_version"`
	Formats   []json.RawMessage `json:"formats"`
	BestAudio []string          `json:"yt_dlp_bestaudio"`
}

func loadYTDLPCase(t *testing.T, name string) ytdlpCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/hls_formats.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []ytdlpCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Case == name {
			return c
		}
	}
	t.Fatalf("fixture case %q missing", name)
	return ytdlpCase{}
}

// resolveWithFormats runs the real Resolve against a stub that prints the
// fixture formats as the single --dump-json line of an extraction.
func resolveWithFormats(t *testing.T, c ytdlpCase) (*provider.MediaSource, error) {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"id": "fixtureVid1", "title": "Fixture Song", "duration": 200,
		"webpage_url": "https://www.youtube.com/watch?v=fixtureVid1", "formats": c.Formats,
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := t.TempDir() + "/answer.json"
	if err := os.WriteFile(answer, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := offlineYTDLP(t, "cat '"+answer+"'\n")
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary})})
	if err != nil {
		t.Fatal(err)
	}
	return p.Resolve(context.Background(), provider.MediaCandidate{ID: "fixtureVid1", URL: "https://www.youtube.com/watch?v=fixtureVid1"})
}

// An audio rendition without a codec is a real audio only stream: yt-dlp's
// own bestaudio picks it. It used to be rejected as "no audio only stream".
func TestAudioRenditionWithoutCodecIsAccepted(t *testing.T) {
	c := loadYTDLPCase(t, "hls_split_audio_renditions")
	if len(c.BestAudio) != 1 {
		t.Fatalf("fixture: yt-dlp bestaudio = %v", c.BestAudio)
	}

	source, err := resolveWithFormats(t, c)
	if err != nil {
		t.Fatalf("yt-dlp %s selects %s as bestaudio, Resolve rejected the item: %v", c.Version, c.BestAudio[0], err)
	}
	ids := make([]string, 0, len(source.Formats))
	for _, f := range source.Formats {
		ids = append(ids, f.ID)
		// The codec stays unknown until the downloaded file is inspected; it
		// is neither invented nor defaulted.
		if f.Codec != "" {
			t.Errorf("format %s got codec %q, yt-dlp reported none", f.ID, f.Codec)
		}
	}
	if !strings.Contains(strings.Join(ids, ","), c.BestAudio[0]) {
		t.Fatalf("accepted formats %v lack yt-dlp's bestaudio %s", ids, c.BestAudio[0])
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "hls-23") {
			t.Fatalf("a video variant was accepted as audio: %v", ids)
		}
	}
}

// Only combined streams: yt-dlp's bestaudio is empty as well, so the item is
// rejected - now with the bitrates needed to judge an audio extraction.
func TestMuxedOnlyItemStaysRejectedWithBandwidthShape(t *testing.T) {
	c := loadYTDLPCase(t, "hls_muxed_only")
	if len(c.BestAudio) != 0 {
		t.Fatalf("fixture: yt-dlp bestaudio = %v", c.BestAudio)
	}
	_, err := resolveWithFormats(t, c)
	// The item exists and answered; only its format shape is unusable here.
	// That is a bounded-retry candidate failure, never a permanent verdict on
	// the item and never a reason to stop the family.
	if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat || apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("err = %v", err)
	}
	if apperr.StopsCandidateFanout(err) {
		t.Fatal("a format limitation stopped the candidate fanout")
	}
	if !apperr.Retryable(err) {
		t.Fatal("a format limitation was classified as permanent")
	}
	msg := apperr.MessageOf(err)
	want := "formats: 2 total, 2 muxed, 0 video only, 0 video with unknown audio, 0 images, 0 unknown, 0 other; muxed up to 1149 kbps total, 0 kbps audio"
	if !strings.Contains(msg, want) {
		t.Fatalf("message %q lacks %q", msg, want)
	}
	if strings.Contains(msg, "http") || strings.Contains(msg, "invalid") {
		t.Fatalf("message leaks an address: %q", msg)
	}
}
