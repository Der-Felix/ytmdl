package orchestrator

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/ytdlp"
)

// DefaultMaxCandidates bounds the fallback candidate evaluation count.
const DefaultMaxCandidates = 5

// CooldownManager coordinates shared rate-limit cooldowns across download workers.
type CooldownManager interface {
	Trigger(provider string, duration time.Duration) time.Duration
	Remaining(provider string) (time.Duration, bool)
	Wait(ctx context.Context, provider string) error
	Clear(provider string)
}

// SessionPool defines the subset of mediasession.SessionPool operations needed by the orchestrator.
type SessionPool interface {
	Acquire(ctx context.Context) (*mediasession.Lease, error)
	ResolveCookiePath(sessionID string) string
	RecordOutcome(sessionID string, err error)
	HasConfiguredSessions() bool
	HasEligibleSession() (bool, time.Duration)
	Availability() mediasession.PoolAvailability
	Sessions() []mediasession.Session
	AcquireDataPlane(ctx context.Context, sessionID string) (func(), error)
	RetainDataPlane(sessionID string)
	ReleaseDataPlane(sessionID string)
	IsInUse(sessionID string) bool
}

// Origin represents the trigger origin of a media resolution request.
type Origin string

const (
	OriginManual       Origin = "manual"
	OriginSubscription Origin = "subscription"
)

type originContextKey struct{}

// WithOrigin returns a context annotated with the resolution trigger origin.
func WithOrigin(ctx context.Context, origin Origin) context.Context {
	return context.WithValue(ctx, originContextKey{}, origin)
}

// OriginFromContext extracts the resolution trigger origin from ctx.
// Defaults to OriginSubscription if not explicitly set to OriginManual.
func OriginFromContext(ctx context.Context) Origin {
	if ctx == nil {
		return OriginSubscription
	}
	if v, ok := ctx.Value(originContextKey{}).(Origin); ok && v == OriginManual {
		return OriginManual
	}
	return OriginSubscription
}

// ResolvedMedia describes the winning candidate and concrete downloadable source
// resolved by ProviderOrchestrator under a specific media session.
type ResolvedMedia struct {
	Candidate      provider.MediaCandidate
	Score          float64
	Source         *provider.MediaSource
	SessionID      string
	AttemptedCount int
}

// Options configures ProviderOrchestrator.
type Options struct {
	Registry    *provider.Registry
	SessionPool SessionPool
	Matcher     *matcher.Matcher
	Cooldown    CooldownManager
	Logger      *slog.Logger
}

// ProviderOrchestrator coordinates media candidate search, candidate matching, and candidate
// resolution across providers and sessions. It manages session leasing, rate limits, session
// affinity, candidate fallback, and systemic error containment.
type ProviderOrchestrator struct {
	registry    *provider.Registry
	sessionPool SessionPool
	matcher     *matcher.Matcher
	cooldown    CooldownManager
	logger      *slog.Logger
}

// New creates a ProviderOrchestrator.
func New(opts Options) *ProviderOrchestrator {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ProviderOrchestrator{
		registry:    opts.Registry,
		sessionPool: opts.SessionPool,
		matcher:     opts.Matcher,
		cooldown:    opts.Cooldown,
		logger:      logger,
	}
}

// ResolveCookiePath returns the filesystem cookie path for an opaque session ID,
// allowing the downloader to use the session credentials without holding a control-plane lease.
func (o *ProviderOrchestrator) ResolveCookiePath(sessionID string) string {
	if o == nil || o.sessionPool == nil || sessionID == "" {
		return ""
	}
	return o.sessionPool.ResolveCookiePath(sessionID)
}

// AcquireDataPlaneLock acquires exclusive data-plane access to sessionID's writable cookie file during download.
func (o *ProviderOrchestrator) AcquireDataPlaneLock(ctx context.Context, sessionID string) (func(), error) {
	if o == nil || o.sessionPool == nil || sessionID == "" {
		return func() {}, nil
	}
	return o.sessionPool.AcquireDataPlane(ctx, sessionID)
}

