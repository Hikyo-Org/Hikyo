# Parked #158 handoff: 1. Start here: owner stopped execution, issue remains open


The owner has explicitly stopped work for now after more than a week and requested this complete ticket handoff, separate issue comments, preservation of uncommitted source, and a later fresh agent. This supersedes the overnight instruction to try unattended completion. Do not resume AWS, bind a clock, create a key, dispatch, approve a deployment, merge, or restart reminders merely because this handoff or an old approval exists.

The accepted scope is still: finish the AWS Secrets Manager synchronization adapter through the smaller, reviewed, owner-assisted acceptance route. Reusable release-controller integration, performance, complete lifecycle and same-controller cleanup qualification belong to issue #1057. Do not make #1057 a prerequisite again. The adapter is substantially implemented. It must not be rebuilt from scratch.

**Delivery is not complete.** PR #969 is open, not draft, not merged. Its exact application head is `c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a`, remote branch `dunky13/finish-issue-158`. The working branch is `fix/issue-158-provider-completion`. Trusted published runner/workflow main is `24e198b32a3ffd8d4013207b2f974d76ec49be6f`. These are distinct identities. The last actual provider run tested the predecessor application `361e02e3915eef840429d88589c39689f4a58dfb`, not c253ff9.

Current-head feedback, repository invariants, ops-floor, CodeQL and `ci-queue-required` passed at the parked application head. `ci-required-next` is queued. Full mandatory CI for the final current-base merge-group pair is **unestablished**. A PR API base field previously showed `14678ba6...` while live main was the trusted 24e198 revision. Query the branch and actual tested pair; do not infer current-main proof from that PR field. No successful complete real-AWS acceptance exists for c253ff9.

The latest actual attempt, c11 run `40600194878`, attempt 1, has all 46 records terminal: **43 pass, 3 fail**, one PostgreSQL lifecycle conflict leaf and two failed parents. Cleanup was independently verified. Local repairs now pass the affected fixture/adapter selections, but those are not provider proof.

C12 is approved historically but **parked, unbound and unused**. No c12 key, alias, IAM write, fixture, clock or hosted dispatch was created. Its 32 charged slots remain unused, at 336 lifetime charged reservations. Its sole hosted attempt and eighth-key allowance remain unused. Parking does not refund slots, reassign old closed pools, close the issue, or authorize a successor.

The last live blocker was root-owner console authentication. A cached regional account header was not proof of current access: the global IAM identity page showed Sign In and fresh native IAM/KMS reads failed. The passive check at `2026-10-28T07:13:15.977Z` still showed Sign In. Private password/MFA cannot be supplied or bypassed by an agent. Root and setup credentials must remain separate.

The 15-minute drift reminder is paused, with no next run. PR watching for this parked task is stopped. No new cloud operation or acceptance test was run to produce this handoff.

### Six acceptance criteria: evidence and missing closure

| Issue criterion | Existing evidence | Still required |
| --- | --- | --- |
| Browser, CLI and API region/auth/naming/KMS/source configuration | Implemented surfaces, protected static intake, controlled authenticated API/CLI/browser and collision tests | Final source correspondence and required current-base CI; preserve all protected-input behavior |
| Workload, AssumeRole and WebIdentity; bounded sessions and private static credentials | Closed credential descriptors, default-deny node policy, real identity/expiry preflights and c11 auth leaves passed | Complete successful current-candidate AWS run; do not treat isolated historical leaves as the full gate |
| Create/update/staging/adopt/retain/prune/pause/retry/teardown without value reads | Adapter, adoption witness/recovery migrations, unit and both-engine integration tests | Corrected PostgreSQL lifecycle must pass on AWS; last actual run failed this leaf |
| Deterministic JSON, collision refusal, per-key atomicity/partial failure | Controlled module, API/CLI and both-engine tests | Final accepted candidate and CI evidence, whole genuine gate |
| Concurrent external update, KMS/IAM denial, expiry, movement, ambiguity and retry bounds | Controlled failure tests; real c11 expiry/KMS/IAM and adoption/replay leaves passed | All genuine current-candidate records together; no weakening of concurrency, expiry or refusal predicates |
| Harness plus real delivery/no-read/replay/no duplicates/cleanup | Harness exists; local repaired selections pass; independent c11 cleanup passed | All 46 genuine terminal records pass for both engines, independent final cleanup, normal merge and actual issue closure |

The resumption endpoint is successful real provider acceptance, independently verified cleanup, final source/artifact and current-base CI correspondence, normal protected merge, and confirmed issue closure. No skipped review is CLEAN. No scheduled deletion is physical deletion.
