# Acquisition throughput

This note records why YTMDL completed few verified acquisitions per hour, what
was changed to spend fewer provider requests per acquisition, how the change
was measured, and how a later twelve-hour operating run must be evaluated.

Three kinds of evidence are kept apart:

- **Historical production measurement** — structured logs of one v0.27.1
  instance (preview rejection not yet deployed) over ~13.5 hours, read-only
  aggregate queries of its database. No provider was contacted for this note.
- **Synthetic fixture** — an offline comparison of `dev` and this change with
  the real providers, orchestrator, session pool and matcher behind a yt-dlp
  stub. It compares request counts and elapsed time under identical
  conditions. It is **not** a measurement of real provider throughput.
- **Unknown** — facts that could only be established with provider traffic.
  They stay explicitly unknown.

## Configuration in the measured instance

Two download workers, one managed YouTube session, one lease per session,
0.5 process starts per second per session and 2 per second family-wide. Every
YouTube metadata request and every YouTube download runs through that
session's single execution slot.

## Historical measurement (v0.27.1, 2026-09-10 18:37 – 2026-09-11 08:xx UTC)

First and last hour are partial.

| Hour (UTC) | Stored | YT resolves failed / ok | SC resolves ok | SC DRM aborts | YT 429 | YT pause (min) | Session-wait cycles |
|---|---:|---:|---:|---:|---:|---:|---:|
| 09-10 18:00 | 6 | 185 / 6 | 19 | 8 | 0 | 0 | 12 |
| 09-10 19:00 | 13 | 408 / 11 | 64 | 9 | 9 | 13 | 13429 |
| 09-10 20:00 | 10 | 412 / 6 | 93 | 0 | 8 | 12 | 12558 |
| 09-10 21:00 | 12 | 565 / 6 | 76 | 9 | 13 | 17 | 17874 |
| 09-10 22:00 | 13 | 618 / 8 | 97 | 4 | 13 | 16 | 18973 |
| 09-10 23:00 | 8 | 595 / 7 | 113 | 0 | 17 | 21 | 24414 |
| 09-11 00:00 | 4 | 551 / 5 | 120 | 0 | 18 | 20 | 24405 |
| 09-11 01:00 | 8 | 444 / 9 | 31 | 21 | 16 | 20 | 23920 |
| 09-11 02:00 | 6 | 479 / 7 | 20 | 30 | 14 | 17 | 19196 |
| 09-11 03:00 | 11 | 592 / 10 | 31 | 23 | 13 | 17 | 16088 |
| 09-11 04:00 | 6 | 658 / 9 | 83 | 10 | 12 | 18 | 17383 |
| 09-11 05:00 | 3 | 184 / 3 | 32 | 0 | 0 | 0 | 0 |
| 09-11 06:00 | 5 | 251 / 5 | 24 | 5 | 0 | 0 | 11 |
| 09-11 07:00 | 4 | 472 / 4 | 84 | 0 | 0 | 0 | 0 |
| 09-11 08:00 | 0 | 46 / 0 | 10 | 0 | 0 | 0 | 0 |
| **Total** | **109** | **6460 / 96** | **897** | **119** | **133** | **170** | **188263** |

Supply was not the limit: 4,220 pending and 2,551 retry-wait items of unpaused
jobs were open. Post-processing was not the limit either: download-to-stored
took 0.6 s (median), 1.9 s (p90).

### Findings

1. **Requests per acquisition.** 6,322 of 6,460 failed YouTube resolutions
   were "offers no audio only stream", spread over only 1,003 distinct videos.
   With a reuse window of 10 minutes 70% of them would have been avoided, with
   60 minutes 78%. 17% of the YouTube-phase resolutions re-resolved a video
   that had just failed in the YouTube Music phase of the same attempt. The
   direct-id path extracted the same video twice (probe, then resolve). Even
   hours without a single rate limit completed only 3–5 acquisitions.
2. **Retry loops through independent providers.** All 119 SoundCloud
   "systemic" failures were `This video is DRM protected`, a property of one
   item that was classified as `PROVIDER_UNAVAILABLE`. Each aborted the
   attempt, paused SoundCloud and scheduled a retry that repeated the whole
   YouTube fan-out. 897 SoundCloud resolutions covered 188 distinct items; most
   were previews that failed verification and were retried (rejected before
   download on `dev`).
3. **Hourly throttling.** The 133 YouTube rate limits (`Your account has been
   rate-limited by YouTube for up to an hour`) formed one episode per hour,
   12–22 minutes long, starting around xx:11–xx:20. The median gap between two
   rate limits was 2.2 minutes — the fixed two-minute pause expired and the
   next request was limited again; 120 of 132 gaps contained no YouTube
   acquisition. 50 of the 133 rate limits (38%) followed the previous one within
   30 seconds (39 within 10 seconds): requests already queued behind the
   session slot started into the active block.
