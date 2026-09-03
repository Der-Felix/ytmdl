package repository_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestAuditRepository_CRUD(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := repository.NewAudit(db)

	// 1. Recover any running runs on startup
	recovered, err := repo.RecoverRunningRuns(ctx)
	if err != nil {
		t.Fatalf("recover running runs: %v", err)
	}
	t.Logf("recovered %d stale running runs", recovered)

	// 2. Create an audit run
	runID := music.NewID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	run := music.AuditRun{
		ID:            runID,
		Mode:          music.AuditModeQuick,
		Status:        music.AuditRunRunning,
		StartedAt:     now,
		Total:         100,
		Scanned:       0,
		FindingsCount: 0,
		CreatedAt:     now,
	}

	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	// 3. Verify Active Run
	active, err := repo.GetActiveRun(ctx)
	if err != nil {
		t.Fatalf("get active run: %v", err)
	}
	if active == nil || active.ID != runID {
		t.Fatalf("expected active run %s, got %+v", runID, active)
	}

	// 4. Update progress
	if err := repo.UpdateRunProgress(ctx, runID, 50, 100, 2); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	// 5. Batch insert findings
	findings := []music.AuditFinding{
		{
			ID:           music.NewID(),
			RunID:        runID,
			FindingCode:  music.FindingFileMissing,
			Severity:     music.SeverityError,
			RelativePath: "Radiohead/1997 - OK Computer/01 - Airbag.opus",
			Evidence: music.FindingEvidence{
				Level:        music.EvidenceExactCatalogID,
				ExpectedPath: "Radiohead/1997 - OK Computer/01 - Airbag.opus",
				Details:      "DB record exists but physical file missing",
			},
			CreatedAt: now,
		},
		{
			ID:           music.NewID(),
			RunID:        runID,
			FindingCode:  music.FindingFileUntracked,
			Severity:     music.SeverityWarning,
			RelativePath: "Radiohead/2025 - Old/01 - Karma.opus",
			Evidence: music.FindingEvidence{
				Level:      music.EvidenceStrongMetadata,
				ActualPath: "Radiohead/2025 - Old/01 - Karma.opus",
				Details:    "Untracked audio file on disk",
			},
			CreatedAt: now,
		},
	}

	if err := repo.InsertFindings(ctx, findings); err != nil {
		t.Fatalf("insert findings: %v", err)
	}

	// 6. Complete run
	if err := repo.CompleteRun(ctx, runID, music.AuditRunCompleted, 100, 100, len(findings), ""); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	// Active run should now be nil
	activeAfter, err := repo.GetActiveRun(ctx)
	if err != nil {
		t.Fatalf("get active run after completion: %v", err)
	}
	if activeAfter != nil {
		t.Fatalf("expected nil active run after completion, got %+v", activeAfter)
	}

	// 7. Query findings with filters & pagination
	listOpts := repository.ListFindingsOptions{
		Limit:  10,
		Offset: 0,
	}
	res, total, err := repo.ListFindings(ctx, runID, listOpts)
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if total != 2 || len(res) != 2 {
		t.Fatalf("expected 2 findings, got total=%d len=%d", total, len(res))
	}

	// Filter by severity 'error'
	listOpts.Severity = string(music.SeverityError)
	resErr, totalErr, err := repo.ListFindings(ctx, runID, listOpts)
	if err != nil {
		t.Fatalf("list error findings: %v", err)
	}
	if totalErr != 1 || len(resErr) != 1 || resErr[0].FindingCode != music.FindingFileMissing {
		t.Fatalf("expected 1 error finding, got total=%d len=%d", totalErr, len(resErr))
	}

	// 8. Delete finding
	if err := repo.DeleteFinding(ctx, findings[0].ID); err != nil {
		t.Fatalf("delete finding: %v", err)
	}
	resAfterDel, totalAfterDel, err := repo.ListFindings(ctx, runID, repository.ListFindingsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list findings after deletion: %v", err)
	}
	if totalAfterDel != 1 || len(resAfterDel) != 1 {
		t.Fatalf("expected 1 finding after deletion, got %d", totalAfterDel)
	}

	// 9. Delete Run (cascade to remaining findings)
	if err := repo.DeleteRun(ctx, runID); err != nil {
		t.Fatalf("delete run: %v", err)
	}
	deletedRun, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get deleted run: %v", err)
	}
	if deletedRun != nil {
		t.Fatalf("expected deleted run to be nil, got %+v", deletedRun)
	}
}

func TestAuditRepository_StartupRecovery(t *testing.T) {
	db := dbtest.Open(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := repository.NewAudit(db)

	runID := music.NewID()
	now := time.Now().UTC()
	run := music.AuditRun{
		ID:        runID,
		Mode:      music.AuditModeDeep,
		Status:    music.AuditRunRunning,
		StartedAt: now,
		Total:     50,
		Scanned:   10,
		CreatedAt: now,
	}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Simulate crash & restart -> recover
	recovered, err := repo.RecoverRunningRuns(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered < 1 {
		t.Fatalf("expected at least 1 recovered run, got %d", recovered)
	}

	reloaded, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if reloaded.Status != music.AuditRunFailed {
		t.Fatalf("expected recovered run to have status 'failed', got %s", reloaded.Status)
	}
	if reloaded.ErrorSummary == "" {
		t.Fatalf("expected error summary on recovered run, got empty string")
	}
}
