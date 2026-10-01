package orchestrator

import (
	"ytdm/backend/internal/apperr"
)

// candidateFailures collects what went wrong across the candidates of one
// attempt and turns it into the single error the item ends with.
//
// Session, bot and rate-limit failures never reach this point: they stop the
// fanout and return on their own, which keeps their precedence untouched.
type candidateFailures struct {
	formatErr error
}

// record files one candidate failure.
func (f *candidateFailures) record(err error) {
	if err != nil && f.formatErr == nil && apperr.CodeOf(err) == apperr.CodeUnsupportedMediaFormat {
		f.formatErr = err
	}
}

// exhausted builds the error of an attempt whose candidates are used up.
//
// An exhausted fanout ends permanently, exactly as it always did: every
// candidate failure is summarised as TRACK_NOT_FOUND with the same wording,
// whatever its own code was. No retry is derived from a candidate failure
// here - the search and resolution answers are reused from the query cache
// for longer than the retry backoff runs, so a retry would only replay them.
//
// The one refinement concerns the combined-stream fallback. Only when it is
// switched on can a candidate fail with UNSUPPORTED_MEDIA_FORMAT; the attempt
// then ends with that code, still permanently, so operators can tell an item
// whose sources offered no usable format apart from one whose sources were
// gone. With the fallback off the result is unchanged from before.
func (f *candidateFailures) exhausted(attempted int, lastErr error) error {
	if f.formatErr != nil {
		return apperr.Wrapf(apperr.CodeUnsupportedMediaFormat, f.formatErr,
			"Keine der %d passenden Quellen bot ein unterstütztes Audioformat.", attempted)
	}
	return apperr.Wrapf(apperr.CodeTrackNotFound, lastErr,
		"Keine der %d passenden Quellen konnte aufgelöst werden.", attempted)
}
