package youtube

import (
	"sort"
	"strings"

	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// minCombinedAudioKbps is the lowest audio bitrate a combined stream may
// report and still be worth transferring its video for. It only rejects a
// format whose audio bitrate yt-dlp actually named: an unnamed bitrate is
// unknown, not low, and is judged by the transfer cost instead.
const minCombinedAudioKbps = 48

// drmProtocols name transport protocols that carry an access protection this
// backend neither can nor may work around.
var drmProtocols = []string{"mss", "ism", "rtmpe", "f4f", "f4m"}

// combinedEligible reports whether the audio of a combined stream may be
// transferred and extracted. Every condition is a property the format record
// states; nothing is inferred from an absent field.
//
// It is only ever consulted when the item offers no audio only stream at all.
func combinedEligible(f ytdlp.Format) bool {
	// The stream must be known to carry both parts. An unknown audio codec
	// would mean paying for a video without knowing whether audio follows.
	if !f.IsCombined() {
		return false
	}
	// A shortened sample is real audio but not the wanted track.
	if f.IsPreview() {
		return false
	}
	// Protected streams are rejected; nothing here circumvents access control.
	if f.HasDRM {
		return false
	}
	protocol := strings.ToLower(strings.TrimSpace(f.Protocol))
	for _, blocked := range drmProtocols {
		if strings.Contains(protocol, blocked) {
			return false
		}
	}
	// The audio must be copyable into a container this backend supports,
	// without a second lossy encode.
	if _, ok := provider.ExtractableAudioCodec(f.ACodec); !ok {
		return false
	}
	// A named audio bitrate below the floor is proven poor quality. An unnamed
	// one stays eligible: unknown is not the same as low.
	if abr := f.AudioBitrate(); abr > 0 && abr < minCombinedAudioKbps {
		return false
	}
	return true
}

// rankCombined orders eligible combined formats by what they cost to obtain,
// not by picture quality. The cheapest transfer that still carries acceptable
// audio wins, which is the smallest resolution rather than the largest.
//
// The order is total and compares like with like: a reported byte size is
// weighed against other byte sizes, a bitrate against other bitrates, and a
// format that reports neither sorts last rather than counting as free. Formats
// that tie on every measurable property are ordered by their format id, so the
// same answer always selects the same stream and the download can request it
// by id.
func rankCombined(formats []ytdlp.Format) []ytdlp.Format {
	out := make([]ytdlp.Format, len(formats))
	copy(out, formats)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]

		// A reported size is the most direct statement of the transfer cost.
		sizeA, sizeB := a.Size(), b.Size()
		if (sizeA > 0) != (sizeB > 0) {
			return sizeA > 0
		}
		if sizeA > 0 && sizeB > 0 && sizeA != sizeB {
			return sizeA < sizeB
		}

		// Without a size, the total bitrate says what the stream costs per
		// second of runtime.
		tbrA, tbrB := a.TransferBitrate(), b.TransferBitrate()
		if (tbrA > 0) != (tbrB > 0) {
			return tbrA > 0
		}
		if tbrA > 0 && tbrB > 0 && tbrA != tbrB {
			return tbrA < tbrB
		}

		// Equal cost: the better audio wins.
		if a.AudioBitrate() != b.AudioBitrate() {
			return a.AudioBitrate() > b.AudioBitrate()
		}
		return a.FormatID < b.FormatID
	})
	return out
}

// combinedAudioFormats returns the combined streams whose audio may be
// extracted, best first. The result is empty when none qualifies.
func combinedAudioFormats(info ytdlp.Info) []ytdlp.Format {
	eligible := make([]ytdlp.Format, 0, len(info.Formats))
	for _, f := range info.CombinedFormats() {
		if combinedEligible(f) {
			eligible = append(eligible, f)
		}
	}
	return rankCombined(eligible)
}
