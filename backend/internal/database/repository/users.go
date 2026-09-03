package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database"
)

// Advisory lock constants for serializing sensitive administrative actions.
const (
	// LockFirstAdminSetup serializes First-Admin Setup across concurrent requests.
	LockFirstAdminSetup int64 = 7_311_402_659_002
	// LockAdminMutation serializes admin delete/disable/demote across concurrent requests.
	LockAdminMutation int64 = 7_311_402_659_003
)

// Users persists user accounts.
type Users struct {
	db *database.DB
}

// NewUsers returns a user repository.
func NewUsers(db *database.DB) *Users { return &Users{db: db} }

const userColumns = `id, username, display_name, password_hash, role, enabled, created_at, updated_at, last_login_at`

func scanUser(scan func(dest ...any) error) (auth.User, error) {
	var (
		u           auth.User
		roleStr     string
		lastLoginAt sql.NullTime
	)
	err := scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &roleStr,
		&u.Enabled, &u.CreatedAt, &u.UpdatedAt, &lastLoginAt)
	if err != nil {
		return auth.User{}, err
	}
	u.Role = auth.Role(roleStr)
	u.LastLoginAt = timePtr(lastLoginAt)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return u, nil
}

// SetupFirstAdmin registers the initial administrator account. It holds an
// exclusive advisory transaction lock to guarantee that under concurrency
// exactly one admin is created and subsequent attempts fail with CodeSetupCompleted.
func (r *Users) SetupFirstAdmin(ctx context.Context, u auth.User) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to start setup transaction", err)
	}
	defer tx.Rollback()

	// Acquire advisory transaction lock
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", LockFirstAdminSetup); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to acquire setup lock", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to count existing users", err)
	}
	if count > 0 {
		return apperr.New(apperr.CodeSetupCompleted, "Das Initial-Setup wurde bereits abgeschlossen.")
	}

	normUsername := auth.NormalizeUsername(u.Username)
	now := time.Now().UTC()
	query := `INSERT INTO users (id, username, display_name, password_hash, role, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err = tx.ExecContext(ctx, query, u.ID, normUsername, strings.TrimSpace(u.DisplayName),
		u.PasswordHash, string(auth.RoleAdmin), true, now, now)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to insert first admin", err)
	}

	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to commit setup transaction", err)
	}
	return nil
}

// Create inserts a new user account.
func (r *Users) Create(ctx context.Context, u auth.User) error {
	normUsername := auth.NormalizeUsername(u.Username)
	if err := auth.ValidateUsername(normUsername); err != nil {
		return err
	}
	if !u.Role.IsValid() {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Benutzerrolle.")
	}

	now := time.Now().UTC()
	query := `INSERT INTO users (id, username, display_name, password_hash, role, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.ExecContext(ctx, query, u.ID, normUsername, strings.TrimSpace(u.DisplayName),
		u.PasswordHash, string(u.Role), u.Enabled, now, now)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.New(apperr.CodeAlreadyExists, "Ein Benutzer mit diesem Benutzernamen existiert bereits.")
		}
		return apperr.Wrap(apperr.CodeInternal, "failed to insert user", err)
	}
	return nil
}

// GetByID returns the user with the given ID.
func (r *Users) GetByID(ctx context.Context, id string) (*auth.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	u, err := scanUser(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to query user by id", err)
	}
	return &u, nil
}

// GetByUsername returns the user with the given username (case-insensitive).
func (r *Users) GetByUsername(ctx context.Context, username string) (*auth.User, error) {
	norm := auth.NormalizeUsername(username)
	query := `SELECT ` + userColumns + ` FROM users WHERE lower(username) = $1`
	row := r.db.QueryRowContext(ctx, query, norm)
	u, err := scanUser(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to query user by username", err)
	}
	return &u, nil
}

