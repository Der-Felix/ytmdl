package orchestrator_test

import (
	"context"
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
)

// mockMediaProvider implements provider.MediaProvider and can be bound to cookie files.
type mockProviderState struct {
	mu             sync.Mutex
	searchCalls    int
	resolveCalls   int
	lastCookieFile string
	candidates     []provider.MediaCandidate
	searchErr      error
	resolveErrs    map[string]error
	sources        map[string]*provider.MediaSource
}

// mockMediaProvider implements provider.MediaProvider and can be bound to cookie files.
type mockMediaProvider struct {
	name       string
	cookieFile string
	state      *mockProviderState
}

func newMockProvider(name string) *mockMediaProvider {
	return &mockMediaProvider{
		name: name,
		state: &mockProviderState{
			resolveErrs: make(map[string]error),
			sources:     make(map[string]*provider.MediaSource),
		},
	}
}

func (p *mockMediaProvider) Name() string { return p.name }

func (p *mockMediaProvider) WithCookieFile(cookieFile string) provider.MediaProvider {
	return &mockMediaProvider{
		name:       p.name,
		cookieFile: cookieFile,
		state:      p.state,
	}
}

func (p *mockMediaProvider) SearchCalls() int {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.searchCalls
}

func (p *mockMediaProvider) ResolveCalls() int {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.resolveCalls
}

func (p *mockMediaProvider) LastCookieFile() string {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	return p.state.lastCookieFile
}

func (p *mockMediaProvider) SetCandidates(candidates []provider.MediaCandidate) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.candidates = candidates
}

func (p *mockMediaProvider) SetSearchErr(err error) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.searchErr = err
}

func (p *mockMediaProvider) SetResolveErr(id string, err error) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.resolveErrs[id] = err
}

func (p *mockMediaProvider) SetSource(id string, src *provider.MediaSource) {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.sources[id] = src
}

func (p *mockMediaProvider) Search(ctx context.Context, track music.Track) ([]provider.MediaCandidate, error) {
	p.state.mu.Lock()
	p.state.searchCalls++
	p.state.lastCookieFile = p.cookieFile
	err := p.state.searchErr
	cands := make([]provider.MediaCandidate, len(p.state.candidates))
	copy(cands, p.state.candidates)
	p.state.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return cands, nil
}

func (p *mockMediaProvider) Resolve(ctx context.Context, candidate provider.MediaCandidate) (*provider.MediaSource, error) {
	p.state.mu.Lock()
	p.state.resolveCalls++
	p.state.lastCookieFile = p.cookieFile
	err, hasErr := p.state.resolveErrs[candidate.ID]
	src := p.state.sources[candidate.ID]
	p.state.mu.Unlock()

	if hasErr && err != nil {
		return nil, err
	}
	if src != nil {
		srcCopy := *src
		return &srcCopy, nil
	}
	return &provider.MediaSource{
		Provider:   p.name,
		ID:         candidate.ID,
		URL:        "https://youtube.com/watch?v=" + candidate.ID,
		Title:      candidate.Title,
		DurationMS: candidate.DurationMS,
	}, nil
}

// mockCooldown implements orchestrator.CooldownManager.
type mockCooldown struct {
	mu        sync.Mutex
	cooldowns map[string]time.Time
}

func newMockCooldown() *mockCooldown {
	return &mockCooldown{cooldowns: make(map[string]time.Time)}
}

func (c *mockCooldown) Trigger(provider string, duration time.Duration) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(provider))
	if key == "ytmusic" || key == "youtube" {
		key = "youtube"
	}
	c.cooldowns[key] = time.Now().Add(duration)
	return duration
}

func (c *mockCooldown) Remaining(provider string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(provider))
	if key == "ytmusic" || key == "youtube" {
		key = "youtube"
	}
	exp, ok := c.cooldowns[key]
	if !ok {
		return 0, false
	}
	rem := time.Until(exp)
	if rem <= 0 {
		return 0, false
	}
	return rem, true
}

func (c *mockCooldown) Wait(ctx context.Context, provider string) error {
	rem, cooling := c.Remaining(provider)
	if !cooling {
		return nil
	}
	timer := time.NewTimer(rem)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *mockCooldown) Clear(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(provider))
	if key == "ytmusic" || key == "youtube" {
		key = "youtube"
	}
	delete(c.cooldowns, key)
}

