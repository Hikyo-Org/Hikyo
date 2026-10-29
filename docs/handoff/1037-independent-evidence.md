# #1037: external independent-evidence route

Status: **PREPARATION with local product/Unix proof; operational acceptance OPEN/HOLD**.
The executable operator entry point is the
[collection runbook](../operations/audit-collector.md), backed by
[receiver.py](../../scripts/ops/audit-collector/receiver.py) and its
[synthetic rehearsal](../../scripts/ops/audit-collector/test_receiver.py).

## Current preparation, 2026-10-29

Inspected base/live main: `ce5e85835383b6428676ff595e2a0c1a72f6a088`.
#1035 and #1036 are merged and closed. Collector PR #1079 is integrated through
#1087. Their historical pending statements below describe the original delivery,
not today's dependency state. TLS #1090 is present; Vault #1091 and stable signing
are outside this preparation. This base is not a selected final release candidate.

The owner authorized preparation, read-only infrastructure discovery, disposable
local rehearsals and a concrete resource-specific deployment packet. No remote
installation/account/ACL/DNS/firewall changes, production collection, existing
installation destruction, paid resources, notifications, merge or release actions
are authorized. #1038 real delivery remains HOLD. Accepted #158/#969/#1057 and
other 1.0 deferrals remain excluded.

The executable [deployment preparation](../operations/audit-collector-deployment.md)
and `scripts/ops/audit-collector/rehearse-local.py` are the new entry points.
The private approval packet names the actual proposed source/destination,
administrators, data exposure, lease, retention and exact permitted operations.
Native fixture IDs, selected artifact and protected-host trust are bound only
after their independently approved creation; unavailable bindings remain HOLD.

Current local evidence, distinct from historical runs:

| Evidence class | Result and limits |
| --- | --- |
| Real product HTTP exports | PASS on SQLite and PostgreSQL 18.6: supported project rename and failed login, existing WebAuthn test authenticator, completed tenant/instance packets, exact acknowledgment replay, partial refusal and separate-process retained reads after exclusively synthetic producer rewrite/removal. No live human/MFA ceremony or release-artifact proof. |
| Separate Unix identities and transport | PASS in a disposable Debian container: real loopback OpenSSH fixed command, separate submit/poll/evidence/read/monitor UIDs, submitter/poller shell/read/write/delete/ACL/code/monitor denial, reader/monitor write denial and exact original packet read. Root deletion tested only on a disposable synthetic probe. Actual SFTP subsystem, PTY-only and remote-forward requests explicitly refused. Protected guest and independent personnel NOTRUN. |
| Source contracts | PASS: both-engine receipt, session revocation, instance route/CLI, retention boundaries/rollback/concurrent export, pagination, PostgreSQL commit-order/cutoff and instance commit-order owner tests. No pruning acknowledgment dependency added. |
| Receiver/monitor controls | Local synthetic suite covers never-received, stale/future receipts, stopped poller/detector, durable failed/unknown source-attempt history, missed detector intervals, capacity threshold, sticky cumulative receiver refusals even between checks, and removed expected streams. Later success cannot resolve historical uncertainty. |
| Consumer | #1038's opt-in receiver compatibility suite passes against current receiver bytes. External notification, acknowledgment and escalation NOTRUN/HOLD. |
| Host preparation | Fixed installer and systemd unit verification are local evidence. Local TLS trust/stale/future/stopped-health and actual task-owned tmpfs ENOSPC/preserved-copy/retry controls PASS. Actual encrypted disk, protected ACL/account/trust, power-loss durability and independent watchdog acceptance remain NOTRUN. |
| Review/CI | Record exact final source head, local feedback and independent Standards/Spec results in the PR. Required maintainer security review and hosted CI are separate from local results. No merge is authorized. |

