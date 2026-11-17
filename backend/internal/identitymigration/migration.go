// Package identitymigration carries stored track identity keys over to the
// current identity scheme.
//
// The key a track is recognised by (discography.IdentityKey) is computed once
// and stored. When the way it is built changes, rows written before keep the
// older form. Instead of lookups that know every older form for ever, every
// row records the scheme its key was built under, and one registered Step per
// change recomputes the key of the rows of scheme N for scheme N+1 from their
// stored fields.
//
// The migration runs in the background after start-up, in batches, and can be
// stopped at any point: a row is either fully carried over (key and scheme in
// one conditional write) or left exactly as it was, still found by the lookup
// for its scheme. It never merges, deletes or re-parents a row; a row it cannot
// carry over safely is held at its scheme and reported.
package identitymigration

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/bgmigration"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
)

// Settings keys.
const (
	// SchemeSettingKey holds the identity scheme every stored row is known to
	// carry. It is written only when no row of an older scheme is left, and a
	// missing key means scheme 1. The rows' own scheme column is what lookups
	// and the migration go by; the marker spares a later start the count.
	SchemeSettingKey = "catalog.identity_scheme"
	// StatusSettingKey holds the outcome of the last run as JSON.
	StatusSettingKey = "catalog.identity_migration"
)

// States of the migration.
const (
	StateDisabled   = "disabled"
	StatePending    = "pending"
	StateRunning    = "running"
	StateWaiting    = "waiting_for_other_process"
	StateIncomplete = "incomplete"
	StateCompleted  = "completed"
	StateFailed     = "failed"
)

// Reasons a row is held at its scheme. A held row keeps its key and scheme,
// so the lookup still finds it exactly as before, and every later run looks at
// it again.
const (
	// HoldCollision: another track already carries the new key. Rewriting
	// would make the two rows one recording to the lookup, which is a merge
	// nobody decided on. Both rows stay; the owner can review them in the
	// duplicate review and delete one, after which the next run carries the
	// remaining row over.
	HoldCollision = "collision"
	// HoldNotDerivable: the stored key is not what the older scheme builds
	// from the row's stored fields (they changed after the key was written),
	// so the step cannot tell what the newer scheme would have written.
	HoldNotDerivable = "not_derivable"
	// HoldStepFailed: the step could not compute a key for the row.
	HoldStepFailed = "step_failed"
)

// maxRecordedHolds bounds the list kept in the status. The count is exact.
const maxRecordedHolds = 200

// DefaultBatchSize is the number of rows read at a time.
const DefaultBatchSize = 200

// errBusy means another backend process holds the migration lock.
var errBusy = errors.New("another backend process is running the identity migration")

// Row is a stored track as a step sees it.
type Row struct {
	ID          string
	Key         string
	Title       string
	Artists     []string
	ReleaseType music.ReleaseType
	// Guard identifies the stored fields the row was read with. A write only
	// applies while the row still matches it, so a row a download updated
	// in the meantime is left for the next run instead of being given a key
	// computed from fields it no longer has.
	Guard string
}

type outcomeKind int

const (
	outcomeKeep outcomeKind = iota
	outcomeRewrite
	outcomeHold
)

// Outcome is what a step decides for one row.
type Outcome struct {
	kind   outcomeKind
	key    string
	reason string
}

// Keep raises the row's scheme and leaves its key: it already is what the
// newer scheme builds.
func Keep() Outcome { return Outcome{kind: outcomeKeep} }

// Rewrite gives the row a new key and raises its scheme.
func Rewrite(key string) Outcome { return Outcome{kind: outcomeRewrite, key: key} }

// HoldRow leaves the row at its scheme for the given reason.
func HoldRow(reason string) Outcome { return Outcome{kind: outcomeHold, reason: reason} }

// Step is one identity change: from scheme From to From+1, recompute the key
// of a row from its stored fields.
type Step struct {
	From, To int
	// Name appears in the log and the status.
	Name string
	// Recompute must be a pure function of the row: the migration may run it
	// for the same row more than once (after a restart, for a held row).
	Recompute func(Row) Outcome
}

// WriteResult is what the store reports for one conditional write.
type WriteResult struct {
	// Applied is true when the row now carries the new key and scheme.
	Applied bool
	// ConflictID names another track that already carries the new key. The
	// row was not changed.
	ConflictID string
}

