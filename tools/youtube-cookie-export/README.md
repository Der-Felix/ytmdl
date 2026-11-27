# YTMDL Cookie Export

A minimal browser extension that saves the `youtube.com` cookies of the current
browser window as a Netscape `cookies.txt`, the format a YTMDL
[managed media session](../../docs/features/providers.md) takes.

It exists so that nobody has to install a third-party cookie exporter for
this. The whole extension is three small files you can read in a minute:

- it asks for the `cookies` permission on `youtube.com` only,
- it makes no network request and stores nothing,
- the file is built in memory and saved through the browser's own download.

## Install (Chrome, Edge, Brave, Vivaldi)

1. Open `chrome://extensions` and switch on **Developer mode**.
2. **Load unpacked** and pick this folder (`tools/youtube-cookie-export`).
3. In the extension's **Details**, switch on **Allow in Incognito**.

Safari is not supported.

## Export

A cookie file is a login. Use a separate Google account for this, not your
main one: an account used for automated downloads can be restricted.

YouTube rotates the cookies of a browser session that stays in use, which
makes an exported file stop working. To get cookies that stay valid:

1. Open a **new Incognito window** and sign in to YouTube there.
2. In the same tab, open `https://www.youtube.com/robots.txt`.
3. Click the extension icon, then **cookies.txt speichern**.
4. **Close the Incognito window** straight away, without signing out.

## Use

In YTMDL: **Servereinstellungen → Session hinzufügen**, pick the
saved `cookies.txt`, save. The server stores the file and never shows it
again. Delete the downloaded `cookies.txt` afterwards.
