package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// memSettings is an in-memory settings table.
type memSettings struct {
	mu   sync.Mutex
	m    map[string]string
	sets map[string]int
}

func newMemSettings() *memSettings {
	return &memSettings{m: make(map[string]string), sets: make(map[string]int)}
}

func (s *memSettings) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memSettings) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	s.sets[key]++
	return nil
}

func (s *memSettings) value(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key]
}

func (s *memSettings) count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets[key]
}

// namingFixture is a library written under the legacy scheme.
type namingFixture struct {
	svc      *Service
	root     string
	catalog  *mockCatalog
	files    *mockFiles
	settings *memSettings
}

func newNamingFixture(t *testing.T) *namingFixture {
	t.Helper()
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	return &namingFixture{svc: svc, root: root, catalog: catalog, files: files, settings: newMemSettings()}
}

// addTrack writes a track at its legacy path (or at rel when given) and
// registers it.
func (f *namingFixture) addTrack(t *testing.T, release music.Release, track music.Track, rel, content string) music.File {
	t.Helper()
	f.catalog.releases[release.ID] = release
	track.ReleaseID = release.ID
	f.catalog.tracks[track.ID] = track
	if rel == "" {
		rel = storage.TrackRelPathFor(storage.NamingSchemeLegacy, release, track, ".opus")
	}
	f.write(t, rel, content)
	file := music.File{ID: "file-" + track.ID, TrackID: track.ID, Path: rel, SourceProvider: "youtube", SourceID: "src-" + track.ID}
	f.files.files[rel] = file
	return file
}

func (f *namingFixture) write(t *testing.T, rel, content string) {
	t.Helper()
	abs := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *namingFixture) read(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.root, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(raw)
}

func (f *namingFixture) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(f.root, rel))
	return err == nil
}

func (f *namingFixture) pathOf(t *testing.T, fileID string) string {
	t.Helper()
	for _, file := range f.files.files {
		if file.ID == fileID {
			return file.Path
		}
	}
	t.Fatalf("file %s is no longer catalogued", fileID)
	return ""
}

func (f *namingFixture) run(t *testing.T) *NamingMigrationStatus {
	t.Helper()
	st, err := f.svc.RunNamingMigration(context.Background(), f.settings)
	if err != nil {
		t.Fatalf("RunNamingMigration: %v", err)
	}
	return st
}

var acdc = music.Release{ID: "rel-acdc", AlbumArtist: "AC/DC", Title: "Who? Live: Part 1", Year: 1980, ReleaseType: music.ReleaseLive}

func TestNamingMigrationMovesTracksSidecarsAndCover(t *testing.T) {
	f := newNamingFixture(t)
	d1 := f.addTrack(t, acdc, music.Track{ID: "t1", Title: "Hells Bells", TrackNumber: 1, DiscNumber: 1, DiscTotal: 2}, "", "audio one")
	d2 := f.addTrack(t, acdc, music.Track{ID: "t2", Title: "Shoot: to Thrill", TrackNumber: 2, DiscNumber: 2, DiscTotal: 2}, "", "audio two")

	oldDir := filepath.Join("AC-DC", "1980 - Who_ Live- Part 1 [Live]")
	newDir := filepath.Join("AC／DC", "1980 - Who？ Live： Part 1 [Live]")
	if filepath.Dir(d1.Path) != oldDir || filepath.Base(d2.Path) != "202 - Shoot- to Thrill.opus" {
		t.Fatalf("fixture paths %q, %q", d1.Path, d2.Path)
	}
	f.write(t, storage.SidecarPathFor(d2.Path, ".lrc"), "[00:01.00] lyrics")
	f.write(t, filepath.Join(oldDir, "cover.jpg"), "jpeg")
	f.write(t, filepath.Join(oldDir, "album.nfo"), "nfo")
	f.write(t, storage.MarkerFileName, "ytmdl-storage:keep")

	st := f.run(t)
	if st.State != NamingStateCompleted || st.Moved != 2 || st.Skipped != 0 {
		t.Fatalf("status %+v", st)
	}
	want1 := filepath.Join(newDir, "1-01 - Hells Bells.opus")
	want2 := filepath.Join(newDir, "2-02 - Shoot： to Thrill.opus")
	if got := f.pathOf(t, d1.ID); got != want1 {
		t.Fatalf("catalog path %q, want %q", got, want1)
	}
	if got := f.pathOf(t, d2.ID); got != want2 {
		t.Fatalf("catalog path %q, want %q", got, want2)
	}
	if f.read(t, want1) != "audio one" || f.read(t, want2) != "audio two" {
		t.Fatal("audio content changed")
	}
	if f.read(t, storage.SidecarPathFor(want2, ".lrc")) != "[00:01.00] lyrics" {
		t.Fatal("lyrics sidecar did not follow")
	}
	if f.read(t, filepath.Join(newDir, "cover.jpg")) != "jpeg" || f.read(t, filepath.Join(newDir, "album.nfo")) != "nfo" {
		t.Fatal("cover or extra file did not follow")
	}
	if f.exists(oldDir) || f.exists("AC-DC") {
		t.Fatal("empty old folders were left behind")
	}
	if !f.exists(storage.MarkerFileName) {
		t.Fatal("a file at the library root was touched")
	}
	if f.settings.value(NamingSchemeSettingKey) != "2" || f.settings.value(NamingMigrationJournalKey) != "" {
		t.Fatalf("marker %q, journal %q", f.settings.value(NamingSchemeSettingKey), f.settings.value(NamingMigrationJournalKey))
	}
	var saved NamingMigrationStatus
	if err := json.Unmarshal([]byte(f.settings.value(NamingMigrationStatusKey)), &saved); err != nil || saved.State != NamingStateCompleted {
		t.Fatalf("persisted status %q (%v)", f.settings.value(NamingMigrationStatusKey), err)
	}
	if st.FoldersRemoved != 2 || st.ExtrasCarried != 2 {
		t.Fatalf("folders removed %d, extras carried %d", st.FoldersRemoved, st.ExtrasCarried)
	}
}

