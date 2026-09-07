# Metadata Providers

YTMDL resolves artist discographies, album tracklists, and canonical ISRC/catalog data through structured metadata providers.

## Supported Metadata Providers

- **Spotify:** Fast artist searches, high-resolution cover artwork, and rich discography listings. Requires a free Spotify Developer Client ID & Secret.
- **Deezer:** Comprehensive international catalog with ISRC codes, barcode identifiers, and album artwork. Does not require API keys.
- **YouTube Music:** Track metadata and audio candidate search.

## Media Acquisition Providers

YTMDL retrieves audio streams through media providers coordinated by the `ProviderOrchestrator`:

- **YouTube Music (`ytmusic`)**: Primary media acquisition provider searching official track releases.
- **YouTube (`youtube`)**: Secondary video catalog fallback within the YouTube platform family.
- **SoundCloud (`soundcloud`)**: Optional independent media acquisition provider. No SoundCloud user account or login credentials are required for supported public media.

### Provider Ordering & Fallback

The default acquisition chain evaluates providers in registration order:
`ytmusic` → `youtube` → `soundcloud`

1. **Candidate/Content Fallback**: If a track cannot be located or resolved on the YouTube family (`TrackNotFound` / candidate exhaustion), acquisition falls back cleanly to SoundCloud.
2. **Protection Failure Isolation**: Systemic or platform-level protection failures (such as `SESSION_BOT_CHALLENGE` or rate limits) immediately halt same-attempt fanout. The item transitions to `retry_wait` rather than hopping providers to bypass platform controls.
3. **Platform Family Isolation**: SoundCloud operates under its own platform family (`soundcloud`), isolated from the YouTube family (`youtube`). Quota limits, family cooldowns, and session health on one family never affect or poison the other. YouTube credentials and session cookies are never sent to SoundCloud.

## Conservative Track Matching

When matching metadata against audio streams:
- Track durations must match within tight bounds (typically ±3 to ±5 seconds).
- Official artist channels and official audio releases are strictly prioritized over user uploads.
- Explicit checks prevent live performances, acoustic covers, or remixes from substituting standard studio album cuts.
