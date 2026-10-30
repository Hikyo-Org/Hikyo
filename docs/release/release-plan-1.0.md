# Hikyo 1.0 release plan

**Scope and dependency reconciliation, 2026-10-28, inspected main
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`. Final candidate acceptance is pending.**
The [acceptance ledger](acceptance-1.0.md) preserves September's executed
results. Those results do not certify this source or the eventual combined
candidate. This plan sequences [#79](https://github.com/Hikyo-Org/Hikyo/issues/79)
and [#527](https://github.com/Hikyo-Org/Hikyo/issues/527) without changing the
five done-verification gates or declaring 1.0 accepted.

Source PRs require normal review, signatures, DCO, CI and the protected merge
queue. The approved stabilization work ends at reviewable PRs unless later
merge authority is given. Stable-policy activation, recovery signing, tags,
Apple submissions, publication, production changes and external messages each
retain their separate authorization. Unexecuted evidence stays pending,
HOLD, NOTRUN or SKIPPED as appropriate.

## Source and accepted decisions

- The inspected main above advertises API revision **12** in
  [api/spec.go](../../api/spec.go). The highest migration filename on each
  engine is `00079_remote_pairing.sql`:
  [SQLite](../../internal/store/migrations/sqlite/00079_remote_pairing.sql) and
  [PostgreSQL](../../internal/store/migrations/postgres/00079_remote_pairing.sql).
  These are source identities, not a measured deployed schema, applied migration
  count or final artifact baseline. The older
  [API/CLI inventory](../spec/api-cli-spellings.md#revision-and-enum-baseline)
  is historical; the qualification owner must refresh candidate identities.
- **Accepted 1.0 exclusion:** AWS acceptance [#158](https://github.com/Hikyo-Org/Hikyo/issues/158),
  its [PR #969](https://github.com/Hikyo-Org/Hikyo/pull/969), and extracted
  controller follow-up [#1057](https://github.com/Hikyo-Org/Hikyo/issues/1057)
  are outside this release path. This release-specific instruction supersedes
  the older conditional AWS inclusion in this plan. It does not delete shipped
  adapter code/tests, claim real-AWS acceptance, or waive review of later work.
  #158 now has `release:post-1.0` and milestone v1.1; #1057 is post-1.0.
- Preserve accepted deferrals: #805 publish/pin webhook push, #161 Cloudflare,
  #148 scheduled rotation, and broad #1051 automation migration. Generic files
  and sealed webhook remain v1.1 under their owning amendments. Existing Compose
  pull delivery, internal key rotation and all tests for shipped code remain.
- [PR #1014](https://github.com/Hikyo-Org/Hikyo/pull/1014) merged
  2026-10-20T21:44:09Z as `0de713d2df6acbb9a3e344fbfba4827b31f86501`,
  an ancestor of the inspected main. This satisfies the merge condition recorded
  when [#71](https://github.com/Hikyo-Org/Hikyo/issues/71#issuecomment-6300682362)
  and [#8](https://github.com/Hikyo-Org/Hikyo/issues/8#issuecomment-6300681859)
  were reopened for the pairing amendment. The
  [owner's review exception](https://github.com/Hikyo-Org/Hikyo/pull/1014#issuecomment-6300666440)
  applies only to #1014. Cross-provider review remains SKIPPED, never CLEAN;
  amendment bookkeeping closure is neither a new security review nor final
  candidate acceptance.
- Marc's [error-enum and SOPS/KMS size dispositions](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5353930183)
  stand. Keep KMS support and measure final binary sizes. The
  [floor amendment](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5354009024)
  accepts constrained native ARM64 CI; physical Pi calibration is optional.
- [Online stable signing](../adr/stable-workflow-signing.md) supersedes the
  old offline-primary/USB recommendation. The existing online-generated root
  remains. Its `pending_release` bootstrap placeholder is not stable approval;
  recovery-authorized stable policy activation is still required.
- All other scoped review/disposition approvals retain their recorded limits.
  Claude subscription review is unavailable; no paid substitute or blanket
  cross-provider waiver is authorized. TLS #966, Transit and conditional
  capability governance have separate owners. Their outcomes must be linked
  before final sign-off; this scope reconciliation does not decide them.
  The TLS owner reports #1004 delivered but final exact-contract approval and
  a #966-only review disposition still unlocated. Governance and signed-consumer
  closure stay **HOLD** pending that lane's correction/evidence handoff, rather
  than becoming missing implementation.

## 1. Assemble one reviewed combined candidate

### Approved stabilization and dependency order

This is the assigned stabilization set, in addition to the existing operative
acceptance criteria. An assignment is not implementation, review or acceptance
proof. The [dependency handoff](../handoff/79-527-release-scope-roadmap.md)
records merge evidence and lane coordination; recheck live heads before acting.

| Order / owner | Required result and dependency | State at this snapshot |
| --- | --- | --- |
| Scope: #79 / #527 | Record exclusions, operative capabilities and claims; reconcile #71/#8 merge bookkeeping | This documentation/admin lane; #79 and #527 remain open. |
| CLI: #1033 / [#1042](https://github.com/Hikyo-Org/Hikyo/pull/1042) | Repair real rotation/re-encryption targeting and completion before #1039's operator procedure | Existing draft PR; repair and review it rather than duplicate it. |
| Audit: #1034, #1035 / [#1045](https://github.com/Hikyo-Org/Hikyo/pull/1045), #1036 / [#1044](https://github.com/Hikyo-Org/Hikyo/pull/1044) | Attributable refusals, authorized instance query/export and detectable export completion; start source work in parallel, reconcile shared consumers | #1044/#1045 are drafts; #1037 consumes #1035/#1036, and #1038 consumes #1034/#1035/#1037. |
| Admission: #1040 | Enforce source budgets through actual bearer resolvers with failure and anti-enumeration tests | Open; no implementation PR identified at inspection. |
| Containment: #1039 | Tested scoped manual containment/recovery, after #1033; retain explicit limits of each revocation action | Open; broad automatic containment is not implicitly admitted. |
| Network: #1041 / [#1043](https://github.com/Hikyo-Org/Hikyo/pull/1043) | Define chart boundaries and prove required allowed/denied traffic on an enforcing isolated CNI | Existing draft; deployed production posture remains #1055's separate operational gate. |
| Development dependencies | Patch or explicitly disposition the two medium development-tool alerts #104/#105 before qualification | Tooling owner; alerts are not evidence of a production-server exploit. |
| Governance and trust | TLS #966 and Transit/conditional-capability decisions; public stable-policy packet and separately authorized trust setup/merge | Separate lanes; trust changes must land before final source selection. No inherited #1014 waiver. |
| External acceptance | Prepare authorized isolated GitHub/MCP fixtures, exact clients and disclosure evidence | Separate lane; #651's public-image/two-process/client proof remains required. No provider writes, paid use or notifications authorized here. |
| Qualification and release: #79 | Select final source/artifacts after required merges, run the complete matrix, reconcile external proof, then seek separate ceremony authority | Separate qualification owner. No acceptance, freeze or release declared here. |

### Operational claims and capability boundaries

[#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037) and
[#1038](https://github.com/Hikyo-Org/Hikyo/issues/1038) remain open release
**decisions**. The approved recommendation permits a documented and tested
operator-controlled collection/alert route to satisfy the agreed launch claim;
it does not silently require a native SIEM, hash chain or generalized automatic
containment product. Their full issue acceptance remains until satisfied or
explicitly scoped by the owning decision. No independent retention, complete
collection, intrusion detection or notification delivery claim is accepted
without actual end-to-end evidence and receipts. Choose the claim and obtain
any required audit/governance amendment through the separate owner before
freezing support documentation. A mocked sender or send-success counter is
not a receipt.

[#1055](https://github.com/Hikyo-Org/Hikyo/issues/1055) concerns actual public
deployments and retains its operational authorization boundary. Coordinate any
isolated #651 ingress proof with that owner; do not use an unverified production
installation as the candidate. Source/API revision 12, published artifacts and
older observed deployments are distinct facts. The code release does not
certify every running installation or authorize their upgrade.

Use the [amended capability inventory](acceptance-1.0.md#amended-capability-inventory-for-the-final-candidate)
for HA #146, dynamic credentials #147, change approvals #151, temporary access
#152 including C-ACC, PKI #154, SSH #155, Transit #156, multi-instance #71,
member access rules #15, developer credentials, signed upgrades and Kubernetes
reporting, plus each
provider's own scope/review conditions. Do not restore an obsolete small MVP
inventory. Vault KV destination membership was approved on October 29 subject
to qualifying review and acceptance; its proposed amendment closeout remains
HOLD under the [scoped packet](../handoff/162-vault-1.0-closeout.md). Social
registration's implemented presence does not override its post-1.0 A7 ruling.

Earlier integration is delivered source, with fresh final-candidate proof still
owed. In particular, dynamic ceiling/TTL repairs (#934/#935, PR #938), backup
retention/recovery (#930, PR #952), developer administration (#807, PR #942),
social synthesis/RFC8628 records (PRs #945/#946), and Transit WebUI (#820,
PR #965) have merged and are current-main ancestors. Do not schedule them as
missing implementation. Keep their acceptance assertions, including the
#930/#941 non-UTC PostgreSQL restore interaction. The
[historical October 16 merge-group run](https://github.com/Hikyo-Org/Hikyo/actions/runs/38929885843)
remains evidence for its recorded source/event only.

1. Verify each required merge SHA and ancestry, current protected policy and
   owner's handoff; draft or queue admission alone is not delivery.
2. After required implementation, governance and trust changes land, select one
   clean main source/tree. Record API revision, both-engine migration identities,
   binary/archive hashes, image RepoDigest and chart digest for every proof.
   Source equality does not imply byte equality. Refresh affected evidence after
   later changes.
3. Regenerate CLI goldens for that candidate and review every diff as a
   surface/spec change. Preserve closed/extensible enums, operation minima,
   oasdiff negative fixtures, generated-client handling, parity, eligibility
   and complete browser registry closure.
4. Before `v1.0.0`, `check-api-freeze.sh` is dormant and
   `check-client-skew.sh` is a pre-freeze rehearsal. After the immutable tag,
   they compare against its spec and unmodified checked-in client. Report
   the actual mode; rehearsal does not prove frozen-tag execution.

## 2. Run the complete acceptance matrix

Use the current workflow files and inputs from the repository root with
maintainer GitHub access and the pinned tools in CONTRIBUTING/the CI guide.
Workflow requests do not establish success: retain actual SHA, event, run,
attempt, all required job outcomes, artifacts, failed attempts and skips.

| Proof | Current entry point | Required boundary |
| --- | --- | --- |
| Full main | `ci-main.yml`, manual on `main`, selecting its exact SHA | `require-green-main.sh` accepts the latest qualifying exact-SHA `ci-main.yml` or legacy `ci.yml` run. PR feedback, queue admission and merge-group success do not substitute. |
| Complete graph | `ci.yml` and `scripts/ci/ci-job-registry.json` | All 16 full domains: client, compose-demo, docs, floor-bench, freeze-guard, fuzz, generated, headline-guarantee, k8s-e2e, lint, race, release-snapshot, supply-chain-checks, test, web, web-go; inspect supporting jobs and all shards. |
| Full isolation race | `reliability.yml`, `suite=race` | All planned isolation-race shards and both engines. Ordinary race coverage excludes `internal/isolation`. |
| Backup/restore and operator | `release-acceptance.yml`, `suite=all` | Exact-candidate native ARM64 recovery and operator artifacts. Use the consolidated workflow, not retired standalone workflow names. |
| Operations floor | `ops-floor.yml`, manual | Native ARM64 and AMD64 diagnostics; authenticated Compose/chart doctor results remain separate evidence. |
| Resource bounds | Required `floor-bench.yml` lane | Four CPUs, 4 GiB, zero swap; committed CPU factors 4.0 and memory 1.0 unless separately calibrated; exact executable hashes, unchanged workload and thresholds. |
| Browser/client/self-hoster | Full graph plus the [candidate checklist](self-hoster-candidate.md) | Both engines, claimed viewport/state coverage, actual packaged journeys and honest rehearsal/frozen mode. |
| Providers | Dedicated GitHub contract, real Forgejo proof, relevant GitLab reliability lane | Actual provider behavior; stubs and skipped opt-in suites are not live-provider proof. |
| Security and docs | Full graph, applicable `reliability.yml` suites, published docs checks | Artifact/security results, O4-O6, current real disclosure receipts and public-site evidence stay distinct. |

Before any full web test run, reserve Go build capacity or supply a compatible,
source-matched `HIKYO_E2E_OIDC_BINARY`. The
[`instance-idp` fixture](../../web/e2e/fixtures/instance-idp.test.ts) runs a Go
build at module load when that setting is absent, including from a Node/Vitest
invocation. Follow the other documented binary/tool prerequisites too and
include this compilation in the run's resource and toolchain requirements.

The eight [ops-spec section 15](../adr/ops-spec.md#15-ci-enforced-invariants-added-by-this-spec)
checks remain: Argon2id floor, password-list hash, bounded metrics labels,
air-gap service, pinned JSON Schema subset, engine durability, keyword allowlist
and composite ARG_MAX preflight. Keep every acceptance criterion and all three
CI/E2E/UI classes. The ledger's inventory test checks references and its recognized row IDs,
not behavior or release acceptance. Named C-criteria, including C-ACC, remain
constrained to their owning ADR inventory.

For the floor recovery lane, independently supply the disposable test secrets
`FLOOR_BACKUP_CUSTODY` and `FLOOR_ROOT_CUSTODY`. Follow
[floor acceptance](floor-acceptance.md); do not substitute production custody
or generate fallback secrets. Preserve backup/destroy/restore/reconcile,
readable values, credential refusal/re-establishment, effective limits and RTO.
This approved CI arrangement does not establish independent production escrow.
Operator process peak RSS and cgroup memory charge are different measurements.

## 3. Complete external and self-hoster evidence

- **Apple distribution:** configure the Developer ID Application identity and
  team notarization API key using the [Apple signing guide](apple-signing.md).
  Require accepted notarization, stapled and verified disk images for both
  architectures, and a quarantined download plus Homebrew install/upgrade on
  separate Apple Silicon and Intel Macs. Enrollment and local fixtures do not
  establish publisher signing or clean-Mac installation acceptance.
- **GitHub:** `TestGitHubDotComContract` requires a dedicated fixture repository,
  organization and protected unattended environment, three minimum-permission
  tokens, three one-permission-less tokens and a distinct harness token.
  `loadGitHubContractConfig` defines the complete configuration and rejects
  token reuse. Missing inputs skip loudly. Obtain scoped authorization/custody
  before this destructive opt-in test; record real API and workflow-consumption
  evidence for M4/GH-E2E/GH-CONTRACT without values or credential dumps.
- **MCP (#651):** share one candidate with that owner; do not create duplicate
  deployments. Bind source, immutable RepoDigest, deployment revision, verified
  public HTTPS, UTC time and exact client versions. Require supported synthetic
  provisioning, two actual PostgreSQL-backed server processes, cursor continuity,
  credential/grant revocation and restoration, cancellation/drain/shared claims,
  canary exclusion and each advertised named client's actual authenticated tool
  result. Distinguish direct TLS cancellation from proxy streaming behavior.
  Record Go/Inspector/Codex and any claimed Claude/Responses profiles separately.
  Unknown or failed mandatory profiles remain open; any narrower release claim
  requires explicit scoped release/ADR disposition. #631's no-OAuth choice is
  not that disposition. No paid fallback or protocol/TLS weakening.
- **Self-hoster:** reassert every operative capability through released
  open-source artifacts and documented public interfaces. Exercise native,
  Compose and chart installation, setup/recovery, import, workload delivery,
  upgrade and backup/restore using the actual selected artifact identities.
  An unsigned development snapshot does not establish official availability.
- **Disclosure/docs:** verify the built site and public endpoints with the
  current docs workflow, `check-oss-policy.sh` and `check-docs-live.sh` as
  documented in the CI guide. Obtain actual quarterly PVR/fallback notification
  receipts through authorized private channels. The recorded fallback receipt
  is dated 2026-08-13T07:09:54Z; its 93-day freshness limit is
  2026-11-14T07:09:54Z. A GET/MX check is not a sent/acknowledged report.
  Do not send external messages as an implicit side effect of this plan.

## 4. Prepare the public trust proposal before human authorization

Follow the [online guide](online-signing.md) and use a clean canonical main
checkout, the pinned Go/Cosign/GitHub CLI/jq toolchain, configured Git signing
and maintainer GitHub access. Confirm that `origin` names the canonical release
repository before using ceremony helpers, which compare against `origin/main`.
Do not access private keys while preparing the public review packet.

The source command
`go run ./scripts/release/stable policy --trust release/trust --out PROPOSAL`
creates an unsigned public proposal at a new output file. Replace `PROPOSAL`
with a new path in a designated external release-state directory. It authenticates
existing public trust first; it neither recovery-signs policy nor authorizes a
release. Review repository/owner numeric IDs, workflow `.github/workflows/release.yml`,
`refs/tags/v*`, runner, issuer, Fulcio/Rekor/SCT/checkpoint constraints, sequence
floor, primary inventory, nightly policy and bridge bindings. Compare the
trusted-root hash with the authenticated nightly root. Keep the reviewed bytes
and source identity for comparison with the later setup output.

The public trust PR is limited to `stable-policy.json`,
`stable-policy.sigstore.json` and `stable-trusted-root.json` under
`release/trust/`; preserve existing root, metadata/catalog and historical
inventory. The unsigned proposal alone cannot produce the signature file.
Prepare custody-recovery/backup instructions without reading key material.
Independent encrypted backup and a harmless restore/sign/verify drill remain
operator evidence. Do not describe online-generated custody as offline or
hardware isolated.

Only after the packet is reviewable and explicit recovery authorization is
provided, run `scripts/release/ceremony.sh setup`. It displays the actual policy
and requires `authorize stable release workflow` before invoking recovery custody
and opening the verified signed+DCO trust PR. Compare it with the reviewed
proposal, review/merge that PR through normal gates, then authenticate main with
`ceremony.sh status`. Any resulting main change precedes selection/revalidation
of the final candidate. Neither activation nor status is a release.

## 5. Separately authorized tag, signed draft and publication

These are future owner ceremonies, not commands executed or authorized by this
documentation PR. The first stable tag is the API/CLI freeze act; prerequisites
must be complete before cutting it, even though publication follows draft review.

1. From clean current canonical main, verify stable policy, synchronized trust,
   exact-main full CI and the complete candidate evidence packet. Use
   `scripts/release/ceremony.sh build 1.0.0` only with explicit release approval.
   It confirms version/commit and pushes a signed immutable tag. A prerelease
   tag does not freeze the surface.
2. `release-build` validates, builds and signs the complete draft, including
   OCI image/chart, manifest, metadata/catalog, SBOM and first-party provenance.
   Record six-platform archives/verifiers, eight native packages, compatibility
   declaration, installer, checksum and OCI subjects. Verify closed inventory,
   exact hashes, identity, revocation/rollback rules and actual OCI signatures.
   A signed draft is not a public release; provenance is not reproducibility.
3. `scripts/release/ceremony.sh review 1.0.0` verifies the downloaded draft and
   prints the manifest SHA-256. Inspect source, notes and final acceptance.
   Keep that exact reviewed hash; valid signatures do not prove an honest build.
4. Only with publication approval, run
   `scripts/release/ceremony.sh publish 1.0.0 REVIEWED_MANIFEST_SHA256`.
   It confirms version/hash, verifies again, checks immutable-release policy
   and inventory, publishes and verifies public downloads/OCI subjects.
   Changed bytes or failed verification are failures, not permission to rebuild
   under the same version. Never bypass the ceremony's confirmation or trust.
5. Run `ceremony.sh sync-trust 1.0.0` after verified publication and review/merge
   its signed metadata/catalog PR before the next build. Homebrew is a separate
   protected downstream proposal from verified public artifacts.

## 6. Record completion at its actual boundary

The September 7 [decision log](../reports/1.0/release-run-2026-09-07.html) and
[handoff](../handoff/79-release-run-2026-09-07.md) remain historical evidence,
including the recorded September 8 advisory publication. Recheck advisory/CVE
state for final notes rather than copying a historical pending-assignment claim.
Retain accepted SOPS/KMS support and report measured final artifact sizes.

Only verified public publication permits the capability owner to mark
`CAP-PUBLIC-RELEASE` implemented and the release owner to fill the ledger's
official release/freeze identities. Coordinate #527's roadmap and #15's ledger
rather than overwriting their work. Reassert the authenticated nightly-to-stable
upgrade bridge using actual packaged bytes where claimed.

**Keep #79 open until its release checkbox is fulfilled.** An implementation
or documentation PR uses `Refs #79`, not an automatic closing keyword. Do not
close or modify parent #41 under this task. No unresolved evidence becomes a
pass because the preparatory PR merged.
