package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// stagedResult is everything the move of one finished download into the
// library needs: the verified, tagged audio in staging and what was learned
// while producing it.
type stagedResult struct {
	track    music.Track
	source   provider.MediaSource
	download downloader.Result
	artwork  *metadata.Artwork
	sha256   string
	size     int64
	score    float64
	// lyrics is the lookup made when the result was staged. Nil means the
	// lookup has not happened yet and is made after the move, as it always was.
	lyrics *lyricsOutcome
}

const (
	// finaliseRecordName is the file in an item's staging directory that says
	// "this audio is complete and only waits for the library". Its presence,
	// together with the item's stored staged checksum, is what lets a later
	// attempt - also one after a restart - skip the download.
	finaliseRecordName = "finalise.json"
	// finaliseCoverPrefix names the staged copy of the release cover. The
	// cover is fetched over the network, so it is kept beside the audio to
	// make the move itself independent of any provider.
	finaliseCoverPrefix   = "finalise-cover"
	finaliseRecordVersion = 1
)

// finaliseRecord is the durable form of a stagedResult. The track is not part
// of it: the item row is the authority for the track and is read again
// after a restart anyway.
type finaliseRecord struct {
	Version  int                  `json:"version"`
	ItemID   string               `json:"item_id"`
	Audio    string               `json:"audio"`
	SHA256   string               `json:"sha256"`
	Size     int64                `json:"size"`
	Source   provider.MediaSource `json:"source"`
	Download downloader.Result    `json:"download"`
	Score    float64              `json:"score"`

	Cover       string `json:"cover,omitempty"`
	CoverMIME   string `json:"cover_mime,omitempty"`
	CoverWidth  int    `json:"cover_width,omitempty"`
	CoverHeight int    `json:"cover_height,omitempty"`
	CoverSHA256 string `json:"cover_sha256,omitempty"`

	Lyrics   lyricsOutcome `json:"lyrics"`
	StagedAt time.Time     `json:"staged_at"`
}

// keepStaged records that a complete staging result only waits for the
// library. It runs when the library turned out to be unreachable at the moment
// of the move. Everything that would otherwise need the network after the move
// - the cover and the lyrics lookup - is settled now. A record that cannot be
// written only costs a second download later, never the item.
func (m *Manager) keepStaged(ctx context.Context, item Item, staged stagedResult, logger *slog.Logger) {
	if m.staging == nil {
		// Without persistent staging the audio lives in a temporary directory
		// that is removed when the attempt returns.
		return
	}
	dir, err := m.staging.ItemDir(item.ID)
	if err != nil {
		logger.Warn("staged download not kept for finalisation", logging.KeyError, err.Error())
		return
	}
	audio := filepath.Base(staged.download.Path)
	if !sameDir(filepath.Dir(staged.download.Path), dir) || !validStagedName(audio) {
		logger.Warn("staged download not kept for finalisation: the audio is not in the item's staging",
			"path", staged.download.Path)
		return
	}

	download := staged.download
	download.Path = ""
	source := staged.source
	// A session and its format list are only meaningful for a download. After
	// a restart the session may not exist any more.
	source.SessionID = ""
	source.Formats = nil

	record := finaliseRecord{
		Version:  finaliseRecordVersion,
		ItemID:   item.ID,
		Audio:    audio,
		SHA256:   staged.sha256,
		Size:     staged.size,
		Source:   source,
		Download: download,
		Score:    staged.score,
		StagedAt: time.Now().UTC(),
	}
	if staged.lyrics != nil {
		record.Lyrics = *staged.lyrics
	} else {
		record.Lyrics = m.lookupLyrics(ctx, staged.track, staged.source.ID, logger)
	}

	if staged.artwork != nil && len(staged.artwork.Data) > 0 {
		name := finaliseCoverPrefix + staged.artwork.Extension()
		if err := writeStagingFile(filepath.Join(dir, name), staged.artwork.Data); err != nil {
			// The audio carries the embedded cover anyway; only the cover file
			// next to the release would be missing.
			logger.Warn("staged cover could not be kept", logging.KeyError, err.Error())
		} else {
			record.Cover = name
			record.CoverMIME = staged.artwork.MIME
			record.CoverWidth = staged.artwork.Width
			record.CoverHeight = staged.artwork.Height
			record.CoverSHA256 = checksumOf(staged.artwork.Data)
		}
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		err = writeStagingFile(filepath.Join(dir, finaliseRecordName), data)
	}
	if err != nil {
		logger.Warn("staged download not kept for finalisation; it will be downloaded again",
			logging.KeyError, err.Error())
		return
	}
	logger.Info("download staged; it is moved into the library once the library is reachable",
		"staged", m.staging.RelPath(staged.download.Path), "sha256", staged.sha256)
}

