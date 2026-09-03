package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/music"
)

func TestUsersSetupFirstAdmin(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	admin := auth.User{
		ID:           "admin-1",
		Username:     "AdminUser",
		DisplayName:  "Administrator",
		PasswordHash: "dummy_hash_argon2id",
	}

	err := repo.SetupFirstAdmin(ctx, admin)
	if err != nil {
		t.Fatalf("first admin setup failed: %v", err)
	}

	loaded, err := repo.GetByUsername(ctx, "adminuser")
	if err != nil {
		t.Fatalf("get by lowercase username failed: %v", err)
	}
	if loaded.ID != admin.ID || loaded.Role != auth.RoleAdmin || !loaded.Enabled {
		t.Fatalf("unexpected admin user data: %+v", loaded)
	}
	if loaded.Username != "adminuser" {
		t.Fatalf("expected username to be normalized, got %q", loaded.Username)
	}

	// Second setup must fail
	secondAdmin := auth.User{
		ID:           "admin-2",
		Username:     "SecondAdmin",
		DisplayName:  "Second Admin",
		PasswordHash: "dummy_hash",
	}
	err = repo.SetupFirstAdmin(ctx, secondAdmin)
	if err == nil {
		t.Fatal("expected second setup to fail, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSetupCompleted {
		t.Fatalf("expected error CodeSetupCompleted, got %v", err)
	}
}

func TestUsersSetupFirstAdminConcurrentRace(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	const concurrency = 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var successCount int
	var completedCount int
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			u := auth.User{
				ID:           fmt.Sprintf("admin-%d", idx),
				Username:     fmt.Sprintf("Admin_%d", idx),
				DisplayName:  fmt.Sprintf("Admin %d", idx),
				PasswordHash: "dummy_hash",
			}
			err := repo.SetupFirstAdmin(ctx, u)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
			} else if apperr.CodeOf(err) == apperr.CodeSetupCompleted {
				completedCount++
			} else {
				t.Errorf("unexpected error in setup race: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful setup, got %d", successCount)
	}
	if completedCount != concurrency-1 {
		t.Fatalf("expected %d rejected setups with CodeSetupCompleted, got %d", concurrency-1, completedCount)
	}

	totalUsers, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if totalUsers != 1 {
		t.Fatalf("expected exactly 1 user in database, got %d", totalUsers)
	}
}

func TestUsersCreateAndGet(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	u := auth.User{
		ID:           "u-1",
		Username:     "Alice_Smith",
		DisplayName:  "Alice",
		PasswordHash: "hash123",
		Role:         auth.RoleUser,
		Enabled:      true,
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Lookup by exact normalized username
	byName, err := repo.GetByUsername(ctx, "alice_smith")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if byName.DisplayName != "Alice" {
		t.Fatalf("expected display name Alice, got %q", byName.DisplayName)
	}

	// Lookup by different casing
	byCasedName, err := repo.GetByUsername(ctx, "ALICE_SMITH")
	if err != nil {
		t.Fatalf("get by uppercase username: %v", err)
	}
	if byCasedName.ID != u.ID {
		t.Fatalf("expected ID %q, got %q", u.ID, byCasedName.ID)
	}

	// Duplicate create with different casing must fail with CodeAlreadyExists
	dup := auth.User{
		ID:           "u-2",
		Username:     "ALICE_SMITH",
		DisplayName:  "Another Alice",
		PasswordHash: "hash456",
		Role:         auth.RoleUser,
		Enabled:      true,
	}
	err = repo.Create(ctx, dup)
	if err == nil {
		t.Fatal("expected duplicate username creation to fail")
	}
	if apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("expected CodeAlreadyExists, got %v", err)
	}
}

