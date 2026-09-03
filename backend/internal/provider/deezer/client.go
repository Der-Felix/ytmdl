// Package deezer implements the metadata provider for the Deezer public
// catalog API.
package deezer

import (
	"context"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/httpx"
)

const (
	defaultAPIBaseURL = "https://api.deezer.com"
	providerName      = "deezer"
)

// Deezer allows roughly 50 requests per five seconds per address, which is ten
// per second. The default stays below that so that the burst and the odd retry
// still fit inside the real ceiling.
const (
	defaultRequestsPerSecond = 8
	defaultBurst             = 5
	defaultMaxRetries        = 3
	defaultRetryBackoff      = 500 * time.Millisecond
	defaultMaxRetryBackoff   = 8 * time.Second
)

// Config configures the Deezer metadata provider.
type Config struct {
	APIBaseURL string
	HTTPClient *http.Client

	// RequestsPerSecond is the sustained ceiling this client paces itself to.
	// Zero selects the default; a negative value switches pacing off, which
	// only the tests do.
	RequestsPerSecond float64
	// Burst is how many requests may go out back to back before the pacing
	// takes hold. Zero selects the default.
	Burst int

	// MaxRetries bounds how often a transient failure is retried. Zero selects
	// the default; a negative value disables retrying.
	MaxRetries int
	// RetryBackoff is the wait before the first retry, doubling from there.
	// MaxRetryBackoff caps it, and also caps a Retry-After the server sends.
	RetryBackoff    time.Duration
	MaxRetryBackoff time.Duration
}

// client handles HTTP requests to the Deezer API.
//
// It owns the pacing and the retrying, so that every consumer of this provider
// — subscription syncs, artist pages, searches — shares one request budget
// and one retry policy rather than each inventing its own.
type client struct {
	baseURL string
	http    *http.Client
	limiter *limiter

	maxRetries   int
	retryBackoff time.Duration
	maxBackoff   time.Duration
}

func newClient(cfg Config) *client {
	baseURL := strings.TrimRight(cfg.APIBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = httpx.New(0)
	}

	rate := cfg.RequestsPerSecond
	if rate == 0 {
		rate = defaultRequestsPerSecond
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = defaultBurst
	}
	retries := cfg.MaxRetries
	if retries == 0 {
		retries = defaultMaxRetries
	}
	if retries < 0 {
		retries = 0
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}
	maxBackoff := cfg.MaxRetryBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxRetryBackoff
	}
	if maxBackoff < backoff {
		maxBackoff = backoff
	}

	return &client{
		baseURL:      baseURL,
		http:         httpClient,
		limiter:      newLimiter(rate, burst),
		maxRetries:   retries,
		retryBackoff: backoff,
		maxBackoff:   maxBackoff,
	}
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type apiErrorWrapper struct {
	Error *apiError `json:"error"`
}

// attemptError carries what the retry loop needs to decide: whether another
// attempt has a realistic chance, and how long the server asked us to wait.
type attemptError struct {
	err        error
	retryable  bool
	retryAfter time.Duration
}

// get performs a GET request against the given path or absolute URL and
// unmarshals the JSON response, pacing the request and retrying it while the
// failure is transient.
func (c *client) get(ctx context.Context, pathOrURL string, dst any) error {
	return c.fetch(ctx, pathOrURL, dst, true)
}

// probe performs a single unpaced request. It exists for the availability
// check and for nothing else.
//
// A liveness probe has to answer whether Deezer is reachable right now. Paced,
// it would queue behind a catalogue walk and report an unreachable provider at
// exactly the moment Deezer is busy serving us; retried, it would spend
// seconds arriving at an answer whose whole value is being prompt. One request
// against a budget of eight per second is not the traffic that causes a rate
// limit.
func (c *client) probe(ctx context.Context, pathOrURL string, dst any) error {
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}
	if result := c.attempt(ctx, pathOrURL, dst); result != nil {
		return result.err
	}
	return nil
}

// fetch is the request loop behind get.
func (c *client) fetch(ctx context.Context, pathOrURL string, dst any, paced bool) error {
	var last error

	for attempt := 0; ; attempt++ {
		// The pacing applies to every attempt, retries included: a retry that
		// jumped the queue would be exactly the traffic that caused the
		// rate limit in the first place.
		if paced {
			if err := c.limiter.Wait(ctx); err != nil {
				return cancelled(ctx, err)
			}
		}

		result := c.attempt(ctx, pathOrURL, dst)
		if result == nil {
			return nil
		}
		last = result.err

		if !result.retryable || attempt >= c.maxRetries {
			return last
		}
		if err := sleep(ctx, c.backoffFor(attempt, result.retryAfter)); err != nil {
			// The context ended during the backoff. The last provider error is
			// the more useful one to report, but a cancellation has to stay
			// recognisable as such.
			return cancelled(ctx, last)
		}
	}
}