func setupTestEnvironment(t *testing.T, sessions ...mediasession.Session) (*orchestrator.ProviderOrchestrator, *mediasession.SessionPool, *mockMediaProvider, *mockMediaProvider, *mockCooldown) {
	t.Helper()

	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	for _, s := range sessions {
		if s.CookieRef != "" && s.ID != mediasession.LegacySessionID {
			_, err := storage.Store(s.ID, []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test\n"))
			if err != nil {
				t.Fatalf("Store: %v", err)
			}
		}
	}

	cfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0, // fast for testing
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}

	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)
	pool.ReloadSessions(sessions)

	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.SetDefaults("ytmusic", "ytmusic")

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

	return orch, pool, ytm, yt, cooldown
}

// TEST 1: Candidate Fallback Regression (Rank 1 fails candidate-specifically -> Rank 2 succeeds)
func TestOrchestrator_CandidateFallback_PreservesSessionHealth(t *testing.T) {
	sess := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, sess)

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	cand1 := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "vid-1",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	cand2 := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "vid-2",
		Title:      "Dancing Queen (Official Audio)",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	ytm.SetCandidates([]provider.MediaCandidate{cand1, cand2})
	// Candidate 1 fails with candidate-specific TrackNotFound
	ytm.SetResolveErr("vid-1", apperr.New(apperr.CodeTrackNotFound, "video unavailable"))

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}

	if res.Candidate.ID != "vid-2" {
		t.Fatalf("expected candidate vid-2, got %s", res.Candidate.ID)
	}
	if res.SessionID != "sess-a" {
		t.Fatalf("expected session sess-a, got %s", res.SessionID)
	}

	// Verify session health remained healthy and no family cooldown was triggered
	s := pool.Sessions()[0]
	if s.HealthStatus != mediasession.HealthHealthy {
		t.Fatalf("expected session to remain healthy, got %s", s.HealthStatus)
	}
	if s.ConsecutiveFailures != 0 {
		t.Fatalf("expected 0 consecutive failures, got %d", s.ConsecutiveFailures)
	}
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("expected no family cooldown on candidate failure")
	}
}

// TEST 2: Session-Specific Failure (RateLimited) stops candidate fanout and does not cycle sessions immediately
func TestOrchestrator_SessionRateLimited_HaltsFanoutAndUpdatesHealth(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	sessB := mediasession.Session{
		ID:             "sess-b",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session B",
		CookieRef:      "managed://cookies/sess-b",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, pool, ytm, _, _ := setupTestEnvironment(t, sessA, sessB)

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	cand1 := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "vid-1",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	cand2 := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "vid-2",
		Title:      "Dancing Queen (Official Audio)",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	ytm.SetCandidates([]provider.MediaCandidate{cand1, cand2})
	// Candidate 1 fails with session-specific rate limit
	ytm.SetResolveErr("vid-1", apperr.New(apperr.CodeSessionRateLimited, "session has been rate-limited"))

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got success")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionRateLimited {
		t.Fatalf("expected CodeSessionRateLimited, got %s", apperr.CodeOf(err))
	}
	if res != nil {
		t.Fatal("expected nil result")
	}

	// Verify only 1 resolve call was made (fanout stopped immediately)
	if ytm.ResolveCalls() != 1 {
		t.Fatalf("expected 1 resolve call, got %d", ytm.ResolveCalls())
	}

	// Verify Session A is now RATE_LIMITED
	sessions := pool.Sessions()
	var sA, sB mediasession.Session
	for _, s := range sessions {
		if s.ID == "sess-a" {
			sA = s
		} else if s.ID == "sess-b" {
			sB = s
		}
	}

	if sA.HealthStatus != mediasession.HealthRateLimited {
		t.Fatalf("expected sess-a HealthRateLimited, got %s", sA.HealthStatus)
	}
	if sA.ConsecutiveFailures != 1 {
		t.Fatalf("expected sess-a 1 failure, got %d", sA.ConsecutiveFailures)
	}

	// Verify Session B was untouched in the same attempt
	if sB.HealthStatus != mediasession.HealthHealthy {
		t.Fatalf("expected sess-b still healthy, got %s", sB.HealthStatus)
	}
	if sB.ConsecutiveFailures != 0 {
		t.Fatalf("expected sess-b 0 failures, got %d", sB.ConsecutiveFailures)
	}

	// On LATER retry: Session B should be selected!
	ytm.SetResolveErr("vid-1", nil) // cleared for Session B
	res2, err2 := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err2 != nil {
		t.Fatalf("later retry failed: %v", err2)
	}
	if res2.SessionID != "sess-b" {
		t.Fatalf("expected later retry to pick sess-b, got %s", res2.SessionID)
	}
}

