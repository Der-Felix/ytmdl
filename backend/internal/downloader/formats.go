package downloader

import (
	"fmt"
	"sort"
	"strings"

	"ytdm/backend/internal/provider"
)

// Plan says what has to happen to the freshly downloaded stream before it can
// be filed away.
type Plan string

const (
	// PlanKeep leaves the file untouched: it already is in a usable container.
	PlanKeep Plan = "keep"
	// PlanRemux copies the audio stream into an Ogg container. The samples are
	// not touched, so no quality is lost.
	PlanRemux Plan = "remux"
	// PlanTranscode re-encodes to Opus. It is only ever chosen when the
	// operator explicitly allowed it.
	PlanTranscode Plan = "transcode"
)

// SelectFormat picks the audio format to download. A native Opus stream always
// wins; among equal codecs the higher bitrate wins. The second result reports
// whether any usable format was found.
func SelectFormat(formats []provider.AudioFormat) (provider.AudioFormat, bool) {
	usable := make([]provider.AudioFormat, 0, len(formats))
	for _, f := range formats {
		if strings.TrimSpace(f.Codec) != "" && !strings.EqualFold(f.Codec, "none") {
			usable = append(usable, f)
		}
	}
	if len(usable) == 0 {
		return provider.AudioFormat{}, false
	}

	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].IsOpus() != usable[j].IsOpus() {
			return usable[i].IsOpus()
		}
		if usable[i].BitrateKbps != usable[j].BitrateKbps {
			return usable[i].BitrateKbps > usable[j].BitrateKbps
		}
		return usable[i].Filesize > usable[j].Filesize
	})
	return usable[0], true
}

// FormatSelector builds the yt-dlp -f expression for a source. When the source
// enumerated its formats, the chosen format id is requested directly; the
// generic preference chain is the fallback.
//
// The selector never asks yt-dlp to convert anything — it only decides which
// stream is fetched.
func FormatSelector(formats []provider.AudioFormat) string {
	if best, ok := SelectFormat(formats); ok && best.ID != "" {
		return fmt.Sprintf("%s/%s", best.ID, defaultSelector)
	}
	return defaultSelector
}

// defaultSelector prefers a native Opus stream, then any audio only stream,
// then whatever the platform offers.
const defaultSelector = "bestaudio[acodec^=opus]/bestaudio/best"

// PlanFor decides how a downloaded file has to be treated.
//
// The rules implement the audio policy of the backend:
//   - native Opus is kept as Opus and never re-encoded, only remuxed into Ogg
//     when it arrived inside a WebM or Matroska container,
//   - any other native codec is kept as it is,
//   - a lossy to lossy conversion only happens when it was configured
//     explicitly.
func PlanFor(info AudioInfo, allowTranscode bool) (Plan, string) {
	codec := strings.ToLower(strings.TrimSpace(info.Codec))
	container := strings.ToLower(strings.TrimSpace(info.Container))

	if codec == "opus" {
		if isOggContainer(container) {
			return PlanKeep, ".opus"
		}
		return PlanRemux, ".opus"
	}

	if codec == "vorbis" {
		if isOggContainer(container) {
			return PlanKeep, ".ogg"
		}
		return PlanRemux, ".ogg"
	}

	if allowTranscode {
		return PlanTranscode, ".opus"
	}
	return PlanKeep, NativeExtension(codec, container)
}

func isOggContainer(container string) bool {
	return container == "ogg" || container == "oga" || container == "opus"
}

// NativeExtension maps a codec and container onto the file extension the audio
// should keep when it is stored unchanged.
func NativeExtension(codec, container string) string {
	switch strings.ToLower(codec) {
	case "aac", "alac":
		return ".m4a"
	case "mp3":
		return ".mp3"
	case "flac":
		return ".flac"
	case "vorbis":
		return ".ogg"
	case "opus":
		return ".opus"
	}
	switch strings.ToLower(container) {
	case "mov", "mp4", "m4a", "3gp":
		return ".m4a"
	case "webm", "matroska", "matroska,webm":
		return ".webm"
	case "mp3":
		return ".mp3"
	case "flac":
		return ".flac"
	case "ogg":
		return ".ogg"
	case "wav":
		return ".wav"
	}
	return ".audio"
}

// TranscodeBitrate returns the target bitrate for an explicitly configured
// re-encode. It never exceeds the source bitrate, because raising it would
// only inflate the file without adding information.
func TranscodeBitrate(sourceKbps float64) int {
	const maxKbps = 192
	if sourceKbps <= 0 {
		return 128
	}
	if sourceKbps > maxKbps {
		return maxKbps
	}
	return int(sourceKbps)
}
