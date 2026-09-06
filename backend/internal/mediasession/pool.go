package mediasession

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// SessionRepository abstracts storage persistence for media session health.
type SessionRepository interface {
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, filter Filter) ([]Session, error)
	UpdateHealth(ctx context.Context, id string, update HealthUpdate) (*Session, error)
}

// PoolConfig tunes SessionPool behavior, concurrency, and rate limiting.
type PoolConfig struct {
	Family                provider.Family
	MaxLeasesPerSession   int
	SessionRequestsPerSec float64
	SessionBurst          int
	GlobalRequestsPerSec  float64
	GlobalBurst           int
	AllowUnknown          bool
}

// DefaultPoolConfig returns conservative baseline pool settings.
func DefaultPoolConfig(family provider.Family) PoolConfig {
	return PoolConfig{
		Family:                family,
		MaxLeasesPerSession:   1,   // conservative default: 1 lease per session
		SessionRequestsPerSec: 0.5, // 1 request per 2 seconds
		SessionBurst:          2,
		GlobalRequestsPerSec:  2.0, // 2 requests per second across all sessions
		GlobalBurst:           4,
		AllowUnknown:          true, // allow controlled single probe of unverified sessions
	}
}

// PlatformFailure holds details about a platform-wide, systemic failure event
// (e.g. IP-level rate limits or provider downtime) that affects all sessions.
type PlatformFailure struct {
	OccurredAt    time.Time
	CooldownUntil time.Time
	Err           error
}

// SessionPool manages a pool of authenticated media sessions for a platform family.
// It implements health-aware least-loaded selection with LRU and stable ID tie-breaks,
// dual token bucket pacing (global ceiling -> per-session limiter), and safe leasing.
type SessionPool struct {
	mu sync.Mutex

	family          provider.Family
	cfg             PoolConfig
	storage         *CookieStorage
	repo            SessionRepository
	legacy          *LegacyAdapter
	globalLimiter   *Limiter
	sessions        map[string]*RuntimeSession
	sessionOrder    []string
	waiters         []chan struct{}
	platformFailure PlatformFailure
	now             func() time.Time
	syncPersist     bool
}

// NewSessionPool initializes a SessionPool for the given provider family.
func NewSessionPool(cfg PoolConfig, storage *CookieStorage, repo SessionRepository, legacy *LegacyAdapter) *SessionPool {
	if cfg.MaxLeasesPerSession <= 0 {
		cfg.MaxLeasesPerSession = 1
	}
	if cfg.SessionRequestsPerSec <= 0 {
		cfg.SessionRequestsPerSec = 0.5
	}
	if cfg.SessionBurst <= 0 {
		cfg.SessionBurst = 2
	}
	if cfg.GlobalRequestsPerSec <= 0 {
		cfg.GlobalRequestsPerSec = 2.0
	}
	if cfg.GlobalBurst <= 0 {
		cfg.GlobalBurst = 4
	}

	p := &SessionPool{
		family:        cfg.Family,
		cfg:           cfg,
		storage:       storage,
		repo:          repo,
		legacy:        legacy,
		globalLimiter: NewLimiter(cfg.GlobalRequestsPerSec, cfg.GlobalBurst),
		sessions:      make(map[string]*RuntimeSession),
		now:           time.Now,
	}
	return p
}

// SetNow overrides time.Now for deterministic testing.
func (p *SessionPool) SetNow(fn func() time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = fn
	if p.globalLimiter != nil {
		p.globalLimiter.now = fn
	}
	for _, rs := range p.sessions {
		if rs.limiter != nil {
			rs.limiter.now = fn
		}
	}
}

// SetSyncPersist enables synchronous repository health persistence for testing.
func (p *SessionPool) SetSyncPersist(sync bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncPersist = sync
}

// GlobalLimiter returns the provider family's global ceiling rate limiter.
func (p *SessionPool) GlobalLimiter() *Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.globalLimiter
}