// RecordDownloadOutcome records the outcome of a download attempt for session health tracking
// and provider family cooldowns. It releases the data-plane in-flight reference count.
func (o *ProviderOrchestrator) RecordDownloadOutcome(ctx context.Context, sessionID string, err error) {
	if o == nil {
		return
	}
	if o.sessionPool != nil && sessionID != "" {
		o.sessionPool.RecordOutcome(sessionID, err)
		o.sessionPool.ReleaseDataPlane(sessionID)
	}
	if err != nil && apperr.ScopeOf(err) == apperr.ScopeProvider && o.cooldown != nil {
		if sessionID != "" {
			fam := provider.FamilyYouTube
			o.logger.Warn("provider-family systemic failure detected during download, triggering cooldown",
				logging.KeyProvider, string(fam),
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
			o.cooldown.Trigger(string(fam), 60*time.Second)
		}
	}
}

// bindProvider returns a copy of p configured with the given cookie path if supported.
// Providers not belonging to FamilyYouTube never receive cookies.
func bindProvider(p provider.MediaProvider, cookiePath string, gate ytdlp.ExecutionGate) provider.MediaProvider {
	if provider.FamilyOf(p.Name()) != provider.FamilyYouTube {
		return p
	}
	if yp, ok := p.(*youtube.MediaProvider); ok {
		return yp.WithCookieFile(cookiePath).WithExecutionGate(gate)
	}
	if saw, ok := p.(interface {
		WithCookieFile(string) provider.MediaProvider
	}); ok {
		return saw.WithCookieFile(cookiePath)
	}
	return p
}

// sessionOutcome records what, if anything, the leased YouTube session itself
// proved during a resolution attempt. It is deliberately independent of the
// error ResolveMedia returns to the caller: a deferral or failure caused by an
// independent provider family must never be charged to the YouTube session.
type sessionOutcome int

const (
	// sessionOutcomeNeutral means the session is not answerable for the result.
	sessionOutcomeNeutral sessionOutcome = iota
	// sessionOutcomeFailure means this session itself failed.
	sessionOutcomeFailure
)

// attemptPlan is the pre-attempt eligibility of every platform family. It is
// decided from in-memory cooldown and session state before any network
// contact.
type attemptPlan struct {
	pref              string
	origin            Origin
	chain             []string
	ytEligible        bool
	ytNoUsableSession bool
	ytRetryAfter      time.Duration
	scEligible        bool
	scRetryAfter      time.Duration
}

// planAttempt resolves the provider chain and the pre-attempt eligibility. A
// non-nil error is the answer ResolveMedia gives without contacting anything.
func (o *ProviderOrchestrator) planAttempt(preferredProvider string, origin Origin) (attemptPlan, error) {
	pref := strings.TrimSpace(preferredProvider)
	if pref == "" && o.registry != nil {
		pref = o.registry.DefaultMediaName()
	}
	if pref == "" {
		pref = "ytmusic"
	}
	plan := attemptPlan{pref: pref, origin: origin, ytEligible: true, scEligible: true}

	// 1. Resolve candidate provider chain
	plan.chain = o.resolveProviderChain(pref)
	if len(plan.chain) == 0 {
		return plan, apperr.New(apperr.CodeProviderNotFound, "no media providers available")
	}

	// 2. Pre-Attempt Eligibility Planning
	// Determine eligibility for each platform family BEFORE network contact.

	// Check YouTube family cooldown
	if o.cooldown != nil {
		if remaining, active := o.cooldown.Remaining(string(provider.FamilyYouTube)); active {
			plan.ytEligible = false
			plan.ytRetryAfter = remaining
		}
	}

	// Check YouTube session pool eligibility
	if o.sessionPool != nil && o.sessionPool.HasConfiguredSessions() {
		avail := o.sessionPool.Availability()
		switch avail.State {
		case mediasession.PoolStateEligible, mediasession.PoolStateCapacityConstrained:
			// Session pool is available or capacity constrained
		case mediasession.PoolStateCooling:
			plan.ytEligible = false
			if avail.RetryAfter > plan.ytRetryAfter {
				plan.ytRetryAfter = avail.RetryAfter
			}
		case mediasession.PoolStateNoUsableSession:
			plan.ytEligible = false
			plan.ytNoUsableSession = true
		}
	}

	// Check SoundCloud family eligibility
	if o.cooldown != nil {
		if remaining, active := o.cooldown.Remaining(string(provider.FamilySoundCloud)); active {
			plan.scEligible = false
			plan.scRetryAfter = remaining
		}
	}

	// Policy check when preferred YouTube family is pre-attempt unavailable:
	if !plan.ytEligible && provider.FamilyOf(pref) == provider.FamilyYouTube {
		if plan.ytNoUsableSession {
			if origin != OriginManual {
				// Subscriptions must not silently substitute independent providers
				// and must not retry forever when no usable session exists.
				return plan, apperr.New(apperr.CodeSessionNotFound, "no eligible media sessions available in pool")
			}
			if !plan.scEligible {
				if plan.scRetryAfter > 0 {
					retryWait := plan.scRetryAfter
					if retryWait < 5*time.Second {
						retryWait = 5 * time.Second
					}
					return plan, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
						"Configured media providers are temporarily unavailable.", retryWait)
				}
				return plan, apperr.New(apperr.CodeSessionNotFound, "no eligible media sessions available in pool")
			}
			// For OriginManual with scEligible, proceed to candidate evaluation loop to attempt SoundCloud!
		} else {
			if origin != OriginManual {
				// Subscriptions must not silently substitute independent providers
				retryWait := plan.ytRetryAfter
				if retryWait < 5*time.Second {
					retryWait = 15 * time.Minute
				}
				return plan, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
					"YouTube acquisition is temporarily paused after a provider protection response.", retryWait)
			}
			if !plan.scEligible {
				// Both YouTube and SoundCloud are unavailable
				retryWait := plan.ytRetryAfter
				if plan.scRetryAfter > 0 && (retryWait == 0 || plan.scRetryAfter < retryWait) {
					retryWait = plan.scRetryAfter
				}
				if retryWait < 5*time.Second {
					retryWait = 15 * time.Minute
				}
				return plan, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
					"Configured media providers are temporarily unavailable.", retryWait)
			}
		}
	}
	return plan, nil
}

