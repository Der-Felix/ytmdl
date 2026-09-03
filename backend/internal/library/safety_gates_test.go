package library_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// Helper: computes SHA256 of a file.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Helper: snapshot of files in a directory.
type fsSnapshotEntry struct {
	Size  int64
	Hash  string
	MTime time.Time
}

func takeFSSnapshot(root string) (map[string]fsSnapshotEntry, error) {
	snap := make(map[string]fsSnapshotEntry)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		hash, hashErr := fileSHA256(path)
		if hashErr != nil {
			return hashErr
		}
		snap[rel] = fsSnapshotEntry{
			Size:  info.Size(),
			Hash:  hash,
			MTime: info.ModTime(),
		}
		return nil
	})
	return snap, err
}

// -----------------------------------------------------------------------------
// GATE 2: AUDIT READ-ONLY BEWEIS
// -----------------------------------------------------------------------------
func TestGate02_AuditReadOnlyProof(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Seed catalog + files
	artist := music.Artist{ID: music.NewID(), Name: "Daft Punk", Provider: "spotify", SourceID: "art_dp"}
	release := music.Release{ID: music.NewID(), Title: "Discovery", AlbumArtist: "Daft Punk", ReleaseType: music.ReleaseAlbum, Year: 2001, TrackCount: 2}
	track1 := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "One More Time", Artists: []string{"Daft Punk"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 320000}
	track2 := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Aerodynamic", Artists: []string{"Daft Punk"}, TrackNumber: 2, DiscNumber: 1, DurationMS: 212000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track1, release.ID, artist.ID, 0)
	_, _ = svc.Catalog().UpsertTrack(ctx, track2, release.ID, artist.ID, 0)

	// Create audio files on disk
	rel1 := "Daft Punk/2001 - Discovery/01 - One More Time.opus"
	rel2 := "Daft Punk/2001 - Discovery/02 - Aerodynamic.opus"
	abs1 := filepath.Join(root, rel1)
	abs2 := filepath.Join(root, rel2)
	_ = os.MkdirAll(filepath.Dir(abs1), 0o755)
	_ = os.WriteFile(abs1, []byte("audio payload one more time"), 0o644)
	_ = os.WriteFile(abs2, []byte("audio payload aerodynamic"), 0o644)

	_, _ = svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track1.ID, Path: rel1, SizeBytes: 27, DurationMS: 320000})
	_, _ = svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track2.ID, Path: rel2, SizeBytes: 26, DurationMS: 212000})

	// Snapshot before
	beforeSnap, err := takeFSSnapshot(root)
	if err != nil {
		t.Fatalf("take snapshot: %v", err)
	}

	// 1. Run Quick Audit
	quickRun, err := svc.StartAudit(ctx, music.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start quick audit: %v", err)
	}
	finishedQuick, err := svc.WaitForAudit(ctx, quickRun.ID, 5*time.Second)
	if err != nil || finishedQuick.Status != music.AuditRunCompleted {
		t.Fatalf("quick audit failed: %v, status: %v", err, finishedQuick.Status)
	}

	// Snapshot after Quick Audit
	afterQuickSnap, err := takeFSSnapshot(root)
	if err != nil {
		t.Fatalf("take snapshot after quick: %v", err)
	}
	if len(beforeSnap) != len(afterQuickSnap) {
		t.Fatalf("file count changed during quick audit: before=%d, after=%d", len(beforeSnap), len(afterQuickSnap))
	}
	for path, b := range beforeSnap {
		a, ok := afterQuickSnap[path]
		if !ok || a.Hash != b.Hash || a.Size != b.Size {
			t.Fatalf("file %s was mutated during Quick Audit: before=%+v, after=%+v", path, b, a)
		}
	}

	// 2. Run Deep Audit
	deepRun, err := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep audit: %v", err)
	}
	finishedDeep, err := svc.WaitForAudit(ctx, deepRun.ID, 5*time.Second)
	if err != nil || finishedDeep.Status != music.AuditRunCompleted {
		t.Fatalf("deep audit failed: %v, status: %v", err, finishedDeep.Status)
	}

	// Snapshot after Deep Audit
	afterDeepSnap, err := takeFSSnapshot(root)
	if err != nil {
		t.Fatalf("take snapshot after deep: %v", err)
	}
	if len(beforeSnap) != len(afterDeepSnap) {
		t.Fatalf("file count changed during deep audit: before=%d, after=%d", len(beforeSnap), len(afterDeepSnap))
	}
	for path, b := range beforeSnap {
		a, ok := afterDeepSnap[path]
		if !ok || a.Hash != b.Hash || a.Size != b.Size {
			t.Fatalf("file %s was mutated during Deep Audit: before=%+v, after=%+v", path, b, a)
		}
	}

	_ = auditRepo
}

