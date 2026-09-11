package orchestrator_test

// Synthetic throughput fixture.
//
// This is a controlled, offline comparison of provider requests and elapsed
// time for the real resolution path: the real YouTube Music, YouTube and
// SoundCloud providers, the real orchestrator, session pool and matcher, and
// a worker loop that applies the job worker's retry rules. Only yt-dlp is
// replaced by a stub that answers from fixed files and sleeps a scaled process
// time. Its numbers compare two code versions under identical conditions; they
// are NOT a measurement of real provider throughput and prove nothing about
// real provider limits.
//
// The fixture is calibrated to production observations (see the PR):
//   - 18% of items share artist and title with another item,
//   - 13% of items carry a direct YouTube video id,
//   - about 9% of distinct YouTube candidates offer an audio only stream,
//   - one of five YouTube search results is also a YouTube Music result,
//   - SoundCloud candidates are 60% previews, 10% DRM protected, 30% full.
//
// Run it explicitly; it is skipped otherwise:
//
//	YTMDL_THROUGHPUT_FIXTURE=1 go test -run TestSyntheticThroughputFixture -v ./internal/orchestrator/
//
// YTMDL_THROUGHPUT_FIXTURE_OUT names a JSON file for the result.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/soundcloud"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/provider/ytmusic"
	"ytdm/backend/internal/ytdlp"
)

// fixtureScale compresses production time: 1 fixture second is 40 real ones.
const fixtureScale = 40.0

const (
	fixtureItems        = 150
	fixtureWorkers      = 2
	fixtureMaxAttempts  = 5
	fixtureProcessSleep = "0.05" // one metadata process: ~2 s at production scale
	fixtureDownload     = 200 * time.Millisecond
)

func scaled(d time.Duration) time.Duration { return time.Duration(float64(d) / fixtureScale) }

// scaledCooldown mirrors jobs.MediaCooldownManager on the fixture clock.
type scaledCooldown struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func cooldownKey(p string) string {
	switch k := strings.ToLower(strings.TrimSpace(p)); k {
	case "youtube", "ytmusic", "youtube-family":
		return "youtube"
	default:
		return k
	}
}

func (c *scaledCooldown) Trigger(p string, d time.Duration) time.Duration {
	if d <= 0 {
		d = time.Minute
	} else if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exp := time.Now().Add(scaled(d))
	if cur, ok := c.until[cooldownKey(p)]; !ok || exp.After(cur) {
		c.until[cooldownKey(p)] = exp
	}
	return d
}

func (c *scaledCooldown) Remaining(p string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rem := time.Until(c.until[cooldownKey(p)])
	if rem <= 0 {
		return 0, false
	}
	// Report production-scale time, as the real manager would.
	return time.Duration(float64(rem) * fixtureScale), true
}

func (c *scaledCooldown) Wait(ctx context.Context, p string) error {
	c.mu.Lock()
	rem := time.Until(c.until[cooldownKey(p)])
	c.mu.Unlock()
	if rem <= 0 {
		return nil
	}
	t := time.NewTimer(rem)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *scaledCooldown) Clear(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.until, cooldownKey(p))
}

type fixtureTrack struct {
	track   music.Track
	ytm     []string
	yt      []string
	sc      []string
	isDupOf int
}

type fixtureWorld struct {
	dir    string
	tracks []fixtureTrack
}

