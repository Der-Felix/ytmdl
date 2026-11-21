package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// existingRecording is a recording the library already holds, together with
// the file a copy would be made from.
type existingRecording struct {
	track music.Track
	// file is the first usable file in playback order: compatible with the
	// requested runtime and present on disk.
	file music.File
}

// findExisting answers whether the recording exists on disk anywhere and, if
// so, which of its files a normal playback would use. Files.ListByTrack
// already returns them in that order.
func (m *Manager) findExisting(ctx context.Context, track music.Track) (*existingRecording, error) {
	known, err := m.catalog.FindTrack(ctx, track, m.toleranceMS)
	if err != nil || known == nil {
		return nil, err
	}
	files, err := m.files.ListByTrack(ctx, known.ID)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if track.DurationMS > 0 && (file.DurationMS <= 0 || !music.CompatibleDuration(track.DurationMS, file.DurationMS)) {
			continue
		}
		if m.library.Exists(filepath.Join(m.library.Root(), file.Path)) {
			return &existingRecording{track: *known, file: file}, nil
		}
	}
	return nil, nil
}

// alreadyExists is the skip outcome of an item whose recording the release
// already holds.
func alreadyExists(track music.Track) error {
	return apperr.Newf(apperr.CodeAlreadyExists, "%q is already in the library.", track.Label())
}

// notInLibrary is the skip outcome of a copies-only item whose recording the
// library does not hold: a backfill copies, it never downloads.
func notInLibrary(track music.Track) error {
	return apperr.Newf(apperr.CodeTrackNotFound,
		"%q is not in the library; a backfill only copies recordings the library already holds.", track.Label())
}

// handleExisting decides what an item whose recording is already in the
// library needs. The release the item asks for either already holds the
// recording (its home release, or a placement whose file is on disk) and the
// item is skipped, or it is a further release and a copy of the existing file
// is placed there - unless completing releases is switched off. A copies-only
// job is the user's explicit request for those copies, so the setting, which
// is about new downloads, does not stop it.
func (w *worker) handleExisting(ctx context.Context, job Job, item Item, existing *existingRecording, logger *slog.Logger) (ItemStatus, error) {
	m := w.manager
	track := item.Track

	if !m.completeReleases.Load() && !job.Options.CopiesOnly {
		return ItemSkipped, alreadyExists(track)
	}
	needed, err := m.needsCopy(ctx, existing, releaseOf(track))
	if err != nil {
		// Without a reliable answer a copy could land next to the home file;
		// skipping is what the item did before placements existed.
		logger.Warn("placement lookup failed", logging.KeyError, err.Error())
		return ItemSkipped, alreadyExists(track)
	}
	if !needed {
		return ItemSkipped, alreadyExists(track)
	}
	return w.placeCopy(ctx, job, item, existing, logger)
}

// copyOnly is the whole attempt of a copies-only item: the copy decision of a
// recording the library holds, and a skip for one it does not.
func (w *worker) copyOnly(ctx context.Context, job Job, item Item, logger *slog.Logger) (ItemStatus, error) {
	m := w.manager
	// An unreachable library looks like one without the recording, so the
	// item waits for it rather than being skipped as unknown.
	if status, err := m.requireLibrary(); err != nil {
		return status, err
	}
	existing, err := m.findExisting(ctx, item.Track)
	if err != nil {
		return ItemFailed, err
	}
	if existing == nil {
		return ItemSkipped, notInLibrary(item.Track)
	}
	return w.handleExisting(ctx, job, item, existing, logger)
}

// requireLibrary reports the wait state of an item that writes into, or
// looks at, a library that is not usable now.
func (m *Manager) requireLibrary() (ItemStatus, error) {
	if m.library.Guard() == nil {
		return "", nil
	}
	if err := m.library.Guard().RequireWritable(); err != nil {
		if apperr.IsSpaceWait(err) {
			return ItemWaitingSpace, err
		}
		return ItemWaitingStorage, err
	}
	return "", nil
}

// needsCopy reports whether a copy of an existing recording belongs on a
// release: the release must be storable and must not hold the recording yet.
// The worker and the backfill scan both decide by it.
func (m *Manager) needsCopy(ctx context.Context, existing *existingRecording, release music.Release) (bool, error) {
	// A placement row needs a stored release, and a release is only stored
	// with a source id.
	if m.placements == nil || release.SourceID == "" {
		return false, nil
	}
	held, err := m.releaseHolds(ctx, existing, release)
	if err != nil {
		return false, err
	}
	return !held, nil
}

