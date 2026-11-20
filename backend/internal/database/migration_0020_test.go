package database_test

import (
	"context"
	"database/sql"
	"testing"

	"ytdm/backend/internal/database/dbtest"
)

// TestMigration0020LeavesExistingSubscriptionsFollowingTheSetting pins the
// migration: a subscription stored before the column existed has no choice of
// its own (NULL), so it follows the server-wide setting, which is on by
// default and therefore keeps every release the provider lists.
func TestMigration0020LeavesExistingSubscriptionsFollowingTheSetting(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `ALTER TABLE artist_subscriptions DROP COLUMN include_appearances;
 DELETE FROM schema_migrations WHERE version=20;
 INSERT INTO artist_subscriptions (id, artist_name, provider, artist_source_id, next_sync_at, download_priority, created_at, updated_at)
 VALUES ('sub20', 'Bebe Rexha', 'deezer', '4492087', now(), 1, now(), now());`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		var include sql.NullBool
		if err = db.QueryRowContext(ctx,
			`SELECT include_appearances FROM artist_subscriptions WHERE id = 'sub20'`).Scan(&include); err != nil {
			t.Fatal(err)
		}
		if include.Valid {
			t.Fatalf("an existing subscription must follow the setting (NULL), got %v", include.Bool)
		}
	}

	var nullable string
	if err = db.QueryRowContext(ctx, `SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'artist_subscriptions' AND column_name = 'include_appearances'`).Scan(&nullable); err != nil {
		t.Fatal(err)
	}
	if nullable != "YES" {
		t.Fatalf("include_appearances must be nullable, is_nullable = %q", nullable)
	}
}
