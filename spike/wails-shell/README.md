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
  server.
- **Android:** run `android-host/build.sh`, or `android-host/build.sh stock` for the
  unmodified host. It needs `ANDROID_HOME` and the NDK. The patched host has never run.

## Tests and probes

`go test -tags production .` covers the proxy's refusals and token rotation, the reauth
handoff, URL validation and download paths.

`-probe FILE` runs JavaScript after each navigation and enables `/__spike/report`
and `/__spike/sleep`. The files in `probes/` produced the report's evidence. Replace
`__ORG__`, `__PROJECT__`, `__ENV__` and `__TOTP_SECRET__` first (`downloads.js` also wants a second `-instance`), and use them only on
a disposable instance: `hardening.js` overwrites the clipboard.
