# #79 / #527: actual 1.0.0 publication record closeout

## Result and ownership

Hikyo 1.0.0 is actually published. This is the original scope/roadmap lane's
post-publication record closeout, not a new preparation or release attempt.
The owner authorized exact publication, subsequently requested the editable
product-oriented release-copy rewrite, and authorized this bounded source
reconciliation through independent Standards/Spec review and protected merge.

This change owns current publication/freeze fields in the release plan,
acceptance scope, #527 and `CAP-PUBLIC-RELEASE` plus its generated views. It
preserves historical ledgers/handoffs, criterion IDs, result levels and future
issue targets. It does not edit `release/trust`, coordinator comment 6501530997
or the release controller's state. No tag, artifact, signature, provider,
credential, external resource or deployment action is performed by this closeout.

## Actual publication and freeze identities

| Identity | Verified record |
| --- | --- |
| Public release | [v1.0.0](https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.0.0), GitHub release 430295277 |
| Publication | 2026-11-01T06:05:47Z; public, latest, immutable, not draft or prerelease |
| Frozen artifact source | `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334` |
| Signed annotated tag object | `4e716bfd43bf82350dbd6fc5e3abbdc52407c41e`; GitHub Verified, reason valid, pointing to that source |
| Manifest SHA-256 | `038fabba9529eb9880aa86002ecc2102d2c4f0c0fc75393da9c928f6accf6094` |
| Immutable asset inventory | 80 asset identities, 906,021,764 total bytes, retained and unchanged |
| Image subject | `ghcr.io/hikyo-org/hikyo@sha256:ef518761b1d64bfe914c660dc14a53c4a9ccd2c6eb7082efaa448a55810d0dc7` |
| Chart subject | `ghcr.io/hikyo-org/charts/hikyo@sha256:514d368ba7c9f50c1c7c832ca4a152363ff8ac2984f320846c0ff60f67190f70` |
| Canonical publisher result | Exit 0 at 2026-11-01T06:14:50.326261 UTC |
| Canonical terminal receipt SHA-256 | `6285ebf6469862113aea99080a1eb64b75dfb278d4a1a5d9ffe2ea4b26defdbc` |

The original release owner retains the complete terminal receipt and associated
execution, API and public-readback receipts in private custody. Their machine
locations are deliberately not published here. This lane read the supplied
terminal receipt and verified its exact hash, rather than inventing a portable
custody path or rebuilding the candidate.

All three complete canonical verification identities match `stable/v1`,
version 1.0.0, sequence 1, frozen source above, manifest above, and compatibility
SHA-256 `d4199f271f37df10cb6ad240dd0d081e05ea3ae79131051e233ad0bde1b4a712`.
Each records `published_oci_verified=true`. The original publication evidence
records image/chart Cosign verification before and after publication and
complete retained-asset verification. This lane does not relabel those checks
as a new provider, native-consumer or specialist acceptance campaign.

