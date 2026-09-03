package library_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestIntegrityEngine_10kFilesBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 10k files benchmark in short mode")
	}

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

	// Seed 10 artists, 100 releases (10 per artist), 100 tracks per release = 10,000 tracks
	const numArtists = 10
	const releasesPerArtist = 10
	const tracksPerRelease = 100
	const totalTracks = numArtists * releasesPerArtist * tracksPerRelease // 10,000

	t.Logf("Seeding %d catalog tracks and filesystem entries...", totalTracks)

	for a := 1; a <= numArtists; a++ {
		artist := music.Artist{
			ID:       music.NewID(),
			Name:     fmt.Sprintf("Artist %03d", a),
			Provider: "spotify",
			SourceID: fmt.Sprintf("src_art_%03d", a),
		}
		createdArtist, err := catRepo.UpsertArtist(ctx, artist)
		if err != nil {
			t.Fatalf("upsert artist: %v", err)
		}

		for r := 1; r <= releasesPerArtist; r++ {
			release := music.Release{
				ID:          music.NewID(),
				Title:       fmt.Sprintf("Album %03d", r),
				AlbumArtist: createdArtist.Name,
				ReleaseType: music.ReleaseAlbum,
				Year:        2000 + r,
				TrackCount:  tracksPerRelease,
				Provider:    "spotify",
				SourceID:    fmt.Sprintf("src_rel_%03d_%03d", a, r),
			}
			createdRelease, err := catRepo.UpsertRelease(ctx, release, createdArtist.ID)
			if err != nil {
				t.Fatalf("upsert release: %v", err)
			}

			relDir := filepath.Join(createdArtist.Name, fmt.Sprintf("%d - %s", createdRelease.Year, createdRelease.Title))
			absDir := filepath.Join(root, relDir)
			if err := os.MkdirAll(absDir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			for trk := 1; trk <= tracksPerRelease; trk++ {
				track := music.Track{
					ID:          music.NewID(),
					ReleaseID:   createdRelease.ID,
					Title:       fmt.Sprintf("Track %02d-%03d", r, trk),
					Artists:     []string{createdArtist.Name},
					Album:       createdRelease.Title,
					AlbumArtist: createdRelease.AlbumArtist,
					Year:        createdRelease.Year,
					TrackNumber: trk,
					DiscNumber:  1,
					DurationMS:  200000 + trk*1000,
					LyricsState: music.LyricsUnknown,
				}
				createdTrack, err := catRepo.UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
				if err != nil {
					t.Fatalf("upsert track: %v", err)
				}

				fileRel := filepath.Join(relDir, fmt.Sprintf("%02d - %s.opus", trk, createdTrack.Title))
				fileAbs := filepath.Join(root, fileRel)
				if err := os.WriteFile(fileAbs, []byte("dummy audio bytes for benchmark"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}

				if _, err := fileRepo.Upsert(ctx, music.File{
					ID:         music.NewID(),
					TrackID:    createdTrack.ID,
					Path:       fileRel,
					SizeBytes:  30,
					DurationMS: createdTrack.DurationMS,
				}); err != nil {
					t.Fatalf("upsert file: %v", err)
				}
			}
		}
	}

	t.Logf("Seeding complete. Running Quick Audit benchmark on 10,000 files...")

	var memStart runtime.MemStats
	runtime.ReadMemStats(&memStart)

	startTime := time.Now()
	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}

	completedRun, err := svc.WaitForAudit(ctx, run.ID, 30*time.Second)
	if err != nil {
		t.Fatalf("wait for audit: %v", err)
	}
	duration := time.Since(startTime)

	var memEnd runtime.MemStats
	runtime.ReadMemStats(&memEnd)

	memUsedMB := float64(memEnd.TotalAlloc-memStart.TotalAlloc) / (1024 * 1024)

	t.Logf("=== 10k Quick Audit Benchmark Results ===")
	t.Logf("Total Tracks / Files Scanned: %d", completedRun.Scanned)
	t.Logf("Total Time: %v (%.2f files/sec)", duration, float64(completedRun.Scanned)/duration.Seconds())
	t.Logf("Total Memory Allocated: %.2f MB", memUsedMB)
	t.Logf("Findings Count: %d (Status: %s)", completedRun.FindingsCount, completedRun.Status)

	if completedRun.Status != music.AuditRunCompleted {
		t.Fatalf("expected audit status %s, got %s", music.AuditRunCompleted, completedRun.Status)
	}
	if completedRun.Scanned != totalTracks {
		t.Fatalf("expected %d scanned, got %d", totalTracks, completedRun.Scanned)
	}
	if completedRun.FindingsCount != 0 {
		findings, _, _ := auditRepo.ListFindings(ctx, completedRun.ID, repository.ListFindingsOptions{Limit: 10})
		for i, f := range findings {
			t.Logf("unexpected finding %d: code=%s path=%s details=%s", i, f.FindingCode, f.RelativePath, f.Evidence.Details)
		}
		t.Fatalf("expected 0 findings for healthy 10k benchmark, got %d", completedRun.FindingsCount)
	}
	if duration > 10*time.Second {
		t.Fatalf("Quick Audit on 10,000 files took %v, which exceeds acceptable performance limit", duration)
	}
}