// CopyCheck is what a copies-only item would do with a track.
type CopyCheck struct {
	// Known is false when the library holds no usable file of the recording;
	// the item would be skipped as not in the library.
	Known bool
	// Held is true when the track's release already holds the recording (or
	// cannot hold a placement); the item would be skipped as existing.
	Held bool
	// SizeBytes is the size of the file a copy would be made from.
	SizeBytes int64
}

// CheckCopy answers, without changing anything, what a copies-only item for
// the track would do. It asks the same lookups the worker does, so a preview
// and the job that follows it agree.
func (m *Manager) CheckCopy(ctx context.Context, track music.Track) (CopyCheck, error) {
	existing, err := m.findExisting(ctx, track)
	if err != nil || existing == nil {
		return CopyCheck{}, err
	}
	needed, err := m.needsCopy(ctx, existing, releaseOf(track))
	if err != nil {
		return CopyCheck{}, err
	}
	return CopyCheck{Known: true, Held: !needed, SizeBytes: existing.file.SizeBytes}, nil
}

// releaseHolds reports whether a release already has the recording: it is the
// recording's home release, or a placement on it has its file on disk. A
// release is the same when provider and source id agree, or, when either side
// lacks them, when it lives in the same folder. The folder also counts on its
// own: a release that renders to the folder a file of the recording already
// lives in must not get a second copy beside it.
func (m *Manager) releaseHolds(ctx context.Context, existing *existingRecording, release music.Release) (bool, error) {
	dir := filepath.Clean(storage.ReleaseDirRel(release))
	if filepath.Clean(filepath.Dir(existing.file.Path)) == dir {
		return true, nil
	}

	home, err := m.placements.HomeRelease(ctx, existing.track.ID)
	if err != nil {
		return false, err
	}
	if home == nil {
		// A recording without a home release cannot be told apart from the
		// requested one; it is left as it is.
		return true, nil
	}
	if sameRelease(home.Provider, home.SourceID, release) || filepath.Clean(storage.ReleaseDirRel(*home)) == dir {
		return true, nil
	}

	placements, err := m.placements.ListByTrack(ctx, existing.track.ID)
	if err != nil {
		return false, err
	}
	for _, p := range placements {
		if !sameRelease(p.ReleaseProvider, p.ReleaseSourceID, release) && filepath.Clean(filepath.Dir(p.Path)) != dir {
			continue
		}
		if m.library.Exists(filepath.Join(m.library.Root(), p.Path)) {
			return true, nil
		}
	}
	return false, nil
}

// sameRelease compares a stored release identity with a requested release.
// Only a complete identity on both sides counts.
func sameRelease(provider, sourceID string, release music.Release) bool {
	if provider == "" || sourceID == "" || release.Provider == "" || release.SourceID == "" {
		return false
	}
	return provider == release.Provider && sourceID == release.SourceID
}

