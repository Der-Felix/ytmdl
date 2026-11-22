package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// fakePlacements is the placement store of the copy tests.
type fakePlacements struct {
	home       *music.Release
	placed     []music.Placement
	persisted  []music.PlacementEntry
	persistErr error
}

func (p *fakePlacements) HomeRelease(context.Context, string) (*music.Release, error) {
	return p.home, nil
}

func (p *fakePlacements) ListByTrack(_ context.Context, trackID string) ([]music.Placement, error) {
	var out []music.Placement
	for _, placement := range p.placed {
		if placement.TrackID == trackID {
			out = append(out, placement)
		}
	}
	return out, nil
}

func (p *fakePlacements) FindByPath(_ context.Context, path string) (*music.Placement, error) {
	for _, placement := range p.placed {
		if placement.Path == path {
			found := placement
			return &found, nil
		}
	}
	return nil, nil
}

func (p *fakePlacements) Persist(_ context.Context, entry music.PlacementEntry) (music.Placement, error) {
	if p.persistErr != nil {
		return music.Placement{}, p.persistErr
	}
	p.persisted = append(p.persisted, entry)
	placement := entry.Placement
	placement.ID = "placement-1"
	placement.ReleaseProvider = entry.Release.Provider
	placement.ReleaseSourceID = entry.Release.SourceID
	p.placed = append(p.placed, placement)
	return placement, nil
}

// albumTagger stands in for the real tagger: it marks the file with the album
// it was tagged for, which also changes its checksum the way retagging does.
type albumTagger struct{ paths []string }

func (t *albumTagger) Apply(_ context.Context, path string, tags metadata.Tags, _ *metadata.Artwork) error {
	t.paths = append(t.paths, path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("|tagged:" + tags.Album)
	return err
}

const (
	homeRel   = "ABBA/1976 - Arrival/02 - Dancing Queen.opus"
	homeAudio = "home audio"
)

var boxSetRel = filepath.Join("ABBA", "1994 - Thank You For The Music [Compilation]", "1-07 - Dancing Queen.opus")

type copyHarness struct {
	manager    *Manager
	root       string
	store      *fakeStore
	placements *fakePlacements
	tagger     *albumTagger
}

// newCopyHarness holds "Dancing Queen" at home on "Arrival", with its file on
// disk, and a job item that asks for the recording on the box set "Thank You
// For The Music" as disc 1, track 7.
func newCopyHarness(t *testing.T) *copyHarness {
	t.Helper()
	home := filepath.FromSlash(homeRel)
	files := &fakeFiles{byTrack: []music.File{{
		ID: "file-1", TrackID: "known", Path: home, DurationMS: 230000,
		SourceProvider: "youtube", SourceID: "vid-1", SourceURL: "https://youtu.be/vid-1",
	}}}
	m, root := newPlaceManager(t, &fakeCatalog{known: &music.Track{ID: "known"}}, files)
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, home), []byte(homeAudio), 0o644); err != nil {
		t.Fatal(err)
	}

	placements := &fakePlacements{home: &music.Release{
		ID: "release-arrival", Title: "Arrival", AlbumArtist: "ABBA", Artists: []string{"ABBA"},
		ReleaseType: music.ReleaseAlbum, Year: 1976, Provider: "deezer", SourceID: "rel-arrival",
	}}
	tagger := &albumTagger{}
	store := &fakeStore{items: map[string]Item{}}
	m.store = store
	m.broker = NewBroker(m.logger)
	m.workers = NewWorkerTracker()
	m.finalizerSem = make(chan struct{}, 1)
	m.tagger = tagger
	m.placements = placements
	m.completeReleases.Store(true)
	return &copyHarness{manager: m, root: root, store: store, placements: placements, tagger: tagger}
}

