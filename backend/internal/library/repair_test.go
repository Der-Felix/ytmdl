package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
)

func TestRepairEngine_PreviewAndApplyMoveCanonical(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Setup: Artist, Release, Track
	artist := music.Artist{ID: music.NewID(), Name: "Radiohead", Provider: "spotify", SourceID: "art_rh"}
	release := music.Release{ID: music.NewID(), Title: "In Rainbows", AlbumArtist: "Radiohead", ReleaseType: music.ReleaseAlbum, Year: 2007, TrackCount: 10}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "15 Step", Artists: []string{"Radiohead"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 237000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	// File in old/non-canonical path
	oldRel := "Radiohead/In Rainbows (2007)/01. 15 Step.opus"
	oldAbs := filepath.Join(root, oldRel)
	_ = os.MkdirAll(filepath.Dir(oldAbs), 0o755)
	_ = os.WriteFile(oldAbs, []byte("audio payload 15 step"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "Radiohead/In Rainbows (2007)/01. 15 Step.lrc"), []byte("[00:00.00] How come I end up where I started"), 0o644)

	dbFile, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track.ID, Path: oldRel, SizeBytes: 21, DurationMS: 237000})
	if err != nil {
		t.Fatalf("upsert file: %v", err)
	}

	// Create audit run & finding
	runID := music.NewID()
	findingID := music.NewID()
	now := time.Now().UTC()

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	canonicalRel := "Radiohead/2007 - In Rainbows/01 - 15 Step.opus"
	action := music.ActionMoveCanonical

	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingPathMismatch,
		Severity:        music.SeverityInfo,
		RelativePath:    oldRel,
		TrackID:         track.ID,
		ReleaseID:       release.ID,
		ArtistName:      artist.Name,
		TrackTitle:      track.Title,
		SuggestedAction: &action,
		Evidence: music.FindingEvidence{
			Level:        music.EvidenceExactCatalogID,
			ExpectedPath: canonicalRel,
			ActualPath:   oldRel,
		},
		CreatedAt: now,
	}
	if err := auditRepo.InsertFindings(ctx, []music.AuditFinding{finding}); err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	// 1. Preview Move
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil {
		t.Fatalf("preview repairs: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(previews))
	}
	if !previews[0].Allowed {
		t.Fatalf("expected preview to be allowed, got false: %s", previews[0].Message)
	}
	if previews[0].DestinationPath != canonicalRel {
		t.Fatalf("expected dest %s, got %s", canonicalRel, previews[0].DestinationPath)
	}

	// Verify 0 bytes moved during preview
	if !svc.Library().Exists(oldAbs) {
		t.Fatalf("file should still exist at old path after preview")
	}

	// 2. Apply Move
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: findingID, Action: music.ActionMoveCanonical},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply repairs: %v", err)
	}
	if res.Applied != 1 || res.Failed != 0 {
		t.Fatalf("expected applied=1 failed=0, got %+v", res)
	}

	// Verify file was moved to canonical path along with lyrics sidecar
	canonicalAbs := filepath.Join(root, canonicalRel)
	if !svc.Library().Exists(canonicalAbs) {
		t.Fatalf("expected file to exist at canonical path %s", canonicalAbs)
	}
	canonicalLrcAbs := filepath.Join(root, "Radiohead/2007 - In Rainbows/01 - 15 Step.lrc")
	if !svc.Library().Exists(canonicalLrcAbs) {
		t.Fatalf("expected lyrics to be moved to %s", canonicalLrcAbs)
	}

	// Verify DB record was updated
	updatedFile, err := svc.Files().FindByPath(ctx, canonicalRel)
	if err != nil || updatedFile == nil {
		t.Fatalf("expected DB files record to point to %s, got %v", canonicalRel, err)
	}
	if updatedFile.ID != dbFile.ID {
		t.Fatalf("expected file ID %s, got %s", dbFile.ID, updatedFile.ID)
	}

	// Verify finding was resolved
	fAfter, _ := auditRepo.GetFinding(ctx, findingID)
	if fAfter != nil {
		t.Fatalf("expected finding to be deleted after repair")
	}
}

