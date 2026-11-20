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
login keychain; it is opt-in because a locked keychain shows a prompt.

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
notarization yet (plan PR 4). `bin/` is ignored by Git.

## Signing and the keychain

Each connection's token and session id are one generic-password item in the
login keychain (service `io.hikyo.desktop`, account = the connection's random
host). The legacy file keychain is used on purpose: its default access control
trusts only the application that created the item.

- A **signed** (Developer ID) build keeps access across updates signed by the
  same identity.
- An **unsigned or ad-hoc** dev build works, but the item is tied to that exact
  binary. After a rebuild macOS asks once whether the new binary may use the
  item; choose "Always Allow", or sign in again from the connection list.

## Developing against a local server

Plain http is refused unless explicitly allowed, and then only for loopback:

```sh
HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP=1 HIKYO_DESKTOP_HOME=$(mktemp -d) ./hikyo-desktop
```

`HIKYO_DESKTOP_HOME` moves the connection list; secrets always use the keychain.
The instance must advertise `desktop-app/1` in `/api/v1/meta`.

## Not shipped yet

Code signing, notarization, an updater and CI are later work (plan PR 4 and
the release process).
