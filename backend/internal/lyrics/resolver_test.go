package lyrics_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/genius"
	"ytdm/backend/internal/provider/ytmusic"
)

type stubProvider struct {
	name    string
	result  *music.Lyrics
	err     error
	calls   int
	mediaID string
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Lyrics(_ context.Context, _ music.Track, mediaID string) (*music.Lyrics, error) {
	s.calls++
	s.mediaID = mediaID
	return s.result, s.err
}

func newResolver(providers ...provider.LyricsProvider) *lyrics.Resolver {
	return lyrics.NewResolver(lyrics.ResolverOptions{
		Providers:     providers,
		RatePerSecond: 10_000,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func aTrack() music.Track {
	return music.Track{Title: "A", Artists: []string{"X"}, DurationMS: 1000}
}

func TestResolverStopsAtTheFirstHit(t *testing.T) {
	primary := &stubProvider{name: "lrclib", result: &music.Lyrics{
		Provider: "lrclib", Synced: true, LRC: "[00:01.00]a", PlainText: "a",
	}}
	fallback := &stubProvider{name: "ytmusic"}

	got, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "v")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.State() != music.LyricsAvailableSynced {
		t.Fatalf("state = %q", got.State())
	}
	if fallback.calls != 0 {
		t.Error("the fallback must not be asked after a hit")
	}
}

func TestResolverInstrumentalIsFinal(t *testing.T) {
	primary := &stubProvider{name: "lrclib", result: &music.Lyrics{Provider: "lrclib", Instrumental: true}}
	fallback := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "wrong"}}

	got, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "v")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.State() != music.LyricsInstrumental {
		t.Fatalf("state = %q, want instrumental", got.State())
	}
	if fallback.calls != 0 {
		t.Error("an instrumental verdict is final; the fallback must not be asked")
	}
}

func TestResolverFallsThroughOnAMiss(t *testing.T) {
	primary := &stubProvider{name: "lrclib"}
	fallback := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "a"}}

	got, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "video-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.State() != music.LyricsAvailablePlain {
		t.Fatalf("state = %q", got.State())
	}
	if fallback.mediaID != "video-1" {
		t.Errorf("the media id was not handed to the fallback: %q", fallback.mediaID)
	}
}

// TestResolverMissIsDefinitive is what lets the caller record not_found.
func TestResolverMissIsDefinitive(t *testing.T) {
	_, err := newResolver(&stubProvider{name: "lrclib"}, &stubProvider{name: "ytmusic"}).
		Resolve(context.Background(), aTrack(), "")
	if !errors.Is(err, lyrics.ErrNoLyrics) {
		t.Fatalf("err = %v, want ErrNoLyrics", err)
	}
	if errors.Is(err, lyrics.ErrLookupFailed) {
		t.Error("a clean miss must not look like a failure")
	}
}

// TestResolverTransientFailureIsNotAMiss is the core of the state machine: a
// provider outage must never be recorded as "this track has no lyrics".
func TestResolverTransientFailureIsNotAMiss(t *testing.T) {
	primary := &stubProvider{name: "lrclib", err: apperr.New(apperr.CodeProviderUnavailable, "down")}
	fallback := &stubProvider{name: "ytmusic"}

	_, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "")
	if !errors.Is(err, lyrics.ErrLookupFailed) {
		t.Fatalf("err = %v, want ErrLookupFailed", err)
	}
	if errors.Is(err, lyrics.ErrNoLyrics) {
		t.Fatal("a failed lookup must never be reported as a definitive miss")
	}
	if fallback.calls != 1 {
		t.Errorf("the fallback was asked %d times, want 1", fallback.calls)
	}
}

func TestResolverAFailingPrimaryStillAllowsAHit(t *testing.T) {
	primary := &stubProvider{name: "lrclib", err: apperr.New(apperr.CodeProviderUnavailable, "down")}
	fallback := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "a"}}

	got, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "")
	if err != nil {
		t.Fatalf("a failing primary must not fail the resolve: %v", err)
	}
	if got.State() != music.LyricsAvailablePlain {
		t.Fatalf("state = %q", got.State())
	}
}

func TestResolverRateLimitStopsImmediately(t *testing.T) {
	primary := &stubProvider{name: "lrclib", err: lyrics.NewRateLimitError("lrclib", 5*time.Second)}
	fallback := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "a"}}

	_, err := newResolver(primary, fallback).Resolve(context.Background(), aTrack(), "")
	wait, limited := lyrics.RetryAfter(err)
	if !limited || wait != 5*time.Second {
		t.Fatalf("RetryAfter = %v, %v", wait, limited)
	}
	if fallback.calls != 0 {
		t.Error("a rate limit must stop the resolve, not move on to the next provider")
	}
}

