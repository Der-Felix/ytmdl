package ytdlp

import (
	"errors"
	"testing"

	"ytdm/backend/internal/apperr"
)

// Verbatim wording observed in production, where it paused the whole YouTube
// family under the generic "yt-dlp failed" fallback: no existing rule matched
// a paid-tier gate on a single item.
const stderrMusicPremiumRequired = "ERROR: [youtube] NbjScDfM4mA: This video is only available to Music Premium members"

func TestPremiumRequiredIsACandidateFailure(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, stderr := range []string{
		stderrMusicPremiumRequired,
		"ERROR: [youtube] x: This video is only available to YouTube Premium members",
		"WARNING: [youtube] x: Some formats may be missing\n" + stderrMusicPremiumRequired,
	} {
		err := ClassifyError(stderr, cause)
		if apperr.CodeOf(err) != apperr.CodeMediaPremiumRequired || !errors.Is(err, ErrPremiumRequired) {
			t.Fatalf("%q: %v", stderr, err)
		}
		if apperr.ScopeOf(err) != apperr.ScopeCandidate || apperr.StopsCandidateFanout(err) || apperr.Retryable(err) {
			t.Fatalf("%q: scope = %s, retryable = %v", stderr, apperr.ScopeOf(err), apperr.Retryable(err))
		}
		if errors.Is(err, ErrAgeRestricted) || errors.Is(err, ErrItemUnavailable) {
			t.Fatalf("%q: misclassified as a different candidate failure", stderr)
		}
	}
}

// A bare mention of "premium" elsewhere must not be mistaken for the gate.
func TestPremiumWordingNeverMasksOtherFailures(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
	}{
		{"unrelated premium mention", "ERROR: [youtube] x: Premium content delivery network error", apperr.CodeProviderUnavailable},
		{"no error line", "WARNING: [youtube] x: This video is only available to Music Premium members", apperr.CodeProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyError(tc.stderr, cause)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), tc.want, err)
			}
			if errors.Is(err, ErrPremiumRequired) {
				t.Fatalf("misclassified as premium-required: %v", err)
			}
		})
	}
}
