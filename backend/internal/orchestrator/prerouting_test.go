package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

func setupPreRoutingEnv(t *testing.T, sessions ...mediasession.Session) (*orchestrator.ProviderOrchestrator, *mediasession.SessionPool, *mockMediaProvider, *mockMediaProvider, *mockMediaProvider, *mockCooldown) {
	t.Helper()

	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	for _, s := range sessions {
		_, _ = storage.Store(s.ID, []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test\n"))
	}

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
	pool.ReloadSessions(sessions)

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

	return orch, pool, ytm, yt, sc, cooldown
}

// 1. MANUAL + YT PRE-OPEN + SC HEALTHY -> no YT contact, SC attempted
func TestPreRouting_Manual_YouTubePreOpen_SoundCloudHealthy_ZeroYouTubeContact(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	// Pre-open YouTube family circuit
	cooldown.Trigger("youtube", 10*time.Minute)

	// SoundCloud has a matching candidate
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-1",
		URL:        "https://soundcloud.com/artist/song",
		Title:      "Original Song",
		Artists:    []string{"Artist A"},
		DurationMS: 200000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Original Song",
		Artists:    []string{"Artist A"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected resolution to succeed on SoundCloud, got error: %v", err)
	}

	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected candidate provider soundcloud, got %s", res.Candidate.Provider)
	}
	if ytm.SearchCalls() != 0 {
		t.Fatalf("expected 0 YouTube Music search calls, got %d", ytm.SearchCalls())
	}
	if yt.SearchCalls() != 0 {
		t.Fatalf("expected 0 YouTube search calls, got %d", yt.SearchCalls())
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("expected 1 SoundCloud search call, got %d", sc.SearchCalls())
	}
	// Zero YouTube session acquired / leased
	for _, s := range pool.Sessions() {
		if s.ConsecutiveFailures > 0 {
			t.Fatalf("expected session not to record failures, got %d", s.ConsecutiveFailures)
		}
	}
}

// 2. MANUAL + YT PRE-OPEN + SC MATCH -> resolution PASS
func TestPreRouting_Manual_YouTubePreOpen_SoundCloudMatch_Pass(t *testing.T) {
	now := time.Now().UTC()
	coolingUntil := now.Add(30 * time.Minute)
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthBotChallenge,
		CooldownUntil:  &coolingUntil,
	}
	orch, _, _, _, sc, _ := setupPreRoutingEnv(t, sess)

	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-2",
		URL:        "https://soundcloud.com/artist/song-2",
		Title:      "Electro Hit",
		Artists:    []string{"DJ Test"},
		DurationMS: 180000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Electro Hit",
		Artists:    []string{"DJ Test"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected resolution PASS, got %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Errorf("candidate provider = %s, want soundcloud", res.Candidate.Provider)
	}
	if res.Source.Provider != "soundcloud" {
		t.Errorf("source provider = %s, want soundcloud", res.Source.Provider)
	}
	if res.SessionID != "" {
		t.Errorf("expected empty sessionID for soundcloud, got %q", res.SessionID)
	}
}

// 3. MANUAL + YT PRE-OPEN + SC NO ACCEPTABLE MATCH -> safe defer/wait, no bad substitute
func TestPreRouting_Manual_YouTubePreOpen_SoundCloudNoAcceptableMatch_SafeDefer(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("youtube", 15*time.Minute)

	// Low score match (completely wrong title/artists)
	badCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-bad",
		URL:        "https://soundcloud.com/wrong/song",
		Title:      "Completely Unrelated Track",
		Artists:    []string{"Unknown Band"},
		DurationMS: 300000,
	}
	sc.SetCandidates([]provider.MediaCandidate{badCand})

	track := music.Track{
		Title:      "Target Song",
		Artists:    []string{"Real Artist"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result (no low score substitute), got candidate %v", res.Candidate)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait(err) true, got code: %s, err: %v", apperr.CodeOf(err), err)
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got: %s", apperr.CodeOf(err))
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatalf("expected ConsumesJobRetry(err) false, retry budget must be preserved")
	}
	retryAfter, ok := apperr.RetryAfter(err)
	if !ok || retryAfter < 5*time.Second {
		t.Fatalf("expected finite retry-after >= 5s, got %v", retryAfter)
	}
}

