// Package mediasession models server-managed media sessions, their lifecycle,
// health classification, and in-memory concurrency lease controls.
package mediasession

import (
	"context"
	"sync"
	"time"

	"ytdm/backend/internal/provider"
)

// HealthStatus represents the runtime health condition of a media session.
type HealthStatus string

const (
	HealthUnknown      HealthStatus = "unknown"
	HealthHealthy      HealthStatus = "healthy"
	HealthCooldown     HealthStatus = "cooldown"
	HealthRateLimited  HealthStatus = "rate_limited"
	HealthBotChallenge HealthStatus = "bot_challenge"
	HealthAuthFailed   HealthStatus = "auth_failed"
)

// Valid reports whether the health status is one of the recognized states.
func (h HealthStatus) Valid() bool {
	switch h {
	case HealthUnknown, HealthHealthy, HealthCooldown, HealthRateLimited, HealthBotChallenge, HealthAuthFailed:
		return true
	default:
		return false
	}
}

// Session represents the persisted metadata and health status of an upstream media session.
// Sensitive cookie contents and ephemeral lease counts are NEVER stored here.
type Session struct {
	ID                  string          `json:"id"`
	ProviderFamily      provider.Family `json:"provider_family"`
	Name                string          `json:"name"`
	CookieRef           string          `json:"cookie_ref"`
	Enabled             bool            `json:"enabled"`
	HealthStatus        HealthStatus    `json:"health_status"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	LastUsedAt          *time.Time      `json:"last_used_at,omitempty"`
	LastSuccessAt       *time.Time      `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time      `json:"last_failure_at,omitempty"`
	LastFailureReason   string          `json:"last_failure_reason,omitempty"`
	CooldownUntil       *time.Time      `json:"cooldown_until,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// Filter defines criteria for querying media sessions.
type Filter struct {
	ProviderFamily string
	Enabled        *bool
	HealthStatus   *HealthStatus
}

// HealthUpdate encapsulates fields modified during session health transitions.
type HealthUpdate struct {
	HealthStatus        HealthStatus
	ConsecutiveFailures int
	LastUsedAt          *time.Time
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	LastFailureReason   string
	CooldownUntil       *time.Time
}

// RuntimeSession wraps a persisted Session with in-memory concurrency controls
// and lease management. Concurrency lease counters are strictly in-memory and
// always start at 0 after a backend restart.
type RuntimeSession struct {
	mu            sync.Mutex
	session       Session
	currentLeases int
	maxLeases     int
	limiter       *Limiter

	// Data-plane concurrency protection: ensures at most one active download
	// process uses this session's writable cookie jar at a time.
	dataPlaneSem  chan struct{}
	dataPlaneRefs int
}

// NewRuntimeSession initializes an in-memory runtime wrapper for a session.
// Leases start at 0.
func NewRuntimeSession(s Session, maxLeases int) *RuntimeSession {
	if maxLeases <= 0 {
		maxLeases = 1
	}
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // initially available
	return &RuntimeSession{
		session:      s,
		maxLeases:    maxLeases,
		dataPlaneSem: sem,
	}
}

// Session returns a snapshot copy of the underlying session.
func (rs *RuntimeSession) Session() Session {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.session
}

// UpdateSession updates the underlying persisted session snapshot.
func (rs *RuntimeSession) UpdateSession(s Session) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.session = s
}

// Limiter returns the per-session rate limiter if configured.
func (rs *RuntimeSession) Limiter() *Limiter {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.limiter
}

// SetLimiter attaches a rate limiter to the runtime session.
func (rs *RuntimeSession) SetLimiter(l *Limiter) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.limiter = l
}

// CurrentLeases returns the number of active in-memory leases.
func (rs *RuntimeSession) CurrentLeases() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.currentLeases
}

// MaxLeases returns the configured maximum concurrent leases.
func (rs *RuntimeSession) MaxLeases() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.maxLeases
}

// TryAcquire attempts to acquire a lease on this session allowing unknown sessions.
func (rs *RuntimeSession) TryAcquire(now time.Time) bool {
	return rs.TryAcquireWithPolicy(now, true)
}

// TryAcquireWithPolicy attempts to acquire a lease respecting health, cooldown, and unknown session policy.
// Unknown sessions are restricted to at most 1 concurrent lease.
func (rs *RuntimeSession) TryAcquireWithPolicy(now time.Time, allowUnknown bool) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.session.Enabled {
		return false
	}

	capLimit := rs.maxLeases

	switch rs.session.HealthStatus {
	case HealthHealthy:
		// Normal capacity

	case HealthUnknown:
		if !allowUnknown {
			return false
		}
		// Strict single-concurrency probe cap for unverified sessions
		capLimit = 1

	case HealthCooldown, HealthRateLimited:
		if rs.session.CooldownUntil != nil && (now.After(*rs.session.CooldownUntil) || now.Equal(*rs.session.CooldownUntil)) {
			// Cooldown elapsed: allow single probe lease to verify recovery
			capLimit = 1
		} else {
			return false
		}

	case HealthBotChallenge:
		if rs.session.CooldownUntil != nil && (now.After(*rs.session.CooldownUntil) || now.Equal(*rs.session.CooldownUntil)) {
			capLimit = 1
		} else {
			return false
		}

	default:
		// AuthFailed or unhandled status requires operator intervention
		return false
	}

	if rs.currentLeases >= capLimit {
		return false
	}
	rs.currentLeases++
	rs.session.LastUsedAt = &now
	return true
}

// Release frees an acquired lease on this session.
func (rs *RuntimeSession) Release() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.currentLeases > 0 {
		rs.currentLeases--
	}
}

// AcquireDataPlane acquires exclusive mutual exclusion for data-plane operations (e.g. yt-dlp download)
// on this session's writable cookie file. It blocks until available or ctx is done.
func (rs *RuntimeSession) AcquireDataPlane(ctx context.Context) (func(), error) {
	if rs == nil {
		return func() {}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-rs.dataPlaneSem:
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			select {
			case rs.dataPlaneSem <- struct{}{}:
			default:
			}
		})
	}
	return release, nil
}

// RetainDataPlane increments the in-flight data-plane reference count for this session.
func (rs *RuntimeSession) RetainDataPlane() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.dataPlaneRefs++
}

// ReleaseDataPlane decrements the in-flight data-plane reference count.
func (rs *RuntimeSession) ReleaseDataPlane() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.dataPlaneRefs > 0 {
		rs.dataPlaneRefs--
	}
}

// DataPlaneRefs returns the active in-flight data-plane reference count.
func (rs *RuntimeSession) DataPlaneRefs() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.dataPlaneRefs
}

// IsInUse reports whether the session has active control-plane leases or active data-plane references.
func (rs *RuntimeSession) IsInUse() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.currentLeases > 0 || rs.dataPlaneRefs > 0
}
