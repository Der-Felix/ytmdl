package library

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
)

// ArtworkRepairStatus describes the evaluation or execution outcome for a release.
type ArtworkRepairStatus string

const (
	RepairStatusNeedsRefresh   ArtworkRepairStatus = "NEEDS_REFRESH"
	RepairStatusAlreadyCorrect ArtworkRepairStatus = "ALREADY_CORRECT"
	RepairStatusProviderFailed ArtworkRepairStatus = "PROVIDER_FAILED"
	RepairStatusImageInvalid   ArtworkRepairStatus = "IMAGE_INVALID"
	RepairStatusApplied        ArtworkRepairStatus = "APPLIED"
	RepairStatusPartialFailed  ArtworkRepairStatus = "PARTIALLY_FAILED"
	RepairStatusFailed         ArtworkRepairStatus = "FAILED"
)

// ArtworkRepairPreview holds dry-run inspection results for a release's artwork.
type ArtworkRepairPreview struct {
	ReleaseID       string              `json:"release_id"`
	ReleaseTitle    string              `json:"release_title"`
	ArtistName      string              `json:"artist_name"`
	Provider        string              `json:"provider"`
	SourceID        string              `json:"source_id"`
	CurrentCoverURL string              `json:"current_cover_url"`
	NewCoverURL     string              `json:"new_cover_url,omitempty"`
	Status          ArtworkRepairStatus `json:"status"`
	Message         string              `json:"message,omitempty"`
	CoverPath       string              `json:"cover_path"`
	CoverExists     bool                `json:"cover_exists"`
	TracksAffected  int                 `json:"tracks_affected"`
	AudioFiles      []string            `json:"audio_files"`
}

// ArtworkRepairResult holds the execution outcome of applying artwork repair on a release.
type ArtworkRepairResult struct {
	ReleaseID      string              `json:"release_id"`
	ReleaseTitle   string              `json:"release_title"`
	Status         ArtworkRepairStatus `json:"status"`
	NewCoverURL    string              `json:"new_cover_url"`
	CoverPath      string              `json:"cover_path"`
	CoverWritten   bool                `json:"cover_written"`
	TracksRepaired int                 `json:"tracks_repaired"`
	TracksFailed   int                 `json:"tracks_failed"`
	Message        string              `json:"message,omitempty"`
}

// PreviewReleaseArtwork evaluates whether a release needs an artwork refresh without modifying any files or DB records.
func (s *Service) PreviewReleaseArtwork(ctx context.Context, releaseID string) (*ArtworkRepairPreview, error) {
	if s.catalog == nil {
		return nil, apperr.New(apperr.CodeInternal, "Catalog store is not configured.")
	}

	release, err := s.catalog.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, apperr.Newf(apperr.CodeReleaseNotFound, "Release %q was not found.", releaseID)
	}

	// 1. Resolve tracks and corresponding audio files on disk (DB-catalogued only)
	tracks, err := s.catalog.ListTracks(ctx, releaseID, 500, 0)
	if err != nil {
		return nil, err
	}

	audioFiles := make([]string, 0, len(tracks))
	for _, track := range tracks {
		files, err := s.files.ListByTrack(ctx, track.ID)
		if err != nil {
			continue
		}
		for _, f := range files {
			absPath := filepath.Join(s.library.Root(), f.Path)
			if s.library.Exists(absPath) {
				audioFiles = append(audioFiles, f.Path)
			}
		}
	}

	// 2. Determine release directory and sidecar path
	releaseDir, err := s.library.Layout().ReleaseDir(*release)
	if err != nil {
		return nil, err
	}
	coverPath, err := s.library.Layout().CoverPath(releaseDir, ".jpg")
	if err != nil {
		return nil, err
	}
	coverExists := s.library.Exists(coverPath)

	preview := &ArtworkRepairPreview{
		ReleaseID:       release.ID,
		ReleaseTitle:    release.DisplayTitle(),
		ArtistName:      release.DisplayAlbumArtist(),
		Provider:        release.Provider,
		SourceID:        release.SourceID,
		CurrentCoverURL: release.CoverURL,
		CoverPath:       s.library.RelPath(coverPath),
		CoverExists:     coverExists,
		TracksAffected:  len(tracks),
		AudioFiles:      audioFiles,
		Status:          RepairStatusNeedsRefresh,
	}

	// 3. Query the metadata provider using release.SourceID
	if s.providers == nil {
		preview.Status = RepairStatusProviderFailed
		preview.Message = "No provider registry is configured on the service."
		return preview, nil
	}

	metaProv, err := s.providers.Metadata(release.Provider)
	if err != nil {
		preview.Status = RepairStatusProviderFailed
		preview.Message = fmt.Sprintf("Metadata provider %q is unavailable: %v", release.Provider, err)
		return preview, nil
	}

	upToDateRelease, err := metaProv.GetRelease(ctx, release.SourceID)
	if err != nil {
		preview.Status = RepairStatusProviderFailed
		preview.Message = fmt.Sprintf("Failed to query release metadata: %v", err)
		return preview, nil
	}

	if upToDateRelease == nil || strings.TrimSpace(upToDateRelease.CoverURL) == "" {
		preview.Status = RepairStatusProviderFailed
		preview.Message = "Provider returned empty cover URL for this release."
		return preview, nil
	}

	preview.NewCoverURL = upToDateRelease.CoverURL

	// 4. Check if already correct: same URL, cover.jpg exists, and tracks have matching URL
	if preview.CurrentCoverURL == preview.NewCoverURL && coverExists {
		preview.Status = RepairStatusAlreadyCorrect
		preview.Message = "Artwork and cover.jpg are already up-to-date."
	}

	return preview, nil
}

