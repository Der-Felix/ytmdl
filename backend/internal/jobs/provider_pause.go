package jobs

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
)

// A provider pause is the queue's answer to a block that concerns a whole
// provider family rather than the item that happened to meet it: a bot
// challenge or a rate limit. Charging such a block to items made every item of
// a large queue spend its retry budget within minutes and end as failed, while
// the workers kept sending requests into the block. Instead the family is
// paused for every worker, the items go back to waiting without spending an
// attempt, and after the pause a single probe item is let through. Only when
// the probe downloads successfully does the family reopen for all workers.

// maxProviderPause bounds a single provider pause.
const maxProviderPause = 60 * time.Minute

// sessionRotationDelay is how long an item waits when the session it used was
// refused but the session pool still holds another usable session. The pool
// has already put the refused session into its own cooldown; the item is
// simply tried again on another session.
const sessionRotationDelay = 30 * time.Second

// protectionPauseSteps escalate the pause after consecutive bot challenges or
// demands to sign in. They clear up slowly, and every request into one
// prolongs it.
var protectionPauseSteps = []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, maxProviderPause}

// rateLimitPauseSteps escalate the pause after consecutive plain rate limits,
// which clear up quickly.
var rateLimitPauseSteps = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute}

// ProviderBlockCodes are the error codes with which a provider refuses work
// for reasons that have nothing to do with the item. Items that failed for
// good with one of them before provider pauses existed can be put back into
// the queue in one step (RetryBlockedFailures).
var ProviderBlockCodes = []apperr.Code{
	apperr.CodeSessionBotChallenge,
	apperr.CodeSessionAuthFailed,
	apperr.CodeSessionRateLimited,
	apperr.CodeSessionUnavailable,
	apperr.CodeProviderRateLimited,
}

// providerBlock reports whether err pauses the provider family, and whether
// the item still spends an attempt on it.
//
//   - SESSION_BOT_CHALLENGE, SESSION_RATE_LIMITED and PROVIDER_RATE_LIMITED
//     are statements about the provider's view of this server, never about
//     the item: pause, no attempt.
//   - SESSION_AUTH_FAILED pauses as well, but still spends the attempt: the
//     sign-in demand can be specific to one item ("login required"), and an
//     item that is refused on its own must not be able to pause the family
//     forever. Its five attempts bound that.
//
// SESSION_UNAVAILABLE is not a block but the wait that an active pause or
// session cooldown already answers with, and PROVIDER_UNAVAILABLE stays an
// ordinary retryable error: it also covers local network trouble and single
// queries that time out, which must keep their attempt budget.
func providerBlock(err error) (pauses bool, consumesAttempt bool) {
	switch apperr.CodeOf(err) {
	case apperr.CodeSessionBotChallenge, apperr.CodeSessionRateLimited, apperr.CodeProviderRateLimited:
		return true, false
	case apperr.CodeSessionAuthFailed:
		return true, true
	default:
		return false, false
	}
}

// pauseSteps returns the escalation table for a block code.
func pauseSteps(code apperr.Code) []time.Duration {
	switch code {
	case apperr.CodeSessionRateLimited, apperr.CodeProviderRateLimited:
		return rateLimitPauseSteps
	default:
		return protectionPauseSteps
	}
}

// pauseDuration is the pause for the level-th consecutive block.
func pauseDuration(code apperr.Code, level int) time.Duration {
	steps := pauseSteps(code)
	if level < 1 {
		level = 1
	}
	if level > len(steps) {
		level = len(steps)
	}
	return steps[level-1]
}

// PausePhase names where a provider pause stands.
type PausePhase string

const (
	// PausePhaseOpen means the family is not paused.
	PausePhaseOpen PausePhase = ""
	// PausePhasePaused means no item of the family is started.
	PausePhasePaused PausePhase = "paused"
	// PausePhaseProbeReady means the pause has run out and the next item is
	// let through as the probe.
	PausePhaseProbeReady PausePhase = "probe_ready"
	// PausePhaseProbing means the probe item runs and every other item waits
	// for its outcome.
	PausePhaseProbing PausePhase = "probing"
)

// familyPause is the state of one paused provider family.
type familyPause struct {
	code      apperr.Code
	level     int
	since     time.Time
	until     time.Time
	probeItem string
}

// providerPauses holds the pause state of every provider family. It is
// process memory only: after a restart the first item meets the block again
// and starts a fresh pause, which costs one request.
type providerPauses struct {
	mu       sync.Mutex
	families map[string]*familyPause
	now      func() time.Time
	logger   func() *slog.Logger
	cooldown func() *MediaCooldownManager
}