// hasFinaliseRecord reports whether the item's staging holds a finalisation
// record, without reading or verifying it.
func (m *Manager) hasFinaliseRecord(itemID string) bool {
	if m.staging == nil {
		return false
	}
	dir, err := m.staging.ItemDir(itemID)
	if err != nil {
		return false
	}
	_, err = os.Lstat(filepath.Join(dir, finaliseRecordName))
	return err == nil
}

// loadStaged returns the staged result of an item that only waits for the
// library. A record whose audio is gone or does not match is discarded
// together with what it describes, and the item is downloaded again.
func (m *Manager) loadStaged(item Item, logger *slog.Logger) (stagedResult, bool) {
	if m.staging == nil {
		return stagedResult{}, false
	}
	dir, err := m.staging.ItemDir(item.ID)
	if err != nil {
		return stagedResult{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, finaliseRecordName))
	if errors.Is(err, os.ErrNotExist) {
		return stagedResult{}, false
	}
	if err == nil {
		var staged stagedResult
		if staged, err = m.verifyStaged(dir, item, data, logger); err == nil {
			return staged, true
		}
	}
	logger.Warn("staged download cannot be finalised; downloading it again",
		logging.KeyOperation, "finalise_staged", logging.KeyError, err.Error())
	m.discardStaged(item.ID, dir, logger)
	return stagedResult{}, false
}

// verifyStaged rebuilds a staged result from its record and proves that the
// audio in staging is still byte for byte the one that was tagged and staged.
func (m *Manager) verifyStaged(dir string, item Item, data []byte, logger *slog.Logger) (stagedResult, error) {
	var record finaliseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return stagedResult{}, fmt.Errorf("the finalisation record is unreadable: %w", err)
	}
	switch {
	case record.Version != finaliseRecordVersion:
		return stagedResult{}, fmt.Errorf("the finalisation record has version %d", record.Version)
	case record.ItemID != item.ID:
		return stagedResult{}, errors.New("the finalisation record belongs to another item")
	case !validStagedName(record.Audio):
		return stagedResult{}, errors.New("the finalisation record names no audio file in the item's staging")
	case record.SHA256 == "" || record.Size <= 0:
		return stagedResult{}, errors.New("the finalisation record carries no checksum")
	case item.StagedSHA256 != "" && item.StagedSHA256 != record.SHA256:
		// The row is written before the record, so a differing row means the
		// record is left over from an earlier staging of the item.
		return stagedResult{}, errors.New("the finalisation record does not match the item's staged checksum")
	}

	audioPath := filepath.Join(dir, record.Audio)
	hash, size, err := storage.ComputeChecksum(audioPath)
	if err != nil {
		return stagedResult{}, fmt.Errorf("the staged audio is missing: %w", err)
	}
	if hash != record.SHA256 || size != record.Size {
		return stagedResult{}, errors.New("the staged audio failed checksum verification")
	}

	download := record.Download
	download.Path = audioPath
	lyrics := record.Lyrics
	staged := stagedResult{
		track:    item.Track,
		source:   record.Source,
		download: download,
		sha256:   record.SHA256,
		size:     record.Size,
		score:    record.Score,
		lyrics:   &lyrics,
	}
	if staged.score == 0 {
		staged.score = item.MatchScore
	}

	if record.Cover != "" {
		cover, err := m.stagedCover(dir, record)
		if err != nil {
			logger.Warn("staged cover is not usable; the track is finalised without a cover file",
				logging.KeyError, err.Error())
		} else {
			staged.artwork = cover
		}
	}
	return staged, nil
}

