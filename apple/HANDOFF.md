# Apple app handoff — preview 0.5.2, build 41

The Apple client arrived in `dev` with
[PR #42](https://github.com/Der-Felix/ytmdl/pull/42) (merged 7 October 2026).
Do not promote it to a stable server release just because it builds locally.
The server release and native preview have independent version numbers.

## Direct distribution without the App Store

Added after the preview itself, with the packaging scripts and workflow below.
[INSTALL.md](INSTALL.md) is the user and maintainer guide. No Apple developer account,
certificate, profile or secret is used anywhere; the project does not sign or notarize.

- `Scripts/package-ipa.sh`: unsigned iPhone/iPad `.ipa` (app plus widget) from an
  unsigned `xcodebuild archive`; it checks the widget, the absence of signing material
  and the packed version. `Scripts/package-dmg.sh`: ad-hoc signed `YTMDL.app` with the
  sandbox entitlements in a `.dmg`; it checks signature, entitlements and version.
  `Scripts/release-info.sh` reads the version from both Info.plist files (they must match),
  requires Xcode 27 and derives the tag `apple-v<version>-<build>`.
- `.github/workflows/apple-release.yml`: on a pushed `apple-v*` tag it builds both,
  re-verifies the checksums and creates a *draft pre-release* (`--latest=false`); a manual
  run only keeps a workflow artifact. The tag must equal the plists and sit on `origin/dev`
  or `origin/main`. Publishing the draft is a manual step.
- The server's update check reads GitHub releases. `apple-v…` is not SemVer, so
  `update.Eligible` ignores it on both channels (`TestAppleReleaseTagsAreNeverServerUpdates`);
  the development channel only looks at the 30 newest releases, so keep Apple releases rare.
- Apple TV has no download (tvOS cannot install from a file): build from source.

Verified locally with Xcode 27: both scripts run; the `.ipa` is 1.4 MB, arm64, unsigned,
with app and widget at 0.3.0 (33), minimum OS 27.0 and no machine paths; the `.dmg` mounts,
the app is ad-hoc signed with hardened runtime and sandbox plus network entitlements, no
team identifier, and Gatekeeper rejects it as documented. Not verified: the workflow has
never run on GitHub (a manually dispatched workflow is only listed once the file is on the
default branch; a tag triggers it from any commit), no sideloading tool was used to install
the `.ipa`, and the `.dmg` app was deliberately not launched here because it shares its
bundle identifier with the installed Mac preview.

## Build 41: iPhone and iPad library and collection pages (CI only, not installed)

Builds on build 40; see FEATURES.md "iPhone and iPad library, album, artist and playlist pages (build 41)".

- **New files:** `App/LibraryViews.swift` (`MobileLibraryView`, `MobileArtistListView`,
  `LibraryTracksView`, `LibraryGenresView`, `GenreAlbumsView`, and `ArtistDirectory`, the
  paged A–Z artist list held by `AppModel.artistDirectory`) and `App/CollectionPages.swift`
  (`CollectionHeader`, `CollectionActionRow`/`CollectionAction`, `CollectionMetrics`,
  `ReadableContentWidth`, `ShelfReleaseCard`, `TrackRowSkeleton`, `MonogramView`,
  `LibraryRoute`, `ReleaseDetailResponse`). Both are registered for the three app targets like
  `MobilePlayerViews.swift`. Core: `Sources/YTMDLCore/LibraryBrowsing.swift` (`GermanDates`,
  `LibraryAlbumSort`/`LibraryPreferences`, `ArtistAlbumSort`, `LibraryIndex`, `ArtistMonogram`,
  `DiscGroup`, `CollectionSummary`, release kinds) with `LibraryBrowsingTests`.
- **Models:** `Track.discNumber`, `Release.releaseType`/`albumArtist`, `Artist.releaseCount`
  (all optional; older servers and saved queues decode as before).
- **Routing:** on iOS `.library` and `.artists` show the new views; `LibraryRoute` adds Titel,
  Genres and one genre. `CollectionView` keeps its state and logic and gains `mobileCollection`
  (album, artist, playlist, favorites); Mac and TV keep `standardCollection`/`desktopCollection`.
- **Album order:** `AppModel.albumSort` (stored under `librarySort`) is sent to every
  `/library/releases` call; `setAlbumSort` reloads. Playlist previews: up to 48, requested per
  visible row (was 12 for the whole list).
- **TrackRow (iOS):** number or cover, playing indicator, swipe actions, context menu; the
  queue's labels. `PlayingIndicator` takes a colour. `ArtworkView` takes a monogram name.
- **Mix page (iOS):** uses `CollectionHeader` with **Einreihen** and **Als Playlist**.
- **Fixture:** release types and years, `GET /library/releases/{id}`, `artist_id` and `q`
  filters, three more artists (A, O and # sections).
- **Tests:** `testMobileLibrarySortPersistsAndFiltersWithoutPlayback`,
  `testMobileAlbumAndArtistPagesWithoutPlayback`, `testMobileArtistListTitlesAndGenresWithoutPlayback`,
  `testTabletLibraryAlbumAndArtistPagesWithoutPlayback`; the playlist tests use the header's
  offline tile instead of the "…" menu.

## Build 40: iPhone and iPad player and queue (CI only, not installed)

Builds on build 39; see FEATURES.md "iPhone and iPad player and queue (build 40)".

- **New file:** `App/MobilePlayerViews.swift`, registered for the three app targets like
  `HomeViews.swift`. It holds `MobileMiniPlayer`, `MobilePlayerView` and `MobileQueueView`
  (moved out of `MobileListeningViews.swift`), `PlayerMetrics` (the player's sizes), the
  scrubber, the mode buttons, the backdrop, `SystemVolumeSlider` and `PlayingIndicator`.
  `MobileLyricsView` gained `compact` for the lyrics in the cover's place.
- **Core:** `PlaybackQueue.setShuffle`/`isShuffled` (shuffle as a mode; a new queue starts
  unshuffled, restored queues too), and `Sources/YTMDLCore/PlayerControls.swift` with
  `RepeatMode`, `PlaybackClock`, `LibraryMatch` and `PlaybackQueue.listMove`, tested in
  `PlayerControlsTests`. `PlayerModel.setShuffle`, `repeatMode` and `setRepeat`; the Mac and TV
  keep the one-off `shuffle()`.
- **Links:** `NowPlayingView` and `MobilePlayerView` take `openCollection`; `RootView.playerLinks`
  closes the player and pushes the artist or album onto the shown tab (iPhone) or the detail
  path (iPad). Artists and albums are found by name in the loaded library, then by search.
- **Play state:** the player and mini player show `isPlaybackRequested` instead of
  `isPlaying`, so the button matches what `toggle()` does while a title loads or stalls.
- **iPad mini player:** `RootView.miniPlayer` uses `MobileMiniPlayer` on iOS (the TV branch is
  unchanged). Its identifier is `mobile-mini-player-open` there too.
- **Tests:** `testPlayerModesLyricsAndArtistLinkWithoutPlayback` (new) and
  `testQueueManagementAndNormalizationSettingsWithoutPlayback` (queue sheet: move, play next,
  shuffle, clear and undo, save). The large-text and tablet tests cover the queue sheet too.
  The large-text test allows 64 pt skip targets.

## Build 39: iPhone search, navigation and Start (CI only, not installed)

Builds on build 38; see FEATURES.md "iPhone search, navigation and Start (build 39)".

- **New files:** `App/SearchViews.swift` (`MobileSearchView`, iPhone and iPad; Mac and TV
  keep `SearchView` in `Views.swift`) and `App/HomeViews.swift` (`MobileHomeView`, moved out
  of `MobileListeningViews.swift`, plus `ListenMetrics`, the spacing/size tokens of both
  pages, and the shelf, card and tile views). Both are registered for the three app targets
  like `TVSoundSettings.swift`. Core: `Sources/YTMDLCore/SearchAndHome.swift` (`SearchMatch`
  top hit, `RecentSearches`, `HomeGreeting`) with `SearchAndHomeTests`.
- **Root:** the compact `TabView` uses the `Tab` API; Search is `Tab(role: .search)`; its field uses `.navigationBarDrawer(displayMode: .always)` because the automatic placement stayed hidden until pulled down (seen in the CI screenshots). The
  `open-mobile-player` toolbar item is gone; the mini player (`mobile-mini-player-open`)
  opens the player, also by swiping up. `mobileSelection` returns a re-chosen tab to its
  first page and raises `searchFocusRequest` for Search. `openGenre` sets the library's
  genre filter and switches to Bibliothek.
- **Feed:** `ListenFeed.frequent` (`/history?sort=frequent`) feeds "Am meisten gehört";
  its failure only hides the shelf. `ContinueListeningCard` was removed (unused).
- **Recent searches** live in UserDefaults under `recentSearches.<server>|<user id>`.
- **CI:** `apple-ui` uploads the attached screenshots of every run as `apple-ui-screens-<device>`.
- UI tests: `testMobileSearchGroupsResultsRemembersSearchesAndBrowsesGenres`; Start tests
  look for `home-quick-favorites` instead of the old hero text.

## Build 37: Apple TV sound settings and queue filter (CI only, not installed)

Builds on PR #112 (build 36); see FEATURES.md "Apple TV sound and queue filter (build 37)".

- **Sound on TV:** `App/TVSoundSettings.swift` (`#if os(tvOS)`, registered for all three
  app targets like `TVListenViews.swift`). `TVSoundSettings` is a Form opened from
  **Optionen → Klang** (`SettingsView`, `tv-settings-sound`); it calls the same
  `PlayerModel`/`EqualizerModel` setters as `MobileAudioSettings`, so the preference keys
  (`crossfadeSeconds`, `playbackRate`, `playerNormalization`, `eq*` …) are shared. Sleep
  mode stays session state, as everywhere. Every value is a `TVChoiceRow` (a NavigationLink with title, current value as accessibility value and the identifier, pushing `TVChoiceList`, one Button per choice with a checkmark; choosing dismisses). In CI, −/+ buttons in a Form row never took focus, a pushed `Picker` showed an empty list and a `.menu` Picker hid its title, so the page uses no Picker;
  its choices come from `SoundStep.options(around:step:in:)` in core (the step grid plus an
  off-grid current value). `TVCountdown` shows the time left of a minute timer. Checked for tvOS: the
  `MTAudioProcessingTap` in `Equalizer.swift`, `AVAudioMix` and `audioTimePitchAlgorithm`
  have no platform guard and are available on tvOS, so EQ, normalization, crossfade and
  tempo use the same engine path; nothing was left out for platform reasons.
- **Player on TV:** `NowPlayingView` hides the navigation bar on tvOS (the large "Player"
  title overlapped the scrolling queue panel), cycles repeat like the iPhone and shows
  `TVPlayerQuickActions` (sleep timer and tempo menus with a `Picker` inside, title actions
  with song radio, add to playlist and handoff).
- **Queue filter on TV:** `TVQueueView` filters with `PlaybackQueue.rows(filter:)` and
  `Track.matchesQueueFilter` (core, now also used by the iPhone queue). Rows keep their queue
  positions; removal focuses `PlaybackQueue.focusAfterRemoving(_:visible:)`. "Als Nächstes"
  now focuses the moved entry. Every row's menu also offers add to playlist and favorite.
- **Settings on TV:** a **Hörverlauf & Übergabe** section mirrors the iPhone's history and
  handoff settings.
- **Offline on TV:** evaluated and not built; the reasons are in WEB-PARITY.md (build 37).
- UI tests: `testTVSoundSettingsShowAChangedCrossfade`, `testTVQueueFilterEditsTheShownEntry`.

## Build 36: the Apple TV side (CI only, not installed)

Builds on PR #111 (build 35); see FEATURES.md "Apple TV and undo (build 36)".

- **Für dich on TV:** `TVForYouView` in `App/TVListenViews.swift` (a grid of `TVMixCard`,
  genre mixes first via `PersonalMix.ordered`, loading/empty/unsupported/error states with
  retry). `RootView.routedContent` maps `ForYouRoute` to it on tvOS. The Start tab ends
  the first mix row with an **Alle Mixe** card (`tv-all-mixes`). `TVMixCard` replaces the
  Start tab's private card, so both places share the long-press menu.
- **Queue editing on TV:** `TVQueueView` has a long-press menu on upcoming rows (play next,
  up, down, remove), **Warteschlange leeren** (upcoming entries, like "Nächste leeren") and
  **Als Playlist speichern**. The rules live in core: `PlaybackQueue.isUpcoming`,
  `canMoveUp`, `canMoveDown`, `canPlayNext`, `focusAfterRemoving`. Focus is set through a
  `@FocusState` after a short delay so the closing menu does not reset it.
- **Undo after clearing:** `QueueClearUndo` (core) keeps the queue before and after;
  `restore(from:)` returns the old queue only while nothing changed since, ignoring
  autoplay entries appended in between. `PlayerModel.clearQueue`/`clearUpcoming` set
  `undoOffer` for `undoSeconds` (10); `undoClear()` restores, and after a full clear
  reloads the title at its position (playing only if it played). `stop()` drops the
  offer, so signing out never restores an old queue. `QueueUndoBar` (iPhone/iPad player,
  Mac player at the top) and a focusable **Rückgängig** in the TV queue show it.
- **Mix sorting:** `MixTrackSort` (core) mirrors the web mix page's columns; `Track`
  decodes optional `album_artist`/`track_number`, so older saved queues still decode.
- UI tests: `testTVForYouOpensFromStartAndFocusesAMix`, `testTVQueueRemovesUpcomingEntryFromMenu`.

## Build 35: rest of the listening parity (CI only, not installed)

Builds on PR #108 (build 34); see FEATURES.md "Rest of the listening parity (build 35)".

- **Autoplay batches:** `PlaybackQueue.appendAutoplay(_:excluding:batch:)` appends at most
  `autoplayBatch` (12) titles and skips queued IDs plus `PlayerModel`'s `SessionPlayHistory`
  (last 200 current titles, recorded in `loadCurrent`/`promotePrepared`). `AppModel`
  scopes the session to server + account (`scopeListeningSession`) at every sign-in,
  sign-out, server change and offline opening. `continueRadio` is limited the same way.
- **Restored sections:** `ListeningSnapshot` gained optional `origins` and `context`; old
  snapshots decode with both nil. `restorable(keeping:)` filters offline titles together
  with their origins; `PlayerModel.play(..., origins:)` hands them to `PlaybackQueue.replace`.
- **Mix als Playlist speichern:** `MixDetailView` opens `AddToPlaylistSheet` with a heading,
  suggested name (`PersonalMix.playlistName`) and a `saved` callback; disabled offline.
- **Apple TV:** `App/TVListenViews.swift` (`#if os(tvOS)`, registered for all three app
  targets like the other App files) holds `TVHomeView` (new first **Start** tab, now the
  default tab) and `TVQueueView` (sections + autoplay switch in the player's queue panel).
  The mix page uses `MixDetailView` with a tvOS header.
- UI tests: `testListeningHomeOpensMixAndStartsItsQueue` (iPhone) and
  `testTVHomeShowsFocusableMixes` (Apple TV).

## Build 33: review fixes (committed, not installed)

Defect fixes from a code review of the branch; no new features and no audio-runtime
rewrite. App and widget are now 0.3.0 (33); the physical iPhone still has build 32.

- **Session loss:** `AppModel.report` treats a 401 on a signed-in online session as
  sign-out (cookies and Keychain entry dropped, offline detached, playback stopped)
  instead of raising an alert on every request.
- **Device sign-in:** polling lives in `AppModel.completeDeviceSignIn`. A timeout,
  dropped connection or 5xx is retried at the next interval; a definite server answer
  (expired code, unsupported server) ends it. `ConfirmDeviceView` keeps its approval message.
- **Offline library:** an unreadable `manifest.json` is moved aside as
  `manifest.unreadable-<uuid>.json` instead of being overwritten by the next save, and
  transfers still start. Record lookups use a lazily rebuilt index, `clearCurrent` and
  `detach` write the manifest once, and offline cover art is decoded once, off the main
  thread, at 512 px and cached (`OfflineArtworkImage` in `Views.swift`).
- **Collections:** `downloadCollection` reads one page past 500, so the "first 500"
  notice appears only when tracks were cut; the favorites sync in `loadLibrary` never
  shows it (`announceLimit: false`).
- **Decoding:** `Track` and `Release` accept `"artists": null` or a missing key as empty.
- **Privacy docs:** listening-history sync is on by default; SECURITY-DESIGN.md now says so.
- **Fixture server:** `/auth/device/poll` answers pending, slow_down, authorized (with
  cookies). No UI test uses it yet.

The same review fixed the backend (`auth/devices.go`: refused starts keep the caller's
quota, a full admission table evicts instead of refusing, `PollDevice` no longer holds
its lock during database calls) and the web (`Playlists.tsx` fetches each preview once,
`PlaylistDetail.tsx` disables the remove confirmation while busy). Those files belong to
[PR #47](https://github.com/Der-Felix/ytmdl/pull/47) (merged into `dev` before #42).

Verification (7 October, local, Xcode 27, serial, silent): the Swift package tests ran
without failure (7 new tests; the two opt-in audible tests stay disabled), and the iOS
Debug, tvOS Release and macOS Release targets build unsigned. Not run: iPhone, iPad or
tvOS simulator UI tests, a physical-device install or playback, final-commit CI.
Open: a first-run notice for history sync.

## 7 October: integration and UI-test repair

The branch now integrates current `dev`, including the recent download and
availability fixes. Backend device-code sign-in and web playlist controls are
also delivered separately in [PR #47](https://github.com/Der-Felix/ytmdl/pull/47).
Integrate that server/web delivery before shipping TV login. The API contract,
expiry, revocation and single-process limitation are documented in
[`docs/features/device-sign-in.md`](../docs/features/device-sign-in.md).

The failed iPhone UI regression was toggling persisted normalization settings
without reliably identifying the enabled state. It now checks the rendered
recheck action and still requires the measured current-track gain. The TV
regression now waits for catalogue restoration, explicitly establishes tab-bar
focus and starts the paused silent fixture in the foreground; the real clock
progress and Pause assertions remain intact. These are test orchestration
repairs, not evidence of a runtime audio fix.

Local verification: the targeted iPhone normalization flow and both TV test
cases pass. Serial Swift tests include the authenticated silent offline transfer;
only the two opt-in audible tests are skipped. Final-head CI is a separate gate.
App/widget remain 0.3.0 (32); no device installation, production mutation,
stable promotion or deployment was performed for this integration.

## Build 32: listening parity audit

[WEB-PARITY.md](WEB-PARITY.md) is the authoritative inventory, including remaining
web-only DSP/sort/shuffle features and hardware/tvOS qualification gaps. This is
still a preview, not a declaration of complete parity.

`PlaybackQueue.move` and `insertNext` preserve queue occurrences/current selection.
`PlayerModel` invalidates prefetch and persists explicit queue edits; empty forced
snapshots now clear stale resume state. Mobile queue tools expose filtering,
playlist save and full clear. Existing playlist/server operations remain online-only.

Normalization uses the existing loudness API with CSRF, finite/range/peak validation,
generation/client checks and bounded session/parameter caches. `Equalizer.swift` gives each
item its own atomic `LoudnessParameters`, independent during crossfade and active
without EQ. `OfflineTrack.loudnessDB` is an optional, account-scoped field; old
manifests remain readable. Missing measurements bypass processing with a status.
There is no audio rewrite or database migration. Mac now shares offline browsing,
collection headers and LRC lyrics with iOS; tvOS still has no offline catalog.

Keep the app and widget on the same version (now 0.5.2 (41)). Use Xcode 27; an older selected Xcode cannot
build the native targets. Keep SwiftPM build output outside iCloud/worktree paths
to avoid Finder resource-fork signing errors. Run Swift tests serially and keep
audible-test opt-in disabled. The fixture server supports deterministic loudness
responses as well as its existing silent WAV/covers.

Verification for build 32: 50 Core/Support tests pass in one serial run, with
only the two opt-in audible tests skipped. Four targeted iPhone simulator flows
pass: playlist mutations, favorites/search, offline playlist navigation and queue
management/normalization. Tests use isolated metadata, account scopes and silent
synthetic transfers. This does not qualify real background downloads or audio
routes; use the parity inventory and release checklist for the remaining work.
iOS simulator, macOS and tvOS simulator Debug builds and the signed iOS Debug
build pass under Xcode 27. Build 32 is installed as an in-place physical iPhone
update; no launch or playback was triggered. Final-commit GitHub CI is separate.

## Build 31: offline library browsing

`OfflineLibraryView` in `App/MobileListeningViews.swift` opens on visible local
collections: playlists, favorites, albums and artists, with cached cover collages,
ready/total counts and collection drilldowns. “Alle Titel” retains legacy and
individually downloaded music. A playlist opens in saved membership order;
`OfflineCatalog` in `App/OfflineLibrary.swift` handles local search and deterministic
artist/album/title/date sorting with ID tie-breaks. All-title sorting is remembered.
A persistent mini-player in the offline root keeps transport reachable while browsing.

The manifest format and audio files are unchanged. No collection is inferred from
individual downloads: an existing playlist must have been saved as a collection.
If an older download has no collection metadata, use “Alle Titel”; when online,
choose the playlist's “Offline speichern” action to save its membership. Already
ready tracks are reused. No automatic server requests run in offline browsing.
Local-file removal also affects other collections sharing that file; confirmation
now explains this, while saved membership and server playlists remain intact.

Verification (7 October): signed iOS Debug build and two silent iPhone simulator
flows pass, including playlist download, logout, local-only entry, cover preview,
original order, alphabetical sorting and no-match search. Offline tests cover
restart, account isolation, shared membership and old manifests without collections.
The Swift tests were also run individually: 44 pass, three optional fixture tests
are skipped. An initial concurrent run exposed existing shared fixture/Now Playing
interference; both affected cases pass in isolation. Do not treat that concurrent
run as green. Physical-phone installation/review is pending: the paired iPhone
was unavailable during this change.

## Build 30: mobile collection design and widgets

`MobileCollectionHeader` in `App/MobileListeningViews.swift` owns the iOS cover,
title/metadata and play/shuffle layout. `CollectionView` in `App/Views.swift` uses
it as one list row, removes the duplicate large navigation heading and groups
playlist download/edit/add/reload/delete in one toolbar menu. Regular buttons
are equal width with 52-point minimum height; accessibility sizes stack them.
The existing mini-player regression verifies button size, menu access and
navigation without starting playback.

`Widgets/MusicWidgets.swift` and `Widgets/Info.plist` define `YTMDLWidgets.appex`,
embedded only in `YTMDL-iOS`. It supports small/medium Home widgets and three
Lock Screen accessory families. These are static navigation shortcuts, not live
Now Playing widgets. The extension has no API client, credentials, network
requests or shared container. `Resources/Info.plist` registers `ytmdl-player`;
`Sources/YTMDLCore/MusicWidgetRoute.swift` whitelists only player/favorites/playlists
with no query, fragment, user info, port or extra path. The root retains a
pending route until sign-in, dismisses the player before a different route,
and never starts audio. Parser rejection has a core unit test.

Keep app/extension version and signing identifiers aligned. Add core files to
the package as usual; widget source belongs only to the extension, not the app
or support target. The app and extension compile together through the existing
iOS scheme. A build does not verify Home/Lock Screen placement or tinted widget
rendering on hardware; these remain explicit review steps in FEATURES.md.

## Build 29: stability candidate

Playlist and artist selection buttons accept taps across the whole row, including
its empty space. Manual playlist creation, adding two titles, persistent ordering,
removal, rename and confirmed deletion are covered by an iPhone UI regression.
Repeated tab switching retains access to the paused mini-player.
The fixed-height system mini-player limits only its compact metadata scaling;
full metadata remains scaled in the expanded view. Symbols retain bounded sizes
and 44-point hit areas, including secondary transport in the expanded player.
Accessibility text switches Home shortcuts/mixes to one
column and removes decorative hero art so it cannot squeeze the heading.

Player seeks requested before AVPlayerItem readiness are retained and applied
after loading. Next/previous preserve pause, and a paused queue boundary cannot
start radio. A playback activity revision prevents delayed mix/radio results or
handoff acknowledgements from replacing or pausing a newer playback decision.
Delayed catalog snapshots cannot undo acknowledged favorite mutations.
Logout invalidates unfinished offline transfer attempts; a completion already
queued by URLSession cannot publish a cancelled download afterward. Ordinary
user pause remains distinct and can accept a completed transfer.

Server form results are tied to the checked origin and HTTP consent. Account
changes close expanded player presentation. Offline track menus disable
operations that require a server session. The privacy manifest also declares
app-container file metadata access, reason C617.1.

Use [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) before stable promotion.
This candidate does not establish physical-device route/background qualification,
distribution signing or Release connectivity to an HTTP-only server.

## Build 28: calmer mobile collections

Start, Library and Playlists use consistent neutral surfaces, a single full-width
primary action and large labeled collection shortcuts. Native tab navigation and
the anchored player remain available. Refresh uses pull-to-refresh; Playlist
sorting/refresh live in one labeled options menu instead of several toolbar icons.
Playlists have an inline filter, cover previews and readable hour/minute durations.

`MobileActionStyle`, `MobileLibraryShortcuts` and `MobilePlaylistRow` live in
`App/MobileListeningViews.swift`; the mobile playlist overview is in `App/Views.swift`.
`AppModel.loadPlaylistPreviews` shares at most twelve previews per playlist revision,
loads each batch serially, retains up to four distinct covers and clears on account changes.
An unavailable preview uses a placeholder; it must never prevent opening/editing a
playlist. The existing API response-size limit still applies.

Review Start, Library and Playlists on the phone, filter a playlist, clear the
filter, open it, create/edit a playlist and return through the tab bar. No playback
is triggered by navigation, artwork previews, filters or refreshing.

Design reference: [Apple buttons](https://developer.apple.com/design/human-interface-guidelines/buttons)
(minimum 44-point hit regions and consistent control sizing).

## Code map

| Area | Files |
| --- | --- |
| Schemes, targets, minimum OS and signing | `YTMDL.xcodeproj`, `Resources/Info.plist`, `Resources/Mac.entitlements` |
| App lifecycle and media shortcuts | `App/YTMDLApp.swift` |
| Library, search, authentication, favorites | `App/AppModel.swift`, `App/APIClient.swift` |
| Mobile player, offline views and sound controls | `App/MobileListeningViews.swift` |
| iPhone/iPad Start and search, layout tokens | `App/HomeViews.swift`, `App/SearchViews.swift` |
| Scoped offline files/background transfer | `App/OfflineLibrary.swift` |
| Timed lyric parser and playback transfer wire models | `Sources/YTMDLCore/ListeningFeatures.swift` |
| Sessions, keychain and logout | `App/SessionVault.swift` |
| Navigation, shared iOS/iPadOS/tvOS views, authenticated artwork | `App/Views.swift` |
| Mac player, queue actions and compact playback options | `App/DesktopViews.swift` |
| Mac Start feed and appearance | `App/DesktopHomeView.swift`, `App/Appearance.swift`, `App/ListeningHistory.swift` |
| Listening home, mixes, mix page (shared) | `App/ListenHome.swift` |
| Apple TV Start tab, Für dich, queue sections, editing and filter | `App/TVListenViews.swift` |
| Apple TV sound settings and player quick actions | `App/TVSoundSettings.swift` |
| Streaming, prefetch, timers, two-player crossfade, metering | `App/PlayerModel.swift` |
| Audio processing and ten-band EQ | `App/Equalizer.swift`, `App/AudioSettingsViews.swift` |
| Cover-derived colors | `App/ArtworkPalette.swift` |
| Native playlist editing, library track selection and smart rules | `App/PlaylistViews.swift` |
| Settings | `App/SettingsViews.swift` |
| API models, origin policy, bounded queue | `Sources/YTMDLCore/` |
| Isolated audio/network/queue regression tests | `Tests/` |
| Synthetic UI server and simulator checks | `Scripts/fixture-server.py`, `UITests/PlayerUITests.swift` |

All app source files are shared with the Swift support test target. If adding
files, ensure both the Xcode project and package include them.

## Build 27: functional audit

[AUDIT.md](AUDIT.md) records each mobile feature's actual verification level,
confirmed corrections and pending physical-device review. Keep this distinction
when extending the preview; a build or fixture result is not an audio-route qualification.
`Tests/YTMDLAppleSupportTests/AuditTests.swift` exercises lock-screen artwork,
delayed account/logout replies and serialized favorites without creating audio items.
The offline integration test also checks local Now Playing artwork without a login
request or playback. `MPMediaItemArtwork` is cached per image, preserved across
progress updates and cleared before the next title's asynchronous image loads.
The callback captures immutable CGImage data rather than main-actor player state.

The iPhone UI suite uses stateful playlist/favorite fixtures and optional
`--audit-failures` for a recoverable collection error. The codec test requires
`YTMDL_CODEC_FIXTURE_URL`; it fully decodes synthetic silent files with AVAssetReader,
without starting AVPlayer. Do not replace them with library media or audible signals.
The expanded mobile transport has `mobile-player-toggle`: a generic first match
for Pause can select the mini-player beneath a sheet and produce a false UI failure.

## Build 26: reachable mobile player

The compact iPhone TabView owns `tabViewBottomAccessory`; do not put a bottom
safe-area inset inside each tab's NavigationStack because floating tabs can cover it.
`MobileMiniPlayer` keeps cover/title, play/pause and next controls reachable in
pushed playlist views. The toolbar also opens the player before any track starts.
The collection header includes a bounded four-cover collage and duration.
`testMiniPlayerRemainsReachableInPlaylistWithoutPlayback` tests tab switching,
pushed playlists and expansion through a paused loopback-only Debug fixture.
The fixture does not create an AVPlayerItem or start audio. Preserve this property.

## Mobile preview 0.2.0

Build 25 added the iPhone/iPad listening feature set documented in
[FEATURES.md](FEATURES.md), including download/restart review steps and explicit
limits. Preserve the existing bundle ID, keychain and preferences during updates.
The background session registers during launch and delegates system wake completion
through `OfflineAppDelegate`. Test fixtures use a distinct temporary store and an
ephemeral transfer session. No task description or manifest contains credentials.

## Build and review

Use Xcode 27, OS 27 SDKs and Swift 6.4. If multiple Xcodes are installed, set
`DEVELOPER_DIR` for each command; do not change global toolchain selection.
Build commands, fixtures, device requirements and codec limits are in
[README.md](README.md). Authentication, local HTTP consent, privacy and Apple
review requirements are in [SECURITY-DESIGN.md](SECURITY-DESIGN.md).

Build 24 binds split-view detail navigation to an explicit NavigationPath.
Albums, artists, playlists and home cards use value-based collection routes;
the selection handler clears the path on section changes or repeated clicks.
Opened album, artist and playlist drilldowns cannot conceal another section's
root. Clicking the already selected Mac sidebar item returns to its overview.
Home shortcuts, mini-player, search and player Escape use the same selection
handler. Search focus is cleared when leaving search and typing does not recreate
its stack per keystroke. Focus is requested after the new search field appears.
Navigation replacement retains the shared AppModel,
PlayerModel, queue and authentication state. Session changes discard drilldowns.
The iPad-only silent UI regression opens an album and playlist, switches sections
and verifies the library overview returns without starting playback.

The Mac layout uses a unified listening card
and a right-hand queue/options area. Artwork is capped at 520 points, with
transport near the bottom and flexible space beneath metadata. Known library
duration takes precedence over unreliable stream estimates for seeking and
crossfade; missing metadata falls back to valid AVPlayer duration. The cover stays bounded; seeking, volume,
AirPlay and big transport controls sit beneath it. Queue menus address
occurrences by index, preserve the playing occurrence and invalidate prefetch
when order changes. Removing from the queue never deletes media.

Queue, lyrics, playback and sound share one right-hand surface with four
underlined tabs: **Warteschlange**, **Lyrics**, **Wiedergabe**, **Klang**. Playback
options are in their own tab, so other content uses the full panel height.
Tabs scroll horizontally when needed and follow the selected tab. Transparent
queue rows reduce competing surfaces; only the playing occurrence is tinted.
Playback options use matching menu rows for EQ, timer and speed. Visualizer
choices and transition protection live in named popovers with aligned fields.
EQ headroom remains reachable in the EQ menu and detailed sound settings.
Shorter windows scroll. Player overlays suppress the underlying toolbar's Escape shortcut
so their close action runs first; a later Escape returns to the library. Sidebar and sign-in use the original repository logo,
and Mac/iOS icons are generated from it by `Scripts/generate-icons.swift`.

Mac favorites and playlist details now use a collection header with cover
preview/collage from already loaded tracks, play/shuffle and local filtering and
sorting. The playlist index uses adaptive cards with a name filter and clear
open action, without extra requests to fetch individual playlists. Long names
reserve three title lines so cards remain aligned. Mac track rows expose favorite
and enqueue controls directly; a labeled **Aktionen** menu replaces the ellipsis.
Favorite mutations stay explicit and are blocked while that row is pending;
removing a loaded favorite adjusts the offset for the next 100-track page.
Loaded totals are labeled when more pages exist. Filtered empty lists disable
transport actions; sorting/filtering do not change stored playlist order or
remove library media. Shared non-Mac list layouts remain available.

Playlist management is native on all three targets: create, rename, describe,
delete with confirmation, add/remove titles and persist order. Track action menus
in collections/search and Mac player/queue offer **Zur Playlist hinzufügen**;
the Mac current-title menu can save its queue as a manual playlist. Library
selection adds up to 100 tracks; queue additions support up to 500 unique tracks
in sequential server batches of 100. Already acknowledged batches remain visible
if a later request fails; errors explain partial changes without deleting music.
Metadata and rules are separate server writes; a rule failure preserves and
reports the successful metadata edit. Reordering is enabled only in the original,
unfiltered order. Deleting a playlist removes its collection, not library media.

Smart playlists use existing server rules: genre, artist, favorites, added-days,
recent/title/frequent/last-played ordering and 1–500 titles. Presets provide a
starting point; all criteria combine with AND. They resolve when opened, and
manual membership changes are disabled. Turning rules off restores earlier
manual membership rather than materializing the current smart result. Listening
sorts use server history, not the Mac's local recent-listening cache. Artist search
is not limited to the first loaded library page. No recommendation model or new
backend migration is introduced. Session/origin/CSRF guards remain shared;
stale catalog reloads cannot overwrite newer playlist mutations.

The optional Visualizer now uses a Hann-windowed 2048-point real FFT (Accelerate)
and 32 logarithmic bands rather than RMS-history graphics. **Spiegel-Spektrum** draws
mirrored bars and falling peaks; **Säulen**, **Orbit**, **Ringe**, **Lichtpunkte**
and **Frequenzband** provide five other frequency-based renderers. Rings summarize
four frequency ranges; the envelope is a frequency display, not a PCM waveform.
The player supports placement beneath the cover, a cover overlay with adjustable
opacity, or a cover-free view over the music-color background. Intensity, peak
markers and colors are device-local settings, shared between player options
and detailed playback settings. Color modes include cover, theme, Aurora,
sunset, ocean, neon and custom. Custom RGB color pickers support a solid color
or two-color gradient; all six renderers and the expanded view use the selected
palette. These preferences do not recolor the rest of the player. Unknown or
absent `playerVisualizerColorMode` respects the old `playerVisualizerCoverColors`
boolean. RGB values persist as validated six-digit strings; invalid values
fall back to defaults. Custom color and gradient choices remain saved when
another palette is selected. Turning visualization off restores
the ordinary cover. Existing bars/curve preference values remain compatible. Main-actor attack,
release and peak decay run near 30 Hz. The large live view closes with Escape
while playback continues. Style, placement and enabled state persist; default/style
migration retain existing preferences. Render callback storage/setup is allocated
before rendering, analysis is activation-gated, and disabling both EQ and
visualization removes the processing tap. EQ bypass leaves samples unchanged.
The first decoded channel is analyzed, including AVPlayer gain. During crossfade
callbacks from either title feed the shared display, not a final mixed-output FFT.
EQ headroom is not title loudness normalization. Reference links are in README.

Local verification must remain silent: the default suite uses zero-valued PCM
for actual AVPlayer transport checks and silent loopback audio for UI checks.
Two signal-dependent tap/meter tests require explicit `YTMDL_AUDIBLE_AUDIO_TESTS=1`
and are skipped by default. Never enable them through local speakers during
unattended verification. Stateless `SpectrumCanvas` allows
rendering checks with CPU-analyzed samples, without scheduling audio playback.

Review wide/narrow windows, long titles, light/dark themes, queue filtering with
removal and duplicates, next-track order, mute/volume, pause, seeking, changing
EQ, crossfade and timers. Use fixtures for automated checks. Do not sign in to
production or mutate its data incidentally during verification.

## Open work

- Measure real device/network startup latency; fixture timings do not establish
  live-server performance. Check actual media formats on physical devices.
- Real per-title loudness normalization needs reliable loudness metadata or
  analysis and bounded processing. The current UI does not pretend it exists.
- Physical-device background completion, offline codec playback and live handoff
  still require qualification. Automatic discovery, sample-perfect gapless, loudness
  analysis, CarPlay entitlement, Siri/widgets and third-party services remain open.
- TV device-code backend routes exist in this branch but require reviewed backend
  integration and deployment before use on an older server. Code approval is
  explicit; approving an admin account grants that account's permissions.
- Confirm final-commit CI, physical iPhone/iPad/TV behavior and accessibility.
  Distribution signing, final TV artwork, privacy disclosures, TestFlight and
  App Store review are separate unfinished steps.

Preserve the bundle identifier, app preferences and keychain when replacing a
local preview. Keep a rollback app bundle, stage and verify signing before
stopping the installed app, then reopen the replacement. Machine paths, private
review captures and local server addresses belong in the local handoff, not in
public repository files.
