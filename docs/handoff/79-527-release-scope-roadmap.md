# #79 / #527 release scope and dependency handoff

Updated 2026-10-28. Canonical repository: `Hikyo-Org/Hikyo`.
Inspected main: `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Start with the [canonical release plan](../release/release-plan-1.0.md) and
[acceptance scope](../release/acceptance-1.0.md). This handoff is the entry point
for this bounded reconciliation, not a replacement acceptance ledger.

## Contract and changes

The owner approved the readiness recommendations and assigned separate
stabilization lanes. This lane owns only #79/#527 scope/dependency records and
satisfied #71/#8 amendment bookkeeping. Its endpoint is a signed, DCO-compliant,
reviewable PR and administrative reconciliation. Maintainer merge, final
acceptance, recovery signing, stable-policy activation, tags, Apple submissions,
publication, production changes and external notifications remain separately
authorized acts. Parent #41 is untouched.

The plan now records the approved #158 AWS / PR #969 / #1057 controller exclusion
for this 1.0, the bounded stabilization set and the order below. #805 push
notifications, #161 Cloudflare, #148 scheduled rotation and broad #1051
migration remain deferred. Generic files/sealed webhook and social registration
retain their own accepted post-1.0 rulings. No shipped-code test is removed,
provider failure erased or optional capability silently promoted.

Current source has `api.Revision = 12` and highest migration filename
`00079_remote_pairing.sql` on SQLite and PostgreSQL. These are file/source
identities, not 79 applied migrations, a deployed schema or final artifact
hashes. Do not copy historical deployed revision 9/11 observations from #1055
into the selected candidate's identity.

Only the current scope/inventory portion of the acceptance ledger changes.
Everything from `## Historical evidence rules` onward, including September
results and all original criterion rows, is preserved byte for byte. C-ACC,
HA, dynamic credentials, approvals, PKI, SSH, Transit, provider-specific
conditions, member rules, developer credentials, multi-instance, signed upgrades
and reporting remain visible. The October 29 owner decision separately approves
Vault destination membership subject to qualifying review and acceptance;
the [Vault closeout packet](./162-vault-1.0-closeout.md) tracks those gates.

## Administrative closure evidence