func TestNamingMigrationSkipsExistingTarget(t *testing.T) {
	f := newNamingFixture(t)
	track := music.Track{ID: "t1", Title: "Who?", TrackNumber: 1}
	release := music.Release{ID: "r", AlbumArtist: "Band", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, track, "", "catalogued")
	occupied := storage.TrackRelPathFor(storage.CurrentNamingScheme, release, track, ".opus")
	f.write(t, occupied, "somebody else's file")
	f.write(t, filepath.Join(filepath.Dir(file.Path), "cover.jpg"), "jpeg")

	st := f.run(t)
	if st.State != NamingStateIncomplete || st.Moved != 0 || st.Skipped != 1 || st.Skips[0].Reason != NamingSkipTargetExists {
		t.Fatalf("status %+v", st)
	}
	if f.read(t, file.Path) != "catalogued" || f.read(t, occupied) != "somebody else's file" {
		t.Fatal("a file was moved or overwritten")
	}
	if f.pathOf(t, file.ID) != file.Path {
		t.Fatal("the catalog changed although the file stayed")
	}
	if f.settings.value(NamingSchemeSettingKey) != "" {
		t.Fatal("the marker was written although a file was left behind")
	}

	// The next start tries again and succeeds once the obstacle is gone.
	if err := os.Remove(filepath.Join(f.root, occupied)); err != nil {
		t.Fatal(err)
	}
	if st := f.run(t); st.State != NamingStateCompleted || st.Moved != 1 {
		t.Fatalf("retry status %+v", st)
	}
	if f.pathOf(t, file.ID) != occupied || f.read(t, occupied) != "catalogued" {
		t.Fatal("the retry did not move the file")
	}
}

func TestNamingMigrationChecksumMismatchMovesTheFileBack(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "original bytes")
	f.write(t, storage.SidecarPathFor(file.Path, ".txt"), "plain lyrics")

	// A filesystem without hard links, and a copy that does not read back as
	// the original.
	restoreLink, restoreHash := namingLink, namingHash
	t.Cleanup(func() { namingLink, namingHash = restoreLink, restoreHash })
	namingLink = func(string, string) error { return errors.New("links are not supported") }
	namingHash = func(path string) (string, error) {
		if strings.Contains(path, "AC／DC") {
			return "damaged", nil
		}
		return computeFileSHA256(path)
	}

	st := f.run(t)
	if st.State != NamingStateIncomplete || st.Skipped != 1 || st.Skips[0].Reason != NamingSkipChecksum {
		t.Fatalf("status %+v", st)
	}
	if f.read(t, file.Path) != "original bytes" || f.read(t, storage.SidecarPathFor(file.Path, ".txt")) != "plain lyrics" {
		t.Fatal("the file or its sidecar is not back at its old path")
	}
	if f.exists(filepath.Join("AC／DC", "2000 - Album", "01 - Song.opus")) || f.exists("AC／DC") {
		t.Fatal("a copy or an empty folder was left at the new path")
	}
	if f.pathOf(t, file.ID) != file.Path || f.settings.value(NamingMigrationJournalKey) != "" {
		t.Fatal("catalog or journal disagree with the disk")
	}
}