func boxSetTrack() music.Track {
	return music.Track{
		Title: "Dancing Queen", Artists: []string{"ABBA"}, AlbumArtist: "ABBA",
		Album: "Thank You For The Music", ReleaseType: music.ReleaseCompilation, Year: 1994,
		DiscNumber: 1, DiscTotal: 4, TrackNumber: 7, TrackTotal: 18, DurationMS: 231000,
		SourceProvider: "deezer", SourceID: "tr-dq-box", ReleaseID: "rel-box",
	}
}

// run processes one item for track the way a release job with skip-existing
// does, and returns the stored item.
func (h *copyHarness) run(t *testing.T, track music.Track) Item {
	t.Helper()
	item := Item{ID: "item-1", JobID: "job-1", Track: track, Label: track.Label(), MaxAttempts: 3}
	h.store.items[item.ID] = item
	job := Job{ID: "job-1", Options: Options{SkipExisting: true}}
	(&worker{manager: h.manager}).process(context.Background(), job, item)
	return h.store.items[item.ID]
}

func (h *copyHarness) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func (h *copyHarness) assertNoCopy(t *testing.T) {
	t.Helper()
	if len(h.tagger.paths) != 0 || len(h.placements.persisted) != 0 {
		t.Fatalf("a copy was made: tagged %v, persisted %d", h.tagger.paths, len(h.placements.persisted))
	}
	if _, err := os.Stat(filepath.Join(h.root, filepath.Dir(boxSetRel))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the box set folder was created: %v", err)
	}
}

func assertSkipped(t *testing.T, item Item) {
	t.Helper()
	if item.Status != ItemSkipped || item.ErrorCode != string(apperr.CodeAlreadyExists) {
		t.Fatalf("item = %s / %s, want skipped with ALREADY_EXISTS", item.Status, item.ErrorCode)
	}
}

func TestExistingRecordingOnItsHomeReleaseIsSkipped(t *testing.T) {
	h := newCopyHarness(t)
	// The same release by provider identity. Without a year it renders to a
	// different folder name, so it is the identity that decides here.
	track := boxSetTrack()
	track.Album, track.ReleaseType, track.Year, track.ReleaseID = "Arrival", music.ReleaseAlbum, 0, "rel-arrival"
	track.DiscTotal, track.DiscNumber, track.TrackNumber = 1, 1, 2

	assertSkipped(t, h.run(t, track))
	h.assertNoCopy(t)
}

func TestExistingRecordingInTheFolderOfTheRequestedReleaseIsSkipped(t *testing.T) {
	h := newCopyHarness(t)
	// Another provider's listing of the same album: the identity differs, the
	// folder does not, and a second copy beside the home file is never made.
	track := boxSetTrack()
	track.Album, track.ReleaseType, track.Year, track.ReleaseID = "Arrival", music.ReleaseAlbum, 1976, "spotify-arrival"
	track.SourceProvider = "spotify"

	assertSkipped(t, h.run(t, track))
	h.assertNoCopy(t)
}

