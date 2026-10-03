package repository_test

import (
	"context"
	"testing"
	"ytdm/backend/internal/database/repository"
)

func TestLocalRadioUsesLocalFilesGenresAndPrivateAffinity(t *testing.T) {
	db, _, user, other, ids := setupPlaylistTest(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO artists(id,name,provider,source_id,genres_json,created_at,updated_at) VALUES('radio-pop','Pop Artist','fixture','pop','["Pop"]',now(),now()),('radio-rock','Rock Artist','fixture','rock','["Rock"]',now(),now());UPDATE tracks SET artist_id='radio-rock'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET artist_id='radio-pop' WHERE id IN($1,$2)`, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO favorite_tracks(user_id,track_id,created_at)VALUES($1,$2,now()),($3,$4,now())`, user, ids[0], other, ids[3]); err != nil {
		t.Fatal(err)
	}
	a, err := c.LocalRadio(ctx, user, "", "", "salt")
	if err != nil || len(a) != 4 || a[0].ID != ids[0] {
		t.Fatal("user affinity absent", err)
	}
	b, err := c.LocalRadio(ctx, other, "", "", "salt")
	if err != nil || len(b) != 4 || b[0].ID != ids[3] {
		t.Fatal("other user preference leaked", err)
	}
	pop, err := c.LocalRadio(ctx, user, ids[0], "pOp", "salt")
	if err != nil || len(pop) != 1 || pop[0].ID != ids[1] {
		t.Fatal("genre or seed incorrect", err)
	}
	if _, err = c.LocalRadio(ctx, user, "missing", "", "salt"); err == nil {
		t.Fatal("missing seed accepted")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM files WHERE track_id=$1`, ids[3]); err != nil {
		t.Fatal(err)
	}
	local, err := c.LocalRadio(ctx, user, "", "", "salt")
	if err != nil || len(local) != 3 {
		t.Fatal("unavailable song included", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET title='Same' WHERE id IN($1,$2)`, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	dedup, err := c.LocalRadio(ctx, user, "", "", "salt")
	if err != nil || len(dedup) != 2 {
		t.Fatal("recording variants not deduplicated", err)
	}
}
