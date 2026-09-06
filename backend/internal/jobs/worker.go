package jobs

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// progressInterval throttles how often a download progress event is published.
// Without it a fast download would produce hundreds of events per second.
const progressInterval = 750 * time.Millisecond

// persistTimeout bounds the catalogue write that follows a finished download.
// It runs detached from the job context, so it needs a limit of its own.
const persistTimeout = 10 * time.Second

// worker processes single job items. One worker instance is shared by all
// goroutines; it holds no mutable state of its own.
type worker struct {
	manager *Manager
}

// process runs one item through its lifecycle and handles retries, waits, and finalization.
func (w *worker) process(ctx context.Context, job Job, item Item) {
	m := w.manager
	logger := m.logger.With(
		logging.KeyJobID, job.ID,
		logging.KeyJobItemID, item.ID,
		logging.KeyTrack, item.Label,
	)

	if err := ctx.Err(); err != nil {
		if m.Stopping() {
			return
		}
		w.finishItem(job, item, ItemCancelled, apperr.New(apperr.CodeJobCancelled, "The job was cancelled."))
		return
	}

	outcome, err := w.attempt(ctx, job, item, logger)
	switch {
	case err == nil:
		w.finishItem(job, item, outcome, nil)
		return

	case apperr.CodeOf(err) == apperr.CodeJobCancelled, errors.Is(err, context.Canceled):
		if m.Stopping() {
			return
		}
		w.finishItem(job, item, ItemCancelled, err)
		return

	case apperr.CodeOf(err) == apperr.CodeAlreadyExists:
		w.finishItem(job, item, ItemSkipped, err)
		return

	case apperr.IsStorageWait(err):
		logger.Warn("item waiting for storage", logging.KeyErrorCode, string(apperr.CodeOf(err)), logging.KeyError, err.Error())
		_ = m.updateItem(ctx, item.ID, ItemUpdate{
			Status:       ItemWaitingStorage,
			ErrorCode:    string(apperr.CodeOf(err)),
			ErrorMessage: apperr.MessageOf(err),
		})
		m.publishItem(job, item, ItemWaitingStorage, 0, err)
		return

	case apperr.IsSpaceWait(err):
		logger.Warn("item waiting for space", logging.KeyErrorCode, string(apperr.CodeOf(err)), logging.KeyError, err.Error())
		_ = m.updateItem(ctx, item.ID, ItemUpdate{
			Status:       ItemWaitingSpace,
			ErrorCode:    string(apperr.CodeOf(err)),
			ErrorMessage: apperr.MessageOf(err),
		})
		m.publishItem(job, item, ItemWaitingSpace, 0, err)
		return

	case apperr.Retryable(err):
		maxAttempts := item.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 5
		}
		newAttempts := item.Attempts + 1
		if newAttempts < maxAttempts {
			backoff := calculateBackoff(newAttempts)
			nextRetry := time.Now().Add(backoff)
			logger.Info("scheduling retry for item",
				"attempt", newAttempts, "max_attempts", maxAttempts,
				"retry_in_ms", backoff.Milliseconds(),
				logging.KeyErrorCode, string(apperr.CodeOf(err)))

			_ = m.updateItem(ctx, item.ID, ItemUpdate{
				Status:       ItemRetryWait,
				Attempts:     &newAttempts,
				NextRetryAt:  &nextRetry,
				ErrorCode:    string(apperr.CodeOf(err)),
				ErrorMessage: apperr.MessageOf(err),
			})
			m.publishItem(job, item, ItemRetryWait, 0, err)
			return
		}

		logger.Error("item exhausted all retry attempts",
			"attempts", newAttempts, "max_attempts", maxAttempts,
			logging.KeyErrorCode, string(apperr.CodeOf(err)))
		w.finishItem(job, item, ItemFailed, err)
		return

	default:
		logger.Error("item failed with permanent error",
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
		w.finishItem(job, item, ItemFailed, err)
		return
	}
}

// calculateBackoff calculates jittered exponential backoff for attempt count.
func calculateBackoff(attempt int) time.Duration {
	var base time.Duration
	switch attempt {
	case 1:
		base = 5 * time.Second
	case 2:
		base = 15 * time.Second
	case 3:
		base = 45 * time.Second
	case 4:
		base = 2 * time.Minute
	default:
		base = 5 * time.Minute
	}
	// Jitter: +/- 20%
	factor := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(base) * factor)
}