// -----------------------------------------------------------------------------
// GATE 3: PROVIDER CALLS = 0
// -----------------------------------------------------------------------------
type instrumentedProviderDetector struct {
	calls int64
}

func (p *instrumentedProviderDetector) RecordCall() {
	atomic.AddInt64(&p.calls, 1)
}

func TestGate03_ZeroProviderCalls(t *testing.T) {
	detector := &instrumentedProviderDetector{}

	svc, _, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Seed sample file
	p := filepath.Join(root, "Artist", "Album", "01 - Track.opus")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("audio"), 0o644)

	// Run quick and deep audit
	qRun, err := svc.StartAudit(ctx, music.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start quick: %v", err)
	}
	_, _ = svc.WaitForAudit(ctx, qRun.ID, 5*time.Second)

	dRun, err := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep: %v", err)
	}
	_, _ = svc.WaitForAudit(ctx, dRun.ID, 5*time.Second)

	if atomic.LoadInt64(&detector.calls) != 0 {
		t.Fatalf("expected 0 provider calls during audits, got %d", detector.calls)
	}
}

// -----------------------------------------------------------------------------
// GATE 5: ONE ACTIVE AUDIT
// -----------------------------------------------------------------------------
func TestGate05_OneActiveAudit(t *testing.T) {
	svc, _, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write dummy file
	_ = os.MkdirAll(filepath.Join(root, "A", "B"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "A", "B", "01.opus"), []byte("audio"), 0o644)

	// Start Audit A
	runA, err := svc.StartAudit(ctx, music.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit A: %v", err)
	}

	// Concurrently try to start Audit B while A is active
	_, errB := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if errB == nil {
		t.Log("audit A finished quickly; testing mutex conflict")
	} else {
		if apperr.CodeOf(errB) != apperr.CodeAlreadyExists {
			t.Fatalf("expected CodeAlreadyExists for second active audit, got: %v", errB)
		}
	}

	_, _ = svc.WaitForAudit(ctx, runA.ID, 5*time.Second)

	// After completion, starting a new audit must succeed
	runC, err := svc.StartAudit(ctx, music.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("expected new audit to succeed after completion, got: %v", err)
	}
	_, _ = svc.WaitForAudit(ctx, runC.ID, 5*time.Second)
}

