# Handoff: #595 Dynamic secrets — provider and lease management WebUI

Issue: https://github.com/Hikyo-Org/Hikyo/issues/595 (follows #147, which shipped
the API + CLI + read-only Leases tab).

WebUI-only. No server, contract, migration, CLI or generated-code change — every
operation already existed from #147 and is imported from the generated client.
This ticket flips the ten `{issue: 595}` parity rows to `{webui: machine-access}`
and gives the `machine-access` page the management surface behind them.

## What landed

**Providers tab** (`machine-access`, project-scoped):
- Lists providers (kind, origin, grant role, credential presence + set date,
  state). The admin credential is never listed.
- `CreateProviderDialog` — kind is `postgres` (the closed enum), origin, grant
  role, write-only credential. No passkey: configuring standing project
  authority is `manage-identities`, not a per-environment disclosure (§#147 d1).
  The server probes the origin with the credential before storing, so a success
  means it was reachable and authenticated; the dialog says so.
- `SetCredentialDialog` / `RevokeCredentialDialog` — the credential is write-only
  (set/replace, never read back). Revoke states the fail-closed consequence:
  existing leases stay minted at the provider but the worker can no longer
  renew/revoke/expire them until a replacement is set.
- `DeleteProviderDialog` — typed-**origin** confirmation (a provider has no
  name). Refuses (409) with live leases unless the cascade is confirmed; the
  dialog surfaces the live-lease count and requires the `revoke_all` tick when
  there are any, and reports `revoked_lease_ids.length` in the success notice.
  When the lease listing could not be read the count is unknown, so the tick is
  offered and the server 409 is the honest fallback.

**Leases tab** — write actions added to the existing read-only listing:
- `LeaseMintDialog` — display-once mint. Reuses the shared mint lifecycle
  (`useMintLifecycle`); a human mint runs one `mint`-purpose passkey ceremony
  over the chosen environment, then the server discloses the **role name and
  password exactly once**. The password lives only in the ephemeral lifecycle
  result (`mintLease` is a plain async call, never a cached mutation — mirrors
  `mintCredential`). Stored-confirmation gates dismiss; navigation/session/unmount
  clear it. A mint whose response was lost is a live role whose password is gone
  → the failure text says so and the listing is refreshed.
- Per-row `renew` / `revoke` / `settle`, gated by the server's own state guards
  (from `store.EnqueueTransition`): **renew** only from `active`, **revoke** from
  any non-terminal state (the fail-safe teardown), **settle** only from
  `unknown`. `unknown` renders as a loud alert row. All three are **queued**: the
  copy says so, because the worker carries them out and the row moves when it does.

## Reuse / refactor

The display-once mint state machine (`mintLifecycle.ts`) was generalized over its
request and result types with **defaults equal to the machine-credential types**,
so the existing credential flow and its tests were unchanged (zero runtime diff).
The wiring (state + sync ref + `moveMint`/`isSubmitting` + the three boundary
clears) was extracted into `useMintLifecycle`, which now backs **both** the
credential mint and the lease mint. One test annotation was needed
(`transitionMintLifecycle<MintRequest, MintResult>` at the review step, where the
result type is not otherwise inferable).

## Parity

`api/parity.yaml`: the ten rows flipped to `{webui: machine-access}`.
`showDynamicProvider` stays a `via: [listDynamicProviders]` row — its `Op` symbol
is deliberately **not** imported (the list row carries every field), and the
parity checker requires a direct row for any imported `*Op`. The other nine
`*Op`s are imported in `web/src/api/dynamic.ts`. `go test ./api/ -run Parity`
green.

## Gotchas for the next session

- **`clients/ts` needs its own `pnpm install`** or `tsc`/build fail with TS2307
  on `zod` (the generated sources resolve `zod` from `clients/ts/node_modules`);
  the CI typecheck job does this explicitly.
- **Node**: default shell Node breaks pnpm — `eval "$(fnm env)" && fnm use 24`.
- **e2e has no PostgreSQL target**, so browser e2e covers the Providers empty
  state, the create-provider client-side validation (empty fields refused before
  any dial), the write-only password field, and the Mint-lease held-back state —
  never a real mint (same boundary #147's e2e drew). Tests were added to
  `machine-access.spec.ts` (already in a `ci.yml` group; the tab count moved 4→5).

## Tests / gates

- `web` typecheck + full vitest (668) green; `dynamic.test.ts` covers the
  per-verb refusal mappers; the shared lifecycle tests cover the display-once
  machine both mints run on.
- `go test ./api/` (parity) green.
- e2e: `machine-access.spec.ts` desktop + mobile.