Independent read-only observation here confirms release API ID, tag target,
publication time, immutable/public/non-prerelease state, all 80 asset IDs/sizes
and total bytes. A credential-free download of the
[public manifest](https://github.com/Hikyo-Org/Hikyo/releases/download/v1.0.0/release-manifest.json)
hashes to the recorded manifest and declares source `2ac7bb36`, version 1.0.0
and stable signing identity. Latest-release readback is checked separately.
The tag establishes the API/CLI compatibility baseline; it does not turn a
pre-freeze rehearsal into a newly executed frozen-client test.

## Editable notes are separate from immutable bytes

| Release-copy record | SHA-256 |
| --- | --- |
| Original publication-approved notes, historically preserved | `b74438ffd9e492f45f6695531e28541aac3d7112b639dbe4260d4eaa756bc309` |
| Current owner-requested editable product copy | `170fdb699837c4c69ac3acbac8e53440df6aa889c65ff631d2e4ba626f159ae2` |

The live release body matches the second hash. The original approved copy is
retained in the original publication custody, along with its original
qualification/follow-up links. The copy rewrite preserves every such link;
source, signed tag, all assets and signatures did not change. This source PR
does not rewrite release notes again or substitute a body hash for a manifest.

## Bounded software claim and preserved qualification levels

The current claim is an immutable stable software distribution, with the
explicit qualification limits retained in its public notes. It is not an
all-platform, all-provider, deployed-installation or all-green MVP certificate.
`CAP-PUBLIC-RELEASE` therefore remains **partial**: its implemented text records
actual publication/freeze, and its remaining text records outstanding wider
normative acceptance instead of incorrectly asking to publish 1.0 again.

The exact-source qualification owner records full-main
[run 41250723965, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/41250723965)
as 47/47 successful jobs at `2ac7bb36`, all 16 domains, and the separate
`require-green-main` selector PASS. The source terminal receipt SHA-256 is
`d758cb1f2a9b9e406b49bc9c75cfa1608377dd18178ae58e79dc4cbc2944378b`.
Its original 139 mapped references retain **53 root PASS, two root SKIP and
84 owner-job-success-only** levels. No individual or universal both-engine
PASS is invented for the last group. Existing browser/race skips, source versus
development bytes and physical-Pi/derating limits remain. The source-qualification
collection lacked separately uploaded standalone native-package bytes and raw
doctor JSON; the later published packages are available, but publication does
not invent those earlier artifacts or packaged execution results. The wider G10 checkbox is not promoted
by source qualification or this publication record.

September's criterion rows and evidence remain byte-for-byte historical.
Publication/freeze is recorded separately, rather than changing every old
FREEZE-PENDING/CEREMONY-PENDING row to PASS or resetting historical internal
successes. Remaining work retains its existing issue, owner and target:

| Existing follow-up | Retained boundary after publication |
| --- | --- |
| Native distribution owner | Hosted Apple Accepted/stapled/signature/Gatekeeper/mounted-content checks and the recorded quarantined ARM64 version/help/install check retain their actual scope. Separate clean-Mac, Intel-native and Homebrew install/upgrade acceptance is unperformed. No new Apple submission is requested. |
| [#1116 GitHub](https://github.com/Hikyo-Org/Hikyo/issues/1116) | The partial real fine-grained contract is HOLD; publication does not supply missing provider/workflow-consumption proof or authorize a retry. |
| [#1119 GitLab](https://github.com/Hikyo-Org/Hikyo/issues/1119) | Disposable CE qualification stays SKIPPED_OWNER_INSTRUCTION / NOTDISPATCHED; no fixture was created or substituted. |
| [#651 MCP](https://github.com/Hikyo-Org/Hikyo/issues/651) | Named public-HTTPS client and deployed two-process evidence remains open. Go SDK 1.8.0, Inspector 2.11.0 and Codex CLI 0.160.1 remain the scoped profiles; Claude/Responses are not verified. Source/conformance results are not public-profile acceptance. |
| [#1037 collection](https://github.com/Hikyo-Org/Hikyo/issues/1037), [#1038 alerts](https://github.com/Hikyo-Org/Hikyo/issues/1038) | Source delivery does not establish the named independent destination, custody, monitor, actual collection or notification/acknowledgment exercise. Operational criteria stay NOTRUN/HOLD. |
| [#1055 installation](https://github.com/Hikyo-Org/Hikyo/issues/1055) | Actual per-installation provenance, ingress/authentication, network and upgrade/retirement decisions remain with the operator. Publication does not certify or authorize production changes. |
| [#1117 disclosure](https://github.com/Hikyo-Org/Hikyo/issues/1117) | Preserve owner-reported successful tests and recovered fallback evidence at their recorded levels. Unrecovered original PVR/external acknowledgment receipts are not independently recovered PASS. |
| [#1118 tooling](https://github.com/Hikyo-Org/Hikyo/issues/1118), [#1112 container-local operator](https://github.com/Hikyo-Org/Hikyo/issues/1112) | Existing dispositions and future targets remain, without calling unpatched tooling fixed or asserting unsupported in-container commands. |
| Signed trust synchronization | The release coordinator owns the separate exact-four-file signed trust-sync PR, watch, merge and authenticated readback. Await its actual merge evidence; this lane changes no trust files and does not duplicate that work. |

Vault membership and operative source closeout follow their accepted owning
records. AWS #158 / PR #969 / #1057 and the other accepted deferrals are not
silently promoted by publication. These residuals are not newly invented
prerequisites for a release already public. #79 and #41 stay OPEN, and no
all-green MVP checkbox or source ticket is blanket-closed here.

## Review, validation and next dependency

The source baseline for this closeout is main
`de0d8866d050c6e729bde76e2960eb6a260f1e3a`, which is distinct from frozen
artifact source `2ac7bb36`. This documentation merge must not be used as a new
artifact binding. The final PR records the exact candidate head/tree, full
range, local results, independent adversarial Standards/Spec source reviews,
current-head CI and protected merge-group readback separately.

The solo-maintainer amendment in
[OSS mechanics](../adr/oss-mechanics.md#contribution-model) permits maintainer-authored
changes to merge on green without inventing an independent human approval.
It does not label the author self-review independent. Fresh delegated
Standards/Spec source reviews use the existing GPT-6.1 Sol/high choice; no
cross-provider or paid fallback is required or claimed.

Relevant local checks are the shared complete-diff feedback driver, status
regeneration/check and contradiction fixtures, documentation preparation/build
consumers, and the existing release acceptance inventory test. Go uses `-p 2`
and at most two test workers directly, with no overlapping heavy local campaign.
No full-main/specialist qualification, provider or resource action is replayed.
The offline HTML companion and sanitized receipt extracts are ignored local
review artifacts, never committed.

Local validation completed for this bounded documentation change:

- Complete-diff `fast-feedback.sh --working-tree --plan` selected documentation
  only; the selected whitespace and read-only preparation checks passed.
- `node scripts/ci/check-doc-status.mjs --write` regenerated only the authority's
  two projections; `--check` verified all 35 entries. Existing contradiction,
  blocking-obligation, missing-evidence and immutable-document fixtures passed.
- `pnpm --dir docs/site run check` reported zero errors, warnings and hints;
  `pnpm --dir docs/site run build` built 79 pages. Built-site OSS policy and
  the preparation-consumer fixtures passed. This is local build evidence,
  not a claim that the public docs deployment is already updated.
- `GOMAXPROCS=2 go test -p 2 -parallel 2 ./api -run
  '^TestReleaseAcceptanceLedgerReferences$' -count=1` passed. This verifies
  inventory/test references, not execution of every criterion.
- Narrow-change assertions preserved every other status entry, all historical
  criterion bytes/IDs and pre-existing handoffs, and checked portable references.
  Publication/API/tag/manifest/notes/inventory checks above are read-only.

The concrete next dependency is the coordinator's signed trust-sync merge and
readback, plus the existing owners' remaining qualification/operational targets.
Do not close #79 or #41 from this PR or its prose.