// Precheck reports, without any network contact, the answer ResolveMedia
// would give before its first provider request: nil when an attempt would
// contact a provider now, otherwise the same wait or configuration error. The
// dispatcher uses it to leave work alone while its provider is known to be
// unavailable, instead of cycling it through a worker just to learn that.
func (o *ProviderOrchestrator) Precheck(ctx context.Context, preferredProvider string) error {
	if o == nil {
		return nil
	}
	_, err := o.planAttempt(preferredProvider, OriginFromContext(ctx))
	return err
}

// midAttemptCooldown reports a family cooldown that began after the attempt
// was planned, typically because another worker just received a protection
// response. Checking it before every request keeps concurrent attempts from
// sending further requests into an active block.
func (o *ProviderOrchestrator) midAttemptCooldown(fam provider.Family) (time.Duration, bool) {
	if o.cooldown == nil {
		return 0, false
	}
	remaining, active := o.cooldown.Remaining(string(fam))
	if !active {
		return 0, false
	}
	if remaining < 5*time.Second {
		remaining = 5 * time.Second
	}
	return remaining, true
}

// isWaitState reports an error that describes no session's own behaviour: the
// execution gate or a cooldown refused to start a request. It must neither
// count as a session failure nor trigger another cooldown.
func isWaitState(err error) bool {
	return apperr.IsSessionWait(err)
}

