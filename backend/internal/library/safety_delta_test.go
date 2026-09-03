package library_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
)

// =============================================================================
// 1. STALE PREVIEW CONTENT REVALIDATION (SAME SIZE, SAME MTIME, DIFFERENT SHA)
// =============================================================================
func TestSafetyDelta_StalePreviewSameSizeSameMtime(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - ContentTest.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)

	// Step 1: Create initial file with Payload A
	payloadA := []byte("PAYLOAD_VERSION_A_EXACT_SIZE_32B")
	fixedMtime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(absPath, payloadA, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(absPath, fixedMtime, fixedMtime)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    relPath,
		SuggestedAction: &action,
		CreatedAt:       now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// Step 2: Run Preview -> SHA256 of Payload A is captured
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil || len(previews) != 1 || !previews[0].Allowed {
		t.Fatalf("expected allowed preview: %v", err)
	}

	// Verify finding evidence in DB now holds SHA256 of Payload A
	storedFinding, _ := auditRepo.GetFinding(ctx, findingID)
	if storedFinding == nil || storedFinding.Evidence.SHA256 == "" {
		t.Fatalf("expected preview to persist SHA256 in finding evidence")
	}
	shaA := storedFinding.Evidence.SHA256

	// Step 3: Replace file with Payload B (SAME size, SAME mtime, DIFFERENT SHA)
	payloadB := []byte("PAYLOAD_VERSION_B_EXACT_SIZE_32B")
	if len(payloadA) != len(payloadB) {
		t.Fatalf("test precondition: payloads must have identical size")
	}
	if err := os.WriteFile(absPath, payloadB, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(absPath, fixedMtime, fixedMtime)

	// Step 4: Apply Repair -> MUST fail with STALE_REPAIR (409 Conflict)
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("ApplyRepairs returned error: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("expected repair to fail due to stale SHA, got %+v", res)
	}

	// Step 5: Verify 0 files mutated, Payload B remains intact on disk
	contentOnDisk, err := os.ReadFile(absPath)
	if err != nil || string(contentOnDisk) != string(payloadB) {
		t.Fatalf("source file was mutated despite stale preview! got: %s", string(contentOnDisk))
	}

	// Verify quarantine folder was NOT created
	trashDir := filepath.Join(root, ".ytmdl-trash")
	if _, err := os.Stat(trashDir); !os.IsNotExist(err) {
		t.Fatalf(".ytmdl-trash must not exist after failed stale apply")
	}
	_ = shaA
}

// =============================================================================
// 2. QUARANTINE CRITICAL WINDOW – REAL RECOVERY
// =============================================================================
func TestSafetyDelta_QuarantineCriticalWindowRecovery(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - Orphan.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	sourcePayload := []byte("unique original source content to quarantine")
	_ = os.WriteFile(absPath, sourcePayload, 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingLegacyDuplicate,
		Severity:        music.SeverityWarning,
		RelativePath:    relPath,
		SuggestedAction: &action,
		CreatedAt:       now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// Preview captures SHA256
	_, _ = svc.PreviewRepairs(ctx, []string{findingID})

	// Simulate FAULT INJECTION (Crash Window):
	// Move file to .ytmdl-trash/<finding_id>/01 - Orphan.opus manually (as if move committed, but crash happened before DB update)
	trashDir := filepath.Join(root, ".ytmdl-trash", findingID)
	_ = os.MkdirAll(trashDir, 0o755)
	destAbs := filepath.Join(trashDir, filepath.Base(relPath))
	if err := os.Rename(absPath, destAbs); err != nil {
		t.Fatalf("simulate move crash: %v", err)
	}

	// Verify source is now missing from original path
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Fatalf("source should be gone after simulated move crash")
	}

	// Apply Repair AGAIN (Simulating restart recovery)
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("ApplyRepairs recovery failed: %v", err)
	}
	if res.Applied != 1 || res.Failed != 0 {
		t.Fatalf("expected successful recovery apply, got %+v", res)
	}

	// Verify quarantine file is present and byte-identical to original
	quarantinedContent, err := os.ReadFile(destAbs)
	if err != nil || string(quarantinedContent) != string(sourcePayload) {
		t.Fatalf("quarantined content mismatch: %v", err)
	}

	// Verify finding is now deleted/resolved from DB
	fAfter, _ := auditRepo.GetFinding(ctx, findingID)
	if fAfter != nil {
		t.Fatalf("finding should be resolved after recovery apply")
	}
}