// fixtureFile maps a yt-dlp target onto its answer file. The stub script
// applies the same transformation.
func fixtureFile(dir, target string) string {
	var b strings.Builder
	for _, r := range target {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 200 {
		name = name[:200]
	}
	return filepath.Join(dir, name)
}

func buildFixtureWorld(t *testing.T) *fixtureWorld {
	t.Helper()
	rng := rand.New(rand.NewSource(20260911))
	dir := t.TempDir()
	w := &fixtureWorld{dir: dir}
	write := func(target, body string) {
		if err := os.WriteFile(fixtureFile(dir, target), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	videoSeq := 0
	newVideo := func() string {
		videoSeq++
		return fmt.Sprintf("v%010d", videoSeq)
	}
	goodVideo := map[string]bool{}

	distinct := 0
	for i := 0; i < fixtureItems; i++ {
		// 18% of the items duplicate an earlier one.
		if i > 0 && rng.Float64() < 0.18 {
			src := rng.Intn(len(w.tracks))
			for w.tracks[src].isDupOf >= 0 {
				src = w.tracks[src].isDupOf
			}
			dup := w.tracks[src]
			dup.isDupOf = src
			w.tracks = append(w.tracks, dup)
			continue
		}
		distinct++
		artist := fmt.Sprintf("Fixture Artist %03d", distinct)
		title := fmt.Sprintf("Fixture Song %03d", distinct)
		durationS := 150 + rng.Intn(150)
		ft := fixtureTrack{isDupOf: -1, track: music.Track{
			Title: title, Artists: []string{artist}, DurationMS: durationS * 1000,
		}}
		for k := 0; k < 5; k++ {
			ft.ytm = append(ft.ytm, newVideo())
		}
		ft.yt = append(ft.yt, ft.ytm[1])
		for k := 0; k < 4; k++ {
			ft.yt = append(ft.yt, newVideo())
		}
		for k := 0; k < 3; k++ {
			ft.sc = append(ft.sc, fmt.Sprintf("sc%d%02d", distinct, k))
		}
		if rng.Float64() < 0.13 {
			ft.track.SourceProvider = "ytmusic"
			ft.track.SourceID = ft.ytm[0]
		}
		for _, id := range append(append([]string{}, ft.ytm...), ft.yt[1:]...) {
			goodVideo[id] = rng.Float64() < 0.09
		}

		query := provider.SearchQuery(ft.track)
		var ytmLines, ytLines, scLines []string
		for _, id := range ft.ytm {
			ytmLines = append(ytmLines, fmt.Sprintf(`{"id":%q,"title":%q,"artist":%q}`, id, title, artist))
		}
		for _, id := range ft.yt {
			ytLines = append(ytLines, fmt.Sprintf(`{"id":%q,"title":%q,"duration":%d,"url":"https://www.youtube.com/watch?v=%s"}`,
				id, artist+" - "+title, durationS, id))
		}
		for _, id := range ft.sc {
			page := "https://soundcloud.com/fixture/" + id
			scLines = append(scLines, fmt.Sprintf(`{"id":%q,"title":%q,"uploader":%q,"duration":%d,"webpage_url":%q}`,
				id, title, artist, durationS, page))
			switch r := rng.Float64(); {
			case r < 0.6:
				write(page, fmt.Sprintf(`{"id":%q,"duration":30,"webpage_url":%q,"formats":[{"format_id":"hls_mp3_1_0_preview","acodec":"mp3","vcodec":"none"}]}`, id, page))
			case r < 0.7:
				// The stub answers "<target>_err" on standard error.
				write(page+"_err", "ERROR: [soundcloud] "+id+": This video is DRM protected")
			default:
				write(page, fmt.Sprintf(`{"id":%q,"duration":%d,"webpage_url":%q,"formats":[{"format_id":"http_mp3_128","acodec":"mp3","vcodec":"none"}]}`, id, durationS, page))
			}
		}
		write("https://music.youtube.com/search?q="+url.QueryEscape(query)+"&sp=EgWKAQIIAWoKEAoQCRADEAQQBQ%3D%3D", strings.Join(ytmLines, "\n"))
		write("ytsearch10:"+query, strings.Join(ytLines, "\n"))
		write("scsearch10:"+query, strings.Join(scLines, "\n"))
		for _, id := range append(append([]string{}, ft.ytm...), ft.yt[1:]...) {
			formats := `[{"format_id":"18","acodec":"mp4a.40.2","vcodec":"avc1"}]`
			if goodVideo[id] {
				formats = `[{"format_id":"251","acodec":"opus","vcodec":"none"}]`
			}
			write("https://www.youtube.com/watch?v="+id, fmt.Sprintf(`{"id":%q,"title":%q,"artist":%q,"duration":%d,"webpage_url":"https://www.youtube.com/watch?v=%s","formats":%s}`,
				id, title, artist, durationS, id, formats))
		}
		w.tracks = append(w.tracks, ft)
	}
	return w
}

const fixtureScript = `#!/bin/sh
for last do :; done
kind=extract
for a do [ "$a" = "--flat-playlist" ] && kind=search; done
name=$(printf '%s' "$last" | tr -c 'A-Za-z0-9' '_' | cut -c1-200)
printf '%s %s\n' "$kind" "$last" >> "$FIXTURE_DIR/calls.log"
sleep ` + fixtureProcessSleep + `
if [ -f "$FIXTURE_DIR/$name" ]; then cat "$FIXTURE_DIR/$name"; exit 0; fi
if [ -f "$FIXTURE_DIR/${name}_err" ]; then cat "$FIXTURE_DIR/${name}_err" >&2; exit 1; fi
echo "ERROR: fixture has no answer" >&2
exit 1
`

type fixtureResult struct {
	Items               int            `json:"items"`
	Completed           int            `json:"completed"`
	Failed              map[string]int `json:"failed"`
	Attempts            int            `json:"attempts"`
	YouTubeSearch       int            `json:"youtube_search_processes"`
	YouTubeExtract      int            `json:"youtube_extract_processes"`
	SoundCloudSearch    int            `json:"soundcloud_search_processes"`
	SoundCloudExtract   int            `json:"soundcloud_extract_processes"`
	Downloads           int            `json:"downloads"`
	YouTubePerSuccess   float64        `json:"youtube_requests_per_success"`
	ProviderPerSuccess  float64        `json:"provider_requests_per_success"`
	FixtureSeconds      float64        `json:"fixture_seconds"`
	ProjectedHours      float64        `json:"projected_production_hours"`
	ProjectedPerHour    float64        `json:"projected_successes_per_hour"`
	OutcomeDigest       string         `json:"outcome_digest"`
	SuccessIDs          map[int]string `json:"success_ids"`
	FailedIDs           map[int]string `json:"failed_ids"`
	SessionFailures     int            `json:"session_consecutive_failures"`
	SessionHealthStatus string         `json:"session_health"`
}

func TestSyntheticThroughputFixture(t *testing.T) {
	if os.Getenv("YTMDL_THROUGHPUT_FIXTURE") == "" {
		t.Skip("synthetic fixture; set YTMDL_THROUGHPUT_FIXTURE=1 to run")
	}
	world := buildFixtureWorld(t)
	binary := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(binary, []byte(fixtureScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIXTURE_DIR", world.dir)

	client := ytdlp.New(ytdlp.Options{Binary: binary})
	ytm, err := ytmusic.NewMediaProvider(ytmusic.MediaConfig{Client: client, Limit: 10, RequestsPerSecond: 1.0 * fixtureScale, Burst: 1})
	if err != nil {
		t.Fatal(err)
	}
	yt, err := youtube.New(youtube.Config{Name: youtube.ProviderName, Mode: youtube.SearchVideos, Client: client, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := soundcloud.New(soundcloud.Config{Client: client.WithCookieFile(""), Limit: 10, RequestsPerSecond: 1.0 * fixtureScale, Burst: 3})
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)
	reg.SetDefaults("deezer", "ytmusic")

	storage, err := mediasession.NewCookieStorage(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	session := mediasession.Session{ID: "fixture-session", ProviderFamily: provider.FamilyYouTube, Name: "Fixture", Enabled: true, HealthStatus: mediasession.HealthHealthy}
	if session.CookieRef, err = storage.Store(session.ID, []byte("# Netscape HTTP Cookie File\n")); err != nil {
		t.Fatal(err)
	}
	// Production pool settings: one lease, 0.5 starts/s per session, 2/s
	// family-wide - on the fixture clock.
	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family: provider.FamilyYouTube, MaxLeasesPerSession: 1,
		SessionRequestsPerSec: 0.5 * fixtureScale, SessionBurst: 1,
		GlobalRequestsPerSec: 2.0 * fixtureScale, GlobalBurst: 4, AllowUnknown: true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{session})

	cooldown := &scaledCooldown{until: map[string]time.Time{}}
	orch := orchestrator.New(orchestrator.Options{
		Registry: reg, SessionPool: pool, Cooldown: cooldown,
		Matcher: matcher.New(matcher.Options{MinScore: 70, DurationToleranceMS: 4000}),
	})

	type queued struct {
		idx      int
		attempts int
		notUntil time.Time
	}
	var (
		mu       sync.Mutex
		queue    []queued
		inFlight int
		result   = fixtureResult{Items: len(world.tracks), Failed: map[string]int{}, SuccessIDs: map[int]string{}, FailedIDs: map[int]string{}}
	)
	for i := range world.tracks {
		queue = append(queue, queued{idx: i})
	}
	backoff := []time.Duration{5 * time.Second, 15 * time.Second, 45 * time.Second, 2 * time.Minute, 5 * time.Minute}

	take := func() (queued, bool, bool) {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		for i, q := range queue {
			if !q.notUntil.After(now) {
				queue = append(queue[:i], queue[i+1:]...)
				inFlight++
				return q, true, false
			}
		}
		return queued{}, false, len(queue) == 0 && inFlight == 0
	}
	finish := func(q queued, requeue bool) {
		mu.Lock()
		defer mu.Unlock()
		inFlight--
		if requeue {
			queue = append(queue, q)
		}
	}

	began := time.Now()
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	var wg sync.WaitGroup
	for w := 0; w < fixtureWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				q, ok, done := take()
				if done {
					return
				}
				if !ok {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				track := world.tracks[q.idx].track
				res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
				mu.Lock()
				result.Attempts++
				mu.Unlock()
				switch {
				case err == nil:
					// The download holds the session's execution slot for its
					// whole duration, exactly like the real downloader.
					if res.SessionID != "" {
						release, gerr := pool.ExecutionGate(res.SessionID).Acquire(context.Background())
						if gerr == nil {
							time.Sleep(fixtureDownload)
							release()
						}
					} else {
						time.Sleep(fixtureDownload)
					}
					orch.RecordDownloadOutcome(context.Background(), res.SessionID, nil)
					mu.Lock()
					result.Downloads++
					result.Completed++
					result.SuccessIDs[q.idx] = res.Source.Provider + ":" + res.Source.ID
					mu.Unlock()
					finish(q, false)
				case apperr.IsSessionWait(err):
					wait, _ := apperr.RetryAfter(err)
					if wait < 5*time.Second {
						wait = 5 * time.Second
					}
					q.notUntil = time.Now().Add(scaled(wait))
					finish(q, true)
				case apperr.Retryable(err) && q.attempts+1 < fixtureMaxAttempts:
					q.attempts++
					q.notUntil = time.Now().Add(scaled(backoff[min(q.attempts, len(backoff))-1]))
					finish(q, true)
				default:
					mu.Lock()
					result.Failed[string(apperr.CodeOf(err))]++
					result.FailedIDs[q.idx] = string(apperr.CodeOf(err))
					mu.Unlock()
					finish(q, false)
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(began)

	raw, err := os.ReadFile(filepath.Join(world.dir, "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		kind, target, _ := strings.Cut(line, " ")
		sound := strings.HasPrefix(target, "scsearch") || strings.Contains(target, "soundcloud.com")
		switch {
		case kind == "search" && sound:
			result.SoundCloudSearch++
		case kind == "search":
			result.YouTubeSearch++
		case sound:
			result.SoundCloudExtract++
		default:
			result.YouTubeExtract++
		}
	}
	ytRequests := result.YouTubeSearch + result.YouTubeExtract
	all := ytRequests + result.SoundCloudSearch + result.SoundCloudExtract
	if result.Completed > 0 {
		result.YouTubePerSuccess = float64(ytRequests) / float64(result.Completed)
		result.ProviderPerSuccess = float64(all) / float64(result.Completed)
	}
	result.FixtureSeconds = elapsed.Seconds()
	result.ProjectedHours = elapsed.Hours() * fixtureScale
	result.ProjectedPerHour = float64(result.Completed) / result.ProjectedHours
	s := pool.Sessions()[0]
	result.SessionFailures = s.ConsecutiveFailures
	result.SessionHealthStatus = string(s.HealthStatus)

	keys := make([]int, 0, len(result.SuccessIDs))
	for k := range result.SuccessIDs {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%d=%s;", k, result.SuccessIDs[k])
	}
	result.OutcomeDigest = hex.EncodeToString(h.Sum(nil))[:16]

	out, _ := json.MarshalIndent(result, "", "  ")
	t.Logf("SYNTHETIC FIXTURE - not a real provider measurement\n%s", out)
	if path := os.Getenv("YTMDL_THROUGHPUT_FIXTURE_OUT"); path != "" {
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if result.SessionHealthStatus != string(mediasession.HealthHealthy) {
		t.Fatalf("fixture run changed session health: %+v", s)
	}
}