// ResolveMedia executes search, candidate matching, and candidate resolution
// with pre-attempt independent provider planning and strict failure containment.
func (o *ProviderOrchestrator) ResolveMedia(ctx context.Context, preferredProvider string, track music.Track, maxCandidates int) (*ResolvedMedia, error) {
	if maxCandidates <= 0 {
		maxCandidates = DefaultMaxCandidates
	}

	origin := OriginFromContext(ctx)
	plan, err := o.planAttempt(preferredProvider, origin)
	if err != nil {
		return nil, err
	}
	pref := plan.pref
	chain := plan.chain
	ytEligible := plan.ytEligible
	ytNoUsableSession := plan.ytNoUsableSession
	ytRetryAfter := plan.ytRetryAfter
	scEligible := plan.scEligible
	scRetryAfter := plan.scRetryAfter

	// 3. Lazy session state for YouTube
	//
	// The lease outcome is tracked separately from the error this call returns.
	// ResolveMedia only resolves a source; successful media acquisition is
	// recorded later by RecordDownloadOutcome after download verification.
	var (
		lease      *mediasession.Lease
		cookiePath string
		sessionID  string
		outcome    = sessionOutcomeNeutral
		sessionErr error
	)
	defer func() {
		if lease == nil {
			return
		}
		switch outcome {
		case sessionOutcomeFailure:
			lease.Release(sessionErr)
		default:
			lease.ReleaseNeutral()
		}
	}()

	acquireYouTubeSession := func() error {
		if lease != nil {
			return nil
		}
		if o.sessionPool == nil || !o.sessionPool.HasConfiguredSessions() {
			return nil
		}
		var err error
		lease, err = o.sessionPool.Acquire(ctx)
		if err != nil {
			return err
		}
		cookiePath = lease.CookiePath()
		sessionID = lease.SessionID()
		return nil
	}

	// Candidate ids already resolved in this attempt, per platform family. The
	// YouTube Music and the YouTube search regularly return the same video, and
	// a failed direct-id candidate is usually the generic search's first hit
	// again. Resolving such a candidate a second time within one attempt cannot
	// change its answer; it only spends session time and provider quota.
	tried := make(map[string]struct{})
	triedKey := func(fam provider.Family, id string) string {
		return string(fam) + "\x00" + strings.TrimSpace(id)
	}

	// 4. Direct-ID Fast Path
	// Only runs if YouTube is eligible and track carries a direct video ID.
	// If YouTube is pre-attempt skipped, fast path is skipped without YouTube contact.
	if ytEligible && track.SourceID != "" && provider.FamilyOf(pref) == provider.FamilyYouTube {
		if err := acquireYouTubeSession(); err != nil {
			return nil, err
		}
		res, ok, triedID, err := o.tryDirectID(ctx, pref, track, lease, cookiePath, sessionID, provider.FamilyYouTube)
		if err != nil {
			// tryDirectID only surfaces session/provider protection failures, and
			// they were produced by this YouTube session - unless the execution
			// gate refused to start the request at all, which proves nothing.
			if !isWaitState(err) {
				outcome, sessionErr = sessionOutcomeFailure, err
			}
			return nil, err
		}
		if ok {
			if o.sessionPool != nil && sessionID != "" {
				o.sessionPool.RetainDataPlane(sessionID)
			}
			return res, nil
		}
		if triedID != "" {
			tried[triedKey(provider.FamilyYouTube, triedID)] = struct{}{}
		}
		o.logger.Info("direct-ID candidate unavailable, falling back to generic search",
			logging.KeyProvider, pref,
			"source_id", track.SourceID,
			logging.KeyTrack, track.Label())
	}

	// 5. Provider Evaluation Loop
	genericTrack := track
	genericTrack.SourceID = ""

	var (
		allAcceptable      []matcher.Result
		attemptedCount     int
		lastResolveErr     error
		bestCandidate      *matcher.Result
		deferredCount      int
		deferredRetryAfter time.Duration
		deferredReason     string
	)

	recordDeferred := func(reason string, retryAfter time.Duration) {
		deferredCount++
		if deferredReason == "" {
			deferredReason = reason
		}
		if retryAfter > 0 {
			if deferredRetryAfter == 0 || retryAfter < deferredRetryAfter {
				deferredRetryAfter = retryAfter
			}
		}
	}

	lastYouTube := -1
	for i, provName := range chain {
		if provider.FamilyOf(provName) == provider.FamilyYouTube {
			lastYouTube = i
		}
	}

	for chainIdx, provName := range chain {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
		}

		provFam := provider.FamilyOf(provName)

		// The YouTube phase is over once no YouTube-family provider follows. A
		// session failure would already have returned, so the session proved
		// nothing either way and its lease goes back neutrally now: an
		// independent provider's search and resolution must not keep the
		// YouTube session away from the other workers.
		if lease != nil && chainIdx > lastYouTube {
			lease.ReleaseNeutral()
			lease, cookiePath, sessionID = nil, "", ""
		}

		// Check pre-attempt skip for this provider:
		if provFam == provider.FamilyYouTube && !ytEligible {
			// YouTube was pre-attempt skipped; do not contact YouTube.
			if !ytNoUsableSession {
				recordDeferred("YouTube session recovery", ytRetryAfter)
			}
			continue
		}
		if provFam == provider.FamilySoundCloud && !scEligible {
			// SoundCloud was pre-attempt skipped.
			recordDeferred("SoundCloud cooldown", scRetryAfter)
			continue
		}
		if o.cooldown != nil && provFam != provider.FamilyYouTube && provFam != provider.FamilySoundCloud {
			if remaining, active := o.cooldown.Remaining(string(provFam)); active {
				recordDeferred(string(provFam)+" cooldown", remaining)
				continue
			}
		}
		if provFam == provider.FamilySoundCloud && !ytEligible && origin != OriginManual &&
			provider.FamilyOf(pref) == provider.FamilyYouTube {
			// Background work must not automatically substitute SoundCloud for an
			// unavailable preferred YouTube family. This is a substitution guard,
			// not a ban: when SoundCloud is itself the preferred provider it is the
			// intended target and subscriptions may use it normally.
			continue
		}

		// A cooldown that began after planning defers the provider instead of
		// parking this worker until it ends: the worker slot is freed for other
		// runnable items and the item is rescheduled for the cooldown's end.
		if provFam != provider.FamilyYouTube {
			if remaining, cooling := o.midAttemptCooldown(provFam); cooling {
				recordDeferred(string(provFam)+" cooldown", remaining)
				continue
			}
		} else if remaining, cooling := o.midAttemptCooldown(provFam); cooling {
			return nil, youTubePaused(remaining)
		}

		// Lazily acquire a YouTube session if entering an eligible YouTube-family provider
		if provFam == provider.FamilyYouTube {
			if err := acquireYouTubeSession(); err != nil {
				return nil, err
			}
		}

		p, err := o.registry.Media(provName)
		if err != nil {
			continue
		}

		bp := bindProvider(p, cookiePath, lease)
		candidates, err := bp.Search(ctx, genericTrack)
		if err != nil {
			if apperr.StopsCandidateFanout(err) {
				if isWaitState(err) {
					return nil, err
				}
				if provFam == provider.FamilyYouTube {
					outcome, sessionErr = sessionOutcomeFailure, err
				}
				o.handleSystemicFailure(err, provFam, provName)
				return nil, err
			}
			lastResolveErr = err
			continue
		}

		if len(candidates) == 0 {
			continue
		}

		acceptable := o.matcher.Acceptable(track, candidates, maxCandidates)
		if len(acceptable) > 0 {
			allAcceptable = append(allAcceptable, acceptable...)
		candidateLoop:
			for rankIdx, candResult := range acceptable {
				candidate := candResult.Candidate
				key := triedKey(provFam, candidate.ID)
				if _, seen := tried[key]; seen && strings.TrimSpace(candidate.ID) != "" {
					continue
				}

				if err := ctx.Err(); err != nil {
					return nil, apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
				}
				if remaining, cooling := o.midAttemptCooldown(provFam); cooling {
					if provFam == provider.FamilyYouTube {
						return nil, youTubePaused(remaining)
					}
					recordDeferred(string(provFam)+" cooldown", remaining)
					break candidateLoop
				}
				attemptedCount++

				source, err := bp.Resolve(ctx, candidate)
				if err == nil {
					if provFam == provider.FamilyYouTube {
						source.SessionID = sessionID
						if o.sessionPool != nil && sessionID != "" {
							o.sessionPool.RetainDataPlane(sessionID)
						}
					} else {
						// An independent provider's success proves nothing about the
						// YouTube session and must not certify its health.
						source.SessionID = ""
					}
					o.logger.Info("media candidate resolved successfully",
						logging.KeyProvider, candidate.Provider,
						"media_id", candidate.ID,
						"rank", rankIdx+1,
						"score", candResult.Score)
					return &ResolvedMedia{
						Candidate:      candidate,
						Score:          candResult.Score,
						Source:         source,
						SessionID:      source.SessionID,
						AttemptedCount: attemptedCount,
					}, nil
				}

				lastResolveErr = err
				tried[key] = struct{}{}

				// Systemic failure: stop candidate fanout immediately
				if apperr.StopsCandidateFanout(err) {
					if isWaitState(err) {
						return nil, err
					}
					if provFam == provider.FamilyYouTube {
						outcome, sessionErr = sessionOutcomeFailure, err
					}
					o.handleSystemicFailure(err, provFam, candidate.Provider)
					return nil, err
				}

				// Candidate-specific failure (e.g. 404 TrackNotFound, format unavailable): continue to next rank
				o.logger.Warn("candidate resolution failed, attempting next rank",
					logging.KeyProvider, candidate.Provider,
					"media_id", candidate.ID,
					"rank", rankIdx+1,
					logging.KeyErrorCode, string(apperr.CodeOf(err)),
					logging.KeyError, err.Error())
			}
		} else {
			ranked := o.matcher.Rank(track, candidates)
			if len(ranked) > 0 && (bestCandidate == nil || ranked[0].Score > bestCandidate.Score) {
				bestCandidate = &ranked[0]
			}
		}
	}

	// If YouTube was skipped (circuit open) and independent providers had NO acceptable match:
	// do NOT accept a low-score substitute and do NOT fail permanently; safely wait for YouTube recovery.
	if !ytEligible && provider.FamilyOf(pref) == provider.FamilyYouTube {
		if ytNoUsableSession {
			return nil, apperr.New(apperr.CodeSessionNotFound, "no eligible media sessions available in pool")
		}
		if ytRetryAfter < 5*time.Second {
			ytRetryAfter = 15 * time.Minute
		}
		return nil, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
			"No acceptable match found on independent providers; waiting for YouTube session recovery.", ytRetryAfter)
	}

	if deferredCount > 0 {
		retryWait := deferredRetryAfter
		if retryWait <= 0 {
			retryWait = 15 * time.Minute
		} else if retryWait < 5*time.Second {
			retryWait = 5 * time.Second
		}
		// SESSION_UNAVAILABLE describes the overall attempt, not this session: the
		// deferral came from an independent provider, so the lease stays neutral.
		return nil, apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
			"Configured media providers are temporarily unavailable; deferred until provider recovery.", retryWait)
	}

	if bestCandidate != nil {
		return nil, apperr.Newf(apperr.CodeMatchFailed,
			"No sufficiently accurate media match found for %q (best score %.1f, required %.1f).",
			track.Label(), bestCandidate.Score, o.matcher.MinScore())
	}

	if lastResolveErr != nil {
		return nil, apperr.Wrapf(apperr.CodeTrackNotFound, lastResolveErr,
			"Keine der %d passenden Quellen konnte aufgelöst werden.", attemptedCount)
	}

	return nil, apperr.Newf(apperr.CodeTrackNotFound, "No media candidates were found for %q.", track.Label())
}

