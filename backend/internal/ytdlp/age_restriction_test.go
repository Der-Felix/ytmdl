package ytdlp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/throughput"
)

// The first two messages are taken verbatim (ids replaced) from the
// production logs of 0.27.1 and 0.27.2-rc.1, where each of them paused the
// whole YouTube family for a minute.
const (
	stderrSorryAgeRestricted = "ERROR: [youtube] AAAAAAAAAAA: Sorry, this content is age-restricted"
	stderrVerifyYourAge      = "ERROR: [youtube] BBBBBBBBBBB: Verify your age. Complete a brief check to show you're old enough to play this content. Learn more"
)

func TestAgeRestrictionIsACandidateFailure(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, stderr := range []string{
		stderrSorryAgeRestricted,
		stderrVerifyYourAge,
		"ERROR: [youtube] CCCCCCCCCCC: This video is age-restricted and only available on YouTube",
		// The id is not part of the statement, even when it happens to spell a hint.
		"ERROR: [youtube] xLoginCooki: Sorry, this content is age-restricted",
		// Warnings about the extraction do not describe the item.
		"WARNING: [youtube] CCCCCCCCCCC: Skipping client since it does not support cookies\n" + stderrSorryAgeRestricted,
		// A statement that names the age gate decides on its own, even when it
		// asks for a sign-in, names cookies or mentions an account: that is how
		// the platform words the gate. Reading it as a session failure is what
		// cooled down and unhealthed working sessions in 0.27.x.
		"ERROR: [youtube] x: Sign in to confirm your age. This video may be inappropriate for some users.",
		"ERROR: [youtube] x: This video is age-restricted. Your cookies are expired.",
		"ERROR: [youtube] x: Sorry, this content is age-restricted. Use --cookies-from-browser or --cookies for the authentication",
		"ERROR: [youtube] x: This video is age-restricted. Please log in to continue",
		"ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm your age or subscription: login required",
		"ERROR: [youtube] x: This video is unavailable. Sign in to confirm your age",
	} {
		err := ClassifyError(stderr, cause)
		if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
			t.Fatalf("%q: code = %s, want %s", stderr, apperr.CodeOf(err), apperr.CodeTrackNotFound)
		}
		if !errors.Is(err, ErrAgeRestricted) {
			t.Fatalf("%q: not marked as age restricted: %v", stderr, err)
		}
		if apperr.ScopeOf(err) != apperr.ScopeCandidate || !apperr.AllowsCandidateFallback(err) || apperr.StopsCandidateFanout(err) {
			t.Fatalf("%q: scope = %s, want a candidate failure that allows fallback", stderr, apperr.ScopeOf(err))
		}
		if apperr.Retryable(err) {
			t.Fatalf("%q: an age restriction must not be retried as such", stderr)
		}
	}
}

// A restriction first reported by the download process is the same candidate
// failure: it is not a generic, retryable download error.
func TestAgeRestrictionReportedByDownloadIsACandidateFailure(t *testing.T) {
	err := classifyDownloadError(stderrSorryAgeRestricted, errors.New("exit status 1"))
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound || !errors.Is(err, ErrAgeRestricted) {
		t.Fatalf("download classification = %v", err)
	}
	if apperr.Retryable(err) || apperr.ScopeOf(err) != apperr.ScopeCandidate {
		t.Fatalf("scope = %s, retryable = %v", apperr.ScopeOf(err), apperr.Retryable(err))
	}
}

