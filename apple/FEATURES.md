# Native listening preview 0.2.0 — build 27

The native app is a player for an existing YTMDL library. Its version is independent
of the server version. This preview does not promise full Spotify/Plexamp parity.

See [AUDIT.md](AUDIT.md) for the build 27 functional audit, test evidence,
corrected defects and the remaining real-device release checks.

| Feature | Current implementation |
| --- | --- |
| iPhone/iPad Start | Recent albums, local recent listening, favorites, genre mixes, playlist shortcuts, resume last queue |
| Library/search | Existing library artists, albums, tracks, genre filter and authenticated artwork |
| Favorites/playlists | Native editing, ordering, membership, smart server rules and favorites |
| Offline music (iOS/iPadOS) | Track, album, artist, favorites and playlist snapshots; local search, collection filter, sorting and shuffle |
| Downloads | Background URLSession on real iOS, progress, pause/retry, Wi-Fi preference, music storage cap, remove local copies |
| Smart offline storage | Optional played-track cache; favorites and selected collections refresh when the library is refreshed |
| Player access | Native mini-player above the floating iPhone tab bar, retained inside pushed collections; direct Player toolbar entry; expandable player |
| Collection artwork | Up to four distinct album covers, track count and duration on iPhone collection headers |
| Mobile transport | Seeking, previous/next, shuffle, repeat queue/track, native device volume and AirPlay |
| System artwork | Now Playing artwork published after authenticated/local loading, retained across progress updates, cleared on title change or stop; actual lock-screen review remains required |
| Mobile sound | Ten-band EQ, presets, preamp/headroom, 0–12 second crossfade, adjacent-album protection, preload, fast start, speed, sleep timer |
| Mobile visualization | Four FFT-driven styles, cover overlay/no cover, preset or custom color, Reduce Motion support |
| Lyrics | Cached/plain text; LRC timing, optional following and tap-to-seek when time tags exist |
| Queue | Occurrence-based play/remove/play-next, clear upcoming, save local queue, restore explicitly |
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
   an album/artist/favorites/playlist also has a download toolbar action.
2. Open **Start → Offline** (also reachable from Library or Settings). Wait for
   **Offline verfügbar**. Cover/lyrics sidecars arrive separately when available.
3. Choose a saved collection and optionally enable refresh with Library updates.
   This downloads newly resolved members, preserving earlier local files.
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
  promised. True loudness normalization and acoustic recommendations require separate
  analysis; EQ headroom is not loudness normalization. Radio uses the existing
  server metadata-based implementation, not Plex's sonic database.
- Spotify's catalog, podcasts/audiobooks, social network, Jam, commercial AI DJs and
  proprietary Spotify Connect/Plex integrations are not part of this self-hosted client.
  CarPlay requires Apple's approved audio entitlement; Siri/Shortcuts, widgets,
  Live Activities and Apple Watch are separate work, not advertised as implemented.

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
