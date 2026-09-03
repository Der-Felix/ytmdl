// Package database opens the PostgreSQL connection pool and applies the
// versioned migrations. Every statement the backend runs goes through the
// repositories in the repository subpackage; no SQL lives in handlers or
// services.
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockKey is the advisory lock the migration runner holds. It keeps
// two processes that start at the same time from applying the same migration
// twice; the value is arbitrary but must stay stable across releases.
const migrationLockKey int64 = 7_311_402_659_001

// maxStartupBackoff caps the exponential backoff between startup attempts.
const maxStartupBackoff = 5 * time.Second

// Options configures the connection pool.
type Options struct {
	// URL is the PostgreSQL connection URL, e.g.
	// postgres://user:password@host:5432/database?sslmode=disable
	URL string

	MaxConns        int
	MinConns        int
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration

	// StartupTimeout bounds the total time Open waits for a database that is
	// not up yet. StartupBackoff is the delay before the first retry; it
	// doubles with every attempt up to maxStartupBackoff.
	StartupTimeout time.Duration
	StartupBackoff time.Duration

	Logger *slog.Logger
}

// DB owns the pgx pool and exposes it through the database/sql interface the
// repositories are written against.
type DB struct {
	*sql.DB
	pool   *pgxpool.Pool
	target string
}

// Open dials PostgreSQL, waits for it to become available within the startup
// budget and applies every pending migration.
func Open(ctx context.Context, opts Options) (*DB, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(opts.URL) == "" {
		return nil, apperr.New(apperr.CodeInternal, "The database URL must not be empty.")
	}

	poolConfig, err := pgxpool.ParseConfig(opts.URL)
	if err != nil {
		// The error text of an unparsable URL can contain the password, so the
		// cause is deliberately not attached here.
		return nil, apperr.New(apperr.CodeInternal, "The database URL could not be parsed.")
	}
	applyPoolOptions(poolConfig, opts)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, apperr.New(apperr.CodeInternal, "The database pool could not be created.")
	}

	target := poolConfig.ConnConfig.Host + ":" + strconv.Itoa(int(poolConfig.ConnConfig.Port)) +
		"/" + poolConfig.ConnConfig.Database

	if err := waitForDatabase(ctx, pool, opts, target, logger); err != nil {
		pool.Close()
		return nil, err
	}

	db := &DB{DB: stdlib.OpenDBFromPool(pool), pool: pool, target: target}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// applyPoolOptions overlays the configured pool limits. Values that were left
// at zero keep pgx's own default.
func applyPoolOptions(cfg *pgxpool.Config, opts Options) {
	if opts.MaxConns > 0 {
		cfg.MaxConns = int32(opts.MaxConns)
	}
	if opts.MinConns > 0 {
		cfg.MinConns = int32(opts.MinConns)
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	}
	if opts.ConnectTimeout > 0 {
		cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout
	}
}

// waitForDatabase retries the first connection until the startup budget is
// spent. Compose starts the backend and PostgreSQL together, so a few seconds
// of "connection refused" are expected rather than fatal — but the wait is
// bounded, so a genuinely misconfigured database still fails the container.
func waitForDatabase(ctx context.Context, pool *pgxpool.Pool, opts Options, target string, logger *slog.Logger) error {
	budget := opts.StartupTimeout
	if budget <= 0 {
		budget = 30 * time.Second
	}
	backoff := opts.StartupBackoff
	if backoff <= 0 {
		backoff = time.Second
	}

	deadline := time.Now().Add(budget)
	var lastErr error
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, connectTimeout(opts))
		lastErr = pool.Ping(attemptCtx)
		cancel()

		if lastErr == nil {
			if attempt > 1 {
				logger.Info("database reachable", "target", target, "attempts", attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return apperr.Wrap(apperr.CodeInternal, "The database wait was interrupted.", ctx.Err())
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := min(backoff, remaining)
		logger.Warn("database is not ready yet, retrying",
			"target", target, "attempt", attempt,
			"retry_in_ms", wait.Milliseconds(),
			"remaining_ms", remaining.Milliseconds(),
			logging.KeyError, lastErr.Error())

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return apperr.Wrap(apperr.CodeInternal, "The database wait was interrupted.", ctx.Err())
		case <-timer.C:
		}
		backoff = min(backoff*2, maxStartupBackoff)
	}

	return apperr.Wrapf(apperr.CodeInternal, lastErr,
		"PostgreSQL at %s did not become available within %s.", target, budget)
}

func connectTimeout(opts Options) time.Duration {
	if opts.ConnectTimeout > 0 {
		return opts.ConnectTimeout
	}
	return 10 * time.Second
}

// Target returns "host:port/database" for logs. It never contains credentials.
func (db *DB) Target() string { return db.target }

// Pool exposes the underlying pgx pool.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Close releases the database/sql handle and the pool behind it.
func (db *DB) Close() error {
	err := db.DB.Close()
	db.pool.Close()
	return err
}

// migration is one versioned schema change.
type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies every migration that has not been recorded yet. The whole
// run is serialised with an advisory lock so that two backends starting
// simultaneously cannot apply the same migration twice.
func (db *DB) Migrate(ctx context.Context) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration connection could not be acquired.", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration lock could not be taken.", err)
	}
	defer func() {
		// The unlock runs on the same connection and must not be skipped when
		// the caller's context is already done.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockKey); err != nil {
			slog.Default().Warn("the migration lock could not be released", logging.KeyError, err.Error())
		}
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer     PRIMARY KEY,
		name       text        NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration table could not be created.", err)
	}

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *sql.Conn) (map[int]struct{}, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The applied migrations could not be read.", err)
	}
	defer rows.Close()

	applied := make(map[int]struct{})
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "A migration row could not be read.", err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The applied migrations could not be read.", err)
	}
	return applied, nil
}

// applyMigration runs one migration and records it in the same transaction, so
// that a failed migration leaves neither schema nor bookkeeping behind.
func applyMigration(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration transaction could not be started.", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return apperr.Wrap(apperr.CodeInternal,
			fmt.Sprintf("Migration %04d (%s) failed.", m.version, m.name), err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`,
		m.version, m.name, time.Now().UTC()); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration could not be recorded.", err)
	}
	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The migration could not be committed.", err)
	}
	return nil
}

// loadMigrations reads the embedded migration files. Names must start with a
// zero padded version followed by an underscore, e.g. "0001_initial.sql".
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The migrations could not be listed.", err)
	}

	out := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".sql")
		prefix, rest, ok := strings.Cut(name, "_")
		if !ok {
			return nil, apperr.Newf(apperr.CodeInternal,
				"Migration %q does not follow the <version>_<name>.sql convention.", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, apperr.Wrapf(apperr.CodeInternal, err,
				"Migration %q has no numeric version.", entry.Name())
		}
		content, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "A migration could not be read.", err)
		}
		out = append(out, migration{version: version, name: rest, sql: string(content)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, apperr.Newf(apperr.CodeInternal,
				"Migration version %d exists more than once.", out[i].version)
		}
	}
	return out, nil
}

// MigrationSQL returns the SQL content for a specific migration version.
func MigrationSQL(version int) (string, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return "", err
	}
	for _, m := range migrations {
		if m.version == version {
			return m.sql, nil
		}
	}
	return "", fmt.Errorf("migration %04d not found", version)
}

// WithTx runs fn inside a transaction and commits it when fn returns nil.
// Any error rolls the transaction back.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The transaction could not be started.", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Default().Warn("the transaction could not be rolled back", logging.KeyError, rollbackErr.Error())
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The transaction could not be committed.", err)
	}
	return nil
}
