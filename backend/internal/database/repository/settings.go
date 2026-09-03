package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ytdm/backend/internal/database"
)

// Settings stores the runtime adjustable settings.
type Settings struct {
	db *database.DB
}

// NewSettings returns a settings repository.
func NewSettings(db *database.DB) *Settings { return &Settings{db: db} }

// Get returns the stored value of a key. The second result reports whether the
// key exists.
func (r *Settings) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, wrapDB("get setting", err)
	}
	return value, true, nil
}

// All returns every stored setting.
func (r *Settings) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, wrapDB("list settings", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, wrapDB("scan setting", err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list settings", err)
	}
	return out, nil
}

// setSettingSQL upserts one key. It is shared by Set and SetMany so that both
// paths behave identically.
const setSettingSQL = `
	INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
	ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

// Set stores a value.
func (r *Settings) Set(ctx context.Context, key, value string) error {
	if _, err := r.db.ExecContext(ctx, setSettingSQL, key, value, time.Now().UTC()); err != nil {
		return wrapDB("set setting", err)
	}
	return nil
}

// SetMany stores several values in one transaction.
func (r *Settings) SetMany(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, setSettingSQL)
		if err != nil {
			return wrapDB("prepare setting insert", err)
		}
		defer stmt.Close()
		for key, value := range values {
			if _, err := stmt.ExecContext(ctx, key, value, now); err != nil {
				return wrapDB("set setting", err)
			}
		}
		return nil
	})
}
