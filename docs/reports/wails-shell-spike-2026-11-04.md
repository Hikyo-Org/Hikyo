# Wails shell spike results

Date: 2026-11-04. Code: [`spike/wails-shell`](../../spike/wails-shell). Tests option C1
from the [decoupling report](./server-webapp-decoupling-2026-11-04.md).

## Summary

**C1's core works on macOS with Wails v3.1.0.** A native window ran an unmodified
v1.0.0 SPA from its instance on a CLI-class session, with no server change and no SPA
change. All 23 authenticated routes rendered, SSE was live, and a secret was revealed
through the real UI with a TOTP code.

**Update, same day: the two main blockers are closed in the spike.** Native handlers
added from our own code give Wails navigation policy, popups and downloads. A passkey
reveal now completes through the system browser, on a protected environment, using a
virtual authenticator. See [Second round](#second-round-handlers-and-passkey-hop).

**Update, 2026-11-05: iOS and Android.** iOS mostly carries over on the simulator. Android
needs its own host, which reaches the real UI on the emulator past sign-in. Screens with
large responses fail until a header fix is built, and two other things still need code. See [Third round](#third-round-ios-and-android).

What remains before a product:

1. **Downloads need a real design**: a save panel, the filename from the click, and the
   macOS quarantine flag.
2. **Only the disclosure ceremony is routed** to the browser. Other passkey sites in the
   SPA still fail in the window.
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
| 3 | Passkey and system browser | **Pass for reveal, virtual authenticator only** | Second round. In the window, `navigator.credentials.get` gives `SecurityError: The origin of the document is not authorized for the provided RP ID`, so the shell hands off to the browser. Device login also completed in real Chromium. Real Touch ID not run |
| 4 | OIDC and SAML | **Not tested** | With handlers, `window.open` to https opens the system browser and the page gets `null`. The SPA's OIDC reauth waits on a `BroadcastChannel`, which cannot cross from the browser to the window, so it likely needs the same handoff. From source, not run |
| 5 | No operation rejects a CLI session | **Pass for what ran, with one design fix** | Reads, TOTP step-up and TOTP reveal all worked, with `Origin: wails://…`, no cookie, no CSRF header. Rotation needs interception |
| 6 | Downloads, clipboard auto-clear | **Downloads pass with handlers, auto-clear fails** | Without handlers, `<a download>` and blob downloads produced no file. Second round fixes that. Clipboard write works when focused. `readText` gives `NotAllowedError`, and `web/src/app/clipboard.ts` treats that as "do not clear" |
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

Options for the gap, as considered before the second round. The first was built and
works:

| Option | Cost | Risk |
|---|---|---|
| Add the delegate methods to Wails' class at runtime from our own cgo | **Built: 120 lines of Objective-C, all 5 methods took effect** | Relies on Wails internals: the class name and when the delegate is set. Each upgrade needs a recheck |
| Carry a Wails fork or upstream a patch | Medium | Fork upkeep, or waiting on upstream |
| Handle downloads in app mode: the SPA hands bytes to a shell route | Small per site, and there are few sites | Needs an SPA change, and does not solve navigation policy |
| Use Electron for the shell | Rewrite the 250 lines | Ships Chromium and its update duty, loses the small binary. WebAuthn still needs the hop |

## Second round: handlers and passkey hop

Built by a Sonnet implementer, against an instance built from this branch with the SPA
change. Downloads and reveal were driven by injected JavaScript and Playwright.

**Native handlers** (`webview_darwin.m`). `class_addMethod` on `WebviewWindowDelegate`
before any window exists works, with no change to the Wails module.

| Scenario | Result |
|---|---|
| `<a download>` to a `wails://` URL | WKDownload cannot load the scheme (`NSURLErrorDomain -1002`). The shell cancels and Go fetches it through the connection instead. Bytes match a direct bearer `curl`: 1855 B, 1553 B and 708 B |
| Blob `<a download>` | Works. 1,000,000 bytes arrived intact, including when the URL is revoked immediately, as `Audit.tsx` does |
| The SPA's real buttons | Both definitions exports saved. Audit "Export JSONL" saved 23703 B of 23703 |
| `window.open`, `target=_blank`, `location.href` to https | Open in the system browser. The window stays where it was |
| `location.href` to the other connection | Cancelled natively, before the Go check |
| `mailto:` | Cancelled |
| `file://`, `javascript:` | Window stayed put with no policy callback. Cause not established, so not counted as a control |

**Passkey reveal** (`reauth.go`, and one `web/src/api/values.ts` branch). On a protected
environment, where the dialog offers only "Use a passkey":

1. Clicking it posts to `/__shell/reauth`, which starts the CLI's existing handoff and
   opens `/reauth/cli` in the browser.
2. The passkey completes there, and the loopback callback returns the code.
3. The shell redeems it, and keeps the rotated token. The page's response holds no token.
4. The value appears in the same window, same view, with page state kept.

The old token then got 401 and `whoami` stayed 200. No server change was needed.

**Things this showed:**

- **The user must return to the window themselves.** The shell could not take focus back
  from the browser. The reveal waits at "Return to this window to complete the reveal"
  and then finishes by itself, with state kept.
- **Downloads lose the filename.** The policy callback never sees the `download`
  attribute, so the definitions exports saved as `export` and `export (1)`.
- **Downloads need hardening before real use.** Instance code can trigger them without a
  prompt, into a fixed directory, and files written by Go carry no
  `com.apple.quarantine` flag, so Gatekeeper would not check them. The quarantine point
  is from reading the code, not tested. `window.open` can also open browser tabs without
  a prompt.
- **Only disclosure is routed.** `runPasskeyCeremony` handles reveal, copy, publish,
  approve, reject and bypass, and only reveal ran. Adapter, developer credential, step-up,
  self-config and passkey enrolment still call WebAuthn directly and fail in the window.
  `mint` and `access` are refused. The branch also skips the SPA's session epoch check.
- **`values.ts` is hash-pinned for security review.** The inventory was not updated, so
  the repo's check would likely refuse this change outside a spike branch.

**Limits:** a virtual authenticator, not Touch ID. The live flow ran on a build before
`location?.protocol` was made optional for the unit tests. Unit tests and `tsc` pass on
the final code. Sign-in through the browser from inside the app was not built: sessions
still come from `hikyo login` or the device endpoints. This round's code had my read-through
and its tests, but no separate Standards and Spec review.

## Automated security review of the second round

An automated review of the second round raised four findings. Two have unit tests;
the native download gate was compiled but not rerun in a live window.

| Finding | Outcome |
|---|---|
| The bearer could follow a redirect off the instance | **Fixed.** Bearer requests no longer follow redirects, and a test confirms the redirect target is never contacted |
| A hostile filename could loop forever in `downloadPath` | **Fixed.** The loop stops on any error other than "not found" and tries at most 1000 names. Control and format characters and over-long names are stripped |
| The download gate accepted a page with no host | **Partly fixed.** The native gate now fails closed, but `saveThroughProxy` accepts any known connection rather than only the window's own. Navigation still allows no host for the first load, covered by the Go window check |
| Instance code can write files with no prompt | **Not fixed, product requirement.** Needs a save panel, a user gesture and the quarantine flag |

While the spike runs, a connected instance's code can drop files into `~/Downloads`.
Run it only against instances you trust.

## Third round: iOS and Android

Date: 2026-11-05. Wails v3.1.0 calls both platforms experimental. The aim was to see
whether the desktop design carries over. **iOS mostly does on the simulator. On Android the
stock host fails on a redirect, and a patched host works on the emulator, with more changes
still needed.**

No real device was used, and no real passkey was attempted. The instance was disposable,
on `127.0.0.1`, with a prebuilt SPA from another worktree behind a small local front,
because `/` returned 404 on `--dev` with the installed binary. That SPA may not match the
server exactly.

### iOS, on the iOS 27.1 simulator

| Question | Result | Evidence |
|---|---|---|
| Proxy compiles and renders the SPA on `wails://conn-0/` | **Pass** | Page, assets, `runtime/status` and `whoami` all returned 200 through the proxy. A host other than `localhost` works |
| POST body, status and rotation through the proxy | **Partial** | A JSON POST got 401 and 200, and the rotation was intercepted. PUT, response headers and SSE not tested |
| Only the enrolment screens were reached | **Limit** | The secrets pages were never seen on iOS |
| Safari hop with the app in the background | **Works only after returning to the app** | See the table below |
| `ASWebAuthenticationSession` hop | **Pass for the redirect** | App stayed foreground, and the redirect reached the listener in 0.7 s. It needs native code to dismiss the sheet |
| Real sign-in and redeem | **Blocked** | Reached `cli-login/start` and the consent alert. The alert then stopped responding to `agent-device` (`main thread execution timed out`) |
| Keychain through `IOS.SecureSet` | **Blocked** | `OSStatus -34018`, a missing entitlement. Ad-hoc signing with the entitlement stopped the app launching. It needs an iOS signing identity |
| External links | **Fixed with native code** | Wails' allow-all policy replaced the app's page with no way back, and `target=_blank` did nothing. Replacing the policy method keeps the page and opens Safari |
| Download links | **Fail, not fixed** | The link replaced the page, which went black. It needs a `WKDownloadDelegate`. Wails' save dialog returns an error on iOS |
| SPA at phone width | **Observed** | Legible, but the keyboard overlaps the form, focus zoom shifts the layout, and the window didn't fill the Duo's screen |
| Size and build time | **Measured** | `.app` about 12 MB. First archive build 1m17s, incremental 6 to 7 s |

Safari hop, on a 127.0.0.1 listener in the app. The app was suspended 6 to 12 s after
Safari opened, and the kernel held the connection until the app returned:

| Delay before the callback | Safari sent | App accepted |
|---|---|---|
| Immediate | 23:38:24 | 23:39:07 |
| 30 s | 23:44:39 | 23:45:25 |
| 2 min | 23:55:12 | 23:55:43 |

Simulator suspension may differ from a real device, so this needs a device run before it
is relied on. On the first run a link-preview client also requested the URL, with a Mac
user agent. The simulator shares the host's network, so it most likely came from the test
Mac rather than iOS, and isn't counted as a platform result.

**Wails on the iOS 27 SDK:** v3.1.0 crashes at launch ("UIScene life cycle is required").
Linking with `-Wl,-platform_version,ios-simulator,15.0,26.0` avoids it, by declaring an
older SDK. That is a workaround, not a fix.

**The hop's real problem:** the server accepts only a loopback redirect
(`internal/service/cli_login.go`, `internal/service/cli_reauth.go`). On iOS,
`ASWebAuthenticationSession` shows a consent alert naming "127.0.0.1", and logs that `http`
support is deprecated and will be removed. A custom-scheme or universal-link callback needs
a new server operation, because closed enums never grow under the freeze. That would be a
new owner decision.

### Android

**The patched host runs on the emulator and reaches the real UI past sign-in, but any large
API response breaks the screen that needs it.** Three things need code before a product: a
response-header fix, a streaming workaround and window confinement. The app is also frozen
while it waits in the background.

The first attempt never completed a device test, because the emulator's framework kept
restarting at load 15 to 21. The rerun was on `Pixel_9_API_36`, Android 16, WebView 133, at
load 8 to 14, and `system_server` kept the same pid throughout. The image has no Chrome, so
there are no Custom Tabs and no passkeys.

| Question | Result | Evidence |
|---|---|---|
| Stock host with the proxy | **Fails on the first redirect** | `GET /index.html -> 301`, then `asset server error: status 301` |
| Patched host renders the SPA | **Pass** (2026-11-05, after the fixes below) | On `https://conn-0.wails.localhost`, secure context. Environment matrix and Values screens list 57 keys |
| Request bodies through the script and bridge | **Pass** | SHA-256 matched for PUT 3 B, PATCH 1 KB, DELETE 16 B, POST 256 KB, PUT 3 MB and POST 20 MB (839 ms). Multipart, urlencoded, Blob and XHR PUT arrived with the right length and type |
| Query and headers | **Pass** | `?q=a%20b&x=%E2%9C%93` and custom headers arrived intact. The bearer was added, no cookie was sent |
| Status codes | **Pass, except redirects** | 200, 400, 401, 404 and 429 reached the page. `WebResourceResponse` rejects 3xx, so the bridge returns 502 with `X-Spike-Redirect` (from code) |
| Response length | **Pass after the 2026-11-05 fix** | The page sees one integer, or `0` only for a chunked body over 8 MiB. Before it, large responses read `0` and the Values screen failed. See the follow-up section |
| Streaming | **Pass with 2 KB padding per event** (2026-11-05) | Without it, five events 800 ms apart all arrived at stream end. See the follow-up section |
| Real sign-in, the app's half | **Pass** | Callback to redeemed session took 59 ms with the app in the background. `whoami` then returned 200, artifact `cli`, factors password and TOTP |
| Enrolment gate in the UI | **Fails, same as iOS** | See [the finding below](#a-finding-that-also-affects-macos). Cleared through the API to go further |
| SPA at phone width, past the gate | **Observed, needs layout work** | 412 CSS px, no page-level horizontal overflow. The breadcrumb overlaps itself and the Values table's action column is cut off at the right (web, left alone). The header under the status bar and content under the navigation bar are fixed in the host (2026-11-05) |
| Per-connection origin | **Pass for storage, gap for requests** | `localStorage` is separate on `conn-0` and `conn-1`. See the isolation finding |
| `Android.SecureSet` | **Pass** | Set, read, wrong value, relaunch and delete. `EncryptedSharedPreferences`, per Wails' source |
| Downloads | **Pass, with limits** | Correct bytes saved and the page stayed put. The file goes to the app's own folder, not the user's Downloads. One click produced two GETs in the log |
| External links | **Pass** | A plain link and a tapped `target=_blank` open the browser, and the app's page was unchanged on return. A scripted `target=_blank` click with no gesture does nothing |
| Back button | **Pass** | Walks WebView history, and the app stays open. Leaving from the first page was not tested |
| Size | **Measured** | APK 16.6 MB, `libwails.so` 12.0 MB |

**`Content-Length`.** With no such header, the WebView reports `content-length: 0` to the
page while delivering the whole body. The SDK client treats `0` as empty. That broke `whoami`
first: it failed its schema, the app showed "Reconnecting to Hikyo", and it polled until the
server answered 429.

Passing the real length through (`SpikeBridge.java`) fixes small responses only, and the
header then reads `0, 207`. The SDK accepts that only because it isn't exactly `0`. From
source, `declaredResponseLength` in `web/src/api/workspace.ts` would reject it as invalid.

Large responses have no length to pass on. Real responses of 4,819 B (`signals`) and
17,794 B (`keys`) reported exactly `0`, while the largest one seen with a length was 1,610 B.
This fits Go's server switching to chunked after about 2 KB. On the real Values screen, each
run waited 65 s first, made 13 API requests and got no 429s:

| Run | `values` response | Screen |
|---|---|---|
| No fix | 200, `content-length: 0` | "The values could not be loaded." |
| `fetch` wrapper that corrects the header, injected through devtools | 200, `content-length: 0` before the wrapper | Lists the keys |

The wrapper also kept streaming working, and returned a complete 8 MB body. It is not built
into the shim. It covers `fetch` only and loses `response.url`. Buffering non-streaming bodies
in the bridge is the other option, and is untested. Either way, **the host needs this before
the UI is usable on Android.** (Done on 2026-11-05: see the follow-up section.)

**Streaming.** The WebView holds small chunks back, although the bridge returns each one as
it arrives (from code). Five events, 800 ms apart, read through `fetch`:

| Padding per event | Arrival, ms after the first read began |
|---|---|
| None | 4008, 4009, 4009, 4009, 4009 (only when the stream ended) |
| 1 KB | 802, 803, 2407, 2407, 4010 |
| 4 KB to 128 KB | 0, 800, 1602, 2404, 3206 |

So a live event can wait indefinitely on a quiet stream. Padding each event with an SSE
comment of about 4 KB fixes the timing, and the cost stays inside the app. The proxy would
have to add it at event boundaries. (Built on 2026-11-05, with 2 KB.) The route was a test one, because the
real `/events` returned 404 from v1.0.0 to this newer SPA.

**Connection isolation.** Android requests carry no window identity, so the macOS window
check passes automatically here (`native_android.go` documents this as a shortcut). A request
that sends another connection's `Origin` is refused, and so is an unknown host. Two things
get through:

- A `no-cors` GET or an `<img>` from `conn-1` to `conn-0` sends no `Origin`, and the log shows
  it answered through `conn-0`'s proxy, which adds that connection's bearer. The page can't
  read the response. The test front sends no CSP, and a hostile instance controls its own
  CSP anyway.
- Script on `conn-0` can navigate the window to `conn-1`.

This breaks the guarantee that connections can't spend each other's sessions. A fix might be
one WebView per connection, or refusing a request whose page belongs to another host. Neither
is built or tested.

**The browser hop and the app freezer.** Android stopped running the backgrounded app about
72 s after the hop (Go ticks ended at 10:16:04 UTC, and `isFrozen=true`). A callback sent before
that was accepted at once. One sent while frozen waited for the user to come back:

| Case | Browser opened or loaded the callback | App accepted |
|---|---|---|
| Immediate | 10:14:52 | 10:14:54 |
| Frozen | 10:18:59 | 10:19:23, on returning to the app |

This is the same class of problem as Safari on iOS, with a longer grace period. Foreground
services were untested here and are measured in the follow-up section. Custom Tabs are untested. Reading `dumpsys` appears to thaw the app, so these
timings may be disturbed.

**What the test setup could not show:**

- **Browser half of sign-in.** The instance's `__Host-` cookies are `Secure`, and the test
  browser's cookie jar stayed empty on plain `http://127.0.0.1`, although TOTP returned 200.
  So password, TOTP and approval ran from the host, and only the callback ran in the emulator
  browser. A consent screen over HTTPS, Custom Tabs and passkeys are untested.
- **The environment matrix error in the UI.** `/projects/{id}/matrix` returned 404 and other
  calls 429. The first is this newer SPA against v1.0.0, and the second is the rate limit
  below. Neither points at the host. The matrix screen was seen working on 2026-11-05.
- **The test routes** for echo, streaming and download were in a scratch front. Since 2026-11-05
  they are in `spike/wails-shell/probes/front`.
- **The probe's `/nav`** answered but did not navigate on the sign-in page. I navigated through
  devtools instead and did not look into why.

From source only, in Wails' stock host:

- `WailsPathHandler.handle` always sends `GET`, and `WebViewAssetLoader` strips the query.
- `serveAssetForAndroid` uses a nil body and a recorder, so nothing streams and SSE can't
  work. Any status but 200 becomes an error.
- `application_android.go` says the WebView can't deliver `fetch()` POST bodies to
  `shouldInterceptRequest`. This is an Android limit, so any proxy needs a script that moves
  bodies over a bridge. That means the page gets an extra native path, which conflicts
  with D10's "no native capability beyond D9" unless the ADR lists it. The shim misses
  `sendBeacon`, native form POSTs and WebSocket. `FormData` through `fetch` passed above. A search of
  `web/src` found no `sendBeacon` or `new WebSocket`, and its one `<form method="post">` has a script
  submit handler (`TransitOperations.tsx`).

### Android follow-up, 2026-11-05

**Answer: the large-response blocker is fixed in the Java host, live events work with 2 KB of
padding, the system bars are fixed in the host, and a short foreground service does hold off the
freeze for up to three minutes.** Everything ran on the same emulator (`Pixel_9_API_36`,
Android 16, WebView 133, no Chrome), against a disposable instance whose server and SPA were
built from `origin/dev` at `d96cd9459`, not v1.0.0. The screens named below were observed on
the device. Real devices, Chrome, Custom Tabs and passkeys are still untested.

**1. Large responses.** The hypothesis holds: Chromium's WebView derives the page's
`Content-Length` from `InputStream.available()`, and `GoStream` returns 0. Evidence is the
pattern, not a one-line toggle. A response whose stream reports `available()` equal to its length
reads exactly that length. A chunked body over the cap, where `available()` is 0, reads `0`.

`SpikeBridge.java` now drops Go's `Content-Length` and `Transfer-Encoding` and buffers
non-streaming bodies up to 8 MiB (the SDK's own data ceiling), then serves them from a
`ByteArrayInputStream`, so the length is exact. Above the cap it streams the rest. A declared
length still reaches `available()`. With no declared length the header reads `0` and the body
is complete. `text/event-stream` is never buffered. Downloads take their own path.
A non-event streaming body (NDJSON, long polling) now waits for the whole body or 8 MiB before
the page sees anything. I did not check whether `web/` has one.

| Case (through `fetch`, nothing injected) | Header seen | Body |
|---|---|---|
| Declared or chunked, 100 B to 8 MiB (10 sizes, either side of 2 KB) | the exact length | complete |
| Declared, 8 MiB + 92 B and 20 MiB | the exact length | complete |
| Chunked, 8 MiB + 92 B and 20 MiB | `0` | complete (20 MiB in about 4 s) |
| Real `values` response, 7,718 B | `7718`, `response.url` kept | Values screen lists its keys |

The only case that still reads `0` is a chunked data response over 8 MiB, which the SDK rejects
anyway, since its ceiling is the same 8 MiB. I left it, rather than refuse the response, so a
large chunked download through `fetch` still works. The SDK check is unchanged. Regression matrix
re-run: POST bodies of 0 B, 1 B, 3 KB and 300 KB arrive with the right length and SHA-256,
statuses 200, 204, 404 and 500 pass, and a 302 still becomes 502 with `X-Spike-Redirect`. A
20 MiB download with a declared length took 628 ms. I did not call `whoami` on its own: the app
reached its signed-in screens on every launch, which needs it.

**2. Live events.** The `available()` work did not change the holdback, so padding was needed.
The WebView releases a stream in 2048-byte units:

| Padding per event | Five events 800 ms apart arrived at (ms) |
|---|---|
| None | 3216, all in one chunk |
| 256 B | 3211, one chunk |
| 1 KB | 806, 2410, 3211 |
| 1.5 KB and 1.9 KB | each event about 800 ms late |
| 2 KB and above | 4, 806, 1607, 2409, 3211 (on time) |

`native_android.go` now adds a 2 KB SSE comment after each blank line of a `text/event-stream`
response, on Android only. `/sse-pad?n=` in the probe changes it. On the real `/events` of the
demo project (the bootstrap project returns 404, a server property) the first padded chunk
arrived at 30 ms. That stream stayed quiet for 6 s, so I did not see a later live event on the
real route, only on the test route. The SDK's `/events` path wraps the response in
`boundedWorkspaceEvents` and never reads `Content-Length`, so the `0` the header shows on a
stream is harmless there.

**3. The freeze and a foreground service.** All times are from the Go tick log and the callback
accept line, with no `dumpsys` polling.

| Run | Result |
|---|---|
| No service | Ticks stopped 72 s after the hop. A callback typed into the browser at +169 s hung. It was accepted 3 s after I brought the app to the front, after a 166 s gap |
| Service, callback at +117 s | Ticks never stopped. Accepted within about 1 s, and the service stopped on the callback |
| Service, no callback for 260 s | The system's `onTimeout` fired 180.003 s after start, both overloads. The service stopped itself. Ticks continued for another 70 s, then stopped the same way. A callback sent after that hung until the app came to the front, then was accepted at once |

The service is Wails' own, switched to `shortService` and started just before the hop while the
app was visible, which the system allowed. What the user sees: with notification permission
granted, a silent "Signing in / Waiting for the browser" notification. With it denied, the
service still ran and held off the freeze, and the shade showed nothing. The host never
asked for the permission, and no prompt appeared in the screenshots I took.
Not measured: what the system does if `onTimeout` does not stop the service.

Verdict on the service: it covers a hop of up to three minutes with no server change. Past that,
or without it, the user returning to the app is the only trigger, and the callback is then
accepted at once. It adds a native capability and a visible notification, which D10's "no
native capability beyond D9" would have to list. That ADR point is flagged, not changed.

Verdict on an intent-delivered callback, read from source only: it needs a server change.
`validCLILoopbackRedirect` accepts only `http://127.0.0.1` or `[::1]` with a port and the path
`/callback`, in both `cli_login.go` and `cli_reauth.go`, and the stored redirect is checked
again before it is used (`cli_reauth.go:339` and `:407`). An App Link needs a verified
`assetlinks.json` on every instance origin, which a shipped app cannot assume for instances it
does not know, so it is not realistic. A custom scheme is possible with the existing PKCE, but
any app can claim it, and the server would have to accept it as a new redirect form. The
`device` transport of `cli-login` needs no callback at all, but `cli-reauth` has no such
transport. I did not test an intent itself. Bringing the app to the front by hand (which an
intent would do) accepted the waiting callback in 3 s.

**4. System bars.** The generated host already targets SDK 36, so Android 15's edge-to-edge
applies. `env(safe-area-inset-*)` read `0` on all four sides with `viewport-fit=cover` already
set, so the page cannot use it on this WebView. The host now applies the system bars, display
cutout and keyboard as padding on `main_container` (`MainActivity.patch`). On the environment
matrix the header sits below the status bar and the last row above the navigation bar. Not
verified: the keyboard (no soft keyboard appeared on the emulator when a field was tapped, and I
did not find out why) and a display cutout (the emulator image has none).

**Still untested:** a real device, Chrome and Custom Tabs, passkeys, the browser half of
sign-in over HTTPS, and a live event on the real `/events` route. The breadcrumb overlap and
the cut-off Values table belong to `web/`.

### A finding that also affects macOS

**A `cli` session can't finish recovery-code regeneration or the enrolment gate in the
SPA.** `useRecoveryCodeHandoff` (`web/src/api/account.ts`) calls `acceptAccountSession`,
which returns false unless the session's artifact is `browser`
(`web/src/app/AuthProvider.tsx`). The screen then throws "The account session changed
before its recovery codes could be displayed." This matches what iOS and Android showed: a 200
response and no progress at "Step 2 of 4". On Android the password field also cleared, and
no codes appeared. The thrown error was not captured there.

The cause is from source, and was not reproduced in a plain browser. Since the server has
already replaced the codes when the response arrives, the app may replace them without
showing the new ones. App mode must handle this, on every platform.

### What this means

- **iOS** looks feasible with `ASWebAuthenticationSession`, but the `http` deprecation makes
  the loopback handoff a dated choice there.
- **Android** needs its own Java host, a body bridge, a response-length fix, an SSE
  workaround, system-bar handling and a way to confine a window to its connection. It is not "the
  same shell". With the first four in (2026-11-05) the large-list screens work on an emulator.
  No real device, Chrome or passkey has been tried.
- **Neither** should change the desktop plan yet. The ADR's D15 already requires a separate
  spike per platform.
- **Not tested, worth comparing:** the instance's own web app in the phone's browser already
  runs passkeys on the real origin, with no proxy, bridge or second release lane.

**Review:** an automated security review flagged the debug server, and the fix is in: it
compiles only under the `probe` tag, needs a per-launch secret and never returns stored
values. Its absence from a normal build was checked by searching the Android library's
strings, not on a device. No independent review of this round has run, including the Android rerun.

## Rate limit: already fixed, not in v1.0.0

Both cookie and bearer sessions got their first 429 after 56 rapid requests on v1.0.0.
In a plain Chromium tab on the same instance, the default route made 94 API requests in
62 s, and 34 were 429. The shell does not cause this.

This is the known issue from the duplicate-polling work, not a new finding. `9732c1262`
stops live sessions spending the 60 per minute source allowance, and `786f1cd9e`,
`355e69049` and `6749314b9` cut the SPA's requests. None is in v1.0.0 or in
`nightly.20261102.103`, and all are in `nightly.20261104.106`. hikyo.dbugit.io now
reports `nightly.20261104.107`. I did not rerun the measurement.

## Evidence and limits

**Not tested:** a hosted instance or real TLS; SPKI pinning; a hardware passkey; the
loopback login, since v1.0.0's CLI refuses loopback http without its socket, so the
device endpoints were called directly and approved once in Chromium; OIDC, SAML; write flows beyond reveal, step-up and
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