func TestIntegrityEngine_Healthy100FilesControl(t *testing.T) {
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

	// Seed 10 artists, 1 release per artist, 10 tracks per release = 100 tracks
	for a := 1; a <= 10; a++ {
		artist := music.Artist{
			ID:       music.NewID(),
			Name:     fmt.Sprintf("Control Artist %02d", a),
			Provider: "spotify",
			SourceID: fmt.Sprintf("ctrl_art_%02d", a),
		}
		createdArtist, err := catRepo.UpsertArtist(ctx, artist)
		if err != nil {
			t.Fatalf("upsert artist: %v", err)
		}

		release := music.Release{
			ID:          music.NewID(),
			Title:       fmt.Sprintf("Control Album %02d", a),
			AlbumArtist: createdArtist.Name,
			ReleaseType: music.ReleaseAlbum,
			Year:        2024,
			TrackCount:  10,
			Provider:    "spotify",
			SourceID:    fmt.Sprintf("src_ctrl_rel_%02d", a),
		}
		createdRelease, err := catRepo.UpsertRelease(ctx, release, createdArtist.ID)
		if err != nil {
			t.Fatalf("upsert release: %v", err)
		}

		relDir := filepath.Join(createdArtist.Name, fmt.Sprintf("%d - %s", createdRelease.Year, createdRelease.Title))
		absDir := filepath.Join(root, relDir)
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		for trk := 1; trk <= 10; trk++ {
			track := music.Track{
				ID:          music.NewID(),
				ReleaseID:   createdRelease.ID,
				Title:       fmt.Sprintf("Control Track %02d", trk),
				Artists:     []string{createdArtist.Name},
				Album:       createdRelease.Title,
				AlbumArtist: createdRelease.AlbumArtist,
				Year:        createdRelease.Year,
				TrackNumber: trk,
				DiscNumber:  1,
				DurationMS:  200000 + trk*1000,
				LyricsState: music.LyricsUnknown,
			}
			createdTrack, err := catRepo.UpsertTrack(ctx, track, createdRelease.ID, createdArtist.ID, 0)
			if err != nil {
				t.Fatalf("upsert track: %v", err)
			}

			fileRel := filepath.Join(relDir, fmt.Sprintf("%02d - %s.opus", trk, createdTrack.Title))
			fileAbs := filepath.Join(root, fileRel)
			if err := os.WriteFile(fileAbs, []byte("healthy audio stream bytes"), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			if _, err := fileRepo.Upsert(ctx, music.File{
				ID:         music.NewID(),
				TrackID:    createdTrack.ID,
				Path:       fileRel,
				SizeBytes:  26,
				DurationMS: createdTrack.DurationMS,
			}); err != nil {
				t.Fatalf("upsert file: %v", err)
			}
		}
	}

	// Run Quick Audit on healthy 100-file library
	run, err := svc.StartAudit(ctx, library.AuditModeQuick, nil)
	if err != nil {
		t.Fatalf("start audit: %v", err)
	}

	done, err := svc.WaitForAudit(ctx, run.ID, 10*time.Second)
	if err != nil {
		t.Fatalf("wait for audit: %v", err)
	}

	if done.Status != music.AuditRunCompleted {
		t.Fatalf("expected audit completed, got %s", done.Status)
	}
	if done.Scanned != 100 {
		t.Fatalf("expected 100 scanned files, got %d", done.Scanned)
	}
	if done.FindingsCount != 0 {
		findings, _, _ := auditRepo.ListFindings(ctx, done.ID, repository.ListFindingsOptions{Limit: 20})
		for i, f := range findings {
			t.Logf("unexpected finding %d: code=%s path=%s details=%s", i, f.FindingCode, f.RelativePath, f.Evidence.Details)
		}
		t.Fatalf("expected exactly 0 findings for healthy 100-file library, got %d", done.FindingsCount)
	}
}
