package mediasession

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/throughput"
	"ytdm/backend/internal/ytdlp"
)

const (
	defaultUnavailableRetry      = 15 * time.Minute
	botChallengeInitialCooldown  = 24 * time.Hour
	botChallengeRepeatedCooldown = 72 * time.Hour
	healthPersistTimeout         = 5 * time.Second
	recoveryNotifyTimeout        = 10 * time.Second

	// platformStrikeReset forgets earlier family-wide rate limits once none
	// has occurred for this long.
	platformStrikeReset = 30 * time.Minute
	// platformCooldownJitter spreads the end of a family-wide pause by up to
	// this fraction in either direction, so the waiting work does not resume
	// in lock step.
	platformCooldownJitter = 0.1
)

// platformRateLimitSteps is the family-wide pause after consecutive provider
// rate limits. Observed throttling episodes lasted 12 to 22 minutes, and a
// fixed two-minute pause met another rate limit right after almost every
// expiry without a single acquisition in between. Escalating to six minutes
// halves those futile requests while overshooting the end of an episode by
// only about a minute on average. Only a verified media acquisition, or a
// quiet period of platformStrikeReset, returns to the first step.
var platformRateLimitSteps = []time.Duration{2 * time.Minute, 4 * time.Minute, 6 * time.Minute}

// PoolState represents the operational availability status of the session pool.
type PoolState string

const (
	PoolStateEligible            PoolState = "eligible"
	PoolStateCapacityConstrained PoolState = "capacity_constrained"
	PoolStateCooling             PoolState = "cooling"
	PoolStateNoUsableSession     PoolState = "no_usable_session"
)

// PoolAvailability provides detailed status on whether sessions in the pool
// can serve requests now, are temporarily waiting for cooldown/capacity, or are
// unconfigured / permanently unusable without operator intervention.
type PoolAvailability struct {
	State      PoolState
	RetryAfter time.Duration
	Reason     string
}

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
		SessionBurst:          1,
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
	recoveryHandler func(ctx context.Context)

	// platformStrikes counts consecutive family-wide rate limits that arrived
	// after the previous pause had ended; lastPlatformRateLimit is the latest.
	platformStrikes       int
	lastPlatformRateLimit time.Time
	jitter                func() float64
	recorder              *throughput.Recorder

	// lifecycleCtx bounds out-of-band work started from pool callbacks to the
	// application lifetime. The recovery handler performs database work, so it
	// must be cancellable at shutdown instead of racing db.Close().
	lifecycleCtx context.Context

	// Health persistence runs outside p.mu on a strictly ordered single-drainer
	// queue: repository I/O must never block the family-wide pool lock, and an
	// older health snapshot must never overwrite a newer one.
	persistMu      sync.Mutex
	persistQueue   []healthPersistJob
	persistRunning bool
	// persistIdle is closed when the drainer goes idle, so a waiter can observe
	// that nothing is queued or in flight any more without polling. It is
	// non-nil exactly while persistRunning is true.
	persistIdle chan struct{}
	// persistSealed stops accepting new snapshots. A controlled shutdown seals
	// the queue once the producers are down, which is what makes the flush
	// behind it the final one: nothing can queue a write that would still be
	// running when the pool is closed.
	persistSealed bool
	// persistInFlight is the session whose snapshot the drainer is writing right
	// now, empty while it holds no write. persistInFlightDropped marks that this
	// session was removed while its write ran, so the outcome is discarded
	// instead of recording a failure against a session that no longer exists.
	persistInFlight        string
	persistInFlightDropped bool
	// persistFailures holds, per session, the health snapshot that never reached
	// the repository and has not been superseded since. Because HealthUpdate
	// stores a complete snapshot, a later successful write for the same session
	// resolves the entry - a success for a different session never does. Beyond
	// that only removing the session clears an entry: a barrier merely reads the
	// map, so a flush that gives up on its context cannot make an unresolved
	// failure disappear for the next one.
	persistFailures map[string]error
}

// healthPersistJob is one queued health snapshot write. A job with an empty
// sessionID is a flush barrier and performs no repository call.
type healthPersistJob struct {
	sessionID string
	update    HealthUpdate
	done      chan struct{}
	// result is set on barrier jobs only. The drainer fills it in before it
	// closes done, so the waiting FlushHealthPersist reads it without a lock.
	result *healthPersistResult
}

// healthPersistResult carries the unresolved failures a barrier observed among
// the writes queued ahead of it.
type healthPersistResult struct {
	err error
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
		cfg.SessionBurst = 1
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
		jitter:        rand.Float64,
	}
	return p
}

// SetRecorder reports family-wide protection responses, their pause time and
// session failures to the hourly throughput summary.
func (p *SessionPool) SetRecorder(r *throughput.Recorder) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recorder = r
}

// setJitter overrides the cooldown jitter source for deterministic tests. fn
// returns values in [0, 1).
func (p *SessionPool) setJitter(fn func() float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jitter = fn
}

