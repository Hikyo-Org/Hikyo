# Hikyo: Product Requirements

Synthesized from the positioning decision ([#3](https://github.com/Hikyo-Org/Hikyo/issues/3)), the competitive research ([competitive-landscape.md](../research/competitive-landscape.md)), the license decision ([#9](https://github.com/Hikyo-Org/Hikyo/issues/9)), the MVP boundary ([mvp-boundary.md](../adr/mvp-boundary.md)), and the product register at the repo root ([PRODUCT.md](../../PRODUCT.md)). This document restates locked decisions; it decides nothing. On any divergence, the owning ADR or ticket resolution wins and the contradiction reopens that ticket.

Originally synthesized on 2026-08-06; reconciled with declared amendments on
2026-10-16 for [#41](https://github.com/Hikyo-Org/Hikyo/issues/41). Read each ADR
through its amendment banners, including any review or implementation conditions.
Scope membership, implemented code and release acceptance are separate facts.

## One-liner

> **Fully open-source secrets & config across environments, with validation, explicit per-environment values, and no enterprise tier.**

"Fully open, no enterprise tier" leads; validation and the environment matrix back it. Compose-first is a feature bullet, not the headline. (The original one-liner said "inheritance"; the flat-model ADR ([flat-model.md](../adr/flat-model.md)) superseded inheritance with explicit per-environment values after matrix-prototype trial. The wedge is unchanged; the mechanism wording follows the locked model.)

## Users

1. **Self-hosting developers** (primary; the beachhead): homelab operators, 1–3 orgs, up to ~25 users. Live in terminals and dashboards, administer their own infrastructure, reach the UI from a desk by day and a phone in the server closet by night.
2. **Platform engineers** (secondary; the goal): the architecture carries them (org layer from day one, pluggable datastores, deployment-module seam) while staying thin for the primary user.

## Problem & wedge

The original [competitive survey](../research/competitive-landscape.md) found that every surveyed competitor (Infisical, Phase, Vault/OpenBao, SOPS, ESO) either paywalled the operational core (audit, RBAC, SSO, SCIM, secret scanning, the classic `/ee` lineup) or lacked the environment-matrix + schema-validation combination entirely. No surveyed product combined explicit multi-environment values + schema validation + fully-open licensing. These are the survey's positioning findings, not a current market comparison.

Hikyo's wedge is structural, not promotional:

- **MPL 2.0**: file-level copyleft and DCO underpin the licensing and contribution model. The no-`/ee` boundary is a governance commitment; see [oss-mechanics.md](../adr/oss-mechanics.md) and [the license decision](https://github.com/Hikyo-Org/Hikyo/issues/9).
- **The no-`/ee` pledge** is published governance (GOVERNANCE.md full text + README sentence), amendable only through the locked-decision procedure: audit, rollback, RBAC, SAML, SCIM, secret scanning ship free.
- **Four classic paywall items are promoted into 1.0** as wedge proof: GitHub Actions adapter, SAML SP, SCIM provisioning, secret scanning ([mvp-boundary.md](../adr/mvp-boundary.md) §2).

## Product principles (settled; reopen only on serious contradiction)

- Fully open source; no paid gate on any production-required capability.
- Self-hosting first: single-server Compose and K8s/K3s are the reference deployments; a Raspberry Pi 4 (4 GB, sqlite) is the calibration floor ([ops-spec.md](../adr/ops-spec.md)).
- The **environment matrix** is the signature surface; secrets vs config distinguished throughout; disclosure is a ceremony.
- Deterministic, explainable values: every value is explicit per environment ([flat-model.md](../adr/flat-model.md)); schema validation before delivery ([schema-model.md](../adr/schema-model.md)).
- Multi-tenant logical instance. Configuration follows its declared activation
  lifecycle: ordinary managed settings replace runtime components live, while
  deployment-owned inputs require the supported controlled bootstrap transitions.
  The [self-configuration contract](./self-configuration-full-catalogue.md)
  preserves owner-side authority, HA coordination and stale-consumer fencing;
  independent remotes retain their own configuration and authority.
- Fail fast, fail loud: every bound is a named refusal, never silent truncation.

## Delivery overview

Hikyo presents delivery as a gradient: **fetch-based delivery** under the workload's own identity (`hikyo run` / rendered dotenv on Compose, the K8s operator, CI federation fetching at runtime) is the first-class path; **push adapters**, starting with Forgejo and GitHub Actions, serve destinations whose workflows need standing values before the first fetch. Additional destination classes and their release membership follow the [deployment-adapter amendments](../adr/deployment-adapter.md) and [MVP boundary](../adr/mvp-boundary.md), including each provider's conditions and value-blindness requirements. The gradient sentence is: ***if your job can fetch, federate; push what must exist before the first fetch.***

## Scale envelope (v1 designed-for)

1 logical instance · 1–3 orgs · ≤25 human users · ≤50 projects · ≤10 environments/project · ≤10k secret entries · ≤100 workload clients. The acceptance load-test envelope is 10× each number; concrete bounds are owned by [ops-spec.md](../adr/ops-spec.md).

Single-node deployment remains the default and SQLite remains single-node.
The declared [#146](https://github.com/Hikyo-Org/Hikyo/issues/146) amendment admits
opt-in, single-region application-tier HA with PostgreSQL, shared root authority
and fenced coordination. This does not admit cross-region active-active
operation ([system architecture](../adr/system-architecture.md),
[ops-spec §13](../adr/ops-spec.md)).

## Scope after declared amendments

The [MVP boundary](../adr/mvp-boundary.md) is the complete scope register.
Its original exclusions have been narrowed by declared amendments:

- Secret-change approvals (#151, C-APV) and approval-mediated temporary access
  (#152, C-ACC) join the acceptance inventory. Their permission formulas and
  environment-scoped boundaries remain in [permission-model.md](../adr/permission-model.md).
- Dynamic PostgreSQL credentials (#147) add a bounded lease lifecycle. This is
  the named provider promotion in the MVP register, not a general secrets-engine
  or arbitrary-provider commitment.
- The PKI (#154), SSH user-certificate (#155) and transit (#156) promotions each
  have a bounded contract. Consult [pki.md](../adr/pki.md), the MVP register's SSH
  row and [transit.md](../adr/transit.md), including their implementation
  conditions and explicit non-goals.

Provider additions and other conditional amendments must be read individually.
AWS Secrets Manager synchronization (#158) is conditionally relevant to 1.0
under MVP amendment 8. The [audit classification correction](https://github.com/Hikyo-Org/Hikyo/issues/158#issuecomment-6220607066)
preserves that condition; it does not establish final promotion, provider
acceptance or a review waiver.
An implementation authorization or merged feature does not itself discharge a
required governance review or change an explicit post-1.0 disposition. When a
tracker classification or release summary conflicts with an amendment, follow
the canonicality rule above and reconcile the exact owner decision before
claiming the release inventory is settled. The
[#41 reconciliation handoff](../handoff/41-spec-reconciliation.md) records the
outstanding scope and evidence joins without making new policy.

## Success criteria

- An operator trusts Hikyo enough to manage production secrets in it.
- Every resolved value's origin is understandable without reading docs.
- The 1.0 gate ([mvp-boundary.md](../adr/mvp-boundary.md) §1.2, §6) is green on sqlite **and** postgres, and the [self-hoster checklist](./self-hoster-checklist.md) passes wholesale.
- The release gate [#79](https://github.com/Hikyo-Org/Hikyo/issues/79) establishes
  final-candidate acceptance, API/CLI freeze and signed, verifiable release
  artifacts. Closed implementation tickets alone do not complete #41 or #79.

## Explicitly not in this product's v1

The complete out-of-scope register with per-item dispositions and reopen triggers
is [mvp-boundary.md](../adr/mvp-boundary.md) §4, read through its amendments.
Remaining exclusions include hosted SaaS + billing (the committed follow-on map
after 1.0), cross-region active-active, a plugin marketplace and LDAP. The earlier
blanket exclusions for HA, dynamic credentials, PKI and approval workflows no
longer describe the amended scope; their bounded promotions above do not admit
the adjacent capabilities that their owning ADRs still exclude.
