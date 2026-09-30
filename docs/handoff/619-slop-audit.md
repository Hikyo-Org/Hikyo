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
| Hand SQL versus system-architecture ADR | All 359 baseline selectors retired; 385 new paired queries plus 9 PostgreSQL-only; 89 exact engine-protocol owners retained | sqlc migration implemented; general raw execution rejected in CI |
| Resolver forwarding surface | 212 delegates generated; 37 transformed/policy methods retained; public surface unchanged | Explicit private allowlist implemented |
| Authz/OpenAPI registry ownership | All 431 ordered wire entries preserved in generated static metadata | OpenAPI-derived metadata implemented; invalid inputs fail generation/CI |
| MachineAccess decomposition | Route reduced from 3,943 to 1,011 lines plus 8 feature modules | Feature-family split implemented; org/project/session confirmation reset covered |

These were the four holds at the start of the mechanical cleanup. Their owner
selections and subsequent implementation are recorded below. They are not
counted as safe deletions, and this pass does not claim the repository has zero
duplication or close #619.

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

### Architecture decision session, 2026-09-30

The owner selected **SQL option A: migrate the remaining inline store queries
to sqlc**, retaining the existing SQLite/PostgreSQL contract checks, transaction
boundaries and dual-engine behavior. PR #634 already enforces contracts for
generated queries; it does not cover the inline `.SQL(...)` surface. The
system-architecture ADR already chooses sqlc, so this selection implements that
decision rather than reopening the ORM choice. The owner also selected named,
CI-enumerated engine-protocol exceptions for operations sqlc cannot represent:
PRAGMA/VACUUM, COPY, migration DDL and historical-schema catalog inspection.
Ordinary runtime/data queries still migrate; general raw-SQL access is prohibited.
For complex tenant-scoped queries beyond the bounded analyzer, the owner selected
exact SQL/API/authority pins with both-engine valid-access and cross-scope refusal
tests. These queries remain tenant-scoped; they are not instance exemptions.
The owner selected **resolver option B: generate explicit forwarding wrappers
from a reviewed allowlist**. Keep the concrete resolver private, preserve the
current exported method surface and renames, and leave policy-bearing methods
handwritten. Adding a resolver method must not automatically expose it to
service callers. Embedding is rejected because it promotes currently hidden
methods and exposes the resolver pointer.

The owner selected **wire-registry option A: OpenAPI owns HTTP class and
single-operation linkage**, with invalid declarations, conflicting exceptions
and stale generated output rejected in **CI**, not by a new startup check.
Generate static Go HTTP metadata from the contract. Keep direct audit events,
multi-operation exceptions and non-contract surfaces explicit; retain
independent router coverage, authorization and audit invariants. This decision
does not remove existing startup safety checks elsewhere in the application.

The owner selected **MachineAccess option B: split by feature family**. Keep
route composition, tab selection, project/session boundary resets and
display-once lifecycles together. Move cohesive panels and dialogs without
changing URLs or behavior. Replace the mirrored reset-test host with actual
route coverage for dialog closure and secret clearing across project/session
changes.

All selections are recorded in `docs/adr/generated-boundaries.md` and implemented
locally on the same PR. All 359 original inline store selectors are removed;
store-wide `.SQL`, `.SQLPerEngine` and `.Placeholders` call counts are zero.
Retired dialect, placeholder-rewrite and raw adapter bridges are removed. The
final query inventory is 1,045 named SQLite queries across 50 source files and
1,054 named PostgreSQL queries across 51 files. Engine protocols retain named
CI-checked owners (89); complex scoped queries retain 66 exact SQL/API/authority
records covering 33 query names per engine and direct positive/refusal tests.

Resolver forwarding now generates 212 explicit delegates; the static wire table
contains 431 entries. Two actual runs of each sqlc/authz generator leave all 108
generated files byte-identical, including their file inventory. Focused core
scope/roundtrip, SSH lifecycle and Dynamic fencing/subsecond checks pass against
both engines after bridge removal (39.162 seconds). Full Store Adapter and
both-engine GitLab/Adapter checks pass after the runtime migration (51.009
seconds); direct move/config controls and refusal checks also pass on both
engines. Resolver, wire metadata and MachineAccess focused checks pass locally.
Web typecheck, lint and 1,321 unit tests pass after the organization-only
boundary regression was added. The renewed provider sensitivity review matches
its pinned source hash. PKI and transit direct foreign-write regressions pass on
both engines, with zero-row/typed refusals and unchanged durable state. The full
lint suite and fast preflight (format/import/build/vet/API/client/web) pass.
Direct generated-query evidence is separate from supplementary service flows;
flow-only references cannot satisfy the direct-proof check. Native Opus
implementation R2 verified all behavior fixes and requested one negative
regression for that evidence distinction; it now passes. Final native Opus implementation R3 is CLEAN and architecture R3 is SOUND.
The canonical core-package script passes with the digest-pinned PostgreSQL
target. Latent generic-helper and build-context guard notes have local fixes
and regression coverage. The separate guard code loop remains capped at R3
CHANGES with its prescribed fixes verified locally, not a fourth CLEAN pass.
Exact-head CI results are recorded per revision in PR #843; this is not merge or deployment evidence.

