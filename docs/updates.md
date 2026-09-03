# Update Detection & Releases

Starting with **v0.15**, YTMDL introduces native update detection within the WebUI, allowing administrators to monitor new official stable releases.

![YTMDL System & Updates](/screenshots/updates.webp)

## Stable Release Distribution

YTMDL uses a two-tier repository architecture:
- **Canonical Development Repository:** Internal builds, feature branches, and full development history.
- **Public GitHub ([Der-Felix/ytmdl](https://github.com/Der-Felix/ytmdl)):** Public stable releases, issue tracking, official documentation, and container distribution.

Public container images are published to the GitHub Container Registry (GHCR):
- `ghcr.io/der-felix/ytmdl-backend:<version>`
- `ghcr.io/der-felix/ytmdl-frontend:<version>`

## How Update Detection Works

1. **GitHub Releases API:** The YTMDL backend queries the official GitHub API (`GET /repos/Der-Felix/ytmdl/releases/latest`) on a background schedule (default: once per hour).
2. **SemVer Comparison:** The installed version (e.g. `0.14.1`) is compared against the latest stable release tag using strict Semantic Versioning. Pre-releases and drafts are filtered out.
3. **WebUI Notifications:** When a newer stable version is available, the WebUI displays an informational banner and details in **Settings → System & Updates**, including release notes and direct links to the release.

## Privacy & Network Transparency

- **No Telemetry:** YTMDL does not transmit library sizes, usernames, music data, or host metrics to GitHub. The update check is a plain anonymous HTTP GET request to the public GitHub Releases endpoint.
- **Can Be Disabled:** Administrators can completely disable outbound update checks by configuring:
  ```sh
  MUSICDL_UPDATE_CHECKS_ENABLED=false
  ```
  When disabled, no external network requests are made.

## Manual Upgrades Only

> [!IMPORTANT]
> **v0.15 introduces update detection only.** YTMDL does not perform automated self-updates, background container restarts, or in-place binary replacements.

To upgrade an existing installation, update your compose configuration file and restart the stack:

```sh
# Example: Upgrading with compose.ghcr.yaml
export YTMDL_VERSION=0.15.0
podman compose -f compose.ghcr.yaml pull
podman compose -f compose.ghcr.yaml up -d
```
