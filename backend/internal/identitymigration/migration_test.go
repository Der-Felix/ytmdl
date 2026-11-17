package identitymigration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/music"
)

// fakeRow is a stored track of the fake store.
type fakeRow struct {
	key     string
	title   string
	artists []string
	scheme  int
}

// fakeStore keeps rows in memory with the same conditional semantics as the
// repository: a write applies only while scheme, key and guard still match.
type fakeStore struct {
	mu     sync.Mutex
	rows   map[string]*fakeRow
	locked bool

	lists      []string // "scheme:after" of every ListIdentityRows call
	writes     int
	failAfter  int // >0: the n-th write fails
	beforeEach func(id string)
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string]*fakeRow{}} }

func (s *fakeStore) add(id, title, key string, scheme int, artists ...string) {
	s.rows[id] = &fakeRow{key: key, title: title, artists: artists, scheme: scheme}
}

func guardOf(r *fakeRow) string { return r.title + "\x00" + strings.Join(r.artists, "\x00") }

func (s *fakeStore) CountIdentityRowsBelow(_ context.Context, scheme int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.rows {
		if r.scheme < scheme {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) ListIdentityRows(_ context.Context, scheme int, after string, limit int) ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists = append(s.lists, strings.Join([]string{string(rune('0' + scheme)), after}, ":"))
	ids := make([]string, 0, len(s.rows))
	for id, r := range s.rows {
		if r.scheme == scheme && id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]Row, 0, len(ids))
	for _, id := range ids {
		r := s.rows[id]
		out = append(out, Row{ID: id, Key: r.key, Title: r.title, Artists: r.artists, Guard: guardOf(r)})
	}
	return out, nil
}

func (s *fakeStore) write(id string) error {
	if s.beforeEach != nil {
		s.beforeEach(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failAfter > 0 && s.writes >= s.failAfter {
		return errors.New("database went away")
	}
	return nil
}

func (s *fakeStore) KeepIdentityKey(_ context.Context, row Row, from, to int) (bool, error) {
	if err := s.write(row.ID); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[row.ID]
	if r == nil || r.scheme != from || r.key != row.Key || guardOf(r) != row.Guard {
		return false, nil
	}
	r.scheme = to
	return true, nil
}

func (s *fakeStore) RewriteIdentityKey(_ context.Context, row Row, key string, from, to int) (WriteResult, error) {
	if err := s.write(row.ID); err != nil {
		return WriteResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.rows))
	for id := range s.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id != row.ID && s.rows[id].key == key {
			return WriteResult{ConflictID: id}, nil
		}
	}
	r := s.rows[row.ID]
	if r == nil || r.scheme != from || r.key != row.Key || guardOf(r) != row.Guard {
		return WriteResult{}, nil
	}
	r.key, r.scheme = key, to
	return WriteResult{Applied: true}, nil
}

func (s *fakeStore) LockIdentityMigration(context.Context) (func(), bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, false, nil
	}
	s.locked = true
	return func() {
		s.mu.Lock()
		s.locked = false
		s.mu.Unlock()
	}, true, nil
}

func (s *fakeStore) row(id string) fakeRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.rows[id]
}

type memSettings struct {
	mu     sync.Mutex
	values map[string]string
	sets   map[string]int
}

func newMemSettings() *memSettings {
	return &memSettings{values: map[string]string{}, sets: map[string]int{}}
}

func (m *memSettings) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memSettings) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	m.sets[key]++
	return nil
}

func (m *memSettings) value(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key]
}

// upper is a test step: the key of scheme N+1 is the upper-cased title,
// derivable only when the stored key is the lower-cased title.
func upperStep(from int) Step {
	return Step{From: from, To: from + 1, Name: "upper", Recompute: func(r Row) Outcome {
		switch r.Key {
		case strings.ToUpper(r.Title):
			return Keep()
		case strings.ToLower(r.Title):
			return Rewrite(strings.ToUpper(r.Title))
		}
		return HoldRow(HoldNotDerivable)
	}}
}