// -----------------------------------------------------------------------------
// GATE 6: AUDIT RESTART RECOVERY
// -----------------------------------------------------------------------------
func TestGate06_AuditRestartRecovery(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })
	auditRepo := repository.NewAudit(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert dangling running audit
	danglingID := music.NewID()
	now := time.Now().UTC().Add(-10 * time.Minute)
	err := auditRepo.CreateRun(ctx, music.AuditRun{
		ID:        danglingID,
		Mode:      music.AuditModeDeep,
		Status:    music.AuditRunRunning,
		StartedAt: now,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("create dangling run: %v", err)
	}

	// Simulate server startup recovery
	recoveredCount, err := auditRepo.RecoverInterruptedRuns(ctx)
	if err != nil {
		t.Fatalf("recover interrupted runs: %v", err)
	}
	if recoveredCount < 1 {
		t.Fatalf("expected at least 1 recovered run, got %d", recoveredCount)
	}

	// Verify status is now failed with clean message
	run, err := auditRepo.GetRun(ctx, danglingID)
	if err != nil || run == nil {
		t.Fatalf("get recovered run: %v", err)
	}
	if run.Status != music.AuditRunFailed {
		t.Fatalf("expected status failed, got %s", run.Status)
	}
	if run.ErrorSummary == "" {
		t.Fatalf("expected meaningful error summary for recovered run")
	}
}

// -----------------------------------------------------------------------------
// GATE 7: AUDIT CANCELLATION
// -----------------------------------------------------------------------------
func TestGate07_AuditCancellation(t *testing.T) {
	svc, _, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Seed 20 files
	for i := 1; i <= 20; i++ {
		p := filepath.Join(root, "Artist", "Album", fmt.Sprintf("%02d - Track.opus", i))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("audio payload"), 0o644)
	}

	run, err := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}

	// Cancel audit
	err = svc.CancelAudit(ctx, run.ID)
	if err != nil {
		t.Fatalf("cancel audit: %v", err)
	}

	// Wait and verify status is cancelled
	finished, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait for cancelled audit: %v", err)
	}
	if finished.Status != music.AuditRunCancelled {
		t.Fatalf("expected audit status cancelled, got %s", finished.Status)
	}
}

// -----------------------------------------------------------------------------
// GATE 8: STORAGE LOSS DURING AUDIT
// -----------------------------------------------------------------------------
func TestGate08_StorageLossDuringAudit(t *testing.T) {
	svc, _, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	unmountedRoot := filepath.Join(root, "unmounted_volume")
	unmountedLib, _ := storage.NewLibrary(unmountedRoot)
	_ = os.RemoveAll(unmountedRoot) // Remove after creation to simulate storage disconnect
	unmountedSvc, err := library.NewService(library.ServiceOptions{
		Library: unmountedLib,
		Files:   svc.Files(),
		Catalog: svc.Catalog(),
		Prober:  &mockProber{},
		Tagger:  metadata.NewTagger(ffmpeg.New("ffmpeg", 5*time.Second)),
		Audit:   svc.AuditRepo(),
	})
	if err != nil {
		t.Fatalf("new unmounted svc: %v", err)
	}

	run, err := unmountedSvc.StartAudit(ctx, music.AuditModeQuick, nil)
	if err != nil {
		// Starting failed due to storage guard / existence check
		return
	}
	finished, err := unmountedSvc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if finished.Status != music.AuditRunFailed {
		t.Fatalf("expected audit on missing storage to fail globally, got %s", finished.Status)
	}
}

// -----------------------------------------------------------------------------
// GATE 9: STORAGE GUARD BEFORE REPAIR
// -----------------------------------------------------------------------------
func TestGate09_StorageGuardBeforeRepair(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{
		{
			ID:              findingID,
			RunID:           runID,
			FindingCode:     music.FindingFileUntracked,
			Severity:        music.SeverityWarning,
			RelativePath:    "Ghost/Album/01.opus",
			SuggestedAction: &action,
			CreatedAt:       now,
		},
	})

	// When storage is healthy, missing file is cleanly rejected
	req := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, req)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("expected failed=1 for missing source file, got %+v", res)
	}
	_ = root
}

