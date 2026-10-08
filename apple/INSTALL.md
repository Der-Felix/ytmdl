# Installing the Apple apps without the App Store

The native YTMDL apps are not in the App Store. Each release on the
[GitHub Releases page](https://github.com/Der-Felix/ytmdl/releases) (look for
"YTMDL Apple apps", tag `apple-v<version>-<build>`, marked *Pre-release*) offers:

| File | For | Signing |
| --- | --- | --- |
| `YTMDL-iOS-<version>-<build>-unsigned.ipa` | iPhone, iPad | **None.** You sign it with your own Apple ID when you install it. |
| `YTMDL-macOS-<version>-<build>.dmg` | Mac | Ad-hoc signature, **not notarized**. macOS asks you to allow it once. |
| `SHA256SUMS` | all | Checksums of the files above. |

No Apple developer account belongs to this project: nothing is signed or notarized on
its behalf, so you only trust the files you build or check yourself (see
[Check your download](#check-your-download)). Apple TV has no download; see
[Apple TV](#apple-tv).

**Requirements:** iOS, iPadOS or macOS 27, and your own YTMDL server. Release builds
only connect over **HTTPS** (a plain `http://` address is refused).

## Check your download

```sh
shasum -a 256 YTMDL-macOS-0.3.0-35.dmg
```

The hash must equal the matching line in `SHA256SUMS`. The files are built by the
`Apple packages` GitHub workflow from the tagged commit, with no secrets involved.

## iPhone and iPad

iOS runs only signed apps, so a sideloading tool signs the `.ipa` with your Apple ID
on the device's behalf. AltStore, SideStore and Sideloadly are the usual choices; they
are third-party tools, so follow their own instructions for setting up and trusting them.

1. Install the tool of your choice and sign in with your Apple ID in it.
2. Open `YTMDL-iOS-…-unsigned.ipa` with the tool and install it on your device.
3. On the device, turn on **Developer Mode** (Settings > Privacy & Security) if iOS asks
   for it, and trust your developer profile if it asks (Settings > General > VPN &
   Device Management).
4. Open YTMDL and connect your server.

Things to know:

- With a **free Apple ID** the installation expires after 7 days and has to be refreshed
  by the tool; free accounts also have a small limit on apps and App IDs. YTMDL's widget
  counts as a second App ID. A paid developer membership of your own lifts these limits.
- **Updating:** install the newer `.ipa` with the same tool and the same Apple ID.
  Many tools rewrite the bundle identifier per Apple ID. Installing with a different
  Apple ID then gives a separate copy with its own sign-in and offline music.
- Offline music and the session live inside the app on your device, not in the file.

## Mac

1. Open the `.dmg` and drag **YTMDL** onto **Applications**.
2. Open YTMDL. macOS refuses because it is not notarized.
3. Open **System Settings > Privacy & Security**, scroll down, click **Open Anyway** next
   to YTMDL and confirm. (The button appears right after the refused launch.)
   Terminal alternative: `xattr -dr com.apple.quarantine /Applications/YTMDL.app`

You only do this once per downloaded version. Replacing the app with a newer one keeps
its data. Because the signature is ad-hoc and changes with every build, macOS may ask
again for Keychain access after an update, or you may have to sign in again.

## Apple TV

There is no download for Apple TV: tvOS cannot install apps from a file. Build it from
source with Xcode 27 and your own Apple ID:

1. `git clone` this repository and open `apple/YTMDL.xcodeproj`.
2. Select the **YTMDL-tvOS** scheme, choose your Apple ID under Signing & Capabilities
   and your paired Apple TV as destination, then Run. A free Apple ID expires after 7 days.
3. On the TV enter your server's HTTPS address; it then shows a sign-in code. Approve the
   code on a signed-in iPhone, iPad, Mac or in the web UI's profile page. This needs a
   server with the device sign-in routes (see `docs/features/device-sign-in.md`).

## Build it yourself

You can produce exactly the release files locally (Xcode 27 selected, for example via
`DEVELOPER_DIR`; use an output folder outside iCloud and Documents):

```sh
apple/Scripts/package-ipa.sh ~/Downloads/ytmdl-packages
apple/Scripts/package-dmg.sh ~/Downloads/ytmdl-packages
```

## For maintainers: publishing a release

1. Raise `CFBundleShortVersionString`/`CFBundleVersion` in **both**
   `Resources/Info.plist` and `Widgets/Info.plist` (the scripts refuse a mismatch).
2. Merge to `dev` or `main`. The workflow only releases commits that are on one of them.
3. Push the tag `apple-v<version>-<build>` (for example `apple-v0.3.0-35`). The tag must
   match the plists. The workflow builds both packages, checks them, and creates a
   **draft pre-release** with the files and `SHA256SUMS`. Review it, then publish it.
4. To test the packaging without a release, run the `Apple packages` workflow manually
   (it is listed once the file is on the default branch); the files stay a workflow artifact.

The tag deliberately does not start with `v`: the server's release workflow and its
update check only look at `v*` releases, and a test pins that `apple-v…` is never offered
as a server update. Apple releases share the repository's release list, so publish them
sparingly; the development channel of the server's update check reads only the 30 most
recent releases.