### Subsequent review fixes

Architecture revision `4f1a2d4f` passed 48 remote checks with 2 intentional skips,
including all isolation and race shards. CodeRabbit's later requested fixes add
checked DEK-version reads, preserve/display carried bigint federation pins and
refuse lossy replacement before any request, cover hidden embedded resolver
fields, retain transit admission serialization while permitting foreign-key
readers, normalize SQL terminators through sqlc, and correct the privacy alias
comment. The same DEK/row-version checks also cover all 16 instance reencryption listers;
PostgreSQL INTEGER input narrowing is checked before execution.
Both-engine corruption and admission-limit tests and the PostgreSQL
lock compatibility regression pass. Native followup R3 is CLEAN. Full store/authz/generator, full lint and 1,325
web tests pass locally. Subsequent revision evidence is tracked in
PR #843; earlier-head green checks cannot stand in for the final head.

### Validation and review of the initial mechanical cleanup

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

## Historical architecture holds, superseded on 2026-09-30

The original series left these four decisions for the owner. All four directions
are now selected in [generated-boundaries](../adr/generated-boundaries.md),
with implementation and verification continuing in PR #843. This list records
the earlier audit; it is not a current exclusion.

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

## Maintenance cost and merge hold

The owner holds the merge while reviewing the maintenance cost. At head
`969548a`, this PR added 55,455 lines and removed 10,638. Generated Go accounts
for 27,635 added lines; handwritten runtime and other files still grew by
2,989 lines, and SQL source added 2,861. Moving SQL and splitting a route do not
by themselves establish reduced total complexity. The remaining growth buys
typed engine boundaries and the selected CI proof boundaries; it remains a
review tradeoff rather than a code-deletion claim.

The additional simplification removes 188 redundant same-name resolver target
annotations while retaining all 24 renames. Generated forwarders remain byte
identical. It removes 146 no-op result temporaries/conversions and replaces 17
copied row literals with compiler-checked struct conversions. Existing
zero-result-on-error guards, actual PostgreSQL widening, timestamp and JSON
conversions remain explicit. These changes remove 403 handwritten runtime
lines without adding a mapping abstraction or another generator.

Scoped query metadata now has 33 shared authority/test records with separate
SQLite and PostgreSQL SQL/API hashes. All 66 prior hash/authority/evidence
records are preserved exactly after expansion. Missing or unknown engines and
empty hashes fail CI. The inventory shrinks by 94 lines; the loader and its
negative tests consume part of that saving. Raw protocol/helper/build-context
pins and the historical annotation boundary remain intact.

Native simplification review R1 found malformed forwarding comments could fall
back to same-name forwarding, plus the preexisting inventory reader accepted
duplicate and unknown JSON members. Both are now refused with negative tests.
The generator and scoped inventory reader share `definitions.DecodeStrict`,
removing the generator's separate JSON token walker instead of copying it into
the scoped inventory checker. Shared decoding also rejects trailing content and
case-variant duplicate fields. R2 found detached directive comments; the parser now accounts
for every directive in the AST comment inventory and refuses unattached ones.
These failures remain generation/CI failures.

Simplification review R3 returned CHANGES for an attempted reuse of the artifact
decoder in the raw protocol inventory's CI reader. Its case folding rejects two
distinct pinned Go helpers, `WithExistingProjectSealer` and
`withExistingProjectSealer`. The prescribed bounded fix is applied: revert only
that optional decoder swap, leaving the original raw reader, all helper pins and
the shared artifact utility unchanged. A schema-aware shared decoder would add
scope and require further review; it is not part of this simplification. The
original guard R3 and this prescribed R3 fix retain human disposition, with no
fourth native round and no whole-PR CLEAN claim.

