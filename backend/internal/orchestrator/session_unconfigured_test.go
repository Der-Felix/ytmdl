package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// 1. zero sessions configured -> SESSION_NOT_FOUND / truthful non-wait semantic -> NOT SESSION_UNAVAILABLE loop
func TestSessionUnconfigured_Case1_ZeroSessionsConfigured(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}
	cfg := mediasession.PoolConfig{
		Family:       provider.FamilyYouTube,
		AllowUnknown: true,
	}
	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)

	// Acquire returns SESSION_NOT_FOUND (not SESSION_UNAVAILABLE)
	_, acqErr := pool.Acquire(context.Background())
	if acqErr == nil {
		t.Fatal("expected Acquire error for zero sessions, got nil")
	}
	if apperr.CodeOf(acqErr) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got: %s", apperr.CodeOf(acqErr))
	}
	if apperr.IsSessionWait(acqErr) {
		t.Fatal("expected IsSessionWait to be false for CodeSessionNotFound")
	}
	if !apperr.ConsumesJobRetry(acqErr) {
		t.Fatal("expected ConsumesJobRetry to be true for CodeSessionNotFound")
	}

	// HasEligibleSession returns (false, 0) - NOT 15m delay
	hasEligible, wait := pool.HasEligibleSession()
	if hasEligible {
		t.Fatal("expected HasEligibleSession to be false for zero sessions")
	}
	if wait != 0 {
		t.Fatalf("expected wait=0 for zero sessions, got %v", wait)
	}

	// Availability reports PoolStateNoUsableSession
	avail := pool.Availability()
	if avail.State != mediasession.PoolStateNoUsableSession {
		t.Fatalf("expected PoolStateNoUsableSession, got: %v", avail.State)
	}
	if avail.RetryAfter != 0 {
		t.Fatalf("expected RetryAfter=0, got %v", avail.RetryAfter)
	}
}

// 2. all sessions disabled -> no endless retry_wait
func TestSessionUnconfigured_Case2_AllSessionsDisabled_NoEndlessRetryWait(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-disabled-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Disabled Session",
		CookieRef:      "managed://cookies/sess-disabled-1",
		Enabled:        false,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, pool, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)

	// Pool returns SESSION_NOT_FOUND on Acquire
	_, acqErr := pool.Acquire(context.Background())
	if apperr.CodeOf(acqErr) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound on Acquire, got: %s", apperr.CodeOf(acqErr))
	}

	// HasEligibleSession returns (false, 0)
	hasEligible, wait := pool.HasEligibleSession()
	if hasEligible || wait != 0 {
		t.Fatalf("expected (false, 0), got (%v, %v)", hasEligible, wait)
	}

	// Availability returns PoolStateNoUsableSession
	avail := pool.Availability()
	if avail.State != mediasession.PoolStateNoUsableSession {
		t.Fatalf("expected PoolStateNoUsableSession, got: %v", avail.State)
	}

	track := music.Track{
		Title:      "Disabled Test Track",
		Artists:    []string{"Disabled Artist"},
		DurationMS: 200000,
	}
	ytm.SetCandidates(nil)
	sc.SetCandidates(nil)

	// Manual request with all disabled and SC no match -> CodeSessionNotFound (NOT SESSION_UNAVAILABLE)
	ctxManual := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, errManual := orch.ResolveMedia(ctxManual, "ytmusic", track, 5)
	if errManual == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(errManual) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got %s", apperr.CodeOf(errManual))
	}
	if apperr.IsSessionWait(errManual) {
		t.Fatal("expected IsSessionWait to be false (no endless wait)")
	}
	if !apperr.ConsumesJobRetry(errManual) {
		t.Fatal("expected ConsumesJobRetry to be true")
	}
}