// attempt runs one processing attempt for an item.
func (w *worker) attempt(ctx context.Context, job Job, item Item, logger *slog.Logger) (ItemStatus, error) {
	m := w.manager
	track := item.Track
	attempts := item.Attempts + 1

	// 1. Skip check: check if already in library
	if job.Options.SkipExisting {
		exists, err := m.alreadyInLibrary(ctx, track)
		if err != nil {
			logger.Warn("library lookup failed", logging.KeyError, err.Error())
		} else if exists {
			return ItemSkipped, apperr.Newf(apperr.CodeAlreadyExists,
				"%q is already in the library.", track.Label())
		}
	}

	// 2. Storage Guard pre-flight check (unless offline staging is allowed)
	if m.library.Guard() != nil && !m.allowOfflineStaging.Load() {
		if err := m.library.Guard().RequireWritable(); err != nil {
			return ItemWaitingStorage, err
		}
	}

	// 3. Staging space check
	if m.staging != nil {
		if err := m.staging.CheckSpace(); err != nil {
			return ItemWaitingSpace, err
		}
	}

	// 4. Staging setup
	var itemDir string
	if m.staging != nil {
		var err error
		itemDir, err = m.staging.EnsureItemDir(item.ID)
		if err != nil {
			return ItemFailed, err
		}
	} else {
		var err error
		itemDir, err = os.MkdirTemp(m.tempDir, "ytdm-item-")
		if err != nil {
			return ItemFailed, err
		}
		defer os.RemoveAll(itemDir)
	}

	// 5. Matching & Provider Resolution with Rate Limit Cooldown
	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		Status:         ItemMatching,
		Attempts:       &attempts,
		ClearNextRetry: true,
	}); err != nil {
		return ItemFailed, err
	}
	m.publishItem(job, item, ItemMatching, 0, nil)

	if m.cooldown != nil {
		if err := m.cooldown.Wait(ctx, job.MediaProvider); err != nil {
			return ItemFailed, err
		}
	}

	rankedCandidates, err := m.matchCandidates(ctx, job, track, DefaultMaxFallbackCandidates)
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderRateLimited && m.cooldown != nil {
			m.cooldown.Trigger(job.MediaProvider, 60*time.Second)
		}
		return ItemFailed, err
	}

	logger.Info("media candidates matched",
		"count", len(rankedCandidates),
		"best_score", rankedCandidates[0].Score,
		"best_media_id", rankedCandidates[0].Candidate.ID,
		"reason", rankedCandidates[0].Reason())

	var (
		selectedResult  matcher.Result
		selectedSource  *provider.MediaSource
		resolvedSuccess bool
		attemptedCount  int
		lastResolveErr  error
	)

	for rankIdx, candResult := range rankedCandidates {
		candidate := candResult.Candidate
		attemptedCount++

		if err := ctx.Err(); err != nil {
			return ItemFailed, apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
		}

		if m.cooldown != nil {
			if err := m.cooldown.Wait(ctx, candidate.Provider); err != nil {
				return ItemFailed, err
			}
		}

		mediaProvider, err := m.registry.Media(candidate.Provider)
		if err != nil {
			lastResolveErr = err
			break
		}

		source, err := mediaProvider.Resolve(ctx, candidate)
		if err == nil {
			selectedResult = candResult
			selectedSource = source
			resolvedSuccess = true
			if rankIdx > 0 {
				logger.Info("media resolved using fallback candidate",
					logging.KeyProvider, candidate.Provider,
					logging.KeyOperation, "resolve_fallback",
					"media_id", candidate.ID,
					"rank", rankIdx+1,
					"score", candResult.Score,
					"total_candidates", len(rankedCandidates),
					"reason", candResult.Reason())
			} else {
				logger.Info("media matched and resolved",
					logging.KeyProvider, candidate.Provider,
					logging.KeyOperation, "match",
					"media_id", candidate.ID,
					"rank", 1,
					"score", candResult.Score,
					"reason", candResult.Reason())
			}
			break
		}

		lastResolveErr = err
		code := apperr.CodeOf(err)

		// Systemic stop: If error is systemic rate limiting, provider outage, or auth failure,
		// stop candidate fan-out immediately to prevent hammering subsequent candidates.
		if isSystemicResolutionError(err) {
			logger.Warn("candidate resolution encountered systemic error, stopping fallback",
				logging.KeyProvider, candidate.Provider,
				"media_id", candidate.ID,
				"rank", rankIdx+1,
				logging.KeyErrorCode, string(code),
				logging.KeyError, err.Error())
			if code == apperr.CodeProviderRateLimited && m.cooldown != nil {
				m.cooldown.Trigger(candidate.Provider, 60*time.Second)
			}
			break
		}

		// Candidate-specific failure (e.g. CodeTrackNotFound, unresolvable specific candidate) -> try next
		logger.Warn("candidate resolution failed, attempting fallback",
			logging.KeyProvider, candidate.Provider,
			"media_id", candidate.ID,
			"rank", rankIdx+1,
			"score", candResult.Score,
			logging.KeyErrorCode, string(code),
			logging.KeyError, err.Error())
	}

	// If direct-ID was the probed candidate and failed resolution, try generic search candidates
	if !resolvedSuccess && track.SourceID != "" && !isSystemicResolutionError(lastResolveErr) {
		fallbackTrack := track
		fallbackTrack.SourceID = ""
		if fallbackCandidates, err := m.matchCandidates(ctx, job, fallbackTrack, DefaultMaxFallbackCandidates); err == nil {
			for rankIdx, candResult := range fallbackCandidates {
				candidate := candResult.Candidate
				attemptedCount++
				if err := ctx.Err(); err != nil {
					return ItemFailed, apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
				}
				if m.cooldown != nil {
					if err := m.cooldown.Wait(ctx, candidate.Provider); err != nil {
						return ItemFailed, err
					}
				}
				mediaProvider, err := m.registry.Media(candidate.Provider)
				if err != nil {
					lastResolveErr = err
					break
				}
				source, err := mediaProvider.Resolve(ctx, candidate)
				if err == nil {
					selectedResult = candResult
					selectedSource = source
					resolvedSuccess = true
					logger.Info("media resolved using generic search fallback candidate",
						logging.KeyProvider, candidate.Provider,
						logging.KeyOperation, "resolve_fallback",
						"media_id", candidate.ID,
						"rank", rankIdx+1,
						"score", candResult.Score)
					break
				}
				lastResolveErr = err
				if isSystemicResolutionError(err) {
					if apperr.CodeOf(err) == apperr.CodeProviderRateLimited && m.cooldown != nil {
						m.cooldown.Trigger(candidate.Provider, 60*time.Second)
					}
					break
				}
			}
		}
	}

	if !resolvedSuccess {
		if isSystemicResolutionError(lastResolveErr) {
			return ItemFailed, lastResolveErr
		}
		return ItemFailed, apperr.Wrapf(apperr.CodeTrackNotFound, lastResolveErr,
			"Keine der %d passenden Quellen konnte aufgelöst werden.", attemptedCount)
	}

	result := selectedResult
	source := selectedSource
	if source.DurationMS == 0 {
		source.DurationMS = track.DurationMS
	}

	// 6. Downloading to persistent staging
	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		Status:        ItemDownloading,
		MediaProvider: result.Candidate.Provider,
		MediaID:       result.Candidate.ID,
		MediaURL:      source.URL,
		MatchScore:    result.Score,
	}); err != nil {
		return ItemFailed, err
	}
	m.publishItem(job, item, ItemDownloading, result.Score, nil)

	release := releaseOf(track)
	stagedAudioDest := filepath.Join(itemDir, storage.TrackFileName(track, ".opus"))

	lastPublish := time.Now()
	download, err := m.downloader.Download(ctx, *source, stagedAudioDest, func(p downloader.Progress) {
		if time.Since(lastPublish) < progressInterval {
			return
		}
		lastPublish = time.Now()
		m.publishProgress(job, item, p.Percent)
	})
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeProviderRateLimited && m.cooldown != nil {
			m.cooldown.Trigger(result.Candidate.Provider, 60*time.Second)
		}
		if apperr.CodeOf(err) == apperr.CodeMediaVerifyFailed && m.staging != nil {
			_ = m.staging.ResetCorruptedAudio(item.ID)
		}
		return ItemFailed, err
	}

	// 7. Tagging
	if err := m.updateItem(ctx, item.ID, ItemUpdate{Status: ItemTagging}); err != nil {
		return ItemFailed, err
	}
	m.publishItem(job, item, ItemTagging, result.Score, nil)

	artwork := m.fetchArtwork(ctx, track, logger)
	if err := m.tagger.Apply(ctx, download.Path, metadata.TagsFor(track, *source), embedded(artwork, m.embedCover.Load())); err != nil {
		return ItemFailed, err
	}

	// 8. Staging Checksum & Metadata Persistence
	stagedHash, stagedSize, err := storage.ComputeChecksum(download.Path)
	if err != nil {
		return ItemFailed, apperr.Wrap(apperr.CodeInternal, "Failed to compute checksum for staged audio.", err)
	}

	stagingRelPath := filepath.Base(download.Path)
	if m.staging != nil {
		stagingRelPath = m.staging.RelPath(download.Path)
		_ = m.staging.SaveMeta(item.ID, storage.StagingMeta{
			ItemID:       item.ID,
			StagedRel:    stagingRelPath,
			StagedSize:   stagedSize,
			StagedSHA256: stagedHash,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})
	}

	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		StagingRelPath: &stagingRelPath,
		StagedSize:     &stagedSize,
		StagedSHA256:   &stagedHash,
	}); err != nil {
		return ItemFailed, err
	}

	// 9. Storage & Space Pre-Finalization Check
	if m.library.Guard() != nil {
		if err := m.library.Guard().RequireWritable(); err != nil {
			return ItemWaitingStorage, err
		}
	}

	// 10. Finalization (with bounded concurrency slot)
	if err := m.updateItem(ctx, item.ID, ItemUpdate{Status: ItemFinalizing}); err != nil {
		return ItemFailed, err
	}
	m.publishItem(job, item, ItemFinalizing, result.Score, nil)

	select {
	case m.finalizerSem <- struct{}{}:
		defer func() { <-m.finalizerSem }()
	case <-ctx.Done():
		return ItemCancelled, ctx.Err()
	}

	file, err := m.placeSafe(ctx, release, track, download, artwork, *source, stagedHash, stagedSize)
	if err != nil {
		return ItemFailed, err
	}

	// 11. Lyrics Attachment (best-effort)
	track = m.attachLyrics(ctx, track, filepath.Join(m.library.Root(), file.Path), source.ID, logger)

	// 12. Database Transaction Commit (PersistDownload)
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancelPersist()

	canonicalArtistID := job.Options.CanonicalArtistID
	if canonicalArtistID == "" && job.MetadataProvider != "" && job.TargetID != "" {
		if a, _ := m.catalog.FindArtistBySource(ctx, job.MetadataProvider, job.TargetID); a != nil {
			canonicalArtistID = a.ID
		}
	}

	stored, err := m.persist(persistCtx, track, release, *source, file, canonicalArtistID)
	if err != nil {
		return ItemFailed, err
	}

	// 13. Cleanup local staging ONLY after DB transaction is committed
	if m.staging != nil {
		_ = m.staging.CleanupItem(item.ID)
	}

	if err := m.updateItem(ctx, item.ID, ItemUpdate{
		Status:  ItemCompleted,
		FileID:  stored.File.ID,
		TrackID: stored.TrackID,
	}); err != nil {
		return ItemFailed, err
	}

	logger.Info("track stored and finalized",
		logging.KeyOperation, "store",
		"path", stored.File.Path,
		"codec", stored.File.Codec,
		"bitrate_kbps", stored.File.BitrateKbps,
		"sha256", stagedHash)

	return ItemCompleted, nil
}

