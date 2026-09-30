# Audio format classification

YouTube resolutions failed thousands of times with "offers no audio only
stream". This note records how yt-dlp describes format streams, where YTMDL
read those fields differently, which misclassifications are proven, what was
corrected, and what an audio extraction from combined audio/video streams
costs. Nothing here changes matching, preview rejection, verification tolerance
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
| `avc1` / `mp4a` | combined | rejected | rejected, unless the combined-stream fallback is switched on — see below |
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

## Relevance for production — answered

The v0.27.1 logs named the rejected media but not their formats, so whether the
6,322 rejections were split renditions (accepted since) or combined streams
only could not be told without provider requests. The v0.27.2-rc.2 window
carried the format shape in every rejection message and settled it: combined
streams dominate. The figures are in the next section.

## Audio from combined streams — implemented, off by default

The twelve-hour v0.27.2-rc.2 production window answered the open question
above. Of 11,127 candidate rejections, **7,930 (71%) were "no audio only
stream"**, spread over **5,860 distinct video ids**, on every candidate rank,
at a constant rate across all twelve hours, and always with the same shape:
four to five formats, exactly one combined stream (64–182 kbps total) and the
rest storyboard images. The share is therefore relevant, and the shape is a
property of the answer rather than of individual items.

A bounded fallback now exists. It is **off by default** and is switched on with
`YTDM_COMBINED_AUDIO_FALLBACK` (see
[Configuration](/configuration#combined-stream-audio-fallback-v0-27-3)).

### What is selected

An audio-only stream always wins. A combined stream is only ever considered
when the item offers none, and only when its own format record proves every one
of these:

| Condition | Rejected when |
|---|---|
| both streams named | `acodec` or `vcodec` unknown or `none` — an unknown audio codec is never read as a promise |
| not a sample | `format_id` or `format_note` contains preview, snippet, sample, clip, teaser or trailer |
| no access protection | `has_drm` set, or a protected transport (`mss`, `ism`, `rtmpe`, `f4f`, `f4m`) |
| audio can be copied | codec outside the allow list below |
| audio quality plausible | a **named** bitrate below 48 kbps. An unnamed bitrate is unknown, not low, and stays eligible |

Among eligible formats the **cheapest transfer wins, not the best picture**: a
reported byte size first, then the total bitrate, then the higher audio
bitrate, then the format id. Like is compared with like — a format that reports
neither a size nor a total bitrate sorts last, never first. The order is total,
so the same answer always selects the same stream, and the download addresses
that stream by the id that was judged instead of asking for `best`.

### Supported codecs and containers

The audio is copied, never re-encoded, so a codec is only accepted when a
container can hold its packets unchanged:

| Audio codec (yt-dlp / ffprobe spelling) | Stored as |
|---|---|
| `mp4a.40.2`, `aac` | `.m4a` |
| `alac` | `.m4a` |
| `mp3` | `.mp3` |
| `opus` | `.opus` (Ogg) |
| `vorbis` | `.ogg` |
| `flac` | `.flac` |

Anything else (`ac-3`, `ec-3`, …) is rejected. The decision is made twice: on
the codec the platform announced, to decide whether the transfer is worth it,
and again on the codec **ffprobe measured** in the arrived file, which is what
the stored container is chosen from. A file extension is never simply renamed.

### What happens to the file

`ffmpeg -i <stream> -vn -map 0:a:0 -c:a copy -map_metadata -1 <target>`, run in
staging only. The combined file is deleted immediately afterwards and is never
published as a music file. The stored file is probed again and rejected if it
still contains a real video stream; an embedded cover is an attached picture
and does not count, and the ordinary cover and tagging steps run afterwards on
the extracted audio. Duration tolerance and every other existing verification
limit apply unchanged, and a session is still only ever certified by a download
that passed verification.

Every download attempt — audio-only or combined — works in a private
directory of its own inside the item's staging directory
(`.ytdm-attempt-*`). yt-dlp writes only there, conversion and extraction write
only there, and only the verified audio is moved next to the staging
destination at the very end, replacing any older file of the same name. A
successful exit of yt-dlp without new output is a failure; a file an earlier
attempt left in the staging directory is never taken as the result of the
current candidate. The attempt directory is removed when the attempt returns,
whatever the outcome, and a directory an interrupted process left behind is
removed by the next attempt of the same item.

### Limits

| Limit | Default | Enforcement |
|---|---|---|
| transfer size | 128 MiB | an announced size above the limit is refused before the transfer; yt-dlp's `--max-filesize` refusal (it exits successfully and prints "File is larger than max-filesize") is recognised as a budget stop; the running transfer is stopped as soon as its reported progress passes the limit; the arrived file is measured afterwards, which is the binding check |
| transfer time | 10 min | process timeout that **starts once the media session's execution slot is granted**; waiting for a busy session never consumes it |
| staging | existing quota and free-space checks | unchanged |

A metadata estimate is never the hard bound: a segmented stream announces no
total size, and its progress reports are not a guarantee either. Waiting for
the session slot is bounded only by the item's own context: a caller who
cancels, or whose deadline passes, while the download waits gets that
cancellation or deadline back (`JOB_CANCELLED`), never a budget stop, and
yt-dlp is not started. The worker then decides by the cause: a cancelled job
ends the item as `cancelled`, a passed track time limit (`YTDM_TRACK_TIMEOUT`)
lets it wait for a bounded retry as `TRACK_TIMEOUT`, and a service shutdown
leaves it for recovery. The slot is given back exactly once on every outcome.

The two limits are independent. The combined transfer budget counts from the
granted slot and ends only the transfer (`TRANSFER_BUDGET_EXCEEDED`, final);
the track time limit counts from the start of the attempt, includes every wait
for the session, and ends the attempt (`TRACK_TIMEOUT`, retried within the
attempt limit). Whichever passes first decides.

### Error contract

| Situation | Code | Scope | Retried |
|---|---|---|---|
| no source exists, is accessible, or matches | `TRACK_NOT_FOUND` | candidate | no |
| single candidate unusable | its own code, fanout continues | candidate | — |
| fallback **off**: item offers no audio-only stream | `DOWNLOAD_FAILED` for the candidate, unchanged from v0.27.2-rc.2 | candidate | the attempt ends as `TRACK_NOT_FOUND`, as before |
| fallback **on**: not even a combined stream is usable, or its codec cannot be copied | `UNSUPPORTED_MEDIA_FORMAT` (HTTP 422) | candidate | no |
| combined transfer stopped by the local byte or time budget | `TRANSFER_BUDGET_EXCEEDED` (HTTP 422) | candidate | no |
| provider or transport failure | `PROVIDER_UNAVAILABLE` / `PROVIDER_RATE_LIMITED` | provider | yes |
| verification failure | `MEDIA_VERIFY_FAILED` / `INVALID_AUDIO` | candidate | as before |

An exhausted candidate fanout ends permanently, exactly as in v0.27.2-rc.2:
every candidate failure is summarised as `TRACK_NOT_FOUND` with the same
wording, whatever its own code was. The only refinement applies when the
fallback is switched on and at least one candidate failed with
`UNSUPPORTED_MEDIA_FORMAT`: the attempt then ends with that code, still
permanently. No retry is derived from a format answer, because search and
extraction answers are reused from the query cache for 10 minutes — longer
than the whole retry backoff (5 s, 15 s, 45 s, 2 min) — so a retry would only
replay the same answer.

A budget stop is this backend's own decision. It is candidate scoped: it never
puts a provider family on hold, never records a platform failure on the session
pool and never marks the session. It is not retried, because the next attempt
would select the same stream and spend the same budget again.

Bot, auth and rate-limit failures keep their precedence unchanged — they stop
the candidate fanout before any of this is reached. **The absence of an
audio-only stream never puts a provider family on hold.** Items that already
failed keep their stored result; nothing is migrated or retried automatically.

### Diagnostics

Every acquisition logs `format_kind` (`audio_only` or `combined`), the
provider, the secret-free media and format id, the source audio and video
codec, `transferred_bytes`, `stored_bytes`, `download_ms`, `extract_ms`, the
verification result and `session_lease_ms`. Hourly counters are recorded under
`download.<family>.<audio_only|combined>.{attempted,stored,transferred_bytes,rejected_over_budget}`;
`rejected_over_budget` counts every budget stop, before, during or after the
transfer.
No stream URL, cookie, token or raw tool output is logged.

**A yt-dlp process start is not an HTTP request.** A combined HLS or DASH
stream is fetched segment by segment, so one process makes many requests;
`transferred_bytes` is the figure to judge the fallback by, not the process
counters.

### Cost, measured offline

Synthetic measurement, 200 s (unchanged from the earlier feasibility study):

| File | Size | vs. Opus audio |
|---|---:|---:|
| Opus audio ~130 kbps | 3.9 MB | 1× |
| AAC audio 128 kbps | 3.2 MB | 0.8× |
| combined 360p, 500 kbps video + AAC 96 kbps | 15.1 MB | ~4× |
| combined 720p, 1.5 Mbps video + AAC 128 kbps | 40.9 MB | ~10× |

Copying the audio took 0.06 s and the audio packets were bit-identical to the
source; the regression tests assert that identity through an audio-stream
checksum.

### What remains unknown

- **Real bandwidth.** The combined bitrates YouTube actually serves for these
  items are not known offline. The 64–182 kbps totals in the rejection messages
  are what the extractor announced, not what a transfer costs.
- **Provider throttling.** Whether segment requests count against the account
  throttle differently from a single audio request is unknown, and the RC-2
  window could not measure it.
- **Session budget.** A combined transfer holds the only session's execution
  slot for its whole duration, so each acquisition may remove several normal
  ones from the hourly budget. Whether the net effect is more completed
  downloads is exactly what the controlled comparison has to establish; fewer
  error messages alone is not the goal. The measurements and abort criteria are
  fixed in advance in
  [Combined-stream fallback — controlled comparison plan](/diagnostics/combined-fallback-comparison).
