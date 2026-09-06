// Package handlers implements the HTTP endpoints. Handlers parse and validate
// the request, delegate to a service and render the answer; the business logic
// itself lives in the services below them.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/resolve"
	"ytdm/backend/internal/settings"
	"ytdm/backend/internal/storage"
	"ytdm/backend/internal/subscriptions"
	"ytdm/backend/internal/update"
)

// Checker reports whether an external dependency is usable.
type Checker interface {
	Available(ctx context.Context) error
}

// Pinger reports whether the database answers.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Deps are the services the handlers use.
type Deps struct {
	Discography    *discography.Service
	Registry       *provider.Registry
	Jobs           *jobs.Manager
	Subscriptions  *subscriptions.Service
	Catalog        *repository.Catalog
	Files          *repository.Files
	Settings       *settings.Service
	Library        *storage.Library
	LibraryService *library.Service
	Resolver       *resolve.Service
	Auth           *auth.Service
	Database       Pinger
	Updates        *update.Service
	MediaSessions  *mediasession.Service

	// Tools are the external programs shown by the health endpoint.
	Tools map[string]Checker

	Version      string
	StartedAt    time.Time
	CookieSecure bool
	Logger       *slog.Logger
}

// Handlers bundles the endpoint implementations.
type Handlers struct {
	deps Deps

	healthMu     sync.Mutex
	healthCache  map[string]checkResult
	healthCached time.Time
}

// New builds the handler set.
func New(deps Deps) (*Handlers, error) {
	switch {
	case deps.Discography == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need a discography service.")
	case deps.Registry == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need a provider registry.")
	case deps.Jobs == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need a job manager.")
	case deps.Catalog == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need a catalogue repository.")
	case deps.Settings == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need a settings service.")
	case deps.Auth == nil:
		return nil, apperr.New(apperr.CodeInternal, "The handlers need an auth service.")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.StartedAt.IsZero() {
		deps.StartedAt = time.Now()
	}
	if deps.Updates == nil {
		deps.Updates = update.NewService(update.Config{Enabled: false}, deps.Version, nil, deps.Logger)
	}
	return &Handlers{deps: deps, healthCache: make(map[string]checkResult)}, nil
}

// NewForTest constructs a Handlers instance for testing without strict dependency checks.
func NewForTest(deps Deps) *Handlers {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.StartedAt.IsZero() {
		deps.StartedAt = time.Now()
	}
	if deps.Updates == nil {
		deps.Updates = update.NewService(update.Config{Enabled: false}, deps.Version, nil, deps.Logger)
	}
	return &Handlers{deps: deps, healthCache: make(map[string]checkResult)}
}

// decodeJSON reads a JSON request body, refusing unknown fields so that a typo
// in a client is reported instead of silently ignored.
func decodeJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return apperr.New(apperr.CodeInvalidRequest, "A request body is required.")
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		media := strings.TrimSpace(strings.Split(contentType, ";")[0])
		if !strings.EqualFold(media, "application/json") {
			return apperr.Newf(apperr.CodeUnsupportedMediaType,
				"The content type %q is not supported; use application/json.", media)
		}
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		var maxBytes *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return apperr.New(apperr.CodeInvalidRequest, "The request body is empty.")
		case errors.As(err, &maxBytes):
			return apperr.New(apperr.CodeInvalidRequest, "The request body is too large.")
		default:
			return apperr.Wrapf(apperr.CodeInvalidRequest, err,
				"The request body could not be read: %s", err.Error())
		}
	}
	if decoder.More() {
		return apperr.New(apperr.CodeInvalidRequest, "The request body contains more than one JSON document.")
	}
	return nil
}

// queryString returns a trimmed query parameter.
func queryString(r *http.Request, name string) string {
	return strings.TrimSpace(r.URL.Query().Get(name))
}