// Rate-limit and bot signals win over an age statement in the same output,
// and wording that never names the age gate outright keeps its previous
// classification: it can mean the session is not accepted.
//
// A sign-in or credential hint no longer does so once the statement names the
// age gate itself - see TestAgeRestrictionIsACandidateFailure.
func TestAgeRestrictionNeverMasksProtectionOrAmbiguity(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
	}{
		{"bot challenge", "ERROR: [youtube] x: Sorry, this content is age-restricted. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge},
		{"http 429", "ERROR: [youtube] x: Sorry, this content is age-restricted: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited},
		{"account rate limit", "ERROR: [youtube] x: Video unavailable. This content isn't available, try again later. Your account has been rate-limited", apperr.CodeProviderRateLimited},
		{"session rate limit", "ERROR: [youtube] x: This video is age-restricted. The current session has been rate-limited by YouTube", apperr.CodeSessionRateLimited},
		{"network error with age warning", "WARNING: [youtube] x: this video is age-restricted\nERROR: [youtube] x: Unable to download API page: [Errno -2] Name does not resolve", apperr.CodeProviderUnavailable},
		{"ambiguous: account", "ERROR: [youtube] x: Verify your age. Your account must show you're old enough to play this content", apperr.CodeProviderUnavailable},
		{"ambiguous: inappropriate only", "ERROR: [youtube] x: This video may be inappropriate for some users.", apperr.CodeProviderUnavailable},
		{"no error line", "WARNING: [youtube] x: Sorry, this content is age-restricted", apperr.CodeProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyError(tc.stderr, cause)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), tc.want, err)
			}
			if errors.Is(err, ErrAgeRestricted) {
				t.Fatalf("classified as a plain age restriction: %v", err)
			}
		})
	}
}

// The error of an age restriction carries a fixed category, never the
// provider's output, so neither logs nor the item's stored message can leak an
// address or a token that output might contain.
func TestAgeRestrictionErrorCarriesNoProviderOutput(t *testing.T) {
	stderr := "ERROR: [youtube] AAAAAAAAAAA: Sorry, this content is age-restricted https://www.youtube.com/watch?v=AAAAAAAAAAA&pot=SECRETTOKEN"
	err := ClassifyError(stderr, errors.New("exit status 1"))
	if !errors.Is(err, ErrAgeRestricted) {
		t.Fatalf("not classified as age restricted: %v", err)
	}
	for _, leak := range []string{"SECRETTOKEN", "https://", "youtube.com"} {
		if strings.Contains(err.Error(), leak) || strings.Contains(apperr.MessageOf(err), leak) {
			t.Fatalf("error leaks %q: %v", leak, err)
		}
	}
}

// An age-restricted item is answered from the bounded negative cache for the
// same session context only, and counted under its own category.
func TestAgeRestrictionUsesBoundedPerSessionReuse(t *testing.T) {
	binary := fakeBinary(t, "echo '"+stderrSorryAgeRestricted+"' >&2\nexit 1\n")
	rec := throughput.New()
	base := New(Options{Binary: binary, Recorder: rec, Label: "youtube", QueryCache: QueryCacheOptions{NegativeTTL: time.Minute}})
	clock := time.Now()
	base.cache.now = func() time.Time { return clock }
	target := "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	sessionA := base.WithCookieFile("/cookies/a.txt")
	sessionB := base.WithCookieFile("/cookies/b.txt")

	query := func(c *Client) {
		t.Helper()
		_, err := c.Query(context.Background(), target, "--no-playlist")
		if !errors.Is(err, ErrAgeRestricted) || apperr.CodeOf(err) != apperr.CodeTrackNotFound {
			t.Fatalf("unexpected answer: %v", err)
		}
	}

	query(sessionA)
	query(sessionA) // replayed: same item, same session context
	if got := processStarts(t, binary); got != 1 {
		t.Fatalf("process starts = %d, want 1 while the answer is fresh", got)
	}
	query(sessionB) // another session context asks again
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("process starts = %d, want one per session context (2)", got)
	}

	clock = clock.Add(time.Minute + time.Second)
	query(sessionA) // expired: the item is checked again
	if got := processStarts(t, binary); got != 3 {
		t.Fatalf("process starts = %d, want the expired answer re-checked (3)", got)
	}

	counts := rec.Drain().Counts
	if counts["ytdlp.youtube.extract.candidate.age_restricted"] != 3 || counts["ytdlp.youtube.extract.error.TRACK_NOT_FOUND"] != 3 {
		t.Fatalf("counters: %v", counts)
	}
	if counts["ytdlp.youtube.extract.cache_hit"] != 1 {
		t.Fatalf("cache hits = %d, want 1", counts["ytdlp.youtube.extract.cache_hit"])
	}
}

