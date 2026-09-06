# PR #686: Hikyo self-configuration handoff

Current state, 2026-09-06: **uncommitted expanded implementation; acceptance and exact-head CI still open.** Historical nine-key CI passed at `152373212c6d78a3bcae91e3300097ff0d893acf`. Do not use that result as evidence for the expansion. No merge or production deployment has occurred.

## Current authority and scope

The user requires every supported server variable, remote Apply only by the target instance admin with fresh passkey/TOTP, separate projects for independent remotes and shared owner settings only within HA. They delegated recommendations and explicitly approved controlled bootstrap pod rollouts. Continue work without reopening those decisions.

The current runtime catalogue has **27 top-level keys**: nine original, 16 added owner settings, secret `HIKYO_NODE_OVERRIDES` and alias-only `HIKYO_BOOTSTRAP_SOURCES`. The inventory classifies 65 recognized inputs; classification is not an activation implementation. Trusted proxy policy is exclusively node-local. See the [full scope and acceptance specification](../spec/self-configuration-full-catalogue.md).

Ordinary owner/node changes now prepare and replace the app graph, listeners, memory TLS, PostgreSQL pool, admission capacity and outbound/backup/auth captures without container restart. Stable DB/keyring/coordination and the coordinator remain supervisor-owned. Actual installation precedes acknowledgement. Stale REST/SCIM/MCP and worker admission are fenced; narrow admin repair remains available. Node seed files import once. An unknown HA node starts only fenced repair, without business acknowledgement.

The concrete controlled bootstrap provider is **explicitly enrolled, singleton non-HA Recreate Kubernetes 1.36 only**. Host, GitOps and HA bootstrap providers remain unimplemented. Database source aliases reconnect to the same verified datastore; true data migration is not implemented. Root custody remains external and there is no automatic root finalization. Do not describe controller fakes or chart rendering as a deployed controller.

## Exact workflow and repair

The UI prepares the exact selected revision first, waits for current participant readiness, displays Reload live or Controlled rollout, and binds the plan digest into fresh final MFA. The preparation lease is five minutes; a synchronous request waits at most 30 seconds and worker evidence must be no older than 30 seconds. A changed revision, owner, generation, incarnation or digest invalidates the old decision.

A partial controlled rollout exposes **Restore deployment inputs** with distinct exact `rollout-restore` MFA. It restores only deployment inputs after verified controller acknowledgement. Desired revision stays unchanged and business stays fenced. A fresh published repair with separate MFA is required to resume. Unresolved external handoffs cannot be silently superseded. The old root wrapper intentionally remains available for this recovery path.

RP hostname changes require exact TOTP plus the initiating admin's current password and confirmed TOTP credentials. Same-host port changes permit a passkey. This is recovery viability, not proof of target DNS, TLS or a successful new-origin login.

## Evidence and next work

[Validation](../reports/self-configuration/validation.md) separates local expansion checks from historical nine-key evidence. Local evidence includes actual HTTP/MCP/listener/TLS/capacity/backup/pool consumers, both-engine origin and deployment restore tests, 934 web tests, typecheck/build and the full app suite after upgrade projection fixes. Expanded desktop and mobile passkey/TOTP channel-Apply journeys now pass against freshly compiled independent instances; actual controlled-rollout acceptance remains incomplete.

Remaining acceptance: actual Kubernetes enrollment/admission/replacement/restore proof; unsupported deployment and startup families; target-origin access; frozen complete checks, independent review and exact-head CI. Parent owns delivery and integration. Report changes must be rebuilt with `node docs/reports/self-configuration/build.mjs` and served at <http://192.168.0.30:8769/>.

The report contains D01 through D30 with alternatives, recommendations, decisions and evidence. Five original decisions have explicit user approval; later recommendations are delegated. The separate bootstrap-rollout approval is explicit. No further design interview is required.

## Historical nine-key integration evidence

The following evidence predates the expansion and is preserved for traceability.

## Integration evidence