func TestNamingMigrationCopiesWhereHardLinksAreMissing(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "original bytes")
	f.write(t, storage.SidecarPathFor(file.Path, ".lrc"), "lyrics")

	restoreLink := namingLink
	t.Cleanup(func() { namingLink = restoreLink })
	namingLink = func(string, string) error { return errors.New("links are not supported") }

	if st := f.run(t); st.State != NamingStateCompleted || st.Moved != 1 {
		t.Fatalf("status %+v", st)
	}
	want := filepath.Join("AC／DC", "2000 - Album", "01 - Song.opus")
	if f.read(t, want) != "original bytes" || f.read(t, storage.SidecarPathFor(want, ".lrc")) != "lyrics" || f.exists(file.Path) {
		t.Fatal("the copied move is incomplete")
	}
}

func TestNamingMigrationWaitsForUnreachableLibrary(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "audio")
	// A guard is configured but its marker is missing: the share is not
	// mounted, or a different one is.
	f.svc.library.SetGuard(storage.NewStorageGuard(f.root, "library-1", 0))

	_, err := f.svc.RunNamingMigration(context.Background(), f.settings)
	if !errors.Is(err, errNamingStorageUnavailable) {
		t.Fatalf("err = %v, want the storage to be reported unavailable", err)
	}
	if st := f.svc.NamingMigrationStatus(); st.State != NamingStateWaitingStorage {
		t.Fatalf("state %q", st.State)
	}
	if f.read(t, file.Path) != "audio" || f.pathOf(t, file.ID) != file.Path || f.settings.value(NamingSchemeSettingKey) != "" {
		t.Fatal("something changed while the library was unavailable")
	}

	f.write(t, storage.MarkerFileName, "ytmdl-storage:library-1")
	if st := f.run(t); st.State != NamingStateCompleted || st.Moved != 1 {
		t.Fatalf("status after the share came back %+v", st)
	}
}

func TestNamingMigrationTreatsAnEmptyMountAsUnreachable(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "audio")
	// The mount point is there but empty.
	if err := os.RemoveAll(filepath.Join(f.root, "AC-DC")); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.RunNamingMigration(context.Background(), f.settings)
	if !errors.Is(err, errNamingStorageUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if f.pathOf(t, file.ID) != file.Path || f.settings.value(NamingSchemeSettingKey) != "" {
		t.Fatal("the catalog or the marker changed")
	}
}

func TestNamingMigrationResumesAnInterruptedRun(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	var moved []music.File
	for i, title := range []string{"One", "Two", "Three"} {
		moved = append(moved, f.addTrack(t, release, music.Track{ID: "t" + title, Title: title + "?", TrackNumber: i + 1}, "", "audio "+title))
	}
	f.write(t, storage.SidecarPathFor(moved[2].Path, ".lrc"), "lyrics three")

	// The process stops after the first move: the context ends.
	ctx, cancel := context.WithCancel(context.Background())
	restoreHash := namingHash
	t.Cleanup(func() { namingHash = restoreHash })
	calls := 0
	namingHash = func(path string) (string, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return computeFileSHA256(path)
	}
	if _, err := f.svc.RunNamingMigration(ctx, f.settings); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the run to stop", err)
	}
	namingHash = restoreHash
	if f.settings.value(NamingSchemeSettingKey) != "" {
		t.Fatal("an interrupted run wrote the marker")
	}
	// Every file is at its old or its new path, and the catalog agrees.
	for _, m := range moved {
		path := f.pathOf(t, m.ID)
		if f.read(t, path) == "" {
			t.Fatalf("%s is not where the catalog says", path)
		}
	}

	// A crash inside the third move: audio already at the new path, sidecar
	// still at the old one, catalog and journal from before the move.
	third := moved[2]
	newThird := storage.TrackRelPathFor(storage.CurrentNamingScheme, release, f.catalog.tracks["tThree"], ".opus")
	sum, _ := computeFileSHA256(filepath.Join(f.root, third.Path))
	raw, _ := json.Marshal(namingJournal{FileID: third.ID, TrackID: third.TrackID, From: third.Path, To: newThird, SHA256: sum})
	_ = f.settings.Set(context.Background(), NamingMigrationJournalKey, string(raw))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(f.root, newThird)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.root, third.Path), filepath.Join(f.root, newThird)); err != nil {
		t.Fatal(err)
	}

	st := f.run(t)
	if st.State != NamingStateCompleted || st.Skipped != 0 {
		t.Fatalf("status after resume %+v", st)
	}
	for _, m := range moved {
		track := f.catalog.tracks[m.TrackID]
		want := storage.TrackRelPathFor(storage.CurrentNamingScheme, release, track, ".opus")
		if got := f.pathOf(t, m.ID); got != want {
			t.Fatalf("%s: catalog %q, want %q", m.ID, got, want)
		}
		if !strings.HasPrefix(f.read(t, want), "audio ") {
			t.Fatalf("%s is missing", want)
		}
	}
	if f.read(t, storage.SidecarPathFor(newThird, ".lrc")) != "lyrics three" {
		t.Fatal("the sidecar of the interrupted move did not follow")
	}
	if f.exists("AC-DC") {
		t.Fatal("the old artist folder is left")
	}
}

