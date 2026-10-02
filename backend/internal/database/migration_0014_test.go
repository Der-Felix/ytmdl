package database_test

import (
	"context"
	"testing"
	"ytdm/backend/internal/database/dbtest"
)

func TestMigration0014PreservesExistingLibraryAndRestarts(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `DROP TRIGGER remove_artist_artwork ON artists;DROP TRIGGER remove_release_artwork ON releases;DROP FUNCTION remove_library_artwork();DROP TABLE track_loudness,listening_history,playback_events,library_artwork,track_overrides;ALTER TABLE playlists DROP COLUMN smart_rules;DELETE FROM schema_migrations WHERE version=14;
 INSERT INTO users(id,username,password_hash,role,enabled,created_at,updated_at) VALUES('u14','fixture','hash','user',true,now(),now());
 INSERT INTO tracks(id,title,identity_key,created_at,updated_at) VALUES('t14','Existing','fixture14',now(),now());
 INSERT INTO playlists(id,user_id,name,description,created_at,updated_at) VALUES('p14','u14','Existing list','keep',now(),now());
 INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES('p14','t14',1,now());
 INSERT INTO favorite_tracks(user_id,track_id,created_at) VALUES('u14','t14',now());`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM playlist_tracks pt JOIN playlists p ON p.id=pt.playlist_id JOIN favorite_tracks f ON f.track_id=pt.track_id AND f.user_id=p.user_id WHERE p.id='p14' AND p.description='keep' AND p.smart_rules IS NULL AND pt.position=1`).Scan(&count); err != nil || count != 1 {
			t.Fatal("migration/restart lost membership", err)
		}
	}
}