// Store is the part of the catalog the migration works on. The database
// repository implements it.
type Store interface {
	// CountIdentityRowsBelow counts the rows whose scheme is below scheme.
	CountIdentityRowsBelow(ctx context.Context, scheme int) (int, error)
	// ListIdentityRows returns up to limit rows of exactly scheme whose ID
	// sorts after afterID, ordered by ID.
	ListIdentityRows(ctx context.Context, scheme int, afterID string, limit int) ([]Row, error)
	// KeepIdentityKey raises the row from scheme from to to, leaving its key.
	// It reports false when the row no longer matches what was read.
	KeepIdentityKey(ctx context.Context, row Row, from, to int) (bool, error)
	// RewriteIdentityKey gives the row key and raises it from scheme from to
	// to, in one transaction that serialises with track writes on the same
	// keys. It writes nothing when another row carries key (ConflictID) or
	// the row no longer matches what was read (neither field set).
	RewriteIdentityKey(ctx context.Context, row Row, key string, from, to int) (WriteResult, error)
	// LockIdentityMigration takes a lock that excludes other backend
	// processes for the duration of a run. ok is false when another process
	// holds it.
	LockIdentityMigration(ctx context.Context) (unlock func(), ok bool, err error)
}

// Hold is one row the migration left at its scheme.
type Hold struct {
	TrackID string `json:"track_id"`
	Title   string `json:"title,omitempty"`
	Step    string `json:"step"`
	Reason  string `json:"reason"`
	// Key and NewKey are shown with " | " between artist, title and versions.
	Key                string `json:"key"`
	NewKey             string `json:"new_key,omitempty"`
	ConflictingTrackID string `json:"conflicting_track_id,omitempty"`
}