4. **Session-wait churn.** 188,263 session-wait cycles (dispatch, two item
   writes, event, log line, no provider contact) in 13.5 hours: every waiting
   retry was cycled through a worker once per pause.

**Main bottleneck:** the number of YouTube requests spent per verified
acquisition, through a single serialized session whose hourly request budget
the account throttle bounds. Throttling and retries amplify it; worker count,
post-processing and supply do not.

## Changes

| Change | Evidence | Effect |
|---|---|---|
| Reuse the outcome of an *identical* yt-dlp metadata invocation for 10 min (searches, extractions) or 15 min (item-scoped `TRACK_NOT_FOUND`), coalesce concurrent identical invocations | findings 1, 2 | Probe, enrichment and resolve share one extraction; retries and duplicate items reuse searches and known dead candidates |
| Pace provider requests at the process start | code path | Reused answers neither wait nor spend the provider budget |
| Resolve a candidate id at most once per attempt and family; remember a failed direct-id candidate | finding 1 | −17% YouTube-phase resolutions in the measured mix |
| `DRM protected` is item-scoped `TRACK_NOT_FOUND` | finding 2 | No SoundCloud pause, no aborted attempt, next candidate within the unchanged fallback bound |
| Release the YouTube lease neutrally before independent providers | code path | SoundCloud work no longer blocks the only YouTube session |
| A cooldown that begins mid-attempt ends the attempt (YouTube) or defers the provider (others) instead of sleeping or sending more requests | finding 3, code path | No requests into an active block, no worker parked for a minute |
| Session gate refuses process starts during an active family-wide pause, as a neutral wait | finding 3 | Removes the in-flight follow-up rate limits |
| Family-wide pause after consecutive rate limits escalates 2 → 4 → 6 min (±10%), reset only by a verified acquisition or 30 quiet minutes | finding 3 | Modelled on the ten observed episodes: 88 → 44 rate limits, +1.1 min mean overshoot per episode |
| Retry no earlier than a known cooldown or wait hint; spread session waits by ≤10% (≤30 s); consistent manager clock | findings 3, 4 | No attempt spent on a known block, no lock-step resumption |
| Dispatcher leaves due retries of a provider that is known to be unavailable untouched (in-memory pre-check, no contact, no writes); other jobs continue | finding 4 | Removes the session-wait churn |
| Sanitized format shape on "no audio only stream" | finding 1 (cause unknown) | Evidence for the next step without URLs or raw output |
| Hourly `throughput summary` log line | run plan | Per-hour acquisitions, requests, reuse, rate limits, pause time, failure reasons, supply |

Unchanged: matching thresholds, candidate limits, preview rejection,
verification, session healing (only `RecordDownloadOutcome(nil)` after a
verified download), bot-challenge and auth handling, pauses, storage guard,
fairness, download window, worker count, cookies. No session, account or IP
rotation was added: the account rate limit stays a family-wide pause.

## Synthetic fixture (not a real provider measurement)

`internal/orchestrator/throughput_fixture_test.go`, 150 items, 2 workers, one
session with production pacing, time compressed 40×. Calibrated to the
historical mix: 18% duplicate items, 13% direct ids, 9% of YouTube videos with
an audio only stream, one shared video between the two YouTube searches,
SoundCloud 60% previews / 10% DRM / 30% full. The identical file was run on
`dev` and on this branch.

| Metric | `dev` | this branch | without query cache |
|---|---:|---:|---:|
| Completed / failed | 133 / 17 | 136 / 14 | 136 / 14 |
| Attempts | 304 | 150 | 150 |
| YouTube processes (search + extract) | 550 + 3,600 | 198 + 824 | 242 + 1,434 |
| SoundCloud processes (search + extract) | 115 + 200 | 52 + 106 | 67 + 140 |
| YouTube requests per success | 31.2 | 7.5 | 12.3 |
| All provider requests per success | 33.6 | 8.7 | 13.8 |
| Fixture time (projected production time) | 331 s (3.7 h) | 92 s (1.0 h) | 134 s (1.5 h) |

Per item, all 133 `dev` successes resolved to the identical source on the
branch; none was lost. The three additional successes are items whose
SoundCloud DRM candidate had aborted the attempt on `dev`; they now use the
next full candidate within the unchanged fallback bound. The fixture has no
rate limits, so it shows the request reduction, not the throttling effects.

## Remaining limits for 100 acquisitions per hour

