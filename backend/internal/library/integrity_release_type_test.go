package library_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
)

// TestIntegrityEngine_ReleaseType_Album_Canonical verifies that canonical albums produce 0 PATH_MISMATCH.
func TestIntegrityEngine_ReleaseType_Album_Canonical(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "The Beatles", Provider: "spotify", SourceID: "art_beatles"}
	release := music.Release{
		ID:          music.NewID(),
		Title:       "Abbey Road",
		AlbumArtist: "The Beatles",
		ReleaseType: music.ReleaseAlbum,
		Year:        1969,
		TrackCount:  1,
		Provider:    "spotify",
		SourceID:    "rel_beatles_abbey",
	}
	track := music.Track{
		ID:          music.NewID(),
		ReleaseID:   release.ID,
		Title:       "Come Together",
		Artists:     []string{"The Beatles"},
		TrackNumber: 1,
		DiscNumber:  1,
		DurationMS:  259000,
	}

	relPath := "The Beatles/1969 - Abbey Road/01 - Come Together.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	if err := os.WriteFile(absPath, []byte("dummy audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := svc.Catalog().UpsertArtist(ctx, artist); err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	track.ReleaseID = createdRelease.ID
	createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, artist.ID, 0)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
		t.Fatalf("upsert file: %v", err)
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			t.Fatalf("unexpected PATH_MISMATCH finding on canonical album: %+v", f)
		}
	}
}

// TestIntegrityEngine_ReleaseType_Single_Canonical verifies that canonical singles ([Single]) produce 0 PATH_MISMATCH.
func TestIntegrityEngine_ReleaseType_Single_Canonical(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Ann Boland", Provider: "spotify", SourceID: "art_ann"}
	release := music.Release{
		ID:          music.NewID(),
		Title:       "Letting Go",
		AlbumArtist: "Ann Boland",
		ReleaseType: music.ReleaseSingle,
		Year:        2011,
		TrackCount:  1,
		Provider:    "spotify",
		SourceID:    "rel_ann_letting_go",
	}
	track := music.Track{
		ID:          music.NewID(),
		ReleaseID:   release.ID,
		Title:       "Letting Go",
		Artists:     []string{"Ann Boland"},
		TrackNumber: 1,
		DiscNumber:  1,
		DurationMS:  210000,
	}

	relPath := "Ann Boland/2011 - Letting Go [Single]/01 - Letting Go.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	if err := os.WriteFile(absPath, []byte("dummy single audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := svc.Catalog().UpsertArtist(ctx, artist); err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	track.ReleaseID = createdRelease.ID
	createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, artist.ID, 0)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
		t.Fatalf("upsert file: %v", err)
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			t.Fatalf("unexpected PATH_MISMATCH finding on canonical single: %+v", f)
		}
	}
}

// TestIntegrityEngine_ReleaseType_EP_Canonical verifies that canonical EPs ([EP]) produce 0 PATH_MISMATCH.
func TestIntegrityEngine_ReleaseType_EP_Canonical(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Kevin MacLeod", Provider: "spotify", SourceID: "art_km"}
	release := music.Release{
		ID:          music.NewID(),
		Title:       "Impact",
		AlbumArtist: "Kevin MacLeod",
		ReleaseType: music.ReleaseEP,
		Year:        2014,
		TrackCount:  1,
		Provider:    "spotify",
		SourceID:    "rel_km_impact",
	}
	track := music.Track{
		ID:          music.NewID(),
		ReleaseID:   release.ID,
		Title:       "Allegretto",
		Artists:     []string{"Kevin MacLeod"},
		TrackNumber: 1,
		DiscNumber:  1,
		DurationMS:  140000,
	}

	relPath := "Kevin MacLeod/2014 - Impact [EP]/01 - Allegretto.opus"
	absPath := filepath.Join(root, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	if err := os.WriteFile(absPath, []byte("dummy ep audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := svc.Catalog().UpsertArtist(ctx, artist); err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	track.ReleaseID = createdRelease.ID
	createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, artist.ID, 0)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
		t.Fatalf("upsert file: %v", err)
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			t.Fatalf("unexpected PATH_MISMATCH finding on canonical EP: %+v", f)
		}
	}
}