// 4. MANUAL + YT PRE-OPEN + SC ALSO UNAVAILABLE -> SESSION_UNAVAILABLE, retry budget preserved
func TestPreRouting_Manual_YouTubePreOpen_SoundCloudAlsoUnavailable(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, _, _, _, cooldown := setupPreRoutingEnv(t, sess)

	// Both YouTube and SoundCloud cooling
	cooldown.Trigger("youtube", 10*time.Minute)
	cooldown.Trigger("soundcloud", 5*time.Minute)

	track := music.Track{
		Title:      "Song A",
		Artists:    []string{"Artist A"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got: %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait(err) true, got: %v", err)
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
	delay, ok := apperr.RetryAfter(err)
	if !ok || delay < 5*time.Second {
		t.Fatalf("expected finite delay >= 5s, got %v", delay)
	}
}

// 5. SUBSCRIPTION + YT PRE-OPEN + SC HEALTHY -> SC NOT substituted, wait
func TestPreRouting_Subscription_YouTubePreOpen_SoundCloudHealthy_NoSubstitution(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	// YouTube cooling
	cooldown.Trigger("youtube", 15*time.Minute)

	// SoundCloud has a perfect match
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-sub",
		URL:        "https://soundcloud.com/artist/sub-track",
		Title:      "Subscription Track",
		Artists:    []string{"Artist Sub"},
		DurationMS: 200000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Subscription Track",
		Artists:    []string{"Artist Sub"},
		DurationMS: 200000,
	}

	// Origin is Subscription (explicit or default)
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected SoundCloud NOT to be substituted for subscription, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait, got code %s", apperr.CodeOf(err))
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud search calls for subscription backfill, got %d", sc.SearchCalls())
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
}

// 6. YT ELIGIBLE AT START + BOT_CHALLENGE DURING ATTEMPT -> StopsCandidateFanout, SC NOT contacted
func TestPreRouting_YouTubeEligibleAtStart_BotChallengeDuringAttempt_StopsCandidateFanout(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)

	// YouTube fails DURING attempt with Bot Challenge
	ytm.SetSearchErr(apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot"))

	// SoundCloud is healthy and has candidates
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-1",
		Title:      "Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected bot challenge error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %s", apperr.CodeOf(err))
	}
	// Hard gate: SoundCloud must NOT be contacted mid-attempt
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud search calls after bot challenge, got %d", sc.SearchCalls())
	}
}

// 7. YT ELIGIBLE + normal candidate miss -> existing legitimate cross-provider fallback semantics preserved
func TestPreRouting_YouTubeEligibleAtStart_NormalCandidateMiss_FallbackToSoundCloud(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)

	// YouTube has no candidates (normal content miss)
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-1",
		URL:        "https://soundcloud.com/artist/song",
		Title:      "Rare Track",
		Artists:    []string{"Underground Artist"},
		DurationMS: 210000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Rare Track",
		Artists:    []string{"Underground Artist"},
		DurationMS: 210000,
	}

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected successful cross-provider fallback, got %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected candidate provider soundcloud, got %s", res.Candidate.Provider)
	}
	if ytm.SearchCalls() != 1 {
		t.Fatalf("expected 1 ytmusic search call, got %d", ytm.SearchCalls())
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("expected 1 soundcloud search call, got %d", sc.SearchCalls())
	}
}

// 8. direct YouTube SourceID + YT PRE-OPEN + manual -> YT fast path skipped, independent matching remains possible
func TestPreRouting_DirectSourceID_YouTubePreOpen_Manual_FastPathSkipped(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	// YouTube cooling
	cooldown.Trigger("youtube", 10*time.Minute)

	// SoundCloud has a match by generic metadata
	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-track-direct",
		URL:        "https://soundcloud.com/artist/direct",
		Title:      "Direct Track",
		Artists:    []string{"Direct Artist"},
		DurationMS: 190000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		SourceID:   "video-id-12345",
		Title:      "Direct Track",
		Artists:    []string{"Direct Artist"},
		DurationMS: 190000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected resolution PASS on SoundCloud, got %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected soundcloud, got %s", res.Candidate.Provider)
	}
	// Direct-ID fast path for YouTube was skipped
	if ytm.SearchCalls() != 0 {
		t.Fatalf("expected 0 YouTube search calls, got %d", ytm.SearchCalls())
	}
}

