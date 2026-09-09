package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

func auditTrack() music.Track {
	return music.Track{
		Title:      "Audit Song",
		Artists:    []string{"Audit Artist"},
		DurationMS: 200000,
	}
}

func auditCandidate(prov, id string) provider.MediaCandidate {
	return provider.MediaCandidate{
		Provider:   prov,
		ID:         id,
		URL:        "https://" + prov + ".example/" + id,
		Title:      "Audit Song",
		Artists:    []string{"Audit Artist"},
		DurationMS: 200000,
	}
}

func healthyAuditSession() mediasession.Session {
	return mediasession.Session{
		ID:             "sess-yt-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session YT",
		CookieRef:      "managed://cookies/sess-yt-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
}

// unusableAuditSession is configured but disabled: the pool reports
// no_usable_session, which is the state that requires operator intervention.
func unusableAuditSession() mediasession.Session {
	s := healthyAuditSession()
	s.Enabled = false
	return s
}

func subscriptionCtx() context.Context {
	return orchestrator.WithOrigin(context.Background(), orchestrator.OriginSubscription)
}

func manualCtx() context.Context {
	return orchestrator.WithOrigin(context.Background(), orchestrator.OriginManual)
}

// ---------------------------------------------------------------------------
// FINDING 1: subscriptions must not skip SoundCloud when SoundCloud itself is
// the explicitly preferred media provider.
// ---------------------------------------------------------------------------

// FINDING 1 / CROSS-FINDING A: subscription + preferred SoundCloud + SC healthy.
func TestFinalAudit_F1_Subscription_PreferredSoundCloud_NoUsableYouTube_Succeeds(t *testing.T) {
	// The only YouTube session is disabled -> pool reports no usable session.
	orch, _, ytm, yt, sc, _ := setupPreRoutingEnv(t, unusableAuditSession())

	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	res, err := orch.ResolveMedia(subscriptionCtx(), "soundcloud", auditTrack(), 5)
	if err != nil {
		t.Fatalf("expected subscription with preferred SoundCloud to resolve, got error: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("candidate provider = %s, want soundcloud", res.Candidate.Provider)
	}
	if sc.SearchCalls() != 1 {
		t.Fatalf("SoundCloud search calls = %d, want 1 (preferred provider must be contacted)", sc.SearchCalls())
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube must not be contacted, got ytmusic=%d youtube=%d", ytm.SearchCalls(), yt.SearchCalls())
	}
}

// FINDING 1: subscription + preferred SoundCloud while the YouTube family is in
// a protection cooldown. SoundCloud is the target, not a substitute.
func TestFinalAudit_F1_Subscription_PreferredSoundCloud_YouTubeCooling_Succeeds(t *testing.T) {
	sess := healthyAuditSession()
	orch, _, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, sess)
	cooldown.Trigger("youtube", 30*time.Minute)

	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	res, err := orch.ResolveMedia(subscriptionCtx(), "soundcloud", auditTrack(), 5)
	if err != nil {
		t.Fatalf("expected subscription with preferred SoundCloud to resolve, got error: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("candidate provider = %s, want soundcloud", res.Candidate.Provider)
	}
	if res.SessionID != "" {
		t.Fatalf("SoundCloud source must carry no YouTube session id, got %q", res.SessionID)
	}
	if ytm.SearchCalls() != 0 || yt.SearchCalls() != 0 {
		t.Fatalf("YouTube must not be contacted while cooling, got ytmusic=%d youtube=%d", ytm.SearchCalls(), yt.SearchCalls())
	}
}

// FINDING 1 / CROSS-FINDING B: preferred YouTube stays protected. Background
// work must not substitute SoundCloud when YouTube becomes unavailable.
func TestFinalAudit_F1_Subscription_PreferredYouTube_Unavailable_NoSoundCloudSubstitution(t *testing.T) {
	sess := healthyAuditSession()
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)
	cooldown.Trigger("youtube", 30*time.Minute)

	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	_, err := orch.ResolveMedia(subscriptionCtx(), "ytmusic", auditTrack(), 5)
	if err == nil {
		t.Fatal("expected subscription with preferred YouTube to defer, got success")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeSessionUnavailable {
		t.Fatalf("error code = %s, want SESSION_UNAVAILABLE", code)
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("SoundCloud contacted %d times; background substitution must stay blocked", sc.SearchCalls())
	}
}

// FINDING 1: the same protection applies when the YouTube pool has no usable session.
func TestFinalAudit_F1_Subscription_PreferredYouTube_NoUsableSession_NoSoundCloudSubstitution(t *testing.T) {
	orch, _, _, _, sc, _ := setupPreRoutingEnv(t, unusableAuditSession())
	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	_, err := orch.ResolveMedia(subscriptionCtx(), "ytmusic", auditTrack(), 5)
	if err == nil {
		t.Fatal("expected subscription with preferred YouTube and no usable session to fail, got success")
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("SoundCloud contacted %d times; background substitution must stay blocked", sc.SearchCalls())
	}
}

// FINDING 1 / CROSS-FINDING C: manual resilience is untouched.
func TestFinalAudit_F1_Manual_PreferredYouTube_Cooling_SoundCloudStillAllowed(t *testing.T) {
	sess := healthyAuditSession()
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)
	cooldown.Trigger("youtube", 30*time.Minute)

	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil {
		t.Fatalf("manual resilience regressed: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("candidate provider = %s, want soundcloud", res.Candidate.Provider)
	}
}

// FINDING 1: an explicit provider preference behaves identically across repeated
// attempts, so a persisted job retried after a restart keeps SoundCloud access.
func TestFinalAudit_F1_PreferredSoundCloud_StableAcrossRetries(t *testing.T) {
	sess := healthyAuditSession()
	orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, sess)
	cooldown.Trigger("youtube", 30*time.Minute)
	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	for attempt := 1; attempt <= 3; attempt++ {
		res, err := orch.ResolveMedia(subscriptionCtx(), "soundcloud", auditTrack(), 5)
		if err != nil {
			t.Fatalf("attempt %d: expected resolution, got %v", attempt, err)
		}
		if res.Candidate.Provider != "soundcloud" {
			t.Fatalf("attempt %d: candidate provider = %s, want soundcloud", attempt, res.Candidate.Provider)
		}
	}
	if sc.SearchCalls() != 3 {
		t.Fatalf("SoundCloud search calls = %d, want 3", sc.SearchCalls())
	}
}

// FINDING 1: a same-attempt BOT_CHALLENGE on YouTube still halts candidate
// fanout and must never fall through to SoundCloud, whichever provider is preferred.
func TestFinalAudit_F1_SameAttemptBotChallenge_NeverFallsThroughToSoundCloud(t *testing.T) {
	for _, pref := range []string{"ytmusic", "soundcloud"} {
		t.Run(pref, func(t *testing.T) {
			sess := healthyAuditSession()
			orch, pool, ytm, _, sc, _ := setupPreRoutingEnv(t, sess)

			ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "yt-1")})
			ytm.SetResolveErr("yt-1", apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot"))
			sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

			// SoundCloud first in the chain when preferred: force the YouTube
			// attempt by removing SoundCloud's candidate for that ordering case.
			if pref == "soundcloud" {
				sc.SetCandidates(nil)
			}

			_, err := orch.ResolveMedia(manualCtx(), pref, auditTrack(), 5)
			if err == nil {
				t.Fatal("expected bot challenge to fail the attempt")
			}
			if code := apperr.CodeOf(err); code != apperr.CodeSessionBotChallenge {
				t.Fatalf("error code = %s, want SESSION_BOT_CHALLENGE", code)
			}
			if pref == "ytmusic" && sc.SearchCalls() != 0 {
				t.Fatalf("SoundCloud contacted %d times after a same-attempt bot challenge", sc.SearchCalls())
			}
			// The protection failure is real and belongs to this session.
			s := pool.Sessions()[0]
			if s.HealthStatus != mediasession.HealthBotChallenge {
				t.Fatalf("session status = %s, want bot_challenge", s.HealthStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FINDING 2: an independent provider's outcome must never mutate the health of
// the leased YouTube session.
// ---------------------------------------------------------------------------

// penalizedSession is a healthy session that already carries failure history, so
// any accidental success or failure attribution is visible.
func penalizedSession(lastFailure time.Time) mediasession.Session {
	s := healthyAuditSession()
	s.ConsecutiveFailures = 2
	s.LastFailureAt = &lastFailure
	s.LastFailureReason = "[SESSION_RATE_LIMITED] earlier failure"
	return s
}

func assertSessionHealthUnchanged(t *testing.T, got mediasession.Session, want mediasession.Session) {
	t.Helper()
	if got.HealthStatus != want.HealthStatus {
		t.Fatalf("HealthStatus = %s, want %s (unchanged)", got.HealthStatus, want.HealthStatus)
	}
	if got.ConsecutiveFailures != want.ConsecutiveFailures {
		t.Fatalf("ConsecutiveFailures = %d, want %d (unchanged)", got.ConsecutiveFailures, want.ConsecutiveFailures)
	}
	switch {
	case got.LastFailureAt == nil && want.LastFailureAt != nil,
		got.LastFailureAt != nil && want.LastFailureAt == nil:
		t.Fatalf("LastFailureAt = %v, want %v (unchanged)", got.LastFailureAt, want.LastFailureAt)
	case got.LastFailureAt != nil && !got.LastFailureAt.Equal(*want.LastFailureAt):
		t.Fatalf("LastFailureAt = %v, want %v (unchanged)", got.LastFailureAt, want.LastFailureAt)
	}
	if got.LastFailureReason != want.LastFailureReason {
		t.Fatalf("LastFailureReason = %q, want %q (unchanged)", got.LastFailureReason, want.LastFailureReason)
	}
	switch {
	case got.CooldownUntil == nil && want.CooldownUntil != nil,
		got.CooldownUntil != nil && want.CooldownUntil == nil:
		t.Fatalf("CooldownUntil = %v, want %v (unchanged)", got.CooldownUntil, want.CooldownUntil)
	case got.CooldownUntil != nil && !got.CooldownUntil.Equal(*want.CooldownUntil):
		t.Fatalf("CooldownUntil = %v, want %v (unchanged)", got.CooldownUntil, want.CooldownUntil)
	}
}

// FINDING 2 / CROSS-FINDING D: healthy YouTube lease + YouTube no-match +
// SoundCloud cooling -> SESSION_UNAVAILABLE, YouTube session health untouched.
func TestFinalAudit_F2_IndependentProviderDeferral_DoesNotPenalizeYouTubeSession(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, before)

	// YouTube session works: search succeeds but yields no candidate.
	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)
	// The independent provider is temporarily cooling.
	cooldown.Trigger("soundcloud", 20*time.Minute)

	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err == nil {
		t.Fatal("expected a deferral, got success")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeSessionUnavailable {
		t.Fatalf("error code = %s, want SESSION_UNAVAILABLE", code)
	}
	if ytm.SearchCalls() == 0 {
		t.Fatal("expected the YouTube session to have been used")
	}
	if sc.SearchCalls() != 0 {
		t.Fatalf("SoundCloud contacted %d times while cooling", sc.SearchCalls())
	}

	assertSessionHealthUnchanged(t, pool.Sessions()[0], before)
}

// FINDING 2: an independent provider's systemic failure must not be recorded
// against the leased YouTube session or the YouTube platform state.
func TestFinalAudit_F2_IndependentProviderFailure_NotAttributedToYouTubeLease(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, yt, sc, _ := setupPreRoutingEnv(t, before)

	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)
	sc.SetSearchErr(apperr.New(apperr.CodeProviderUnavailable, "soundcloud is down"))

	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err == nil {
		t.Fatal("expected the SoundCloud provider failure to surface")
	}
	if code := apperr.CodeOf(err); code != apperr.CodeProviderUnavailable {
		t.Fatalf("error code = %s, want PROVIDER_UNAVAILABLE", code)
	}

	assertSessionHealthUnchanged(t, pool.Sessions()[0], before)
	if pool.IsPlatformCooling() {
		t.Fatal("a SoundCloud provider failure must not put the YouTube pool into platform cooldown")
	}
}

// FINDING 2: an independent provider's success must not certify the health of
// the YouTube session that happened to be leased.
func TestFinalAudit_F2_IndependentProviderSuccess_DoesNotCertifyYouTubeSession(t *testing.T) {
	before := healthyAuditSession()
	before.HealthStatus = mediasession.HealthUnknown
	orch, pool, ytm, yt, sc, _ := setupPreRoutingEnv(t, before)

	ytm.SetCandidates(nil)
	yt.SetCandidates(nil)
	sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil {
		t.Fatalf("expected SoundCloud fallback to succeed: %v", err)
	}
	if res.Candidate.Provider != "soundcloud" {
		t.Fatalf("candidate provider = %s, want soundcloud", res.Candidate.Provider)
	}
	if s := pool.Sessions()[0]; s.HealthStatus != mediasession.HealthUnknown {
		t.Fatalf("session status = %s, want unknown (SoundCloud success proves nothing about YouTube)", s.HealthStatus)
	}
}

// FINDING 2 / CROSS-FINDING E: genuine YouTube session failures are still recorded.
func TestFinalAudit_F2_RealYouTubeSessionFailures_StillRecorded(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus mediasession.HealthStatus
	}{
		{"bot_challenge", apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot"), mediasession.HealthBotChallenge},
		{"rate_limited", apperr.New(apperr.CodeSessionRateLimited, "HTTP 429"), mediasession.HealthRateLimited},
		{"auth_failed", apperr.New(apperr.CodeSessionAuthFailed, "cookies expired"), mediasession.HealthAuthFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch, pool, ytm, _, _, _ := setupPreRoutingEnv(t, healthyAuditSession())
			ytm.SetSearchErr(tc.err)

			_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
			if err == nil {
				t.Fatal("expected the session failure to surface")
			}

			s := pool.Sessions()[0]
			if s.HealthStatus != tc.wantStatus {
				t.Fatalf("HealthStatus = %s, want %s", s.HealthStatus, tc.wantStatus)
			}
			if s.ConsecutiveFailures != 1 {
				t.Fatalf("ConsecutiveFailures = %d, want 1", s.ConsecutiveFailures)
			}
			if s.LastFailureAt == nil {
				t.Fatal("LastFailureAt must be recorded for a real session failure")
			}
			if s.LastFailureReason == "" {
				t.Fatal("LastFailureReason must be recorded for a real session failure")
			}
		})
	}
}

// FINDING 2: a real YouTube resolution still records a confirmed success.
func TestFinalAudit_F2_YouTubeSuccess_StillRecordsHealthy(t *testing.T) {
	before := healthyAuditSession()
	before.HealthStatus = mediasession.HealthUnknown
	orch, pool, ytm, _, _, _ := setupPreRoutingEnv(t, before)

	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "yt-1")})

	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil {
		t.Fatalf("expected YouTube resolution to succeed: %v", err)
	}
	if res.SessionID != "sess-yt-1" {
		t.Fatalf("SessionID = %q, want sess-yt-1", res.SessionID)
	}
	if s := pool.Sessions()[0]; s.HealthStatus != mediasession.HealthHealthy {
		t.Fatalf("session status = %s, want healthy after real media success", s.HealthStatus)
	}
}