func TestNamingMigrationResumesAMoveCaughtBetweenLinkAndRemove(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "Band", Title: "Album", Year: 2000}
	track := music.Track{ID: "t1", Title: "Why?", TrackNumber: 1}
	file := f.addTrack(t, release, track, "", "audio")
	target := storage.TrackRelPathFor(storage.CurrentNamingScheme, release, track, ".opus")
	if err := os.Link(filepath.Join(f.root, file.Path), filepath.Join(f.root, target)); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	sum, _ := computeFileSHA256(filepath.Join(f.root, file.Path))
	raw, _ := json.Marshal(namingJournal{FileID: file.ID, TrackID: file.TrackID, From: file.Path, To: target, SHA256: sum})
	_ = f.settings.Set(context.Background(), NamingMigrationJournalKey, string(raw))

	if st := f.run(t); st.State != NamingStateCompleted {
		t.Fatalf("status %+v", st)
	}
	if f.exists(file.Path) || f.read(t, target) != "audio" || f.pathOf(t, file.ID) != target {
		t.Fatal("the half-finished move was not completed")
	}
}

func TestNamingMigrationResolvesCollisions(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "Band", Title: "Album", Year: 2000}

	// Distinct under the old scheme, the same name under the new one:
	// "Who?" was "Who_", and a title already spelled "Who？" kept its name.
	question := f.addTrack(t, release, music.Track{ID: "q", Title: "Who?", TrackNumber: 1}, "", "question mark")
	fullwidth := f.addTrack(t, release, music.Track{ID: "fw", Title: "Who？", TrackNumber: 1}, "", "fullwidth")

	// The same name under the old scheme, distinct under the new one: "A:B"
	// and "A-B" were both "A-B"; the second download got a source suffix.
	colon := f.addTrack(t, release, music.Track{ID: "c", Title: "A:B", TrackNumber: 2}, "", "colon")
	hyphenTrack := music.Track{ID: "h", Title: "A-B", TrackNumber: 2}
	suffixed := filepath.Join(filepath.Dir(colon.Path),
		storage.WithSourceSuffix(filepath.Base(colon.Path), storage.SourceSuffix("youtube", "src-h")))
	hyphen := f.addTrack(t, release, hyphenTrack, suffixed, "hyphen")

	st := f.run(t)
	if st.State != NamingStateCompleted || st.Skipped != 0 {
		t.Fatalf("status %+v", st)
	}
	dir := filepath.Join("Band", "2000 - Album")
	if got := f.pathOf(t, fullwidth.ID); got != fullwidth.Path {
		t.Fatalf("a file already on the new scheme moved to %q", got)
	}
	wantQuestion := filepath.Join(dir, storage.WithSourceSuffix("01 - Who？.opus", storage.SourceSuffix("youtube", "src-q")))
	if got := f.pathOf(t, question.ID); got != wantQuestion || f.read(t, wantQuestion) != "question mark" {
		t.Fatalf("colliding file at %q, want %q", got, wantQuestion)
	}
	if f.read(t, fullwidth.Path) != "fullwidth" {
		t.Fatal("the file holding the name was touched")
	}
	wantColon := filepath.Join(dir, "02 - A：B.opus")
	wantHyphen := filepath.Join(dir, "02 - A-B.opus")
	if f.pathOf(t, colon.ID) != wantColon || f.read(t, wantColon) != "colon" {
		t.Fatalf("colon track at %q", f.pathOf(t, colon.ID))
	}
	if f.pathOf(t, hyphen.ID) != wantHyphen || f.read(t, wantHyphen) != "hyphen" {
		t.Fatalf("hyphen track at %q, want its plain name back", f.pathOf(t, hyphen.ID))
	}
}

