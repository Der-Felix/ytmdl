package repository_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
	"ytdm/backend/internal/database/repository"
)

func TestAudioGroupsFindDifferentNamesAndRejectStaleGenerations(t *testing.T) {
	db, _, user, other, ids := setupPlaylistTest(t)
	ctx := context.Background()
	cat := repository.NewCatalog(db)
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET title=id,duration_ms=45000`); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(23, 27))
	words := make([]uint32, 350)
	for i := range words {
		words[i] = r.Uint32()
	}
	for _, id := range ids[:2] {
		f, err := cat.FingerprintFile(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = cat.SaveAudioFingerprint(ctx, id, f.ID, f.UpdatedAt, 1000, 1, words, "ready"); err != nil {
			t.Fatal(err)
		}
	}
	status, err := cat.AudioAnalysisStatus(ctx)
	if err != nil || status.Ready != 2 {
		t.Fatalf("analysis status %#v %v", status, err)
	}
	groups, err := cat.DuplicateGroupsForUser(ctx, user, 0, false, "")
	if err != nil || len(groups) != 1 || groups[0].Source != "audio" {
		t.Fatalf("audio group absent %#v %v", groups, err)
	}
	g := groups[0]
	review := repository.DuplicateReview{GroupKey: g.Key, Fingerprint: g.Fingerprint, Outcome: "preferred", PreferredTrackID: ids[0]}
	if err = cat.SaveDuplicateReview(ctx, user, review); err != nil {
		t.Fatal(err)
	}
	if err = cat.ValidateDuplicateRemoval(ctx, user, review, ids[1:2]); err != nil {
		t.Fatal(err)
	}
	if err = cat.ValidateDuplicateRemoval(ctx, other, review, ids[1:2]); err == nil {
		t.Fatal("another user decision leaked")
	}
	f, _ := cat.FingerprintFile(ctx, ids[1])
	if _, err = db.ExecContext(ctx, `UPDATE files SET updated_at=$1 WHERE id=$2`, time.Now().Add(time.Second), f.ID); err != nil {
		t.Fatal(err)
	}
	groups, err = cat.DuplicateGroupsForUser(ctx, user, 0, true, "")
	if err != nil || len(groups) != 0 {
		t.Fatal("stale audio group remained")
	}
	if err = cat.ValidateDuplicateRemoval(ctx, user, review, ids[1:2]); err == nil {
		t.Fatal("stale removal allowed")
	}
	status, err = cat.AudioAnalysisStatus(ctx)
	if err != nil || status.Ready != 1 {
		t.Fatal("changed generation not pending")
	}
	if err = cat.ResetAudioAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	tracks, err := cat.TracksByIDs(ctx, ids)
	if err != nil || len(tracks) != len(ids) {
		t.Fatal("reset affected original tracks")
	}
}
