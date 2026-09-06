package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

func TestMigration0011_UpgradeFromSchema10(t *testing.T) {
	baseURL := dbtest.URL(t)
	schema := fmt.Sprintf("upgrade_0011_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = conn.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	// 1. Apply migrations 1 through 10 (Schema 10)
	for i := 1; i <= 10; i++ {
		sqlContent, err := database.MigrationSQL(i)
		if err != nil {
			t.Fatalf("get migration %04d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, sqlContent); err != nil {
			t.Fatalf("apply migration %04d: %v", i, err)
		}
	}

	// Record migrations 1-10 in schema_migrations
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	now := time.Now().UTC()
	for i := 1; i <= 10; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`,
			i, fmt.Sprintf("%04d_migration", i), now); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 2. Insert Schema 10 test fixtures across all core tables:
	// - Users
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES ('u_test_1', 'admin', 'hash123', 'admin', true, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// - Subscriptions (with priority 3 allowed in Schema 10)
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
		VALUES ('sub_test_1', 'Daft Punk', 'spotify', 'art_dp', $1, 3, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// - Library (Artists, Releases, Tracks, Files)
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, created_at, updated_at)
		VALUES ('art_1', 'Daft Punk', 'daft punk', 'spotify', 'art_dp', $1, $1)
	`, now); err != nil {
		t.Fatalf("insert artist: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO releases (id, artist_id, title, provider, source_id, year, created_at, updated_at)
		VALUES ('rel_1', 'art_1', 'Discovery', 'spotify', 'rel_disc', 2001, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert release: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO tracks (id, release_id, artist_id, title, identity_key, duration_ms, created_at, updated_at)
		VALUES ('trk_1', 'rel_1', 'art_1', 'One More Time', 'daft punk|one more time', 320000, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO files (id, track_id, path, size_bytes, codec, duration_ms, created_at, updated_at)
		VALUES ('file_1', 'trk_1', '/music/Daft Punk/Discovery/01.opus', 5000000, 'opus', 320000, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	// - Jobs (with priority 3) and Job Items
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
		VALUES ('job_test_1', 'artist', 'queued', 'Test Job', 'spotify', 'ytmusic', 'art_dp', 3, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_items (id, job_id, position, status, track_id, track_json, label, created_at, updated_at)
		VALUES ('item_test_1', 'job_test_1', 0, 'pending', 'trk_1', '{"title":"One More Time"}', 'Daft Punk - One More Time', $1, $1)
	`, now); err != nil {
		t.Fatalf("insert job item: %v", err)
	}

	// Count rows before migration 11
	tables := []string{"users", "artist_subscriptions", "artists", "releases", "tracks", "files", "jobs", "job_items"}
	countsBefore := make(map[string]int)
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count before %s: %v", tbl, err)
		}
		countsBefore[tbl] = cnt
	}

	// 3. Apply Migration 0011
	sql0011, err := database.MigrationSQL(11)
	if err != nil {
		t.Fatalf("get migration 0011: %v", err)
	}
	if _, err := conn.ExecContext(ctx, sql0011); err != nil {
		t.Fatalf("apply migration 0011: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (11, '0011_media_sessions', $1)`, now); err != nil {
		t.Fatalf("record migration 11: %v", err)
	}

	// 4. Verify existing data preserved exactly
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count after %s: %v", tbl, err)
		}
		if countsBefore[tbl] != cnt {
			t.Fatalf("table %s row count changed after migration 11: before=%d, after=%d", tbl, countsBefore[tbl], cnt)
		}
	}

	// Verify existing job priority 3 preserved
	var pri int
	if err := conn.QueryRowContext(ctx, `SELECT priority FROM jobs WHERE id = 'job_test_1'`).Scan(&pri); err != nil || pri != 3 {
		t.Fatalf("expected job priority 3, got %d (err: %v)", pri, err)
	}

	// 5. Verify media_sessions table exists and has all required columns
	requiredColumns := []string{
		"id", "provider_family", "name", "cookie_ref", "enabled",
		"health_status", "consecutive_failures", "last_used_at",
		"last_success_at", "last_failure_at", "last_failure_reason",
		"cooldown_until", "created_at", "updated_at",
	}
	for _, col := range requiredColumns {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'media_sessions' AND column_name = $2
			)`, schema, col).Scan(&exists)
		if err != nil || !exists {
			t.Fatalf("column %s missing in media_sessions: %v", col, err)
		}
	}

	// Verify required indexes exist
	requiredIndexes := []string{
		"idx_media_sessions_lookup",
		"idx_media_sessions_updated_at",
	}
	for _, idx := range requiredIndexes {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = $1 AND indexname = $2
			)`, schema, idx).Scan(&exists)
		if err != nil || !exists {
			t.Fatalf("index %s missing on media_sessions: %v", idx, err)
		}
	}

	// 6. Verify valid health statuses succeed
	validStatuses := []mediasession.HealthStatus{
		mediasession.HealthUnknown,
		mediasession.HealthHealthy,
		mediasession.HealthCooldown,
		mediasession.HealthRateLimited,
		mediasession.HealthBotChallenge,
		mediasession.HealthAuthFailed,
	}
	for i, st := range validStatuses {
		_, err := conn.ExecContext(ctx, `
			INSERT INTO media_sessions (id, provider_family, name, cookie_ref, enabled, health_status)
			VALUES ($1, $2, $3, $4, true, $5)
		`, fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1), provider.FamilyYouTube, fmt.Sprintf("Session %d", i), "managed://ref", st)
		if err != nil {
			t.Fatalf("inserting valid health status %s failed: %v", st, err)
		}
	}

	// 7. Verify invalid health status fails CHECK constraint
	_, err = conn.ExecContext(ctx, `
		INSERT INTO media_sessions (provider_family, name, cookie_ref, health_status)
		VALUES ('youtube', 'Bad Health', 'managed://ref', 'disabled')
	`)
	if err == nil {
		t.Fatal("expected CHECK constraint violation for health_status='disabled', got nil")
	}

	// 8. Verify negative consecutive failures fails CHECK constraint
	_, err = conn.ExecContext(ctx, `
		INSERT INTO media_sessions (provider_family, name, cookie_ref, consecutive_failures)
		VALUES ('youtube', 'Neg Failures', 'managed://ref', -1)
	`)
	if err == nil {
		t.Fatal("expected CHECK constraint violation for consecutive_failures=-1, got nil")
	}

	// 9. Verify NO cookie secrets stored in database
	var countWithSecret int
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_sessions
		WHERE cookie_ref LIKE '%Netscape%' OR cookie_ref LIKE '%SID%' OR cookie_ref LIKE '%HSID%'
	`).Scan(&countWithSecret); err != nil || countWithSecret != 0 {
		t.Fatalf("expected 0 rows with cookie secrets in cookie_ref, got %d (err: %v)", countWithSecret, err)
	}
}

func TestMigration0011_FreshDB(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	if count != 11 {
		t.Fatalf("expected 11 migrations applied, got %d", count)
	}

	var latestVersion int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&latestVersion); err != nil {
		t.Fatalf("failed to query max version: %v", err)
	}
	if latestVersion != 11 {
		t.Fatalf("expected latest schema version 11, got %d", latestVersion)
	}

	// Verify media_sessions operational on fresh DB
	_, err := db.ExecContext(ctx, `
		INSERT INTO media_sessions (provider_family, name, cookie_ref, health_status)
		VALUES ('youtube', 'Fresh Session', 'managed://vault/fresh', 'unknown')
	`)
	if err != nil {
		t.Fatalf("inserting media_session on fresh DB failed: %v", err)
	}

	// Idempotent migration
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("idempotent Migrate call failed: %v", err)
	}
}