func TestNamingMigrationSwitchedOff(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "audio")

	f.svc.StartNamingMigration(NamingMigrationOptions{Enabled: false, Settings: f.settings})
	time.Sleep(50 * time.Millisecond)
	if st := f.svc.NamingMigrationStatus(); st == nil || st.State != NamingStateDisabled {
		t.Fatalf("status %+v", st)
	}
	if f.pathOf(t, file.ID) != file.Path || f.read(t, file.Path) != "audio" || f.settings.value(NamingSchemeSettingKey) != "" {
		t.Fatal("the switched-off migration changed something")
	}
}

func TestNamingMigrationStartsInTheBackgroundAndRunsOnce(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "audio")
	want := filepath.Join("AC／DC", "2000 - Album", "01 - Song.opus")

	f.svc.StartNamingMigration(NamingMigrationOptions{Enabled: true, Settings: f.settings})
	deadline := time.Now().Add(5 * time.Second)
	for f.svc.NamingMigrationStatus().State != NamingStateCompleted {
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", f.svc.NamingMigrationStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.pathOf(t, file.ID) != want {
		t.Fatal("the background run did not move the file")
	}

	// A second start is a no-op: a file that sits at an old-scheme path now
	// (restored by hand, say) is left to the integrity audit.
	other := f.addTrack(t, release, music.Track{ID: "t2", Title: "Other", TrackNumber: 2}, "", "audio 2")
	statusWrites := f.settings.count(NamingMigrationStatusKey)
	st := f.run(t)
	if st.State != NamingStateCompleted || f.pathOf(t, other.ID) != other.Path {
		t.Fatalf("second run status %+v moved %q", st, f.pathOf(t, other.ID))
	}
	if f.settings.count(NamingMigrationStatusKey) != statusWrites {
		t.Fatal("the second run wrote its status again")
	}
}

func TestNamingMigrationWaitsForADownloadInTheSameFolder(t *testing.T) {
	f := newNamingFixture(t)
	release := music.Release{ID: "r", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	file := f.addTrack(t, release, music.Track{ID: "t1", Title: "Song", TrackNumber: 1}, "", "audio")

	// A download is finalizing into the new release folder.
	unlock := f.svc.library.ShareReleaseDir(storage.ReleaseDirRel(release))
	done := make(chan *NamingMigrationStatus, 1)
	go func() {
		st, _ := f.svc.RunNamingMigration(context.Background(), f.settings)
		done <- st
	}()
	select {
	case <-done:
		t.Fatal("the migration moved files while a download held the folder")
	case <-time.After(100 * time.Millisecond):
	}
	if !f.exists(file.Path) {
		t.Fatal("the file moved while the folder was held")
	}
	unlock()
	select {
	case st := <-done:
		if st.State != NamingStateCompleted {
			t.Fatalf("status %+v", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the migration never continued")
	}
}

func TestNamingMigrationCopiesCoversWhenAFolderSplits(t *testing.T) {
	f := newNamingFixture(t)
	// "AC/DC" and "AC-DC" shared one folder under the old scheme.
	slash := music.Release{ID: "r1", AlbumArtist: "AC/DC", Title: "Album", Year: 2000}
	hyphen := music.Release{ID: "r2", AlbumArtist: "AC-DC", Title: "Album", Year: 2000}
	a := f.addTrack(t, slash, music.Track{ID: "a", Title: "Song A", TrackNumber: 1}, "", "a")
	b := f.addTrack(t, hyphen, music.Track{ID: "b", Title: "Song B", TrackNumber: 2}, "", "b")
	oldDir := filepath.Dir(a.Path)
	if filepath.Dir(b.Path) != oldDir {
		t.Fatal("fixture releases do not share a folder")
	}
	f.write(t, filepath.Join(oldDir, "cover.jpg"), "jpeg")

	st := f.run(t)
	if st.Moved != 1 || f.pathOf(t, b.ID) != b.Path {
		t.Fatalf("status %+v", st)
	}
	if f.read(t, filepath.Join("AC／DC", "2000 - Album", "cover.jpg")) != "jpeg" || f.read(t, filepath.Join(oldDir, "cover.jpg")) != "jpeg" {
		t.Fatal("the cover was not copied to the new folder or left the old one")
	}
}
