# Issue 1111 / PR 1113: nightly Helm, Flux and Argo CD deployment

This draft now includes verified nightly Helm publication, a complete manual
singleton Kubernetes path, and both requested GitOps installation recipes.
The owner extended the existing draft with "Fold it in" on 1 November 2026.
The [deployment comparison](../reports/deployment-landscape-2026-10-31.md) remains
an explicitly dated baseline; the [support matrix](../operations/deployment-support.md)
and [GitOps runbook](../operations/kubernetes-gitops.md) describe this candidate.

## Source and authorized endpoint

- Original base: `e9da355b5fd6a6e1c4cee4d6bbb84b26ef51304c`.
- Current base: `b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`, merged in
  `4d7925a6a` after stable 1.0.0 published.
- Previously delivered signed head: `523b3b66387d6db0587bd607ebd9cab1a163ea76`.
- Independently reviewed GitOps increment SHA-256, excluding this handoff:
  `b20a7e2626ecbb1aa3b47ddc4bf3fe3fe9a5d49c18b81596eac926a2b3abcfef`.
- Endpoint: signed source delivery to existing draft
  [PR #1113](https://github.com/Hikyo-Org/Hikyo/pull/1113), with local checks,
  independent source reviews and a self-contained delivery report.

Stable 1.0.0 published from `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334` while this
draft was open, and its chart excludes this work. The owner chose to add stable
support here: the generator now accepts stable charts and refuses 1.0.0 for the
reasons recorded below. This work does not add an unconditional stable-release gate, merge the draft, publish a release, or
authorize a production installation/upgrade. Human
maintainer/adversarial security review remains required under CONTRIBUTING.

## Resulting behavior

The nightly publisher binds chart version/appVersion, full source SHA and the
verified immutable image. It renders the chart, compares packaged source bytes
on immutable readback, signs only a fresh source-owned upload, refuses unsigned
or conflicting existing artifacts, and checks anonymous exact-version access.
Optional nightly Kyverno policy rendering binds its source SHA; live enforcement
is separate. The existing staged root-file path is exposed to local admin
commands, and the Kind missing-root fixture removes both flag and environment
sources. Original trust, signing and runtime admission boundaries are retained.

The GitOps generator verifies the selected chart and its matching image against
the workflow identity for that version, issuer and source SHA. It checks
version and channel or profile metadata, source annotations on a nightly, and the
real Helm render before writing any output. Values accept the
manual singleton surface with existing Secret references, independent claims and
native TLS. Image overrides, secret-value fields, HA, delivery/configuration
operators, unattended enrollment and cluster-wide admission installation refuse.
The generator performs no Kubernetes operations and does not overwrite a prior
reviewed output file.

Flux uses an exact-digest OCIRepository, a suspended HelmRelease and a namespaced
impersonation Role. It repeats keyless issuer/subject verification in-cluster,
skips delivery CRDs and refuses silent resource adoption. Explicit zero-retry
remediation policies avoid automatic uninstall/rollback or changed controller
defaults. Drift correction is disabled. The shared chart label helper now
normalizes Flux's OCI `+digest` version metadata into a valid Kubernetes label.

Argo uses a generic OCI source pinned by digest, an Application and a restricted
AppProject. Automatic sync, self-healing, retries and pruning are disabled. The
Application has no cascading-deletion finalizer. Argo renders the chart directly;
this path does not create a Helm release or install a Cosign verification plugin.
The operator retains the generator's independent verification result before sync.

Both use manual custody and the [complete singleton prerequisites](../operations/kubernetes-singleton.md).
The database, root/TLS/database Secrets, custody claims and public L4 Service stay
operator-owned. For populated release changes the operator freezes parent
reconciliation, waits for current controller work to finish, stops every writer,
proves an encrypted export and same-engine readable restore, runs one verified
migrator against the same installed custody, and explicitly activates the exact
target. Failed/uncertain writes stay in maintenance; reverting Git to an old
image is not recovery. No migration hook acquires private recovery authority.

The separate unattended local-operator resolver remains
[#1112](https://github.com/Hikyo-Org/Hikyo/issues/1112). The broader automated
maintenance initiative remains [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).
This draft does not implement either by changing an installation flag.

## Verification for this increment

| Evidence | Result and practical boundary |
| --- | --- |
| GitOps generator and policy tests | PASS: 8 tests with real Helm 4.2.3 packaging/rendering, verification-boundary refusals, explicit controller policies and Flux label regression |
| Structured chart owner checks | PASS: retained security/RBAC/probe/topology/custody assertions |
| Chart mutation consumers | PASS: valid chart accepted; all retained security/network/bootstrap mutations refused |
| Nightly publisher/release fixtures | PASS: retained source/image/version, retry/refusal and anonymous-client fixture checks; no real upload |
| Changed-path routing | PASS: GitOps-only edits select their existing supply-chain owner suite, lint and Kubernetes consumers |
| Docs | PASS: Astro check, 48 files, zero errors/warnings/hints; static build, 80 pages including Kubernetes GitOps; content preparation tests |
| Shared fast feedback | PASS: selected shell/workflow/docs checks; Go/shell/workflow/docs feedback completed in 21 seconds |
| Frozen CI coverage registration | PASS: unchanged baseline accepted; 13 narrowing/fail-open/ordering mutations refused; historical hashes retained |
| CI-owner package | PASS: complete `scripts/ci` suite, two Go workers, final rerun 397.016 seconds |
| Flux/Argo controller campaign | PASS on nightly-shaped fixtures, most recently at `8143ff680`; later commits change only the CLI's published-release check and refusal text, not `manifests()`: Flux 2.10.0 / Argo CD 3.5.6, Helm 4.2.3, Kubernetes 1.36.1; eight recorded controller-mechanics checks, 34 functional input hashes matching that revision and successful cleanup |
| Standards source review | Initial P2 CI-routing omission repaired; independent exact-increment recheck reports no remaining actionable source findings |
| Spec source review | Initial pass and independent exact-increment recheck report no actionable source findings |
| Post-merge focused checks at `dedea8424`; generator tests and fast feedback rerun after doc repairs | PASS with Helm 4.2.3: generator (9 tests), nightly chart, chart owner and mutation checks, publisher fixtures, classifier, and exact coverage-registration Go tests |
| Full CI-owner suite and Astro check/build after merge | NOTRUN locally: the shared job queue was paused and host memory was under pressure. Hosted repository invariants own the full suite; candidate CI owns the site build and OSS policy |
| Post-merge Standards and Spec review of `88484ec94`, rechecked at `ac19a140a` | Isolated reviews found no hard violations and confirmed the artifact claims live; stable routing and citation gaps repaired. No complete stable Kubernetes first-install runbook exists, and first-administrator on the 1.0.0 chart is unexercised. Historical; see the homelab row below |
| Stable and nightly tag coexistence | PASS with Helm 3.16.1 and a disposable registry: unpinned and `>=1.0.0` pulls resolved to `1.0.0`, not a nightly. Helm 4 was not exercised |
| Hosted CI / protected merge / human security review | Pending on the new pushed head; local source review is not independent human acceptance |
| Published nightly chart / clean-client production signatures | HOLD: this lane has not run the real publisher. On 2 November 2026 the anonymous registry listed only stable `1.0.0` |
| Stable chart generator | PASS with Helm 4.2.3 and 3.16.1 fixtures, which fake the digest pull: stable `X.Y.Z` (major 1 or higher) binds `release.yml@refs/tags/vX.Y.Z` for Cosign and the Flux subject, plus the `stable` channel. A chart without `HIKYO_ROOT_KEY_FILE` is refused with no output for both providers. Mutating the root-key check, identity, subject or channel check fails tests |
| Signed draft chart | ENFORCED, owner decision: the tag job signs before draft review, so the generator requires `gh` to report a published, non-prerelease release before any trust check on a stable chart. Tests cover draft, prerelease and wrong-tag refusals. The check needs an authenticated `gh` |
| Stable generator review and local checks at `8143ff680` | Isolated Standards and Spec reviews, then an exact-increment recheck, found no High or Critical issues; their findings are repaired. Generator suite (13 tests at the following test-only commit) passes on Helm 4.2.3 and 3.16.1, and fast feedback passes. Astro check/build and the full CI-owner suite were not rerun locally; hosted CI owns them |
| Real published 1.0.0 through the generator | REFUSED as designed with Helm 4.2.3 and Cosign: chart and image signatures verified against `2ac7bb36`, then the render was refused on the missing root-key setting, for Flux and Argo, with no output |
| Homelab cluster, namespace `hikyo-gitops-test`, Kubernetes v1.36.2+k3s1, helm-controller v1.6.2, source-controller v1.9.2 (owner-requested test; not the qualified Flux 2.10.0) | Real 1.0.0 chart installed with Helm 3.16.1, with the released image, an authenticated release bundle and PostgreSQL over verified TLS: the server became ready, and `admin create` in the pod failed with `crypto: no root key configured`. Released package re-pushed to an in-namespace registry, which changed its OCI digest: Flux failed on `metadata.labels` value `hikyo-1.0.0+2c631cfb4f5a` and created no Deployment. Unsigned chart packaged from this source as `1.0.1`, with the released 1.0.0 image, on the same database and custody: nothing installed while suspended, Flux installed it after approval, label `hikyo-1.0.1_581fd75d0112`, `admin create` in the pod succeeded, and the administrator persisted across pod replacement. Namespace and its volumes removed. `hikyo-dbugit` is outside the driver's scope, and its pods were running in a check after the test |
| What the homelab test does not prove | The test chart has no signature, Flux verification was removed, and it came from a plain-HTTP in-namespace registry. It was not a fresh install: phase 2's Helm-installed 1.0.0 chart had already initialized the database and custody. `admin create` used `--output-file`, not the runbook's terminal disclosure. `check_published` and the generator CLI did not run against it. No published stable chart exists that passes, the CLI login and browser steps did not run, and Argo was not exercised on stable |
| Final local checks | Generator suite (14 tests) passes on Helm 4.2.3 and 3.16.1 at `135e0f6e6`. Real generator with `gh` and Helm 4.2.3, run at `37fdb8dbb` and reproduced at `135e0f6e6` by the independent reviewer: 1.0.0 passes the published check, then is refused on the root key for Flux and Argo with no output; unreleased `v9.9.9` is refused at the published check |
| Homelab Flux signature verification | NOT RUN THERE: pods cannot reach Sigstore or GHCR because search domain `local.went.io` has a wildcard DNS record, so those names resolve to Cloudflare and fail the TLS handshake. Trailing-dot lookups succeed. Diagnosed from a test pod's lookups, TLS failures and trailing-dot successes. Pre-existing cluster condition, left unchanged and reported to the owner. Flux 2.10.0 verified the real signature in Kind |
| Real Flux 2.10.0 on real 1.0.0, Kind, zero replicas | Resources came from `manifests()` directly, bypassing the generator's refusal, because this probe only observes the controller. Signature verified with that release subject and the exact digest stored. Install then failed on `metadata.labels` value `hikyo-1.0.0+514d368ba7c9`; no Deployment was created. Argo was not exercised on stable, and no published stable chart has completed a controller install |
| Actual Hikyo boot/admin, CSI/CNI/remount, populated migration and restore | NOTRUN by the GitOps campaign; retain separate runtime acceptance |

Review rechecks used OpenAI `gpt-6.1-sol` at high reasoning and inspected the
exact increment hash above. They performed source review, not independent
controller execution. Optional cross-provider review was not requested.

The live campaign installs the actual chart at **zero replicas**, checks unsigned
Flux refusal and digest selection, holds target changes until explicit approval,
retains a failed Flux candidate without remediation, and exercises Argo manual
sync/failure/deletion semantics. Its local unsigned HTTP registry and unusable
image digests are isolated fixtures, not production trust bypasses. It creates
its own kubeconfig, cluster and registry, runs the controllers sequentially and
finalizes Flux resources before deleting controller/RBAC dependencies. Functional
input hashes and actual controller image IDs are recorded; a success receipt is
written only after owned resource cleanup succeeds. This is controller-mechanics
proof, not application/database or production-signature acceptance.

## Reproduce and continue

Use repository-pinned Go/Node tooling, Helm 4.2.3, ShellCheck 0.11.0, Python 3.11+
with PyYAML 6.0.3, and the separate Kind/Docker prerequisites for the live campaign.
From the repository root:

```sh
set -eu
python3 -B -m unittest discover -s scripts/deploy -p 'test_*.py'
scripts/ci/check-chart.sh
scripts/ci/check-chart_test.sh
scripts/ci/check-nightly-release_test.sh
scripts/ci/classify-changed-paths_test.sh
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
GOMAXPROCS=2 go test -p 2 ./scripts/ci -count=1
pnpm --dir docs/site install --frozen-lockfile
pnpm --dir docs/site run check
pnpm --dir docs/site run build
node --test docs/site/scripts/test-prepare-content.mjs
python3 -B scripts/ci/check_gitops_kind.py --receipt gitops-controller-receipt.json
```

The GitOps increment hash is reproducible in Bash or Zsh with:

```sh
set -o pipefail
git diff 523b3b66387d6db0587bd607ebd9cab1a163ea76 8b49fa4feef99689edc40d5ae03294f41ddc22d2 --binary -- .   ':!docs/handoff/1111-nightly-helm-distribution.md' | sha256sum
```

Require exact-head human/workflow security review and the normal protected merge
proof before merging. After a future main nightly publishes, require its actual
chart/image digest signatures and clean anonymous pull before advertising that
artifact. Package access/visibility repair, if needed, belongs to the exact
package owner rather than a blanket organization policy change. Follow the
[GitOps runbook](../operations/kubernetes-gitops.md) for a separately authorized
real installation and retain its storage, network, first-admin, restart and
recovery evidence. No production action is supplied by this handoff.

Task-owned controller clusters/registries and scratch tools are disposable;
required source reviews, logs and the local offline HTML companion are retained
as chat deliverables. No HTML report is committed.
