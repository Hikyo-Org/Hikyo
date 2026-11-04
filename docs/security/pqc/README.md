# Post-quantum migration contract, draft v0.7

Tracking: [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124), under
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123). Assessed 2026-11-02.

**Status: owner decided and security approved, not operative.** The
owner accepted [the register](../../adr/post-quantum-migration.md#decision-register)
as recommended and gave maintainer security approval on 2026-11-02. This package grants no authority. Four ADR amendments are recorded as banners that are not owner-locked and become operative only when the implementation they name merges, so existing
operative ADRs and runtime behavior remain authoritative until then. Source exploration and
bounded planning may proceed in dependent tickets; implementation needs the ADR
amendments it depends on and its own security approval before it can merge.
On 2026-11-04 the owner authorized merge to `dev` once required checks are green, except
#1172, and that instruction is the security approval for each final reviewed head. Merging
this contract makes nothing here operative. A banner whose trigger is the #1126 or #1127
implementation becomes operative when #1158 or #1156 merges, although those tickets stay open.

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

Feature PRs target `dev` since #1159, so the stack is rooted there. Rechecked on
2026-11-04 against `dev` `5d3705d872974040c2727d015c54df45f7e979df` (a diff of
the pin against it, intersected with the paths the contract cites). This replaces
the 2026-11-03 note, which said only `.github/workflows/nightly.yml` and
`CONTRIBUTING.md` changed; that was incomplete. These cited paths changed, and a
child must recheck these rows before reuse:

- **I27**: `.github/workflows/release.yml` (release validation now requires a
  source proof; the keyless line moved from 622 to 633), `.github/workflows/nightly.yml`
  (now publishes a cosign-signed Helm chart, which I27 does not list) and 20 files
  under `scripts/release`, including `nightly-chart.sh`, which signs and verifies
  the chart with the workflow identity.
- **I31**: `chart/hikyo/templates/kyverno-verify-images.yaml` and `deployment.yaml`,
  changed by #1113 (now merged). Nightly charts get a keyless policy bound to
  `nightly.yml` on `main` and an exact source revision. The row lists neither.
- **Census, search 1**: 182 non-test Go files in 72 directories, up from 179 in 71.
  The three new files import only digests (`crypto/sha256`, and `crypto/sha1` in
  `resolve.go`), so none holds a keyed primitive: `internal/admission/live_sessions.go`,
  `scripts/ci/pr-gate/dev.go` and `scripts/ci/source-proof/resolve.go`.
  `scripts/ci/source-proof` is classed as test tooling in the inventory but gates
  `release.yml`, `nightly.yml`, `nightly-promote.sh` and `require-green-main.sh`;
  the inventory now says so.
- **Not security-reviewed since the pin**: `internal/cli` (`compose.go`, `compose_run.go`,
  `reveal_window.go`, `values.go`; I04, I05, I06) and `internal/service`
  (`saml.go`, `oidc_flow.go`, `oauth2_flow.go`; I16 to I19). They changed for
  admission and session work; no keyed primitive was checked.
- **Process documents**: `CONTRIBUTING.md` and `docs/development/ci.md` describe a
  dev-based flow now; `docs/adr/threat-model.md` also changed on `dev`.

Rows are rewritten only where they were stale: I27, I31, the inventory's I31 introduction,
the census class for `scripts/ci`, and one sentence on the nightly Helm chart in the migration
policy. The other rows are unchanged and still need the child's recheck.

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