// Status reports the migration. The counters are those of the last run.
type Status struct {
	FromScheme int        `json:"from_scheme"`
	ToScheme   int        `json:"to_scheme"`
	State      string     `json:"state"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// Pending is the number of rows below ToScheme when the run started.
	Pending   int `json:"pending"`
	Examined  int `json:"examined"`
	Rewritten int `json:"rewritten"`
	Unchanged int `json:"unchanged"`
	Held      int `json:"held"`
	// Deferred counts rows that changed while the run looked at them; they
	// keep their scheme and are carried over by the next run.
	Deferred int `json:"deferred"`
	// Remaining is the number of rows below ToScheme when the run ended.
	Remaining int    `json:"remaining"`
	Holds     []Hold `json:"holds,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

func (s *Status) clone() *Status {
	cp := *s
	cp.Holds = append([]Hold(nil), s.Holds...)
	return &cp
}

// Options configures a Migrator.
type Options struct {
	// Enabled switches the background run on (the default). Switched off,
	// rows keep their scheme and the lookup keeps finding them by their
	// older forms.
	Enabled  bool
	Store    Store
	Settings bgmigration.Settings
	Logger   *slog.Logger
	// Steps defaults to Registered.
	Steps []Step
	// Target defaults to discography.IdentityScheme.
	Target        int
	BatchSize     int
	StartDelay    time.Duration
	RetryInterval time.Duration
}

// Migrator runs the registered steps.
type Migrator struct {
	opts   Options
	steps  []Step
	target int

	mu     sync.Mutex
	status atomic.Pointer[Status]

	wg       sync.WaitGroup
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// New checks the steps and returns a Migrator. The steps must form one
// unbroken chain from scheme 1 to the target, so that no row can be stranded
// at a scheme no step starts from.
func New(opts Options) (*Migrator, error) {
	if opts.Store == nil || opts.Settings == nil {
		return nil, apperr.New(apperr.CodeInternal, "The identity migration needs a store and a settings store.")
	}
	if opts.Steps == nil {
		opts.Steps = Registered
	}
	if opts.Target == 0 {
		opts.Target = discography.IdentityScheme
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if err := checkSteps(opts.Steps, opts.Target); err != nil {
		return nil, err
	}
	m := &Migrator{opts: opts, steps: opts.Steps, target: opts.Target}
	m.status.Store(&Status{FromScheme: discography.IdentitySchemeUnrecorded, ToScheme: opts.Target, State: StatePending})
	return m, nil
}

func checkSteps(steps []Step, target int) error {
	want := discography.IdentitySchemeUnrecorded
	for _, s := range steps {
		if s.From != want || s.To != s.From+1 || s.Recompute == nil || s.Name == "" {
			return apperr.Newf(apperr.CodeInternal,
				"Identity migration step %q goes from %d to %d; expected %d to %d.", s.Name, s.From, s.To, want, want+1)
		}
		want = s.To
	}
	if want != target {
		return apperr.Newf(apperr.CodeInternal,
			"The identity migration steps end at scheme %d, but tracks are written under scheme %d.", want, target)
	}
	return nil
}

// Start runs the migration in the background after the start delay. It
// returns at once.
func (m *Migrator) Start(ctx context.Context) {
	initial := m.loadStatus(ctx)
	if !m.opts.Enabled {
		if initial.State != StateCompleted {
			initial.State = StateDisabled
			m.opts.Logger.Info("the identity key migration is switched off; tracks stored by earlier versions are still found by their older keys",
				"to_scheme", m.target)
		}
		m.status.Store(initial)
		return
	}
	m.status.Store(initial)
	ctx, m.cancel = context.WithCancel(ctx)
	bgmigration.Schedule(ctx, &m.wg, m.opts.Logger, "identity key migration",
		m.opts.StartDelay, m.opts.RetryInterval, func(ctx context.Context) error {
			_, err := m.Run(ctx)
			return err
		})
}

// Stop ends a background run at the next row and waits for it. A row in
// flight is completed or left unchanged by its own transaction.
func (m *Migrator) Stop() {
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.wg.Wait()
	})
}

// Status returns a copy of the current status.
func (m *Migrator) Status() *Status {
	return m.status.Load().clone()
}

func (m *Migrator) loadStatus(ctx context.Context) *Status {
	st := &Status{FromScheme: discography.IdentitySchemeUnrecorded, ToScheme: m.target, State: StatePending}
	applied, ok := bgmigration.ReadScheme(ctx, m.opts.Settings, SchemeSettingKey)
	if ok {
		st.FromScheme = applied
	}
	var saved Status
	if bgmigration.LoadStatus(ctx, m.opts.Settings, StatusSettingKey, &saved) && saved.ToScheme == m.target {
		if saved.State == StateRunning || saved.State == StateWaiting {
			// The previous process stopped in the middle; it resumes.
			saved.State = StatePending
		}
		if saved.State == StateCompleted && !(ok && applied >= m.target) {
			saved.State = StatePending
		}
		return &saved
	}
	if ok && applied >= m.target {
		st.State = StateCompleted
	}
	return st
}

// run is the state of one run.
type run struct {
	status      *Status
	lastPersist time.Time
}

// Run carries every row over to the target scheme as far as it safely can. It
// is safe to call any number of times and from several processes: rows
// already carried over are not read again, held rows are looked at again, and
// a second process waits for the first.
//
// The error is non-nil when the run has to be repeated (database failure,
// another process running, context ended). Held rows are not an error: they
// need a decision, not a retry.
func (m *Migrator) Run(ctx context.Context) (*Status, error) {
	if !m.mu.TryLock() {
		return m.Status(), apperr.New(apperr.CodeConflict, "The identity migration is already running.")
	}
	defer m.mu.Unlock()

	from := discography.IdentitySchemeUnrecorded
	applied, markerOK := bgmigration.ReadScheme(ctx, m.opts.Settings, SchemeSettingKey)
	if markerOK {
		from = applied
	}

	// The marker alone is not trusted: a trash restore or an older binary
	// can write a row of an older scheme after a completed run.
	pending, err := m.opts.Store.CountIdentityRowsBelow(ctx, m.target)
	if err != nil {
		return m.fail(ctx, &run{status: m.newStatus(from)}, StateFailed, err)
	}
	if pending == 0 {
		prev := m.status.Load()
		if markerOK && applied >= m.target && prev.State == StateCompleted {
			return prev.clone(), nil
		}
		now := time.Now().UTC()
		r := &run{status: m.newStatus(from)}
		r.status.StartedAt, r.status.FinishedAt = &now, &now
		if err := bgmigration.WriteScheme(ctx, m.opts.Settings, SchemeSettingKey, m.target); err != nil {
			return m.fail(ctx, r, StateFailed, err)
		}
		r.status.State = StateCompleted
		m.persist(ctx, r, true)
		return m.Status(), nil
	}

	unlock, ok, err := m.opts.Store.LockIdentityMigration(ctx)
	if err != nil {
		return m.fail(ctx, &run{status: m.newStatus(from)}, StateFailed, err)
	}
	if !ok {
		r := &run{status: m.newStatus(from)}
		r.status.Pending = pending
		r.status.State = StateWaiting
		m.status.Store(r.status.clone())
		return m.Status(), errBusy
	}
	defer unlock()

	now := time.Now().UTC()
	r := &run{status: m.newStatus(from)}
	r.status.StartedAt = &now
	r.status.Pending = pending
	r.status.State = StateRunning
	m.opts.Logger.Info("identity key migration started", "to_scheme", m.target, "rows", pending)
	m.persist(ctx, r, true)

	for _, step := range m.steps {
		if err := m.runStep(ctx, r, step); err != nil {
			return m.fail(ctx, r, StateFailed, err)
		}
	}

	remaining, err := m.opts.Store.CountIdentityRowsBelow(ctx, m.target)
	if err != nil {
		return m.fail(ctx, r, StateFailed, err)
	}
	finished := time.Now().UTC()
	r.status.FinishedAt = &finished
	r.status.Remaining = remaining
	if remaining == 0 {
		if err := bgmigration.WriteScheme(ctx, m.opts.Settings, SchemeSettingKey, m.target); err != nil {
			return m.fail(ctx, r, StateFailed, err)
		}
		r.status.State = StateCompleted
	} else {
		r.status.State = StateIncomplete
	}
	m.persist(ctx, r, true)
	m.opts.Logger.Info("identity key migration finished",
		"state", r.status.State,
		"examined", r.status.Examined,
		"rewritten", r.status.Rewritten,
		"unchanged", r.status.Unchanged,
		"held", r.status.Held,
		"deferred", r.status.Deferred,
		"remaining", remaining)
	return m.Status(), nil
}

func (m *Migrator) newStatus(from int) *Status {
	return &Status{FromScheme: from, ToScheme: m.target, State: StatePending}
}

func (m *Migrator) fail(ctx context.Context, r *run, state string, err error) (*Status, error) {
	if ctx.Err() != nil {
		// A shutdown is not a failure; the next start resumes.
		state = StatePending
	}
	r.status.State = state
	r.status.LastError = err.Error()
	m.persist(ctx, r, true)
	if state == StateFailed {
		m.opts.Logger.Warn("identity key migration failed", logging.KeyError, err.Error())
	}
	return m.Status(), err
}

// runStep carries the rows of step.From over to step.To, one batch at a time.
// The cursor only moves forward, so a held row is not read twice in a run.
func (m *Migrator) runStep(ctx context.Context, r *run, step Step) error {
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := m.opts.Store.ListIdentityRows(ctx, step.From, after, m.opts.BatchSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			after = row.ID
			if err := m.carry(ctx, r, step, row); err != nil {
				return err
			}
		}
		m.persist(ctx, r, false)
		if len(rows) < m.opts.BatchSize {
			return nil
		}
	}
}