After that prescribed revert, full lint/authz/generator/definitions checks pass,
and full generation/supply-chain preflight passes in 154 seconds with no
generated drift. Full store checks and ten both-engine scope/runtime groups
passed on the unchanged runtime delta. Exact pushed-head CI remains a separate
live fact in PR #843; no earlier revision's result proves a later head.

The Jev recheck identified no concrete missing test or remaining original guard
fix. All eight direct test groups behind the 66 scoped pins ran afresh with
both database engines and no skips, passing in 17.103 seconds. Lint/authz and
generator checks passed independently. Jev's original readiness score was
2.01/3 with confidence 0.01 and probabilities 3% blocked, 29% not ready, 31%
nearly ready, 37% ready. A focused factual recheck returned 0.23 probability of
a concrete coverage gap, 0.27 of an unaddressed original guard fix and 0.89 of
pending human disposition. These advisory probabilities do not establish a
defect or clear the review cap. The original guard R3 CHANGES remains for human
disposition; it is not relabeled CLEAN. New simplification checks and exact-head
CI are recorded separately in the PR. Merge remains held by the owner.

## Historical implementation and review evidence

Moved from the ADR during the Opus maintenance review. Counts and results below
refer to their recorded revisions, not the current head.

## Local implementation outcome, 2026-09-30

All 359 original store `.SQL`/`.SQLPerEngine` call sites are removed. The store
has zero dialect selectors or placeholder-rewrite calls; ordinary repository,
worker and coordination queries use typed per-engine sqlc owners. The retired
raw adapter bridge and handwritten row scanners are removed. Query sources now
contain 1,045 SQLite statements in 50 files and 1,054 PostgreSQL statements in
51 files; engine-specific lock/clock protocols account for intentional differences.
Catalog, migration and other non-static engine protocols retain 89 named
CI-checked owners rather than becoming general query access. Complex scoped
queries have 66 exact records covering 33 query names in each engine, with
individualized authority and direct owning/refusal regressions. Direct SQL
evidence and supplementary service-flow evidence are recorded separately; flow
coverage cannot substitute for a direct query reference. Protocol hashes bind
resolved named constants and 986 transitive helper records (242 distinct
helpers), including the default/UI/macOS/Windows source variants.

The authz generator emits 212 allowlisted resolver delegates and 431 static wire
entries. Two consecutive actual runs of `go tool sqlc generate` and
`go run ./internal/authz/gen` leave all 108 generated files byte-identical,
including new files, against the current worktree.

Focused both-engine checks pass for migrated core, move/config, runtime, PKI,
transit and coordination families. Direct core scope tests cover owning controls,
foreign org/project/environment refusal and credential-write no-mutation;
runtime checks cover immutable claimed-chain refusal, fencing and SQLite
subsecond deadline ordering. PostgreSQL integer inputs reject overflow before
execution. These local results do not replace final combined review, full-suite
validation or exact-head remote CI.

## Adversarial review

Native Anthropic Claude Opus 5.5 high R1 reviewed this OpenAI-authored draft and
returned OBJECTIONS without reopening the owner's four choices. Blocking
findings require a raw-SQL regression gate, explicit dynamic-list bind handling,
proof-binding evidence and per-query annotation criteria, removal of the wire
registry's init panic, and preservation of authz-owned forwarding documentation.
The signature/doc interface and CI-only table-validation revisions address the
last two. The implemented query gates, typed list binds and per-query scope
evidence address the SQL design objections locally. Final combined adversarial
verification is recorded below.

The separate code review of the CI guards reached its three-round cap with
R3 CHANGES. Its remaining parser mismatch is fixed by matching sqlc's raw-line
comment stripping, with multiline-literal UNION/upsert regressions; the
PostgreSQL array fixture now uses the PostgreSQL grammar. Focused regressions
and the full lint suite pass after both fixes. This is local fix evidence, not
a fourth review or a CLEAN guard verdict.

Architecture R2 returned SOUND: all five original design objections were
resolved. Its implementation checks required helper/constant dependency pins,
direct query references in evidence bodies, and exact HTTP-extra route
validation. These revisions pass the full lint suite and generator tests;
final R3 verification is recorded below.

The separate implementation code review R1 returned CHANGES. Its sensitive
provider-file inventory was re-reviewed and refreshed, foreign PKI/transit
writes now prove direct execution and unchanged state on both engines, direct
and flow evidence are distinct, and adapter retry timestamp parsing retains the
original queued-with-attempts condition. Regression coverage pins the SQLite
six-digit microsecond timestamp and adds organization-only dialog resets. Web
typecheck, lint and all 1,321 unit tests pass, as do the focused both-engine
PKI/transit tests and full lint suite. Final implementation verification is recorded below.