Reproduce the full private local product/Unix exercise with the one command in
the deployment preparation. Retain its exact source hashes, receipts and ownership
inventory outside Git. No Python linter/typechecker is configured in this scope;
the unittest suite, Python compilation, Go owner checks and shared feedback apply.
The final expanded local runner passed and verified removal of its task
containers, exact anonymous volumes and image. Its failures remain separately
recorded rather than relabeled PASS. All owned disposable resources must be removed and
verified; private evidence and offline HTML may remain for the review window.

## Historical source-delivery record

The remaining dated evidence records the original implementation and PR repair.
Current operational gates above supersede its old pending integration statements.

## Contract and approved boundary

This slice prepares the approved minimal external-collector route for
[#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037). It changes no Hikyo
authentication, source datastore, audit retention, event registry, API or native
forwarding behavior. It creates no unattended org/instance authority. It does
not own #1038 alert delivery, release records, roadmap or acceptance ledgers.
No production source/destination, external notification, cloud resource, paid
provider, stable signing, release or deployment was used or activated.

Source inspection baseline: main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`,
rechecked on 2026-10-28. #1037 has no pre-existing implementation PR. Dependencies
[#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035) /
[PR #1045](https://github.com/Hikyo-Org/Hikyo/pull/1045) and
[#1036](https://github.com/Hikyo-Org/Hikyo/issues/1036) /
[PR #1044](https://github.com/Hikyo-Org/Hikyo/pull/1044) were open at inspection.
The example deliberately refuses baseline main's unreceipted HTTP 200.
The receipt contract was cross-checked against #1036's signed local integration
`1a19f61065bd5d4274a6a84fa7cd146b32237b9a` and
`docs/handoff/1036-audit-export-completion.md` in that integration. Its broader
validation was pending at coordination; this example does not claim it is merged.

The [audit ADR](../adr/audit-model.md) already permits per-project automation
exports and independently retained copies. Its native external-sink/hash-chain
exclusions and org/instance automation limits remain. The
[threat model](../adr/threat-model.md) treats a compromised operator/datastore as
able to rewrite producer history. No general ADR review waiver was inferred.
Stronger source completeness or new instance automation must use the ordinary
[amendment process](../adr/oss-mechanics.md).

## What the reference does

The finite poller uses installed curl and OpenSSH, with HTTPS certificate
validation, pinned SSH host keys and credentials held in private operator files.
It only accepts the exact configured scope's full export route, `receipt=v1`,
HTTP 200, the negotiated content type and response-bound export ID/snapshot.
It rejects a missing/failing/uncertain terminal receipt, mismatched extent/count,
trailing content, invalid UTF-8, duplicate JSON keys, foreign scope and bounded
byte/row/line violations. It never publishes a partial export or advances on 200.

Pending bytes are fsynced and retained across retries. An independently
administered receiver commits original packet, digest, deduplicated event
content and complete batch identity/time in one durable SQLite transaction
before acknowledgment. Same-ID/different-content is a refusal, not replacement.
Only a matching destination acknowledgment advances the poller checkpoint.
The destination's independent `status` check observes complete-receipt age and
receiver refusals. Old retries do not refresh it. Delivery of that signal belongs
to #1038, including a dead-man check on the monitoring process itself.

Every new poll replays the full retained authorized scope. It never resumes from
`seq` or a wall-clock lower bound. Source SQLite's single writer and PostgreSQL's
database-owned commit ordering, cutoff registration and writer barrier remain
owned by `internal/service/audit.go`, the store, and their existing tests. A
collector does not export the internal commit cursor as a new contract.

## Evidence and remaining acceptance

| Requirement | Evidence/disposition |
| --- | --- |
| Credential/read/destination deletion separation | Concrete identity matrix and fixed-command installation packet in the runbook. Actual independent ACL/account proof **NOTRUN**. |
| Real tenant event and instance authentication event | Synthetic tenant and instance envelopes accepted, source/trail/scope/digest bound. Real Hikyo receipt-to-destination exercise **HOLD** on #1035/#1036 and resource-specific fixture authority. |
| Complete acknowledged checkpoints, retry/dedup/crash recovery | **PASS locally:** receiver subprocess exits before commit and after commit/before ack; restart, SQL destination failure, exact retry, conflict refusal, mismatched ack and durable pending recovery. |
| Both source engines / concurrent commit coverage | **PASS locally:** existing audit core, PostgreSQL commit-order/cutoff, page-cap and retention cases on SQLite and PostgreSQL 18. Synthetic late-lower-seq replay alone is **not** PostgreSQL proof. No source cursor/retention implementation changed. |
| Independent lag/heartbeat and incomplete collection | **PASS locally:** never-received, stale/future clock, sticky refusal, authority loss, partial 200 and old replay controls. `receipt_age_seconds` measures receipt freshness, not producer-generation lag. Protected monitor/dead-man and #1038 delivery **NOTRUN**. |
| Sensitive content, access, reconciliation and deletion | Private configuration/spool, bounded parsing, fixed diagnostic and scoped reads. Destination receives authorized audit event content, including regulated metadata, so independent access/encryption/retention review is **HOLD**. |
| Producer rewrite/removal after receipt | **PASS synthetic:** delete/rewrite producer fixture, read original packet from destination in a separate connection. Real protected-host exercise and submitter delete/shell refusal **NOTRUN**. |
| Retention boundary and outage | Synthetic producer pruning does not delete received evidence. Actual source retention/export failure remains subject to existing owner tests. No export acknowledgment gates source pruning. Unknown intervals stay **HOLD**, even after a fresh successful export. |

Local commands:

```sh
python3 -I -B -m unittest discover -s scripts/ops/audit-collector -p 'test_*.py' -v
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
bash scripts/ci/fast-feedback_test.sh
```

Focused local rehearsal: **17 tests PASS** on Python 3.14.4. The poller transport
controls stub curl/SSH; the destination and crash/restart paths use real SQLite
and receiver subprocesses. No Python linter/typechecker is configured for this
scope. The rehearsal is selected by shared fast-feedback for the full PR diff
and is mandatory in the full CI `test` job. Workflow/shell changes retain their
existing Actionlint/ShellCheck and feedback-driver regression checks.

Validation batch: agent-heavy job **2580**, including feedback-driver owner
regressions, full-diff selected feedback and the following existing source tests
against SQLite and an exclusively owned disposable PostgreSQL 18 fixture:

```sh
go test -p 2 -parallel 2 ./internal/isolation \
  -run '^(TestAuditCore|TestPostgresAuditExportCommitOrder|TestPostgresAuditExportCutoffRegistration|TestAuditExportDoesNotTruncateAboveThePageCap|TestAuditRetention.*)$' \
  -count=1 -v
```

Batch status: **PASS**, exit 0, without increasing limits or bypassing resource
admission. Full-diff selected feedback passed, as did its refusal/lane/deadline/
dependency fixtures. The existing source suite passed in 107.686 seconds on
both engines, including retention rollback/concurrent-export cases. The PR records
the committed revision and hosted check state separately. Hosted full-candidate proof,
maintainer security review and merge are not established by local results.
Cross-provider review is **SKIPPED**, the configured Anthropic subscription is
unavailable and no paid/provider fallback is authorized. This is never CLEAN.

### PR monitoring follow-up

The owner authorized monitoring, review repairs and merge when green, without
changing the operational acceptance gates below. At head
`e78124983da05855e523d8e1e4411d4ebc94b542`, hosted
[invariant run 40670430654, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/40670430654)
failed in `Repository invariant tests`: the new supply-chain rehearsal step was
not represented in the frozen coverage-conservation contract. This was a real
integration failure, not a waived advisory finding.

The repair preserves `scripts/ci/testdata/full-suite-baseline.json` unchanged.
It requires exactly the unconditional, fail-closed receiver rehearsal before
normalizing that exact additive step for comparison with the historical inventory.
Mutation coverage rejects removal, duplication, renaming, skipping, conditions,
fail-open changes, narrowed/wrong suites, ambient imports, bytecode writes,
environment/directory changes and removal of an existing baseline check.
Focused coverage owner tests pass. Full invariant and complete-diff feedback
validation were submitted as agent-heavy job **2604** and remained queued when
the focused repair was pushed. Their pending status is not a pass; current-head
hosted invariants and protected merge-group proof are still required. The PR
records subsequent validation results against the exact repair head.

CodeRabbit reported its free-review quota exhausted, with no substantive review;
that is **SKIPPED**, not CLEAN. Independent human security review remains required
by `CONTRIBUTING.md`, including for maintainer-authored delivery-path changes.
Protected queue admission and full current-base group proof remain separate
from both local tests and the owner's merge authorization.

## Claim proposal, not an acceptance waiver

Recommended wording after protected-destination and prerequisite acceptance:

> Operators can retain completed, acknowledged audit exports with independent
> destination administration and monitor receiver freshness through the external
> reference recipe. Collection is automated per project and supervised for org
> or instance scope. Hikyo does not claim an independently complete producer
> history, source tamper proofing, or a built-in external SIEM feed.

Until those gates pass, the claim is only **tested local reference, operational
acceptance pending**. Keep #1037 open, including both-engine live receipts,
independent destination custody, retention/outage reconciliation and #1038
integration. Source rows can be committed and pruned/deleted between polls.
A malicious producer can omit, rewrite or fabricate future exports. A successful
receipt proves what that producer claimed to export, not that it emitted all
events. No sequence-gap or source-timestamp heuristic can close that boundary.

## Historical human-only request (superseded by the concrete preparation packet)

Choose **A (recommended)**: nominate and authorize an isolated Hikyo fixture and
a separately administered disposable destination, naming the source scopes,
independent evidence administrator/deletion owner, polling identity and trusted
TLS/SSH endpoints. Authorize only synthetic tenant activity, a supervised failed
instance login, the fixed receiver install/ACL tests, bounded outage/crash tests
and post-receipt deletion of the synthetic producer copies. Retain the resulting
private receipt packet until review, then let the independent owner approve
fixture deletion. Supply any needed credentials through the protected secret
tool only after the named resources and authority are agreed. No production
destination writes or messages are included.

Alternatively **B**: leave external operational acceptance on HOLD and describe
the launch deliverable solely as this local reference. This makes no new
independent-retention operational claim and does not close or waive #1037.
The broader narrowed release claim remains a maintainer decision.

After A and the final merged #1035/#1036 contracts are available, execute on both
source engines:

1. Bind exact server build, source installation, scope, destination account,
   receiver revision, TLS/SSH trust and independent monitoring inventory.
2. Generate one real synthetic tenant event and failed instance login through
   supported interfaces, collect both complete streams, retain actual receiver
   acknowledgments and independently read the event IDs and packets.
3. Inject partial 200, revocation/expiry, destination outage and lost ack. Check
   unchanged complete checkpoint, durable pending retry, dedup and independent
   monitor failure. Exercise concurrent commit/cutoff and retention boundaries.
4. Rewrite/remove only those synthetic producer copies. Demonstrate independent
   retained read access and refusal of submitter shell/update/delete access.
   Have #1038's owner attach its authorized delivery/dead-man receipts separately.
5. Record actual results, unresolved intervals, capacity and retention agreement.
   Keep failed/partial proof HOLD. Obtain maintainer security review before merge
   and separate operational approval before any real deployment.

Cleanup: unit tests own and remove their temporary files and subprocesses. The
source batch owned only its labeled PostgreSQL fixture and removed it on
exit, verified absent afterward; no shared database/container was used. Local ignored logs and the full
offline HTML companion are retained for review, outside the commit. No UI is
rendered by this PR, so screenshots are not applicable.