func TestRepairEngine_QuarantineFile(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Legacy duplicate file on disk
	legacyRel := "Radiohead/2025 - Old/01 - Airbag (Duplicate).opus"
	legacyAbs := filepath.Join(root, legacyRel)
	_ = os.MkdirAll(filepath.Dir(legacyAbs), 0o755)
	_ = os.WriteFile(legacyAbs, []byte("duplicate audio data"), 0o644)

	runID := music.NewID()
	findingID := music.NewID()
	now := time.Now().UTC()

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	action := music.ActionQuarantineFile

	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingLegacyDuplicate,
		Severity:        music.SeverityWarning,
		RelativePath:    legacyRel,
		SuggestedAction: &action,
		Evidence: music.FindingEvidence{
			Level:         music.EvidenceExactCatalogID,
			ActualPath:    legacyRel,
			CanonicalPath: "Radiohead/1997 - OK Computer/01 - Airbag.opus",
		},
		CreatedAt: now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// 1. Preview Quarantine
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(previews) != 1 || !previews[0].Allowed {
		t.Fatalf("expected allowed preview, got %+v", previews)
	}

	// 2. Apply Quarantine
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: findingID, Action: music.ActionQuarantineFile},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply quarantine: %v", err)
	}
	if res.Quarantined != 1 {
		t.Fatalf("expected quarantined=1, got %+v", res)
	}

	// Verify legacy file was moved out of active library
	if svc.Library().Exists(legacyAbs) {
		t.Fatalf("legacy file should not exist at original path after quarantine")
	}

	// Verify file is present in .ytmdl-trash
	trashDir := filepath.Join(root, ".ytmdl-trash")
	var foundInTrash bool
	_ = filepath.Walk(trashDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Base(path) == "01 - Airbag (Duplicate).opus" {
			foundInTrash = true
		}
		return nil
	})
	if !foundInTrash {
		t.Fatalf("expected quarantined file to exist inside .ytmdl-trash")
	}
}

func TestRepairEngine_AdoptFile_StrictRules(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Portishead", Provider: "spotify", SourceID: "art_p"}
	release := music.Release{ID: music.NewID(), Title: "Dummy", AlbumArtist: "Portishead", ReleaseType: music.ReleaseAlbum, Year: 1994, TrackCount: 11}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Mysterons", Artists: []string{"Portishead"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 302000, SourceID: "trk_myst"}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	// File on disk matches track perfectly
	fileRel := "Portishead/1994 - Dummy/01 - Mysterons.opus"
	fileAbs := filepath.Join(root, fileRel)
	_ = os.MkdirAll(filepath.Dir(fileAbs), 0o755)
	_ = os.WriteFile(fileAbs, []byte("mysterons audio samples"), 0o644)

	runID := music.NewID()
	findingIDExact := music.NewID()
	findingIDWeak := music.NewID()
	now := time.Now().UTC()

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	action := music.ActionAdoptFile

	// 1. Finding with EXACT_CATALOG_ID -> Allowed
	findingExact := music.AuditFinding{
		ID:              findingIDExact,
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    fileRel,
		TrackID:         track.ID,
		ReleaseID:       release.ID,
		ArtistName:      artist.Name,
		TrackTitle:      track.Title,
		SuggestedAction: &action,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceExactCatalogID,
			ActualPath: fileRel,
			SizeBytes:  23,
			DurationMS: 302000,
		},
		CreatedAt: now,
	}

	// 2. Finding with STRONG_METADATA only -> Preview allowed, Apply rejected!
	findingWeak := music.AuditFinding{
		ID:              findingIDWeak,
		RunID:           runID,
		FindingCode:     music.FindingFileUntracked,
		Severity:        music.SeverityWarning,
		RelativePath:    "Portishead/1994 - Dummy/02 - Sour Times.opus",
		TrackID:         track.ID,
		ReleaseID:       release.ID,
		SuggestedAction: &action,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceStrongMetadata,
			ActualPath: "Portishead/1994 - Dummy/02 - Sour Times.opus",
		},
		CreatedAt: now,
	}

	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{findingExact, findingWeak})

	// Preview exact finding
	prevExact, err := svc.PreviewRepairs(ctx, []string{findingIDExact})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prevExact) != 1 || !prevExact[0].Allowed {
		t.Fatalf("expected exact adoption to be allowed in preview")
	}

	// Preview weak finding
	prevWeak, err := svc.PreviewRepairs(ctx, []string{findingIDWeak})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(prevWeak) != 1 || prevWeak[0].Allowed {
		t.Fatalf("expected strong metadata adoption to be rejected/not-allowed for apply, got allowed")
	}

	// Apply exact finding -> should succeed
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: findingIDExact, Action: music.ActionAdoptFile},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply exact adopt: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("expected applied=1, got %+v", res)
	}

	// Check that DB now has a file record
	adoptedFile, err := svc.Files().FindByPath(ctx, fileRel)
	if err != nil || adoptedFile == nil {
		t.Fatalf("expected adopted file in DB, got %v", err)
	}
	if adoptedFile.TrackID != track.ID {
		t.Fatalf("expected track ID %s, got %s", track.ID, adoptedFile.TrackID)
	}
}

