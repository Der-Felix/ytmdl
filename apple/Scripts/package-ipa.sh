#!/bin/bash
# Builds an UNSIGNED iPhone/iPad .ipa (with the widget extension) for sideloading.
#
# No Apple account, certificate or provisioning profile is involved. Whoever installs
# the file signs it with their own Apple ID (AltStore, SideStore, Sideloadly, Xcode).
#
#   DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer \
#     apple/Scripts/package-ipa.sh <output-dir>
#
# Keep <output-dir> outside iCloud/Documents: Finder resource forks break signing later.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/release-info.sh"

out="${1:?usage: package-ipa.sh <output-dir>}"
mkdir -p "$out"; out="$(cd "$out" && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/ytmdl-ipa.XXXXXX")"
trap 'rm -rf "$work"' EXIT

run_xcodebuild "$work/build.log" archive \
  -project "$apple_dir/YTMDL.xcodeproj" -scheme YTMDL-iOS -configuration Release \
  -destination 'generic/platform=iOS' \
  -archivePath "$work/YTMDL-iOS.xcarchive" -derivedDataPath "$work/derived" \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY=

app="$(find "$work/YTMDL-iOS.xcarchive/Products/Applications" -maxdepth 1 -name '*.app' -print -quit)"
[ -d "$app" ] || { echo "The archive contains no app." >&2; exit 1; }

mkdir -p "$work/ipa/Payload"
ditto --norsrc --noextattr --noqtn "$app" "$work/ipa/Payload/$(basename "$app")"
find "$work/ipa" -name .DS_Store -delete

ipa="$out/YTMDL-iOS-${app_version}-${app_build}-unsigned.ipa"
rm -f "$ipa"
(cd "$work/ipa" && zip -qry -X "$ipa" Payload)

# Check what was packed, not what was intended.
listing="$(unzip -Z1 "$ipa")"
grep -Eq '^Payload/[^/]+\.app/Info\.plist$' <<<"$listing" || { echo "No app Info.plist in the .ipa." >&2; exit 1; }
grep -Eq '^Payload/[^/]+\.app/PlugIns/[^/]+\.appex/Info\.plist$' <<<"$listing" || { echo "The widget extension is missing." >&2; exit 1; }
if grep -Eq 'embedded\.mobileprovision|_CodeSignature/' <<<"$listing"; then
  echo "The .ipa carries signing material; it must be unsigned." >&2; exit 1
fi
packed_build="$(unzip -p "$ipa" 'Payload/*.app/Info.plist' | plutil -extract CFBundleVersion raw -o - -)"
packed_version="$(unzip -p "$ipa" 'Payload/*.app/Info.plist' | plutil -extract CFBundleShortVersionString raw -o - -)"
if [ "$packed_version ($packed_build)" != "$app_version ($app_build)" ]; then
  echo "The .ipa is $packed_version ($packed_build), expected $app_version ($app_build)." >&2; exit 1
fi

echo "Created $ipa"
shasum -a 256 "$ipa"
