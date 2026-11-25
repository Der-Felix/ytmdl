# Native listening preview 0.4.3 — build 37

The native app is a player for an existing YTMDL library. Its version is independent
of the server version. This preview does not promise full Spotify/Plexamp parity.

Build 33 only fixes defects (see [HANDOFF.md](HANDOFF.md)). The matrix below includes the listening parity with web 1.10 that build 34 adds and build 35 completes, the Apple TV side that build 36 adds and the Apple TV sound settings of build 37 (see below).
See [AUDIT.md](AUDIT.md) for the dated functional audit through build 32, test evidence,
corrected defects and the remaining real-device release checks.
The [release checklist](RELEASE-CHECKLIST.md) tracks qualification separately
from implemented features and simulator verification.

| Feature | Current implementation |
| --- | --- |
| iPhone/iPad Start | Listening home like the web's **Hören**: Weiterhören (current title or last queue), personal mixes, server history **Zuletzt gehört** (local list as fallback), **Neu in der Mediathek**, favorites/playlist shortcuts; offline hint when the server is unreachable. Mac Start shows the same mixes and server history. Apple TV has its own **Start** tab with the same sections as focusable shelves |
| Personal mixes | `GET /library/mixes`: "Dein Mix 1…n" with genre label plus the listening mixes, collage cover, track count, play button; **Für dich** overview (also on Apple TV) and mix page with play, shuffle, play next, add to queue, **Als Playlist speichern** (needs a connection) and track sorting like the web. Older servers keep the previous local genre shortcuts |
| Artist page | **Am meisten gehört** from `GET /library/artists/{id}/top-tracks` (5, up to 10 on request); queue actions for the whole artist |
| Library/search | Existing library artists, albums, tracks, genre filter and authenticated artwork |
| Favorites/playlists | Native editing, ordering, membership, smart server rules and favorites |
| Offline music (iOS/iPadOS/macOS) | Track, album, artist, favorites and playlist snapshots; visible collection groups, cached covers, saved playlist order, local search, artist/album/title/date sorting and shuffle |
| Downloads | Background URLSession on real iOS, progress, pause/retry, Wi-Fi preference, music storage cap, remove local copies |
| Smart offline storage | Optional played-track cache; favorites and selected collections refresh when the library is refreshed |
| Player access | Native mini-player above the floating iPhone tab bar, retained inside pushed collections; direct Player toolbar entry; expandable player |
| Collection artwork | Up to four distinct album covers on collection headers; shared, bounded previews in mobile Start and Playlists |
| Mobile collection design | Integrated cover/title/metadata header, equal-width play/shuffle actions with 52-point minimum height; playlist editing/download actions in one menu; stacked actions at accessibility text sizes |
| Home/Lock Screen widgets (iOS/iPadOS) | Small/medium Home widgets and circular/rectangular/inline Lock Screen widgets open the player; medium Home widgets also link to favorites and playlists. Navigation only, no live song state or playback controls |
| Mobile transport | Seeking, previous/next, shuffle, repeat queue/track, native device volume and AirPlay |
| System artwork | Now Playing artwork published after authenticated/local loading, retained across progress updates, cleared on title change or stop; actual lock-screen review remains required |
| Mobile sound | Ten-band EQ, presets, preamp/headroom, measured loudness normalization, 0–12 second crossfade, adjacent-album protection, preload, fast start, speed, sleep timer. Apple TV has the same settings under **Optionen → Klang & Wiedergabe** (values with −/+ buttons) plus sleep timer and tempo menus in the player |
| Mobile visualization | Four mobile FFT-driven styles, cover overlay/no cover, preset or custom color, Reduce Motion support |
| Lyrics | Cached/plain text; LRC timing, optional following and tap-to-seek when time tags exist |
| Queue | Sections like the web: **Als Nächstes** (queued), **Weiter aus: …** (started context), **Ähnliche Titel** (autoplay). Occurrence-based play/remove/insert-next/reorder, filter, clear upcoming/all with **Rückgängig** for a few seconds, save as playlist, local resume state and explicit restore that keeps each entry's section and the started context; whole albums, artists, playlists and mixes can be queued. Apple TV edits upcoming entries through a long-press menu and has the same filter |
| Song/genre radio | Existing local-library server recommendations; **Automatisch weiterspielen** (on by default) appends at most 12 similar titles when two or fewer remain, leaving out titles already queued or played earlier in the session (last 200, per account and server), and tops up again at the next lookahead; an explicitly started song radio keeps its full list. Not with repeat, sleep timer or offline-only playback |
| Listening history | Local recent metadata and queue; plays and skips with the web's rules (`ListeningTracker`) as idempotent server events, also for local files, queued while offline and scoped to origin/account |
| Playback transfer | Explicit save/pause and fetch/accept via existing server handoff routes; no remote automatic playback |
| Appearance | Light/dark/system and six mobile accent palettes; cover-derived player background |
| Device login | Existing device-code approval; backend support/deployment required |