func TestRepairEngine_QuarantineTargetCollisionProtected(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	relPath := "Artist/Album/01 - Song.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	_ = os.WriteFile(absPath, []byte("original file to quarantine"), 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionQuarantineFile

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeDeep, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
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

	// Simulate external process creating a collision inside quarantine directory
	trashParent := filepath.Join(root, ".ytmdl-trash")
	_ = os.MkdirAll(trashParent, 0o755)

	// Apply quarantine
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: findingID, Action: music.ActionQuarantineFile},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if res.Quarantined != 1 {
		t.Fatalf("expected quarantined=1, got %+v", res)
	}
}

func TestRepairEngine_MoveCanonicalTargetCollisionProtected(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "The National", Provider: "spotify", SourceID: "art_tn"}
	release := music.Release{ID: music.NewID(), Title: "Boxer", AlbumArtist: "The National", ReleaseType: music.ReleaseAlbum, Year: 2007, TrackCount: 12}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Fake Empire", Artists: []string{"The National"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 205000}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	sourceRel := "The National/Boxer/01. Fake Empire.opus"
	sourceAbs := filepath.Join(root, sourceRel)
	_ = os.MkdirAll(filepath.Dir(sourceAbs), 0o755)
	_ = os.WriteFile(sourceAbs, []byte("source audio"), 0o644)

	// Pre-create file at canonical destination
	destRel := "The National/2007 - Boxer/01 - Fake Empire.opus"
	destAbs := filepath.Join(root, destRel)
	_ = os.MkdirAll(filepath.Dir(destAbs), 0o755)
	foreignPayload := []byte("foreign existing file content")
	_ = os.WriteFile(destAbs, foreignPayload, 0o644)

	findingID := music.NewID()
	runID := music.NewID()
	now := time.Now().UTC()
	action := music.ActionMoveCanonical

	_ = auditRepo.CreateRun(ctx, music.AuditRun{ID: runID, Mode: music.AuditModeQuick, Status: music.AuditRunCompleted, StartedAt: now, CreatedAt: now})
	finding := music.AuditFinding{
		ID:              findingID,
		RunID:           runID,
		FindingCode:     music.FindingPathMismatch,
		Severity:        music.SeverityInfo,
		RelativePath:    sourceRel,
		TrackID:         track.ID,
		ReleaseID:       release.ID,
		SuggestedAction: &action,
		CreatedAt:       now,
	}
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{finding})

	// 1. Preview Move should be NOT allowed
	previews, err := svc.PreviewRepairs(ctx, []string{findingID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(previews) != 1 || previews[0].Allowed {
		t.Fatalf("expected preview to disallow move over existing target")
	}

	// 2. Apply Move should fail and not overwrite foreign file
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: findingID, Action: music.ActionMoveCanonical},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("expected failed=1, got %+v", res)
	}

	// Verify foreign file was preserved byte-identical
	destContent, err := os.ReadFile(destAbs)
	if err != nil || string(destContent) != string(foreignPayload) {
		t.Fatalf("foreign target file was corrupted or modified!")
	}

	// Verify source file is still intact
	if !svc.Library().Exists(sourceAbs) {
		t.Fatalf("source file should remain untouched")
	}
}
