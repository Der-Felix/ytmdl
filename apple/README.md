# YTMDL for Apple

Native SwiftUI listening client for iPhone, iPad, Mac and Apple TV. The first
review build is **0.1.0** and requires OS 27 and Xcode 27. It is a client of the
existing YTMDL API, not a second downloader or a replacement database.

## Open and run

Open `apple/YTMDL.xcodeproj` in Xcode 27. Choose `YTMDL-iOS`,
`YTMDL-macOS`, or `YTMDL-tvOS`, select your destination and run. The iOS target
supports both iPhone and iPad. Choose your own Apple development team and a
unique bundle identifier before installing on physical devices. A simulator
build needs no paid developer membership. Nothing is submitted to TestFlight
or the App Store by these commands.

Enter your server's **HTTPS origin**, then sign in using an existing account.
The native client already works with the v1.2.0 library/authentication API.
Device-code login additionally requires the new device routes in this branch.
No server data migration is needed. The server version is not changed by
building the app. Use the root repository workflow for backend integration and
deployment; pending code requests are invalidated by a backend restart.

Debug builds offer an explicit local HTTP switch for literal RFC1918 IPv4 or
loopback addresses. This transmits credentials and music without encryption;
use it only for an intentional local development test. HTTP consent is not
remembered. Release builds reject HTTP, regardless of the switch's state.
The app does not disable certificate verification or enable arbitrary ATS loads.

## Listening and layout

- Compact iPhone tab navigation, expandable player and persistent mini-player.
- iPad/Mac sidebar navigation and adaptive album grids. Mac uses a compact
  library header, artist cards and a desktop transport bar with seeking. Its
  player keeps the queue/lyrics independently scrollable in wide windows and
  stacks the panes in narrow windows; Escape returns to the library.
- Apple TV native tabs/focus, readable covers, remote-operated transport and seek.
- Local library search, artists, albums, genre filtering, favorites and playlists.
- Bounded paginated library reads, queue up to 500 tracks, shuffle upcoming
  songs, repeat queue, seeking, lyrics, explicit retry and format error messages.
- Native audio-session/background configuration, system media controls and
  AirPlay output selection on iPhone/iPad/Mac. TV uses system output controls.
- Administration stays in the existing website, accessed through Settings.

Audio is streamed in its existing container/codec. No transcoding, library
rewrites, external-provider requests or automatic download retries occur.
Actual format support must be checked on each target device. OS 27 simulator
UI tests play an authenticated synthetic Opus/Ogg stream with advancing time.
A local Mac asset check opens Opus/Ogg and AAC/M4A, but rejects Opus/WebM.
These checks do not replace playback checks on physical devices with actual
library media. Unsupported media is
reported rather than silently skipped. Crossfade, native offline storage,
automatic device discovery, timed lyric highlighting and seamless handoff are
follow-up work, not advertised as implemented in 0.1.0.

## Apple TV code login

After connecting the TV to your server, it requests an eight-character code
and QR code. On an already signed-in device open native **Settings → Anderes
Gerät anmelden**, or web **Profile & Security → Apple TV & Geräte anmelden**.
The QR links to `/profile?device_code=…` on the same server. It never approves
automatically. First inspect the device name, then explicitly confirm.

Only approve a code requested on your own device. Confirmation grants an
ordinary session with your account's permissions, including administrator
permissions if you approve as an administrator. The app presents listening
controls, but this is not a new server-side reduced-privilege role. Revoke the
device session through the existing profile session list when needed.

## Security and Apple guidance

See [SECURITY-DESIGN.md](SECURITY-DESIGN.md) for credential handling, threat model,
privacy and the App Store review requirements. This is a review build, not an
Apple-reviewed or App Store-approved application. There is no telemetry,
advertising SDK, analytics SDK or third-party account integration.

Apple's bundled `swiftui-whats-new-27` skill and its AsyncImage and State-macro
references were read from Xcode 27 before implementation. Authenticated artwork
uses the new `AsyncImage(request:)` and a custom ephemeral URLSession. No shared
cookie jar or persistent private artwork cache is used.

## Verification

```sh
swift test --package-path apple --scratch-path /tmp/ytmdl-apple-tests
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-macOS CODE_SIGNING_ALLOWED=NO build
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-iOS -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-tvOS -destination 'generic/platform=tvOS Simulator' CODE_SIGNING_ALLOWED=NO build
```

Use Xcode 27's `DEVELOPER_DIR` if another Xcode is the system default; changing
the global `xcode-select` setting is unnecessary. Native UI tests use only a
loopback fixture started by `python3 apple/Scripts/fixture-server.py` on port
59583. Choose an iPhone/iPad or TV simulator and run Test for that scheme. The
shared test schemes set `YTMDL_FIXTURE_URL` to the local fixture. They never
authenticate to a production host. `--fixture-server` is accepted only in Debug
and only for literal loopback; its cookies are not saved to the keychain.

The included iPhone/iPad/Mac icon adapts the repository music-note mark.
Regenerate it with `swift apple/Scripts/generate-icons.swift` from the repo root.
For an App Store build, supply final layered TV icons, signing/provisioning, privacy
policy, support contact, screenshots, accurate metadata and a reviewer account
using authorized sample music. Verify interruption, AirPlay, background audio,
accessibility, format support and remote focus on physical devices.