// TEST 3: Bot Challenge stops candidate and provider fanout
func TestOrchestrator_BotChallenge_HaltsProviderFanout(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, pool, ytm, yt, _ := setupTestEnvironment(t, sessA)

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	// ytmusic returns bot challenge during search
	ytm.SetSearchErr(apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you’re not a bot"))
	yt.SetCandidates([]provider.MediaCandidate{{Provider: "youtube", ID: "yt-1", Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000}})

	ctx := context.Background()
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %s", apperr.CodeOf(err))
	}

	// Verify youtube provider was NOT called (no ytmusic -> youtube fallback on session error)
	if yt.SearchCalls() != 0 {
		t.Fatalf("expected 0 search calls on youtube provider, got %d", yt.SearchCalls())
	}

	// Verify session health transitioned to HealthBotChallenge
	s := pool.Sessions()[0]
	if s.HealthStatus != mediasession.HealthBotChallenge {
		t.Fatalf("expected HealthBotChallenge, got %s", s.HealthStatus)
	}
}

// TEST 4: Family-level Systemic Failure (HTTP 429) triggers family cooldown
func TestOrchestrator_FamilySystemicRateLimit_TriggersFamilyCooldown(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, pool, ytm, yt, cooldown := setupTestEnvironment(t, sessA)

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	ytm.SetSearchErr(apperr.New(apperr.CodeProviderRateLimited, "HTTP Error 429: Too Many Requests"))

	ctx := context.Background()
	_, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeProviderRateLimited {
		t.Fatalf("expected CodeProviderRateLimited, got %s", apperr.CodeOf(err))
	}

	// Verify youtube provider was NOT attempted
	if yt.SearchCalls() != 0 {
		t.Fatalf("expected 0 search calls on youtube, got %d", yt.SearchCalls())
	}

	// Verify family cooldown is active for youtube family
	if _, cooling := cooldown.Remaining("youtube"); !cooling {
		t.Fatal("expected family cooldown active on youtube")
	}
	if _, cooling := cooldown.Remaining("ytmusic"); !cooling {
		t.Fatal("expected family cooldown active on ytmusic")
	}

	// Verify session A itself was NOT marked auth failed or bot challenge
	s := pool.Sessions()[0]
	if s.HealthStatus == mediasession.HealthAuthFailed || s.HealthStatus == mediasession.HealthBotChallenge {
		t.Fatalf("session should not be penalized with auth failure on provider-wide 429, got %s", s.HealthStatus)
	}
}

