package database_test

import (
	"context"
	"testing"
	"ytdm/backend/internal/database/dbtest"
)

func TestMigration0016LeavesExistingSongsAndMembershipsIntact(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TABLE library_trash;DELETE FROM schema_migrations WHERE version=16;
 INSERT INTO users(id,username,password_hash,role,created_at,updated_at) VALUES('u16','u16','fixture','admin',now(),now());
 INSERT INTO tracks(id,title,identity_key,created_at,updated_at) VALUES('t16','Keep','key16',now(),now());
 INSERT INTO files(id,track_id,path,created_at,updated_at) VALUES('f16','t16','fixture16.opus',now(),now());
 INSERT INTO playlists(id,user_id,name,created_at,updated_at) VALUES('p16','u16','Keep',now(),now());
 INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES('p16','t16',1,now());
 INSERT INTO favorite_tracks(user_id,track_id,created_at) VALUES('u16','t16',now());`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		var n int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM files f JOIN tracks t ON t.id=f.track_id JOIN playlist_tracks p ON p.track_id=t.id JOIN favorite_tracks v ON v.track_id=t.id WHERE t.id='t16' AND p.position=1`).Scan(&n); err != nil || n != 1 {
			t.Fatal("migration changed existing library", err)
		}
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM library_trash`).Scan(&n); err != nil || n != 0 {
			t.Fatal("migration archived a recording", err)
		}
	}
}
