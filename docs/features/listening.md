# Listening API: Mixes, Radio, Statistics, Lyrics Search and Preferences

This page is the contract for clients (web, iPhone, iPad, Apple TV) of the
listening endpoints. Everything is computed on the server from the user's own
library and listening history; nothing leaves the server.

All paths are under `/api/v1`, need a signed-in session and answer in the
usual envelope `{"data": …}` or `{"error": {"code", "message", "request_id"}}`.
State-changing requests need the CSRF header and cookie (see the
[API reference](../api.md)). Every answer is private to the signed-in user:
another user's history, mixes, statistics and preferences are never visible.

A `track` object (written `LibraryTrack` below) is the same object
`GET /library/tracks` returns (`id`, `title`, `artists`, `album`,
`album_artist`, `duration_ms`, `year`, `release_id`, `artist_id`, …).

## Detecting a server that has these endpoints

`GET /health` (public) lists optional capabilities in `data.features`:

```json
{"data": {"status": "ok", "version": "…", "uptime_seconds": 42, "checks": {…},
  "features": ["mixes.groups", "mixes.artist", "mixes.decade", "mixes.favorites", "mixes.daypart",
               "radio.seeds", "radio.station", "history.stats", "search.lyrics", "listening.preferences"]}}
```

| Feature | Meaning |
| :--- | :--- |
| `mixes.groups` | Mixes carry `group` (and `artist_id`, `decade`, `daypart` where they apply); `/library/mixes` accepts `tz`. |
| `mixes.artist`, `mixes.decade`, `mixes.favorites`, `mixes.daypart` | The new mix kinds can appear. |
| `radio.seeds` | `/library/radio` accepts `artist`, `release`, `limit` and `exclude`. |
| `radio.station` | `GET /library/radio/station` exists. |
| `history.stats` | `GET /history/stats` exists. |
| `search.lyrics` | `/library/search` returns the `lyrics` group. |
| `listening.preferences` | `/listening-preferences` exists, and mixes and radio honour it. |

A server without `features` (1.10.1 and older) has none of them. Check the
flag rather than probing: such a server answers a path it does not know with
`400 INVALID_REQUEST` and the message "The requested endpoint does not
exist." (not `404`), and its existing endpoints ignore the new query
parameters (`/library/radio?artist=…` there is the personal radio). Fall back
as follows on such a server:

- Mixes: show what `/library/mixes` returns; without `group`, lay out genre
  mixes first and the rest after (as the web does).
- Radio: call `/library/radio?seed=<track>` (track seeds only) and build the
  title on the client; drop artist, release and genre radio, or use
  `/library/radio?genre=<name>` for a genre (personal radio filtered to it).
- Statistics: hide the screen, or derive simple figures from `GET /history`
  (play counts, no periods).
- Lyrics search: no lyric hits; the other search groups are unchanged.
- Preferences: hide "Künstler ausschließen", "Genre ausschließen" and
  "Weniger davon".

## Time zone

Mixes and statistics depend on the user's day. Send the device's IANA time
zone as `tz` (for example `tz=Europe/Berlin`). Without it the server uses its
own `TZ` setting, else UTC. An unknown zone is `400 INVALID_REQUEST`.

## Personal mixes

### `GET /library/mixes`

Query: `tz` (optional).