// Sessions returns snapshot copies of all sessions in the pool.
func (p *SessionPool) Sessions() []Session {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]Session, 0, len(p.sessionOrder))
	for _, id := range p.sessionOrder {
		if rs := p.sessions[id]; rs != nil {
			out = append(out, rs.Session())
		}
	}
	return out
}

// RuntimeSessions returns the in-memory runtime session wrappers.
func (p *SessionPool) RuntimeSessions() []*RuntimeSession {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*RuntimeSession, 0, len(p.sessionOrder))
	for _, id := range p.sessionOrder {
		if rs := p.sessions[id]; rs != nil {
			out = append(out, rs)
		}
	}
	return out
}

// ReloadSessions resets runtime lease state to 0 and reloads sessions from metadata.
func (p *SessionPool) ReloadSessions(sessions []Session) {
	p.mu.Lock()
	defer p.mu.Unlock()

	newMap := make(map[string]*RuntimeSession, len(sessions))
	var newOrder []string

	for _, s := range sessions {
		if s.ProviderFamily != p.family {
			continue
		}
		if old, ok := p.sessions[s.ID]; ok && old != nil {
			old.UpdateSession(s)
			newMap[s.ID] = old
		} else {
			rs := NewRuntimeSession(s, p.cfg.MaxLeasesPerSession)
			rs.limiter = NewLimiter(p.cfg.SessionRequestsPerSec, p.cfg.SessionBurst)
			rs.limiter.now = p.now
			newMap[s.ID] = rs
		}
		newOrder = append(newOrder, s.ID)
	}

	// If no managed sessions exist and legacy is configured, include synthetic session
	if len(newMap) == 0 && p.legacy != nil && p.legacy.IsConfigured() {
		syn := p.legacy.SyntheticSession(p.family)
		if syn != nil {
			if old, ok := p.sessions[syn.ID]; ok && old != nil {
				old.UpdateSession(*syn)
				newMap[syn.ID] = old
			} else {
				rs := NewRuntimeSession(*syn, p.cfg.MaxLeasesPerSession)
				rs.limiter = NewLimiter(p.cfg.SessionRequestsPerSec, p.cfg.SessionBurst)
				rs.limiter.now = p.now
				newMap[syn.ID] = rs
			}
			newOrder = append(newOrder, syn.ID)
		}
	}

	p.sessions = newMap
	p.sessionOrder = newOrder
}

// Lease represents an acquired concurrency lease on a runtime media session.
// Releasing the lease updates health metrics and decrements active lease count.
type Lease struct {
	session     *RuntimeSession
	pool        *SessionPool
	cookiePath  string
	releaseOnce sync.Once
}

// CookiePath returns the filesystem path to the cookie file for trusted internal use.
func (l *Lease) CookiePath() string {
	if l == nil {
		return ""
	}
	return l.cookiePath
}

// CookieRef returns the opaque cookie reference.
func (l *Lease) CookieRef() string {
	if l == nil || l.session == nil {
		return ""
	}
	return l.session.Session().CookieRef
}

// SessionID returns the ID of the leased session.
func (l *Lease) SessionID() string {
	if l == nil || l.session == nil {
		return ""
	}
	return l.session.Session().ID
}

// Session returns a snapshot copy of the underlying session metadata.
func (l *Lease) Session() Session {
	if l == nil || l.session == nil {
		return Session{}
	}
	return l.session.Session()
}

// Release releases the acquired lease and evaluates health state transitions based on err.
// It is protected by sync.Once and is safe for double-release and defer calls.
func (l *Lease) Release(err error) {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		if l.pool != nil && l.session != nil {
			l.pool.releaseLease(l.session, err)
		}
	})
}