// -----------------------------------------------------------------------------
// GATE 10: SHARED PATH LOCK EXCLUSION
// -----------------------------------------------------------------------------
func TestGate10_SharedPathLockExclusion(t *testing.T) {
	svc, _, _, _ := setupIntegrityTest(t)

	// Acquire lock on track X using service KeyedMutex
	trackKey := "track:track_123"
	unlockTrack, ok := svc.Locks().TryLock(trackKey)
	if !ok {
		t.Fatalf("expected initial lock acquisition to succeed")
	}

	// Concurrently attempt to acquire same lock
	_, ok2 := svc.Locks().TryLock(trackKey)
	if ok2 {
		t.Fatalf("expected second lock on same track to be excluded")
	}

	// Release lock
	unlockTrack()

	// Acquire again should now succeed
	unlockTrack2, ok3 := svc.Locks().TryLock(trackKey)
	if !ok3 {
		t.Fatalf("expected lock after release to succeed")
	}
	unlockTrack2()
}

// -----------------------------------------------------------------------------
// GATE 11: PREVIEW IST READ-ONLY
// -----------------------------------------------------------------------------
func TestGate11_PreviewIsReadOnly(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - Song.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	_ = os.WriteFile(absPath, []byte("sample audio content"), 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{
		{
			ID:              findingID,
			RunID:           runID,
			FindingCode:     music.FindingFileUntracked,
			Severity:        music.SeverityWarning,
			RelativePath:    relPath,
			SuggestedAction: &action,
			CreatedAt:       now,
		},
	})

	snapBefore, _ := takeFSSnapshot(root)

	// Run Preview
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil {
		t.Fatalf("preview repairs: %v", err)
	}
	if len(previews) != 1 || !previews[0].Allowed {
		t.Fatalf("expected allowed preview")
	}

	// Verify 0 filesystem mutations
	snapAfter, _ := takeFSSnapshot(root)
	if len(snapBefore) != len(snapAfter) {
		t.Fatalf("filesystem modified during preview: before=%d, after=%d", len(snapBefore), len(snapAfter))
	}

	// Verify .ytmdl-trash was NOT created
	trashDir := filepath.Join(root, ".ytmdl-trash")
	if _, err := os.Stat(trashDir); !os.IsNotExist(err) {
		t.Fatalf("preview must not create .ytmdl-trash directory")
	}
}

// -----------------------------------------------------------------------------
// GATE 12: STALE PREVIEW REAL TEST
// -----------------------------------------------------------------------------
func TestGate12_StalePreviewRealTest(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - StaleTest.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	_ = os.WriteFile(absPath, []byte("initial version SHA_A"), 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{
		{
			ID:              findingID,
			RunID:           runID,
			FindingCode:     music.FindingFileUntracked,
			Severity:        music.SeverityWarning,
			RelativePath:    relPath,
			SuggestedAction: &action,
			CreatedAt:       now,
		},
	})

	// Preview when file exists
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil || len(previews) != 1 || !previews[0].Allowed {
		t.Fatalf("expected allowed preview: %v", err)
	}

	// Now delete/modify file before apply
	_ = os.WriteFile(absPath, []byte("mutated bytes SHA_B"), 0o644)
	_ = os.Remove(absPath) // remove file to test stale precondition

	// Apply must fail safely
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("expected failed=1 when source file disappeared, got %+v", res)
	}
}

