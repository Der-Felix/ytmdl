// Package ytdlp is the technical adapter around the yt-dlp binary. It builds
// argument vectors, runs the process under a context and turns the machine
// readable output into Go values. No music or job logic lives here.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
)

// progressMarker prefixes the machine readable progress lines. yt-dlp renders
// the template verbatim, so the backend never has to parse the human readable
// progress bar.
const progressMarker = "@YTDM-PROGRESS@"

// progressTemplate is passed to --progress-template. Missing values are
// rendered as "NA" by yt-dlp.
const progressTemplate = "download:" + progressMarker +
	"%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s"

var validPlayerClientsRe = regexp.MustCompile(`^[a-zA-Z0-9_,+-]+$`)

// ValidatePlayerClients checks whether the player clients override syntax is valid.
func ValidatePlayerClients(clients string) error {
	trimmed := strings.TrimSpace(clients)
	if trimmed == "" {
		return nil
	}
	if !validPlayerClientsRe.MatchString(trimmed) {
		return fmt.Errorf("invalid player clients %q: must contain only alphanumeric characters, commas, underscores, pluses, and hyphens", clients)
	}
	return nil
}

// Options configures the client.
type Options struct {
	// Binary is the yt-dlp executable, either an absolute path or a name that
	// is resolved through PATH.
	Binary string
	// CookieFile is an optional cookies.txt for age restricted content.
	CookieFile string
	// PlayerClients is an optional comma-separated list of player client names
	// to pass to yt-dlp via --extractor-args "youtube:player_client=...".
	PlayerClients string
	// Timeout bounds metadata queries. Downloads use the caller's context.
	Timeout time.Duration
	// FFmpegLocation is passed on when set, so that yt-dlp finds the same
	// ffmpeg the backend uses.
	FFmpegLocation string
	Logger         *slog.Logger
}

// Client runs yt-dlp.
type Client struct {
	binary         string
	cookieFile     string
	playerClients  string
	timeout        time.Duration
	ffmpegLocation string
	logger         *slog.Logger
}

// New builds a client. An empty binary name falls back to "yt-dlp".
func New(opts Options) *Client {
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		binary = "yt-dlp"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var playerClients string
	if trimmed := strings.TrimSpace(opts.PlayerClients); trimmed != "" {
		if err := ValidatePlayerClients(trimmed); err == nil {
			playerClients = trimmed
		} else {
			logger.Warn("invalid player clients configuration rejected",
				"player_clients", opts.PlayerClients,
				"error", err.Error())
		}
	}
	return &Client{
		binary:         binary,
		cookieFile:     opts.CookieFile,
		playerClients:  playerClients,
		timeout:        timeout,
		ffmpegLocation: opts.FFmpegLocation,
		logger:         logger,
	}
}

// Binary returns the configured executable.
func (c *Client) Binary() string { return c.binary }

// CookieFile returns the currently configured cookie file path.
func (c *Client) CookieFile() string { return c.cookieFile }

// WithCookieFile returns a shallow copy of Client configured to use the specified cookie file.
// The original Client remains unmodified, ensuring thread-safety for concurrent multi-worker use.
func (c *Client) WithCookieFile(cookieFile string) *Client {
	clone := *c
	clone.cookieFile = strings.TrimSpace(cookieFile)
	return &clone
}

// Available reports whether the binary can be executed.
func (c *Client) Available(ctx context.Context) error {
	if _, err := c.Version(ctx); err != nil {
		return err
	}
	return nil
}

// Version returns the yt-dlp version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	out, err := c.run(ctx, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// baseArgs are the flags every invocation shares.
func (c *Client) baseArgs() []string {
	args := []string{
		"--ignore-config", // never pick up a user configuration file
		"--no-colors",
		"--no-warnings",
		"--socket-timeout", "30",
	}
	if c.cookieFile != "" {
		args = append(args, "--cookies", c.cookieFile)
	}
	if c.playerClients != "" {
		args = append(args, "--extractor-args", fmt.Sprintf("youtube:player_client=%s", c.playerClients))
	}
	if c.ffmpegLocation != "" {
		args = append(args, "--ffmpeg-location", c.ffmpegLocation)
	}
	return args
}

