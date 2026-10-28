# #79 integrated candidate qualification preparation

## Disposition

**Preparation delivered; final integrated qualification is HOLD.** This task
adds a read-only executable requirement/source/test/workflow/artifact inventory.
It does not refresh the acceptance ledger with baseline results or authorize a
merge, stable-policy activation, freeze tag, draft registry push, publication,
Apple submission or production deployment.

Inspected source on 2026-10-28: main
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`. Live exact-SHA `ci-main.yml` lookup
returned zero runs; `v1.0.0` ref lookup returned HTTP 404. Required stabilization
PRs were still open. These are dated observations, not a selected final source.
Source API revision is 12; highest filename is `00079_remote_pairing.sql` on
SQLite and PostgreSQL. Filename, migration count and measured runtime schema
identity are different facts.

The [release plan](../release/release-plan-1.0.md), owning ADRs and accepted
scope decisions remain authoritative. Scope/roadmap [PR #1070](https://github.com/Hikyo-Org/Hikyo/pull/1070)
owns their reconciliation. This PR owns preparation and later exact-candidate
result evidence, leaving historical outcomes and canonical scope text intact.

## Generate the executable map

Run from the repository root with the Go version pinned in `go.mod`, Git, and
the shared `agent-heavy` command installed. No credentials are required.

```sh
set -eu
mkdir -p .local/reports/qualification
go run -p 2 ./scripts/ci/qualification \
  > .local/reports/qualification/preparation.json
agent-heavy run --label 'Hikyo 79 qualification preparation' -- sh -c '
  set -eu
  go run -p 2 ./scripts/ci/qualification --plans \
    > .local/reports/qualification/preparation-with-plans.json
