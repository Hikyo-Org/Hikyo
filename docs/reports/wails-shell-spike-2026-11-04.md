# Wails shell spike results

Date: 2026-11-04. Code: [`spike/wails-shell`](../../spike/wails-shell). Tests option C1
from the [decoupling report](./server-webapp-decoupling-2026-11-04.md).

## Summary

**C1's core works on macOS with Wails v3.1.0.** A native window ran an unmodified
v1.0.0 SPA from its instance on a CLI-class session, with no server change and no SPA
change. All 23 authenticated routes rendered, SSE was live, and a secret was revealed
through the real UI with a TOTP code.

Three things block a product as things stand:

1. **Wails has no download, popup or navigation handling on macOS.** Downloads fail
   silently. This is the framework's gap, not C1's.
2. **Passkeys cannot run in the window**, as predicted. The system-browser path for
   a passkey reveal needs SPA work and was not built, so that half is unproven.
3. **Copied secrets are not auto-cleared**, because WebKit refuses the clipboard read
   the SPA does first.

The spike also corrected the design: **CLI sessions rotate on step-up, and the new
token comes back in the JSON body.** A plain proxy leaks it to JavaScript and then
signs itself out. The shell must intercept it, which the spike does.

The shell is 11.5 MB (4.4 MB gzip) without DWARF, against 150 MB (42.9 MB tar.gz) for
the v1.0.0 `hikyo` binary.

## Results against the report's tests

Run against published v1.0.0 (`-tags=ui`, checksum verified) as `server --dev` on
`http://localhost`, on macOS arm64.

| # | Test | Result | Evidence |
|---|---|---|---|
| 0 | Secure context under `wails://` (added) | **Pass** | `isSecureContext` true. `crypto.subtle`, `clipboard`, `credentials`, `localStorage`, `BroadcastChannel` present |
| 1 | SPA and every route render | **Pass** | 23 authenticated routes, `AuthProvider` via `whoami`, no JS errors in the route walk. 403 and 404 responses matched missing grants. `/instance` needed a settled rate budget (see below) |
| 2 | SSE live through the handler | **Pass when focused** | Events reached the page at the same millisecond as a direct reader (`08:27:34.598`). An occluded window saw about 21 s delay on one event |
| 3 | Passkey and system browser | **Partly** | Device handoff completed in real Chromium, with TOTP, giving a `cli` session with `password, totp`. In the window, `navigator.credentials.get` gives `SecurityError: The origin of the document is not authorized for the provided RP ID`. No passkey hop was built or run |
| 4 | OIDC and SAML | **Not tested** | `window.open` returned `null`, so the SPA's fallback would navigate the window itself to the provider. From source, not run |
| 5 | No operation rejects a CLI session | **Pass for what ran, with one design fix** | Reads, TOTP step-up and TOTP reveal all worked, with `Origin: wails://…`, no cookie, no CSRF header. Rotation needs interception |
| 6 | Downloads, clipboard auto-clear | **Fail** | `<a download>` and blob downloads produced no file and no navigation. Clipboard write works when focused. `readText` gives `NotAllowedError`, and `web/src/app/clipboard.ts` treats that as "do not clear" |
| 7 | Connection isolation | **Pass after a fix** | `localStorage` is separate. CSP blocked cross-connection `fetch` and `<img>`. Top-level navigation was not blocked until the spike bound each window ID to its connection |
| 8 | CSP, COOP, CORP | **Pass** | Assets load, and `connect-src 'self'` and `img-src 'self'` are enforced with correct origin semantics. One `script-src eval` violation of unknown source, no visible effect |

The reveal flow was: open values, click Reveal, choose "Authorise with a code", enter
the code. The value appeared and the page showed "Disclosure recorded". The value is
withheld until the window has focus ("Return to this window to complete the reveal"),
and this gate worked in the shell.

## What this changes in the design

**Rotation.** `POST /auth/totp/enrol/confirm`, `/auth/totp/step-up` and
`/auth/reauth/totp` returned a new `session_token`; after each rotation the old token
got 401 where checked. `api/openapi.yaml` references `LoginResult` 17 times and
`ReauthResult` also carries the field, sometimes nested under `login`. The spike reads the top level and `login`
only, because a nested key could be a secret's name. Its logs show "session rotated,
token withheld" and `whoami` stayed 200. A product would also need:

- A shared token across in-flight requests. The spike applies rotation immediately,
  and requests already sent with the old token can 401.
- Durable storage on rotation, or the session is lost on relaunch.
- A session of its own, never shared with the CLI, since either one rotating logs out
  the other.

