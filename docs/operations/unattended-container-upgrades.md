# Unattended container deployment

Release status: implementation and deployment acceptance are being verified.
Do not assume an older published image supports these settings. The
[design and acceptance record](../design/unattended-container-upgrades.md)
distinguishes the approved custody exception from completed runtime evidence.
The [published docs entry](https://hikyo.app/docs/container-upgrades/) summarizes
this runbook; [manual upgrades](manual-upgrades.md) remain the default.

## Enrollment and authority

Choose `HIKYO_UPGRADE_UNATTENDED=true` at the first deployment of an empty
installation. Preserve the choice and the same persistent state on replacement.
Do not adopt an existing manually managed database by adding the flag. In
particular, it cannot assert that legacy writers stopped or replace a previous
operator pin.

Enrollment gives the container local recovery and attestation authority. The
vault is encrypted with a secret derived from the persistent root key; a process
that controls the runtime and root key can unlock it. Keep the encrypted vault,
root key and encrypted backup through your disaster-recovery process. Local
custody is not an off-host backup.

Each new image is the exact target. Hikyo verifies that release and its allowed
route, excludes writers before backup, performs a real scratch restore, migrates
and checks health before serving. It does not independently switch to a newer
`latest` release. No manual public bundle or operator key mount is needed in
this mode. Network access to the release and trust endpoints remains necessary.

## Docker with SQLite

Use [server-unattended.yaml](../../install/compose/server-unattended.yaml) for a
new single-container SQLite installation. It runs as UID/GID 65532 with a
read-only container filesystem and without a Docker socket.

Provision separate persistent data and upgrade directories, owned by UID/GID
65532 with mode 0700. The upgrade filesystem must support private modes, durable
file operations and execution of verified intermediate binaries. Generate the root key once, store its 64 hexadecimal
characters in a file readable only by UID 65532, and retain that file through
every image change. Never regenerate it during restart.

Create a protected environment file naming existing paths and nonsecret
deployment inputs:

```dotenv
HIKYO_SERVER_IMAGE=ghcr.io/hikyo-org/hikyo:<chosen-signed-release-or-channel>
HIKYO_SERVER_DATA_DIR=/srv/hikyo/data
HIKYO_UPGRADE_INSTALLATION_DIR=/srv/hikyo/upgrade
HIKYO_SERVER_ROOT_KEY_FILE=/srv/hikyo/root-key
HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com
HIKYO_TRUSTED_PROXY_CIDRS=<exact-reverse-proxy-CIDRs>
```

The example binds browser and operational ports on host loopback. Configure
your existing HTTPS reverse proxy for port 8080. Validate and start:

```sh
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server-unattended.yaml config --quiet
docker compose --env-file /etc/hikyo/compose.env -f install/compose/server-unattended.yaml up -d
```

Watchtower or another external updater may replace the chosen image tag while
retaining these mounts and settings. The runtime authenticates the actual new
image's release and refuses an unsupported route. Do not run a second writer
against the SQLite file or configure automatic old-image rollback. A restart
policy can restart the same image; it is not permission to restore a database.

## Kubernetes with PostgreSQL

The first supported topology is non-HA with one replica. The chart renders
`strategy: Recreate`, rejects multiple replicas and HA, and rejects simultaneous
configuration rollout authority. Hikyo receives no Kubernetes deployment token.

Provision an empty production PostgreSQL database and a separate empty scratch
database. The scratch DSN must target the same engine and must not alias the
production database. Store DSNs in existing Secrets; never place their values
in Helm values or shell command arguments. The runtime verifies the actual
database identities and pins the scratch identity for subsequent owned drills.
Do not share the scratch database with another application.

Provision a persistent upgrade PVC whose volume root has group 65532 and mode
2770, with an `operator-custody` child owned by UID/GID 65532 and mode 0700.
The storage driver must preserve these modes on remount so
`fsGroupChangePolicy: OnRootMismatch` does not recursively broaden private file
permissions. Data, custody and journal must survive pod replacement; `emptyDir`
does not satisfy this requirement. The upgrade volume must allow execution of
verified intermediate binaries. The existing persistent root-key Secret is
staged into an owner-only runtime volume by the chart.

```yaml
replicaCount: 1
ha:
  enabled: false
rollout:
  enabled: false
database:
  existingSecret: hikyo-database # key: HIKYO_DB
rootKey:
  existingSecret: hikyo-root-key
upgrade:
  stateExistingClaim: hikyo-upgrade-state
  unattended:
    enabled: true
    scratchDatabaseExistingSecret: hikyo-upgrade-scratch
    scratchDatabaseKey: HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN
```

Also configure the signed `image.digest`, external HTTPS origin and TLS or
trusted proxy settings required by the existing chart. Omit `upgrade.existingClaim`,
manual evidence, legacy-writer and target-manifest overrides. The chart injects
the scratch DSN through `secretKeyRef`. It cannot inspect the secret's DSN at
render time; runtime engine and database-alias checks remain mandatory.

Readiness uses `/readyz` and stays false throughout maintenance. In unattended
mode startup and liveness use `/healthz`, which remains available during the
upgrade. Before that listener opens, signed artifact preflight has a configurable
startup budget: `upgrade.unattended.startupFailureThreshold` defaults to 180
five-second probes, or 15 minutes. Tune this preflight allowance for the expected
download environment; it does not authorize incomplete backup or migration work.

## Interruption and browser behavior

An already loaded browser automatically shows confirmed maintenance phases and
reconnects after readiness and session revalidation. Ordinary drafts and the URL
are retained; secret disclosures follow existing retirement rules. No form is
submitted automatically. A disconnected proxy response is called reconnecting,
not claimed as evidence of an upgrade.

With no ready Kubernetes backend, a fresh navigation may receive an ingress
unavailable page before the application loads. The browser maintenance screen
does not replace an external ingress error page.

If a schema write fails or its outcome is uncertain, preserve the journal and
backup and use explicit operator recovery. The service remains fenced and can
show recovery-required status. Never delete the state directory, switch to an
older image or restore the database automatically to make readiness green.
