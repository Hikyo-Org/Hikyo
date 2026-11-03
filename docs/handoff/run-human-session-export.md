# `hikyo run --use-human-session`: handoff

## Result

The command always failed on a real server with `not found` (exit 5). It fetched from `GET /delivery`, which refuses human sessions at
artifact admission. The CLI tests missed this because their fake server accepted any caller.

The run now lists key names from `GET /revisions/latest`, asks for confirmation, runs the reauthentication ceremony, then calls
`POST /values/export` pinned to the confirmed revision. No contract or ADR changes: [compose-integration.md](../adr/compose-integration.md)
already binds this exception to `values export`.

## Behaviour to know

- **Declining:** creates no disclosure record and starts no process.
- **Publish during a run:** a full run is refused with nothing disclosed, unless the caller holds `reveal-history`. Then it injects the
  revision that was confirmed. `--config-only` also injects the confirmed revision.
- **Loader-control keys:** refused before the prompt.
- **`--config-only` at a 0-second window:** refused. The server never spends a single-decision window on that projection, so the ceremony
  would authorize nothing.
- **Stale single-decision windows:** `ensureRevealWindow` now runs a new ceremony instead of trusting one, for every caller. A valid window
  left live by a failed export or a partly completed `values diff --reveal` costs one extra passkey on the rerun.
- **Rate limit:** each launch counts toward the export limit of 5 per minute per principal, shared with audit export. The sixth exits 7.
- **`values export` consent:** `exportSecretKeyIDs` now includes sticky secrets, which the server's unit already counted.

## Evidence

- `internal/isolation/run_human_session_e2e_test.go` runs against a booted server. It reproduced the bug before the fix and now covers
  decline, the full projection, `--config-only`, and both publish cases. SQLite only locally. The Postgres leg skips without
  `HIKYO_TEST_POSTGRES_DSN`.
- `internal/cli/run_human_session_test.go` covers ordering, the rotated session, ceremony units and error reporting.
- Three independent review rounds on `claude-opus-5-5`. Their findings were fixed except the items below.
- A real terminal and a real browser or passkey ceremony were not exercised.

## Open items

1. **Owner decision:** a human `--config-only` run writes no server audit event, like `values export` without `--reveal`.
   [compose-integration.md](../adr/compose-integration.md) requires a recorded config-only fetch for machine delivery. Accept it as the
   human path's behaviour, or add a server event.
2. **Empty ceremony unit:** a full run with no secrets at a 0-second window is refused, although the server needs no ceremony for it.
3. **Bound sliding windows:** a live adapter-bound window counts as satisfied, because the CLI cannot see its binding. The disclosure then
   gets `not permitted` until the window lapses. This is pre-existing, and the fix needs the server to report the binding.
4. **Browser consent copy:** `web/src/routes/CLIReauth.tsx` says a passkey authorizes one decision over the listed keys. Above a 0-second
   window it opens an environment-wide sliding window. This is pre-existing, but a config-only unit makes it more misleading.
5. **Historical consent unit:** `values export --reveal --revision N` at a 0-second window is refused for a key tightened to secret after
   revision N, because `Sensitive` does not count the current classification. Pre-existing, and it fails closed.
