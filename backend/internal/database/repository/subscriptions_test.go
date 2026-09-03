package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/subscriptions"
)

func newSubscription(provider, sourceID, name string) subscriptions.NewSubscription {
	return subscriptions.NewSubscription{
		Provider:       provider,
		ArtistSourceID: sourceID,
		ArtistName:     name,
	}
}

func TestSubscriptionCreateAndGet(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("the subscription got no id")
	}
	if created.LastSyncStatus != subscriptions.StatusPending {
		t.Fatalf("a new subscription should be pending, got %q", created.LastSyncStatus)
	}
	if !created.Enabled {
		t.Fatal("a new subscription should be enabled")
	}
	if created.AutoDownload {
		t.Fatal("auto download must be off unless it was asked for")
	}
	if created.LastSyncAt != nil {
		t.Fatal("a subscription that never ran must have no last sync time")
	}
	if created.NextSyncAt.IsZero() {
		t.Fatal("a new subscription must be due for a first sync")
	}

	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.ArtistName != "Daft Punk" || loaded.Provider != "deezer" || loaded.ArtistSourceID != "27" {
		t.Fatalf("the stored subscription does not match: %+v", loaded)
	}
}

// TestSubscriptionCreateIsIdempotent pins the rule from the specification:
// the same artist on the same provider must never produce a second row.
func TestSubscriptionCreateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	first, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk (Remastered)"))
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("a duplicate subscription created a second row: %q vs %q", first.ID, second.ID)
	}

	list, err := repo.List(ctx, subscriptions.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one subscription, got %d", len(list))
	}
}

// The same artist id on a different provider is a different artist and must
// be watchable on its own.
func TestSubscriptionCreateSeparatesProviders(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	if _, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk")); err != nil {
		t.Fatalf("deezer: %v", err)
	}
	if _, err := repo.Create(ctx, newSubscription("ytmusic", "27", "Someone Else")); err != nil {
		t.Fatalf("ytmusic: %v", err)
	}

	list, err := repo.List(ctx, subscriptions.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected two subscriptions, got %d", len(list))
	}
}

// Two concurrent subscribe requests for the same artist must still leave one
// row behind; the unique constraint has to be handled, not raced against.
func TestSubscriptionCreateIsRaceFree(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	const workers = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		ids   = make(map[string]struct{})
		fails []error
	)
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sub, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fails = append(fails, err)
				return
			}
			ids[sub.ID] = struct{}{}
		}()
	}
	close(start)
	wg.Wait()

	if len(fails) > 0 {
		t.Fatalf("concurrent subscribe failed: %v", fails[0])
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent subscribe produced %d distinct subscriptions", len(ids))
	}
}