Returns an array of mixes. Each mix is stable for the user's calendar day
(the same `id` lists the same tracks all day) and changes the next day. A mix
that cannot reach 10 tracks is not listed. A mix holds at most 50 tracks, at
most 3 per artist (the artist of an artist mix up to 35), one version per
recording, only tracks with a file, and never a track the user excluded (see
[Preferences](#listening-preferences)) or skipped repeatedly.

| Field | Type | Notes |
| :--- | :--- | :--- |
| `id` | string | Stable per day; see the table below. |
| `kind` | string | See the table below. Treat unknown kinds like any other mix. |
| `title` | string | German, ready to show. |
| `description` | string | German, ready to show. |
| `group` | string | `made_for_you`, `discover` or `rewind`. New; absent on older servers. |
| `genre` | string? | Genre mixes only. |
| `artist_id` | string? | Artist mixes only. New. |
| `decade` | int? | Decade mixes only (e.g. `1990`). New. |
| `daypart` | string? | Daypart mixes only: `morning`, `afternoon`, `evening`, `night`. New. |
| `track_count` | int | |
| `cover_track_ids` | string[] | Up to four, different releases first; feed `/library/tracks/{id}/artwork`. |
| `cover_release_ids` | string[] | Their releases. |

| `kind` | `id` | Title | Group | Source |
| :--- | :--- | :--- | :--- | :--- |
| `genre` | `genre-<hash>` | Dein Mix 1 … 6 | made_for_you | The user's genres by plays, favourites and skips; the library's largest genres for a new user. About 60 % liked tracks, 40 % rarely heard ones. |
| `daypart` | `daypart-morning` … | Dein Morgen-Mix, Dein Nachmittags-Mix, Dein Abend-Mix, Dein Nacht-Mix | made_for_you | Tracks played at this time of day (morning 5–11, afternoon 11–17, evening 17–22, night 22–5) in the last 120 days; plays on the same kind of day (weekday or weekend) count 1.5. Needs at least 20 such plays. The list offers the mix of the current daypart; every `daypart-*` id can be opened at any time. |
| `artist` | `artist-<hash>` | Mix: <Künstler> | made_for_you | Up to three of the user's top artists (plays and favourites): about 20 tracks of the artist, the rest from artists of its main genre. |
| `favorites` | `favorites` | Favoriten-Mix | made_for_you | The user's favourites, those not played for longest first. |
| `on_repeat` | `on_repeat` | In Dauerschleife | made_for_you | Played at least twice in the last 30 days. |
| `rediscover` | `rediscover` | Wiederentdecken | rewind | Played at least three times, not in the last 60 days. |
| `forgotten_favorites` | `forgotten_favorites` | Vergessene Favoriten | rewind | Favourites older than 30 days, not played for 90 days. |
| `decade` | `decade-1990` | Deine 90er, Deine 2000er | rewind | Up to two decades with at least five different played tracks (track year, else release year): the user's tracks of the decade plus barely known ones by the user's artists. |
| `unheard` | `unheard` | Noch nicht gehört | discover | Never played tracks by the user's artists. |
| `new` | `new` | Neu in der Mediathek | discover | First file added in the last 30 days. |

Order of the list: genre mixes, daypart, artist mixes, favourites, on repeat,
rediscover, forgotten favourites, decades, unheard, new.

Example:

```json
{"data": [
  {"id": "genre-3f1a9c0b2d4e", "kind": "genre", "title": "Dein Mix 1", "description": "Deine Favoriten aus Rock und Titel, die du kaum kennst",
   "group": "made_for_you", "genre": "Rock", "track_count": 50, "cover_track_ids": ["t1", "t2", "t3", "t4"], "cover_release_ids": ["r1", "r2", "r3", "r4"]},
  {"id": "daypart-evening", "kind": "daypart", "title": "Dein Abend-Mix", "description": "Was du abends gern hörst",
   "group": "made_for_you", "daypart": "evening", "track_count": 38, "cover_track_ids": ["t9"], "cover_release_ids": ["r9"]},
  {"id": "artist-9b0c11d2e3f4", "kind": "artist", "title": "Mix: Daft Punk", "description": "Daft Punk und ähnliche Künstler",
   "group": "made_for_you", "artist_id": "a1", "track_count": 50, "cover_track_ids": ["t5"], "cover_release_ids": ["r5"]},
  {"id": "decade-1990", "kind": "decade", "title": "Deine 90er", "description": "Deine Titel aus den 1990er-Jahren und mehr aus dem Jahrzehnt",
   "group": "rewind", "decade": 1990, "track_count": 50, "cover_track_ids": ["t7"], "cover_release_ids": ["r7"]}
]}
```

### `GET /library/mixes/{id}`

Query: `tz` (optional; send the same as for the list). Returns the list
entry plus `tracks` (`LibraryTrack[]`, mix order). `404 PLAYLIST_NOT_FOUND`
when the mix is not offered today (reload the list).

## Radio

Seeds and parameters, shared by both endpoints:

| Parameter | Meaning |
| :--- | :--- |
| `seed` | A track id: song radio (existing behaviour). |
| `artist` | An artist id: artist radio. New. |
| `release` | A release id: album radio (its artist, the artists on it, its era). New. |
| `genre` | Without a seed: genre radio. With a seed: only artists with this genre. |
| `nonce` | Varies the shuffle; the same nonce gives the same order. |
| `exclude` | Track ids to leave out, comma separated or repeated, at most 500 (for example the queue and the session's played tracks). New. |

At most one of `seed`, `artist` and `release`; more is `400 INVALID_REQUEST`.
Without any seed or genre the radio is personal ("Dein Radio"). Seeded radio
stays near the seed (the seed's artist, artists with the same main genre or a
shared release first, artists that list the main genre after; never artists
that share only a secondary genre), at most five tracks per artist. Recently
played tracks rank lower, skipped tracks lower still. Excluded artists, genres
and recordings never play, also when they are the seed. Errors:
`404 TRACK_NOT_FOUND` (seed track has no file), `ARTIST_NOT_FOUND`,
`RELEASE_NOT_FOUND`, `400 INVALID_REQUEST`.

### `GET /library/radio`

Unchanged response: `LibraryTrack[]`. New parameters `artist`, `release`,
`exclude` and `limit` (1–500, default 50).

### `GET /library/radio/station`

A titled station served in batches, without repeats.

Query: the seed parameters above, `limit` (batch size, 1–100, default 25),
`cursor` (from the previous batch).

| Field | Type | Notes |
| :--- | :--- | :--- |
| `title` | string | `Radio: <track title>`, `Radio: <artist>`, `Radio: <album>`, `Radio: <genre>`, or `Dein Radio`. |
| `seed.kind` | string | `track`, `artist`, `release`, `genre` or `personal`. |
| `seed.id` | string? | Track, artist or release id. |
| `seed.name` | string | What the title names. |
| `tracks` | LibraryTrack[] | This batch, station order. |
| `next_cursor` | string or null | Pass as `cursor` for the next batch; `null` when the station is used up (at most 500 tracks). |
| `remaining` | int | Tracks left in the station after this batch. |

The first request computes the whole station (up to 500 tracks) and keeps it
on the server for six hours; following batches continue exactly where the
last one ended, so nothing repeats. Exclusions made meanwhile, and `exclude`,
still apply to every batch. Send the same seed parameters with every cursor:
if the station has expired (or the server restarted), it is computed again
from them with the same `nonce` and continues at the cursor's position; then
repeats are possible but unlikely, and `exclude` rules them out.
`400 INVALID_REQUEST` for a malformed cursor.

```json
{"data": {"title": "Radio: Daft Punk", "seed": {"kind": "artist", "id": "a1", "name": "Daft Punk"},
  "tracks": [{"id": "t5", "title": "Digital Love", "artists": ["Daft Punk"], "duration_ms": 301000}],
  "next_cursor": "5f2c0e9a1b7d3c44aa01e2f3.25", "remaining": 212}}
```

## Listening statistics

### `GET /history/stats`

Query: `period` = `week` (last 7 days), `month` (last 30 days, default),
`year` (last 365 days) or `all`; `limit` (top lists, 1–50, default 10); `tz`.

| Field | Type | Notes |
| :--- | :--- | :--- |
| `period` | string | |
| `from` | timestamp or null | Start of the window (`null` for `all`). |
| `to` | timestamp | Now. |
| `since` | timestamp or null | The user's first timestamped play. |
| `complete` | bool | See below. |
| `source` | string | `events` (week, month, year) or `aggregate` (all). |
| `time_zone` | string | The zone days and months are counted in. |
| `totals.plays` | int | |
| `totals.distinct_tracks`, `totals.distinct_artists` | int | |
| `totals.listening_ms` | int | Estimate: each play counted with the track's full duration. |
| `totals.untimed_plays` | int | `all` only: plays without a timestamp (not in `activity`). |
| `top_tracks[]` | `{track: LibraryTrack, plays}` | Most played first. |
| `top_artists[]` | `{id, name, plays, distinct_tracks}` | |
| `top_releases[]` | `{id, title, artist_id, artist_name, plays}` | |
| `top_genres[]` | `{name, plays}` | A play counts for every genre of its artist. |
| `activity.bucket` | string | `day` (week, month) or `month` (year, all). |
| `activity.points[]` | `{start: "YYYY-MM-DD", plays, listening_ms}` | Every day or month of the window, zeros included. |

What is exact: every play is stored twice, as a timestamped event
(`playback_events`, since the listening history arrived in 1.1.0) and in a per-track
counter (`listening_history`). `week`, `month` and `year` count events, so
their figures are exact for the window, unless the user's history starts
inside it: then `complete` is `false` and the figures cover only `since` to
now. `all` uses the counters, which can hold plays without a timestamp (for
example restored from older data); they count in `totals` and the top lists
but not in `activity`, and `untimed_plays` says how many; `complete` is
`true` when there are none. Listening time is always an estimate. A track
deleted from the library takes its plays with it.

```json
{"data": {"period": "week", "from": "2026-11-19T13:00:00Z", "to": "2026-11-26T13:00:00Z",
  "since": "2026-02-03T18:21:07Z", "complete": true, "source": "events", "time_zone": "Europe/Berlin",
  "totals": {"plays": 41, "distinct_tracks": 33, "distinct_artists": 19, "listening_ms": 9512000, "untimed_plays": 0},
  "top_tracks": [{"track": {"id": "t5", "title": "Digital Love"}, "plays": 4}],
  "top_artists": [{"id": "a1", "name": "Daft Punk", "plays": 9, "distinct_tracks": 6}],
  "top_releases": [{"id": "r5", "title": "Discovery", "artist_id": "a1", "artist_name": "Daft Punk", "plays": 7}],
  "top_genres": [{"name": "Electronic", "plays": 15}],
  "activity": {"bucket": "day", "points": [{"start": "2026-11-19", "plays": 3, "listening_ms": 690000}]}}}
```

Errors: `400 INVALID_REQUEST` for an unknown `period` or `tz`.

## Lyrics search

`GET /library/search?q=…` (existing) returns a new group `lyrics` next to
`artists`, `releases` and `tracks`, whose meaning is unchanged:

```json
{"data": {"artists": [], "releases": [], "tracks": [],
  "lyrics": [{"track": {"id": "t9", "title": "Über den Wolken"},
              "snippet": "… Abend / Über den Wolken muss die Freiheit wohl grenzenlos sein …",
              "match": "Über den Wolken"}]}}
```

- Every word of `q` must occur in the lyrics, the last one as a prefix
  (`über den wol` finds "Über den Wolken"). Hits with the words in order come
  first. Upper and lower case, accents and umlauts do not matter
  (`uber` = `über`, `strasse` = `Straße`). No stemming, so German and English
  match as written.
- `snippet` is about 45 characters around the match, cut at word boundaries;
  line breaks are shown as ` / `, `…` marks cut text. `match` is the matched
  text exactly as it appears in `snippet`, for highlighting.
- As many lyric hits as track hits (`limit`, at most 20); a query shorter
  than three letters returns none. `lyrics=false` skips the group (it is
  then `[]`).
- Only stored lyrics count: tracks whose lyrics state is available and whose
  `.lrc`/`.txt` sidecar has been indexed. The sidecar stays the source of
  truth; the server keeps a search index of it, filled in the background
  (about 200 tracks every few seconds until it has caught up, then every ten
  minutes) and right away when lyrics are fetched or deleted in the app. A
  large library is searchable within minutes to an hour after the update.

## Listening preferences

Steering for mixes and radio. An exclusion is never chosen for the user in a
mix or a radio station; playing it explicitly (album, playlist, search,
favourites) stays possible, and it still counts in statistics.

| `kind` | `target` | Effect |
| :--- | :--- | :--- |
| `artist` | artist id | No track whose artist is this one. |
| `genre` | genre name (case-insensitive) | No track by an artist listing this genre; no genre mix of it. |
| `track` | track id | "Weniger davon": this recording in every version (same artist, same title) never again; stronger than a skip. |

### `GET /listening-preferences`

```json
{"data": [
  {"id": "9f1c…", "kind": "track", "target": "t5", "name": "Digital Love", "artist_name": "Daft Punk", "created_at": "2026-11-26T12:00:00Z"},
  {"id": "77ab…", "kind": "genre", "target": "Schlager", "name": "Schlager", "created_at": "2026-11-25T09:30:00Z"},
  {"id": "03de…", "kind": "artist", "target": "a7", "name": "DJ Ötzi", "created_at": "2026-11-24T20:00:00Z"}
]}
```

Newest first. `name` is the artist's name, the track's title or the genre;
`artist_name` is set for tracks.

### `PUT /listening-preferences` (CSRF)

Body `{"kind": "artist" | "genre" | "track", "target": "…"}`. Returns the
preference (`200`); adding one that exists returns the existing one.
Errors: `400 INVALID_REQUEST` (unknown kind, empty target, genre over 80
characters, more than 1000 preferences), `404 ARTIST_NOT_FOUND`,
`404 TRACK_NOT_FOUND`.

### `DELETE /listening-preferences/{id}` (CSRF)

`204`. `404 PREFERENCE_NOT_FOUND` when the id does not exist or belongs to
another user.

### `DELETE /listening-preferences?kind=…&target=…` (CSRF)

Removes the preference for that target ("Ausschluss aufheben" from a track
or artist menu without looking up the id). `204`, also when there was none.

Preferences of a deleted artist or track disappear with it; clearing the
listening history keeps them.

## Cost on a large library

Expected cost for about 150,000 tracks (by construction of the queries and
the indexes they use; not benchmarked against a production copy):

- Mix candidates are bounded by the user's own history or by a daily
  shuffle with a `LIMIT` (2000 liked, 300–600 new per mix); the artist mix
  reads the artists table once and at most 30 related artists through
  `idx_tracks_artist`; the daypart mix reads at most 20,000 events through
  `playback_events_user_time`. The whole list runs about 20 bounded queries.
- Seeded radio expands at most 100 candidate artists through
  `idx_tracks_artist`; the personal and genre radio scan the tracks once, as
  before. A station is computed once and its later batches are a lookup.
- Statistics read the window's events through `playback_events_user_time`
  (`user_id, played_at`), `all` the counters through the primary key of
  `listening_history`.
- Lyrics search uses the GIN index `idx_track_lyrics_search_document` and
  ranks at most 1000 matches.
- Exclusions are a few rows per user (`listening_exclusions_unique`), turned
  into hashed anti-joins; the scan of artists for excluded genres only runs
  when the user excluded a genre.
