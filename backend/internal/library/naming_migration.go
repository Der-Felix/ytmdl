package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/bgmigration"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// Settings keys of the naming migration. They live in the settings table,
// like every other piece of runtime state the backend keeps.
const (
	// NamingSchemeSettingKey holds the naming scheme the library on disk is
	// known to follow. It is written only when a run finished with nothing
	// left behind, and a missing key means the scheme of v1.4 and earlier.
	NamingSchemeSettingKey = "library.naming_scheme"
	// NamingMigrationStatusKey holds the outcome of the last run as JSON.
	NamingMigrationStatusKey = "library.naming_migration"
	// NamingMigrationJournalKey holds the one move in flight, written before
	// the file is touched and cleared after the database agrees. A restart
	// finishes or abandons that move from it.
	NamingMigrationJournalKey = "library.naming_migration.journal"
)

// Naming migration states.
const (
	NamingStateDisabled       = "disabled"
	NamingStatePending        = "pending"
	NamingStateRunning        = "running"
	NamingStateWaitingStorage = "waiting_for_storage"
	NamingStateIncomplete     = "incomplete"
	NamingStateCompleted      = "completed"
	NamingStateFailed         = "failed"
)

// Reasons a file is left where it is. Every one of them leaves the file and
// its database row unchanged, so a later run can try again.
const (
	NamingSkipTargetExists    = "target_exists"
	NamingSkipChecksum        = "checksum_mismatch"
	NamingSkipSourceMissing   = "source_missing"
	NamingSkipMoveFailed      = "move_failed"
	NamingSkipDatabase        = "database_update_failed"
	NamingSkipOutsideLibrary  = "outside_library"
	NamingSkipPartialTarget   = "partial_target"
	NamingSkipFolderShared    = "folder_shared"
	NamingSkipExtraNotCarried = "extra_not_carried"
)

// maxRecordedSkips bounds the list kept in the status. The count is exact.
const maxRecordedSkips = 200

// errNamingStorageUnavailable means the library cannot be written right now.
// Nothing was recorded as done; the run is repeated later.
var errNamingStorageUnavailable = errors.New("library storage is not reachable or not writable")

// SettingsStore is the key value store the migration keeps its marker in. It
// is the one every background data migration uses.
type SettingsStore = bgmigration.Settings

// filePathUpdater moves a files row to a new path while keeping its ID. The
// database repository implements it; a store without it falls back to delete
// and insert.
type filePathUpdater interface {
	UpdatePath(ctx context.Context, id, from, to string) error
}

// NamingMigrationOptions configures the automatic move to the current naming
// scheme.
type NamingMigrationOptions struct {
	// Enabled switches the automatic move on (the default). Switched off, the
	// library keeps its names until it is switched on again; the integrity
	// audit still reports every such file as a path mismatch.
	Enabled  bool
	Settings SettingsStore
	// StartDelay lets the backend finish starting before the first run.
	StartDelay time.Duration
	// RetryInterval is the wait before a run that found the storage
	// unreachable is repeated.
	RetryInterval time.Duration
}

// NamingMigrationSkip is one file the migration left where it was.
type NamingMigrationSkip struct {
	Path   string `json:"path"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// NamingMigrationStatus reports the automatic move to the current naming
// scheme.
type NamingMigrationStatus struct {
	FromScheme     int                   `json:"from_scheme"`
	ToScheme       int                   `json:"to_scheme"`
	State          string                `json:"state"`
	StartedAt      *time.Time            `json:"started_at,omitempty"`
	FinishedAt     *time.Time            `json:"finished_at,omitempty"`
	Planned        int                   `json:"planned"`
	Moved          int                   `json:"moved"`
	Skipped        int                   `json:"skipped"`
	ExtrasCarried  int                   `json:"extras_carried"`
	FoldersRemoved int                   `json:"folders_removed"`
	Skips          []NamingMigrationSkip `json:"skips,omitempty"`
	// Notes lists files other than catalogued tracks (covers, files of the
	// user) that stayed in an old folder, for example because the folder was
	// shared by releases that are now apart. They never block completion.
	Notes     []NamingMigrationSkip `json:"notes,omitempty"`
	LastError string                `json:"last_error,omitempty"`
}

type namingJournal struct {
	FileID  string `json:"file_id"`
	TrackID string `json:"track_id"`
	From    string `json:"from"`
	To      string `json:"to"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// namingMove is one catalogued file whose path differs only because the
// naming scheme changed.
type namingMove struct {
	file    music.File
	trackID string
	// target is the path under the current scheme.
	target string
	// fallback is the source-suffixed form of target. It is used when target
	// is held by a catalogued file of another track, exactly as a download
	// would do in that situation. fallbackAlways also uses it when target is
	// held by anything at all (a file whose suffix was only needed under the
	// old scheme, see planNamingMoves).
	fallback       string
	fallbackAlways bool
}

type namingOutcome int

const (
	namingMoved namingOutcome = iota
	namingDeferred
	namingSkipped
	namingUnchanged
)

// Test seams. They let the tests simulate a filesystem without hard links and
// a damaged copy without a damaged filesystem.
var (
	namingHash = computeFileSHA256
	namingLink = os.Link
)

// errNamingChecksum means a copy did not read back as the original.
var errNamingChecksum = errors.New("the copy differs from the original")

// namingMoveTrack moves an audio file and its lyrics sidecars as one unit.
//
// Each file first gets its new name as a hard link: that refuses an existing
// target atomically and cannot change a byte. Only where links are not
// available (some SMB, FUSE and exFAT mounts) is the file copied, and the copy
// is read back and compared with the checksum taken before the move while the
// original still exists. The original name is removed only after that. A
// failure part way moves back what already arrived, and folders created for
// the move are removed again if they stayed empty.
func namingMoveTrack(srcAudio, dstAudio, audioSum string) error {
	type pair struct{ from, to, sum string }
	pairs := []pair{{srcAudio, dstAudio, audioSum}}
	for _, ext := range storage.LyricsExtensions() {
		from := storage.SidecarPathFor(srcAudio, ext)
		if info, err := os.Lstat(from); err == nil && info.Mode().IsRegular() {
			pairs = append(pairs, pair{from, storage.SidecarPathFor(dstAudio, ext), ""})
		}
	}
	for _, p := range pairs {
		if _, err := os.Lstat(p.to); err == nil {
			return fmt.Errorf("%w: %s", os.ErrExist, p.to)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	var created []string
	for dir := filepath.Dir(dstAudio); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil || filepath.Dir(dir) == dir {
			break
		}
		created = append(created, dir)
	}
	if err := os.MkdirAll(filepath.Dir(dstAudio), 0o755); err != nil {
		return err
	}
	removeCreated := func() {
		for _, dir := range created {
			_ = os.Remove(dir)
		}
	}

	done := make([]pair, 0, len(pairs))
	for _, p := range pairs {
		if err := namingMoveFile(p.from, p.to, p.sum); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				_ = namingMoveFile(done[i].to, done[i].from, done[i].sum)
			}
			removeCreated()
			return err
		}
		done = append(done, p)
	}
	return nil
}