[PR #1014](https://github.com/Hikyo-Org/Hikyo/pull/1014) is MERGED at
2026-10-20T21:44:09Z, merge
`0de713d2df6acbb9a3e344fbfba4827b31f86501`. GitHub reports its merge signature
`verified: true`, reason `valid`. The merge is an ancestor of inspected main.

The issue timelines show prior closures and a last reopening on October 20
specifically for this amendment. The older workspace implementation is recorded
complete in [its handoff](71-multi-instance-workspace.md); it is not reopened as
new missing implementation. The latest #71/#8 reopen records say to re-close
after verified #1014 merge, and the three owning ADRs contain that exact
operative-on-merge condition.

- [#71 closeout](https://github.com/Hikyo-Org/Hikyo/issues/71#issuecomment-6465779719):
  closed as completed, for the amendment bookkeeping only.
- [#8 closeout](https://github.com/Hikyo-Org/Hikyo/issues/8#issuecomment-6465780854):
  closed as completed, for the amendment bookkeeping only.
- [Owner exception](https://github.com/Hikyo-Org/Hikyo/pull/1014#issuecomment-6300666440):
  completed GPT 6.1 Sol High Standards and Spec/security reviews are accepted
  for #1014 only. Cross-provider review stays SKIPPED, never CLEAN. This is
  neither an independent cryptographic audit nor a TLS/Transit waiver.

#79 stays open for the actual final acceptance/freeze/release outcome. #527
stays open as the execution tracker. Their current scope/dependency updates
preserve the historical checkpoints and original unchecked release gates.

## Delivered source that must not be scheduled again

Live PR records report these merges, and each SHA is an ancestor of the
inspected main. These are source delivery facts only; fresh candidate evidence
still belongs to the acceptance owners.

| PR | Merge SHA | Delivered owner |
| --- | --- | --- |
| [#938](https://github.com/Hikyo-Org/Hikyo/pull/938) | `48d7abc494a1021ae6b6421210e09c9d6d295455` | #934/#935 dynamic renewal ceilings and TTLs |
| [#942](https://github.com/Hikyo-Org/Hikyo/pull/942) | `35db63eb030da144a867bc4c7ddb5a3b9ec7a00b` | #807 developer credential administration and operative implementation condition |
| [#945](https://github.com/Hikyo-Org/Hikyo/pull/945) | `73b00f4d7eca27c36334c693d81c38300f715b52` | #589 social synthesis, preserving post-1.0 A7 |
| [#946](https://github.com/Hikyo-Org/Hikyo/pull/946) | `888d9b61fb20aea8cc8addec488adadba2bd6d7f` | #32/#612 scoped RFC8628 amendment record |
| [#952](https://github.com/Hikyo-Org/Hikyo/pull/952) | `6a3bf38a5d696e79809616192556dd6c6b63c0ab` | #930 backup retention/recovery contract |
| [#965](https://github.com/Hikyo-Org/Hikyo/pull/965) | `6da8f57cb8c8aae4cb58b7cc65ec5971c1705e0c` | #820 Transit WebUI, with separate review debt preserved |
| [#1004](https://github.com/Hikyo-Org/Hikyo/pull/1004) | `f3581e7bb6ec65cb416d5213f7e55819ef19782f` | #966 TLS preparation, not final amendment/consumer acceptance |
| [#1014](https://github.com/Hikyo-Org/Hikyo/pull/1014) | `0de713d2df6acbb9a3e344fbfba4827b31f86501` | #71/#8 pairing amendment merge condition |

## Current dependency handoff

The launch coordination index was consulted. Assignment metadata does not prove
completion. Separate TLS, governance, trust, collection, alerts and qualification
lanes were running with no linked outcome PR at this check. Consume their
reviewed outcome links when published; this PR does not duplicate their edits
or wait for them to declare their evidence passed.

| Owner / artifact | Required input and next result | Release disposition |
| --- | --- | --- |
| [#1033 / #1042](https://github.com/Hikyo-Org/Hikyo/pull/1042) | Continue existing draft; actual CLI/API target, refusal and completion proof | Required repair; precedes #1039's executable procedure. |
| [#1034](https://github.com/Hikyo-Org/Hikyo/issues/1034) | Bounded attributable authentication refusals | Required minimum; input to #1038. |
| [#1035 / #1045](https://github.com/Hikyo-Org/Hikyo/pull/1045) | Continue draft instance query/export with actual authority tests | Required minimum; input to #1037/#1038. |
| [#1036 / #1044](https://github.com/Hikyo-Org/Hikyo/pull/1044) | Continue draft completion protocol and real client checkpoint behavior | Required repair; #1037 must refuse incomplete collection. |
| [#1040](https://github.com/Hikyo-Org/Hikyo/issues/1040) | Real bearer-resolution path budgeting and anti-enumeration | Required repair; no implementation PR identified at initial inspection. |
| [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039) | #1033, then tested scoped manual containment/recovery with explicit revocation limits | Required minimum; no broad automatic quarantine inferred. |
| [#1041 / #1043](https://github.com/Hikyo-Org/Hikyo/pull/1043) | Continue draft chart contract; isolated enforcing-CNI allowed/denied probes | Required supported network boundary; #1055 deployment application is separate. |
| [Tooling alerts #104/#105](https://github.com/Hikyo-Org/Hikyo/security/dependabot) | Compatible development-tool patch or reasoned disposition | Before qualification; no production-server exploit inferred. |
| [TLS #966](https://github.com/Hikyo-Org/Hikyo/issues/966) | Owner/review disposition and signed consumer requirements despite closed implementation ticket | Governance/consumer HOLD pending the TLS-specific correction/evidence handoff; implementation is delivered. |
| [Transit #156](https://github.com/Hikyo-Org/Hikyo/issues/156), [Vault governance](../adr/vault-kv-adapter.md), [A7 ruling](../adr/mvp-boundary.md) | Recorded scoped governance disposition; preserve implemented UI and existing membership conditions | Separate lane; no #1014 waiver inheritance. |
| [#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037) | #1035/#1036, then scoped collection claim/route and complete independent receipt/checkpoint/outage evidence | Decision HOLD until owner result; full ticket acceptance preserved. |
| [#1038](https://github.com/Hikyo-Org/Hikyo/issues/1038) | #1034/#1035/#1037, then scoped alert claim and actual detector-to-operator receipt/escalation proof | Decision HOLD until owner result; full ticket acceptance preserved. |
| [#1055](https://github.com/Hikyo-Org/Hikyo/issues/1055) | Deliberate anonymous surface, deployed provenance, approved fixture/operations and #1041 runtime boundaries | Operational HOLD; no production mutation or blanket certification. |
| [Stable trust](../release/online-signing.md) | Public packet, human recovery authorization, normal trust PR/merge and authenticated recovery proof | Separate lane; any main change precedes final source selection. |
| [#651 MCP](https://github.com/Hikyo-Org/Hikyo/issues/651) | Approved exact-image/public-HTTPS fixture, named clients and real two-process PostgreSQL behavior | External HOLD until actual proof or explicit narrower claim disposition. |
| [GitHub contract and disclosure](../release/release-plan-1.0.md#3-complete-external-and-self-hoster-evidence) | Authorized isolated fixture/distinct credentials; actual provider consumption and current private-channel receipts | Separate external lanes; NOTRUN here. |
| [#79 final qualification](https://github.com/Hikyo-Org/Hikyo/issues/79) | All required source/governance/trust outcomes, then one source/tree and exact artifacts, complete current-main and specialized suites | Final evidence owner; every pending/failure/skip remains visible, then separate ceremony approval. |

#1037/#1038 launch recommendations allow a supported tested external route.
They do not alone amend the audit ADR's external-forwarding boundary, settle
custody, approve external notifications, waive ticket assertions or require a
native SIEM/hash-chain feature. Their owners must record the launch claim and
any canonical amendment. #1055 is about installations, not publication proof.
An unknown-path 404 is not an authorization test. No production exercises or
external messages were performed by this lane.

### TLS coordination update

The TLS owner rechecked main at the same source, #1004 merge `f3581e7` and
closed #966/#25. Final exact-contract approval and a #966-only waiver were not
located; that lane is preparing a concrete correction/evidence PR and scoped
approval request. Governance/consumer closure remains HOLD pending its handoff.
No general acceptance outcome is edited here.

The owner reports public nightly
[`v0.0.1-nightly.20261028.98.g24e198b3`](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261028.98.g24e198b3)
and successful [build run 40651678580](https://github.com/Hikyo-Org/Hikyo/actions/runs/40651678580)
from inspected source. Trusted artifact verification and isolated synthetic
consumer checks are being prepared by that owner. Publication does not stand
in for those pending checks, final governance or stable release acceptance.

## Recheck and validation

Run from a checkout of this PR using the pinned contributor tools. These are
read-only source/metadata checks; merge ancestry returns status 0 when present.

```sh
git fetch hikyo-upstream main
gh pr view 1014 --repo Hikyo-Org/Hikyo --json state,mergedAt,mergeCommit
git merge-base --is-ancestor 0de713d2df6acbb9a3e344fbfba4827b31f86501 hikyo-upstream/main
gh issue view 71 --repo Hikyo-Org/Hikyo --json state,closedAt
gh issue view 8 --repo Hikyo-Org/Hikyo --json state,closedAt
scripts/ci/fast-feedback.sh --plan
scripts/ci/fast-feedback.sh
agent-heavy run --label 'Hikyo scope ledger inventory' -- go test -p 2 ./api -run '^TestReleaseAcceptanceLedgerReferences$' -count=1
scripts/ci/check-commit-signatures.sh hikyo-upstream/main HEAD
```

Use `--working-tree` for feedback on local edits; the plan selects docs only.
The local feedback driver checks whitespace and read-only docs preparation.
The existing API inventory test verifies criterion closure and linked Go test
names, not behavior or release acceptance. Additional source assertions verify
unchanged historical bytes, preserved criterion IDs, all new relative links,
source revision/migration names and absence of machine paths/em dashes.

Local results: the shared feedback driver's docs-only plan and selected checks
passed using Node 26.12.1; the existing inventory test passed using Go 1.27.3
through the shared heavy-command queue. Historical-byte/inventory/link/source
and portability assertions passed. Rerun the affected checks after any edit.

Only Markdown changes. No persistence, runtime, rendered product UI, workflow
or generator source is changed. Both-engine application suites, screenshots,
full qualification, live providers, public deployment and release ceremonies
are NOTRUN by this lane and remain with their owners. Hosted PR checks and
review status must be reported against the pushed head separately. This is not
an independent review; no cross-provider review is claimed or substituted.

No runtime/storage service was created. Ignored local evidence and the complete
offline HTML companion are retained for review; no HTML/evidence is committed.
The active worktree remains for the unmerged PR. No disposable worker or server
is needed after the focused validation finishes.
