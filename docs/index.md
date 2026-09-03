---
layout: home

hero:
  name: "YTMDL"
  text: "Self-Hosted Music Hub"
  tagline: "High-fidelity Opus downloader, automated discography manager, multi-tier lyrics, and audiophile web player."
  image:
    src: /logo-mark.png
    alt: YTMDL Brand Logo
  actions:
    - theme: brand
      text: Get Started →
      link: /getting-started
    - theme: alt
      text: Deployment Guide
      link: /deployment
    - theme: alt
      text: GitHub
      link: https://github.com/Der-Felix/ytmdl

features:
  - icon: 🎧
    title: Native Opus Audio
    details: Prefers native Opus streams when available, avoiding unnecessary re-encoding. Verifies stream properties with ffprobe and preserves standard metadata.
  - icon: 🎛️
    title: Integrated Web Player
    details: Persistent in-browser audio engine with gapless playback, 10-band graphic EQ, parametric filters, crossfade, and synchronized lyrics display.
  - icon: 📜
    title: Multi-Tier Lyrics Resolution
    details: Resolves synchronized .lrc and plain .txt lyrics seamlessly via LRCLIB, YouTube Music, and an optional Genius fallback chain.
  - icon: 🔄
    title: Automated Artist Subscriptions
    details: Track artist discographies, automatically queue new releases, and sync catalogs with starvation-protected fair scheduling.
  - icon: 🗄️
    title: Media Server Standardized
    details: Strict directory structure (Artist/YYYY - Album/NN - Title.opus) optimized for Jellyfin, Navidrome, Plex, and Emby with cover art sidecars.
  - icon: 🛡️
    title: Reliable Storage & Audits
    details: Two-phase atomic staging to local disks or host-mounted SMB/NFS shares, protected by a Storage Identity Guard and non-destructive repair previews.
---

<div class="hero-showcase">
  <div class="hero-showcase-window">
    <img src="/screenshots/library.webp" alt="YTMDL Web Application Interface" />
  </div>
</div>

<div class="home-quickstart">
  <div class="home-quickstart-title">⚡ Quick Start with Official Images</div>
  <div class="home-quickstart-desc">Deploy the full stack in under 60 seconds with Docker Compose or Podman:</div>

```sh
# 1. Download official compose stack and environment template
curl -fsSL -O https://raw.githubusercontent.com/Der-Felix/ytmdl/main/compose.ghcr.yaml
curl -fsSL -O https://raw.githubusercontent.com/Der-Felix/ytmdl/main/.env.example
cp .env.example .env

# 2. Launch production stack
podman compose -f compose.ghcr.yaml up -d
# or: docker compose -f compose.ghcr.yaml up -d
```

  <div style="margin-top: 14px; font-size: 0.9rem; color: var(--vp-c-text-2);">
    Then open <code>http://localhost:8080</code> to complete first-run setup. Read the full <a href="/getting-started">Getting Started Guide →</a>
  </div>
</div>
