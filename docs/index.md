---
layout: home

hero:
  name: "YTMDL"
  text: "Self-Hosted Music Hub"
  tagline: "High-fidelity Opus downloader, discography manager, multi-tier lyrics, and integrated web player."
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started
    - theme: alt
      text: Installation & Deployment
      link: /deployment
    - theme: alt
      text: GitHub
      link: https://github.com/Der-Felix/ytmdl

features:
  - title: High-Fidelity Opus Audio
    details: Prefers native Opus streams without lossy transcoding. Verifies stream bitrates and sample rates with ffprobe and embeds standard Vorbis comments.
  - title: Integrated Web Player
    details: Persistent HTML5 audio player featuring gapless playback, 10-band graphic EQ, parametric filters, crossfade, and synchronized lyrics display.
  - title: Multi-Tier Lyrics Resolution
    details: Resolves synchronized .lrc and plain .txt lyrics seamlessly via LRCLIB, YouTube Music, and an optional Genius fallback chain.
  - title: Automated Artist Subscriptions
    details: Track artist discographies, automatically queue new releases, and sync catalogs with starvation-protected fair scheduling.
  - title: Media Server Standardized
    details: Strict directory structure (Artist/YYYY - Album/NN - Title.opus) optimized for Plex, Jellyfin, Navidrome, and Emby with cover art sidecars.
  - title: Reliable Storage & Audits
    details: Two-phase atomic staging to local disks or host-mounted SMB/NFS shares, protected by a Storage Identity Guard and non-destructive repair previews.
---