func newProviderPauses(now func() time.Time, logger func() *slog.Logger, cooldown func() *MediaCooldownManager) *providerPauses {
	return &providerPauses{
		families: make(map[string]*familyPause),
		now:      now,
		logger:   logger,
		cooldown: cooldown,
	}
}

func (p *providerPauses) log() *slog.Logger {
	if l := p.logger(); l != nil {
		return l
	}
	return slog.Default()
}

// phaseLocked reports the phase of a family's pause; p.mu is held.
func (p *providerPauses) phaseLocked(fp *familyPause, now time.Time) PausePhase {
	switch {
	case fp == nil:
		return PausePhaseOpen
	case now.Before(fp.until):
		return PausePhasePaused
	case fp.probeItem != "":
		return PausePhaseProbing
	default:
		return PausePhaseProbeReady
	}
}

// Phase reports where the pause of family stands.
func (p *providerPauses) Phase(family string) PausePhase {
	family = CanonicalCooldownKey(family)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phaseLocked(p.families[family], p.now())
}

// Block records that the family refused work with code and returns when the
// family reopens for a probe. The first block starts a pause; a block met
// after the pause ran out - normally by the probe - escalates it. Blocks that
// items already in flight report while a pause is running neither escalate
// nor extend it: they are the same event seen by several workers.
func (p *providerPauses) Block(family string, code apperr.Code, itemID string) time.Time {
	family = CanonicalCooldownKey(family)
	now := p.now()

	p.mu.Lock()
	fp := p.families[family]
	if fp != nil && now.Before(fp.until) {
		until := fp.until
		p.mu.Unlock()
		return until
	}

	escalated := fp != nil
	if fp == nil {
		fp = &familyPause{since: now}
		p.families[family] = fp
	}
	fp.level++
	fp.code = code
	fp.probeItem = ""
	pause := pauseDuration(code, fp.level)
	fp.until = now.Add(pause)
	level, since, until := fp.level, fp.since, fp.until
	p.mu.Unlock()

	if cd := p.cooldown(); cd != nil {
		cd.Hold(family, pause)
	}

	attrs := []any{
		logging.KeyProvider, family,
		logging.KeyErrorCode, string(code),
		"pause_ms", pause.Milliseconds(),
		"resumes_at", until.UTC().Format(time.RFC3339),
		"level", level,
	}
	if escalated {
		attrs = append(attrs, "paused_since", since.UTC().Format(time.RFC3339), "probe_item", itemID)
		p.log().Warn("provider pause escalated: the probe met the block again", attrs...)
	} else {
		p.log().Warn("provider paused for all workers", attrs...)
	}
	return until
}

// Succeeded records a verified download from the family. It ends a pause whose
// time has run out - the probe succeeded - and resets the escalation. A
// download that finishes while a pause is still running was started before
// the block and proves nothing about it.
func (p *providerPauses) Succeeded(family string) {
	family = CanonicalCooldownKey(family)
	now := p.now()

	p.mu.Lock()
	fp := p.families[family]
	if fp == nil || now.Before(fp.until) {
		p.mu.Unlock()
		return
	}
	delete(p.families, family)
	p.mu.Unlock()

	p.log().Info("provider pause ended: the probe succeeded",
		logging.KeyProvider, family,
		logging.KeyErrorCode, string(fp.code),
		"paused_ms", now.Sub(fp.since).Milliseconds(),
		"level", fp.level)
}

// Admit decides whether an item of family may start now. An open family
// admits everything. A running pause admits nothing, except an item that may
// fall back to an independent provider (fallback). After the pause, exactly
// one item is admitted as the probe; the others wait for its outcome.
func (p *providerPauses) Admit(family, itemID string, fallback bool) bool {
	family = CanonicalCooldownKey(family)
	now := p.now()

	p.mu.Lock()
	fp := p.families[family]
	switch p.phaseLocked(fp, now) {
	case PausePhaseOpen:
		p.mu.Unlock()
		return true
	case PausePhasePaused:
		p.mu.Unlock()
		return fallback
	case PausePhaseProbing:
		p.mu.Unlock()
		return false
	}
	fp.probeItem = itemID
	code, level := fp.code, fp.level
	p.mu.Unlock()

	p.log().Info("provider pause over, probing with a single item",
		logging.KeyProvider, family,
		logging.KeyErrorCode, string(code),
		logging.KeyJobItemID, itemID,
		"level", level)
	return true
}

// Finished releases the probe slot when the probe item ended without a
// verdict - neither a verified download nor a block, for example a track
// without any match. The next item becomes the probe.
func (p *providerPauses) Finished(family, itemID string) {
	family = CanonicalCooldownKey(family)
	p.mu.Lock()
	defer p.mu.Unlock()
	if fp := p.families[family]; fp != nil && fp.probeItem == itemID {
		fp.probeItem = ""
	}
}

