package database_test

import (
	"context"
	"testing"
	"ytdm/backend/internal/database/dbtest"
)

func TestMigration0013UpgradePreservesMetadata(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	// Reconstruct schema 12 inside this isolated schema and seed an existing artist.
	if _, err := db.ExecContext(ctx, `ALTER TABLE artists DROP COLUMN genres_json, DROP COLUMN genres_manual;
 DELETE FROM schema_migrations WHERE version = 13;
 INSERT INTO artists (id,name,sort_key,provider,source_id,image_url,created_at,updated_at)
 VALUES ('existing','Existing','existing','ytmusic','artist:existing','cover-fixture',now(),now());`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var genres, image string
	var manual bool
	var version int
	if err := db.QueryRowContext(ctx, `SELECT genres_json,image_url,genres_manual FROM artists WHERE id = 'existing'`).Scan(&genres, &image, &manual); err != nil {
		t.Fatal(err)
	}
	if genres != "[]" || image != "cover-fixture" || manual {
		t.Fatal("migration changed existing metadata or did not initialize empty genres")
	}
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 13 {
		t.Fatalf("schema %d: %v", version, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE artists SET genres_json = '["Pop"]', genres_manual = true WHERE id = 'existing'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT genres_json FROM artists WHERE id = 'existing'`).Scan(&genres); err != nil || genres != "[\"Pop\"]" {
		t.Fatal("restart lost genre metadata")
	}
}
