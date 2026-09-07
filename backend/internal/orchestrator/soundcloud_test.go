package orchestrator_test

import (
	"context"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// TEST B6: Content Fallback Test
// YouTube-family returns TrackNotFound -> SoundCloud fallback is selected and succeeds
func TestOrchestrator_SoundCloud_YouTubeTrackNotFound_FallsBackToSoundCloud(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}
	_, _ = storage.Store("sess-a", []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test\n"))

	cfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sessA})

	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")
	sc := newMockProvider("soundcloud")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)
	reg.SetDefaults("deezer", "ytmusic")

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	cooldown := newMockCooldown()
	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     engine,
		Cooldown:    cooldown,
	})

	// YouTube Music & YouTube return no candidates (or candidate exhaustion)
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	// SoundCloud has a matching candidate
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-1",
		URL:        "https://soundcloud.com/artist/track-1",
		Title:      "Indie Song",
		Artists:    []string{"Indie Artist"},
		DurationMS: 200000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Indie Song",
		Artists:    []string{"Indie Artist"},
		DurationMS: 200000,
	}

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}

	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected SoundCloud candidate, got %s", res.Candidate.Provider)
	}
	if res.Source.Provider != "soundcloud" {
		t.Fatalf("expected SoundCloud source, got %s", res.Source.Provider)
	}
	if res.SessionID != "" {
		t.Fatalf("expected empty SessionID on SoundCloud source, got %q", res.SessionID)
	}
	if sc.LastCookieFile() != "" {
		t.Fatalf("expected SoundCloud to NEVER receive YouTube cookies, got %q", sc.LastCookieFile())
	}

	// YouTube session in pool should still be healthy (not penalized for content TrackNotFound)
	s := pool.Sessions()[0]
	if s.HealthStatus != mediasession.HealthHealthy {
		t.Fatalf("expected session A to remain healthy, got %s", s.HealthStatus)
	}
}

// TEST B7: YouTube Protection Failure Test
// YouTube returns session/family protection failure -> current attempt STOP, NO immediate SoundCloud
func TestOrchestrator_SoundCloud_YouTubeProtectionFailure_NoImmediateFallback(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	storageDir := t.TempDir()
	storage, _ := mediasession.NewCookieStorage(storageDir, nil)
	_, _ = storage.Store("sess-a", []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test\n"))

	cfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sessA})

	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")
	sc := newMockProvider("soundcloud")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)
	reg.SetDefaults("deezer", "ytmusic")

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	cooldown := newMockCooldown()
	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     engine,
		Cooldown:    cooldown,
	})

	// YouTube Music encounters a Bot Challenge
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you’re not a bot")
	ytm.SetSearchErr(botErr)

	// SoundCloud has a valid candidate
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-1",
		URL:        "https://soundcloud.com/artist/track-1",
		Title:      "Some Song",
		Artists:    []string{"Some Artist"},
		DurationMS: 180000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Some Song",
		Artists:    []string{"Some Artist"},
		DurationMS: 180000,
	}

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)

	// Must stop attempt immediately with Bot Challenge error
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %v", apperr.CodeOf(err))
	}
	if res != nil {
		t.Fatalf("expected nil result, got %+v", res)
	}

	// SoundCloud must NOT have been called
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud search calls, got %d", sc.SearchCalls())
	}
}

// TEST B8: SoundCloud Systemic Failure Test
// SoundCloud systemic failure cools down soundcloud family, does NOT poison YouTube
func TestOrchestrator_SoundCloud_SystemicFailure_IsolatedCooldown(t *testing.T) {
	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")
	sc := newMockProvider("soundcloud")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	cooldown := newMockCooldown()
	orch := orchestrator.New(orchestrator.Options{
		Registry: reg,
		Matcher:  engine,
		Cooldown: cooldown,
	})

	// SoundCloud search fails with 429 ProviderRateLimited
	rateErr := apperr.New(apperr.CodeProviderRateLimited, "HTTP Error 429: Too Many Requests")
	sc.SetSearchErr(rateErr)

	track := music.Track{
		Title:      "Track X",
		Artists:    []string{"Artist X"},
		DurationMS: 200000,
	}

	ctx := context.Background()
	_, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if err == nil {
		t.Fatal("expected rate limit error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeProviderRateLimited {
		t.Fatalf("expected CodeProviderRateLimited, got %v", apperr.CodeOf(err))
	}

	// SoundCloud family should be cooling down
	if _, cooling := cooldown.Remaining("soundcloud"); !cooling {
		t.Fatal("expected soundcloud family to be cooling")
	}

	// YouTube family must NOT be cooling down
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("youtube family must NOT be cooling due to soundcloud failure")
	}
	if _, cooling := cooldown.Remaining("ytmusic"); cooling {
		t.Fatal("ytmusic must NOT be cooling due to soundcloud failure")
	}
}