// namingMoveFile gives one file a new name without ever replacing a file and
// without removing the original before an identical copy exists.
func namingMoveFile(from, to, sum string) error {
	err := namingLink(from, to)
	if err == nil {
		if rmErr := os.Remove(from); rmErr != nil {
			_ = os.Remove(to)
			return rmErr
		}
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return err
	}
	if sum == "" {
		if sum, err = namingHash(from); err != nil {
			return err
		}
	}
	if err := copyFileExclusive(from, to); err != nil {
		return err
	}
	got, err := namingHash(to)
	if err != nil || got != sum {
		_ = os.Remove(to)
		return errNamingChecksum
	}
	return os.Remove(from)
}

// namingRun carries the state of one run.
type namingRun struct {
	from, to storage.NamingScheme
	settings SettingsStore
	status   *NamingMigrationStatus
	// pendingFrom holds the current paths of moves not done yet; a target
	// held by one of them may become free later in the run.
	pendingFrom map[string]bool
	// dirs maps an old release folder to the folders its files went to, and
	// records whether any of its files stayed behind.
	dirs map[string]*namingDir
	// currentDirs holds the folder of every catalogued release under the
	// current scheme; such a folder is never emptied.
	currentDirs  map[string]bool
	lastProgress time.Time
	lastPersist  time.Time
}

type namingDir struct {
	targets map[string]bool
	blocked bool
	// inUse means the folder is also the current folder of a release, so it
	// is never emptied; covers are only copied out of it.
	inUse bool
}

// StartNamingMigration starts the automatic move to the current naming scheme
// in the background. It returns at once; the API and the downloads are not
// held up, and a library that is not reachable is tried again later.
func (s *Service) StartNamingMigration(opts NamingMigrationOptions) {
	s.namingSettings = opts.Settings
	if opts.RetryInterval <= 0 {
		opts.RetryInterval = 10 * time.Minute
	}
	initial := s.loadNamingStatus(s.ctx, opts.Settings)
	if !opts.Enabled {
		if initial.State != NamingStateCompleted {
			initial.State = NamingStateDisabled
			s.logger.Info("automatic renaming to the current naming scheme is switched off; files keep their old names until it is switched on",
				"from_scheme", initial.FromScheme, "to_scheme", initial.ToScheme)
		}
		s.namingStatus.Store(initial)
		return
	}
	s.namingStatus.Store(initial)
	if opts.Settings == nil {
		return
	}

	bgmigration.Schedule(s.ctx, &s.wg, s.logger, "library naming migration",
		opts.StartDelay, opts.RetryInterval, func(ctx context.Context) error {
			_, err := s.RunNamingMigration(ctx, opts.Settings)
			return err
		})
}

// NamingMigrationStatus returns the state of the automatic move, or nil when
// it was never started.
func (s *Service) NamingMigrationStatus() *NamingMigrationStatus {
	st := s.namingStatus.Load()
	if st == nil {
		return nil
	}
	cp := *st
	cp.Skips = append([]NamingMigrationSkip(nil), st.Skips...)
	return &cp
}

