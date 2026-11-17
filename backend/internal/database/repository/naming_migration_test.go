package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

func TestFilesUpdatePathKeepsTheRow(t *testing.T) {
	ctx := context.Background()
	files := NewFiles(openTestDB(t))

	moved, err := files.Upsert(ctx, music.File{Path: "AC-DC/1980 - Album/01 - Who_.opus", Codec: "opus", SizeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	other, err := files.Upsert(ctx, music.File{Path: "Band/2000 - Album/01 - Song.opus", Codec: "opus", SizeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}

	to := "AC／DC/1980 - Album/01 - Who？.opus"
	if err := files.UpdatePath(ctx, moved.ID, moved.Path, to); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}
	found, err := files.FindByPath(ctx, to)
	if err != nil || found == nil || found.ID != moved.ID {
		t.Fatalf("row at the new path = %+v (%v), want ID %s", found, err, moved.ID)
	}
	if gone, _ := files.FindByPath(ctx, moved.Path); gone != nil {
		t.Fatal("the old path is still catalogued")
	}

	// A retry after a lost response is harmless.
	if err := files.UpdatePath(ctx, moved.ID, moved.Path, to); err != nil {
		t.Fatalf("retry: %v", err)
	}
	// A path another row holds is refused.
	if err := files.UpdatePath(ctx, other.ID, other.Path, to); apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("taken path: err = %v", err)
	}
	// A row that no longer carries the expected path is refused.
	if err := files.UpdatePath(ctx, other.ID, "Somewhere/else.opus", "Band/x.opus"); apperr.CodeOf(err) != apperr.CodeConflict {
		t.Fatalf("stale path: err = %v", err)
	}
}

func TestRetargetTrashOnlyChangesAReadyEntry(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	cat := NewCatalog(db)

	payload := `{"track":{"id":"t1"},"files":[{"id":"f1","path":"AC-DC/1980 - Album/01 - Who_.opus","size_bytes":12345678901}]}`
	moves := `[{"original":"AC-DC/1980 - Album/01 - Who_.opus","stored":".ytmdl-trash/songs/x/000.opus","size":5,"mtime_ns":1}]`
	if _, err := db.ExecContext(ctx, `INSERT INTO library_trash(id,track_id,title,state,payload,moves) VALUES('e1','t1','Who?','ready',$1,$2)`, payload, moves); err != nil {
		t.Fatal(err)
	}

	newPayload := json.RawMessage(strings.ReplaceAll(payload, "AC-DC/1980 - Album/01 - Who_.opus", "AC／DC/1980 - Album/01 - Who？.opus"))
	newMoves := []TrashMove{{Original: "AC／DC/1980 - Album/01 - Who？.opus", Stored: ".ytmdl-trash/songs/x/000.opus", Size: 5, MTimeNS: 1}}
	if err := cat.RetargetTrash(ctx, "e1", newMoves, newPayload); err != nil {
		t.Fatalf("RetargetTrash: %v", err)
	}
	entry, err := cat.GetTrash(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Moves) != 1 || entry.Moves[0].Original != newMoves[0].Original {
		t.Fatalf("moves = %+v", entry.Moves)
	}
	if !strings.Contains(string(entry.Payload), "AC／DC") || !strings.Contains(string(entry.Payload), "12345678901") {
		t.Fatalf("payload = %s", entry.Payload)
	}

	if err := cat.SetTrashState(ctx, "e1", "ready", "restoring"); err != nil {
		t.Fatal(err)
	}
	if err := cat.RetargetTrash(ctx, "e1", nil, newPayload); apperr.CodeOf(err) != apperr.CodeConflict {
		t.Fatalf("restoring entry: err = %v", err)
	}
}
