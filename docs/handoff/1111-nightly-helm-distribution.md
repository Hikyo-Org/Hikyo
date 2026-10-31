# Issue 1111: nightly Helm distribution and singleton installation

The first approved deployment-readiness slice adds a signed, retry-safe nightly Helm publisher and a complete manual-custody singleton PostgreSQL installation path. It also commits the [source-cited deployment comparison](../reports/deployment-landscape-2026-10-31.md) and an artifact/runtime support matrix. General Flux/Argo installation recipes, their automated migration lifecycle, monitoring/off-host backup integrations and additional platform paths remain later slices.

## Source and behavior

Base: `e9da355b5fd6a6e1c4cee4d6bbb84b26ef51304c`.
Implementation patch SHA-256, excluding this handoff: `6673bbfc5074910e18a7fa208ffa634f83d7554c5ae6fc566a44c0822521839b`.

The nightly job packages only after authenticating its canonical image. Chart version/appVersion, full source revision and immutable image pin travel together. It inspects rendered workloads, compares all packaged file bytes on immutable readback and signs only a fresh source-owned upload. Existing charts must already have the correct source signature and matching contents; unsigned/conflicting bytes and inconclusive registry reads fail. An isolated anonymous Helm client resolves the exact version and verifies its digest/content; chart and image signatures must also be anonymously retrievable.

The optional nightly Kyverno policy binds the exact source SHA. Its rendering is tested; live Kyverno enforcement is not established here. Stable trust and Apple signing/submission behavior are unchanged.

The chart now exports the existing staged root-key file path for local operator commands. The Kind gate exercises the actual same-container admin flow instead of injecting a root value into separate Jobs. Its retained broken fixture removes both flag and environment root-file sources, preserving the required readiness refusal.

## Installation and discovered gap

Use [the singleton runbook](../operations/kubernetes-singleton.md), [example values](../../chart/hikyo/examples/singleton-manual.yaml) and [support matrix](../operations/deployment-support.md). They cover exact artifact verification, file-backed Secrets, independent public/state PVCs, guarded empty-volume initialization, native TLS, first-admin authority disclosure, CLI trust/login and separately held recovery prerequisites.

Source tracing found that unattended startup resolves bundle/operator paths only in process-local configuration, so a separately executed host-only CLI does not inherit them. This is tracked in [#1112](https://github.com/Hikyo-Org/Hikyo/issues/1112). The complete example deliberately uses the existing manual-custody contract rather than bypassing admission or launching a second updater.

## Verification

| Evidence | Result and scope |
| --- | --- |
| Shared fast-feedback plan and execution | PASS: shellcheck, pinned workflow lint, docs-content preparation and whitespace; no unrelated frontend checks selected |
| Nightly publisher/consumer fixtures | PASS: real Helm 4.2.3 packaging/rendering with synthetic registry/OIDC boundaries; fresh/retry/refusal/public-readback cases |
| Python package boundaries | PASS: 7 tests, including source symlinks, archive links/traversal/duplicates, changed contents and source/version mismatch |
| Chart owner and unattended contract | PASS: structured chart/RBAC/security/probe/HA/configuration checks and retained incompatible-mode refusals |
| Manual example | PASS: Helm 4.2.3 rendering, separate PVC references, TLS service selectors and missing-root fixture shape; no cluster execution |
| Chart mutation consumers | PASS: valid chart accepted; all retained RBAC/env/args/hardening/network mutations refused |
| CI-owner package | PASS: `go test -p 2 ./scripts/ci -count=1`, final candidate, 389.291 seconds |
| Standards source review | Initial P1 missing-root consumer regression repaired; independent recheck found no remaining actionable findings |
| Spec source review | Initial P1 actual Helm4 missing-tag response repaired; independent recheck found no remaining actionable findings |
| Hosted CI / merge | Pending PR workflow and separate protected merge gates |
| Published chart / anonymous registry acceptance | HOLD; initial anonymous attempt returned403; installed user token lacks read:packages for package inventory |
| Live Kind/admin, CSI/CNI/remount, Kyverno and restore acceptance | NOTRUN |
| Human maintainer/adversarial security review | Required, not supplied by automated source review |

Initial reviews and exact-candidate rechecks were separate Standards and Spec passes. Rechecks used OpenAI `gpt-6.1-sol`, high reasoning. The Spec reviewer additionally ran local fixtures with Helm3.16.1; the parent performed the pinned Helm4.2.3 checks above. Optional cross-provider review was not requested.

## Reproduce local checks

Use the repository-pinned Go/Node toolchain, Helm4.2.3, ShellCheck0.11.0 and Python with PyYAML6.0.3. From the repository root:

```sh
set -eu
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
scripts/ci/check-nightly-release_test.sh
scripts/ci/check-chart.sh
scripts/ci/check-chart_test.sh
scripts/ci/check-unattended-chart_test.sh
GOMAXPROCS=2 go test -p 2 ./scripts/ci -count=1
```

These commands do not publish, provision a cluster or establish runtime acceptance. The existing chart/Kind gate needs the exact candidate binary and matching fixture bundle specified by its runner; do not substitute an arbitrary source build or weaken its admission checks.

## Remaining delivery gates

1. Complete the exact-head workflow-change and human security review; use the normal protected merge process. No merge, stable release or production rollout is implied by this PR.
2. After the change lands, inspect the selected main nightly's image/chart job. For the specific `hikyo-org/charts/hikyo` package, an organization package owner may need to establish repository Actions access and public visibility. Package permissions do not imply public visibility. Follow [GitHub's package access/visibility guidance](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility). Do not broaden organization-wide policy merely to clear a failure.
3. Rerun the failed container/chart job only after its actual cause is repaired. A matching signed private upload can be reused after visibility repair. An unsigned interrupted upload is refused and needs explicit operator investigation; the script will not manufacture a signature for it.
4. Require the clean-client exact-version pull and chart/image signature verification before advertising chart availability. Record version/source/digests and the real installation's storage, ingress, admin, restart and restore evidence separately.
5. Repair #1112 before calling unattended deployments a complete host-operator experience. Deliver controller-tested Flux and Argo installation recipes against the demonstrated package/installation contract, then separately prove migration-safe automation.

Temporary test directories are trap-cleaned. No runtime cluster, application server, container, database or cloud resource was created by this implementation task. Local reports and validation receipts remain as evidence; HTML companions are ignored local artifacts and are not committed.
