# Native listening preview 0.3.0 — build 35

The native app is a player for an existing YTMDL library. Its version is independent
of the server version. This preview does not promise full Spotify/Plexamp parity.

Builds 33 to 35 only fix defects and change how the app starts without a connection (see
[HANDOFF.md](HANDOFF.md)); the feature matrix below is the build 32 state.
See [AUDIT.md](AUDIT.md) for the dated functional audit through build 32, test evidence,
corrected defects and the remaining real-device release checks.
The [release checklist](RELEASE-CHECKLIST.md) tracks qualification separately
from implemented features and simulator verification.

| Feature | Current implementation |
| --- | --- |
| iPhone/iPad Start | Recent albums, local recent listening, favorites, genre mixes, playlist shortcuts, resume last queue |
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
| Mobile sound | Ten-band EQ, presets, preamp/headroom, measured loudness normalization, 0–12 second crossfade, adjacent-album protection, preload, fast start, speed, sleep timer |
| Mobile visualization | Four mobile FFT-driven styles, cover overlay/no cover, preset or custom color, Reduce Motion support |
| Lyrics | Cached/plain text; LRC timing, optional following and tap-to-seek when time tags exist |
| Queue | Occurrence-based play/remove/insert-next/reorder, filter, clear upcoming/all, save as playlist, local resume state and explicit restore |
| Song/genre radio | Existing local-library server recommendations; optional autoplay after the queue ends |
| Listening history | Local recent metadata and queue; optional idempotent server events, queued while offline, scoped to origin/account |
| Playback transfer | Explicit save/pause and fetch/accept via existing server handoff routes; no remote automatic playback |
| Appearance | Light/dark/system and six mobile accent palettes; cover-derived player background |
| Device login | Existing device-code approval; backend support/deployment required |

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
   titles. Quit and reopen the app. It reopens your server and, when the server cannot be
   reached, shows your downloaded music by itself ("Server nicht erreichbar"), with a
   **Erneut verbinden** button. It also retries when the network returns or the app comes back
   to the foreground, but only while nothing is loaded in the player. After you sign out on
   purpose the music does not open by itself: choose **Offline-Musik öffnen** on the connection
   screen (a single saved account opens directly). Offline music needs no login request,
   keychain token or reachable server; it displays only local music.
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
