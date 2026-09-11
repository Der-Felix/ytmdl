package orchestrator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// hookProvider lets a test observe or steer single provider calls.
type hookProvider struct {
	name     string
	search   func(ctx context.Context) ([]provider.MediaCandidate, error)
	resolve  func(ctx context.Context, c provider.MediaCandidate) (*provider.MediaSource, error)
	resolves atomic.Int32
}

func (p *hookProvider) Name() string { return p.name }

func (p *hookProvider) Search(ctx context.Context, _ music.Track) ([]provider.MediaCandidate, error) {
	if p.search == nil {
		return nil, nil
	}
	return p.search(ctx)
}

func (p *hookProvider) Resolve(ctx context.Context, c provider.MediaCandidate) (*provider.MediaSource, error) {
	p.resolves.Add(1)
	if p.resolve == nil {
		return &provider.MediaSource{Provider: p.name, ID: c.ID, URL: c.URL, DurationMS: c.DurationMS}, nil
	}
	return p.resolve(ctx, c)
}

func notFound() error { return apperr.New(apperr.CodeTrackNotFound, "no audio only stream") }

func newOrchestrator(pool *mediasession.SessionPool, cooldown *mockCooldown, providers ...provider.MediaProvider) *orchestrator.ProviderOrchestrator {
	reg := provider.NewRegistry()
	for _, p := range providers {
		reg.RegisterMedia(p)
	}
	reg.SetDefaults("deezer", "ytmusic")
	return orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Cooldown:    cooldown,
		Matcher:     matcher.New(matcher.Options{MinScore: 70, DurationToleranceMS: 5000}),
	})
}

func TestSameVideoIsResolvedOncePerAttempt(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "v1"), auditCandidate("ytmusic", "v2")})
	yt.SetCandidates([]provider.MediaCandidate{auditCandidate("youtube", "v2"), auditCandidate("youtube", "v3")})
	for _, id := range []string{"v1", "v2", "v3"} {
		ytm.SetResolveErr(id, notFound())
		yt.SetResolveErr(id, notFound())
	}
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("err = %v", err)
	}
	if ytm.ResolveCalls() != 2 {
		t.Fatalf("ytmusic resolves = %d, want 2", ytm.ResolveCalls())
	}
	// v2 already failed in the YouTube Music phase of this attempt.
	if yt.ResolveCalls() != 1 {
		t.Fatalf("youtube resolves = %d, want 1 (v3 only)", yt.ResolveCalls())
	}
}

func TestSameVideoOnIndependentFamilyIsStillTried(t *testing.T) {
	// Deduplication is per platform family: an identical id on SoundCloud is a
	// different item and must still be resolved.
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "same")})
	ytm.SetResolveErr("same", notFound())
	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "same")})
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil || res.Candidate.Provider != "soundcloud" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestFailedDirectIDCandidateIsNotResolvedAgain(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "direct"), auditCandidate("ytmusic", "other")})
	ytm.SetResolveErr("direct", notFound())
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	track := auditTrack()
	track.SourceProvider = "ytmusic"
	track.SourceID = "direct"
	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", track, 5)
	if err != nil || res.Candidate.ID != "other" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ytm.ResolveCalls() != 2 {
		t.Fatalf("ytmusic resolves = %d, want 2 (direct once, other once)", ytm.ResolveCalls())
	}
	orch.RecordDownloadOutcome(context.Background(), res.SessionID, nil)
}

func TestYouTubeLeaseIsReleasedBeforeIndependentProviders(t *testing.T) {
	_, pool, ytm, yt, _, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "v1")})
	ytm.SetResolveErr("v1", notFound())

	entered := make(chan struct{})
	proceed := make(chan struct{})
	sc := &hookProvider{name: "soundcloud", search: func(ctx context.Context) ([]provider.MediaCandidate, error) {
		close(entered)
		<-proceed
		return []provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")}, nil
	}}
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	done := make(chan error, 1)
	go func() {
		_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
		done <- err
	}()
	<-entered

	// While the SoundCloud phase runs, the only YouTube session is free for
	// another worker.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := pool.Acquire(ctx)
	if err != nil {
		close(proceed)
		t.Fatalf("YouTube lease still held during the SoundCloud phase: %v", err)
	}
	lease.ReleaseNeutral()
	close(proceed)

	if err := <-done; err != nil {
		t.Fatalf("resolution failed: %v", err)
	}
	if s := pool.Sessions()[0]; s.HealthStatus != mediasession.HealthHealthy || s.ConsecutiveFailures != 0 {
		t.Fatalf("early release changed session health: %+v", s)
	}
}