// Acquire requests a concurrency lease on an eligible media session.
// It blocks until a session is available, or until ctx is done.
// Pacing order: global family ceiling limiter -> per-session limiter.
func (p *SessionPool) Acquire(ctx context.Context) (*Lease, error) {
	if p == nil {
		return nil, apperr.New(apperr.CodeInvalidRequest, "session pool is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for {
		p.mu.Lock()
		now := p.now()

		hasAny := false
		hasPotentiallyEligible := false
		candidateList := make([]*RuntimeSession, 0, len(p.sessions))

		for _, id := range p.sessionOrder {
			rs := p.sessions[id]
			if rs == nil {
				continue
			}
			hasAny = true
			s := rs.Session()
			if s.Enabled && s.HealthStatus != HealthAuthFailed && strings.TrimSpace(s.CookieRef) != "" {
				hasPotentiallyEligible = true
			}
			candidateList = append(candidateList, rs)
		}

		if !hasAny || !hasPotentiallyEligible {
			p.mu.Unlock()
			return nil, apperr.New(apperr.CodeSessionNotFound, "no eligible media sessions available in pool")
		}

		selected := selectBestSession(candidateList, now, p.cfg.AllowUnknown)
		if selected != nil {
			// Acquire in-memory slot
			selected.TryAcquireWithPolicy(now, p.cfg.AllowUnknown)
			s := selected.Session()
			selectedLimiter := selected.limiter
			p.mu.Unlock()

			// Resolve cookie file path
			cookiePath := ""
			if p.storage != nil {
				path, err := p.storage.ResolvePath(s.CookieRef)
				if err != nil {
					p.releaseLease(selected, err)
					return nil, err
				}
				cookiePath = path
			} else if s.ID == LegacySessionID && p.legacy != nil {
				cookiePath = p.legacy.CookiePath()
			}

			// Pacing order: 1. Global family ceiling -> 2. Per-session limiter
			if p.globalLimiter != nil && p.globalLimiter.Enabled() {
				if err := p.globalLimiter.Wait(ctx); err != nil {
					p.releaseLease(selected, err)
					return nil, err
				}
			}

			if selectedLimiter != nil && selectedLimiter.Enabled() {
				if err := selectedLimiter.Wait(ctx); err != nil {
					p.releaseLease(selected, err)
					return nil, err
				}
			}

			return &Lease{
				session:    selected,
				pool:       p,
				cookiePath: cookiePath,
			}, nil
		}

		totalActiveLeases := 0
		for _, rs := range candidateList {
			totalActiveLeases += rs.CurrentLeases()
		}

		if totalActiveLeases == 0 {
			p.mu.Unlock()
			return nil, apperr.New(apperr.CodeSessionNotFound, "no media sessions currently available in pool")
		}

		// All eligible sessions are currently leased to capacity. Wait for a release.
		waitCh := make(chan struct{}, 1)
		p.waiters = append(p.waiters, waitCh)
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			p.mu.Lock()
			for i, ch := range p.waiters {
				if ch == waitCh {
					p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
					break
				}
			}
			p.mu.Unlock()
			return nil, ctx.Err()

		case <-waitCh:
			// Woken up by a lease release, loop back to re-evaluate
			continue
		}
	}
}