// placeSafe commits the staged file into the library using safe copy-commit,
// idempotent hash validation, and writing artwork.
func (m *Manager) placeSafe(ctx context.Context, release music.Release, track music.Track,
	download *downloader.Result, artwork *metadata.Artwork, source provider.MediaSource,
	expectedSHA256 string, expectedSize int64) (music.File, error) {

	releaseDir, err := m.library.EnsureReleaseDir(release)
	if err != nil {
		return music.File{}, err
	}

	target, err := m.library.TrackPath(release, track, filepath.Ext(download.Path))
	if err != nil {
		return music.File{}, err
	}

	if m.library.Exists(target) {
		owned, err := m.ownsTarget(ctx, track, m.library.RelPath(target))
		if err != nil {
			return music.File{}, err
		}
		if owned {
			if err := m.library.Replace(download.Path, target); err != nil {
				return music.File{}, err
			}
		} else {
			alreadyPlaced, err := m.library.CommitStaged(download.Path, target, expectedSHA256, expectedSize)
			if err != nil {
				return music.File{}, err
			}
			if alreadyPlaced {
				m.logger.Info("idempotently recovered already committed target file",
					"target", m.library.RelPath(target))
			}
		}
	} else {
		alreadyPlaced, err := m.library.CommitStaged(download.Path, target, expectedSHA256, expectedSize)
		if err != nil {
			return music.File{}, err
		}
		if alreadyPlaced {
			m.logger.Info("idempotently recovered already committed target file",
				"target", m.library.RelPath(target))
		}
	}

	if m.writeCoverFile.Load() && artwork != nil {
		m.writeCover(releaseDir, artwork)
	}

	return music.File{
		Path:           m.library.RelPath(target),
		SizeBytes:      expectedSize,
		Codec:          download.Info.Codec,
		Container:      download.Info.Container,
		BitrateKbps:    download.Info.BitrateKbps,
		SampleRate:     download.Info.SampleRate,
		Channels:       download.Info.Channels,
		DurationMS:     download.Info.DurationMS,
		SourceProvider: source.Provider,
		SourceID:       source.ID,
		SourceURL:      source.URL,
	}, nil
}

