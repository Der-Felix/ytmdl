# Device-code sign-in

An Apple TV can request a short code instead of accepting an account password
through the remote. An already signed-in browser, iPhone, iPad or Mac inspects
the code and explicitly approves the displayed device. On the web, use
**Profile & Security → Apple TV & Devices**. Scanning a QR link only fills the
code; it never grants access automatically.

The device receives an independent, ordinary account session. An administrator
approving a device grants administrator permissions too. Revoke the device
through **Profile → Sessions**; the approving browser session remains separate.
Use HTTPS for normal operation.

## API contract

All routes use the usual `/api/v1` prefix and JSON response envelopes. Every
POST requires the existing CSRF cookie/header contract, including the unauthenticated
start and poll. First fetch `/auth/status` to establish the CSRF cookie.

| POST route | Request | Access and result |
| --- | --- | --- |
| `/auth/device` | `device_name` (1–64 bytes) | Public; 201 with `device_code`, `user_code`, `expires_in: 300`, `interval: 5` |
| `/auth/device/preview` | `user_code` | Signed-in account; requesting name and expiry, without approval |
| `/auth/device/confirm` | `user_code` | Signed-in account; 204 after explicit approval |
| `/auth/device/poll` | `device_code` | Public; `authorization_pending`, `slow_down` or `authorized` |

After `authorized`, the response sets the existing HttpOnly session cookie and
fresh CSRF cookie. Fetch `/auth/status` to restore the account; the JSON body
contains no session token. Only the opaque device secret can exchange a grant;
the short code cannot do so. Poll at the advertised interval, increasing it by
five seconds after `slow_down`. Invalid, expired and consumed codes require a
new request.

## Bounds and persistence

Codes expire after five minutes. Pending codes and opaque secrets are held only
as hashes in bounded process memory. Start requests and code-inspection/approval
attempts are limited; grant consumption is atomic and single use. The approving
session and enabled account are checked again before issuing a device session.
Revoking the approving session before exchange invalidates the pending grant.

A backend restart invalidates pending codes. Established sessions use the normal
database persistence and revocation rules. This implementation requires one
backend process; multiple replicas would need shared grant storage or sticky
routing. No database migration is required.

Older backends without these routes cannot complete device-code login. Deploy
the backend and web confirmation UI together before offering it to TV clients.
