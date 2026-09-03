package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

type mockProber struct {
	probeFunc func(ctx context.Context, path string) (*library.AudioInfo, error)
}

func (m *mockProber) Probe(ctx context.Context, path string) (*library.AudioInfo, error) {
	if m.probeFunc != nil {
		return m.probeFunc(ctx, path)
	}
	return &library.AudioInfo{
		DurationMS: 180000,
		Codec:      "opus",
		SampleRate: 48000,
		Channels:   2,
		SizeBytes:  1024,
	}, nil
}

func setupIntegrityTest(t *testing.T) (*library.Service, *repository.Audit, *storage.Library, string) {
	t.Helper()
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

	return svc, auditRepo, lib, root
}

func TestIntegrityEngine_QuickAudit_MissingAndUntracked(t *testing.T) {
	svc, auditRepo, lib, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Create Artist, Release, Track in DB
	artist := music.Artist{ID: music.NewID(), Name: "Radiohead", Provider: "spotify", SourceID: "art_1"}
	release := music.Release{ID: music.NewID(), Title: "OK Computer", AlbumArtist: "Radiohead", ReleaseType: music.ReleaseAlbum, Year: 1997, TrackCount: 12}
	track1 := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Airbag", Artists: []string{"Radiohead"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 284000, LyricsState: music.LyricsAvailableSynced}
	track2 := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Paranoid Android", Artists: []string{"Radiohead"}, TrackNumber: 2, DiscNumber: 1, DurationMS: 383000, LyricsState: music.LyricsUnknown}

	// Insert into DB
	dbFile1Rel := "Radiohead/1997 - OK Computer/01 - Airbag.opus"
	dbFile1Abs := filepath.Join(root, dbFile1Rel)
	_ = os.MkdirAll(filepath.Dir(dbFile1Abs), 0o755)

	// File 1: Track 1 has DB record and physical file on disk (Healthy)
	if err := os.WriteFile(dbFile1Abs, []byte("OggS dummy audio data 1"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	// Also create lyrics sidecar for track 1
	_ = os.WriteFile(filepath.Join(root, "Radiohead/1997 - OK Computer/01 - Airbag.lrc"), []byte("[00:00.00] In the next world war"), 0o644)

	// File 2: Track 2 has DB record but physical file is MISSING
	dbFile2Rel := "Radiohead/1997 - OK Computer/02 - Paranoid Android.opus"

	// Create records in catalog
	if _, err := svc.Catalog().UpsertArtist(ctx, artist); err != nil {
		t.Fatalf("upsert artist: %v", err)
	}
	if _, err := svc.Catalog().UpsertRelease(ctx, release, artist.ID); err != nil {
		t.Fatalf("upsert release: %v", err)
	}
	if _, err := svc.Catalog().UpsertTrack(ctx, track1, release.ID, artist.ID, 0); err != nil {
		t.Fatalf("upsert track1: %v", err)
	}
	if _, err := svc.Catalog().UpsertTrack(ctx, track2, release.ID, artist.ID, 0); err != nil {
		t.Fatalf("upsert track2: %v", err)
	}

	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track1.ID, Path: dbFile1Rel, SizeBytes: 24, DurationMS: 284000}); err != nil {
		t.Fatalf("upsert file1: %v", err)
	}
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track2.ID, Path: dbFile2Rel, SizeBytes: 30, DurationMS: 383000}); err != nil {
		t.Fatalf("upsert file2: %v", err)
	}

	// File 3: Physical untracked audio file on disk
	untrackedRel := "Radiohead/2025 - Old/01 - Untracked Song.opus"
	untrackedAbs := filepath.Join(root, untrackedRel)
	_ = os.MkdirAll(filepath.Dir(untrackedAbs), 0o755)
	if err := os.WriteFile(untrackedAbs, []byte("OggS dummy audio untracked"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	// File 4: Reserved directory file (.ytmdl-trash) - must be ignored!
	trashDir := filepath.Join(root, ".ytmdl-trash", "20260828_123")
	_ = os.MkdirAll(trashDir, 0o755)
	_ = os.WriteFile(filepath.Join(trashDir, "trashed.opus"), []byte("trash audio"), 0o644)

	// Run Quick Audit
	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}

	// Wait for audit to complete
	completedRun, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait for audit: %v", err)
	}
	if completedRun.Status != music.AuditRunCompleted {
		t.Fatalf("expected audit status completed, got %s (err: %s)", completedRun.Status, completedRun.ErrorSummary)
	}

	// Verify findings
	findings, total, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}

	findingsByCode := make(map[music.FindingCode][]music.AuditFinding)
	for _, f := range findings {
		findingsByCode[f.FindingCode] = append(findingsByCode[f.FindingCode], f)
	}

	// Check FILE_MISSING for Track 2
	missing := findingsByCode[music.FindingFileMissing]
	if len(missing) != 1 {
		t.Fatalf("expected 1 FILE_MISSING finding, got %d", len(missing))
	}
	if missing[0].RelativePath != dbFile2Rel {
		t.Fatalf("expected missing path %s, got %s", dbFile2Rel, missing[0].RelativePath)
	}

	// Check FILE_UNTRACKED for untracked song
	untracked := findingsByCode[music.FindingFileUntracked]
	if len(untracked) != 1 {
		t.Fatalf("expected 1 FILE_UNTRACKED finding, got %d", len(untracked))
	}
	if untracked[0].RelativePath != untrackedRel {
		t.Fatalf("expected untracked path %s, got %s", untrackedRel, untracked[0].RelativePath)
	}

	// Check RELEASE_INCOMPLETE for OK Computer (12 expected, only 1 present on disk)
	incomplete := findingsByCode[music.FindingReleaseIncomplete]
	if len(incomplete) != 1 {
		t.Fatalf("expected 1 RELEASE_INCOMPLETE finding, got %d", len(incomplete))
	}

	// Verify trashed file was completely ignored
	for _, f := range findings {
		if filepath.HasPrefix(f.RelativePath, ".ytmdl-trash") {
			t.Fatalf("expected .ytmdl-trash to be ignored, but found: %s", f.RelativePath)
		}
	}
	_ = total
	_ = lib
}

