package downloader

import (
	"os"
	"path/filepath"
	"strings"

	"ytdm/backend/internal/storage"
)

// attemptDirPrefix names the private directory of one download attempt; the
// staging manager counts the partials inside such directories.
const attemptDirPrefix = storage.DownloadAttemptPrefix

// stagedAudioName is the base name of the verified audio inside an attempt
// directory. It can never collide with yt-dlp's "source.<ext>" output.
const stagedAudioName = "audio"

// removeStaleAttempts deletes attempt directories a previous, interrupted
// process left behind. A work directory belongs to one item, and an item is
// never downloaded twice at the same time, so no running attempt is touched.
func removeStaleAttempts(workDir string) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), attemptDirPrefix) {
			_ = os.RemoveAll(filepath.Join(workDir, entry.Name()))
		}
	}
}