// Query runs yt-dlp in metadata only mode and decodes the newline delimited
// JSON it prints. target is a URL or a search expression such as
// "ytsearch10:artist title".
//
// --dump-json implies --simulate, so the query never writes a file and needs
// no further flags to keep it from doing so.
func (c *Client) Query(ctx context.Context, target string, extra ...string) ([]Info, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := append(c.baseArgs(), "--dump-json")
	args = append(args, extra...)
	args = append(args, "--", target)

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return decodeInfoLines(out)
}

// ChannelID resolves a YouTube channel address to its canonical UC id. It is
// what turns a handle URL such as https://www.youtube.com/@artist — which
// carries no id at all — into something the providers can look up.
//
// --flat-playlist keeps yt-dlp from walking the channel's videos, and
// --playlist-items 1 bounds the work further; only the channel object itself
// is of interest. The answer is a single JSON document rather than the newline
// delimited stream Query decodes, which is why this does not go through it.
func (c *Client) ChannelID(ctx context.Context, target string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := append(c.baseArgs(),
		"--dump-single-json",
		"--flat-playlist",
		"--playlist-items", "1",
		"--", target,
	)

	out, err := c.run(ctx, args...)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeProviderUnavailable,
			"The YouTube channel could not be resolved.", err)
	}

	var info Info
	if err := json.Unmarshal(out, &info); err != nil {
		return "", apperr.Wrap(apperr.CodeProviderUnavailable,
			"The answer for the YouTube channel could not be read.", err)
	}
	if info.ChannelID == "" {
		return "", apperr.New(apperr.CodeArtistNotFound,
			"This YouTube address does not point at a channel.")
	}
	return info.ChannelID, nil
}

// Search returns the flat search results for a query on the given search
// prefix, for example "ytsearch" or "ytmsearch".
func (c *Client) Search(ctx context.Context, prefix, query string, limit int) ([]Info, error) {
	if limit <= 0 {
		limit = 10
	}
	target := fmt.Sprintf("%s%d:%s", prefix, limit, query)
	return c.Query(ctx, target, "--flat-playlist")
}

// Progress is one download progress report.
type Progress struct {
	DownloadedBytes int64
	TotalBytes      int64
	Percent         float64
	SpeedBytesPerS  float64
	ETASeconds      int
}

// ProgressFunc receives progress updates during a download.
type ProgressFunc func(Progress)

// DownloadRequest describes a single download.
type DownloadRequest struct {
	// URL is the page to download from.
	URL string
	// Dir is an empty directory the file is written into.
	Dir string
	// FormatSelector is passed to -f.
	FormatSelector string
	// Retries bounds yt-dlp's own retry attempts.
	Retries int
	// RateLimit optionally limits download rate (e.g. "2M", "5M", "10M").
	RateLimit string
	// CookieFile optionally overrides the cookie file for this invocation.
	CookieFile string
}

// Download fetches the audio stream and returns the path of the written file.
// The process is bound to ctx: cancelling it terminates yt-dlp, which is how
// job cancellation reaches the running download.
func (c *Client) Download(ctx context.Context, req DownloadRequest, onProgress ProgressFunc) (string, error) {
	if strings.TrimSpace(req.URL) == "" {
		return "", apperr.New(apperr.CodeInvalidRequest, "A download needs a source URL.")
	}
	if strings.TrimSpace(req.Dir) == "" {
		return "", apperr.New(apperr.CodeInternal, "A download needs a target directory.")
	}
	if err := os.MkdirAll(req.Dir, 0o755); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The download directory could not be created.", err)
	}

	selector := req.FormatSelector
	if selector == "" {
		selector = DefaultFormatSelector
	}
	retries := req.Retries
	if retries <= 0 {
		retries = 3
	}

	client := c
	if trimmed := strings.TrimSpace(req.CookieFile); trimmed != "" {
		client = c.WithCookieFile(trimmed)
	}

	args := append(client.baseArgs(), downloadArgs(selector, retries, req.Dir, req.RateLimit)...)
	args = append(args, "--", req.URL)

	cmd := client.command(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "The yt-dlp output could not be captured.", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", startError(client.binary, err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, progressMarker) {
				continue
			}
			if onProgress == nil {
				continue
			}
			if p, ok := parseProgress(strings.TrimPrefix(line, progressMarker)); ok {
				onProgress(p)
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", apperr.Wrap(apperr.CodeJobCancelled, "The download was cancelled.", ctxErr)
		}
		return "", classifyDownloadError(stderr.String(), waitErr)
	}

	path, err := singleFileIn(req.Dir)
	if err != nil {
		return "", err
	}
	return path, nil
}

