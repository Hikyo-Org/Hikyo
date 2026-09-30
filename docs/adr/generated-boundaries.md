# Generated boundaries and MachineAccess decomposition

Status: implemented and selected architecture verified on 2026-09-30. CI results
are tracked for each exact revision in [PR #843](https://github.com/Hikyo-Org/Hikyo/pull/843);
the earlier `d747caf6` result covers only the initial mechanical cleanup. Addresses the architectural holds in
[#619](https://github.com/Hikyo-Org/Hikyo/issues/619).

## Context

The mechanical cleanup in PR #843 preserves four larger architectural surfaces.
The owner selected SQL option A, resolver option B, wire-registry option A with
CI enforcement, and MachineAccess option B. This decision records those choices
without treating historical audit estimates as current migration inventory.

## Decisions

### Store queries use sqlc

Implement the existing system-architecture ADR: move static inline repository
queries into per-engine sqlc source files and consume generated typed results.
Keep SQLite and PostgreSQL, existing public repository contracts, proof-derived
tenant bindings, transaction retry/isolation semantics and audit ordering.
Reuse the cross-engine contract and tenant-predicate checks added by PR #634;
do not replace sqlc with an ORM or admit unrestricted handwritten scanning.

Inventory every raw execution surface, not only calls to `.SQL(...)`. Dynamic
query construction, bulk bind lists and catalog inspection require explicit
disposition before claiming complete migration. Prefer sqlc-supported query
forms; a genuinely non-static engine/catalog operation cannot silently become
an analyzer exception. The generator owns generated files, never hand edits.

The owner explicitly selected named engine-protocol exceptions for PRAGMA/VACUUM, COPY,
migration DDL and historical-schema catalog inspection. Preserve these through
an explicitly enumerated, CI-checked function inventory, with identifiers
admitted by the existing authenticated catalog/session boundaries. No broad
package or filename permission and no general raw-SQL accessor is granted.
Ordinary runtime/data queries, including static coordination statements, still
move to sqlc. A raw-execution regression gate covers method aliases and callback
paths; exceptions state their protocol purpose and exact source owner.

Preserve batch semantics for dynamic IN-lists. Teach the generated bind checker
only the pinned sqlc slice-expansion shape, rejecting unknown rewrites and list
parameter swaps. PostgreSQL array and SQLite list parameters need explicit
logical contract matching, not a generic alias exception. Query annotations
require a per-query justification and exact content pin: proof-free global
workers retain their existing closed authority; tenant-request queries cannot
be relabeled instance-scoped to avoid analysis. Add both-engine positive controls
for each migrated family's proof-derived chain values. PostgreSQL-only generated
locks and clocks receive exact statement/API/reason pins; SQLite write admission
is their behavioral counterpart, never a fabricated no-op query. New scope
annotations carry explicit authority reasons and content pins; legacy annotations
retain their previously reviewed inventory.

For complex tenant-scoped statements beyond the bounded analyzer, the owner
selected exact per-engine SQL/API/authority pins and both-engine positive and
cross-scope refusal regressions. These remain unannotated tenant-scoped sqlc
queries. The reviewed inventory references the actual regression tests; removed
queries, missing tests, changed SQL, unknown binds or changed generated APIs fail
CI. Review must verify the owning chain and any deliberately distinct old/new
environment bridge. It cannot grant general raw SQL or relabel tenant requests
as instance operations.

### Resolver forwarding is generated from an explicit allowlist

`TxAuthorizer` retains its private concrete resolver and its reviewed public
method surface. Generate only pure delegation methods from an explicit list of
public method name, target resolver method, signature and original authz-owned
documentation. A private Go interface is the reviewed allowlist; a forwarding
directive is required only when the resolver target has a different name.
`go/types` checks signature identity including aliases.
Duplicate public names fail; multiple wrappers may intentionally target the
same resolver method. Preserve renames and documented contracts. Methods with
constant injection or argument transformations stay handwritten, as do methods
with authorization, proof construction, denial capture, session fencing or other
policy.

New resolver methods do not automatically enter the forwarding list. Invalid
targets, signatures, duplicates and collisions with handwritten methods fail
generation or CI. Generated output must be deterministic and freshness-checked.
Boundary coverage must detect added public methods or an exposed resolver.

Rejected: embedding the resolver. It promotes currently hidden method names,
exposes the resolver pointer and implicitly expands the reachable surface when
the resolver changes. Keeping all delegates handwritten retains avoidable
maintenance without adding boundary protection over a reviewed allowlist.

### OpenAPI owns HTTP wire metadata, enforced before publication

Generate static Go class and single-operation linkage for contract HTTP routes
from OpenAPI. Invalid declarations, absent required metadata, duplicate route
keys, unknown classes/operations, conflicting overrides and stale output fail
CI. Emit a plain static table; remove this registry's existing
`mustNewWireRegistry` package-init panic and perform its validation in generation
and CI. Ordinary startup consumes compiled metadata. Existing `api.Warm` and
unrelated runtime safety checks remain intact; there is no new runtime contract
parse or metadata-validation path.

Keep audit-event linkage, multi-operation route exceptions, operational HTTP
routes and CLI/MCP/job/SSE surfaces explicitly declared where OpenAPI cannot
represent their current contract. Define conflict rules narrowly: extensions
may supply missing facts, not silently replace contract-owned values. Preserve
ordering/set semantics required by existing completeness checks.

Keep independent actual-router coverage and authorization/audit invariants.
Remove redundant comparisons only where both sides would now be generated from
the same source. Derivation does not prove handler dispatch or audit emission.
Do not generate OpenAPI from Go: the API/CLI ADR already gives the hand-reviewed
contract ownership of the wire surface.

### MachineAccess is decomposed by feature family

Keep one route composition owner for tab selection, project/session boundaries,
dialog resets and display-once mint lifecycles. Move cohesive account, mint,
federation binding, grant, provider and lease panels/dialogs to focused modules.
Avoid a generic dialog framework or a new cross-feature state abstraction.

Preserve URLs, visible text, layout, query keys, invalidation, readiness gates,
refusal behavior and secret custody. Navigation or session replacement must
close scoped dialogs and discard minted secrets even when async work completes
after the boundary. Tests must exercise the actual route rather than a mirrored
test host; verify desktop/mobile states and existing dialog interactions. The
machine-reveal confirmation surviving those boundaries is a reproduced defect
included in this change. Use one NUL-separated boundary signature for the route
reset and policy-component key. Review moved sensitive state ownership before
refreshing sensitivity-inventory entries or hashes.

## Validation and delivery

Each implementation slice includes useful regression coverage and generated
freshness checks. SQL changes require both engines' conformance and isolation
checks; generated seams require negative evasion and drift cases; UI changes
require typecheck, lint, unit and affected browser tests. Retain signatures and
DCO, fold changes into PR #843, verify its exact-head remote CI, and keep the
human merge gate. Cross-provider design and code findings remain blocking until
resolved; record objections and disposition here before locking this ADR.

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

## Consequences and reversibility

There are more generated files and focused UI modules, with fewer manually
repeated contracts. Reviewed inputs become the source of truth; generated LOC
are not counted as removed complexity. Full SQL migration carries substantial
behavioral risk and must preserve transaction and proof ownership per slice.
No schema migration, authorization widening, API change or deployment is
authorized by this cleanup alone.

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