func TestIntegrityEngine_DeepAudit_LegacyAndAudioInvalid(t *testing.T) {
	svc, auditRepo, _, root := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Create Artist, Release, Track in DB
	artist := music.Artist{ID: music.NewID(), Name: "The Smile", Provider: "spotify", SourceID: "art_smile"}
	release := music.Release{ID: music.NewID(), Title: "Wall of Eyes", AlbumArtist: "The Smile", ReleaseType: music.ReleaseAlbum, Year: 2024, TrackCount: 8}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Teleharmonic", Artists: []string{"The Smile"}, TrackNumber: 2, DiscNumber: 1, DurationMS: 310000, SourceID: "trk_tele"}

	_, _ = svc.Catalog().UpsertArtist(ctx, artist)
	_, _ = svc.Catalog().UpsertRelease(ctx, release, artist.ID)
	_, _ = svc.Catalog().UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	// Canonical path for track
	canonicalRel := "The Smile/2024 - Wall of Eyes/02 - Teleharmonic.opus"
	canonicalAbs := filepath.Join(root, canonicalRel)
	_ = os.MkdirAll(filepath.Dir(canonicalAbs), 0o755)
	_ = os.WriteFile(canonicalAbs, []byte("canonical valid opus content"), 0o644)

	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: track.ID, Path: canonicalRel, SizeBytes: 28, DurationMS: 310000, SourceID: "trk_tele"}); err != nil {
		t.Fatalf("upsert file: %v", err)
	}

	// Legacy file on disk (e.g. from single download in older folder)
	legacyRel := "The Smile/2024 - Teleharmonic [Single]/01 - Teleharmonic.opus"
	legacyAbs := filepath.Join(root, legacyRel)
	_ = os.MkdirAll(filepath.Dir(legacyAbs), 0o755)
	_ = os.WriteFile(legacyAbs, []byte("legacy duplicate audio"), 0o644)

	// Corrupt audio file in DB
	corruptTrack := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Friend of a Friend", Artists: []string{"The Smile"}, TrackNumber: 3, DiscNumber: 1, DurationMS: 275000}
	_, _ = svc.Catalog().UpsertTrack(ctx, corruptTrack, release.ID, artist.ID, 0)

	corruptRel := "The Smile/2024 - Wall of Eyes/03 - Friend of a Friend.opus"
	corruptAbs := filepath.Join(root, corruptRel)
	_ = os.WriteFile(corruptAbs, []byte("corrupted non-audio file"), 0o644)
	if _, err := svc.Files().Upsert(ctx, music.File{ID: music.NewID(), TrackID: corruptTrack.ID, Path: corruptRel, SizeBytes: 24, DurationMS: 275000}); err != nil {
		t.Fatalf("upsert corrupt file: %v", err)
	}

	// Custom prober that fails on corruptAbs
	svc.SetProber(&mockProber{
		probeFunc: func(ctx context.Context, path string) (*library.AudioInfo, error) {
			if strings.HasSuffix(path, corruptRel) {
				return nil, os.ErrInvalid
			}
			return &library.AudioInfo{
				DurationMS: 310000,
				Codec:      "opus",
				SampleRate: 48000,
				Channels:   2,
				SizeBytes:  1024,
			}, nil
		},
	})

	// Run Deep Audit
	run, err := svc.StartAudit(ctx, library.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start deep audit: %v", err)
	}

	completedRun, err := svc.WaitForAudit(ctx, run.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait for audit: %v", err)
	}
	if completedRun.Status != music.AuditRunCompleted {
		t.Fatalf("expected completed audit, got %s (err: %s)", completedRun.Status, completedRun.ErrorSummary)
	}

	findings, _, err := auditRepo.ListFindings(ctx, run.ID, repository.ListFindingsOptions{Limit: 50})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}

	var hasAudioInvalid, hasUntrackedOrLegacy bool
	for _, f := range findings {
		if f.FindingCode == music.FindingAudioInvalid && f.RelativePath == corruptRel {
			hasAudioInvalid = true
		}
		if (f.FindingCode == music.FindingLegacyDuplicate || f.FindingCode == music.FindingFileUntracked) && f.RelativePath == legacyRel {
			hasUntrackedOrLegacy = true
		}
	}

	if !hasAudioInvalid {
		t.Fatalf("expected AUDIO_INVALID for corrupt track %s", corruptRel)
	}
	if !hasUntrackedOrLegacy {
		t.Fatalf("expected LEGACY_DUPLICATE or FILE_UNTRACKED for %s", legacyRel)
	}
}

func TestIntegrityEngine_SingleActiveAuditConstraint(t *testing.T) {
	svc, _, _, _ := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run1, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit 1: %v", err)
	}

	// Starting a second audit while first is active should fail with Conflict
	_, err = svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err == nil {
		t.Fatalf("expected error when starting concurrent audit, got nil")
	}

	_, _ = svc.WaitForAudit(ctx, run1.ID, 5*time.Second)
}

func TestIntegrityEngine_CancelAudit(t *testing.T) {
	svc, auditRepo, _, _ := setupIntegrityTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run, err := svc.StartAudit(ctx, library.AuditModeDeep, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}

	// Cancel audit
	if err := svc.CancelAudit(ctx, run.ID); err != nil {
		t.Fatalf("cancel audit: %v", err)
	}

	reloaded, err := auditRepo.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if reloaded.Status != music.AuditRunCancelled && reloaded.Status != music.AuditRunCompleted {
		t.Fatalf("expected cancelled status, got %s", reloaded.Status)
	}
}
