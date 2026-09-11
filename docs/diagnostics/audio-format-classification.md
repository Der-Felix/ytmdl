# Audio format classification

YouTube resolutions failed thousands of times with "offers no audio only
stream". This note records how yt-dlp describes format streams, where YTMDL
read those fields differently, which misclassifications are proven, what was
corrected, and what an audio extraction from combined audio/video streams
would cost. It changes no matching, preview rejection, verification tolerance
or provider protection.

Evidence classes:

- **yt-dlp code and output** — the yt-dlp release shipped in the backend image
  (2026.08.19), read locally and run offline in a throw-away container without
  network on synthetic manifests (`backend/internal/provider/youtube/testdata`).
- **Existing production data** — aggregate, read-only: v0.27.1 logs and the
  file catalogue. No provider request was made.
- **Synthetic media** — short files rendered with ffmpeg.
- **Unknown** — stays unknown until the extended diagnostic runs in production.

## How yt-dlp describes a stream

Every format has an `acodec` and a `vcodec` with three states: a codec name
(stream present), the literal `none` (stream known to be absent), and no value
(unknown). `--dump-json` omits unknown values; JSON `null` means the same.
yt-dlp's own `bestaudio` selects any format whose `vcodec` is `none` and whose
`acodec` is not `none` — an unknown audio codec qualifies.

Its HLS parser creates separate audio renditions (`EXT-X-MEDIA TYPE=AUDIO`)
with `vcodec: none` and no `acodec`; the video variants that reference them get
`acodec: none` (the source carries a TODO to add the codec later). The YouTube
extractor does not fill it in afterwards. Reproduced offline:

| Manifest | Formats as `--dump-json` prints them | yt-dlp `bestaudio` |
|---|---|---|
| split audio renditions | `hls-233-Default`, `hls-234-Default`: `vcodec none`, no `acodec`; two variants `avc1…`, `acodec none` | `hls-234-Default` |
| combined variants only | two variants `avc1…` + `mp4a…` | none |

## What YTMDL did

| `vcodec` / `acodec` | yt-dlp meaning | before | now |
|---|---|---|---|
| `none` / `opus` | audio only | accepted | accepted |
| `none` / — | audio only, codec unknown | **rejected** | accepted; codec decided by ffprobe on the downloaded file |
| — / `mp4a.40.2` | audio, video unknown | accepted unchecked | accepted; the downloaded file must not contain video |
| `avc1` / `mp4a` | combined | rejected | rejected |
| `avc1` / `none` or — | video (audio absent/unknown) | rejected | rejected |
| `none` / `none` | images (storyboard) | rejected | rejected |
| — / — | nothing known | rejected | rejected (yt-dlp's `bestaudio` does not select it either) |

### Proven and corrected

1. **Audio renditions without codec were rejected.** `Format.HasAudio()`
   treated a missing `acodec` like `none`, so `IsAudioOnly()` rejected exactly
   the format yt-dlp selects as `bestaudio`. The regression test replays the
   yt-dlp output: the old code fails with the production message "offers no
   audio only stream", the corrected one resolves the renditions. The session
   prober used the same check and would have refused replacement cookies whose
   probe video offered only renditions; a probe success still never heals a
   session.
2. **The downloader could not request such a rendition explicitly.**
   `SelectFormat` dropped formats without codec name. They are now eligible,
   ranked below named codecs, so the download asks for the format the resolver
   accepted.
3. **A combined stream could be stored as an audio file.** The generic format
   selector ended in `best`, which yt-dlp answers with a combined stream when
   no audio only stream is left at download time. ffprobe reported the AAC
   stream, the AAC plan kept the file unchanged, and it was filed as `.m4a` with
   its video track — reproduced with synthetic media. It was **not observed**
   in the library: all 418 stored YouTube AAC files contain only AAC plus an
   embedded cover (attached picture), the other 16,351 YouTube files are Opus.
   The selector no longer falls back to `best`, and a downloaded file that
   contains a real video stream (cover pictures excluded) is rejected at the raw
   stage with verification reason `unexpected_video_stream`
   (`INVALID_AUDIO`, candidate scope: no cooldown, no session effect) and
   removed. This is also what settles a missing `vcodec`.

The "no audio only stream" message carries the format shape without
addresses: counts of combined, video-only, video-with-unknown-audio, image and
unknown formats, and for combined formats the highest total and audio bitrate.

## Relevance for production — unknown

The v0.27.1 logs name the rejected media but not their formats. Whether the
6,322 rejections were split renditions (accepted from now on) or combined
streams only (still rejected) cannot be told without provider requests. After
a separately authorized rollout, group the format shapes of the first
rejections: a large "combined" share points to the extraction question below;
remaining "unknown" or "video with unknown audio" shares need their own look.

## Audio from combined streams — feasibility, not introduced

When only combined streams exist, the audio could be copied out without
re-encoding (`ffmpeg -vn -map 0:a:0 -c:a copy`). Synthetic measurement, 200 s:

| File | Size | vs. Opus audio |
|---|---:|---:|
| Opus audio ~130 kbps | 3.9 MB | 1× |
| AAC audio 128 kbps | 3.2 MB | 0.8× |
| combined 360p, 500 kbps video + AAC 96 kbps | 15.1 MB | ~4× |
| combined 720p, 1.5 Mbps video + AAC 128 kbps | 40.9 MB | ~10× |

Copying the audio took 0.06 s, and the audio packets were bit-identical to the
source. Real YouTube bitrates of combined streams are unknown offline; the
extended diagnostic reports them.

Consequences that must be decided before such a path exists:

- **Bandwidth and session time:** every byte of video is transferred and
  discarded, roughly 4–10× the audio. A YouTube download holds the only
  session's execution slot for its whole duration, so each such acquisition
  removes several normal ones from the hourly budget.
- **Requests:** combined HLS variants are fetched segment by segment; whether
  those requests count against the account throttle is unknown.
- **Format and quality:** the result is AAC in an `.m4a` container, typically
  at a lower bitrate than the Opus streams otherwise stored; there is no
  re-encoding, but also no Opus.
- **Implementation:** an explicit, separately switched selector for combined
  formats, a plan that always strips the video, and the same verification.

Recommendation: do not introduce it before the production format shapes show
that combined-only items are a relevant share, and weigh their bitrates against
the session budget.
