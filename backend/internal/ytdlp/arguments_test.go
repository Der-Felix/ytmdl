package ytdlp

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
)

// TestArgumentVectorsAreAcceptedByYTDLP runs the exact flag combinations the
// backend uses against the installed yt-dlp. yt-dlp parses its options before
// it touches the network, so appending --version turns each invocation into an
// offline check: a flag that no longer exists fails here instead of failing
// every download at runtime.
func TestArgumentVectorsAreAcceptedByYTDLP(t *testing.T) {
	binary, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is not installed; the container image provides it")
	}

	client := New(Options{Binary: binary, CookieFile: "/tmp/cookies.txt", FFmpegLocation: "/usr/bin"})

	vectors := map[string][]string{
		"query":   append(client.baseArgs(), "--dump-json"),
		"search":  append(client.baseArgs(), "--dump-json", "--flat-playlist"),
		"resolve": append(client.baseArgs(), "--dump-json", "--no-playlist"),
		"music search": append(client.baseArgs(),
			"--dump-json", "--flat-playlist", "--playlist-items", "1:10"),
		"download": append(client.baseArgs(),
			downloadArgs(DefaultFormatSelector, 3, t.TempDir(), "")...),
		"download rate limit": append(client.baseArgs(),
			downloadArgs(DefaultFormatSelector, 3, t.TempDir(), "5M")...),
		"version": append(client.baseArgs(), "--version"),
	}

	for name, args := range vectors {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(binary, append(args, "--version")...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			cmd.Stdout = &bytes.Buffer{}

			if err := cmd.Run(); err != nil {
				t.Fatalf("yt-dlp rejected the %s arguments: %v\n%s", name, err, stderr.String())
			}
			if message := stderr.String(); strings.Contains(message, "no such option") {
				t.Fatalf("yt-dlp reported an unknown option for %s:\n%s", name, message)
			}
		})
	}
}

func TestDownloadArgsRateLimit(t *testing.T) {
	argsNoLimit := downloadArgs(DefaultFormatSelector, 3, "/tmp", "")
	for i, arg := range argsNoLimit {
		if arg == "--limit-rate" {
			t.Fatalf("expected no --limit-rate, found at index %d", i)
		}
	}

	argsWithLimit := downloadArgs(DefaultFormatSelector, 3, "/tmp", "10M")
	var found bool
	for i, arg := range argsWithLimit {
		if arg == "--limit-rate" {
			found = true
			if i+1 >= len(argsWithLimit) || argsWithLimit[i+1] != "10M" {
				t.Fatalf("expected --limit-rate 10M, got following arg %v", argsWithLimit[i+1:])
			}
		}
	}
	if !found {
		t.Fatal("expected --limit-rate in downloadArgs when rateLimit is specified")
	}
}

