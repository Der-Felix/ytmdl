package orchestrator_test

import (
	"errors"
	"fmt"
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

// With the fallback switched on, a candidate whose format answer offered no
// usable stream ends the attempt as UNSUPPORTED_MEDIA_FORMAT. It is permanent:
// the answer is reused from the query cache for longer than the retry backoff
// runs, so a retry would only replay it.
func TestFormatLimitationEndsTheAttemptPermanently(t *testing.T) {
	err := exhaustCandidates(t, absent(), formatLimitation(), absent())
	if apperr.CodeOf(err) != apperr.CodeUnsupportedMediaFormat {
		t.Fatalf("err = %v", err)
	}
	if apperr.Retryable(err) {
		t.Fatal("a format limitation became retryable against an unchanged cached answer")
	}
	if apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("scope = %v, want candidate", apperr.ScopeOf(err))
	}
	if apperr.StopsCandidateFanout(err) {
		t.Fatal("a format limitation stopped the fanout")
	}
}

// rc2NoAudioOnlyStream is the rejection the YouTube resolver reports with the
// fallback switched off - the same code and wording as in v0.27.2-rc.2.
func rc2NoAudioOnlyStream(id string) error {
	return apperr.Newf(apperr.CodeDownloadFailed,
		"The media item %q offers no audio only stream (formats: 5 total, 1 muxed, 0 video only, "+
			"0 video with unknown audio, 4 images, 0 unknown, 0 other; muxed up to 182 kbps total, 0 kbps audio).", id)
}

// With the fallback switched off, an exhausted fanout ends exactly as in
// v0.27.2-rc.2: permanently, as TRACK_NOT_FOUND with the same wording, whatever
// the candidates' own codes were - including download and verification codes
// that are retryable on their own.
func TestFallbackOffKeepsTheRc2Summary(t *testing.T) {
	for _, tc := range []struct {
		name string
		errs []error
	}{
		{"no audio only stream", []error{rc2NoAudioOnlyStream("a"), rc2NoAudioOnlyStream("b")}},
		{"mixed with absent", []error{absent(), rc2NoAudioOnlyStream("b"), absent()}},
		{"mixed with verification", []error{
			rc2NoAudioOnlyStream("a"),
			apperr.New(apperr.CodeMediaVerifyFailed, "downloaded audio failed verification"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := exhaustCandidates(t, tc.errs...)
			if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
				t.Fatalf("err = %v, want TRACK_NOT_FOUND", err)
			}
			if apperr.Retryable(err) {
				t.Fatal("the rc.2 summary became retryable")
			}
			want := fmt.Sprintf("Keine der %d passenden Quellen konnte aufgelöst werden.", len(tc.errs))
			if apperr.MessageOf(err) != want {
				t.Fatalf("message = %q, want %q", apperr.MessageOf(err), want)
			}
			if !errors.Is(err, tc.errs[len(tc.errs)-1]) {
				t.Fatalf("the summary lost the last candidate failure as its cause: %v", err)
			}
		})
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