// ownsTarget reports whether a library path already belongs to the recording
// that is about to be written there.
func (m *Manager) ownsTarget(ctx context.Context, track music.Track, relPath string) (bool, error) {
	existing, err := m.files.FindByPath(ctx, relPath)
	if err != nil {
		return false, err
	}
	if existing == nil || existing.TrackID == "" {
		return false, nil
	}
	known, err := m.catalog.FindTrack(ctx, track, m.toleranceMS)
	if err != nil {
		return false, err
	}
	return known != nil && known.ID == existing.TrackID, nil
}

// place is the backwards-compatible entry point for placing downloads in the library.
func (m *Manager) place(ctx context.Context, release music.Release, track music.Track,
	download *downloader.Result, artwork *metadata.Artwork, source provider.MediaSource) (music.File, error) {
	hash, size, err := storage.ComputeChecksum(download.Path)
	if err != nil {
		hash = ""
		size = download.Info.SizeBytes
	}
	return m.placeSafe(ctx, release, track, download, artwork, source, hash, size)
}

// writeCover writes the release cover under the name that matches the image's
// real format. An existing cover is left alone.
func (m *Manager) writeCover(releaseDir string, artwork *metadata.Artwork) {
	existing, err := m.library.Layout().CoverPaths(releaseDir)
	if err != nil {
		m.logger.Warn("cover path could not be built",
			logging.KeyOperation, "artwork", logging.KeyError, err.Error())
		return
	}
	for _, path := range existing {
		if m.library.Exists(path) {
			return
		}
	}
	if _, err := m.library.WriteCover(releaseDir, artwork.Extension(), artwork.Data); err != nil {
		m.logger.Warn("cover file could not be written",
			logging.KeyOperation, "artwork", logging.KeyError, err.Error())
	}
}