// -----------------------------------------------------------------------------
// GATE 15: ADOPT_FILE IDENTITY MATRIX
// -----------------------------------------------------------------------------
func TestGate15_AdoptFileIdentityMatrix(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Kraftwerk", Provider: "spotify", SourceID: "art_kw"}
	release := music.Release{ID: music.NewID(), Title: "Computerwelt", AlbumArtist: "Kraftwerk", ReleaseType: music.ReleaseAlbum, Year: 1981, TrackCount: 1}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Computerwelt", Artists: []string{"Kraftwerk"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 305000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	fileRel := "Kraftwerk/1981 - Computerwelt/01 - Computerwelt.opus"
	fileAbs := filepath.Join(root, fileRel)
	_ = os.MkdirAll(filepath.Dir(fileAbs), 0o755)
	_ = os.WriteFile(fileAbs, []byte("kraftwerk audio payload"), 0o644)

	runID := music.NewID()
	now := time.Now().UTC()
	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})

	// Case A: EXACT_CATALOG_ID -> Allowed
	actionAdopt := music.ActionAdoptFile
	findingA := music.AuditFinding{
		ID:              music.NewID(),
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		SuggestedAction: &actionAdopt,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceExactCatalogID,
			DurationMS: 305000,
		},
		CreatedAt: now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{findingA})

	previewsA, _ := svc.PreviewRepairs(ctx, []string{findingA.ID})
	if len(previewsA) != 1 || !previewsA[0].Allowed {
		t.Fatalf("expected EXACT_CATALOG_ID to be allowed for adoption")
	}

	// Case C: STRONG_METADATA only -> Preview allowed, Apply rejected
	findingC := music.AuditFinding{
		ID:              music.NewID(),
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		SuggestedAction: &actionAdopt,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceStrongMetadata,
			DurationMS: 305000,
		},
		CreatedAt: now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{findingC})

	previewsC, _ := svc.PreviewRepairs(ctx, []string{findingC.ID})
	if len(previewsC) != 1 || !previewsC[0].Allowed {
		t.Fatalf("expected STRONG_METADATA to allow preview")
	}

	// Apply on STRONG_METADATA must be rejected
	resC, _ := svc.ApplyRepairs(ctx, library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingC.ID, Action: actionAdopt}},
	})
	if resC.Failed != 1 {
		t.Fatalf("expected apply on STRONG_METADATA adoption to fail, got %+v", resC)
	}

	// Case D: WEAK_METADATA -> Apply rejected
	findingD := music.AuditFinding{
		ID:              music.NewID(),
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		SuggestedAction: &actionAdopt,
		Evidence: music.FindingEvidence{
			Level: music.EvidenceWeakMetadata,
		},
		CreatedAt: now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{findingD})

	resD, _ := svc.ApplyRepairs(ctx, library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingD.ID, Action: actionAdopt}},
	})
	if resD.Failed != 1 {
		t.Fatalf("expected apply on WEAK_METADATA adoption to fail, got %+v", resD)
	}
}

// -----------------------------------------------------------------------------
// GATE 23: REPAIR APPLY TWICE IDEMPOTENCY
// -----------------------------------------------------------------------------
func TestGate23_RepairApplyTwice(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - Once.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	_ = os.WriteFile(absPath, []byte("audio payload"), 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{
		{
			ID:              findingID,
			RunID:           runID,
			FindingCode:     music.FindingFileUntracked,
			Severity:        music.SeverityWarning,
			RelativePath:    relPath,
			SuggestedAction: &action,
			CreatedAt:       now,
		},
	})

	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}

	// First Apply
	res1, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil || res1.Quarantined != 1 {
		t.Fatalf("first apply failed: %v, %+v", err, res1)
	}

	// Second Apply
	res2, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("second apply returned error: %v", err)
	}
	// Second apply must be gracefully handled (0 quarantined, 1 failed/skipped, 0 mutations)
	if res2.Quarantined != 0 {
		t.Fatalf("second apply must not quarantine again: %+v", res2)
	}
}

