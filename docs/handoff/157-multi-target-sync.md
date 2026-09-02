# Handoff: #157 multi-target synchronization

Issue: https://github.com/Hikyo-Org/Hikyo/issues/157. Base: `5276ba8d` (main,
2026-09-02). Implements the workflow over the #65 seam; no new sync-target
object, `adapter_targets` is the target.

## What landed

- **Migration 00040** (both engines, additive): `paused_at`,
  `last_attempted_revision`, `last_attempted_at`, `last_error_class` (closed
  CHECK), `drift_attention` on `adapter_targets`. The stored `sync_status`
  keeps its four outcomes; operator health is DERIVED (`adapter.DeriveHealth`)
  from those columns and the active outbox job, so pausing never erases the
  last recorded outcome and no sqlite table rebuild was needed.
- **Runtime**: `ClaimDue` skips paused targets and settles every effect whose
  INTENT never got an OUTCOME as `unknown` with a correlated
  `adapter.push_outcome` audit row (`closeIndeterminateEffects`).
  `Retry`/`Fail` now carry the loaded revision and cause; `finishJob` stamps
  `last_attempted_*`, the bounded error class, and raises `drift_attention`
  on conflict/ambiguity; success clears both. Conflicts and route-move orphans
  raise attention too.
- **Store**: `PauseTarget` (supersede + generation bump + `paused_at`,
  idempotent), `ResumeTarget` (clear + one converge + published revision),
  `TargetKeys` (membership by name), `HealthCounts` (instance-wide).
  `EnqueuePublished` excludes paused targets; `EnqueueManual` refuses them
  (`ErrAdapterTargetPaused`, 409).
- **Contract**: `POST .../adapter-targets/{target}/pause` (adapter.configure,
  no ceremony) and `/resume` (adapter.sync, ceremony; `AdapterResume` names
  the revision). `AdapterTargetInput.key_selection` (names, include/exclude
  globs, classification) resolves to ids at save via the catalogue and is not
  stored. `AdapterTarget` gained the health fields and `keys`.
  `RetentionHealth` gained the four adapter counts.
- **Service**: `resolveKeySelection` (pure, tested), `PauseTarget`,
  `ResumeTarget`; every response echoes `Keys`. Audit: `adapter.configure`
  with `mutation: target-pause`; `adapter.sync_requested` trigger `resume`.
- **CLI**: `adapter target pause|resume`, `--names/--include/--exclude/
  --classification` on create/update/target add, status columns; help golden.
- **Operators**: four label-free gauges, `hikyo doctor` finding
  `adapter-targets`.
- **Web**: surface `adapters` (project section): list, detail with facts and
  verbs, create adapter / add target / edit keys, adopt enumerated conflicts,
  retain-or-prune dialog, adapter-purpose ceremony dialog (TOTP for sliding
  environments, passkey per zero-window environment). Rides
  `machine-access.spec.ts` (group 3) with the pinned set.
- **Dev fake provider**: `HIKYO_DEV_ADAPTER_FAKE_PROVIDER` (refused outside
  `--dev`) swaps the provider HTTP peer for an in-memory store so the browser
  flow can push; ceremonies, outbox, ledger, journal and audit stay real.
- **Tests**: store control suite (pause/resume/claim gate, attempt columns,
  crash-window settlement, stale-worker fencing, health counts); isolation
  fan-out suite on both engines (two adapter kinds, failure isolation, pause
  skips publish, resume revision, idempotent resync, crash-window replay with
  INTENT/OUTCOME linkage, no plaintext in audit); CLI and doctor goldens.

## Deviations from the handoff comment, stated

- **Decision 1 (widen the `sync_status` CHECK)**: not done. Health is derived;
  the stored column stays truthful and pause is orthogonal to the last
  outcome. Same seven states are exposed on the contract.
