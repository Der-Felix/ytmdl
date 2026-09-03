// Package logging configures the structured logger used across the backend.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// Context keys used consistently in log records so that job related lines can
// be correlated.
const (
	KeyJobID     = "job_id"
	KeyJobItemID = "job_item_id"
	KeyArtist    = "artist"
	KeyTrack     = "track"
	KeyRelease   = "release"
	KeyProvider  = "provider"
	KeyOperation = "operation"
	KeyError     = "error"
	KeyErrorCode = "error_code"
)

// New builds a slog logger for the given level and format. Unknown levels fall
// back to info, unknown formats to JSON.
func New(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