## Apple TV sound and queue filter (build 37)

Added on top of build 36; it uses the server API of v1.10.x unchanged. It ships as 0.4.3 (37).

- **Klang & Wiedergabe on Apple TV**: **Optionen** has a **Klang** section that opens the
  iPhone's sound settings for the remote. **Sleep-Timer** (all choices, with the time left
  while a minute timer runs), **Tempo** (0.5× to 2×), **Überblendung** (0–12 s),
  **Albentitel ohne Überblendung**, **Nächsten Titel vorladen**, **Schneller
  Abspielstart**, **Automatisch weiterspielen**, **Lautstärke-Normalisierung** with its
  status and **Lautheit erneut prüfen**, and the equalizer: **Equalizer aktivieren**,
  **Klangprofil**, the ten bands and **Vorverstärkung** (1 dB per press), **Automatischer
  EQ-Pegelschutz** and **Klang zurücksetzen**. tvOS has no slider, so values change with −
  and + buttons; a button at the end of the range is disabled. The bands appear only while
  the equalizer is on. The settings use the same player and the same stored values as the
  other platforms; the audio processing tap and time-pitch algorithms run on tvOS too.
- **Player on Apple TV**: under the transport, **Sleep-Timer** (shows the time left),
  **Tempo** and **Titel-Aktionen** (**Song-Radio starten**, **Zur Playlist hinzufügen**)
  menus. Repeat cycles off, queue and this title like on the iPhone. The large "Player"
  title is gone; it overlapped the queue panel when that scrolled (the tab bar names the tab).
- **Queue filter on Apple TV**: **Warteschlange filtern** above the titles, the same filter
  as on the iPhone (title, artists, album). Selecting it opens the keyboard; **Filter
  leeren** shows everything again. Rows keep their place in the queue, so the long-press
  menu edits the title that is shown; after removing one, focus moves to the next shown
  title. **Warteschlange leeren** still removes all upcoming titles, as **Nächste leeren**
  does on the iPhone, and **Rückgängig** brings them back.
