package ytdlp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/throughput"
)

// fakeBinary writes an offline yt-dlp stand-in. Every process start appends one
// line to <binary>.calls, so a test can count real process starts.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\necho \"$*\" >> \"$0.calls\"\n" + body
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func processStarts(t *testing.T, binary string) int {
	t.Helper()
	raw, err := os.ReadFile(binary + ".calls")
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

const videoJSON = `echo '{"id":"dQw4w9WgXcQ","title":"Song","duration":200,"webpage_url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ","url":"https://stream.invalid/expiring","formats":[{"format_id":"251","acodec":"opus","vcodec":"none"}]}'`

func TestQueryCacheReusesIdenticalExtraction(t *testing.T) {
	binary := fakeBinary(t, videoJSON+"\n")
	rec := throughput.New()
	client := New(Options{Binary: binary, Recorder: rec, Label: "youtube"})

	for i := 0; i < 3; i++ {
		infos, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "--no-playlist")
		if err != nil {
			t.Fatal(err)
		}
		if len(infos) != 1 || infos[0].ID != "dQw4w9WgXcQ" || len(infos[0].Formats) != 1 {
			t.Fatalf("unexpected result: %+v", infos)
		}
		// A reused extraction must never hand out a stream URL.
		if infos[0].URL != "" {
			t.Fatalf("stream url survived: %q", infos[0].URL)
		}
		// Mutating one answer must not alter the next.
		infos[0].Formats[0].FormatID = "mutated"
		infos[0].Title = "mutated"
	}
	if got := processStarts(t, binary); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
	counts := rec.Drain().Counts
	if counts["ytdlp.youtube.extract.process"] != 1 || counts["ytdlp.youtube.extract.cache_hit"] != 2 {
		t.Fatalf("counters: %v", counts)
	}
}

func TestQueryCacheSeparatesSessionContexts(t *testing.T) {
	binary := fakeBinary(t, videoJSON+"\n")
	base := New(Options{Binary: binary})
	target := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	for _, c := range []*Client{
		base.WithCookieFile("/cookies/a.txt"),
		base.WithCookieFile("/cookies/b.txt"),
		base, // anonymous
		base.WithCookieFile("/cookies/a.txt"),
	} {
		if _, err := c.Query(context.Background(), target, "--no-playlist"); err != nil {
			t.Fatal(err)
		}
	}
	if got := processStarts(t, binary); got != 3 {
		t.Fatalf("process starts = %d, want one per cookie context (3)", got)
	}
}

func TestQueryCacheNeverStoresTemporaryFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stderr string
		code   apperr.Code
	}{
		{"rate_limited", "ERROR: [youtube] x: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited},
		{"network", "ERROR: [youtube] x: Connection reset by peer", apperr.CodeProviderUnavailable},
		{"bot_challenge", "ERROR: [youtube] x: Sign in to confirm you are not a bot", apperr.CodeSessionBotChallenge},
		{"unknown", "ERROR: something odd happened", apperr.CodeProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := fakeBinary(t, "echo '"+tc.stderr+"' >&2\nexit 1\n")
			client := New(Options{Binary: binary})
			for i := 0; i < 2; i++ {
				_, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "--no-playlist")
				if apperr.CodeOf(err) != tc.code {
					t.Fatalf("code = %s, want %s", apperr.CodeOf(err), tc.code)
				}
			}
			if got := processStarts(t, binary); got != 2 {
				t.Fatalf("temporary failure was replayed: %d process starts, want 2", got)
			}
		})
	}
}

func TestQueryCacheReplaysItemFailureUntilExpiry(t *testing.T) {
	binary := fakeBinary(t, "echo 'ERROR: [youtube] x: Video unavailable' >&2\nexit 1\n")
	client := New(Options{Binary: binary, QueryCache: QueryCacheOptions{NegativeTTL: time.Minute}})
	clock := time.Now()
	client.cache.now = func() time.Time { return clock }
	target := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	for i := 0; i < 3; i++ {
		_, err := client.Query(context.Background(), target, "--no-playlist")
		if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
			t.Fatalf("attempt %d: code = %s", i, apperr.CodeOf(err))
		}
	}
	if got := processStarts(t, binary); got != 1 {
		t.Fatalf("process starts = %d, want 1 while the failure is fresh", got)
	}

	clock = clock.Add(time.Minute + time.Second)
	if _, err := client.Query(context.Background(), target, "--no-playlist"); apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("code after expiry = %s", apperr.CodeOf(err))
	}
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("expired failure was not re-checked: %d process starts", got)
	}
}

