# Wails shell spike

Throwaway. Do not merge or ship. It tests option C1 from
[the decoupling report](../../docs/reports/server-webapp-decoupling-2026-11-04.md).
Results are in [the spike report](../../docs/reports/wails-shell-spike-2026-11-04.md).

One window per connection, on `wails://conn-N/`. Each request goes through an
in-process proxy to the instance, which serves its own SPA. The proxy adds the CLI
session as a bearer, so the page never holds the token.

## Run it against a hosted instance

Needs Go and Xcode command line tools, on macOS. Use a separate state directory:
a reveal or step-up rotates the session, and the spike keeps the new token only in
memory, so the copy on disk stops working. Your normal `hikyo` login stays safe this
way.

```sh
export HIKYO_STATE_DIR="$(mktemp -d)"
hikyo login https://your-instance.example --tls-mode system-ca --name spike
cd spike/wails-shell
go run -tags production . -instance spike
```

Expect a window with your instance's UI. Run `hikyo login` again after each run in
which you revealed a value. Pinned (`spki`) instances fail, since the spike uses
system CAs only.

## Known gaps

No sign-in, keychain, connection list or app bundle. See the spike report.

`webview_darwin.{go,m}` adds the navigation policy, download and popup handlers Wails
v3.1.0 lacks, to its delegate class at start-up. External links open in the system
browser; `HIKYO_SPIKE_BROWSER=print` logs them instead. Downloads go to
`HIKYO_SPIKE_DOWNLOAD_DIR`, else `~/Downloads`. Passkey reveals run through the system
browser: the SPA posts to `/__shell/reauth` (see `web/src/api/values.ts`), and the shell
does the CLI's loopback handoff.

The passkey path needs that SPA change on the instance, because the SPA comes from the
instance. On a released instance, "Use a passkey" still fails in the window. TOTP works.

## Mobile

Results are in the spike report's third round. Both platforms need a local, gitignored
`mobile-dev-config.json` with a disposable instance's origin and token.

- **iOS:** build with `GOOS=ios`, `-buildmode=c-archive`, the Wails overlay and
  `-tags production,ios`, then link for the simulator. Add `probe` to the tags for the debug
  server. Link `-framework AuthenticationServices`. Wails 3.1.0 has no UIScene lifecycle, so an app that records the
  iOS 27 SDK aborts at launch (`UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption`). Record SDK 26 with
  `vtool -set-build-version iossim 15.0 26.0 -replace -output X.new X`, then sign again.
- **Android:** run `android-host/build.sh`, or `android-host/build.sh stock` for the
  unmodified host. It needs `ANDROID_HOME` and the NDK. The patched host runs on the emulator.

## Connection isolation

Each connection gets its own renderer and a random host (D2), and a request's connection comes from the renderer that
sent it, never from the page. The Go shell refuses a request whose window does not own the host, one with no window,
a foreign `Origin`, and an unknown host, in that order. Results are in
[the isolation report](../../docs/reports/wails-connection-isolation-2026-11-05.md).

- **macOS:** one window each, the Go window check, and the native navigation policy in `webview_darwin.m`.
- **iOS:** the same policy, installed at start-up by `native_ios.go`, and a segmented control over the root view
  (`spikeInstallSwitcher`) that swaps which connection's view controller is in front. The views stay alive.
- **Android:** `android-host/java/Connections.java`. One `WebView` and one `Profile` per connection, the owner taken
  from the `view` argument, no `ServiceWorkerClient`, a body channel and download listener per view, and a second
  connection refused when `MULTI_PROFILE` is missing. `MainActivity.patch` only calls it. The patched host also refuses
  `/wails/*` and `/__capture__/*` and has no `wails` JavaScript interface, so instance code gets no native runtime.
  Removing a connection destroys its view and tries `ProfileStore.deleteProfile`. A profile still loaded in the process
  is deleted at the next launch.

The spike has no connection store, so hosts are new at every launch. A product keeps a connection's host with the
connection, which also keeps its page storage between launches.

## Isolation rig

Two scratch fronts with fake tokens play the instances, and `conn-0`'s front attacks `conn-1`. The victim front's log
is the result. The ports are 18500 to 18599.

```sh
go build -o /tmp/front ./isolation/front
A=c00000000000000000000000000000001; V=c00000000000000000000000000000002   # the rig names the hosts
/tmp/front -addr 127.0.0.1:18500 -name conn0-front -attack -victim wails://$V \
  -rig '{"run":"r1","thennav":"1","vh":"'$V'"}' -control 127.0.0.1:18502 -tls 127.0.0.1:18503 -log r1/attacker.jsonl &
/tmp/front -addr 127.0.0.1:18501 -name conn1-front -log r1/victim.jsonl &
python3 isolation/summarize.py r1 r1/attacker.jsonl r1/victim.jsonl r1/shell.log
```

Without the rig's `host` field, a connection's host is random, so the victim is only known after launch. With
`-tags isorig`, `mobile-dev-config.json` (and a CLI `sessions.json`) may carry `"host": "..."` per entry to fix it:
`{"conn0": {"origin": "http://127.0.0.1:18500", "token": "conn0-fake", "host": "<A>"}, ...}`. On Android the host is
`<A>.wails.localhost`. Rig keys: `run`, `thennav`, `only`, `skip`, `phase` (`sub`, `nav`, `race`), `n`, `vh`, `direct`, `wait`.

**macOS and iOS builds:** `isocontrol` turns the Go window check off, and `isonopolicy` turns the native policy off, on
both. These are controls: they show what each layer stops.

**Android:** build with `ISO_TAGS=isorig android-host/build.sh`, which swaps the empty `Rig.java` for `android-host/rig/Rig.java`.
`adb reverse` ports 18500 to 18503, then set the flags before launching:

```sh
curl -G --data-urlencode 'set=swprofile' http://127.0.0.1:18502/ctl/mode      # or: none
curl -G --data-urlencode 'cmd=switch 1' http://127.0.0.1:18502/ctl/push
adb logcat -v time IsoRig:I IsoGo:I Connections:I AndroidRuntime:E '*:S' > r1/shell.log
```

Flags, joined by `+`: `sw` (one `ServiceWorkerClient` for every profile, the gap), `swdeny`, `swprofile` (one client per
profile), `blocknet`, `csp` and `tlsok`. Commands: `load <url>`, `switch <host|index>`, `back`, `mem <label>`, `gc`,
`profiles`, `add <name>` and `remove <host|index>`. The old comparison modes (a shared view, a pinned connection) were
removed with the product code; commit c842958c9 has them.

A refused top-level navigation replaces the attacker's page and stops the chain, so run later vectors with `phase=nav` and `n=K`.

## Tests and probes

`go test -race -tags production .` covers the proxy's refusals, a request with no owner, a page-supplied window id,
connections added and removed while serving, random hosts, token rotation, the reauth handoff, URL validation and
download paths.

`-probe FILE` runs JavaScript after each navigation and enables `/__spike/report`
and `/__spike/sleep`. The files in `probes/` produced the report's evidence. Replace
`__ORG__`, `__PROJECT__`, `__ENV__` and `__TOTP_SECRET__` first (`downloads.js` also wants a second `-instance`), and use them only on
a disposable instance: `hardening.js` overwrites the clipboard.
