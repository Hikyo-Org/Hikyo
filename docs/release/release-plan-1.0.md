# Hikyo 1.0 release plan

**Preparation snapshot, 2026-10-16, source
`6769ee55dcd4087eeaf42547e9abde01c4f0818c`. Final candidate acceptance is pending.**
The [acceptance ledger](acceptance-1.0.md) preserves September's executed
results; those results are not certification of this source or the eventual
combined candidate. This plan sequences [#79](https://github.com/Hikyo-Org/Hikyo/issues/79)
without changing its five done-verification gates or authorizing a release.

Evidence preparation and a documentation merge do not authorize stable-policy
activation, tag creation, release publication or production deployment. Those
remain separate owner-authorized ceremonies. Record unexecuted checks as
pending, never passed or waived.

## Source and accepted decisions

- [#936 / PR #937](https://github.com/Hikyo-Org/Hikyo/pull/937) merged as the
  snapshot above. Its [48-job run](https://github.com/Hikyo-Org/Hikyo/actions/runs/38929885843)
  succeeded at that SHA with event `merge_group`. This is exact merge-group
  evidence, not the full-main release proof selected by
  `scripts/release/require-green-main.sh`.
- The [API/CLI source inventory](../spec/api-cli-spellings.md#revision-and-enum-baseline)
  records revision 11 and migration source head 75 on both engines. Inventory
  and artifact identities must be refreshed after outstanding changes land,
  including #805; these are not the final freeze baseline.
- Marc's [accepted error-enum and SOPS/KMS size dispositions](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5353930183)
  stand. Keep KMS support and measure final binary sizes. The
  [floor amendment](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5354009024)
  accepts constrained native ARM64 CI; physical Pi calibration is optional.
- [Online stable signing](../adr/stable-workflow-signing.md) supersedes the
  old offline-primary/USB recommendation. The existing online-generated root
  remains. Its `pending_release` bootstrap placeholder is not stable approval;
  recovery-authorized stable policy activation is still required.
- Scoped review/disposition approvals for #930, #805, #631, #612, #15 and
  the approved #589 / PR #945 head apply only to their recorded scope. They do
  not approve later heads, waive unrelated ADR gates or establish completed
  review. Claude subscription review is unavailable; no paid substitute or
  blanket cross-provider waiver is authorized.

## 1. Assemble one reviewed combined candidate

1. Follow the [current roadmap](https://github.com/Hikyo-Org/Hikyo/issues/527)
   and each owner's acceptance/handoff. Verify mergedAt, merge SHA, tested tree
   and ancestry in main before treating a dependency as delivered. Queue
   admission and a green group alone are not merge proof. Use current protected
   policy and the [CI guide](../development/ci.md), not historical direct-merge
   instructions.
2. Reconcile operative capabilities, remaining defects and scoped governance
   reviews. #589 owns A7's release-scope disposition; #15 owns the capability
   ledger; #527 owns roadmap sequencing. Implemented capability, review approval,
   release scope and candidate acceptance are separate facts. No historical
   post-1.0 list silently excludes functionality now shipped or newly authorized.
3. Select one clean main SHA after required implementation and trust changes.
   Record source/tree, API revision, both-engine migration identities and the
   exact binary/archive hashes, image RepoDigest and chart digest used by each
   proof. Source equality does not imply byte equality. Later changes invalidate
   affected proof and require fresh exact-main validation.
4. Regenerate CLI goldens for the selected candidate, reviewing every
   diff as a surface/spec change. Audit closed/extensible response enums and
   operation minima against the final server revision. Preserve oasdiff negative
   fixtures, generated-client unknown-value handling, operation parity,
   eligibility and complete browser registry closure.
5. Before `v1.0.0`, `check-api-freeze.sh` is explicitly dormant and
   `check-client-skew.sh` executes a pre-freeze rehearsal. After the immutable
   tag, they compare against its spec and unmodified checked-in generated
   client. Report the actual mode; rehearsal does not prove frozen-tag execution.

Keep the current correctness and integration work visible: #930's approved
retention/erasure contract, #934/#935 lease ceiling/TTL fixes, and #941's
PostgreSQL schema rendering must be represented in final recovery and provider
proof. Coordinate #941 with #930's non-UTC PostgreSQL restore path; a UTC-only
fixture is not proof of that repair. #931/#932/#933 and browser administration
under #807/#819/#820 retain their own acceptance obligations. This snapshot
records only #936 as newly merged; it does not predict the other owners' results.

Release membership remains independently governed by accepted amendments.
For example, generic file destinations remain v1.1 under mvp-boundary amendment
9 and Cloudflare amendment 6 expressly leaves the 1.0 criteria unchanged.
Their implementation/review work does not silently promote them into 1.0.
Likewise, preserve #589's scoped A7 decision. Add proof for shipped surfaces
without rewriting the owning release boundary.

Use the [amended capability inventory](acceptance-1.0.md#amended-capability-inventory-for-the-final-candidate)
as part of candidate selection: HA #146, dynamic credentials #147, change
approvals #151, temporary access #152, PKI #154, SSH #155, transit #156 and each
provider's own scope/review conditions. C-ACC was absent from the historical
49-row ledger; its new pending row restores the operative requirement. Signed
upgrade compatibility and Kubernetes condition reporting have designs and
source implementations; final proof and pending repairs remain separate.
AWS #158's conditional In amendment remains governed by its merge/review
conditions. The [audit label correction](https://github.com/Hikyo-Org/Hikyo/issues/158#issuecomment-6220607066)
marks conditional 1.0 relevance and a required decision; it is not promotion or
acceptance. Reconcile the unchanged title/milestone with its owner before final
membership sign-off. Vault KV's proposed governance condition and developer credentials'
implementation-merge condition likewise remain explicit.

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
