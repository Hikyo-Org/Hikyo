# Client nightly self-upgrade on macOS and Windows

## Problem

On a Mac running a signed nightly, `sudo hikyo upgrade` failed with
`automatic upgrades require sudo hikyo upgrade on the Linux systemd server`.
`hikyo update check` only staged the verified nightly in the CLI state
directory and left the running binary in place. The only way to upgrade was to
unpack the staged archive by hand.

The staging-only behaviour came from #687. On Linux the CLI binary may also be
the systemd server's executable, and only `sudo hikyo upgrade` may replace that
executable, after backup and scratch-restore proof. macOS and Windows have no
supported server host, so that reasoning does not apply to them.

## Outcome

- `selfupdate.StagesNightlies()` is true only on Linux. `Installer.Apply` stages
  there as before. On other platforms it runs the same `prepareNightly`
  verification with extraction, re-hashes the extracted executable, prunes the
  cache down to the target release, and then atomically replaces the running
  binary.
- `hikyo upgrade` on non-Linux dispatches to `cli.RunUpgrade`. It refreshes
  release metadata, selects the newest release on the binary's channel and
  applies it without a prompt, because naming the command is the confirmation.
  `upgrade operator` and the Linux path are unchanged.
- `hikyo update check` and the pre-command notice use the same `Apply`, so an
  accepted nightly also installs on macOS and Windows. The prompt still says
  "manual server upgrade" on Linux only.
- `Installer.CheckReplaceable` runs before any state write. A root process
  refuses to replace an executable owned by a non-root user, because the rename
  would leave `~/.local/bin/hikyo` root-owned. Root-owned installs, such as
  `/usr/local/bin`, still update under sudo. This check also covers the
  existing stable path.
- Explicit `update check` and `upgrade` wait up to 30 seconds for release
  metadata. The passive pre-command notice keeps its 2 second budget.

## Not changed

- A Linux workstation without a server still stages only. Telling it apart
  from a server host would need its own design.
- `--target` and `--config` are rejected on the client path, and downgrades
  remain refused.
- The runtime bundle is still assembled on clients, because assembly verifies
  the bridge evidence. It is then pruned. The verified release directory and
  its extracted executable are kept, about 150 MiB for one release.

## Validation

- `go test ./internal/selfupdate ./internal/cli ./cmd/hikyo` passes. Every
  nightly mutation is covered in both staging and replace modes, and the replace
  mode asserts that the binary changed and no bundle was left behind.
- `go vet ./...` passes, and so does `go vet` for GOOS linux and windows.
- End to end on macOS arm64: a binary stamped as
  `0.0.1-nightly.20260924.57.g1260fffc`, with production trust and the nightly
  channel, ran `hikyo -vv upgrade`. It replaced itself with
  `0.0.1-nightly.20260926.59.gdaa2bd76` (commit `daa2bd76`), exit 0.
- ADR amendment: `docs/adr/signed-upgrade-compatibility.md` (2026-09-26).