func TestSubscriptionListFiltersByProvider(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	if _, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Create(ctx, newSubscription("ytmusic", "UC1", "Kevin MacLeod")); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := repo.List(ctx, subscriptions.ListFilter{Provider: "deezer"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Provider != "deezer" {
		t.Fatalf("the provider filter did not narrow the listing: %+v", list)
	}
}

func TestSubscriptionUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	on := true
	updated, err := repo.Update(ctx, created.ID, subscriptions.Update{AutoDownload: &on})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.AutoDownload {
		t.Fatal("auto download was not switched on")
	}
	if !updated.Enabled {
		t.Fatal("a field the update did not name must keep its value")
	}

	off := false
	updated, err = repo.Update(ctx, created.ID, subscriptions.Update{Enabled: &off})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Enabled {
		t.Fatal("the subscription was not disabled")
	}
	if !updated.AutoDownload {
		t.Fatal("disabling a subscription must not reset auto download")
	}
}

func TestSubscriptionReleaseFilterAndPriority(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	filter := music.ReleaseFilter{
		Albums: true, Singles: false, EPs: true, Live: true, Compilations: false, Remixes: false,
	}
	priority := jobs.PriorityHigh

	created, err := repo.Create(ctx, subscriptions.NewSubscription{
		Provider:         "deezer",
		ArtistSourceID:   "rf_test_1",
		ArtistName:       "Custom Filter Artist",
		ReleaseFilter:    &filter,
		DownloadPriority: &priority,
	})
	if err != nil {
		t.Fatalf("create subscription with custom filter: %v", err)
	}

	if created.DownloadPriority != jobs.PriorityHigh {
		t.Fatalf("expected priority high, got %s", created.DownloadPriority)
	}
	if !created.ReleaseFilter.Live || created.ReleaseFilter.Singles {
		t.Fatalf("unexpected release filter: %+v", created.ReleaseFilter)
	}

	// Update release filter and priority
	newFilter := music.ReleaseFilter{
		Albums: true, Singles: true, EPs: false, Live: false, Compilations: true, Remixes: true,
	}
	newPriority := jobs.PriorityNormal
	updated, err := repo.Update(ctx, created.ID, subscriptions.Update{
		ReleaseFilter:    &newFilter,
		DownloadPriority: &newPriority,
	})
	if err != nil {
		t.Fatalf("update subscription: %v", err)
	}
	if updated.DownloadPriority != jobs.PriorityNormal {
		t.Fatalf("expected priority normal, got %s", updated.DownloadPriority)
	}
	if !updated.ReleaseFilter.Compilations || !updated.ReleaseFilter.Remixes || updated.ReleaseFilter.EPs {
		t.Fatalf("unexpected updated release filter: %+v", updated.ReleaseFilter)
	}
}

func TestSubscriptionUpdateUnknownID(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	on := true
	_, err := repo.Update(ctx, "does-not-exist", subscriptions.Update{AutoDownload: &on})
	if apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("expected SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

func TestSubscriptionDelete(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("the subscription is still readable: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("deleting twice should report the missing subscription, got %v", err)
	}
}

func TestSubscriptionFindBySource(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	found, err := repo.FindBySource(ctx, "deezer", "27")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil || found.ID != created.ID {
		t.Fatalf("the subscription was not found by its provider identity: %+v", found)
	}

	missing, err := repo.FindBySource(ctx, "deezer", "999")
	if err != nil {
		t.Fatalf("find missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("an unwatched artist reported a subscription: %+v", missing)
	}
}

func TestSubscriptionRecordSyncClearsTheError(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	failedAt := time.Now().UTC().Truncate(time.Millisecond)
	err = repo.RecordSync(ctx, created.ID, subscriptions.SyncOutcome{
		At:     failedAt,
		NextAt: failedAt.Add(time.Hour),
		Status: subscriptions.StatusFailed,
		Error:  "Deezer is unavailable.",
	})
	if err != nil {
		t.Fatalf("record failure: %v", err)
	}

	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != subscriptions.StatusFailed {
		t.Fatalf("expected a failed status, got %q", loaded.LastSyncStatus)
	}
	if loaded.LastError != "Deezer is unavailable." {
		t.Fatalf("the error was not stored: %q", loaded.LastError)
	}
	if loaded.LastSyncAt == nil || !loaded.LastSyncAt.Equal(failedAt) {
		t.Fatalf("the sync time was not stored: %v", loaded.LastSyncAt)
	}

	okAt := failedAt.Add(2 * time.Hour)
	err = repo.RecordSync(ctx, created.ID, subscriptions.SyncOutcome{
		At:     okAt,
		NextAt: okAt.Add(24 * time.Hour),
		Status: subscriptions.StatusSuccess,
	})
	if err != nil {
		t.Fatalf("record success: %v", err)
	}

	loaded, err = repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != subscriptions.StatusSuccess {
		t.Fatalf("expected success, got %q", loaded.LastSyncStatus)
	}
	if loaded.LastError != "" {
		t.Fatalf("a successful run must clear the previous error, got %q", loaded.LastError)
	}
}

func TestSubscriptionRecordSyncUnknownID(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	err := repo.RecordSync(ctx, "does-not-exist", subscriptions.SyncOutcome{
		At: time.Now().UTC(), NextAt: time.Now().UTC(), Status: subscriptions.StatusSuccess,
	})
	if apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("expected SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

func TestSubscriptionListDueForSync(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))
	now := time.Now().UTC()

	due, err := repo.Create(ctx, newSubscription("deezer", "1", "Due"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	notDue, err := repo.Create(ctx, newSubscription("deezer", "2", "Not due"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	disabled, err := repo.Create(ctx, newSubscription("deezer", "3", "Disabled"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	mustRecord := func(id string, next time.Time) {
		t.Helper()
		if err := repo.RecordSync(ctx, id, subscriptions.SyncOutcome{
			At: now.Add(-time.Hour), NextAt: next, Status: subscriptions.StatusSuccess,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	mustRecord(due.ID, now.Add(-time.Minute))
	mustRecord(notDue.ID, now.Add(24*time.Hour))
	mustRecord(disabled.ID, now.Add(-time.Minute))

	off := false
	if _, err := repo.Update(ctx, disabled.ID, subscriptions.Update{Enabled: &off}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	list, err := repo.ListDueForSync(ctx, now, 10)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one due subscription, got %d: %+v", len(list), list)
	}
	if list[0].ID != due.ID {
		t.Fatalf("the wrong subscription was reported as due: %+v", list[0])
	}
}

// A freshly created subscription is due at once, so that subscribing leads to
// a first sync rather than to a day of silence.
func TestSubscriptionIsDueImmediatelyAfterCreation(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := repo.ListDueForSync(ctx, time.Now().UTC().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("a new subscription was not due: %+v", list)
	}
}

// The status column carries a CHECK constraint; it and the Go type must agree.
func TestSubscriptionStatusConstraintAcceptsEveryGoStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSubscriptions(db)

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now().UTC()

	for _, status := range []subscriptions.SyncStatus{
		subscriptions.StatusPending, subscriptions.StatusSuccess,
		subscriptions.StatusPartial, subscriptions.StatusFailed,
	} {
		if err := repo.RecordSync(ctx, created.ID, subscriptions.SyncOutcome{
			At: now, NextAt: now, Status: status,
		}); err != nil {
			t.Fatalf("status %q was rejected by the database: %v", status, err)
		}
	}

	_, err = db.ExecContext(ctx,
		`UPDATE artist_subscriptions SET last_sync_status = 'not-a-status' WHERE id = $1`, created.ID)
	if err == nil {
		t.Fatal("an unknown sync status was accepted")
	}
}

func TestSubscriptionTimestampsAreStoredInUTC(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, offset := created.CreatedAt.Zone(); offset != 0 {
		t.Fatalf("created_at is not UTC: %v", created.CreatedAt)
	}
	if _, offset := created.NextSyncAt.Zone(); offset != 0 {
		t.Fatalf("next_sync_at is not UTC: %v", created.NextSyncAt)
	}
}

func TestSubscriptionGetNotFound(t *testing.T) {
	repo := NewSubscriptions(openTestDB(t))
	_, err := repo.Get(context.Background(), "missing")
	if apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("expected SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("the error is not an application error: %v", err)
	}
}

// TestSubscriptionMigrationAppliesToAnExistingDatabase covers the upgrade path
// the release actually takes: a v0.5.0 database that already holds a
// catalogue and a job history, onto which 0002 is applied.
//
// The v0.5.0 state is reconstructed by removing what 0002 created — the table
// and its bookkeeping row — from a fully migrated database. What is left is
// byte for byte a v0.5.0 schema with live data in it.
func TestSubscriptionMigrationAppliesToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)
	jobsRepo := NewJobs(db)

	artist, err := catalog.UpsertArtist(ctx, music.Artist{
		Name: "Daft Punk", Provider: "deezer", SourceID: "27",
	})
	if err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	release, err := catalog.UpsertRelease(ctx, music.Release{
		Title: "Discovery", Provider: "deezer", SourceID: "302127",
		ReleaseType: music.ReleaseAlbum, Year: 2001,
	}, artist.ID)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	if _, err := catalog.UpsertTrack(ctx, music.Track{
		Title: "One More Time", Artists: []string{"Daft Punk"}, DurationMS: 320_000,
	}, release.ID, artist.ID, 4000); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	job := &jobs.Job{Type: jobs.TypeArtist, Status: jobs.StatusCompleted, Label: "Daft Punk"}
	if err := jobsRepo.Create(ctx, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	// Roll the schema back to v0.5.0.
	if _, err := db.ExecContext(ctx, `DROP TABLE artist_subscriptions`); err != nil {
		t.Fatalf("roll back the subscriptions table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version IN (2, 7)`); err != nil {
		t.Fatalf("roll back the migration record: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate a v0.5.0 database: %v", err)
	}

	// The new table is there and usable...
	repo := NewSubscriptions(db)
	sub, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create a subscription after the upgrade: %v", err)
	}
	if sub.ID == "" {
		t.Fatal("the subscription got no id")
	}

	// ...and nothing that was there before was lost.
	if _, err := catalog.GetArtist(ctx, artist.ID); err != nil {
		t.Fatalf("the artist did not survive the migration: %v", err)
	}
	if _, err := catalog.GetRelease(ctx, release.ID); err != nil {
		t.Fatalf("the release did not survive the migration: %v", err)
	}
	if _, err := jobsRepo.Get(ctx, job.ID); err != nil {
		t.Fatalf("the job did not survive the migration: %v", err)
	}

	var tracks int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tracks`).Scan(&tracks); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	if tracks != 1 {
		t.Fatalf("expected the seeded track to survive, found %d", tracks)
	}

	// Applying the migration a second time must stay a no-op.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("re-running the migration failed: %v", err)
	}
}

func TestSubscriptionListFiltersByArtist(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	wanted, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Create(ctx, newSubscription("deezer", "99", "Someone Else")); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := repo.List(ctx, subscriptions.ListFilter{Provider: "deezer", ArtistSourceID: "27"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != wanted.ID {
		t.Fatalf("the artist filter did not narrow the listing: %+v", list)
	}

	empty, err := repo.List(ctx, subscriptions.ListFilter{ArtistSourceID: "does-not-exist"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("an unwatched artist matched: %+v", empty)
	}
}

// TestSubscriptionStateSurvivesARestart closes the pool the way a shutdown
// does and opens a new one against the same schema. Everything the scheduler
// needs after a restart — the flags, the recorded outcome and above all
// next_sync_at — has to still be there.
func TestSubscriptionStateSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSubscriptions(db)

	created, err := repo.Create(ctx, newSubscription("deezer", "27", "Daft Punk"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	on := true
	if _, err := repo.Update(ctx, created.ID, subscriptions.Update{AutoDownload: &on}); err != nil {
		t.Fatalf("enable auto download: %v", err)
	}

	syncedAt := time.Now().UTC().Truncate(time.Millisecond)
	nextAt := syncedAt.Add(24 * time.Hour)
	if err := repo.RecordSync(ctx, created.ID, subscriptions.SyncOutcome{
		At: syncedAt, NextAt: nextAt, Status: subscriptions.StatusPartial,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// A new process reaches the same rows through a new repository. The pool
	// itself is left to the test cleanup, which is also what a real restart
	// does: the data outlives the connection, not the other way round.
	restarted := NewSubscriptions(db)

	loaded, err := restarted.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if !loaded.AutoDownload {
		t.Fatal("auto download did not survive the restart")
	}
	if !loaded.Enabled {
		t.Fatal("the enabled flag did not survive the restart")
	}
	if loaded.LastSyncStatus != subscriptions.StatusPartial {
		t.Fatalf("the recorded status did not survive: %q", loaded.LastSyncStatus)
	}
	if loaded.LastSyncAt == nil || !loaded.LastSyncAt.Equal(syncedAt) {
		t.Fatalf("the last sync time did not survive: %v", loaded.LastSyncAt)
	}
	if !loaded.NextSyncAt.Equal(nextAt) {
		t.Fatalf("next_sync_at did not survive: %v, want %v", loaded.NextSyncAt, nextAt)
	}

	// And the restarted scheduler must not treat it as due.
	due, err := restarted.ListDueForSync(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("list due after restart: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("the restart made a subscription due that is not: %+v", due)
	}

	// Once the interval has passed it is picked back up.
	due, err = restarted.ListDueForSync(ctx, nextAt.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("list due later: %v", err)
	}
	if len(due) != 1 || due[0].ID != created.ID {
		t.Fatalf("the overdue subscription was not picked up: %+v", due)
	}
}

// TestSubscriptionCreateKeepsTheBetterArtistDetails pins the rule that a
// second subscribe only ever improves what is stored. A request that names
// only the artist id must not replace a good name with the placeholder.
func TestSubscriptionCreateKeepsTheBetterArtistDetails(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	first, err := repo.Create(ctx, subscriptions.NewSubscription{
		Provider: "deezer", ArtistSourceID: "27",
		ArtistName: "Daft Punk", ArtistImageURL: "https://example.test/daft.jpg",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A request carrying no artist details at all.
	second, err := repo.Create(ctx, subscriptions.NewSubscription{
		Provider: "deezer", ArtistSourceID: "27",
	})
	if err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-subscribe produced a second row: %q vs %q", first.ID, second.ID)
	}
	if second.ArtistName != "Daft Punk" {
		t.Fatalf("the stored name was overwritten: %q", second.ArtistName)
	}
	if second.ArtistImageURL != "https://example.test/daft.jpg" {
		t.Fatalf("the stored image was overwritten: %q", second.ArtistImageURL)
	}

	// A request that does carry better details still updates them.
	third, err := repo.Create(ctx, subscriptions.NewSubscription{
		Provider: "deezer", ArtistSourceID: "27",
		ArtistName: "Daft Punk (Remastered)", ArtistImageURL: "https://example.test/new.jpg",
	})
	if err != nil {
		t.Fatalf("re-subscribe with details: %v", err)
	}
	if third.ArtistName != "Daft Punk (Remastered)" {
		t.Fatalf("a named request did not refresh the name: %q", third.ArtistName)
	}
	if third.ArtistImageURL != "https://example.test/new.jpg" {
		t.Fatalf("a named request did not refresh the image: %q", third.ArtistImageURL)
	}
}

// An artist that genuinely arrives without a name still gets the placeholder
// rather than an empty string, because the column is NOT NULL and the UI has
// to render something.
func TestSubscriptionCreateFallsBackToThePlaceholderName(t *testing.T) {
	ctx := context.Background()
	repo := NewSubscriptions(openTestDB(t))

	created, err := repo.Create(ctx, subscriptions.NewSubscription{
		Provider: "deezer", ArtistSourceID: "999",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ArtistName != music.UnknownArtist {
		t.Fatalf("expected the placeholder name, got %q", created.ArtistName)
	}
}

// TestUnfinishedJobLookupBacksTheDuplicateGuard covers the query the
// subscription queue relies on: a release whose job is still running must be
// recognisable, and one whose job has finished must not be.
func TestUnfinishedJobLookupBacksTheDuplicateGuard(t *testing.T) {
	ctx := context.Background()
	repo := NewJobs(openTestDB(t))

	running := &jobs.Job{
		Type: jobs.TypeRelease, Status: jobs.StatusQueued,
		TargetID: "302127", Options: jobs.DefaultOptions(),
	}
	if err := repo.Create(ctx, running); err != nil {
		t.Fatalf("create: %v", err)
	}
	finished := &jobs.Job{
		Type: jobs.TypeRelease, Status: jobs.StatusQueued,
		TargetID: "6982633", Options: jobs.DefaultOptions(),
	}
	if err := repo.Create(ctx, finished); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.SetStatus(ctx, finished.ID, jobs.StatusCompleted, "", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}

	unfinished, err := repo.ListUnfinished(ctx)
	if err != nil {
		t.Fatalf("list unfinished: %v", err)
	}

	targets := make(map[string]jobs.Type, len(unfinished))
	for _, job := range unfinished {
		targets[job.TargetID] = job.Type
	}
	if _, ok := targets["302127"]; !ok {
		t.Fatal("the running job was not reported as unfinished")
	}
	if _, ok := targets["6982633"]; ok {
		t.Fatal("a completed job was reported as unfinished")
	}
}
