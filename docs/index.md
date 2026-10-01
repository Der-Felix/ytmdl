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
    details: Two-phase atomic staging to local disks or host-mounted SMB/CIFS shares, protected by a Storage Identity Guard and non-destructive repair previews.
---

<div class="hero-showcase">
  <div class="hero-showcase-window">
    <img src="/screenshots/dashboard.webp" alt="YTMDL dashboard with active downloads — v0.27.0" />
  </div>
</div>

<div class="home-quickstart">
  <div class="home-quickstart-title">⚡ Quick Start with Official Images</div>
  <div class="home-quickstart-desc">Install the versioned package with Docker Compose or rootless Podman:</div>

```sh
curl -fLO https://github.com/Der-Felix/ytmdl/releases/download/v1.0.0/ytmdl-1.0.0.tar.gz
curl -fLO https://github.com/Der-Felix/ytmdl/releases/download/v1.0.0/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
# macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
tar -xzf ytmdl-1.0.0.tar.gz
cd ytmdl-1.0.0
python3 scripts/install.py --engine docker
# Or: python3 scripts/install.py --engine podman
```

  <div style="margin-top: 14px; font-size: 0.9rem; color: var(--vp-c-text-2);">
    Then open <code>http://localhost:8080</code> to complete first-run setup. Read the full <a href="/getting-started">Getting Started Guide →</a>
  </div>
  <div style="margin-top: 8px; font-size: 0.9rem; color: var(--vp-c-text-2);">
    New here? See the <a href="/faq">FAQ</a>, <a href="/troubleshooting">Troubleshooting</a>, and <a href="/tips">Tips &amp; Best Practices</a>.
  </div>
</div>