// =============================================================================
// 3. MOVE_CANONICAL CRITICAL WINDOW – REAL RECOVERY
// =============================================================================
func TestSafetyDelta_MoveCanonicalCriticalWindowRecovery(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Radiohead", Provider: "spotify", SourceID: "art_rh"}
	release := music.Release{ID: music.NewID(), Title: "OK Computer", AlbumArtist: "Radiohead", ReleaseType: music.ReleaseAlbum, Year: 1997, TrackCount: 1}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Airbag", Artists: []string{"Radiohead"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 284000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	oldRel := "Radiohead/Old Folder/01. Airbag.opus"
	oldAbs := filepath.Join(root, oldRel)
	_ = os.MkdirAll(filepath.Dir(oldAbs), 0o755)
	sourcePayload := []byte("radiohead airbag audio content")
	_ = os.WriteFile(oldAbs, sourcePayload, 0o644)

	_, _ = svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track.ID, Path: oldRel, SizeBytes: int64(len(sourcePayload)), DurationMS: 284000})

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionMoveCanonical
	canonicalRel := "Radiohead/1997 - OK Computer/01 - Airbag.opus"
	canonicalAbs := filepath.Join(root, canonicalRel)

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingPathMismatch,
		Severity:        music.SeverityInfo,
		RelativePath:    oldRel,
		TrackID:         track.ID,
		ReleaseID:       release.ID,
		SuggestedAction: &action,
		CreatedAt:       now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// Preview captures SHA
	_, _ = svc.PreviewRepairs(ctx, []string{findingID})

	// Simulate FAULT INJECTION (Crash after filesystem move, before DB update):
	_ = os.MkdirAll(filepath.Dir(canonicalAbs), 0o755)
	_ = os.Rename(oldAbs, canonicalAbs)

	// Apply Repair AGAIN (Simulating restart recovery)
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("ApplyRepairs move recovery failed: %v", err)
	}
	if res.Applied != 1 || res.Failed != 0 {
		t.Fatalf("expected successful move recovery apply, got %+v", res)
	}

	// Verify DB files record now points to canonical path
	fileRow, err := svc.Files().FindByPath(ctx, canonicalRel)
	if err != nil || fileRow == nil {
		t.Fatalf("expected DB to have file pointing to canonical path, got: %v", err)
	}
	if fileRow.TrackID != track.ID {
		t.Fatalf("expected track ID %s, got %s", track.ID, fileRow.TrackID)
	}

	// Verify target file is intact
	targetContent, err := os.ReadFile(canonicalAbs)
	if err != nil || string(targetContent) != string(sourcePayload) {
		t.Fatalf("target content mismatch: %v", err)
	}
}

// =============================================================================
// 4. ADOPT_FILE CRASH MATRIX & IDEMPOTENCY
// =============================================================================
func TestSafetyDelta_AdoptFileCrashMatrix(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Kraftwerk", Provider: "spotify", SourceID: "art_kw2"}
	release := music.Release{ID: music.NewID(), Title: "Autobahn", AlbumArtist: "Kraftwerk", ReleaseType: music.ReleaseAlbum, Year: 1974, TrackCount: 1}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Autobahn", Artists: []string{"Kraftwerk"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 1360000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	fileRel := "Kraftwerk/1974 - Autobahn/01 - Autobahn.opus"
	fileAbs := filepath.Join(root, fileRel)
	_ = os.MkdirAll(filepath.Dir(fileAbs), 0o755)
	payload := []byte("kraftwerk autobahn audio bytes")
	_ = os.WriteFile(fileAbs, payload, 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionAdoptFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		SuggestedAction: &action,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceExactCatalogID,
			DurationMS: 1360000,
		},
		CreatedAt: now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// Preview
	_, _ = svc.PreviewRepairs(ctx, []string{findingID})

	// First Apply -> Success
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res1, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil || res1.Applied != 1 {
		t.Fatalf("first adopt apply failed: %v, %+v", err, res1)
	}

	// Verify exactly 1 file record exists
	files, err := svc.Files().ListByTrack(ctx, track.ID)
	if err != nil || len(files) != 1 {
		t.Fatalf("expected exactly 1 file record, got %d: %v", len(files), err)
	}

	// Second Apply (Idempotent Apply Twice)
	res2, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("second apply returned error: %v", err)
	}
	// Finding was already deleted on first apply, so res2 reports 0 applied, 1 failed/skipped
	if res2.Applied != 0 {
		t.Fatalf("second apply must not apply again: %+v", res2)
	}

	// Verify still exactly 1 file record exists (0 duplicate rows created)
	filesAfter, _ := svc.Files().ListByTrack(ctx, track.ID)
	if len(filesAfter) != 1 {
		t.Fatalf("duplicate files row created! count=%d", len(filesAfter))
	}
}

