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

// Explicit protection signals win over an age statement in the same output,
// and statements that ask for a sign-in, name credentials or an account keep
// their previous classification: they can mean the session is not accepted.
func TestAgeRestrictionNeverMasksProtectionOrAmbiguity(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
	}{
		{"bot challenge", "ERROR: [youtube] x: Sorry, this content is age-restricted. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge},
		{"sign in to confirm your age", "ERROR: [youtube] x: Sign in to confirm your age. This video may be inappropriate for some users.", apperr.CodeSessionAuthFailed},
		{"expired cookies", "ERROR: [youtube] x: This video is age-restricted. Your cookies are expired.", apperr.CodeSessionAuthFailed},
		{"http 429", "ERROR: [youtube] x: Sorry, this content is age-restricted: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited},
		{"account rate limit", "ERROR: [youtube] x: Video unavailable. This content isn't available, try again later. Your account has been rate-limited", apperr.CodeProviderRateLimited},
		{"session rate limit", "ERROR: [youtube] x: This video is age-restricted. The current session has been rate-limited by YouTube", apperr.CodeSessionRateLimited},
		{"network error with age warning", "WARNING: [youtube] x: this video is age-restricted\nERROR: [youtube] x: Unable to download API page: [Errno -2] Name does not resolve", apperr.CodeProviderUnavailable},
		{"ambiguous: cookies hint", "ERROR: [youtube] x: Sorry, this content is age-restricted. Use --cookies-from-browser or --cookies for the authentication", apperr.CodeProviderUnavailable},
		{"ambiguous: log in", "ERROR: [youtube] x: This video is age-restricted. Please log in to continue", apperr.CodeProviderUnavailable},
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
