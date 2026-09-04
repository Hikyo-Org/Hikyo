# Issue #619: slop audit remediation

## What this is

A repo-wide audit for agent-accumulated slop (dead code, useless tests, copy-paste
duplication, wrappers, pre-modern idioms, doc rot) and the PR series that removes
it. The full audit report with per-finding grep evidence is the body of #619.
Each PR below appends a "what landed" entry here.

## Categories found

- Copy-paste duplication (~4,500 LOC): same authorize prelude 206x in
  `internal/service`, five per-engine styles in `internal/store`, wire registry
  restating `openapi.yaml`, four atomic writers in `internal/cli`, three
  `useEnvironments` hooks, 47 failure-text ladders with identical arms.
- Useless tests (~1,600 LOC): source/AST/YAML-grep tests, constant-equals-copy
  pins, compile-time-dead branches, tests of test helpers, duplicate coverage.
- Dead code (~900 LOC): symbols with zero callers, a removed feature's API chain,
  an inert prototype panel shipped to prod, test-only exports in prod files.
- Wrappers and yagni (~1,200 LOC): 186 pass-throughs, a hand-rolled TypeScript
  lexer for three string lookups, runtime checks the type system already makes.
- Pre-modern idioms (~300 LOC): `sort.Slice` for `slices.Sort`, `append([]T(nil))`
  for `slices.Clone`, manual busy/failure state around react-query mutations.
- Doc slop (~4,400 LOC): a 1,978-line session log as a handoff, executed plans,
  screenshots in git, comment shouting.

## Bugs found by the audit

1. `internal/cli/update.go` shadowed `err`; failed state reload after a
   successful refresh was silently dropped. Fixed in PR A.
2. `internal/dynamic/postgres` integration test never ran in CI; the script that
   provides its target was committed but never wired. Wired in the CI PR.
3. Audit-column invariant test pinned columns by regex over `CREATE TABLE` text
   and missed `ALTER TABLE ADD COLUMN`; stale and green. Replaced in PR B2.
4. `internal/store` AdapterRuntime opens Postgres transactions Serializable while
   DynamicRuntime uses the default level under a comment claiming parity.
   Unified in PR C-store.
5. `internal/store` wrote timestamps in three text formats; migration 00034
   exists only to repair the mismatch. One rule in PR C-store.
6. Status ledger under-reported #595. Fixed upstream in #618 before this series.
7. `scripts/ci/analysis-shards_test.sh` referenced only itself and never ran.
   Wired in the CI PR.

## Deliberately not done (owner decisions, ADR-locked)

These are architecture calls that Marc's own rules say get grilled and locked,
not changed under "trust your judgement". Options with verdicts are in #619.

1. `internal/store` hand SQL over a home-made dialect shim (~5,400 LOC) against
   the ADR that rejected hand SQL; invisible to the tenant-isolation analyzer.
2. `TxAuthorizer` 186 pass-throughs: embed the resolver or keep the explicit set.
3. `authz` wire registry derivation from `api.Operations()`.
4. `MachineAccess.tsx` split.

Also kept on judgement: `browserIPv4` (security edge), `scanning/bench` harness
(ADR SS1), all `docs/handoff` per-task files (owner mandate), the e2e flow
registry (ADR mvp-boundary S3), the SQLite/Postgres dual query sets.

## PR log

(appended per PR)

### PR A: mechanical and dead code

What landed:
- Bug fix: `internal/cli/update.go` no longer shadows `err` around the state
  reload after a release-snapshot refresh. No regression test: with or without
  the fix `NotifyUpdate` returns false with no output, so nothing observable
  distinguishes them without an injected reload seam, which was not built.
- staticcheck: every hit fixed except four deferred ones: `GetEventRecorderFor`
  (the replacement emits `events.k8s.io/v1` events the chart RBAC does not grant,
  so it needs its own operator plus chart PR), the two seam tests' `ParseDir`
  (PR B2 moves them into `internal/lint`), `fixtureref/validator.go` `ast.Object`
  (PR D replaces the lexer), and `scripts/release` (CI PR).
- Deleted or moved to `_test.go`: 18 production symbols reachable only from tests,
  two whole files holding one unread const each, the unused `RetireTier3Key`
  sqlc query in both dialects (regenerated), `Env.Home`/`Env.StateD`, the
  `bench-scan -check` flag, `readCloser`, `ErrFrozen`.
- `sigs.k8s.io/yaml` became indirect (harness test now uses apimachinery's
  yaml, which honours json tags; `gopkg.in/yaml.v3` would have silently zeroed
  every CRD name).
- Idiom sweep: `sort.Strings`/`sort.Slice` 128 to 17 (the rest are multi-key
  comparators or off-limits CI scripts), `append([]T(nil), x...)` 114 to 76
  (the rest are deliberate nil-preserving copies), one-off helpers replaced by
  `slices`, `maps`, `cmp.Or`, `strings.Cut`, `clear`, `strconv.Itoa`.
- Web: 52 files, dead exports and test-only helpers removed, 76 `export`
  keywords dropped, re-export shims removed, six dead CSS selectors and one
  token removed. The audit's claim that `@typescript/native` was unused was
  wrong (it provides `tsc` for clients/ts); kept.
- `api/parity.yaml`: `getScimBinding` now `via: [listScimBindings]`. The SPA
  never called the get-one operation; the row was held up by a dead hook.
- Kept after re-verification: `Config.SQLiteDriver` (query-count test needs
  a custom driver), `CanonicalKeySet` (now called where it was re-inlined).
