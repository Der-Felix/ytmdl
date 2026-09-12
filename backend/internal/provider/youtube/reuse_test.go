package youtube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// offlineYTDLP is a yt-dlp stand-in: it records every process start in
// <binary>.calls and answers from fixed JSON, so no provider is contacted.
func offlineYTDLP(t *testing.T, body string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\necho \"$*\" >> \"$0.calls\"\n" + body
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func starts(t *testing.T, binary string) []string {
	t.Helper()
	raw, err := os.ReadFile(binary + ".calls")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

const directVideo = `*watch?v=dQw4w9WgXcQ*)
 echo '{"id":"dQw4w9WgXcQ","title":"Song","artist":"Artist","duration":200,"webpage_url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ","formats":[{"format_id":"251","acodec":"opus","vcodec":"none","abr":130}]}'
 ;;`

func TestDirectIDProbeAndResolveShareOneExtraction(t *testing.T) {
	binary := offlineYTDLP(t, "case \"$*\" in\n"+directVideo+"\n *) exit 7;;\nesac\n")
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary})})
	if err != nil {
		t.Fatal(err)
	}
	track := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000, SourceProvider: "ytmusic", SourceID: "dQw4w9WgXcQ"}

	candidates, err := p.Search(context.Background(), track)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("search: %v %v", candidates, err)
	}
	source, err := p.Resolve(context.Background(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if source.ID != "dQw4w9WgXcQ" || len(source.Formats) != 1 || source.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("unexpected source: %+v", source)
	}
	if got := starts(t, binary); len(got) != 1 {
		t.Fatalf("process starts = %d (%q), want the probe only", len(got), got)
	}
}

func TestEnrichedCandidateResolvesFromTheEnrichment(t *testing.T) {
	binary := offlineYTDLP(t, `case "$*" in
 *ytsearch*)
 echo '{"id":"aaaaaaaaaaa","title":"Artist - Song","url":"https://www.youtube.com/watch?v=aaaaaaaaaaa"}'
 ;;
 *watch?v=aaaaaaaaaaa*)
 echo '{"id":"aaaaaaaaaaa","title":"Artist - Song","duration":200,"webpage_url":"https://www.youtube.com/watch?v=aaaaaaaaaaa","formats":[{"format_id":"251","acodec":"opus","vcodec":"none"}]}'
 ;;
 *) exit 7;;
esac
`)
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary})})
	if err != nil {
		t.Fatal(err)
	}
	track := music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}

	for attempt := 0; attempt < 2; attempt++ {
		candidates, err := p.Search(context.Background(), track)
		if err != nil || len(candidates) != 1 || candidates[0].DurationMS != 200000 {
			t.Fatalf("attempt %d search: %+v %v", attempt, candidates, err)
		}
		if _, err := p.Resolve(context.Background(), candidates[0]); err != nil {
			t.Fatalf("attempt %d resolve: %v", attempt, err)
		}
	}
	// One flat search plus one extraction serve both attempts.
	if got := starts(t, binary); len(got) != 2 {
		t.Fatalf("process starts = %d (%q), want 2", len(got), got)
	}
}

func TestPacingAppliesToProcessStartsOnly(t *testing.T) {
	binary := offlineYTDLP(t, "case \"$*\" in\n"+directVideo+"\n *) exit 7;;\nesac\n")
	// One start per 400 ms, no burst beyond the first.
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary}), RequestsPerSecond: 2.5, Burst: 1})
	if err != nil {
		t.Fatal(err)
	}
	candidate := provider.MediaCandidate{Provider: ProviderName, ID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}

	if _, err := p.Resolve(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	for i := 0; i < 5; i++ {
		if _, err := p.Resolve(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(began); elapsed > 300*time.Millisecond {
		t.Fatalf("reused answers waited for request pacing: %v", elapsed)
	}
	if got := starts(t, binary); len(got) != 1 {
		t.Fatalf("process starts = %d, want 1", len(got))
	}
}

func TestPacingStillSpacesRealProcessStarts(t *testing.T) {
	binary := offlineYTDLP(t, `echo '{"id":"x","title":"X","duration":1,"formats":[{"format_id":"251","acodec":"opus","vcodec":"none"}]}'`+"\n")
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary, DisableQueryCache: true}), RequestsPerSecond: 5, Burst: 1})
	if err != nil {
		t.Fatal(err)
	}
	candidate := provider.MediaCandidate{Provider: ProviderName, ID: "dQw4w9WgXcQ", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	began := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := p.Resolve(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
	}
	// Three starts at 5/s with burst 1 need at least two intervals.
	if elapsed := time.Since(began); elapsed < 350*time.Millisecond {
		t.Fatalf("process starts were not paced: %v", elapsed)
	}
}

func TestNoAudioStreamErrorCarriesOnlyFormatCounts(t *testing.T) {
	binary := offlineYTDLP(t, `echo '{"id":"bbbbbbbbbbb","title":"Song","duration":200,"webpage_url":"https://www.youtube.com/watch?v=bbbbbbbbbbb","formats":[{"format_id":"sb0","vcodec":"none","acodec":"none","url":"https://i.invalid/sb"},{"format_id":"18","vcodec":"avc1","acodec":"mp4a.40.2","url":"https://stream.invalid/18"},{"format_id":"137","vcodec":"avc1","acodec":"none"},{"format_id":"x1","url":"https://stream.invalid/x1"}]}'`+"\n")
	p, err := New(Config{Name: ProviderName, Client: ytdlp.New(ytdlp.Options{Binary: binary})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Resolve(context.Background(), provider.MediaCandidate{ID: "bbbbbbbbbbb", URL: "https://www.youtube.com/watch?v=bbbbbbbbbbb"})
	if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat || apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("error = %v", err)
	}
	msg := apperr.MessageOf(err)
	if !strings.Contains(msg, "4 total, 1 muxed, 1 video only, 0 video with unknown audio, 1 images, 1 unknown, 0 other") {
		t.Fatalf("message lacks the format shape: %q", msg)
	}
	if strings.Contains(msg, "http") || strings.Contains(msg, "invalid") {
		t.Fatalf("message leaks an address: %q", msg)
	}
}