// ProbeNow ends a running pause early: the family's cooldown is cleared and
// the next item is let through as the probe. The escalation level stays, so a
// probe that meets the block again pauses for the next, longer step. It
// reports whether a pause existed.
func (p *providerPauses) ProbeNow(family string) bool {
	family = CanonicalCooldownKey(family)
	now := p.now()

	p.mu.Lock()
	fp := p.families[family]
	if fp == nil {
		p.mu.Unlock()
		return false
	}
	ended := now.Before(fp.until)
	if ended {
		fp.until = now
	}
	fp.probeItem = ""
	code, level := fp.code, fp.level
	p.mu.Unlock()

	if cd := p.cooldown(); cd != nil {
		cd.Clear(family)
	}
	p.log().Info("provider pause ended early on request, probing with a single item",
		logging.KeyProvider, family,
		logging.KeyErrorCode, string(code),
		"level", level)
	return true
}

// pauseSnapshot is one family's pause as the queue summary reports it.
type pauseSnapshot struct {
	family string
	code   apperr.Code
	level  int
	since  time.Time
	until  time.Time
	phase  PausePhase
}

// Snapshot lists the families that are paused or waiting for their probe.
func (p *providerPauses) Snapshot() []pauseSnapshot {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]pauseSnapshot, 0, len(p.families))
	for family, fp := range p.families {
		out = append(out, pauseSnapshot{
			family: family,
			code:   fp.code,
			level:  fp.level,
			since:  fp.since,
			until:  fp.until,
			phase:  p.phaseLocked(fp, now),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].family < out[j].family })
	return out
}

// providerPauses returns the manager's pause state, created on first use so
// that managers built directly in tests have one too.
func (m *Manager) providerPauses() *providerPauses {
	m.pausesOnce.Do(func() {
		m.pauses = newProviderPauses(m.now,
			func() *slog.Logger { return m.logger },
			func() *MediaCooldownManager { return m.cooldown })
	})
	return m.pauses
}

// providerPauseViews describes the running provider pauses for the queue
// summary. A family that the session pool or a cooldown still holds after its
// pause ran out is reported with the later resume time, because the probe
// waits for it. When the default provider is held without a pause of the
// queue's own - every session cooling, for example - that wait is reported
// too: it stops the queue just the same.
func (m *Manager) providerPauseViews(ctx context.Context) []ProviderPause {
	now := m.now()
	loc := m.displayLocation()
	snapshots := m.providerPauses().Snapshot()
	views := make([]ProviderPause, 0, len(snapshots)+1)
	seen := make(map[string]bool, len(snapshots))

	for _, s := range snapshots {
		seen[s.family] = true
		view := ProviderPause{
			Provider:   s.family,
			ReasonCode: string(s.code),
			Phase:      s.phase,
			Since:      s.since,
			Level:      s.level,
		}
		resumes := s.until
		if s.phase != PausePhaseProbing {
			if wait, held := m.familyWait(ctx, s.family); held && now.Add(wait).After(resumes) {
				resumes = now.Add(wait)
				view.Phase = PausePhasePaused
			}
		}
		if view.Phase == PausePhasePaused {
			view.ResumesAt = &resumes
		}
		view.Message = providerPauseMessage(view, loc, now)
		views = append(views, view)
	}

	if family := m.mediaFamilyOf(Job{}); family != "" && !seen[family] {
		if wait, held := m.familyWait(ctx, family); held {
			resumes := now.Add(wait)
			view := ProviderPause{
				Provider:   family,
				ReasonCode: string(apperr.CodeSessionUnavailable),
				Phase:      PausePhasePaused,
				ResumesAt:  &resumes,
				Since:      now,
			}
			view.Message = providerPauseMessage(view, loc, now)
			views = append(views, view)
		}
	}
	return views
}

// familyWait asks the orchestrator, without contacting anything, whether an
// attempt for family would wait right now, and for how long.
func (m *Manager) familyWait(ctx context.Context, family string) (time.Duration, bool) {
	checker, ok := m.getOrchestrator().(providerPrechecker)
	if !ok {
		return 0, false
	}
	err := checker.Precheck(ctx, family)
	if !apperr.IsSessionWait(err) {
		return 0, false
	}
	wait, _ := apperr.RetryAfter(err)
	return wait, true
}

// providerDisplayName is the name a provider family is shown under.
func providerDisplayName(family string) string {
	switch family {
	case "youtube":
		return "YouTube"
	case "soundcloud":
		return "SoundCloud"
	default:
		return family
	}
}

