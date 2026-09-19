# Native HTTPS nightly container

[`nightly.yaml`](nightly.yaml) runs one Hikyo container with SQLite, native
HTTPS on port 8443, and operational endpoints published only on host loopback
port 8081. Use a **fresh, empty installation** and a signed release supporting
[unattended container upgrades](../../../docs/operations/unattended-container-upgrades.md).
Do not adopt a manually managed database by adding this configuration.
Nightlies are prereleases, not supported stable releases.

## Prepare the host

Explicit unattended enrollment gives the container local recovery and
attestation authority. Its encrypted custody vault depends on the persistent
root key. Keep that key, upgrade custody/journal, and encrypted database
backups through a tested disaster-recovery procedure. Local custody does not
replace off-host backups.

Provision and mount persistent storage before starting Docker Compose. Create
separate empty data and upgrade directories owned by UID/GID `65532`, mode
`0700`. The upgrade filesystem must preserve private permissions and durable
file operations and allow execution of verified intermediate binaries. Arrange
host boot ordering so Docker starts only after this filesystem is mounted.
Missing bind sources must fail; this template never formats disks or creates
host paths.

Generate the root key once as 64 hexadecimal characters in a protected file
readable by UID `65532`. Never regenerate it on restart or image replacement.
Provide a TLS certificate for your DNS name and its private key, both readable
by the container user; keep private key and root key owner-only. The Compose
template does not generate or print keys.

Create `/etc/hikyo/compose.env`, protected with mode `0600`, containing paths
and nonsecret inputs:

```dotenv
HIKYO_SERVER_IMAGE=ghcr.io/hikyo-org/hikyo:nightly
HIKYO_SERVER_DATA_DIR=/srv/hikyo/data
HIKYO_UPGRADE_INSTALLATION_DIR=/srv/hikyo/upgrade
HIKYO_SERVER_ROOT_KEY_FILE=/srv/hikyo/root-key
HIKYO_TLS_CERT_FILE=/srv/hikyo/tls/tls.crt
HIKYO_TLS_KEY_FILE=/srv/hikyo/tls/tls.key
HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com:8443
```

You may instead set `HIKYO_SERVER_IMAGE` to a verified
`ghcr.io/hikyo-org/hikyo@sha256:<digest>`. Select an authenticated signed image;
the actual image is the exact upgrade target. Hikyo authenticates its release
and upgrade route before serving. No manual public bundle or operator public
key mount is required in this explicitly enrolled mode. Outbound connectivity
to release and trust endpoints remains necessary.

## Start and verify

From the repository root, run:

```sh
docker compose --env-file /etc/hikyo/compose.env -f install/cloud/compose/nightly.yaml config --quiet
docker compose --env-file /etc/hikyo/compose.env -f install/cloud/compose/nightly.yaml up -d
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/readyz
```

Allow time for authenticated release preflight and upgrade checks. Readiness
stays false during maintenance. The image has no shell-dependent health check;
use the HTTP endpoints from the host or an external monitoring system.

Restrict TCP 8443 at the cloud firewall to intended clients, keep 8081 closed,
and point DNS at the host. Check `https://hikyo.example.com:8443/` from an
allowed client. Follow the [nightly installation guide](https://hikyo.app/docs/cloud-nightlies/)
for initial administrator creation and a restore drill. No administrator
credentials are created by this template. This uses native TLS, so no trusted
proxy configuration is supplied.

## Replace the image

Preserve the same bind mounts, root key, and enrollment on every replacement:

```sh
docker compose --env-file /etc/hikyo/compose.env -f install/cloud/compose/nightly.yaml pull
docker compose --env-file /etc/hikyo/compose.env -f install/cloud/compose/nightly.yaml up -d
```

For a pinned digest, deliberately update the image input before pulling. Do not
run a second SQLite writer or automatically roll back to an older image after
database writes. A failed or uncertain migration requires operator recovery;
never delete the journal to make readiness pass. `unless-stopped` restarts the
same selected image and does not authorize a database restore.

TLS files supply the initial managed configuration. Renew certificates through
Hikyo's managed configuration publish-and-Apply workflow; replacing the mounted
files alone does not reload the active certificate. Keep protected recovery
copies of TLS material alongside the other separately managed recovery inputs.
