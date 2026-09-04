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

func TestMigration0009_UpgradeFromV016(t *testing.T) {
	baseURL := dbtest.URL(t)
	schema := fmt.Sprintf("upgrade_0009_%d", time.Now().UnixNano())
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

	// 2. Insert test data in Schema 8:
	// - Alan Walker real Deezer row
	// - Alan Walker synthetic YTMusic row
	// - John Williams (film composer, Deezer 1158)
	// - John Williams (guitarist, Deezer 8740)
	now := time.Now().UTC()
	_, err = conn.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, source_url, image_url, created_at, updated_at)
		VALUES
			('alan_real', 'Alan Walker', 'alan walker', 'deezer', '288164', 'https://deezer.com/artist/288164', 'https://img.test/alan.jpg', $1, $1),
			('alan_synth', 'Alan Walker', 'alan walker', 'ytmusic', 'artist:alan-walker', '', '', $1, $1),
			('jw_film', 'John Williams', 'john williams', 'deezer', '1158', 'https://deezer.com/artist/1158', '', $1, $1),
			('jw_guitar', 'John Williams', 'john williams', 'deezer', '8740', 'https://deezer.com/artist/8740', '', $1, $1)`, now)
	if err != nil {
		t.Fatalf("insert Schema 8 test data: %v", err)
	}

	// Record migrations 1-8 in schema_migrations
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version integer PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL
		)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	for i := 1; i <= 8; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`, i, fmt.Sprintf("%04d_migration", i), now); err != nil {
			t.Fatalf("record migration %d: %v", i, err)
		}
	}

	// 3. Apply migration 0009
	sql0009, err := database.MigrationSQL(9)
	if err != nil {
		t.Fatalf("get migration 0009: %v", err)
	}
	if _, err := conn.ExecContext(ctx, sql0009); err != nil {
		t.Fatalf("apply migration 0009: %v", err)
	}

	// 4. Verify artist_sources backfill
	type sourceRecord struct {
		artistID   string
		provider   string
		sourceKind string
		sourceID   string
		isPrimary  bool
	}

	rows, err := conn.QueryContext(ctx, `
		SELECT artist_id, provider, source_kind, source_id, is_primary
		FROM artist_sources
		ORDER BY artist_id, provider`)
	if err != nil {
		t.Fatalf("query artist_sources: %v", err)
	}
	defer rows.Close()

	records := make(map[string]sourceRecord)
	for rows.Next() {
		var r sourceRecord
		if err := rows.Scan(&r.artistID, &r.provider, &r.sourceKind, &r.sourceID, &r.isPrimary); err != nil {
			t.Fatalf("scan artist_sources: %v", err)
		}
		records[r.artistID] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate artist_sources: %v", err)
	}

	if len(records) != 4 {
		t.Fatalf("expected 4 artist_sources records, got %d", len(records))
	}

	// Verify Alan Walker real
	alanReal, ok := records["alan_real"]
	if !ok || alanReal.sourceKind != "external" || alanReal.sourceID != "288164" || !alanReal.isPrimary {
		t.Fatalf("alan_real source mismatch: %+v", alanReal)
	}

	// Verify Alan Walker synthetic
	alanSynth, ok := records["alan_synth"]
	if !ok || alanSynth.sourceKind != "legacy_synthetic" || alanSynth.sourceID != "artist:alan-walker" {
		t.Fatalf("alan_synth source mismatch: %+v", alanSynth)
	}

	// Verify John Williams film composer
	jwFilm, ok := records["jw_film"]
	if !ok || jwFilm.sourceKind != "external" || jwFilm.sourceID != "1158" {
		t.Fatalf("jw_film source mismatch: %+v", jwFilm)
	}

	// Verify John Williams classical guitarist
	jwGuitar, ok := records["jw_guitar"]
	if !ok || jwGuitar.sourceKind != "external" || jwGuitar.sourceID != "8740" {
		t.Fatalf("jw_guitar source mismatch: %+v", jwGuitar)
	}

	// 5. Verify artists_provider_source_id_key constraint was dropped from artists
	var hasOldConstraint bool
	err = conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'artists'::regclass
			  AND conname = 'artists_provider_source_id_key'
		)`).Scan(&hasOldConstraint)
	if err != nil {
		t.Fatalf("check dropped constraint: %v", err)
	}
	if hasOldConstraint {
		t.Fatalf("artists_provider_source_id_key should have been dropped")
	}

	// 6. Verify UNIQUE (provider, source_id) in artist_sources prevents duplicates
	_, err = conn.ExecContext(ctx, `
		INSERT INTO artist_sources (id, artist_id, provider, source_kind, source_id, created_at, updated_at)
		VALUES ('dup_src', 'alan_synth', 'deezer', 'external', '288164', now(), now())`)
	if err == nil {
		t.Fatalf("expected duplicate source_id to fail unique constraint in artist_sources")
	}
}