func newTestMigrator(t *testing.T, store *fakeStore, settings *memSettings, steps []Step, batch int) *Migrator {
	t.Helper()
	target := discography.IdentitySchemeUnrecorded
	if len(steps) > 0 {
		target = steps[len(steps)-1].To
	}
	m, err := New(Options{
		Enabled: true, Store: store, Settings: settings, Steps: steps, Target: target, BatchSize: batch,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStepsMustFormOneChainToTheCurrentScheme(t *testing.T) {
	if err := checkSteps(Registered, discography.IdentityScheme); err != nil {
		t.Fatalf("registered steps: %v", err)
	}
	cases := map[string][]Step{
		"gap":           {upperStep(1), upperStep(3)},
		"wrong start":   {upperStep(2)},
		"skips":         {{From: 1, To: 3, Name: "x", Recompute: upperStep(1).Recompute}},
		"no function":   {{From: 1, To: 2, Name: "x"}},
		"short of goal": {upperStep(1)},
	}
	for name, steps := range cases {
		target := 2
		if name == "short of goal" {
			target = 3
		}
		if err := checkSteps(steps, target); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStepsRunInOrderInBatches(t *testing.T) {
	store := newFakeStore()
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		store.add(id, "Title "+id, "title "+id, 1)
	}
	// Step 2→3 turns "TITLE X" into "Title x!": it only recognises the key
	// step 1→2 wrote, so a wrong order would hold every row.
	second := Step{From: 2, To: 3, Name: "bang", Recompute: func(r Row) Outcome {
		if r.Key == strings.ToUpper(r.Title) {
			return Rewrite(r.Title + "!")
		}
		return HoldRow(HoldNotDerivable)
	}}
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1), second}, 2)

	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateCompleted || st.Rewritten != 10 || st.Held != 0 || st.Pending != 5 || st.Remaining != 0 {
		t.Fatalf("status %+v", st)
	}
	for id, r := range store.rows {
		if r.scheme != 3 || r.key != "Title "+id+"!" {
			t.Errorf("row %s = %+v", id, r)
		}
	}
	// Three batches of at most two rows per step, each continuing after the
	// last row of the one before.
	want := []string{"1:", "1:b", "1:d", "2:", "2:b", "2:d"}
	if strings.Join(store.lists, ",") != strings.Join(want, ",") {
		t.Errorf("batches %v, want %v", store.lists, want)
	}
	if settings.value(SchemeSettingKey) != "3" {
		t.Errorf("marker %q", settings.value(SchemeSettingKey))
	}
}

func TestRunIsIdempotent(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)
	store.add("b", "Other", "OTHER", 1)
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 10)

	first, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.State != StateCompleted || first.Rewritten != 1 || first.Unchanged != 1 {
		t.Fatalf("first run %+v", first)
	}
	writes, statusWrites := store.writes, settings.sets[StatusSettingKey]

	second, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.State != StateCompleted || store.writes != writes || settings.sets[StatusSettingKey] != statusWrites {
		t.Fatalf("second run %+v wrote %d rows and %d statuses", second, store.writes-writes, settings.sets[StatusSettingKey]-statusWrites)
	}
	if store.row("a").key != "SONG" || store.row("b").key != "OTHER" {
		t.Fatal("keys changed on the second run")
	}

	// A row of the older scheme that appears later (a trash restore, an older
	// binary) is picked up although the marker says done.
	store.add("c", "Late", "late", 1)
	third, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.State != StateCompleted || store.row("c").key != "LATE" || store.row("c").scheme != 2 {
		t.Fatalf("third run %+v, row %+v", third, store.row("c"))
	}
}

