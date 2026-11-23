# #1531: CLI reauthentication browser approval, handoff

Status: **pull request open, stacked on the #1516 pull request (#1529), not
merged.** Closes [#1531](https://github.com/Hikyo-Org/Hikyo/issues/1531). Part A
of the CLI approval follow-up; part C is epic
[#1532](https://github.com/Hikyo-Org/Hikyo/issues/1532).

## Owner decisions (2026-11-23)

- Emergency temporary access (`hikyo access request emergency`, #152) gets the
  browser passkey handoff, like `mint`. This needed `access` and `access.bypass`
  in the closed `CLIReauthStartRequest` and `CLIReauthTransaction` enums: a
  third narrow `v1.0.0` freeze amendment (`AccessReauthEnumGrowth` in
  `api/freeze.go`, and the 2026-11-23 #1531 banner in the api-cli-surface ADR).
- Code first, browser when a passkey is required or no code is possible stays
  the default. A per-instance `reauth.method = auto | browser` setting and a
  `--approve-in-browser` flag force the browser.
- A forced browser never falls back to a typed code. It refuses clearly where a
  purpose has no handoff, or where the host has no graphical session.

## What changed

- **Server.** `internal/service/cli_reauth.go` admits `access` / `access.bypass`,
  bound to the environment alone (no keys), matching `NewAccessBypassReauthIntent`.
  Starting it authorizes `access.bypass` over the environment (read level). The
  bypasser designation is checked when the access is taken.
- **Migration 00098** (Postgres and SQLite) admits `access` rows with an empty
  key set. 00097 is untouched.
- **Freeze.** The enum rule now judges each added value against the amendment
  lists for its exact location, because against `v1.0.0` each enum grows by both
  `mint` and `access`. Removals and unlisted values still refuse.
- **CLI.**
  - `internal/cli/settings.go`: a strict `settings.json` keyed by trusted instance,
    and the new client-local `hikyo config get|set reauth.method`, admitted to the
    closed v1 verb set by the #1531 amendment.
  - `--approve-in-browser` is registered once in `parseCommon`.
  - `chosenBrowserOpener` refuses on a host without `DISPLAY`, `WAYLAND_DISPLAY`
    or `BROWSER` (not macOS or Windows), prints a pre-wait hint, and wraps open
    errors. `State.browserChoice` names the cause and the remedy everywhere.
  - Account-security proofs and factor prompts print that the flag does not
    apply to them (`State.readProof`).
  - `context delete --instance` forgets the instance's settings first.
- **Web.** `CLIReauth.tsx` explains the access approval and hides the key list.

## Out of scope

- A launcher that starts but shows nothing (for example `xdg-open` with a broken
  `BROWSER`) is not detectable; the CLI waits until the approval expires, with
  the hint printed.
- A host without a display is not refused when the browser is the only way
  (0-window or protected environment), because dropping the flag would not help.
- Action approval (part C) is epic #1532, starting with the ADR in #1533.

## Evidence

See the pull request body for the commands and results at the pushed head.
