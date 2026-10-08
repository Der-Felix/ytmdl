#!/bin/bash
# Builds a macOS .dmg with an ad-hoc signed, NOT notarized YTMDL.app.
#
# No Apple account or certificate is involved, so Gatekeeper blocks the first launch;
# apple/INSTALL.md explains the one-time "Open Anyway" step.
#
#   DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer \
#     apple/Scripts/package-dmg.sh <output-dir>
#
# Keep <output-dir> outside iCloud/Documents: Finder resource forks break signing.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/release-info.sh"

out="${1:?usage: package-dmg.sh <output-dir>}"
mkdir -p "$out"; out="$(cd "$out" && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/ytmdl-dmg.XXXXXX")"
trap 'rm -rf "$work"' EXIT

run_xcodebuild "$work/build.log" archive \
  -project "$apple_dir/YTMDL.xcodeproj" -scheme YTMDL-macOS -configuration Release \
  -destination 'generic/platform=macOS' \
  -archivePath "$work/YTMDL-macOS.xcarchive" -derivedDataPath "$work/derived" \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY=

built="$(find "$work/YTMDL-macOS.xcarchive/Products/Applications" -maxdepth 1 -name '*.app' -print -quit)"
[ -d "$built" ] || { echo "The archive contains no app." >&2; exit 1; }

stage="$work/stage"
mkdir -p "$stage"
ditto --norsrc --noextattr --noqtn "$built" "$stage/YTMDL.app"

# Ad-hoc signature ("-"): it seals the bundle and carries the sandbox entitlements,
# but names no developer. Hardened runtime stays on, as the project configures it.
codesign --force --sign - --timestamp=none --options runtime \
  --entitlements "$apple_dir/Resources/Mac.entitlements" "$stage/YTMDL.app"
codesign --verify --strict --verbose=2 "$stage/YTMDL.app"
if ! codesign -d --entitlements :- "$stage/YTMDL.app" 2>/dev/null | grep -q 'com.apple.security.app-sandbox'; then
  echo "The sandbox entitlement is missing from the signed app." >&2; exit 1
fi
packed="$(plutil -extract CFBundleShortVersionString raw "$stage/YTMDL.app/Contents/Info.plist") ($(plutil -extract CFBundleVersion raw "$stage/YTMDL.app/Contents/Info.plist"))"
[ "$packed" = "$app_version ($app_build)" ] || { echo "The app is $packed, expected $app_version ($app_build)." >&2; exit 1; }

ln -s /Applications "$stage/Applications"
cat > "$stage/READ ME FIRST.txt" <<'EOF'
YTMDL is not notarized by Apple, because no paid developer account is tied to this
project. macOS therefore blocks it the first time you open it.

1. Drag YTMDL.app onto Applications.
2. Open it once. When macOS refuses: System Settings > Privacy & Security, scroll down,
   click "Open Anyway" next to YTMDL, then confirm.
   (Terminal instead: xattr -dr com.apple.quarantine /Applications/YTMDL.app)
3. Check your download: "shasum -a 256 <this file>.dmg" must match SHA256SUMS on the
   release page.

More: apple/INSTALL.md in the YTMDL repository.
EOF

dmg="$out/YTMDL-macOS-${app_version}-${app_build}.dmg"
rm -f "$dmg"
hdiutil create -quiet -volname "YTMDL ${app_version}" -srcfolder "$stage" -format UDZO -ov "$dmg"
hdiutil verify -quiet "$dmg"

echo "Created $dmg ($(lipo -archs "$stage/YTMDL.app/Contents/MacOS/"* | head -n 1))"
shasum -a 256 "$dmg"