// tryDirectID resolves the video the metadata already names. Besides the
// result it reports the id of a candidate whose resolution failed for reasons
// of its own, so the generic search does not resolve it a second time.
func (o *ProviderOrchestrator) tryDirectID(ctx context.Context, pref string, track music.Track, lease *mediasession.Lease, cookiePath string, sessionID string, fam provider.Family) (*ResolvedMedia, bool, string, error) {
	p, err := o.registry.Media(pref)
	if err != nil {
		p, err = o.registry.Media("youtube")
		if err != nil {
			return nil, false, "", nil
		}
	}

	bp := bindProvider(p, cookiePath, lease)
	candidates, err := bp.Search(ctx, track)
	if err != nil {
		if apperr.StopsCandidateFanout(err) {
			if !isWaitState(err) {
				o.handleSystemicFailure(err, fam, pref)
			}
			return nil, false, "", err
		}
		// Candidate-specific error
		return nil, false, "", nil
	}

	if len(candidates) == 0 {
		return nil, false, "", nil
	}

	directCand := candidates[0]
	source, err := bp.Resolve(ctx, directCand)
	if err == nil {
		source.SessionID = sessionID
		o.logger.Info("direct-ID candidate resolved successfully",
			logging.KeyProvider, directCand.Provider,
			"media_id", directCand.ID)
		return &ResolvedMedia{
			Candidate:      directCand,
			Score:          100.0,
			Source:         source,
			SessionID:      sessionID,
			AttemptedCount: 1,
		}, true, "", nil
	}

	if apperr.StopsCandidateFanout(err) {
		if !isWaitState(err) {
			o.handleSystemicFailure(err, fam, directCand.Provider)
		}
		return nil, false, "", err
	}

	// Candidate-specific resolution failure
	return nil, false, directCand.ID, nil
}