// attachLyrics resolves the lyrics of a finished track, writes the sidecar and
// returns the track with the outcome recorded on it.
func (m *Manager) attachLyrics(ctx context.Context, track music.Track,
	audioPath, mediaID string, logger *slog.Logger) music.Track {

	if m.lyrics == nil || !m.lyricsEnabled.Load() {
		return track
	}

	result, err := m.lyrics.Resolve(ctx, track, mediaID)
	switch {
	case err != nil && errors.Is(err, lyrics.ErrNoLyrics):
		track.LyricsState = music.LyricsNotFound
		track.LyricsProvider = ""
		track.LyricsCheckedAt = nowPtr()
		return track

	case err != nil:
		logger.Warn("lyrics could not be looked up",
			logging.KeyOperation, "lyrics",
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
		return track

	case result == nil:
		return track
	}

	if _, err := m.library.WriteLyrics(audioPath, *result); err != nil {
		logger.Warn("lyrics sidecar could not be written",
			logging.KeyOperation, "lyrics", logging.KeyError, err.Error())
		return track
	}

	track.LyricsState = result.State()
	track.LyricsProvider = result.Provider
	track.LyricsCheckedAt = nowPtr()
	return track
}

// nowPtr returns a pointer to the current UTC time.
func nowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}

// alreadyInLibrary reports whether the recording already exists on disk.
func (m *Manager) alreadyInLibrary(ctx context.Context, track music.Track) (bool, error) {
	existing, err := m.catalog.FindTrack(ctx, track, m.toleranceMS)
	if err != nil || existing == nil {
		return false, err
	}
	files, err := m.files.ListByTrack(ctx, existing.ID)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		if m.library.Exists(filepath.Join(m.library.Root(), file.Path)) {
			return true, nil
		}
	}
	return false, nil
}