// ApplyReleaseArtwork executes the artwork repair for a single release:
// 1. Revalidates storage guard writability
// 2. Queries provider for true release artwork URL (or uses pre-computed preview)
// 3. Fetches and validates image format, dimensions, size
// 4. Atomically writes <Release>/cover.jpg on CIFS/SMB storage
// 5. Losslessly updates embedded cover on all catalogued .opus tracks (zero audio re-encoding)
// 6. Updates catalog releases.cover_url and tracks.cover_url
func (s *Service) ApplyReleaseArtwork(ctx context.Context, releaseID string, preview *ArtworkRepairPreview) (*ArtworkRepairResult, error) {
	// Storage Guard revalidation
	if s.library.Guard() != nil {
		if err := s.library.Guard().RequireWritable(); err != nil {
			return nil, err
		}
	}

	var err error
	if preview == nil || preview.NewCoverURL == "" {
		preview, err = s.PreviewReleaseArtwork(ctx, releaseID)
		if err != nil {
			return nil, err
		}
	}

	res := &ArtworkRepairResult{
		ReleaseID:    releaseID,
		ReleaseTitle: preview.ReleaseTitle,
		NewCoverURL:  preview.NewCoverURL,
		CoverPath:    preview.CoverPath,
		Status:       RepairStatusFailed,
	}

	if preview.Status == RepairStatusAlreadyCorrect {
		res.Status = RepairStatusAlreadyCorrect
		res.Message = "Release artwork is already correct; no files modified."
		return res, nil
	}

	if preview.NewCoverURL == "" {
		res.Status = RepairStatusProviderFailed
		res.Message = preview.Message
		return res, nil
	}

	// 1. Fetch and validate artwork image
	fetcher := s.artworkFetcher
	if fetcher == nil {
		fetcher = metadata.NewArtworkFetcher(nil)
	}
	artwork, err := fetcher.Fetch(ctx, preview.NewCoverURL)
	if err != nil || artwork == nil {
		res.Status = RepairStatusImageInvalid
		res.Message = fmt.Sprintf("Failed to fetch valid cover image from %s: %v", preview.NewCoverURL, err)
		return res, nil
	}

	// 2. Write cover.jpg sidecar
	release, err := s.catalog.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	releaseDir, err := s.library.EnsureReleaseDir(*release)
	if err != nil {
		return nil, err
	}

	// Guard against blind overwrite of existing foreign or custom cover.jpg
	if preview.CoverPath != "" {
		coverFullPath := filepath.Join(s.library.Root(), preview.CoverPath)
		if existingData, readErr := os.ReadFile(coverFullPath); readErr == nil {
			if !bytes.Equal(existingData, artwork.Data) {
				res.Status = RepairStatusFailed
				res.Message = fmt.Sprintf("Existing sidecar %s has different content; refusing to overwrite to preserve existing artwork", preview.CoverPath)
				return res, nil
			}
		}
	}

	writtenPath, err := s.library.WriteCover(releaseDir, artwork.Extension(), artwork.Data)
	if err != nil {
		res.Status = RepairStatusFailed
		res.Message = fmt.Sprintf("Failed to write cover sidecar: %v", err)
		return res, nil
	}
	res.CoverWritten = true
	res.CoverPath = s.library.RelPath(writtenPath)

	// 3. Losslessly update embedded cover in every track file without audio re-encoding
	tracks, err := s.catalog.ListTracks(ctx, releaseID, 500, 0)
	if err != nil {
		return nil, err
	}

	var trackErrors []string
	repairedCount := 0

	for _, track := range tracks {
		files, err := s.files.ListByTrack(ctx, track.ID)
		if err != nil {
			trackErrors = append(trackErrors, fmt.Sprintf("track %s: %v", track.ID, err))
			continue
		}

		trackSuccess := true
		for _, f := range files {
			absPath := filepath.Join(s.library.Root(), f.Path)
			if !s.library.Exists(absPath) {
				continue
			}

			// In-place Vorbis comment update preserves audio bitstream
			if err := s.tagger.UpdateArtwork(ctx, absPath, artwork); err != nil {
				trackSuccess = false
				trackErrors = append(trackErrors, fmt.Sprintf("file %s: %v", f.Path, err))
				break
			}

			// Update file size in repository if changed
			if fi, err := os.Stat(absPath); err == nil {
				f.SizeBytes = fi.Size()
				f.UpdatedAt = time.Now().UTC()
				_, _ = s.files.Upsert(ctx, f)
			}
		}

		if trackSuccess {
			repairedCount++
		}
	}

	res.TracksRepaired = repairedCount
	res.TracksFailed = len(tracks) - repairedCount

	// 4. Update Database only after filesystem writes succeed
	if res.TracksFailed == 0 {
		if err := s.catalog.UpdateReleaseCover(ctx, releaseID, preview.NewCoverURL); err != nil {
			res.Status = RepairStatusFailed
			res.Message = fmt.Sprintf("Files written but database update failed: %v", err)
			return res, nil
		}
		res.Status = RepairStatusApplied
		res.Message = fmt.Sprintf("Successfully refreshed artwork: cover.jpg written and %d tracks updated losslessly.", repairedCount)
	} else if repairedCount > 0 || res.CoverWritten {
		res.Status = RepairStatusPartialFailed
		res.Message = fmt.Sprintf("Partial repair (%d/%d tracks updated): %s", repairedCount, len(tracks), strings.Join(trackErrors, "; "))
	} else {
		res.Status = RepairStatusFailed
		res.Message = strings.Join(trackErrors, "; ")
	}

	return res, nil
}

