# Multi-Tier Lyrics Resolution

YTMDL automatically resolves lyrics for downloaded tracks using a prioritized provider chain.

## Resolution Chain

```text
Track Metadata
      │
      ▼
┌──────────────┐      HIT (LRC / TXT)
│    LRCLIB    │ ────────────────────────> Save Sidecar (.lrc / .txt)
└──────────────┘
      │ MISS
      ▼
┌──────────────┐      HIT (Plain)
│YouTube Music │ ────────────────────────> Save Sidecar (.txt)
└──────────────┘
      │ MISS
      ▼
┌──────────────┐      HIT (Plain)
│Genius (opt.) │ ────────────────────────> Save Sidecar (.txt)
└──────────────┘
      │ MISS
      ▼
   Missing
```

1. **LRCLIB:** Primary lyrics source providing synchronized `.lrc` and plain `.txt` lyrics. Tracks identified as instrumental terminate the chain immediately to prevent false matches.
2. **YouTube Music:** Secondary fallback extracting official plain lyrics.
3. **Genius (Optional Fallback):** Conservative plain-text search fallback. Disabled by default; can be enabled via `MUSICDL_PROVIDERS_GENIUS_ENABLED=true`.

> [!NOTE]
> Existing lyrics files are never silently overwritten. Manual refreshes can be initiated on demand from the WebUI.
