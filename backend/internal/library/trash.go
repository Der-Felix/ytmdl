package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

type TrashStore interface {
	PrepareTrash(context.Context, string, string, []repository.TrashMove) (*repository.TrashEntry, error)
	CommitTrash(context.Context, string) error
	GetTrash(context.Context, string) (*repository.TrashEntry, error)
	ListTrash(context.Context, int, bool) ([]repository.TrashEntry, error)
	SetTrashState(context.Context, string, string, string) error
	ForgetTrash(context.Context, string) error
	CheckTrashRestore(context.Context, *repository.TrashEntry) error
	RestoreTrashMetadata(context.Context, *repository.TrashEntry) error
}

func (s *Service) trashStore() (TrashStore, error) {
	store, ok := s.catalog.(TrashStore)
	if !ok {
		return nil, apperr.New(apperr.CodeInternal, "Papierkorb nicht verfügbar.")
	}
	return store, nil
}
func (s *Service) trashTrackLocked(ctx context.Context, root *os.Root, id, user string, files []music.File, paths map[string]string, protected map[string]bool) error {
	store, err := s.trashStore()
	if err != nil {
		return err
	}
	unlock, ok := s.locks.TryLock("trash:operations")
	if !ok {
		return apperr.New(apperr.CodeConflict, "Der Papierkorb wird bereits verarbeitet.")
	}
	defer unlock()
	dir := filepath.Join(TrashDirName, "songs", music.NewID())
	if err = root.MkdirAll(dir, 0700); err != nil {
		return trashStorageError()
	}
	for _, p := range []string{TrashDirName, filepath.Join(TrashDirName, "songs"), dir} {
		st, e := root.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return trashStorageError()
		}
	}
	moves := []repository.TrashMove{}
	seen := map[string]bool{}
	for _, f := range files {
		candidates := []string{paths[f.ID]}
		for _, ext := range storage.LyricsExtensions() {
			p := storage.SidecarPathFor(paths[f.ID], ext)
			if !protected[p] {
				candidates = append(candidates, p)
			}
		}
		for _, p := range candidates {
			if seen[p] {
				continue
			}
			seen[p] = true
			st, e := root.Lstat(p)
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil || !st.Mode().IsRegular() {
				return trashStorageError()
			}
			moves = append(moves, repository.TrashMove{Original: p, Stored: filepath.Join(dir, fmt.Sprintf("%03d%s", len(moves), filepath.Ext(p))), Size: st.Size(), MTimeNS: st.ModTime().UnixNano()})
		}
	}
	entry, err := store.PrepareTrash(ctx, id, user, moves)
	if err != nil {
		return err
	}
	// A cancellation cannot prevent restoring media after a failed DB transaction.
	rollback := func() error {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if e := moveTrashPaths(root, entry, false); e != nil {
			return e
		}
		return store.ForgetTrash(recovery, entry.ID)
	}
	if err = moveTrashPaths(root, entry, true); err != nil {
		_ = rollback()
		return err
	}
	if err = store.CommitTrash(ctx, entry.ID); err != nil {
		verify, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		saved, readErr := store.GetTrash(verify, entry.ID)
		if readErr == nil && saved.State == "ready" {
			return nil
		}
		// A lost commit response is not proof of a rollback. Preserve the
		// journal/media when the DB cannot establish which side committed.
		if readErr == nil && saved.State == "preparing" {
			_ = rollback()
		}
		return err
	}
	return nil
}
func trashStorageError() error {
	return apperr.New(apperr.CodeStorageUnavailable, "Dateien konnten nicht sicher verschoben werden. Nichts wird überschrieben; bitte den Papierkorb neu laden.")
}