// placeCopy puts a copy of a recording's existing library file into the folder
// of a further release, tagged for that release. It needs no provider, no
// yt-dlp run and no match: the audio is the recording the library already
// holds. The copy goes through the same verified no-overwrite commit and
// release-folder lock as a download, and is catalogued in track_placements,
// never in files, because it is not a version of the recording.
func (w *worker) placeCopy(ctx context.Context, job Job, item Item, existing *existingRecording, logger *slog.Logger) (ItemStatus, error) {
	m := w.manager
	track := item.Track
	release := releaseOf(track)
	sourceFile := existing.file
	source := provider.MediaSource{
		Provider: sourceFile.SourceProvider,
		ID:       sourceFile.SourceID,
		URL:      sourceFile.SourceURL,
	}

	// A copy writes into the library, so it waits for it like a download's
	// move does. Nothing is staged: redoing a copy later is cheap.
	if status, err := m.requireLibrary(); err != nil {
		return status, err
	}
	if m.staging != nil {
		if err := m.staging.CheckSpace(); err != nil {
			return ItemWaitingSpace, err
		}
	}

	attempts := item.Attempts + 1
	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		Status:         ItemFinalizing,
		Attempts:       &attempts,
		ClearNextRetry: true,
		MediaProvider:  source.Provider,
		MediaID:        source.ID,
		MediaURL:       source.URL,
	}); err != nil {
		return ItemFailed, err
	}
	m.publishItem(job, item, ItemFinalizing, 0, nil)

	workDir, cleanup, err := m.placementWorkDir(item.ID)
	if err != nil {
		return ItemFailed, err
	}
	defer cleanup()

	sourceAbs := filepath.Join(m.library.Root(), sourceFile.Path)
	ext := filepath.Ext(sourceFile.Path)
	copyPath := filepath.Join(workDir, storage.TrackFileName(track, ext))
	if err := copyFile(sourceAbs, copyPath); err != nil {
		if !m.library.Exists(sourceAbs) {
			// The source vanished since it was looked up; a later attempt
			// decides afresh, possibly for a real download.
			return ItemFailed, apperr.Wrap(apperr.CodeFileNotFound, "The library file to copy is gone.", err)
		}
		return ItemFailed, apperr.Wrap(apperr.CodeInternal, "The library file could not be copied.", err)
	}

	artwork := m.fetchArtwork(ctx, track, logger)
	if err := m.tagger.Apply(ctx, copyPath, metadata.TagsFor(track, source), embedded(artwork, m.embedCover.Load())); err != nil {
		return ItemFailed, err
	}
	sha256, size, err := storage.ComputeChecksum(copyPath)
	if err != nil {
		return ItemFailed, apperr.Wrap(apperr.CodeInternal, "Failed to compute checksum for the copy.", err)
	}

	select {
	case m.finalizerSem <- struct{}{}:
		defer func() { <-m.finalizerSem }()
	case <-ctx.Done():
		return ItemCancelled, ctx.Err()
	}
	unlockRelease := m.library.ShareReleaseDir(storage.ReleaseDirRel(release))
	defer unlockRelease()

	// Finalizations run one at a time, so an item for the same recording and
	// release that finished while this one tagged its copy is seen here, and
	// no second copy is placed beside it.
	if held, err := m.releaseHolds(ctx, existing, release); err == nil && held {
		return ItemSkipped, alreadyExists(track)
	}

	releaseDir, err := m.library.EnsureReleaseDir(release)
	if err != nil {
		return ItemFailed, err
	}
	target, err := m.library.TrackPath(release, track, ext)
	if err != nil {
		return ItemFailed, err
	}
	// Whatever occupies the conventional name is not this placement (it would
	// have been found above), so it is never replaced: an identical file is
	// adopted, anything else sends the copy to the source-suffixed name.
	if _, err := m.commitRecording(copyPath, &target, source, sha256, size); err != nil {
		return ItemFailed, err
	}
	relPath := m.library.RelPath(target)

	if m.writeCoverFile.Load() && artwork != nil {
		m.writeCover(releaseDir, artwork)
	}
	m.copyLyrics(sourceAbs, target, logger)

	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancelPersist()
	stored, err := m.placements.Persist(persistCtx, music.PlacementEntry{
		Artist:  releaseArtist(release, m.canonicalArtistID(ctx, job)),
		Release: release,
		Placement: music.Placement{
			TrackID:      existing.track.ID,
			Path:         relPath,
			SizeBytes:    size,
			SHA256:       sha256,
			DiscNumber:   track.DiscNumber,
			TrackNumber:  track.TrackNumber,
			Title:        track.DisplayTitle(),
			SourceFileID: sourceFile.ID,
		},
	})
	if err != nil {
		m.removeUncatalogued(relPath, sha256, logger)
		return ItemFailed, err
	}

	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		Status:  ItemCompleted,
		TrackID: existing.track.ID,
	}); err != nil {
		return ItemFailed, err
	}
	logger.Info("recording placed on a further release",
		logging.KeyOperation, "place_copy",
		"path", stored.Path,
		"source_path", sourceFile.Path,
		"sha256", sha256)
	return ItemCompleted, nil
}

// placementWorkDir returns a scratch directory for the copy that is being
// tagged. It lives in the item's staging when there is one, so it is on the
// disk set aside for work in progress, and it is always removed afterwards.
func (m *Manager) placementWorkDir(itemID string) (string, func(), error) {
	parent := m.tempDir
	if m.staging != nil {
		dir, err := m.staging.EnsureItemDir(itemID)
		if err != nil {
			return "", nil, err
		}
		parent = dir
	}
	dir, err := os.MkdirTemp(parent, "ytdm-placement-")
	if err != nil {
		return "", nil, apperr.Wrap(apperr.CodeInternal, "The temporary directory could not be created.", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// copyLyrics gives the copy the lyrics sidecar its source has. No lyrics
// provider is asked: the copy is the same recording.
func (m *Manager) copyLyrics(sourceAudio, targetAudio string, logger *slog.Logger) {
	path, body, err := m.library.ReadLyrics(sourceAudio)
	if err != nil || strings.TrimSpace(body) == "" {
		return
	}
	lyrics := music.Lyrics{PlainText: body}
	if strings.EqualFold(filepath.Ext(path), ".lrc") {
		lyrics = music.Lyrics{Synced: true, LRC: body}
	}
	if _, err := m.library.WriteLyrics(targetAudio, lyrics); err != nil {
		logger.Warn("lyrics sidecar could not be copied",
			logging.KeyOperation, "lyrics", logging.KeyError, err.Error())
	}
}

// copyFile copies a file byte for byte into a new file.
func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
