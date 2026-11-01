# Issue 1111 / PR 1113: nightly Helm, Flux and Argo CD deployment

This draft now includes verified nightly Helm publication, a complete manual
singleton Kubernetes path, and both requested GitOps installation recipes.
The owner extended the existing draft with "Fold it in" on 1 November 2026.
The [deployment comparison](../reports/deployment-landscape-2026-10-31.md) remains
an explicitly dated baseline; the [support matrix](../operations/deployment-support.md)
and [GitOps runbook](../operations/kubernetes-gitops.md) describe this candidate.

## Source and authorized endpoint

- Original base: `e9da355b5fd6a6e1c4cee4d6bbb84b26ef51304c`.
- Previously delivered signed head: `523b3b66387d6db0587bd607ebd9cab1a163ea76`.
- Independently reviewed GitOps increment SHA-256, excluding this handoff:
  `b20a7e2626ecbb1aa3b47ddc4bf3fe3fe9a5d49c18b81596eac926a2b3abcfef`.
- Endpoint: signed source delivery to existing draft
  [PR #1113](https://github.com/Hikyo-Org/Hikyo/pull/1113), with local checks,
  independent source reviews and a self-contained delivery report.

The separately frozen 1.0 source is outside these bytes. This work does not
reselect that source, add an unconditional stable-release gate, merge the draft,
publish a release, or authorize a production installation/upgrade. Human
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
the exact nightly workflow, issuer and source SHA. It checks source/version/profile
metadata and the real Helm render before writing any output. Values accept the
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
| Flux/Argo controller campaign | PASS: Flux 2.10.0 / Argo CD 3.5.6, Helm 4.2.3, Kubernetes 1.36.1; eight recorded controller-mechanics checks, 34 unchanged functional input hashes and successful cleanup |
| Standards source review | Initial P2 CI-routing omission repaired; independent exact-increment recheck reports no remaining actionable source findings |
| Spec source review | Initial pass and independent exact-increment recheck report no actionable source findings |
| Hosted CI / protected merge / human security review | Pending on the new pushed head; local source review is not independent human acceptance |
| Published nightly chart / clean-client production signatures | HOLD: this lane has not run the real publisher or proven that selected chart available |
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
git diff 523b3b66387d6db0587bd607ebd9cab1a163ea76 HEAD --binary -- .   ':!docs/handoff/1111-nightly-helm-distribution.md' | sha256sum
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