func TestExistingRecordingIsCopiedOntoAFurtherRelease(t *testing.T) {
	h := newCopyHarness(t)
	home := filepath.Join(h.root, filepath.FromSlash(homeRel))
	if _, err := h.manager.library.WriteLyrics(home, music.Lyrics{Synced: true, LRC: "[00:01.00]You can dance"}); err != nil {
		t.Fatal(err)
	}
	lyrics := &fakeLyrics{}
	h.manager.lyrics = lyrics

	item := h.run(t, boxSetTrack())

	if item.Status != ItemCompleted || item.ErrorCode != "" {
		t.Fatalf("item = %s / %s (%s), want completed", item.Status, item.ErrorCode, item.ErrorMessage)
	}
	if item.TrackID != "known" || item.MediaProvider != "youtube" || item.MediaID != "vid-1" || item.FileID != "" {
		t.Fatalf("item records %+v", item)
	}
	if got := h.read(t, boxSetRel); got != homeAudio+"|tagged:Thank You For The Music" {
		t.Fatalf("copy = %q, want the home audio tagged for the box set", got)
	}
	if got := h.read(t, filepath.FromSlash(homeRel)); got != homeAudio {
		t.Fatalf("the source file changed: %q", got)
	}
	if lrc, err := os.ReadFile(storage.SidecarPathFor(filepath.Join(h.root, boxSetRel), ".lrc")); err != nil || string(lrc) != "[00:01.00]You can dance" {
		t.Fatalf("lyrics sidecar of the copy = %q, %v", lrc, err)
	}
	if lyrics.calls != 0 {
		t.Fatal("a lyrics provider was asked for a copy")
	}

	if len(h.placements.persisted) != 1 {
		t.Fatalf("persisted %d placements, want 1", len(h.placements.persisted))
	}
	entry := h.placements.persisted[0]
	p := entry.Placement
	if p.TrackID != "known" || p.Path != boxSetRel || p.SourceFileID != "file-1" ||
		p.DiscNumber != 1 || p.TrackNumber != 7 || p.Title != "Dancing Queen" || p.SHA256 == "" || p.SizeBytes == 0 {
		t.Fatalf("placement = %+v", p)
	}
	if entry.Release.SourceID != "rel-box" || entry.Release.Provider != "deezer" || entry.Artist == nil {
		t.Fatalf("placement release = %+v, artist %+v", entry.Release, entry.Artist)
	}
	if len(h.tagger.paths) != 1 || strings.HasPrefix(h.tagger.paths[0], h.root) {
		t.Fatalf("tagged %v; want one scratch copy outside the library", h.tagger.paths)
	}

	// The next item for the same release finds the placement and skips.
	h.tagger.paths = nil
	assertSkipped(t, h.run(t, boxSetTrack()))
	if len(h.tagger.paths) != 0 || len(h.placements.persisted) != 1 {
		t.Fatal("a placement whose file is on disk was copied again")
	}
}

