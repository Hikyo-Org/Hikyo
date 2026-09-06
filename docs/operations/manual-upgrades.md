# Manual verified installation and upgrade

For nightly releases on a Linux systemd host with local SQLite, use
`sudo hikyo upgrade`. It automates the procedure below, including encrypted local
operator custody, signed legacy bridges, backup restoration to scratch and
service health checks. See the [one-command upgrade instructions](https://hikyo.app/docs/upgrades/).
This manual runbook remains available for other deployment types and recovery.

Every production boot and `hikyo migrate` authenticates its exact build and
offline release bundle. Installing an image or binary alone is insufficient.
Remote apply, the old host helpers and WebUI apply remain disabled. The
Kubernetes secrets delivery operator does not coordinate server upgrades.

## Required public artifacts

The runtime directory has this layout. All members are regular files and all
directory components are real directories; projected Kubernetes Secret or
ConfigMap symlinks cannot be used directly.

```text
public/
  operator.pub
  bundle/
    index.json
    metadata.json
    metadata.sigstore.json
    catalog.json
    catalog.sigstore.json
    keys/<primary-key-id>.pub
    releases/<manifest-sha256>/manifest.json
    releases/<manifest-sha256>/manifest.sigstore.json
    releases/<manifest-sha256>/release-candidate.json
    releases/<manifest-sha256>/upgrade-compatibility.json
    bridges/<statement-sha256>/statement.json          # if authorized
    bridges/<statement-sha256>/statement.sigstore.json # if authorized
  evidence/                     # populated upgrade or restored incarnation
    receipt.json
    attestation.json
    attestation.sigstore.json
  backup.age                    # exact encrypted bytes bound by receipt
```

The closed index names the keys, release manifests and every bridge authorized
by the current signed catalog:

```json
{
  "format": "hikyo.dev/offline-upgrade-bundle/v1",
  "primary_key_ids": ["primary-1"],
  "releases": [{"profile": "stable/v1", "manifest_sha256": "<64 lowercase hex>"}],
  "bridges": []
}
```

The index is a locator, not authority. Preserve exact signed bytes when copying
release `release-manifest.json` and `release-manifest.sigstore.json` to their
fixed bundle names `manifest.json` and `manifest.sigstore.json`. Include the
installed source and every intermediate/target release needed by the route.
Retain separately verified binary/image payloads for all hops before stopping
writers. The stable metadata must authorize those exact manifest digests.

The catalog is produced by `scripts/release/create-upgrade-catalog.sh` and signed
in the offline recovery-key ceremony described in
[upgrade artifacts](upgrade-artifacts.md). Catalog generation and authenticated
offline bundle assembly are available. Publication of authentic production
metadata, catalog signatures and release proofs still requires the real signing
ceremony. The assembler never creates trust keys or signatures. Do not create
synthetic trust or enable development mode to make a production deployment start.

### Assemble a public stable bundle offline

On Linux or macOS, prepare separate directories containing only these regular
files. Copy signed files byte for byte; do not reformat JSON. The input directory
and its members must not be symlinks. Parent directories must be operator-owned
and stable throughout assembly.

```text
snapshot/
  metadata.json
  metadata.sigstore.json
  catalog.json
  catalog.sigstore.json
primary-keys/
  <each public_key filename in metadata.json>
target-proofs/
  release-manifest.json
  release-manifest.sigstore.json
  release-candidate.json
  upgrade-compatibility.json
```

Copy the catalog producer's `upgrade-catalog.json` to `catalog.json` and its
matching detached signature to `catalog.sigstore.json`, preserving their bytes.
From each release download, copy only the four named public proof documents to
a separate proof directory. Retain the downloaded binary/chart/image payloads
and their verification results separately. Extra files, including per-artifact
signature sidecars, executable archives and private keys, are refused in these
input directories.

Run from the checked-out Hikyo source with its pinned Go toolchain. Supply the
independently authenticated release root and recovery public key, never a root
discovered inside the downloaded bundle:

```sh
go run ./scripts/release/assemble-upgrade \
  --root /operator/trust/root.json \
  --recovery-key /operator/trust/recovery.pub \
  --snapshot /operator/assembly/snapshot \
  --keys /operator/assembly/primary-keys \
  --release /operator/assembly/target-proofs \
  --out /operator/assembly/bundle
```

Repeat `--release PATH` for the installed source and every intermediate/target
release. For every bridge authorized by the current catalog, pass `--bridge PATH`
to a directory containing exactly `statement.json` and `statement.sigstore.json`.
Omitting an authorized bridge is refused. Optionally pass `--floor PATH` with
the existing `SnapshotFloor` JSON to reject an older or conflicting snapshot;
assembly does not update that floor. Runtime always enforces the installation's
own persisted floor and actual datastore state.

The command verifies the recovery-signed snapshot, authorized primary keys,
stable release signatures and bound compatibility documents. It then runs the
production bundle loader over the exact staged bytes, including bridge proofs.
It publishes a new output directory atomically and refuses an existing path,
including a concurrently created directory. Inputs remain untouched. A reported
post-publication durability error identifies the retained output for inspection;
do not assume that invocation completed successfully.

For `nightly/v1`, pass `--nightly PATH` with the complete signed download,
including every payload. See [signed nightlies](signed-nightlies.md) for trust
bootstrap, bundle assembly and the recovery-signed legacy bridge. The assembler
does not inspect executable archive contents, sign operator evidence, or apply an
upgrade. Continue to verify each executable/image before use. Provision the
completed public bundle with read access for the runtime UID and mount it
read-only alongside the separately pinned `operator.pub` and any required
backup evidence. The assembler's staging/output directories use mode 0700 and
files use mode 0600, so assign runtime ownership during provisioning.

Fresh installation pins the configured operator public key before database
mutation and binds the generated instance identity at bootstrap. An existing
datastore with missing external installation custody refuses. Keep its private
signing key, age identity and backup root escrow off the server and deployment
controller. The existing runtime root key remains separate, with its current
owner-only access rules. Public artifacts may be read by UID 65532; the encrypted
backup is not plaintext custody.

Keep a separate persistent installation directory for
`HIKYO_UPGRADE_STATE_DIR`. It contains the operator pin/rotation journal, not
tenant data. Never populate it from a database backup or replace it when
restoring an older database. Provision it mode 0700 for the runtime UID. Every
HA replica and local operator command against the same database must use the
same installation authority, with working shared locks and file/directory
durability. An ephemeral volume or unrelated per-pod directory is unsuitable.

## Fresh native installation

Set actual paths and the already configured production datastore. These
variables contain public paths; the DSN remains in the operator's protected
environment configuration.

```sh
export HIKYO_UPGRADE_BUNDLE=/srv/hikyo-public/bundle
export HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY=/srv/hikyo-public/operator.pub
export HIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo-installation
hikyo migrate
hikyo server --auto-migrate=false --root-key-file=/run/hikyo-root/root-key
```

A genuinely empty database requires no source-data receipt. `migrate` is
DDL-only: it leaves durable maintenance at `schema-applied`. Only the exact
target boot completes candidate configuration and hierarchy checks, marks
healthy and admits serving. Keep the public bundle and installation state
available for every subsequent boot and operator command. Same-release healthy
restarts require no new backup proof.

## Populated upgrade

First obtain all signed route artifacts and verify each executable/image using
the release installation procedure. Stop every pre-gate writer before the first
legacy export; the new binary cannot retroactively fence an old process. For
gated HA, full stop remains the default before migration. Suspend any external
reconciler that could restart an old deployment and wait for its actual processes
to exit. A heartbeat observation is insufficient.

Run the exact target preparation binary with the instance's public bundle and
operator pin. For multiple hops, set `HIKYO_UPGRADE_TARGET_MANIFEST` to the exact
final manifest SHA-256 on every command; execute each intermediate target in
the verified route order.

```sh
hikyo backup upgrade-export --out /srv/upgrade-export --recipient "$AGE_RECIPIENT"
```

On the operator custody host, use the reported ciphertext and receipt paths,
the same public trust and an independently empty scratch target:

```sh
hikyo backup upgrade-drill \
  --from "$UPGRADE_ARCHIVE" --receipt "$UPGRADE_RECEIPT" \
  --identity-file /custody/age-identity \
  --root-key-file /separate-escrow/root-key \
  --signing-key /operator-custody/attestation.key \
  --target-sqlite /scratch/unique-upgrade-drill.db \
  --principal "$RECOVERY_PRINCIPAL" --project "$RECOVERY_PROJECT" \
  --out /srv/upgrade-attestations
```

For PostgreSQL replace `--target-sqlite` with
`--target-postgres-dsn-file /custody/empty-scratch-dsn`. The drill must use the
source engine. An offline drill can name the independently recorded instance
through `HIKYO_UPGRADE_OPERATOR_INSTANCE`. Remove that override for live export.
Only clean scratch resources owned by this run; retain failures for inspection.

Copy the reported public statement and signature to `evidence/attestation.json`
and `evidence/attestation.sigstore.json`, the receipt to `evidence/receipt.json`,
and the exact ciphertext to `backup.age`. Leave private custody on the drill
host. Then, with all writers stopped:

```sh
export HIKYO_UPGRADE_EVIDENCE=/srv/hikyo-public/evidence
export HIKYO_UPGRADE_BACKUP=/srv/hikyo-public/backup.age
# Set only for a verified legacy first hop, after every old writer has exited.
export HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED=true
hikyo migrate
hikyo server --auto-migrate=false --root-key-file=/run/hikyo-root/root-key
```

The gate rechecks source identity, schema, epoch, incarnation, generation,
ciphertext and signatures under exclusion. A populated upgrade without fresh
readable-restore proof refuses. Attestations expire within 24 hours and their
nonces are consumed atomically. If the exact intermediate target finishes but
requests the next binary, retain maintenance and switch to that verified binary;
do not expose tenant traffic between hops. After the final healthy boot, remove
the evidence/backup/legacy-stop environment settings for ordinary restarts.

## Compose and Kubernetes

`install/compose/server.yaml` provides a rootless manual server deployment.
Supply its required environment variables in a protected local environment file;
`HIKYO_SERVER_IMAGE` must be the verified repository plus `@sha256:` digest.
Provision the bind paths before running it. The public directory mounts read-only,
the installation directory mounts persistently writable, and no private drill
custody or Docker socket is mounted. For SQLite set
`HIKYO_DB=sqlite:/var/lib/hikyo/hikyo.db`. This example expects a separately owned
HTTPS reverse proxy; choose its exact trusted CIDRs.

```sh
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server.yaml config --quiet
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server.yaml stop server
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server.yaml run --rm --no-deps server migrate
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server.yaml up -d server
```

For populated Compose upgrades set the evidence paths to the container paths
`/run/hikyo-upgrade/evidence` and `/run/hikyo-upgrade/backup.age`. This is a manual
sequence; never configure automatic old-image rollback after possible SQL writes.

For Helm, prepopulate separate PVCs with the public directory and installation
state. Provision the state PVC root with group 65532 and mode **2770**, including
setgid. Inside it create `operator-custody`, owned by UID 65532 with mode **0700**.
The chart sets `HIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo-upgrade/operator-custody`.
This root layout satisfies Kubernetes `fsGroupChangePolicy: OnRootMismatch`
without recursive permission changes that would invalidate the private journal.
A 0700 volume root, or a 0770 root without setgid, does not satisfy that check.
Use a storage driver that preserves the private child's ownership and modes on
remount; validate this before admission. Local migration/rotation commands must
point at that same child directory. HA requires storage accessible consistently
by all replicas. Add these
values to the existing database/root-key/TLS configuration:

```yaml
upgrade:
  existingClaim: hikyo-upgrade-public
  stateExistingClaim: hikyo-installation
  evidence: false
  targetManifestSHA256: ""
  legacyWritersStopped: false
```

Set `evidence: true` during a populated upgrade and the legacy assertion only
for its first pre-gate hop. The chart uses `Recreate`, which prevents ordinary
rolling replacement but does not prove external writers stopped. Explicitly
scale to zero and confirm pod/process termination before changing artifacts.
Run exactly one verified `hikyo migrate` from the operator host against the
same PostgreSQL datastore and installation state, then install the exact target
chart/image and start replicas. Do not use Helm `--atomic` or a GitOps rollback
to reinstall an old binary after a failed post-write candidate boot.

## Operator-key rotation

Prepare the exact `operator-key-rotation/v1` statement for the current instance,
incarnation, strongest credential epoch and old/new public-key IDs. Sign it with
the prior operator key using the maintained Cosign profile. Apply it locally:

```sh
hikyo upgrade operator rotate --statement /operator/rotation.json \
  --signature /operator/rotation.sigstore.json \
  --new-public-key /operator/new-operator.pub
```

Break glass additionally requires `--local-break-glass --root-key-file PATH`
and the new-key-signed break-glass statement. Private custody stays on that
operator host. The durable journal coordinates the installation pin and database
epoch transition across crashes. Rotation retains maintenance and invalidates
pending proof. After success update the separately mounted `operator.pub` to the
new public key and prepare fresh export/drill evidence before reactivation.
Never overwrite the installation journal or restore an old copy as a rotation
shortcut.

## Failure and restore

Keep the stopped deployment and its public evidence when migration or candidate
health fails. Only the exact target/generation may resume an interrupted hop.
After possible schema writes, recovery is an explicit authenticated restore into
an empty target, never reverse SQL or an automatic old-container restart.
Preserve the external installation state and current operator key. Restore
advances credential authority and creates a new recovery incarnation; historical
attestations cannot reactivate it. Export and drill again for that current
incarnation before boot admission, then reconcile principals individually.
Use [backup and restore](/docs/backup-and-restore/) for the recovery commands.