// loadNamingStatus returns the persisted status, or a pending one.
func (s *Service) loadNamingStatus(ctx context.Context, settings SettingsStore) *NamingMigrationStatus {
	st := &NamingMigrationStatus{
		FromScheme: int(storage.NamingSchemeLegacy),
		ToScheme:   int(storage.CurrentNamingScheme),
		State:      NamingStatePending,
	}
	if settings == nil {
		return st
	}
	if applied, ok := s.appliedNamingScheme(ctx, settings); ok {
		st.FromScheme = int(applied)
		if applied >= storage.CurrentNamingScheme {
			st.State = NamingStateCompleted
		}
	}
	if raw, ok, err := settings.Get(ctx, NamingMigrationStatusKey); err == nil && ok {
		var saved NamingMigrationStatus
		if json.Unmarshal([]byte(raw), &saved) == nil && saved.ToScheme == st.ToScheme {
			if st.State != NamingStateCompleted && saved.State == NamingStateCompleted {
				saved.State = NamingStatePending
			}
			if saved.State == NamingStateRunning {
				// The previous process stopped in the middle; it resumes.
				saved.State = NamingStatePending
			}
			return &saved
		}
	}
	return st
}

// appliedNamingScheme reads the marker. ok is false when there is none.
func (s *Service) appliedNamingScheme(ctx context.Context, settings SettingsStore) (storage.NamingScheme, bool) {
	n, ok := bgmigration.ReadScheme(ctx, settings, NamingSchemeSettingKey)
	return storage.NamingScheme(n), ok
}

// RunNamingMigration moves every catalogued file whose path differs only
// because the naming scheme changed. It is safe to run any number of times:
// files already at their new path are left alone, and the marker makes a run
// after a complete one a no-op.
//
// The error is non-nil when the run has to be repeated: the storage is not
// reachable, the database failed, or the context ended. Files that could not
// be moved for a reason of their own (a target that exists, a checksum that
// does not match) are recorded in the status, the run ends without error, and
// the marker is not written, so the next start tries them again.
func (s *Service) RunNamingMigration(ctx context.Context, settings SettingsStore) (*NamingMigrationStatus, error) {
	if settings == nil {
		return nil, apperr.New(apperr.CodeInternal, "The naming migration needs a settings store.")
	}
	if !s.namingMu.TryLock() {
		return s.NamingMigrationStatus(), apperr.New(apperr.CodeConflict, "The naming migration is already running.")
	}
	defer s.namingMu.Unlock()

	from := storage.NamingSchemeLegacy
	if applied, ok := s.appliedNamingScheme(ctx, settings); ok {
		if applied >= storage.CurrentNamingScheme {
			st := &NamingMigrationStatus{FromScheme: int(applied), ToScheme: int(storage.CurrentNamingScheme), State: NamingStateCompleted}
			if prev := s.namingStatus.Load(); prev != nil && prev.State == NamingStateCompleted {
				st = prev
			}
			s.namingStatus.Store(st)
			return st, nil
		}
		from = applied
	}

	now := time.Now().UTC()
	run := &namingRun{
		from:        from,
		to:          storage.CurrentNamingScheme,
		settings:    settings,
		pendingFrom: make(map[string]bool),
		dirs:        make(map[string]*namingDir),
		currentDirs: make(map[string]bool),
		status: &NamingMigrationStatus{
			FromScheme: int(from),
			ToScheme:   int(storage.CurrentNamingScheme),
			State:      NamingStateRunning,
			StartedAt:  &now,
		},
	}
	s.storeNamingSnapshot(run)

	fail := func(state string, err error) (*NamingMigrationStatus, error) {
		run.status.State = state
		run.status.LastError = err.Error()
		s.persistNamingStatus(ctx, run, true)
		return s.NamingMigrationStatus(), err
	}

	if err := s.namingStorageReady(ctx, true); err != nil {
		s.logger.Warn("library naming migration is waiting for the library storage", logging.KeyError, err.Error())
		return fail(NamingStateWaitingStorage, fmt.Errorf("%w: %v", errNamingStorageUnavailable, err))
	}

	if err := s.resumeNamingJournal(ctx, run); err != nil {
		return fail(NamingStateFailed, err)
	}

	moves, err := s.planNamingMoves(ctx, run)
	if err != nil {
		return fail(NamingStateFailed, err)
	}
	run.status.Planned = len(moves)
	if len(moves) > 0 && !s.anyNamingSourceExists(moves) {
		// Not one file is where the catalog says: the library is most likely
		// not mounted. Nothing is recorded as done.
		err := fmt.Errorf("%w: none of the %d files to rename was found below %s", errNamingStorageUnavailable, len(moves), s.library.Root())
		s.logger.Warn("library naming migration is waiting for the library storage", logging.KeyError, err.Error())
		return fail(NamingStateWaitingStorage, err)
	}

	s.logger.Info("library naming migration started",
		"from_scheme", int(run.from), "to_scheme", int(run.to), "files", len(moves))
	s.persistNamingStatus(ctx, run, true)

	if err := s.executeNamingMoves(ctx, run, moves); err != nil {
		if errors.Is(err, errNamingStorageUnavailable) {
			s.logger.Warn("library naming migration paused: the library storage became unavailable", logging.KeyError, err.Error())
			return fail(NamingStateWaitingStorage, err)
		}
		return fail(NamingStateFailed, err)
	}

	if err := s.carryNamingFolders(ctx, run); err != nil {
		if errors.Is(err, errNamingStorageUnavailable) {
			return fail(NamingStateWaitingStorage, err)
		}
		return fail(NamingStateFailed, err)
	}

	finished := time.Now().UTC()
	run.status.FinishedAt = &finished
	if run.status.Skipped == 0 {
		if err := bgmigration.WriteScheme(ctx, settings, NamingSchemeSettingKey, int(run.to)); err != nil {
			return fail(NamingStateFailed, err)
		}
		run.status.State = NamingStateCompleted
	} else {
		run.status.State = NamingStateIncomplete
	}
	s.persistNamingStatus(ctx, run, true)
	s.logger.Info("library naming migration finished",
		"state", run.status.State,
		"moved", run.status.Moved,
		"skipped", run.status.Skipped,
		"extras_carried", run.status.ExtrasCarried,
		"folders_removed", run.status.FoldersRemoved)
	return s.NamingMigrationStatus(), nil
}