// selectBestSession implements health-aware least-loaded selection with LRU tie-break
// and stable ID final tie-break.
func selectBestSession(candidates []*RuntimeSession, now time.Time, allowUnknown bool) *RuntimeSession {
	var best *RuntimeSession

	for _, rs := range candidates {
		s := rs.Session()
		if !s.Enabled || strings.TrimSpace(s.CookieRef) == "" {
			continue
		}

		// Check eligibility by health and cooldown
		switch s.HealthStatus {
		case HealthHealthy:
			if rs.CurrentLeases() >= rs.MaxLeases() {
				continue
			}

		case HealthUnknown:
			if !allowUnknown {
				continue
			}
			// Strict single concurrency probe cap for unverified sessions
			if rs.CurrentLeases() >= 1 {
				continue
			}

		case HealthCooldown, HealthRateLimited:
			if s.CooldownUntil != nil && (now.After(*s.CooldownUntil) || now.Equal(*s.CooldownUntil)) {
				// Cooldown expired: allow single probe lease
				if rs.CurrentLeases() >= 1 {
					continue
				}
			} else {
				continue
			}

		case HealthBotChallenge:
			if s.CooldownUntil != nil && (now.After(*s.CooldownUntil) || now.Equal(*s.CooldownUntil)) {
				if rs.CurrentLeases() >= 1 {
					continue
				}
			} else {
				continue
			}

		default:
			// AuthFailed or unhandled state
			continue
		}

		if best == nil {
			best = rs
			continue
		}

		// 1. Lowest active lease count
		curLeases := rs.CurrentLeases()
		bestLeases := best.CurrentLeases()
		if curLeases < bestLeases {
			best = rs
			continue
		} else if curLeases > bestLeases {
			continue
		}

		// 2. Oldest LastUsedAt (nil / never used is older than any timestamp)
		bestS := best.Session()
		rsUsed := s.LastUsedAt
		bestUsed := bestS.LastUsedAt

		if rsUsed == nil && bestUsed != nil {
			best = rs
			continue
		} else if rsUsed != nil && bestUsed == nil {
			continue
		} else if rsUsed != nil && bestUsed != nil {
			if rsUsed.Before(*bestUsed) {
				best = rs
				continue
			} else if rsUsed.After(*bestUsed) {
				continue
			}
		}

		// 3. Final deterministic tie-break: stable session ID (alphabetical)
		if s.ID < bestS.ID {
			best = rs
		}
	}

	return best
}

func (p *SessionPool) releaseLease(rs *RuntimeSession, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	rs.Release()
	p.updateSessionHealthLocked(rs, err, now)

	// Wake up first waiter if any
	if len(p.waiters) > 0 {
		ch := p.waiters[0]
		p.waiters = p.waiters[1:]
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// RecordOutcome records the outcome of an operation (such as download) executed
// under an affine session, updating health state and success/used timestamps.
func (p *SessionPool) RecordOutcome(sessionID string, err error) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		return
	}
	now := p.now()
	p.updateSessionHealthLocked(rs, err, now)
}

// GetSession retrieves the RuntimeSession for the given session ID, or nil if not present.
func (p *SessionPool) GetSession(sessionID string) *RuntimeSession {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[sessionID]
}

// UpsertSession adds or updates a session in the runtime pool.
func (p *SessionPool) UpsertSession(s *Session) {
	if p == nil || s == nil || s.ProviderFamily != p.family {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if old, ok := p.sessions[s.ID]; ok && old != nil {
		old.UpdateSession(*s)
		return
	}

	rs := NewRuntimeSession(*s, p.cfg.MaxLeasesPerSession)
	rs.limiter = NewLimiter(p.cfg.SessionRequestsPerSec, p.cfg.SessionBurst)
	rs.limiter.now = p.now
	p.sessions[s.ID] = rs
	p.sessionOrder = append(p.sessionOrder, s.ID)
}

// RemoveSession removes a session from the runtime pool.
func (p *SessionPool) RemoveSession(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.sessions, sessionID)
	for i, id := range p.sessionOrder {
		if id == sessionID {
			p.sessionOrder = append(p.sessionOrder[:i], p.sessionOrder[i+1:]...)
			break
		}
	}
}

// RecordSuccess records a successful operation on a session.
func (p *SessionPool) RecordSuccess(ctx context.Context, sessionID string, now time.Time) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		return
	}
	p.updateSessionHealthLocked(rs, nil, now)
}

// RecordFailure records a failure on a session.
func (p *SessionPool) RecordFailure(ctx context.Context, sessionID string, err error, now time.Time) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		return
	}
	p.updateSessionHealthLocked(rs, err, now)
}

