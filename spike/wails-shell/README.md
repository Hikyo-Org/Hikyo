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
  unmodified host. It needs `ANDROID_HOME` and the NDK. The patched host runs on the emulator.

## Isolation rig

Results are in [the isolation report](../../docs/reports/wails-connection-isolation-2026-11-05.md).
Two scratch fronts play the instances, with fake tokens, and `conn-0`'s front attacks `conn-1`.
The victim front's log is the result.

```sh
go build -o /tmp/front ./isolation/front
/tmp/front -addr 127.0.0.1:18200 -name conn0-front -attack -victim https://conn-1.wails.localhost \
  -rig '{"run":"r1","thennav":"1"}' -control 127.0.0.1:18202 -tls 127.0.0.1:18203 -log r1/attacker.jsonl &
/tmp/front -addr 127.0.0.1:18201 -name conn1-front -log r1/victim.jsonl &
python3 isolation/summarize.py r1 r1/attacker.jsonl r1/victim.jsonl r1/shell.log
```

`mobile-dev-config.json` points `conn-0` and `conn-1` at the two fronts. Use `-victim wails://conn-1`
on macOS and iOS. Rig keys: `run`, `thennav`, `only`, `skip`, `phase` (`sub`, `nav`, `race`), `n`, `vh`, `direct`, `wait`.

**macOS and iOS builds:** the `isocontrol` tag turns the Go window check off, and `isonopolicy` turns the
native policy off on macOS. On iOS the policy stays off until `POST 127.0.0.1:18099/policy` with the `probe` tag.

**Android:** build with `ISO_TAGS=isorig android-host/build.sh`, `adb reverse` ports 18200 to 18203, then set the
mode before launching. URL-encode it, because `+` would decode to a space:

```sh
curl -G --data-urlencode 'set=perview-profile' http://127.0.0.1:18202/ctl/mode
curl -G --data-urlencode 'cmd=switch conn-1.wails.localhost' http://127.0.0.1:18202/ctl/push
adb logcat -v time IsoRig:I IsoGo:I AndroidRuntime:E '*:S' > r1/shell.log
```

Modes are `none`, `perview`, `perview-profile`, `pinned` and `pinned-drain`. Flags go after `+`: `sw`, `swdeny`, `blocknet`,
`csp` and `tlsok`. Commands are `load <url>`, `switch <host>`, `back`, `mem <label>`, `gc` and `recreate delete|keep`.

A refused top-level navigation replaces the attacker's page and stops the chain, so run later vectors with `phase=nav` and `n=K`.

## Tests and probes

`go test -tags production .` covers the proxy's refusals and token rotation, the reauth
handoff, URL validation and download paths.

`-probe FILE` runs JavaScript after each navigation and enables `/__spike/report`
and `/__spike/sleep`. The files in `probes/` produced the report's evidence. Replace
`__ORG__`, `__PROJECT__`, `__ENV__` and `__TOTP_SECRET__` first (`downloads.js` also wants a second `-instance`), and use them only on
a disposable instance: `hardening.js` overwrites the clipboard.