func TestPlacementWhoseFileIsOnDiskIsSkipped(t *testing.T) {
	h := newCopyHarness(t)
	elsewhere := filepath.Join("ABBA", "1994 - Thank You For The Music [Compilation]", "07 - Dancing Queen.opus")
	if err := os.MkdirAll(filepath.Join(h.root, filepath.Dir(elsewhere)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, elsewhere), []byte("placed earlier"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.placements.placed = []music.Placement{{
		ID: "p-old", TrackID: "known", Path: elsewhere,
		ReleaseProvider: "deezer", ReleaseSourceID: "rel-box",
	}}

	assertSkipped(t, h.run(t, boxSetTrack()))
	if len(h.tagger.paths) != 0 || len(h.placements.persisted) != 0 {
		t.Fatal("an existing placement was copied again")
	}
}

func TestPlacementWhoseFileIsGoneIsPlacedAgain(t *testing.T) {
	h := newCopyHarness(t)
	h.placements.placed = []music.Placement{{
		ID: "p-old", TrackID: "known", Path: boxSetRel,
		ReleaseProvider: "deezer", ReleaseSourceID: "rel-box",
	}}

	if item := h.run(t, boxSetTrack()); item.Status != ItemCompleted {
		t.Fatalf("item = %s / %s, want completed", item.Status, item.ErrorCode)
	}
	if got := h.read(t, boxSetRel); !strings.HasPrefix(got, homeAudio) {
		t.Fatalf("copy = %q", got)
	}
}

func TestCopiesAreNotMadeWhenCompletingReleasesIsOff(t *testing.T) {
	h := newCopyHarness(t)
	h.manager.SetCompleteReleases(false)

	assertSkipped(t, h.run(t, boxSetTrack()))
	h.assertNoCopy(t)
}

func TestACopyNeverOverwritesAForeignFile(t *testing.T) {
	h := newCopyHarness(t)
	target := filepath.Join(h.root, boxSetRel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("somebody else's file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if item := h.run(t, boxSetTrack()); item.Status != ItemCompleted {
		t.Fatalf("item = %s / %s (%s), want completed", item.Status, item.ErrorCode, item.ErrorMessage)
	}
	if got := h.read(t, boxSetRel); got != "somebody else's file" {
		t.Fatalf("the foreign file was overwritten: %q", got)
	}
	want := filepath.Join(filepath.Dir(boxSetRel),
		storage.WithSourceSuffix(filepath.Base(boxSetRel), storage.SourceSuffix("youtube", "vid-1")))
	if len(h.placements.persisted) != 1 || h.placements.persisted[0].Placement.Path != want {
		t.Fatalf("placements = %+v, want the copy at %s", h.placements.persisted, want)
	}
	if got := h.read(t, want); !strings.HasPrefix(got, homeAudio) {
		t.Fatalf("suffixed copy = %q", got)
	}
}

func TestACopyIsRemovedWhenItsPlacementCannotBeStored(t *testing.T) {
	h := newCopyHarness(t)
	h.placements.persistErr = apperr.New(apperr.CodeInternal, "database down")

	item := h.run(t, boxSetTrack())

	if item.Status == ItemCompleted || item.Status == ItemSkipped {
		t.Fatalf("item = %s, want it to fail or retry", item.Status)
	}
	if _, err := os.Stat(filepath.Join(h.root, boxSetRel)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the uncatalogued copy stayed in the library: %v", err)
	}
	if _, err := os.Stat(storage.SidecarPathFor(filepath.Join(h.root, boxSetRel), ".lrc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a sidecar of the uncatalogued copy stayed: %v", err)
	}
	if got := h.read(t, filepath.FromSlash(homeRel)); got != homeAudio {
		t.Fatalf("the source file changed: %q", got)
	}
}

func TestACopyWaitsForAnUnreachableLibrary(t *testing.T) {
	h := newCopyHarness(t)
	// A guard that expects an identity file the library does not have.
	h.manager.library.SetGuard(storage.NewStorageGuard(h.root, "expected-guard-id", 0))

	item := h.run(t, boxSetTrack())

	if item.Status != ItemWaitingStorage {
		t.Fatalf("item = %s / %s, want waiting_storage", item.Status, item.ErrorCode)
	}
	if item.Attempts != 0 {
		t.Fatalf("waiting for storage spent an attempt: %d", item.Attempts)
	}
	h.assertNoCopy(t)
}

// A recording can have several files. The copy is made from the first usable
// one, but a later one that already lives in the requested release's folder
// holds the recording there: no second file may appear beside it.
func TestExistingRecordingWithAnotherFileInTheReleaseFolderIsSkipped(t *testing.T) {
	h := newCopyHarness(t)
	files := h.manager.files.(*fakeFiles)
	second := filepath.Join(filepath.Dir(boxSetRel), "1-07 - Dancing Queen.opus")
	files.byTrack = append(files.byTrack, music.File{
		ID: "file-2", TrackID: "known", Path: second, DurationMS: 230000,
		SourceProvider: "youtube", SourceID: "vid-2",
	})
	if err := os.MkdirAll(filepath.Join(h.root, filepath.Dir(second)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, second), []byte("box set audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	assertSkipped(t, h.run(t, boxSetTrack()))
	if len(h.tagger.paths) != 0 || len(h.placements.persisted) != 0 {
		t.Fatalf("a copy was made: tagged %v, persisted %d", h.tagger.paths, len(h.placements.persisted))
	}
	entries, err := os.ReadDir(filepath.Join(h.root, filepath.Dir(second)))
	if err != nil || len(entries) != 1 {
		t.Fatalf("release folder holds %d entries (%v), want the one file", len(entries), err)
	}
	if got := h.read(t, second); got != "box set audio" {
		t.Fatalf("the file in the release folder changed: %q", got)
	}
}