// pauseReasonText says in German what the provider answered.
func pauseReasonText(name string, code string) string {
	switch apperr.Code(code) {
	case apperr.CodeSessionBotChallenge:
		return name + " verlangt eine Bot-Prüfung."
	case apperr.CodeSessionAuthFailed:
		return name + " verlangt eine Anmeldung."
	case apperr.CodeSessionRateLimited, apperr.CodeProviderRateLimited:
		return name + " begrenzt gerade die Anfragen."
	default:
		return name + " nimmt gerade keine Anfragen an."
	}
}

// formatResume renders a resume time: the clock time on the same day,
// otherwise date and clock time.
func formatResume(t time.Time, loc *time.Location, now time.Time) string {
	local, today := t.In(loc), now.In(loc)
	if local.Year() == today.Year() && local.YearDay() == today.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("02.01. 15:04")
}

// providerPauseMessage explains a pause in German.
func providerPauseMessage(p ProviderPause, loc *time.Location, now time.Time) string {
	name := providerDisplayName(p.Provider)
	reason := pauseReasonText(name, p.ReasonCode)
	switch {
	case p.Phase == PausePhaseProbing:
		return reason + " Ein einzelner Download prüft gerade, ob die Sperre aufgehoben ist."
	case p.Phase == PausePhaseProbeReady || p.ResumesAt == nil:
		return reason + " Der nächste Download prüft, ob die Sperre aufgehoben ist."
	default:
		return reason + " Downloads pausieren bis " + formatResume(*p.ResumesAt, loc, now) + ", dann wird erneut geprüft."
	}
}

// providerPauseETAText is the short state shown in place of an estimate.
func providerPauseETAText(p ProviderPause, loc *time.Location, now time.Time) string {
	name := providerDisplayName(p.Provider)
	if p.Phase == PausePhasePaused && p.ResumesAt != nil {
		return name + " pausiert bis " + formatResume(*p.ResumesAt, loc, now)
	}
	return name + " wird erneut geprüft"
}

// ProbeProviderNow ends the running pause of the default media provider early
// and lets a single probe item through. Cooldowns the queue set are cleared;
// the cooldowns of individual sessions are the session pool's own and stay -
// a session is checked again on the sessions page. It reports whether a pause
// of the queue's own was running.
func (m *Manager) ProbeProviderNow() bool {
	family := m.mediaFamilyOf(Job{})
	if family == "" {
		family = CanonicalCooldownKey("youtube")
	}
	ended := m.providerPauses().ProbeNow(family)
	if !ended && m.cooldown != nil {
		m.cooldown.Clear(family)
	}
	m.signal()
	return ended
}

// FailedItemFilter selects failed items across all jobs.
type FailedItemFilter struct {
	// Codes selects items whose error code is one of these.
	Codes []string
	// ZeroScoreMatchFailures also selects MATCH_FAILED items whose best
	// candidate scored 0.0. During a block that was how a refused extraction
	// surfaced, but a genuine artist mismatch reads the same, so they are only
	// selected on request.
	ZeroScoreMatchFailures bool
}

// BlockedFailureFilter selects the items that failed for good with one of
// the ProviderBlockCodes.
func BlockedFailureFilter() FailedItemFilter {
	codes := make([]string, len(ProviderBlockCodes))
	for i, code := range ProviderBlockCodes {
		codes[i] = string(code)
	}
	return FailedItemFilter{Codes: codes}
}

// RetryBlockedResult reports what RetryBlockedFailures put back.
type RetryBlockedResult struct {
	Items int `json:"items"`
	Jobs  int `json:"jobs"`
}

// RetryBlockedFailures puts every item that failed for good because a
// provider blocked the server back into the queue with a fresh attempt
// budget, across all jobs. With includeZeroScoreMatches it also requeues
// MATCH_FAILED items whose best candidate scored 0.0 (see FailedItemFilter).
// It never runs on its own; the owner starts it.
func (m *Manager) RetryBlockedFailures(ctx context.Context, includeZeroScoreMatches bool) (RetryBlockedResult, error) {
	filter := BlockedFailureFilter()
	filter.ZeroScoreMatchFailures = includeZeroScoreMatches
	items, jobs, err := m.store.ResetFailedItems(ctx, filter)
	if err != nil {
		return RetryBlockedResult{}, err
	}
	if items > 0 && m.logger != nil {
		m.logger.Info("blocked failures requeued",
			"items", items, "jobs", jobs, "zero_score_matches", includeZeroScoreMatches)
	}
	m.signal()
	return RetryBlockedResult{Items: items, Jobs: jobs}, nil
}

// mediaFamilyOf names the provider family a job's items are paused under.
func (m *Manager) mediaFamilyOf(job Job) string {
	return CanonicalCooldownKey(m.mediaProviderOf(job))
}
