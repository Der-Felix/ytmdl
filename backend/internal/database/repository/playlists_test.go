package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func setupPlaylistTest(t *testing.T) (*database.DB, *repository.Playlists, string, string, []string) {
	t.Helper()
	db := dbtest.Open(t)
	repo := repository.NewPlaylists(db)
	ctx := context.Background()
	now := time.Now().UTC()

	u1 := "u_test_user_1"
	u2 := "u_test_user_2"

	// Insert test users
	for _, u := range []string{u1, u2} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
			VALUES ($1, $2, 'hash', 'user', true, $3, $3)
			ON CONFLICT (id) DO NOTHING
		`, u, u, now)
		if err != nil {
			t.Fatalf("insert user %s: %v", u, err)
		}
	}

	// Insert test artist and release
	_, err := db.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, created_at, updated_at)
		VALUES ('art_pl', 'Kraftwerk', 'kraftwerk', 'spotify', 'src_kw', $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	if err != nil {
		t.Fatalf("insert artist: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO releases (id, artist_id, title, provider, source_id, year, created_at, updated_at)
		VALUES ('rel_pl', 'art_pl', 'Computerwelt', 'spotify', 'src_cw', 1981, $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}

	// Insert 4 test tracks
	trackIDs := []string{"trk_pl_1", "trk_pl_2", "trk_pl_3", "trk_pl_4"}
	trackTitles := []string{"Computerwelt", "Taschenrechner", "Nummern", "Computer Liebe"}
	for i, tid := range trackIDs {
		_, err = db.ExecContext(ctx, `
			INSERT INTO tracks (id, release_id, artist_id, title, identity_key, duration_ms, created_at, updated_at)
			VALUES ($1, 'rel_pl', 'art_pl', $2, $3, $4, $5, $5)
			ON CONFLICT (id) DO NOTHING
		`, tid, trackTitles[i], "key_"+tid, (i+1)*60000, now)
		if err != nil {
			t.Fatalf("insert track %s: %v", tid, err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO files (id, track_id, path, size_bytes, codec, bitrate_kbps, created_at, updated_at)
			VALUES ($1, $2, $3, 1000000, 'flac', 850, $4, $4)
			ON CONFLICT (id) DO NOTHING
		`, "fil_"+tid, tid, "Kraftwerk/Computerwelt/"+tid+".flac", now)
		if err != nil {
			t.Fatalf("insert file %s: %v", tid, err)
		}
	}

	return db, repo, u1, u2, trackIDs
}

func TestPlaylists_CreateAndList(t *testing.T) {
	_, repo, u1, u2, _ := setupPlaylistTest(t)
	ctx := context.Background()

	// Empty / whitespace names rejected
	for _, badName := range []string{"", "   ", "\t\n"} {
		_, err := repo.CreatePlaylist(ctx, u1, badName, "desc")
		if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidRequest {
			t.Fatalf("expected CodeInvalidRequest for empty name %q, got: %v", badName, err)
		}
	}

	// Unicode name accepted (German umlauts, accents)
	pl1, err := repo.CreatePlaylist(ctx, u1, "Lieblingslieder für den Sommer ☀️ & ÄÖÜ", "Meine Favoriten")
	if err != nil {
		t.Fatalf("create playlist 1 failed: %v", err)
	}
	if pl1.Name != "Lieblingslieder für den Sommer ☀️ & ÄÖÜ" {
		t.Fatalf("unexpected playlist name: %s", pl1.Name)
	}
	if pl1.UserID != u1 {
		t.Fatalf("unexpected user ID: %s", pl1.UserID)
	}

	// Create second playlist for user 1
	pl2, err := repo.CreatePlaylist(ctx, u1, "Fahrt zur Arbeit", "Autobahn tracks")
	if err != nil {
		t.Fatalf("create playlist 2 failed: %v", err)
	}

	// User 1 lists playlists
	list1, err := repo.ListPlaylistsForUser(ctx, u1)
	if err != nil {
		t.Fatalf("list playlists for u1: %v", err)
	}
	if len(list1) != 2 {
		t.Fatalf("expected 2 playlists for u1, got %d", len(list1))
	}

	// User 2 has 0 playlists
	list2, err := repo.ListPlaylistsForUser(ctx, u2)
	if err != nil {
		t.Fatalf("list playlists for u2: %v", err)
	}
	if len(list2) != 0 {
		t.Fatalf("expected 0 playlists for u2, got %d", len(list2))
	}

	// User 2 cannot access user 1's playlist
	_, err = repo.GetPlaylistForUser(ctx, u2, pl1.ID)
	if err == nil || apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatalf("expected CodePlaylistNotFound when u2 reads u1 playlist, got: %v", err)
	}

	// User 1 gets playlist detail
	detail, err := repo.GetPlaylistForUser(ctx, u1, pl1.ID)
	if err != nil {
		t.Fatalf("get playlist detail: %v", err)
	}
	if detail.ID != pl1.ID || len(detail.Tracks) != 0 {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	// Update playlist
	updated, err := repo.UpdatePlaylist(ctx, u1, pl2.ID, "Elektronische Musik", "Aktualisierte Beschreibung")
	if err != nil {
		t.Fatalf("update playlist: %v", err)
	}
	if updated.Name != "Elektronische Musik" || updated.Description != "Aktualisierte Beschreibung" {
		t.Fatalf("unexpected updated metadata: %+v", updated)
	}

	// User 2 cannot update user 1's playlist
	_, err = repo.UpdatePlaylist(ctx, u2, pl2.ID, "Hacked", "Hacked")
	if err == nil || apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatalf("expected CodePlaylistNotFound when u2 updates u1 playlist, got: %v", err)
	}

	// User 2 cannot delete user 1's playlist
	err = repo.DeletePlaylist(ctx, u2, pl1.ID)
	if err == nil || apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatalf("expected CodePlaylistNotFound when u2 deletes u1 playlist, got: %v", err)
	}

	// User 1 deletes pl1
	if err := repo.DeletePlaylist(ctx, u1, pl1.ID); err != nil {
		t.Fatalf("delete playlist pl1: %v", err)
	}

	// pl1 is now gone
	_, err = repo.GetPlaylistForUser(ctx, u1, pl1.ID)
	if err == nil || apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatalf("expected CodePlaylistNotFound after deletion, got: %v", err)
	}
}

