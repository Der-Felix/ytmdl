package database_test

import (
	"context"
	"testing"

	"ytdm/backend/internal/database/dbtest"
)

// TestMigration0020KeepsExistingSubscriptionsIncludingAppearances pins the
// default: a subscription stored before the switch existed keeps considering
// every release the provider lists.
func TestMigration0020KeepsExistingSubscriptionsIncludingAppearances(t *testing.T) {
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
		var include bool
		if err = db.QueryRowContext(ctx,
			`SELECT include_appearances FROM artist_subscriptions WHERE id = 'sub20'`).Scan(&include); err != nil {
			t.Fatal(err)
		}
		if !include {
			t.Fatal("an existing subscription must keep including appearances")
		}
	}
}