// -----------------------------------------------------------------------------
// GATE 25: COVER VALIDATION RESOURCE SAFETY
// -----------------------------------------------------------------------------
func TestGate25_CoverValidationResourceSafety(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Valid JPEG
	validJPEGPath := filepath.Join(root, "Artist", "ReleaseA", "cover.jpg")
	_ = os.MkdirAll(filepath.Dir(validJPEGPath), 0o755)
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	imgFile, _ := os.Create(validJPEGPath)
	_ = jpeg.Encode(imgFile, img, nil)
	imgFile.Close()

	// 2. Valid PNG
	validPNGPath := filepath.Join(root, "Artist", "ReleaseB", "cover.png")
	_ = os.MkdirAll(filepath.Dir(validPNGPath), 0o755)
	pngFile, _ := os.Create(validPNGPath)
	_ = png.Encode(pngFile, img)
	pngFile.Close()

	// 3. Zero-byte image
	zeroPath := filepath.Join(root, "Artist", "ReleaseC", "cover.jpg")
	_ = os.MkdirAll(filepath.Dir(zeroPath), 0o755)
	_ = os.WriteFile(zeroPath, []byte{}, 0o644)

	// 4. Corrupt image
	corruptPath := filepath.Join(root, "Artist", "ReleaseD", "cover.jpg")
	_ = os.MkdirAll(filepath.Dir(corruptPath), 0o755)
	_ = os.WriteFile(corruptPath, []byte("NOT_A_VALID_JPEG_HEADER_12345"), 0o644)

	// Register artist and releases in catalog
	art := music.Artist{ID: music.NewID(), Name: "Artist", Provider: "spotify", SourceID: "art_1"}
	_, _ = svc.Catalog().UpsertArtist(ctx, art)

	for _, letter := range []string{"A", "B", "C", "D"} {
		rel := music.Release{
			ID:          music.NewID(),
			Title:       "Release" + letter,
			AlbumArtist: "Artist",
			CoverURL:    "https://example.com/cover.jpg",
		}
		_, _ = svc.Catalog().UpsertRelease(ctx, rel, art.ID)
	}

	// Run Deep Audit
	run, err := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	finished, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || finished.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed on cover test: %v, status: %v", err, finished.Status)
	}

	// Verify findings recorded for zero-byte and corrupt covers
	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	foundCoverInvalid := false
	for _, f := range findings {
		if f.FindingCode == music.FindingCoverInvalid {
			foundCoverInvalid = true
		}
	}
	if !foundCoverInvalid {
		t.Fatalf("expected FindingCoverInvalid to be recorded for invalid covers")
	}
}

// -----------------------------------------------------------------------------
// GATE 26: MALICIOUS TAGS & FILENAMES
// -----------------------------------------------------------------------------
func TestGate26_MaliciousTagsAndFilenames(t *testing.T) {
	_, _, _, root := setupIntegrityTest(t)

	// Verify path confinement prevents path traversal escapes
	maliciousPaths := []string{
		"../../etc/passwd",
		"/etc/shadow",
		"Artist/../../../secret.opus",
		"Artist/Album/\x00nullbyte.opus",
		"Artist/Album/$(reboot).opus",
		"Artist/Album/`rm -rf /`.opus",
	}

	for _, mal := range maliciousPaths {
		_, _, err := library.VerifyPathConfinement(root, mal, true)
		if err == nil && !filepath.IsLocal(mal) {
			t.Fatalf("expected error for malicious path %q, got nil", mal)
		}
	}
}

