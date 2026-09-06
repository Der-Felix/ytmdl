// Package apperr defines the machine readable error codes used across the
// backend and the HTTP API. Errors carry a stable code so that clients can
// react programmatically instead of parsing free text messages.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine readable error identifier.
type Code string

const (
	CodeProviderUnavailable  Code = "PROVIDER_UNAVAILABLE"
	CodeProviderRateLimited  Code = "PROVIDER_RATE_LIMITED"
	CodeProviderNotFound     Code = "PROVIDER_NOT_FOUND"
	CodeArtistNotFound       Code = "ARTIST_NOT_FOUND"
	CodeReleaseNotFound      Code = "RELEASE_NOT_FOUND"
	CodeTrackNotFound        Code = "TRACK_NOT_FOUND"
	CodeJobNotFound          Code = "JOB_NOT_FOUND"
	CodeSubscriptionNotFound Code = "SUBSCRIPTION_NOT_FOUND"
	CodeFileNotFound         Code = "FILE_NOT_FOUND"
	CodeMatchFailed          Code = "MATCH_FAILED"
	CodeDownloadFailed       Code = "DOWNLOAD_FAILED"
	CodeInvalidAudio         Code = "INVALID_AUDIO"
	CodeTaggingFailed        Code = "TAGGING_FAILED"
	CodeAlreadyExists        Code = "ALREADY_EXISTS"
	CodePathConflict         Code = "PATH_CONFLICT"
	CodeConflict             Code = "STALE_REPAIR"
	CodeJobCancelled         Code = "JOB_CANCELLED"
	CodeInvalidRequest       Code = "INVALID_REQUEST"
	CodeUnsupportedMediaType Code = "UNSUPPORTED_MEDIA_TYPE"
	CodeToolUnavailable      Code = "TOOL_UNAVAILABLE"
	CodeUnauthenticated      Code = "UNAUTHENTICATED"
	CodeForbidden            Code = "FORBIDDEN"
	CodeInvalidCredentials   Code = "INVALID_CREDENTIALS"
	CodeUserNotFound         Code = "USER_NOT_FOUND"
	CodeSessionNotFound      Code = "SESSION_NOT_FOUND"
	CodeSessionInUse         Code = "SESSION_IN_USE"
	CodeLastAdmin            Code = "LAST_ADMIN"
	CodeCSRFInvalid          Code = "CSRF_INVALID"
	CodeRateLimited          Code = "RATE_LIMITED"
	CodeSetupRequired        Code = "SETUP_REQUIRED"
	CodeSetupCompleted       Code = "SETUP_COMPLETED"
	CodeShuttingDown         Code = "SHUTTING_DOWN"
	CodeStorageUnavailable   Code = "STORAGE_UNAVAILABLE"
	CodeStorageGuardMismatch Code = "STORAGE_GUARD_MISMATCH"
	CodeStorageReadOnly      Code = "STORAGE_READ_ONLY"
	CodeStorageLowSpace      Code = "STORAGE_LOW_SPACE"
	CodeStagingLowSpace      Code = "STAGING_LOW_SPACE"
	CodeMediaVerifyFailed    Code = "MEDIA_VERIFY_FAILED"
	CodeSessionAuthFailed    Code = "SESSION_AUTH_FAILED"
	CodeSessionBotChallenge  Code = "SESSION_BOT_CHALLENGE"
	CodeSessionRateLimited   Code = "SESSION_RATE_LIMITED"
	CodeInternal             Code = "INTERNAL_ERROR"
)

// Error is an application error with a stable code and a human readable
// message. The wrapped cause is kept for logging but never exposed over HTTP.
type Error struct {
	Code    Code
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.cause }

// New builds an application error without a cause.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Newf builds an application error with a formatted message.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap attaches a code and message to an existing error.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// Wrapf attaches a code and a formatted message to an existing error.
func Wrapf(code Code, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), cause: cause}
}

// CodeOf reports the code of err, or CodeInternal when err is not an
// application error. A nil error yields an empty code.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}

// MessageOf returns the public message of err.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "An unexpected internal error occurred."
}