// ResolveCookiePath returns the filesystem cookie path for a given session ID,
// or empty string if not found or unmanaged.
func (p *SessionPool) ResolveCookiePath(sessionID string) string {
	if p == nil || sessionID == "" {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		if sessionID == LegacySessionID && p.legacy != nil {
			return p.legacy.CookiePath()
		}
		return ""
	}
	s := rs.Session()
	if s.ID == LegacySessionID && p.legacy != nil {
		return p.legacy.CookiePath()
	}
	if p.storage != nil && s.CookieRef != "" {
		path, err := p.storage.ResolvePath(s.CookieRef)
		if err == nil {
			return path
		}
	}
	return ""
}

// HasConfiguredSessions reports whether the pool contains any managed or legacy sessions.
func (p *SessionPool) HasConfiguredSessions() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions) > 0
}

// AcquireDataPlane acquires exclusive data-plane execution on sessionID's writable cookie file.
// It blocks until available or ctx is done.
func (p *SessionPool) AcquireDataPlane(ctx context.Context, sessionID string) (func(), error) {
	if p == nil || sessionID == "" {
		return func() {}, nil
	}
	p.mu.Lock()
	rs := p.sessions[sessionID]
	p.mu.Unlock()

	if rs == nil {
		return func() {}, nil
	}
	return rs.AcquireDataPlane(ctx)
}

// RetainDataPlane increments the in-flight data-plane reference count for sessionID.
func (p *SessionPool) RetainDataPlane(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	rs := p.sessions[sessionID]
	p.mu.Unlock()

	if rs != nil {
		rs.RetainDataPlane()
	}
}

// ReleaseDataPlane decrements the in-flight data-plane reference count for sessionID.
func (p *SessionPool) ReleaseDataPlane(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	rs := p.sessions[sessionID]
	p.mu.Unlock()

	if rs != nil {
		rs.ReleaseDataPlane()
	}
}

// IsInUse reports whether sessionID is currently leased or has in-flight data-plane operations.
func (p *SessionPool) IsInUse(sessionID string) bool {
	if p == nil || sessionID == "" {
		return false
	}
	p.mu.Lock()
	rs := p.sessions[sessionID]
	p.mu.Unlock()

	if rs != nil {
		return rs.IsInUse()
	}
	return false
}

func (p *SessionPool) updateSessionHealthLocked(rs *RuntimeSession, err error, now time.Time) {
	s := rs.Session()
	var healthUpdated bool
	var update HealthUpdate

	if err == nil {
		// Confirmed success: transition UNKNOWN -> HEALTHY, clear failures and cooldowns
		prevStatus := s.HealthStatus
		s.HealthStatus = HealthHealthy
		s.ConsecutiveFailures = 0
		s.LastSuccessAt = &now
		s.LastUsedAt = &now
		s.LastFailureReason = ""
		s.CooldownUntil = nil
		s.UpdatedAt = now
		rs.UpdateSession(s)

		if prevStatus != HealthHealthy {
			healthUpdated = true
			update = HealthUpdate{
				HealthStatus:        s.HealthStatus,
				ConsecutiveFailures: s.ConsecutiveFailures,
				LastUsedAt:          s.LastUsedAt,
				LastSuccessAt:       s.LastSuccessAt,
				CooldownUntil:       nil,
			}
		}
	} else {
		// Error handling: differentiate candidate vs session vs provider vs infrastructure
		s.LastUsedAt = &now
		s.UpdatedAt = now

		code := apperr.CodeOf(err)
		scope := apperr.ScopeOf(err)

		switch {
		case apperr.AllowsCandidateFallback(err):
			// Candidate-specific failure (e.g. 404 TrackNotFound): do NOT mark session unhealthy!
			rs.UpdateSession(s)

		case scope == apperr.ScopeInfrastructure:
			// Infrastructure failure: do NOT penalize session!
			rs.UpdateSession(s)

		case scope == apperr.ScopeProvider:
			// Platform-systemic failure: record on pool, do NOT mark individual session unhealthy!
			p.recordPlatformFailureLocked(err, now)
			rs.UpdateSession(s)

		case scope == apperr.ScopeSession:
			// Session-specific failure
			s.ConsecutiveFailures++
			s.LastFailureAt = &now
			s.LastFailureReason = sanitizeFailureReason(err)

			switch code {
			case apperr.CodeSessionRateLimited:
				s.HealthStatus = HealthRateLimited
				cd := calculateRateLimitCooldown(s.ConsecutiveFailures)
				until := now.Add(cd)
				s.CooldownUntil = &until

			case apperr.CodeSessionBotChallenge:
				s.HealthStatus = HealthBotChallenge
				if s.ConsecutiveFailures == 1 {
					until := now.Add(24 * time.Hour)
					s.CooldownUntil = &until
				} else {
					s.CooldownUntil = nil // Review required
				}

			case apperr.CodeSessionAuthFailed:
				s.HealthStatus = HealthAuthFailed
				s.CooldownUntil = nil // Excluded until replacement
			}

			rs.UpdateSession(s)
			healthUpdated = true
			update = HealthUpdate{
				HealthStatus:        s.HealthStatus,
				ConsecutiveFailures: s.ConsecutiveFailures,
				LastUsedAt:          s.LastUsedAt,
				LastFailureAt:       s.LastFailureAt,
				LastFailureReason:   s.LastFailureReason,
				CooldownUntil:       s.CooldownUntil,
			}
		default:
			rs.UpdateSession(s)
		}
	}

	// Persist health update if repository available and session is persisted
	if healthUpdated && p.repo != nil && s.ID != LegacySessionID {
		sessionID := s.ID
		if p.syncPersist {
			_, _ = p.repo.UpdateHealth(context.Background(), sessionID, update)
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = p.repo.UpdateHealth(ctx, sessionID, update)
			}()
		}
	}
}

