# Generated boundaries and MachineAccess decomposition

Status: accepted. Addresses the architectural holds in
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
The Go compiler checks target assignability, including aliases; CI pins the
allowlisted surface and refuses collisions with handwritten methods.
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
resolved; record validation and review disposition in the PR and handoff.

## Consequences and reversibility

There are more generated files and focused UI modules, with fewer manually
repeated contracts. Reviewed inputs become the source of truth; generated LOC
are not counted as removed complexity. Full SQL migration carries substantial
behavioral risk and must preserve transaction and proof ownership per slice.
No schema migration, authorization widening, API change or deployment is
authorized by this cleanup alone.