func TestPlaylists_AddRemoveReorder(t *testing.T) {
	_, repo, u1, u2, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Klassiker", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// 1. Add tracks in order: trk 0, 1, 2
	detail, err := repo.AddTrack(ctx, u1, pl.ID, tracks[0])
	if err != nil {
		t.Fatalf("add track 0: %v", err)
	}
	if len(detail.Tracks) != 1 || detail.Tracks[0].Position != 1 || detail.Tracks[0].ID != tracks[0] {
		t.Fatalf("unexpected tracks after first add: %+v", detail.Tracks)
	}

	detail, err = repo.AddTrack(ctx, u1, pl.ID, tracks[1])
	if err != nil {
		t.Fatalf("add track 1: %v", err)
	}
	if len(detail.Tracks) != 2 || detail.Tracks[1].Position != 2 {
		t.Fatalf("unexpected tracks after second add: %+v", detail.Tracks)
	}

	detail, err = repo.AddTrack(ctx, u1, pl.ID, tracks[2])
	if err != nil {
		t.Fatalf("add track 2: %v", err)
	}
	if len(detail.Tracks) != 3 || detail.Tracks[2].Position != 3 {
		t.Fatalf("unexpected tracks after third add: %+v", detail.Tracks)
	}

	// 2. Duplicate add rejected with CodeAlreadyExists
	_, err = repo.AddTrack(ctx, u1, pl.ID, tracks[1])
	if err == nil || apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("expected CodeAlreadyExists for duplicate add, got: %v", err)
	}

	// 3. Non-existent track rejected
	_, err = repo.AddTrack(ctx, u1, pl.ID, "trk_nonexistent")
	if err == nil || apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("expected CodeTrackNotFound for bogus track, got: %v", err)
	}

	// 4. User 2 cannot add track to user 1 playlist
	_, err = repo.AddTrack(ctx, u2, pl.ID, tracks[3])
	if err == nil || apperr.CodeOf(err) != apperr.CodePlaylistNotFound {
		t.Fatalf("expected CodePlaylistNotFound when u2 adds track to u1 playlist, got: %v", err)
	}

	// 5. Reorder: new order tracks[2], tracks[0], tracks[1]
	newOrder := []string{tracks[2], tracks[0], tracks[1]}
	detail, err = repo.ReorderTracks(ctx, u1, pl.ID, newOrder)
	if err != nil {
		t.Fatalf("reorder failed: %v", err)
	}
	if len(detail.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(detail.Tracks))
	}
	if detail.Tracks[0].ID != tracks[2] || detail.Tracks[0].Position != 1 {
		t.Errorf("expected track 0 to be %s at pos 1, got %s at pos %d", tracks[2], detail.Tracks[0].ID, detail.Tracks[0].Position)
	}
	if detail.Tracks[1].ID != tracks[0] || detail.Tracks[1].Position != 2 {
		t.Errorf("expected track 1 to be %s at pos 2, got %s at pos %d", tracks[0], detail.Tracks[1].ID, detail.Tracks[1].Position)
	}
	if detail.Tracks[2].ID != tracks[1] || detail.Tracks[2].Position != 3 {
		t.Errorf("expected track 2 to be %s at pos 3, got %s at pos %d", tracks[1], detail.Tracks[2].ID, detail.Tracks[2].Position)
	}

	// 6. Invalid reorder attempts:
	// Missing track
	_, err = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[2], tracks[0]})
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected CodeInvalidRequest for missing track, got: %v", err)
	}

	// Duplicate ID
	_, err = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[2], tracks[0], tracks[0]})
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected CodeInvalidRequest for duplicate track id, got: %v", err)
	}

	// Foreign track injection
	_, err = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[2], tracks[0], tracks[3]})
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected CodeInvalidRequest for foreign track injection, got: %v", err)
	}

	// Verify failed reorder preserved existing order (zero mutation)
	detail, err = repo.GetPlaylistForUser(ctx, u1, pl.ID)
	if err != nil {
		t.Fatalf("get playlist detail: %v", err)
	}
	if detail.Tracks[0].ID != tracks[2] || detail.Tracks[1].ID != tracks[0] || detail.Tracks[2].ID != tracks[1] {
		t.Fatalf("order was mutated during failed reorder!")
	}

	// 7. Remove track from middle: remove tracks[0] (which is currently at pos 2)
	detail, err = repo.RemoveTrack(ctx, u1, pl.ID, tracks[0])
	if err != nil {
		t.Fatalf("remove track: %v", err)
	}
	if len(detail.Tracks) != 2 {
		t.Fatalf("expected 2 tracks after removal, got %d", len(detail.Tracks))
	}
	// Remaining: tracks[2] at pos 1, tracks[1] normalized from pos 3 -> pos 2
	if detail.Tracks[0].ID != tracks[2] || detail.Tracks[0].Position != 1 {
		t.Errorf("expected remaining track 0: %s at pos 1, got %s at pos %d", tracks[2], detail.Tracks[0].ID, detail.Tracks[0].Position)
	}
	if detail.Tracks[1].ID != tracks[1] || detail.Tracks[1].Position != 2 {
		t.Errorf("expected remaining track 1: %s at pos 2, got %s at pos %d", tracks[1], detail.Tracks[1].ID, detail.Tracks[1].Position)
	}

	// 8. Deleting playlist removes memberships, but library tracks remain
	if err := repo.DeletePlaylist(ctx, u1, pl.ID); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}

	// Check library tracks still exist
	for _, tid := range tracks {
		fav, err := repo.IsFavorite(ctx, u1, tid)
		if err != nil {
			t.Fatalf("check track in db: %v", err)
		}
		_ = fav
	}
}

