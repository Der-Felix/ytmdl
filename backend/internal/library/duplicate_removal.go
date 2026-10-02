package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// RemoveDuplicateTracks protects the winner and validates the complete plan
// before the first mutation. Filesystem/DB failures can still yield a partial
// result; callers must report those completed track deletions accurately.
func (s *Service) RemoveDuplicateTracks(ctx context.Context, winner string, ids []string, validate func() error) (deleted []string, failedID string, err error) {
	deleted = []string{}
	if winner == "" || len(ids) == 0 || len(ids) > 99 {
		return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Ungültige Löschliste.")
	}
	seen := map[string]bool{winner: true}
	locks := []string{winner}
	for _, id := range ids {
		if id == "" || seen[id] {
			return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Die bevorzugte Version darf nicht gelöscht werden.")
		}
		seen[id] = true
		locks = append(locks, id)
	}
	sort.Strings(locks)
	unlocks := []func(){}
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}()
	for _, id := range locks {
		unlock, ok := s.locks.TryLock("track:" + id)
		if !ok {
			return deleted, "", apperr.New(apperr.CodeAlreadyExists, "Eine Version wird gerade bearbeitet. Bitte später erneut versuchen.")
		}
		unlocks = append(unlocks, unlock)
	}
	if validate != nil {
		if err = validate(); err != nil {
			return deleted, "", err
		}
	}
	if guard := s.library.Guard(); guard != nil {
		if err = guard.RequireWritable(); err != nil {
			return deleted, "", err
		}
	}
	root, err := os.OpenRoot(s.library.Root())
	if err != nil {
		return deleted, "", apperr.New(apperr.CodeStorageUnavailable, "Die Bibliothek ist nicht erreichbar.")
	}
	defer root.Close()
	files := map[string][]music.File{}
	paths := map[string]string{}
	protected := map[string]bool{}
	for _, id := range append([]string{winner}, ids...) {
		track, e := s.catalog.GetTrack(ctx, id)
		if e != nil {
			return deleted, "", e
		}
		if track == nil {
			return deleted, "", apperr.New(apperr.CodeTrackNotFound, "Eine Version fehlt. Bitte neu vergleichen.")
		}
		if s.jobs != nil {
			targets := []struct {
				kind jobs.Type
				id   string
			}{{jobs.TypeTrack, id}, {jobs.TypeTrack, track.SourceID}, {jobs.TypeRelease, track.ReleaseID}}
			if track.ReleaseID != "" {
				rel, e := s.catalog.GetRelease(ctx, track.ReleaseID)
				if e != nil {
					return deleted, "", e
				}
				if rel != nil {
					targets = append(targets, struct {
						kind jobs.Type
						id   string
					}{jobs.TypeRelease, rel.SourceID})
				}
			}
			for _, target := range targets {
				if target.id == "" {
					continue
				}
				busy, e := s.jobs.HasUnfinishedJob(ctx, target.kind, target.id)
				if e != nil {
					return deleted, "", e
				}
				if busy {
					return deleted, "", apperr.New(apperr.CodeAlreadyExists, "Für eine Version läuft noch ein Downloadauftrag. Bitte später erneut versuchen.")
				}
			}
		}
		files[id], err = s.files.ListByTrack(ctx, id)
		if err != nil {
			return deleted, "", err
		}
		if id == winner && len(files[id]) == 0 {
			return deleted, "", apperr.New(apperr.CodeFileNotFound, "Die bevorzugte Audiodatei fehlt.")
		}
		for _, f := range files[id] {
			_, rel, e := VerifyPathConfinement(s.library.Root(), f.Path, id != winner)
			if e != nil {
				return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Eine Audiodatei ist nicht sicher in der Bibliothek erreichbar.")
			}
			lexical := f.Path
			if !filepath.IsAbs(lexical) {
				lexical = filepath.Join(s.library.Root(), lexical)
			}
			info, e := os.Lstat(lexical)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return deleted, "", apperr.New(apperr.CodeFileNotFound, "Eine Audiodatei konnte nicht geprüft werden.")
			}
			if e == nil && !info.Mode().IsRegular() {
				return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Verknüpfungen und Verzeichnisse werden nicht als Duplikate gelöscht.")
			}
			paths[f.ID] = rel
			if id == winner {
				protected[rel] = true
				for _, ext := range storage.LyricsExtensions() {
					protected[storage.SidecarPathFor(rel, ext)] = true
				}
			}
		}
	}
	// Protect paths and shared lyric sidecars referenced by unselected recordings.
	all, e := s.files.ListAll(ctx)
	if e != nil {
		return deleted, "", e
	}
	removing := map[string]bool{}
	for _, id := range ids {
		removing[id] = true
	}
	for _, f := range all {
		if removing[f.TrackID] {
			continue
		}
		path := f.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.library.Root(), path)
		}
		rel, e := filepath.Rel(s.library.Root(), filepath.Clean(path))
		if e != nil {
			return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Eine Dateireferenz konnte nicht geprüft werden.")
		}
		protected[rel] = true
		for _, ext := range storage.LyricsExtensions() {
			protected[storage.SidecarPathFor(rel, ext)] = true
		}
	}
	for _, id := range ids {
		for _, f := range files[id] {
			if protected[paths[f.ID]] {
				return deleted, "", apperr.New(apperr.CodeInvalidRequest, "Eine andere Version verweist auf die bevorzugte Datei.")
			}
		}
	}
	for _, id := range ids {
		failedID = id
		for _, f := range files[id] {
			rel := paths[f.ID]
			if e := root.Remove(rel); e != nil && !errors.Is(e, os.ErrNotExist) {
				return deleted, failedID, apperr.New(apperr.CodeInternal, "Eine Audiodatei konnte nicht entfernt werden. Die Verarbeitung wurde gestoppt.")
			}
			for _, ext := range storage.LyricsExtensions() {
				sidecar := storage.SidecarPathFor(rel, ext)
				if !protected[sidecar] {
					if e := root.Remove(sidecar); e != nil && !errors.Is(e, os.ErrNotExist) {
						return deleted, failedID, apperr.New(apperr.CodeInternal, "Zugehörige Lyrics konnten nicht entfernt werden. Die Verarbeitung wurde gestoppt.")
					}
				}
			}
			if err = s.files.Delete(ctx, f.ID); err != nil {
				return deleted, failedID, err
			}
		}
		if err = s.catalog.DeleteTrack(ctx, id); err != nil {
			return deleted, failedID, err
		}
		deleted = append(deleted, id)
	}
	return deleted, "", nil
}