// namingStorageReady checks that the library can be written: the root is
// there, the storage guard identifies the right share and has space, and with
// probe also that a file can actually be created.
func (s *Service) namingStorageReady(ctx context.Context, probe bool) error {
	info, err := os.Stat(s.library.Root())
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("library root %s is not a directory", s.library.Root())
	}
	guard := s.library.Guard()
	if guard == nil {
		return nil
	}
	if err := guard.RequireWritable(); err != nil {
		return err
	}
	if probe {
		health := guard.CheckHealth(ctx, true)
		if !health.Writable {
			return fmt.Errorf("library storage is %s: %s", health.Status, health.LastError)
		}
	}
	return nil
}

// planNamingMoves finds every catalogued file whose stored path is exactly
// what the previous scheme produced from the catalog and differs from what the
// current scheme produces. Files whose path differs for any other reason are
// left to the integrity audit.
func (s *Service) planNamingMoves(ctx context.Context, run *namingRun) ([]*namingMove, error) {
	files, err := s.files.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := s.catalog.ListAllTracks(ctx)
	if err != nil {
		return nil, err
	}
	releases, err := s.catalog.ListAllReleases(ctx)
	if err != nil {
		return nil, err
	}
	tracks := make(map[string]repository.StoredTrack, len(stored))
	for _, st := range stored {
		tracks[st.Track.ID] = st
	}
	releaseByID := make(map[string]music.Release, len(releases))
	for _, r := range releases {
		releaseByID[r.ID] = r
	}

	// Every old release folder that still exists is registered with its new
	// folder, so its cover follows even when its tracks moved in an earlier,
	// interrupted run.
	seenRelease := make(map[string]bool)
	registerRelease := func(release music.Release) {
		oldDir := storage.ReleaseDirRelFor(run.from, release)
		newDir := storage.ReleaseDirRelFor(run.to, release)
		run.currentDirs[newDir] = true
		if seenRelease[oldDir+"\x00"+newDir] || oldDir == newDir {
			return
		}
		seenRelease[oldDir+"\x00"+newDir] = true
		if info, err := os.Stat(filepath.Join(s.library.Root(), oldDir)); err == nil && info.IsDir() {
			run.dir(oldDir).targets[newDir] = true
		}
	}
	for _, r := range releases {
		registerRelease(r)
	}
	for _, st := range stored {
		if _, ok := releaseByID[st.Track.ReleaseID]; !ok {
			registerRelease(releaseFromStored(st))
		}
	}

	type paths struct{ oldPlain, newPlain string }
	computed := make(map[string]paths, len(files))
	ownerOfOld := make(map[string]string, len(files))
	for _, f := range files {
		ownerOfOld[f.Path] = f.ID
		st, ok := tracks[f.TrackID]
		if !ok {
			continue
		}
		release, ok := releaseByID[st.Track.ReleaseID]
		if !ok {
			release = releaseFromStored(st)
		}
		ext := filepath.Ext(f.Path)
		computed[f.ID] = paths{
			oldPlain: storage.TrackRelPathFor(run.from, release, st.Track, ext),
			newPlain: storage.TrackRelPathFor(run.to, release, st.Track, ext),
		}
	}

	moves := make([]*namingMove, 0)
	for _, f := range files {
		p, ok := computed[f.ID]
		if !ok {
			continue
		}
		var mv *namingMove
		switch {
		case f.Path == p.oldPlain:
			if p.oldPlain == p.newPlain {
				continue
			}
			mv = &namingMove{file: f, trackID: f.TrackID, target: p.newPlain}
			if suffix := storage.SourceSuffix(f.SourceProvider, f.SourceID); suffix != "" {
				mv.fallback = withSuffix(p.newPlain, suffix)
			}
		default:
			suffix, isSuffixed := storage.SplitSourceSuffix(filepath.Base(f.Path))
			if !isSuffixed || f.Path != withSuffix(p.oldPlain, suffix) {
				continue
			}
			newSuffixed := withSuffix(p.newPlain, suffix)
			// The suffix was added because another recording already held
			// the plain name. When that other file was another catalogued
			// track whose name is different under the new scheme, the two
			// only ever collided because of the old substitution, and this
			// file gets its plain name back once the other one has moved.
			other, held := ownerOfOld[p.oldPlain]
			if held && other != f.ID && computed[other].newPlain != "" && computed[other].newPlain != p.newPlain {
				mv = &namingMove{file: f, trackID: f.TrackID, target: p.newPlain, fallback: newSuffixed, fallbackAlways: true}
			} else {
				if newSuffixed == f.Path {
					continue
				}
				mv = &namingMove{file: f, trackID: f.TrackID, target: newSuffixed}
			}
		}
		moves = append(moves, mv)
		run.pendingFrom[f.Path] = true
	}
	sort.SliceStable(moves, func(i, j int) bool { return moves[i].file.Path < moves[j].file.Path })
	return moves, nil
}

func withSuffix(rel, suffix string) string {
	return filepath.Join(filepath.Dir(rel), storage.WithSourceSuffix(filepath.Base(rel), suffix))
}

