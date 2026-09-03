# Metadata Providers

YTMDL resolves artist discographies, album tracklists, and canonical ISRC/catalog data through structured metadata providers.

## Supported Providers

- **Spotify:** Fast artist searches, high-resolution cover artwork, and rich discography listings. Requires a free Spotify Developer Client ID & Secret.
- **Deezer:** Comprehensive international catalog with ISRC codes, barcode identifiers, and album artwork. Does not require API keys.
- **YouTube Music:** Native audio matching source used to locate stream candidates, duration parity, and official audio tracks.

## Conservative Track Matching

When matching metadata against audio streams:
- Track durations must match within tight bounds (typically ±3 to ±5 seconds).
- Official artist channels and official audio releases are strictly prioritized over user uploads.
- Explicit checks prevent live performances, acoustic covers, or remixes from substituting standard studio album cuts.