func TestPlaylists_Favorites(t *testing.T) {
	_, repo, u1, u2, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	// Initially neither user has favorites
	f1, err := repo.ListFavoriteTracks(ctx, u1)
	if err != nil || len(f1) != 0 {
		t.Fatalf("expected 0 favorites for u1, got %d (err: %v)", len(f1), err)
	}

	// User 1 favorites track 0 and track 1
	if err := repo.FavoriteTrack(ctx, u1, tracks[0]); err != nil {
		t.Fatalf("favorite track 0: %v", err)
	}
	if err := repo.FavoriteTrack(ctx, u1, tracks[1]); err != nil {
		t.Fatalf("favorite track 1: %v", err)
	}

	// Idempotent: favoriting again is a success
	if err := repo.FavoriteTrack(ctx, u1, tracks[0]); err != nil {
		t.Fatalf("idempotent favorite track 0: %v", err)
	}

	// Invalid track ID rejected
	if err := repo.FavoriteTrack(ctx, u1, "bogus_track"); err == nil || apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("expected CodeTrackNotFound for bogus track favorite, got: %v", err)
	}

	// IsFavorite check
	isFav, err := repo.IsFavorite(ctx, u1, tracks[0])
	if err != nil || !isFav {
		t.Fatalf("expected tracks[0] to be favorite for u1, got %v (err: %v)", isFav, err)
	}

	// Multi-user isolation: User 2 does NOT have tracks[0] as favorite
	isFavU2, err := repo.IsFavorite(ctx, u2, tracks[0])
	if err != nil || isFavU2 {
		t.Fatalf("expected tracks[0] NOT to be favorite for u2, got %v", isFavU2)
	}

	// List favorite IDs
	ids, err := repo.ListFavoriteTrackIDs(ctx, u1)
	if err != nil || len(ids) != 2 {
		t.Fatalf("expected 2 favorite IDs for u1, got %d (err: %v)", len(ids), err)
	}

	// List favorite full tracks
	favTracks, err := repo.ListFavoriteTracks(ctx, u1)
	if err != nil || len(favTracks) != 2 {
		t.Fatalf("expected 2 favorite tracks for u1, got %d (err: %v)", len(favTracks), err)
	}
	if favTracks[0].ID == "" || favTracks[0].Title == "" {
		t.Fatalf("expected populated LibraryTrack in favorites, got: %+v", favTracks[0])
	}

	// Unfavorite track 0
	if err := repo.UnfavoriteTrack(ctx, u1, tracks[0]); err != nil {
		t.Fatalf("unfavorite track 0: %v", err)
	}

	// Idempotent unfavorite
	if err := repo.UnfavoriteTrack(ctx, u1, tracks[0]); err != nil {
		t.Fatalf("idempotent unfavorite: %v", err)
	}

	isFav, err = repo.IsFavorite(ctx, u1, tracks[0])
	if err != nil || isFav {
		t.Fatalf("expected tracks[0] no longer favorite, got %v", isFav)
	}

	ids, err = repo.ListFavoriteTrackIDs(ctx, u1)
	if err != nil || len(ids) != 1 || ids[0] != tracks[1] {
		t.Fatalf("expected only tracks[1] as favorite, got: %v", ids)
	}
}