// DefaultMaxFallbackCandidates bounds the number of ranked acceptable candidates
// evaluated during media resolution.
const DefaultMaxFallbackCandidates = 5

// isSystemicResolutionError reports whether an error from media resolution indicates
// a systemic condition (rate limiting, provider outage, network partition, auth challenge)
// that would affect subsequent candidates, meaning candidate fan-out should stop immediately.
func isSystemicResolutionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch apperr.CodeOf(err) {
	case apperr.CodeProviderRateLimited, apperr.CodeProviderUnavailable, apperr.CodeToolUnavailable, apperr.CodeJobCancelled:
		return true
	default:
		return false
	}
}

// matchCandidates queries the media providers in chain and returns up to maxCandidates
// acceptable candidates, sorted descending by score and deduplicated by candidate ID.
func (m *Manager) matchCandidates(ctx context.Context, job Job, track music.Track, maxCandidates int) ([]matcher.Result, error) {
	if maxCandidates <= 0 {
		maxCandidates = DefaultMaxFallbackCandidates
	}

	providers := m.registry.MediaChain(job.MediaProvider)
	if len(providers) == 0 {
		return nil, apperr.New(apperr.CodeProviderNotFound, "No media provider is configured.")
	}

	var (
		allAcceptable []matcher.Result
		lastErr       error
		haveBest      bool
		bestResult    matcher.Result
	)
	for _, mediaProvider := range providers {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeJobCancelled, "The job was cancelled.", err)
		}

		candidates, err := mediaProvider.Search(ctx, track)
		if err != nil {
			if track.SourceID != "" {
				fallbackTrack := track
				fallbackTrack.SourceID = ""
				candidates, err = mediaProvider.Search(ctx, fallbackTrack)
			}
			if err != nil {
				lastErr = err
				continue
			}
		}

		acceptable := m.matcher.Acceptable(track, candidates, maxCandidates)
		if len(acceptable) > 0 {
			allAcceptable = append(allAcceptable, acceptable...)
			continue
		}

		// If direct-ID was probed but failed the match score criteria, retry with generic search
		if track.SourceID != "" && len(candidates) == 1 && candidates[0].ID == track.SourceID {
			fallbackTrack := track
			fallbackTrack.SourceID = ""
			fallbackCandidates, fallbackErr := mediaProvider.Search(ctx, fallbackTrack)
			if fallbackErr == nil && len(fallbackCandidates) > 0 {
				fallbackAcceptable := m.matcher.Acceptable(track, fallbackCandidates, maxCandidates)
				if len(fallbackAcceptable) > 0 {
					allAcceptable = append(allAcceptable, fallbackAcceptable...)
					continue
				}
				ranked := m.matcher.Rank(track, fallbackCandidates)
				if len(ranked) > 0 && (!haveBest || ranked[0].Score > bestResult.Score) {
					bestResult = ranked[0]
					haveBest = true
				}
				continue
			}
		}

		ranked := m.matcher.Rank(track, candidates)
		if len(ranked) > 0 && (!haveBest || ranked[0].Score > bestResult.Score) {
			bestResult = ranked[0]
			haveBest = true
		}
	}

	if len(allAcceptable) > 0 {
		seen := make(map[string]struct{}, len(allAcceptable))
		deduped := make([]matcher.Result, 0, len(allAcceptable))
		for _, res := range allAcceptable {
			id := strings.TrimSpace(res.Candidate.ID)
			if id != "" {
				if _, exists := seen[id]; exists {
					continue
				}
				seen[id] = struct{}{}
			}
			deduped = append(deduped, res)
			if len(deduped) >= maxCandidates {
				break
			}
		}
		return deduped, nil
	}

	if haveBest {
		return nil, apperr.Newf(apperr.CodeMatchFailed,
			"No sufficiently accurate media match found for %q (best score %.1f, required %.1f).",
			track.Label(), bestResult.Score, m.matcher.MinScore())
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, apperr.Newf(apperr.CodeMatchFailed,
		"No media candidates were found for %q.", track.Label())
}

