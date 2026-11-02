# Post-quantum migration program

Status: **DRAFT, parent acceptance HOLD**. This is the issue-owned coordination
and acceptance draft for [#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123),
not an approved cryptographic profile or an implementation-completion claim.
The [delivery handoff](../handoff/1123-post-quantum-program.md) records the
scope and continuation procedure for this draft.

## Ownership and decision boundary

The parent owns the relationship between requirements, child deliverables,
qualification evidence and the final capability statement. The inventory,
parameters, compatibility/strict profiles, composition and proposed ADR amendments
belong to [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124). Approval alone
does not amend a locked ADR: each amendment follows the
[amendment procedure](../../GOVERNANCE.md#amendment-procedure), which reopens that
ADR's originating ticket, runs the same adversarial cross-model review that locked
it and records the amendment in the ADR. Integrated
fixtures, measured budgets and executable staging/operator runbooks belong to
[#1135](https://github.com/Hikyo-Org/Hikyo/issues/1135). This document replaces
neither child. Read each child's full acceptance criteria before implementing it.

Marc Went, as the current maintainer/BDFL, owns scope and policy decisions;
maintainer security approval is a separate recorded gate. With a
[single maintainer](../../GOVERNANCE.md), both come from one person, so neither is
independent human review. [CONTRIBUTING](../../CONTRIBUTING.md#security-sensitive-contributions)
also requires adversarial security review of maintainer-authored cryptography
changes, and automated review only compensates for independent human review.
All eleven children were unassigned at this checkpoint. A child issue's work area
does not claim that a person has accepted custody or that a security reviewer has
approved the work.

This draft retains the existing symmetric hierarchy, immutable key purposes,
tenant/environment authority, admission and egress restrictions, sealed custody,
durable redacted audit, restore holds and multi-node fences. It selects no new
default, algorithm parameter set, PAKE composition or signature policy. Candidate
algorithms named by the issues are not product approval. Do not change a closed
algorithm set or locked ADR through a capability label or planning document.

The issue's exclusions also bind every child: no blanket AES rewrite, custom
cryptographic primitive, ad hoc PAKE, mandatory new HSM/KMIP/KMS product, public
CA, ACME/OCSP expansion or FIPS certification. Official provider contracts remain
authoritative, so GitHub's required libsodium secret envelope cannot simply be
replaced with age, SSH hybrid key exchange does not migrate host/user/CA
signatures, and WebAuthn/SAML/OIDC and OS signing remain independently qualified.

Existing decisions remain authoritative:
[encryption](../adr/encryption-model.md), [threat model](../adr/threat-model.md),
[Transit](../adr/transit.md), [pairing](../adr/remote-instance-pairing.md),
[private PKI](../adr/pki.md), and
[signed upgrades](../adr/signed-upgrade-compatibility.md).
Delivered #75/#76/#154/#155/#156/#198 work stays delivered. This draft infers that
reopening a ticket for an ADR amendment covers that amendment only; neither the
governance procedure nor #1123 states this, so #1124 must confirm it. The 1.0 boundary was set under
[#26](https://github.com/Hikyo-Org/Hikyo/issues/26), now closed. The open 1.0 gate
[#79](https://github.com/Hikyo-Org/Hikyo/issues/79) records 1.0.0 as published on
2026-11-01, and [#527](https://github.com/Hikyo-Org/Hikyo/issues/527) holds the
roadmap. This migration does not automatically create a 1.0 blocker or rewrite
those dispositions.

## Source checkpoint

The parent and children were read on 2026-11-01; their states and open PRs were
rechecked on 2026-11-02. Canonical main and this draft's starting base were
`b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`. The issue's older assessment base
was `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334`. The source entry points below,
the five encryption/threat/Transit/pairing/PKI ADRs and the signed-upgrade ADR
have no changes between those two revisions. This is a bounded navigation check,
not the exhaustive inventory required by #1124 or a runtime PQ test.

| Observed source boundary | Entry points at the starting base | Owning follow-up |
| --- | --- | --- |
| 32-byte symmetric key and one envelope algorithm identifier | `internal/crypto/crypto.go`, `internal/crypto/envelope.go` | #1124 retains the hierarchy unless an explicit evidence-backed decision changes it, and states its bounds |
| Concrete age X25519 readers/writers and classical receipt namespace | `internal/crypto/backup/backup.go`, `internal/crypto/backup/recipients.go`, `internal/backupreceipt/artifacts.go` | #1126 traces export, receipts, custody and recovery together |
| Native listener permits TLS 1.2 | `internal/app/tls.go` | #1127 traces every actual connection owner and terminator |
| Sealed delivery protocol v1 uses X25519 and Ed25519 | `internal/crypto/sealedhook/sealedhook.go` | #1128 migrates receiver, sender and acknowledgement together |
| Closed Transit algorithm set and Ed25519 signing | `internal/crypto/transit.go` | #1129 preserves key identity/purpose and custody |
| 2048-bit SRP and Ed25519 DPoP | `internal/pairing/srp.go`, `internal/pairing/dpop.go` | #1130 retains the separate protocol feasibility/security gate |
| Ed25519 rollout command authority | `internal/configrollout/transport.go` | #1131 binds enrollment, signer, verifier and reconciliation |
| Classical key policies and release verification | `internal/pki/keys.go`, `internal/sshca/keys.go`, `internal/releasetrust/signature.go` | #1133/#1134/#1132 distinguish first-party authority from external support |

Open PRs were also checked at this checkpoint. The only ticket-owned PQ PR found
was draft [#1141](https://github.com/Hikyo-Org/Hikyo/pull/1141), a documentation
proposal for #1133 that enables nothing and leaves that child on HOLD.
Adjacent open PRs [#1137](https://github.com/Hikyo-Org/Hikyo/pull/1137)
maintenance, [#1113](https://github.com/Hikyo-Org/Hikyo/pull/1113) deployment and
[#1140](https://github.com/Hikyo-Org/Hikyo/pull/1140) release trust are separately
owned; release documentation [#1138](https://github.com/Hikyo-Org/Hikyo/pull/1138)
and [#1139](https://github.com/Hikyo-Org/Hikyo/pull/1139) are merged into this
base. None of them is PQ acceptance. Recheck live
main, PR heads and ownership before taking a child; do not overwrite an active
owner or duplicate its changes. Stack a dependent delta when necessary.

## Dependency and deliverable register

This copies the dependency order from #1123 as a reviewable checkpoint. All
eleven children were open when read. No approved #1124 profile receipt was
present in the retrieved issue discussion. Open/closed state is not approval
or implementation evidence; verify the linked decisions and source each time.

| Child and work area | Prerequisites | Deliverable consumed by the parent | Initial gate |
| --- | --- | --- | --- |
| [#1124 policy/inventory](https://github.com/Hikyo-Org/Hikyo/issues/1124) | None | Versioned full inventory, two scoped profiles, strength/bounds, ADR decisions, exceptions, requirement-to-test map and refined effort/critical path | Drafting can proceed; approval HOLD |
| [#1126 backups](https://github.com/Hikyo-Org/Hikyo/issues/1126) | #1124 | Hybrid generation/export, algorithm-aware receipts/custody, both-engine restore and frozen legacy recovery | Implementation HOLD |
| [#1127 transport](https://github.com/Hikyo-Org/Hikyo/issues/1127) | #1124 | Effective per-hop policy, actual-client negotiation/refusal evidence and separate authentication limits | Implementation HOLD |
| [#1128 sealed webhooks](https://github.com/Hikyo-Org/Hikyo/issues/1128) | #1124, #1127 | Receiver-first protocol, delivery/ack lifecycle, bounded negative vectors and durable reconciliation | Implementation HOLD |
| [#1129 Transit](https://github.com/Hikyo-Org/Hikyo/issues/1129) | #1124 | Approved algorithm amendment, full key lifecycle, custody and API/CLI/client/UI consumers | Implementation HOLD |
| [#1130 pairing/proofs](https://github.com/Hikyo-Org/Hikyo/issues/1130) | #1124, #1127 | Reviewed feasible protocol and two-instance lifecycle, replay/revocation/restore evidence | Implementation HOLD; protocol feasibility also HOLD |
| [#1131 rollout authority](https://github.com/Hikyo-Org/Hikyo/issues/1131) | #1124 | Authenticated trust transition, real producer/controller path and invalid-input zero-mutation proof | Implementation HOLD |
| [#1132 release trust](https://github.com/Hikyo-Org/Hikyo/issues/1132) | #1124, #1126 | Legacy-to-verifier bridge, mandatory artifact verification and independent recovery/CI authority | Implementation HOLD |
| [#1133 private PKI](https://github.com/Hikyo-Org/Hikyo/issues/1133) | #1124, #1127 | Qualified issuer/leaf/CRL lifecycle, relying-party and both-engine recovery evidence | Implementation HOLD |
| [#1134 external compatibility](https://github.com/Hikyo-Org/Hikyo/issues/1134) | #1124 | Versioned owned route exceptions, actual-consumer evidence and bounded allowed claims | Qualification HOLD; source exploration can proceed |
| [#1135 integrated qualification](https://github.com/Hikyo-Org/Hikyo/issues/1135) | #1124, #1126, #1127, #1128, #1129, #1130, #1131, #1132, #1133, #1134 | Exact-candidate packet, measured budgets, portable executable runbooks and surface-specific statement | Integrated acceptance HOLD |

Source exploration and bounded planning can proceed while prerequisites are
unresolved, and #1124 itself permits inventory and design drafting. Do not select unapproved defaults or treat a
prepared plan as passed qualification.
After profile acceptance, confidentiality of long-lived material leads:
backups and controlled transport, then independent protocol/signature owners.
Pairing feasibility, release-root bootstrap and external interoperability remain
critical-path uncertainties.
The issue's estimates are preliminary; #1124 owns refinement, not a delivery date.

## Parent requirement-to-evidence ledger

The stable IDs below correspond in order to the fifteen parent acceptance
criteria. Each starts **HOLD** because none can pass without an approval or
prerequisite that does not yet exist. A future evidence packet must map the full
criterion and its child's detailed criteria, not only the short row label.
Do not mark a row PASS from this draft's documentation checks.

| ID | Required result | Evidence owner and required proof |
| --- | --- | --- |
| PQ-01 | Complete cryptographic/data-flow inventory | #1124: pinned revision; every first-party/external boundary, consumer, recovery door, format, custody and trust root |
| PQ-02 | Approved confidentiality and authenticity profiles | #1124: owner and maintainer security receipts binding profile version, strength/parameters, scope, compatibility, ADR amendments, protocol versions and trust bootstrap |
| PQ-03 | Recoverable hybrid backups | #1126: generation -> protected escrow/export -> receipt/custody drill -> SQLite and PostgreSQL restore; frozen legacy archives and mixed-recipient refusal |
| PQ-04 | Enforced controlled-hop confidentiality | #1127: effective runtime policy and actual negotiated group for every claimed hop; strict connections refuse classical-only peers before credentials/data, with the strict/compatibility scope set by #1124; certificate authentication reported separately |
| PQ-05 | Complete delivery and command/proof lifecycles | #1128/#1129/#1130/#1131: real consumers, end-to-end success and negative cases, version skew, restart/restore and authority fences |
| PQ-06 | Qualified certificates and external identities | #1133/#1134: issuer/leaf/CRL/chain lifecycle on named relying parties; explicit owned SSH/federation/authenticator dispositions |
| PQ-07 | Authenticated installer/updater transition | #1132 with #1126: legacy reader -> authenticated verifier bridge -> mandatory PQ verification of exact artifacts; no classical bypass; roots/recovery distinct from delegated CI |
| PQ-08 | Bounded, independently checked formats | Every implementation child, aggregated by #1135: maintained-reference vectors, fuzz/negative evidence, algorithm/scope binding and reviewed size/allocation bounds; sanitized evidence only |
| PQ-09 | Safe transition and historical recovery | #1135 with all lifecycle owners: reader/verifier-first deployment, skew, rollover/revocation/retirement, historical verification, retained archives, interruptions and enforced rollback floor |
| PQ-10 | Preserved security and persistence invariants | Each affected owner, aggregated by #1135: tenant/environment permissions, admission/egress, sealed custody, durable redacted audit, re-encryption coverage, restore reconciliation and multi-node fences on both engines where applicable |
| PQ-11 | Measured platform budgets | Budgets are approved with the #1124 policy, inferred from #1127's "budgets selected by the approved policy" because #1124 itself names only maximum sizes; #1135 records representative amd64/arm64 size, latency, CPU/memory and bounded-concurrency results; compilation/counts/mocks do not establish performance or interoperability |
| PQ-12 | Owned external compatibility register | #1134: each route's exact protocol/version evidence, classification, owner, impact on each profile, recheck trigger and linked follow-up; unresolved mandatory scope keeps that profile HOLD |
| PQ-13 | Validated final candidate and approval | #1135: complete-diff local checks, applicable exact-head hosted CI, independent Standards and Spec, adversarial security review, maintainer security approval; separate source/fixture/hosted/provider/artifact/runtime records |
| PQ-14 | Executable operator handoff | #1135 and surface owners: portable runbooks and a self-contained handoff, with commands validated against the tested binary; protected provisioning, staged rollout, abort, restore/rollback, incident recovery and retention; current before/after evidence for relevant rendered UI changes |
| PQ-15 | Honest closure and separately authorized delivery | Parent maintainer: exact implemented/qualified scope and remaining exceptions; mandatory children pass or explicit scoped owner reduction links residual work; release/publication/deployment status and authorization recorded separately |

## Evidence record contract

Keep one record per requirement, surface and evidence class. Missing evidence
means NOTRUN, missing approval/prerequisite means HOLD, observed failure means
FAIL, and a deliberately omitted check means SKIPPED with its reason. PASS
names only the observed result and scope. These are evidence statuses, not
protocol identifiers or runtime configuration values.

Copy this Markdown record into the future qualification packet. Replace every
placeholder; an incomplete record cannot support PASS.

```text
Requirement ID and full criterion:
Owning child, PR and accountable execution owner:
Claim: confidentiality | authenticity (separate records if both)
Surface, direction, complete path/hops and explicit exclusions:
Approved profile version/digest and immutable decision/security receipts:
Base commit, candidate commit and tree:
Binary, toolchain, dependency, peer and verifier versions:
Artifact names/digests and topology/datastore/architecture:
Effective configuration, precedence and trust generation (sanitized):
Evidence class: source | local fixture | hosted CI | provider | artifact | observed runtime
Exact command/flow, prerequisites and authorization reference:
Expected outcome and actual observed outcome, including negative cases:
Result: PASS | FAIL | HOLD | NOTRUN | SKIPPED
Immutable run/attempt/job or retained sanitized evidence reference and digest:
Review provenance, coverage and remaining findings:
Exception owner, impact, linked follow-up and recheck/expiry trigger:
Key/archive retention, rollback floor and remaining human action:
```

Keep private material, passwords, plaintext fixtures, bearer credentials,
sensitive transcripts and cryptographic payloads out of public records. Public
key identifiers and sanitized digests may identify evidence without conveying
custody. Refer to protected evidence by an approved opaque reference, never a
machine-local path. If the protected receipt is unavailable, retain HOLD.

A source review establishes inspected behavior, not a successful handshake or
restore. A local actual-peer fixture does not establish production negotiation.
Hosted CI does not establish provider acceptance, artifact publication or
deployment. A publication receipt does not establish installed enforcement.
Hybrid KEX does not establish PQ certificate authentication; independent
classical decryption or either-signature verification can defeat the claimed
profile. Scope confidentiality and authenticity separately, never as a universal
"PQ-ready" badge. #1124 owns the precise cryptographic policy for these cases.

Re-pin the entire final candidate after any repair, rebase or dependency change.
Review/test evidence from older bytes remains historical. It can be carried
only with an explicit source/consumer impact analysis and permitted evidence
reuse; exact-final-range review and applicable current-head CI still need their
own bindings. Recheck live peers and expiring decisions before environment use.

## Staging, abort and closure gates

1. **Draft preparation:** maintain this ledger, the #1124 draft and child plans. Parent
   acceptance remains HOLD. No new cryptographic writer or production trust is
   enabled by a planning PR.
2. **Policy approval:** #1124 records owner and maintainer security approval,
   accepted parameters, strict/compatibility behavior, trust bootstrap, budgets
   (see PQ-11), the scope of a ticket reopened for an ADR amendment and exception
   decisions, and each locked ADR records its amendment under the
   governance procedure. Only then may dependent behavior be implemented and
   presented as ready for its own review.
3. **Reader/verifier qualification:** surface owners and #1135 prove historical
   recovery/verification and mixed versions with frozen fixtures. Determine the
   minimum new-format-capable reader/verifier and authenticated bridge before
   writing new formats or requiring new signatures.
4. **Isolated migration qualification:** use separately authorized disposable
   fixtures for new keys/writes, downgrade/stripping/substitution refusal,
   restart/failover and both-engine restore. Keep old-key/archive custody until
   approved retention and retirement criteria are demonstrated. Rewrapping
   cannot revoke attacker-held historical copies.
5. **Scoped acceptance:** #1135 supplies the complete packet; the parent owner
   evaluates PQ-01 through PQ-15 and records adversarial security review and
   maintainer security approval.
   Unresolved mandatory scope keeps the relevant profile HOLD. A narrowed scope
   requires explicit owner approval and linked residual work, not a checkbox.
6. **Environment rollout:** requires a later resource-specific authorization,
   tested operator runbook, custody/provisioning and observable cohort success.
   Source acceptance grants no production activation or publication authority.
   Record enforcement and certificate authenticity separately on the real hops.

Abort progression on a failed or missing mandatory decision, unknown algorithm
or trust generation, missing custody/restore proof, lost audit closure, unsafe
downgrade, exceeded approved budget or an unexplained changed consumer. Preserve
the operation's existing recovery/maintenance fences and reconcile the exact
failed stage. Do not clear a hold, reactivate revoked authority or fall back to
classical behavior to finish migration. Roll back only to a reader/verifier
above the established format/policy floor with valid trust and retained custody.
Executable commands and measured success/abort criteria remain #1135's deliverable;
these gates are not a production cutover runbook.

This draft grants no authority. Merge, issue closure, release/tag/publication,
production deployment or trust activation, provider mutation, purchases/grants
and private test/disclosure messages require their own explicit authority from
the maintainer. Human and external-resource gates
remain visible even when all source checks pass.