func (m *Migrator) carry(ctx context.Context, r *run, step Step, row Row) error {
	r.status.Examined++
	out := recompute(step, row)
	switch out.kind {
	case outcomeKeep:
		ok, err := m.opts.Store.KeepIdentityKey(ctx, row, step.From, step.To)
		if err != nil {
			return err
		}
		if ok {
			r.status.Unchanged++
		} else {
			r.status.Deferred++
		}
	case outcomeRewrite:
		res, err := m.opts.Store.RewriteIdentityKey(ctx, row, out.key, step.From, step.To)
		if err != nil {
			return err
		}
		switch {
		case res.Applied:
			r.status.Rewritten++
		case res.ConflictID != "":
			m.hold(r, step, row, HoldCollision, out.key, res.ConflictID)
		default:
			r.status.Deferred++
		}
	default:
		m.hold(r, step, row, out.reason, "", "")
	}
	return nil
}

// recompute runs the step and turns a broken step into a held row rather than
// a crashed backend or a wrong key.
func recompute(step Step, row Row) (out Outcome) {
	defer func() {
		if p := recover(); p != nil {
			out = HoldRow(HoldStepFailed)
		}
	}()
	out = step.Recompute(row)
	switch {
	case out.kind == outcomeRewrite && strings.TrimSpace(out.key) == "":
		return HoldRow(HoldStepFailed)
	case out.kind == outcomeRewrite && out.key == row.Key:
		return Keep()
	case out.kind == outcomeHold && out.reason == "":
		return HoldRow(HoldStepFailed)
	}
	return out
}

func (m *Migrator) hold(r *run, step Step, row Row, reason, newKey, conflict string) {
	r.status.Held++
	if len(r.status.Holds) >= maxRecordedHolds {
		// The count stays exact; the summary at the end of the run logs it.
		return
	}
	m.opts.Logger.Warn("identity key migration left a track at its older key",
		"track_id", row.ID, "step", step.Name, "reason", reason, "conflicting_track_id", conflict)
	r.status.Holds = append(r.status.Holds, Hold{
		TrackID:            row.ID,
		Title:              row.Title,
		Step:               step.Name,
		Reason:             reason,
		Key:                displayKey(row.Key),
		NewKey:             displayKey(newKey),
		ConflictingTrackID: conflict,
	})
}

func displayKey(key string) string { return strings.ReplaceAll(key, "\x1f", " | ") }

// persist publishes the status and stores it, at most every few seconds
// unless forced, so that a large library does not turn into a write per batch.
func (m *Migrator) persist(ctx context.Context, r *run, force bool) {
	m.status.Store(r.status.clone())
	now := time.Now()
	if !force && now.Sub(r.lastPersist) < 5*time.Second {
		return
	}
	r.lastPersist = now
	if err := bgmigration.SaveStatus(ctx, m.opts.Settings, StatusSettingKey, r.status); err != nil {
		m.opts.Logger.Warn("identity migration status could not be saved", logging.KeyError, err.Error())
	}
}