- **Hourly YouTube budget.** In throttled hours the session sent at least
  roughly 900 YouTube metadata requests per hour before the account throttle
  began: about 600 logged resolutions, 110 direct-id probes and two searches
  per attempt (about 200); enrichment requests were not logged. The budget is
  external and unchanged.
- **Projection, not a promise.** Over the whole window about 10,500–12,000
  YouTube metadata requests produced 83 YouTube acquisitions, roughly 100–145
  per acquisition. Applying the synthetic reduction factor (31.2 → 7.5, about
  4.2×) gives 24–35 requests per acquisition, i.e. about 25–45 acquisitions per
  hour if the budget stays the same. Whether fewer requests also shorten the
  throttling episodes is unknown. 100 per hour is not supported by the
  evidence so far.
- **Resolvable candidates.** Only about 9% of distinct YouTube candidates
  offered an audio only stream. yt-dlp lists HLS audio renditions without a
  codec, and YTMDL rejected them; that misclassification is corrected (see
  [audio format classification](./audio-format-classification.md)). How many
  production rejections it explains is **unknown** until the format shape in
  the error message is evaluated. This is the largest remaining lever.
- **Age-restricted candidates.** An age restriction of a single video paused
  the whole YouTube family and was hit again on every retry: a quarter of all
  family pauses in the 0.27.1 logs, eleven of twelve in the first hour of
  0.27.2-rc.1; `This video is unavailable` caused the remaining one. Both are
  now candidate failures (see
  [age-restricted candidates](./age-restricted-candidates.md)); their effect on
  live throughput is not measured yet.
- **SoundCloud.** Mostly previews and DRM-protected items; it cannot close the
  gap.
- **No further sessions or accounts.** Adding them would bypass the account
  rate limit and is out of scope.

## Twelve-hour operating run

Requires separate authorization, a release and a rollout; none is part of
this change.

**Before the start.** Keep workers, session count, cookies, pacing, download
window, pauses (including the protected paused parent jobs) and storage guard
exactly as configured. Record the supply with a read-only query:

```sql
BEGIN READ ONLY;
SELECT i.status, count(*),
       count(*) FILTER (WHERE i.status = 'retry_wait' AND (i.next_retry_at IS NULL OR i.next_retry_at <= now())) AS due
FROM job_items i JOIN jobs j ON j.id = i.job_id
WHERE NOT j.paused AND i.status IN ('pending', 'retry_wait')
GROUP BY i.status;
COMMIT;
```

**During the run.** Every hour the backend logs one `throughput summary`
(and a partial one at shutdown) with, among others:

| Field | Meaning |
|---|---|
| `items.completed` | new verified, stored acquisitions (skips and existing files are `items.skipped`) |
| `items.error.<CODE>`, `items.session_wait` | failure reasons per attempt |
| `ytdlp.youtube.{search,extract,download}.process` | YouTube processes started |
| `ytdlp.<family>.<op>.{cache_hit,shared,gate_refused}` | answers reused or refused without a request |
| `ytdlp.<family>.<op>.error.<CODE>` | provider responses by class |
| `platform.youtube.PROVIDER_RATE_LIMITED`, `platform.youtube.cooldown_ms` | rate limits and pause time added |
| `cooldown.<family>.trigger`, `cooldown.<family>.ms` | family cooldowns |
| `session.youtube.<CODE>` | session-scoped failures |
| `items.ready.last/max`, `items.held_for_provider.last/max` | runnable supply and work held for a blocked provider |
| `dispatch.free_worker_without_ready_item` | dispatch passes (at least every 5 s) that found a free worker but nothing runnable |

Tabulate the run:

```sh
jq -r 'select(.msg=="throughput summary") | [.window_start, (.["items.completed"]//0),
  ((.["ytdlp.youtube.search.process"]//0)+(.["ytdlp.youtube.extract.process"]//0)),
  (.["ytdlp.youtube.download.process"]//0), (.["platform.youtube.PROVIDER_RATE_LIMITED"]//0),
  ((.["platform.youtube.cooldown_ms"]//0)/60000|floor), (.["items.ready.max"]//0),
  (.["dispatch.free_worker_without_ready_item"]//0)] | @tsv' backend.log
```

**Evaluation.** Report every hour separately: acquisitions, YouTube requests
per acquisition, rate limits, pause minutes, failure reasons, supply. The goal
counts only `items.completed` — never skips, existing files, duplicates or
previews. An hour with many `dispatch.free_worker_without_ready_item` passes
while `items.held_for_provider` stayed zero is marked supply-limited, not
provider-limited. Group the "no audio only stream" format
shapes to decide the next lever.

**Stop criteria.** Any bot challenge or authentication failure on the session;
a sustained rise of verification failures; storage guard or space waits. Do
not rotate sessions, change cookies, reset cooldowns or bulk-retry to recover.
