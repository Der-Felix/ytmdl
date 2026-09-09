package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// 1. SC cooling + no successful alternative -> temporary unavailable -> NOT TRACK_NOT_FOUND
func TestPreRoutingDeferral_SoundCloudCooling_NoAlternative_ReturnsSessionUnavailable(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	// SoundCloud is in cooldown
	cooldown.Trigger("soundcloud", 60*time.Second)

	// YouTube providers have no candidates for this track (candidate exhaustion)
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	// SoundCloud would have the candidate if searched
	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-1",
		URL:        "https://soundcloud.com/artist/song",
		Title:      "My Song",
		Artists:    []string{"My Artist"},
		DurationMS: 200000,
	}})

	track := music.Track{
		Title:      "My Song",
		Artists:    []string{"My Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must NOT be TRACK_NOT_FOUND
	if apperr.CodeOf(err) == apperr.CodeTrackNotFound {
		t.Fatalf("critical failure: temporarily cooled provider returned TRACK_NOT_FOUND: %v", err)
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait(err) to be true")
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected ConsumesJobRetry(err) to be false (retry budget preserved)")
	}

	retryAfter, ok := apperr.RetryAfter(err)
	if !ok || retryAfter < 5*time.Second {
		t.Fatalf("expected finite retryAfter >= 5s, got %v (ok=%v)", retryAfter, ok)
	}
}

// 2. SC cooling -> SC provider receives ZERO calls
func TestPreRoutingDeferral_SoundCloudCooling_ZeroSoundCloudCalls(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("soundcloud", 60*time.Second)
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	track := music.Track{
		Title:      "Zero Call Track",
		Artists:    []string{"Artist"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, _ = orch.ResolveMedia(ctx, "soundcloud", track, 5)

	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud search calls while cooling, got %d", sc.SearchCalls())
	}
}

// 3. SC cooling + other eligible provider success -> success
func TestPreRoutingDeferral_SoundCloudCooling_OtherProviderSuccess_Wins(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	// SoundCloud cooling
	cooldown.Trigger("soundcloud", 60*time.Second)

	// YouTube Music has valid candidate
	ytm.SetCandidates([]provider.MediaCandidate{{
		Provider:   "ytmusic",
		ID:         "ytm-success",
		Title:      "Dual Hosted Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}})

	track := music.Track{
		Title:      "Dual Hosted Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}

	// Preferred is soundcloud, but soundcloud is cooling -> falls back to eligible ytmusic
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if err != nil {
		t.Fatalf("expected success on alternate provider, got error: %v", err)
	}
	if res.Candidate.Provider != "ytmusic" {
		t.Fatalf("expected provider ytmusic, got %s", res.Candidate.Provider)
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud calls, got %d", sc.SearchCalls())
	}
	if ytm.SearchCalls() != 1 {
		t.Fatalf("expected 1 ytmusic search call, got %d", ytm.SearchCalls())
	}
}

// 4. SC cooling + later real provider no-match -> temporary-unavailable reason preserved
func TestPreRoutingDeferral_SoundCloudCooling_LaterProviderNoMatch_PreservesTemporaryUnavailable(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	// SoundCloud is preferred but cooling
	cooldown.Trigger("soundcloud", 45*time.Second)

	// Alternate providers are healthy and searched, but produce no matching candidate
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	track := music.Track{
		Title:      "Unmatched Later Track",
		Artists:    []string{"Artist"},
		DurationMS: 190000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Crucial: do NOT return TRACK_NOT_FOUND because soundcloud was never searched
	if apperr.CodeOf(err) == apperr.CodeTrackNotFound {
		t.Fatalf("expected temporary unavailable, got false TRACK_NOT_FOUND: %v", err)
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait(err) = true")
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud calls while cooling, got %d", sc.SearchCalls())
	}
}

// 4b. YouTube preferred healthy + genuine content miss, SoundCloud fallback cooling -> temporary-unavailable preserved
func TestPreRoutingDeferral_YouTubePreferredMiss_SoundCloudFallbackCooling_PreservesTemporaryUnavailable(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	// YouTube is healthy but has no candidates
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	// SoundCloud fallback is cooling
	cooldown.Trigger("soundcloud", 60*time.Second)

	track := music.Track{
		Title:      "Indie Song Missed on YT",
		Artists:    []string{"Indie Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must NOT be TRACK_NOT_FOUND because SoundCloud fallback could not be searched
	if apperr.CodeOf(err) == apperr.CodeTrackNotFound {
		t.Fatalf("expected temporary unavailable, got false TRACK_NOT_FOUND: %v", err)
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait(err) = true")
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud calls while cooling, got %d", sc.SearchCalls())
	}
}

// 5. SC healthy + searched + genuine no-match -> normal TRACK_NOT_FOUND semantics remain
func TestPreRoutingDeferral_SoundCloudHealthy_GenuineNoMatch_ReturnsTrackNotFound(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)

	// All providers healthy, all return 0 candidates
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)
	sc.SetCandidates(nil)

	track := music.Track{
		Title:      "Nonexistent Song",
		Artists:    []string{"Ghost Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Genuine no-match must remain TRACK_NOT_FOUND
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("expected CodeTrackNotFound for genuine miss, got %s", apperr.CodeOf(err))
	}
	if apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait(err) = false for genuine miss")
	}
	if !apperr.ConsumesJobRetry(err) {
		t.Fatal("expected genuine candidate miss to consume retry budget / fail permanently")
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("expected 1 SoundCloud search call, got %d", sc.SearchCalls())
	}
}

// 6. manual + YT pre-open + SC cooling -> temporary wait, finite retry, retry budget preserved
func TestPreRoutingDeferral_Manual_YouTubePreOpen_SoundCloudCooling_TemporaryWait(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("youtube", 10*time.Minute)
	cooldown.Trigger("soundcloud", 40*time.Second)

	track := music.Track{
		Title:      "Waiting Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait, got %s", apperr.CodeOf(err))
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
	delay, ok := apperr.RetryAfter(err)
	if !ok || delay < 5*time.Second {
		t.Fatalf("expected delay >= 5s, got %v (ok=%v)", delay, ok)
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 || sc.SearchCalls() != 0 {
		t.Fatalf("expected zero calls to cooling providers, got ytm=%d yt=%d sc=%d",
			ytm.SearchCalls(), yt.SearchCalls(), sc.SearchCalls())
	}
}

// 7. manual + YT pre-open + SC healthy + acceptable match -> Step 3 success unchanged
func TestPreRoutingDeferral_Manual_YouTubePreOpen_SoundCloudHealthy_MatchPass(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("youtube", 15*time.Minute)

	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-valid",
		Title:      "Electro Hit",
		Artists:    []string{"DJ Test"},
		DurationMS: 180000,
	}})

	track := music.Track{
		Title:      "Electro Hit",
		Artists:    []string{"DJ Test"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected resolution PASS, got error: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected soundcloud candidate, got %s", res.Candidate.Provider)
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("expected zero YouTube calls, got ytm=%d yt=%d", ytm.SearchCalls(), yt.SearchCalls())
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("expected 1 SoundCloud call, got %d", sc.SearchCalls())
	}
}

// 8. manual + YT pre-open + SC healthy + unacceptable/no match -> no weak substitute -> temporary YT availability preserved
func TestPreRoutingDeferral_Manual_YouTubePreOpen_SoundCloudNoAcceptableMatch_NoWeakSubstitute(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, cooldown := setupPreRoutingEnv(t, sess)

	cooldown.Trigger("youtube", 12*time.Minute)

	// Low score match (completely wrong title/artists)
	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-unrelated",
		Title:      "Wrong Song Completely",
		Artists:    []string{"Wrong Band"},
		DurationMS: 300000,
	}})

	track := music.Track{
		Title:      "Real Target Song",
		Artists:    []string{"Target Band"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result (no weak substitute), got candidate: %v", res.Candidate)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait true, got %s", apperr.CodeOf(err))
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected retry budget preserved")
	}
	if ytm.SearchCalls() != 0 {
		t.Fatalf("expected 0 YouTube calls, got %d", ytm.SearchCalls())
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("expected 1 SoundCloud call, got %d", sc.SearchCalls())
	}
}

// 9. subscription + YT pre-open + SC healthy -> SC NOT contacted/substituted
func TestPreRoutingDeferral_Subscription_YouTubePreOpen_SoundCloudHealthy_NoSubstitution(t *testing.T) {
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

	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-sub",
		Title:      "Sub Track",
		Artists:    []string{"Sub Artist"},
		DurationMS: 200000,
	}})

	track := music.Track{
		Title:      "Sub Track",
		Artists:    []string{"Sub Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait, got %s", apperr.CodeOf(err))
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud calls for subscription, got %d", sc.SearchCalls())
	}
}