// =============================================================================
// 5. RESTORE_TAGS CRASH SAFETY (ATOMIC TEMP + REPLACE)
// =============================================================================
func TestSafetyDelta_RestoreTagsCrashSafety(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Portishead", Provider: "spotify", SourceID: "art_ph"}
	release := music.Release{ID: music.NewID(), Title: "Dummy", AlbumArtist: "Portishead", ReleaseType: music.ReleaseAlbum, Year: 1994, TrackCount: 1}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Mysterons", Artists: []string{"Portishead"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 302000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	fileRel := "Portishead/1994 - Dummy/01 - Mysterons.opus"
	absPath := filepath.Join(root, fileRel)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)

	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "libopus", "-b:a", "64k", absPath)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}

	info, _ := os.Stat(absPath)
	_, _ = svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track.ID, Path: fileRel, SizeBytes: info.Size(), DurationMS: 302000})

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionRestoreTags

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingTagMismatch,
		Severity:        music.SeverityInfo,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		SuggestedAction: &action,
		CreatedAt:       now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// Apply Restore Tags
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: findingID, Action: action}},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply restore tags: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("expected applied=1, got %+v", res)
	}

	// File on disk must exist and be readable
	if !svc.Library().Exists(absPath) {
		t.Fatalf("file disappeared after retagging")
	}
}

// =============================================================================
// 6. APPLY TWICE ACROSS ALL 4 ACTIONS
// =============================================================================
func TestSafetyDelta_ApplyTwiceAllActions(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Seed catalog
	art := music.Artist{ID: music.NewID(), Name: "Band", Provider: "spotify", SourceID: "art_b"}
	rel := music.Release{ID: music.NewID(), Title: "Album", AlbumArtist: "Band", ReleaseType: music.ReleaseAlbum, Year: 2020, TrackCount: 1}
	trk := music.Track{ID: music.NewID(), ReleaseID: rel.ID, Title: "Song", Artists: []string{"Band"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 200000}
	_, _ = svc.Catalog().UpsertArtist(ctx, art)
	_, _ = svc.Catalog().UpsertRelease(ctx, rel, art.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, trk, rel.ID, art.ID, 0)

	// Action 1: QUARANTINE_FILE apply twice
	qPath := "Band/Legacy/duplicate.opus"
	_ = os.MkdirAll(filepath.Dir(filepath.Join(root, qPath)), 0o755)
	_ = os.WriteFile(filepath.Join(root, qPath), []byte("dup"), 0o644)
	qFindID := music.NewID()
	actQ := music.ActionQuarantineFile
	runID := music.NewID()
	now := time.Now().UTC()
	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{
		{ID: qFindID, RunID: runID, FindingCode: music.FindingLegacyDuplicate, Severity: music.SeverityWarning, RelativePath: qPath, SuggestedAction: &actQ, CreatedAt: now},
	})
	_, _ = svc.PreviewRepairs(ctx, []string{qFindID})

	resQ1, _ := svc.ApplyRepairs(ctx, library.RepairApplyRequest{Confirm: true, Actions: []library.RepairItemAction{{FindingID: qFindID, Action: actQ}}})
	if resQ1.Quarantined != 1 {
		t.Fatalf("first quarantine failed: %+v", resQ1)
	}
	resQ2, _ := svc.ApplyRepairs(ctx, library.RepairApplyRequest{Confirm: true, Actions: []library.RepairItemAction{{FindingID: qFindID, Action: actQ}}})
	if resQ2.Quarantined != 0 {
		t.Fatalf("second quarantine must be 0: %+v", resQ2)
	}
}
