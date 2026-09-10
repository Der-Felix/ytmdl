package downloader

import (
	"errors"
	"log/slog"

	"ytdm/backend/internal/apperr"
)

// verificationFailure carries only the reason and measurements needed to
// diagnose rejection. It preserves the existing application error taxonomy.
type verificationFailure struct {
	reason string
	info   *AudioInfo
	cause  error
}

func (e *verificationFailure) Error() string { return e.cause.Error() }
func (e *verificationFailure) Unwrap() error { return e.cause }

func invalidAudio(reason, message string, info *AudioInfo) error {
	return &verificationFailure{reason: reason, info: info, cause: apperr.New(apperr.CodeInvalidAudio, message)}
}

// logVerificationFailure deliberately excludes paths, URLs, sessions, tool
// output and error strings. Unknown measurements remain null, not zero.
func logVerificationFailure(logger *slog.Logger, stage string, info *AudioInfo, expectedMS, toleranceMS int, err error) {
	reason := "ffprobe_error"
	var failure *verificationFailure
	if errors.As(err, &failure) {
		reason = failure.reason
		if info == nil {
			info = failure.info
		}
	}
	var duration, size any
	codec, container := "", ""
	if info != nil {
		size = info.SizeBytes
		if reason != "ffprobe_error" {
			duration = info.DurationMS
		}
		codec, container = diagnosticToken(info.Codec), diagnosticToken(info.Container)
	}
	logger.Warn("media verification failed",
		"verification_stage", stage,
		"verification_reason", reason,
		"expected_duration_ms", expectedMS,
		"measured_duration_ms", duration,
		"duration_tolerance_ms", toleranceMS,
		"codec", codec,
		"container", container,
		"size_bytes", size,
	)
}

// Provider/tool controlled identifiers are bounded tokens, never free text.
func diagnosticToken(s string) string {
	if len(s) > 128 {
		return "redacted"
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return "redacted"
		}
	}
	return s
}