func TestResolverWithoutProvidersFails(t *testing.T) {
	_, err := newResolver().Resolve(context.Background(), aTrack(), "")
	if !errors.Is(err, lyrics.ErrLookupFailed) {
		t.Fatalf("err = %v, want ErrLookupFailed", err)
	}
}

func TestResolverHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newResolver(&stubProvider{name: "lrclib"}).Resolve(ctx, aTrack(), "")
	if !errors.Is(err, lyrics.ErrLookupFailed) {
		t.Fatalf("err = %v, want ErrLookupFailed", err)
	}
}

func TestLimiterPacesCalls(t *testing.T) {
	limiter := lyrics.NewLimiter(2, 1) // 500 ms apart
	if !limiter.Enabled() {
		t.Fatal("the limiter must be enabled for a positive rate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("the first call must pass immediately: %v", err)
	}
	if err := limiter.Wait(ctx); err == nil {
		t.Fatal("the second call must wait, and here it must run out of context")
	}
}

func TestLimiterDisabled(t *testing.T) {
	limiter := lyrics.NewLimiter(0, 1)
	if limiter.Enabled() {
		t.Fatal("a rate of zero disables pacing")
	}
	for i := 0; i < 5; i++ {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
}

func TestResolverThreeTierChain_LRCLIB_YTM_Genius(t *testing.T) {
	ctx := context.Background()
	track := aTrack()

	// 1. LRCLIB hit: YTM and Genius must not be called
	pLRC := &stubProvider{name: "lrclib", result: &music.Lyrics{Provider: "lrclib", Synced: true, LRC: "[00:01.00]test", PlainText: "test"}}
	pYTM := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "ytm"}}
	pGenius := &stubProvider{name: "genius", result: &music.Lyrics{Provider: "genius", PlainText: "genius"}}

	res1, err := newResolver(pLRC, pYTM, pGenius).Resolve(ctx, track, "")
	if err != nil || res1 == nil || res1.Provider != "lrclib" {
		t.Fatalf("expected lrclib win, got %v, %v", res1, err)
	}
	if pYTM.calls != 0 || pGenius.calls != 0 {
		t.Errorf("expected 0 calls to fallback providers, got ytm=%d, genius=%d", pYTM.calls, pGenius.calls)
	}

	// 2. LRCLIB miss, YTM hit: Genius must not be called
	pLRC2 := &stubProvider{name: "lrclib"}
	pYTM2 := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", PlainText: "ytm"}}
	pGenius2 := &stubProvider{name: "genius", result: &music.Lyrics{Provider: "genius", PlainText: "genius"}}

	res2, err := newResolver(pLRC2, pYTM2, pGenius2).Resolve(ctx, track, "")
	if err != nil || res2 == nil || res2.Provider != "ytmusic" {
		t.Fatalf("expected ytmusic win, got %v, %v", res2, err)
	}
	if pGenius2.calls != 0 {
		t.Errorf("expected 0 calls to genius fallback, got %d", pGenius2.calls)
	}

	// 3. LRCLIB miss, YTM miss, Genius hit: Genius wins as last resort
	pLRC3 := &stubProvider{name: "lrclib"}
	pYTM3 := &stubProvider{name: "ytmusic"}
	pGenius3 := &stubProvider{name: "genius", result: &music.Lyrics{Provider: "genius", PlainText: "genius lyrics"}}

	res3, err := newResolver(pLRC3, pYTM3, pGenius3).Resolve(ctx, track, "")
	if err != nil || res3 == nil || res3.Provider != "genius" {
		t.Fatalf("expected genius win, got %v, %v", res3, err)
	}
	if pLRC3.calls != 1 || pYTM3.calls != 1 || pGenius3.calls != 1 {
		t.Errorf("expected 1 call each, got lrc=%d, ytm=%d, genius=%d", pLRC3.calls, pYTM3.calls, pGenius3.calls)
	}

	// 4. All miss: clean ErrNoLyrics
	pLRC4 := &stubProvider{name: "lrclib"}
	pYTM4 := &stubProvider{name: "ytmusic"}
	pGenius4 := &stubProvider{name: "genius"}

	_, err = newResolver(pLRC4, pYTM4, pGenius4).Resolve(ctx, track, "")
	if !errors.Is(err, lyrics.ErrNoLyrics) {
		t.Fatalf("expected ErrNoLyrics when all providers miss, got %v", err)
	}
}

