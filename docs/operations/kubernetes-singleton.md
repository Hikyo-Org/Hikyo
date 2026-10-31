# Fresh singleton Kubernetes installation

This installs a **new, empty** PostgreSQL-backed Hikyo server using explicit
manual release/custody inputs. It is not HA, an upgrade command, or an
unattended-enrollment procedure. Hikyo is still prerelease software; this
example does not turn a nightly into a supported stable release. See the
[unattended operator-command gap](deployment-support.md#unattended-operator-command-gap)
before choosing a different initial custody model.

Run repository commands from the repository root. Kubernetes changes below are
operator actions in the explicitly selected cluster. No cluster, cloud account,
database, root key or public route is created merely by reading this runbook.

## 1. Prepare the dependencies and operator tools

Use a selected Kubernetes context, Helm with OCI digest support, kubectl, Cosign
and OpenSSL. The publisher uses Helm 4.2.3 and Cosign 3.1.3. The local source
example was rendered with Helm 3.16.1; rendering does not prove CSI/CNI or runtime
compatibility. Install a verified matching Hikyo CLI from the complete signed
release for administrator and recovery commands.

Before applying resources, provide:

- An empty PostgreSQL production database with the permissions required by the
  [manual deployment contract](manual-upgrades.md#compose-and-kubernetes).
- An authenticated offline bundle that authorizes the exact selected nightly,
  an independently pinned operator public key and off-server private signing
  custody. Prepare these through [manual bundle assembly](manual-upgrades.md#required-public-artifacts)
  and [signed nightly trust/assembly](signed-nightlies.md). The assembler accepts
  complete signed nightly downloads through `--nightly`. It does not manufacture
  recovery signatures or trust. Stop if those prerequisites are unavailable.
- Verified TLS connectivity to PostgreSQL, including hostname verification. If
  using a private CA, prepare a separate CA Secret and set
  `database.tls.existingSecret`; the DSN must name the mounted CA path
  `/run/hikyo-database-ca/ca.crt` where needed.
- A POSIX filesystem StorageClass with durable writes, executable mounts and
  preserved ownership/modes. Qualify its remount behavior; an arbitrary CSI
  driver or `emptyDir` is not a substitute for this storage contract.
- A DNS name and certificate/key for the browser-visible origin. This example
  serves native HTTPS through an operator-owned L4 service. TLS termination at
  a reverse proxy instead requires a deliberately different values file with
  its exact trusted CIDRs. Do not leave the application plaintext to the cluster.
- Cluster permission to install the chart and its CRDs, create the namespace,
  Secrets and PVC, and use local operator `exec`. The delivery controller is
  disabled here, but Helm still installs the chart's CRDs.
- Outbound access required by signed release/trust verification. The base chart
  does not restrict egress. Its default ingress policy permits TCP 8080 broadly;
  native TLS/authentication are not network isolation. Plan and verify a
  restricted NetworkPolicy against your CNI before sensitive use.

Confirm the intended context and that this is a new release:

```sh
set -eu
kubectl config current-context
helm list --all-namespaces --all
kubectl get storageclass
```

Review that output before continuing. Namespace `hikyo`, release `hikyo` and the
example PVC/Secret names must be reserved for this installation.

## 2. Select and authenticate one chart/image pair

Get the exact chart version, chart digest, image digest and full source commit
from a successful nightly publisher record. Do not select `nightly` or a loose
version range. The source chart's development image placeholder is not usable.

The new nightly chart mechanism is available only after its change reaches main
and a nightly publishes successfully. Original anonymous access returned 403;
public retrieval remains **HOLD** until the selected package is readable. If no
successful chart exists, stop here. Do not invent a chart version from an archive
tag or bypass authentication by rendering arbitrary source.

Set these nonsecret inputs to that record's exact values:

```sh
HIKYO_VERSION='0.0.1-nightly.YYYYMMDD.RUN.gSOURCE8'
HIKYO_SOURCE_COMMIT='<40-lowercase-hex-source-commit>'
HIKYO_CHART_DIGEST='sha256:<64-lowercase-hex-chart-digest>'
HIKYO_IMAGE_DIGEST='sha256:<64-lowercase-hex-image-digest>'
HIKYO_ORIGIN='https://hikyo.example.com'
HIKYO_RELEASE_DIR='./hikyo-verified-release'
mkdir -m 0700 "$HIKYO_RELEASE_DIR"

cosign verify \
  --certificate-identity='https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  --certificate-github-workflow-sha="$HIKYO_SOURCE_COMMIT" \
  "ghcr.io/hikyo-org/charts/hikyo@$HIKYO_CHART_DIGEST"
cosign verify \
  --certificate-identity='https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  --certificate-github-workflow-sha="$HIKYO_SOURCE_COMMIT" \
  "ghcr.io/hikyo-org/hikyo@$HIKYO_IMAGE_DIGEST"
helm pull "oci://ghcr.io/hikyo-org/charts/hikyo@$HIKYO_CHART_DIGEST" \
  --destination "$HIKYO_RELEASE_DIR"
helm show chart "$HIKYO_RELEASE_DIR/hikyo-$HIKYO_VERSION.tgz"
helm show values "$HIKYO_RELEASE_DIR/hikyo-$HIKYO_VERSION.tgz" \
  > "$HIKYO_RELEASE_DIR/chart-values.yaml"
grep -Fx '  repository: ghcr.io/hikyo-org/hikyo' "$HIKYO_RELEASE_DIR/chart-values.yaml"
grep -Fx "  digest: $HIKYO_IMAGE_DIGEST" "$HIKYO_RELEASE_DIR/chart-values.yaml"
```

Require both signature verifications and image-pin checks to succeed. Inspect
chart `version`, `appVersion` and source-revision annotation for the selected
record before installation. The chart signature authenticates its contents;
the independent image check confirms the same intended publisher/source. Neither
replaces runtime release admission. The packaged chart can render an optional
Kyverno policy for this exact nightly source identity. The example leaves admission disabled: enabling that policy
requires an installed, qualified Kyverno version and its own runtime acceptance;
chart rendering alone does not establish enforcement.

## 3. Create the file-backed bootstrap Secrets

Use a private directory on the operator workstation, outside version control.
Prepare a `production-dsn` file through your protected secret workflow, with no
trailing comments or shell syntax. It contains the complete PostgreSQL DSN using
`sslmode=verify-full`. Place the certificate and private key
in `tls.crt` and `tls.key`. Do not put DSNs or root keys in chart values, argv,
shell history, logs, Git, or this runbook's evidence record.

```sh
umask 077
: "${HIKYO_INPUT_DIR:?choose a new private directory outside the checkout}"
mkdir -m 0700 "$HIKYO_INPUT_DIR"
```

Populate `production-dsn`, `tls.crt` and `tls.key` in that directory
using the protected workflow, then create the Secrets:

```sh
test -s "$HIKYO_INPUT_DIR/production-dsn"
test -s "$HIKYO_INPUT_DIR/tls.crt"
test -s "$HIKYO_INPUT_DIR/tls.key"
test ! -e "$HIKYO_INPUT_DIR/root-key"
openssl rand -hex 32 > "$HIKYO_INPUT_DIR/root-key"
chmod 0600 "$HIKYO_INPUT_DIR/"*

kubectl create namespace hikyo
kubectl --namespace hikyo create secret generic hikyo-root-key \
  --from-file=root-key="$HIKYO_INPUT_DIR/root-key"
kubectl --namespace hikyo create secret generic hikyo-database \
  --from-file=HIKYO_DB="$HIKYO_INPUT_DIR/production-dsn"
kubectl --namespace hikyo create secret tls hikyo-server-tls \
  --cert="$HIKYO_INPUT_DIR/tls.crt" --key="$HIKYO_INPUT_DIR/tls.key"
```

For a private database CA, create `hikyo-database-ca` with
`--from-file=ca.crt=PATH` and set that name under `database.tls.existingSecret`
in the next step. Back up the original root key separately from database exports
and backup-decryption identities. Generate it once only; never rerun the root-key
generation step during a restart, Helm retry or restore.

## 4. Provision the public bundle and persistent installation state

Review the default StorageClass before applying the two independent claims.
The examples request 1 GiB for public artifacts and 5 GiB for installation state,
not a capacity guarantee. Add an explicit `storageClassName` in reviewed local
copies when your cluster requires one.

```sh
kubectl apply -f chart/hikyo/examples/singleton/upgrade-public-pvc.yaml
kubectl apply -f chart/hikyo/examples/singleton/upgrade-state-pvc.yaml
kubectl --namespace hikyo get pvc hikyo-upgrade-public hikyo-upgrade-state
```

The storage administrator must provision both **new, empty** filesystems before
Hikyo starts. Use the driver's approved administrative mount/provisioning path.
For `WaitForFirstConsumer`, arrange binding through that procedure. Do not launch
a root pod that blindly changes an arbitrary claim's existing contents.

Prepare `HIKYO_PUBLIC_INPUT_DIR` containing only the assembled `bundle/` tree and
separately pinned `operator.pub` described in the manual runbook. Preserve the
verified bytes. No private operator signer, backup identity or root escrow
belongs on this public volume.

On the Linux storage-administration host, set `HIKYO_PUBLIC_VOLUME` and
`HIKYO_UPGRADE_VOLUME` to the verified mounted roots of the corresponding claims.
Initialize only these new empty roots as root:

```sh
set -eu
: "${HIKYO_PUBLIC_INPUT_DIR:?select the verified public bundle and operator pin}"
: "${HIKYO_PUBLIC_VOLUME:?select the new public PVC filesystem mount point}"
: "${HIKYO_UPGRADE_VOLUME:?select the new installation-state PVC mount point}"
test "$(id -u)" -eq 0
mountpoint -q -- "$HIKYO_PUBLIC_VOLUME"
mountpoint -q -- "$HIKYO_UPGRADE_VOLUME"
test "$(stat -c '%d:%i' "$HIKYO_PUBLIC_VOLUME")" != "$(stat -c '%d:%i' "$HIKYO_UPGRADE_VOLUME")"
public_entry=$(find "$HIKYO_PUBLIC_VOLUME" -mindepth 1 -maxdepth 1 -print -quit)
state_entry=$(find "$HIKYO_UPGRADE_VOLUME" -mindepth 1 -maxdepth 1 -print -quit)
test -z "$public_entry"
test -z "$state_entry"
test -d "$HIKYO_PUBLIC_INPUT_DIR/bundle"
test -f "$HIKYO_PUBLIC_INPUT_DIR/operator.pub"
public_link=$(find "$HIKYO_PUBLIC_INPUT_DIR" -type l -print -quit)
test -z "$public_link"
cp -R -- "$HIKYO_PUBLIC_INPUT_DIR/bundle" "$HIKYO_PUBLIC_VOLUME/bundle"
chown -R 65532:65532 "$HIKYO_PUBLIC_VOLUME/bundle"
install -o 65532 -g 65532 -m 0444 "$HIKYO_PUBLIC_INPUT_DIR/operator.pub" "$HIKYO_PUBLIC_VOLUME/operator.pub"
install -d -o 65532 -g 65532 -m 0700 "$HIKYO_UPGRADE_VOLUME/operator-custody"
chown 0:65532 "$HIKYO_PUBLIC_VOLUME" "$HIKYO_UPGRADE_VOLUME"
chmod 2770 "$HIKYO_PUBLIC_VOLUME" "$HIKYO_UPGRADE_VOLUME"
stat -c '%u:%g %a %n' "$HIKYO_UPGRADE_VOLUME" "$HIKYO_UPGRADE_VOLUME/operator-custody"
```

Expected installation root: `0:65532 2770`; private child: `65532:65532 700`.
The recursive ownership change above touches only the newly copied, verified
public bundle, never an existing installation journal. An existing file or
symlink refuses this initialization. Do not remove files or recursively chmod
a populated claim to force this example to fit.

Validate modes after actual unmount/remount through the CSI driver. The chart's
`fsGroupChangePolicy: OnRootMismatch` must not recursively broaden the private
child. Unmount administrative attachments before the ReadWriteOnce workload
uses the claims. A bound claim does not establish these guarantees. The chart
mounts the public claim read-only and the private child as its shared installed
authority; local operator commands must use that exact authority.

## 5. Review values and install once

Copy the example and edit the origin and any private database CA reference:

```sh
cp chart/hikyo/examples/singleton-manual.yaml hikyo-values.yaml
```

The example disables unattended upgrades, the delivery operator, HA and
configuration rollout. It references only existing Secret/PVC names, leaves the chart's signed image pin
intact, and sets starter server requests of 100m CPU/256 MiB. These are scheduling
inputs, not a measured capacity or workload guarantee; size them using actual
workload and operator diagnostics. The TLS helper has its own chart resources.

Render locally, inspect the result, then install the fresh release:

```sh
helm template hikyo "$HIKYO_RELEASE_DIR/hikyo-$HIKYO_VERSION.tgz" \
  --namespace hikyo --values hikyo-values.yaml > "$HIKYO_RELEASE_DIR/rendered.yaml"
helm install hikyo "$HIKYO_RELEASE_DIR/hikyo-$HIKYO_VERSION.tgz" \
  --namespace hikyo --values hikyo-values.yaml --wait --timeout 20m
kubectl --namespace hikyo rollout status deployment/hikyo-hikyo --timeout=20m
kubectl --namespace hikyo get pods -l app.kubernetes.io/instance=hikyo
```

Do not use `--atomic`, automatic old-image rollback, or `helm upgrade --install`
as an unreviewed retry shortcut. If installation times out, inspect pod status
and sanitized events. Preserve the database/root/claim and follow the recovery
contract. This manual deployment uses the chart's existing startup/readiness/liveness
probes. Readiness remains separate from liveness; a database outage must not be
misrepresented as a healthy service.
Never remove custody, disable release trust or add `--dev` to make boot succeed.

## 6. Establish the public route and first administrator

The chart owns a ClusterIP Service, not public ingress. If your cluster has a
working LoadBalancer implementation and an approved external address, review
and apply the optional operator-owned L4 service:

```sh
kubectl apply -f chart/hikyo/examples/singleton/public-service.yaml
kubectl --namespace hikyo get service hikyo-public
```

It forwards TCP 443 to the application's native-TLS port. Configure any
provider-specific address/annotations in your reviewed local copy, point the
chosen DNS name at the approved address, and test from outside the cluster:

```sh
curl --fail --silent --show-error "$HIKYO_ORIGIN/api/v1/meta"
curl --fail --silent --show-error --output /dev/null "$HIKYO_ORIGIN/"
```

A cluster without an LB implementation needs its existing L4 frontend configured
to reach the chart's Service. Do not create a second TLS-terminating proxy
without reviewing the origin/trusted-proxy contract. The operational listener
8081 stays private. Public HTML/metadata success is not proof of authentication,
network isolation or a successful restore. Follow the separately authorized
[public-deployment acceptance procedure](public-deployment-acceptance.md).

Create the first administrator in the running server container. `-it` is required
for controlling-terminal disclosure; do not redirect or record the terminal:

```sh
hikyo context create hikyo-bootstrap --instance "$HIKYO_ORIGIN" --tls-mode system-ca
kubectl --namespace hikyo exec -it deployment/hikyo-hikyo -c server -- \
  /usr/local/bin/hikyo admin create --username admin
hikyo account establish-credential --instance "$HIKYO_ORIGIN" --as admin
hikyo login "$HIKYO_ORIGIN" --local --as admin --tls-mode system-ca
```

Confirm the origin and enter the single-use authority at the local CLI's protected
terminal prompt, then choose the administrator password there. The context step
establishes explicit system-CA/hostname trust before credential establishment;
the final login creates the local CLI session used by `doctor` below. Do not pass
the authority or password in argv or a pipe. Log in through HTTPS in the browser
as well and verify the expected account.
This chart supplies the staged root-key path through `HIKYO_ROOT_KEY_FILE`, so
host-only operator commands use the same source as the server. The image has no
shell, `cat` or `tar`; do not base authority retrieval on `kubectl cp`.

The delivery operator can be enabled separately with reviewed namespace/RBAC
and designated-ServiceAccount values after this server works. Enabling it grants
no server-release upgrade authority.

## 7. Prove restart and recovery before sensitive use

Record desired and observed identities without exporting Secrets:

```sh
kubectl --namespace hikyo get deployment hikyo-hikyo \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="server")].image}{"\n"}'
kubectl --namespace hikyo get pods -l app.kubernetes.io/name=hikyo \
  -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.containerStatuses[?(@.name=="server")].imageID}{"\n"}{end}'
kubectl --namespace hikyo rollout restart deployment/hikyo-hikyo
kubectl --namespace hikyo rollout status deployment/hikyo-hikyo --timeout=20m
hikyo doctor --instance "$HIKYO_ORIGIN" --as admin -o json
```

Confirm that the same database, root key and upgrade journal survived replacement,
the actual image matches the selected platform of the verified image index, and
HTTPS/login still work. A version string alone does not establish those bytes.
Investigate `doctor` findings; the example has not configured a normal off-host
backup schedule merely by provisioning installation custody.

Use the [encrypted backup and restore guide](https://hikyo.app/docs/backup-and-restore/)
to configure ordinary exports and a readable restore drill. These host-only
commands require more than a DSN: the matching verified executable, the same
authenticated public bundle/operator pin, and the same installed custody used
by the server. Run as runtime UID 65532 in an approved trusted execution
environment with access to the real public/state volumes and protected
PostgreSQL connectivity. Copying the journal creates different authority and is
not a substitute for sharing its locks. Do not chmod/chown existing custody to
make a different operator UID work.

Set the following paths for that execution environment. `HIKYO_OPERATOR_DSN_FILE`
must select the live database, with verified TLS and its CA path adjusted for
this host when necessary. `HIKYO_OPERATOR_PUBLIC_DIR` contains `bundle/` and
`operator.pub`; `HIKYO_OPERATOR_STATE_DIR` names the actual private
`operator-custody` child, not the PVC root:

```sh
: "${HIKYO_OPERATOR_DSN_FILE:?select the private live database DSN file}"
: "${HIKYO_OPERATOR_PUBLIC_DIR:?select the authenticated mounted public directory}"
: "${HIKYO_OPERATOR_STATE_DIR:?select the actual mounted private custody child}"
test "$(id -u)" -eq 65532
HIKYO_DB=$(cat "$HIKYO_OPERATOR_DSN_FILE")
HIKYO_UPGRADE_BUNDLE="$HIKYO_OPERATOR_PUBLIC_DIR/bundle"
HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY="$HIKYO_OPERATOR_PUBLIC_DIR/operator.pub"
HIKYO_UPGRADE_STATE_DIR="$HIKYO_OPERATOR_STATE_DIR"
export HIKYO_DB HIKYO_UPGRADE_BUNDLE HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY HIKYO_UPGRADE_STATE_DIR
```

Do not print the environment or enable shell tracing. If the storage platform
cannot provide this execution environment, stop and arrange its approved
operator access rather than treating the commands below as runnable from an
unrelated workstation. This runbook does not install a remote administrative API.

Set `HIKYO_BACKUP_IDENTITY_FILE` to a new private file in separate backup custody,
`HIKYO_EXPORT_DIR` to the managed encrypted-export directory, and
`HIKYO_DRILL_DSN_FILE` to a private file containing the dedicated drill database
DSN. Create a disposable test project/value through the authenticated UI before
exporting so the drill can prove readability. Create an encrypted export using
the recipient printed by key generation:

```sh
: "${HIKYO_BACKUP_IDENTITY_FILE:?choose a new separately held identity file}"
: "${HIKYO_EXPORT_DIR:?choose the encrypted export directory}"
hikyo backup keygen --output-file "$HIKYO_BACKUP_IDENTITY_FILE"
# Set HIKYO_BACKUP_RECIPIENT to the public age recipient printed above.
: "${HIKYO_BACKUP_RECIPIENT:?set the generated public age recipient}"
hikyo backup export --recipient "$HIKYO_BACKUP_RECIPIENT" --out "$HIKYO_EXPORT_DIR"
```

Select the actual exported filename as `HIKYO_BACKUP_ARCHIVE`. Set
`HIKYO_DRILL_ROOT_KEY_FILE` to the original root key recovered through its
separate escrow, in an owner-only file readable by this operator UID. Set
`HIKYO_DRILL_PRINCIPAL` to the test human principal ID and
`HIKYO_DRILL_PROJECT` to its `org-id/project-id`, then run:

```sh
: "${HIKYO_BACKUP_ARCHIVE:?select the actual encrypted archive}"
: "${HIKYO_DRILL_ROOT_KEY_FILE:?select the private original root key from escrow}"
: "${HIKYO_DRILL_DSN_FILE:?select the private empty drill database DSN file}"
: "${HIKYO_DRILL_PRINCIPAL:?select the test human principal ID}"
: "${HIKYO_DRILL_PROJECT:?select org-id/project-id containing the test value}"
hikyo restore drill \
    --from "$HIKYO_BACKUP_ARCHIVE" \
    --identity-file "$HIKYO_BACKUP_IDENTITY_FILE" \
    --root-key-file "$HIKYO_DRILL_ROOT_KEY_FILE" \
    --principal "$HIKYO_DRILL_PRINCIPAL" \
    --project "$HIKYO_DRILL_PROJECT" \
    --target-postgres-dsn-file "$HIKYO_DRILL_DSN_FILE" -o json
```

Require a successful readable drill, inspect its result, and retire the drill
database through its database administrator; the command does not drop a
PostgreSQL database. Preserve the export, original root key, backup identity and
installation custody in their documented separate recovery locations. The
routine above is a backup/readability check, not the separate signed upgrade
attestation or a complete disaster-recovery exercise. Credential reconciliation
after a real restore remains required; a PostgreSQL disk snapshot or local vault
alone is insufficient.

Future image changes follow [manual verified upgrades](manual-upgrades.md)
and the [signed-upgrade ADR](../adr/signed-upgrade-compatibility.md). The app
authenticates the exact selected release and migration route. An uncertain schema
write remains fenced; never restore an old image or old installation journal to
make readiness green. Full Flux/Argo orchestration is a later, separately tested
delivery slice, related to [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).

## Removal and retained state

`helm uninstall hikyo --namespace hikyo` removes chart-owned workloads. The
example's Secrets, PVC and optional public Service are operator-owned, and Helm
does not remove the installed CRDs on uninstall. Retain datastore, root key,
encrypted archives and installation custody through any recovery window. Remove
the public Service separately when retiring exposure. Do not delete the namespace
as a cleanup shortcut: that would also delete its Secrets and PVCs, with storage
reclaim behavior determined by the actual PV/StorageClass. Retiring real data
requires an explicit retention and recovery decision.
