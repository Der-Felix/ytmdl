package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

// MediaSessions provides CRUD operations for upstream media provider authentication sessions.
// Cookie file I/O is strictly prohibited in this repository.
type MediaSessions struct {
	db *database.DB
}

// NewMediaSessions returns a new media session repository.
func NewMediaSessions(db *database.DB) *MediaSessions {
	return &MediaSessions{db: db}
}

const mediaSessionColumns = `id, provider_family, name, cookie_ref, enabled, health_status, consecutive_failures, last_used_at, last_success_at, last_failure_at, last_failure_reason, cooldown_until, created_at, updated_at`

func scanMediaSession(scan func(dest ...any) error) (mediasession.Session, error) {
	var s mediasession.Session
	var (
		family   string
		status   string
		lastUsed sql.NullTime
		lastSucc sql.NullTime
		lastFail sql.NullTime
		cooldown sql.NullTime
	)

	err := scan(
		&s.ID,
		&family,
		&s.Name,
		&s.CookieRef,
		&s.Enabled,
		&status,
		&s.ConsecutiveFailures,
		&lastUsed,
		&lastSucc,
		&lastFail,
		&s.LastFailureReason,
		&cooldown,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return mediasession.Session{}, err
	}

	s.ProviderFamily = provider.Family(family)
	s.HealthStatus = mediasession.HealthStatus(status)
	s.LastUsedAt = timePtr(lastUsed)
	s.LastSuccessAt = timePtr(lastSucc)
	s.LastFailureAt = timePtr(lastFail)
	s.CooldownUntil = timePtr(cooldown)
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()

	return s, nil
}

// CreateSession inserts a new media session. If ID is empty, the database assigns a UUID.
// Initial health status defaults to UNKNOWN.
func (r *MediaSessions) CreateSession(ctx context.Context, s *mediasession.Session) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	if s.HealthStatus == "" {
		s.HealthStatus = mediasession.HealthUnknown
	}
	if !s.HealthStatus.Valid() {
		return apperr.Newf(apperr.CodeInvalidRequest, "Invalid health status %q.", s.HealthStatus)
	}
	if strings.TrimSpace(s.Name) == "" {
		return apperr.New(apperr.CodeInvalidRequest, "Session name must not be empty.")
	}
	if strings.TrimSpace(string(s.ProviderFamily)) == "" {
		return apperr.New(apperr.CodeInvalidRequest, "Session provider family must not be empty.")
	}

	var row *sql.Row
	if strings.TrimSpace(s.ID) != "" {
		query := `INSERT INTO media_sessions (
			id, provider_family, name, cookie_ref, enabled, health_status,
			consecutive_failures, last_used_at, last_success_at, last_failure_at,
			last_failure_reason, cooldown_until, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING ` + mediaSessionColumns
		row = r.db.QueryRowContext(ctx, query,
			s.ID, string(s.ProviderFamily), s.Name, s.CookieRef, s.Enabled, string(s.HealthStatus),
			s.ConsecutiveFailures, nullTime(s.LastUsedAt), nullTime(s.LastSuccessAt), nullTime(s.LastFailureAt),
			s.LastFailureReason, nullTime(s.CooldownUntil), s.CreatedAt.UTC(), s.UpdatedAt.UTC(),
		)
	} else {
		query := `INSERT INTO media_sessions (
			provider_family, name, cookie_ref, enabled, health_status,
			consecutive_failures, last_used_at, last_success_at, last_failure_at,
			last_failure_reason, cooldown_until, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING ` + mediaSessionColumns
		row = r.db.QueryRowContext(ctx, query,
			string(s.ProviderFamily), s.Name, s.CookieRef, s.Enabled, string(s.HealthStatus),
			s.ConsecutiveFailures, nullTime(s.LastUsedAt), nullTime(s.LastSuccessAt), nullTime(s.LastFailureAt),
			s.LastFailureReason, nullTime(s.CooldownUntil), s.CreatedAt.UTC(), s.UpdatedAt.UTC(),
		)
	}

	created, err := scanMediaSession(row.Scan)
	if err != nil {
		return wrapDB("create media session", err)
	}
	*s = created
	return nil
}