- **More in the queue menu on Apple TV**: every title also offers **Zur Playlist
  hinzufügen** and **Zu Favoriten hinzufügen**/**Aus Favoriten entfernen** while online.
- **History and handoff on Apple TV**: **Optionen** also has the iPhone's **Hörverlauf &
  Übergabe** settings (local history, server sync, clearing, **Wiedergabe von anderem Gerät
  prüfen** and **Wiedergabe hier fortsetzen**); **Wiedergabe übergeben** is in the player's
  **Titel-Aktionen**.
- **Not on Apple TV**: the visualizer (there is no cover-overlay player on TV) and offline
  music (see WEB-PARITY.md for why tvOS has no offline catalogue).

Review on Apple TV with the remote: **Optionen → Klang & Wiedergabe**, set a 15-minute
sleep timer and watch the time left; set tempo 1.25× and listen; raise **Überblendung** to
6 s and let a title run out; turn on the equalizer, choose **Mehr Bass**, then
change one band and see **Eigener Klang**. In **Player**, open the sleep and tempo menus,
filter the queue for an artist and long-press a shown title to play it next.

## Apple TV and undo (build 36)

Added on top of build 35; it uses the server API of v1.10.x unchanged. It ships as 0.4.2 (36).

- **Für dich on Apple TV**: the first mix row on **Start** ends with an **Alle Mixe** card.
  It opens a grid of all mixes of the day, "Dein Mix 1…n" first, then the mixes from
  listening, each a focusable cover card with label, title and track count. Select opens
  the mix page; a long press offers **Abspielen** and **Zufällig abspielen**, as on Start.
  Loading, no mixes yet, a server without mixes and errors each have their own state;
  errors and the empty state offer **Erneut laden**.
- **Editing the queue on Apple TV**: in **Player**, a long press on an upcoming title
  offers **Als Nächstes abspielen**, **Nach oben**, **Nach unten** and **Aus
  Warteschlange entfernen**, only where they apply. The playing title and titles already
  played have no menu. Focus stays on the moved title, or on the title that took the
  removed one's place. **Warteschlange leeren** removes the upcoming titles (the current
  one keeps playing); **Als Playlist speichern** saves the queue (needs a connection).
- **Rückgängig after clearing the queue** (all platforms), like the web's toast: after
  **Leeren und stoppen** or **Nächste leeren** a small bar (iPhone/iPad at the bottom of
  the player, Mac at its top) or, on Apple TV, a focused button in the queue panel offers
  **Rückgängig** for ten seconds. It restores the removed titles with their sections and
  the started context; after a full clear the title returns at its position and plays
  only if it was playing. Starting or queuing something else ends the offer; similar
  titles that autoplay appended in the meantime make way for the restored ones.
- **Sorting a mix** (all platforms), as on the web's mix page: **Sortieren** offers the
  mix order, title, artist, album, duration or track number, ascending or descending.
  Playing and **Als Playlist speichern** follow the shown order. On iPhone, iPad and Mac
  it sits in the toolbar, on Apple TV above the titles.

Review on Apple TV with the remote: open **Start**, move to the end of **Deine Mixe** and
open **Alle Mixe**; move through the grid and long-press a mix to play it. In **Player**,
long-press upcoming titles to move them up and down, play one next and remove one, and
check where focus lands. Choose **Warteschlange leeren**, then **Rückgängig**. On a phone:
**Leeren und stoppen**, then **Rückgängig** within ten seconds; sort a mix by artist and
play it.

## Rest of the listening parity (build 35)

Added on top of build 34; it uses the server API of v1.10.x unchanged. It ships as 0.4.1 (35).

- **Autoplay like web 1.10.1**: each top-up appends at most `PlaybackQueue.autoplayBatch`
  (12) similar titles and leaves out titles already in the queue and the last 200 titles
  played in this session (`SessionPlayHistory`). The session belongs to one account on one
  server and starts over when either changes. When the lookahead (two titles left) is
  reached again, the next batch is requested. A song radio the user starts keeps its full
  list; the fallback radio after a queue that already ended is limited like a top-up.
- **Queue origins survive a restart**: the saved queue (same account and server binding as
  before) stores each entry's origin and the started context, so **Letzte Wiedergabe
  fortsetzen** shows the same sections and autoplay seeds from the right title. Queues saved
  by build 34 or earlier have no origins and restore as a plain context, as before. Offline,
  only titles with a local file return, each with its own origin.
- **Mix als Playlist speichern** on the mix page (iPhone, iPad, Mac, Apple TV): the existing
  playlist sheet with the heading "Mix als Playlist speichern" and the suggested name
  "{Mix} · dd.mm.yyyy", using the same create and bulk-add calls as saving the queue.
  Success shows a row that opens the new playlist; errors stay in the sheet. Without a
  connection the button is disabled and a note explains why. There is no mix menu
  elsewhere in the app, so nothing else gained the action.
- **Apple TV**: a **Start** tab (now the first tab) with Weiterhören, the day's mixes as
  focusable cover cards (long press plays), **Zuletzt gehört** and **Neu in der Mediathek**;
  the mix page has a large cover and a row of focusable actions. The player's queue panel
  shows the sections and the **Automatisch weiterspielen** switch.

Review on a phone: play a mix to its last two titles with autoplay on and check that
**Ähnliche Titel** grows by at most 12 and never repeats what you already heard; quit the
app, reopen, choose **Weiterhören** and check the sections are unchanged; save a mix as a
playlist and open it. On Apple TV: open **Start**, move through the shelves with the remote,
open a mix, play and shuffle it, and check the queue sections and the autoplay switch in
**Player**.
## Listening parity with web 1.10 (build 34)

Added in [PR #108](https://github.com/Der-Felix/ytmdl/pull/108); it uses the server API of
v1.10.0 unchanged. It ships as 0.4.0 (34).

- **Plays and skips** follow `frontend/src/lib/audio/listeningTracker.ts`: a play counts
  after min(30 s, half the duration ÷ tempo) of actual playing time, once per start.
  Moving on actively (next, previous to another title, choosing a queue entry, starting
  something else) after at least 2 s and before that threshold is a skip, unless the
  title stood paused for more than three minutes. Natural ends, failed titles and staying
  on the same title are never skips. Unlike the web, plays and skips of local files are
  queued and sent later, so offline listening shapes the mixes too. The local "recently
  played" list and automatic offline caching still record after one second.
- Events wait in the existing account- and origin-scoped queue (`ListeningHistory`) and
  keep their event ID across retries. A definite refusal of one event (for example 404
  from a server without skips) drops it instead of blocking later events.
- Each Start section loads on its own and never raises an alert. A server without mixes or
  history hides those sections; an unreachable server shows a hint to the saved music.
- In build 34 tvOS kept its tab layout without a Start screen and its queue panel had no sections; build 35 adds both (see above).

Review on a phone: open **Start** and check mixes, **Alle Mixe**, a mix page (play, shuffle,
queue), **Zuletzt gehört** and the artist page's **Am meisten gehört**. Play a title for
10 s and skip it, play another for more than 30 s, then check the web's mixes/history
reflect it after a day's recomputation. Repeat in airplane mode with saved music, then
go online and refresh: the queued events are sent once. In the player, queue a title with
**Als Nächstes** and an album with **Zur Warteschlange** and check the sections; with
**Automatisch weiterspielen** on, **Ähnliche Titel** appear when two titles remain.

## Player access review (build 26)

1. Start a track. The mini-player must remain above the tab bar on Start, Search, Library, Playlists and Settings, including opened playlists.
2. Tap its cover/title to open **Jetzt läuft**. Seeking and transport controls are visible; **Werkzeuge** opens EQ, transitions, timer, tempo and visualizer settings.
3. Close the player and change tabs. The queue is retained. Before starting music, the **Player** toolbar button opens the empty player instead of forcing playback.
4. The iPhone and iPad regression tests seed a paused queue using a loopback fixture, without creating an audio item or playing test sounds. Real codecs, AirPlay and background audio still need device review.

## Offline review on a phone

1. Sign in to your server. On a track's actions choose **Offline speichern**;
   an album/artist/favorites also has a download toolbar action. For playlists,
   choose **Playlist (…) → Offline speichern**.
2. Open **Start → Offline** (also reachable from Library or Settings). The
   **Sammlungen** view groups saved playlists, favorites, albums and artists. Wait
   until the card says all intended tracks are offline; open it to inspect titles
   and cached covers. Cover/lyrics sidecars arrive separately when available.
3. Verify the playlist's original order. Try **Sortierung** (title, artist, album,
   date) and search for an artist or a nonexistent title. Return to the overview
   and choose **Alle Titel** for individually saved/legacy downloads. The all-title
   sort preference persists. Refresh synchronization is optional while online;
   it downloads newly resolved members, preserving earlier local files.
4. Turn on airplane mode. Play a downloaded title, seek and skip between downloaded
   titles. Quit and reopen the app. From the connection screen choose
   **Offline-Musik öffnen**, then your saved account. This path requires no login
   request, keychain token or reachable server; it displays only local music.
5. Online again, sign in and refresh the library. Pending listening events use the
   same account and stable event IDs. Server sync and local recording can be disabled.
6. Removing a local copy affects only the device. Logout can optionally remove this
   account's offline music; otherwise audio and metadata remain locally accessible
   through the offline picker. This is explained in Settings.

## Bounds and limitations

- A collection action snapshots at most 500 tracks; a device stores at most 10,000
  records. Music budget is 1–128 GiB (8 by default), shared across saved accounts;
  individual audio files are capped at 256 MiB. Covers/metadata require additional
  space. Pinned downloads are never automatically evicted; played-cache eviction
  excludes current/prepared titles. Downloads beyond available budget fail visibly.
- Transfers are serialized and redirect-free. The iOS background daemon schedules
  work; force-quitting can interrupt it. Missing tasks become paused and require
  authenticated retry. Paused live tasks resume; lost/interrupted tasks start again,
  without persisting credential-bearing resume blobs. Wi-Fi settings apply to new
  transfers. Cached files take precedence over streaming, including next-track preload.
- Offline mode has local search/collection filters, playback and sound settings.
  Server playlist mutations, recommendations and device-transfer calls require online
  sign-in. It never presents a saved account as proof of current server authorization.
- Header/container checks reject HTML, unsupported containers, wrong origins and
  oversized files. They are not a decoder certification. Real codecs, background
  completion after suspension, interruption behavior and airplane-mode playback still
  need physical-device qualification. No library audio is re-encoded or downloaded
  from an external provider by the client.
- Adjacent album tracks skip crossfade; sample-perfect gapless playback is **not**
  promised. Loudness normalization uses the existing server analysis and cached offline
  measurements; missing or invalid measurements bypass normalization. EQ headroom is
  separate. Acoustic recommendations require separate analysis. Radio uses the existing
  server metadata-based implementation, not Plex's sonic database.
- Spotify's catalog, podcasts/audiobooks, social network, Jam, commercial AI DJs and
  proprietary Spotify Connect/Plex integrations are not part of this self-hosted client.
  CarPlay requires Apple's approved audio entitlement; Siri/Shortcuts, live
  playback widgets, Live Activities and Apple Watch remain separate work.

## Widgets and collection design review (build 30)

1. Open a playlist or favorites. The cover, title and readable duration share one
   header. **Abspielen** and **Zufall** have equal width and at least 52 points of
   height. At accessibility text sizes they stack vertically. Empty collections
   disable both actions. **Playlist (…)** groups download, edit, add, reload and
   confirmed deletion instead of scattering actions across the toolbar.
2. Add **YTMDL → Deine Musik** from the system's Home Screen widget gallery. The
   small widget opens the player; the medium widget also opens favorites/playlists.
   The app does not place widgets automatically. Add a YTMDL accessory from the
   Lock Screen widget gallery to open the player after the system unlocks.
3. Opening a widget never starts music. If signed out, authenticate in the app;
   the pending destination is then opened. Widgets have no server requests,
   session tokens, shared account state, or App Group. They are entry points,
   not current-track displays or replacement system Now Playing controls.
   In explicitly opened offline mode, collection shortcuts retain the local
   saved-music overview; the player shortcut opens the local player.
4. Review the actual system widget gallery, light/dark/tinted rendering and Lock
   Screen on a physical device. Compilation and app deep-link tests do not
   establish system widget appearance or placement on that device.

## Verification

All local verification must remain silent; do not run the audible AVPlayer audio
suite on the user's speakers. The full support target is compiled. CPU/state tests
cover queue/history isolation, LRC parsing and the handoff wire payload. Isolated
file tests cover reboot persistence, symlink/error-page rejection and account-safe
removal. A loopback-only test additionally downloads synthetic silent WAV bytes,
cover and lyrics, then inspects an AVPlayerItem local URL **without playback**.

Run the bounded loopback test with the provided fixture in a separate terminal:

```sh
python3 apple/Scripts/fixture-server.py --port 59583
YTMDL_OFFLINE_FIXTURE_URL=http://127.0.0.1:59583 swift test --package-path apple \
  --filter 'offline|timedLyrics|deviceHandoff|listeningQueue'
```

The iPhone UI test `testMobileHomeDownloadsAndSoundSettingsWithoutPlayback`
checks Home → album download → Offline → sound settings without playing.
Normal fixture transfers use an isolated ephemeral session and private temporary
store, never the production background session or user offline store.

Sources used for the design: [Apple background downloads](https://developer.apple.com/documentation/foundation/downloading-files-in-the-background),
[Apple CarPlay entitlements](https://developer.apple.com/documentation/carplay/requesting-carplay-entitlements),
[Spotify offline listening](https://support.spotify.com/ly-en/article/listen-offline/),
[Plexamp overview](https://www.plex.tv/en-gb/plexamp/),
[Plex sonic analysis](https://support.plex.tv/articles/sonic-analysis-music/).

## Web parity audit (build 32)

See [WEB-PARITY.md](WEB-PARITY.md) for the complete listening-feature inventory,
closed gaps and explicit remaining differences. Build 32 adds occurrence-safe queue
reordering, insert-next, mobile queue filter/save/clear, measured normalization, Mac
offline browsing and synchronized lyrics, and the missing 45-minute/1.75× choices.
Advanced web DSP, reversible shuffle and tvOS parity remain open; this preview is
not a complete web-parity or hardware-qualified stable release.