func TestRunResumesAfterAnInterruption(t *testing.T) {
	store := newFakeStore()
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		store.add(id, "T"+id, "t"+id, 1)
	}
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 2)

	// The database fails on the fourth write: three rows are done, the
	// fourth is untouched, nothing is half written.
	store.failAfter = 4
	st, err := m.Run(context.Background())
	if err == nil || st.State != StateFailed {
		t.Fatalf("interrupted run: %+v, %v", st, err)
	}
	done := 0
	for id, r := range store.rows {
		switch {
		case r.scheme == 2 && r.key == strings.ToUpper("T"+id):
			done++
		case r.scheme == 1 && r.key == "t"+id:
		default:
			t.Errorf("row %s is half migrated: %+v", id, r)
		}
	}
	if done != 3 {
		t.Fatalf("%d rows done before the failure, want 3", done)
	}
	if settings.value(SchemeSettingKey) != "" {
		t.Fatal("marker written after a failed run")
	}

	// A new process (new Migrator, state from the settings) resumes: it reads
	// only the rows still at scheme 1 and finishes them.
	store.failAfter = 0
	store.lists = nil
	again := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 2)
	if loaded := again.loadStatus(context.Background()); loaded.State != StateFailed {
		t.Fatalf("loaded status %+v", loaded)
	}
	st, err = again.Run(context.Background())
	if err != nil || st.State != StateCompleted || st.Pending != 3 || st.Examined != 3 {
		t.Fatalf("resumed run: %+v, %v", st, err)
	}
	for id, r := range store.rows {
		if r.scheme != 2 || r.key != strings.ToUpper("T"+id) {
			t.Errorf("row %s = %+v", id, r)
		}
	}
}

func TestStopEndsTheRunBetweenRows(t *testing.T) {
	store := newFakeStore()
	for _, id := range []string{"a", "b", "c", "d"} {
		store.add(id, "T"+id, "t"+id, 1)
	}
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	store.beforeEach = func(id string) {
		if id == "b" {
			cancel()
		}
	}
	st, err := m.Run(ctx)
	if !errors.Is(err, context.Canceled) || st.State != StatePending {
		t.Fatalf("stopped run: %+v, %v", st, err)
	}
	if store.row("c").scheme != 1 || store.row("d").scheme != 1 {
		t.Fatal("rows after the stop were migrated")
	}
}

// A collision never merges or drops a row: the row whose new key another track
// already carries keeps its old key and scheme and is reported, and the run
// carries it over once the other track is gone.
func TestCollisionHoldsTheRowAndReportsIt(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)     // would become SONG
	store.add("b", "Song", "SONG", 1)     // already SONG
	store.add("c", "Other", "other", 1)   // unaffected
	store.add("d", "Third", "garbled", 1) // not derivable
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 10)

	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateIncomplete || st.Held != 2 || st.Remaining != 2 || st.Rewritten != 1 || st.Unchanged != 1 {
		t.Fatalf("status %+v", st)
	}
	if a := store.row("a"); a.key != "song" || a.scheme != 1 {
		t.Fatalf("colliding row changed: %+v", a)
	}
	if b := store.row("b"); b.key != "SONG" || b.scheme != 2 {
		t.Fatalf("other row changed: %+v", b)
	}
	if d := store.row("d"); d.key != "garbled" || d.scheme != 1 {
		t.Fatalf("not derivable row changed: %+v", d)
	}
	reasons := map[string]Hold{}
	for _, h := range st.Holds {
		reasons[h.TrackID] = h
	}
	if h := reasons["a"]; h.Reason != HoldCollision || h.ConflictingTrackID != "b" || h.NewKey != "SONG" || h.Key != "song" {
		t.Errorf("collision hold %+v", h)
	}
	if h := reasons["d"]; h.Reason != HoldNotDerivable {
		t.Errorf("not derivable hold %+v", h)
	}
	if settings.value(SchemeSettingKey) != "" {
		t.Fatal("marker written while rows are held")
	}
	if saved := settings.value(StatusSettingKey); !strings.Contains(saved, `"collision"`) {
		t.Fatalf("saved status %s", saved)
	}

	// The owner removes the duplicate; the next start carries the row over.
	delete(store.rows, "b")
	st, err = m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a := store.row("a"); a.key != "SONG" || a.scheme != 2 || st.Held != 1 || st.State != StateIncomplete {
		t.Fatalf("after removing the duplicate: row %+v status %+v", a, st)
	}
}