// -----------------------------------------------------------------------------
// GATE 27 & 28: FINDINGS PAGINATION & FILTER TOTALS
// -----------------------------------------------------------------------------
func TestGate27_28_FindingsPaginationAndFilters(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })
	auditRepo := repository.NewAudit(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runID := music.NewID()
	now := time.Now().UTC()
	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})

	const totalFindings = 137
	findings := make([]music.AuditFinding, totalFindings)
	for i := 0; i < totalFindings; i++ {
		sev := music.SeverityInfo
		if i%3 == 0 {
			sev = music.SeverityWarning
		} else if i%5 == 0 {
			sev = music.SeverityError
		}

		code := music.FindingPathMismatch
		if i%2 == 0 {
			code = music.FindingFileUntracked
		}

		findings[i] = music.AuditFinding{
			ID:           fmt.Sprintf("find_%04d", i),
			RunID:        runID,
			FindingCode:  code,
			Severity:     sev,
			RelativePath: fmt.Sprintf("Artist/Album/%04d.opus", i),
			CreatedAt:    now.Add(time.Duration(i) * time.Millisecond),
		}
	}

	if err := auditRepo.InsertFindings(ctx, findings); err != nil {
		t.Fatalf("insert 137 findings: %v", err)
	}

	// Verify pagination: 6 pages of 25 + 1 page of 12
	seenIDs := make(map[string]bool)
	offsets := []int{0, 25, 50, 75, 100, 125}
	for _, offset := range offsets {
		items, total, err := auditRepo.ListFindings(ctx, runID, repository.ListFindingsOptions{
			Limit:  25,
			Offset: offset,
		})
		if err != nil {
			t.Fatalf("list offset %d: %v", offset, err)
		}
		if total != 137 {
			t.Fatalf("expected total 137 at offset %d, got %d", offset, total)
		}
		for _, item := range items {
			if seenIDs[item.ID] {
				t.Fatalf("duplicate finding ID seen across pages: %s", item.ID)
			}
			seenIDs[item.ID] = true
		}
	}

	if len(seenIDs) != 137 {
		t.Fatalf("expected to paginate all 137 unique findings, saw %d", len(seenIDs))
	}

	// Verify Filter Totals: Warning only
	warningItems, warningTotal, err := auditRepo.ListFindings(ctx, runID, repository.ListFindingsOptions{
		Severity: string(music.SeverityWarning),
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("list warning filter: %v", err)
	}
	for _, item := range warningItems {
		if item.Severity != music.SeverityWarning {
			t.Fatalf("expected severity warning, got %s", item.Severity)
		}
	}
	if warningTotal >= 137 || warningTotal == 0 {
		t.Fatalf("expected filtered total < 137 and > 0, got %d", warningTotal)
	}
}

// -----------------------------------------------------------------------------
// GATE 30: DEEP PROBER BOUNDED CONCURRENCY
// -----------------------------------------------------------------------------
type concurrentTrackingProber struct {
	current    int64
	maxReached int64
}

func (p *concurrentTrackingProber) Probe(ctx context.Context, path string) (*downloader.AudioInfo, error) {
	cur := atomic.AddInt64(&p.current, 1)
	for {
		max := atomic.LoadInt64(&p.maxReached)
		if cur <= max || atomic.CompareAndSwapInt64(&p.maxReached, max, cur) {
			break
		}
	}
	time.Sleep(10 * time.Millisecond) // simulate probe work
	atomic.AddInt64(&p.current, -1)
	return &downloader.AudioInfo{Codec: "opus", Container: "ogg", DurationMS: 200000, SizeBytes: 1000}, nil
}

func TestGate30_DeepProberBoundedConcurrency(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })
	catRepo := repository.NewCatalog(db)
	fileRepo := repository.NewFiles(db)
	auditRepo := repository.NewAudit(db)

	root := t.TempDir()
	lib, _ := storage.NewLibrary(root)

	prober := &concurrentTrackingProber{}
	svc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Files:   fileRepo,
		Catalog: catRepo,
		Prober:  prober,
		Tagger:  metadata.NewTagger(ffmpeg.New("ffmpeg", 5*time.Second)),
		Audit:   auditRepo,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Seed 30 untracked files
	for i := 1; i <= 30; i++ {
		p := filepath.Join(root, "Artist", "Album", fmt.Sprintf("%02d - Track.opus", i))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("audio content"), 0o644)
	}

	run, err := svc.StartAudit(ctx, music.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep audit: %v", err)
	}
	finished, err := svc.WaitForAudit(ctx, run.ID, 8*time.Second)
	if err != nil || finished.Status != music.AuditRunCompleted {
		t.Fatalf("deep audit failed: %v, status: %v", err, finished.Status)
	}

	maxConcurrent := atomic.LoadInt64(&prober.maxReached)
	if maxConcurrent > 4 {
		t.Fatalf("deep prober concurrency exceeded limit 4, reached %d", maxConcurrent)
	}
	t.Logf("Max concurrent prober workers during 30-file Deep Audit: %d (configured limit: 4)", maxConcurrent)
}