Merged current main `72f5a71f` while preserving its account-profile and adapter
findings work. The unreleased managed-configuration migration is now 50; the
historical recovery cutoff and legacy archive fixture match. Full core checks,
focused both-engine authorization/audit/formula checks, Go vet, all 919 web tests,
SDK checks and fresh desktop local/independent-owner browser journeys pass.
The pinned Go formatter fixes the initial CI format failure. Exact-head CI is
recorded on PR #686.

## CI follow-up corrections

Browser closure now executes all four required configuration assertion runs.
Approval scheduler claims retain a provisional fence on commit error and require
exact renewal before any job can run; deterministic both-engine and race tests
cover lost responses, rollback, successors and shutdown. Schema fan-out avoids
repeated project-storage sums using attempt-local exact accounting; every
environment retains the existing high-water check. Three alternating local
100,000-cell benchmarks improved median time by 15.21%. CI floor limits and
lease/takeover timings remain unchanged. See the report validation record for
measurements and exact failure history.


## Pending integration corrections

Host administrator creation must consume the running server’s fresh sealed owner/node seed, rather than evaluating the CLI process environment or changing a live listener to its default. Missing or stale evidence must refuse before principal creation and explain that the server must be started before retrying. Local regression proof now passes: service race 35.027 s, app 16.236 s, including both-engine maximum-size/MFA authority checks. Both R1 corrections now preserve discovery mode and read a fresh clock after the membership lock; both-engine regressions passed in 5.308 s and R2 review is clean for those fixes. Durable exact-authorized Submit/Restore/Observe renewal and no-effect restoration passed R2 review, race checks (module 4.877 s, service 111.817 s, app 17.108 s) and full lint (78.122 s). Renewal changes transport sequence/timestamps only and cannot grant an uncommitted preparation authority. Real Kubernetes outage/recovery proof remains open.

Root-finalization guard is implemented and R3 review is CLEAN. Binding-before-hierarchy serialization preserves both wrappers while the current generation/incarnation has an unresolved rollout, including superseded repair states. Both-engine regressions passed in 15.885 s and existing rotation invariants in 2.166 s. The actual Kubernetes fixture also refused finalization with conflict while the replacement awaited executor acknowledgement and retained epochs 1 and 2. The full fixture recovery chain remains pending.

## Current integration and remaining work

Development controls and next-root selection are integrated atop signed baseline `2a070efb`. Full affected package checks, lint, vet, docs and combined R1 review pass; see validation.md. Original patches remain in `/tmp/hikyo-dev-controls-slice.patch` and `/tmp/hikyo-next-root-slice.patch` as archived slice inputs; do not apply them again.

D33 HA/node-identity implementation runs in `/tmp/hikyo-topology-818e`; D34 enrolled upgrade-custody implementation runs in `/tmp/hikyo-upgrade-custody-818e`. Each new worktree has the copied integrated baseline staged without a commit. Agents must not stage or commit. Only each unstaged delta plus new files is its feature slice. Parent owns integration, review, generated metadata and exact-head delivery. The topology agent owns any migration/SQLC change; development manifest 137 is reserved only if schema changes prove necessary. The upgrade agent and chart agent share the upgrade worktree with separate file ownership.

D33 uses exact singleton topology intent, old/new membership correspondence and stale-process fencing while retaining Recreate one-replica limits. D34 resolves a coherent seven-value enrolled profile before startup admission and performs read-only preparation without minting upgrade authority. Existing gate, operator/custody proof and signed evidence remain authoritative. Both implementations remain pending and must not be described as delivered.

Latest complete isolation coverage: all 369 planned cases pass with SQLite/PostgreSQL configured. Shards 0 and 1 passed in 321.622 s and 457.832 s. Shard 2 reached the unchanged ten-minute process limit without an individual assertion failure; its exact 134-case set passed in three local groups (229.973 s, 123.820 s, 176.508 s). Logs: `/tmp/hikyo-selfconfig-isolation-shard-{0,1,2}.log` and `/tmp/hikyo-selfconfig-isolation-shard-2-part-{0,1,2}.jsonl`. No timeout or test coverage changed. These final results supersede the earlier pending isolation status above.