func (m *Manager) stagedCover(dir string, record finaliseRecord) (*metadata.Artwork, error) {
	if !validStagedName(record.Cover) || !strings.HasPrefix(record.Cover, finaliseCoverPrefix) {
		return nil, errors.New("the record names an unexpected cover file")
	}
	data, err := os.ReadFile(filepath.Join(dir, record.Cover))
	if err != nil {
		return nil, err
	}
	if record.CoverSHA256 != "" && checksumOf(data) != record.CoverSHA256 {
		return nil, errors.New("the staged cover failed checksum verification")
	}
	return &metadata.Artwork{
		Data:   data,
		MIME:   record.CoverMIME,
		Width:  record.CoverWidth,
		Height: record.CoverHeight,
	}, nil
}

// discardStaged removes a finalisation record and the files it described, so
// that the download that follows starts from a clean item directory. Partial
// downloads are left alone; they may still be continued.
func (m *Manager) discardStaged(itemID, dir string, logger *slog.Logger) {
	for _, name := range []string{finaliseRecordName, finaliseCoverPrefix + ".jpg", finaliseCoverPrefix + ".png"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("staged file could not be removed", "file", name, logging.KeyError, err.Error())
		}
	}
	if err := m.staging.ResetCorruptedAudio(itemID); err != nil {
		logger.Warn("staged audio could not be removed", logging.KeyError, err.Error())
	}
}

// finaliseStaged moves a staged result into the library without any provider
// request, yt-dlp run or transcode. handled is false when the item has no
// usable staged result and has to be downloaded the normal way.
//
// The attempt counter is left alone: the download it was spent on already
// succeeded, and waiting for the library is not a failure of the item.
func (w *worker) finaliseStaged(ctx context.Context, job Job, item Item, logger *slog.Logger) (status ItemStatus, handled bool, err error) {
	m := w.manager
	if !m.hasFinaliseRecord(item.ID) {
		return "", false, nil
	}
	// The library is checked before the staged audio is read: while it is
	// away, every dispatch would otherwise hash the whole file for nothing.
	// A record that turns out to be unusable is only discovered - and the
	// download repeated - once the library is back.
	if guard := m.library.Guard(); guard != nil {
		if err := guard.RequireWritable(); err != nil {
			return ItemWaitingStorage, true, err
		}
	}
	staged, ok := m.loadStaged(item, logger)
	if !ok {
		return "", false, nil
	}
	logger.Info("moving the staged download into the library without downloading it again",
		logging.KeyOperation, "finalise_staged", "sha256", staged.sha256)
	status, err = w.commitStaged(ctx, job, item, staged, logger)
	return status, true, err
}

// dropFinishedStaging removes the staging of items that a job cancellation or
// failure ended without their worker. A worker removes its item's staging
// itself when the item ends; items ended in bulk would otherwise keep staged
// audio until the next start of the service.
func (m *Manager) dropFinishedStaging(ctx context.Context, jobID string) {
	if m.staging == nil {
		return
	}
	items, err := m.store.ListItems(ctx, jobID)
	if err != nil {
		m.logger.Warn("staging of ended items not removed",
			logging.KeyJobID, jobID, logging.KeyError, err.Error())
		return
	}
	for _, it := range items {
		if !it.Status.Terminal() {
			continue
		}
		if _, running := m.inFlight.Load(it.ID); running {
			// Its worker is still unwinding and removes the staging itself
			// once the final state is stored.
			continue
		}
		if err := m.staging.RemoveItemDir(it.ID); err != nil {
			m.logger.Warn("item staging could not be removed",
				logging.KeyJobItemID, it.ID, logging.KeyError, err.Error())
		}
	}
}

// validStagedName accepts a plain file name, never a path.
func validStagedName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.Base(name) == name && !strings.ContainsAny(name, `/\`)
}

func checksumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// writeStagingFile replaces a file atomically, so that a crash never leaves a
// half written record that a later attempt would have to make sense of.
func writeStagingFile(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".ytdm-finalise-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
