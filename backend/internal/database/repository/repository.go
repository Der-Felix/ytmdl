// Package repository contains every SQL statement the backend runs. Services
// and HTTP handlers talk to these types instead of to the database directly.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// executor is the subset of *sql.DB and *sql.Tx the repositories need.
type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Timestamps are stored in timestamptz columns. The application always writes
// UTC so that a server with a different local timezone cannot shift a value.

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	utc := v.Time.UTC()
	return &utc
}

// encodeStrings stores a string slice as a JSON array.
func encodeStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// decodeStrings reads a JSON array back into a string slice. Unreadable data
// yields an empty slice rather than an error, so that one broken row cannot
// make the whole library unreadable.
func decodeStrings(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func stringOf(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// wrapDB turns a driver error into an application error.
func wrapDB(operation string, err error) error {
	return apperr.Wrapf(apperr.CodeInternal, err, "The database operation %q failed.", operation)
}

// clampLimit keeps list queries bounded.
func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