func (s *Service) anyNamingSourceExists(moves []*namingMove) bool {
	for _, mv := range moves {
		if abs, _, err := VerifyPathConfinement(s.library.Root(), mv.file.Path, false); err == nil {
			if _, err := os.Lstat(abs); err == nil {
				return true
			}
		}
	}
	return false
}

// executeNamingMoves works through the moves one release folder at a time.
// A move whose target is still held by a file that is itself waiting to move
// is retried after the others; once a pass makes no progress, what is left is
// decided without waiting any longer.
func (s *Service) executeNamingMoves(ctx context.Context, run *namingRun, moves []*namingMove) error {
	pending := moves
	allowDefer := true
	for len(pending) > 0 {
		deferred, progressed, err := s.namingPass(ctx, run, pending, allowDefer)
		if err != nil {
			return err
		}
		if !allowDefer {
			break
		}
		if !progressed {
			allowDefer = false
		}
		pending = deferred
	}
	return nil
}

func (s *Service) namingPass(ctx context.Context, run *namingRun, moves []*namingMove, allowDefer bool) ([]*namingMove, bool, error) {
	groups := make(map[string][]*namingMove)
	order := make([]string, 0)
	for _, mv := range moves {
		dir := filepath.Dir(mv.file.Path)
		if _, seen := groups[dir]; !seen {
			order = append(order, dir)
		}
		groups[dir] = append(groups[dir], mv)
	}

	var deferred []*namingMove
	progressed := false
	for _, dir := range order {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if err := s.namingStorageReady(ctx, false); err != nil {
			return nil, false, fmt.Errorf("%w: %v", errNamingStorageUnavailable, err)
		}
		group := groups[dir]
		lockDirs := []string{dir}
		for _, mv := range group {
			lockDirs = append(lockDirs, filepath.Dir(mv.target))
			if mv.fallback != "" {
				lockDirs = append(lockDirs, filepath.Dir(mv.fallback))
			}
		}
		unlock := s.library.LockReleaseDirs(lockDirs...)
		for _, mv := range group {
			if err := ctx.Err(); err != nil {
				unlock()
				return nil, false, err
			}
			outcome, err := s.applyNamingMove(ctx, run, mv, allowDefer)
			if err != nil {
				unlock()
				return nil, false, err
			}
			switch outcome {
			case namingDeferred:
				deferred = append(deferred, mv)
			case namingMoved, namingUnchanged:
				progressed = true
			case namingSkipped:
				progressed = true
				run.dir(dir).blocked = true
			}
		}
		unlock()
		s.reportNamingProgress(ctx, run)
	}
	return deferred, progressed, nil
}

func (r *namingRun) dir(dir string) *namingDir {
	d, ok := r.dirs[dir]
	if !ok {
		d = &namingDir{targets: make(map[string]bool)}
		r.dirs[dir] = d
	}
	return d
}

