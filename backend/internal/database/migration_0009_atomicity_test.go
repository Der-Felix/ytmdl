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

func TestMigration0009_Atomicity_RollbackOnFailure(t *testing.T) {
	baseURL := dbtest.URL(t)
	schema := fmt.Sprintf("atomicity_0009_%d", time.Now().UnixNano())
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

	// 1. Apply migrations 1 through 8
	for i := 1; i <= 8; i++ {
		sqlContent, err := database.MigrationSQL(i)
		if err != nil {
			t.Fatalf("get migration %04d: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, sqlContent); err != nil {
			t.Fatalf("apply migration %04d: %v", i, err)
		}
	}

	// 2. Setup schema_migrations table with versions 1-8
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	now := time.Now().UTC()
	for i := 1; i <= 8; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`, i, fmt.Sprintf("%04d_migration", i), now); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 3. Insert Schema 8 artist row
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, source_url, image_url, created_at, updated_at)
		VALUES ('art_1', 'Artist One', 'artist one', 'deezer', '1001', 'https://deezer.com/artist/1001', '', $1, $1)`, now); err != nil {
		t.Fatalf("insert artist: %v", err)
	}

	// Verify pre-state:
	// - artist_sources table must not exist
	// - artists_provider_source_id_key constraint must exist on artists
	var tableExists bool
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = 'artist_sources'
		)`, schema).Scan(&tableExists)
	if err != nil || tableExists {
		t.Fatalf("artist_sources should not exist yet: exists=%v, err=%v", tableExists, err)
	}

	var hasConstraint bool
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_namespace n ON n.oid = c.connamespace
			WHERE n.nspname = $1 AND c.conname = 'artists_provider_source_id_key'
		)`, schema).Scan(&hasConstraint)
	if err != nil || !hasConstraint {
		t.Fatalf("artists_provider_source_id_key should exist before migration 9: has=%v, err=%v", hasConstraint, err)
	}

	// 4. Attempt to run migration 0009 with an intentional fault injected at the very end
	validSQL, err := database.MigrationSQL(9)
	if err != nil {
		t.Fatalf("get migration 0009: %v", err)
	}
	faultySQL := validSQL + "\n-- INJECTED FAULT\nDO $$ BEGIN RAISE EXCEPTION 'intentional transaction fault'; END $$;\n"

	// Run faulty migration in transaction (mirroring database.applyMigration)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_, txErr := tx.ExecContext(ctx, faultySQL)
	if txErr == nil {
		_ = tx.Rollback()
		t.Fatalf("expected error from injected fault, got nil")
	}
	_ = tx.Rollback()

	// 5. Verify transaction rollback:
	// - artist_sources table must NOT exist
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = 'artist_sources'
		)`, schema).Scan(&tableExists)
	if err != nil || tableExists {
		t.Errorf("artist_sources table exists after rolled-back transaction! exists=%v, err=%v", tableExists, err)
	}

	// - artists_provider_source_id_key constraint must STILL exist
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_namespace n ON n.oid = c.connamespace
			WHERE n.nspname = $1 AND c.conname = 'artists_provider_source_id_key'
		)`, schema).Scan(&hasConstraint)
	if err != nil || !hasConstraint {
		t.Errorf("artists_provider_source_id_key was dropped even though transaction failed! has=%v, err=%v", hasConstraint, err)
	}

	// - schema_migrations must still be at version 8
	var maxVersion int
	err = conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion)
	if err != nil || maxVersion != 8 {
		t.Errorf("schema_migrations version changed after failed transaction! version=%d, err=%v", maxVersion, err)
	}

	// 6. Now apply legitimate migration 0009 in transaction and commit
	cleanTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin clean tx: %v", err)
	}
	if _, err := cleanTx.ExecContext(ctx, validSQL); err != nil {
		_ = cleanTx.Rollback()
		t.Fatalf("clean migration 0009 failed: %v", err)
	}
	if _, err := cleanTx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (9, '0009_artist_sources', now())`); err != nil {
		_ = cleanTx.Rollback()
		t.Fatalf("record clean migration 0009: %v", err)
	}
	if err := cleanTx.Commit(); err != nil {
		t.Fatalf("commit clean migration 0009: %v", err)
	}

	// 7. Verify post-state:
	// - artist_sources table exists
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_schema = $1 AND table_name = 'artist_sources'
		)`, schema).Scan(&tableExists)
	if err != nil || !tableExists {
		t.Errorf("artist_sources table does not exist after clean migration: exists=%v, err=%v", tableExists, err)
	}

	// - artists_provider_source_id_key constraint dropped
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_namespace n ON n.oid = c.connamespace
			WHERE n.nspname = $1 AND c.conname = 'artists_provider_source_id_key'
		)`, schema).Scan(&hasConstraint)
	if err != nil || hasConstraint {
		t.Errorf("artists_provider_source_id_key should have been dropped: has=%v, err=%v", hasConstraint, err)
	}

	// - schema_migrations max version is 9
	err = conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion)
	if err != nil || maxVersion != 9 {
		t.Errorf("expected schema_migrations version 9, got %d (err: %v)", maxVersion, err)
	}

	// - backfill verified
	var backfillCount int
	err = conn.QueryRowContext(ctx, `SELECT count(*) FROM artist_sources WHERE artist_id = 'art_1'`).Scan(&backfillCount)
	if err != nil || backfillCount != 1 {
		t.Errorf("expected 1 backfilled source, got %d (err: %v)", backfillCount, err)
	}
}
