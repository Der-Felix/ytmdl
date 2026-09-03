package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

func seedTrackWithFileAndArtists(t *testing.T, catalog *mockCatalog, files *mockFiles,
	relPath string, artists []string, albumArtist, album string, relType music.ReleaseType, year int) string {
	t.Helper()
	trackID := music.NewID()
	track := music.Track{
		ID:          trackID,
		Title:       album,
		Artists:     artists,
		Album:       album,
		AlbumArtist: albumArtist,
		ReleaseType: relType,
		TrackNumber: 1,
		TrackTotal:  1,
		DiscNumber:  1,
		DiscTotal:   1,
		Year:        year,
		LyricsState: music.LyricsAvailablePlain,
	}
	catalog.tracks[trackID] = track
	files.files[relPath] = music.File{
		ID:        music.NewID(),
		TrackID:   trackID,
		Path:      relPath,
		Codec:     "opus",
		Container: "ogg",
	}
	return trackID
}

func seedMultiDiscTrack(t *testing.T, catalog *mockCatalog, files *mockFiles,
	relPath string, discNumber, discTotal, trackNumber int) string {
	t.Helper()
	trackID := music.NewID()
	track := music.Track{
		ID:          trackID,
		Title:       "A",
		Artists:     []string{"X"},
		Album:       "B",
		AlbumArtist: "X",
		ReleaseType: music.ReleaseAlbum,
		TrackNumber: trackNumber,
		TrackTotal:  1,
		DiscNumber:  discNumber,
		DiscTotal:   discTotal,
		Year:        2001,
		LyricsState: music.LyricsAvailablePlain,
	}
	catalog.tracks[trackID] = track
	files.files[relPath] = music.File{
		ID:        music.NewID(),
		TrackID:   trackID,
		Path:      relPath,
		Codec:     "opus",
		Container: "ogg",
	}
	return trackID
}

func seedTrackWithoutTotals(t *testing.T, catalog *mockCatalog, files *mockFiles, relPath string) string {
	t.Helper()
	trackID := music.NewID()
	track := music.Track{
		ID:          trackID,
		Title:       "A",
		Artists:     []string{"X"},
		Album:       "B",
		AlbumArtist: "X",
		ReleaseType: music.ReleaseAlbum,
		TrackNumber: 1,
		TrackTotal:  0, // missing
		DiscNumber:  1,
		DiscTotal:   0, // missing
		Year:        2001,
		LyricsState: music.LyricsAvailablePlain,
	}
	catalog.tracks[trackID] = track
	files.files[relPath] = music.File{
		ID:        music.NewID(),
		TrackID:   trackID,
		Path:      relPath,
		Codec:     "opus",
		Container: "ogg",
	}
	return trackID
}

func TestCompatibilityReportFindsJoinedArtistFolder(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rel := "LACAZETTE & Bushido/2025 - CCN [Single]/01 - CCN.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedTrackWithFileAndArtists(t, catalog, files, rel,
		[]string{"LACAZETTE", "Bushido"}, "LACAZETTE & Bushido", "CCN", music.ReleaseSingle, 2025)

	report, err := svc.CompatibilityReport(context.Background())
	if err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	var finding *CompatIssue
	for i := range report.Issues {
		if report.Issues[i].Kind == CompatArtistFolder {
			finding = &report.Issues[i]
		}
	}
	if finding == nil {
		t.Fatalf("no artist_folder finding in %+v", report.Issues)
	}
	if finding.To != "LACAZETTE/2025 - CCN [Single]/01 - CCN.opus" {
		t.Errorf("To = %q", finding.To)
	}
	if finding.From != rel {
		t.Errorf("From = %q", finding.From)
	}
}

func TestCompatibilityReportIsReadOnly(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rel := "A & B/2025 - C [Single]/01 - C.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedTrackWithFileAndArtists(t, catalog, files, rel, []string{"A", "B"}, "A & B", "C", music.ReleaseSingle, 2025)

	if _, err := svc.CompatibilityReport(context.Background()); err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal("the report must not move a single file")
	}
}

func TestCompatibilityReportFindsLegacyMultiDiscName(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rel := "X/2001 - B/2-01 - A.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedMultiDiscTrack(t, catalog, files, rel, 2, 2, 1)

	report, err := svc.CompatibilityReport(context.Background())
	if err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Kind == CompatMultiDiscName && issue.To == "X/2001 - B/201 - A.opus" {
			return
		}
	}
	t.Fatalf("no multidisc finding in %+v", report.Issues)
}