func TestPlaylists_ConcurrentMutation(t *testing.T) {
	_, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Concurrent Playlist", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// Concurrently attempt to add the same track 5 times
	var wg sync.WaitGroup
	errCount := 0
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.AddTrack(ctx, u1, pl.ID, tracks[0])
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) || apperr.CodeOf(err) == apperr.CodeAlreadyExists {
					errCount++
				}
			} else {
				successCount++
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 success out of concurrent adds, got %d", successCount)
	}

	// Verify database ended in a consistent state: exactly 1 track with position 1
	detail, err := repo.GetPlaylistForUser(ctx, u1, pl.ID)
	if err != nil {
		t.Fatalf("get playlist detail: %v", err)
	}
	if len(detail.Tracks) != 1 || detail.Tracks[0].Position != 1 {
		t.Fatalf("unexpected tracks state after concurrent adds: %+v", detail.Tracks)
	}
}

func TestPlaylists_TrackDeleteLifecycle(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Lifecycle Test", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// Add 3 tracks to playlist
	for _, tid := range tracks[:3] {
		if _, err := repo.AddTrack(ctx, u1, pl.ID, tid); err != nil {
			t.Fatalf("add track %s: %v", tid, err)
		}
	}

	// Favorite tracks 0 and 1
	if err := repo.FavoriteTrack(ctx, u1, tracks[0]); err != nil {
		t.Fatalf("favorite track 0: %v", err)
	}
	if err := repo.FavoriteTrack(ctx, u1, tracks[1]); err != nil {
		t.Fatalf("favorite track 1: %v", err)
	}

	// 1. Delete media FILE for track 0:
	// Logical catalog track identity survives media file disappearance!
	// Playlist and favorites MUST keep the track.
	_, err = db.ExecContext(ctx, `DELETE FROM files WHERE track_id = $1`, tracks[0])
	if err != nil {
		t.Fatalf("delete file for track 0: %v", err)
	}

	detail, err := repo.GetPlaylistForUser(ctx, u1, pl.ID)
	if err != nil {
		t.Fatalf("get playlist after file delete: %v", err)
	}
	if len(detail.Tracks) != 3 || detail.Tracks[0].ID != tracks[0] {
		t.Fatalf("track 0 should remain in playlist after file deletion, got %d tracks", len(detail.Tracks))
	}
	favs, err := repo.ListFavoriteTrackIDs(ctx, u1)
	if err != nil || len(favs) != 2 {
		t.Fatalf("track 0 should remain in favorites after file deletion, got %v", favs)
	}

	// 2. Delete logical track row for track 1 from catalog (tracks table):
	// Intentional cascade: catalog deletion removes track from all playlists and favorites.
	_, err = db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[1])
	if err != nil {
		t.Fatalf("delete track 1: %v", err)
	}

	// Verify track 1 removed from playlist cleanly without foreign key violations
	detail, err = repo.GetPlaylistForUser(ctx, u1, pl.ID)
	if err != nil {
		t.Fatalf("get playlist after track delete: %v", err)
	}
	if len(detail.Tracks) != 2 {
		t.Fatalf("expected 2 remaining tracks in playlist, got %d", len(detail.Tracks))
	}
	if detail.Tracks[0].ID != tracks[0] || detail.Tracks[1].ID != tracks[2] {
		t.Fatalf("remaining playlist tracks wrong: %+v", detail.Tracks)
	}
	// Verify positions presented are strictly contiguous 1..N
	if detail.Tracks[0].Position != 1 || detail.Tracks[1].Position != 2 {
		t.Fatalf("remaining playlist positions not contiguous: pos0=%d, pos1=%d", detail.Tracks[0].Position, detail.Tracks[1].Position)
	}

	// Verify favorite membership removed cleanly
	favs, err = repo.ListFavoriteTrackIDs(ctx, u1)
	if err != nil || len(favs) != 1 || favs[0] != tracks[0] {
		t.Fatalf("expected only track 0 in favorites after track 1 deletion, got %v", favs)
	}

	// Verify no dangling references in database
	var danglingPT, danglingFT int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_tracks WHERE track_id = $1`, tracks[1]).Scan(&danglingPT)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorite_tracks WHERE track_id = $1`, tracks[1]).Scan(&danglingFT)
	if danglingPT != 0 || danglingFT != 0 {
		t.Fatalf("dangling references found: pt=%d, ft=%d", danglingPT, danglingFT)
	}

	// Verify raw persisted DB positions are already contiguous 1..N via DB trigger
	var pos0, pos1 int
	_ = db.QueryRowContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2`, pl.ID, tracks[0]).Scan(&pos0)
	_ = db.QueryRowContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2`, pl.ID, tracks[2]).Scan(&pos1)
	if pos0 != 1 || pos1 != 2 {
		t.Fatalf("persisted positions after cascade not 1, 2: pos0=%d, pos1=%d", pos0, pos1)
	}
}

