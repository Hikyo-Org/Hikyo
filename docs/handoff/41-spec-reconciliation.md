# #41: reconcile the product synthesis with the amended specification

## Contract and status

This is the synthesis handoff for
[#41](https://github.com/Hikyo-Org/Hikyo/issues/41), dated 2026-10-16 against
`6769ee55dcd4087eeaf42547e9abde01c4f0818c`. It corrects
[product-requirements.md](../spec/product-requirements.md) using existing
amendments. It makes no new scope or review decision. #41 and the release gate
[#79](https://github.com/Hikyo-Org/Hikyo/issues/79) remain open.

The [spec canonicality rule](../spec/README.md) still applies: read the owning
ADRs through their banners and operative conditions. A tracker checkbox,
implementation merge or green CI total does not replace an acceptance clause,
required review or release act.

## Synthesis corrections

- The unconditional no-HA statement now points to the single-region,
  PostgreSQL-only #146 promotion; SQLite remains single-node and cross-region
  active-active remains excluded.
- The blanket restart-only statement now points to live managed-component
  replacement and supported controlled bootstrap transitions under the
  [full-catalogue self-configuration contract](../spec/self-configuration-full-catalogue.md).
  This includes the accepted expansion beyond the earlier nine-key proposal;
  independent remotes retain their own configuration and authority.
- The stale exclusions for dynamic credentials, PKI and approvals now reference
  their bounded promotions. Temporary access, SSH user certificates and transit
  are represented without broadening their contracts.
- Delivery points to per-provider amendments rather than presenting the first
  two adapters as a complete current destination inventory.
- Competitive findings are identified as the original survey; scope and
  implementation are separated from release acceptance.

## Acceptance reconciliation routed to #79

#79 owns [acceptance-1.0.md](../release/acceptance-1.0.md),
[release-plan-1.0.md](../release/release-plan-1.0.md) and
[self-hoster-candidate.md](../release/self-hoster-candidate.md). This change does
not edit those files. Reconcile the following against the final candidate:

| Requirement | Source evidence | Required reconciliation |
| --- | --- | --- |
| C-ACC temporary access | MVP amendment 7 and §1.2; `internal/isolation/access_e2e_test.go` (`TestAccessLifecycle`, `TestAccessConcurrentApprovals`, `TestAccessExpiryAcrossNodes`); `web/e2e/registry.ts` temporary-access flow; `web/e2e/flows/matrix.spec.ts` | The September 49-row ledger omits C-ACC. Include every clause, engine and check class with actual candidate evidence. Source presence is not PASS. |
| Later bounded promotions | MVP amendments and §4: HA #146, dynamic #147, approvals #151, temporary access #152, PKI #154, SSH #155, transit #156 | Preserve the full amended capability list in acceptance and self-hoster reconciliation; do not stop at the original 39 stories or September row count. |
| Provider additions | `docs/adr/deployment-adapter.md` and each provider's governing amendment | Preserve destination-specific scope, formal-review conditions, live-provider obligations and explicit post-1.0 dispositions. |
| Reporting | Closed #791; `docs/adr/k8s-integration.md`; `internal/isolation/k8s_reporting_e2e_test.go` | The old release plan calls reporting undesigned. Replace that historical assertion with exact implementation and candidate-proof boundaries. |
| Current fixes | Existing implementation owners and their exact merged SHAs | Reconcile source and recorded execution after merge. Local tests, queue admission, merge-group proof and final release acceptance are distinct. |

The #41 source-only investigation resolved 198 local source anchors across the
49 existing release-ledger rows. It did not execute them or certify that those
references cover every clause. Its outside-Git audit artifacts are not required
to follow the source pointers above; their machine-local paths are intentionally
absent here.

## Preserve unresolved authority

**AWS #158:** [MVP amendment 8](../adr/mvp-boundary.md) describes a promoted-In
destination, conditional on implementation merge and the required governance
review. The [2026-10-16 audit correction](https://github.com/Hikyo-Org/Hikyo/issues/158#issuecomment-6220607066)
replaced `release:post-1.0` with `release:1.0` and `release:decision` to preserve
that conditional relevance and governance conflict. The Post-1.0 title and v1.1
milestone were unchanged pending reconciliation. These labels do not establish
final promotion, accepted real-provider behavior or a review waiver. #158 retains
the scoped review and real-provider reconciliation; #79 records the conditional
disposition in its acceptance inventory. This handoff neither drops AWS from
the gate because of older metadata nor declares its conditions discharged
because code merged.

**Explicit later scope:** MVP amendment 6 names Cloudflare #161 as v1.1 and
amendment 9 names file destinations #164 as v1.1. The #807 banner is conditional
on its implementation merge. The social-sign-in/A7 disposition remains owned by
#589 and its pre-freeze amendment. Authorization to implement the entire work
queue does not silently promote each item into the 1.0 release.

**Scoped review dispositions:** the owner has approved the #612, #15 and #589
review paths for this implementation run. That approval is not a general
cross-provider-review waiver. At this handoff, PRs #945 and #946 remain pending
per parent coordination; their merges and resulting source have not been
claimed here. #15 owns the capability ledger/generator, #589 owns A7, and #527
owns the site roadmap. Reconcile their final merged work rather than editing
their files concurrently.

## Existing provider-owner reproduction requests

The following source findings were sent to the #79 owner for acceptance
reconciliation and remain with their existing implementation owners:

- #158: `internal/isolation/aws_external_e2e_test.go` invokes concurrency,
  replacement-race and interrupted-adoption helpers in the emulator entrypoint;
  the real smoke invokes only `runExternalAWSLifecycle`. Request the missing
  real-provider and durable-outbox proof in #158's allocated sandbox. A passing
  emulator is not evidence of real AWS identity, permission or concurrency
  behavior.
- #161: `internal/adapter/cloudflare/module.go` checks active token metadata,
  account cardinality and target resolution; these are not an attestation of
  exact project/write permissions. The real lifecycle uses an in-memory journal,
  writes Pages preview only and ignores foreign-name cleanup errors. Request
  the activation-policy, failure/cleanup and bidirectional preview/production
  isolation reproductions from #161's owner before accepting those clauses.

These are existing acceptance gaps, not new demonstrated production failures or
duplicate implementation assignments. No provider calls were made for this
synthesis change.

## Evidence boundaries and delivery

- Historical main `4af540bf377cc1e2a34163d4de150d395bbea5a9` passed
  [CI run 38887776523](https://github.com/Hikyo-Org/Hikyo/actions/runs/38887776523),
  all 48 jobs. This is historical exact-source aggregate evidence, not a new
  final-candidate run or proof for subsequent amendments.
- #936's [PR #937](https://github.com/Hikyo-Org/Hikyo/pull/937) merged on
  2026-10-16 at 16:40:31 UTC as the base SHA above. Its exact
  [merge-group run 38929885843](https://github.com/Hikyo-Org/Hikyo/actions/runs/38929885843)
  reports 48 successful jobs, and the merge is an ancestor of this checkout.
  This verifies that merger; it is not final-main release acceptance.
- This documentation patch has no application runtime changes. During its
  preparation, installs, builds, tests, generators and browsers remain held by
  resource coordination. Source review does not satisfy those checks.
- After independent source review, request the small documentation validation
  slot. Complete applicable checks, signed+DCO commit and push, PR review repairs,
  protected merge validation and exact main-merge verification. Keep HTML report
  companions outside Git.
- #79 still owns final-candidate acceptance, external provider/client evidence,
  stable-policy activation, freeze and verified public artifacts. Release
  publication and production deployment are not authorized by this task. A
  merge of this documentation patch cannot close #41 or #79.