// 3. enabled but unusable credential configuration -> no endless retry_wait
func TestSessionUnconfigured_Case3_UnusableCredentialConfig_NoEndlessRetryWait(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	sess := mediasession.Session{
		ID:             "sess-empty-cookie",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Empty Cookie Session",
		CookieRef:      "   ", // empty / whitespace cookie ref
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	cfg := mediasession.PoolConfig{
		Family:       provider.FamilyYouTube,
		AllowUnknown: true,
	}
	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sess})

	// HasEligibleSession returns (false, 0)
	hasEligible, wait := pool.HasEligibleSession()
	if hasEligible || wait != 0 {
		t.Fatalf("expected (false, 0), got (%v, %v)", hasEligible, wait)
	}

	avail := pool.Availability()
	if avail.State != mediasession.PoolStateNoUsableSession {
		t.Fatalf("expected PoolStateNoUsableSession, got: %v", avail.State)
	}

	_, acqErr := pool.Acquire(context.Background())
	if apperr.CodeOf(acqErr) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got: %s", apperr.CodeOf(acqErr))
	}

	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")
	sc := newMockProvider("soundcloud")
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.RegisterMedia(sc)
	reg.SetDefaults("deezer", "ytmusic")

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
	})

	track := music.Track{Title: "Unusable Config Track", Artists: []string{"Artist"}, DurationMS: 200000}
	ctxSub := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	_, errSub := orch.ResolveMedia(ctxSub, "ytmusic", track, 5)
	if apperr.CodeOf(errSub) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound for subscription, got %s", apperr.CodeOf(errSub))
	}
	if apperr.IsSessionWait(errSub) {
		t.Fatal("expected IsSessionWait to be false")
	}
}

// 4. BOT_CHALLENGE finite cooldown -> SESSION_UNAVAILABLE -> finite retry preserved
func TestSessionUnconfigured_Case4_BotChallenge_FiniteCooldown_PreservesRetry(t *testing.T) {
	now := time.Now()
	cooldownUntil := now.Add(24 * time.Hour)
	sess := mediasession.Session{
		ID:             "sess-bot-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Bot Challenged Session",
		CookieRef:      "managed://cookies/sess-bot-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthBotChallenge,
		CooldownUntil:  &cooldownUntil,
	}
	orch, pool, _, _, _, _ := setupPreRoutingEnv(t, sess)
	pool.SetNow(func() time.Time { return now })

	avail := pool.Availability()
	if avail.State != mediasession.PoolStateCooling {
		t.Fatalf("expected PoolStateCooling, got: %v", avail.State)
	}
	if avail.RetryAfter < 23*time.Hour {
		t.Fatalf("expected retryAfter ~24h, got: %v", avail.RetryAfter)
	}

	track := music.Track{Title: "Bot Challenge Track", Artists: []string{"Artist"}, DurationMS: 200000}
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait to be true")
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatal("expected ConsumesJobRetry to be false for temporary session wait")
	}
	retryWait, ok := apperr.RetryAfter(err)
	if !ok || retryWait < 23*time.Hour {
		t.Fatalf("expected retryAfter >= 23h, got %v (ok=%v)", retryWait, ok)
	}
}

// 5. RATE_LIMITED finite cooldown -> SESSION_UNAVAILABLE
func TestSessionUnconfigured_Case5_RateLimited_FiniteCooldown_ReturnsSessionUnavailable(t *testing.T) {
	now := time.Now()
	cooldownUntil := now.Add(10 * time.Minute)
	sess := mediasession.Session{
		ID:             "sess-rl-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Rate Limited Session",
		CookieRef:      "managed://cookies/sess-rl-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthRateLimited,
		CooldownUntil:  &cooldownUntil,
	}
	orch, pool, _, _, _, _ := setupPreRoutingEnv(t, sess)
	pool.SetNow(func() time.Time { return now })

	avail := pool.Availability()
	if avail.State != mediasession.PoolStateCooling {
		t.Fatalf("expected PoolStateCooling, got: %v", avail.State)
	}

	track := music.Track{Title: "Rate Limited Track", Artists: []string{"Artist"}, DurationMS: 200000}
	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	retryWait, ok := apperr.RetryAfter(err)
	if !ok || retryWait < 9*time.Minute {
		t.Fatalf("expected retryAfter >= 9m, got %v (ok=%v)", retryWait, ok)
	}
}

