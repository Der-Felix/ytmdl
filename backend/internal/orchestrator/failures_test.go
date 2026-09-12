package orchestrator_test

import (
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

func formatLimitation() error {
	return apperr.New(apperr.CodeUnsupportedMediaFormat, "offers no audio only stream")
}

func absent() error {
	return apperr.New(apperr.CodeTrackNotFound, "The media item is unavailable; it is skipped.")
}

func transient() error {
	return apperr.New(apperr.CodeProviderUnavailable, "network error contacting media provider")
}

// exhaustCandidates makes every candidate of one attempt fail with the given
// errors, in order, and returns the error the item ends with.
func exhaustCandidates(t *testing.T, errs ...error) error {
	t.Helper()
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	candidates := make([]provider.MediaCandidate, 0, len(errs))
	for i, err := range errs {
		id := string(rune('a' + i))
		candidates = append(candidates, auditCandidate("ytmusic", id))
		ytm.SetResolveErr(id, err)
	}
	ytm.SetCandidates(candidates)
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)
	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err == nil {
		t.Fatal("the attempt was expected to fail")
	}
	return err
}

// Every candidate was genuinely unavailable: the item fails permanently, with
// the wording and the code it always had.
func TestAllCandidatesAbsentStaysPermanent(t *testing.T) {
	err := exhaustCandidates(t, absent(), absent(), absent())
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("err = %v", err)
	}
	if apperr.Retryable(err) {
		t.Fatal("an item whose sources are all gone became retryable")
	}
	if want := "Keine der 3 passenden Quellen konnte aufgelöst werden."; apperr.MessageOf(err) != want {
		t.Fatalf("message = %q, want %q", apperr.MessageOf(err), want)
	}
}

// A single candidate this backend could not use for technical reasons is
// enough to keep the item retryable: the format answer may look different next
// time, and the attempt budget bounds how often that is tried.
func TestOneFormatLimitationMakesTheAttemptRetryable(t *testing.T) {
	err := exhaustCandidates(t, absent(), formatLimitation(), absent())
	if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", err)
	}
	if !apperr.Retryable(err) {
		t.Fatal("a technical format limitation was written off as permanent")
	}
	if apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("scope = %v, want candidate", apperr.ScopeOf(err))
	}
	if apperr.StopsCandidateFanout(err) {
		t.Fatal("a format limitation stopped the fanout")
	}
}

// A transient failure outranks a format limitation: it is the class with the
// best chance of passing, and it keeps its own code and retry timing.
func TestTransientFailureOutranksTheOtherClasses(t *testing.T) {
	err := exhaustCandidates(t, absent(), formatLimitation(), transient())
	if apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("err = %v", err)
	}
	if !apperr.Retryable(err) {
		t.Fatal("a transient failure was written off as permanent")
	}
}

// A wait hint of the underlying failure survives the wrapping, so the item is
// not retried before the provider said it makes sense.
func TestTransientRetryHintSurvivesTheFanoutSummary(t *testing.T) {
	err := exhaustCandidates(t, absent(),
		apperr.NewRetryAfter(apperr.CodeProviderRateLimited, "slow down", 7*time.Minute))
	if got, ok := apperr.RetryAfter(err); !ok || got != 7*time.Minute {
		t.Fatalf("retry hint = %v (%v)", got, ok)
	}
}

// No candidate at all is a different statement from candidates that failed,
// and it keeps its own wording.
func TestNoCandidatesAtAllStaysItsOwnResult(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)
	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("err = %v", err)
	}
	if apperr.Retryable(err) {
		t.Fatal("an item without any candidate became retryable")
	}
}

// A format limitation must never put the provider family on hold: the absence
// of an audio only stream says nothing about the platform's state.
func TestFormatLimitationTriggersNoFamilyCooldown(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "a"), auditCandidate("ytmusic", "b")})
	ytm.SetResolveErr("a", formatLimitation())
	ytm.SetResolveErr("b", formatLimitation())
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	if _, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5); err == nil {
		t.Fatal("the attempt was expected to fail")
	}
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("the YouTube family was put on hold because of a format limitation")
	}
	if ytm.ResolveCalls() != 2 {
		t.Fatalf("resolve calls = %d, want both candidates tried", ytm.ResolveCalls())
	}
}

// Bot, auth and rate-limit failures keep their precedence: they stop the
// fanout at once and are never reclassified by the new summary.
func TestProtectionFailuresKeepTheirPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code apperr.Code
	}{
		{"rate limited", apperr.New(apperr.CodeProviderRateLimited, "rate limited"), apperr.CodeProviderRateLimited},
		{"bot challenge", apperr.New(apperr.CodeSessionBotChallenge, "confirm you are not a bot"), apperr.CodeSessionBotChallenge},
		{"auth failed", apperr.New(apperr.CodeSessionAuthFailed, "sign in to confirm"), apperr.CodeSessionAuthFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
			ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "a"), auditCandidate("ytmusic", "b")})
			ytm.SetResolveErr("a", tc.err)
			ytm.SetResolveErr("b", formatLimitation())
			orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

			_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
			if apperr.CodeOf(err) != tc.code {
				t.Fatalf("err = %v, want %s to keep precedence", err, tc.code)
			}
			if ytm.ResolveCalls() != 1 {
				t.Fatalf("resolve calls = %d, want the fanout stopped at once", ytm.ResolveCalls())
			}
		})
	}
}

// Resolving a combined stream is not an acquisition. It must not certify the
// session: only a download that passed verification may do that, and the
// downloader reports one only after ffprobe accepted the stored file.
func TestResolvingACombinedStreamDoesNotHealTheSession(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "a")})
	ytm.SetSource("a", &provider.MediaSource{
		Provider: "ytmusic", ID: "a", URL: "https://ytmusic.example/a", DurationMS: 200000,
		Formats: []provider.AudioFormat{{
			ID: "18", Codec: "mp4a.40.2", Container: "m4a", Combined: true,
			VideoCodec: "avc1.42001E", BitrateKbps: 96, TransferBitrateKbps: 500,
		}},
	})
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)

	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil {
		t.Fatalf("ResolveMedia: %v", err)
	}
	if !res.Source.Formats[0].Combined {
		t.Fatalf("source = %+v, want the combined format passed through", res.Source.Formats)
	}
	if res.SessionID == "" {
		t.Fatal("the resolved source lost its session affinity")
	}
	for _, s := range pool.Sessions() {
		if s.LastSuccessAt != nil {
			t.Fatalf("resolving alone certified session %s", s.ID)
		}
	}

	// A download that failed verification is a failure, not an acquisition.
	orch.RecordDownloadOutcome(manualCtx(), res.SessionID,
		apperr.New(apperr.CodeMediaVerifyFailed, "downloaded audio failed verification"))
	for _, s := range pool.Sessions() {
		if s.LastSuccessAt != nil {
			t.Fatalf("a failed verification certified session %s", s.ID)
		}
	}

	// Only the verified acquisition does.
	orch.RecordDownloadOutcome(manualCtx(), res.SessionID, nil)
	healed := false
	for _, s := range pool.Sessions() {
		if s.LastSuccessAt != nil {
			healed = true
		}
	}
	if !healed {
		t.Fatal("a verified acquisition did not certify the session")
	}
}