// match asks the media providers for candidates and picks the best one.
func (m *Manager) match(ctx context.Context, job Job, track music.Track) (matcher.Result, error) {
	results, err := m.matchCandidates(ctx, job, track, 1)
	if err != nil {
		return matcher.Result{}, err
	}
	return results[0], nil
}

// fetchArtwork loads the cover of a track.
func (m *Manager) fetchArtwork(ctx context.Context, track music.Track, logger *slog.Logger) *metadata.Artwork {
	if strings.TrimSpace(track.CoverURL) == "" {
		return nil
	}
	artwork, err := m.artwork.Fetch(ctx, track.CoverURL)
	if err != nil {
		logger.Warn("cover could not be fetched",
			logging.KeyOperation, "artwork",
			logging.KeyErrorCode, string(apperr.CodeOf(err)),
			logging.KeyError, err.Error())
		return nil
	}
	return artwork
}

// embedded returns the artwork only when embedding is switched on.
func embedded(artwork *metadata.Artwork, embed bool) *metadata.Artwork {
	if !embed {
		return nil
	}
	return artwork
}

// persist writes the catalogue entries for a finished track.
func (m *Manager) persist(ctx context.Context, track music.Track, release music.Release,
	source provider.MediaSource, file music.File, canonicalArtistID ...string) (music.StoredEntry, error) {

	entry := music.LibraryEntry{Track: track, File: file}

	var canonID string
	if len(canonicalArtistID) > 0 {
		canonID = strings.TrimSpace(canonicalArtistID[0])
	}

	if release.Provider != "" && release.SourceID != "" {
		artistName := release.DisplayAlbumArtist()
		if len(release.Artists) > 0 && music.PrimaryArtist(release.Artists) != "" {
			artistName = music.PrimaryArtist(release.Artists)
		}
		entry.Artist = &music.Artist{
			ID:       canonID,
			Name:     artistName,
			Provider: release.Provider,
			SourceID: "artist:" + matcher.NormalizeArtist(artistName),
		}
	}
	if release.SourceID != "" {
		entry.Release = &release
	}
	if track.SourceProvider != "" && track.SourceID != "" {
		entry.Sources = append(entry.Sources, music.Source{
			Provider:  track.SourceProvider,
			Kind:      music.SourceMetadata,
			SourceID:  track.SourceID,
			SourceURL: track.SourceURL,
		})
	}
	if source.Provider != "" && source.ID != "" {
		entry.Sources = append(entry.Sources, music.Source{
			Provider:  source.Provider,
			Kind:      music.SourceMedia,
			SourceID:  source.ID,
			SourceURL: source.URL,
		})
	}

	return m.catalog.PersistDownload(ctx, entry, m.toleranceMS)
}

// releaseOf reconstructs the release a track belongs to.
func releaseOf(track music.Track) music.Release {
	title := strings.TrimSpace(track.Album)
	releaseType := track.ReleaseType
	if title == "" {
		title = track.DisplayTitle()
		if releaseType == "" {
			releaseType = music.ReleaseSingle
		}
	}
	if releaseType == "" {
		releaseType = music.ReleaseAlbum
	}

	albumArtist := track.DisplayAlbumArtist()
	return music.Release{
		Title:       title,
		Artists:     []string{albumArtist},
		AlbumArtist: albumArtist,
		ReleaseType: releaseType,
		Year:        track.Year,
		TrackCount:  track.TrackTotal,
		CoverURL:    track.CoverURL,
		Provider:    track.SourceProvider,
		SourceID:    track.ReleaseID,
	}
}

// finishItem applies the final item status.
func (w *worker) finishItem(job Job, item Item, status ItemStatus, err error) {
	m := w.manager
	ctx := context.Background()

	var errorCode, errorMessage string
	if err != nil {
		errorCode = string(apperr.CodeOf(err))
		errorMessage = apperr.MessageOf(err)
	}

	// Clean up local staging on non-retryable finished/skipped/cancelled outcomes
	if (status == ItemCompleted || status == ItemSkipped || status == ItemCancelled) && m.staging != nil {
		_ = m.staging.CleanupItem(item.ID)
	}

	_ = m.updateItem(ctx, item.ID, ItemUpdate{
		Status:         status,
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
		ClearNextRetry: true,
	})
	m.publishItem(job, item, status, 0, err)
}