// 10. YT healthy at start + BOT_CHALLENGE during attempt -> SC NOT contacted
func TestPreRoutingDeferral_YouTubeHealthyAtStart_BotChallengeDuringAttempt_NoSoundCloudFallback(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)

	ytm.SetSearchErr(apperr.New(apperr.CodeSessionBotChallenge, "bot challenge mid-attempt"))
	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-1",
		Title:      "Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}})

	track := music.Track{
		Title:      "Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %s", apperr.CodeOf(err))
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("expected 0 SoundCloud search calls after bot challenge, got %d", sc.SearchCalls())
	}
}

// 11. RATE_LIMITED / AUTH_FAILED during YT attempt -> same-attempt fanout still blocked
func TestPreRoutingDeferral_RateLimitedAndAuthFailed_HaltsSameAttemptFanout(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	// Subtest 11a: RATE_LIMITED
	t.Run("RATE_LIMITED", func(t *testing.T) {
		orch, _, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)
		ytm.SetSearchErr(apperr.New(apperr.CodeSessionRateLimited, "session rate limited"))
		sc.SetCandidates([]provider.MediaCandidate{{Provider: "soundcloud", ID: "sc-1", Title: "S", Artists: []string{"A"}, DurationMS: 200000}})
		track := music.Track{Title: "S", Artists: []string{"A"}, DurationMS: 200000}

		ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
		_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
		if err == nil {
			t.Fatal("expected error")
		}
		if apperr.CodeOf(err) != apperr.CodeSessionRateLimited {
			t.Fatalf("expected CodeSessionRateLimited, got %s", apperr.CodeOf(err))
		}
		if sc.SearchCalls() != 0 {
			t.Fatalf("expected 0 SoundCloud search calls, got %d", sc.SearchCalls())
		}
	})

	// Subtest 11b: AUTH_FAILED
	t.Run("AUTH_FAILED", func(t *testing.T) {
		orch, _, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)
		ytm.SetSearchErr(apperr.New(apperr.CodeSessionAuthFailed, "session auth failed"))
		sc.SetCandidates([]provider.MediaCandidate{{Provider: "soundcloud", ID: "sc-1", Title: "S", Artists: []string{"A"}, DurationMS: 200000}})
		track := music.Track{Title: "S", Artists: []string{"A"}, DurationMS: 200000}

		ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
		_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
		if err == nil {
			t.Fatal("expected error")
		}
		if apperr.CodeOf(err) != apperr.CodeSessionAuthFailed {
			t.Fatalf("expected CodeSessionAuthFailed, got %s", apperr.CodeOf(err))
		}
		if sc.SearchCalls() != 0 {
			t.Fatalf("expected 0 SoundCloud search calls, got %d", sc.SearchCalls())
		}
	})
}

// 12. No busy loop: finite delay >= 5 seconds
func TestPreRoutingDeferral_NoBusyLoop_MinFiveSeconds(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, _, cooldown := setupPreRoutingEnv(t, sess)

	// Tiny cooldown of 1 second on SoundCloud
	cooldown.Trigger("soundcloud", 1*time.Second)
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)

	track := music.Track{
		Title:      "Anti-Busy-Loop Track",
		Artists:    []string{"Artist"},
		DurationMS: 180000,
	}

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, err := orch.ResolveMedia(ctx, "soundcloud", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	delay, ok := apperr.RetryAfter(err)
	if !ok {
		t.Fatal("expected RetryAfter present")
	}
	if delay < 5*time.Second {
		t.Fatalf("expected retry delay clamped to >= 5s, got %v", delay)
	}
}