// GetSession retrieves a single media session by ID.
func (r *MediaSessions) GetSession(ctx context.Context, id string) (*mediasession.Session, error) {
	query := `SELECT ` + mediaSessionColumns + ` FROM media_sessions WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	s, err := scanMediaSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
		}
		return nil, wrapDB("get media session", err)
	}
	return &s, nil
}

// ListSessions queries media sessions matching the optional filter criteria.
func (r *MediaSessions) ListSessions(ctx context.Context, filter mediasession.Filter) ([]mediasession.Session, error) {
	var (
		whereClauses []string
		args         []any
		argIdx       = 1
	)

	if filter.ProviderFamily != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("provider_family = $%d", argIdx))
		args = append(args, filter.ProviderFamily)
		argIdx++
	}
	if filter.Enabled != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *filter.Enabled)
		argIdx++
	}
	if filter.HealthStatus != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("health_status = $%d", argIdx))
		args = append(args, string(*filter.HealthStatus))
		argIdx++
	}

	query := `SELECT ` + mediaSessionColumns + ` FROM media_sessions`
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	query += " ORDER BY created_at ASC, id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapDB("list media sessions", err)
	}
	defer rows.Close()

	var sessions []mediasession.Session
	for rows.Next() {
		s, err := scanMediaSession(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan media session", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list media sessions", err)
	}
	return sessions, nil
}

// UpdateSessionMetadata updates user-configurable properties (name, enabled status).
func (r *MediaSessions) UpdateSessionMetadata(ctx context.Context, id string, name string, enabled bool) (*mediasession.Session, error) {
	if strings.TrimSpace(name) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Session name must not be empty.")
	}
	now := time.Now().UTC()
	query := `UPDATE media_sessions
		SET name = $1, enabled = $2, updated_at = $3
		WHERE id = $4
		RETURNING ` + mediaSessionColumns
	row := r.db.QueryRowContext(ctx, query, name, enabled, now, id)
	s, err := scanMediaSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
		}
		return nil, wrapDB("update media session metadata", err)
	}
	return &s, nil
}

// UpdateCookieRef updates the managed cookie reference for a session.
func (r *MediaSessions) UpdateCookieRef(ctx context.Context, id string, cookieRef string) (*mediasession.Session, error) {
	now := time.Now().UTC()
	query := `UPDATE media_sessions
		SET cookie_ref = $1, updated_at = $2
		WHERE id = $3
		RETURNING ` + mediaSessionColumns
	row := r.db.QueryRowContext(ctx, query, cookieRef, now, id)
	s, err := scanMediaSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
		}
		return nil, wrapDB("update media session cookie reference", err)
	}
	return &s, nil
}

// UpdateHealth updates runtime health status, failures, and cooldown timestamps.
func (r *MediaSessions) UpdateHealth(ctx context.Context, id string, params mediasession.HealthUpdate) (*mediasession.Session, error) {
	if !params.HealthStatus.Valid() {
		return nil, apperr.Newf(apperr.CodeInvalidRequest, "Invalid health status %q.", params.HealthStatus)
	}
	now := time.Now().UTC()
	query := `UPDATE media_sessions
		SET health_status = $1, consecutive_failures = $2, last_used_at = $3,
		    last_success_at = $4, last_failure_at = $5, last_failure_reason = $6,
		    cooldown_until = $7, updated_at = $8
		WHERE id = $9
		RETURNING ` + mediaSessionColumns
	row := r.db.QueryRowContext(ctx, query,
		string(params.HealthStatus), params.ConsecutiveFailures,
		nullTime(params.LastUsedAt), nullTime(params.LastSuccessAt),
		nullTime(params.LastFailureAt), params.LastFailureReason,
		nullTime(params.CooldownUntil), now, id,
	)
	s, err := scanMediaSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
		}
		return nil, wrapDB("update media session health", err)
	}
	return &s, nil
}

// DeleteSession removes a media session by ID.
func (r *MediaSessions) DeleteSession(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM media_sessions WHERE id = $1`, id)
	if err != nil {
		return wrapDB("delete media session", err)
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return apperr.Newf(apperr.CodeSessionNotFound, "Media session %q was not found.", id)
	}
	return nil
}
