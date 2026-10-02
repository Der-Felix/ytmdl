package repository_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"ytdm/backend/internal/database/repository"
)

func TestDuplicateReviewIsolationStalenessAndRemovalValidation(t *testing.T) {
	db, ps, u1, u2, ids := setupPlaylistTest(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET title='Same',album_artist='Artist' WHERE id IN ($1,$2,$3)`, ids[0], ids[1], ids[2]); err != nil {
		t.Fatal(err)
	}
	list, err := c.DuplicateGroupsForUser(ctx, u1, 0, false, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("candidates: %v %v", list, err)
	}
	g := list[0]
	r := repository.DuplicateReview{GroupKey: g.Key, Fingerprint: g.Fingerprint, Outcome: "preferred", PreferredTrackID: ids[0]}
	p, err := ps.CreatePlaylist(ctx, u1, "Preserve", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ps.AddTracks(ctx, u1, p.ID, ids[:3]); err != nil {
		t.Fatal(err)
	}
	if err = ps.FavoriteTrack(ctx, u1, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err = c.ValidateDuplicateRemoval(ctx, u1, r, []string{ids[1]}); err == nil {
		t.Fatal("unsaved review accepted")
	}
	if err = c.SaveDuplicateReview(ctx, u1, r); err != nil {
		t.Fatal(err)
	}
	mine, err := c.DuplicateGroupsForUser(ctx, u1, 0, false, "")
	if err != nil || len(mine) != 0 {
		t.Fatal("review not hidden", err)
	}
	other, err := c.DuplicateGroupsForUser(ctx, u2, 0, false, "")
	if err != nil || len(other) != 1 || other[0].Outcome != "" {
		t.Fatal("decision leaked", err)
	}
	if err = c.ValidateDuplicateRemoval(ctx, u2, r, []string{ids[1]}); err == nil {
		t.Fatal("foreign review accepted")
	}
	for _, bad := range [][]string{{ids[0]}, {ids[1], ids[1]}, {ids[3]}, {"missing"}, {}} {
		if err = c.ValidateDuplicateRemoval(ctx, u1, r, bad); err == nil {
			t.Fatal("invalid deletion set accepted", bad)
		}
	}
	if err = c.ValidateDuplicateRemoval(ctx, u1, r, ids[1:3]); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM playlist_tracks WHERE playlist_id=$1`, p.ID).Scan(&n); err != nil || n != 3 {
		t.Fatal("review changed playlist", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM favorite_tracks WHERE user_id=$1`, u1).Scan(&n); err != nil || n != 1 {
		t.Fatal("review changed favorites", err)
	}
	// File replacement invalidates both a saved review and a pending confirmed deletion.
	if _, err = db.ExecContext(ctx, `UPDATE files SET size_bytes=size_bytes+1 WHERE track_id=$1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err = c.SaveDuplicateReview(ctx, u1, r); err == nil {
		t.Fatal("stale choice saved")
	}
	if err = c.ValidateDuplicateRemoval(ctx, u1, r, []string{ids[1]}); err == nil {
		t.Fatal("stale deletion accepted")
	}
	mine, err = c.DuplicateGroupsForUser(ctx, u1, 0, false, "")
	if err != nil || len(mine) != 1 || mine[0].Outcome != "" {
		t.Fatal("changed group hidden", err)
	}
	r.Fingerprint = mine[0].Fingerprint
	r.Outcome = "distinct"
	r.PreferredTrackID = ""
	if err = c.SaveDuplicateReview(ctx, u1, r); err != nil {
		t.Fatal(err)
	}
	if err = c.ResetDuplicateReview(ctx, u2, g.Key); err != nil {
		t.Fatal(err)
	}
	mine, _ = c.DuplicateGroupsForUser(ctx, u1, 0, false, "")
	if len(mine) != 0 {
		t.Fatal("foreign reset changed decision")
	}
	if err = c.ResetDuplicateReview(ctx, u1, g.Key); err != nil {
		t.Fatal(err)
	}
	mine, _ = c.DuplicateGroupsForUser(ctx, u1, 0, false, "")
	if len(mine) != 1 {
		t.Fatal("reset failed")
	}
	if _, err = c.DuplicateGroupsForUser(ctx, u1, 0, false, "invalid"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestDuplicateCursorDoesNotSkipGroupsWhenReviewedPageShrinks(t *testing.T) {
	db, _, u, _, _ := setupPlaylistTest(t)
	ctx := context.Background()
	c := repository.NewCatalog(db)
	for i := 0; i < 25; i++ {
		_, err := db.ExecContext(ctx, `INSERT INTO tracks(id,title,album_artist,identity_key,created_at,updated_at) SELECT 'dup_'||$1::text||'_'||j,'Song '||$1::text,'Artist','dup_'||$1::text||'_'||j,now(),now() FROM generate_series(1,2)j`, strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(ctx, `INSERT INTO files(id,track_id,path,size_bytes,created_at,updated_at) SELECT 'file_'||id,id,id||'.opus',100,now(),now() FROM tracks WHERE title='Song '||$1::text`, strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := c.DuplicateGroupsForUser(ctx, u, 0, false, "")
	if err != nil || len(first) != 20 {
		t.Fatal("first page", err)
	}
	after := first[len(first)-1].Key
	for _, g := range first {
		if err = c.SaveDuplicateReview(ctx, u, repository.DuplicateReview{GroupKey: g.Key, Fingerprint: g.Fingerprint, Outcome: "distinct"}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := c.DuplicateGroupsForUser(ctx, u, 0, false, after)
	if err != nil || len(next) != 5 {
		t.Fatal("cursor skipped remaining groups", len(next), err)
	}
	for _, g := range next {
		if strings.Compare(g.Key, after) <= 0 {
			t.Fatal("cursor repeated groups")
		}
	}
}
