package apperr_test

import (
	"errors"
	"net/http"
	"testing"

	"ytdm/backend/internal/apperr"
)

func TestErrorScopesAndSemantics(t *testing.T) {
	tests := []struct {
		name                 string
		err                  error
		wantCode             apperr.Code
		wantScope            apperr.Scope
		wantAllowsFallback   bool
		wantStopsFanout      bool
		wantConsumesJobRetry bool
		wantRetryable        bool
		wantHTTPStatus       int
	}{
		// Candidate-specific
		{
			name:                 "candidate track not found",
			err:                  apperr.New(apperr.CodeTrackNotFound, "track not found"),
			wantCode:             apperr.CodeTrackNotFound,
			wantScope:            apperr.ScopeCandidate,
			wantAllowsFallback:   true,
			wantStopsFanout:      false,
			wantConsumesJobRetry: true,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusNotFound,
		},
		{
			name:                 "candidate match failed",
			err:                  apperr.New(apperr.CodeMatchFailed, "match score too low"),
			wantCode:             apperr.CodeMatchFailed,
			wantScope:            apperr.ScopeCandidate,
			wantAllowsFallback:   true,
			wantStopsFanout:      false,
			wantConsumesJobRetry: true,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusUnprocessableEntity,
		},
		{
			name:                 "candidate invalid audio format",
			err:                  apperr.New(apperr.CodeInvalidAudio, "no usable audio stream"),
			wantCode:             apperr.CodeInvalidAudio,
			wantScope:            apperr.ScopeCandidate,
			wantAllowsFallback:   true,
			wantStopsFanout:      false,
			wantConsumesJobRetry: true,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusInternalServerError,
		},

		// Session-specific
		{
			name:                 "session auth failed (cookie expired / sign-in required)",
			err:                  apperr.New(apperr.CodeSessionAuthFailed, "cookies expired or invalid"),
			wantCode:             apperr.CodeSessionAuthFailed,
			wantScope:            apperr.ScopeSession,
			wantAllowsFallback:   false,
			wantStopsFanout:      true,
			wantConsumesJobRetry: true,
			wantRetryable:        true,
			wantHTTPStatus:       http.StatusBadGateway,
		},
		{
			name:                 "session bot challenge encountered",
			err:                  apperr.New(apperr.CodeSessionBotChallenge, "sign in to confirm you're not a bot"),
			wantCode:             apperr.CodeSessionBotChallenge,
			wantScope:            apperr.ScopeSession,
			wantAllowsFallback:   false,
			wantStopsFanout:      true,
			wantConsumesJobRetry: true,
			wantRetryable:        true,
			wantHTTPStatus:       http.StatusBadGateway,
		},
		{
			name:                 "session rate limited",
			err:                  apperr.New(apperr.CodeSessionRateLimited, "session cooled down / rate limited"),
			wantCode:             apperr.CodeSessionRateLimited,
			wantScope:            apperr.ScopeSession,
			wantAllowsFallback:   false,
			wantStopsFanout:      true,
			wantConsumesJobRetry: true,
			wantRetryable:        true,
			wantHTTPStatus:       http.StatusTooManyRequests,
		},

		// Provider-systemic
		{
			name:                 "provider rate limited (IP throttling / 429)",
			err:                  apperr.New(apperr.CodeProviderRateLimited, "HTTP Error 429: Too Many Requests"),
			wantCode:             apperr.CodeProviderRateLimited,
			wantScope:            apperr.ScopeProvider,
			wantAllowsFallback:   false,
			wantStopsFanout:      true,
			wantConsumesJobRetry: true,
			wantRetryable:        true,
			wantHTTPStatus:       http.StatusTooManyRequests,
		},
		{
			name:                 "provider unavailable (network or outage)",
			err:                  apperr.New(apperr.CodeProviderUnavailable, "connection timed out"),
			wantCode:             apperr.CodeProviderUnavailable,
			wantScope:            apperr.ScopeProvider,
			wantAllowsFallback:   false,
			wantStopsFanout:      true,
			wantConsumesJobRetry: true,
			wantRetryable:        true,
			wantHTTPStatus:       http.StatusBadGateway,
		},

		// Global / Infrastructure
		{
			name:                 "storage unavailable wait state",
			err:                  apperr.New(apperr.CodeStorageUnavailable, "library mount unreachable"),
			wantCode:             apperr.CodeStorageUnavailable,
			wantScope:            apperr.ScopeInfrastructure,
			wantAllowsFallback:   false,
			wantStopsFanout:      false,
			wantConsumesJobRetry: false,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusBadGateway,
		},
		{
			name:                 "storage low space wait state",
			err:                  apperr.New(apperr.CodeStorageLowSpace, "insufficient disk space"),
			wantCode:             apperr.CodeStorageLowSpace,
			wantScope:            apperr.ScopeInfrastructure,
			wantAllowsFallback:   false,
			wantStopsFanout:      false,
			wantConsumesJobRetry: false,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusInsufficientStorage,
		},
		{
			name:                 "tool unavailable",
			err:                  apperr.New(apperr.CodeToolUnavailable, "yt-dlp binary missing"),
			wantCode:             apperr.CodeToolUnavailable,
			wantScope:            apperr.ScopeInfrastructure,
			wantAllowsFallback:   false,
			wantStopsFanout:      false,
			wantConsumesJobRetry: false,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusBadGateway,
		},
		{
			name:                 "server shutting down",
			err:                  apperr.New(apperr.CodeShuttingDown, "shutting down"),
			wantCode:             apperr.CodeShuttingDown,
			wantScope:            apperr.ScopeInfrastructure,
			wantAllowsFallback:   false,
			wantStopsFanout:      false,
			wantConsumesJobRetry: false,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusServiceUnavailable,
		},
		{
			name:                 "untyped generic error",
			err:                  errors.New("something raw broke"),
			wantCode:             apperr.CodeInternal,
			wantScope:            apperr.ScopeInfrastructure,
			wantAllowsFallback:   false,
			wantStopsFanout:      false,
			wantConsumesJobRetry: false,
			wantRetryable:        false,
			wantHTTPStatus:       http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apperr.CodeOf(tt.err); got != tt.wantCode {
				t.Errorf("CodeOf = %v, want %v", got, tt.wantCode)
			}
			if got := apperr.ScopeOf(tt.err); got != tt.wantScope {
				t.Errorf("ScopeOf = %v, want %v", got, tt.wantScope)
			}
			if got := apperr.AllowsCandidateFallback(tt.err); got != tt.wantAllowsFallback {
				t.Errorf("AllowsCandidateFallback = %v, want %v", got, tt.wantAllowsFallback)
			}
			if got := apperr.StopsCandidateFanout(tt.err); got != tt.wantStopsFanout {
				t.Errorf("StopsCandidateFanout = %v, want %v", got, tt.wantStopsFanout)
			}
			if got := apperr.ConsumesJobRetry(tt.err); got != tt.wantConsumesJobRetry {
				t.Errorf("ConsumesJobRetry = %v, want %v", got, tt.wantConsumesJobRetry)
			}
			if got := apperr.Retryable(tt.err); got != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", got, tt.wantRetryable)
			}
			if got := apperr.HTTPStatus(apperr.CodeOf(tt.err)); got != tt.wantHTTPStatus {
				t.Errorf("HTTPStatus = %d, want %d", got, tt.wantHTTPStatus)
			}
		})
	}
}
