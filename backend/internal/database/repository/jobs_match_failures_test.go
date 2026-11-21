package repository

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// finishedReleaseJob stores a release job whose items ended as given: an
// empty code is a completed item, anything else a failed one with that code.
func finishedReleaseJob(t *testing.T, repo *Jobs, target string, status jobs.Status, codes map[string]apperr.Code) {
	t.Helper()
	finishedReleaseJobWith(t, repo, target, status, codes, jobs.DefaultOptions())
}

// finishedReleaseJobWith is finishedReleaseJob with the job options given.
func finishedReleaseJobWith(t *testing.T, repo *Jobs, target string, status jobs.Status, codes map[string]apperr.Code, options jobs.Options) {
	t.Helper()
	ctx := context.Background()

	job := &jobs.Job{
		Type: jobs.TypeRelease, Status: jobs.StatusQueued, Label: target,
		MetadataProvider: "deezer", TargetID: target, Options: options,
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	items := make([]jobs.Item, 0, len(codes))
	for sourceID := range codes {
		items = append(items, jobs.Item{
			Position: len(items),
			Track:    music.Track{Title: sourceID, SourceProvider: "deezer", SourceID: sourceID},
		})
	}
	if err := repo.AddItems(ctx, job.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}
	stored, err := repo.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	for _, item := range stored {
		update := jobs.ItemUpdate{Status: jobs.ItemCompleted}
		if code := codes[item.Track.SourceID]; code != "" {
			update = jobs.ItemUpdate{Status: jobs.ItemFailed, ErrorCode: string(code), ErrorMessage: "failed"}
		}
		if err := repo.UpdateItem(ctx, item.ID, update); err != nil {
			t.Fatalf("update item: %v", err)
		}
	}
	if status != jobs.StatusQueued {
		if err := repo.SetStatus(ctx, job.ID, status, "", ""); err != nil {
			t.Fatalf("set status: %v", err)
		}
	}
	// The jobs of one target are told apart by their creation time.
	time.Sleep(5 * time.Millisecond)
}

func TestReleaseMatchFailures(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	got, err := repo.ReleaseMatchFailures(ctx, "never-downloaded")
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 0 || len(got.TrackSourceIDs) != 0 {
		t.Fatalf("a release without a job has no history: %+v", got)
	}

	const target = "unmatched-release"
	finishedReleaseJob(t, repo, target, jobs.StatusFailed, map[string]apperr.Code{
		"track-ok": "", "track-lost": apperr.CodeMatchFailed,
	})
	before := time.Now()
	finishedReleaseJob(t, repo, target, jobs.StatusFailed, map[string]apperr.Code{
		"track-lost": apperr.CodeMatchFailed,
	})
	// A job that is still running does not replace the last finished one.
	finishedReleaseJob(t, repo, target, jobs.StatusQueued, map[string]apperr.Code{"track-lost": ""})

	got, err = repo.ReleaseMatchFailures(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 2 {
		t.Fatalf("expected two unmatched attempts, got %d", got.Attempts)
	}
	if len(got.TrackSourceIDs) != 1 || got.TrackSourceIDs[0] != "track-lost" {
		t.Fatalf("expected the unmatched track only, got %v", got.TrackSourceIDs)
	}
	if got.LastAt.Before(before.Add(-time.Minute)) || got.LastAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("the last attempt is not dated to the last finished job: %s", got.LastAt)
	}

	// A job that lost a track for another reason says nothing about the match.
	const mixed = "mixed-release"
	finishedReleaseJob(t, repo, mixed, jobs.StatusFailed, map[string]apperr.Code{
		"a": apperr.CodeMatchFailed, "b": apperr.CodeInternal,
	})
	got, err = repo.ReleaseMatchFailures(ctx, mixed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 0 {
		t.Fatalf("a mixed failure must not report a match history: %+v", got)
	}

	// A later job that fetched everything ends the history.
	finishedReleaseJob(t, repo, target, jobs.StatusCompleted, map[string]apperr.Code{"track-lost": ""})
	got, err = repo.ReleaseMatchFailures(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 0 {
		t.Fatalf("a completed job ends the history: %+v", got)
	}
}

func TestReleaseMatchFailuresIgnoreCopiesOnlyJobs(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	const target = "unmatched-then-backfilled"
	finishedReleaseJob(t, repo, target, jobs.StatusFailed, map[string]apperr.Code{
		"track-lost": apperr.CodeMatchFailed,
	})
	// The backfill of incomplete releases finished a copies-only job for the
	// release later. It never looked for media, so the wait stays.
	copies := jobs.DefaultOptions()
	copies.CopiesOnly = true
	finishedReleaseJobWith(t, repo, target, jobs.StatusCompleted, map[string]apperr.Code{"track-copied": ""}, copies)

	got, err := repo.ReleaseMatchFailures(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 1 || len(got.TrackSourceIDs) != 1 || got.TrackSourceIDs[0] != "track-lost" {
		t.Fatalf("a copies-only job ended the match history: %+v", got)
	}
}
