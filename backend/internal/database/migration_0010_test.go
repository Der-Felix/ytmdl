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

func TestMigration0010_UpgradeFromSchema9(t *testing.T) {
	baseURL := dbtest.URL(t)
	schema := fmt.Sprintf("upgrade_0010_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	// 1. Apply migrations 1 through 9 (Schema 9)
	for i := 1; i <= 9; i++ {
		sqlContent, err := database.MigrationSQL(i)
		if err != nil {
			t.Fatalf("get migration %04d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, sqlContent); err != nil {
			t.Fatalf("apply migration %04d: %v", i, err)
		}
	}

	now := time.Now().UTC()

	// 2. Insert Schema 9 test data (priorities 0, 1, 2)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
		VALUES
			('job_low', 'artist', 'queued', 'Low Job', 'spotify', 'ytmusic', 't_low', 0, $1, $1),
			('job_norm', 'artist', 'queued', 'Normal Job', 'spotify', 'ytmusic', 't_norm', 1, $1, $1),
			('job_high', 'artist', 'queued', 'High Job', 'spotify', 'ytmusic', 't_high', 2, $1, $1);
	`, now)
	if err != nil {
		t.Fatalf("insert Schema 9 jobs: %v", err)
	}

	_, err = conn.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
		VALUES
			('sub_low', 'Low Artist', 'spotify', 's_low', $1, 0, $1, $1),
			('sub_norm', 'Normal Artist', 'spotify', 's_norm', $1, 1, $1, $1),
			('sub_high', 'High Artist', 'spotify', 's_high', $1, 2, $1, $1);
	`, now)
	if err != nil {
		t.Fatalf("insert Schema 9 subscriptions: %v", err)
	}

	// In Schema 9, priority 3 MUST fail check constraint
	_, err = conn.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
		VALUES ('job_pri3_fail', 'artist', 'queued', 'Urgent Job', 'spotify', 'ytmusic', 't_urgent', 3, $1, $1);
	`, now)
	if err == nil {
		t.Fatal("expected error inserting priority 3 in Schema 9, got nil")
	}

	// Record migrations 1-9 in schema_migrations
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	for i := 1; i <= 9; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`, i, fmt.Sprintf("%04d_migration", i), now); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 3. Apply migration 0010 (Upgrade to Schema 10)
	sql0010, err := database.MigrationSQL(10)
	if err != nil {
		t.Fatalf("get migration 0010: %v", err)
	}
	if _, err := conn.ExecContext(ctx, sql0010); err != nil {
		t.Fatalf("apply migration 0010: %v", err)
	}

	// 4. Verify existing rows with priority 0, 1, 2 remain unchanged
	var pLow, pNorm, pHigh int
	if err := conn.QueryRowContext(ctx, `SELECT priority FROM jobs WHERE id = 'job_low'`).Scan(&pLow); err != nil || pLow != 0 {
		t.Fatalf("expected job_low priority 0, got %d (err: %v)", pLow, err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT priority FROM jobs WHERE id = 'job_norm'`).Scan(&pNorm); err != nil || pNorm != 1 {
		t.Fatalf("expected job_norm priority 1, got %d (err: %v)", pNorm, err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT priority FROM jobs WHERE id = 'job_high'`).Scan(&pHigh); err != nil || pHigh != 2 {
		t.Fatalf("expected job_high priority 2, got %d (err: %v)", pHigh, err)
	}

	// 5. In Schema 10, inserting priority 3 now SUCCEEDS for jobs and subscriptions
	_, err = conn.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
		VALUES ('job_very_high', 'artist', 'queued', 'Very High Job', 'spotify', 'ytmusic', 't_vh', 3, $1, $1);
	`, now)
	if err != nil {
		t.Fatalf("inserting priority 3 job in Schema 10 failed: %v", err)
	}

	_, err = conn.ExecContext(ctx, `
		INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
		VALUES ('sub_very_high', 'Very High Artist', 'spotify', 's_vh', $1, 3, $1, $1);
	`, now)
	if err != nil {
		t.Fatalf("inserting download_priority 3 subscription in Schema 10 failed: %v", err)
	}

	// 6. In Schema 10, invalid priorities (< 0 or > 3) are still REJECTED
	for _, badP := range []int{-1, 4, 99} {
		_, err = conn.ExecContext(ctx, `
			INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
			VALUES ($1, 'artist', 'queued', 'Bad Job', 'spotify', 'ytmusic', 't_bad', $2, $3, $3);
		`, fmt.Sprintf("job_bad_%d", badP), badP, now)
		if err == nil {
			t.Fatalf("expected error for job priority %d, got nil", badP)
		}

		_, err = conn.ExecContext(ctx, `
			INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
			VALUES ($1, 'Bad Artist', 'spotify', 's_bad', $3, $2, $3, $3);
		`, fmt.Sprintf("sub_bad_%d", badP), badP, now)
		if err == nil {
			t.Fatalf("expected error for subscription priority %d, got nil", badP)
		}
	}
}

func TestMigration0010_FreshDB(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 migrations applied, got %d", count)
	}

	now := time.Now().UTC()

	// Verify priorities 0, 1, 2, 3 accepted on fresh DB
	for p := 0; p <= 3; p++ {
		_, err := db.ExecContext(ctx, `
			INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
			VALUES ($1, 'artist', 'queued', $2, 'spotify', 'ytmusic', $3, $4, $5, $5);
		`, fmt.Sprintf("fresh_job_%d", p), fmt.Sprintf("Fresh Job %d", p), fmt.Sprintf("target_%d", p), p, now)
		if err != nil {
			t.Fatalf("inserting priority %d job on fresh DB failed: %v", p, err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
			VALUES ($1, $2, 'spotify', $3, $4, $5, $4, $4);
		`, fmt.Sprintf("fresh_sub_%d", p), fmt.Sprintf("Fresh Sub %d", p), fmt.Sprintf("asrc_%d", p), now, p)
		if err != nil {
			t.Fatalf("inserting download_priority %d sub on fresh DB failed: %v", p, err)
		}
	}

	// Verify priorities -1, 4 rejected on fresh DB
	for _, badP := range []int{-1, 4} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id, priority, created_at, updated_at)
			VALUES ($1, 'artist', 'queued', 'Bad Job', 'spotify', 'ytmusic', 'target_bad', $2, $3, $3);
		`, fmt.Sprintf("fresh_bad_job_%d", badP), badP, now)
		if err == nil {
			t.Fatalf("expected error inserting bad priority %d into jobs on fresh DB, got nil", badP)
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
			VALUES ($1, 'Bad Sub', 'spotify', 'asrc_bad', $2, $3, $2, $2);
		`, fmt.Sprintf("fresh_bad_sub_%d", badP), now, badP)
		if err == nil {
			t.Fatalf("expected error inserting bad download_priority %d into subs on fresh DB, got nil", badP)
		}
	}
}
