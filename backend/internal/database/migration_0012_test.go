package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
)

func TestMigration0012_UpgradeFromSchema11(t *testing.T) {
	baseURL := dbtest.URL(t)
	schema := fmt.Sprintf("upgrade_0012_%d", time.Now().UnixNano())
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

	// 1. Apply migrations 1 through 11 (Schema 11)
	for i := 1; i <= 11; i++ {
		sqlContent, err := database.MigrationSQL(i)
		if err != nil {
			t.Fatalf("get migration %04d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, sqlContent); err != nil {
			t.Fatalf("apply migration %04d: %v", i, err)
		}
	}

	// Record migrations 1-11 in schema_migrations
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	now := time.Now().UTC()
	for i := 1; i <= 11; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`,
			i, fmt.Sprintf("%04d_migration", i), now); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 2. Insert Schema 11 test fixtures across all core tables:
	// - Users
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES ('u_test_1', 'admin', 'hash123', 'admin', true, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// - Subscriptions
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
		VALUES ('trk_1', 'rel_1', 'art_1', 'One More Time', 'id_key_1', 320000, $1, $1),
		       ('trk_2', 'rel_1', 'art_1', 'Aerodynamic', 'id_key_2', 212000, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert tracks: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO files (id, track_id, path, size_bytes, codec, bitrate_kbps, created_at, updated_at)
		VALUES ('fil_1', 'trk_1', 'Daft Punk/Discovery/01.flac', 10485760, 'flac', 900, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert files: %v", err)
	}

	// - Media Sessions (Schema 11 table)
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO media_sessions (id, provider_family, name, cookie_ref, enabled, health_status, consecutive_failures, created_at, updated_at)
		VALUES ('a0000000-0000-0000-0000-000000000001', 'youtube', 'test_session', 'sess_test', true, 'healthy', 0, $1, $1);
	`, now); err != nil {
		t.Fatalf("insert media session: %v", err)
	}

	// - Jobs
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, created_at, updated_at)
		VALUES ('job_1', 'artist', 'completed', 'Daft Punk', $1, $1);
	`, now); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	// 3. Open via database.Open to run migration 12
	db, err := database.Open(ctx, database.Options{
		URL: baseURL + "&search_path=" + schema,
	})
	if err != nil {
		t.Fatalf("database.Open failed: %v", err)
	}
	defer db.Close()

	// 4. Verify migration 12 was recorded in schema_migrations
	var maxVersion int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maxVersion); err != nil {
		t.Fatalf("query max version: %v", err)
	}
	if maxVersion != 12 {
		t.Fatalf("expected schema version 12, got %d", maxVersion)
	}

	// 5. Verify all new tables exist
	for _, tbl := range []string{"playlists", "playlist_tracks", "favorite_tracks"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables 
				WHERE table_schema = CURRENT_SCHEMA() AND table_name = $1
			)`, tbl).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", tbl, err)
		}
		if !exists {
			t.Fatalf("expected table %s to exist after migration", tbl)
		}
	}

	// 6. Verify indexes exist
	expectedIndexes := []string{
		"idx_playlists_user_id",
		"idx_playlists_user_updated",
		"idx_playlist_tracks_order",
		"idx_playlist_tracks_track",
		"idx_favorite_tracks_user",
		"idx_favorite_tracks_track",
	}
	for _, idx := range expectedIndexes {
		var exists bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes 
				WHERE schemaname = CURRENT_SCHEMA() AND indexname = $1
			)`, idx).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", idx, err)
		}
		if !exists {
			t.Fatalf("expected index %s to exist", idx)
		}
	}

	// 7. Verify all Schema 11 fixtures are intact
	var userCount, subCount, artCount, relCount, trkCount, filCount, sessCount, jobCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = 'u_test_1'").Scan(&userCount); err != nil || userCount != 1 {
		t.Fatalf("users preserved: count=%d, err=%v", userCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM artist_subscriptions WHERE id = 'sub_test_1'").Scan(&subCount); err != nil || subCount != 1 {
		t.Fatalf("subscriptions preserved: count=%d, err=%v", subCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM artists WHERE id = 'art_1'").Scan(&artCount); err != nil || artCount != 1 {
		t.Fatalf("artists preserved: count=%d, err=%v", artCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM releases WHERE id = 'rel_1'").Scan(&relCount); err != nil || relCount != 1 {
		t.Fatalf("releases preserved: count=%d, err=%v", relCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracks WHERE release_id = 'rel_1'").Scan(&trkCount); err != nil || trkCount != 2 {
		t.Fatalf("tracks preserved: count=%d, err=%v", trkCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM files WHERE id = 'fil_1'").Scan(&filCount); err != nil || filCount != 1 {
		t.Fatalf("files preserved: count=%d, err=%v", filCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_sessions WHERE name = 'test_session'").Scan(&sessCount); err != nil || sessCount != 1 {
		t.Fatalf("media_sessions preserved: count=%d, err=%v", sessCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE id = 'job_1'").Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("jobs preserved: count=%d, err=%v", jobCount, err)
	}

	// 8. Test inserting playlist, playlist_tracks, and favorite_tracks
	if _, err := db.ExecContext(ctx, `
		INSERT INTO playlists (id, user_id, name, description, created_at, updated_at)
		VALUES ('pl_1', 'u_test_1', 'French Touch', 'Best of Daft Punk', $1, $1)
	`, now); err != nil {
		t.Fatalf("insert playlist: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at)
		VALUES ('pl_1', 'trk_1', 1, $1),
		       ('pl_1', 'trk_2', 2, $1)
	`, now); err != nil {
		t.Fatalf("insert playlist tracks: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO favorite_tracks (user_id, track_id, created_at)
		VALUES ('u_test_1', 'trk_1', $1)
	`, now); err != nil {
		t.Fatalf("insert favorite track: %v", err)
	}

	// 9. Re-running Migrate() on already-Schema-12 DB is safe/idempotent
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("re-run Migrate() failed: %v", err)
	}

	// 10. Simulate backend restart: open new connection to same database and verify persisted playlist/favorites
	dbRestarted, err := database.Open(ctx, database.Options{
		URL: baseURL + "&search_path=" + schema,
	})
	if err != nil {
		t.Fatalf("database.Open on restarted backend: %v", err)
	}
	defer dbRestarted.Close()

	var plName string
	if err := dbRestarted.QueryRowContext(ctx, "SELECT name FROM playlists WHERE id = 'pl_1'").Scan(&plName); err != nil || plName != "French Touch" {
		t.Fatalf("persisted playlist across restart: got %q, err: %v", plName, err)
	}
	var favCount int
	if err := dbRestarted.QueryRowContext(ctx, "SELECT COUNT(*) FROM favorite_tracks WHERE user_id = 'u_test_1'").Scan(&favCount); err != nil || favCount != 1 {
		t.Fatalf("persisted favorites across restart: got %d, err: %v", favCount, err)
	}
}

func TestMigration0012_FreshDB(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	if count < 12 {
		t.Fatalf("expected at least 12 migrations applied on fresh DB, got %d", count)
	}

	for _, tbl := range []string{"playlists", "playlist_tracks", "favorite_tracks"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables 
				WHERE table_schema = CURRENT_SCHEMA() AND table_name = $1
			)`, tbl).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", tbl, err)
		}
		if !exists {
			t.Fatalf("expected table %s to exist on fresh DB", tbl)
		}
	}
}
