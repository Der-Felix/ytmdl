package mediasession

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// SessionView is the sanitized public/admin DTO for a media session.
// Sensitive fields such as CookieRef, raw cookie contents, filesystem paths,
// auth headers, and stderr are NEVER exposed.
type SessionView struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	ProviderFamily    provider.Family `json:"provider_family"`
	Enabled           bool            `json:"enabled"`
	HealthStatus      HealthStatus    `json:"health_status"`
	HasCredentials    bool            `json:"has_credentials"`
	InUse             bool            `json:"in_use"`
	CooldownUntil     *time.Time      `json:"cooldown_until,omitempty"`
	LastUsedAt        *time.Time      `json:"last_used_at,omitempty"`
	LastSuccessAt     *time.Time      `json:"last_success_at,omitempty"`
	LastFailureAt     *time.Time      `json:"last_failure_at,omitempty"`
	LastFailureReason string          `json:"last_failure_reason,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// CreateSessionRequest encapsulates fields accepted when creating a new session.
type CreateSessionRequest struct {
	Name           string          `json:"name"`
	ProviderFamily provider.Family `json:"provider_family"`
	Enabled        *bool           `json:"enabled"`
}

// UpdateSessionRequest encapsulates mutable administrative fields.
type UpdateSessionRequest struct {
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
}

// Repository defines database operations required by the media session service.
type Repository interface {
	ListSessions(ctx context.Context, filter Filter) ([]Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	CreateSession(ctx context.Context, s *Session) error
	UpdateSessionMetadata(ctx context.Context, id string, name string, enabled bool) (*Session, error)
	UpdateCookieRef(ctx context.Context, id string, cookieRef string) (*Session, error)
	UpdateHealth(ctx context.Context, id string, params HealthUpdate) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
}

// ServiceOptions bundles dependencies for the media session administration service.
type ServiceOptions struct {
	Repo          Repository
	Storage       *CookieStorage
	Pool          *SessionPool
	LegacyAdapter *LegacyAdapter
	Prober        Prober
	Logger        *slog.Logger
}

// Service coordinates administrative operations on media sessions.
type Service struct {
	repo          Repository
	storage       *CookieStorage
	pool          *SessionPool
	legacyAdapter *LegacyAdapter
	prober        Prober
	logger        *slog.Logger

	probeMu   sync.Mutex
	lastProbe map[string]time.Time
}

// NewService creates a new media session administrative service.
func NewService(opts ServiceOptions) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:          opts.Repo,
		storage:       opts.Storage,
		pool:          opts.Pool,
		legacyAdapter: opts.LegacyAdapter,
		prober:        opts.Prober,
		logger:        logger,
		lastProbe:     make(map[string]time.Time),
	}
}

// ListSessions returns a deterministically ordered list of all media sessions.
// Ordering: enabled first, then case-insensitive name ascending, then ID ascending.
func (s *Service) ListSessions(ctx context.Context) ([]SessionView, error) {
	var views []SessionView

	if s.repo != nil {
		sessions, err := s.repo.ListSessions(ctx, Filter{})
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			views = append(views, s.sessionToView(&sess))
		}
	}

	if s.legacyAdapter != nil && s.legacyAdapter.IsConfigured() {
		views = append(views, s.legacyToView())
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].Enabled != views[j].Enabled {
			return views[i].Enabled
		}
		nameI := strings.ToLower(views[i].Name)
		nameJ := strings.ToLower(views[j].Name)
		if nameI != nameJ {
			return nameI < nameJ
		}
		return views[i].ID < views[j].ID
	})

	return views, nil
}

// GetSession retrieves a single session view by ID.
func (s *Service) GetSession(ctx context.Context, id string) (*SessionView, error) {
	if id == LegacySessionID {
		if s.legacyAdapter == nil || !s.legacyAdapter.IsConfigured() {
			return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
		}
		view := s.legacyToView()
		return &view, nil
	}

	if s.repo == nil {
		return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
	}

	sess, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.sessionToView(sess)
	return &view, nil
}

// CreateSession creates metadata for a new media session.
// Initial health is UNKNOWN and has_credentials is false until credentials are provided.
func (s *Service) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Session name must not be empty.")
	}

	family := req.ProviderFamily
	if strings.TrimSpace(string(family)) == "" {
		family = provider.FamilyYouTube
	}
	if family != provider.FamilyYouTube {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Invalid provider family.")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	sess := &Session{
		Name:           name,
		ProviderFamily: family,
		Enabled:        enabled,
		HealthStatus:   HealthUnknown,
		CookieRef:      "",
	}

	if s.repo == nil {
		return nil, apperr.New(apperr.CodeInternal, "Database repository not configured.")
	}

	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	if s.pool != nil {
		s.pool.UpsertSession(sess)
	}

	s.logger.Info("media session created",
		"session_id", sess.ID,
		"name", sess.Name,
		"provider_family", sess.ProviderFamily,
		"enabled", sess.Enabled,
	)

	view := s.sessionToView(sess)
	return &view, nil
}

// UploadCookies installs or replaces Netscape cookies for a session.
// Returns 409 Conflict if the session is currently in active data-plane use.
// On replacement, validates candidate with probe before promoting; previous credentials remain intact on failure.
func (s *Service) UploadCookies(ctx context.Context, id string, content []byte) (*SessionView, *ProbeResult, error) {
	if id == LegacySessionID {
		return nil, nil, apperr.New(apperr.CodeInvalidRequest, "Legacy session cookies are managed externally via environment configuration and cannot be modified through the admin API.")
	}

	if s.repo == nil {
		return nil, nil, apperr.New(apperr.CodeInternal, "Database repository not configured.")
	}

	sess, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	// In-use guard: reject mutation while session is actively performing downloads
	if s.pool != nil && s.pool.IsInUse(id) {
		return nil, nil, apperr.New(apperr.CodeSessionInUse, "Media session is currently in use by an active download operation.")
	}

	// Netscape structural validation: bounds, record structure, no NUL bytes, no secret echoing
	if err := ValidateNetscapeCookies(content); err != nil {
		return nil, nil, err
	}

	if s.storage == nil {
		return nil, nil, apperr.New(apperr.CodeStorageUnavailable, "Cookie storage not configured.")
	}

	hasExistingCredentials := s.storage.HasCookie(sess.CookieRef)

	if !hasExistingCredentials {
		// INITIAL UPLOAD: atomic store without assuming HEALTHY
		cookieRef, err := s.storage.Store(sess.ID, content)
		if err != nil {
			return nil, nil, err
		}

		updated, err := s.repo.UpdateCookieRef(ctx, sess.ID, cookieRef)
		if err != nil {
			// Compensation: delete stored file to prevent DB/file inconsistency
			_ = s.storage.Delete(cookieRef)
			return nil, nil, err
		}

		if s.pool != nil {
			s.pool.UpsertSession(updated)
		}

		s.logger.Info("media session cookies installed", "session_id", sess.ID)
		view := s.sessionToView(updated)
		return &view, nil, nil
	}

	// REPLACEMENT: isolate candidate in temporary file, run probe, promote only if healthy
	candidatePath, cleanup, err := s.storage.SaveCandidate(sess.ID, content)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	var probeRes *ProbeResult
	if s.prober != nil {
		var probeErr error
		probeRes, probeErr = s.prober.Probe(ctx, sess.ID, candidatePath)
		if probeErr != nil || probeRes == nil || probeRes.Status != HealthHealthy {
			// Old cookie file remains completely untouched!
			return nil, probeRes, apperr.Wrap(apperr.CodeSessionAuthFailed, "Replacement cookie credentials failed verification probe; previous credentials preserved.", probeErr)
		}
	}

	// Candidate probe succeeded: promote candidate atomically over old file
	cookieRef, err := s.storage.PromoteCandidate(sess.ID, candidatePath)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	healthUpdate := HealthUpdate{
		HealthStatus:        HealthHealthy,
		ConsecutiveFailures: 0,
		LastSuccessAt:       &now,
		CooldownUntil:       nil,
	}

	updated, err := s.repo.UpdateHealth(ctx, sess.ID, healthUpdate)
	if err != nil {
		return nil, nil, err
	}

	if updated.CookieRef != cookieRef {
		updated, _ = s.repo.UpdateCookieRef(ctx, sess.ID, cookieRef)
	}

	if s.pool != nil {
		s.pool.UpsertSession(updated)
	}

	s.logger.Info("media session cookies replaced", "session_id", sess.ID)
	view := s.sessionToView(updated)
	return &view, probeRes, nil
}

// ProbeSession executes an explicit administrator probe against a session.
// Enforces global and session token bucket rate limits, plus endpoint debounce protection.
func (s *Service) ProbeSession(ctx context.Context, id string) (*ProbeResult, *SessionView, error) {
	var cookiePath string
	var sessID string

	if id == LegacySessionID {
		if s.legacyAdapter == nil || !s.legacyAdapter.IsConfigured() {
			return nil, nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found or unconfigured.", id)
		}
		cookiePath = s.legacyAdapter.CookiePath()
		sessID = LegacySessionID
	} else {
		if s.repo == nil {
			return nil, nil, apperr.New(apperr.CodeInternal, "Database repository not configured.")
		}
		sess, err := s.repo.GetSession(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if s.storage == nil || !s.storage.HasCookie(sess.CookieRef) {
			return nil, nil, apperr.New(apperr.CodeInvalidRequest, "Cannot probe session without credentials.")
		}
		resolved, err := s.storage.ResolvePath(sess.CookieRef)
		if err != nil {
			return nil, nil, err
		}
		cookiePath = resolved
		sessID = sess.ID
	}

	// Debounce check: prevent rapid repeated button clicks per session
	s.probeMu.Lock()
	last, ok := s.lastProbe[sessID]
	if ok && time.Since(last) < 2*time.Second {
		s.probeMu.Unlock()
		return nil, nil, apperr.New(apperr.CodeRateLimited, "A probe for this session was recently executed. Please wait a moment before trying again.")
	}
	s.lastProbe[sessID] = time.Now()
	s.probeMu.Unlock()

	// Rate limiting: participate in global ceiling limiter and per-session limiter
	if s.pool != nil {
		if gl := s.pool.GlobalLimiter(); gl != nil {
			if err := gl.Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
		if rs := s.pool.GetSession(sessID); rs != nil && rs.Limiter() != nil {
			if err := rs.Limiter().Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
	}

	if s.prober == nil {
		return nil, nil, apperr.New(apperr.CodeToolUnavailable, "Session prober is not configured.")
	}

	res, probeErr := s.prober.Probe(ctx, sessID, cookiePath)
	now := time.Now().UTC()

	// Record outcome in runtime pool and update database
	if s.pool != nil {
		if probeErr == nil && res != nil && res.Status == HealthHealthy {
			s.pool.RecordSuccess(ctx, sessID, now)
		} else if probeErr != nil {
			s.pool.RecordFailure(ctx, sessID, probeErr, now)
		}
	}

	if s.repo != nil && sessID != LegacySessionID {
		var update HealthUpdate
		if probeErr == nil && res != nil && res.Status == HealthHealthy {
			update = HealthUpdate{
				HealthStatus:        HealthHealthy,
				ConsecutiveFailures: 0,
				LastSuccessAt:       &now,
				LastUsedAt:          &now,
			}
		} else if res != nil {
			update = HealthUpdate{
				HealthStatus:        res.Status,
				ConsecutiveFailures: 1,
				LastFailureAt:       &now,
				LastFailureReason:   res.FailureCategory,
				CooldownUntil:       res.CooldownUntil,
				LastUsedAt:          &now,
			}
		}
		_, _ = s.repo.UpdateHealth(ctx, sessID, update)
	}

	s.logger.Info("media session probe executed",
		"session_id", sessID,
		"status", res.Status,
		"metadata_ok", res.MetadataOK,
		"usable_audio_formats", res.UsableAudioFormats,
	)

	// Fetch updated session view
	view, _ := s.GetSession(ctx, id)
	return res, view, nil
}

// UpdateSession modifies metadata (name, enabled) for a session.
// Changing enabled immediately excludes the session from new leases without aborting active in-flight operations.
func (s *Service) UpdateSession(ctx context.Context, id string, req UpdateSessionRequest) (*SessionView, error) {
	if id == LegacySessionID {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Legacy session metadata cannot be modified.")
	}

	if s.repo == nil {
		return nil, apperr.New(apperr.CodeInternal, "Database repository not configured.")
	}

	sess, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}

	name := sess.Name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		name = strings.TrimSpace(*req.Name)
	}

	enabled := sess.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	updated, err := s.repo.UpdateSessionMetadata(ctx, id, name, enabled)
	if err != nil {
		return nil, err
	}

	if s.pool != nil {
		s.pool.UpsertSession(updated)
	}

	s.logger.Info("media session metadata updated",
		"session_id", id,
		"name", updated.Name,
		"enabled", updated.Enabled,
	)

	view := s.sessionToView(updated)
	return &view, nil
}

// DeleteSession safely deletes a media session.
// Rejects deletion with 409 Conflict if the session is currently in active data-plane use.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	if id == LegacySessionID {
		return apperr.New(apperr.CodeInvalidRequest, "Legacy session cannot be deleted through the admin API.")
	}

	if s.repo == nil {
		return apperr.New(apperr.CodeInternal, "Database repository not configured.")
	}

	sess, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return err
	}

	// In-use guard: reject deletion while session is actively performing downloads
	if s.pool != nil && s.pool.IsInUse(id) {
		return apperr.New(apperr.CodeSessionInUse, "Media session is currently in use by an active download operation.")
	}

	// Delete from DB first
	if err := s.repo.DeleteSession(ctx, id); err != nil {
		return err
	}

	// Remove from runtime pool
	if s.pool != nil {
		s.pool.RemoveSession(id)
	}

	// Delete backing cookie file if present
	if s.storage != nil && sess.CookieRef != "" {
		_ = s.storage.Delete(sess.CookieRef)
	}

	s.logger.Info("media session deleted", "session_id", id)
	return nil
}

func (s *Service) sessionToView(sess *Session) SessionView {
	hasCreds := false
	if s.storage != nil && sess.CookieRef != "" {
		hasCreds = s.storage.HasCookie(sess.CookieRef)
	}

	inUse := false
	if s.pool != nil {
		inUse = s.pool.IsInUse(sess.ID)
	}

	return SessionView{
		ID:                sess.ID,
		Name:              sess.Name,
		ProviderFamily:    sess.ProviderFamily,
		Enabled:           sess.Enabled,
		HealthStatus:      sess.HealthStatus,
		HasCredentials:    hasCreds,
		InUse:             inUse,
		CooldownUntil:     sess.CooldownUntil,
		LastUsedAt:        sess.LastUsedAt,
		LastSuccessAt:     sess.LastSuccessAt,
		LastFailureAt:     sess.LastFailureAt,
		LastFailureReason: sanitizeFailureReasonStr(sess.LastFailureReason),
		CreatedAt:         sess.CreatedAt,
		UpdatedAt:         sess.UpdatedAt,
	}
}

func (s *Service) legacyToView() SessionView {
	hasCreds := false
	if s.legacyAdapter != nil {
		hasCreds = s.legacyAdapter.IsConfigured()
	}

	inUse := false
	health := HealthUnknown
	if s.pool != nil {
		inUse = s.pool.IsInUse(LegacySessionID)
		if rs := s.pool.GetSession(LegacySessionID); rs != nil {
			health = rs.Session().HealthStatus
		}
	}

	now := time.Now().UTC()
	return SessionView{
		ID:             LegacySessionID,
		Name:           "Legacy Cookie File",
		ProviderFamily: provider.FamilyYouTube,
		Enabled:        true,
		HealthStatus:   health,
		HasCredentials: hasCreds,
		InUse:          inUse,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func sanitizeFailureReasonStr(reason string) string {
	if reason == "" {
		return ""
	}
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate-limit") || strings.Contains(lower, "429"):
		return "Session rate limited by provider"
	case strings.Contains(lower, "bot"):
		return "Session encountered bot challenge"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "cookie"):
		return "Session authentication expired or failed"
	default:
		return "Provider failure"
	}
}
