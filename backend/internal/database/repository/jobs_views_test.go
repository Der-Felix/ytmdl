package repository

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
)

func TestJobViewsFilterBeforePagination(t *testing.T) {
	db := openTestDB(t)
	repo := NewJobs(db)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	var failedIDs []string
	// Old failures must remain discoverable even when recent queued jobs fill
	// the unfiltered first page.
	for i := range 2 {
		job := &jobs.Job{Type: jobs.TypeTrack, Status: jobs.StatusFailed, Priority: jobs.PriorityHigh, Options: jobs.DefaultOptions()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE jobs SET created_at = $1 WHERE id = $2", base.Add(time.Duration(i)*time.Second), job.ID); err != nil {
			t.Fatal(err)
		}
		failedIDs = append(failedIDs, job.ID)
	}
	for range 25 {
		job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusQueued, Options: jobs.DefaultOptions()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	for page := range 2 {
		list, total, err := repo.List(ctx, jobs.ListFilter{View: "failed", Limit: 1, Offset: page, Priority: jobs.PriorityHigh})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(list) != 1 || list[0].ID != failedIDs[1-page] {
			t.Fatalf("page %d: total=%d jobs=%v", page, total, list)
		}
	}
	list, total, err := repo.List(ctx, jobs.ListFilter{View: "failed", Priority: jobs.PriorityLow})
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("priority intersection: %d %v %v", total, list, err)
	}
}

func TestJobViewsMatchQueueGroupsAndPausedOverlay(t *testing.T) {
	repo := NewJobs(openTestDB(t))
	ctx := context.Background()
	fixtures := []struct {
		status jobs.Status
		paused bool
	}{
		{jobs.StatusQueued, false}, {jobs.StatusQueued, true},
		{jobs.StatusDownloading, false}, {jobs.StatusRetryWait, true},
		{jobs.StatusCompleted, true}, {jobs.StatusCancelled, true}, {jobs.StatusFailed, true},
	}
	for _, f := range fixtures {
		job := &jobs.Job{Type: jobs.TypeTrack, Status: f.status, Paused: f.paused, Options: jobs.DefaultOptions()}
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	for view, expected := range map[jobs.ListView]int{"all": 7, "active": 2, "queued": 2, "paused": 2, "done": 2, "failed": 1} {
		list, total, err := repo.List(ctx, jobs.ListFilter{View: view, Limit: 20})
		if err != nil || total != expected || len(list) != expected {
			t.Fatalf("%s: total=%d length=%d err=%v", view, total, len(list), err)
		}
	}
	_, _, err := repo.List(ctx, jobs.ListFilter{View: "not-a-view"})
	if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("invalid view: %v", err)
	}
}