func TestRowChangedDuringTheRunIsDeferred(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)
	settings := newMemSettings()
	m := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 10)
	// A download updates the title between the read and the write.
	store.beforeEach = func(id string) {
		store.mu.Lock()
		store.rows[id].title = "Song (Remastered)"
		store.mu.Unlock()
		store.beforeEach = nil
	}
	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Deferred != 1 || st.State != StateIncomplete || store.row("a").key != "song" || store.row("a").scheme != 1 {
		t.Fatalf("status %+v row %+v", st, store.row("a"))
	}
}

func TestBrokenStepHoldsInsteadOfFailing(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)
	store.add("b", "Empty", "empty", 1)
	steps := []Step{{From: 1, To: 2, Name: "broken", Recompute: func(r Row) Outcome {
		if r.ID == "a" {
			panic("bug")
		}
		return Rewrite("  ")
	}}}
	m := newTestMigrator(t, store, newMemSettings(), steps, 10)
	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Held != 2 || store.row("a").key != "song" || store.row("b").key != "empty" {
		t.Fatalf("status %+v", st)
	}
	for _, h := range st.Holds {
		if h.Reason != HoldStepFailed {
			t.Errorf("hold %+v", h)
		}
	}
}

func TestSecondProcessWaits(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)
	store.locked = true // another backend process runs the migration
	m := newTestMigrator(t, store, newMemSettings(), []Step{upperStep(1)}, 10)
	st, err := m.Run(context.Background())
	if !errors.Is(err, errBusy) || st.State != StateWaiting || store.writes != 0 {
		t.Fatalf("status %+v, %v, %d writes", st, err, store.writes)
	}
	store.locked = false
	if st, err := m.Run(context.Background()); err != nil || st.State != StateCompleted {
		t.Fatalf("after the other process: %+v, %v", st, err)
	}
}

func TestStartInTheBackgroundAndSwitchedOff(t *testing.T) {
	store := newFakeStore()
	store.add("a", "Song", "song", 1)
	settings := newMemSettings()

	off, err := New(Options{Enabled: false, Store: store, Settings: settings, Steps: []Step{upperStep(1)}, Target: 2,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	off.Start(context.Background())
	off.Stop()
	if off.Status().State != StateDisabled || store.row("a").scheme != 1 {
		t.Fatalf("switched off: %+v", off.Status())
	}

	on := newTestMigrator(t, store, settings, []Step{upperStep(1)}, 10)
	on.Start(context.Background())
	defer on.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for on.Status().State != StateCompleted {
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", on.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if store.row("a").key != "SONG" {
		t.Fatal("the background run did not carry the row over")
	}
}

// The registered step on rows in old and new form, as versions up to 1.5.0
// left them.
func TestRegisteredStepOnRowsOfBothForms(t *testing.T) {
	remix := music.Track{Title: "Blinding Lights Major Lazer Remix", Artists: []string{"The Weeknd"}}
	current := discography.IdentityKey(remix)
	older := discography.IdentityOf(remix).OlderSchemeKeys[0]

	store := newFakeStore()
	store.add("old", remix.Title, older, 1, "The Weeknd")
	store.add("new", "Save Your Tears", discography.IdentityKey(music.Track{Title: "Save Your Tears", Artists: []string{"The Weeknd"}}), 1, "The Weeknd")
	store.add("renamed", "Renamed Later", older, 1, "The Weeknd")
	settings := newMemSettings()
	m, err := New(Options{Enabled: true, Store: store, Settings: settings,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := store.row("old"); r.key != current || r.scheme != discography.IdentityScheme {
		t.Errorf("old form row = %+v, want %q at scheme %d", r, current, discography.IdentityScheme)
	}
	if r := store.row("new"); r.scheme != discography.IdentityScheme {
		t.Errorf("new form row = %+v", r)
	}
	if r := store.row("renamed"); r.key != older || r.scheme != discography.IdentitySchemeUnrecorded {
		t.Errorf("row whose key does not follow from its title = %+v, want it held", r)
	}
	if st.Rewritten != 1 || st.Unchanged != 1 || st.Held != 1 || st.Holds[0].Reason != HoldNotDerivable {
		t.Errorf("status %+v", st)
	}
}
