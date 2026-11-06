# Connection isolation in the native shell

Date: 2026-11-05. Rig code: [`spike/wails-shell`](https://github.com/Hikyo-Org/Hikyo/tree/5dbac35e4f01a980d4beb0f107ed32de4706bd5f/spike/wails-shell). Follows the
[spike report](./wails-shell-spike-2026-11-04.md). The rig code lives on its own branch and never merges. The recommendation is now implemented in the spike (see [Implementation results](#implementation-results)); no product code exists, and `desktop/` is not created.

## Recommendation

**Use one renderer per connection, and take a request's connection from that renderer, never from
anything the page sends.** Requests aimed at another connection are refused, and native code
cancels navigations to another connection.

| Platform | What that means | New work |
|---|---|---|
| macOS | Already true. The Go window check stops subresources and the native policy stops navigation. Each was shown to be needed by turning it off | None beyond plan PR 3, plus locking on the window map when connections are added at runtime |
| iOS | Identity is already true. The navigation policy exists but only a debug route installs it | Install the policy at startup, and add a real view switcher |
| Android | Not true today. Requests carry no window, so the check passes automatically | One `WebView` and one `Profile` per connection, no shared `ServiceWorkerClient`, a per-view body channel |

Use D2's random hosts on every platform as defence in depth. They stop guessing a host, not using
a known one.

**Why this one:** in one-view mode, zero of the attack table's requests reached the other connection,
including under a 1 ms request flood during a switch. Recording the "current" connection in one
WebView leaked 6 to 14 authenticated requests in each of three switches. Headers can't work: no
field identifies the sender except `Referer`, which the page can remove.

**Cost on Android:** about 35 MB more with two connections on the login page (255 MB against 220 MB), and
native host work that the plan doesn't size. Switching gets faster (11 to 37 ms against 38 to 184 ms), and each connection keeps its page state.

**Implemented and measured.** The recommendation is built in `spike/wails-shell` and checks 1 to 6 and 8 ran on an Android emulator, the macOS shell and an iOS simulator. Check 7 (authenticated SPA) did not run. See [Implementation results](#implementation-results) and [Not measured](#not-measured).

## Severity

All of this concerns a hostile or compromised instance attacking a second connection in the same app.

- **Navigation is the larger harm.** On Android today, script on `conn-0` landed the window on `conn-1`, which ran with
  `conn-1`'s session. The window can then run the honest SPA through deep links while the user believes
  they are looking at the first instance.
- **The blind request is smaller, but real.** The page can't read the response, but it does learn the status
  (`onload` for 200, `onerror` for 404), and each request spends rate limit and writes server logs.
  Some GETs have side effects, read in `api/openapi.yaml` and not run: the four audit exports record
  `audit.export_started` before the first byte (`x-hikyo-artifacts: [human-session]`), and
  `getInstanceUpdateJob` can reconcile an audit outcome.

## How it was tested

Two scratch fronts with fake tokens played the instances, and `conn-0`'s front served the attack. **The
result is what the victim front logged**, not what the page reported. Every one of the 7848 attack
requests that reached a victim front carried `conn1-fake`, in every mode. No real instance, real
session or physical device was used.

| Platform | Setup |
|---|---|
| macOS | WKWebView, Wails v3.1.0, built with and without the check (`isocontrol`) and the policy (`isonopolicy`) |
| iOS | Simulator, iPhone Duo, iOS 27.1 |
| Android | Emulator, Pixel_9_API_36, Android 16, WebView 133.0.6943.137, `MULTI_PROFILE` supported |

## Question 1: is the blind request possible on macOS and iOS?

**No, as shipped: zero requests reached the victim front on either platform. Yes, once the check is off.**

- **macOS:** subresources were refused by the window check, or by the `Origin` check for CORS types.
  Navigations and frames were cancelled by the native policy.
- **iOS:** the window check refused every subresource, and every navigation that reached it. Without the
  policy the refusal replaces the attacker's own page with a 403. With the policy installed the
  navigation is cancelled, and the page survived on the five vectors whose timing could show it.
- **The window check is what protects them.** With it off, both platforms sent `<img>`, `no-cors` fetch and the
  rest with `conn1-fake`. With the policy off too, script navigation landed on `conn-1`.
- **`Origin` isn't enough on WebKit.** `fetch-post-no-cors` and the CSP report are POSTs with no `Origin`. They reached
  the victim once the window check was off. Chromium does send `Origin` on those.

## Question 2: what reaches the proxy across connections?

A bold cell means the victim front received that many requests, all with `conn1-fake`.

| Cell | Meaning |
|---|---|
| `window` | Refused by the window check |
| `origin` | Refused by the `Origin` check |
| `host` | Refused as an unknown host |
| `cancel` | Cancelled by the native navigation policy |
| `none` | The vector fired, nothing reached the victim, and no refusal was logged |
| `-` | Not run in this column |

**Reading notes:**

- Counts sum across runs, and some vectors fire once per run, so compare zero against non-zero.
- `csp-report` and `link-header` fire on every page load, so they show up in runs meant for other vectors.
- iOS policy-on navigations show `none` because the summarizer doesn't parse iOS native cancels.
  Zero hits is still the measurement.
- `Android view + profile` is `-` for navigations because only the subresource phase ran in that mode.
- Pinned and drain lack `meta-refresh`, `iframe-top` and `nav-attach`, because a refused `form-post-top` replaces the
  attacker's page and stopped the chain.
- `sw-own` isn't a request across connections. It is covered under [Service workers](#service-workers).

| Vector | macOS: check and policy | iOS: check, no policy | iOS: policy on | Android today | Android one view | Android view + profile | Android pinned | Android drain |
|---|---|---|---|---|---|---|---|---|
| `a-blank` | cancel | - | - | - | - | - | - | - |
| `a-click` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `a-download` | cancel | - | - | **1 GET** | cancel | - | - | - |
| `a-ping` | - | - | - | origin | origin | origin | origin | origin |
| `a-top` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `audio` | window | - | - | **1 GET** | window | window | window | window |
| `beacon` | none | none | - | origin | origin | origin | origin | origin |
| `csp-report` | window | window | window | origin | origin | origin | origin | origin |
| `css-import` | window | window | - | **2 GET** | window | window | window | window |
| `css-url` | window | window | - | **2 GET** | window | window | window | window |
| `embed` | cancel | window | - | **2 GET** | window | window | window | window |
| `eventsource` | origin | origin | - | origin | origin | origin | origin | origin |
| `fetch-cors` | origin | origin | - | origin | origin | origin | origin | origin |
| `fetch-keepalive` | window | window | - | **2 GET** | window | window | window | window |
| `fetch-no-cors` | window | window | - | **2 GET** | window | window | window | window |
| `fetch-no-cors-noreferrer` | window | window | - | **2 GET** | window | window | window | window |
| `fetch-post-no-cors` | window | window | - | origin | origin | origin | origin | origin |
| `font` | window | window | - | origin | origin | origin | origin | origin |
| `form-get-iframe` | cancel | window | - | **1 GET** | window | window | window | window |
| `form-get-top` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `form-post-iframe` | cancel | origin | - | origin | origin | origin | origin | origin |
| `form-post-top` | cancel | origin | none | origin | origin | - | origin | origin |
| `guess-unknown-host` | host | host | - | host | host | host | host | host |
| `iframe` | cancel | window | - | **2 GET** | window | window | window | window |
| `iframe-attach` | cancel | window | - | **4 GET** | window | window | window | window |
| `iframe-hard` | cancel | window | - | **2 GET** | window | window | window | window |
| `iframe-sandbox` | cancel | window | - | **2 GET** | window | window | window | window |
| `iframe-sandbox-post` | - | origin | - | origin | origin | origin | origin | origin |
| `iframe-top` | cancel | window | none | **1 GET** | cancel | - | - | - |
| `img` | window | window | - | **2 GET** | window | window | window | window |
| `img-cors-attr` | origin | origin | - | origin | origin | origin | origin | origin |
| `img-noreferrer` | window | window | - | **2 GET** | window | window | window | window |
| `import-dynamic` | origin | origin | - | origin | origin | origin | origin | origin |
| `input-image` | window | window | - | **2 GET** | window | window | window | window |
| `link-header` | window | window | window | **5 GET** | window | window | window | window |
| `link-modulepreload` | origin | origin | - | origin | origin | origin | origin | origin |
| `link-prefetch` | - | - | - | **2 GET** | window | window | window | window |
| `link-preload` | window | window | - | **2 GET** | window | window | window | window |
| `link-stylesheet` | window | window | - | **2 GET** | window | window | window | window |
| `location-assign` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `location-href` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `location-replace` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `meta-refresh` | cancel | window | none | **1 GET** | cancel | - | - | - |
| `nav-attach` | cancel | window | none | **2 GET** | cancel | - | - | - |
| `object` | cancel | window | - | **2 GET** | window | window | window | window |
| `oracle-hard-img-200` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-hard-img-404` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-hard-script-200` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-hard-script-404` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-img-200` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-img-404` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-script-200` | window | window | - | **2 GET** | window | window | window | window |
| `oracle-script-404` | window | window | - | **2 GET** | window | window | window | window |
| `redir-direct` | none | none | - | none | none | none | none | none |
| `redir-direct-iframe` | - | - | - | none | none | none | none | none |
| `redir-direct-nav` | - | none | - | none | - | - | - | - |
| `redir-own-nav` | none | none | none | none | none | - | - | - |
| `redir-own-proxy` | none | none | - | none | none | none | none | none |
| `script` | window | window | - | **2 GET** | window | window | window | window |
| `sharedworker-fetch` | - | - | - | none | none | none | none | none |
| `spoof-window-id-cross` | origin | origin | - | origin | origin | origin | origin | origin |
| `spoof-window-id-own` | none | none | - | none | none | none | none | none |
| `svg-image` | window | window | - | **2 GET** | window | window | window | window |
| `sw-cross-origin` | none | none | - | none | none | none | none | none |
| `sw-own` | none | none | - | none | none | none | none | none |
| `video` | window | - | - | **2 GET** | window | window | window | window |
| `websocket` | - | - | - | none | none | none | none | none |
| `window-open` | none | none | - | none | none | none | none | none |
| `window-open-self` | cancel | window | none | **1 GET** | cancel | - | cancel | cancel |
| `worker-cross-origin` | none | none | - | none | none | none | none | none |
| `worker-fetch` | window | window | - | **1 GET** | window | window | window | window |
| `xhr` | origin | origin | - | origin | origin | origin | origin | origin |

Controls, showing only vectors that reached the victim in at least one:

| Vector | macOS: check off | macOS: check and policy off | iOS: check and policy off |
|---|---|---|---|
| `a-click` | cancel | **1 GET** | **1 GET** |
| `a-top` | cancel | **1 GET** | **1 GET** |
| `audio` | **3 GET** | - | - |
| `csp-report` | **17 POST** | **1 POST** | **23 POST** |
| `css-import` | **1 GET** | - | **1 GET** |
| `css-url` | **1 GET** | - | **1 GET** |
| `embed` | cancel | **1 GET** | **1 GET** |
| `fetch-keepalive` | **1 GET** | - | **1 GET** |
| `fetch-no-cors` | **1 GET** | - | **1 GET** |
| `fetch-no-cors-noreferrer` | **1 GET** | - | **1 GET** |
| `fetch-post-no-cors` | **1 POST** | - | **1 POST** |
| `font` | **1 GET** | - | **1 GET** |
| `form-get-iframe` | cancel | none | **1 GET** |
| `form-get-top` | cancel | none | **1 GET** |
| `iframe` | cancel | **1 GET** | **1 GET** |
| `iframe-attach` | - | - | **1 GET** |
| `iframe-hard` | cancel | **1 GET** | **1 GET** |
| `iframe-sandbox` | cancel | **1 GET** | **1 GET** |
| `img` | **1 GET** | - | **1 GET** |
| `img-noreferrer` | **1 GET** | - | **1 GET** |
| `input-image` | **1 GET** | - | **1 GET** |
| `link-header` | **1 GET** | **1 GET** | **6 GET** |
| `link-preload` | **1 GET** | - | **1 GET** |
| `link-stylesheet` | **1 GET** | - | **1 GET** |
| `location-assign` | cancel | **1 GET** | **1 GET** |
| `location-href` | cancel | **1 GET** | - |
| `location-replace` | cancel | **1 GET** | **1 GET** |
| `meta-refresh` | cancel | - | **1 GET** |
| `object` | cancel | **1 GET** | **1 GET** |
| `oracle-hard-img-200` | **1 GET** | - | **1 GET** |
| `oracle-hard-img-404` | **1 GET** | - | **1 GET** |
| `oracle-hard-script-200` | **1 GET** | - | **1 GET** |
| `oracle-hard-script-404` | **1 GET** | - | **1 GET** |
| `oracle-img-200` | **1 GET** | - | **1 GET** |
| `oracle-img-404` | **1 GET** | - | **1 GET** |
| `oracle-script-200` | **1 GET** | - | **1 GET** |
| `oracle-script-404` | **1 GET** | - | **1 GET** |
| `script` | **1 GET** | - | **1 GET** |
| `svg-image` | **1 GET** | - | **1 GET** |
| `video` | **3 GET** | - | - |
| `window-open-self` | cancel | **1 GET** | **1 GET** |
| `worker-fetch` | **1 GET** | - | **1 GET** |

**Details the table can't show:**

- **Redirects.** The proxy never follows one. `redir-own-proxy` produced no request to `conn-1` anywhere. On Android, 3xx becomes a 502
  because `WebResourceResponse` rejects it. On macOS, `redir-direct` reached the attacker's direct front with no bearer, then failed. On
  Android, cleartext to loopback is blocked, so the HTTPS hop wasn't measured.
- **Popups.** `window.open` returned `null` on all three. `a-blank` ran only on macOS, where the policy cancelled it. On Android the source hands the URL to
  a throwaway view that calls `openExternal`, with no bearer. That is read, not measured.
- **Dead vectors.** `a-ping` sent nothing on WebKit, and on Android it is a POST with `Origin`, so it was refused. `beacon` throws on WebKit. `websocket`
  errored on Android, and `worker-cross-origin` and `sw-cross-origin` threw. `SharedWorker` is undefined on Android. Spoofing `x-wails-window-id` didn't help: the shell's value wins, and a cross-connection spoof needs a preflight, which is refused.
- **Status oracle.** Android today gives `onload` for 200 and `onerror` for 404, including with `CORP: same-origin` on the response (`oracle-hard-*`).
- **Downloads.** `a-download` and `nav-attach` went through `conn-1`'s proxy on Android today. One-view mode cancelled both.

### Service workers

These only exist on Android here. macOS rejects registration because the scheme isn't HTTP or HTTPS, and on iOS `navigator.serviceWorker` is absent.

| Android setup | Registration | Reached victim |
|---|---|---|
| No client (today, one view, view + profile, blocked network, shell CSP) | Rejected, script fetch fails | 0 |
| Today + shared `ServiceWorkerClient` | Registered | **2, `conn1-fake`** |
| One view + shared client | Registered | **2, `conn1-fake`** |
| One view + blocked network loads + shared client | Registered | **2, `conn1-fake`** |
| View + profile + per-profile client bound to its connection | Registered | 0, refused by the window check |

**One WebView per connection plus a shared client reopens the gap**, because `ServiceWorkerClient` has no view argument. The SPA has no service worker
use (`rg serviceWorker web/src` finds none), so installing no client costs nothing.

## Question 3: which signals could identify the sender?

**None can, on Android. Only the view argument does.** Logged at the callback, and by the Go shell.

| Signal | macOS, iOS | Android | Can the page suppress it? |
|---|---|---|---|
| `Origin` | Absent on `no-cors` GET and POST, and on CSP reports | Absent on GET. Present on POST and CORS | Its absence is the attacker's choice of request type |
| `Referer` | Present by default | Present by default | **Yes.** `referrerpolicy="no-referrer"` removes it, measured with `img-noreferrer` and `fetch-no-cors-noreferrer` |
| `Sec-Fetch-Site`, `Sec-Fetch-Dest` | Never seen | Never seen, in 343 logged requests | Not applicable, they aren't there |
| `Sec-Fetch-Mode` | Never seen | Once, as `cors`, on one preflight, logged by both Java and Go | Not applicable |
| `isForMainFrame` | Not logged | True for top-level navigation | Describes the frame, not the sender |
| `hasGesture` | Not logged | False, including for a scripted click | Same. A real tap wasn't tested |
| `x-wails-window-id` | Set per handler, and the page's value is overwritten | Absent from Wails. The rig's Go code sets it from the view | No |
| `view` argument | Not applicable | The renderer's identity. The service worker callback has none | No |

## Question 4: candidates

| Candidate | Verdict | Evidence |
|---|---|---|
| **One WebView and Profile per connection** | **Recommended** | 0 hits on subresources. 2 sandboxed renderers. Service worker refused either way. Its own navigation and race runs are not done |
| One WebView per connection, shared profile | **Works, weaker** | 0 hits on subresources and navigation, 0 in the race. Shares a renderer. A shared service worker client reopens the gap. No per-connection delete. Good fallback if profiles prove hard |
| One WebView, pinned to the current connection | **Rejected** | The race leaked 6, 14 and 12 authenticated requests during a switch |
| Pinned, with drain to a blank page first | **Rejected** | 0, 0, 0 in the race, but one connection at a time. That loses SPA state and SSE, and conflicts with acceptance item 10 "Two connections open together stay isolated" |
| Refuse by request headers | **Rejected** | See question 3 |
| Unguessable hostnames (D2) | **Keep, as defence in depth** | `conn-7` was refused as unknown everywhere. A hostile instance that learns a host through a shared link or a log isn't stopped |
| CORP and other response headers | **Rejected as a control** | The request is sent before any response exists, and `oracle-hard-*` still reached the victim and leaked status |
| CSP added by the shell | **Optional second layer** | With the rig's CSP on HTML only, only `worker-fetch` reached the victim. The all-responses variant wasn't run. It doesn't stop top-level navigation (first spike round) |
| `setBlockNetworkLoads` | **Optional second layer** | Pages still load through the intercept. It didn't stop the shared-client leak. Direct egress wasn't measured |
| One process per connection (`setDataDirectorySuffix`) | **Not tested** | Read only. Heavier, and profiles give separate renderers without it on this version |

### Race during a switch

The attacker page fired an image every 1 ms, native code switched away, then went back. Three runs each. A hit is an authenticated request at the victim front.

| Mode | Hits during the switch | Hits after going back |
|---|---|---|
| Today | 13 to 25 (and about 1400 to 1600 before it) | About 1000 |
| Pinned | 6, 14, 12 | 0 |
| Pinned with drain | 0, 0, 0 | 0 |
| One view | 0, 0, 0 (3000+ refused) | 0 |

In one-view mode the hidden attacker view keeps running, and its requests stay attributed to it. Pinned fails because the recorded connection changes before the old page dies, and `pagehide`, `visibilitychange` and `keepalive` requests fire in that window. The bfcache never restored a page in these runs (`persisted=false`).

### Switching time

Real SPA login page, fake fronts, in ms. Cached SPA on the second connection, so these are lower bounds for the reload modes.

| Mode | Time | State |
|---|---|---|
| One view (visibility flip) | 13 to 36 | Kept |
| View + profile (visibility flip) | 11 to 37 | Kept |
| Today (reload) | 38 to 184 | Lost |
| Pinned (reload) | 41 to 176 | Lost |
| Pinned with drain (reload) | 43 to 157 | Lost |

The second connection's first load took 627 to 1024 ms.

### Memory

Real SPA login page in each view, read with `dumpsys meminfo`, three runs. App PSS includes the embedded Go. An authenticated SPA will use more, so these are lower bounds.

| Mode | Renderers | Renderer MB | App MB | Sum MB | Added |
|---|---|---|---|---|---|
| One WebView | 1 | 99 to 100 | About 126 | 220 to 221 | |
| One view per connection | 1, shared | 113 to 115 | 129 to 130 | 237 to 238 | 17 |
| View + profile | 2, sandboxed | About 68 each | About 125 | 255 to 256 | 35 |

### Storage

Origins already separate `localStorage` in every mode, and the first spike round measured that on macOS. Profile mode reports `conn-0` and `conn-1` as separate profiles. On-disk separation and deletion weren't measured.

## What the fix stops and what it doesn't

| | |
|---|---|
| **Stops** | Blind requests spending another connection's session. Script, frame and form navigation to another connection. A service worker reaching another connection. Spoofing the window id |
| **Doesn't stop** | A hostile instance spending **its own** session, or misleading the user inside its own window. A page sending requests to other hosts: that is the instance's CSP (D15), carries no bearer, and the shell's optional CSP would only narrow it |
| **Limits** | A compromised renderer is out of scope. Separate renderers are a bonus measured on WebView 133, not a contract to rely on, and Chromium says WebView "currently only supports a single renderer process in most cases" |

## Decisions for Marc

The recommendation changes no ADR decision. It matches D2, D4 ("an unbound request fails closed") and D10. Three points need a decision if mobile goes ahead, and I haven't amended anything:

1. **D15 names macOS first and doesn't mention iOS or Android.** Mobile needs an ADR addition, and this report can be its evidence.
2. **D2 deletes storage "together" with the rest.** The docs say `deleteProfile` throws while the profile is loaded and may finish later. On Android, deletion may have to wait for the next launch. D2's never-reused identifiers make that safe, because a new connection can't inherit an old profile.
3. **The body channel still conflicts with D10's "no native capability beyond D9"**, as the spike report said. Whatever is decided, it must be per view.

## Implementation brief

This stands alone. The spike never merges, so port the behaviour into the product shell. The plan's PR 3 creates `desktop/`.

### All platforms

- Keep the shell's check order: unknown host, foreign `Origin`, window mismatch, then the proxy. See `shell()` in `spike/wails-shell/main.go`.
- Refuse any request with no window identity. This is D4.
- Use D2's random hosts. The mapping from window to host must exist before the first load.
- `windowHosts` in `main.go` is a plain map read by goroutines. Adding connections at runtime needs a lock, or a data race follows.

### macOS

No new control. Keep the window check and the native policy (`webview_darwin.m`), and do the plan's startup check that refuses to open a window if the handlers are missing.

### iOS

- Install the policy at startup. `native_ios.m` has it, but today only `POST /policy` behind the `probe` tag turns it on.
- Add a real switcher. `ios_create_webview_with_id` makes only the first view controller the root.
- Test `a-download` with the policy on. Without it, the link replaced the page.

### Android

| File in the spike | What to carry over |
|---|---|
| `android-host/java/IsoRig.java` | The per-view parts only: `setProfile` before the first navigation, `views` map, `owner()`, `allowNavigation()`, the visibility switch. Drop modes, control port and logging |
| `android-host/MainActivity.patch` | `installView(v)` with a per-view body channel and download listener, and `saveDownload(source, ...)` with the owner. Back acts on the active view |
| `android-host/java/SpikeBridge.java` | `fetch(request, owner)`, `download(url, out, owner)` |
| `native_android.go` | Delete any page-supplied `x-wails-window-id`, **then** set it from the owner. No match fails closed |
| `android-host/build.sh` | `androidx.webkit` 1.12.1 or newer |

**Behaviour:**

1. Each connection gets one `WebView` and one `Profile`, with `WebViewCompat.setProfile` called before anything else touches the view. It throws afterwards.
2. `shouldInterceptRequest(view, req)` takes the owner from `view`. A view not in the map gets no owner, and is refused.
3. `shouldOverrideUrlLoading` cancels any main-frame navigation to a connection host other than its owner, and hands `https` links to the D9 and D10 path. `loadUrl` skips this callback, so native switches never use it to cross connections: they flip visibility.
4. POST navigations don't reach `shouldOverrideUrlLoading`. Measured: `form-post-top` reached the intercept, the `Origin` check refused it, and the 403 replaced the attacker's page. That only harms the attacker, so accept it.
5. Install no `ServiceWorkerClient`. If the product ever needs one, bind one per profile. Never use the shared controller.
6. If `MULTI_PROFILE` isn't supported, refuse a second connection rather than silently sharing a profile.
7. On removal, destroy the view and try `ProfileStore.deleteProfile`. If it throws, record it and delete at the next launch before any view exists.

### Tests

- Unit: `spike/wails-shell/main_test.go` already covers the proxy's refusals. Add: a request with no owner is refused, a page-supplied window id is overwritten, and adding a connection concurrently is safe under `-race`.
- Driven: rerun the attack table with the rig. See `spike/wails-shell/README.md`.

### Acceptance checks

Ground truth is the victim front's log. Each must show zero attacker-originated requests at the victim.

1. **Run the navigation phase in the per-profile mode.** Every vector in `nav` must be cancelled, and the page must stay, except `form-post-top`. This is the unmeasured half of the recommendation.
2. Run the race in the per-profile mode, three times.
3. Run the subresource phase on the product host.
4. Register a service worker from `conn-0`. It must be rejected.
5. HTTPS redirect hop: redirect from an outside HTTPS page to `conn-1`, as `redir-tls-img`, `redir-tls-fetch` and `direct-tls-img` do. Expect no request at the victim.
6. Remove a connection, add a new one, and confirm the old storage is gone, including after a restart.
7. Authenticated SPA: confirm SSE and reveal keep working across switches, and re-measure memory.
8. On macOS and iOS, rerun the table with random hosts, and repeat on a real device before release.

## Implementation results

The brief is implemented in `spike/wails-shell` (branch `dunky13/shell-connection-isolation`, stacked on the spike). It stays out of `dev`. Ground truth for every row is the victim front's log, read by `isolation/summarize.py`. The rig fixes the hosts through the `host` field under the `isorig` tag; the product path uses random hosts.

| # | Check | Result | How |
|---|---|---|---|
| 1 | Navigation phase, per-profile mode (Android) | **0 requests at the victim**, 4 runs. Every `nav` vector to the victim was cancelled and the page stayed, except `form-post-top` (not seen by `shouldOverrideUrlLoading`; the Go `Origin` check refused it, and the 403 replaced the attacker's page), `redir-own-nav` (stays on the attacker's `/redir`; the 3xx becomes a 502) and the cleartext `redir-direct-nav` (page stayed) | Measured |
| 2 | Race, three runs | **0 victim hits** in each, about 15,000 to 17,000 refused per run, 4 switches each | Measured |
| 3 | Subresource phase on the product host | **0 reached the victim**, about 48 vectors | Measured |
| 4 | Service worker from `conn-0` | Rejected: `sw-own` "unknown error occurred when fetching the script" (no `ServiceWorkerClient` is installed), `sw-cross-origin` SecurityError | Measured |
| 5 | HTTPS redirect hop (`tlsok`) | `redir-tls-img` onerror, `redir-tls-fetch` rejected, `direct-tls-img` onload (the attacker's own redirector). **0 at the victim** | Measured |
| 6 | Remove, add, restart | `deleteProfile` on a loaded profile was postponed ("Cannot delete in-use profile") and recorded. A new connection saw `seen=null`, where a same-host reload had seen the old value. After a restart the old profiles were deleted and `profilesToDelete` was empty | Measured |
| 7 | Authenticated SPA: SSE, reveal, memory | **Not run.** See below | |
| 8 | macOS and iOS with random hosts | macOS: native cancels for navigations and `refused-window`, `refused-origin` or `refused-host` for subresources, **0 at the victim**; the control build with both layers off leaked 24 vectors, so the rig can see a leak. iOS (iPhone Duo simulator, iOS 27.1): 33 subresource vectors, **0 at the victim**, 21 native cancels, `a-download` cancelled with the policy on, and the switcher confirmed by screenshots and the `switched to connection N` log | Measured |

Also measured: downloads on Android land in `Download/<connection label>/` with unique names (`rig.bin`, `rig-1.bin`). `wails-runtime` and `wails-capture` return 403 on Android and `typeof window.wails` is `undefined`. On iOS `wails-capture` returned 404 before the fix below and is rejected after it. On macOS `window.wails` is an object (the Wails runtime is in the page) and `wails-capture` is 404 from the instance.

The branch also merged `dunky13/android-host-response-fixes` (large responses, 2 KB event padding, system bars, foreground service). After the merge, the subresource and navigation phases were rerun on the merged APK: 0 victim hits.

### Independent review

A fresh `claude-opus-5-5` agent reviewed the code against the attack table and found no cross-connection break. Findings and dispositions:

| Finding | Severity | Disposition |
|---|---|---|
| Body-channel stash leaked accounting across owners | Medium | Fixed: keyed by owner and id, per-owner cap. Read in source, not run |
| Android download could overwrite another connection's file | Medium | Fixed and measured (per-connection folder, unique names) |
| iOS served `/__capture__/` before Go's checks | Medium | Fixed and measured |
| Wails' WebKit message handlers (`external`, `wails:drag`) are reachable from a page | Low | Upstream; documented, no change |
| macOS native policy install was not fatal | Low | Fixed: start-up aborts unless all five handlers install |
| macOS download connection comes from the URL | Low | No bypass, because `webView.URL.host` is checked first; documented |
| Profiles of removed connections piled up | Low | Fixed: orphan sweep at launch |
| `probe` debug server always built on Android | Low | Fixed: only with `ISO_TAGS=probe` |

Unconfirmed, so residual: the native policies allow an empty `webView.URL.host` on a first load, and shared workers were not tested on Android.

### Differences from the brief

- Hosts are new at every launch, because the spike has no connection store. A product keeps a host with its connection, which also keeps page storage between launches.
- A connection added at runtime must be bound to its window before its first load.
- The comparison modes (shared view, pinned connection) are gone from the code. Commit `c842958c9` has them.
- iOS 27.1 aborts apps without a UIScene lifecycle, which Wails 3.1.0 lacks. The simulator build records SDK 26 with `vtool` and links `AuthenticationServices`.

## Not measured

| Item | Status |
|---|---|
| **Check 7: SSE and reveal across switches, memory with an authenticated SPA** | **Not run.** A disposable instance built from `origin/dev` ran and served its SPA, but `hikyo login` for a session token requires an interactive terminal so a human can confirm the instance. It has no file output and I did not fake a terminal. The merged event padding was checked in source and by the subresource runs only. Memory with an authenticated SPA is not re-measured |
| Isolation with the native policy off on iOS (`isonopolicy`) | No run |
| A real device, on any platform | Not run |
| `a-blank` on Android | Not run. macOS and iOS cancelled it |
| A real user tap on Android, where `hasGesture` may differ | Not run |
| `WebView.onPause` on hidden views | Not run |
| The Android extra vectors (`wails-runtime`, `wails-capture`) before the change | Not measured |
| Body-channel stash logic (review fix) | Read in source, not run |
| `sharedworker-fetch` on macOS and iOS | No request reached the victim; no page result recorded |
| Shell CSP on all responses, `setBlockNetworkLoads`, deny-all `ServiceWorkerClient` | Not needed by the recommendation, not run |

**To run check 7:** start a disposable instance, run `hikyo login <url> --local --as admin --name connN` twice from a terminal against it, put both origins and tokens into the gitignored `mobile-dev-config.json`, build with `ISO_TAGS=isorig android-host/build.sh`, and use the rig commands (`switch`, `mem`) while the SPA holds an event stream.

## Read in source, not measured

- [Chromium process model](https://chromium.googlesource.com/chromium/src/+/main/docs/process_model_and_site_isolation.md): "two documents from different profiles or StoragePartitions can never share the same renderer process." It also says WebView "currently only supports a single renderer process in most cases."
- [`WebViewCompat`](https://developer.android.com/reference/androidx/webkit/WebViewCompat): `setProfile` "should be called before doing anything else with WebView other than attaching it." It throws if the view has navigated. Renderers "may be shared with other WebViews in the application."
- [`ProfileStore`](https://developer.android.com/reference/androidx/webkit/ProfileStore): `deleteProfile` throws with living WebViews, or for a profile loaded in memory. "Some data may be deleted async and is not guaranteed to be cleared from disk by the time this method returns."
- [`Profile`](https://developer.android.com/reference/androidx/webkit/Profile): "each profile holds its own set of data."
- [`ServiceWorkerClient`](https://developer.android.com/reference/android/webkit/ServiceWorkerClient): `shouldInterceptRequest` has no `WebView` argument.
- [`WebViewClient`](https://developer.android.com/reference/android/webkit/WebViewClient): `shouldOverrideUrlLoading` isn't called for `loadUrl`.
- Chromium main, `android_webview/browser/network_service/aw_proxying_url_loader_factory.cc`: redirected requests aren't offered to the intercept, and service worker requests use the browser-context client.
- Wails v3.1.0, `webview_window_darwin.m` and `webview_window_ios.m`: one scheme handler per view, with its window id. `Header()` uses `Set`, so a page's value is overwritten.
- `internal/server/spa.go`: an honest instance already sends a strict CSP, `CORP: same-origin` and `frame-ancestors 'none'`. A hostile instance doesn't have to, which is why none of it counts here.