func TestResolverShortCircuit_Instrumental_Synced_Plain(t *testing.T) {
	ctx := context.Background()
	track := aTrack()

	// 1. LRCLIB instrumental -> YTM and Genius MUST NOT be called (0 calls)
	pLRCInst := &stubProvider{name: "lrclib", result: &music.Lyrics{Provider: "lrclib", Instrumental: true}}
	pYTM1 := &stubProvider{name: "ytmusic"}
	pGenius1 := &stubProvider{name: "genius"}

	resInst, err := newResolver(pLRCInst, pYTM1, pGenius1).Resolve(ctx, track, "")
	if err != nil || resInst == nil {
		t.Fatalf("expected instrumental hit, got res=%v, err=%v", resInst, err)
	}
	if resInst.State() != music.LyricsInstrumental {
		t.Errorf("expected state Instrumental, got %s", resInst.State())
	}
	if pYTM1.calls != 0 {
		t.Errorf("YTM was called %d times after instrumental, want 0", pYTM1.calls)
	}
	if pGenius1.calls != 0 {
		t.Errorf("Genius was called %d times after instrumental, want 0", pGenius1.calls)
	}

	// 2. LRCLIB available_synced -> later calls = 0
	pLRCSynced := &stubProvider{name: "lrclib", result: &music.Lyrics{Provider: "lrclib", Synced: true, LRC: "[00:01.00]test"}}
	pYTM2 := &stubProvider{name: "ytmusic"}
	pGenius2 := &stubProvider{name: "genius"}

	resSynced, err := newResolver(pLRCSynced, pYTM2, pGenius2).Resolve(ctx, track, "")
	if err != nil || resSynced == nil || resSynced.State() != music.LyricsAvailableSynced {
		t.Fatalf("expected synced hit, got res=%v, err=%v", resSynced, err)
	}
	if pYTM2.calls != 0 || pGenius2.calls != 0 {
		t.Errorf("later calls after synced: ytm=%d, genius=%d, want 0", pYTM2.calls, pGenius2.calls)
	}

	// 3. LRCLIB available_plain -> later calls = 0
	pLRCPlain := &stubProvider{name: "lrclib", result: &music.Lyrics{Provider: "lrclib", Synced: false, PlainText: "plain"}}
	pYTM3 := &stubProvider{name: "ytmusic"}
	pGenius3 := &stubProvider{name: "genius"}

	resPlain, err := newResolver(pLRCPlain, pYTM3, pGenius3).Resolve(ctx, track, "")
	if err != nil || resPlain == nil || resPlain.State() != music.LyricsAvailablePlain {
		t.Fatalf("expected plain hit, got res=%v, err=%v", resPlain, err)
	}
	if pYTM3.calls != 0 || pGenius3.calls != 0 {
		t.Errorf("later calls after plain: ytm=%d, genius=%d, want 0", pYTM3.calls, pGenius3.calls)
	}

	// 4. LRCLIB miss + YTM available_plain -> Genius calls = 0
	pLRCMiss := &stubProvider{name: "lrclib"}
	pYTMPlain := &stubProvider{name: "ytmusic", result: &music.Lyrics{Provider: "ytmusic", Synced: false, PlainText: "ytm plain"}}
	pGenius4 := &stubProvider{name: "genius"}

	resYTM, err := newResolver(pLRCMiss, pYTMPlain, pGenius4).Resolve(ctx, track, "")
	if err != nil || resYTM == nil || resYTM.Provider != "ytmusic" {
		t.Fatalf("expected ytm hit, got res=%v, err=%v", resYTM, err)
	}
	if pGenius4.calls != 0 {
		t.Errorf("Genius was called %d times after YTM hit, want 0", pGenius4.calls)
	}

	// 5. LRCLIB miss + YTM miss + Genius available_plain -> Genius calls = 1
	pLRCMiss2 := &stubProvider{name: "lrclib"}
	pYTMMiss := &stubProvider{name: "ytmusic"}
	pGeniusHit := &stubProvider{name: "genius", result: &music.Lyrics{Provider: "genius", Synced: false, PlainText: "genius plain"}}

	resGenius, err := newResolver(pLRCMiss2, pYTMMiss, pGeniusHit).Resolve(ctx, track, "")
	if err != nil || resGenius == nil || resGenius.Provider != "genius" {
		t.Fatalf("expected genius hit, got res=%v, err=%v", resGenius, err)
	}
	if pGeniusHit.calls != 1 {
		t.Errorf("Genius was called %d times on fallback, want 1", pGeniusHit.calls)
	}
}