// TEST 5: Direct-ID Fast Path
func TestOrchestrator_DirectID_SuccessAndFallbacks(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, _, ytm, _, _ := setupTestEnvironment(t, sessA)

	// 1. Direct-ID candidate success
	trackDirect := music.Track{
		Title:          "Dancing Queen",
		Artists:        []string{"ABBA"},
		DurationMS:     231000,
		SourceProvider: "ytmusic",
		SourceID:       "xFrGuyw1Vm8",
	}

	candDirect := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "xFrGuyw1Vm8",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	ytm.SetCandidates([]provider.MediaCandidate{candDirect})

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", trackDirect, 5)
	if err != nil {
		t.Fatalf("direct-ID resolve failed: %v", err)
	}
	if res.Candidate.ID != "xFrGuyw1Vm8" {
		t.Fatalf("expected direct candidate, got %s", res.Candidate.ID)
	}

	// 2. Direct-ID candidate failure (video unavailable) -> generic search fallback
	trackFallback := trackDirect
	ytm.SetResolveErr("xFrGuyw1Vm8", apperr.New(apperr.CodeTrackNotFound, "video unavailable"))

	candGeneric := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "generic-vid",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	// When generic search is performed without SourceID, return generic candidate
	ytm.SetCandidates([]provider.MediaCandidate{candGeneric})

	resGeneric, err := orch.ResolveMedia(ctx, "ytmusic", trackFallback, 5)
	if err != nil {
		t.Fatalf("generic fallback failed: %v", err)
	}
	if resGeneric.Candidate.ID != "generic-vid" {
		t.Fatalf("expected generic candidate, got %s", resGeneric.Candidate.ID)
	}

	// 3. Direct-ID session failure (bot challenge) -> NO generic search fallback
	ytm.SetResolveErr("xFrGuyw1Vm8", apperr.New(apperr.CodeSessionBotChallenge, "bot challenge"))
	ytm.SetCandidates([]provider.MediaCandidate{candDirect, candGeneric})

	_, errDirectBot := orch.ResolveMedia(ctx, "ytmusic", trackDirect, 5)
	if errDirectBot == nil {
		t.Fatal("expected bot challenge error, got nil")
	}
	if apperr.CodeOf(errDirectBot) != apperr.CodeSessionBotChallenge {
		t.Fatalf("expected CodeSessionBotChallenge, got %s", apperr.CodeOf(errDirectBot))
	}
}

// TEST 6: Zero-Cookie Mode (No managed sessions, no legacy cookie)
func TestOrchestrator_ZeroCookieMode_Preserved(t *testing.T) {
	// Empty sessions list
	orch, pool, ytm, _, _ := setupTestEnvironment(t)

	if pool.HasConfiguredSessions() {
		t.Fatal("expected HasConfiguredSessions to be false")
	}

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}

	cand := provider.MediaCandidate{
		Provider:   "ytmusic",
		ID:         "pub-1",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	ytm.SetCandidates([]provider.MediaCandidate{cand})

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("zero-cookie resolve failed: %v", err)
	}
	if res.Candidate.ID != "pub-1" {
		t.Fatalf("expected candidate pub-1, got %s", res.Candidate.ID)
	}
	if res.SessionID != "" {
		t.Fatalf("expected empty SessionID in zero-cookie mode, got %s", res.SessionID)
	}
}

// TEST 7: Download Affinity (Download resolves cookie path without control-plane lease)
func TestOrchestrator_DownloadAffinity_NoControlPlaneLease(t *testing.T) {
	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      "managed://cookies/sess-a",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	orch, pool, ytm, _, _ := setupTestEnvironment(t, sessA)

	track := music.Track{
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}
	ytm.SetCandidates([]provider.MediaCandidate{{
		Provider:   "ytmusic",
		ID:         "vid-1",
		Title:      "Dancing Queen",
		Artists:    []string{"ABBA"},
		DurationMS: 231000,
	}})

	ctx := context.Background()
	res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}

	// Verify lease was RELEASED immediately upon resolve completion!
	rs := pool.RuntimeSessions()[0]
	if rs.CurrentLeases() != 0 {
		t.Fatalf("expected 0 active leases after ResolveMedia, got %d", rs.CurrentLeases())
	}

	// Verify cookie path can be resolved for Download using session ID
	cookiePath := orch.ResolveCookiePath(res.SessionID)
	if cookiePath == "" {
		t.Fatal("expected non-empty cookie path for session sess-a")
	}
	if !strings.Contains(cookiePath, "sess-a") {
		t.Fatalf("expected cookie path for sess-a, got %s", cookiePath)
	}

	// Verify recording download outcome
	orch.RecordDownloadOutcome(ctx, res.SessionID, nil)
	s := pool.Sessions()[0]
	if s.LastSuccessAt == nil {
		t.Fatal("expected LastSuccessAt updated after download success")
	}

	// Verify recording download failure (session expired)
	orch.RecordDownloadOutcome(ctx, res.SessionID, apperr.New(apperr.CodeSessionAuthFailed, "cookies are expired"))
	sAfter := pool.Sessions()[0]
	if sAfter.HealthStatus != mediasession.HealthAuthFailed {
		t.Fatalf("expected HealthAuthFailed after auth failed download, got %s", sAfter.HealthStatus)
	}
}