// 6. HealthUnknown controlled revalidation -> not misclassified as permanent
func TestSessionUnconfigured_Case6_HealthUnknown_ControlledRevalidation(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-unknown-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Unknown Health Session",
		CookieRef:      "managed://cookies/sess-unknown-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthUnknown,
	}
	orch, pool, ytm, _, _, _ := setupPreRoutingEnv(t, sess)

	avail := pool.Availability()
	if avail.State != mediasession.PoolStateEligible {
		t.Fatalf("expected PoolStateEligible for HealthUnknown with AllowUnknown:true, got: %v", avail.State)
	}

	hasEligible, wait := pool.HasEligibleSession()
	if !hasEligible || wait != 0 {
		t.Fatalf("expected hasEligible=true, wait=0, got (%v, %v)", hasEligible, wait)
	}

	track := music.Track{Title: "Unknown Track", Artists: []string{"Artist"}, DurationMS: 200000}
	ytm.SetCandidates([]provider.MediaCandidate{{
		Provider:   "ytmusic",
		ID:         "yt-cand-1",
		Title:      "Unknown Track",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}})

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected resolve success, got err: %v", err)
	}
	if res == nil || res.SessionID != "sess-unknown-1" {
		t.Fatalf("expected leased session sess-unknown-1, got %v", res)
	}
}

// 7. manual + no usable YT + SC valid match -> SC success (YT not contacted)
func TestSessionUnconfigured_Case7_Manual_NoUsableYT_SoundCloudMatch_Success(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-disabled-yt",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Disabled Session",
		CookieRef:      "managed://cookies/sess-disabled-yt",
		Enabled:        false,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)

	track := music.Track{Title: "Matchable Song", Artists: []string{"Good Artist"}, DurationMS: 210000}

	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-valid-1",
		URL:        "https://soundcloud.com/artist/song",
		Title:      "Matchable Song",
		Artists:    []string{"Good Artist"},
		DurationMS: 210000,
	}})

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("expected success on SoundCloud fallback, got err: %v", err)
	}
	if res == nil || res.Candidate.Provider != "soundcloud" {
		t.Fatalf("expected SoundCloud candidate, got %v", res)
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube was contacted (ytm: %d, yt: %d), want 0", ytm.SearchCalls(), yt.SearchCalls())
	}
	if sc.SearchCalls() == 0 {
		t.Fatal("expected SoundCloud to be searched, got 0 calls")
	}
}

// 8. manual + no usable YT + SC no match -> no endless wait -> truthful session/config error
func TestSessionUnconfigured_Case8_Manual_NoUsableYT_SoundCloudNoMatch_TruthfulSessionNotFound(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-disabled-yt",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Disabled Session",
		CookieRef:      "managed://cookies/sess-disabled-yt",
		Enabled:        false,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)

	track := music.Track{Title: "No Match Song", Artists: []string{"Rare Artist"}, DurationMS: 210000}
	sc.SetCandidates(nil) // Genuinely searched, 0 candidates

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must be SESSION_NOT_FOUND (truthful root cause), NOT SESSION_UNAVAILABLE, NOT TRACK_NOT_FOUND
	if apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got %s", apperr.CodeOf(err))
	}
	if apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait to be false (must not retry forever)")
	}
	if !apperr.ConsumesJobRetry(err) {
		t.Fatal("expected ConsumesJobRetry to be true")
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube was contacted (ytm: %d, yt: %d), want 0", ytm.SearchCalls(), yt.SearchCalls())
	}
	if sc.SearchCalls() == 0 {
		t.Fatal("expected SoundCloud to be searched")
	}
}

// 9. subscription + no usable YT -> SC NOT substituted -> no endless 15m wait
func TestSessionUnconfigured_Case9_Subscription_NoUsableYT_NoSoundCloudSubstitution_TruthfulSessionNotFound(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-disabled-yt",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Disabled Session",
		CookieRef:      "managed://cookies/sess-disabled-yt",
		Enabled:        false,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)

	track := music.Track{Title: "Sub Song", Artists: []string{"Artist"}, DurationMS: 200000}
	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-valid-1",
		Title:      "Sub Song",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}})

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must return CodeSessionNotFound, NOT CodeSessionUnavailable
	if apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got %s", apperr.CodeOf(err))
	}
	if apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait to be false (must not wait 15m)")
	}
	// SoundCloud must NOT be contacted or substituted
	if sc.SearchCalls() != 0 {
		t.Fatalf("SoundCloud was contacted for subscription (%d calls), want 0", sc.SearchCalls())
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube was contacted, want 0")
	}
}