func TestPlayerClientsConfiguration(t *testing.T) {
	// 1. Unset: no --extractor-args
	clientDefault := New(Options{CookieFile: "/path/to/cookies.txt"})
	argsDefault := clientDefault.baseArgs()
	for _, arg := range argsDefault {
		if strings.Contains(arg, "player_client") || arg == "--extractor-args" {
			t.Fatalf("unexpected player_client argument in default configuration: %s", arg)
		}
	}
	// Cookie preserved
	var hasCookie bool
	for i, arg := range argsDefault {
		if arg == "--cookies" && i+1 < len(argsDefault) && argsDefault[i+1] == "/path/to/cookies.txt" {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Fatal("expected --cookies flag to be preserved when player_client is unset")
	}

	// 2. Configured valid: --extractor-args youtube:player_client=android,web
	clientCustom := New(Options{
		CookieFile:    "/path/to/cookies.txt",
		PlayerClients: "android,web",
	})
	argsCustom := clientCustom.baseArgs()
	var hasExtractorArg bool
	for i, arg := range argsCustom {
		if arg == "--extractor-args" && i+1 < len(argsCustom) && argsCustom[i+1] == "youtube:player_client=android,web" {
			hasExtractorArg = true
		}
	}
	if !hasExtractorArg {
		t.Fatalf("expected --extractor-args youtube:player_client=android,web, got: %v", argsCustom)
	}

	// 3. Malformed: rejected safely by ValidatePlayerClients
	malformedVectors := []string{
		"android; rm -rf /",
		"web|curl evil",
		"android$(whoami)",
		"web\nnewline",
	}
	for _, malformed := range malformedVectors {
		if err := ValidatePlayerClients(malformed); err == nil {
			t.Fatalf("expected ValidatePlayerClients to reject malformed input: %q", malformed)
		}
		// When passed to New(), malformed configuration must be ignored/dropped
		clientRejected := New(Options{PlayerClients: malformed})
		for _, arg := range clientRejected.baseArgs() {
			if strings.Contains(arg, "player_client") || arg == "--extractor-args" {
				t.Fatalf("malformed player client was not rejected in baseArgs: %s", arg)
			}
		}
	}
}

func TestClassifyError_Taxonomy(t *testing.T) {
	cause := errors.New("exit status 1")

	tests := []struct {
		name     string
		stderr   string
		wantCode apperr.Code
	}{
		{
			name:     "rate limit with video unavailable text",
			stderr:   "ERROR: [youtube] 2vQYmGkynmc: Video unavailable. This content isn't available, try again later. The current session has been rate-limited by YouTube for up to an hour",
			wantCode: apperr.CodeProviderRateLimited,
		},
		{
			name:     "http 429 too many requests",
			stderr:   "ERROR: [youtube] ABC: HTTP Error 429: Too Many Requests",
			wantCode: apperr.CodeProviderRateLimited,
		},
		{
			name:     "exceeding rate limit",
			stderr:   "ERROR: [youtube] 123: The request cannot be completed because you are exceeding the rate limit",
			wantCode: apperr.CodeProviderRateLimited,
		},
		{
			name:     "bot challenge",
			stderr:   "ERROR: [youtube] dQw4w9WgXcQ: Sign in to confirm you're not a bot",
			wantCode: apperr.CodeProviderUnavailable,
		},
		{
			name:     "bot verification required",
			stderr:   "ERROR: [youtube] dQw4w9WgXcQ: bot verification required",
			wantCode: apperr.CodeProviderUnavailable,
		},
		{
			name:     "network timeout",
			stderr:   "ERROR: [youtube] connection timed out after 30 seconds",
			wantCode: apperr.CodeProviderUnavailable,
		},
		{
			name:     "connection reset",
			stderr:   "ERROR: [youtube] read: connection reset by peer",
			wantCode: apperr.CodeProviderUnavailable,
		},
		{
			name:     "clean video unavailable",
			stderr:   "ERROR: [youtube] 2vQYmGkynmc: Video unavailable",
			wantCode: apperr.CodeTrackNotFound,
		},
		{
			name:     "private video",
			stderr:   "ERROR: [youtube] 2vQYmGkynmc: Private video. Sign in if you've been granted access",
			wantCode: apperr.CodeTrackNotFound,
		},
		{
			name:     "removed by uploader",
			stderr:   "ERROR: [youtube] 2vQYmGkynmc: Video removed by the uploader",
			wantCode: apperr.CodeTrackNotFound,
		},
		{
			name:     "unsupported url",
			stderr:   "ERROR: Unsupported URL: https://invalid-url.com",
			wantCode: apperr.CodeInvalidRequest,
		},
		{
			name:     "generic yt-dlp error",
			stderr:   "ERROR: something unexpected happened",
			wantCode: apperr.CodeProviderUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyError(tc.stderr, cause)
			gotCode := apperr.CodeOf(err)
			if gotCode != tc.wantCode {
				t.Fatalf("classifyError(%q) = %v, want %v", tc.stderr, gotCode, tc.wantCode)
			}
			// Verify Retryable consistency
			if tc.wantCode == apperr.CodeProviderRateLimited || tc.wantCode == apperr.CodeProviderUnavailable {
				if !apperr.Retryable(err) {
					t.Fatalf("expected code %v to be retryable", gotCode)
				}
			}
			if tc.wantCode == apperr.CodeTrackNotFound {
				if apperr.Retryable(err) {
					t.Fatalf("expected code %v NOT to be retryable", gotCode)
				}
			}
		})
	}
}