func (s *Service) resolveReleases(ctx context.Context, artistID string, releaseIDs []string) ([]music.Release, error) {
	if len(releaseIDs) > 0 {
		var releases []music.Release
		for _, id := range releaseIDs {
			rel, err := s.catalog.GetRelease(ctx, id)
			if err != nil {
				return nil, err
			}
			if rel != nil {
				releases = append(releases, *rel)
			}
		}
		return releases, nil
	}
	if strings.TrimSpace(artistID) != "" {
		return s.catalog.ListReleases(ctx, artistID, 500, 0)
	}
	return nil, apperr.New(apperr.CodeInvalidRequest, "artist_id or release_ids must be provided.")
}

// PreviewBulkArtwork evaluates releases for an artist or given release IDs with rate limiting between provider calls.
func (s *Service) PreviewBulkArtwork(ctx context.Context, artistID string, releaseIDs []string) ([]*ArtworkRepairPreview, error) {
	releases, err := s.resolveReleases(ctx, artistID, releaseIDs)
	if err != nil {
		return nil, err
	}

	previews := make([]*ArtworkRepairPreview, 0, len(releases))

	for i, rel := range releases {
		select {
		case <-ctx.Done():
			return previews, ctx.Err()
		default:
		}

		// Rate limit between external metadata provider calls
		if i > 0 {
			time.Sleep(150 * time.Millisecond)
		}

		p, err := s.PreviewReleaseArtwork(ctx, rel.ID)
		if err != nil {
			previews = append(previews, &ArtworkRepairPreview{
				ReleaseID:    rel.ID,
				ReleaseTitle: rel.DisplayTitle(),
				ArtistName:   rel.DisplayAlbumArtist(),
				Status:       RepairStatusFailed,
				Message:      err.Error(),
			})
			continue
		}
		previews = append(previews, p)
	}

	return previews, nil
}

// ApplyBulkArtwork executes artwork repair across multiple releases for an artist or given release IDs with rate limiting.
func (s *Service) ApplyBulkArtwork(ctx context.Context, artistID string, releaseIDs []string) ([]*ArtworkRepairResult, error) {
	previews, err := s.PreviewBulkArtwork(ctx, artistID, releaseIDs)
	if err != nil {
		return nil, err
	}

	results := make([]*ArtworkRepairResult, 0, len(previews))

	for i, prev := range previews {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Skip already correct releases
		if prev.Status == RepairStatusAlreadyCorrect {
			results = append(results, &ArtworkRepairResult{
				ReleaseID:    prev.ReleaseID,
				ReleaseTitle: prev.ReleaseTitle,
				Status:       RepairStatusAlreadyCorrect,
				Message:      "Already correct; no action required.",
			})
			continue
		}

		// Rate limit
		if i > 0 {
			time.Sleep(200 * time.Millisecond)
		}

		res, err := s.ApplyReleaseArtwork(ctx, prev.ReleaseID, prev)
		if err != nil {
			results = append(results, &ArtworkRepairResult{
				ReleaseID:    prev.ReleaseID,
				ReleaseTitle: prev.ReleaseTitle,
				Status:       RepairStatusFailed,
				Message:      err.Error(),
			})
			continue
		}
		results = append(results, res)
	}

	return results, nil
}