// pagination reads the limit and offset parameters.
func pagination(r *http.Request, defaultLimit, maxLimit int) (int, int, error) {
	limit := defaultLimit
	if raw := queryString(r, "limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, apperr.Newf(apperr.CodeInvalidRequest, "limit must be a positive number, got %q.", raw)
		}
		limit = min(parsed, maxLimit)
	}

	offset := 0
	if raw := queryString(r, "offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return 0, 0, apperr.Newf(apperr.CodeInvalidRequest, "offset must not be negative, got %q.", raw)
		}
		offset = parsed
	}
	return limit, offset, nil
}

// releaseFilterFromQuery reads the release type selection. When no flag is
// given at all, the default selection applies.
func releaseFilterFromQuery(r *http.Request) (music.ReleaseFilter, error) {
	query := r.URL.Query()
	filter := music.ReleaseFilter{}

	fields := []struct {
		name  string
		field *bool
	}{
		{"albums", &filter.Albums},
		{"singles", &filter.Singles},
		{"eps", &filter.EPs},
		{"live", &filter.Live},
		{"compilations", &filter.Compilations},
		{"remixes", &filter.Remixes},
	}

	var any bool
	for _, field := range fields {
		raw := strings.TrimSpace(query.Get(field.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return filter, apperr.Newf(apperr.CodeInvalidRequest,
				"%s must be true or false, got %q.", field.name, raw)
		}
		*field.field = value
		any = true
	}
	if !any {
		return music.DefaultReleaseFilter(), nil
	}
	return filter, nil
}

// Health reports whether the server and its dependencies work.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	// Container liveness only needs the in-process application and the database
	// connection. Tool diagnostics stay available in the default detailed
	// response but are deliberately skipped for the fast, deterministic
	// healthcheck scope; no external provider is ever contacted.
	includeTools := !strings.EqualFold(queryString(r, "scope"), "essential")
	checks := h.runChecks(r.Context(), includeTools)

	status := "ok"
	httpStatus := http.StatusOK
	for name, result := range checks {
		if result.OK {
			continue
		}
		if name == "database" {
			status = "unavailable"
			httpStatus = http.StatusServiceUnavailable
			break
		}
		status = "degraded"
	}

	response.JSON(w, httpStatus, map[string]any{"data": map[string]any{
		"status":         status,
		"version":        h.deps.Version,
		"uptime_seconds": int(time.Since(h.deps.StartedAt).Seconds()),
		"checks":         checks,
	}})
}

// checkResult is one health check outcome.
type checkResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// healthCacheTTL bounds how often the external programs are probed. Starting a
// process on every health request would be wasteful for a frequently polled
// endpoint.
const healthCacheTTL = 30 * time.Second

// runChecks probes the dependencies, reusing recent results for the external
// programs.
func (h *Handlers) runChecks(ctx context.Context, includeTools bool) map[string]checkResult {
	checks := make(map[string]checkResult, len(h.deps.Tools)+2)
	checks["application"] = checkResult{OK: true}

	if h.deps.Database != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := h.deps.Database.PingContext(pingCtx)
		cancel()
		checks["database"] = resultOf(err)
	}
	if !includeTools {
		return checks
	}

	h.healthMu.Lock()
	fresh := time.Since(h.healthCached) < healthCacheTTL && len(h.healthCache) > 0
	if fresh {
		for name, result := range h.healthCache {
			checks[name] = result
		}
		h.healthMu.Unlock()
		return checks
	}
	h.healthMu.Unlock()

	tools := make(map[string]checkResult, len(h.deps.Tools))
	for name, checker := range h.deps.Tools {
		toolCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		tools[name] = resultOf(checker.Available(toolCtx))
		cancel()
	}

	h.healthMu.Lock()
	h.healthCache = tools
	h.healthCached = time.Now()
	h.healthMu.Unlock()

	for name, result := range tools {
		checks[name] = result
	}
	return checks
}

func resultOf(err error) checkResult {
	if err != nil {
		return checkResult{OK: false, Detail: apperr.MessageOf(err)}
	}
	return checkResult{OK: true}
}

// Providers lists the configured providers and their availability.
func (h *Handlers) Providers(w http.ResponseWriter, r *http.Request) {
	infos := h.deps.Registry.List(r.Context())
	response.List(w, infos, response.Meta{Count: len(infos)})
}
