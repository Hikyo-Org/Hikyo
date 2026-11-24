# #1516: machine credential mint lifetime, CLI reauthentication and expiry status, handoff

Status: **pull request open against `dev`, not merged.** Closes
[#1516](https://github.com/Hikyo-Org/Hikyo/issues/1516).

## Owner decisions (2026-11-23)

- Web lifetime choice: presets plus the server clamp. There is no new API for the
  instance ceiling, because a project identity manager cannot read
  `GET /instance/credential-policy` (`instance-config@instance`).
- CLI `sa credential mint|rotate`: inline TOTP where the environment allows it,
  and the browser passkey handoff otherwise. The handoff needed `mint` and
  `identity.credential-mint` in the closed `CLIReauthStartRequest` and
  `CLIReauthTransaction` enums. The owner approved a second, narrow amendment to
  the v1.0.0 API freeze in this PR (see the api-cli-surface ADR amendment dated
  2026-11-23 and `MintReauthEnumGrowth` in `api/freeze.go`).

## What changed

- **Web.** The mint and rotate dialog offers Instance default (30 days), 30 days,
  90 days, 180 days and 1 year; indefinite is shown disabled. It shows the
  estimated expiry before confirm, and after minting it says so when the ceiling
  shortened the credential. The federation binding dialog shares the same preset
  list.
- **CLI.** `withMintCeremony` (`internal/cli/identities.go`) retries the mint after
  each refusal that names an environment still missing a window, one environment
  per round. A refused mint commits nothing, so earlier windows survive. An
  environment named twice returns the server's refusal. TOTP prompts and
  no-browser refusals are purpose-aware.
  The mint's ceremony is `authorized`: the server's mint check accepts
  `reveal-history` for history-only reach, so the CLI's `can_reveal` (read and
  current reveal) does not veto it.
  The ceremony itself still needs `read` over the environment (the window
  read and the handoff start), so a minter holding `reveal-history` without
  `read` gets the server's 403; the built-in maintainer role includes `read`.
- **`access grant` refusals.** The grant enters its ceremony only on the
  server's widening refusal (`withWideningCeremony`); a missing grant's 403 is
  returned unchanged instead of advice about a window.
- **Server.** `internal/service/cli_reauth.go` admits purpose `mint`, which is not
  key-bound (its unit is the environment). Starting it needs `read` over the
  environment, as reveal does; the mint itself re-checks disclosure
  authority (reveal or reveal-history per reach) and manage-identities.
- **Migration 00097** (Postgres and SQLite) admits `mint` rows with an empty key
  set. It also fixes a pre-existing bug: since 00074 the checks refused publish,
  approve, reject and bypass handoff rows. `cli_reauth_schema_test.go` now derives
  every admitted pair from the service's own table, so the two cannot drift again.
- **`sa credential list`** shows `expired` once the expiry has passed.

## Out of scope

- Superseded-epoch credentials still show `live` in the CLI list: the API
  exposes no epoch field.
- The `hikyo doctor` expiry warning (nice to have in the issue) is not
  implemented.
- The web ceiling bound for instance admins (who can read the policy) is not
  implemented. Everyone gets presets plus the clamp notice.
- The desktop shell's SPA reauth purpose lists (`web/src/app/shell.ts`,
  `desktop/internal/shell`) do not include mint, because the desktop app does not
  run the mint ceremony.

## Evidence

See the pull request body for the commands and results at the pushed head.