// FINDING 2: a cancellation is not a session fault and must not be attributed.
func TestFinalAudit_F2_Cancellation_NotAttributedToSession(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, _, _, _ := setupPreRoutingEnv(t, before)

	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "yt-1")})

	ctx, cancel := context.WithCancel(manualCtx())
	cancel()

	if _, err := orch.ResolveMedia(ctx, "ytmusic", auditTrack(), 5); err == nil {
		t.Fatal("expected cancellation to fail the attempt")
	}

	assertSessionHealthUnchanged(t, pool.Sessions()[0], before)
}

// ---------------------------------------------------------------------------
// CROSS-FINDING H: legacy jobs with no persisted origin must not gain the
// manual policy, end to end from the job record to the provider decision.
// ---------------------------------------------------------------------------

func TestFinalAudit_CrossH_LegacyJobOrigin_DrivesProviderPolicy(t *testing.T) {
	cases := []struct {
		name            string
		job             jobs.Job
		wantSoundCloud  bool
		wantResolveFail bool
	}{
		{
			name:            "legacy release job keeps the conservative subscription policy",
			job:             jobs.Job{Type: jobs.TypeRelease},
			wantSoundCloud:  false,
			wantResolveFail: true,
		},
		{
			name:            "legacy artist job is provably manual and keeps resilience",
			job:             jobs.Job{Type: jobs.TypeArtist},
			wantSoundCloud:  true,
			wantResolveFail: false,
		},
		{
			name:            "tagged subscription job stays protected",
			job:             jobs.Job{Type: jobs.TypeRelease, Options: jobs.Options{Origin: jobs.OriginSubscription}},
			wantSoundCloud:  false,
			wantResolveFail: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch, _, _, _, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
			cooldown.Trigger("youtube", 30*time.Minute)
			sc.SetCandidates([]provider.MediaCandidate{auditCandidate("soundcloud", "sc-1")})

			origin := orchestrator.OriginSubscription
			if tc.job.ResolveOrigin() == jobs.OriginManual {
				origin = orchestrator.OriginManual
			}
			ctx := orchestrator.WithOrigin(context.Background(), origin)

			_, err := orch.ResolveMedia(ctx, "ytmusic", auditTrack(), 5)
			if tc.wantResolveFail && err == nil {
				t.Fatal("expected the attempt to defer, got success")
			}
			if !tc.wantResolveFail && err != nil {
				t.Fatalf("expected resolution to succeed, got %v", err)
			}

			gotSoundCloud := sc.SearchCalls() > 0
			if gotSoundCloud != tc.wantSoundCloud {
				t.Fatalf("SoundCloud contacted = %v, want %v", gotSoundCloud, tc.wantSoundCloud)
			}
		})
	}
}
