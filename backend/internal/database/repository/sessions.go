package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database"
)

// Sessions persists user login sessions.
type Sessions struct {
	db *database.DB
}

// NewSessions returns a session repository.
func NewSessions(db *database.DB) *Sessions { return &Sessions{db: db} }

const sessionColumns = `id, user_id, token_hash, user_agent, ip_address, created_at, expires_at, last_seen_at`

func scanSession(scan func(dest ...any) error) (auth.Session, error) {
	var s auth.Session
	err := scan(&s.ID, &s.UserID, &s.TokenHash, &s.UserAgent, &s.IPAddress,
		&s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt)
	if err != nil {
		return auth.Session{}, err
	}
	s.CreatedAt = s.CreatedAt.UTC()
	s.ExpiresAt = s.ExpiresAt.UTC()
	s.LastSeenAt = s.LastSeenAt.UTC()
	return s, nil
}

// Create stores a new session.
func (r *Sessions) Create(ctx context.Context, s auth.Session) error {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.LastSeenAt.IsZero() {
		s.LastSeenAt = now
	}

	query := `INSERT INTO sessions (id, user_id, token_hash, user_agent, ip_address, created_at, expires_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.ExecContext(ctx, query, s.ID, s.UserID, s.TokenHash, s.UserAgent,
		s.IPAddress, s.CreatedAt.UTC(), s.ExpiresAt.UTC(), s.LastSeenAt.UTC())
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to insert session", err)
	}
	return nil
}

// GetByTokenHash returns the session matching the given SHA-256 token hash.
func (r *Sessions) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions WHERE token_hash = $1`
	row := r.db.QueryRowContext(ctx, query, tokenHash)
	s, err := scanSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.New(apperr.CodeSessionNotFound, "Sitzung wurde nicht gefunden oder ist abgelaufen.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to query session by token hash", err)
	}
	return &s, nil
}

// GetByID returns the session with the given ID.
func (r *Sessions) GetByID(ctx context.Context, id string) (*auth.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	s, err := scanSession(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.New(apperr.CodeSessionNotFound, "Sitzung wurde nicht gefunden.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to query session by id", err)
	}
	return &s, nil
}

// ListByUser returns all active sessions for a user, ordered by most recently active.
func (r *Sessions) ListByUser(ctx context.Context, userID string) ([]auth.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions WHERE user_id = $1 ORDER BY last_seen_at DESC`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to list user sessions", err)
	}
	defer rows.Close()

	var sessions []auth.Session
	for rows.Next() {
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "failed to scan session row", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// Touch updates the last seen timestamp of a session and optionally the IP.
func (r *Sessions) Touch(ctx context.Context, id string, lastSeenAt time.Time, ipAddress string) error {
	query := `UPDATE sessions SET last_seen_at = $1, ip_address = CASE WHEN $2 <> '' THEN $2 ELSE ip_address END WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, lastSeenAt.UTC(), ipAddress, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to touch session", err)
	}
	return nil
}

// Delete removes a specific session by ID.
func (r *Sessions) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to delete session", err)
	}
	return nil
}

// DeleteByUser removes sessions for a user. If exceptSessionID is non-empty,
// that specific session is preserved (used when revoking other sessions).
func (r *Sessions) DeleteByUser(ctx context.Context, userID string, exceptSessionID string) error {
	var err error
	if exceptSessionID != "" {
		query := `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`
		_, err = r.db.ExecContext(ctx, query, userID, exceptSessionID)
	} else {
		query := `DELETE FROM sessions WHERE user_id = $1`
		_, err = r.db.ExecContext(ctx, query, userID)
	}
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to delete user sessions", err)
	}
	return nil
}

// DeleteExpired purges expired sessions from the database.
func (r *Sessions) DeleteExpired(ctx context.Context, now time.Time) error {
	query := `DELETE FROM sessions WHERE expires_at <= $1`
	_, err := r.db.ExecContext(ctx, query, now.UTC())
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to delete expired sessions", err)
	}
	return nil
}
