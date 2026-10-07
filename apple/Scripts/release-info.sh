# Shared by package-ipa.sh and package-dmg.sh (sourced, not executed).
#
# Reads the app version from the Info.plist files, so the file names and the
# release tag can never disagree with the binary. Nothing here touches an Apple
# account, certificate or provisioning profile.

apple_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

plist_value() { /usr/libexec/PlistBuddy -c "Print :$2" "$1"; }

for plist in "$apple_dir/Resources/Info.plist" "$apple_dir/Widgets/Info.plist"; do
  [ -f "$plist" ] || { echo "Missing $plist (is this script being run from a YTMDL checkout?)." >&2; exit 1; }
done

app_version="$(plist_value "$apple_dir/Resources/Info.plist" CFBundleShortVersionString)"
app_build="$(plist_value "$apple_dir/Resources/Info.plist" CFBundleVersion)"
widget_version="$(plist_value "$apple_dir/Widgets/Info.plist" CFBundleShortVersionString)"
widget_build="$(plist_value "$apple_dir/Widgets/Info.plist" CFBundleVersion)"
if [ "$app_version ($app_build)" != "$widget_version ($widget_build)" ]; then
  echo "App is $app_version ($app_build) but the widget is $widget_version ($widget_build); they must match." >&2
  exit 1
fi

# The tag that publishes this version. It deliberately does not start with "v":
# the server release workflow and the server's update check only look at v* tags.
release_tag="apple-v${app_version}-${app_build}"

# No pipe into head: with pipefail the early-closed pipe would report a failure.
xcode_version="$(xcodebuild -version)"
if [[ ! "${xcode_version%%$'\n'*}" =~ ^Xcode\ 27(\.|$) ]]; then
  echo "Xcode 27 is required (select it per command with DEVELOPER_DIR)." >&2
  exit 1
fi

# Runs xcodebuild quietly and shows the end of the log only if it fails.
run_xcodebuild() {
  local log="$1"; shift
  if ! xcodebuild "$@" > "$log" 2>&1; then
    tail -n 60 "$log" >&2
    echo "xcodebuild failed; see the log above." >&2
    exit 1
  fi
}