// attempt performs one request and returns nil on success.
func (c *client) attempt(ctx context.Context, pathOrURL string, dst any) *attemptError {
	targetURL := c.resolve(pathOrURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return &attemptError{err: apperr.Wrap(apperr.CodeInternal, "failed to build request", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "YTMDL/0.14.1")

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &attemptError{
				err: apperr.Wrap(apperr.CodeJobCancelled, "Deezer request was cancelled", ctx.Err()),
			}
		}
		// A connection that failed to establish is worth one more try; a
		// hostname that does not resolve simply fails again, cheaply.
		return &attemptError{
			err:       apperr.Wrap(apperr.CodeProviderUnavailable, "Deezer API is unreachable", err),
			retryable: true,
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return &attemptError{
				err: apperr.Wrap(apperr.CodeJobCancelled, "Deezer request was cancelled", ctx.Err()),
			}
		}
		return &attemptError{
			err:       apperr.Wrap(apperr.CodeProviderUnavailable, "failed to read Deezer API response", err),
			retryable: true,
		}
	}

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &attemptError{
			err:        apperr.New(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded"),
			retryable:  true,
			retryAfter: retryAfter,
		}
	case resp.StatusCode >= 500:
		return &attemptError{
			err: apperr.Newf(apperr.CodeProviderUnavailable,
				"Deezer API server error (%d)", resp.StatusCode),
			retryable:  true,
			retryAfter: retryAfter,
		}
	case resp.StatusCode == http.StatusBadRequest:
		return &attemptError{
			err: apperr.Newf(apperr.CodeInvalidRequest, "Deezer rejected request (%d)", resp.StatusCode),
		}
	case resp.StatusCode == http.StatusNotFound:
		return &attemptError{
			err: apperr.New(apperr.CodeProviderNotFound, "Deezer does not know this item."),
		}
	}

	// Deezer commonly returns HTTP 200 with an error payload on failures, and
	// a breached quota arrives that way rather than as a 429. This is the path
	// the live syncs actually hit.
	var errCheck apiErrorWrapper
	if err := json.Unmarshal(body, &errCheck); err == nil && errCheck.Error != nil {
		translated := translateDeezerError(errCheck.Error)
		return &attemptError{
			err:        translated,
			retryable:  retryableCode(errCheck.Error.Code),
			retryAfter: retryAfter,
		}
	}

	if err := json.Unmarshal(body, dst); err != nil {
		// A payload that is not the expected shape will not become one on a
		// second read.
		return &attemptError{
			err: apperr.Wrap(apperr.CodeProviderUnavailable, "Deezer returned invalid JSON payload", err),
		}
	}
	return nil
}

// resolve turns a path or an absolute URL into the URL to request.
func (c *client) resolve(pathOrURL string) string {
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		return pathOrURL
	}
	if !strings.HasPrefix(pathOrURL, "/") {
		pathOrURL = "/" + pathOrURL
	}
	return c.baseURL + pathOrURL
}

// backoffFor returns how long to wait before the next attempt. A Retry-After
// the server sent wins, capped so that one unlucky request cannot park a sync
// for minutes; otherwise the wait doubles per attempt.
func (c *client) backoffFor(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, c.maxBackoff)
	}

	wait := c.retryBackoff << attempt
	if wait <= 0 || wait > c.maxBackoff {
		wait = c.maxBackoff
	}
	// Full jitter over the upper half of the window. Without it, every release
	// that was rate limited in the same burst would come back at the same
	// moment and cause the next one.
	half := wait / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// retryableCode reports whether a Deezer error payload describes a condition
// that a later attempt could get past.
func retryableCode(code int) bool {
	switch code {
	case 4: // QuotaException: the rate limit, and the one that matters here.
		return true
	case 500, 501: // Deezer's own internal errors.
		return true
	default:
		// 800 DataException and the OAuth codes are permanent: the item is not
		// there, or the request is not allowed. Retrying only burns budget.
		return false
	}
}

// parseRetryAfter reads the header in both forms the RFC allows. An
// unparsable value yields zero, which leaves the normal backoff in charge.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
	}
	return 0
}

// cancelled prefers the context's own error once it has one, so that a
// shutdown is never reported as a provider failure.
func cancelled(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if apperr.CodeOf(err) == apperr.CodeJobCancelled {
			return err
		}
		return apperr.Wrap(apperr.CodeJobCancelled, "The Deezer request was cancelled.", ctxErr)
	}
	return err
}

func translateDeezerError(e *apiError) error {
	switch e.Code {
	case 800: // DataException ("no data")
		return apperr.Newf(apperr.CodeProviderNotFound, "Deezer does not know this item (%s).", e.Message)
	case 4: // QuotaException / Rate limit
		return apperr.Newf(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded: %s", e.Message)
	case 200, 300: // OAuthException
		return apperr.Newf(apperr.CodeProviderUnavailable, "Deezer auth error: %s", e.Message)
	case 500, 501:
		return apperr.Newf(apperr.CodeProviderUnavailable, "Deezer internal error: %s", e.Message)
	default:
		return apperr.Newf(apperr.CodeProviderUnavailable, "Deezer API error (%d: %s)", e.Code, e.Message)
	}
}