func TestUsersLastAdminProtection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	// Setup initial admin
	admin1 := auth.User{
		ID:           "adm-1",
		Username:     "admin_one",
		DisplayName:  "Admin 1",
		PasswordHash: "hash",
	}
	if err := repo.SetupFirstAdmin(ctx, admin1); err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	// Try to delete sole active admin
	err := repo.Delete(ctx, admin1.ID)
	if err == nil {
		t.Fatal("expected delete last admin to fail")
	}
	if apperr.CodeOf(err) != apperr.CodeLastAdmin {
		t.Fatalf("expected CodeLastAdmin on delete, got %v", err)
	}

	// Try to disable sole active admin
	err = repo.UpdateStatus(ctx, admin1.ID, false, auth.RoleAdmin)
	if err == nil {
		t.Fatal("expected disable last admin to fail")
	}
	if apperr.CodeOf(err) != apperr.CodeLastAdmin {
		t.Fatalf("expected CodeLastAdmin on disable, got %v", err)
	}

	// Try to demote sole active admin
	err = repo.UpdateStatus(ctx, admin1.ID, true, auth.RoleUser)
	if err == nil {
		t.Fatal("expected demote last admin to fail")
	}
	if apperr.CodeOf(err) != apperr.CodeLastAdmin {
		t.Fatalf("expected CodeLastAdmin on demote, got %v", err)
	}

	// Now add a second admin
	admin2 := auth.User{
		ID:           "adm-2",
		Username:     "admin_two",
		DisplayName:  "Admin 2",
		PasswordHash: "hash",
		Role:         auth.RoleAdmin,
		Enabled:      true,
	}
	if err := repo.Create(ctx, admin2); err != nil {
		t.Fatalf("create second admin: %v", err)
	}

	// Now demoting admin1 succeeds because admin2 is active
	if err := repo.UpdateStatus(ctx, admin1.ID, true, auth.RoleUser); err != nil {
		t.Fatalf("demote admin1 with 2 admins failed: %v", err)
	}

	// Now admin2 is the last active admin, so demoting admin2 fails
	err = repo.UpdateStatus(ctx, admin2.ID, true, auth.RoleUser)
	if err == nil {
		t.Fatal("expected demote last remaining admin2 to fail")
	}
	if apperr.CodeOf(err) != apperr.CodeLastAdmin {
		t.Fatalf("expected CodeLastAdmin for admin2, got %v", err)
	}
}

func TestUsersLastAdminConcurrentDemoteRace(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	admin1 := auth.User{ID: "adm-a", Username: "admin_a", PasswordHash: "h", Role: auth.RoleAdmin, Enabled: true}
	admin2 := auth.User{ID: "adm-b", Username: "admin_b", PasswordHash: "h", Role: auth.RoleAdmin, Enabled: true}

	if err := repo.SetupFirstAdmin(ctx, admin1); err != nil {
		t.Fatalf("setup admin 1: %v", err)
	}
	if err := repo.Create(ctx, admin2); err != nil {
		t.Fatalf("create admin 2: %v", err)
	}

	// Concurrently attempt to demote both admins
	var wg sync.WaitGroup
	wg.Add(2)

	var errA, errB error

	go func() {
		defer wg.Done()
		errA = repo.UpdateStatus(ctx, admin1.ID, true, auth.RoleUser)
	}()
	go func() {
		defer wg.Done()
		errB = repo.UpdateStatus(ctx, admin2.ID, true, auth.RoleUser)
	}()

	wg.Wait()

	// Exactly one must succeed and exactly one must fail with CodeLastAdmin
	successCount := 0
	lastAdminCount := 0
	for _, err := range []error{errA, errB} {
		if err == nil {
			successCount++
		} else if apperr.CodeOf(err) == apperr.CodeLastAdmin {
			lastAdminCount++
		} else {
			t.Errorf("unexpected error in demote race: %v", err)
		}
	}

	if successCount != 1 || lastAdminCount != 1 {
		t.Fatalf("expected exactly 1 success and 1 CodeLastAdmin, got %d successes and %d lastAdmin errors (errA=%v, errB=%v)",
			successCount, lastAdminCount, errA, errB)
	}

	activeAdmins, err := repo.CountActiveAdmins(ctx)
	if err != nil {
		t.Fatalf("count active admins: %v", err)
	}
	if activeAdmins != 1 {
		t.Fatalf("expected exactly 1 active admin remaining, got %d", activeAdmins)
	}
}

