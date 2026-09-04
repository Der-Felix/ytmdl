// Package dbtest opens isolated PostgreSQL databases for the integration
// tests. Every caller gets its own schema, so tests can run in parallel and in
// any order without seeing each other's rows.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"ytdm/backend/internal/database"
)

// EnvURL names the environment variable that points the integration tests at a
// PostgreSQL server.
const EnvURL = "MUSICDL_TEST_DATABASE_URL"

// URL returns the configured test server URL, skipping the test when none is
// configured. Running the unit tests must not require a database.
func URL(t *testing.T) string {
	t.Helper()
	value := os.Getenv(EnvURL)
	if value == "" {
		t.Skipf("%s is not set; start PostgreSQL and point %s at it to run the integration tests", EnvURL, EnvURL)
	}
	return value
}

// Open returns a migrated database that lives in a schema of its own. The
// schema is dropped when the test finishes.
func Open(t *testing.T) *database.DB {
	t.Helper()
	db, _ := OpenWithURL(t)
	return db
}

// OpenWithURL returns a migrated database and the connection URL pointing to its dedicated test schema.
func OpenWithURL(t *testing.T) (*database.DB, string) {
	t.Helper()
	base := URL(t)
	schema := "musicdl_test_" + randomSuffix(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to the test server: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatalf("close the admin connection: %v", err)
	}

	testURL := withSearchPath(t, base, schema)
	opts := database.Options{
		URL:            testURL,
		MaxConns:       6,
		ConnectTimeout: 10 * time.Second,
		StartupTimeout: 20 * time.Second,
		StartupBackoff: 200 * time.Millisecond,
	}

	db, err := database.Open(ctx, opts)
	if err != nil {
		dropSchema(t, base, schema)
		t.Fatalf("open the test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close the test database: %v", err)
		}
		dropSchema(t, base, schema)
	})
	return db, testURL
}

// OpenWithOptions is Open with room to override the pool settings, which the
// concurrency tests need.
func OpenWithOptions(t *testing.T, opts database.Options) *database.DB {
	t.Helper()

	base := URL(t)
	schema := "musicdl_test_" + randomSuffix(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to the test server: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatalf("close the admin connection: %v", err)
	}

	opts.URL = withSearchPath(t, base, schema)
	if opts.MaxConns == 0 {
		opts.MaxConns = 6
	}
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = 10 * time.Second
	}
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 20 * time.Second
	}
	if opts.StartupBackoff == 0 {
		opts.StartupBackoff = 200 * time.Millisecond
	}

	db, err := database.Open(ctx, opts)
	if err != nil {
		dropSchema(t, base, schema)
		t.Fatalf("open the test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close the test database: %v", err)
		}
		dropSchema(t, base, schema)
	})
	return db
}

// withSearchPath points a connection URL at one schema. pgx forwards unknown
// query parameters as PostgreSQL runtime settings, so the migrations and every
// later statement operate inside the test's own schema.
func withSearchPath(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", EnvURL, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func dropSchema(t *testing.T, base, schema string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Errorf("connect to drop schema %s: %v", schema, err)
		return
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
		t.Errorf("drop schema %s: %v", schema, err)
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return hex.EncodeToString(buf[:])
}