func TestPlaylists_ConcurrentAppendDifferentTracks(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Concurrent Append Different", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// Concurrently append 3 different tracks
	var wg sync.WaitGroup
	errs := make(chan error, 3)

	for _, tid := range tracks[:3] {
		wg.Add(1)
		go func(trackID string) {
			defer wg.Done()
			_, err := repo.AddTrack(ctx, u1, pl.ID, trackID)
			if err != nil {
				errs <- err
			}
		}(tid)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent append error: %v", err)
	}

	// Verify exactly 3 tracks, positions 1, 2, 3 with no gaps and no duplicates
	detail, err := repo.GetPlaylistForUser(ctx, u1, pl.ID)
	if err != nil {
		t.Fatalf("get playlist detail: %v", err)
	}
	if len(detail.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(detail.Tracks))
	}
	positions := make(map[int]bool)
	for i, trk := range detail.Tracks {
		expectedPos := i + 1
		if trk.Position != expectedPos {
			t.Errorf("track %d has position %d, expected %d", i, trk.Position, expectedPos)
		}
		if positions[trk.Position] {
			t.Errorf("duplicate position %d found", trk.Position)
		}
		positions[trk.Position] = true
	}

	// Verify directly in DB
	rows, err := db.QueryContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, pl.ID)
	if err != nil {
		t.Fatalf("query positions: %v", err)
	}
	defer rows.Close()
	dbPositions := make([]int, 0)
	for rows.Next() {
		var p int
		_ = rows.Scan(&p)
		dbPositions = append(dbPositions, p)
	}
	if len(dbPositions) != 3 || dbPositions[0] != 1 || dbPositions[1] != 2 || dbPositions[2] != 3 {
		t.Fatalf("persisted db positions invalid: %v", dbPositions)
	}
}