// An age statement is about the item; a timeout, a reset connection or a
// challenge is about everything. When both are in the same output the systemic
// answer wins, because the age rule is answered before the network case and
// would otherwise turn a passing outage into a permanent candidate failure -
// one that is never retried and that queryCache.ttlFor would keep for its
// whole negative TTL.
func TestAgeStatementNeverMasksSystemicEvidence(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
	}{
		{"timed out", "ERROR: [youtube] x: This video is age-restricted. The read operation timed out", apperr.CodeProviderUnavailable},
		{"connection refused", "ERROR: [youtube] x: Sorry, this content is age-restricted. connection refused", apperr.CodeProviderUnavailable},
		// Vetoed, this falls through to the sign-in rule exactly as it did
		// before the age rule existed: still systemic, still session scoped.
		{"connection reset on a sign-in prompt", "ERROR: [youtube] x: Sign in to confirm your age. connection reset by peer", apperr.CodeSessionAuthFailed},
		{"network unreachable", "ERROR: [youtube] x: This video is age-restricted: network is unreachable", apperr.CodeProviderUnavailable},
		{"name resolution", "ERROR: [youtube] x: age-restricted: temporary failure in name resolution", apperr.CodeProviderUnavailable},
		// The evidence counts wherever yt-dlp put it, as it does for the
		// network case itself.
		{"network on a warning line", "WARNING: [youtube] x: The read operation timed out\nERROR: [youtube] x: Sorry, this content is age-restricted", apperr.CodeProviderUnavailable},
		// A captcha names no bot, so the age rule is the only thing that could
		// answer it.
		{"captcha", "ERROR: [youtube] x: Sorry, this content is age-restricted. Please solve the captcha to continue", apperr.CodeProviderUnavailable},
		{"captcha on a sign-in prompt", "ERROR: [youtube] x: Sign in to confirm your age. Complete the captcha first", apperr.CodeSessionAuthFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyError(tc.stderr, cause)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), tc.want, err)
			}
			if apperr.CodeOf(err) == apperr.CodeTrackNotFound {
				t.Fatal("a systemic failure must not become a candidate failure")
			}
			if errors.Is(err, ErrAgeRestricted) {
				t.Fatalf("classified as an age restriction: %v", err)
			}
			// Never a candidate failure means never stored by the negative
			// cache, which keeps TrackNotFound extraction failures only.
			if (&queryCache{opts: QueryCacheOptions{NegativeTTL: time.Minute}}).ttlFor(queryExtract, nil, err) != 0 {
				t.Fatal("a systemic failure must not enter the negative cache")
			}
			if apperr.ScopeOf(err) == apperr.ScopeCandidate {
				t.Fatalf("scope = %s, want a systemic scope", apperr.ScopeOf(err))
			}
		})
	}
}

// The veto is narrow: it must not reach the wording the v0.28 policy
// deliberately answers as a candidate failure, and it must not disturb the
// signals that already had precedence.
func TestSystemicVetoLeavesTheAgeGatePolicyIntact(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
		age    bool
	}{
		{"plain age gate", "ERROR: [youtube] x: Sign in to confirm your age", apperr.CodeTrackNotFound, true},
		{"age gate with expired cookies", "ERROR: [youtube] x: This video is age-restricted. Your cookies are expired.", apperr.CodeTrackNotFound, true},
		{"age gate with a login prompt", "ERROR: [youtube] x: This video is age-restricted. Please log in to continue", apperr.CodeTrackNotFound, true},
		{"auth without an age gate", "ERROR: [youtube] x: Sign in to confirm your identity", apperr.CodeSessionAuthFailed, false},
		{"expired cookies without an age gate", "ERROR: [youtube] x: Your cookies are expired", apperr.CodeSessionAuthFailed, false},
		{"bot challenge beside an age gate", "ERROR: [youtube] x: Sorry, this content is age-restricted. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge, false},
		{"provider rate limit beside an age gate", "ERROR: [youtube] x: Sorry, this content is age-restricted: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited, false},
		{"session rate limit beside an age gate", "ERROR: [youtube] x: This video is age-restricted. The current session has been rate-limited by YouTube", apperr.CodeSessionRateLimited, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyError(tc.stderr, cause)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), tc.want, err)
			}
			if errors.Is(err, ErrAgeRestricted) != tc.age {
				t.Fatalf("ErrAgeRestricted = %v, want %v", !tc.age, tc.age)
			}
			if tc.age && apperr.ScopeOf(err) != apperr.ScopeCandidate {
				t.Fatalf("scope = %s, want %s", apperr.ScopeOf(err), apperr.ScopeCandidate)
			}
		})
	}
}
