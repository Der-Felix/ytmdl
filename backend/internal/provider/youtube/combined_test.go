package youtube

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// resolveFormats runs the real Resolve against an offline stub that answers
// with exactly these formats. No provider is contacted.
func resolveFormats(t *testing.T, fallback bool, formats []map[string]any) (*provider.MediaSource, error) {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"id": "combinedVid", "title": "Combined Song", "duration": 200,
		"webpage_url": "https://www.youtube.com/watch?v=combinedVid", "formats": formats,
	})
	if err != nil {
		t.Fatal(err)
	}
	answer := t.TempDir() + "/answer.json"
	if err := os.WriteFile(answer, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{
		Name:                  ProviderName,
		Client:                ytdlp.New(ytdlp.Options{Binary: offlineYTDLP(t, "cat '"+answer+"'\n")}),
		CombinedAudioFallback: fallback,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p.Resolve(context.Background(), provider.MediaCandidate{
		ID: "combinedVid", URL: "https://www.youtube.com/watch?v=combinedVid",
	})
}

// muxed builds a combined format record the way yt-dlp prints one.
func muxed(id string, tbr, abr float64, extra map[string]any) map[string]any {
	f := map[string]any{"format_id": id, "ext": "mp4", "vcodec": "avc1.42001E", "acodec": "mp4a.40.2", "tbr": tbr, "abr": abr}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func storyboard(id string) map[string]any {
	return map[string]any{"format_id": id, "ext": "mhtml", "vcodec": "none", "acodec": "none"}
}

// The shape the production measurement found: one combined stream next to
// storyboard images and no audio only stream at all.
func TestCombinedFallbackResolvesTheMuxedStream(t *testing.T) {
	source, err := resolveFormats(t, true, []map[string]any{
		storyboard("sb0"), storyboard("sb1"), muxed("18", 500, 96, nil),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(source.Formats) != 1 {
		t.Fatalf("formats = %+v", source.Formats)
	}
	f := source.Formats[0]
	if !f.Combined || f.ID != "18" {
		t.Fatalf("format = %+v, want the combined stream addressed by its id", f)
	}
	if f.Codec != "mp4a.40.2" || f.VideoCodec != "avc1.42001E" {
		t.Fatalf("codecs = %q / %q", f.Codec, f.VideoCodec)
	}
	if f.Container != "m4a" {
		t.Fatalf("container = %q, want the container the audio can be copied into", f.Container)
	}
	if f.BitrateKbps != 96 || f.TransferBitrateKbps != 500 {
		t.Fatalf("bitrates = %v audio / %v transfer", f.BitrateKbps, f.TransferBitrateKbps)
	}
}

// With the fallback off the item is rejected exactly as before, and the
// rejection stays a bounded-retry candidate failure.
func TestCombinedFallbackDisabledKeepsTheRejection(t *testing.T) {
	_, err := resolveFormats(t, false, []map[string]any{storyboard("sb0"), muxed("18", 500, 96, nil)})
	if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", err)
	}
	if apperr.StopsCandidateFanout(err) {
		t.Fatal("the rejection stopped the candidate fanout")
	}
}

// An audio only stream is never traded for a combined one, whatever the
// combined stream promises.
func TestAudioOnlyStreamKeepsPrecedenceOverCombined(t *testing.T) {
	source, err := resolveFormats(t, true, []map[string]any{
		muxed("18", 500, 192, nil),
		{"format_id": "251", "ext": "webm", "vcodec": "none", "acodec": "opus", "abr": 130},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, f := range source.Formats {
		if f.Combined {
			t.Fatalf("a combined stream was offered next to an audio only one: %+v", source.Formats)
		}
	}
	if len(source.Formats) != 1 || source.Formats[0].ID != "251" {
		t.Fatalf("formats = %+v", source.Formats)
	}
}

// Every property that makes a combined stream unusable is read from the format
// record, and an absent field is never read as a promise.
func TestCombinedStreamsAreRejectedOnProvenGrounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format map[string]any
	}{
		{"audio codec unknown", map[string]any{"format_id": "x", "vcodec": "avc1", "tbr": 500}},
		{"audio absent", map[string]any{"format_id": "x", "vcodec": "avc1", "acodec": "none", "tbr": 500}},
		{"nothing known", map[string]any{"format_id": "x", "tbr": 500}},
		{"preview by id", muxed("18_preview", 500, 96, nil)},
		{"preview by note", muxed("18", 500, 96, map[string]any{"format_note": "30 second sample"})},
		{"drm protected", muxed("18", 500, 96, map[string]any{"has_drm": true})},
		{"protected transport", muxed("18", 500, 96, map[string]any{"protocol": "ism"})},
		{"codec cannot be copied", map[string]any{"format_id": "x", "vcodec": "avc1", "acodec": "ec-3", "tbr": 500, "abr": 96}},
		{"audio bitrate proven too low", muxed("18", 500, 16, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveFormats(t, true, []map[string]any{storyboard("sb0"), tc.format})
			if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat {
				t.Fatalf("err = %v, want the item rejected", err)
			}
		})
	}
}

// An unknown audio bitrate is unknown, not low: the stream stays eligible and
// is judged by what it costs to transfer.
func TestUnknownAudioBitrateDoesNotDisqualifyACombinedStream(t *testing.T) {
	source, err := resolveFormats(t, true, []map[string]any{
		storyboard("sb0"),
		{"format_id": "18", "vcodec": "avc1", "acodec": "mp4a.40.2", "tbr": 500},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(source.Formats) != 1 || !source.Formats[0].Combined {
		t.Fatalf("formats = %+v", source.Formats)
	}
	if source.Formats[0].BitrateKbps != 0 {
		t.Fatalf("an unknown audio bitrate was invented: %v", source.Formats[0].BitrateKbps)
	}
}

// The cheapest transfer wins, not the best picture: a combined stream is paid
// for in bandwidth and session time, and its video is discarded either way.
func TestCombinedSelectionPrefersTheCheapestTransfer(t *testing.T) {
	source, err := resolveFormats(t, true, []map[string]any{
		muxed("22", 1500, 128, map[string]any{"height": 720, "filesize": 40_000_000}),
		muxed("18", 500, 96, map[string]any{"height": 360, "filesize": 15_000_000}),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if source.Formats[0].ID != "18" {
		t.Fatalf("selected %q, want the smaller transfer 18", source.Formats[0].ID)
	}
}

// A reported size is weighed against other sizes and a bitrate against other
// bitrates; a format that reports neither sorts last instead of looking free.
func TestCombinedSelectionIsDeterministicWithPartialInformation(t *testing.T) {
	formats := []map[string]any{
		muxed("c", 0, 96, nil),
		muxed("b", 800, 96, nil),
		muxed("a", 400, 96, nil),
	}
	first, err := resolveFormats(t, true, formats)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if first.Formats[0].ID != "a" {
		t.Fatalf("selected %q, want the lowest known transfer bitrate", first.Formats[0].ID)
	}
	// The same answer must always select the same stream: the download
	// addresses the format by the id that was judged.
	for i := 0; i < 3; i++ {
		again, err := resolveFormats(t, true, formats)
		if err != nil || again.Formats[0].ID != first.Formats[0].ID {
			t.Fatalf("run %d selected %v (%v)", i, again.Formats, err)
		}
	}
}