// TestIntegrityEngine_ReleaseType_OtherTypes_Canonical tests compilation, live, remix.
func TestIntegrityEngine_ReleaseType_OtherTypes_Canonical(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testCases := []struct {
		relType music.ReleaseType
		suffix  string
	}{
		{music.ReleaseCompilation, "[Compilation]"},
		{music.ReleaseLive, "[Live]"},
		{music.ReleaseRemix, "[Remix]"},
	}

	for idx, tc := range testCases {
		artName := fmt.Sprintf("Artist %d", idx)
		relTitle := fmt.Sprintf("Release %d", idx)
		artist := music.Artist{ID: music.NewID(), Name: artName, Provider: "spotify", SourceID: fmt.Sprintf("art_%d", idx)}
		release := music.Release{
			ID:          music.NewID(),
			Title:       relTitle,
			AlbumArtist: artName,
			ReleaseType: tc.relType,
			Year:        2020 + idx,
			TrackCount:  1,
			Provider:    "spotify",
			SourceID:    fmt.Sprintf("rel_other_%d", idx),
		}
		track := music.Track{
			ID:          music.NewID(),
			ReleaseID:   release.ID,
			Title:       fmt.Sprintf("Track %d", idx),
			Artists:     []string{artName},
			TrackNumber: 1,
			DiscNumber:  1,
			DurationMS:  180000,
		}

		relPath := fmt.Sprintf("%s/%04d - %s %s/01 - %s.opus", artName, release.Year, relTitle, tc.suffix, track.Title)
		absPath := filepath.Join(root, relPath)
		_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
		if err := os.WriteFile(absPath, []byte("dummy audio"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		createdArtist, err := svc.Catalog().UpsertArtist(ctx, artist)
		if err != nil {
			t.Fatalf("upsert artist: %v", err)
		}
		createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, createdArtist.ID)
		if err != nil {
			t.Fatalf("upsert release: %v", err)
		}
		track.ReleaseID = createdRelease.ID
		createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
		if err != nil {
			t.Fatalf("upsert track: %v", err)
		}
		if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
			t.Fatalf("register file: %v", err)
		}
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			t.Fatalf("unexpected PATH_MISMATCH finding on canonical %s: %+v", f.RelativePath, f)
		}
	}
}

// TestIntegrityEngine_Real_Path_Mismatch_Detected ensures real path mismatches are still caught.
func TestIntegrityEngine_Real_Path_Mismatch_Detected(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Daft Punk", Provider: "spotify", SourceID: "art_daft"}
	release := music.Release{
		ID:          music.NewID(),
		Title:       "One More Time",
		AlbumArtist: "Daft Punk",
		ReleaseType: music.ReleaseSingle,
		Year:        2000,
		TrackCount:  1,
		Provider:    "spotify",
		SourceID:    "rel_daft_single",
	}
	track := music.Track{
		ID:          music.NewID(),
		ReleaseID:   release.ID,
		Title:       "One More Time",
		Artists:     []string{"Daft Punk"},
		TrackNumber: 1,
		DiscNumber:  1,
		DurationMS:  320000,
	}

	// Deliberately wrong physical and DB path (no [Single] suffix)
	wrongRelPath := "Daft Punk/2000 - Wrong Folder/01 - One More Time.opus"
	absPath := filepath.Join(root, wrongRelPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	if err := os.WriteFile(absPath, []byte("dummy audio"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	createdArtist, err := svc.Catalog().UpsertArtist(ctx, artist)
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, createdArtist.ID)
	if err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	track.ReleaseID = createdRelease.ID
	createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
	if err != nil {
		t.Fatalf("upsert track: %v", err)
	}
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: wrongRelPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
		t.Fatalf("register file: %v", err)
	}

	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}

	var mismatchFinding *music.AuditFinding
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			mismatchFinding = &f
			break
		}
	}

	if mismatchFinding == nil {
		t.Fatalf("expected PATH_MISMATCH finding for misplaced single, got 0 findings")
	}
	expectedCanonical := "Daft Punk/2000 - One More Time [Single]/01 - One More Time.opus"
	if mismatchFinding.Evidence.ExpectedPath != expectedCanonical {
		t.Fatalf("expected canonical path %q, got %q", expectedCanonical, mismatchFinding.Evidence.ExpectedPath)
	}

	// Verify Repair Preview consistency
	prevs, err := svc.PreviewRepairs(ctx, []string{mismatchFinding.ID})
	if err != nil {
		t.Fatalf("preview repair: %v", err)
	}
	if len(prevs) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(prevs))
	}
	if prevs[0].DestinationPath != expectedCanonical {
		t.Fatalf("preview destination %q != audit expected %q", prevs[0].DestinationPath, expectedCanonical)
	}
	if !prevs[0].Allowed {
		t.Fatalf("expected repair to be allowed, got message: %s", prevs[0].Message)
	}
}

