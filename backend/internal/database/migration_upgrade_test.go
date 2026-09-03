package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
)

func TestMigration0007_FreshDB(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	if count != 8 {
		t.Fatalf("expected 8 migrations applied, got %d", count)
	}

	// Verify all indexes exist
	expectedIndexes := []string{
		"idx_tracks_created_at",
		"idx_releases_created_at",
		"idx_releases_year",
		"idx_tracks_lyrics_state",
		"idx_job_items_retry_due",
		"idx_job_items_waiting_storage",
		"idx_jobs_priority_created",
		"idx_jobs_history_pagination",
		"idx_audit_runs_started",
		"idx_audit_runs_status",
		"idx_audit_findings_run_sev",
		"idx_audit_findings_run_code",
		"idx_audit_findings_track",
	}
	for _, idx := range expectedIndexes {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes 
				WHERE schemaname = CURRENT_SCHEMA() AND indexname = $1
			)`, idx).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check index %s: %v", idx, err)
		}
		if !exists {
			t.Fatalf("expected index %s to exist on fresh DB", idx)
		}
	}
}

func TestMigration0006_UpgradeFromV010(t *testing.T) {
	base := dbtest.URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schema := fmt.Sprintf("musicdl_upgrade_0006_%d", time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		_ = admin.Close(context.Background())
	}()

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	opts := database.Options{
		URL: base + "&search_path=" + schema,
	}
	conn, err := sql.Open("pgx", opts.URL)
	if err != nil {
		t.Fatalf("open raw sql: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	// Apply migrations 0001 to 0005 manually
	migrations0001to0005 := []struct {
		version int
		sql     string
	}{
		{
			version: 1,
			sql: `
				CREATE TABLE artists (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					sort_key TEXT NOT NULL DEFAULT '',
					provider TEXT NOT NULL,
					source_id TEXT NOT NULL,
					source_url TEXT NOT NULL DEFAULT '',
					image_url TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL,
					UNIQUE (provider, source_id)
				);
				CREATE TABLE releases (
					id TEXT PRIMARY KEY,
					artist_id TEXT REFERENCES artists (id) ON DELETE SET NULL,
					title TEXT NOT NULL,
					artists_json JSONB NOT NULL DEFAULT '[]'::jsonb,
					album_artist TEXT NOT NULL DEFAULT '',
					release_type TEXT NOT NULL DEFAULT 'album',
					year INTEGER NOT NULL DEFAULT 0,
					release_date TEXT NOT NULL DEFAULT '',
					track_count INTEGER NOT NULL DEFAULT 0,
					cover_url TEXT NOT NULL DEFAULT '',
					provider TEXT NOT NULL,
					source_id TEXT NOT NULL,
					source_url TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL,
					UNIQUE (provider, source_id)
				);
				CREATE TABLE tracks (
					id TEXT PRIMARY KEY,
					release_id TEXT REFERENCES releases (id) ON DELETE SET NULL,
					artist_id TEXT REFERENCES artists (id) ON DELETE SET NULL,
					title TEXT NOT NULL,
					artists_json JSONB NOT NULL DEFAULT '[]'::jsonb,
					album TEXT NOT NULL DEFAULT '',
					album_artist TEXT NOT NULL DEFAULT '',
					track_number INTEGER NOT NULL DEFAULT 0,
					track_total INTEGER NOT NULL DEFAULT 0,
					disc_number INTEGER NOT NULL DEFAULT 0,
					disc_total INTEGER NOT NULL DEFAULT 0,
					duration_ms INTEGER NOT NULL DEFAULT 0,
					year INTEGER NOT NULL DEFAULT 0,
					isrc TEXT NOT NULL DEFAULT '',
					cover_url TEXT NOT NULL DEFAULT '',
					identity_key TEXT NOT NULL,
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL
				);
				CREATE TABLE track_sources (
					id TEXT PRIMARY KEY,
					track_id TEXT NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
					provider TEXT NOT NULL,
					kind TEXT NOT NULL,
					source_id TEXT NOT NULL,
					source_url TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					UNIQUE (track_id, provider, kind, source_id)
				);
				CREATE TABLE jobs (
					id TEXT PRIMARY KEY,
					type TEXT NOT NULL,
					status TEXT NOT NULL,
					label TEXT NOT NULL DEFAULT '',
					metadata_provider TEXT NOT NULL DEFAULT '',
					media_provider TEXT NOT NULL DEFAULT '',
					target_id TEXT NOT NULL DEFAULT '',
					options_json JSONB NOT NULL DEFAULT '{}'::jsonb,
					total INTEGER NOT NULL DEFAULT 0,
					completed INTEGER NOT NULL DEFAULT 0,
					failed INTEGER NOT NULL DEFAULT 0,
					skipped INTEGER NOT NULL DEFAULT 0,
					error_code TEXT NOT NULL DEFAULT '',
					error_message TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL,
					started_at TIMESTAMPTZ,
					finished_at TIMESTAMPTZ,
					CONSTRAINT jobs_status_known CHECK (status IN (
						'queued', 'resolving_artist', 'resolving_releases', 'resolving_tracks',
						'deduplicating', 'matching', 'downloading', 'tagging', 'finalizing',
						'completed', 'failed', 'cancelled'
					))
				);
				CREATE TABLE job_items (
					id TEXT PRIMARY KEY,
					job_id TEXT NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
					position INTEGER NOT NULL,
					status TEXT NOT NULL,
					track_id TEXT REFERENCES tracks (id) ON DELETE SET NULL,
					track_json JSONB NOT NULL,
					label TEXT NOT NULL DEFAULT '',
					media_provider TEXT NOT NULL DEFAULT '',
					media_id TEXT NOT NULL DEFAULT '',
					media_url TEXT NOT NULL DEFAULT '',
					match_score DOUBLE PRECISION NOT NULL DEFAULT 0,
					file_id TEXT,
					attempts INTEGER NOT NULL DEFAULT 0,
					error_code TEXT NOT NULL DEFAULT '',
					error_message TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL,
					started_at TIMESTAMPTZ,
					finished_at TIMESTAMPTZ,
					UNIQUE (job_id, position),
					CONSTRAINT job_items_status_known CHECK (status IN (
						'pending', 'matching', 'downloading', 'tagging',
						'completed', 'failed', 'skipped', 'cancelled'
					))
				);
				CREATE TABLE files (
					id TEXT PRIMARY KEY,
					track_id TEXT REFERENCES tracks (id) ON DELETE SET NULL,
					path TEXT NOT NULL UNIQUE,
					size_bytes BIGINT NOT NULL DEFAULT 0,
					codec TEXT NOT NULL DEFAULT '',
					container TEXT NOT NULL DEFAULT '',
					bitrate_kbps DOUBLE PRECISION NOT NULL DEFAULT 0,
					sample_rate INTEGER NOT NULL DEFAULT 0,
					channels INTEGER NOT NULL DEFAULT 0,
					duration_ms INTEGER NOT NULL DEFAULT 0,
					source_provider TEXT NOT NULL DEFAULT '',
					source_id TEXT NOT NULL DEFAULT '',
					source_url TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL
				);
				CREATE TABLE settings (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL
				);
			`,
		},
		{
			version: 2,
			sql: `
				CREATE TABLE artist_subscriptions (
					id TEXT PRIMARY KEY,
					provider TEXT NOT NULL,
					artist_source_id TEXT NOT NULL,
					artist_name TEXT NOT NULL,
					artist_image_url TEXT NOT NULL DEFAULT '',
					enabled BOOLEAN NOT NULL DEFAULT true,
					auto_download BOOLEAN NOT NULL DEFAULT false,
					last_sync_at TIMESTAMPTZ,
					next_sync_at TIMESTAMPTZ NOT NULL,
					last_sync_status TEXT NOT NULL DEFAULT 'pending',
					last_error TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL,
					UNIQUE (provider, artist_source_id)
				);
			`,
		},
		{
			version: 3,
			sql: `
				CREATE TABLE users (
					id TEXT PRIMARY KEY,
					username TEXT NOT NULL UNIQUE,
					password_hash TEXT NOT NULL,
					role TEXT NOT NULL DEFAULT 'user',
					is_active BOOLEAN NOT NULL DEFAULT true,
					created_at TIMESTAMPTZ NOT NULL,
					updated_at TIMESTAMPTZ NOT NULL
				);
				CREATE TABLE user_sessions (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					token_hash TEXT NOT NULL UNIQUE,
					ip_address TEXT NOT NULL DEFAULT '',
					user_agent TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL,
					last_seen_at TIMESTAMPTZ NOT NULL,
					expires_at TIMESTAMPTZ NOT NULL
				);
			`,
		},
		{
			version: 4,
			sql: `
				ALTER TABLE tracks ADD COLUMN lyrics_state TEXT NOT NULL DEFAULT 'unknown';
				ALTER TABLE tracks ADD COLUMN lyrics_provider TEXT NOT NULL DEFAULT '';
				ALTER TABLE tracks ADD COLUMN lyrics_checked_at TIMESTAMPTZ;
				ALTER TABLE tracks ADD COLUMN compilation BOOLEAN NOT NULL DEFAULT false;
				ALTER TABLE releases ADD COLUMN compilation BOOLEAN NOT NULL DEFAULT false;
			`,
		},
		{
			version: 5,
			sql: `
				CREATE INDEX IF NOT EXISTS idx_tracks_created_at ON tracks (created_at DESC);
				CREATE INDEX IF NOT EXISTS idx_releases_created_at ON releases (created_at DESC);
				CREATE INDEX IF NOT EXISTS idx_releases_year ON releases (year DESC, title);
				CREATE INDEX IF NOT EXISTS idx_tracks_lyrics_state ON tracks (lyrics_state);
			`,
		},
	}

	for _, m := range migrations0001to0005 {
		if _, err := conn.ExecContext(ctx, m.sql); err != nil {
			t.Fatalf("apply migration %04d: %v", m.version, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
			t.Fatalf("record migration %04d: %v", m.version, err)
		}
	}

	// Insert realistic v0.10.0 dataset
	now := time.Now().UTC()
	fixtures := []string{
		`INSERT INTO artists (id, name, sort_key, provider, source_id, created_at, updated_at) VALUES ('art_1', 'Daft Punk', 'daft punk', 'deezer', '27', $1, $1)`,
		`INSERT INTO releases (id, artist_id, title, provider, source_id, created_at, updated_at) VALUES ('rel_1', 'art_1', 'Discovery', 'deezer', '302127', $1, $1)`,
		`INSERT INTO tracks (id, release_id, artist_id, title, identity_key, created_at, updated_at) VALUES ('trk_1', 'rel_1', 'art_1', 'One More Time', 'daft punk|one more time', $1, $1)`,
		`INSERT INTO jobs (id, type, status, target_id, created_at, updated_at) VALUES ('job_1', 'track', 'downloading', 'trk_1', $1, $1)`,
		`INSERT INTO job_items (id, job_id, position, status, track_id, track_json, label, attempts, created_at, updated_at) VALUES ('item_1', 'job_1', 0, 'downloading', 'trk_1', '{"title":"One More Time"}', 'Daft Punk - One More Time', 2, $1, $1)`,
	}
	for _, f := range fixtures {
		if _, err := conn.ExecContext(ctx, f, now); err != nil {
			t.Fatalf("insert v0.10.0 fixture dataset: %v", err)
		}
	}

	// Count rows before migration 0006
	tables := []string{"artists", "releases", "tracks", "track_sources", "jobs", "job_items", "files", "settings", "artist_subscriptions", "users", "user_sessions"}
	countsBefore := make(map[string]int)
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count before %s: %v", tbl, err)
		}
		countsBefore[tbl] = cnt
	}

	// Apply Migration 0006
	migration0006SQL := `
		ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_known;
		ALTER TABLE jobs ADD CONSTRAINT jobs_status_known CHECK (
			status IN (
				'queued', 'resolving_artist', 'resolving_releases', 'resolving_tracks',
				'deduplicating', 'matching', 'downloading', 'tagging', 'finalizing',
				'retry_wait', 'waiting_for_storage', 'waiting_for_space',
				'completed', 'failed', 'cancelled'
			)
		);

		ALTER TABLE job_items DROP CONSTRAINT IF EXISTS job_items_status_known;
		ALTER TABLE job_items ADD CONSTRAINT job_items_status_known CHECK (
			status IN (
				'pending', 'matching', 'downloading', 'tagging', 'finalizing',
				'retry_wait', 'waiting_for_storage', 'waiting_for_space',
				'completed', 'failed', 'skipped', 'cancelled'
			)
		);

		ALTER TABLE job_items
			ADD COLUMN IF NOT EXISTS max_attempts     integer     NOT NULL DEFAULT 5,
			ADD COLUMN IF NOT EXISTS next_retry_at    timestamptz,
			ADD COLUMN IF NOT EXISTS staging_relpath  text        NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS staged_size      bigint      NOT NULL DEFAULT 0,
			ADD COLUMN IF NOT EXISTS staged_sha256    text        NOT NULL DEFAULT '';

		CREATE INDEX IF NOT EXISTS idx_job_items_retry_due
			ON job_items (next_retry_at)
			WHERE status = 'retry_wait';

		CREATE INDEX IF NOT EXISTS idx_job_items_waiting_storage
			ON job_items (status)
			WHERE status IN ('waiting_for_storage', 'waiting_for_space');
	`
	if _, err := conn.ExecContext(ctx, migration0006SQL); err != nil {
		t.Fatalf("execute migration 0006: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (6)"); err != nil {
		t.Fatalf("record migration 0006: %v", err)
	}

	// Verify exact count equality
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count after %s: %v", tbl, err)
		}
		if countsBefore[tbl] != cnt {
			t.Fatalf("table %s count changed: before=%d, after=%d", tbl, countsBefore[tbl], cnt)
		}
	}

	// Test inserting new statuses (waiting_for_storage, retry_wait)
	if _, err := conn.ExecContext(ctx, `
		UPDATE job_items SET status = 'waiting_for_storage', max_attempts = 5, staged_sha256 = 'abc123sha' WHERE id = 'item_1';
		UPDATE jobs SET status = 'waiting_for_storage' WHERE id = 'job_1';
	`); err != nil {
		t.Fatalf("failed to update new v0.11 statuses on upgraded DB: %v", err)
	}
}

func TestMigration0007_UpgradeFromV011(t *testing.T) {
	base := dbtest.URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schema := fmt.Sprintf("musicdl_upgrade_0007_%d", time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to test server: %v", err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		_ = admin.Close(context.Background())
	}()

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	opts := database.Options{
		URL: base + "&search_path=" + schema,
	}
	conn, err := sql.Open("pgx", opts.URL)
	if err != nil {
		t.Fatalf("open raw sql: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	// Apply migrations 0001 to 0006 manually
	for v := 1; v <= 6; v++ {
		content, err := database.MigrationSQL(v)
		if err != nil {
			t.Fatalf("load migration %04d: %v", v, err)
		}
		if _, err := conn.ExecContext(ctx, content); err != nil {
			t.Fatalf("apply migration %04d: %v", v, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", v); err != nil {
			t.Fatalf("record migration %04d: %v", v, err)
		}
	}

	// Insert v0.11 fixture data
	now := time.Now().UTC()
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (
			id, provider, artist_source_id, artist_name, artist_image_url,
			enabled, auto_download, next_sync_at, last_sync_status,
			created_at, updated_at
		) VALUES (
			'sub_1', 'spotify', 'art_123', 'Radiohead', 'https://img.example/rh.jpg',
			true, true, $1, 'pending', $1, $1
		)`, now); err != nil {
		t.Fatalf("insert subscription fixture: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO jobs (
			id, type, status, label, metadata_provider, media_provider, target_id,
			options_json, total, completed, failed, skipped, created_at, updated_at
		) VALUES (
			'job_1', 'artist', 'downloading', 'Radiohead', 'spotify', 'youtube', 'art_123',
			'{}', 10, 2, 0, 0, $1, $1
		)`, now); err != nil {
		t.Fatalf("insert job fixture: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO job_items (
			id, job_id, position, status, track_json, label, created_at, updated_at
		) VALUES (
			'item_1', 'job_1', 1, 'downloading', '{"title": "Karma Police"}', 'Radiohead - Karma Police', $1, $1
		)`, now); err != nil {
		t.Fatalf("insert job_item fixture: %v", err)
	}

	// Record counts before migration 0007
	tables := []string{"artist_subscriptions", "jobs", "job_items"}
	countsBefore := make(map[string]int)
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		countsBefore[tbl] = cnt
	}

	// Apply migration 0007
	migration0007SQL, err := database.MigrationSQL(7)
	if err != nil {
		t.Fatalf("load migration 0007: %v", err)
	}
	if _, err := conn.ExecContext(ctx, migration0007SQL); err != nil {
		t.Fatalf("execute migration 0007: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (7)"); err != nil {
		t.Fatalf("record migration 0007: %v", err)
	}

	// Verify exact count equality
	for _, tbl := range tables {
		var cnt int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&cnt); err != nil {
			t.Fatalf("count after %s: %v", tbl, err)
		}
		if countsBefore[tbl] != cnt {
			t.Fatalf("table %s count changed: before=%d, after=%d", tbl, countsBefore[tbl], cnt)
		}
	}

	// Verify existing subscription received priority 1 (normal) and proper default release filter
	var (
		subPriority      int
		subReleaseFilter string
	)
	if err := conn.QueryRowContext(ctx, `
		SELECT download_priority, release_filter::text FROM artist_subscriptions WHERE id = 'sub_1'
	`).Scan(&subPriority, &subReleaseFilter); err != nil {
		t.Fatalf("query updated subscription: %v", err)
	}
	if subPriority != 1 {
		t.Fatalf("expected existing subscription download_priority=1 (normal), got %d", subPriority)
	}
	if !strings.Contains(subReleaseFilter, `"albums": true`) || !strings.Contains(subReleaseFilter, `"singles": true`) {
		t.Fatalf("expected release filter with albums/singles true, got %s", subReleaseFilter)
	}

	// Verify existing job received priority 1 (normal) and paused=false
	var (
		jobPriority int
		jobPaused   bool
	)
	if err := conn.QueryRowContext(ctx, `
		SELECT priority, paused FROM jobs WHERE id = 'job_1'
	`).Scan(&jobPriority, &jobPaused); err != nil {
		t.Fatalf("query updated job: %v", err)
	}
	if jobPriority != 1 {
		t.Fatalf("expected existing job priority=1 (normal), got %d", jobPriority)
	}
	if jobPaused {
		t.Fatalf("expected existing job paused=false, got true")
	}

	// Verify new indexes exist
	expectedIndexes := []string{
		"idx_jobs_priority_created",
		"idx_jobs_history_pagination",
	}
	for _, idx := range expectedIndexes {
		var exists bool
		err := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes 
				WHERE schemaname = $1 AND indexname = $2
			)`, schema, idx).Scan(&exists)
		if err != nil {
			t.Fatalf("check index %s: %v", idx, err)
		}
		if !exists {
			t.Fatalf("expected index %s to exist after migration 0007", idx)
		}
	}
}

func TestMigration0008_UpgradeFromV012(t *testing.T) {
	base := dbtest.URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schema := fmt.Sprintf("upgrade_0008_%d", time.Now().UnixNano())
	conn, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = conn.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	// 1. Apply migrations 0001 through 0007

	for i := 1; i <= 7; i++ {
		sqlContent, err := database.MigrationSQL(i)
		if err != nil {
			t.Fatalf("load migration %d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, sqlContent); err != nil {
			t.Fatalf("apply migration %d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", i, fmt.Sprintf("%04d_migration", i)); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 2. Insert sample user and artist
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
		VALUES ('user_1', 'admin', 'hash', 'admin', NOW(), NOW());
	`); err != nil {
		t.Fatalf("insert sample user: %v", err)
	}

	// 3. Open via database.Open to run migration 0008
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	schemaURL := base + sep + "search_path=" + schema

	db, err := database.Open(ctx, database.Options{URL: schemaURL})
	if err != nil {
		t.Fatalf("Open with migration 0008: %v", err)
	}
	defer db.Close()

	// 4. Verify version is 8
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("query version: %v", err)
	}
	if version != 8 {
		t.Fatalf("expected version 8, got %d", version)
	}

	// 5. Verify tables and indexes exist
	var runsExists, findingsExists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = 'library_audit_runs'
		)`, schema).Scan(&runsExists); err != nil || !runsExists {
		t.Fatalf("expected library_audit_runs table to exist: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = 'library_audit_findings'
		)`, schema).Scan(&findingsExists); err != nil || !findingsExists {
		t.Fatalf("expected library_audit_findings table to exist: %v", err)
	}
}
