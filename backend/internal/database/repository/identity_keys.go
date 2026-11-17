package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"sort"
	"time"

	"ytdm/backend/internal/identitymigration"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
)

// identityMigrationLockKey is the session advisory lock one identity
// migration run holds, so that two backend processes do not both walk the
// catalogue. The writes themselves are conditional and would stay correct
// without it. The second half of the key is the schema, so that catalogues in
// different schemas of one database (the integration tests) do not wait for
// each other.
const identityMigrationLockKey int32 = 7_311_402

const identityMigrationLockArgs = `$1, hashtext(current_schema())`

// identityGuardSQL fingerprints the stored fields a step reads from a track
// row "t". A write only applies while the row still has them.
const identityGuardSQL = `md5(t.title || chr(31) || t.artists_json::text || chr(31) || COALESCE(t.release_id, ''))`

var _ identitymigration.Store = (*Catalog)(nil)

// CountIdentityRowsBelow counts the tracks whose identity scheme is below
// scheme.
func (c *Catalog) CountIdentityRowsBelow(ctx context.Context, scheme int) (int, error) {
	var n int
	if err := c.db.QueryRowContext(ctx,
		`SELECT count(*) FROM tracks WHERE identity_scheme < $1`, scheme).Scan(&n); err != nil {
		return 0, wrapDB("count tracks by identity scheme", err)
	}
	return n, nil
}

// ListIdentityRows returns up to limit tracks of exactly scheme whose ID sorts
// after afterID, in ID order.
func (c *Catalog) ListIdentityRows(ctx context.Context, scheme int, afterID string, limit int) ([]identitymigration.Row, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT t.id, t.identity_key, t.title, t.artists_json::text,
			COALESCE(r.release_type, ''), `+identityGuardSQL+`
		FROM tracks t LEFT JOIN releases r ON r.id = t.release_id
		WHERE t.identity_scheme = $1 AND t.id > $2
		ORDER BY t.id
		LIMIT $3`, scheme, afterID, limit)
	if err != nil {
		return nil, wrapDB("list tracks by identity scheme", err)
	}
	defer rows.Close()

	var out []identitymigration.Row
	for rows.Next() {
		var (
			row         identitymigration.Row
			artistsJSON string
			releaseType string
		)
		if err := rows.Scan(&row.ID, &row.Key, &row.Title, &artistsJSON, &releaseType, &row.Guard); err != nil {
			return nil, wrapDB("scan track identity", err)
		}
		row.Artists = decodeStrings(artistsJSON)
		row.ReleaseType = music.ReleaseType(releaseType)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list tracks by identity scheme", err)
	}
	return out, nil
}

// KeepIdentityKey raises a track from scheme from to to and leaves its key.
// updated_at is left alone: nothing a user or a client sees has changed.
func (c *Catalog) KeepIdentityKey(ctx context.Context, row identitymigration.Row, from, to int) (bool, error) {
	res, err := c.db.ExecContext(ctx, `
		UPDATE tracks AS t SET identity_scheme = $3
		WHERE t.id = $1 AND t.identity_scheme = $2 AND t.identity_key = $4
		  AND `+identityGuardSQL+` = $5`,
		row.ID, from, to, row.Key, row.Guard)
	if err != nil {
		return false, wrapDB("raise track identity scheme", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapDB("raise track identity scheme", err)
	}
	return n == 1, nil
}

// RewriteIdentityKey gives a track a new identity key and raises its scheme.
//
// It takes the same transaction scoped locks upsertTrack takes for the old
// and the new key, so a download storing the same recording at this moment
// either runs first (and its row is then seen as a collision) or after (and
// finds this row under its new key). Each row is its own short transaction
// on purpose: holding the locks of a whole batch while a download waits for
// one of them, and holds a row lock this batch needs, would be a deadlock.
func (c *Catalog) RewriteIdentityKey(ctx context.Context, row identitymigration.Row, key string, from, to int) (identitymigration.WriteResult, error) {
	var result identitymigration.WriteResult
	err := c.db.WithTx(ctx, func(tx *sql.Tx) error {
		keys := []string{row.Key, key}
		sort.Strings(keys)
		for _, k := range keys {
			if err := lockTrackIdentity(ctx, tx, "identity:", k); err != nil {
				return err
			}
		}

		var other string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM tracks WHERE identity_key = $1 AND id <> $2 ORDER BY id LIMIT 1`,
			key, row.ID).Scan(&other)
		switch {
		case err == nil:
			result.ConflictID = other
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return wrapDB("find identity key collision", err)
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE tracks AS t SET identity_key = $3, identity_scheme = $4
			WHERE t.id = $1 AND t.identity_scheme = $2 AND t.identity_key = $5
			  AND `+identityGuardSQL+` = $6`,
			row.ID, from, key, to, row.Key, row.Guard)
		if err != nil {
			return wrapDB("rewrite track identity key", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return wrapDB("rewrite track identity key", err)
		}
		result.Applied = n == 1
		return nil
	})
	if err != nil {
		return identitymigration.WriteResult{}, err
	}
	return result, nil
}

// LockIdentityMigration takes the session lock on a connection of its own and
// returns the function that releases it. ok is false when another backend
// process holds it.
func (c *Catalog) LockIdentityMigration(ctx context.Context) (func(), bool, error) {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, false, wrapDB("identity migration lock connection", err)
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+identityMigrationLockArgs+`)`, identityMigrationLockKey).Scan(&ok); err != nil {
		_ = conn.Close()
		return nil, false, wrapDB("take identity migration lock", err)
	}
	if !ok {
		_ = conn.Close()
		return nil, false, nil
	}
	unlock := func() {
		// The unlock must run when the run ended because of a shutdown too.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(`+identityMigrationLockArgs+`)`, identityMigrationLockKey); err != nil {
			slog.Default().Warn("the identity migration lock could not be released; the connection is discarded", logging.KeyError, err.Error())
			// A pooled connection would keep the session lock for ever.
			// Marking it bad makes the pool close it, which releases the lock.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return unlock, true, nil
}