// pathTaken reports whether the audio path or any of its sidecar paths is in
// use. A sidecar in the way would make the move fail half way, so it counts.
func pathTaken(abs string) (bool, error) {
	for _, p := range append([]string{abs}, sidecarsOf(abs)...) {
		if _, err := os.Lstat(p); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func sidecarsOf(abs string) []string {
	out := make([]string, 0, 2)
	for _, ext := range storage.LyricsExtensions() {
		out = append(out, storage.SidecarPathFor(abs, ext))
	}
	return out
}

// chooseNamingTarget picks where a file goes. It returns "" with a skip
// reason, or deferred when the target may still become free in this run.
func (s *Service) chooseNamingTarget(ctx context.Context, run *namingRun, mv *namingMove, allowDefer bool) (target string, deferred bool, reason, detail string) {
	try := func(rel string) (free bool, err error) {
		abs, _, err := VerifyPathConfinement(s.library.Root(), rel, true)
		if err != nil {
			return false, err
		}
		taken, err := pathTaken(abs)
		return !taken, err
	}

	free, err := try(mv.target)
	if err != nil {
		return "", false, NamingSkipOutsideLibrary, err.Error()
	}
	if free {
		return mv.target, false, "", ""
	}
	if allowDefer && run.pendingFrom[mv.target] {
		return "", true, "", ""
	}
	if mv.fallback == "" {
		return "", false, NamingSkipTargetExists, "A file already exists at the new path."
	}
	if !mv.fallbackAlways {
		occupant, err := s.files.FindByPath(ctx, mv.target)
		if err != nil || occupant == nil || occupant.TrackID == "" || occupant.TrackID == mv.trackID {
			return "", false, NamingSkipTargetExists, "A file that is not another catalogued track already exists at the new path."
		}
	}
	if mv.fallback == mv.file.Path {
		return mv.file.Path, false, "", ""
	}
	free, err = try(mv.fallback)
	if err != nil {
		return "", false, NamingSkipOutsideLibrary, err.Error()
	}
	if !free {
		if allowDefer && run.pendingFrom[mv.fallback] {
			return "", true, "", ""
		}
		return "", false, NamingSkipTargetExists, "Files already exist at the new path and at its source-suffixed form."
	}
	return mv.fallback, false, "", ""
}

// applyNamingMove moves one file and its sidecars and then its database row.
// The journal written first lets a restart finish or abandon the move; at no
// point is a file overwritten or removed without an identical copy remaining.
func (s *Service) applyNamingMove(ctx context.Context, run *namingRun, mv *namingMove, allowDefer bool) (namingOutcome, error) {
	unlockTrack := s.locks.Lock("track:" + mv.trackID)
	defer unlockTrack()

	from := mv.file.Path
	skip := func(target, reason, detail string) (namingOutcome, error) {
		delete(run.pendingFrom, from)
		s.recordNamingSkip(run, NamingMigrationSkip{Path: from, Target: target, Reason: reason, Detail: detail})
		return namingSkipped, nil
	}

	srcAbs, _, err := VerifyPathConfinement(s.library.Root(), from, true)
	if err != nil {
		return skip(mv.target, NamingSkipOutsideLibrary, err.Error())
	}
	srcInfo, err := os.Lstat(srcAbs)
	if err != nil || !srcInfo.Mode().IsRegular() {
		return skip(mv.target, NamingSkipSourceMissing, "The file is not where the catalog says.")
	}

	target, deferred, reason, detail := s.chooseNamingTarget(ctx, run, mv, allowDefer)
	if deferred {
		return namingDeferred, nil
	}
	if target == "" {
		return skip(mv.target, reason, detail)
	}
	if target == from {
		delete(run.pendingFrom, from)
		return namingUnchanged, nil
	}
	dstAbs, dstRel, err := VerifyPathConfinement(s.library.Root(), target, true)
	if err != nil {
		return skip(target, NamingSkipOutsideLibrary, err.Error())
	}

	sum, err := namingHash(srcAbs)
	if err != nil {
		return skip(target, NamingSkipSourceMissing, "The file could not be read: "+err.Error())
	}
	journal := namingJournal{FileID: mv.file.ID, TrackID: mv.trackID, From: from, To: dstRel, SHA256: sum, Size: srcInfo.Size()}
	if err := s.writeNamingJournal(ctx, run.settings, &journal); err != nil {
		return namingSkipped, err
	}

	if err := namingMoveTrack(srcAbs, dstAbs, sum); err != nil {
		_ = s.writeNamingJournal(ctx, run.settings, nil)
		switch {
		case errors.Is(err, os.ErrExist):
			return skip(dstRel, NamingSkipTargetExists, err.Error())
		case errors.Is(err, errNamingChecksum):
			return skip(dstRel, NamingSkipChecksum, "The copy at the new path differed from the original; it was removed and the original kept.")
		default:
			return skip(dstRel, NamingSkipMoveFailed, err.Error())
		}
	}

	if err := s.updateFilePath(ctx, mv.file, dstRel); err != nil {
		if backErr := namingMoveTrack(dstAbs, srcAbs, sum); backErr != nil {
			return skip(dstRel, NamingSkipDatabase, "The catalog could not be updated and the file could not be moved back; the next run completes it: "+err.Error())
		}
		_ = s.writeNamingJournal(ctx, run.settings, nil)
		return skip(dstRel, NamingSkipDatabase, err.Error())
	}
	if err := s.writeNamingJournal(ctx, run.settings, nil); err != nil {
		s.logger.Warn("naming migration journal could not be cleared; the next run clears it", logging.KeyError, err.Error())
	}

	delete(run.pendingFrom, from)
	run.status.Moved++
	run.dir(filepath.Dir(from)).targets[filepath.Dir(dstRel)] = true
	return namingMoved, nil
}

// updateFilePath points a files row at its new path, keeping its ID.
func (s *Service) updateFilePath(ctx context.Context, file music.File, to string) error {
	if updater, ok := s.files.(filePathUpdater); ok {
		return updater.UpdatePath(ctx, file.ID, file.Path, to)
	}
	if err := s.files.DeleteByPath(ctx, file.Path); err != nil {
		return err
	}
	file.Path = to
	_, err := s.files.Upsert(ctx, file)
	return err
}

func (s *Service) writeNamingJournal(ctx context.Context, settings SettingsStore, j *namingJournal) error {
	value := ""
	if j != nil {
		raw, err := json.Marshal(j)
		if err != nil {
			return err
		}
		value = string(raw)
	}
	return settings.Set(context.WithoutCancel(ctx), NamingMigrationJournalKey, value)
}

// resumeNamingJournal settles the move a previous process left in flight. It
// only completes what provably happened: the file at the new path must carry
// the checksum taken before the move.
func (s *Service) resumeNamingJournal(ctx context.Context, run *namingRun) error {
	raw, ok, err := run.settings.Get(ctx, NamingMigrationJournalKey)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var j namingJournal
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		s.logger.Warn("naming migration journal is unreadable and is discarded", logging.KeyError, err.Error())
		return s.writeNamingJournal(ctx, run.settings, nil)
	}
	s.logger.Info("naming migration resumes an interrupted move", "from", j.From, "to", j.To)

	srcAbs, _, errSrc := VerifyPathConfinement(s.library.Root(), j.From, true)
	dstAbs, _, errDst := VerifyPathConfinement(s.library.Root(), j.To, true)
	if errSrc != nil || errDst != nil {
		return s.writeNamingJournal(ctx, run.settings, nil)
	}
	srcInfo, srcErr := os.Lstat(srcAbs)
	dstInfo, dstErr := os.Lstat(dstAbs)
	srcExists, dstExists := srcErr == nil, dstErr == nil

	complete := func() error {
		// The audio is at its new path. Sidecars that had not followed yet
		// are moved now, never over an existing file.
		for _, ext := range storage.LyricsExtensions() {
			from := storage.SidecarPathFor(srcAbs, ext)
			to := storage.SidecarPathFor(dstAbs, ext)
			if _, err := os.Lstat(from); err != nil {
				continue
			}
			if err := namingMoveFile(from, to, ""); err != nil {
				s.recordNamingSkip(run, NamingMigrationSkip{Path: s.library.RelPath(from), Target: s.library.RelPath(to), Reason: NamingSkipTargetExists, Detail: err.Error()})
			}
		}
		file, err := s.files.FindByID(ctx, j.FileID)
		if err != nil {
			return err
		}
		if file != nil && file.Path != j.To {
			if err := s.updateFilePath(ctx, *file, j.To); err != nil {
				return err
			}
		}
		run.status.Moved++
		run.dir(filepath.Dir(j.From)).targets[filepath.Dir(j.To)] = true
		return s.writeNamingJournal(ctx, run.settings, nil)
	}

	switch {
	case !srcExists && dstExists:
		got, err := namingHash(dstAbs)
		if err == nil && got == j.SHA256 {
			return complete()
		}
		s.recordNamingSkip(run, NamingMigrationSkip{Path: j.From, Target: j.To, Reason: NamingSkipChecksum,
			Detail: "An interrupted move left a file at the new path whose checksum does not match; nothing was changed."})
	case srcExists && dstExists:
		same := os.SameFile(srcInfo, dstInfo)
		if !same {
			srcSum, e1 := namingHash(srcAbs)
			dstSum, e2 := namingHash(dstAbs)
			same = e1 == nil && e2 == nil && srcSum == j.SHA256 && dstSum == j.SHA256
		}
		if same {
			// Both names hold the same bytes: the move stopped between
			// placing the new name and removing the old one.
			if err := os.Remove(srcAbs); err != nil {
				return err
			}
			return complete()
		}
		s.recordNamingSkip(run, NamingMigrationSkip{Path: j.From, Target: j.To, Reason: NamingSkipPartialTarget,
			Detail: "An interrupted move left a different file at the new path; both were kept."})
	case !srcExists && !dstExists:
		s.recordNamingSkip(run, NamingMigrationSkip{Path: j.From, Target: j.To, Reason: NamingSkipSourceMissing,
			Detail: "The file of an interrupted move is at neither path."})
	}
	// Source still in place and nothing at the target: the move never
	// happened or was rolled back. The plan picks the file up again.
	return s.writeNamingJournal(ctx, run.settings, nil)
}

// carryNamingFolders lets everything else in an old release folder follow its
// tracks: covers, and any other file a user or a media server put there. A
// folder some of whose files could not move is left for a later run.
func (s *Service) carryNamingFolders(ctx context.Context, run *namingRun) error {
	dirs := make([]string, 0, len(run.dirs))
	for dir := range run.dirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, oldDir := range dirs {
		info := run.dirs[oldDir]
		targets := make([]string, 0, len(info.targets))
		for t := range info.targets {
			if t != oldDir {
				targets = append(targets, t)
			}
		}
		if info.blocked || len(targets) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.namingStorageReady(ctx, false); err != nil {
			return fmt.Errorf("%w: %v", errNamingStorageUnavailable, err)
		}
		sort.Strings(targets)
		unlock := s.library.LockReleaseDirs(append([]string{oldDir}, targets...)...)
		result := s.carryReleaseFolder(oldDir, targets, info.inUse || run.currentDirs[oldDir])
		unlock()
		run.status.ExtrasCarried += result.carried
		run.status.FoldersRemoved += result.removed
		for _, note := range result.skips {
			if len(run.status.Notes) < maxRecordedSkips {
				run.status.Notes = append(run.status.Notes, note)
			}
			s.logger.Info("library naming migration kept a file in its old folder",
				"path", note.Path, "target", note.Target, "reason", note.Reason, "detail", note.Detail)
		}
	}
	return nil
}

type carryResult struct {
	carried int
	removed int
	skips   []NamingMigrationSkip
}

// carryReleaseFolder moves the remaining entries of a release folder whose
// tracks have moved. With one new folder everything follows. When the tracks
// went to several folders, audio stays behind, or the folder is still the
// current folder of another release (inUse), only the covers are copied, so
// every new folder has its artwork; the rest stays and is reported.
// Afterwards the old folder and its artist folder are removed if, and only
// if, they are empty.
func (s *Service) carryReleaseFolder(oldRel string, newRels []string, inUse bool) carryResult {
	var res carryResult
	root, err := CanonicalRoot(s.library.Root())
	if err != nil {
		return res
	}
	oldAbs, _, err := VerifyPathConfinement(s.library.Root(), oldRel, true)
	if err != nil || oldAbs == root {
		return res
	}
	entries, err := os.ReadDir(oldAbs)
	if err != nil {
		return res
	}

	hasAudio := false
	for _, e := range entries {
		if e.Type().IsRegular() && IsSupportedAudio(filepath.Ext(e.Name())) {
			hasAudio = true
		}
	}

	if !hasAudio && !inUse && len(newRels) == 1 {
		newAbs, _, err := VerifyPathConfinement(s.library.Root(), newRels[0], true)
		if err != nil {
			return res
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".ytmdl-") || strings.HasPrefix(name, ".ytdm-") {
				continue
			}
			src := filepath.Join(oldAbs, name)
			dst := filepath.Join(newAbs, name)
			var moveErr error
			if e.Type().IsRegular() {
				moveErr = s.library.Place(src, dst)
				if apperr.CodeOf(moveErr) == apperr.CodePathConflict && sameContent(src, dst) {
					// An identical copy is already there (a cover copied
					// while the folder still held other tracks).
					moveErr = os.Remove(src)
				}
			} else if _, statErr := os.Lstat(dst); statErr == nil {
				moveErr = apperr.Newf(apperr.CodePathConflict, "%q already exists.", s.library.RelPath(dst))
			} else {
				moveErr = os.Rename(src, dst)
			}
			if moveErr != nil {
				res.skips = append(res.skips, NamingMigrationSkip{Path: s.library.RelPath(src), Target: s.library.RelPath(dst), Reason: NamingSkipExtraNotCarried, Detail: moveErr.Error()})
				continue
			}
			res.carried++
		}
	} else {
		covers := make([]string, 0, 2)
		for _, e := range entries {
			if e.Type().IsRegular() && (e.Name() == storage.CoverFileName || e.Name() == storage.CoverFileNameFor(".png")) {
				covers = append(covers, e.Name())
			}
		}
		allCopied := true
		for _, cover := range covers {
			src := filepath.Join(oldAbs, cover)
			for _, nr := range newRels {
				newAbs, _, err := VerifyPathConfinement(s.library.Root(), nr, true)
				if err != nil {
					allCopied = false
					continue
				}
				if hasCover(newAbs) {
					if !sameContent(src, filepath.Join(newAbs, cover)) {
						allCopied = false
					}
					continue
				}
				if err := copyFileExclusive(src, filepath.Join(newAbs, cover)); err != nil {
					allCopied = false
					continue
				}
				res.carried++
			}
		}
		onlyCovers := !hasAudio && !inUse && len(entries) == len(covers)
		if onlyCovers && allCopied {
			for _, cover := range covers {
				_ = os.Remove(filepath.Join(oldAbs, cover))
			}
		} else if !inUse && (len(entries) > len(covers) || hasAudio) {
			res.skips = append(res.skips, NamingMigrationSkip{Path: oldRel, Reason: NamingSkipFolderShared,
				Detail: fmt.Sprintf("The folder's tracks went to %d folders or it still holds audio; covers were copied, everything else stays.", len(newRels))})
		}
	}

	if os.Remove(oldAbs) == nil {
		res.removed++
		parent := filepath.Dir(oldAbs)
		if rel, err := filepath.Rel(root, parent); err == nil && rel != "." && filepath.IsLocal(rel) && os.Remove(parent) == nil {
			res.removed++
		}
	}
	return res
}