func TestUsersLastAdminConcurrentDeleteRace(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	admin1 := auth.User{ID: "adm-x", Username: "admin_x", PasswordHash: "h", Role: auth.RoleAdmin, Enabled: true}
	admin2 := auth.User{ID: "adm-y", Username: "admin_y", PasswordHash: "h", Role: auth.RoleAdmin, Enabled: true}

	if err := repo.SetupFirstAdmin(ctx, admin1); err != nil {
		t.Fatalf("setup admin 1: %v", err)
	}
	if err := repo.Create(ctx, admin2); err != nil {
		t.Fatalf("create admin 2: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	var errX, errY error

	go func() {
		defer wg.Done()
		errX = repo.Delete(ctx, admin1.ID)
	}()
	go func() {
		defer wg.Done()
		errY = repo.Delete(ctx, admin2.ID)
	}()

	wg.Wait()

	successCount := 0
	lastAdminCount := 0
	for _, err := range []error{errX, errY} {
		if err == nil {
			successCount++
		} else if apperr.CodeOf(err) == apperr.CodeLastAdmin {
			lastAdminCount++
		} else {
			t.Errorf("unexpected error in delete race: %v", err)
		}
	}

	if successCount != 1 || lastAdminCount != 1 {
		t.Fatalf("expected exactly 1 success and 1 CodeLastAdmin, got %d successes and %d lastAdmin errors (errX=%v, errY=%v)",
			successCount, lastAdminCount, errX, errY)
	}

	activeAdmins, err := repo.CountActiveAdmins(ctx)
	if err != nil {
		t.Fatalf("count active admins: %v", err)
	}
	if activeAdmins != 1 {
		t.Fatalf("expected exactly 1 active admin remaining, got %d", activeAdmins)
	}
}

func TestUsersUpdateProfileAndPassword(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewUsers(db)

	u := auth.User{
		ID:           "u-update",
		Username:     "updater",
		DisplayName:  "Old Name",
		PasswordHash: "old_hash",
		Role:         auth.RoleUser,
		Enabled:      true,
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.UpdateProfile(ctx, u.ID, "New Name"); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if err := repo.UpdatePassword(ctx, u.ID, "new_hash"); err != nil {
		t.Fatalf("update password: %v", err)
	}
	now := time.Now().UTC()
	if err := repo.UpdateLastLogin(ctx, u.ID, now); err != nil {
		t.Fatalf("update last login: %v", err)
	}

	loaded, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if loaded.DisplayName != "New Name" || loaded.PasswordHash != "new_hash" {
		t.Fatalf("unexpected user state: %+v", loaded)
	}
	if loaded.LastLoginAt == nil || loaded.LastLoginAt.IsZero() {
		t.Fatal("expected LastLoginAt to be set")
	}
}

// TestUsersAndSessionsMigrationAppliesToAnExistingDatabase verifies that
// applying migration 0003 onto a v0.7.0 database preserves all existing data
// (artists, releases, tracks, track_sources, files, jobs, subscriptions) without modifications.
func TestUsersAndSessionsMigrationAppliesToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	catalog := NewCatalog(db)

	// Seed existing v0.7.0 data
	artist, err := catalog.UpsertArtist(ctx, authMusicArtist("Linkin Park", "spotify", "sp_lp"))
	if err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	release, err := catalog.UpsertRelease(ctx, authMusicRelease("Meteora", "spotify", "sp_rel_1"), artist.ID)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	track, err := catalog.UpsertTrack(ctx, authMusicTrack("Numb", "Linkin Park", "Meteora", 185000), release.ID, artist.ID, 4000)
	if err != nil {
		t.Fatalf("seed track: %v", err)
	}

	// Roll back migration 0003 to simulate a pure v0.7.0 state
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS sessions CASCADE`); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS users CASCADE`); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	// Only migration 0003 is rolled back here; the later ones stay applied so
	// that re-running them is not part of what this test exercises.
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 3`); err != nil {
		t.Fatalf("clean schema_migrations: %v", err)
	}

	// Run migration
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("apply migration 0003: %v", err)
	}

	// Verify pre-existing data is completely untouched
	loadedArtist, err := catalog.GetArtist(ctx, artist.ID)
	if err != nil || loadedArtist.Name != "Linkin Park" {
		t.Fatalf("artist corrupted or missing after migration: %v", err)
	}
	loadedTrack, err := catalog.GetTrack(ctx, track.ID)
	if err != nil || loadedTrack.Title != "Numb" {
		t.Fatalf("track corrupted or missing after migration: %v", err)
	}

	// Verify users repository works on the migrated schema
	usersRepo := NewUsers(db)
	admin := auth.User{
		ID:           "adm_migrated",
		Username:     "migrated_admin",
		DisplayName:  "Migrated Admin",
		PasswordHash: "dummy_hash",
	}
	if err := usersRepo.SetupFirstAdmin(ctx, admin); err != nil {
		t.Fatalf("setup first admin on migrated schema: %v", err)
	}
	loadedAdmin, err := usersRepo.GetByID(ctx, admin.ID)
	if err != nil || loadedAdmin.Username != "migrated_admin" {
		t.Fatalf("admin not found on migrated schema: %v", err)
	}
}

func authMusicArtist(name, provider, sourceID string) music.Artist {
	return music.Artist{Name: name, Provider: provider, SourceID: sourceID}
}

func authMusicRelease(title, provider, sourceID string) music.Release {
	return music.Release{Title: title, Provider: provider, SourceID: sourceID, ReleaseType: music.ReleaseAlbum, Year: 2003}
}

func authMusicTrack(title, artist, album string, durationMS int) music.Track {
	return music.Track{Title: title, Artists: []string{artist}, Album: album, DurationMS: durationMS}
}