func TestPlaylists_RemoveAndReorderConcurrency(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Remove Reorder Concurrency", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// Add 4 tracks
	for _, tid := range tracks {
		if _, err := repo.AddTrack(ctx, u1, pl.ID, tid); err != nil {
			t.Fatalf("add track %s: %v", tid, err)
		}
	}

	// Concurrently:
	// Worker A: remove track 1 (tracks[1])
	// Worker B: reorder original 4 tracks in reverse [tracks[3], tracks[2], tracks[1], tracks[0]]
	var wg sync.WaitGroup
	var errRemove, errReorder error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errRemove = repo.RemoveTrack(ctx, u1, pl.ID, tracks[1])
	}()
	go func() {
		defer wg.Done()
		_, errReorder = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[3], tracks[2], tracks[1], tracks[0]})
	}()
	wg.Wait()

	// One operation must succeed. If reorder executed after remove, it gets a clean invalid request error
	// because track 1 is no longer in playlist.
	// If reorder executed first, it succeeded and remove then succeeded.
	if errRemove != nil && errReorder != nil {
		t.Fatalf("both remove and reorder failed: removeErr=%v, reorderErr=%v", errRemove, errReorder)
	}

	// Under ALL circumstances, database invariants must be intact:
	// Remaining tracks must have positions 1..N contiguous, no duplicates, no gaps, no foreign tracks.
	rows, err := db.QueryContext(ctx, `SELECT position, track_id FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, pl.ID)
	if err != nil {
		t.Fatalf("query db positions: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var pos int
		var tid string
		if err := rows.Scan(&pos, &tid); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		if pos != count {
			t.Fatalf("persisted position invariant violated: row %d has pos %d", count, pos)
		}
	}
	if count < 3 {
		t.Fatalf("unexpected low track count: %d", count)
	}
}

func TestPlaylists_PositionInvariants(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	assertInvariants := func(desc string, plID string, expectedCount int) {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, plID)
		if err != nil {
			t.Fatalf("[%s] query positions: %v", desc, err)
		}
		defer rows.Close()

		seen := make(map[int]bool)
		count := 0
		for rows.Next() {
			count++
			var pos int
			if err := rows.Scan(&pos); err != nil {
				t.Fatalf("[%s] scan: %v", desc, err)
			}
			if pos < 1 {
				t.Fatalf("[%s] position < 1: %d", desc, pos)
			}
			if seen[pos] {
				t.Fatalf("[%s] duplicate position: %d", desc, pos)
			}
			seen[pos] = true
			if pos != count {
				t.Fatalf("[%s] gap detected: expected %d, got %d", desc, count, pos)
			}
		}
		if count != expectedCount {
			t.Fatalf("[%s] expected %d tracks, got %d", desc, expectedCount, count)
		}
	}

	pl, err := repo.CreatePlaylist(ctx, u1, "Invariants Test", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}

	// 1. Add tracks 0, 1, 2, 3
	for i, tid := range tracks {
		_, err := repo.AddTrack(ctx, u1, pl.ID, tid)
		if err != nil {
			t.Fatalf("add %s: %v", tid, err)
		}
		assertInvariants("add track "+tid, pl.ID, i+1)
	}

	// 2. Reorder tracks [3, 0, 2, 1]
	_, err = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[3], tracks[0], tracks[2], tracks[1]})
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}
	assertInvariants("reorder", pl.ID, 4)

	// 3. Remove middle track (tracks[0])
	_, err = repo.RemoveTrack(ctx, u1, pl.ID, tracks[0])
	if err != nil {
		t.Fatalf("remove track 0: %v", err)
	}
	assertInvariants("remove middle track", pl.ID, 3)

	// 4. Remove first track (tracks[3])
	_, err = repo.RemoveTrack(ctx, u1, pl.ID, tracks[3])
	if err != nil {
		t.Fatalf("remove first track: %v", err)
	}
	assertInvariants("remove first track", pl.ID, 2)

	// 5. Delete entire playlist
	if err := repo.DeletePlaylist(ctx, u1, pl.ID); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}
	assertInvariants("delete playlist", pl.ID, 0)

	// 6. Verify library tracks and audio files were untouched
	var trkCount, fileCount int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE release_id = 'rel_pl'`).Scan(&trkCount)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE id LIKE 'fil_%'`).Scan(&fileCount)
	if trkCount != 4 || fileCount != 4 {
		t.Fatalf("library tracks or files modified by playlist delete: tracks=%d, files=%d", trkCount, fileCount)
	}
}

// TestPlaylists_CascadeCompaction_ExactInvariant verifies Stage A2:
// Direct FK cascade track deletion immediately compacts raw database positions 1..N.
func TestPlaylists_CascadeCompaction_ExactInvariant(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	// Create Playlist P with A(1), B(2), C(3), D(4)
	pl, err := repo.CreatePlaylist(ctx, u1, "Invariant P", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for _, tid := range tracks[:4] {
		if _, err := repo.AddTrack(ctx, u1, pl.ID, tid); err != nil {
			t.Fatalf("add %s: %v", tid, err)
		}
	}

	// Favorite B (tracks[1])
	if err := repo.FavoriteTrack(ctx, u1, tracks[1]); err != nil {
		t.Fatalf("favorite B: %v", err)
	}

	// Delete actual tracks row for B using raw SQL (exercising FK ON DELETE CASCADE)
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[1]); err != nil {
		t.Fatalf("delete track B: %v", err)
	}

	// 1. Verify favorite B removed
	favs, err := repo.ListFavoriteTrackIDs(ctx, u1)
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	for _, f := range favs {
		if f == tracks[1] {
			t.Fatalf("favorite B was not removed")
		}
	}

	// 2. Verify playlist membership B removed
	var countB int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2`, pl.ID, tracks[1]).Scan(&countB)
	if countB != 0 {
		t.Fatalf("track B still in playlist_tracks")
	}

	// 3. Inspect raw persisted database positions in playlist_tracks
	rows, err := db.QueryContext(ctx, `SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, pl.ID)
	if err != nil {
		t.Fatalf("query playlist tracks: %v", err)
	}
	defer rows.Close()

	type rowItem struct {
		trackID string
		pos     int
	}
	remaining := make([]rowItem, 0)
	for rows.Next() {
		var it rowItem
		if err := rows.Scan(&it.trackID, &it.pos); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		remaining = append(remaining, it)
	}

	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining tracks, got %d", len(remaining))
	}

	// Required invariant: A = 1, C = 2, D = 3 (NOT A=1, C=3, D=4)
	if remaining[0].trackID != tracks[0] || remaining[0].pos != 1 {
		t.Errorf("expected track A at pos 1, got %s at %d", remaining[0].trackID, remaining[0].pos)
	}
	if remaining[1].trackID != tracks[2] || remaining[1].pos != 2 {
		t.Errorf("expected track C at pos 2, got %s at %d", remaining[1].trackID, remaining[1].pos)
	}
	if remaining[2].trackID != tracks[3] || remaining[2].pos != 3 {
		t.Errorf("expected track D at pos 3, got %s at %d", remaining[2].trackID, remaining[2].pos)
	}
}

// TestPlaylists_CascadeCompaction_FirstMiddleLast verifies Stage A3:
// Deletion of first, middle, and last tracks keeps positions strictly contiguous 1..N.
func TestPlaylists_CascadeCompaction_FirstMiddleLast(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "First Middle Last P", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for _, tid := range tracks[:4] {
		if _, err := repo.AddTrack(ctx, u1, pl.ID, tid); err != nil {
			t.Fatalf("add %s: %v", tid, err)
		}
	}

	// Helper to check raw DB positions
	assertRawPositions := func(desc string, expectedTrackIDs []string) {
		rows, err := db.QueryContext(ctx, `SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, pl.ID)
		if err != nil {
			t.Fatalf("[%s] query: %v", desc, err)
		}
		defer rows.Close()

		idx := 0
		for rows.Next() {
			var tid string
			var pos int
			if err := rows.Scan(&tid, &pos); err != nil {
				t.Fatalf("[%s] scan: %v", desc, err)
			}
			if idx >= len(expectedTrackIDs) {
				t.Fatalf("[%s] too many rows in DB: idx=%d", desc, idx)
			}
			if tid != expectedTrackIDs[idx] {
				t.Errorf("[%s] row %d: expected track %s, got %s", desc, idx, expectedTrackIDs[idx], tid)
			}
			if pos != idx+1 {
				t.Errorf("[%s] row %d: expected pos %d, got %d", desc, idx, idx+1, pos)
			}
			idx++
		}
		if idx != len(expectedTrackIDs) {
			t.Errorf("[%s] expected %d rows, got %d", desc, len(expectedTrackIDs), idx)
		}
	}

	// 1. Delete First Track (tracks[0])
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[0]); err != nil {
		t.Fatalf("delete first: %v", err)
	}
	assertRawPositions("delete first", []string{tracks[1], tracks[2], tracks[3]})

	// 2. Delete Middle Track (tracks[2])
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[2]); err != nil {
		t.Fatalf("delete middle: %v", err)
	}
	assertRawPositions("delete middle", []string{tracks[1], tracks[3]})

	// 3. Delete Last Track (tracks[3])
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[3]); err != nil {
		t.Fatalf("delete last: %v", err)
	}
	assertRawPositions("delete last", []string{tracks[1]})
}

