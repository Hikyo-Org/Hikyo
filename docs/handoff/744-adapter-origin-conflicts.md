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

## Adoption 500 + empty audit trail (follow-up, same branch)

Marc: "why adopting would lead to a 500 error. I can't find anything in the
audit trail." Root-caused to three error kinds that escaped `adoptAdapter`
(`internal/store/repos_adapters.go`) / the service `Adopt`
(`internal/service/adapters.go`) as *unmapped* faults, which the server default
turns into 500 (`internal/server/errors.go` `wireErrorFor` → `ErrorCodeInternal`):

1. **Raw DB errors.** Every post-`Exec` return in `adoptAdapter` was
   `return AdapterAdoptionResult{}, err` — un-wrapped. A genuine unique-violation
   (pg 23505 / sqlite UNIQUE) on the owned-ledger insert therefore hit the
   500 default instead of a mapped 409. Fixed by routing all six mutation
   errors through `constraint(err)` (the same map `repos.go` uses everywhere
   else). The reachable collision is the owned-ledger insert tripping
   `UNIQUE(target_id, surface, normalized_name)` (or the cross-target
   `adapter_ledger_active_provider_name`) when a leftover ledger row already
   owns the name.
2. **`adapter.ErrProviderBusy`** (provider lease held, or the generation-guarded
   target update lost a race) and **3. `adapter.ErrSuperseded`** (the prior job
   was no longer queued/running) are plain `errors.New` sentinels with **no
   errmap entry**, so they too fell through to 500. Fixed by two rows in
   `wireErrorRules` mapping both to `ErrorCodeConflict` — the documented
   single-decision point, so this also closes the identical latent 500 on the
   `RemoveTarget`/`Delete` fence-timeout path. Neither sentinel carries a
   `SafeDetail`, so the 409 body stays byte-identical to every other conflict
   (uniformity invariant preserved).
   `Adopt` was also the only adapter mutation **not** wrapped in
   `retryAdapterProviderFence` (RemoveTarget/Delete are); it is now, so a
   transient lease retries to success instead of surfacing a spurious 409.

**Why nothing in the audit trail:** the audit `InsertTenant` is inside the same
`tx.Write` as the mutation (service `Adopt`), so *any* returned error rolls the
audit row back with it. The empty trail is the shared-tx rollback, not a second
bug — expected, and it is why a failed adoption leaves no trace.

**The outbox dedup insert (line ~667) is defense-wrapped but unreachable as a
500.** `adapter_outbox_active_dedup` permits at most one queued/running row per
`dedup_key` (= target id), and the invariant "queued/running converge ⟺
`active_job_id` set" holds across every writer (completion `active_job_id=NULL`
pairs with the job leaving queued/running — `adapter_runtime.go:1287`; pause
supersedes then nulls — `repos_adapters.go:846`; enqueue/adopt/tombstone/moves
likewise). So adoption's supersede always clears the one active job before the
new insert. The `constraint(err)` wrap there is belt-and-braces; there is no
legitimate state to regress (a second queued row can't even be seeded — the
index rejects it).

## Audit filtering: no empty pages + wildcards / multi-outcome / principal-by-name

Two further Marc asks on the same trail, same branch. Both turn on **tenant
isolation invariant 8** (`internal/lint/sqlpredicate.go`): tenant-owned tables
admit only `col OP param` conjuncts, so the audit filter's optional
equality/glob predicates have no provable SQL shape and MUST run in Go
(`AuditFilter.Matches`), never as SQL. Every fix below lives on the Go side of
that line.

**#2 — "filters apply on result, not search; pages can be empty" (commit
`f8c91a72`).** The paged reader scanned exactly one store page and Go-filtered
it, so a selective filter returned a near-empty page and the operator had to
"load more" to learn whether anything matched. Root cause: filtering happened
*after* a fixed-size read, not as part of filling the requested page. Fix is a
service-layer fill loop (`internal/service/audit.go` `fillPage`): read store
pages — advancing `AfterSeq` over **scanned** rows — until the caller's `Limit`
of *matches* is collected, the trail is exhausted, or a per-request scan budget
(16 chunks) is spent. Chunk size = `Limit` when the filter is unselective,
`AuditMaxPageSize` (1000) when selective (`AuditFilter.Selective()`). On budget
truncation `NextSeq` = last **returned** row and `Exhausted=false`, so "load
more" resumes correctly; `filterPage` stays the tested single-chunk primitive
underneath. The pinned session ceiling (`ToSeq`/`upper_seq`) still gives stable
paging across concurrent writes.

