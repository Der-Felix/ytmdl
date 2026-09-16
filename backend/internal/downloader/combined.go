package downloader

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// DefaultCombinedMaxBytes bounds what one combined stream may transfer. A
// combined stream carries a video that is discarded, so the budget is about
// bandwidth and session time, not about disk. 128 MiB covers a long track even
// at the higher combined bitrates measured in
// docs/diagnostics/audio-format-classification.md.
const DefaultCombinedMaxBytes int64 = 128 << 20

// DefaultCombinedTimeout bounds how long one combined transfer may run. It is
// the second half of the budget: a stream that trickles in below the byte cap
// still occupies the only media session for as long as it runs.
const DefaultCombinedTimeout = 10 * time.Minute

// count reports one acquisition event under the provider family that served
// it. Counter names are fixed identifiers; no value taken from a provider
// answer ever becomes part of one.
func (d *YTDLPDownloader) count(family provider.Family, kind, what string, n uint64) {
	if d.recorder == nil || family == "" {
		return
	}
	d.recorder.Add("download."+string(family)+"."+kind+"."+what, n)
}

// budgetKind records which limit, if any, this backend used to stop a
// transfer. It is kept apart from a cancelled job so the error the item ends
// with says which of the two happened.
type budgetKind int32

const (
	budgetNone budgetKind = iota
	budgetBytes
)

// transferBudget stops a combined download that outgrows its byte limit while
// it runs. The limit is enforced twice: here, from the progress the running
// transfer reports, and afterwards on the file that actually arrived. The
// second check is the binding one - a segmented stream reports its progress
// per fragment and may never announce a total size at all, so a metadata
// estimate can never be the hard bound.
//
// The time limit is not watched here. It is handed to the yt-dlp client as a
// process timeout, which starts only once the session slot has been granted,
// so waiting for a busy session never consumes it.
type transferBudget struct {
	maxBytes int64
	observed atomic.Int64
	kind     atomic.Int32
	cancel   context.CancelFunc
}

func newTransferBudget(maxBytes int64, cancel context.CancelFunc) *transferBudget {
	return &transferBudget{maxBytes: maxBytes, cancel: cancel}
}

// observe records the progress of a running transfer and stops it as soon as
// it passes the byte limit, so the remaining bytes are never fetched.
func (b *transferBudget) observe(downloaded int64) {
	if b == nil || downloaded <= 0 {
		return
	}
	for {
		current := b.observed.Load()
		if downloaded <= current {
			break
		}
		if b.observed.CompareAndSwap(current, downloaded) {
			break
		}
	}
	if b.maxBytes > 0 && downloaded > b.maxBytes && b.kind.CompareAndSwap(int32(budgetNone), int32(budgetBytes)) {
		if b.cancel != nil {
			b.cancel()
		}
	}
}

// transferred reports the largest progress figure the transfer announced.
func (b *transferBudget) transferred() int64 {
	if b == nil {
		return 0
	}
	return b.observed.Load()
}

// classify turns the error of a stopped transfer into the reason this backend
// stopped it. A stop caused by a local budget is TRANSFER_BUDGET_EXCEEDED:
// candidate scoped, so it neither pauses the provider family nor records a
// platform failure on the session pool. A job the caller cancelled, or whose
// own deadline passed, keeps its own error.
func (b *transferBudget) classify(parent context.Context, err error) error {
	if b == nil || err == nil {
		return err
	}
	if parent.Err() != nil {
		return err
	}
	if budgetKind(b.kind.Load()) == budgetBytes {
		// The transfer ended through the budget's own cancellation. That
		// cancellation is deliberately not carried along: the worker treats a
		// context.Canceled cause as a cancelled job.
		return apperr.Newf(apperr.CodeTransferBudgetExceeded,
			"The combined stream exceeded the transfer budget of %d bytes and was stopped.", b.maxBytes)
	}
	switch {
	case errors.Is(err, ytdlp.ErrProcessTimeout):
		return apperr.Wrap(apperr.CodeTransferBudgetExceeded,
			"The combined stream did not finish within the transfer time budget and was stopped.", err)
	case errors.Is(err, ytdlp.ErrMaxFilesize):
		return apperr.Wrapf(apperr.CodeTransferBudgetExceeded, err,
			"The combined stream is larger than the transfer budget of %d bytes and was not downloaded.", b.maxBytes)
	}
	return err
}

// extractAudio copies the audio packets of a combined stream into their own
// container. Nothing is re-encoded: -c:a copy moves the same packets, so the
// stored audio is bit for bit what the platform delivered. -vn drops the video
// and any attached picture; the cover the library wants is embedded later by
// the ordinary tagging step.
func (d *YTDLPDownloader) extractAudio(ctx context.Context, source, target string) error {
	return d.ffmpeg.Run(ctx, apperr.CodeDownloadFailed,
		"-i", source,
		"-vn",
		"-map", "0:a:0",
		"-c:a", "copy",
		"-map_metadata", "-1",
		target,
	)
}

// combinedExtension decides the container the extracted audio is written to.
// The codec is the one ffprobe measured on the arrived file, never the one the
// platform announced, and the container comes from the shared allow list - a
// codec that has no container here is rejected instead of being renamed into
// one that cannot hold it.
func combinedExtension(info AudioInfo) (string, error) {
	container, ok := provider.ExtractableAudioCodec(info.Codec)
	if !ok {
		return "", apperr.Newf(apperr.CodeUnsupportedMediaFormat,
			"The combined stream carries audio in a codec this backend cannot store without re-encoding (%s).",
			diagnosticToken(info.Codec))
	}
	return "." + container, nil
}

// combinedFormatFrom reports the combined format a source was resolved to,
// if any. A source never mixes the two kinds: the resolver offers a combined
// stream only when the item has no audio only stream at all.
func combinedFormatFrom(source provider.MediaSource) (provider.AudioFormat, bool) {
	chosen, ok := SelectFormat(source.Formats)
	if !ok || !chosen.Combined {
		return provider.AudioFormat{}, false
	}
	return chosen, true
}

// combinedRequest builds the yt-dlp request for a combined transfer. The
// format is addressed by the id the resolver picked, so the stream that was
// judged is the stream that is fetched. --max-filesize lets yt-dlp refuse an
// oversized stream before the first byte whenever it knows the size; it is an
// early exit, not the bound, because the size is often unknown. The time
// budget runs as a process timeout from the moment the session slot is held.
func combinedRequest(base ytdlp.DownloadRequest, maxBytes int64, timeout time.Duration) ytdlp.DownloadRequest {
	if timeout <= 0 {
		timeout = DefaultCombinedTimeout
	}
	base.MaxFilesizeBytes = maxBytes
	base.ProcessTimeout = timeout
	return base
}
