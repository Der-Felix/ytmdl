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
	Sessions() []mediasession.Session
	AcquireDataPlane(ctx context.Context, sessionID string) (func(), error)
	RetainDataPlane(sessionID string)
	ReleaseDataPlane(sessionID string)
	IsInUse(sessionID string) bool
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
		fam := provider.FamilyYouTube
		o.logger.Warn("provider-family systemic failure detected during download, triggering cooldown",
			logging.KeyProvider, string(fam),
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
		o.cooldown.Trigger(string(fam), 60*time.Second)
	}
}

// bindProvider returns a copy of p configured with the given cookie path if supported.
func bindProvider(p provider.MediaProvider, cookiePath string) provider.MediaProvider {
	if yp, ok := p.(*youtube.MediaProvider); ok {
		return yp.WithCookieFile(cookiePath)
	}
	if saw, ok := p.(interface {
		WithCookieFile(string) provider.MediaProvider
	}); ok {
		return saw.WithCookieFile(cookiePath)
	}
	return p
}

// ResolveMedia executes search, candidate matching, and candidate resolution
// under a leased media session with strict failure containment and session affinity.
func (o *ProviderOrchestrator) ResolveMedia(ctx context.Context, preferredProvider string, track music.Track, maxCandidates int) (*ResolvedMedia, error) {
	if maxCandidates <= 0 {
		maxCandidates = DefaultMaxCandidates
	}

	pref := strings.TrimSpace(preferredProvider)
	if pref == "" && o.registry != nil {
		pref = o.registry.DefaultMediaName()
	}
	if pref == "" {
		pref = "ytmusic"
	}

	fam := provider.FamilyOf(pref)

	// 1. Check family-level cooldown before acquiring any session
	if o.cooldown != nil {
		if err := o.cooldown.Wait(ctx, string(fam)); err != nil {
			return nil, err
		}
	}

	// 2. Session Acquisition
	var (
		lease      *mediasession.Lease
		cookiePath string
		sessionID  string
		leaseErr   error
	)

	if o.sessionPool != nil && o.sessionPool.HasConfiguredSessions() {
		var err error
		lease, err = o.sessionPool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		cookiePath = lease.CookiePath()
		sessionID = lease.SessionID()
		defer func() {
			lease.Release(leaseErr)
		}()
	}

	// 3. Direct-ID Fast Path (if track carries a direct video ID)
	if track.SourceID != "" {
		res, ok, err := o.tryDirectID(ctx, pref, track, cookiePath, sessionID, fam)
		if err != nil {
			leaseErr = err
			return nil, err
		}
		if ok {
			if o.sessionPool != nil && sessionID != "" {
				o.sessionPool.RetainDataPlane(sessionID)
			}
			leaseErr = nil
			return res, nil
		}
		// Direct-ID failed with candidate-specific error (e.g. video unavailable) -> allow generic catalog fallback
		o.logger.Info("direct-ID candidate unavailable, falling back to generic search",
			logging.KeyProvider, pref,
			"source_id", track.SourceID,
			logging.KeyTrack, track.Label())
	}

	// 4. Provider Ordering and Candidate Search
	chain := o.resolveProviderChain(pref)
	if len(chain) == 0 {
		leaseErr = apperr.New(apperr.CodeProviderNotFound, "no media providers available")
		return nil, leaseErr
	}

	genericTrack := track
	genericTrack.SourceID = ""

	var (
		allAcceptable  []matcher.Result
		attemptedCount int
		lastResolveErr error
		bestCandidate  *matcher.Result
	)

	for _, provName := range chain {
		if err := ctx.Err(); err != nil {
			leaseErr = apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
			return nil, leaseErr
		}

		if o.cooldown != nil {
			if err := o.cooldown.Wait(ctx, provName); err != nil {
				leaseErr = err
				return nil, leaseErr
			}
		}

		p, err := o.registry.Media(provName)
		if err != nil {
			continue
		}

		bp := bindProvider(p, cookiePath)
		candidates, err := bp.Search(ctx, genericTrack)
		if err != nil {
			if apperr.StopsCandidateFanout(err) {
				leaseErr = err
				o.handleSystemicFailure(err, fam, provName)
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
			// Evaluate resolved sources for acceptable candidates
			for rankIdx, candResult := range acceptable {
				attemptedCount++
				candidate := candResult.Candidate

				if err := ctx.Err(); err != nil {
					leaseErr = apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
					return nil, leaseErr
				}

				source, err := bp.Resolve(ctx, candidate)
				if err == nil {
					source.SessionID = sessionID
					if o.sessionPool != nil && sessionID != "" {
						o.sessionPool.RetainDataPlane(sessionID)
					}
					leaseErr = nil
					o.logger.Info("media candidate resolved successfully",
						logging.KeyProvider, candidate.Provider,
						"media_id", candidate.ID,
						"rank", rankIdx+1,
						"score", candResult.Score)
					return &ResolvedMedia{
						Candidate:      candidate,
						Score:          candResult.Score,
						Source:         source,
						SessionID:      sessionID,
						AttemptedCount: attemptedCount,
					}, nil
				}

				lastResolveErr = err

				// Systemic failure: stop candidate fanout immediately
				if apperr.StopsCandidateFanout(err) {
					leaseErr = err
					o.handleSystemicFailure(err, fam, candidate.Provider)
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

	if bestCandidate != nil {
		leaseErr = apperr.Newf(apperr.CodeMatchFailed,
			"No sufficiently accurate media match found for %q (best score %.1f, required %.1f).",
			track.Label(), bestCandidate.Score, o.matcher.MinScore())
		return nil, leaseErr
	}

	if lastResolveErr != nil {
		leaseErr = lastResolveErr
		return nil, apperr.Wrapf(apperr.CodeTrackNotFound, lastResolveErr,
			"Keine der %d passenden Quellen konnte aufgelöst werden.", attemptedCount)
	}

	leaseErr = apperr.Newf(apperr.CodeTrackNotFound, "No media candidates were found for %q.", track.Label())
	return nil, leaseErr
}

func (o *ProviderOrchestrator) tryDirectID(ctx context.Context, pref string, track music.Track, cookiePath string, sessionID string, fam provider.Family) (*ResolvedMedia, bool, error) {
	p, err := o.registry.Media(pref)
	if err != nil {
		p, err = o.registry.Media("youtube")
		if err != nil {
			return nil, false, nil
		}
	}

	bp := bindProvider(p, cookiePath)
	candidates, err := bp.Search(ctx, track)
	if err != nil {
		if apperr.StopsCandidateFanout(err) {
			o.handleSystemicFailure(err, fam, pref)
			return nil, false, err
		}
		// Candidate-specific error
		return nil, false, nil
	}

	if len(candidates) == 0 {
		return nil, false, nil
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
		}, true, nil
	}

	if apperr.StopsCandidateFanout(err) {
		o.handleSystemicFailure(err, fam, directCand.Provider)
		return nil, false, err
	}

	// Candidate-specific resolution failure
	return nil, false, nil
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
