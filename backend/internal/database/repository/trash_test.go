package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/storage"
)

func TestTrashRoundTripRestoresAudioFavoritesAndPlaylistOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "original order", true: "reverse order"}[reverse], func(t *testing.T) {
			db, pl, u1, u2, ids := setupPlaylistTest(t)
			ctx := context.Background()
			cat := repository.NewCatalog(db)
			files := repository.NewFiles(db)
			root := t.TempDir()
			if _, err := db.ExecContext(ctx, `UPDATE tracks SET title='Same',album_artist='Artist'`); err != nil {
				t.Fatal(err)
			}
			originals, err := files.ListAll(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range originals {
				path := filepath.Join(root, f.Path)
				if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, []byte("fixture audio "+f.TrackID), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(storage.SidecarPathFor(path, ".lrc"), []byte("fixture lyrics"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			playlists := []string{}
			for _, user := range []string{u1, u2} {
				p, err := pl.CreatePlaylist(ctx, user, "Keep order", "")
				if err != nil {
					t.Fatal(err)
				}
				playlists = append(playlists, p.ID)
				if _, err = pl.AddTracks(ctx, user, p.ID, ids); err != nil {
					t.Fatal(err)
				}
				for _, id := range ids {
					if err = pl.FavoriteTrack(ctx, user, id); err != nil {
						t.Fatal(err)
					}
				}
			}
			lib, err := storage.NewLibrary(root)
			if err != nil {
				t.Fatal(err)
			}
			svc, err := library.NewService(library.ServiceOptions{Library: lib, Catalog: cat, Files: files})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Stop()
			groups, err := cat.DuplicateGroupsForUser(ctx, u1, 0, true, "")
			if err != nil || len(groups) != 1 {
				t.Fatal("group", err)
			}
			r := repository.DuplicateReview{GroupKey: groups[0].Key, Fingerprint: groups[0].Fingerprint, Outcome: "preferred", PreferredTrackID: ids[2]}
			if err = cat.SaveDuplicateReview(ctx, u1, r); err != nil {
				t.Fatal(err)
			}
			removed, failed, err := svc.MoveDuplicateTracksToTrash(ctx, ids[2], ids[:2], u1, func() error { return cat.ValidateDuplicateRemoval(ctx, u1, r, ids[:2]) })
			if err != nil || failed != "" || !reflect.DeepEqual(removed, ids[:2]) {
				t.Fatalf("trash failed %v %v %v", removed, failed, err)
			}
			entries, err := cat.ListTrash(ctx, 100, true)
			if err != nil || len(entries) != 2 {
				t.Fatal("journal", err)
			}
			for _, e := range entries {
				full, err := cat.GetTrash(ctx, e.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range full.Moves {
					if _, err = os.Stat(filepath.Join(root, m.Original)); !os.IsNotExist(err) {
						t.Fatal("original still present")
					}
					if _, err = os.Stat(filepath.Join(root, m.Stored)); err != nil {
						t.Fatal("recoverable file missing")
					}
				}
			}
			if reverse {
				entries[0], entries[1] = entries[1], entries[0]
			}
			for _, e := range entries {
				if err = svc.RestoreTrash(ctx, e.ID); err != nil {
					t.Fatal("restore", err)
				}
			}
			for _, f := range originals {
				raw, err := os.ReadFile(filepath.Join(root, f.Path))
				if err != nil || string(raw) != "fixture audio "+f.TrackID {
					t.Fatal("audio not restored", err)
				}
			}
			for i, user := range []string{u1, u2} {
				rows, err := db.QueryContext(ctx, `SELECT track_id FROM playlist_tracks WHERE playlist_id=$1 ORDER BY position`, playlists[i])
				if err != nil {
					t.Fatal(err)
				}
				got := []string{}
				for rows.Next() {
					var id string
					if err = rows.Scan(&id); err != nil {
						t.Fatal(err)
					}
					got = append(got, id)
				}
				rows.Close()
				if !reflect.DeepEqual(got, ids) {
					t.Fatalf("wrong restored order %v", got)
				}
				var n int
				if err = db.QueryRowContext(ctx, `SELECT count(*) FROM favorite_tracks WHERE user_id=$1`, user).Scan(&n); err != nil || n != 4 {
					t.Fatal("favorites", n, err)
				}
			}
		})
	}
}
func TestTrashJournalRecoveryAndNoOverwrite(t *testing.T) {
	db, _, u, _, ids := setupPlaylistTest(t)
	ctx := context.Background()
	cat := repository.NewCatalog(db)
	files := repository.NewFiles(db)
	root := t.TempDir()
	path := "fixture.flac"
	original := []byte("original audio")
	if err := os.WriteFile(filepath.Join(root, path), original, 0600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(root, path))
	stored := filepath.Join(library.TrashDirName, "songs", "fixture", path)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, stored)), 0700); err != nil {
		t.Fatal(err)
	}
	entry, err := cat.PrepareTrash(ctx, ids[0], u, []repository.TrashMove{{Original: path, Stored: stored, Size: st.Size(), MTimeNS: st.ModTime().UnixNano()}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Link(filepath.Join(root, path), filepath.Join(root, stored)); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, path)); err != nil {
		t.Fatal(err)
	}
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := library.NewService(library.ServiceOptions{Library: lib, Catalog: cat, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	if err = svc.RecoverTrash(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || !reflect.DeepEqual(raw, original) {
		t.Fatal("interrupted move not recovered", err)
	}
	if _, err = cat.GetTrash(ctx, entry.ID); err == nil {
		t.Fatal("recovered journal retained")
	}
	entry, err = cat.PrepareTrash(ctx, ids[0], u, []repository.TrashMove{{Original: path, Stored: stored, Size: st.Size(), MTimeNS: st.ModTime().UnixNano()}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(root, path), filepath.Join(root, stored)); err != nil {
		t.Fatal(err)
	}
	if err = cat.CommitTrash(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, path), []byte("new unrelated audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = svc.RestoreTrash(ctx, entry.ID); err == nil {
		t.Fatal("existing audio overwritten")
	}
	raw, _ = os.ReadFile(filepath.Join(root, path))
	if string(raw) != "new unrelated audio" {
		t.Fatal("new media changed")
	}
	if _, err = os.Stat(filepath.Join(root, stored)); err != nil {
		t.Fatal("quarantined media lost", err)
	}
}
func TestTrashExpirationAndExplicitPurgePreserveRetainedVersion(t *testing.T) {
	db, _, u, _, ids := setupPlaylistTest(t)
	ctx := context.Background()
	cat := repository.NewCatalog(db)
	files := repository.NewFiles(db)
	root := t.TempDir()
	all, err := files.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range all {
		p := filepath.Join(root, f.Path)
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, []byte("fixture "+f.TrackID), 0600); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := library.NewService(library.ServiceOptions{Library: lib, Catalog: cat, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	if _, _, err = svc.MoveDuplicateTracksToTrash(ctx, ids[0], ids[1:2], u, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := cat.ListTrash(ctx, 100, true)
	if err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
	e, err := cat.GetTrash(ctx, entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.PurgeTrash(ctx, e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = cat.GetTrash(ctx, e.ID); err != nil {
		t.Fatal("retention not respected")
	}
	if _, err = db.ExecContext(ctx, `UPDATE library_trash SET expires_at=now()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.PurgeTrash(ctx, e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = cat.GetTrash(ctx, e.ID); err == nil {
		t.Fatal("expired journal not removed")
	}
	for _, m := range e.Moves {
		if _, err = os.Stat(filepath.Join(root, m.Stored)); !os.IsNotExist(err) {
			t.Fatal("expired audio retained")
		}
	}
	for _, f := range all {
		if f.TrackID != ids[1] {
			if _, err = os.Stat(filepath.Join(root, f.Path)); err != nil {
				t.Fatal("retained version changed")
			}
		}
	}
}