**Sign-in in the window cannot work.** The SPA signs in with `artifact: 'browser'`,
which delivers the session as a cookie, and the proxy strips cookies. A dead session
drops the window to `/login` cleanly, so the shell has to catch that and start its own
handoff. Grant changes also invalidate sessions, so this path is common. Only the
wrong-password path was run through the proxy.

**App mode reaches users only through server upgrades.** The SPA comes from the
instance, so an app-mode change does nothing on an older server. The shell needs a
capability check (the report's meta capability), and older instances keep failing on
passkey reveals and downloads.

**Occluded windows.** WKWebView throttled `setTimeout` to about once a minute while
the window was covered. Network replies still arrived. Foreground use is unaffected.
A browser tab behaves similarly in general, but that was not measured here.

**Go reuse depends on where the shell lives.** The spike imports no Hikyo package. A
nested module whose path starts with `github.com/Hikyo-Org/hikyo/` can import
`internal/` packages, and one on another path is refused (both built on 2026-11-04).
Importing `internal/cli` itself would pull in most of its 88.8 MB, so the login and
trust code would still need moving to small packages first.

**Passkey decision.** Like the CLI, the app opens the system browser to validate the
passkey. The server side already exists (`runCLIDisclosureReauth`, `/reauth/cli`). The
SPA has to hand the ceremony to the shell instead of calling `navigator.credentials`.

## Wails v3.1.0 on macOS

Works: per-connection hosts (`wails://conn-0/`), streaming (each write goes straight to
`didReceiveData`), JSON request bodies, a middleware that runs before Wails' own paths,
and per-request `x-wails-window-id`.

Missing: `webview_window_darwin.m` has no `decidePolicyForNavigationAction`,
`WKDownload` handling or `createWebViewWithConfiguration`. The consequences are no
downloads, no popups, and no way to stop the window navigating to an external site.
From source, not run: a page navigated elsewhere keeps the `external` message handler,
whose accepted messages are low-risk (drag, resize, ready) unless
`AllowSimpleEventEmit` is set.

Also: the scheme is hard-coded as `wails`, and `window._wails` is injected into every
page. v3.1.0 is 3 days old. Windows and Linux were not tested.

Options for the gap, in the order I would try them:

| Option | Cost | Risk |
|---|---|---|
| Add the delegate methods to Wails' class at runtime from our own cgo | Small, roughly 50 lines of Objective-C by estimate, untested | Relies on Wails internals, so each upgrade needs a recheck |
| Carry a Wails fork or upstream a patch | Medium | Fork upkeep, or waiting on upstream |
| Handle downloads in app mode: the SPA hands bytes to a shell route | Small per site, and there are few sites | Needs an SPA change, and does not solve navigation policy |
| Use Electron for the shell | Rewrite the 250 lines | Ships Chromium and its update duty, loses the small binary. WebAuthn still needs the hop |

## Rate limit: already fixed, not in v1.0.0

Both cookie and bearer sessions got their first 429 after 56 rapid requests on v1.0.0.
In a plain Chromium tab on the same instance, the default route made 94 API requests in
62 s, and 34 were 429. The shell does not cause this.

This is the known issue from the duplicate-polling work, not a new finding. `9732c1262`
stops live sessions spending the 60 per minute source allowance, and `786f1cd9e`,
`355e69049` and `6749314b9` cut the SPA's requests. None is in v1.0.0 or in
`nightly.20261102.103`, and all are in `nightly.20261104.106`. I did not rerun the
measurement on that nightly.

## Evidence and limits

**Not tested:** a hosted instance or real TLS; SPKI pinning; a real passkey (no
virtual or hardware authenticator was used); the loopback handoff, since v1.0.0's CLI
refuses loopback http without its socket, so the device endpoints were called directly
and approved once in Chromium; OIDC, SAML; write flows beyond reveal, step-up and
enrolment; Windows, Linux; signing or notarization.

**Weaker evidence:** the enrolment POST ran directly against the API with the bearer,
not through the window. Clipboard non-clearing is inferred from a refused read plus the
source, because the SPA's copy button was not run. Whether `totp_offered` becomes false
at reveal window 0 is from source only (`internal/service/ceremony.go`, `effective > 0`):
setting `HIKYO_REAUTH_WINDOW_SECONDS=0` on `--dev` still reported 900, for a reason I did
not chase. So this run's in-window TOTP reveal may not be available on a default
production instance, where the hop would apply to every reveal.

**Method:** the window was driven by injected JavaScript, not by a person, and no
screenshot was kept because captures included other windows. One early screenshot
confirmed the SPA painting. The probes are in
[`spike/wails-shell/probes`](../../spike/wails-shell/probes). All instances, sessions and
binaries were disposable and removed. No real instance or secret was touched.
