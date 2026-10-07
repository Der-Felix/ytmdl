package orchestrator

import "ytdm/backend/internal/apperr"

// candidateFailures summarises one attempt without changing fanout, session
// rotation or protection-error precedence. A specific cause is reported only
// when every failed candidate agrees; mixed or unknown causes stay generic.
type candidateFailures struct {
	formatErr error
	reason    apperr.Code
	recorded  bool
	mixed     bool
}

func (f *candidateFailures) record(err error) {
	if err == nil {
		return
	}
	code := apperr.CodeOf(err)
	if f.formatErr == nil && code == apperr.CodeUnsupportedMediaFormat {
		f.formatErr = err
	}
	if f.recorded && f.reason != code {
		f.mixed = true
	}
	if !f.recorded {
		f.reason = code
		f.recorded = true
	}
}

func (f *candidateFailures) exhausted(attempted int, lastErr error) error {
	if f.formatErr != nil {
		return apperr.Wrapf(apperr.CodeUnsupportedMediaFormat, f.formatErr,
			"Keine der %d passenden Quellen bot ein unterstütztes Audioformat.", attempted)
	}
	code := apperr.CodeTrackNotFound
	if !f.mixed {
		switch f.reason {
		case apperr.CodeMediaAgeRestricted, apperr.CodeMediaPremiumRequired, apperr.CodeMediaUnavailable:
			code = f.reason
		}
	}
	return apperr.Wrapf(code, lastErr,
		"Keine der %d passenden Quellen konnte aufgelöst werden.", attempted)
}