func TestCooldownStartedMidAttemptDefersInsteadOfBlocking(t *testing.T) {
	_, pool, _, yt, _, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	// While YouTube Music is searched, another worker puts SoundCloud on hold.
	ytm := &hookProvider{name: "ytmusic", search: func(context.Context) ([]provider.MediaCandidate, error) {
		cooldown.Trigger("soundcloud", 30*time.Second)
		return nil, nil
	}}
	sc := &hookProvider{name: "soundcloud", search: func(context.Context) ([]provider.MediaCandidate, error) {
		t.Error("SoundCloud was contacted during its cooldown")
		return nil, nil
	}}
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	began := time.Now()
	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("worker was parked for the cooldown: %v", elapsed)
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("err = %v, want a session wait", err)
	}
	if wait, ok := apperr.RetryAfter(err); !ok || wait < 20*time.Second {
		t.Fatalf("retry hint %v %v does not reflect the cooldown", wait, ok)
	}
}

func TestYouTubeCooldownStartedMidAttemptStopsFurtherRequests(t *testing.T) {
	_, pool, _, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm := &hookProvider{
		name: "ytmusic",
		search: func(context.Context) ([]provider.MediaCandidate, error) {
			return []provider.MediaCandidate{auditCandidate("ytmusic", "a"), auditCandidate("ytmusic", "b"), auditCandidate("ytmusic", "c")}, nil
		},
		resolve: func(context.Context, provider.MediaCandidate) (*provider.MediaSource, error) {
			// Another worker's request met a rate limit meanwhile.
			cooldown.Trigger("youtube", time.Minute)
			return nil, notFound()
		},
	}
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if !apperr.IsSessionWait(err) {
		t.Fatalf("err = %v, want a session wait", err)
	}
	if n := ytm.resolves.Load(); n != 1 {
		t.Fatalf("resolves during the cooldown: %d, want 1", n)
	}
	if yt.SearchCalls() != 0 || sc.SearchCalls() != 0 {
		t.Fatal("other providers were contacted after the cooldown began")
	}
	if s := pool.Sessions()[0]; s.HealthStatus != mediasession.HealthHealthy || s.ConsecutiveFailures != 0 {
		t.Fatalf("cooldown deferral charged the session: %+v", s)
	}
}

func TestGateRefusalIsNeutralAndTriggersNoCooldown(t *testing.T) {
	_, pool, _, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	refusal := apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "paused", time.Minute)
	ytm := &hookProvider{name: "ytmusic", search: func(context.Context) ([]provider.MediaCandidate, error) {
		return nil, refusal
	}}
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	_, err := orch.ResolveMedia(subscriptionCtx(), "ytmusic", auditTrack(), 5)
	if !apperr.IsSessionWait(err) {
		t.Fatalf("err = %v", err)
	}
	if s := pool.Sessions()[0]; s.ConsecutiveFailures != 0 || s.HealthStatus != mediasession.HealthHealthy {
		t.Fatalf("gate refusal charged the session: %+v", s)
	}
	cooldown.mu.Lock()
	n := len(cooldown.cooldowns)
	cooldown.mu.Unlock()
	if n != 0 {
		t.Fatal("gate refusal triggered a cooldown")
	}
}

func TestPrecheckMatchesPreAttemptPlanningWithoutContact(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	if err := orch.Precheck(subscriptionCtx(), "ytmusic"); err != nil {
		t.Fatalf("eligible YouTube precheck: %v", err)
	}
	cooldown.Trigger("youtube", 2*time.Minute)
	err := orch.Precheck(subscriptionCtx(), "ytmusic")
	if !apperr.IsSessionWait(err) {
		t.Fatalf("subscription precheck during cooldown: %v", err)
	}
	_, resolveErr := orch.ResolveMedia(subscriptionCtx(), "ytmusic", auditTrack(), 5)
	if apperr.CodeOf(resolveErr) != apperr.CodeOf(err) {
		t.Fatalf("precheck %v disagrees with ResolveMedia %v", err, resolveErr)
	}
	// A manual job may still use SoundCloud, so it is not held back.
	if err := orch.Precheck(manualCtx(), "ytmusic"); err != nil {
		t.Fatalf("manual precheck: %v", err)
	}
	if ytm.SearchCalls()+yt.SearchCalls()+sc.SearchCalls() != 0 {
		t.Fatal("precheck contacted a provider")
	}
}
