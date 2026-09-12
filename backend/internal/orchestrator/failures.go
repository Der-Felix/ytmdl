package orchestrator

import (
	"ytdm/backend/internal/apperr"
)

// failureClass sorts the reasons a candidate can fail by what they say about
// trying again. The order matters: a higher class describes a condition more
// likely to pass, and the class of the whole attempt is the highest class any
// of its candidates reached.
type failureClass int

const (
	// classAbsent: the source is genuinely not there, not accessible, or does
	// not match. Trying the same candidates again cannot change that.
	classAbsent failureClass = iota
	// classFormat: the platform answered and the item exists, but the format
	// answer is one this backend cannot turn into a stored audio file. That is
	// a property of the answer, and answers change - so it is retried, inside
	// the item's ordinary attempt budget and never beyond it.
	classFormat
	// classTransient: a provider, transport or verification failure. It is
	// retried the same way it always was.
	classTransient
)

// classify sorts one candidate failure. Session, bot and rate-limit failures
// never reach this point: they stop the fanout and return on their own, which
// keeps their existing precedence untouched.
//
// An unknown code is treated as absent rather than transient: a failure whose
// meaning is not established must not keep an item cycling through workers.
func classify(err error) failureClass {
	switch apperr.CodeOf(err) {
	case apperr.CodeUnsupportedMediaFormat:
		return classFormat
	case apperr.CodeProviderUnavailable, apperr.CodeProviderRateLimited,
		apperr.CodeDownloadFailed, apperr.CodeMediaVerifyFailed,
		apperr.CodeSessionUnavailable, apperr.CodeSessionRateLimited,
		apperr.CodeSessionBotChallenge, apperr.CodeSessionAuthFailed:
		return classTransient
	default:
		return classAbsent
	}
}

// candidateFailures collects what went wrong across the candidates of one
// attempt and turns it into the single error the item ends with.
type candidateFailures struct {
	counts   [3]int
	worst    failureClass
	worstErr error
}

// record files one candidate failure.
func (f *candidateFailures) record(err error) {
	if err == nil {
		return
	}
	class := classify(err)
	f.counts[class]++
	if f.worstErr == nil || class > f.worst {
		f.worst, f.worstErr = class, err
	}
}

// exhausted builds the error of an attempt whose candidates are used up.
//
// The rule for mixed failures is that the most retryable class wins. If any
// candidate failed for a reason that may pass, the item keeps a bounded chance
// rather than being written off because another candidate was hopeless. Only
// when every candidate was hopeless does the item fail permanently.
func (f *candidateFailures) exhausted(attempted int, lastErr error) error {
	cause := f.worstErr
	if cause == nil {
		cause = lastErr
	}
	switch f.worst {
	case classTransient:
		// The transient failure keeps its own code, and with it its retry
		// timing; only the context of the exhausted fanout is added.
		return apperr.Wrapf(apperr.CodeOf(cause), cause,
			"Keine der %d passenden Quellen konnte aufgelöst werden; mindestens eine scheiterte vorübergehend.", attempted)
	case classFormat:
		return apperr.Wrapf(apperr.CodeUnsupportedMediaFormat, cause,
			"Keine der %d passenden Quellen bot ein unterstütztes Audioformat.", attempted)
	default:
		// Unchanged wording and code: every candidate was genuinely
		// unavailable, and that stays a permanent result.
		return apperr.Wrapf(apperr.CodeTrackNotFound, cause,
			"Keine der %d passenden Quellen konnte aufgelöst werden.", attempted)
	}
}
