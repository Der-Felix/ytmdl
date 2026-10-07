# Apple client security and design decisions

Research date: 2026-10-04. Guidance comes from Apple's documentation and the
device-authorization RFC; Xcode 27's bundled SwiftUI skills are implementation
references. No unreviewed community skill was installed.

## Credentials and transport

The user enters an existing server origin. HTTPS is mandatory in Release.
Debug allows explicit local HTTP only for literal private/loopback IPv4 or
localhost; names resembling IP addresses do not qualify. Userinfo, query
strings, fragments and base paths are rejected. Certificate validation is the
system default. There is no trust-all delegate or `NSAllowsArbitraryLoads`.
The local-network purpose string explains connecting to the user's server.

The existing server cookie session and double-submit CSRF mechanism remain in
use; the client does not invent an auth bypass. Passwords are used once and
cleared from the input. Only session/CSRF cookies are persisted in Keychain,
scoped to the exact server origin, nonsynchronizable and device-only, accessible
after first unlock to support background playback. Secrets are not kept in
UserDefaults, URLs, logs, fixture screenshots or the project. Ephemeral sessions
avoid the shared cookie jar and persistent response caches. API/artwork redirects
are rejected. Audio assets receive origin-scoped cookies using the public
AVURLAsset cookie API; backend stream routes must serve audio directly without
cross-origin redirects. User-selected server origins are trusted for the user's
media. Servers should never return cookie-domain settings outside their origin.

Logout attempts server revocation and clears local cookies/keychain state even
if the network request fails. If the server is offline, remotely revoking that
session is still necessary through an online device's session list; local
forgetting does not falsely claim successful remote revocation.