// calculateRateLimitCooldown computes progressive backoff for repeated rate-limiting.
func calculateRateLimitCooldown(failures int) time.Duration {
	switch failures {
	case 1:
		return 1 * time.Minute
	case 2:
		return 2 * time.Minute
	case 3:
		return 5 * time.Minute
	case 4:
		return 15 * time.Minute
	case 5:
		return 30 * time.Minute
	default:
		return 1 * time.Hour
	}
}

// sanitizeFailureReason ensures that no raw stderr, auth secrets, cookies,
// or sensitive paths are stored in the database failure reason.
func sanitizeFailureReason(err error) string {
	if err == nil {
		return ""
	}
	code := apperr.CodeOf(err)
	msg := apperr.MessageOf(err)
	if msg == "" {
		msg = string(code)
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return fmt.Sprintf("[%s] %s", code, strings.TrimSpace(msg))
}

func (p *SessionPool) recordPlatformFailureLocked(err error, now time.Time) {
	cooldown := 1 * time.Minute
	if apperr.CodeOf(err) == apperr.CodeProviderRateLimited {
		cooldown = 2 * time.Minute
	}
	p.platformFailure = PlatformFailure{
		OccurredAt:    now,
		CooldownUntil: now.Add(cooldown),
		Err:           err,
	}
}

// RecordPlatformFailure marks a platform-wide systemic failure event.
func (p *SessionPool) RecordPlatformFailure(err error, cooldown time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.platformFailure = PlatformFailure{
		OccurredAt:    now,
		CooldownUntil: now.Add(cooldown),
		Err:           err,
	}
}

// LastPlatformFailure returns the last platform-systemic failure if recorded.
func (p *SessionPool) LastPlatformFailure() (PlatformFailure, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.platformFailure.OccurredAt.IsZero() {
		return PlatformFailure{}, false
	}
	return p.platformFailure, true
}

// IsPlatformCooling reports whether the provider family is currently in a platform-wide cooldown.
func (p *SessionPool) IsPlatformCooling() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.platformFailure.OccurredAt.IsZero() {
		return false
	}
	return p.now().Before(p.platformFailure.CooldownUntil)
}
