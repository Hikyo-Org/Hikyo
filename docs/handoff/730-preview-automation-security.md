# Preview automation: #721, #722, #723, #725, #730

This branch implements the five issues as one coordinated security change.
The originating tickets are [#721](https://github.com/Hikyo-Org/Hikyo/issues/721),
[#722](https://github.com/Hikyo-Org/Hikyo/issues/722),
[#723](https://github.com/Hikyo-Org/Hikyo/issues/723),
[#725](https://github.com/Hikyo-Org/Hikyo/issues/725), and
[#730](https://github.com/Hikyo-Org/Hikyo/issues/730).

## Viability and dependency order

The lifecycle request fits the existing automation capability model. Opening
machine transport admission does not grant capabilities. Environment creation
and deletion remain governed by project-wide `definitions-edit`; a `pr-*` name
is not an authorization boundary. Hikyo has no production-tier concept.
Dedicated preview projects provide the intended isolation. A machine cannot
delete a protected environment, including through a definitions plan.

Address #730 before the executable #722 guide. #721 and #725 are independent
integration prerequisites when the deployment uses cluster-CA federation or
native Kubernetes consumers. #723 is the separate alternative for previews
whose differences are public configuration only, with explicit amendments to
the flat, revision and API models. It does not provide per-preview secrets.

## Delivered behavior

- #721: instance administrators can set or clear a per-issuer PEM CA bundle.
  It replaces system roots for that issuer's discovery/JWKS fetches, including
  redirects. HTTPS, hostname verification, bounded responses and explicit
  private-network egress policy remain effective. Static JWKS rejects a bundle.
  Cached keys and the final authorization transaction are bound to CA changes.
- #730: machine credentials can reach environment metadata/create/clone/delete,
  staging/clear, copy, publish, pin-list and revision-metadata routes. Live grants,
  machine-reveal opt-in, protected publication and approval policies still bind.
  Copying secrets requires source reveal and destination reveal/publish.
- #730: machine CLI export uses delivery rather than the human historical-export
  endpoint. No reveal requests config-only projection; reveal refuses before
  output if any set secret is withheld. Machines cannot select an arbitrary
  `--revision`. The response identifies the actual selected snapshot without a
  second metadata lookup. Workload history remains pin-bound.
- #722: the discoverable preview guide resolves names to IDs, uses stable
  per-preview secrets, explicitly publishes returned draft versions, stops on
  approval requests, exports through private files, and covers teardown,
  credential revocation, source-copy omissions and capacity limits.
- #723 and #725: see the detailed
  [parameter handoff](723-environment-parameters.md) and
  [native Secret handoff](725-native-secret-types.md).

Environment teardown also releases only the deleted environment's scoped grants
and their origins, invalidating affected principals in the same transaction.
Without this, existing grant foreign keys could refuse cleanup with a conflict.
Both direct deletion and definitions-plan deletion use this cleanup.

## Parameter boundaries

Only declared `${NAME}` references in config values are substituted, once.
Secret values remain literal. Supplied values are bounded public inputs and
appear in disclosure audit records. Missing/invalid inputs or invalid resolved
config refuse the entire fetch. Inputs bind delivery cursors and operator stamps.

Declarations activate on the next publication. Each snapshot preserves its
contract and relevant config schemas, so edits cannot reinterpret historical
delivery. Contracts are bounded to 256 KiB and charged to snapshot storage
budgets. Hidden secret bytes do not contribute to a caller-visible render limit.

Parameter declaration management is CLI/API, with a narrowly documented UI
parity exception. Definitions bundles do not carry these declarations. A
parameter-template source cannot be cloned into an undeclared destination;
that operation refuses atomically. Use the parameter delivery workflow or a
concrete clone source, as described in the guide.

## Verification and delivery state

Dedicated isolation regressions cover the real machine HTTP lifecycle, foreign
scope refusal, credential revocation, protected deletion, config-only and revealed
delivery, human-only historical export, environment grant cleanup, CA changes,
snapshot parameter history, parameter audit, atomic schema refusal and bounded
contracts. CLI tests cover real command routing and secure-output refusal.

Local validation completed on 2026-09-12:

- Default Go package coverage completed with failed checks fixed and rerun.
  PostgreSQL-heavy packages were serialized after the initial combined run
  exposed checkpoint contention. The service package passed 563 tests and app
  passed 299, including actual SQLite/PostgreSQL restore and upgrade drills.
- Isolation covered 379 top-level tests across three sequential SQLite/PostgreSQL
  shards, plus one new static regression. Shards 0 and 1 passed outright. Shard 2
  passed every database case; its sole failure was an old static capability
  checker that omitted the existing conditional machine-reveal opt-in. The
  corrected checker, new negative cases, and API pin-bound-history guard passed
  separately. No runtime code changed for that final correction.
- `go vet ./...`, repository-wide gofmt, diff checks, focused race suites,
  regenerated Go/CRD drift checks, chart structural/mutation checks, generated
  TypeScript verification (20 tests), web typecheck and 959 web tests passed.
  Docs verification built 61 pages and passed navigation, policy, CSP and browser
  offline checks.
- Ordinary Standards and Spec reviews completed without unresolved code findings.
  The cross-provider review was explicitly skipped by the user.
- Existing external Forgejo, frozen-client/server and MCP deployment fixtures
  were not configured. The new native Kubernetes acceptance fixture compiles
  and is wired into CI, but local kind bootstrap failed before its tests ran.
  See the #725 handoff for exact diagnostics and the shared Docker memory limit.
  Both owned clusters were removed; the existing cluster subsequently reported
  node Ready and API `/readyz` healthy. No native consumer pass is claimed.

No push, PR, merge, deployment or external issue closure was performed.

## PR #731 review remediation

PR [#731](https://github.com/Hikyo-Org/Hikyo/pull/731) is open; merge remains on
hold. The implementation above was pushed as `ce795929`. Its native Kubernetes
acceptance job passed in CI, superseding the workstation-only limitation above.

Review fixes preserve literal `${...}` config in environments without declared
parameters and add `$${` escaping to version-1 template contracts. Pre-feature
empty contracts remain literal; no never-shipped template grammar is retained.
Clone/copy paths explicitly
refuse template sources without destination declarations. Owner draft advisories
and the publish sheet identify schema validation deferred until fetch.

Config export no longer emits per-key secret disclosure records. Supplied public
parameters produce one export-level event per successful request; revealed secrets
retain their per-key records. Environment deletion records every removed grant
using the ordinary revocation payload and invalidates the affected sessions.
Parameter and export services are wired through compile-time interfaces.

Native Secret support now requires explicit `operator.nativeSecretTypes` opt-in,
which also gates Secret delete RBAC. Missing authorized mandatory data withdraws
the target; malformed present content retains the last accepted target. CEL
admission checks UTF-8 bytes and includes the schema bounds required by
Kubernetes' CEL cost budget. Parameterized operator responses require positive
snapshot revision metadata, refusing older servers that may ignore the inputs.

API revision 3 advertises parameter operations and delivery revision metadata.
Machine export checks server compatibility and treats an unexpected conditional
response as an internal error. Empty issuer CA hashes are omitted; parameter
name/delete validation, bounded regex caching, compatible versioned contract
decoding and a shared leaf-package text sanitizer address the smaller findings.

The initial CI failures were traced to literal interpolation, config audit
cardinality, two formatting defects, and the registry fixture importing bcrypt
outside the crypto boundary. Those paths are corrected. A pinned handwritten-Go
import formatter now runs in CI; generated files remain owned by their generators.
The SQLite storage query retains its projection coroutine before grouping, as
checked with `EXPLAIN QUERY PLAN`; its anti-flattening rationale is restored.

Cross-provider review remains explicitly skipped. Local review and regression
evidence is recorded with the PR follow-up; no merge is authorized.

Remediation validation passed: affected Go packages (including full service,
server, store and operator suites), `go vet ./...`, focused race regressions,
SQLite/PostgreSQL parameter/export/grant integrations, static audit
and authorization invariants, generated Go/SQL/CRD drift, chart/admission checks,
TypeScript generation/typecheck/20 tests, web typecheck/960 tests, and docs checks.
The real history browser flow passed 14 desktop cases (one mobile-only case
skipped) and all 15 mobile cases. Compose delivered 21 values byte-exactly and
passed its refusal/doctor/sync cases. Gofmt and the new 1,292-file handwritten
import check passed. Ordinary Standards and Spec reviews found no unresolved
findings after compatibility corrections.

### Second review round

Copy and clone now inspect live source declarations and live config, matching
the data they actually copy instead of consulting the last published contract.
Pending user drafts remain outside the copy source. Template preflight opens no
secret material and refuses an undeclared destination atomically.

The version-0 template parser and its synthetic fixtures were removed: that
format existed only on this unmerged branch and never shipped. Nonempty
parameter contracts require version 1; old `{}` snapshots keep literal behavior.
The guide and grammar tests state exactly how repeated dollar signs escape.

Human export consent now reads immutable revision key metadata, without rendering
config or emitting an export event. The actual export alone validates parameters
and records the export event. The unused production `Export` convenience wrapper
was removed and its test callers use `ExportWithParameters` directly.

The regex cache is registered with its bounded key and authorization model. The
closed audit-emission lifecycle now invokes a real parameterized export, fixing
the two CI failures without exempting either invariant.

Second-round validation passed: full SQLite isolation suite (1,653 tests and
subtests; PostgreSQL and optional external fixtures skipped), service,
conformance, API, server and CLI suites, parameter/authz unit tests, focused
parameter and consent race checks, `go vet ./...`, docs verification, gofmt and
1,293-file import formatting. The live copy/clone regressions and repaired cache
and audit closure invariants passed separately on both SQLite and PostgreSQL.
The broader PostgreSQL run was interrupted when the local Docker runtime stopped
responding during a restore test; no assertion had failed. The owned disposable
database was removed. Full PostgreSQL coverage remains assigned to PR CI.
Ordinary Standards and Spec reviews found no remaining findings. Cross-provider
review remains explicitly skipped, and merge remains on hold.

CI run 34715502278 subsequently passed core tests and all three SQLite/PostgreSQL
isolation shards, closing the local runtime coverage gap. Native Kubernetes
acceptance passed too. Desktop browser group 1 failed before tests during fixture
startup; other groups started the same binary successfully. The original cause
cannot be determined because setup cleanup discarded output from a child that
remained alive but never served healthz.

Startup health failures now include child exit/signal state and the last 64 KiB
of captured stdout/stderr before cleanup. Signal exits fail immediately. A
controlled failure of the real global setup confirmed both output streams appear
at the unchanged 30-second deadline. Web typecheck and all 960 tests passed.
Timeouts and retry behavior are unchanged; the next CI run must verify startup.

Migrations 51 and 52 apply to SQLite and PostgreSQL. The generated development
compatibility manifest includes both. Generated Go, TypeScript and CRD artifacts
belong to this change; regenerate them from their source definitions.


### PR #731 review corrections

Environment grant cleanup now emits the ordinary `grant.revoked` payload for each
removed grant, including all released origin kinds and session invalidation.
Direct deletion and definitions apply share this transaction-bound implementation.
Human holders of deleted environment grants must sign in again.

Config exports no longer create per-key disclosure events. Successful exports with
public parameters bind those inputs once in `disclosure.values_exported`; secret
exports retain one `disclosure.value_revealed` per secret. Nonparameterized config
exports retain their existing unaudited behavior. Parameter management and export
are required compile-time server service methods, including transport test fakes.
