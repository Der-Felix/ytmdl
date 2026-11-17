package repository

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/identitymigration"
	"ytdm/backend/internal/music"
)

func identityScheme(t *testing.T, c *Catalog, id string) (string, int) {
	t.Helper()
	var (
		key    string
		scheme int
	)
	if err := c.db.QueryRowContext(context.Background(),
		`SELECT identity_key, identity_scheme FROM tracks WHERE id = $1`, id).Scan(&key, &scheme); err != nil {
		t.Fatal(err)
	}
	return key, scheme
}

// asOlderVersion turns a stored row into what versions up to 1.3.0 wrote: the
// key of the earlier remix title reading, no scheme recorded.
func asOlderVersion(t *testing.T, c *Catalog, id, title, key string) {
	t.Helper()
	if _, err := c.db.ExecContext(context.Background(),
		`UPDATE tracks SET title = $2, identity_key = $3, identity_scheme = 1 WHERE id = $1`, id, title, key); err != nil {
		t.Fatal(err)
	}
}

func runIdentityMigration(t *testing.T, c *Catalog) *identitymigration.Status {
	t.Helper()
	m, err := identitymigration.New(identitymigration.Options{
		Enabled: true, Store: c, Settings: NewSettings(c.db),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMigration0019RecordsTheIdentityScheme(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	var def string
	if err := db.QueryRowContext(ctx, `SELECT column_default FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'tracks' AND column_name = 'identity_scheme'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if def != "1" {
		t.Fatalf("identity_scheme default = %q, want 1 so that a row of unknown origin is examined", def)
	}

	stored, err := c.UpsertTrack(ctx, music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if _, scheme := identityScheme(t, c, stored.ID); scheme != discography.IdentityScheme {
		t.Fatalf("a new row is written under scheme %d, want %d", scheme, discography.IdentityScheme)
	}
}

// The legacy case the framework was built for: rows written by versions up to
// 1.3.0 carry the key of the earlier reading of an unbracketed remix title.
// They are found while the migration has not reached them, carried over by
// it, and found by the current key afterwards; the older key form no longer
// matches a migrated row.
func TestIdentityMigrationCarriesOverTheEarlierRemixReading(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	remix := music.Track{Title: "Blinding Lights Major Lazer Remix", Artists: []string{"The Weeknd"}, DurationMS: 200000}
	older := discography.IdentityOf(remix).OlderSchemeKeys
	if len(older) != 1 {
		t.Fatalf("fixture: older keys %q", older)
	}

	oldRow, err := c.UpsertTrack(ctx, remix, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	asOlderVersion(t, c, oldRow.ID, remix.Title, older[0])
	newForm := music.Track{Title: "Save Your Tears", Artists: []string{"The Weeknd"}, DurationMS: 215000}
	newRow, err := c.UpsertTrack(ctx, newForm, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// A row of 1.3.1 to 1.5.0: current key, no scheme recorded.
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET identity_scheme = 1 WHERE id = $1`, newRow.ID); err != nil {
		t.Fatal(err)
	}

	found := func(stage string, track music.Track, want string) {
		t.Helper()
		got, err := c.FindTrack(ctx, track, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.ID != want {
			t.Errorf("%s: found %v, want %s", stage, got, want)
		}
	}
	found("before the migration", remix, oldRow.ID)
	found("before the migration", newForm, newRow.ID)

	st := runIdentityMigration(t, c)
	if st.State != identitymigration.StateCompleted || st.Rewritten != 1 || st.Unchanged != 1 || st.Held != 0 {
		t.Fatalf("status %+v", st)
	}
	if key, scheme := identityScheme(t, c, oldRow.ID); key != discography.IdentityKey(remix) || scheme != discography.IdentityScheme {
		t.Fatalf("old form row = %q at %d", key, scheme)
	}
	if key, scheme := identityScheme(t, c, newRow.ID); key != discography.IdentityKey(newForm) || scheme != discography.IdentityScheme {
		t.Fatalf("new form row = %q at %d", key, scheme)
	}
	found("after the migration", remix, oldRow.ID)
	found("after the migration", newForm, newRow.ID)

	// A second run changes nothing.
	if again := runIdentityMigration(t, c); again.State != identitymigration.StateCompleted {
		t.Fatalf("second run %+v", again)
	}
	var marker string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, identitymigration.SchemeSettingKey).Scan(&marker); err != nil || marker == "" {
		t.Fatalf("marker %q, %v", marker, err)
	}
}

// The earlier reading of one title can be the current key of another: "Blinding
// Lights Major Lazer Remix" was read as "blinding" + remix, which is what
// "Blinding (Remix)" is now. Only a row that has not been migrated may be
// found by the older form.
func TestOlderKeyFormsOnlyMatchUnmigratedRows(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	remix := music.Track{Title: "Blinding Lights Major Lazer Remix", Artists: []string{"The Weeknd"}, DurationMS: 200000}
	other := music.Track{Title: "Blinding (Remix)", Artists: []string{"The Weeknd"}, DurationMS: 200000}
	if discography.IdentityKey(other) != discography.IdentityOf(remix).OlderSchemeKeys[0] {
		t.Fatalf("fixture: %q is not the older key of %q", discography.IdentityKey(other), remix.Title)
	}
	stored, err := c.UpsertTrack(ctx, other, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.FindTrack(ctx, remix, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a migrated row of another title was found by an older key form: %s", got.ID)
	}

	if _, err := db.ExecContext(ctx, `UPDATE tracks SET identity_scheme = 1 WHERE id = $1`, stored.ID); err != nil {
		t.Fatal(err)
	}
	got, err = c.FindTrack(ctx, remix, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != stored.ID {
		t.Fatalf("an unmigrated row must still be found by the older key form, as before (got %v)", got)
	}
}

// A row whose new key another track already carries is held: both rows stay,
// with their keys, and the status names both.
func TestIdentityMigrationHoldsACollision(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	remix := music.Track{Title: "Blinding Lights Major Lazer Remix", Artists: []string{"The Weeknd"}, DurationMS: 200000}
	current, err := c.UpsertTrack(ctx, remix, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	// The same title with a different runtime: a second row, as an earlier
	// version stored it.
	longer := remix
	longer.DurationMS = 260000
	legacy, err := c.UpsertTrack(ctx, longer, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ID == current.ID {
		t.Fatal("fixture: the rows must be distinct")
	}
	olderKey := discography.IdentityOf(remix).OlderSchemeKeys[0]
	asOlderVersion(t, c, legacy.ID, remix.Title, olderKey)

	st := runIdentityMigration(t, c)
	if st.State != identitymigration.StateIncomplete || st.Held != 1 || st.Remaining != 1 || len(st.Holds) != 1 {
		t.Fatalf("status %+v", st)
	}
	h := st.Holds[0]
	if h.TrackID != legacy.ID || h.ConflictingTrackID != current.ID || h.Reason != identitymigration.HoldCollision {
		t.Fatalf("hold %+v", h)
	}
	if key, scheme := identityScheme(t, c, legacy.ID); key != olderKey || scheme != discography.IdentitySchemeUnrecorded {
		t.Fatalf("held row = %q at %d, want it unchanged", key, scheme)
	}
	if key, _ := identityScheme(t, c, current.ID); key != discography.IdentityKey(remix) {
		t.Fatalf("other row = %q", key)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d tracks, want both (%v)", n, err)
	}
	// The held row is still found as before.
	got, err := c.FindTrack(ctx, longer, 4000)
	if err != nil || got == nil || got.ID != legacy.ID {
		t.Fatalf("held row not found: %v, %v", got, err)
	}

	// Once the owner deletes the other row, the next run carries it over.
	if err := c.DeleteTrack(ctx, current.ID); err != nil {
		t.Fatal(err)
	}
	if st := runIdentityMigration(t, c); st.State != identitymigration.StateCompleted || st.Rewritten != 1 {
		t.Fatalf("after the delete: %+v", st)
	}
}

func TestIdentityMigrationLockExcludesASecondProcess(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	unlock, ok, err := c.LockIdentityMigration(ctx)
	if err != nil || !ok {
		t.Fatalf("first lock: %v %v", ok, err)
	}
	if _, ok, err := c.LockIdentityMigration(ctx); err != nil || ok {
		t.Fatalf("second lock taken while the first is held: %v %v", ok, err)
	}
	unlock()
	again, ok, err := c.LockIdentityMigration(ctx)
	if err != nil || !ok {
		t.Fatalf("lock after release: %v %v", ok, err)
	}
	again()
}

// A track trashed before schema 19 has no identity_scheme in its payload. It
// is restored as scheme 1, not refused by the NOT NULL column.
func TestTrashRestoreOfAnOlderPayloadGetsSchemeOne(t *testing.T) {
	db := openTestDB(t)
	c := NewCatalog(db)
	ctx := context.Background()

	stored, err := c.UpsertTrack(ctx, music.Track{Title: "Song", Artists: []string{"Artist"}, DurationMS: 200000}, "", "", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO library_trash (id, track_id, title, state, payload, moves)
		SELECT 'e1', t.id, t.title, 'restoring',
			jsonb_build_object('track', to_jsonb(t) - 'identity_scheme', 'files', '[]'::jsonb,
				'sources', '[]'::jsonb, 'overrides', '[]'::jsonb, 'favorites', '[]'::jsonb,
				'playlists', '[]'::jsonb),
			'[]'::jsonb
		FROM tracks t WHERE t.id = $1`, stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, stored.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.RestoreTrashMetadata(ctx, &TrashEntry{ID: "e1", TrackID: stored.ID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, scheme := identityScheme(t, c, stored.ID); scheme != discography.IdentitySchemeUnrecorded {
		t.Fatalf("restored row at scheme %d, want %d", scheme, discography.IdentitySchemeUnrecorded)
	}
}
