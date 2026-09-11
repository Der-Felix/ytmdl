# Age-restricted candidates

In production a YouTube video that is age restricted paused the whole YouTube
family for a minute, and the next attempt of the same item resolved the same
video and paused the family again. This note records the path that caused it,
how an age restriction of a single item is told apart from a session or
platform failure, and what changed. Age restrictions are never unlocked or
worked around: an inaccessible source is skipped.

Evidence: read-only aggregation of the existing backend logs of 0.27.1
(2026-09-10 18:37 – 2026-09-11 11:31 UTC) and of the first hour of
0.27.2-rc.1, plus offline tests. No provider request was made.

## The rule that paused the family

1. `ytdlp.ClassifyError` knew no age restriction. The two statements YouTube
   returned fell through to the default branch and became
   `PROVIDER_UNAVAILABLE`, which has provider scope:
   - `Sorry, this content is age-restricted`
   - `Verify your age. Complete a brief check to show you're old enough to play
     this content.`
2. Provider scope stops the candidate fan-out (`apperr.StopsCandidateFanout`).
   The orchestrator calls `handleSystemicFailure`, which triggers a 60 s
   cooldown for the whole family (`cooldown.Trigger("youtube", 60s)`), and
   releases the lease with the error, so the session pool records a
   platform-wide failure (`recordPlatformFailureLocked`) as well.
3. The worker retries `PROVIDER_UNAVAILABLE` (`apperr.Retryable`) after about
   62 s. Every other YouTube item meanwhile waits with `SESSION_UNAVAILABLE`.
   The next attempt ranks the same video first and repeats step 1, up to the
   item's five attempts.

| Log window | YouTube family pauses | caused by an age restriction | distinct videos |
|---|---|---|---|
| 0.27.1, 16.9 h | 187 | 47 (30 "Verify your age", 17 "Sorry, … age-restricted") | 10 (nine of them 5 times each) |
| 0.27.2-rc.1, first 53 min | 12 | 11 | 3 |

The remaining pauses were rate limits (138 in 0.27.1), two DNS failures and
one `This video is unavailable` (see [below](#this-video-is-unavailable)).

## Content restriction versus session failure

| Statement | Meaning | Handling |
|---|---|---|
| `Sorry, this content is age-restricted`, `This video is age-restricted …` | the item is refused to this session context | candidate failure (new) |
| `Verify your age … old enough to play this content` | the signed-in account has not verified its age; every age-restricted item is refused, every other item plays | candidate failure for this session context (new) |
| `Sign in to confirm your age …` | the platform does not see a signed-in session although cookies are configured | session authentication failure (unchanged) |
| `Sign in to confirm you're not a bot` | bot challenge | session protection (unchanged) |
| `HTTP Error 429`, `… rate-limited`, `try again later` | rate limit | family or session protection (unchanged) |
| age statement plus a sign-in, login, cookie, authentication or account hint | ambiguous | previous handling (unchanged) |

The classifier checks rate limits, bot challenges and sign-in or credential
failures first, so an age statement in the same output can never hide them.
The new rule only reads yt-dlp's `ERROR:` lines; warnings about the
extraction do not describe the item.

## Correction

- An unambiguous age restriction is `TRACK_NOT_FOUND` — the candidate scope
  that DRM-protected and unavailable items already use — with a fixed message
  that contains no provider output. The orchestrator logs it with the media id
  and moves on to the next acceptable candidate within the unchanged fallback
  bound. No family cooldown, no platform failure, no session failure count,
  and no session healing: the lease is released neutrally.
- A candidate that failed is not resolved again in the same attempt (the
  existing per-attempt set). Between attempts and across items the existing
  negative query cache answers the same video for 15 minutes; its key contains
  the cookie file, so the answer is kept per session context and expires.
- When every candidate is restricted the attempt ends with the existing
  permanent "Keine der N passenden Quellen konnte aufgelöst werden." result
  instead of a retry loop. A restriction first reported by the download is the
  same candidate failure, no longer a retryable `DOWNLOAD_FAILED`.
- Diagnostics: the hourly `throughput summary` counts
  `ytdlp.youtube.<op>.candidate.age_restricted` next to
  `ytdlp.youtube.<op>.error.TRACK_NOT_FOUND`.

## "This video is unavailable"

The existing rule for unavailable items matches `Video unavailable`, not
YouTube's `This video is unavailable`, which therefore also fell to the
default branch and paused the family (once in the first hour of
0.27.2-rc.1). The same statement about the requested video is now the same
candidate failure (`candidate.unavailable`), with the same precedence and
ambiguity rules: rate limits, bot challenges and sign-in or cookie failures
win; wording that also mentions the account, sign-in, cookies, "try again",
"temporarily", "later", a rate limit or `Service Unavailable` keeps its
previous handling. Any other sentence that merely contains "unavailable"
is untouched.

## Resolve phase versus download phase

Both classifications apply wherever yt-dlp reports them, but the item reacts
differently:

| Phase | Counter in the hourly `throughput summary` | Item result |
|---|---|---|
| resolve (metadata extraction) | `ytdlp.youtube.extract.candidate.{age_restricted,unavailable}` | next acceptable candidate; when none is left, "Keine der N passenden Quellen konnte aufgelöst werden." |
| download | `ytdlp.<family>.download.candidate.{age_restricted,unavailable}` | the item fails at once with `TRACK_NOT_FOUND` and keeps the resolved media id; **no other candidate is selected** |

The download-phase row is a known limit of this change: a source that
resolved but is refused only by the download process ends the item without a
second candidate selection. It is permanent and triggers no pause, so it
cannot loop, but a later candidate is not tried. The counters show how often
it happens in normal operation; a download-phase fallback is a separate
change. In the job items the two phases are also distinguishable: the
resolve-phase result starts with "Keine der", the download-phase result is
the plain category message with the media id set.

## Expected effect and open questions

- In 0.27.1 age restrictions caused a quarter of all YouTube family pauses, in
  the first hour of 0.27.2-rc.1 eleven of twelve; `This video is unavailable`
  caused the twelfth. Each pause stopped every YouTube item for a minute. How
  much measured throughput this frees is unknown until a separate,
  normal-operation measurement of a release that contains the change; it is
  not claimed here.
- Items whose only acceptable candidates are restricted or unavailable now
  fail permanently instead of after five attempts. They were never going to
  succeed on this session.