Final native Opus implementation R3 returned CLEAN and architecture R3 returned
SOUND. The reviewer independently mutated the flow-only evidence guard and three
real protocol SQL constants/helpers; the corresponding checks failed, including
all four build contexts. Its two latent guard notes are addressed locally:
generic receiver keys omit type arguments to match instantiated method
references, and unknown/duplicate build dependency contexts have negative
regressions. These local revisions are not a fourth review of the capped guard
code loop, whose R3 CHANGES disposition remains explicit above.

The canonical core-package script passes on the task's digest-pinned disposable
PostgreSQL target. Earlier monolithic isolation and broad race commands reached
their timeouts during active tests; focused regressions and targeted race checks
pass. Complete isolation and race coverage remains the unchanged three/six CI
shard gate on the final pushed head. No timeout or gate was weakened.

## Subsequent review fixes

Remote validation of implementation revision `4f1a2d4f` passed 48 checks with
2 intentional skips, including all three isolation and six race shards.
CodeRabbit then requested followups: hidden embedded resolver exposure,
corrupt DEK-version narrowing, transit admission lock compatibility, SQLite
statement terminators, and unsafe carried federation pins. These are addressed
with embedded-field regressions, checked DEK reads on both engines, a PostgreSQL
lock-compatibility/mutual-exclusion test, regenerated SQL, and exact bigint
preservation with pre-request refusal. The activity result-alias comment is
corrected. The earlier `d747caf6` CI result does not prove the architecture or
these followups. CI and review results for each later revision are recorded in
PR #843; they must not be inferred from an earlier head.

The followup review also found the same version-narrowing flaw in the instance
reencryption inventory. All 16 versioned instance listers now share checked DEK
and row-version decoding. PostgreSQL INTEGER limits and self-config version
inputs are checked before narrowing; BIGINT inputs retain their full range.
Corrupt PKI issuer versions are refused by signing, CRL and reencryption reads,
so a wrapped value cannot be mistaken for an already-current DEK during retire.

Native Opus followup R3 returned CLEAN after the reencryption read/input fixes.
It independently checked build/vet, stored-integer boundaries and binding form
regressions. The task's separate dual-engine checks cover PostgreSQL, which the
reviewer's environment did not run. Full store/authz/generator, full lint and
1,325 web tests pass locally; later-head CI results remain in PR #843.

## New tenant-scope exemptions: explicit security-review boundary

The review is correct: the original migration adds 175 `instance-scoped`
engine/query records across 92 query names, against 764 baseline annotations.
None of those 92 names enters the unchanged 381-name legacy grandfather list.
A reason and a SQL hash are evidence to review, not approval by themselves.

The current sources and `internal/isolation/testdata/annotated_queries.json`
are the exact review inventory. Semantic worker renames are replacements, not
new authority. The normal pin invariant refuses changed SQL, reasons and names;
the tenant analyzer separately checks ordinary proof-bound queries. Do not use
the new pin updater to add tenant annotations or expand the legacy list.

| Authority family | Engine records / names | Review boundary and owning evidence |
|---|---:|---|
| Adapter claims, settlement and gauges | 16 / 8 | Claim ID/lease owner and immutable chain; fenced settlement transaction; host-only provider switch rejects network operation contexts. `internal/store/adapter_runtime.go`, adapter runtime scope/refusal tests. |
| Admission and audit clocks/barriers | 4 / 4 | Connection/session writer-lock protocols, no tenant projection. `internal/store/admission_serialized.go`, audit-export transaction tests. |
| Coordination and singleton lease | 54 / 29 | Installation HA/leader metadata and admission counters; owner/fence/expiry, membership and MCP locks, topology generation checked. `internal/store/coordination*.go`, coordination and pool-replacement tests. |
| Global PKI repositories | 56 / 28 | Each repository operation verifies its instance/configuration or closed signer atom; CA policies/keys are instance-wide. Certificate mutations retain preceding scoped CAS and same-transaction audit. `internal/store/pki_store_queries.go`, PKI generated-scope and issuer-corruption tests. |
| Restore diagnostics | 1 / 1 | Recovery-admitted migration seed inspection under the restore transaction locks. `internal/store/backup.go`, backup diagnostics tests. |
| Dynamic, PKI and SSH runtimes | 34 / 17 | Closed scheduler/metrics owners; selected immutable tenant chain rechecked before writes, expiry/state/fence CAS and transactional audit. Runtime generated-scope and end-to-end tests. |
| Transit scheduler and gauges | 10 / 5 | Verified closed scheduler authority; per-candidate tenant transaction, label-free aggregate metrics, no material projection in gauges. `internal/store/transit_store_queries.go`, transit generated-state, lifecycle, trim and create-limit tests. |

