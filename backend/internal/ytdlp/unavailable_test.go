package ytdlp

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/throughput"
)

// Verbatim from the 0.27.2-rc.1 log (id replaced), where it paused the whole
// YouTube family: the existing "video unavailable" rule never matched it.
const stderrThisVideoIsUnavailable = "ERROR: [youtube] DDDDDDDDDDD: This video is unavailable"

func TestThisVideoIsUnavailableIsACandidateFailure(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, stderr := range []string{
		stderrThisVideoIsUnavailable,
		"WARNING: [youtube] DDDDDDDDDDD: Some formats may be missing\n" + stderrThisVideoIsUnavailable,
		// Provider prefix and id are not part of the statement.
		"ERROR: [youtube] xLaterLogin: This video is unavailable",
	} {
		err := ClassifyError(stderr, cause)
		if apperr.CodeOf(err) != apperr.CodeTrackNotFound || !errors.Is(err, ErrItemUnavailable) {
			t.Fatalf("%q: %v", stderr, err)
		}
		if apperr.ScopeOf(err) != apperr.ScopeCandidate || apperr.StopsCandidateFanout(err) || apperr.Retryable(err) {
			t.Fatalf("%q: scope = %s, retryable = %v", stderr, apperr.ScopeOf(err), apperr.Retryable(err))
		}
		if errors.Is(err, ErrAgeRestricted) {
			t.Fatalf("%q: counted as an age restriction", stderr)
		}
	}
}

// Only the exact statement about the requested video is a candidate failure.
// Every protection signal keeps precedence, and other uses of "unavailable"
// keep their classification.
func TestUnavailableWordingNeverMasksProtectionOrOutages(t *testing.T) {
	cause := errors.New("exit status 1")
	for _, tc := range []struct {
		name   string
		stderr string
		want   apperr.Code
	}{
		{"http 429", "ERROR: [youtube] x: This video is unavailable: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited},
		{"try again later", "ERROR: [youtube] x: This video is unavailable, try again later", apperr.CodeProviderRateLimited},
		{"account rate limit", "ERROR: [youtube] x: Video unavailable. This content isn't available, try again later. Your account has been rate-limited", apperr.CodeProviderRateLimited},
		{"bot challenge", "ERROR: [youtube] x: This video is unavailable. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge},
		{"expired cookies", "ERROR: [youtube] x: This video is unavailable. Your cookies are expired", apperr.CodeSessionAuthFailed},
		{"sign in to confirm", "ERROR: [youtube] x: This video is unavailable. Sign in to confirm your age", apperr.CodeSessionAuthFailed},
		{"network", "WARNING: [youtube] x: This video is unavailable\nERROR: [youtube] x: Unable to download API page: [Errno -2] Name does not resolve", apperr.CodeProviderUnavailable},
		{"service unavailable", "ERROR: [youtube] x: HTTP Error 503: Service Unavailable", apperr.CodeProviderUnavailable},
		{"ambiguous: account", "ERROR: [youtube] x: This video is unavailable for this account", apperr.CodeProviderUnavailable},
		{"ambiguous: temporarily", "ERROR: [youtube] x: This video is unavailable temporarily", apperr.CodeProviderUnavailable},
		{"other unavailable wording", "ERROR: [youtube] x: The requested content is unavailable", apperr.CodeProviderUnavailable},
		{"no error line", "WARNING: [youtube] x: This video is unavailable", apperr.CodeProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyError(tc.stderr, cause)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), tc.want, err)
			}
			if errors.Is(err, ErrItemUnavailable) {
				t.Fatalf("classified as an unavailable item: %v", err)
			}
		})
	}
}

// The unavailable video is replayed from the negative cache per session
// context until it expires, like every other item-scoped failure.
func TestThisVideoIsUnavailableUsesBoundedPerSessionReuse(t *testing.T) {
	binary := fakeBinary(t, "echo '"+stderrThisVideoIsUnavailable+"' >&2\nexit 1\n")
	rec := throughput.New()
	base := New(Options{Binary: binary, Recorder: rec, Label: "youtube", QueryCache: QueryCacheOptions{NegativeTTL: time.Minute}})
	clock := time.Now()
	base.cache.now = func() time.Time { return clock }
	target := "https://www.youtube.com/watch?v=DDDDDDDDDDD"

	for _, c := range []*Client{base.WithCookieFile("/cookies/a.txt"), base.WithCookieFile("/cookies/a.txt"), base.WithCookieFile("/cookies/b.txt")} {
		if _, err := c.Query(context.Background(), target, "--no-playlist"); !errors.Is(err, ErrItemUnavailable) {
			t.Fatalf("unexpected answer: %v", err)
		}
	}
	if got := processStarts(t, binary); got != 2 {
		t.Fatalf("process starts = %d, want one per session context (2)", got)
	}
	clock = clock.Add(time.Minute + time.Second)
	if _, err := base.WithCookieFile("/cookies/a.txt").Query(context.Background(), target, "--no-playlist"); !errors.Is(err, ErrItemUnavailable) {
		t.Fatalf("after expiry: %v", err)
	}
	if got := processStarts(t, binary); got != 3 {
		t.Fatalf("process starts = %d, want the expired answer re-checked (3)", got)
	}
	if got := rec.Drain().Counts["ytdlp.youtube.extract.candidate.unavailable"]; got != 3 {
		t.Fatalf("candidate.unavailable = %d, want 3", got)
	}
}

// A candidate failure reported by the download process is counted under the
// download operation, a failure reported while resolving under extract, so
// the hourly summary tells the two phases apart.
func TestCandidateFailuresAreCountedPerPhase(t *testing.T) {
	for _, tc := range []struct {
		stderr string
		what   string
	}{
		{stderrSorryAgeRestricted, "candidate.age_restricted"},
		{stderrThisVideoIsUnavailable, "candidate.unavailable"},
	} {
		binary := fakeBinary(t, "echo '"+tc.stderr+"' >&2\nexit 1\n")
		rec := throughput.New()
		client := New(Options{Binary: binary, Recorder: rec, Label: "youtube"})

		if _, err := client.Query(context.Background(), "https://www.youtube.com/watch?v=AAAAAAAAAAA", "--no-playlist"); apperr.CodeOf(err) != apperr.CodeTrackNotFound {
			t.Fatalf("resolve: %v", err)
		}
		if _, err := client.Download(context.Background(), DownloadRequest{URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Dir: t.TempDir()}, nil); apperr.CodeOf(err) != apperr.CodeTrackNotFound {
			t.Fatalf("download: %v", err)
		}

		counts := rec.Drain().Counts
		if counts["ytdlp.youtube.extract."+tc.what] != 1 || counts["ytdlp.youtube.download."+tc.what] != 1 {
			t.Fatalf("%s per phase: %v", tc.what, counts)
		}
		if counts["ytdlp.youtube.extract.error.TRACK_NOT_FOUND"] != 1 || counts["ytdlp.youtube.download.error.TRACK_NOT_FOUND"] != 1 {
			t.Fatalf("error codes per phase: %v", counts)
		}
	}
}
