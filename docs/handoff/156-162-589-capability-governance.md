# #156 / #162 / #589: pre-freeze capability governance reconciliation

Assessed canonical `Hikyo-Org/Hikyo` main on 2026-10-28:
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`.

This is the entry point for the Transit, Vault KV and social governance lane.
It records existing decisions, corrects stale delivery text and prepares the
remaining owner decisions. It does not approve an amendment, select a release
candidate, change release membership, reopen delivered implementation, or
authorize removing shipped code or tests. The release-scope coordinator owns
`docs/release/release-plan-1.0.md`, `docs/release/acceptance-1.0.md`, the roadmap
and canonical status ledger; those files are unchanged by this reconciliation.

## Operative scope for the coordinator

| Capability | Delivered source | Governance and membership | Pre-freeze disposition |
| --- | --- | --- | --- |
| Transit #156, including #820 WebUI | #826 and #965 merged; all 15 parity rows use `webui: transit-keys` | Owner-approved September 26 promotion remains In. The ADR explicitly retains owed amendment review. | **HOLD** on amendment closeout. Keep delivered #156/#820 closed. |
| Vault/OpenBao KV v2 destination #162 | #823 merged; `vault-kv` is compiled in | ADR and MVP destination amendment remain proposed, subject to #26 governance. This is separate from the already-admitted read-only import source. | **Conditional / HOLD**. Implementation and issue closure do not decide 1.0 membership. |
| Social sign-in and open registration A7 / #589 | Implementation #605-#613 and synthesis reconciliation are delivered | September 3 owner ruling places A7 post-1.0, first; #945 and separate #946 closeout completed its recorded dependencies. | **Existing scope settled**. A7 is not a 1.0 acceptance gate; preserve shipped behavior and tests. |

These inputs do not transfer the #1014-only or #966-specific review
dispositions, promote another capability, or change accepted exclusions.
#158 AWS acceptance, PR #969 and extracted controller #1057 remain outside
this 1.0 assignment. Accepted #805/#161/#148 and broad #1051 deferrals remain
outside this lane. A shipped-code test is not removable merely because its
capability is outside a release gate.

## Exact delivery and approval bindings

All five merges below were verified as ancestors of the assessed main with
`git merge-base --is-ancestor`. Live GitHub readback verified their merged
state and merge IDs. Hosted runs are existing execution evidence, not fresh
tests performed by this task.

| Record | Exact binding | What it establishes |
| --- | --- | --- |
| [Transit #826](https://github.com/Hikyo-Org/Hikyo/pull/826) | Head `d5bd0a388abfa9dc7ae0dbfb71b472089676428b`; merge `ebd2d2f8c0cd154963fe9919a56e79c049c929e2`, September 28 | Delivered backend/API/CLI. Its body explicitly says the amendment review is owed. |
| [Transit WebUI #965](https://github.com/Hikyo-Org/Hikyo/pull/965) | Head `c2342eeb517129832376314b0a6611cc94c5eaff`; merge `6da8f57cb8c8aae4cb58b7cc65ec5971c1705e0c`, October 18 | Delivered WebUI. [Integration 39137162163](https://github.com/Hikyo-Org/Hikyo/actions/runs/39137162163) succeeded on that merge. |
| [Vault KV #823](https://github.com/Hikyo-Org/Hikyo/pull/823) | Head `3ab3766cbc00861531dc833dcb817e4a519fe028`; merge `d118f0bbf45a6366828358adc873d9940bcb2e01`, September 27 | Delivered adapter and existing fixture coverage. No completed #26 destination amendment is inferred. |
| [Social reconciliation #945](https://github.com/Hikyo-Org/Hikyo/pull/945) | Approved head `8f7b34b76abf8709b4cdaf1366f1ad3a84838515`; merge `73b00f4d7eca27c36334c693d81c38300f715b52`, October 17 | Existing #589-scoped exception and editorial reconciliation. [Integration 39030048102](https://github.com/Hikyo-Org/Hikyo/actions/runs/39030048102) succeeded on that merge; CodeRabbit approval is on the approved head. |
| [Polling closeout #946](https://github.com/Hikyo-Org/Hikyo/pull/946) | Head `42a9204facf2d69ceb43e276aa96870db274e38b`; merge `888d9b61fb20aea8cc8addec488adadba2bd6d7f`, October 17 | Separately dispositioned #612 amendment dependency. [Integration 39026644027](https://github.com/Hikyo-Org/Hikyo/actions/runs/39026644027) succeeded on that merge. |

### Transit approval and remaining debt

The [Transit ADR](../adr/transit.md) records the owner's September 26
approval to redraw the map and choose the implementation handoff's
recommendations. It deliberately retains the
[oss-mechanics amendment procedure](../adr/oss-mechanics.md): reopen
the owning ticket, run the qualifying adversarial cross-model review, record
the amendment. The [#156 handoff](./156-transit.md) records that this review
is owed; the [#820 handoff](./820-transit-webui.md) expressly does not close it.

The [#826 review disclosure](https://github.com/Hikyo-Org/Hikyo/pull/826#issuecomment-5859506239)
says the native cross-provider pass was skipped. CodeRabbit findings,
ordinary source inspection, regression tests and a merged implementation do
not establish the missing named amendment review. The
[#79 October 19 record](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-6261802520)
explicitly preserves this debt after #965 delivery.

This task inspected #156/#820, #826/#965, relevant #79 and #26 comments,
current ADRs/handoffs and their Git history. No qualifying existing
Transit amendment review or Transit-scoped owner review disposition was found
in those records. That is a bounded evidence finding, not a claim that no
private review can exist. Cross-provider review is **SKIPPED** under the
current reviewer-availability rule; amendment closeout remains **HOLD**.

### Vault KV is a distinct conditional destination

The [Vault KV ADR](../adr/vault-kv-adapter.md),
[deployment-adapter sibling banner](../adr/deployment-adapter.md) and
[MVP boundary cloud-destination row](../adr/mvp-boundary.md) all retain the
proposed status. The [#162 delivery comment](https://github.com/Hikyo-Org/Hikyo/issues/162#issuecomment-5869201164)
closes implementation, not that governance condition. The
[#162 handoff](./162-vault-kv-adapter.md) explicitly records skipped
cross-provider review and the open #26 amendment.

Live #26 inspection found its September 3 social synthesis reopen/re-close
record, not a Vault KV-specific disposition. #823's inspected PR discussion
and reviews do not establish that procedure either. Keep the gate open unless
the owner supplies qualifying scoped evidence or explicitly dispositions it.
There is no runtime feature switch demonstrated by this governance gate:
`internal/adapter/provider.go` declares `VaultKVProvider`, and
`internal/app/adapter_provider.go` constructs the client/module. This task
does not promise an artifact that omits those bytes.

### Social membership is already decided by its own owner

The [September 3 #589 ruling](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-5531905677)
selected shipping 1.0 without social A7 as an acceptance member; its
freeze-sensitive #617 prerequisites remained pre-1.0. The current MVP boundary
puts A7 in section 4.3, post-1.0, first. Implementation presence is not an
approved reversal of that ruling, nor proof of physical absence from artifacts.

The [October 16 #589 exception](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6220313789)
qualifies Astra high authorship plus independent GPT-6.1 Sol high review for
the exact #945 editorial head in the table. The
[later #589 direction](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6224728411)
preserves independent substantive review of final bytes after repairs within
that scope. Neither grants a new feature or release-scope decision. The
[#612-only exception](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-6220244515)
is separate. Verified #945/#946 delivery discharged the dependencies; the
[#589 closeout](https://github.com/Hikyo-Org/Hikyo/issues/589#issuecomment-6250614401)
records completion. Shared #32 need not close globally for that result.

The original cross-provider route remains historically unperformed. No new
social membership decision is required to preserve this operative scope.
The old exceptions do not automatically qualify this reconciliation's new
bytes; its ordinary review and protected merge requirements remain.

## Owner decision packet

No option below is selected by this document or by merging the factual
corrections. Record Transit and Vault KV separately, with an owner-authored
decision link. A generic approval of the stabilization effort is not a review
waiver. Recommended interim disposition: **T1 + V1**, preserving the existing
gates until the owner selects a qualifying path. This keeps both closeouts on
HOLD; it does not make a candidate ready to freeze.

| Choice | Concrete owner action | Consequence |
| --- | --- | --- |
| T1: retain Transit gate | Supply qualifying existing cross-provider amendment review, or wait for an authorized qualifying review route. | Promotion/delivery stay recorded; freeze closeout remains HOLD until the review and amendment ceremony are complete. |
| T2: scoped alternate route | Explicitly name the authorship/verification and independent reviewer providers, models and effort accepted for the #156 amendment only, plus the source revision and covered paths. | A new substantive review must actually cover the final bytes and disposition findings before closeout; naming a route is not a completed review. |
| T3: explicit Transit disposition | State exactly which #156 amendment obligation is waived or otherwise resolved, for which revision/paths, with rationale and retained risks. | Changes this scope's governance only by explicit owner decision. No such waiver is presently recorded; implementation/CI approval is insufficient. |
| V1: retain Vault conditional gate | Supply qualifying #162/#26 amendment evidence, or retain conditional membership pending the procedure. | No operative promotion is claimed; 1.0 membership and amendment closeout stay HOLD. |
| V2: approve Vault destination membership | Explicitly approve the named KV v2 destination for 1.0 and supply the required review or a separately explicit Vault-scoped alternate route/disposition. | Update #26 and its MVP/ADR records through reviewed closeout. Approval of membership alone does not complete review or candidate acceptance. |
| V3: defer Vault destination membership | Explicitly assign the destination to a later release and state how the existing shipped surface is to be represented at freeze. | Coordinator records the decision; any needed runtime/artifact change is a separately scoped task. Do not remove code/tests or assert absence from this documentation decision. |

### Review boundaries and executable closeout

1. Freeze the review input with `git rev-parse HEAD` and
   `git rev-parse HEAD^{tree}` in the actual review checkout. Record the
   source SHA, covered path/blob identities, reviewer provider/model/effort,
   substantive findings and dispositions. For an older review, account for
   every later change before treating it as evidence for current bytes.
2. Transit scope is `docs/adr/transit.md` and its #156 pointer banners in
   `mvp-boundary.md`, `encryption-model.md`, `permission-model.md` and
   `tenant-isolation.md`, plus the corresponding implementation consumers.
   Review key custody/derivation, version/lifecycle fences, permissions,
   scheduler doors, audit non-disclosure, admission and WebUI sensitive-data
   handling. Keep #820 implementation delivery separate from the amendment.
3. Vault scope is `docs/adr/vault-kv-adapter.md`, its proposed MVP destination
   row and `deployment-adapter.md` sibling banner, plus the corresponding
   adapter/import consumers. Include no-read closure, CAS/ownership,
   dispatched unknown outcomes, version-bound prune, credentials/trust,
   namespace/destination identity and import-loop refusal. Existing fixture
   tests do not certify governance or a live managed provider.
4. The maintainer records the relevant reopen/re-close ceremony and scoped
   disposition on the owning tickets, then the reviewed ADR closeout records
   the exact evidence. Transit pointers belong to #26, encryption #14,
   permission #15 and tenant isolation #23; Vault's gate expressly names #26
   and its deployment seam is owned by #28. Preserve unrelated obligations
   on shared tickets. This task posts no issue comments or owner decisions.
5. The release-scope coordinator consumes the resulting decision links and
   updates its canonical records. Qualification then selects and tests its
   own final integrated candidate. Source checks, governance approval,
   release membership and candidate acceptance remain separate evidence.

## Verification and handoff boundary

The changes are Markdown only. No application behavior, schema, dependencies,
provider fixture, release trust, publication or deployment changes. The shared
fast-feedback plan/run from the complete PR diff and a focused local
Markdown link/anchor check validate this documentation change; the PR records
their final command results and signed source head. Site preparation mappings
were inspected: these ADR/handoff/spec paths are not generated site inputs.
Application/both-engine/provider/browser suites are **NOTRUN** by this lane
because no executable or persistence surface changed. Historical hosted runs
above cannot qualify a later integrated candidate.

Cross-provider amendment review remains **SKIPPED**, never CLEAN. Transit and
Vault closeout remain **HOLD** pending the independent decisions above.
Ordinary PR review, hosted feedback and maintainer merge remain separate;
this assignment authorizes a reviewable PR, not merge or release. An offline
HTML companion is a local-only chat deliverable, outside committed paths.
