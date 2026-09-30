# Issue #619: slop audit remediation

## What this is

A repo-wide audit for agent-accumulated slop (dead code, useless tests, copy-paste
duplication, wrappers, pre-modern idioms, doc rot) and the PR series that removes
it. The full audit report with per-finding grep evidence is the body of #619.
Each PR below appends a "what landed" entry here.

## Remaining-work recheck, 2026-09-30

Baseline: `cebc7d4b` (current `main` when this pass began). The six original
remediation PRs and the separate production-verification fixes are already
merged. The September audit is evidence to recheck, not a current deletion list.

### What this pass changes

- **C-cli:** four atomic writers use `securefile`, including directory fsync;
  session/trust writes no longer use predictable `.tmp` paths. A symlink
  regression verifies existing victim bytes remain unchanged. Import artifacts
  share the canonical strict JSON decoder while retaining duplicate-member,
  malformed/trailing, version-mismatch and connector-specific refusal codes.
  Import summary names and artifact tables have one owner.
- **C-web:** environment and retention query options share resource owners;
  caller-specific transport, enabled gates and invalidations remain intact.
  Thirteen simple refusal dispatchers and 43 repeated messages share helpers.
  A temporary before/after characterization compared each dispatcher across
  40 API/non-API inputs, including empty details and server/network fallbacks.
  Passkey completion is shared without combining the normal/adapter purpose
  binding; regression coverage checks binary response payload and session fences.
  Timestamp rendering and JSON syntax parsing have shared owners.
- **B1/B2:** route tests use the existing mount/rerender/unmount harness; deferred
  promises share one helper. Redundant registry count, embedded-file equality,
  help-substring and copied-prototype-constant checks are removed. Useful guards
  are strengthened: typed lint replaces two untyped service seam checks and a
  repository byte scan for test-only observers; applied database schemas and
  parsed API fields replace the masked-state source scan. The acceptance ledger
  points at the new schema test file.
- **Preview fixture:** the prototype identity omitted the required delivery
  reporting grant. It now satisfies `zWhoAmI`, with a contract regression, and
  the browser opens the app instead of remaining on the reconnect screen.

### What remains and why

| Remaining item | Current evidence | Disposition |
| --- | --- | --- |
| Hand SQL versus system-architecture ADR | 284 `.SQL(...)` call sites in non-test `internal/store` files; count is call sites, not estimated LOC | Owner selects sqlc migration or documented dialect/analyzer policy |
| Resolver forwarding surface | 221 simple `TxAuthorizer` methods delegate to `a.r`; excludes methods with real policy | Owner selects explicit set, generation, or embedding after trusted-set analysis |
| Authz/OpenAPI registry ownership | Existing registry still explicitly links route, operation and audit events | Owner selects canonical derivation direction; no boundary widened here |
| MachineAccess decomposition | `web/src/routes/MachineAccess.tsx`: 3,943 lines | Owner-approved split remains held |

The four holds above are preserved from the accepted audit disposition. They
are not counted as safe deletions, and this pass does not claim the repository
has zero duplication or close #619.

Formerly flagged items intentionally retained after source verification:

- Environment lifecycle now has a rendered settings surface; #623 is closed.
- Metric tests scrape actual emitted metrics; migration uniqueness, SAML guarded
  constructor wiring, SCIM acceptance coverage and fixtureref lexer negatives
  protect real contracts. The earlier lexer-to-regex change was rejected in review.
- Chrome identity controls remain the ADR-pinned visual surface (181 lines,
  already smaller than the original 330-line report); moving them out of the
  production tree previously broke the visual contract.
- Unknown-engine refusal, deliberate nil-preserving copies and per-task handoffs
  remain intentional. No schema migration, feature removal or authorization
  widening occurs in this pass.

### Validation and review

Local checks: TypeScript typecheck, full web lint, all 1,272 unit tests (148
files), and production build pass. All 23 migrated route-test files also pass
169 focused tests. Full conformance passes against SQLite and the digest-pinned
PostgreSQL CI image; typed lint passes its default, UI and Windows contexts.
Targeted CLI/importer/definitions/securefile/compose/authz/scanning/fixtureref/API
tests and scoped go vet pass. Graphify structural data is refreshed locally.

T3 browser verification: prototype login opens the app; project settings renders
its environment/retention controls at 1280x800 and 390x844 without horizontal
overflow. The audit page renders its existing generic failure state because
that prototype endpoint has no success fixture. These are local prototype
checks, not production deployment evidence. Native Claude Opus 5.5 high R1 found a local-interface budget-check bypass,
missing observer-owner fail-closed checks, escaped unknown-field diagnostics and
incomplete PostgreSQL schema-object coverage. Fixes and R2 verification are
recorded in the PR. The unknown-field refusal uses ASCII punctuation under the
repository's explicit no-em-dash rule; its code, field escaping and version
mismatch wording remain intact. R2 returned CLEAN; its evidence condition is
satisfied by a fresh two-engine run of both schema scenarios, including every
injected PostgreSQL default/index/view/trigger/function case. Final code was
integrated onto `0e5e9de6`; that base advance changes only unrelated lockfiles.
Exact-head remote CI is recorded in the PR.
A native macOS PostgreSQL 18.6 run failed the existing signed-schema admission
fingerprint before the scenario ran. The exact digest-pinned CI image passes the
new scenario on PostgreSQL, as does SQLite; no gate was weakened to accommodate
the native variant. The disposable database is owned by this task.

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