'
```

The first form validates references and reads source. The queued form also
builds the existing shard planner in an automatically removed temporary
directory and runs its planners, without executing application tests. A failure
returns nonzero; discard partial JSON as proof and retain its error. The output
directory is ignored and must remain outside commits. No server, container,
provider fixture or persistent runtime is created.

Expected output is `phase: PREPARATION`, `release_verdict: HOLD`, and `NOTRUN`
for every criterion, amended requirement and gate. Exit zero means the
inventory was generated, never that the candidate passed. It includes:

- Source commit, committed tree, dirty flag, API revision, all migration
  filenames and SHA-256 hashes for both engines, and hashes of the mapped inputs.
  A dirty working tree is explicitly identified and cannot qualify as clean main.
- All 50 current owning ledger criteria, including C-ACC, their actual linked
  Go symbols/packages, and supplemental amendment test files. Missing criteria,
  duplicate/unowned rows, deleted symbols and missing source paths fail.
- All jobs, dependencies, runner/matrix declarations and artifact upload names
  and paths from the six relevant current workflows. Full-graph job inventory
  must match `ci-job-registry.json`. Embedded Actions expressions remain
  expressions until bound to an actual run; they are not artifact receipts.
- With `--plans`, all ordinary race/fuzz/isolation assignments from the existing
  planner, and the complete separate isolation-race assignments. Analyzer
  targets move to the reliability analyzer job. Duplicate or omitted race
  targets fail against an independent one-shard inventory. Named isolation
  criterion tests in the default build must map to both ordinary and separate
  race owners. Build-tagged Kubernetes/recovery/floor tests name their dedicated
  job and explicitly disclose exclusion from the default race planner; no race
  execution is inferred for them. Unknown build constraints fail mapping.
  Package discovery explicitly uses Linux/AMD64 with cgo enabled, matching the
  hosted planner jobs rather than inheriting a macOS workstation's build-file
  selection. Native ARM64 and cross-platform jobs retain their separate owners.
  Compare these preparation assignments with the actual hosted planner output.
- Explicit dependency holds, approved exclusions, executable dispatch/readback
  instructions where applicable and the actual evidence each gate must retain.
  Commands in the JSON are instructions only; the generator never runs them.

Before using a generated map after integration, rerun it. Source/tree equality
does not imply binary-byte equality: embedded commit/version metadata, archive
creation, signing and packaging can change bytes. Measure each binary/archive
hash and size, image RepoDigest and chart digest independently. Runtime API/meta
and migrated SQLite/PostgreSQL identities require actual runtime evidence.

## Gate-to-evidence map

The executable details live in
[`requirements.json`](../../scripts/ci/qualification/requirements.json).
The generator reads the owning ledger and workflows rather than copying
historical PASS statuses into a new result table.

| Gate | Current execution owner | Evidence and boundary |
| --- | --- | --- |
| Full main | `ci-main.yml` calling complete `ci.yml` | Latest qualifying exact-SHA main run, event/path/run/attempt, full changes plan, every registered domain/supporting job, 12 ordinary race shards, three isolation shards, three fuzz shards, platform legs and actual both-engine results. Inspect all failed jobs even when the workflow is cancelled. Queue admission and merge-group proof remain separate. |
| Full isolation race | `reliability.yml`, `suite=race` | Eight nonempty shards plus analyzer job, independent complete inventory, SQLite/PostgreSQL, timings/logs/failures/skips. Ordinary race excludes isolation. |
| Recovery and operator | `release-acceptance.yml`, `suite=all` | Native ARM64 recovery from two distinct dedicated test custody sources, backup/destroy/restore/reconcile/readability/credential/RTO assertions, and actual shipped operator process/RSS/resource phases. Keep provenance and exact executable hashes. No production-custody substitution. |
| Operations floor | `ops-floor.yml` | Native AMD64 and ARM64 results, eight doctor states and bound checks, cgroup limits, binary/test hashes and provenance. No emulated or physical-Pi claim. |
| Required benchmark | `floor-bench / measure` in full main | Required native ARM64 workload/readback, unchanged thresholds, CPU factor 4.0 and memory factor 1.0 unless separately calibrated, raw and estimated values, exact executables. This workflow has no manual dispatch. |
| Deployment doctors | `compose-demo`, `k8s-e2e` | Actual authenticated Compose SQLite and installed chart PostgreSQL findings for all 12 families through `assert-doctor-findings.jq`. Preserve raw redacted JSON when available; disclose if only exact assertion/log evidence is retained. Native doctor checks do not substitute. |
| Browser/client/API | `web`, `web-closure`, `client`, `preflight`, `web-go`, `storybook`, `scan-xplat` | All eight desktop/mobile flow legs, registry closure and legitimate viewport skips; both-engine SDK/CLI skew and minimum-revision refusal; API diff/negative fixtures, parity, goldens, open/closed enums and operation minima. |
| Packaged self-hoster | Self-hoster checklist against selected packages/image/chart | Actual install/UI/version/persistence, setup/admin/recovery, import/identity/grant/workload delivery, authenticated upgrade/backup/drain/migrate/health/restart/restore/readable values and credential reconciliation. Source tests are insufficient for selected-package acceptance. |
| Providers, MCP and docs | Separate owning lanes | Required real GitHub API/workflow consumption, refreshed applicable provider bindings, #651 exact-image public HTTPS/two-process PostgreSQL/named clients, live O4-O6 and actual disclosure/fallback receipts. Controlled fixtures and GET checks do not replace external proof. |
| Trust/freeze/distribution | Stable trust owner, later human authorization | Source-changing trust setup precedes final selection. Later signed immutable tag, complete signed draft, reviewed manifest/SBOM/provenance and public-download/OCI/Apple consumer proof bind retained bytes. This PR authorizes none of those writes. |

The amended requirements include change approvals and distinct temporary
access C-ACC; PostgreSQL-only HA and dynamic credentials; private PKI, SSH user
certificates and Transit; multi-target/GitLab and conditional Vault governance;
developer credentials; signed upgrades/reporting; and the stabilization seams.
Keep the owning normative assertions, governance conditions, all three check
classes and both-engine obligations. Do not turn source presence into approval.

## Exact dependency holds

At the inspected main, these are not integrated final-candidate evidence:

| Owner | Hold before final qualification |
| --- | --- |
| #1033 / [#1042](https://github.com/Hikyo-Org/Hikyo/pull/1042) | CLI target/scope/completion repair, reviewed and human-merged. |
| #1036 / [#1044](https://github.com/Hikyo-Org/Hikyo/pull/1044) | Negotiated audit completion and supported consumer behavior, reviewed and human-merged. |
| #1035 / [#1045](https://github.com/Hikyo-Org/Hikyo/pull/1045) | Instance query/export interfaces and both-engine/consumer validation. |
| #1041 / [#1043](https://github.com/Hikyo-Org/Hikyo/pull/1043) | Restrictive chart configuration and actual enforcing-network evidence where claimed. |
| #1040, #1034, #1039 | Real-path admission budgets, attributable refusals and scoped containment/recovery, including dependencies on the CLI and audit repairs. |
| Development alerts #104/#105 | Compatible patched Vitest tooling or recorded exposure disposition, then integration. |
| #79/#527 / [#1070](https://github.com/Hikyo-Org/Hikyo/pull/1070), #966, #156 and conditional governance | Current release membership, exact TLS/Transit obligations and issue-specific review routes. #1014's exception does not waive another ADR gate. |
| #1037, #1038, #1055 | Concrete independent-evidence, alerting and deployed authentication/containment launch dispositions. No implicit native SIEM or production change. |
| #651, GitHub, O4-O6 | Authorized isolated fixtures and protected credentials, exact final candidate external proof and actual current receipts. Consume their scoped packets. |
| Stable trust and final integration | Any required source-changing trust PR and this preparation PR need review/human merge. Final clean main must follow every required source/governance/trust merge. |

Refresh the parent's execution index when available and query live PR heads and
merge SHAs. It is local coordination context, never a committed path or release
authority. Stable-policy activation, credential/fixture/provider permission,
production mutation, Apple submissions and publication remain separate holds.
Cross-provider review is SKIPPED when unavailable, never CLEAN; no paid fallback
or general amendment waiver is inferred.

AWS #158 acceptance, PR #969 and extracted controller #1057 are excluded.
Accepted deferrals #805/#161/#148 and broad #1051 remain out. Preserve all
already-shipped tests, including excluded-provider controlled tests, and their
failure witnesses. A feature's testing does not promote its release claim.

## Integration and eventual result-ledger procedure

1. Confirm every dependency's disposition, merge ancestry, accepted scope and
   required review. Fetch current main and tags; verify live refs independently.
   Select one clean main only after required source-changing work is merged.
2. Regenerate this map with plans, recording commit/tree/input hashes. Capture
   GitHub run/attempt/workflow/event/head/base identities at dispatch and
   completion. A moving main needs fresh candidate proof; old proof stays
   attached to its original identity.
3. Execute authorized checks through the shared heavy queue locally, and the
   mapped hosted workflows when their fixture/secret permissions are satisfied.
   Before resource creation, record exact ownership and teardown identifiers.
   Keep the full workload, worker limits, thresholds and custody assertions.
4. Preserve raw failures, partial results, skips and missing artifacts. Record
   named test/engine/viewport outcomes only when actual output supports them;
   nonverbose package success does not establish zero skips or live providers.
   Retain artifact bytes before workflow retention expires. A new run, attempt,
   build, signature or package requires explicit identity reconciliation.
5. Add exact-candidate results to the acceptance ledger only after integration.
   Record `PASS`, `FAIL`, `NOTRUN`, `SKIPPED` or `HOLD` per gate with the exact
   evidence and unresolved next requirement. Preserve historical rows. Source,
   local, hosted, live-provider, draft, public and deployed proof stay distinct.
6. Only when the necessary gates justify it, prepare a concrete review packet
   identifying proposed tag/source/tree, retained candidate bytes and digests,
   trust state, manifest subjects, complete check/receipt inventory, remaining
   human actions and failure recovery. Do not cut the tag, activate policy,
   push draft subjects or publish under this assignment. Before the first tag,
   freeze is dormant and client skew is a rehearsal. Their green process exit
   cannot establish a frozen-tag PASS.

## Validation and cleanup

Focused Go regressions cover the real repository map, missing C-ACC, duplicate
criteria, deleted test references, workflow/registry drift, and the required
separate isolation-race binding, split-package filters and tagged operator
ownership. Initial queued planner job 2576 exposed a mapping error: the tagged
`TestK8sOperator` was incorrectly required in the default isolation plan. Its
failure remains preserved; the mapper now records the actual `k8se2e` owner
and explicitly avoids a default-race coverage claim. The complete-diff
fast-feedback plan and run,
queued shard-planner generation and targeted owning inventory tests establish
preparation quality only. Hosted checks must be reported separately at the
pushed head. No baseline final acceptance is run by this task.

Retain ignored JSON/log/report deliverables for review. The planner removes its
owned temporary executable directory on success or failure; it creates no
container/service/database. Before handoff, verify no owned disposable process,
temporary build directory or runtime remains. Do not prune shared caches or
another lane's fixtures.
