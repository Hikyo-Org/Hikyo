# Post-quantum migration contract, draft v0.6

Tracking: [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124), under
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123). Assessed 2026-11-02.

**Status: owner decided and security approved, not operative.** The
owner accepted [the register](../../adr/post-quantum-migration.md#decision-register)
as recommended and gave maintainer security approval on 2026-11-02. This package grants no authority, and no ADR amendment has landed, so existing
operative ADRs and runtime behavior remain authoritative. Source exploration and
bounded planning may proceed in dependent tickets; implementation needs the ADR
amendments it depends on and its own security approval before it can merge.

## Read this package

| Document | Responsibility |
| --- | --- |
| [Inventory](inventory.md) | Source-pinned boundaries I01 to I32: purpose, custody, lifetime, writers, readers, recovery, provenance; census and bounds |
| [Profiles](profiles.md) | Confidentiality and authenticity defined separately; strength, parameters, encodings, bounds, strict and compatibility profiles, composition |
| [Migration](migration.md) | Reader-first ladder, keys and escrow, mixed paths, retirement, floor, recovery, dispositions, exception register |
| [External compatibility](external-compatibility.md) | #1134 register: external routes, classification, owners, recheck triggers, allowed capability statement, strict-route HOLD |
| [Qualification](qualification.md) | Requirement-to-test matrix, scope, estimates, critical path |
| [Proposed ADR](../../adr/post-quantum-migration.md) | Decision register DEC-01 to DEC-20 (the owner's decisions), waiver and governance route, proposed amendment text |
| [Handoff](../../handoff/1124-pqc-contract.md) | Acceptance-criterion map, dated PR state, evidence, HOLD and NOTRUN, next steps |

Start with the [decision register](../../adr/post-quantum-migration.md#decision-register).

## Source pin

Canonical main `b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`, tree
`95447e83be458194042938c7d43e743bda71effb`, read on 2026-11-02. This package is
stacked on the program draft (PR #1148). Source references are repo-relative at
the pin, not promises about future main; re-pin before reuse. The issue's
`2ac7bb36633d3ae0a99d7724968ca8c3cbf18334` assessment is historical navigation.

## Approval and claim rules

Marc Went owns scope, strength and exception decisions and assigns the
maintainer security reviewer; as sole maintainer he is both, so adversarial
review compensates. A model review is not human approval. Each decision records
the reviewed revision, package version, approver and an immutable receipt. The
receipts are in the ADR. Changes to parameters, composition, trust, bounds or exceptions need a
new reviewed version. The 2026-11-02 owner decisions are recorded once, and the
amendment route is stated, in the [ADR](../../adr/post-quantum-migration.md#owner-decisions-already-made-2026-11-02).

Claims are per surface, direction and peer version. Source capability, local
fixtures, hosted CI, provider behavior, artifacts and observed runtime are
separate evidence classes. Missing evidence is NOTRUN; a missing mandatory
decision or prerequisite is HOLD. Negotiating a hybrid group is not
authenticating the peer, and migration cannot revoke copies already harvested.

Production key generation, rotation, trust activation, provider changes, issue
closure, merge, release and deployment each need their own authorization.