// TestIntegrityEngine_ProductionLike_53Singles_28EPs_ZeroMismatch simulates the exact production scenario.
func TestIntegrityEngine_ProductionLike_53Singles_28EPs_ZeroMismatch(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	artist := music.Artist{ID: music.NewID(), Name: "Production Artist", Provider: "spotify", SourceID: "art_prod"}
	createdArtist, err := svc.Catalog().UpsertArtist(ctx, artist)
	if err != nil {
		t.Fatalf("upsert artist: %v", err)
	}

	// Create 53 singles
	for i := 1; i <= 53; i++ {
		title := fmt.Sprintf("Single Song %02d", i)
		release := music.Release{
			ID:          music.NewID(),
			Title:       title,
			AlbumArtist: createdArtist.Name,
			ReleaseType: music.ReleaseSingle,
			Year:        2020 + (i % 5),
			TrackCount:  1,
			Provider:    "spotify",
			SourceID:    fmt.Sprintf("prod_single_rel_%d", i),
		}
		track := music.Track{
			ID:          music.NewID(),
			ReleaseID:   release.ID,
			Title:       title,
			Artists:     []string{createdArtist.Name},
			TrackNumber: 1,
			DiscNumber:  1,
			DurationMS:  200000,
		}
		relPath := fmt.Sprintf("%s/%04d - %s [Single]/01 - %s.opus", createdArtist.Name, release.Year, title, title)
		absPath := filepath.Join(root, relPath)
		_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
		_ = os.WriteFile(absPath, []byte("single audio"), 0o644)

		createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, createdArtist.ID)
		if err != nil {
			t.Fatalf("upsert single release %d: %v", i, err)
		}
		track.ReleaseID = createdRelease.ID
		createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
		if err != nil {
			t.Fatalf("upsert single track %d: %v", i, err)
		}
		if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
			t.Fatalf("register single file %d: %v", i, err)
		}
	}

	// Create 28 EPs
	for i := 1; i <= 28; i++ {
		title := fmt.Sprintf("EP Title %02d", i)
		release := music.Release{
			ID:          music.NewID(),
			Title:       title,
			AlbumArtist: createdArtist.Name,
			ReleaseType: music.ReleaseEP,
			Year:        2015 + (i % 8),
			TrackCount:  1,
			Provider:    "spotify",
			SourceID:    fmt.Sprintf("prod_ep_rel_%d", i),
		}
		track := music.Track{
			ID:          music.NewID(),
			ReleaseID:   release.ID,
			Title:       fmt.Sprintf("EP Track %02d", i),
			Artists:     []string{createdArtist.Name},
			TrackNumber: 1,
			DiscNumber:  1,
			DurationMS:  180000,
		}
		relPath := fmt.Sprintf("%s/%04d - %s [EP]/01 - %s.opus", createdArtist.Name, release.Year, title, track.Title)
		absPath := filepath.Join(root, relPath)
		_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
		_ = os.WriteFile(absPath, []byte("ep audio"), 0o644)

		createdRelease, err := svc.Catalog().UpsertRelease(ctx, release, createdArtist.ID)
		if err != nil {
			t.Fatalf("upsert EP release %d: %v", i, err)
		}
		track.ReleaseID = createdRelease.ID
		createdTrack, err := svc.Catalog().UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
		if err != nil {
			t.Fatalf("upsert EP track %d: %v", i, err)
		}
		if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: createdTrack.ID, Path: relPath, SizeBytes: 1024, Codec: "opus"}); err != nil {
			t.Fatalf("register EP file %d: %v", i, err)
		}
	}

	// Run Quick Audit
	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}
	comp, err := svc.WaitForAudit(ctx, run.ID, 15*time.Second)
	if err != nil || comp.Status != music.AuditRunCompleted {
		t.Fatalf("quick audit failed: %v, status: %s", err, comp.Status)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 200})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}

	var mismatchCount int
	for _, f := range findings {
		if f.FindingCode == music.FindingPathMismatch {
			mismatchCount++
		}
	}

	if mismatchCount != 0 {
		t.Fatalf("expected 0 PATH_MISMATCH findings on 53 Singles + 28 EPs canonical fixture, got %d", mismatchCount)
	}

	// Also verify Deep Audit yields 0 PATH_MISMATCH
	deepRun, err := svc.StartAudit(ctx, library.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep audit: %v", err)
	}
	deepComp, err := svc.WaitForAudit(ctx, deepRun.ID, 15*time.Second)
	if err != nil || deepComp.Status != music.AuditRunCompleted {
		t.Fatalf("deep audit failed: %v, status: %s", err, deepComp.Status)
	}

	deepFindings, _, err := auditRepo.ListFindings(ctx, deepRun.ID, repository.ListFindingsOptions{Limit: 200})
	if err != nil {
		t.Fatalf("list deep findings: %v", err)
	}
	for _, f := range deepFindings {
		if f.FindingCode == music.FindingPathMismatch {
			t.Fatalf("unexpected PATH_MISMATCH finding in deep audit: %+v", f)
		}
	}
}
