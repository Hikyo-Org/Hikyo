# Handoff: #744 adapter origin reuse + conflict accumulation

Issue: https://github.com/Hikyo-Org/Hikyo/issues/744 (bug, DBugIT dogfooding on
CI secret delivery). Fixed point before this work:
`1180ad0f91b0d67410b08ff3e29f47378196c198`.

Two user-visible failures in the Forgejo adapter lifecycle:

- **Bug 1 — adoption accumulates duplicate conflicts.** After adopting existing
  destination names, later sync attempts showed the original group plus repeated
  single-name groups (PROD_SSH_KEY 3× at generation 4), each a distinct
  persisted artifact; target ended `sync_status=failed`,
  `last_error_class=conflict`, `drift_attention=true`.
- **Bug 2 — deleted adapter cannot be recreated.** After tombstoning, recreating
  at the same origin failed on Save ("The current state of this target refuses
  the request."), because `UNIQUE (org_id, project_id, origin)` reserved the
  origin for tombstoned rows too.

## Contract

- **Bug 2** — origin uniqueness is scoped to live adapters. Migration `00054`
  (both engines) replaces the unconditional `UNIQUE (org_id, project_id, origin)`
  with partial index `adapters_active_origin ... WHERE state <> 'tombstoned'`.
  Postgres drops the named auto-constraint; SQLite cannot drop an inline UNIQUE,
  so it rebuilds `adapters` (11 cols, keeping `UNIQUE (org_id, project_id, id)`
  and both FKs) under `legacy_alter_table=ON`, same dance as `00025`. Active and
  moving rows still collide; tombstoned history is retained and no longer
  reserves the origin.
- **Bug 1a** — retry flooding stopped. `insertConflict`
  (`internal/store/adapter_runtime.go`) is now idempotent: before inserting it
  counts un-adopted conflicts for the same
  `(target, org, project, environment, generation, surface, effective_name)`;
  if one exists it only re-raises drift attention instead of minting a fresh
  artifact per attempt.
- **Bug 1b** — stale groups hidden. `Repos().Conflicts`
  (`internal/store/repos_adapters.go`) joins `adapter_targets` and filters
  `c.target_generation = t.generation`, so a superseded group can no longer be
  surfaced (adopting it would fail the generation-scoped adoption COUNT with a
  generic 409). Artifacts are hidden, not deleted — history is preserved.
- Active-vs-active origin collision, the generation-scoped adoption check, and
  the audit/conflict history are all unchanged.

### Scope call — "useful actionable error"

The validation bullet "UI shows a useful actionable error and does not present
stale conflict artifacts" is read as: the reported flow (recreate at a freed
origin) must succeed rather than dead-end on the opaque "The current state of
this target refuses the request." — which it now does. Stale artifacts are
hidden by the generation scope. A genuine active-vs-active origin collision
still returns a plain conflict with the generic message; that is **left as-is
on purpose**: `internal/server/errors.go:93-98` documents a deliberate
invariant that conflict wire bodies stay byte-identical except one opt-in
(protected-destination), and the web client already renders `error.detail`
when a server supplies one. Adding a bespoke origin-collision detail would
break that uniformity invariant to describe a refusal the report never hit.
If Marc wants the live collision to name the origin, that is a follow-up that
touches the error-uniformity policy, not this bug.

## Convergence — what is and is not root-caused

- Fixed and proven by regression: retry flooding (1a) and stale-group display
  (1b). Tombstone origin reuse (Bug 2) fixed and proven on both engines.
- **Not** independently reproduced as a defect: the report's worker re-conflict
  *after* adoption. Traced the two provider paths — Forgejo re-conflicts only
  when `state == Reserved` (a fresh reservation); an adopted name loads from the
  ledger as `Owned`, takes the `Update` disposition, and skips the conflict
  branch. GitHub-Actions `possible_capture` release (`ReleaseLedger: true`) is
  guarded by `freshReservation`, so an owned/adopted row is not deleted. Both
  guards hold given a ledger key that matches adoption's written
  `effective_name`. If Marc still sees re-conflict on the preview env after
  these fixes, the next step is to capture the live `adapter_ledger` state for
  the target right after adoption and confirm the stored `effective_name`
  matches the desired row's name for the surface.

## Validation

- `go test ./internal/store/ -count=1`: green (SQLite legs + migration-checksum
  gate).
- `HIKYO_TEST_POSTGRES_DSN=... go test ./internal/store/ -run Postgres -count=1`:
  green.
- New regressions (`internal/store/adapter_bug744_test.go`), all PASS on both
  engines where dual: `TestAdapterOriginReusableAfterTombstone{SQLite,Postgres}`,
  `TestAdapterConflictInsertIdempotentAcrossRetries`,
  `TestAdapterConflictsHideStaleGeneration`.
- `go test ./internal/lint/ -count=1`: green. The new test file is admitted to
  the driver-handle allowlist (`internal/lint/handle_positions.go`) for its
  both-engine fixture seeding, same as the other adapter `_test.go` files.
- `go build ./...`, `go vet ./internal/store/... ./internal/lint/...`: clean.
- Broader sweep `go test ./internal/store/... ./internal/service/...
  ./internal/adapter/... ./internal/upgradegate/... ./internal/buildcompat/...
  -count=1` (with Postgres DSN): all green. No existing test encoded the old
  one-artifact-per-attempt flooding, so nothing had to be re-baselined.
- SQLite rebuild drift check: no migration between the base and `00054` adds a
  column or index to `adapters` (only `00025` added `credential_expires_at`,
  which the 11-column rebuild copies). The rebuild drops only the origin UNIQUE,
  replaced by the partial index; every other constraint/FK is preserved.

## Migration fixture note (read before adding the next migration)

Any new migration invalidates `internal/buildcompat/development.json` (the
migration-checksum gate at `internal/upgradegate/gate.go` compares the live
embedded-FS digest against it), which fails **all** store tests with "embedded
migration bytes differ from verified build". Regenerate it:

```
# scratch DB must be empty; generator computes per-engine SchemaSHA256 by
# actually migrating. Use a PG major that matches CI (postgres:18).
psql ... -c 'CREATE DATABASE compat_744'
HIKYO_RELEASE_SCHEMA_POSTGRES_DSN='postgres://.../compat_744?sslmode=disable' \
  go run ./scripts/release/compatibility --development --out /tmp/dev.new.json
# --out is O_EXCL and the package //go:embed's development.json, so it must
# exist to compile but not exist to write: generate to a temp path, then move.
cp /tmp/dev.new.json internal/buildcompat/development.json
psql ... -c 'DROP DATABASE compat_744'
```

Sanity: the diff must change exactly the new `{"version":N}` entry (version +
sha256) per engine and the `schema_sha256` per engine — nothing else.

## Review

- Advisor checkpoint before regen confirmed the procedure and flagged two fix
  blind spots (all other conflict reads; worker re-conflict loop); both were
  chased down before commit (see Convergence).
- Cross-provider adversarial review: pending.
