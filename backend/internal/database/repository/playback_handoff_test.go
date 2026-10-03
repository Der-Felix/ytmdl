package repository_test

import (
	"context"
	"math"
	"testing"
	"ytdm/backend/internal/database/repository"
)

func TestPlaybackHandoffPreservesQueueAndIsolatesUsers(t *testing.T) {
	db, _, user, other, ids := setupPlaylistTest(t)
	c := repository.NewCatalog(db)
	ctx := context.Background()
	h := repository.PlaybackHandoff{QueueIDs: []string{ids[1], ids[0], ids[1]}, QueueIndex: 2, PositionSeconds: 12.5, RepeatMode: "queue", SourceName: "Laptop"}
	saved, err := c.SavePlaybackHandoff(ctx, user, h)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.PlaybackHandoff(ctx, user)
	if err != nil || got == nil || got.ID != saved.ID || len(got.Queue) != 3 || got.Queue[0].ID != ids[1] || got.Queue[1].ID != ids[0] || got.Queue[2].ID != ids[1] || got.QueueIndex != 2 || got.PositionSeconds != 12.5 {
		t.Fatal("handoff changed order or position", err)
	}
	private, err := c.PlaybackHandoff(ctx, other)
	if err != nil || private != nil {
		t.Fatal("handoff exposed to another account")
	}
	if err = c.DeletePlaybackHandoff(ctx, other, saved.ID); err == nil {
		t.Fatal("cross-user deletion allowed")
	}
	bad := h
	bad.PositionSeconds = math.NaN()
	if _, err = c.SavePlaybackHandoff(ctx, user, bad); err == nil {
		t.Fatal("nonfinite position accepted")
	}
	bad = h
	bad.QueueIDs = []string{ids[0], "missing"}
	bad.QueueIndex = 0
	if _, err = c.SavePlaybackHandoff(ctx, user, bad); err == nil {
		t.Fatal("missing queue member accepted")
	}
	still, _ := c.PlaybackHandoff(ctx, user)
	if still == nil || still.ID != saved.ID {
		t.Fatal("failed save replaced good handoff")
	}
	replacement, err := c.SavePlaybackHandoff(ctx, user, h)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.DeletePlaybackHandoff(ctx, user, saved.ID); err == nil {
		t.Fatal("stale delete removed newer handoff")
	}
	if _, err = db.ExecContext(ctx, `UPDATE playback_handoffs SET expires_at=now()-interval '1 second' WHERE user_id=$1`, user); err != nil {
		t.Fatal(err)
	}
	expired, err := c.PlaybackHandoff(ctx, user)
	if err != nil || expired != nil {
		t.Fatal("expired handoff returned")
	}
	if err = c.DeletePlaybackHandoff(ctx, user, replacement.ID); err != nil {
		t.Fatal(err)
	}
}