// TEST B9: Candidate Fallback within SoundCloud
// Candidate 1 fails with candidate error -> Candidate 2 resolves successfully
func TestOrchestrator_SoundCloud_CandidateFallback(t *testing.T) {
	sc := newMockProvider("soundcloud")
	reg := provider.NewRegistry()
	reg.RegisterMedia(sc)

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	orch := orchestrator.New(orchestrator.Options{
		Registry: reg,
		Matcher:  engine,
	})

	cand1 := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-cand-1",
		URL:        "https://soundcloud.com/artist/cand-1",
		Title:      "Awesome Track",
		Artists:    []string{"Great Artist"},
		DurationMS: 200000,
	}
	cand2 := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-cand-2",
		URL:        "https://soundcloud.com/artist/cand-2",
		Title:      "Awesome Track",
		Artists:    []string{"Great Artist"},
		DurationMS: 200000,
	}

	sc.SetCandidates([]provider.MediaCandidate{cand1, cand2})
	sc.SetResolveErr("sc-cand-1", apperr.New(apperr.CodeTrackNotFound, "track removed by uploader"))

	track := music.Track{
		Title:      "Awesome Track",
		Artists:    []string{"Great Artist"},
		DurationMS: 200000,
	}

	res, err := orch.ResolveMedia(context.Background(), "soundcloud", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}

	if res.Candidate.ID != "sc-cand-2" {
		t.Fatalf("expected candidate 2 to be resolved, got %s", res.Candidate.ID)
	}
	if res.Source.SessionID != "" {
		t.Fatalf("expected empty session ID, got %q", res.Source.SessionID)
	}
}

// TEST B20: Complete Provider Exhaustion
func TestOrchestrator_SoundCloud_CompleteExhaustion(t *testing.T) {
	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")
	sc := newMockProvider("soundcloud")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)

	engine := matcher.New(matcher.Options{
		MinScore: 70.0,
	})

	orch := orchestrator.New(orchestrator.Options{
		Registry: reg,
		Matcher:  engine,
	})

	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)
	sc.SetCandidates(nil)

	track := music.Track{
		Title:   "Nonexistent Track",
		Artists: []string{"Nonexistent Artist"},
	}

	_, err := orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error on exhaustion, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("expected CodeTrackNotFound, got %v", apperr.CodeOf(err))
	}
}

// TEST B3 & B20: RecordDownloadOutcome does not trigger YouTube cooldown on empty SessionID
func TestOrchestrator_RecordDownloadOutcome_SoundCloudIsolation(t *testing.T) {
	cooldown := newMockCooldown()
	orch := orchestrator.New(orchestrator.Options{
		Cooldown: cooldown,
	})

	// Download failed with Provider scope error, but sessionID is empty (SoundCloud)
	orch.RecordDownloadOutcome(context.Background(), "", apperr.New(apperr.CodeProviderRateLimited, "rate limit"))

	// YouTube family cooldown should NOT have been triggered
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("expected YouTube family NOT to be cooling for SoundCloud download outcome")
	}

	// But if sessionID is non-empty (YouTube session):
	orch.RecordDownloadOutcome(context.Background(), "yt-sess-1", apperr.New(apperr.CodeProviderRateLimited, "rate limit"))
	if _, cooling := cooldown.Remaining("youtube"); !cooling {
		t.Fatal("expected YouTube family to be cooling for YouTube session download outcome")
	}
}
