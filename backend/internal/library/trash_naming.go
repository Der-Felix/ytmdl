package library

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// trashedTrack holds the fields of a trashed track that its file name is
// built from. The payload is the tracks row as JSON.
type trashedTrack struct {
	Title       string  `json:"title"`
	TrackNumber int     `json:"track_number"`
	DiscNumber  int     `json:"disc_number"`
	DiscTotal   int     `json:"disc_total"`
	ReleaseID   *string `json:"release_id"`
}

// retargetTrashEntry brings a song trashed under an older naming scheme back
// under its current name. The trash keeps the path a file had when it was
// trashed; after the naming migration that folder may no longer exist, and
// restoring into it would split a release across two folders.
//
// Only a path that is exactly what the older scheme produced is changed, and
// only when the library has moved to the current scheme or the old folder is
// gone. Otherwise the song goes back beside its siblings, and a pending
// migration takes it along. The new targets are saved before any file moves,
// so an interrupted restore is recovered with the paths it actually used.
func (s *Service) retargetTrashEntry(ctx context.Context, store TrashStore, e *repository.TrashEntry) (*repository.TrashEntry, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &doc); err != nil {
		return e, nil
	}
	var trk trashedTrack
	if err := json.Unmarshal(doc["track"], &trk); err != nil || trk.ReleaseID == nil {
		return e, nil
	}
	release, err := s.catalog.GetRelease(ctx, *trk.ReleaseID)
	if err != nil || release == nil {
		return e, nil
	}
	var files []map[string]json.RawMessage
	if err := json.Unmarshal(doc["files"], &files); err != nil {
		return e, nil
	}

	from := storage.NamingSchemeLegacy
	libraryCurrent := false
	if s.namingSettings != nil {
		if applied, ok := s.appliedNamingScheme(ctx, s.namingSettings); ok {
			from = applied
			libraryCurrent = applied >= storage.CurrentNamingScheme
		}
	}
	if from >= storage.CurrentNamingScheme {
		from = storage.NamingSchemeLegacy
	}
	track := music.Track{Title: trk.Title, TrackNumber: trk.TrackNumber, DiscNumber: trk.DiscNumber, DiscTotal: trk.DiscTotal}

	renamed := make(map[string]string)
	for i, f := range files {
		var path string
		if err := json.Unmarshal(f["path"], &path); err != nil || path == "" {
			continue
		}
		ext := filepath.Ext(path)
		oldPlain := storage.TrackRelPathFor(from, *release, track, ext)
		newPlain := storage.TrackRelPathFor(storage.CurrentNamingScheme, *release, track, ext)
		var target string
		switch {
		case path == oldPlain:
			target = newPlain
		default:
			if suffix, ok := storage.SplitSourceSuffix(filepath.Base(path)); ok && path == withSuffix(oldPlain, suffix) {
				target = withSuffix(newPlain, suffix)
			}
		}
		if target == "" || target == path {
			continue
		}
		if !libraryCurrent && s.oldFolderExists(path) {
			continue
		}
		raw, err := json.Marshal(target)
		if err != nil {
			return e, err
		}
		files[i]["path"] = raw
		renamed[path] = target
	}
	if len(renamed) == 0 {
		return e, nil
	}

	moves := make([]repository.TrashMove, len(e.Moves))
	copy(moves, e.Moves)
	for i, m := range moves {
		if to, ok := renamed[m.Original]; ok {
			moves[i].Original = to
			continue
		}
		for audioFrom, audioTo := range renamed {
			for _, ext := range storage.LyricsExtensions() {
				if m.Original == storage.SidecarPathFor(audioFrom, ext) {
					moves[i].Original = storage.SidecarPathFor(audioTo, ext)
				}
			}
		}
	}

	rawFiles, err := json.Marshal(files)
	if err != nil {
		return e, err
	}
	doc["files"] = rawFiles
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return e, err
	}
	newPayload := json.RawMessage(bytes.TrimSpace(payload.Bytes()))

	if err := store.RetargetTrash(ctx, e.ID, moves, newPayload); err != nil {
		return e, err
	}
	for from, to := range renamed {
		s.logger.Info("trashed song is restored under the current naming scheme", "from", from, "to", to)
	}
	out := *e
	out.Moves = moves
	out.Payload = newPayload
	return &out, nil
}

func (s *Service) oldFolderExists(rel string) bool {
	info, err := os.Stat(filepath.Join(s.library.Root(), filepath.Dir(rel)))
	return err == nil && info.IsDir()
}
