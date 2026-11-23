# Hikyo Desktop

The native macOS shell for hosted Hikyo instances (issue #1215). It is its own
Go module so the Wails dependency and cgo/WebKit never reach the server build.
The decisions are in [docs/adr/desktop-app.md](../docs/adr/desktop-app.md) and
the shell's wire contract is in
[docs/spec/desktop-shell-protocol.md](../docs/spec/desktop-shell-protocol.md).

## Layout

- `internal/shell`: all logic, with no cgo. Builds and tests on any OS.
  Connections, the per-window proxy, the D4 refusal list derived from
  `api/openapi.yaml`, token rotation, the `/__shell/*` routes, navigation
  policy, downloads and the clipboard guard.
- `main_darwin.go`, `native_darwin.{go,m}`, `keychain_darwin.go`: the Wails app,
  the WebKit delegate methods Wails lacks, native dialogs and the keychain.
  macOS only. `main_other.go` is a stub that prints "macOS only".

## Build and test

```sh
cd desktop
go test ./...                        # pure logic, any OS
go build -o hikyo-desktop .          # macOS, needs Xcode command line tools
```

`HIKYO_DESKTOP_KEYCHAIN_TEST=1 go test -run Keychain .` also exercises the real
login keychain; it is opt-in because a locked keychain shows a prompt. The
tests plant their fixtures into `~/Library/Keychains/login.keychain-db` by path,
the file the app uses, whichever keychain is your default.

Wails is pinned to `v3.1.0` in `go.mod`. `go build` does not need its CLI. The
bundled, ad-hoc signed `.app` does:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
cd desktop
wails3 package        # runs the Taskfile's build, writes bin/hikyo-desktop.app
open bin/hikyo-desktop.app
```

`wails3 build` alone writes `bin/hikyo-desktop`. `Taskfile.yml` and
`build/darwin/Info.plist` are all it reads: no icons, Developer ID signing or
notarization yet (plan PR 5). `bin/` is ignored by Git.

## Signing and the keychain

Each connection's token and session id are one generic-password item in the
login keychain (service `app.hikyo.desktop`, account = the connection's random
host). The item also records the origin, trust mode, pin and principal id the
token belongs to; `connections.json` is plain user-writable data, so at start
the shell compares it with the item and stops a connection that disagrees,
with a visible error and no repair. Saving a session deletes any existing item
and adds a new one whose access list names only this application, so an item
planted under the same name never keeps access. The legacy file keychain is
used on purpose: it needs no entitlements or provisioning profile.

The shell reads an item only if its decrypt access list names exactly one
application and that application's code-signing requirement equals this app's.
It compares requirements, not paths, so moving the app (including App
Translocation) does not matter. An item open to any application, or to a
different signer or build, is refused and the connection shows "Sign in again";
signing in replaces the item. Removal still deletes it.

- A **signed** (Developer ID) build keeps access across moves and across
  updates signed by the same identity and identifier.
- An **unsigned or ad-hoc** dev build's requirement is its own code hash, so the
  same binary keeps access wherever it sits, but a rebuild is another app: its
  items are refused until you sign in again.

The requirement is read with `SecTrustedApplicationCopyRequirement`, exported
by Security.framework but declared only in a private header. If a macOS update
removes it every item is refused rather than the check being weakened.

`HIKYO_DESKTOP_KEYCHAIN_TEST=1` also runs the moved/other-build cases from
copies of the test binary; add `HIKYO_DESKTOP_SIGN_IDENTITY=<codesign identity>`
for the signed-requirement case.

## Developing against a local server

Plain http is refused unless explicitly allowed, and then only for loopback.
The two variables below are read only by a development build, `go build -tags
probe`. A release build ignores them, so another process of the same user can't
set them to move the connection list or allow plain http:

```sh
go build -tags probe -o hikyo-desktop-dev .
HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP=1 HIKYO_DESKTOP_HOME=$(mktemp -d) ./hikyo-desktop-dev
```

`HIKYO_DESKTOP_HOME` moves the connection list; secrets always use the keychain.
The instance must advertise `desktop-app/1` in `/api/v1/meta`.

`sh probe/run.sh` drives a probe build against a stand-in instance (macOS GUI
session needed): it checks that the Wails runtime stays inert in a window and
that downloads save once, named and gated as the ADR requires.

## Releases

Stable release builds are Developer ID signed and notarized by
`scripts/release/build-desktop-app.sh` and published as a `desktop-v<version>`
release; see [Apple signing](../docs/release/apple-signing.md#desktop-app). No
desktop release exists until the first stable tag. v1 has no updater (ADR D13);
adding one needs its own decision.

## CI

The CI definitions for this module (the pinned-macOS `desktop` job in
`.github/workflows/ci.yml`, the advisory `.github/workflows/ci-golangci-desktop.yml`
lint, `scripts/ci/check-desktop-deps.sh` and the
[Desktop shell (macOS)](../docs/development/ci.md#desktop-shell-macos) section of the
CI guide) run for this module wherever `desktop/go.mod` exists. The `desktop` job
runs the module boundary check, `go test -race ./...`, the probe and release builds
and the driven WKWebView lane.

Run the same checks locally from `desktop/`:

```sh
export CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
go test -race ./...
go build -tags probe -o /tmp/hikyo-desktop-probe .
go build -o /tmp/hikyo-desktop .   # the release build, without the probe tag
go run ./probes/driven -app /tmp/hikyo-desktop-probe   # opens a window briefly
../scripts/ci/check-desktop-deps.sh
```

The driven lane seeds one login-keychain item for a fake connection and deletes it
when the run ends.