// 10. temporary YT cooldown + SC no match -> F2 temporary deferral preserved
func TestSessionUnconfigured_Case10_TemporaryYTCooldown_SoundCloudNoMatch_PreservesSessionUnavailable(t *testing.T) {
	now := time.Now()
	cooldownUntil := now.Add(30 * time.Minute)
	sess := mediasession.Session{
		ID:             "sess-cooling-yt",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Cooling Session",
		CookieRef:      "managed://cookies/sess-cooling-yt",
		Enabled:        true,
		HealthStatus:   mediasession.HealthBotChallenge,
		CooldownUntil:  &cooldownUntil,
	}
	orch, pool, ytm, yt, sc, _ := setupPreRoutingEnv(t, sess)
	pool.SetNow(func() time.Time { return now })

	track := music.Track{Title: "Cooling Track", Artists: []string{"Artist"}, DurationMS: 200000}
	sc.SetCandidates(nil) // SoundCloud has no match

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if res != nil {
		t.Fatalf("expected nil result, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// F2 preserved: temporary YT cooldown returns CodeSessionUnavailable
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %s", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatal("expected IsSessionWait to be true for temporary cooldown")
	}
	wait, ok := apperr.RetryAfter(err)
	if !ok || wait < 29*time.Minute {
		t.Fatalf("expected retryAfter ~30m, got %v (ok=%v)", wait, ok)
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube was contacted while cooling, want 0")
	}
	if sc.SearchCalls() == 0 {
		t.Fatal("expected SoundCloud to be searched for manual origin")
	}
}

// 11. same-attempt BOT_CHALLENGE -> SC still NOT attempted
func TestSessionUnconfigured_Case11_MidAttempt_BotChallenge_BlocksSoundCloud(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-healthy-yt",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Healthy Session",
		CookieRef:      "managed://cookies/sess-healthy-yt",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	orch, _, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)

	track := music.Track{Title: "Bot Attempt Track", Artists: []string{"Artist"}, DurationMS: 200000}
	ytm.SetSearchErr(apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot"))

	sc.SetCandidates([]provider.MediaCandidate{{
		Provider:   "soundcloud",
		ID:         "sc-cand-1",
		Title:      "Bot Attempt Track",
		Artists:    []string{"Artist"},
		DurationMS: 200000,
	}})

	ctx := orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected bot challenge error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %s", apperr.CodeOf(err))
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("SoundCloud was contacted after mid-attempt bot challenge (%d calls), want 0", sc.SearchCalls())
	}
}

// 12-15. Comprehensive Regression Checks
func TestSessionUnconfigured_Step1Step2F1F2_Regressions(t *testing.T) {
	// Verify Step 1/2 error properties
	unavailErr := apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "wait", 10*time.Minute)
	if !apperr.IsSessionWait(unavailErr) {
		t.Fatal("expected IsSessionWait == true for CodeSessionUnavailable")
	}
	if apperr.ConsumesJobRetry(unavailErr) {
		t.Fatal("expected ConsumesJobRetry == false for CodeSessionUnavailable")
	}

	notFoundErr := apperr.New(apperr.CodeSessionNotFound, "no session")
	if apperr.IsSessionWait(notFoundErr) {
		t.Fatal("expected IsSessionWait == false for CodeSessionNotFound")
	}
	if !apperr.ConsumesJobRetry(notFoundErr) {
		t.Fatal("expected ConsumesJobRetry == true for CodeSessionNotFound")
	}

	// Verify message in notFoundErr contains truthful reason
	if !strings.Contains(notFoundErr.Error(), "no session") {
		t.Fatalf("expected message to contain 'no session', got: %s", notFoundErr.Error())
	}
}