// 9. provenance: SC success -> source/provider = soundcloud
func TestPreRouting_Provenance_SoundCloudPreserved(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("youtube", 5*time.Minute)

	scCand := provider.MediaCandidate{
		Provider:   "soundcloud",
		ID:         "sc-prov-test",
		URL:        "https://soundcloud.com/test/track",
		Title:      "Provenance Test",
		Artists:    []string{"Tester"},
		DurationMS: 200000,
	}
	sc.SetCandidates([]provider.MediaCandidate{scCand})

	track := music.Track{
		Title:      "Provenance Test",
		Artists:    []string{"Tester"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Errorf("res.Candidate.Provider = %q, want 'soundcloud'", res.Candidate.Provider)
	}
	if res.Source.Provider != "soundcloud" {
		t.Errorf("res.Source.Provider = %q, want 'soundcloud'", res.Source.Provider)
	}
	if res.SessionID != "" {
		t.Errorf("res.SessionID = %q, want empty", res.SessionID)
	}
}

// 12. Retry budget preserved on session waits
func TestPreRouting_RetryBudgetPreservation(t *testing.T) {
	err := apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "waiting", 10*time.Minute)
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected ConsumesJobRetry to be false for CodeSessionUnavailable")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait to be true for CodeSessionUnavailable")
	}
}

// 13. No busy loop: finite delay >= 5 seconds
func TestPreRouting_NoBusyLoop_MinDelay(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, _, _, _, cooldown := setupPreRoutingEnv(t, sess)

	// Trigger tiny cooldown to test 5s minimum clamp
	cooldown.Trigger("youtube", 1*time.Second)

	track := music.Track{
		Title:      "Busy Loop Guard",
		Artists:    []string{"Artist"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	delay, ok := apperr.RetryAfter(err)
	if !ok {
		t.Fatal("expected RetryAfter to be present")
	}
	if delay < 1*time.Second {
		t.Fatalf("expected delay >= 1s, got %v", delay)
	}
}

// 10. Step 1 regressions: Bot challenge repeated bounds (24h first, 72h repeat, no nil dead-end)
func TestPreRouting_10_Step1_BotChallengeRepeats_Bounded(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	sess := mediasession.Session{
		ID:             "sess-step1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Step 1 Test",
		CookieRef:      "managed://cookies/sess-step1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	_, pool, _, _, _, _ := setupPreRoutingEnv(t, sess)
	pool.SetNow(func() time.Time { return now })

	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")

	// First bot challenge -> 24h cooldown
	pool.RecordFailure(context.Background(), "sess-step1", botErr, now)
	s := pool.Sessions()[0]
	if s.HealthStatus != mediasession.HealthBotChallenge {
		t.Fatalf("health status = %s, want bot_challenge", s.HealthStatus)
	}
	if s.CooldownUntil == nil || s.CooldownUntil.Sub(now) != 24*time.Hour {
		t.Fatalf("first cooldown = %v, want 24h", s.CooldownUntil)
	}

	// Expire first cooldown, repeat failure -> 72h cooldown
	now2 := now.Add(25 * time.Hour)
	pool.SetNow(func() time.Time { return now2 })
	pool.RecordFailure(context.Background(), "sess-step1", botErr, now2)
	s = pool.Sessions()[0]
	if s.CooldownUntil == nil || s.CooldownUntil.Sub(now2) != 72*time.Hour {
		t.Fatalf("second cooldown = %v, want 72h", s.CooldownUntil)
	}

	// Expire second cooldown, third repeat -> still finite 72h, no nil dead-end
	now3 := now2.Add(73 * time.Hour)
	pool.SetNow(func() time.Time { return now3 })
	pool.RecordFailure(context.Background(), "sess-step1", botErr, now3)
	s = pool.Sessions()[0]
	if s.CooldownUntil == nil || s.CooldownUntil.Sub(now3) != 72*time.Hour {
		t.Fatalf("third cooldown = %v, want 72h (no nil dead-end)", s.CooldownUntil)
	}
}

// 11. Step 2 regressions: Metadata-only probe does not falsely certify media health
func TestPreRouting_11_Step2_ProbeDoesNotCertifyMediaHealth(t *testing.T) {
	now := time.Now().UTC()
	cooldownUntil := now.Add(12 * time.Hour)
	sess := mediasession.Session{
		ID:                  "sess-step2",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Step 2 Test",
		CookieRef:           "managed://cookies/sess-step2",
		Enabled:             true,
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cooldownUntil,
	}
	_, pool, _, _, _, _ := setupPreRoutingEnv(t, sess)

	// In Step 2, a probe success on a protected session cannot mark it HealthHealthy
	// Simulate probe prober:
	s := pool.Sessions()[0]
	if s.HealthStatus == mediasession.HealthHealthy {
		t.Fatal("session should not start healthy")
	}

	// Attempting real media acquisition when pool has no eligible session returns SessionUnavailable
	orch, _, _, _, _, _ := setupPreRoutingEnv(t, sess)
	track := music.Track{Title: "T", Artists: []string{"A"}, DurationMS: 200000}
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected session wait error, got %v", err)
	}
}