func hasCover(dir string) bool {
	for _, name := range []string{storage.CoverFileName, storage.CoverFileNameFor(".png")} {
		if info, err := os.Lstat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func sameContent(a, b string) bool {
	ha, errA := computeFileSHA256(a)
	hb, errB := computeFileSHA256(b)
	return errA == nil && errB == nil && ha == hb
}

// copyFileExclusive copies src to a new file dst and never replaces one.
func copyFileExclusive(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

func (s *Service) recordNamingSkip(run *namingRun, skip NamingMigrationSkip) {
	run.status.Skipped++
	if len(run.status.Skips) < maxRecordedSkips {
		run.status.Skips = append(run.status.Skips, skip)
	}
	s.logger.Warn("library naming migration left a file in place",
		"path", skip.Path, "target", skip.Target, "reason", skip.Reason, "detail", skip.Detail)
}

// reportNamingProgress logs progress and saves the status now and then, so a
// large library shows where it stands without a log line per file.
func (s *Service) reportNamingProgress(ctx context.Context, run *namingRun) {
	now := time.Now()
	if now.Sub(run.lastProgress) >= 30*time.Second {
		run.lastProgress = now
		s.logger.Info("library naming migration in progress",
			"moved", run.status.Moved, "skipped", run.status.Skipped, "planned", run.status.Planned)
	}
	if now.Sub(run.lastPersist) >= 10*time.Second {
		s.persistNamingStatus(ctx, run, false)
	}
}

func (s *Service) persistNamingStatus(ctx context.Context, run *namingRun, force bool) {
	now := time.Now()
	if !force && now.Sub(run.lastPersist) < 10*time.Second {
		return
	}
	run.lastPersist = now
	// The row is written before the snapshot is published, so a reader that
	// sees a finished state also finds it saved.
	raw, err := json.Marshal(run.status)
	if err == nil {
		if err := run.settings.Set(context.WithoutCancel(ctx), NamingMigrationStatusKey, string(raw)); err != nil {
			s.logger.Warn("naming migration status could not be saved", logging.KeyError, err.Error())
		}
	}
	s.storeNamingSnapshot(run)
}

// storeNamingSnapshot publishes a copy of the run's status for readers; the
// run keeps mutating its own.
func (s *Service) storeNamingSnapshot(run *namingRun) *NamingMigrationStatus {
	snapshot := *run.status
	snapshot.Skips = append([]NamingMigrationSkip(nil), run.status.Skips...)
	s.namingStatus.Store(&snapshot)
	return &snapshot
}

// namingState is embedded in Service.
type namingState struct {
	namingMu       sync.Mutex
	namingSettings SettingsStore
	namingStatus   atomic.Pointer[NamingMigrationStatus]
}