References:
- [Keychain services](https://developer.apple.com/documentation/security/keychain-services)
- [App Transport Security](https://developer.apple.com/documentation/bundleresources/information-property-list/nsapptransportsecurity)
- [Local network privacy](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy)
- [AVURLAsset cookies](https://developer.apple.com/documentation/avfoundation/avurlassethttpcookieskey)

## Navigation widgets

Build 30 embeds an iOS WidgetKit extension with static Home/Lock Screen entry
points. It has no network client, session storage, App Group, shared cookies or
current-track data. `ytmdl-player` URLs accept only the player, favorites and
playlists hosts; user info, ports, queries, fragments and additional paths are
rejected. Another app can request these public navigation routes but cannot
select an account/server, supply credentials, mutate a playlist or start audio.
The root waits for an existing authenticated session or explicit sign-in before
opening a destination. Offline collection shortcuts retain the saved-music
overview and do not make authenticated API calls. Widgets supplement the app;
they do not replace the system's media controls or bypass device unlocking.

Reference: [Apple widget strategy](https://developer.apple.com/documentation/widgetkit/developing-a-widgetkit-strategy).

## Device code authorization

This is a cookie-session pairing flow inspired by RFC 8628, **not an OAuth
implementation**. The TV retains a separate 256-bit secret in memory. Its
displayed human code has 40 bits of entropy, is formatted `ABCD-EFGH`, expires
after five minutes and never substitutes for the secret at exchange. Only
SHA-256 hashes are retained in the backend process. A restart expires all
pending grants. Completed sessions use the existing PostgreSQL session storage.

Preview and confirmation require authentication and CSRF. Short-code queries
use POST bodies; no raw device secrets occur in paths or query strings.
Confirmation is explicit, shows the requesting device name and warns against
approving unsolicited codes. Preview/confirm attempts are limited to ten per
account per five minutes. Grant creation is limited to ten per client IP per
five minutes. Admission, approval and consumption are atomic. Pending grants
are capped at 128; each admission-key map is capped at 1024 and expiry-pruned.
Polling uses a five-second minimum and backs off on excess requests.

The granting session and enabled user are rechecked at exchange. Codes cannot
be reassigned or replayed. Exchange creates a separate revocable session and
returns it only as an HttpOnly cookie, never as JSON. All pairing responses are
`Cache-Control: no-store`. Approval grants ordinary account permissions; the
current implementation does not claim player-only authorization scopes.

[RFC 8628](https://www.rfc-editor.org/rfc/rfc8628) supplies the security principles
for expiring codes, authorization on a second device and polling backoff. TLS
is required for normal deployments; the explicitly chosen local HTTP Debug
mode has no transport secrecy and must not be exposed to the internet.

## Layout, accessibility and native behavior

Use system TabView/NavigationSplitView/List/Menu/Picker/Slider and focus behavior.
Album art remains content; translucent materials are reserved for player
controls/navigation. Adaptive grids, scrollable player content and bounded cover
sizes address the original proportions problem. SF Symbols, semantic fonts,
text wrapping, VoiceOver labels and clear button targets are used. Do not force
focus changes as content loads. No automatic animation loop, hover motion or
custom cursor is added. TV has system remote focus and discrete seek buttons.
Accessibility and large text need physical-device review, especially long names.

Audio interruption and unplugging output pause playback. SDK 27's new inactive
audio-session notification is used. Resume is explicit. Pending async playback
loads respect pause and selection changes; stale results cannot start another
song. A failed codec/stream is not reported as successful playback.

References:
- [Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines)
- [Layout](https://developer.apple.com/design/human-interface-guidelines/layout)
- [Materials](https://developer.apple.com/design/human-interface-guidelines/materials)
- [Accessibility](https://developer.apple.com/design/human-interface-guidelines/accessibility)
- [Focus and selection](https://developer.apple.com/design/human-interface-guidelines/focus-and-selection)
- [Media playback configuration](https://developer.apple.com/documentation/avfoundation/configuring-your-app-for-media-playback)

## Privacy and distribution

No tracking, advertising or analytics dependencies are present. The privacy
manifest declares the app's own UserDefaults usage (CA92.1) and file timestamps,
sizes and metadata inside its own container for offline storage (C617.1).
These uses follow Apple's
[required-reason API declarations](https://developer.apple.com/documentation/bundleresources/app-privacy-configuration/nsprivacyaccessedapitypes/nsprivacyaccessedapitype).
It is not a privacy
policy or an assertion that the user's server processes no personal data:
accounts, favorites, playlists, session/IP metadata and requested audio remain
on the configured server. Store privacy answers must reflect the actual service
and deployment, not blindly copy a 'no data collected' label.

Mac listening history is enabled by default and stores only metadata for up to
40 played tracks in the app's local preferences. Keys are scoped to the server
origin and authenticated account; this does not encrypt the metadata. That local
list is not sent anywhere. Separately, "Hörverlauf mit meinem Server
synchronisieren" is **on by default** (it feeds the server's most-played and
recently-played smart playlists): each played track ID is sent with an idempotent
event ID to the signed-in server only, never to another service. Settings can turn
it off, which also discards pending events. Settings allow stopping new recording
and clearing the current account's history. Disabling preserves earlier entries;
logout clears the in-memory view. Fixture sessions do not persist history.

Before public distribution, provide a real privacy policy/support contact,
correct store data disclosures, signing and final icons. Provide a reachable
reviewer server/account with authorized sample media. No account creation is
included; the operator provisions accounts. If registration is added later,
evaluate Apple's account-deletion requirements at the same time.

The native client does not contact YouTube or initiate provider downloads.
However, separating a player from the downloader does not guarantee acceptance:
media rights and service usage still need review. Do not hide functionality from
reviewers or use TestFlight to evade App Review. This project does not claim
Apple endorsement or preapproval.

[App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/)
are the source for completeness, privacy, intellectual-property and media
requirements, including sections 2.1, 4.2, 5.1 and 5.2.3.

## Native listening processing

Mac cover palettes are computed from bounded thumbnails of authenticated
same-origin artwork. Cover/lyric tasks are cancelled on track changes and
logout; invalidated API clients reject late requests before creating a network
task or persisting response cookies. No extracted colors leave the device.

EQ runs on decoded PCM using the OS 27 mixed-output AVPlayer tap. The direct
path has no tap while EQ is disabled. A prepared incoming AVPlayer uses the same
origin and session cookies as the current stream. Crossfade and next-track
buffering retain at most one incoming title. Separately requested offline storage
is described below. Stop, seek, logout and timer boundaries cancel incoming playback.
Audio settings are local device preferences; active sleep timers are ephemeral.

See [Apple’s streaming/audio guidance](https://developer.apple.com/streaming/Whats-new-HLS.pdf)
for the OS 27 mixed-output tap. This does not change the existing transport,
codec, account-permission or physical-device qualification boundaries.

## Native offline library (preview 0.2.0)

Offline audio, track metadata and optional artwork/lyrics are stored under the
app's Application Support directory, excluded from backup. Origin/account hashes
partition data, and profile/file IDs are validated when reading manifests. Audio
is published only after a successful same-origin 200 response, size checks and
recognized audio container headers. Local playback rejects partial/missing files
and symbolic links. These checks do not certify physical decoder support.

iOS transfers use a dedicated background URLSession with no shared cookies/cache,
explicit same-origin session cookies and rejected redirects. Neither credentials
nor URLSession resume blobs enter manifests. Logout cancels authenticated tasks,
removes the API session and offers deletion of local music. Retention is an explicit
UI policy: otherwise files and metadata remain accessible from the offline picker,
even after logout. Offline entry creates an unauthenticated client for local source
resolution and routes only to local playback/download views; it is not server login.
File protection permits continued playback after the device has been unlocked once.

The configurable cap covers audio bytes across profiles; sidecars are separately
bounded. Automatic eviction affects only opted-in played-cache records and excludes
currently playing/prepared tracks. Manual copies are removed only by explicit local
removal/clear actions. No local delete calls a server media-delete API.

Optional history synchronization sends idempotent events only to the signed-in
origin/account. Pending events are bounded and partitioned by the same identity;
fixtures never persist or send listening events. Disable recording/synchronization
or explicitly clear local history to discard pending events. Device handoff is an
explicit user action; receiving a handoff never automatically starts playback.
