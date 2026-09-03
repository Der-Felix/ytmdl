package library

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// generateTestOpus creates a genuine Opus audio file using ffmpeg.
func generateTestOpus(t *testing.T, targetPath string, durationSec int) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg executable not found in PATH")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "1",
		"-c:a", "libopus",
		"-b:a", "96k",
		targetPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate test opus: %v, output: %s", err, string(out))
	}
}

func TestE2EReconciliationSixStatesAndRepairs(t *testing.T) {
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockCatalog()
	files := newMockFiles()
	jobMgr := &mockJobs{unfinishedMap: make(map[string]bool)}

	ffRunner := ffmpeg.New("ffmpeg", 30*time.Second)
	tagger := metadata.NewTagger(ffRunner)
	prober := downloader.NewProber(downloader.ProberOptions{Binary: "ffprobe", Timeout: 30 * time.Second})
	broker := &mockBroker{}

	svc, err := NewService(ServiceOptions{
		Library:     lib,
		Catalog:     catalog,
		Files:       files,
		Jobs:        jobMgr,
		Prober:      prober,
		Tagger:      tagger,
		Broker:      broker,
		Concurrency: 4,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// A: Healthy Track
	relDirA := filepath.Join("Artist A", "2021 - Album A")
	fileRelA := filepath.Join(relDirA, "01 - Healthy Track.opus")
	fileAbsA := filepath.Join(root, fileRelA)
	generateTestOpus(t, fileAbsA, 1)

	trackA := music.Track{
		ID:          "trk-a",
		ReleaseID:   "rel-a",
		Title:       "Healthy Track",
		Artists:     []string{"Artist A"},
		Album:       "Album A",
		AlbumArtist: "Artist A",
		TrackNumber: 1,
		Year:        2021,
	}
	catalog.tracks[trackA.ID] = trackA
	catalog.releases["rel-a"] = music.Release{ID: "rel-a", Title: "Album A", Artists: []string{"Artist A"}, Year: 2021}
	catalog.sources[trackA.ID] = []music.Source{
		{TrackID: trackA.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-a"},
	}
	tagsA := metadata.TagsFor(trackA, provider.MediaSource{Provider: "deezer", ID: "src-a"})
	if err := tagger.Apply(ctx, fileAbsA, tagsA, nil); err != nil {
		t.Fatalf("failed to tag healthy track: %v", err)
	}
	probeA, _ := prober.Probe(ctx, fileAbsA)
	files.files[fileRelA] = music.File{
		ID:        "file-a",
		TrackID:   trackA.ID,
		Path:      fileRelA,
		SizeBytes: probeA.SizeBytes,
		Codec:     "opus",
	}

	// B: Missing File (DB row exists, physical file missing)
	trackB := music.Track{
		ID:          "trk-b",
		ReleaseID:   "rel-b",
		Title:       "Missing Track",
		Artists:     []string{"Artist B"},
		Album:       "Album B",
		AlbumArtist: "Artist B",
		TrackNumber: 1,
		Year:        2022,
	}
	fileRelB := filepath.Join("Artist B", "2022 - Album B", "01 - Missing Track.opus")
	catalog.tracks[trackB.ID] = trackB
	catalog.releases["rel-b"] = music.Release{ID: "rel-b", Title: "Album B", Artists: []string{"Artist B"}, Year: 2022}
	catalog.sources[trackB.ID] = []music.Source{
		{TrackID: trackB.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-b"},
	}
	files.files[fileRelB] = music.File{
		ID:        "file-b",
		TrackID:   trackB.ID,
		Path:      fileRelB,
		SizeBytes: 50000,
		Codec:     "opus",
	}

	// C: Orphan Audio File (File on disk, no DB row)
	fileRelC := filepath.Join("Artist C", "2020 - Album C", "01 - Orphan Track.opus")
	fileAbsC := filepath.Join(root, fileRelC)
	generateTestOpus(t, fileAbsC, 1)

	// D: Invalid Audio File (Damaged/corrupt bytes with .opus extension)
	fileRelD := filepath.Join("Artist D", "2023 - Album D", "01 - Corrupted Track.opus")
	fileAbsD := filepath.Join(root, fileRelD)
	if err := os.MkdirAll(filepath.Dir(fileAbsD), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileAbsD, []byte("NOT_AN_OPUS_FILE_CORRUPTED_BYTES_1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	trackD := music.Track{
		ID:          "trk-d",
		ReleaseID:   "rel-d",
		Title:       "Corrupted Track",
		Artists:     []string{"Artist D"},
		Album:       "Album D",
		AlbumArtist: "Artist D",
		TrackNumber: 1,
		Year:        2023,
	}
	catalog.tracks[trackD.ID] = trackD
	catalog.releases["rel-d"] = music.Release{ID: "rel-d", Title: "Album D", Artists: []string{"Artist D"}, Year: 2023}
	catalog.sources[trackD.ID] = []music.Source{
		{TrackID: trackD.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-d"},
	}
	files.files[fileRelD] = music.File{
		ID:        "file-d",
		TrackID:   trackD.ID,
		Path:      fileRelD,
		SizeBytes: 42,
		Codec:     "opus",
	}

	// E: Metadata Mismatch (Valid audio, but tags do not match DB catalog)
	fileRelE := filepath.Join("Artist E", "2024 - Album E", "01 - Mismatch Track.opus")
	fileAbsE := filepath.Join(root, fileRelE)
	generateTestOpus(t, fileAbsE, 1)
	trackE := music.Track{
		ID:          "trk-e",
		ReleaseID:   "rel-e",
		Title:       "Correct Catalog Title",
		Artists:     []string{"Correct Artist"},
		Album:       "Correct Album",
		AlbumArtist: "Correct Artist",
		TrackNumber: 1,
		Year:        2024,
	}
	catalog.tracks[trackE.ID] = trackE
	catalog.releases["rel-e"] = music.Release{ID: "rel-e", Title: "Correct Album", Artists: []string{"Correct Artist"}, Year: 2024}
	catalog.sources[trackE.ID] = []music.Source{
		{TrackID: trackE.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-e"},
	}
	// Write wrong tags onto file E
	wrongTrack := trackE
	wrongTrack.Title = "Wrong Title From Old Source"
	wrongTrack.Album = "Wrong Album Name"
	wrongTags := metadata.TagsFor(wrongTrack, provider.MediaSource{Provider: "deezer", ID: "src-e"})
	// Also create a dummy cover.jpg in release E folder
	coverPathE := filepath.Join(filepath.Dir(fileAbsE), storage.CoverFileName)
	_ = os.WriteFile(coverPathE, []byte("fake-jpeg-cover-data"), 0o644)
	if err := tagger.Apply(ctx, fileAbsE, wrongTags, &metadata.Artwork{Data: []byte("fake-jpeg-cover-data"), MIME: "image/jpeg"}); err != nil {
		t.Fatalf("failed to apply wrong tags on file E: %v", err)
	}
	probeE, _ := prober.Probe(ctx, fileAbsE)
	files.files[fileRelE] = music.File{
		ID:        "file-e",
		TrackID:   trackE.ID,
		Path:      fileRelE,
		SizeBytes: probeE.SizeBytes,
		Codec:     "opus",
	}

	// F: Duplicate File (Two track records pointing to identical file path)
	trackF1 := music.Track{
		ID:          "trk-f1",
		ReleaseID:   "rel-f",
		Title:       "Duplicate Track 1",
		Artists:     []string{"Artist F"},
		Album:       "Album F",
		AlbumArtist: "Artist F",
		TrackNumber: 1,
		Year:        2025,
	}
	trackF2 := music.Track{
		ID:          "trk-f2",
		ReleaseID:   "rel-f",
		Title:       "Duplicate Track 2",
		Artists:     []string{"Artist F"},
		Album:       "Album F",
		AlbumArtist: "Artist F",
		TrackNumber: 2,
		Year:        2025,
	}
	fileRelF := filepath.Join("Artist F", "2025 - Album F", "01 - Dup.opus")
	fileAbsF := filepath.Join(root, fileRelF)
	generateTestOpus(t, fileAbsF, 1)
	catalog.tracks[trackF1.ID] = trackF1
	catalog.tracks[trackF2.ID] = trackF2
	catalog.releases["rel-f"] = music.Release{ID: "rel-f", Title: "Album F", Artists: []string{"Artist F"}, Year: 2025}
	catalog.sources[trackF1.ID] = []music.Source{{TrackID: trackF1.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-f1"}}
	catalog.sources[trackF2.ID] = []music.Source{{TrackID: trackF2.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-f2"}}
	files.files[fileRelF] = music.File{ID: "file-f1", TrackID: trackF1.ID, Path: fileRelF, SizeBytes: 100, Codec: "opus"}
	// Second file record with different ID but same Path
	files.files[fileRelF+"-dup"] = music.File{ID: "file-f2", TrackID: trackF2.ID, Path: fileRelF, SizeBytes: 100, Codec: "opus"}

	// STEP 1: Execute Full Reconciliation Scan
	scanResult, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconciliation scan failed: %v", err)
	}

	// Verify Scan Summary Counts

	if scanResult.Summary.Healthy != 1 {
		t.Errorf("expected 1 healthy, got %d", scanResult.Summary.Healthy)
	}
	if scanResult.Summary.MissingFiles != 1 {
		t.Errorf("expected 1 missing, got %d", scanResult.Summary.MissingFiles)
	}
	if scanResult.Summary.OrphanFiles != 1 {
		t.Errorf("expected 1 orphan, got %d", scanResult.Summary.OrphanFiles)
	}
	if scanResult.Summary.InvalidFiles != 1 {
		t.Errorf("expected 1 invalid, got %d", scanResult.Summary.InvalidFiles)
	}
	if scanResult.Summary.MetadataMismatches != 1 {
		t.Errorf("expected 1 mismatch, got %d", scanResult.Summary.MetadataMismatches)
	}
	if scanResult.Summary.DuplicateFiles < 1 {
		t.Errorf("expected at least 1 duplicate, got %d", scanResult.Summary.DuplicateFiles)
	}

	// Find the orphan issue ID for Step 3
	var orphanIssueID string
	for _, iss := range scanResult.Issues {
		if iss.Status == StatusOrphanFile && iss.Path == fileRelC {
			orphanIssueID = iss.ID
		}
	}
	if orphanIssueID == "" {
		t.Fatal("orphan issue ID not found in scan results")
	}

	// STEP 2: Repair E2E - Missing Track Redownload
	job, err := svc.RedownloadTrack(ctx, trackB.ID)
	if err != nil {
		t.Fatalf("RedownloadTrack failed: %v", err)
	}
	if job.TargetID != "src-b" {
		t.Errorf("expected redownload job targetID 'src-b', got %q", job.TargetID)
	}

	// STEP 3: Repair E2E - In-Place Retagging with ffprobe verification
	// Probe before retagging
	probeBefore, err := prober.Probe(ctx, fileAbsE)
	if err != nil {
		t.Fatalf("ffprobe before retag failed: %v", err)
	}

	// Execute Retag
	if err := svc.RetagTrack(ctx, trackE.ID); err != nil {
		t.Fatalf("RetagTrack failed: %v", err)
	}

	// Probe after retagging
	probeAfter, err := prober.Probe(ctx, fileAbsE)
	if err != nil {
		t.Fatalf("ffprobe after retag failed: %v", err)
	}

	// Assert Stream Copy Integrity (no audio re-encoding)
	if probeBefore.Codec != probeAfter.Codec {
		t.Errorf("Codec mismatch: before=%s, after=%s", probeBefore.Codec, probeAfter.Codec)
	}
	if probeBefore.SampleRate != probeAfter.SampleRate {
		t.Errorf("SampleRate mismatch: before=%d, after=%d", probeBefore.SampleRate, probeAfter.SampleRate)
	}
	if probeBefore.Channels != probeAfter.Channels {
		t.Errorf("Channels mismatch: before=%d, after=%d", probeBefore.Channels, probeAfter.Channels)
	}
	if probeBefore.DurationMS != probeAfter.DurationMS {
		t.Errorf("DurationMS mismatch: before=%d, after=%d", probeBefore.DurationMS, probeAfter.DurationMS)
	}

	// Verify Cover File on disk is preserved
	if _, err := os.Stat(coverPathE); os.IsNotExist(err) {
		t.Error("cover.jpg was unexpectedly removed during retag")
	}

	// STEP 4: Repair E2E - Track Delete
	if err := svc.DeleteTrack(ctx, trackA.ID); err != nil {
		t.Fatalf("DeleteTrack failed: %v", err)
	}
	if _, err := os.Stat(fileAbsA); !os.IsNotExist(err) {
		t.Errorf("file A was not deleted from disk")
	}
	if _, ok := catalog.tracks[trackA.ID]; ok {
		t.Errorf("track A was not deleted from catalog")
	}

	// STEP 5: Repair E2E - Orphan Delete via Issue ID
	if err := svc.DeleteOrphanIssue(ctx, orphanIssueID); err != nil {
		t.Fatalf("DeleteOrphanIssue failed: %v", err)
	}
	if _, err := os.Stat(fileAbsC); !os.IsNotExist(err) {
		t.Errorf("orphan file C was not deleted from disk")
	}

	// STEP 6: Repair E2E - Surgical Release Delete
	// Create a release with 1 track, 1 cover.jpg, and 1 foreign file (e.g. manual text note)
	relDirSurgical := filepath.Join(root, "Artist S", "2026 - Release S")
	trackS := music.Track{
		ID:          "trk-s",
		ReleaseID:   "rel-s",
		Title:       "Surgical Track",
		Artists:     []string{"Artist S"},
		Album:       "Release S",
		AlbumArtist: "Artist S",
		TrackNumber: 1,
		Year:        2026,
	}
	catalog.tracks[trackS.ID] = trackS
	catalog.releases["rel-s"] = music.Release{ID: "rel-s", Title: "Release S", Artists: []string{"Artist S"}, Year: 2026}
	fileRelS := filepath.Join("Artist S", "2026 - Release S", "01 - Surgical Track.opus")
	fileAbsS := filepath.Join(root, fileRelS)
	generateTestOpus(t, fileAbsS, 1)
	files.files[fileRelS] = music.File{ID: "file-s", TrackID: trackS.ID, Path: fileRelS, SizeBytes: 100, Codec: "opus"}

	coverPathS := filepath.Join(relDirSurgical, storage.CoverFileName)
	_ = os.WriteFile(coverPathS, []byte("cover-data"), 0o644)

	foreignFile := filepath.Join(relDirSurgical, "custom_notes.txt")
	_ = os.WriteFile(foreignFile, []byte("user manual notes"), 0o644)

	// Execute Release Delete
	if err := svc.DeleteRelease(ctx, "rel-s"); err != nil {
		t.Fatalf("DeleteRelease failed: %v", err)
	}

	// Verify track file and cover are deleted
	if _, err := os.Stat(fileAbsS); !os.IsNotExist(err) {
		t.Errorf("track file S was not deleted")
	}
	if _, err := os.Stat(coverPathS); !os.IsNotExist(err) {
		t.Errorf("cover.jpg was not deleted")
	}
	// Verify foreign file remains intact
	if _, err := os.Stat(foreignFile); os.IsNotExist(err) {
		t.Errorf("foreign file custom_notes.txt was unexpectedly deleted by Release Delete!")
	}
}

func TestE2ESecurityAttacks(t *testing.T) {
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockCatalog()
	files := newMockFiles()
	jobMgr := &mockJobs{unfinishedMap: make(map[string]bool)}

	svc, err := NewService(ServiceOptions{
		Library:     lib,
		Catalog:     catalog,
		Files:       files,
		Jobs:        jobMgr,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 1. Path Traversal Attack: ../../etc/passwd
	_, _, err = VerifyPathConfinement(root, "../../etc/passwd", true)
	if err == nil {
		t.Error("expected Path Traversal '../../etc/passwd' to be rejected")
	}

	// 2. Absolute Path Outside Root: /etc/passwd
	_, _, err = VerifyPathConfinement(root, "/etc/passwd", true)
	if err == nil {
		t.Error("expected Absolute Path '/etc/passwd' to be rejected")
	}

	// 3. Symlink Escaping Root
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.opus")
	_ = os.WriteFile(secretFile, []byte("secret content"), 0o644)

	symlinkPath := filepath.Join(root, "symlink_escape.opus")
	_ = os.Symlink(secretFile, symlinkPath)

	_, _, err = VerifyPathConfinement(root, "symlink_escape.opus", false)
	if err == nil {
		t.Error("expected Symlink escaping root to be rejected")
	}

	// 4. Nested Symlink Directory Escaping Root
	nestedSymlinkDir := filepath.Join(root, "nested_sym_dir")
	_ = os.Symlink(outsideDir, nestedSymlinkDir)
	nestedTargetPath := filepath.Join("nested_sym_dir", "secret.opus")

	_, _, err = VerifyPathConfinement(root, nestedTargetPath, false)
	if err == nil {
		t.Error("expected Nested symlink directory escaping root to be rejected")
	}

	// 5. Manipulated DB Path
	manipulatedTrack := music.Track{
		ID:          "trk-evil",
		ReleaseID:   "rel-evil",
		Title:       "Evil",
		Artists:     []string{"Evil"},
		Album:       "Evil",
		AlbumArtist: "Evil",
		TrackNumber: 1,
		Year:        2026,
	}
	catalog.tracks[manipulatedTrack.ID] = manipulatedTrack
	files.files["../../../etc/shadow"] = music.File{
		ID:      "file-evil",
		TrackID: manipulatedTrack.ID,
		Path:    "../../../etc/shadow",
	}
	// Try DeleteTrack on manipulated path
	if err := svc.DeleteTrack(ctx, manipulatedTrack.ID); err != nil {
		// Should not panic or delete outside
	}
	// Secret file must remain untouched
	if _, err := os.Stat(secretFile); os.IsNotExist(err) {
		t.Error("secret file was deleted!")
	}

	// 6. Manipulated / Stale Orphan Issue ID
	err = svc.DeleteOrphanIssue(ctx, "non-existent-or-manipulated-issue-id")
	if err == nil {
		t.Error("expected manipulated/stale orphan issue ID to be rejected with 404")
	}
}

func TestE2EJobConflictHandling(t *testing.T) {
	root := t.TempDir()
	lib, _ := storage.NewLibrary(root)
	catalog := newMockCatalog()
	files := newMockFiles()
	jobMgr := &mockJobs{unfinishedMap: make(map[string]bool)}

	svc, _ := NewService(ServiceOptions{
		Library: lib,
		Catalog: catalog,
		Files:   files,
		Jobs:    jobMgr,
	})

	ctx := context.Background()

	track := music.Track{
		ID:          "trk-active",
		ReleaseID:   "rel-active",
		Title:       "Active Track",
		Artists:     []string{"Active"},
		Album:       "Active Album",
		AlbumArtist: "Active",
		SourceID:    "src-active",
	}
	catalog.tracks[track.ID] = track
	catalog.releases[track.ReleaseID] = music.Release{ID: track.ReleaseID, SourceID: "rel-src-active"}
	catalog.sources[track.ID] = []music.Source{{TrackID: track.ID, Kind: music.SourceMetadata, Provider: "deezer", SourceID: "src-active"}}
	files.files["active.opus"] = music.File{ID: "f-active", TrackID: track.ID, Path: "active.opus"}
	_ = os.WriteFile(filepath.Join(root, "active.opus"), []byte("active"), 0o644)

	// Mark download job as active
	jobMgr.unfinishedMap["src-active"] = true
	jobMgr.unfinishedMap["trk-active"] = true
	jobMgr.unfinishedMap["rel-src-active"] = true
	jobMgr.unfinishedMap["rel-active"] = true

	// 1. Redownload should return 409 Conflict (CodeAlreadyExists)
	_, err := svc.RedownloadTrack(ctx, track.ID)
	if err == nil {
		t.Error("expected RedownloadTrack to return conflict error")
	}

	// 2. Retag should return 409 Conflict
	err = svc.RetagTrack(ctx, track.ID)
	if err == nil {
		t.Error("expected RetagTrack to return conflict error")
	}

	// 3. DeleteTrack should return 409 Conflict
	err = svc.DeleteTrack(ctx, track.ID)
	if err == nil {
		t.Error("expected DeleteTrack to return conflict error")
	}

	// 4. DeleteRelease should return 409 Conflict
	err = svc.DeleteRelease(ctx, track.ReleaseID)
	if err == nil {
		t.Error("expected DeleteRelease to return conflict error")
	}
}
