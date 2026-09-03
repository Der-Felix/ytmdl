package ytdlp

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
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