func TestResolverGeniusDisabled(t *testing.T) {
	ctx := context.Background()
	track := aTrack()

	pLRC := &stubProvider{name: "lrclib"}
	pYTM := &stubProvider{name: "ytmusic"}

	// When Genius is disabled via config, it returns (nil, nil) immediately without making any requests
	geniusProv := genius.NewLyricsProvider(genius.Config{
		Enabled: false,
	})

	resolver := lyrics.NewResolver(lyrics.ResolverOptions{
		Providers: []provider.LyricsProvider{pLRC, pYTM, geniusProv},
	})

	res, err := resolver.Resolve(ctx, track, "")
	if !errors.Is(err, lyrics.ErrNoLyrics) {
		t.Fatalf("expected ErrNoLyrics when genius disabled and prior providers miss, got res=%v, err=%v", res, err)
	}
	if pLRC.calls != 1 || pYTM.calls != 1 {
		t.Errorf("expected 1 call to lrc and ytm, got lrc=%d, ytm=%d", pLRC.calls, pYTM.calls)
	}
}

type countedProvider struct {
	inner provider.LyricsProvider
	calls int
}

func (c *countedProvider) Name() string { return c.inner.Name() }
func (c *countedProvider) Lyrics(ctx context.Context, t music.Track, m string) (*music.Lyrics, error) {
	c.calls++
	return c.inner.Lyrics(ctx, t, m)
}

func TestLiveResolverCallCounter_Instrumental_ShortCircuit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpClient := &http.Client{Timeout: 10 * time.Second}

	realLRC := &countedProvider{inner: lyrics.NewLRCLib(lyrics.LRCLibConfig{Client: httpClient})}
	realYTM := &countedProvider{inner: ytmusic.NewLyricsProvider(ytmusic.Config{HTTPClient: httpClient})}
	realGenius := &countedProvider{inner: genius.NewLyricsProvider(genius.Config{
		Enabled:    true,
		Timeout:    10 * time.Second,
		HTTPClient: httpClient,
		Logger:     logger,
	})}

	resolver := lyrics.NewResolver(lyrics.ResolverOptions{
		Providers: []provider.LyricsProvider{realLRC, realYTM, realGenius},
		Logger:    logger,
	})

	ctx := context.Background()

	// 1. Sample 6: Kevin MacLeod - Bumbly March (LRCLIB Instrumental)
	trackBumbly := music.Track{Title: "Bumbly March", Artists: []string{"Kevin MacLeod"}}
	resBumbly, err := resolver.Resolve(ctx, trackBumbly, "")
	if err != nil || resBumbly == nil {
		t.Fatalf("expected resolution for Bumbly March, got %v, %v", resBumbly, err)
	}
	if resBumbly.State() != music.LyricsInstrumental {
		t.Fatalf("expected state Instrumental for Bumbly March, got %s", resBumbly.State())
	}
	if realLRC.calls != 1 {
		t.Errorf("LRCLIB calls = %d, want 1", realLRC.calls)
	}
	if realYTM.calls != 0 {
		t.Errorf("YTM calls = %d, want 0", realYTM.calls)
	}
	if realGenius.calls != 0 {
		t.Errorf("Genius calls = %d, want 0", realGenius.calls)
	}

	// Reset counters
	realLRC.calls = 0
	realYTM.calls = 0
	realGenius.calls = 0

	// 2. Sample 7: Camille Saint-Saëns - Aquarium (LRCLIB Instrumental)
	trackAquarium := music.Track{Title: "Aquarium", Artists: []string{"Camille Saint-Saëns"}}
	resAquarium, err := resolver.Resolve(ctx, trackAquarium, "")
	if err != nil || resAquarium == nil {
		t.Fatalf("expected resolution for Aquarium, got %v, %v", resAquarium, err)
	}
	if resAquarium.State() != music.LyricsInstrumental {
		t.Fatalf("expected state Instrumental for Aquarium, got %s", resAquarium.State())
	}
	if realLRC.calls != 1 {
		t.Errorf("LRCLIB calls = %d, want 1", realLRC.calls)
	}
	if realYTM.calls != 0 {
		t.Errorf("YTM calls = %d, want 0", realYTM.calls)
	}
	if realGenius.calls != 0 {
		t.Errorf("Genius calls = %d, want 0", realGenius.calls)
	}
}