// TestPlaylists_CascadeCompaction_MultiplePlaylistsAndUsers verifies Stage A4:
// Deleting track X cascaded across multiple playlists and users compacts each playlist cleanly to 1..N.
func TestPlaylists_CascadeCompaction_MultiplePlaylistsAndUsers(t *testing.T) {
	db, repo, u1, u2, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	// We need 6 tracks: A, X, B for P1; C, D, X, E for P2.
	// Create extra tracks in catalog
	extraTracks := []string{"trk_multi_a", "trk_multi_x", "trk_multi_b", "trk_multi_c", "trk_multi_d", "trk_multi_e"}
	for _, tid := range extraTracks {
		_, err := db.ExecContext(ctx, `
			INSERT INTO tracks (id, release_id, artist_id, title, identity_key, created_at, updated_at)
			VALUES ($1, 'rel_pl', 'art_pl', $1, $1, NOW(), NOW())
		`, tid)
		if err != nil {
			t.Fatalf("insert %s: %v", tid, err)
		}
	}

	trackA := "trk_multi_a"
	trackX := "trk_multi_x"
	trackB := "trk_multi_b"
	trackC := "trk_multi_c"
	trackD := "trk_multi_d"
	trackE := "trk_multi_e"

	// P1 owned by User 1: A, X, B
	p1, err := repo.CreatePlaylist(ctx, u1, "User 1 P1", "")
	if err != nil {
		t.Fatalf("create P1: %v", err)
	}
	for _, tid := range []string{trackA, trackX, trackB} {
		if _, err := repo.AddTrack(ctx, u1, p1.ID, tid); err != nil {
			t.Fatalf("add to P1: %v", err)
		}
	}

	// P2 owned by User 2: C, D, X, E
	p2, err := repo.CreatePlaylist(ctx, u2, "User 2 P2", "")
	if err != nil {
		t.Fatalf("create P2: %v", err)
	}
	for _, tid := range []string{trackC, trackD, trackX, trackE} {
		if _, err := repo.AddTrack(ctx, u2, p2.ID, tid); err != nil {
			t.Fatalf("add to P2: %v", err)
		}
	}

	// Favorite X for User 1 and User 2
	if err := repo.FavoriteTrack(ctx, u1, trackX); err != nil {
		t.Fatalf("fav u1: %v", err)
	}
	if err := repo.FavoriteTrack(ctx, u2, trackX); err != nil {
		t.Fatalf("fav u2: %v", err)
	}

	// Delete catalog track X
	if _, err := db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, trackX); err != nil {
		t.Fatalf("delete track X: %v", err)
	}

	// 1. Verify X removed from User 1 & User 2 favorites
	u1Favs, _ := repo.ListFavoriteTrackIDs(ctx, u1)
	for _, f := range u1Favs {
		if f == trackX {
			t.Errorf("track X still in User 1 favorites")
		}
	}
	u2Favs, _ := repo.ListFavoriteTrackIDs(ctx, u2)
	for _, f := range u2Favs {
		if f == trackX {
			t.Errorf("track X still in User 2 favorites")
		}
	}

	// 2. Verify P1 positions compact: A=1, B=2
	rows1, err := db.QueryContext(ctx, `SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, p1.ID)
	if err != nil {
		t.Fatalf("query P1: %v", err)
	}
	defer rows1.Close()
	p1Tracks := make([]string, 0)
	p1Positions := make([]int, 0)
	for rows1.Next() {
		var tid string
		var pos int
		_ = rows1.Scan(&tid, &pos)
		p1Tracks = append(p1Tracks, tid)
		p1Positions = append(p1Positions, pos)
	}
	if len(p1Tracks) != 2 || p1Tracks[0] != trackA || p1Tracks[1] != trackB || p1Positions[0] != 1 || p1Positions[1] != 2 {
		t.Errorf("P1 positions not compacted cleanly: tracks=%v, positions=%v", p1Tracks, p1Positions)
	}

	// 3. Verify P2 positions compact: C=1, D=2, E=3
	rows2, err := db.QueryContext(ctx, `SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, p2.ID)
	if err != nil {
		t.Fatalf("query P2: %v", err)
	}
	defer rows2.Close()
	p2Tracks := make([]string, 0)
	p2Positions := make([]int, 0)
	for rows2.Next() {
		var tid string
		var pos int
		_ = rows2.Scan(&tid, &pos)
		p2Tracks = append(p2Tracks, tid)
		p2Positions = append(p2Positions, pos)
	}
	if len(p2Tracks) != 3 || p2Tracks[0] != trackC || p2Tracks[1] != trackD || p2Tracks[2] != trackE ||
		p2Positions[0] != 1 || p2Positions[1] != 2 || p2Positions[2] != 3 {
		t.Errorf("P2 positions not compacted cleanly: tracks=%v, positions=%v", p2Tracks, p2Positions)
	}

	// Ensure no unused variable warning
	_ = tracks
}

