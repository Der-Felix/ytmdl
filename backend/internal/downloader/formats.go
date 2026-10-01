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
	// PlanExtractAudio copies the audio packets out of a combined audio/video
	// stream. The samples are not touched, the video never reaches the
	// library, and no second lossy encode happens on this path whatever the
	// transcode setting says.
	PlanExtractAudio Plan = "extract_audio"
)

// SelectFormat picks the audio format to download. An audio only format always
// wins over a combined one: a combined stream is offered only when the item has
// no audio only stream at all, and paying for its video is the last resort.
// Among audio only formats a native Opus stream wins; a format with a named
// codec wins over one whose codec the provider did not name; among equals the
// higher bitrate wins. Among combined formats the resolver already ordered the
// candidates by transfer cost, so the first one offered stays first. The second
// result reports whether any usable format was found.
//
// A format without a codec name is an audio only stream the resolver accepted
// - for example an HLS audio rendition, which yt-dlp lists without acodec. It
// stays eligible: the downloaded file is inspected before it is kept, and that
// inspection, not a guess, decides its codec.
func SelectFormat(formats []provider.AudioFormat) (provider.AudioFormat, bool) {
	usable := make([]provider.AudioFormat, 0, len(formats))
	for _, f := range formats {
		if strings.EqualFold(strings.TrimSpace(f.Codec), "none") {
			continue
		}
		// A combined format without a named audio codec proves nothing about
		// carrying audio, and its video would be transferred for nothing.
		if f.Combined && strings.TrimSpace(f.Codec) == "" {
			continue
		}
		usable = append(usable, f)
	}
	if len(usable) == 0 {
		return provider.AudioFormat{}, false
	}

	named := func(f provider.AudioFormat) bool { return strings.TrimSpace(f.Codec) != "" }
	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].Combined != usable[j].Combined {
			return !usable[i].Combined
		}
		if usable[i].Combined && usable[j].Combined {
			// The resolver ranked combined formats by transfer cost already;
			// keep that order instead of re-ranking on bitrate here.
			return false
		}
		if usable[i].IsOpus() != usable[j].IsOpus() {
			return usable[i].IsOpus()
		}
		if named(usable[i]) != named(usable[j]) {
			return named(usable[i])
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

// defaultSelector prefers a native Opus stream, then any audio only stream.
// It deliberately has no "best" fallback: that answers with whatever combined
// stream the platform happens to offer, chosen by picture quality and never
// judged. A combined stream is only ever fetched when the resolver examined it
// and put its id in front of this chain (see
// docs/diagnostics/audio-format-classification.md); without such an id and
// without an audio only stream the download fails like the resolution would
// have.
const defaultSelector = "bestaudio[acodec^=opus]/bestaudio"

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
