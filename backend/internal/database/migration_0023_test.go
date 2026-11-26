package database_test

import (
	"context"
	"testing"

	"ytdm/backend/internal/database/dbtest"
)

// TestMigrations0023And0024AddListeningTables pins the two listening
// migrations: they only add tables, apply to a database that has rows, and
// listening_exclusions accepts exactly one target matching its kind, once
// per user (genres case-insensitively).
func TestMigrations0023And0024AddListeningTables(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE track_lyrics_search; DROP TABLE listening_exclusions;
 DELETE FROM schema_migrations WHERE version IN (23,24);
 INSERT INTO users(id,username,password_hash,role,enabled,created_at,updated_at) VALUES('m23','m23','hash','user',true,now(),now());
 INSERT INTO artists(id,name,provider,source_id,created_at,updated_at) VALUES('m23-a','A','fixture','m23-a',now(),now());
 INSERT INTO tracks(id,artist_id,title,identity_key,created_at,updated_at) VALUES('m23-t','m23-a','T','m23-t',now(),now());`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version < 24 {
		t.Fatal("schema version", version, err)
	}
	for _, ok := range []string{
		`INSERT INTO listening_exclusions(id,user_id,kind,artist_id) VALUES('e1','m23','artist','m23-a')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,track_id) VALUES('e2','m23','track','m23-t')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,genre) VALUES('e3','m23','genre','Schlager')`,
		`INSERT INTO track_lyrics_search(track_id,body,document) VALUES('m23-t','Über den Wolken',to_tsvector('simple','uber den wolken'))`,
	} {
		if _, err := db.ExecContext(ctx, ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`INSERT INTO listening_exclusions(id,user_id,kind,genre) VALUES('b1','m23','genre',' SCHLAGER ')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,artist_id) VALUES('b2','m23','artist','m23-a')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,genre) VALUES('b3','m23','artist','x')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,artist_id,track_id) VALUES('b4','m23','track','m23-a','m23-t')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,genre) VALUES('b5','m23','genre','  ')`,
		`INSERT INTO listening_exclusions(id,user_id,kind,genre) VALUES('b6','m23','mood','calm')`,
	} {
		if _, err := db.ExecContext(ctx, bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM track_lyrics_search WHERE document @@ to_tsquery('simple','wolk:*')`).Scan(&n); err != nil || n != 1 {
		t.Fatal("lyrics index", n, err)
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM tracks WHERE id='m23-t'`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM track_lyrics_search)+(SELECT count(*) FROM listening_exclusions WHERE kind='track')`).Scan(&n); err != nil || n != 0 {
		t.Fatal("rows of a deleted track kept", n, err)
	}
}