// List returns a page of users ordered by creation date.
func (r *Users) List(ctx context.Context, limit, offset int) ([]auth.User, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + userColumns + ` FROM users ORDER BY created_at ASC LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "failed to list users", err)
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		u, err := scanUser(rows.Scan)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "failed to scan user row", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Count returns the total number of users.
func (r *Users) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		return 0, apperr.Wrap(apperr.CodeInternal, "failed to count users", err)
	}
	return count, nil
}

// CountActiveAdmins returns the number of enabled admin accounts.
func (r *Users) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	query := "SELECT count(*) FROM users WHERE role = 'admin' AND enabled = true"
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, apperr.Wrap(apperr.CodeInternal, "failed to count active admins", err)
	}
	return count, nil
}

// UpdateProfile updates the display name of a user.
func (r *Users) UpdateProfile(ctx context.Context, id, displayName string) error {
	now := time.Now().UTC()
	query := `UPDATE users SET display_name = $1, updated_at = $2 WHERE id = $3`
	res, err := r.db.ExecContext(ctx, query, strings.TrimSpace(displayName), now, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to update user profile", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to check affected rows", err)
	}
	if rows == 0 {
		return apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
	}
	return nil
}

// UpdatePassword updates the password hash of a user.
func (r *Users) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	now := time.Now().UTC()
	query := `UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`
	res, err := r.db.ExecContext(ctx, query, passwordHash, now, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to update user password", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to check affected rows", err)
	}
	if rows == 0 {
		return apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
	}
	return nil
}

// UpdateStatus updates the role and enabled status of a user while serializing
// with an advisory transaction lock to protect the last-active-admin invariant.
func (r *Users) UpdateStatus(ctx context.Context, id string, enabled bool, role auth.Role) error {
	if !role.IsValid() {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Benutzerrolle.")
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to start status update transaction", err)
	}
	defer tx.Rollback()

	// Acquire advisory transaction lock to serialize all admin-state mutations
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", LockAdminMutation); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to acquire admin mutation lock", err)
	}

	// Fetch current state of target user
	var currentRole string
	var currentEnabled bool
	queryUser := "SELECT role, enabled FROM users WHERE id = $1"
	if err := tx.QueryRowContext(ctx, queryUser, id).Scan(&currentRole, &currentEnabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
		}
		return apperr.Wrap(apperr.CodeInternal, "failed to inspect user", err)
	}

	// Check if this change would demote or disable an active admin
	isActiveAdmin := currentRole == string(auth.RoleAdmin) && currentEnabled
	willRemainActiveAdmin := role == auth.RoleAdmin && enabled

	if isActiveAdmin && !willRemainActiveAdmin {
		var activeAdmins int
		countQuery := "SELECT count(*) FROM users WHERE role = 'admin' AND enabled = true"
		if err := tx.QueryRowContext(ctx, countQuery).Scan(&activeAdmins); err != nil {
			return apperr.Wrap(apperr.CodeInternal, "failed to count active admins", err)
		}
		if activeAdmins <= 1 {
			return apperr.New(apperr.CodeLastAdmin, "Der letzte aktive Administrator kann nicht deaktiviert oder herabgestuft werden.")
		}
	}

	now := time.Now().UTC()
	updateQuery := `UPDATE users SET enabled = $1, role = $2, updated_at = $3 WHERE id = $4`
	if _, err := tx.ExecContext(ctx, updateQuery, enabled, string(role), now, id); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to update user status", err)
	}

	// If disabled, revoke all sessions immediately
	if !enabled {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = $1", id); err != nil {
			return apperr.Wrap(apperr.CodeInternal, "failed to revoke disabled user sessions", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to commit user status transaction", err)
	}
	return nil
}

// Delete removes a user account while serializing with an advisory transaction
// lock to protect the last-active-admin invariant. Sessions are deleted via CASCADE.
func (r *Users) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to start user delete transaction", err)
	}
	defer tx.Rollback()

	// Acquire advisory transaction lock to serialize all admin-state mutations
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", LockAdminMutation); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to acquire admin mutation lock", err)
	}

	// Fetch current state of target user
	var currentRole string
	var currentEnabled bool
	queryUser := "SELECT role, enabled FROM users WHERE id = $1"
	if err := tx.QueryRowContext(ctx, queryUser, id).Scan(&currentRole, &currentEnabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
		}
		return apperr.Wrap(apperr.CodeInternal, "failed to inspect user", err)
	}

	if currentRole == string(auth.RoleAdmin) && currentEnabled {
		var activeAdmins int
		countQuery := "SELECT count(*) FROM users WHERE role = 'admin' AND enabled = true"
		if err := tx.QueryRowContext(ctx, countQuery).Scan(&activeAdmins); err != nil {
			return apperr.Wrap(apperr.CodeInternal, "failed to count active admins", err)
		}
		if activeAdmins <= 1 {
			return apperr.New(apperr.CodeLastAdmin, "Der letzte aktive Administrator kann nicht gelöscht werden.")
		}
	}

	deleteQuery := `DELETE FROM users WHERE id = $1`
	res, err := tx.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to delete user", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to check affected rows", err)
	}
	if rows == 0 {
		return apperr.New(apperr.CodeUserNotFound, "Benutzer wurde nicht gefunden.")
	}

	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to commit user delete transaction", err)
	}
	return nil
}

// UpdateLastLogin records the timestamp of a successful login.
func (r *Users) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	query := `UPDATE users SET last_login_at = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, t.UTC(), id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "failed to update last login", err)
	}
	return nil
}