// HTTPStatus maps an error code onto the HTTP status the API should answer
// with. Unknown codes are treated as internal errors.
func HTTPStatus(code Code) int {
	switch code {
	case CodeInvalidRequest:
		return http.StatusBadRequest
	case CodeUnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case CodeArtistNotFound, CodeReleaseNotFound, CodeTrackNotFound,
		CodeJobNotFound, CodeSubscriptionNotFound, CodeProviderNotFound,
		CodeFileNotFound, CodeUserNotFound, CodeSessionNotFound:
		return http.StatusNotFound

	case CodeUnauthenticated, CodeInvalidCredentials:
		return http.StatusUnauthorized
	case CodeForbidden, CodeCSRFInvalid:
		return http.StatusForbidden
	case CodeAlreadyExists, CodeLastAdmin, CodeSetupCompleted, CodePathConflict, CodeConflict, CodeSessionInUse:
		return http.StatusConflict
	case CodeSetupRequired:
		return http.StatusPreconditionRequired
	case CodeProviderRateLimited, CodeRateLimited, CodeSessionRateLimited:
		return http.StatusTooManyRequests
	case CodeProviderUnavailable, CodeToolUnavailable, CodeStorageUnavailable, CodeStorageGuardMismatch, CodeStorageReadOnly,
		CodeSessionAuthFailed, CodeSessionBotChallenge:
		return http.StatusBadGateway
	case CodeStorageLowSpace, CodeStagingLowSpace:
		return http.StatusInsufficientStorage
	case CodeShuttingDown:
		return http.StatusServiceUnavailable
	case CodeMatchFailed, CodeMediaVerifyFailed:
		return http.StatusUnprocessableEntity
	case CodeJobCancelled:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// Retryable reports whether an operation that failed with this error has a
// realistic chance of succeeding when repeated. Storage and space errors are
// non-penalized wait states, not standard retryable failures.
func Retryable(err error) bool {
	switch CodeOf(err) {
	case CodeProviderUnavailable, CodeProviderRateLimited, CodeDownloadFailed, CodeMediaVerifyFailed,
		CodeSessionRateLimited, CodeSessionBotChallenge, CodeSessionAuthFailed:
		return true
	default:
		return false
	}
}

// Scope categorizes the operational blast radius and fallback behavior of an error.
type Scope string

const (
	ScopeCandidate      Scope = "candidate"
	ScopeSession        Scope = "session"
	ScopeProvider       Scope = "provider"
	ScopeInfrastructure Scope = "infrastructure"
)

// ScopeOf reports the operational scope of an error.
func ScopeOf(err error) Scope {
	switch CodeOf(err) {
	case CodeTrackNotFound, CodeMatchFailed, CodeInvalidAudio, CodeUnsupportedMediaType:
		return ScopeCandidate
	case CodeSessionAuthFailed, CodeSessionBotChallenge, CodeSessionRateLimited:
		return ScopeSession
	case CodeProviderRateLimited, CodeProviderUnavailable, CodeProviderNotFound:
		return ScopeProvider
	case CodeStorageUnavailable, CodeStorageGuardMismatch, CodeStorageReadOnly,
		CodeStorageLowSpace, CodeStagingLowSpace, CodeToolUnavailable,
		CodeShuttingDown, CodeInternal:
		return ScopeInfrastructure
	default:
		return ScopeCandidate
	}
}

// AllowsCandidateFallback reports whether trying an alternate candidate makes sense.
// Candidate-specific failures allow fallback; session, provider, and infrastructure failures do not.
func AllowsCandidateFallback(err error) bool {
	return ScopeOf(err) == ScopeCandidate
}

// StopsCandidateFanout reports whether candidate evaluation on this session/provider should abort immediately.
// Session-specific and provider-systemic failures stop candidate fan-out.
func StopsCandidateFanout(err error) bool {
	s := ScopeOf(err)
	return s == ScopeSession || s == ScopeProvider
}

// ConsumesJobRetry reports whether an error should consume the standard job/item retry budget.
// Infrastructure wait states (storage/space/shutdown) must not penalize job retries.
func ConsumesJobRetry(err error) bool {
	return ScopeOf(err) != ScopeInfrastructure
}

// IsStorageWait reports whether an error indicates that the library storage
// is unavailable, mismatched, or read-only and requires pausing without consuming retries.
func IsStorageWait(err error) bool {
	switch CodeOf(err) {
	case CodeStorageUnavailable, CodeStorageGuardMismatch, CodeStorageReadOnly:
		return true
	default:
		return false
	}
}

// IsSpaceWait reports whether an error indicates that staging or library disk
// space is low and requires pausing without consuming retries.
func IsSpaceWait(err error) bool {
	switch CodeOf(err) {
	case CodeStorageLowSpace, CodeStagingLowSpace:
		return true
	default:
		return false
	}
}
