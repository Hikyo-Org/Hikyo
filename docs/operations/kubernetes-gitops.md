# Install Hikyo with Flux or Argo CD

These recipes install an exact signed Helm chart into a **fresh singleton
PostgreSQL installation with manual custody**. Flux starts suspended; Argo CD
requires manual synchronization. Both use an immutable chart digest and the
chart's matching image digest. They do not enroll unattended updates or supply
an automatic migration coordinator. Every populated release change still uses
the full-stop, backup, readable-restore and migration procedure below.

The version you select decides what the generator requires:

| Version | Signing identity | Chart identity |
| --- | --- | --- |
| Stable `X.Y.Z` | `release.yml` at tag `vX.Y.Z` | `stable` update channel |
| Nightly | `nightly.yml` on `main` | `nightly/v1` profile and source annotations |

Nightlies remain unsupported prereleases. Passing the generator does not qualify a
stable chart; record its controller and application results as described below.

**Stable 1.0.0 is refused, and no published stable chart passes yet.** The 1.0.0
chart does not set `HIKYO_ROOT_KEY_FILE`, and it has no value that could add it.
`admin` has no root-key flag, so the first-administrator step fails with
`no root key configured`. The 1.0.0 image and chart signatures themselves verify.
Under Flux 2.10.0 the same chart also fails to install: Flux versions it
`1.0.0+<digest>`, and the chart copies that into an invalid `helm.sh/chart` label.
Both are fixed in this source, so the first stable release cut from it can pass
the generator. No published stable chart has completed a controller install yet.
See the [deployment matrix](deployment-support.md#runtime-matrix).

These recipes do not authorize installation into an existing cluster. Source
tests, controller reconciliation, application readiness and publication are
separate evidence. Publication of the chart you select remains a prerequisite, not
an assumption.

## Prerequisites and artifact selection

Choose **one** controller for an isolated application namespace. Do not point
either recipe at an existing manually installed release or a release owned by
the other controller. Ownership transfer needs a separately reviewed procedure.
The recipes neither install the controllers nor give their credentials to the
Hikyo server. Namespace and controller administration are operator actions.

1. Complete sections 1 to 4 of the [singleton runbook](kubernetes-singleton.md):
   empty PostgreSQL with verified TLS, independent root/database/server-TLS
   Secrets, authenticated public bundle and operator pin, and separately
   qualified public/private installation claims. Keep private recovery custody
   outside Git. Existing claims alone do not prove that their contents are ready.
   That runbook is written for a nightly. For a stable chart, change these steps:
   - Build the bundle with the
     [stable assembler](manual-upgrades.md#assemble-a-public-stable-bundle-offline),
     not `--nightly`.
   - Authenticate the chart and image with `release.yml@refs/tags/vX.Y.Z`.
   - Take the version and digests from the published release, not a nightly
     publisher record.
   - Skip the source-revision annotation check, because stable charts carry none.
2. Use Python 3.11 or newer with PyYAML 6.0.3, Helm with OCI digest support,
   Cosign 3.1.3, kubectl and jq, plus an authenticated `gh` for a stable chart.
   The publisher and controller fixture use Helm 4.2.3. The controller campaign
   pins Flux 2.10.0 and Argo CD 3.5.6; other versions need their own recorded
   qualification.
3. Select the exact publicly retrievable chart and record its version, OCI
   manifest digest and full source commit. For a stable chart, take the digest from
   that release's notes or `chart-index.digest`, and the commit from
   `git rev-parse 'vX.Y.Z^{commit}'`. For a nightly, take all three from its
   successful [publisher receipt](signed-nightlies.md#nightly-helm-charts). A
   version label, moving `nightly` tag, or archive release alone is insufficient.
   Older nightlies do not acquire charts retroactively.

From the repository root, prepare nonsecret values:

```sh
cp chart/hikyo/examples/singleton-manual.yaml operator-values.yaml
# Edit operator-values.yaml for your HTTPS origin and existing Secret/PVC names.
# Retain the explicit false values for HA, rollout, operator and unattended mode.
```

Set `HIKYO_CHART_VERSION`, `HIKYO_CHART_DIGEST` and `HIKYO_SOURCE_COMMIT` from
that reviewed record. Set `HIKYO_KUBE_CONTEXT` to the intended cluster
context. These are public identifiers, not credentials. Stop if the selected
package or its signatures are unavailable; do not substitute a source checkout.

For a stable chart, the generator first requires `gh` to report a published,
non-prerelease release for that tag. The tag job signs its chart before the draft
is reviewed, so a valid signature alone does not mean the chart was released. It
then verifies the chart digest with the workflow identity for that version, the GitHub issuer and the source SHA. It pulls that digest, checks the
chart's version and channel or profile and its image binding, independently
verifies the pinned image against the same source, and renders the real chart. A
stable chart carries no source annotations, so its source binding is the signed
tag identity and workflow SHA. The render must set `HIKYO_ROOT_KEY_FILE`. The
generator accepts only the manual singleton values surface, existing Secret
references, separate custody claims and native TLS. Image overrides, secret-value fields, cluster-wide policy
installation, HA, delivery/configuration operators and unattended enrollment
refuse. It writes a new output file only after these checks pass and never
contacts Kubernetes. Existing output files are not overwritten.

The runtime still verifies the authenticated release bundle, compatibility
route, installation custody and recovery evidence. OCI signatures do not replace
that [signed-upgrade contract](../adr/signed-upgrade-compatibility.md).

## Flux

Flux's [OCIRepository](https://fluxcd.io/flux/components/source/ocirepositories/)
supports a digest reference and keyless signer matching. The recipe repeats the
selected release's issuer/subject constraint in-cluster. Source-SHA verification occurs in
the generator; the controller's identity matcher alone does not check that
certificate extension. Review and retain the generator's exact source record.

Signature verification and the chart pull run inside source-controller, so its pods
need HTTPS egress to at least `tuf-repo-cdn.sigstore.dev`, `ghcr.io` and
`pkg-containers.githubusercontent.com`. A node search domain with a wildcard
DNS record can send those names elsewhere, and the OCIRepository then reports a TLS
handshake failure. If that happens, test the same URL from a pod with and without a
trailing dot on the hostname.

```sh
python3 -B scripts/deploy/render_gitops.py \
  --provider flux \
  --chart-version "$HIKYO_CHART_VERSION" \
  --chart-digest "$HIKYO_CHART_DIGEST" \
  --source-commit "$HIKYO_SOURCE_COMMIT" \
  --values operator-values.yaml --output hikyo-flux-reviewed.yaml
kubectl --context "$HIKYO_KUBE_CONTEXT" apply --dry-run=server \
  -f hikyo-flux-reviewed.yaml
# Review the complete generated file, then apply the same bytes.
kubectl --context "$HIKYO_KUBE_CONTEXT" apply -f hikyo-flux-reviewed.yaml
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo wait \
  --for=condition=Ready ocirepository/hikyo --timeout=180s
```

The application namespace is `hikyo` by default; use `--namespace` and adjust
commands together for another namespace. Flux's source/Helm controllers must
watch it. The HelmRelease impersonates `hikyo-deployer` there. Its Role can manage
Helm history Secrets, Deployments, Services, public trust ConfigMaps and
NetworkPolicies, and inspect Pods/ReplicaSets/Events. It has no cluster-scoped
resource authority. Helm history storage requires namespace Secret access, so
isolate this namespace and limit who may impersonate that service account.

The chart's delivery CRDs are skipped, matching `operator.enabled=false`.
No chart-created resources may be silently adopted. Explicit
[HelmRelease policies](https://fluxcd.io/flux/components/helm/helmreleases/)
select `RemediateOnFailure` with zero retries and `remediateLastFailure=false`
for both install and upgrade. Cleanup and drift correction are disabled. This
avoids dependence on a controller's default strategy or feature gates. A failed
candidate stays failed instead of uninstalling it or reinstalling an old image.

Before activation, inspect the OCIRepository's current generation, verified
Ready condition and artifact revision; the selected revision must identify the
recorded chart digest. If a parent Flux Kustomization manages this file, suspend
that parent for the approval window and let its in-flight reconciliation finish.
Keep pruning disabled for this application path. The shipped HelmRelease and
OCIRepository also carry Flux's prune-disabled annotation. Neither annotation
prevents an administrator explicitly deleting a HelmRelease, which uninstalls
the release.

With the fresh database and storage prerequisites satisfied:

```sh
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo patch helmrelease hikyo \
  --type=merge -p '{"spec":{"suspend":false}}'
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo wait \
  --for=condition=Ready helmrelease/hikyo --timeout=660s
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo rollout status \
  deployment/hikyo-hikyo --timeout=180s
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo patch helmrelease hikyo \
  --type=merge -p '{"spec":{"suspend":true}}'
```

Retain the HelmRelease's `lastAttemptedRevisionDigest`, exact deployed image and
observed generation. Confirm the Helm action is terminal before considering
suspension effective. Commit the reviewed suspended manifest to the operator's
GitOps repository and then resume its parent. Do not add image automation or a
SemVer range. A suspended release deliberately requires approval for subsequent
chart or values changes.

## Argo CD

Use Argo CD's [generic OCI source](https://argo-cd.readthedocs.io/en/stable/user-guide/oci/):
`repoURL` includes `oci://`, `targetRevision` is the exact chart digest, and
`path` is `.`. This differs from the older Helm-repository `chart`/version form.
Argo renders the Helm chart and owns the resulting resources; it does not
create a Helm release. Use Argo status, not `helm history`, for this installation.

```sh
python3 -B scripts/deploy/render_gitops.py \
  --provider argo-cd \
  --chart-version "$HIKYO_CHART_VERSION" \
  --chart-digest "$HIKYO_CHART_DIGEST" \
  --source-commit "$HIKYO_SOURCE_COMMIT" \
  --values operator-values.yaml --output hikyo-argo-reviewed.yaml
kubectl --context "$HIKYO_KUBE_CONTEXT" apply --dry-run=server \
  -f hikyo-argo-reviewed.yaml
# Review the complete generated file, then apply the same bytes.
kubectl --context "$HIKYO_KUBE_CONTEXT" apply -f hikyo-argo-reviewed.yaml
kubectl --context "$HIKYO_KUBE_CONTEXT" -n argocd get application hikyo
```

The Application and AppProject live in `argocd`; `--controller-namespace` can
select another Argo namespace. The project restricts sources to the canonical
OCI chart, destinations to the selected local application namespace, and
resources to namespaced Deployment/Service/ConfigMap/NetworkPolicy objects.
The application namespace must already exist. CRDs are skipped.

The generator performs the exact-source Cosign checks; this recipe does not add
a Cosign verification plugin to Argo. Review those results before manual sync.
The immutable digest protects artifact selection, while runtime release
admission remains independent. Reverify before any later approved release change.

Automated sync, self-healing, retries and pruning are disabled. The Application
has no resource-deletion finalizer, and `FailOnSharedResource=true` refuses
resources tracked by another Argo application. This is not a general ownership
transfer check. The
[prune option](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/)
also protects manual sync from accidental pruning. Keep this manual policy in
the parent source; an ApplicationSet must not regenerate an automated policy.

When the fresh installation is ready for its first activation, initiate an
explicit sync of the selected digest:

```sh
sync_request=$(jq -nc --arg revision "$HIKYO_CHART_DIGEST" \
  '{operation:{sync:{revision:$revision,prune:false}}}')
kubectl --context "$HIKYO_KUBE_CONTEXT" -n argocd patch application hikyo \
  --type=merge -p "$sync_request"
kubectl --context "$HIKYO_KUBE_CONTEXT" -n argocd wait application/hikyo \
  --for=jsonpath='{.status.operationState.phase}'=Succeeded --timeout=660s
kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo rollout status \
  deployment/hikyo-hikyo --timeout=180s
```

This first-install wait starts with no previous operation. For later operations,
check that `operationState.syncResult.revision` names the selected digest and the
new operation is terminal; an old `Succeeded` result is not fresh proof. Inspect
Application health and the exact deployed image as well. Avoid sync override
flags, force/replace operations and rollback actions.

## Complete installation and retain evidence

For either controller, continue with sections 6 and 7 of the
[singleton runbook](kubernetes-singleton.md): operator-owned L4/native-TLS route,
first administrator, trusted CLI context/login, authenticated diagnostics,
restart, and encrypted restore drill. The public Service, Secrets, PostgreSQL
and custody PVCs remain operator-owned, outside the chart/controller's lifecycle.
Do not commit their contents. Do not install both recipes for the same server.

Record the actual source/patch, chart and image digests, generator result,
controller/Kubernetes/Helm/CSI/CNI versions, observed generations and current
controller action, application/admin result, restart evidence and recovery
receipt. Controller Ready or Synced alone does not establish those outcomes.

## Change a populated release or bootstrap configuration

Changing an OCI reference is an upgrade request, not migration authority.
Do not configure automatic chart/image promotion, Helm rollback/uninstall
remediation, Argo rollback, or an automatic database restore. `Recreate` alone
does not exclude external writers.

1. Prepare and authenticate the complete target route under
   [manual upgrades](manual-upgrades.md#populated-upgrade). Preserve the current
   generated manifest and installed custody. Verify every required intermediate
   target, not just the final chart's signature.
2. Freeze parent reconcilers that could undo maintenance. For Flux, set and
   persist `spec.suspend=true`, wait for any current Helm action to finish, and
   confirm its terminal release state. Suspension does not cancel an in-flight
   action. For Argo, retain disabled automatic sync/self-heal and confirm no
   operation is running; stop an in-flight operation through the approved Argo
   administration flow before proceeding. Prevent other operators and pipelines
   from starting a sync during the maintenance window.
3. Explicitly stop the current server and wait for **all** its Pods to disappear:

   ```sh
   kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo scale \
     deployment/hikyo-hikyo --replicas=0
   kubectl --context "$HIKYO_KUBE_CONTEXT" -n hikyo wait --for=delete pod \
     -l app.kubernetes.io/name=hikyo,app.kubernetes.io/instance=hikyo --timeout=180s
   ```

   Confirm the Deployment still requests zero, no terminating server process
   remains, and any external/legacy writers are stopped. An empty selector,
   stale heartbeat, or successful scale request alone is not exclusion proof.
4. With writers stopped, export the source, prove a same-engine scratch restore,
   prepare the authenticated public evidence, and run exactly one verified
   target migrator against the same production PostgreSQL and **same installed
   custody**. Follow the manual runbook's exact commands, receipt expiry, route
   ordering, maintenance, incarnation and generation checks. The existing chart
   is still frozen throughout this work. No PreSync/Helm hook performs a second
   migration or receives private drill custody.
5. Generate a new exact-target controller manifest using the verified target
   chart and the correctly prepared manual evidence values. Keep Flux suspended
   or Argo manual while updating that record. Approve only the verified target
   after migration; then resume Flux for that single action or explicitly sync
   Argo. Check the new action/revision, exact target readiness and authenticated
   diagnostics before restoring traffic. Suspend Flux again after success and
   reconcile the parent source to that reviewed state.
6. If migration or candidate health fails, leave the installation in maintenance
   with reconciliation frozen and preserve public evidence and custody. Resume
   only the exact target/generation when allowed. If SQL writes are uncertain,
   use explicit authenticated restore into an empty target under the recovery
   runbook. Never revert Git to the old binary as recovery.

Root-key, operator-pin, datastore or custody replacement is also a controlled
operation. Do not use a values commit or Secret overwrite to bypass its owning
rotation/move protocol. Removal is similarly deliberate: freeze reconciliation,
retain off-host recovery material and release records, then retire the runtime
without deleting PostgreSQL/root/custody storage. Explicit HelmRelease deletion
uninstalls its chart; Argo Application deletion without a finalizer retains the
workload. Neither constitutes data erasure or disaster recovery.

## Reproduce controller qualification

The fast source suite authenticates mocked verification boundaries and uses
real Helm packaging/rendering:

```sh
python3 -B -m unittest discover -s scripts/deploy -p 'test_*.py'
```

With Docker, Kind, kubectl, Helm 4.2.3 and PyYAML available, the separate campaign
creates its own cluster, registry and isolated kubeconfig, runs the pinned
controllers sequentially, and deletes those owned resources on exit:

```sh
python3 -B scripts/ci/check_gitops_kind.py --receipt gitops-controller-receipt.json
```

It installs the actual chart at zero replicas, checks Flux unsigned-artifact
refusal, exact digest selection, suspension and failure without rollback, and
Argo's manual synchronization, target-change hold, failed sync and retained
workload/PVC after Application removal. Its unsigned local HTTP registry and
unusable image digests are explicitly test fixtures; the production generator
and templates have no such bypass. It performs no database writes or actual
Hikyo boot. This is controller-mechanics evidence, not CSI/CNI qualification,
real nightly signature interoperability, populated migration, recovery or live
deployment acceptance, and is not an unconditional stable-release gate.
