package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// The tests in this file follow one track through a library that is away
// when its download finishes and comes back later, with a service restart in
// between: a new Manager over the same database, staging root and library.

const stagedGuardID = "staged-library"

// stagedStore is the database of the scenario. It survives a restart; the
// Manager built on top of it does not.
type stagedStore struct {
	*fakeStore
	job Job
}

func (s *stagedStore) Get(context.Context, string) (*Job, error) {
	job := s.job
	return &job, nil
}

func (s *stagedStore) ListUnfinished(context.Context) ([]Job, error) {
	if s.job.Status.Terminal() {
		return nil, nil
	}
	return []Job{s.job}, nil
}

func (s *stagedStore) HasItems(context.Context, string) (bool, error) {
	return len(s.items) > 0, nil
}

func (s *stagedStore) ListItems(_ context.Context, jobID string) ([]Item, error) {
	var out []Item
	for _, it := range s.items {
		if it.JobID == jobID {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

func (s *stagedStore) ItemStatuses(_ context.Context, ids []string) (map[string]ItemStatus, error) {
	out := map[string]ItemStatus{}
	for _, id := range ids {
		if it, ok := s.items[id]; ok {
			out[id] = it.Status
		}
	}
	return out, nil
}

func (s *stagedStore) ResetInFlightItems(context.Context) (int, error) {
	var n int
	for id, it := range s.items {
		switch it.Status {
		case ItemMatching, ItemDownloading, ItemTagging, ItemFinalizing:
			it.Status = ItemPending
			s.items[id] = it
			n++
		}
	}
	return n, nil
}

func (s *stagedStore) ResetInterruptedJobs(context.Context) (int, error) {
	if s.job.Status.Terminal() {
		return 0, nil
	}
	s.job.Status = StatusQueued
	return 1, nil
}

func (s *stagedStore) SetStatus(_ context.Context, _ string, status Status, code, message string) error {
	s.job.Status, s.job.ErrorCode, s.job.ErrorMessage = status, code, message
	return nil
}

func (s *stagedStore) CancelPendingItems(context.Context, string) (int, error) {
	var n int
	for id, it := range s.items {
		if !it.Status.Terminal() {
			it.Status = ItemCancelled
			s.items[id] = it
			n++
		}
	}
	return n, nil
}

func (s *stagedStore) RefreshCounters(ctx context.Context, id string) (*Job, error) {
	return s.Get(ctx, id)
}

// countingDownloader stands for yt-dlp: every call is a provider transfer.
type countingDownloader struct {
	calls atomic.Int32
	audio []byte
}

func (d *countingDownloader) Download(_ context.Context, _ provider.MediaSource, destination string, _ downloader.ProgressCallback) (*downloader.Result, error) {
	d.calls.Add(1)
	if err := os.WriteFile(destination, d.audio, 0o644); err != nil {
		return nil, err
	}
	return &downloader.Result{
		Path: destination,
		Info: downloader.AudioInfo{
			Codec: "opus", Container: "ogg", BitrateKbps: 141.5, SampleRate: 48000,
			Channels: 2, DurationMS: 349000, SizeBytes: int64(len(d.audio)),
		},
		SourceCodec: "opus",
		NativeOpus:  true,
		FormatKind:  "audio_only",
	}, nil
}

// countingTagger writes something into the file, as the real tagger does, so
// that a second tagging run would change the bytes.
type countingTagger struct{ calls atomic.Int32 }

func (t *countingTagger) Apply(_ context.Context, path string, _ metadata.Tags, _ *metadata.Artwork) error {
	t.calls.Add(1)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("|tags")
	return err
}

type countingArtwork struct{ calls atomic.Int32 }

func (a *countingArtwork) Fetch(context.Context, string) (*metadata.Artwork, error) {
	a.calls.Add(1)
	return &metadata.Artwork{Data: []byte("jpeg-cover"), MIME: "image/jpeg", Width: 600, Height: 600}, nil
}

type countingLyrics struct {
	calls atomic.Int32
	none  bool
}

func (l *countingLyrics) Resolve(context.Context, music.Track, string) (*music.Lyrics, error) {
	l.calls.Add(1)
	if l.none {
		return nil, lyrics.ErrNoLyrics
	}
	return &music.Lyrics{Provider: "lrclib", PlainText: "One of us is crying"}, nil
}

// recordingCatalog keeps what would have become database rows.
type recordingCatalog struct {
	fakeCatalog
	mu      sync.Mutex
	entries []music.LibraryEntry
}

func (c *recordingCatalog) PersistDownload(_ context.Context, entry music.LibraryEntry, _ int) (music.StoredEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
	file := entry.File
	file.ID = "file-1"
	return music.StoredEntry{ArtistID: "artist-1", ReleaseID: "release-1", TrackID: "track-1", File: file}, nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stagedEnv is everything that outlives a restart.
type stagedEnv struct {
	libRoot     string
	stagingRoot string
	store       *stagedStore
	itemID      string
	prov        *mockFallbackMediaProvider
	dl          *countingDownloader
	tagger      *countingTagger
	art         *countingArtwork
	lyrics      *countingLyrics
	catalog     *recordingCatalog
	logs        *syncBuffer
}

func newStagedEnv(t *testing.T) *stagedEnv {
	t.Helper()
	itemID := music.NewID()
	env := &stagedEnv{
		libRoot:     t.TempDir(),
		stagingRoot: t.TempDir(),
		itemID:      itemID,
		prov:        newMockFallbackMediaProvider("youtube", timeoutCandidates()),
		dl:          &countingDownloader{audio: []byte("opus-audio")},
		tagger:      &countingTagger{},
		art:         &countingArtwork{},
		lyrics:      &countingLyrics{},
		catalog:     &recordingCatalog{},
		logs:        &syncBuffer{},
	}
	env.store = &stagedStore{
		fakeStore: &fakeStore{items: map[string]Item{
			itemID: {
				ID: itemID, JobID: "job-1", Status: ItemPending, MaxAttempts: 5,
				Track: music.Track{
					Title: "The Visitors", Artists: []string{"ABBA"}, Album: "The Visitors",
					AlbumArtist: "ABBA", TrackNumber: 1, DurationMS: 349000,
					CoverURL: "https://covers.invalid/the-visitors.jpg",
				},
				Label: "ABBA - The Visitors",
			},
		}},
		job: Job{ID: "job-1", Type: TypeTrack, Status: StatusDownloading, MediaProvider: "youtube", Total: 1},
	}
	env.setLibraryOnline(t, true)
	return env
}

// setLibraryOnline simulates a network share coming and going: an unmounted
// share leaves an empty mount point without the storage marker behind.
func (e *stagedEnv) setLibraryOnline(t *testing.T, online bool) {
	t.Helper()
	marker := filepath.Join(e.libRoot, storage.MarkerFileName)
	if online {
		if err := os.WriteFile(marker, []byte(storage.MarkerPrefix+stagedGuardID), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// start builds the Manager one service process would run, and recovers the
// stored work the way Start does before any worker runs.
func (e *stagedEnv) start(t *testing.T) *Manager {
	t.Helper()
	library, err := storage.NewLibrary(e.libRoot)
	if err != nil {
		t.Fatal(err)
	}
	library.SetGuard(storage.NewStorageGuard(e.libRoot, stagedGuardID, 0))
	staging, err := storage.NewStagingManager(e.stagingRoot, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.RegisterMedia(e.prov)
	logger := slog.New(slog.NewTextHandler(e.logs, nil))
	m := &Manager{
		store:        e.store,
		catalog:      e.catalog,
		files:        &fakeFiles{},
		library:      library,
		staging:      staging,
		registry:     reg,
		matcher:      matcher.New(matcher.Options{MinScore: 70, DurationToleranceMS: 4000}),
		downloader:   e.dl,
		tagger:       e.tagger,
		artwork:      e.art,
		lyrics:       e.lyrics,
		cooldown:     NewMediaCooldownManager(),
		broker:       NewBroker(logger),
		logger:       logger,
		finalizerSem: make(chan struct{}, 1),
		resolveQueue: make(chan string, resolveQueueSize),
		wake:         make(chan struct{}, 1),
		concurrency:  1,
		trackTimeout: time.Minute,
	}
	m.lyricsEnabled.Store(true)
	m.writeCoverFile.Store(true)
	m.embedCover.Store(true)
	m.allowOfflineStaging.Store(true)
	m.ctx, m.stop = context.WithCancel(context.Background())
	t.Cleanup(m.stop)

	if err := m.recover(m.ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	m.pruneStaging(m.ctx)
	return m
}

func (e *stagedEnv) item() Item { return e.store.items[e.itemID] }

// ready reports whether the dispatcher would hand the item to a worker now.
func (e *stagedEnv) ready(m *Manager) (Job, Item, bool) {
	for _, c := range m.collectCandidates(context.Background()) {
		for _, it := range c.ready {
			if it.ID == e.itemID {
				return c.job, it, true
			}
		}
	}
	return Job{}, Item{}, false
}

func (e *stagedEnv) stagingDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(e.stagingRoot, e.itemID)
}

// expectedTarget is where a normal download of the item lands.
func (e *stagedEnv) expectedTarget(t *testing.T, m *Manager) string {
	t.Helper()
	track := e.item().Track
	target, err := m.library.TrackPath(releaseOf(track), track, ".opus")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// processWithEvents runs one worker attempt and returns the item states it
// announced.
func processWithEvents(m *Manager, job Job, item Item) []ItemStatus {
	events, unsubscribe := m.broker.Subscribe()
	(&worker{manager: m}).process(context.Background(), job, item)
	unsubscribe()
	var states []ItemStatus
	for ev := range events {
		if ev.Type == EventItemStatus && ev.ItemID == item.ID {
			states = append(states, ev.ItemStatus)
		}
	}
	return states
}

// stageWhileOffline is the first process: the download finishes while the
// library is away.
func (e *stagedEnv) stageWhileOffline(t *testing.T) {
	t.Helper()
	e.setLibraryOnline(t, false)
	m := e.start(t)
	job, item, ok := e.ready(m)
	if !ok {
		t.Fatal("with offline staging the pending item must be dispatched")
	}
	(&worker{manager: m}).process(context.Background(), job, item)

	got := e.item()
	if got.Status != ItemWaitingStorage {
		t.Fatalf("status after staging = %v (%s), want %v", got.Status, got.ErrorMessage, ItemWaitingStorage)
	}
	if got.StagedSHA256 == "" {
		t.Fatal("the staged checksum was not recorded on the item")
	}
	if _, err := os.Stat(filepath.Join(e.stagingDir(t), finaliseRecordName)); err != nil {
		t.Fatalf("no finalisation record was written: %v", err)
	}
	m.stop()
}

func TestStagedDownloadIsFinalisedAfterARestartWithoutDownloadingAgain(t *testing.T) {
	env := newStagedEnv(t)
	env.stageWhileOffline(t)
	if env.dl.calls.Load() != 1 || env.tagger.calls.Load() != 1 {
		t.Fatalf("downloads = %d, taggings = %d after staging, want 1 / 1", env.dl.calls.Load(), env.tagger.calls.Load())
	}
	if len(env.catalog.entries) != 0 {
		t.Fatal("a catalogue row was written while the library was away")
	}
	attempts := env.item().Attempts

	// Restart while the library is still away: the staging survives the
	// start-up prune and the item is not dispatched.
	m := env.start(t)
	if _, err := os.Stat(env.stagingDir(t)); err != nil {
		t.Fatalf("the start-up prune removed the staging of a waiting item: %v", err)
	}
	if _, _, ok := env.ready(m); ok {
		t.Fatal("a staged item was dispatched while the library is away")
	}

	// Further attempts while the library is away - for instance a probe that
	// raced the share - neither download nor spend the retry budget.
	for i := 0; i < 3; i++ {
		(&worker{manager: m}).process(context.Background(), env.store.job, env.item())
	}
	if env.item().Status != ItemWaitingStorage || env.item().Attempts != attempts {
		t.Fatalf("status/attempts while waiting = %v/%d, want %v/%d",
			env.item().Status, env.item().Attempts, ItemWaitingStorage, attempts)
	}

	// The library comes back; the storage monitor's forced probe sees it.
	env.setLibraryOnline(t, true)
	m.library.Guard().CheckHealth(context.Background(), true)
	job, item, ok := env.ready(m)
	if !ok {
		t.Fatal("the staged item was not dispatched once the library was back")
	}
	resolvesBefore := len(env.prov.resolvedOrder)
	states := processWithEvents(m, job, item)

	got := env.item()
	if got.Status != ItemCompleted {
		t.Fatalf("status = %v (%s), want completed", got.Status, got.ErrorMessage)
	}
	if env.dl.calls.Load() != 1 {
		t.Fatalf("downloads = %d, want 1: the staged file must only be moved", env.dl.calls.Load())
	}
	if len(env.prov.resolvedOrder) != resolvesBefore {
		t.Fatal("the media provider was asked again")
	}
	if env.tagger.calls.Load() != 1 || env.art.calls.Load() != 1 || env.lyrics.calls.Load() != 1 {
		t.Fatalf("tagger/artwork/lyrics calls = %d/%d/%d, want 1/1/1",
			env.tagger.calls.Load(), env.art.calls.Load(), env.lyrics.calls.Load())
	}
	if got.Attempts != attempts {
		t.Fatalf("attempts = %d, want %d: waiting for the library is not an attempt", got.Attempts, attempts)
	}
	if got.FileID != "file-1" || got.TrackID != "track-1" {
		t.Fatalf("file/track ids = %q/%q", got.FileID, got.TrackID)
	}
	if want := []ItemStatus{ItemFinalizing, ItemCompleted}; !reflect.DeepEqual(states, want) {
		t.Fatalf("item events = %v, want %v", states, want)
	}

	target := env.expectedTarget(t, m)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the track is not at %s: %v", target, err)
	}
	if string(data) != "opus-audio|tags" {
		t.Fatalf("library file = %q, want the staged, tagged audio", data)
	}
	if len(env.catalog.entries) != 1 || env.catalog.entries[0].File.Path != m.library.RelPath(target) {
		t.Fatalf("catalogue rows = %+v", env.catalog.entries)
	}
	if _, err := os.Stat(storage.SidecarPathFor(target, ".txt")); err != nil {
		t.Fatalf("the staged lyrics were not written: %v", err)
	}
	if cover, err := os.ReadFile(filepath.Join(filepath.Dir(target), "cover.jpg")); err != nil || string(cover) != "jpeg-cover" {
		t.Fatalf("cover = %q, %v", cover, err)
	}
	if _, err := os.Stat(env.stagingDir(t)); !os.IsNotExist(err) {
		t.Fatalf("the staging was kept after the track was finished: %v", err)
	}
	if !strings.Contains(env.logs.String(), "without downloading it again") {
		t.Fatal("the finalisation of a staged download was not logged")
	}
}

// A process can also stop while it is moving a staged file. Recovery puts the
// item back to pending, and the next attempt still only moves the file.
func TestStagedDownloadInterruptedDuringTheMoveIsFinalisedAfterARestart(t *testing.T) {
	env := newStagedEnv(t)
	env.stageWhileOffline(t)
	item := env.item()
	item.Status = ItemFinalizing
	env.store.items[env.itemID] = item
	env.setLibraryOnline(t, true)

	m := env.start(t)
	if env.item().Status != ItemPending {
		t.Fatalf("status after recovery = %v, want pending", env.item().Status)
	}
	job, ready, ok := env.ready(m)
	if !ok {
		t.Fatal("the recovered item was not dispatched")
	}
	(&worker{manager: m}).process(context.Background(), job, ready)

	if env.item().Status != ItemCompleted || env.dl.calls.Load() != 1 {
		t.Fatalf("status = %v, downloads = %d; want completed after one download", env.item().Status, env.dl.calls.Load())
	}
}

// The result of the detour must be indistinguishable from a download whose
// library was there all along: the same file at the same path, the same
// catalogue rows and the same final item state and events.
func TestStagedFinalisationMatchesANormalDownload(t *testing.T) {
	normal := newStagedEnv(t)
	nm := normal.start(t)
	job, item, _ := normal.ready(nm)
	normalStates := processWithEvents(nm, job, item)

	staged := newStagedEnv(t)
	staged.stageWhileOffline(t)
	staged.setLibraryOnline(t, true)
	sm := staged.start(t)
	job, item, ok := staged.ready(sm)
	if !ok {
		t.Fatal("the staged item was not dispatched")
	}
	stagedStates := processWithEvents(sm, job, item)

	n, s := normal.item(), staged.item()
	if n.Status != ItemCompleted || s.Status != ItemCompleted {
		t.Fatalf("statuses = %v / %v", n.Status, s.Status)
	}
	if n.FileID != s.FileID || n.TrackID != s.TrackID || n.Attempts != s.Attempts ||
		n.MediaID != s.MediaID || n.MediaProvider != s.MediaProvider || n.MatchScore != s.MatchScore {
		t.Fatalf("items differ:\nnormal %+v\nstaged %+v", n, s)
	}
	// The normal path also announces matching, downloading and tagging; the
	// end of both is the same.
	tail := func(states []ItemStatus) []ItemStatus { return states[len(states)-2:] }
	if !reflect.DeepEqual(tail(normalStates), tail(stagedStates)) {
		t.Fatalf("final events differ: %v / %v", normalStates, stagedStates)
	}

	nTarget, sTarget := normal.expectedTarget(t, nm), staged.expectedTarget(t, sm)
	if nm.library.RelPath(nTarget) != sm.library.RelPath(sTarget) {
		t.Fatalf("paths differ: %s / %s", nm.library.RelPath(nTarget), sm.library.RelPath(sTarget))
	}
	for _, rel := range []string{
		nm.library.RelPath(nTarget),
		nm.library.RelPath(storage.SidecarPathFor(nTarget, ".txt")),
		filepath.Join(filepath.Dir(nm.library.RelPath(nTarget)), "cover.jpg"),
	} {
		a, errA := os.ReadFile(filepath.Join(normal.libRoot, rel))
		b, errB := os.ReadFile(filepath.Join(staged.libRoot, rel))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Fatalf("%s differs: %q (%v) / %q (%v)", rel, a, errA, b, errB)
		}
	}

	if len(normal.catalog.entries) != 1 || len(staged.catalog.entries) != 1 {
		t.Fatalf("catalogue writes = %d / %d", len(normal.catalog.entries), len(staged.catalog.entries))
	}
	ne, se := normal.catalog.entries[0], staged.catalog.entries[0]
	if ne.Track.LyricsCheckedAt == nil || se.Track.LyricsCheckedAt == nil {
		t.Fatal("the lyrics check time was not recorded")
	}
	ne.Track.LyricsCheckedAt, se.Track.LyricsCheckedAt = nil, nil
	if !reflect.DeepEqual(ne, se) {
		t.Fatalf("catalogue rows differ:\nnormal %+v\nstaged %+v", ne, se)
	}
}

// When the staged audio is gone or no longer matches its checksum, the item
// is downloaded again the normal way, and that is logged.
func TestStagedDownloadFallsBackToANormalDownload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(t *testing.T, path string)
		reason string
	}{
		{"staged file missing", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, "staged audio is missing"},
		{"staged file changed", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("bit rot"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "failed checksum verification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newStagedEnv(t)
			env.stageWhileOffline(t)
			record := readFinaliseRecord(t, env.stagingDir(t))
			tc.damage(t, filepath.Join(env.stagingDir(t), record.Audio))
			attempts := env.item().Attempts

			env.setLibraryOnline(t, true)
			m := env.start(t)
			job, item, ok := env.ready(m)
			if !ok {
				t.Fatal("the staged item was not dispatched")
			}
			(&worker{manager: m}).process(context.Background(), job, item)

			got := env.item()
			if got.Status != ItemCompleted {
				t.Fatalf("status = %v (%s)", got.Status, got.ErrorMessage)
			}
			if env.dl.calls.Load() != 2 {
				t.Fatalf("downloads = %d, want a second download", env.dl.calls.Load())
			}
			if got.Attempts != attempts+1 {
				t.Fatalf("attempts = %d, want %d: the new download is a real attempt", got.Attempts, attempts+1)
			}
			data, err := os.ReadFile(env.expectedTarget(t, m))
			if err != nil || string(data) != "opus-audio|tags" {
				t.Fatalf("library file = %q, %v", data, err)
			}
			logs := env.logs.String()
			if !strings.Contains(logs, "cannot be finalised; downloading it again") || !strings.Contains(logs, tc.reason) {
				t.Fatalf("the fallback was not logged with its reason %q:\n%s", tc.reason, logs)
			}
		})
	}
}

// "No lyrics exist" is an answer too. It is learned while staging and recorded
// on the track at the move, without a second lookup.
func TestStagedLyricsNotFoundIsRecordedWithoutASecondLookup(t *testing.T) {
	env := newStagedEnv(t)
	env.lyrics.none = true
	env.stageWhileOffline(t)
	env.setLibraryOnline(t, true)
	m := env.start(t)
	job, item, ok := env.ready(m)
	if !ok {
		t.Fatal("the staged item was not dispatched")
	}
	(&worker{manager: m}).process(context.Background(), job, item)

	if env.item().Status != ItemCompleted || env.lyrics.calls.Load() != 1 {
		t.Fatalf("status = %v, lyrics lookups = %d", env.item().Status, env.lyrics.calls.Load())
	}
	track := env.catalog.entries[0].Track
	if track.LyricsState != music.LyricsNotFound || track.LyricsCheckedAt == nil {
		t.Fatalf("lyrics state = %q, checked at %v", track.LyricsState, track.LyricsCheckedAt)
	}
}

// A job cancelled while its track waits for the library gives the staged
// audio up at once instead of keeping it until the next start.
func TestCancellingAJobRemovesTheStagingOfAWaitingItem(t *testing.T) {
	env := newStagedEnv(t)
	env.stageWhileOffline(t)
	m := env.start(t)

	if _, err := m.Cancel(context.Background(), "job-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if env.item().Status != ItemCancelled {
		t.Fatalf("status = %v", env.item().Status)
	}
	if _, err := os.Stat(env.stagingDir(t)); !os.IsNotExist(err) {
		t.Fatalf("the staging of an abandoned item was kept: %v", err)
	}
}

// A record that does not fit the item is never trusted: it is discarded and
// the item downloads again.
func TestFinaliseRecordIsVerifiedAgainstTheItem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(r *finaliseRecord, it *Item)
	}{
		{"other item", func(r *finaliseRecord, _ *Item) { r.ItemID = music.NewID() }},
		{"path instead of a name", func(r *finaliseRecord, _ *Item) { r.Audio = filepath.Join("..", r.Audio) }},
		{"unknown version", func(r *finaliseRecord, _ *Item) { r.Version = 99 }},
		{"left over from an earlier staging", func(_ *finaliseRecord, it *Item) { it.StagedSHA256 = strings.Repeat("0", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newStagedEnv(t)
			env.stageWhileOffline(t)
			dir := env.stagingDir(t)
			record := readFinaliseRecord(t, dir)
			item := env.item()
			tc.mutate(&record, &item)
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, finaliseRecordName), data, 0o644); err != nil {
				t.Fatal(err)
			}

			m := env.start(t)
			if _, ok := m.loadStaged(item, m.logger); ok {
				t.Fatal("a record that does not fit the item was accepted")
			}
			for _, name := range []string{finaliseRecordName, finaliseCoverPrefix + ".jpg"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("%s was kept after the record was rejected", name)
				}
			}
		})
	}
}

// A download whose library is reachable never writes a finalisation record
// and is finished in the same attempt, exactly as before.
func TestReachableLibraryWritesNoFinaliseRecord(t *testing.T) {
	env := newStagedEnv(t)
	m := env.start(t)
	job, item, _ := env.ready(m)

	var recorded bool
	events, unsubscribe := m.broker.Subscribe()
	(&worker{manager: m}).process(context.Background(), job, item)
	unsubscribe()
	for ev := range events {
		if ev.ItemStatus == ItemWaitingStorage {
			recorded = true
		}
	}
	if recorded || env.item().Status != ItemCompleted {
		t.Fatalf("status = %v, waited = %v", env.item().Status, recorded)
	}
	if strings.Contains(env.logs.String(), "download staged") {
		t.Fatal("a staged result was recorded although the library was reachable")
	}
	if env.lyrics.calls.Load() != 1 {
		t.Fatalf("lyrics lookups = %d, want 1", env.lyrics.calls.Load())
	}
}

// Offline staging is not needed for the detour: a share that drops while the
// track is being downloaded and tagged leaves a complete staged result too.
func TestLibraryLostDuringTheAttemptKeepsTheStagedResult(t *testing.T) {
	env := newStagedEnv(t)
	m := env.start(t)
	m.allowOfflineStaging.Store(false)
	m.tagger = &offlineAfterTagging{env: env, t: t, inner: env.tagger}
	job, item, _ := env.ready(m)
	(&worker{manager: m}).process(context.Background(), job, item)

	if env.item().Status != ItemWaitingStorage {
		t.Fatalf("status = %v", env.item().Status)
	}
	if _, err := os.Stat(filepath.Join(env.stagingDir(t), finaliseRecordName)); err != nil {
		t.Fatalf("no finalisation record: %v", err)
	}
	if !apperr.IsStorageWait(apperr.New(apperr.Code(env.item().ErrorCode), "")) {
		t.Fatalf("error code = %s", env.item().ErrorCode)
	}
}

type offlineAfterTagging struct {
	env   *stagedEnv
	t     *testing.T
	inner Tagger
}

func (o *offlineAfterTagging) Apply(ctx context.Context, path string, tags metadata.Tags, art *metadata.Artwork) error {
	err := o.inner.Apply(ctx, path, tags, art)
	o.env.setLibraryOnline(o.t, false)
	return err
}

func readFinaliseRecord(t *testing.T, dir string) finaliseRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, finaliseRecordName))
	if err != nil {
		t.Fatal(err)
	}
	var record finaliseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}