**#3 — wildcards, multiple outcomes, principal-by-name (this change).** All
three are additive filter parameters; the OpenAPI freeze gate (`api/freeze.go`)
is dormant (no v1.0.0 tag) and the parity registry is operation-level, so new
query params add no CLI/parity obligation.

- **Wildcards** on the free-text fields (`operation`/type, `object_type`,
  `object_id`): `matchGlob` in `repos_audit.go` — a `*`-only glob (no `*` = exact
  case-sensitive match, so existing filters are unchanged), no malformed-pattern
  error path, so no validation plumbing at the transport boundary. `Actor` and
  `CorrelationID` stay exact (opaque ids).
- **Multiple outcomes**: new repeatable `outcomes` param (`AuditFilter.Outcomes
  []string`, set-membership via `slices.Contains`) unioned with the retained
  singular `outcome` (back-compat). `Normalized()` records one outcome as the
  scalar `filter_outcome` and several as the `filter_outcomes` list
  (`KindStringList` in the audit registry — asserts `[]string`, pinned by
  `TestNormalizedOutcomeAndActorName`).
- **Principal-by-name** (option c, Marc's "we can offer self"): `actor_name` is a
  glob over the acting principal's **display name**. Names need the authorizer to
  resolve, which `Matches` (pure, engine-independent) cannot do, so it runs as a
  service-side `keep` pass after `Matches` — `actorNameKeep` in `audit.go`
  resolves id→name via `principalNames.get(ctx, az, id)` and applies
  `MatchesActorName`. **No directory lookup, no enumeration surface**:
  `principalNames` only ever resolves ids that already appear on rows the caller
  is authorized to read (`principal_names.go` "never exposes a directory"). This
  is sound because the filter only *narrows* an already-authorized view — the
  same names are disclosed per-page without it. `get` errors only on genuine
  infra faults (NotFound → empty name, no error); both the interactive query
  (`keep`) and the export loop propagate that error identically (fail-loud), and
  fall back to `""` (→ glob miss → row dropped) only on NotFound. The browser
  "Self" button fills the exact-id `actor` field with the current session's own
  principal id — no server `me` token, just the id `whoami` already returned.

Frontend (`web/src/api/audit.ts`, `web/src/routes/Audit.tsx`): `AuditFilter`
gains `actorName` and `outcomes: readonly string[]`; `auditQuery`/`auditExportUrl`
serialize `outcomes` as a repeated form param (`URLSearchParams.append` per
value). Outcomes render as checkboxes (not a hostile multi-select); text fields
carry `*`-wildcard placeholders. The stale sparse-page comment on `useAuditTrail`
was re-baselined to the post-#2 dense-fill behavior.

## Convergence — what is and is not root-caused

- Fixed and proven by regression: retry flooding (1a), stale-group display
  (1b), tombstone origin reuse (Bug 2), and the adoption 500 (three unmapped
  error sources above; `internal/store/adapter_bug744_test.go`
  `TestAdapterAdoptionLedgerCollisionIsConflict{SQLite,Postgres}` +
  `internal/server/errors_internal_test.go` provider-busy/superseded rows).
- **The 500 is the most likely root cause of Bug 1's re-conflict.**
  `EnqueuePublished` bumps `generation` on every publish
  (`repos_adapters.go:746/759`). If every `Adopt` 500'd, the adoption tx rolled
  back (including its ledger `owned` writes and the conflict `adopted_at` mark),
  so the names were never actually adopted — and each publish-driven generation
  bump re-ran the converge and re-conflicted the same names (PROD_SSH_HOST at
  gen 1, again at gen 2, PROD_SSH_KEY ×3 by gen 4). The empty audit trail is
  consistent with "every adoption rolled back." This supersedes the earlier
  "worker re-conflict after adoption" hypothesis: the worker analysis assumed a
  *committed* adoption; if adoption never committed, there was no owned ledger
  row for the worker to load, so it kept reserving fresh.
- **Unconfirmed:** *which* of the three sources fired in Marc's run. All three
  reduce to a routine 409 now, so the user-visible dead-end is resolved either
  way. To close it precisely, batch for Marc (below).

## Validation

- `go test ./internal/store/ -count=1`: green (SQLite legs + migration-checksum
  gate).
- `HIKYO_TEST_POSTGRES_DSN=... go test ./internal/store/ -run Postgres -count=1`:
  green.
- New regressions (`internal/store/adapter_bug744_test.go`), all PASS on both
  engines where dual: `TestAdapterOriginReusableAfterTombstone{SQLite,Postgres}`,
  `TestAdapterConflictInsertIdempotentAcrossRetries`,
  `TestAdapterConflictsHideStaleGeneration`,
  `TestAdapterAdoptionLedgerCollisionIsConflict{SQLite,Postgres}`.
- Adoption-500 mapping: `internal/server/errors_internal_test.go`
  `TestWirePolicyClassifiesWrappedErrors` gains `adapter.ErrProviderBusy` and
  `adapter.ErrSuperseded` → 409 conflict cases (green). `go vet` on
  store/service/server clean; full SQLite `store/service/server/lint/adapter`
  suite green; Postgres adapter regressions green.

### #2/#3 audit filtering (this change)

- SQLite `go test ./internal/store/ ./internal/service/ ./internal/audit/
  ./internal/server/ -count=1`: green. Same four packages with
  `HIKYO_TEST_POSTGRES_DSN=…`: green (`store 74.9s`, `service 73.4s`,
  `audit 0.2s`, `server 63.1s`). PG isolation suite
  (`go test ./internal/isolation/ -count=1`, CI runs it with `-timeout 150m`):
  `ok … 920.743s` (~15 min; the go default 10 min timeout is too short — use
  `-timeout 30m` locally).
- New regressions: `internal/store/repos_audit_test.go` `TestMatchGlob`
  (exact-when-no-star, prefix/suffix/mid-segment, empty runs),
  `TestAuditFilterMatches` (glob fields + `Outcomes` set membership + `Actor`/
  `CorrelationID` stay exact), `TestNormalizedOutcomeAndActorName` (one outcome →
  scalar `filter_outcome`, several → `filter_outcomes` list; `filter_actor_name`
  recorded); `internal/service/audit_internal_test.go` `TestFillPageDensePaging`
  (#2 no-empty-page fill loop + budget-truncation cursor) and
  `TestFillPageKeepFilter` (the `actorNameKeep` post-`Matches` pass, hand-fed
  id→name map, no DB — no real-authorizer e2e leg: the isolation fixture's
  principals table has no display-name column, so a live-name test would need
  account+name seeding for no extra coverage over the hand-fed map).
  `AuditFilter.Matches`/`MatchesActorName`/`Selective`/
  `Normalized` are pure and engine-independent by construction (invariant 8), so
  a `forEngines` leg would add nothing over these Go unit tests.
- **Outcome params are a CLOSED enum enforced by contract validation, not by the
  handler or the audit registry.** `api/openapi.yaml` declares both `outcome`
  (scalar) and `outcomes` (repeated, `explode: true`, `style: form`) with a
  closed `enum:` (and the array's *items* too), so `openapi3filter.ValidateRequest`
  — run by `validateAgainstContract` *before* the handler and before auth —
  refuses a stranger with a 400 naming the offending param. Verified empirically
  and pinned by the new `internal/server/audit_contract_test.go`
  `TestAuditOutcomeParamsAreAClosedEnumAtTheContract` (`?outcome=bogus`,
  `?outcomes=bogus`, `?outcomes=denied&outcomes=bogus` each → 400 `bad_request`
  with `Error.Detail` naming the member). Consequences that shaped the change:
  `mergeOutcomes` only de-duplicates (≤6 licensed values ever reach it, no bloat
  vector); the `filter_outcomes` registry entry needs no `Enum`/`MaxLen` (adding
  one would turn a bogus value into a **500**, not a 400 — `Query` self-audits
  inline via one fail-closed `InsertTenant`, whose payload-validation error
  propagates as 500); no handler-level outcome validation was added.

## Open questions for Marc (batched, non-blocking)

- Pod log line at the 500 timestamp: `wireErrorFor` logs the cause and never
  returns it, so the log names which source fired — a pg constraint name (ledger
  collision), "adapter: provider write is still in flight" (ErrProviderBusy), or
  "adapter: target generation superseded" (ErrSuperseded). Any one ends the
  which-fired question.
- On the live DB for the affected target:
  `SELECT id, active_job_id, sync_status, generation FROM adapter_targets WHERE id=…`
  and `SELECT target_id, state, surface, normalized_name FROM adapter_ledger
  WHERE normalized_name IN ('PROD_SSH_KEY','PROD_SSH_HOST')` — confirms whether a
  leftover ledger row was the collision.
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
