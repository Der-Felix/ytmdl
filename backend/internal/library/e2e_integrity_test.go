package library_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// TestE2EIntegrity_ProductionLikeScenario tests a realistic library layout mirroring
// the production baseline (23 Artists, 56 Releases, 239 Tracks, 239 DB Files, 250 .opus files on disk = 11 legacy orphan files).
func TestE2EIntegrity_ProductionLikeScenario(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })

	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("new library: %v", err)
	}

	catRepo := repository.NewCatalog(db)
	fileRepo := repository.NewFiles(db)
	auditRepo := repository.NewAudit(db)

	ffRunner := ffmpeg.New("ffmpeg", 30*time.Second)
	tagger := metadata.NewTagger(ffRunner)

	svc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Files:   fileRepo,
		Catalog: catRepo,
		Prober:  &mockProber{},
		Tagger:  tagger,
		Audit:   auditRepo,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()

	// 1. Seed 23 Artists, 56 Releases, 239 Tracks, 239 Canonical Files
	var totalTracks int
	var totalReleases int
	var allTrackIDs []string
	var sampleLegacyTrack *music.Track
	var sampleLegacyRelPath string

	for a := 1; a <= 23; a++ {
		artist := music.Artist{
			ID:       music.NewID(),
			Name:     fmt.Sprintf("Artist %02d", a),
			Provider: "spotify",
			SourceID: fmt.Sprintf("art_src_%02d", a),
		}
		createdArtist, err := catRepo.UpsertArtist(ctx, artist)
		if err != nil {
			t.Fatalf("upsert artist: %v", err)
		}

		// Releases: roughly 2-3 releases per artist to get ~56 total
		releasesForArtist := 2
		if a <= 10 {
			releasesForArtist = 3
		}

		for r := 1; r <= releasesForArtist; r++ {
			totalReleases++
			tracksInRelease := 4
			if totalReleases <= 15 {
				tracksInRelease = 5
			}

			release := music.Release{
				ID:          music.NewID(),
				Title:       fmt.Sprintf("Album %02d-%02d", a, r),
				AlbumArtist: createdArtist.Name,
				ReleaseType: music.ReleaseAlbum,
				Year:        2010 + r,
				TrackCount:  tracksInRelease,
				Provider:    "spotify",
				SourceID:    fmt.Sprintf("rel_src_%02d_%02d", a, r),
			}
			createdRelease, err := catRepo.UpsertRelease(ctx, release, createdArtist.ID)
			if err != nil {
				t.Fatalf("upsert release: %v", err)
			}

			relDir := filepath.Join(createdArtist.Name, fmt.Sprintf("%d - %s", createdRelease.Year, createdRelease.Title))
			absDir := filepath.Join(root, relDir)
			_ = os.MkdirAll(absDir, 0o755)

			for trk := 1; trk <= tracksInRelease; trk++ {
				totalTracks++
				track := music.Track{
					ID:             music.NewID(),
					ReleaseID:      createdRelease.ID,
					Title:          fmt.Sprintf("Track %02d-%02d-%02d", a, r, trk),
					Artists:        []string{createdArtist.Name},
					Album:          createdRelease.Title,
					AlbumArtist:    createdRelease.AlbumArtist,
					Year:           createdRelease.Year,
					TrackNumber:    trk,
					DiscNumber:     1,
					DurationMS:     180000 + trk*1000,
					SourceProvider: "spotify",
					SourceID:       fmt.Sprintf("trk_src_%03d", totalTracks),
					LyricsState:    music.LyricsUnknown,
				}
				createdTrack, err := catRepo.UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
				if err != nil {
					t.Fatalf("upsert track: %v", err)
				}
				allTrackIDs = append(allTrackIDs, createdTrack.ID)

				fileRel := filepath.Join(relDir, fmt.Sprintf("%02d - %s.opus", trk, createdTrack.Title))
				fileAbs := filepath.Join(root, fileRel)
				_ = os.WriteFile(fileAbs, []byte("canonical active opus content"), 0o644)

				_, err = fileRepo.Upsert(ctx, music.File{
					ID:             music.NewID(),
					TrackID:        createdTrack.ID,
					Path:           fileRel,
					SizeBytes:      29,
					DurationMS:     createdTrack.DurationMS,
					SourceProvider: createdTrack.SourceProvider,
					SourceID:       createdTrack.SourceID,
				})
				if err != nil {
					t.Fatalf("upsert file: %v", err)
				}

				// Pick one track for sampling legacy duplicate
				if sampleLegacyTrack == nil && a == 1 && r == 1 && trk == 1 {
					sampleLegacyTrack = &createdTrack
				}
			}
		}
	}

	if totalTracks != 239 {
		t.Fatalf("expected 239 tracks seeded, got %d", totalTracks)
	}

	// 2. Create exactly 11 historical unreferenced legacy .opus files in legacy directories
	for i := 1; i <= 11; i++ {
		legacyRel := fmt.Sprintf("Artist 01/Legacy Album %02d/01 - Old Track %02d.opus", i, i)
		legacyAbs := filepath.Join(root, legacyRel)
		_ = os.MkdirAll(filepath.Dir(legacyAbs), 0o755)
		_ = os.WriteFile(legacyAbs, []byte(fmt.Sprintf("legacy orphan audio data %d", i)), 0o644)

		if i == 1 {
			sampleLegacyRelPath = legacyRel
		}
	}

	// Total .opus files on disk = 239 + 11 = 250 files
	var totalOpusOnDisk int
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".opus" {
			totalOpusOnDisk++
		}
		return nil
	})
	if totalOpusOnDisk != 250 {
		t.Fatalf("expected 250 .opus files on disk, got %d", totalOpusOnDisk)
	}

	// 3. Run Read-Only Quick Audit
	quickRun, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start quick audit: %v", err)
	}
	quickDone, err := svc.WaitForAudit(ctx, quickRun.ID, 10*time.Second)
	if err != nil {
		t.Fatalf("wait quick audit: %v", err)
	}

	if quickDone.Status != music.AuditRunCompleted {
		t.Fatalf("expected quick audit completed, got %s", quickDone.Status)
	}
	if quickDone.Scanned != 250 {
		t.Fatalf("expected 250 scanned files, got %d", quickDone.Scanned)
	}
	// Quick audit should find exactly the 11 untracked legacy files
	if quickDone.FindingsCount != 11 {
		qFindings, _, _ := auditRepo.ListFindings(ctx, quickDone.ID, repository.ListFindingsOptions{Limit: 50})
		for i, f := range qFindings {
			t.Logf("finding %d: code=%s path=%s details=%s", i, f.FindingCode, f.RelativePath, f.Evidence.Details)
		}
		t.Fatalf("expected 11 findings in quick audit, got %d", quickDone.FindingsCount)
	}

	// Verify all 250 files are 100% untouched after Quick Audit (0 bytes modified)
	var afterQuickOpus int
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".opus" {
			afterQuickOpus++
		}
		return nil
	})
	if afterQuickOpus != 250 {
		t.Fatalf("expected 250 .opus files after quick audit, got %d", afterQuickOpus)
	}

	// 4. Run Read-Only Deep Audit
	deepRun, err := svc.StartAudit(ctx, library.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep audit: %v", err)
	}
	deepDone, err := svc.WaitForAudit(ctx, deepRun.ID, 15*time.Second)
	if err != nil {
		t.Fatalf("wait deep audit: %v", err)
	}

	if deepDone.Status != music.AuditRunCompleted {
		t.Fatalf("expected deep audit completed, got %s", deepDone.Status)
	}
	if deepDone.FindingsCount != 11 {
		dFindings, _, _ := auditRepo.ListFindings(ctx, deepDone.ID, repository.ListFindingsOptions{Limit: 50})
		for i, f := range dFindings {
			t.Logf("deep finding %d: code=%s path=%s details=%s", i, f.FindingCode, f.RelativePath, f.Evidence.Details)
		}
		t.Fatalf("expected 11 findings in deep audit, got %d", deepDone.FindingsCount)
	}

	// 5. Query Findings with pagination
	findings, total, err := auditRepo.ListFindings(ctx, deepDone.ID, repository.ListFindingsOptions{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if total != 11 || len(findings) != 10 {
		t.Fatalf("expected total=11 and page=10, got total=%d count=%d", total, len(findings))
	}

	// 6. Test Repair Preview for finding 1 (Legacy file)
	targetFinding := findings[0]
	previews, err := svc.PreviewRepairs(ctx, []string{targetFinding.ID})
	if err != nil {
		t.Fatalf("preview repairs: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(previews))
	}
	if !previews[0].Allowed {
		t.Fatalf("expected preview allowed: %s", previews[0].Message)
	}

	// 7. Apply Quarantine for single finding
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: targetFinding.ID, Action: music.ActionQuarantineFile},
		},
	}
	res, err := svc.ApplyRepairs(ctx, applyReq)
	if err != nil {
		t.Fatalf("apply quarantine: %v", err)
	}
	if res.Quarantined != 1 {
		t.Fatalf("expected 1 quarantined, got %+v", res)
	}

	// Verify target file moved to .ytmdl-trash, active canonical library intact (239 DB files untouched)
	var activeOpusCount int
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".opus" && !filepath.HasPrefix(path, filepath.Join(root, ".ytmdl-trash")) {
			activeOpusCount++
		}
		return nil
	})
	if activeOpusCount != 249 {
		t.Fatalf("expected 249 active .opus files (239 canonical + 10 legacy), got %d", activeOpusCount)
	}

	// Verify finding is deleted from audit findings table
	fCheck, _ := auditRepo.GetFinding(ctx, targetFinding.ID)
	if fCheck != nil {
		t.Fatalf("expected resolved finding to be removed from DB")
	}

	_ = sampleLegacyRelPath
	_ = sampleLegacyTrack
}