// Existing destinations are never replaced, including on recovery after interruption.
func moveTrashPaths(root *os.Root, e *repository.TrashEntry, toTrash bool) error {
	for _, m := range e.Moves {
		if !filepath.IsLocal(m.Original) || !filepath.IsLocal(m.Stored) || !strings.HasPrefix(m.Stored, TrashDirName+string(filepath.Separator)+"songs"+string(filepath.Separator)) {
			return trashStorageError()
		}
		from, to := m.Stored, m.Original
		if toTrash {
			from, to = m.Original, m.Stored
		}
		src, se := root.Lstat(from)
		dest, de := root.Lstat(to)
		if errors.Is(se, os.ErrNotExist) && de == nil && dest.Mode().IsRegular() && dest.Size() == m.Size && dest.ModTime().UnixNano() == m.MTimeNS {
			continue
		}
		if se == nil && de == nil && src.Mode().IsRegular() && dest.Mode().IsRegular() && os.SameFile(src, dest) {
			if err := root.Remove(from); err != nil {
				return trashStorageError()
			}
			continue
		}
		if se != nil || !src.Mode().IsRegular() || !errors.Is(de, os.ErrNotExist) {
			return trashStorageError()
		}
		if src.Size() != m.Size || src.ModTime().UnixNano() != m.MTimeNS {
			return trashStorageError()
		}
		// Link is atomic and refuses an existing destination. A crash with both
		// names is recognized through SameFile, without replacing any media.
		if err := root.Link(from, to); err != nil {
			return trashStorageError()
		}
		if err := root.Remove(from); err != nil {
			return trashStorageError()
		}
	}
	return nil
}
func (s *Service) trashRoot() (*os.Root, error) {
	if g := s.library.Guard(); g != nil {
		if err := g.RequireWritable(); err != nil {
			return nil, err
		}
	}
	r, e := os.OpenRoot(s.library.Root())
	if e != nil {
		return nil, trashStorageError()
	}
	return r, nil
}
func (s *Service) RestoreTrash(ctx context.Context, id string) error {
	store, err := s.trashStore()
	if err != nil {
		return err
	}
	unlock, ok := s.locks.TryLock("trash:operations")
	if !ok {
		return apperr.New(apperr.CodeConflict, "Eintrag wird bereits verarbeitet.")
	}
	defer unlock()
	e, err := store.GetTrash(ctx, id)
	if err != nil {
		return err
	}
	if e.State != "ready" {
		return apperr.New(apperr.CodeConflict, "Eintrag ist noch nicht bereit; bitte die Wiederherstellung unterbrochener Aktionen starten.")
	}
	root, err := s.trashRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err = store.CheckTrashRestore(ctx, e); err != nil {
		return err
	}
	if err = store.SetTrashState(ctx, id, "ready", "restoring"); err != nil {
		return err
	}
	if err = moveTrashPaths(root, e, false); err == nil {
		err = store.RestoreTrashMetadata(ctx, e)
	}
	if err != nil {
		recoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		saved, readErr := store.GetTrash(recoverCtx, id)
		if apperr.CodeOf(readErr) == apperr.CodeTrackNotFound {
			track, trackErr := s.catalog.GetTrack(recoverCtx, e.TrackID)
			if trackErr == nil && track != nil {
				return nil
			}
		}
		if readErr == nil && saved.State == "restoring" && moveTrashPaths(root, e, true) == nil {
			_ = store.SetTrashState(recoverCtx, id, "restoring", "ready")
		}
	}
	return err
}
func (s *Service) PurgeTrash(ctx context.Context, id string, expiredOnly bool) error {
	store, err := s.trashStore()
	if err != nil {
		return err
	}
	unlock, ok := s.locks.TryLock("trash:operations")
	if !ok {
		return apperr.New(apperr.CodeConflict, "Eintrag wird bereits verarbeitet.")
	}
	defer unlock()
	e, err := store.GetTrash(ctx, id)
	if err != nil {
		return err
	}
	if e.State != "ready" && e.State != "purging" {
		return apperr.New(apperr.CodeConflict, "Eintrag kann noch nicht endgültig entfernt werden.")
	}
	if e.State == "ready" && expiredOnly && time.Now().Before(e.ExpiresAt) {
		return nil
	}
	root, err := s.trashRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if e.State == "ready" {
		if err = store.SetTrashState(ctx, id, "ready", "purging"); err != nil {
			return err
		}
	}
	for _, m := range e.Moves {
		if !filepath.IsLocal(m.Stored) || !strings.HasPrefix(m.Stored, TrashDirName+string(filepath.Separator)+"songs"+string(filepath.Separator)) {
			return trashStorageError()
		}
		st, err := root.Lstat(m.Stored)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !st.Mode().IsRegular() || st.Size() != m.Size || st.ModTime().UnixNano() != m.MTimeNS {
			return trashStorageError()
		}
		if err = root.Remove(m.Stored); err != nil {
			return trashStorageError()
		}
	}
	return store.ForgetTrash(ctx, id)
}

// Only incomplete journaled operations are recovered. Quarantined songs are not scanned or adopted.
func (s *Service) RecoverTrash(ctx context.Context) error {
	unlock, ok := s.locks.TryLock("trash:operations")
	if !ok {
		return apperr.New(apperr.CodeConflict, "Der Papierkorb wird bereits verarbeitet.")
	}
	defer unlock()
	store, err := s.trashStore()
	if err != nil {
		return err
	}
	entries, err := store.ListTrash(ctx, 100, true)
	if err != nil {
		return err
	}
	root, err := s.trashRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	for _, e := range entries {
		if e.State != "preparing" && e.State != "restoring" {
			continue
		}
		unlock, ok := s.locks.TryLock("trash:" + e.ID)
		if !ok {
			continue
		}
		err = func() error {
			full, err := store.GetTrash(ctx, e.ID)
			if err != nil {
				return err
			}
			if e.State == "preparing" {
				track, err := s.catalog.GetTrack(ctx, e.TrackID)
				if err != nil {
					return err
				}
				if track == nil {
					return apperr.New(apperr.CodeConflict, "Unterbrochener Eintrag benötigt eine manuelle Prüfung.")
				}
				if err = moveTrashPaths(root, full, false); err != nil {
					return err
				}
				return store.ForgetTrash(ctx, e.ID)
			}
			if err = store.CheckTrashRestore(ctx, full); err != nil {
				return err
			}
			if err = moveTrashPaths(root, full, true); err != nil {
				return err
			}
			return store.SetTrashState(ctx, e.ID, "restoring", "ready")
		}()
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) StartTrashMaintenance() {
	if _, err := s.trashStore(); err != nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
				ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
				if err := s.RecoverTrash(ctx); err == nil {
					store, _ := s.trashStore()
					entries, e := store.ListTrash(ctx, 100, true)
					if e == nil {
						for _, entry := range entries {
							if entry.State == "purging" || (entry.State == "ready" && time.Now().After(entry.ExpiresAt)) {
								if s.PurgeTrash(ctx, entry.ID, true) != nil {
									break
								}
							}
						}
					}
				}
				cancel()
				timer.Reset(time.Hour)
			}
		}
	}()
}