func TestQueryCacheSearchRules(t *testing.T) {
	// An empty listing is never reused; a non-empty one is, until it expires.
	binary := fakeBinary(t, `case "$*" in
 *empty*) ;;
 *) echo '{"id":"a","title":"A","url":"https://www.youtube.com/watch?v=a"}' ;;
esac
`)
	client := New(Options{Binary: binary, QueryCache: QueryCacheOptions{SearchTTL: time.Minute}})
	clock := time.Now()
	client.cache.now = func() time.Time { return clock }

	for i := 0; i < 2; i++ {
		infos, err := client.Search(context.Background(), "ytsearch", "empty", 5)
		if err != nil || len(infos) != 0 {
			t.Fatalf("empty search: %v %v", infos, err)
		}
	}
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("empty search was reused: %d starts", got)
	}

	for i := 0; i < 2; i++ {
		infos, err := client.Search(context.Background(), "ytsearch", "artist song", 5)
		if err != nil || len(infos) != 1 {
			t.Fatalf("search: %v %v", infos, err)
		}
		// Flat entries keep their page address.
		if infos[0].URL == "" {
			t.Fatal("flat entry lost its page url")
		}
	}
	if got := processStarts(t, binary); got != 3 {
		t.Fatalf("process starts = %d, want 3", got)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := client.Search(context.Background(), "ytsearch", "artist song", 5); err != nil {
		t.Fatal(err)
	}
	if got := processStarts(t, binary); got != 4 {
		t.Fatalf("expired search was reused: %d starts", got)
	}
}

func TestQueryCacheCoalescesConcurrentIdenticalQueries(t *testing.T) {
	binary := fakeBinary(t, "sleep 0.3\n"+videoJSON+"\n")
	rec := throughput.New()
	client := New(Options{Binary: binary, Recorder: rec, Label: "youtube"})

	const callers = 8
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "--no-playlist")
			if err != nil || len(infos) != 1 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d callers failed", failures.Load())
	}
	if got := processStarts(t, binary); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
	counts := rec.Drain().Counts
	if counts["ytdlp.youtube.extract.process"]+counts["ytdlp.youtube.extract.shared"]+counts["ytdlp.youtube.extract.cache_hit"] != callers {
		t.Fatalf("answers do not add up: %v", counts)
	}
}

func TestQueryCacheFollowerSurvivesLeaderCancellation(t *testing.T) {
	binary := fakeBinary(t, "sleep 0.4\n"+videoJSON+"\n")
	client := New(Options{Binary: binary})
	target := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := client.Query(leaderCtx, target, "--no-playlist")
		leaderDone <- err
	}()
	waitForStarts(t, binary, 1)

	followerDone := make(chan error, 1)
	go func() {
		infos, err := client.Query(context.Background(), target, "--no-playlist")
		if err == nil && len(infos) != 1 {
			err = errors.New("empty answer")
		}
		followerDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancelLeader()

	if err := <-leaderDone; err == nil {
		t.Fatal("cancelled leader reported success")
	}
	if err := <-followerDone; err != nil {
		t.Fatalf("follower inherited the leader's cancellation: %v", err)
	}
	// The abandoned attempt was not stored: the follower ran its own process.
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("process starts = %d, want 2", got)
	}
}

func TestQueryCacheFollowerCancellationDoesNotAffectLeader(t *testing.T) {
	binary := fakeBinary(t, "sleep 0.3\n"+videoJSON+"\n")
	client := New(Options{Binary: binary})
	target := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	leaderDone := make(chan error, 1)
	go func() {
		_, err := client.Query(context.Background(), target, "--no-playlist")
		leaderDone <- err
	}()
	waitForStarts(t, binary, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Query(ctx, target, "--no-playlist"); err == nil {
		t.Fatal("cancelled follower reported success")
	}
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader failed: %v", err)
	}
}

func TestQueryCacheIsBounded(t *testing.T) {
	binary := fakeBinary(t, `echo '{"id":"x","title":"X"}'`+"\n")
	client := New(Options{Binary: binary, QueryCache: QueryCacheOptions{MaxEntries: 4}})
	for i := 0; i < 20; i++ {
		if _, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=id"+strconv.Itoa(i), "--no-playlist"); err != nil {
			t.Fatal(err)
		}
		if n := client.cache.len(); n > 4 {
			t.Fatalf("cache holds %d entries, bound is 4", n)
		}
	}
	// The most recent entry survived eviction.
	before := processStarts(t, binary)
	if _, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=id19", "--no-playlist"); err != nil {
		t.Fatal(err)
	}
	if processStarts(t, binary) != before {
		t.Fatal("most recent entry was evicted")
	}
}

func TestWithoutQueryCacheAlwaysRuns(t *testing.T) {
	binary := fakeBinary(t, videoJSON+"\n")
	client := New(Options{Binary: binary}).WithoutQueryCache()
	for i := 0; i < 2; i++ {
		if _, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "--no-playlist"); err != nil {
			t.Fatal(err)
		}
	}
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("process starts = %d, want 2", got)
	}
}

func TestQueryCacheKeyHidesCredentialPath(t *testing.T) {
	key := queryCacheKey("yt-dlp", []string{"--cookies", "/secret/cookies.txt", "--", "x"})
	if strings.Contains(key, "secret") || strings.Contains(key, "cookies") {
		t.Fatalf("key exposes its inputs: %s", key)
	}
}

func waitForStarts(t *testing.T, binary string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processStarts(t, binary) < n {
		if time.Now().After(deadline) {
			t.Fatalf("process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