- **Decision 3 (`adapter_target_renames`)**: not implemented. The
  deployment-adapter ADR (Multi-environment section) says the prefix "is
  deliberately not a per-key rename table ... zero mapping state to drift",
  and the handoff's own gate says a divergence reopens the ADR. Prefix-only
  mapping satisfies the acceptance criterion (deterministic, collision-checked
  across the manifest and against other targets on the destination, values
  never transformed). If Marc wants per-key renames, that is an ADR amendment
  first, then a follow-up over `ManifestEntry` (DesiredRows, both validators,
  renderWorkflow, loader Gate, collision SQL, move claims).
- **Correction 2 (multi-node)**: #146 has merged. The adapter worker runs on
  every node; fencing is the existing outbox lease (`lease_owner`,
  `lease_expires_at`) plus the provider-write lease triple.
  `TestAdapterStaleWorkerIsFencedAfterReclaim` proves push (Prepare), succeed,
  retry, fail and scrub completion are all refused after a reclaim. No new
  lease machinery.
- **Browser create/update**: the handoff's web list omitted them; the ticket's
  first criterion names them, so the surface creates adapters, adds targets,
  and edits keys/prefix. A destination MOVE stays CLI-only (scrub-before-switch
  ceremony).

## Gotchas for the next reader

- Every `adapterTargetColumns` read must use `adapterTargetFrom` (outer join
  to the active job); a Postgres lock must be `FOR UPDATE OF t`.
- `finishJob` must not move `converged_revision` on a retry: the revision now
  flows through `Retry` for `last_attempted_revision` only.
- `HealthCounts` is granted on the scheduler SYSTEM SITE (not the publish
  op); the unauthenticated `/metrics` scrape reads under it.
- New audit enum values (`trigger: resume`) need no schema bump; a NEW event
  type would need a real emitter in `internal/isolation`.
- The Playwright flow needs the fake provider env in `instance.ts`; the
  ceremony dialog is authorised with `nextTotpCode()` against the seeded
  `dev` environment (sliding window). `prod` is zero-window and would take a
  passkey.

## Gates run

`go build ./... && go vet ./... && HIKYO_TEST_POSTGRES_DSN=... go test ./...`
(both engines), sqlc and oapi-codegen diff-clean, `pnpm --dir clients/ts
verify`, `web` typecheck + vitest, Playwright machine-access spec (desktop and
mobile), `pnpm --dir docs/site run check`.

## Cross-model review (Codex gpt-5.6-sol, high effort, R1-R3)

R1 raised five findings; R2 resolved four and accepted two dispositions; the
final R3 verdict is SOUND with one human-disposition residual.

Fixed during review: browser adoption reauthenticates over the adapter's whole
environment set (matches the server's `TargetEnvironments`, which spans every
non-tombstoned sibling target); the crash-window effect settlement binds the
full tenant chain; exclude-only key selection fails loud; the drift_attention
raise is one helper; the pause route declares 403; `StoreAdaptersHealthCounts`
is a reviewed scheduler shared-door.

Accepted dispositions: narrowing a key set under the prior authority without
reveal/reauth is the locked #15 + deployment-adapter design; a crash-window
`unknown` does not raise `drift_attention` because the dispatched row is
presumed-written and the next converge self-heals; the adapters surface is
project-local like machine-access (not workspace-scoped, `?remote` ignored),
and its sidebar link is disabled for remote workspaces.

Residual (R3, for human disposition): an HTTP request Hikyo times out at 15s
(client `Timeout`, strictly < the 2-minute provider-write lease) may still be
processed server-side by the provider after Hikyo gives up, so a write can land
remotely after `PauseTarget` returns. This is the deployment-adapter ADR's
already-accepted non-transactional-provider residual: pause deliberately does
NOT release the ledger, so the row stays `dispatched`/`owned` (presumed
written) and a late landing remains consistent with the record; the next
converge reconciles idempotently. Eliminating it would need a provider-side
conditional write (a provider feature, not an Hikyo choice). No code change;
recorded so the acceptance is explicit rather than silent.