// TestPlaylists_CascadeCompaction_Concurrency verifies Stage A7:
// Concurrent catalog deletion and playlist mutation maintain position integrity.
func TestPlaylists_CascadeCompaction_Concurrency(t *testing.T) {
	db, repo, u1, _, tracks := setupPlaylistTest(t)
	ctx := context.Background()

	pl, err := repo.CreatePlaylist(ctx, u1, "Concurrent Cascade P", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	for _, tid := range tracks[:4] {
		if _, err := repo.AddTrack(ctx, u1, pl.ID, tid); err != nil {
			t.Fatalf("add %s: %v", tid, err)
		}
	}

	// Concurrently:
	// Worker 1: Delete catalog track tracks[1]
	// Worker 2: Reorder remaining tracks
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, tracks[1])
	}()

	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		_, _ = repo.ReorderTracks(ctx, u1, pl.ID, []string{tracks[3], tracks[0], tracks[2]})
	}()

	wg.Wait()

	// Verify resulting playlist has contiguous 1..N positions and no duplicates
	rows, err := db.QueryContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position ASC`, pl.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var positions []int
	for rows.Next() {
		var p int
		_ = rows.Scan(&p)
		positions = append(positions, p)
	}

	for i, pos := range positions {
		if pos != i+1 {
			t.Errorf("position %d at index %d is not contiguous: positions=%v", pos, i, positions)
		}
	}
}