Security sign-off on these exemptions remains an explicit merge-review item.
Passing tests do not supply that sign-off. Merge remains held by the owner.

## Opus maintenance review: option A implementation

The owner selected the recommended cleanup while holding merge. Resolver option
B stays locked; narrowed-interface embedding was not introduced. The review
correctly identified that the earlier runtime total omitted lint. Report runtime
including lint, and report generator inputs/tooling, tests, SQL and generated
output separately. Replacing JSON with compile-checked Go metadata also moves
lines between categories; that movement is not removed complexity.

Authz exceptions now live in generator-owned typed Go maps with all 379 historic
rationale comments restored. The compiler and regression fixtures replace the
custom signature reparse/overlay typechecker. The private 212-method allowlist,
24 renames and wire output remain intact. Blank comment filler is removed. Exported generated method docs remain available
to Go documentation; the private allowlist is their canonical source. The `-check`
flag is used by freshness tests and native validation, so it remains.

One reviewed inventory/loader now owns cross-engine contracts, engine protocols,
scoped queries and raw-protocol pins. Shared helper hashes remove 744 repeated
hash values while retaining each owner's named dependencies and one Windows
variant. The explicit `-update-reviewed-pins` test updates existing hashes for a
reviewable diff, refuses new raw owners/callers, and does not modify tenant
annotations or authority reasons. Pretty-printed JSON is larger in lines after
normalizing the old compact Go literals; report bytes and repeated values too.

All three SQL quote consumers share one scanner. Restricted raw method references
use the same receiver-qualified owner name as declaration pins, with regression
coverage. Strict JSON duplicate handling follows the destination schema: ordinary map keys
remain case-sensitive; struct-field duplicates remain case-insensitive. Promoted
fields follow direct-field/tag dominance and anonymous-pointer cycles terminate.
Opaque custom JSON destinations and authenticated manifest authority retain
conservative duplicate checks. Existing
importer unknown-field, duplicate-field and trailing-content refusal is preserved.

Store has 98 list mapping loops sharing one error-aware, nil-preserving helper.
Transit uses embedded generated models and one key converter per engine; corrupt
versions fail before compromise/fence mutations. Provider and SSH metadata keep
narrow projections rather than fetching ciphertext to use full-row embedding.
Only transit identity/exportable/purge flags expand the key projections; filters,
locks, ordering and limits remain unchanged. Dead scanner wrappers, collectStrings,
Scan-shaped assignments, unused TargetID and no-op aliases are removed. Worker
job names describe retry, completion and cancellation rather than numeric suffixes.

Web status dispatch retains status-specific safe detail and passkey/uncertain-mint
messages. Eleven standing ceremony notices share markup without state or secret
ownership. Sensitivity hashes were refreshed after source comparison. Isolation
refusal helpers preserve each owning and foreign-axis case; the mixed followup
file is split into PKI stored-version, transit admission and reencrypt input tests.
CLI help spellings and safe credential channels again have independent policy
assertions, so regenerating a golden cannot erase them.

Validation and the new bounded cross-provider review are recorded in the PR for
the exact pushed revision. Earlier capped R3 reports remain historical; this
implementation is a new maintenance-review delta and is not a fourth pass on
their old targets. Security sign-off on the 175 exemptions and owner merge hold
remain explicit.

The new maintenance R1 returned CHANGES. Exported generated method docs and
negative generator fixtures were restored; failed pin decoding exposes no
exception maps. The updater deep-copies its input and checks unchanged owners,
caller allowlists, reasons, query authority, engines and evidence before writing.
Stored integer checks share one bound predicate while preserving both historical
corruption diagnostics. Native fix verification and remote exact-head evidence
remain in PR #843 rather than this architecture record.

The exhaustive 175-record/92-name exemption review found one reason overstatement:
`AdapterWorkerCompleteJob` intentionally settles an owned stale-generation job
without requiring the superseded target fence. Both-engine annotation reasons
now state that exception. The closed worker must retain the unchanged job returned
by ClaimDue; abort settlement does not independently validate a caller-modified
job chain. Both-engine regression coverage proves wrong-owner refusal, unchanged
newer target and a single audit carrying the original claimed chain. SQL, generated
API, annotation names, classes and hashes remain unchanged by this correction.
Security sign-off remains an owner review item; this evidence is not approval.