// platformCooldownRemaining reports an active family-wide pause.
func (p *SessionPool) platformCooldownRemaining() (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.platformFailure.OccurredAt.IsZero() || !now.Before(p.platformFailure.CooldownUntil) {
		return 0, false
	}
	return p.platformFailure.CooldownUntil.Sub(now), true
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

// Now returns the pool's current time (or overridden time if configured for testing).
func (p *SessionPool) Now() time.Time {
	if p == nil {
		return time.Now().UTC()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.now == nil {
		return time.Now().UTC()
	}
	return p.now()
}

// SetSyncPersist enables synchronous repository health persistence for testing.
func (p *SessionPool) SetSyncPersist(sync bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncPersist = sync
}

// SetRecoveryHandler installs an optional callback invoked when a session
// is confirmed to have recovered its health via real media-path success.
func (p *SessionPool) SetRecoveryHandler(fn func(ctx context.Context)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recoveryHandler = fn
}

// SetLifecycleContext binds recovery notifications to the application lifecycle.
// Cancelling ctx aborts an in-flight recovery callback, so a slow or closing
// database cannot hold a download worker - and therefore shutdown - open.
// Without it the pool falls back to context.Background().
func (p *SessionPool) SetLifecycleContext(ctx context.Context) {
	if p == nil || ctx == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lifecycleCtx = ctx
}

// recoveryContextLocked derives the bounded, cancellable context handed to the
// recovery handler.
func (p *SessionPool) recoveryContextLocked() context.Context {
	if p.lifecycleCtx != nil {
		return p.lifecycleCtx
	}
	return context.Background()
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

	// Include synthetic legacy session alongside managed sessions if legacy is configured
	if p.legacy != nil && p.legacy.IsConfigured() {
		syn := p.legacy.SyntheticSession(p.family)
		if syn != nil {
			if old, ok := p.sessions[syn.ID]; ok && old != nil {
				cur := old.Session()
				cur.Name = syn.Name
				cur.Enabled = syn.Enabled
				cur.CookieRef = syn.CookieRef
				old.UpdateSession(cur)
				newMap[syn.ID] = old
			} else if _, exists := newMap[syn.ID]; !exists {
				rs := NewRuntimeSession(*syn, p.cfg.MaxLeasesPerSession)
				rs.limiter = NewLimiter(p.cfg.SessionRequestsPerSec, p.cfg.SessionBurst)
				rs.limiter.now = p.now
				newMap[syn.ID] = rs
			}
			found := false
			for _, id := range newOrder {
				if id == syn.ID {
					found = true
					break
				}
			}
			if !found {
				newOrder = append(newOrder, syn.ID)
			}
		}
	}

	// Sessions the reload dropped are gone for the same reasons a delete removes
	// one, so their pending and failed health state goes with them.
	for id := range p.sessions {
		if _, kept := newMap[id]; !kept {
			p.forgetHealthPersistLocked(id)
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

// Acquire makes Lease a yt-dlp ExecutionGate bound to its selected session.
//
// The lease gates the metadata requests of an attempt. A family-wide
// protection response can arrive while this caller waits for the session slot
// or for pacing - typically from another worker's request that was already in
// flight. Starting the request anyway would only meet the same block, so the
// gate refuses it with a session wait instead. The refusal is a wait state: it
// is not attributed to the session, and it triggers no further cooldown.
func (l *Lease) Acquire(ctx context.Context) (func(), error) {
	if l == nil || l.session == nil {
		return func() {}, nil
	}
	if err := l.platformPause(); err != nil {
		return nil, err
	}
	var global *Limiter
	if l.pool != nil {
		global = l.pool.GlobalLimiter()
	}
	release, err := l.session.acquireExecution(ctx, global)
	if err != nil {
		return nil, err
	}
	if err := l.platformPause(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (l *Lease) platformPause() error {
	if l.pool == nil {
		return nil
	}
	remaining, cooling := l.pool.platformCooldownRemaining()
	if !cooling {
		return nil
	}
	if remaining < 5*time.Second {
		remaining = 5 * time.Second
	}
	return apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
		"YouTube acquisition is temporarily paused after a provider protection response.", remaining)
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
//
// err must be attributable to this session: a nil err records a confirmed
// success on it. Callers whose outcome was decided by something other than this
// session must use ReleaseNeutral instead.
func (l *Lease) Release(err error) {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		if l.pool != nil && l.session != nil {
			l.pool.releaseLease(l.session, err, true)
		}
	})
}

// ReleaseNeutral releases the acquired lease without attributing any health
// outcome to the session. It frees the concurrency slot and wakes waiters, but
// records neither a success nor a failure.
//
// It is the correct release when the leased session did its own work without
// fault yet the operation ended for an unrelated reason - an independent
// provider cooldown, a cancellation, or a failure on another provider family.
func (l *Lease) ReleaseNeutral() {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		if l.pool != nil && l.session != nil {
			l.pool.releaseLease(l.session, nil, false)
		}
	})
}

// Acquire requests a concurrency lease on an eligible media session.
// It blocks until a session is available, or until ctx is done.
// Process pacing happens later at the yt-dlp execution boundary.
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
		hasConfigured := false
		candidateList := make([]*RuntimeSession, 0, len(p.sessions))

		for _, id := range p.sessionOrder {
			rs := p.sessions[id]
			if rs == nil {
				continue
			}
			hasAny = true
			s := rs.Session()
			if s.Enabled && strings.TrimSpace(s.CookieRef) != "" {
				hasConfigured = true
			}
			candidateList = append(candidateList, rs)
		}

		if !hasAny || !hasConfigured {
			p.drainWaitersLocked()
			p.mu.Unlock()
			return nil, apperr.New(apperr.CodeSessionNotFound, "no eligible media sessions available in pool")
		}

		if !p.platformFailure.OccurredAt.IsZero() && now.Before(p.platformFailure.CooldownUntil) {
			retryAfter := p.platformFailure.CooldownUntil.Sub(now)
			p.drainWaitersLocked()
			p.mu.Unlock()
			return nil, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
				"YouTube acquisition is temporarily paused after a provider protection response.", retryAfter)
		}

		selected := selectBestSession(candidateList, now, p.cfg.AllowUnknown)
		if selected != nil {
			// Acquire in-memory slot
			selected.TryAcquireWithPolicy(now, p.cfg.AllowUnknown)
			if len(p.waiters) > 0 && selectBestSession(candidateList, now, p.cfg.AllowUnknown) != nil {
				p.wakeOneWaiterLocked()
			}
			s := selected.Session()
			p.mu.Unlock()

			// Resolve cookie file path
			cookiePath := ""
			if p.storage != nil {
				path, err := p.storage.ResolvePath(s.CookieRef)
				if err != nil {
					p.releaseLease(selected, err, true)
					return nil, err
				}
				cookiePath = path
			} else if s.ID == LegacySessionID && p.legacy != nil {
				cookiePath = p.legacy.CookiePath()
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
			p.drainWaitersLocked()
			err := p.sessionUnavailableLocked(now)
			p.mu.Unlock()
			return nil, err
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

func (p *SessionPool) sessionUnavailableLocked(now time.Time) error {
	var retryAfter time.Duration
	if !p.platformFailure.OccurredAt.IsZero() && now.Before(p.platformFailure.CooldownUntil) {
		retryAfter = p.platformFailure.CooldownUntil.Sub(now)
	}
	for _, id := range p.sessionOrder {
		rs := p.sessions[id]
		if rs == nil {
			continue
		}
		s := rs.Session()
		if !s.Enabled || strings.TrimSpace(s.CookieRef) == "" || s.CooldownUntil == nil || !s.CooldownUntil.After(now) {
			continue
		}
		if wait := s.CooldownUntil.Sub(now); retryAfter == 0 || wait < retryAfter {
			retryAfter = wait
		}
	}
	if retryAfter == 0 {
		retryAfter = defaultUnavailableRetry
	}
	if retryAfter < 5*time.Second {
		retryAfter = 5 * time.Second
	}
	return apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
		"Configured YouTube sessions are temporarily unavailable; the item will wait for session eligibility.", retryAfter)
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

// releaseLease frees rs's concurrency slot. When attributeHealth is false the
// session's health state is left untouched, because the outcome was not caused
// by this session.
func (p *SessionPool) releaseLease(rs *RuntimeSession, err error, attributeHealth bool) {
	p.mu.Lock()
	now := p.now()
	rs.Release()
	var notify func()
	if attributeHealth {
		notify = p.updateSessionHealthLocked(rs, err, now, p.syncPersist)
	}

	if len(p.waiters) == 0 {
		p.mu.Unlock()
		if notify != nil {
			notify()
		}
		return
	}

	candidateList := p.candidateListLocked()
	selected := selectBestSession(candidateList, now, p.cfg.AllowUnknown)
	if selected != nil {
		// A session is ready to accept a lease: wake the next waiter in FIFO order
		p.wakeOneWaiterLocked()
		p.mu.Unlock()
		if notify != nil {
			notify()
		}
		return
	}

	// No session is ready right now.
	// Check if any other leases are still active in the pool.
	totalActiveLeases := 0
	for _, s := range candidateList {
		totalActiveLeases += s.CurrentLeases()
	}

	if totalActiveLeases == 0 {
		// Zero sessions available AND zero active leases remain.
		// No future lease release will ever occur to wake these waiters.
		// Drain all waiters so they return SESSION_NOT_FOUND promptly without starving.
		p.drainWaitersLocked()
	}
	p.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// RecordOutcome records the outcome of an operation (such as download) executed
// under an affine session, updating health state and success/used timestamps.
func (p *SessionPool) RecordOutcome(sessionID string, err error) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		p.mu.Unlock()
		return
	}
	now := p.now()
	notify := p.updateSessionHealthLocked(rs, err, now, p.syncPersist)
	p.mu.Unlock()

	if notify != nil {
		notify()
	}
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
		if s.HealthStatus == HealthHealthy && (s.CooldownUntil == nil || !s.CooldownUntil.After(p.now())) {
			p.platformFailure = PlatformFailure{}
		}
		if len(p.waiters) > 0 {
			candidateList := p.candidateListLocked()
			if selectBestSession(candidateList, p.now(), p.cfg.AllowUnknown) != nil {
				p.wakeOneWaiterLocked()
			}
		}
		return
	}

	rs := NewRuntimeSession(*s, p.cfg.MaxLeasesPerSession)
	rs.limiter = NewLimiter(p.cfg.SessionRequestsPerSec, p.cfg.SessionBurst)
	rs.limiter.now = p.now
	p.sessions[s.ID] = rs
	p.sessionOrder = append(p.sessionOrder, s.ID)
	if s.HealthStatus == HealthHealthy && (s.CooldownUntil == nil || !s.CooldownUntil.After(p.now())) {
		p.platformFailure = PlatformFailure{}
	}
	if len(p.waiters) > 0 {
		candidateList := p.candidateListLocked()
		if selectBestSession(candidateList, p.now(), p.cfg.AllowUnknown) != nil {
			p.wakeOneWaiterLocked()
		}
	}
}

// ClearPlatformFailure clears any active platform-wide systemic failure cooldown.
func (p *SessionPool) ClearPlatformFailure() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.platformFailure = PlatformFailure{}
}

// RemoveSession removes a session from the runtime pool.
func (p *SessionPool) RemoveSession(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// The row is already gone by the time the service gets here, so queued or
	// recorded health state for it can never reach the database again.
	p.forgetHealthPersistLocked(sessionID)

	delete(p.sessions, sessionID)
	for i, id := range p.sessionOrder {
		if id == sessionID {
			p.sessionOrder = append(p.sessionOrder[:i], p.sessionOrder[i+1:]...)
			break
		}
	}
	if len(p.waiters) > 0 {
		candidateList := p.candidateListLocked()
		totalActiveLeases := 0
		for _, s := range candidateList {
			totalActiveLeases += s.CurrentLeases()
		}
		if totalActiveLeases == 0 && selectBestSession(candidateList, p.now(), p.cfg.AllowUnknown) == nil {
			p.drainWaitersLocked()
		}
	}
}

func (p *SessionPool) candidateListLocked() []*RuntimeSession {
	candidateList := make([]*RuntimeSession, 0, len(p.sessions))
	for _, id := range p.sessionOrder {
		if s := p.sessions[id]; s != nil {
			candidateList = append(candidateList, s)
		}
	}
	return candidateList
}

func (p *SessionPool) wakeOneWaiterLocked() {
	if len(p.waiters) > 0 {
		ch := p.waiters[0]
		p.waiters = p.waiters[1:]
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (p *SessionPool) drainWaitersLocked() {
	for len(p.waiters) > 0 {
		ch := p.waiters[0]
		p.waiters = p.waiters[1:]
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// RecordSuccess records a successful operation on a session.
func (p *SessionPool) RecordSuccess(ctx context.Context, sessionID string, now time.Time) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		p.mu.Unlock()
		return
	}
	notify := p.updateSessionHealthLocked(rs, nil, now, true)
	p.mu.Unlock()

	if notify != nil {
		notify()
	}
}

// RecordFailure records a failure on a session.
func (p *SessionPool) RecordFailure(ctx context.Context, sessionID string, err error, now time.Time) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	rs, ok := p.sessions[sessionID]
	if !ok || rs == nil {
		p.mu.Unlock()
		return
	}
	notify := p.updateSessionHealthLocked(rs, err, now, true)
	p.mu.Unlock()

	if notify != nil {
		notify()
	}
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

// Availability reports the operational availability status of the pool.
func (p *SessionPool) Availability() PoolAvailability {
	if p == nil {
		return PoolAvailability{State: PoolStateNoUsableSession, Reason: "pool is nil"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if !p.platformFailure.OccurredAt.IsZero() && now.Before(p.platformFailure.CooldownUntil) {
		wait := p.platformFailure.CooldownUntil.Sub(now)
		if wait < 5*time.Second {
			wait = 5 * time.Second
		}
		return PoolAvailability{
			State:      PoolStateCooling,
			RetryAfter: wait,
			Reason:     "platform protection cooldown",
		}
	}

	hasConfigured := false
	candidateList := make([]*RuntimeSession, 0, len(p.sessions))
	for _, id := range p.sessionOrder {
		rs := p.sessions[id]
		if rs == nil {
			continue
		}
		s := rs.Session()
		if s.Enabled && strings.TrimSpace(s.CookieRef) != "" {
			hasConfigured = true
		}
		candidateList = append(candidateList, rs)
	}

	if !hasConfigured {
		return PoolAvailability{
			State:  PoolStateNoUsableSession,
			Reason: "no enabled sessions with configured credentials",
		}
	}

	selected := selectBestSession(candidateList, now, p.cfg.AllowUnknown)
	if selected != nil {
		return PoolAvailability{State: PoolStateEligible}
	}

	totalActiveLeases := 0
	for _, rs := range candidateList {
		totalActiveLeases += rs.CurrentLeases()
	}
	if totalActiveLeases > 0 {
		return PoolAvailability{State: PoolStateCapacityConstrained}
	}

	var retryAfter time.Duration
	for _, rs := range candidateList {
		s := rs.Session()
		if !s.Enabled || strings.TrimSpace(s.CookieRef) == "" || s.CooldownUntil == nil || !s.CooldownUntil.After(now) {
			continue
		}
		if wait := s.CooldownUntil.Sub(now); retryAfter == 0 || wait < retryAfter {
			retryAfter = wait
		}
	}
	if retryAfter > 0 {
		if retryAfter < 5*time.Second {
			retryAfter = 5 * time.Second
		}
		return PoolAvailability{
			State:      PoolStateCooling,
			RetryAfter: retryAfter,
			Reason:     "sessions in cooldown",
		}
	}

	return PoolAvailability{
		State:  PoolStateNoUsableSession,
		Reason: "no eligible media sessions available in pool",
	}
}

// HasEligibleSession reports whether the pool currently has at least one session
// eligible for work (or active leases in flight), and if not, returns the duration
// until the earliest session cooldown expires. If no session is cooling and none is
// eligible, it returns (false, 0).
func (p *SessionPool) HasEligibleSession() (bool, time.Duration) {
	if p == nil {
		return false, 0
	}
	avail := p.Availability()
	switch avail.State {
	case PoolStateEligible, PoolStateCapacityConstrained:
		return true, 0
	case PoolStateCooling:
		return false, avail.RetryAfter
	default:
		return false, 0
	}
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

type sessionExecutionGate struct {
	pool    *SessionPool
	session *RuntimeSession
}

func (g *sessionExecutionGate) Acquire(ctx context.Context) (func(), error) {
	if g == nil || g.session == nil {
		return func() {}, nil
	}
	var global *Limiter
	if g.pool != nil {
		global = g.pool.GlobalLimiter()
	}
	return g.session.acquireExecution(ctx, global)
}

// ExecutionGate returns the process gate for a managed session. Different
// sessions receive different mutexes while sharing only the family start-rate
// ceiling.
func (p *SessionPool) ExecutionGate(sessionID string) ytdlp.ExecutionGate {
	if p == nil || sessionID == "" {
		return nil
	}
	p.mu.Lock()
	rs := p.sessions[sessionID]
	p.mu.Unlock()
	if rs == nil {
		return nil
	}
	return &sessionExecutionGate{pool: p, session: rs}
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

// healthUpdateOf snapshots the complete health state of s for persistence.
//
// repo.UpdateHealth rewrites every health column, so a partial HealthUpdate does
// not "leave a field alone" - it nulls it. Persisting the full in-memory snapshot
// is what keeps the SessionPool and the database in the same state after every
// transition, and what makes a reload reproduce the pre-restart pool state.
func healthUpdateOf(s Session) HealthUpdate {
	return HealthUpdate{
		HealthStatus:        s.HealthStatus,
		ConsecutiveFailures: s.ConsecutiveFailures,
		LastUsedAt:          s.LastUsedAt,
		LastSuccessAt:       s.LastSuccessAt,
		LastFailureAt:       s.LastFailureAt,
		LastFailureReason:   s.LastFailureReason,
		CooldownUntil:       s.CooldownUntil,
	}
}

func (p *SessionPool) updateSessionHealthLocked(rs *RuntimeSession, err error, now time.Time, sync bool) func() {
	s := rs.Session()
	var healthUpdated bool
	var update HealthUpdate
	var notifyRecovery func()

	if err != nil && apperr.IsSessionWait(err) {
		// A wait state means no request was made on this session's behalf -
		// the gate or a cooldown refused it. There is nothing to attribute.
		return nil
	}

	if err == nil {
		// Only a verified media acquisition ends a run of family-wide rate
		// limits; the next one starts again at the first pause step.
		p.platformStrikes = 0
		p.lastPlatformRateLimit = time.Time{}

		// Confirmed success: transition UNKNOWN -> HEALTHY, clear failures and cooldowns.
		// LastFailureAt is cleared together with LastFailureReason: repo.UpdateHealth
		// overwrites every health column, so leaving the timestamp in memory would
		// make the pool disagree with the row it just wrote.
		prevStatus := s.HealthStatus
		prevFailures := s.ConsecutiveFailures
		prevLastFailureAt := s.LastFailureAt
		prevLastFailureReason := s.LastFailureReason
		prevCooldownUntil := s.CooldownUntil

		s.HealthStatus = HealthHealthy
		s.ConsecutiveFailures = 0
		s.LastSuccessAt = &now
		s.LastUsedAt = &now
		s.LastFailureAt = nil
		s.LastFailureReason = ""
		s.CooldownUntil = nil
		s.UpdatedAt = now
		rs.UpdateSession(s)

		statusChanged := prevStatus != HealthHealthy
		failureCleared := prevFailures != 0 || prevLastFailureAt != nil || prevLastFailureReason != "" || prevCooldownUntil != nil
		hadPlatformFailure := !p.platformFailure.OccurredAt.IsZero()

		if statusChanged || failureCleared {
			healthUpdated = true
			update = healthUpdateOf(s)
		}

		if statusChanged || hadPlatformFailure {
			p.platformFailure = PlatformFailure{}
			if p.recoveryHandler != nil {
				h := p.recoveryHandler
				base := p.recoveryContextLocked()
				notifyRecovery = func() {
					ctx, cancel := context.WithTimeout(base, recoveryNotifyTimeout)
					defer cancel()
					h(ctx)
				}
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
			prevStatus := s.HealthStatus
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
				cd := calculateBotChallengeCooldown(prevStatus == HealthBotChallenge)
				until := now.Add(cd)
				s.CooldownUntil = &until

			case apperr.CodeSessionAuthFailed:
				s.HealthStatus = HealthAuthFailed
				s.CooldownUntil = nil // Excluded until replacement
			}
			p.recorder.Inc("session." + string(p.family) + "." + string(code))

			rs.UpdateSession(s)
			healthUpdated = true
			update = healthUpdateOf(s)
		default:
			rs.UpdateSession(s)
		}
	}

	// Queue the health snapshot for persistence. Enqueueing performs no I/O, so
	// it is safe under p.mu; the actual repository write happens on the ordered
	// drainer after the caller has released the pool lock. Only a live session
	// may queue one: the in-memory transition above is harmless on a runtime
	// session nobody can reach any more, but its write would fail against a row
	// that is gone and leave an unresolvable failure behind.
	var persisted <-chan struct{}
	if healthUpdated && p.repo != nil && s.ID != LegacySessionID && p.sessionIsLiveLocked(s.ID, rs) {
		persisted = p.enqueueHealthPersistLocked(s.ID, update)
	}

	waitForPersist := sync && persisted != nil
	if !waitForPersist && notifyRecovery == nil {
		return nil
	}

	return func() {
		if waitForPersist {
			<-persisted
		}
		if notifyRecovery != nil {
			notifyRecovery()
		}
	}
}

// sessionIsLiveLocked reports whether rs is still the pool's runtime session for
// id. Callers must hold p.mu.
//
// A Lease holds its RuntimeSession directly and never looks the id up again, so
// it can outlive both the removal of that session and a later session created
// under the same id. Comparing identity rather than merely the id separates the
// two: the orphaned runtime session of a deleted row is rejected, while the
// session a reload or an upsert carried over keeps the very same pointer and is
// unaffected. Every RuntimeSession the pool hands out is inserted into p.sessions
// when it is created, so no live session is ever missed here.
func (p *SessionPool) sessionIsLiveLocked(id string, rs *RuntimeSession) bool {
	current, ok := p.sessions[id]
	return ok && current == rs
}

// enqueueHealthPersistLocked appends a health snapshot to the ordered persistence
// queue and starts the drainer if it is idle. It performs no blocking I/O and is
// safe to call while p.mu is held. The returned channel is closed once this exact
// snapshot has been written; it is nil once the queue has been sealed for
// shutdown and the snapshot is therefore not going to be written at all.
func (p *SessionPool) enqueueHealthPersistLocked(sessionID string, update HealthUpdate) <-chan struct{} {
	p.persistMu.Lock()
	defer p.persistMu.Unlock()

	if p.persistSealed {
		// The final flush has already claimed everything that can still reach
		// the database. Queueing behind it would only race the teardown.
		return nil
	}

	job := healthPersistJob{sessionID: sessionID, update: update, done: make(chan struct{})}
	p.persistQueue = append(p.persistQueue, job)
	p.startHealthDrainerLocked()

	return job.done
}

// startHealthDrainerLocked starts the single drainer unless one is already
// running. Callers must hold p.persistMu. Pairing the running flag with a fresh
// idle channel in one place keeps the two in step, so persistIdle is non-nil
// exactly while a drainer is alive.
func (p *SessionPool) startHealthDrainerLocked() {
	if p.persistRunning {
		return
	}
	p.persistRunning = true
	p.persistIdle = make(chan struct{})
	go p.drainHealthPersist()
}

// SealHealthPersist stops accepting new health snapshots for persistence.
// A controlled shutdown seals the queue once the producers are down and before
// the final flush: that is what makes the flush final, because nothing can queue
// a write behind it that would still be running when the pool is closed.
// Snapshots already queued are unaffected and still drain in FIFO order.
func (p *SessionPool) SealHealthPersist() {
	if p == nil {
		return
	}
	p.persistMu.Lock()
	p.persistSealed = true
	p.persistMu.Unlock()
}

// forgetHealthPersistLocked drops every trace of sessionID from the health
// persistence machinery: the snapshots still queued for a row that is already
// gone, the outcome of a write that is in flight right now, and any failure
// recorded for it. Callers must hold p.mu.
//
// Without it a session deleted between the enqueue and the write leaves a
// SessionNotFound failure behind that no later snapshot for that session can
// ever supersede, so every flush from then on reports the same dead session and
// the failure map grows with every delete.
func (p *SessionPool) forgetHealthPersistLocked(sessionID string) {
	if sessionID == "" {
		return
	}

	p.persistMu.Lock()
	kept := p.persistQueue[:0]
	var abandoned []chan struct{}
	for _, job := range p.persistQueue {
		// Barriers carry no session and must survive: dropping one would leave
		// a flush waiting for a write that is never going to happen.
		if job.sessionID == sessionID {
			abandoned = append(abandoned, job.done)
			continue
		}
		kept = append(kept, job)
	}
	p.persistQueue = kept
	if p.persistInFlight == sessionID {
		p.persistInFlightDropped = true
	}
	delete(p.persistFailures, sessionID)
	p.persistMu.Unlock()

	// The waiters are synchronous HealthUpdate callers, never the drainer, so
	// releasing them here cannot deadlock the queue.
	for _, done := range abandoned {
		close(done)
	}
}

// drainHealthPersist writes queued health snapshots strictly in transition order.
// Exactly one drainer runs at a time, so a stale snapshot can never overwrite a
// newer status or cooldown. Every snapshot gets exactly one bounded attempt -
// retrying here would delay shutdown without bound - and a failed attempt is
// recorded for the next barrier instead of being discarded. It never takes p.mu
// and never performs repository I/O while holding p.persistMu.
func (p *SessionPool) drainHealthPersist() {
	for {
		p.persistMu.Lock()
		if len(p.persistQueue) == 0 {
			p.persistRunning = false
			if p.persistIdle != nil {
				close(p.persistIdle)
				p.persistIdle = nil
			}
			p.persistMu.Unlock()
			return
		}
		job := p.persistQueue[0]
		p.persistQueue = p.persistQueue[1:]

		if job.sessionID == "" {
			// Barrier: report what is still unresolved among the writes queued
			// ahead of it. Reading is all it does - consuming here would lose the
			// state whenever the waiting flush has already given up on its
			// context and nobody reads the result.
			if job.result != nil {
				job.result.err = p.unresolvedPersistErrorLocked()
			}
			p.persistMu.Unlock()
			close(job.done)
			continue
		}
		// Publish the write, so a removal that lands while it runs can discard
		// its outcome instead of recording a failure for a session that is gone.
		p.persistInFlight = job.sessionID
		p.persistInFlightDropped = false
		p.persistMu.Unlock()

		var (
			attempted bool
			err       error
		)
		if p.repo != nil {
			attempted = true
			ctx, cancel := context.WithTimeout(context.Background(), healthPersistTimeout)
			_, err = p.repo.UpdateHealth(ctx, job.sessionID, job.update)
			cancel()
		}

		p.persistMu.Lock()
		dropped := p.persistInFlightDropped
		p.persistInFlight = ""
		p.persistInFlightDropped = false
		switch {
		case !attempted || dropped:
			// Either nothing was written at all, or the session was removed
			// underneath the write. In both cases there is no snapshot state
			// left worth tracking for it.
		case err != nil:
			if _, seen := p.persistFailures[job.sessionID]; !seen {
				if p.persistFailures == nil {
					p.persistFailures = make(map[string]error)
				}
				p.persistFailures[job.sessionID] = fmt.Errorf(
					"persist health snapshot for session %s: %w", job.sessionID, err)
			}
		default:
			// The write stored a complete snapshot for this session, so
			// whatever failed for it earlier is superseded. Sessions that
			// did not get a new snapshot stay unresolved.
			delete(p.persistFailures, job.sessionID)
		}
		p.persistMu.Unlock()
		close(job.done)
	}
}

// unresolvedPersistErrorLocked summarises the health snapshots that never
// reached the repository and have not been superseded by a later write for the
// same session. Callers must hold p.persistMu.
func (p *SessionPool) unresolvedPersistErrorLocked() error {
	if len(p.persistFailures) == 0 {
		return nil
	}
	ids := make([]string, 0, len(p.persistFailures))
	for id := range p.persistFailures {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	joined := make([]error, 0, len(ids))
	for _, id := range ids {
		joined = append(joined, p.persistFailures[id])
	}
	return errors.Join(joined...)
}

// FlushHealthPersist blocks until every health snapshot queued before the call
// has been written, or until ctx is done. It then reports the snapshots that are
// still missing from the repository: a failed write for a session that a later
// write did not supersede. Snapshots queued after the call belong to the next
// flush. A nil return therefore means the pending health state reached the
// database, not merely that the queue drained. An unresolved failure keeps being
// reported until a successful snapshot for that session replaces it, so neither
// a cancelled flush nor a success for an unrelated session can lose it.
func (p *SessionPool) FlushHealthPersist(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.persistMu.Lock()
	if !p.persistRunning && len(p.persistQueue) == 0 {
		err := p.unresolvedPersistErrorLocked()
		p.persistMu.Unlock()
		return err
	}
	result := &healthPersistResult{}
	barrier := healthPersistJob{done: make(chan struct{}), result: result}
	p.persistQueue = append(p.persistQueue, barrier)
	p.startHealthDrainerLocked()
	p.persistMu.Unlock()

	select {
	case <-barrier.done:
		return result.err
	case <-ctx.Done():
		select {
		case <-barrier.done:
			return result.err
		default:
			return ctx.Err()
		}
	}
}

// AwaitHealthPersistIdle reports whether health persistence has come to rest:
// nothing queued, and no repository write in flight. It waits for that state
// until ctx is done.
//
// The answer comes from the pool's own state, never from ctx: a flush over an
// already drained queue returns instantly even on an expired deadline, so an
// expired deadline on its own is no evidence that a write is still running. A
// caller that has to decide whether the connection pool may be torn down must
// ask this rather than inspect its own context.
func (p *SessionPool) AwaitHealthPersistIdle(ctx context.Context) bool {
	if p == nil {
		return true
	}
	for {
		p.persistMu.Lock()
		if !p.persistRunning && len(p.persistQueue) == 0 {
			p.persistMu.Unlock()
			return true
		}
		idle := p.persistIdle
		p.persistMu.Unlock()

		if idle == nil {
			// A non-empty queue without a drainer cannot happen - both enqueue
			// paths start one under the same lock - but nothing would ever
			// drain it, so "busy" is the only safe answer.
			return false
		}
		select {
		case <-idle:
			// The drainer went idle; confirm under the lock, because a new
			// snapshot may have started another one in the meantime.
		case <-ctx.Done():
			return false
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

// calculateBotChallengeCooldown computes bounded cooldown for bot challenge events.
// First challenge: 24h. Repeated/subsequent challenges: 72h.
func calculateBotChallengeCooldown(isRepeat bool) time.Duration {
	if !isRepeat {
		return botChallengeInitialCooldown
	}
	return botChallengeRepeatedCooldown
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
		cooldown = p.nextRateLimitCooldownLocked(now)
	}
	p.setPlatformFailureLocked(err, now, now.Add(cooldown))
	p.recorder.Inc("platform." + string(p.family) + "." + string(apperr.CodeOf(err)))
}

// nextRateLimitCooldownLocked returns the pause for a provider rate limit.
// A rate limit reported while the previous pause is still running came from a
// request that was already in flight; it proves nothing new and does not
// escalate. One after the pause ended does.
func (p *SessionPool) nextRateLimitCooldownLocked(now time.Time) time.Duration {
	active := !p.platformFailure.OccurredAt.IsZero() && now.Before(p.platformFailure.CooldownUntil)
	if !p.lastPlatformRateLimit.IsZero() && now.Sub(p.lastPlatformRateLimit) > platformStrikeReset {
		p.platformStrikes = 0
	}
	if !active || p.platformStrikes == 0 {
		p.platformStrikes++
	}
	p.lastPlatformRateLimit = now

	step := platformRateLimitSteps[min(p.platformStrikes, len(platformRateLimitSteps))-1]
	factor := 1.0
	if p.jitter != nil {
		factor = 1 - platformCooldownJitter + 2*platformCooldownJitter*p.jitter()
	}
	return time.Duration(float64(step) * factor)
}

// setPlatformFailureLocked records a family-wide failure. An active pause is
// never shortened by a later, milder failure, and only the time a failure
// adds beyond the running pause is reported as cooldown time.
func (p *SessionPool) setPlatformFailureLocked(err error, now, until time.Time) {
	from := now
	if !p.platformFailure.OccurredAt.IsZero() && p.platformFailure.CooldownUntil.After(now) {
		from = p.platformFailure.CooldownUntil
		if !until.After(from) {
			until = from
		}
	}
	p.recorder.AddDuration("platform."+string(p.family)+".cooldown_ms", until.Sub(from))
	p.platformFailure = PlatformFailure{
		OccurredAt:    now,
		CooldownUntil: until,
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

// PlatformStrikes reports how many consecutive family-wide rate limits the
// current pause step is based on.
func (p *SessionPool) PlatformStrikes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.platformStrikes
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