// youTubePaused is the wait answer for a YouTube cooldown that began during
// the attempt. It ends the attempt without charging the session.
func youTubePaused(remaining time.Duration) error {
	return apperr.NewRetryAfter(apperr.CodeSessionUnavailable,
		"YouTube acquisition is temporarily paused after a provider protection response.", remaining)
}

func (o *ProviderOrchestrator) resolveProviderChain(preferred string) []string {
	if o.registry == nil {
		return []string{preferred}
	}
	raw := o.registry.MediaChain(preferred)
	chain := make([]string, 0, len(raw))
	for _, p := range raw {
		chain = append(chain, p.Name())
	}
	return chain
}

func (o *ProviderOrchestrator) handleSystemicFailure(err error, fam provider.Family, provName string) {
	if apperr.ScopeOf(err) == apperr.ScopeProvider && o.cooldown != nil {
		o.logger.Warn("provider-systemic failure encountered, triggering family cooldown",
			logging.KeyProvider, string(fam),
			"origin_provider", provName,
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
		o.cooldown.Trigger(string(fam), 60*time.Second)
	} else if apperr.ScopeOf(err) == apperr.ScopeSession {
		o.logger.Warn("session protection failure encountered, halting candidate fanout",
			logging.KeyProvider, provName,
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
	}
}