func classifyDownloadError(stderr string, cause error) error {
	classified := ClassifyError(stderr, cause)
	// If ClassifyError mapped to generic ProviderUnavailable without specific network failure indicators,
	// preserve CodeDownloadFailed for generic download failures.
	if apperr.CodeOf(classified) == apperr.CodeProviderUnavailable {
		lower := strings.ToLower(stderr)
		if !strings.Contains(lower, "timed out") &&
			!strings.Contains(lower, "connection reset") &&
			!strings.Contains(lower, "temporary failure in name resolution") &&
			!strings.Contains(lower, "network is unreachable") &&
			!strings.Contains(lower, "connection refused") {
			return apperr.Wrapf(apperr.CodeDownloadFailed, cause, "yt-dlp failed: %s", firstLine(stderr))
		}
	}
	return classified
}

// downloadArgs are the flags a download adds to the shared base arguments.
// They are built here rather than inline so that a test can check the exact
// vector the backend passes to yt-dlp.
func downloadArgs(selector string, retries int, dir string, rateLimit string) []string {
	args := []string{
		"--no-playlist",
		"--no-simulate",
		"--no-mtime",
		"--newline",
		"--progress",
		"--progress-template", progressTemplate,
		"--retries", strconv.Itoa(retries),
		"--fragment-retries", strconv.Itoa(retries),
		"-f", selector,
		"-o", filepath.Join(dir, "source.%(ext)s"),
	}
	if trimmed := strings.TrimSpace(rateLimit); trimmed != "" {
		args = append(args, "--limit-rate", trimmed)
	}
	return args
}

// command builds the exec.Cmd. Arguments are passed individually; no shell is
// involved at any point, so provider supplied text can never be interpreted as
// a command.
func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.binary, args...)
	// yt-dlp may spawn ffmpeg. Keeping the complete process tree in its own
	// group lets cancellation terminate every child instead of orphaning it.
	configureProcessGroup(cmd)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// run executes yt-dlp and returns its standard output.
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := c.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, startError(c.binary, err)
	}
	if err := cmd.Wait(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The yt-dlp query timed out.", ctxErr)
		}
		return nil, classifyError(stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

func startError(binary string, err error) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return apperr.Wrapf(apperr.CodeToolUnavailable, err,
			"yt-dlp was not found at %q. Install it or set the yt-dlp path in the configuration.", binary)
	}
	return apperr.Wrap(apperr.CodeToolUnavailable, "yt-dlp could not be started.", err)
}