func TestCompatibilityReportFindsMissingTotals(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rel := "X/2001 - B/01 - A.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedTrackWithoutTotals(t, catalog, files, rel)

	report, err := svc.CompatibilityReport(context.Background())
	if err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Kind == CompatMissingTotals {
			return
		}
	}
	t.Fatalf("no missing-totals finding in %+v", report.Issues)
}

func TestReorganizeRequiresConfirmation(t *testing.T) {
	svc, _, _, _, _, _, _ := setupTestService(t)
	_, err := svc.Reorganize(context.Background(), ReorganizeRequest{Confirm: false, IssueIDs: []string{"x"}})
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("an unconfirmed reorganize must be refused, got %v", err)
	}
}

func TestReorganizeMovesOnlyTheSelectedIssues(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	moved := "LACAZETTE & Bushido/2025 - CCN [Single]/01 - CCN.opus"
	kept := "AVIE & LACAZETTE/2025 - XOX [Single]/01 - XOX.opus"
	for _, rel := range []string{moved, kept} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("x"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(root, filepath.FromSlash(
		"LACAZETTE & Bushido/2025 - CCN [Single]/01 - CCN.lrc")), []byte("[00:01.00]a"), 0o644)
	seedTrackWithFileAndArtists(t, catalog, files, moved, []string{"LACAZETTE", "Bushido"}, "LACAZETTE & Bushido", "CCN", music.ReleaseSingle, 2025)
	seedTrackWithFileAndArtists(t, catalog, files, kept, []string{"AVIE", "LACAZETTE"}, "AVIE & LACAZETTE", "XOX", music.ReleaseSingle, 2025)

	report, err := svc.CompatibilityReport(context.Background())
	if err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	var selected string
	for _, issue := range report.Issues {
		if issue.Kind == CompatArtistFolder && issue.From == moved {
			selected = issue.ID
		}
	}
	if selected == "" {
		t.Fatal("the moved track produced no finding")
	}

	result, err := svc.Reorganize(context.Background(), ReorganizeRequest{Confirm: true, IssueIDs: []string{selected}})
	if err != nil {
		t.Fatalf("Reorganize: %v", err)
	}
	if result.Moved != 1 {
		t.Fatalf("moved = %d, want 1", result.Moved)
	}
	newAudio := filepath.Join(root, filepath.FromSlash("LACAZETTE/2025 - CCN [Single]/01 - CCN.opus"))
	if _, err := os.Stat(newAudio); err != nil {
		t.Fatalf("the audio file was not moved: %v", err)
	}
	newLyrics := filepath.Join(root, filepath.FromSlash("LACAZETTE/2025 - CCN [Single]/01 - CCN.lrc"))
	if _, err := os.Stat(newLyrics); err != nil {
		t.Fatalf("the sidecar did not follow its audio file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(kept))); err != nil {
		t.Fatal("an unselected file must not be touched")
	}
	stored, _ := files.FindByPath(context.Background(), "LACAZETTE/2025 - CCN [Single]/01 - CCN.opus")
	if stored == nil {
		t.Fatal("the files row was not updated to the new path")
	}
}

func TestReorganizeRefusesToOverwrite(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	from := "A & B/2025 - C [Single]/01 - C.opus"
	to := "A/2025 - C [Single]/01 - C.opus"
	for _, rel := range []string{from, to} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("x"), 0o644)
	}
	seedTrackWithFileAndArtists(t, catalog, files, from, []string{"A", "B"}, "A & B", "C", music.ReleaseSingle, 2025)

	report, err := svc.CompatibilityReport(context.Background())
	if err != nil {
		t.Fatalf("CompatibilityReport: %v", err)
	}
	var id string
	for _, issue := range report.Issues {
		if issue.Kind == CompatArtistFolder {
			id = issue.ID
		}
	}
	result, err := svc.Reorganize(context.Background(), ReorganizeRequest{Confirm: true, IssueIDs: []string{id}})
	if err != nil {
		t.Fatalf("Reorganize: %v", err)
	}
	if result.Moved != 0 || len(result.Warnings) == 0 {
		t.Fatalf("result = %+v, want a refusal with a warning", result)
	}
}