// ClassifyError maps yt-dlp's stderr onto an application error code following
// the refined error taxonomy: candidate-specific, session-specific, or provider-systemic.
func ClassifyError(stderr string, cause error) error {
	lower := strings.ToLower(stderr)
	message := firstLine(stderr)
	switch {
	// 1. Session throttle vs provider rate limit
	case strings.Contains(lower, "session has been rate-limited") ||
		strings.Contains(lower, "session rate-limited") ||
		strings.Contains(lower, "session rate limited"):
		return apperr.Wrapf(apperr.CodeSessionRateLimited, cause,
			"The media session was rate limited: %s", message)

	case strings.Contains(lower, "http error 429") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate-limit") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "rate-limited") ||
		strings.Contains(lower, "rate limited") ||
		strings.Contains(lower, "try again later") ||
		strings.Contains(lower, "exceeding the rate limit"):
		return apperr.Wrapf(apperr.CodeProviderRateLimited, cause,
			"The media provider rate limited the request: %s", message)

	// 2. Authentication and bot challenges (session-specific)
	case strings.Contains(lower, "not a bot") ||
		strings.Contains(lower, "bot verification") ||
		strings.Contains(lower, "bot challenge"):
		return apperr.Wrapf(apperr.CodeSessionBotChallenge, cause,
			"The media session encountered a bot challenge: %s", message)

	case strings.Contains(lower, "sign in to confirm") ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "cookies are expired") ||
		strings.Contains(lower, "cookie has expired") ||
		strings.Contains(lower, "invalid cookies"):
		return apperr.Wrapf(apperr.CodeSessionAuthFailed, cause,
			"The media session requires authentication: %s", message)

	// 3. Transient network failures
	case strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "temporary failure in name resolution") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "connection refused"):
		return apperr.Wrapf(apperr.CodeProviderUnavailable, cause,
			"Network error contacting media provider: %s", message)

	// 4. Candidate-specific no usable format
	case strings.Contains(lower, "requested format is not available") ||
		strings.Contains(lower, "no suitable format"):
		return apperr.Wrapf(apperr.CodeTrackNotFound, cause, "No usable audio format for candidate: %s", message)

	// 5. Candidate-specific permanent unavailable errors
	case strings.Contains(lower, "video unavailable") ||
		strings.Contains(lower, "is not available") ||
		strings.Contains(lower, "private video") ||
		strings.Contains(lower, "removed by the uploader") ||
		strings.Contains(lower, "this video has been removed") ||
		strings.Contains(lower, "account associated with this video has been terminated"):
		return apperr.Wrapf(apperr.CodeTrackNotFound, cause, "The media item is unavailable: %s", message)

	// 6. Unsupported URL
	case strings.Contains(lower, "unsupported url"):
		return apperr.Wrapf(apperr.CodeInvalidRequest, cause, "The URL is not supported: %s", message)

	default:
		return apperr.Wrapf(apperr.CodeProviderUnavailable, cause, "yt-dlp failed: %s", message)
	}
}

func classifyError(stderr string, cause error) error {
	return ClassifyError(stderr, cause)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no error output"
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	const maxLen = 500
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

// decodeInfoLines parses the newline delimited JSON yt-dlp prints.
func decodeInfoLines(raw []byte) ([]Info, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 256*1024), 32*1024*1024)

	out := make([]Info, 0, 8)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var info Info
		if err := json.Unmarshal(line, &info); err != nil {
			return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The yt-dlp output could not be decoded.", err)
		}
		out = append(out, info)
	}
	if err := scanner.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeProviderUnavailable, "The yt-dlp output could not be read.", err)
	}
	return out, nil
}

// parseProgress decodes one progress line.
func parseProgress(line string) (Progress, bool) {
	fields := strings.Split(strings.TrimSpace(line), "|")
	if len(fields) < 5 {
		return Progress{}, false
	}
	p := Progress{
		DownloadedBytes: parseInt(fields[0]),
		TotalBytes:      parseInt(fields[1]),
		SpeedBytesPerS:  parseFloat(fields[3]),
		ETASeconds:      int(parseInt(fields[4])),
	}
	if p.TotalBytes <= 0 {
		p.TotalBytes = parseInt(fields[2])
	}
	if p.TotalBytes > 0 {
		p.Percent = float64(p.DownloadedBytes) / float64(p.TotalBytes) * 100
		if p.Percent > 100 {
			p.Percent = 100
		}
	}
	return p, true
}

func parseInt(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" || s == "None" {
		return 0
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return 0
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" || s == "None" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// singleFileIn returns the one regular media output file inside dir.
func singleFileIn(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", apperr.Wrap(apperr.CodeDownloadFailed, "The download directory could not be read.", err)
	}
	var found []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") ||
			name == "meta.json" || strings.HasPrefix(name, "cover.") {
			continue
		}
		found = append(found, filepath.Join(dir, name))
	}
	switch len(found) {
	case 0:
		return "", apperr.New(apperr.CodeDownloadFailed, "yt-dlp produced no output file.")
	case 1:
		return found[0], nil
	default:
		// If multiple files, prefer source.* or audio file
		for _, f := range found {
			base := filepath.Base(f)
			if strings.HasPrefix(base, "source.") {
				return f, nil
			}
		}
		return found[0], nil
	}
}
