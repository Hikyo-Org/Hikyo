# Security maintenance operator runbook

This is a draft capability for [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).
Do not enroll production installations until the key ceremony and the relevant
deployment acceptance are done. The
[design](../design/security-maintenance.md) names the exact contracts and gaps.
Nothing here authorizes a project release, publication or deployment.

## Provision the local owner

Use a signed Hikyo release containing this capability. Unstamped source builds
cannot replace production release trust with a downloaded key. Keep the checker
outside the serving process with persistent private state. On Linux provision
the example paths under `/etc/hikyo` and `/var/lib/hikyo-security-maintenance` as
root, without symlink ancestry or runtime-user write access. Configuration and
public root files may be mode 0644; private keys and journal files are 0600,
private directories 0700. Use the actual installed Docker path.

Choose the [native](../../install/security-maintenance/native.json.example),
[Compose](../../install/security-maintenance/compose.json.example) or
[Kubernetes](../../install/security-maintenance/kubernetes.json.example)
configuration. Save the selected, fully populated configuration as
`/etc/hikyo/security-maintenance.json`. The examples read the project feed at
`https://hikyo.app/security-feed/`. Install its root from the tagged source you
checked out, never from the feed it authorizes:

```sh
sudo install -m 0644 install/security-maintenance/security-feed-root.json /etc/hikyo/security-feed-root.json
```

That file exists only in releases cut after the
[key ceremony](#publisher-and-offline-operation); until then this step fails and
nothing enrolls. A mirror or an offline repository keeps the same root and floors,
so only the feed URLs change.

The defaults allow necessary feature upgrades and maintenance through signed
compatible routes. Set `policy.mode` to `manual` to disable automatic updates (a
scheduled `run` still restores a source whose target stopped before writing),
or choose `patch`/`minor` upgrade classes, an explicit `pin`, severity threshold
and UTC window. A restricted choice can leave an advisory blocked. No backport
is implied. `urgent_severity`, `critical` in the examples, lets a verified fix at
that severity skip the window; remove it to keep the window for every severity. It
overrides nothing else: pins, upgrade classes, maintenance consent, signatures and
recovery fences still bind.

## Native and Compose

Both recipes run the coordinator from the signed release package on the host,
which installs `/usr/bin/hikyo`. The binary installer's `/usr/local/bin/hikyo` is
not used: the maintenance unit and native upgrades name the packaged path, and
the ceremony stops with that explanation if the package is absent. Release 1.0.0
does not contain `security-maintenance`, and a host installed from it is not adopted:
it keeps the [manual procedure](manual-upgrades.md), and `enroll` refuses it and says
why. Only an installation created through these recipes from a release that contains
this feature can enroll. On Compose, both the host package and the image must be
that release or later. On native, the package must be, because source proof hashes
`/usr/bin/hikyo` against the release it came from. The
release client dials GitHub directly and ignores `HTTP_PROXY`, so a host that can
reach it only through a proxy cannot apply updates yet.

Release archives contain only `LICENSE`. Take the recipe files from the source
tag of the release you install:

```sh
export HIKYO_TAG=vX.Y.Z # the signed release you install
git clone --depth 1 --branch "$HIKYO_TAG" https://github.com/Hikyo-Org/Hikyo.git hikyo-src
cd hikyo-src
```

### The coordinator's own binary

An update replaces the server image, never the `/usr/bin/hikyo` that runs the
coordinator, so a flaw in the coordinator itself would stay. On by default for every
Compose host and for a Kubernetes or ECS coordinator on a separate host (it needs no
option, because the coordinator is part of what must be patched), a run that leaves
the installation healthy and idle compares the coordinator's release with the running
image's. If the coordinator is older, it fetches that exact release through the same
build-stamped release root and rollback floors as an update, and atomically replaces
its own file with that release's signed binary for this platform. Nothing restarts;
the next timer run is the new binary. A coordinator that is equal or newer, including
across the stable and nightly channels, is left alone, and so is one waiting on an
operation or an unhealthy journal. Native hosts update their own binary, and the
chart's in-cluster coordinator follows through its CronJob image.

The running release comes from the serving container, which a compromise controls,
so the coordinator trusts none of it by itself. It refuses a release that the latest
authenticated catalog withdrew (`status` carries `running_withdrawn`), and it requires
the image the deployment owner observed (not the container) to be in that release's
independently signed image inventory, the same check an update target passes. Only
`run` follows; `check`, `enroll`, `reconcile` and `status` never change the executable
or the CronJob.

It replaces only the running executable, and only when root owns it and every
directory above it, none is a symlink, and neither it nor those directories are
writable by others. Anything else is skipped with a log line. A failure, such as a
missing artifact, a read-only filesystem or a failed verification, never marks the
update failed and never blocks later maintenance. It appears only in the journal, as
`security maintenance coordinator executable was not moved to the running release`
(`journalctl -u hikyo-security-maintenance`), because `status` reports the update, not
this courtesy, and is retried every run. A later package upgrade simply overwrites the
file.

### Compose from nothing

This recipe is for a new installation. An existing, manually managed populated
database cannot be adopted by adding `HIKYO_UPGRADE_UNATTENDED`. See the
[unattended container runbook](unattended-container-upgrades.md) for custody and
reverse-proxy requirements. The serving container has no Docker socket.

The scratch restore of an update proves that a principal who manages identities
can mint and revoke a credential in an organisation. An installation with only its
first administrator has just the instance's own configuration organisation, which
no restore can use, so the restore records that no credential scope existed and the
update proceeds; so does one that stores no secret. Once a tenant organisation
exists, the restore needs a principal who manages identities in it (creating the
organisation grants that); without one the proof refuses. That refusal happens
before the database is written. The target reports it and stays up without serving,
this coordinator starts the source image again (Compose from its prepared snapshot,
Kubernetes by writing the prepared Deployment template back), and the attempt is
retried with the usual backoff. Each such attempt stops the service for a few
minutes, so after three of them at the same target the coordinator stops trying: see
[repeated failure](#repeated-failure-at-one-target). In this recipe, `hikyo admin create` inside the container needs
`HIKYO_UPGRADE_BUNDLE` and `HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY` set to the bundle
and key cached under `/var/lib/hikyo-upgrade/unattended`.

1. Install the [signed package](https://hikyo.app/docs/installation/#native-packages)
   for this release, plus Docker Engine with the Compose plugin and `jq`.

2. Create the private data, upgrade and root-key paths, owned by the container
   user. Generate the root key once and retain it through every update.

   ```sh
   sudo install -d -m 0755 -o root -g root /etc/hikyo /srv/hikyo
   sudo install -d -m 0700 -o 65532 -g 65532 /srv/hikyo/data /srv/hikyo/upgrade
   sudo install -d -m 0755 -o root -g root /var/lib/hikyo-security-status
   openssl rand -hex 32 | sudo install -m 0400 -o 65532 -g 65532 /dev/stdin /srv/hikyo/root-key
   ```

3. Install the recipe. `security-maintenance.json` must keep `image_repository`
   equal to the image's repository and name your actual Docker path, without
   symbolic links.

   ```sh
   sudo install -m 0644 install/security-maintenance/compose.yaml /etc/hikyo/compose.yaml
   sudo install -m 0644 install/security-maintenance/compose.json.example /etc/hikyo/security-maintenance.json
   ```

4. Write `/etc/hikyo/install.env`, owned by root and exactly mode 0600. Use the
   image digest published with the release. Every variable below is required.
   Exactly one line must start with `HIKYO_SERVER_IMAGE=`, because each proven
   update rewrites that line. If you edit it while an update runs, the update stays
   pinned and is reported as a conflict until the line holds the running target or
   the value it had before.

   ```sh
   sudo install -m 0600 -o root -g root /dev/null /etc/hikyo/install.env
   sudo tee /etc/hikyo/install.env >/dev/null <<'EOF'
   HIKYO_SERVER_IMAGE=ghcr.io/hikyo-org/hikyo@sha256:<published-image-digest>
   HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com
   HIKYO_TRUSTED_PROXY_CIDRS=<exact-reverse-proxy-CIDRs>
   HIKYO_UPGRADE_INSTALLATION_DIR=/srv/hikyo/upgrade
   HIKYO_SERVER_DATA_DIR=/srv/hikyo/data
   HIKYO_SERVER_ROOT_KEY_FILE=/srv/hikyo/root-key
   EOF
   ```

5. Start the workload and wait until its state is `healthy`. The first start may
   take several minutes. Point your HTTPS reverse proxy at `127.0.0.1:8080`.

   ```sh
   sudo docker compose --project-directory /etc/hikyo --project-name hikyo \
     --file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env up --detach
   sudo docker compose --project-directory /etc/hikyo --project-name hikyo \
     --file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env ps
   ```

6. Install the feed root as described above, then check, enroll and schedule. `check` reports
   `explicit_enrollment_required` until the ceremony enrolls this installation.

   ```sh
   sudo /usr/bin/hikyo security-maintenance check
   sudo sh install/security-maintenance/install-systemd.sh
   systemctl list-timers hikyo-security-maintenance.timer
   sudo /usr/bin/hikyo security-maintenance status
   ```

After each proven healthy update the coordinator atomically rewrites
`HIKYO_SERVER_IMAGE` in `install.env`, so the command in step 5 keeps selecting the
running release. No separate override file exists. Do not run it while `status`
shows an operation in progress. Keep this installation under one deployment
owner and do not run an unrelated image watcher against it.

The coordinator reads the feed from `hikyo.app`. The container reaches
`api.github.com`, `github.com` and `raw.githubusercontent.com` for signed release
artifacts on its first start and whenever a target is staged, so
preparation fails without that egress even under an offline feed. An admitted
replacement itself made no such request in rehearsal.

A failed or refused update leaves the running source able to restart: staging uses
its own cache, per target, under the upgrade directory.

If the coordinator dies after stopping the source, finish by selecting the staged
target:

```sh
sudo sh -c 'image=$(jq -er "select(.phase == \"applying\" and (.rolling_back | not)) | .target.image" /var/lib/hikyo-security-maintenance/deployment/deployment.json) &&
  sed -i "s|^HIKYO_SERVER_IMAGE=.*|HIKYO_SERVER_IMAGE=$image|" /etc/hikyo/install.env' &&
sudo docker compose --project-directory /etc/hikyo --project-name hikyo \
  --file /etc/hikyo/compose.yaml --env-file /etc/hikyo/install.env up --detach
```

This completes the update under the normal startup gate. It changes nothing when the journal is not
`applying`, or when a rollback has begun (`rolling_back`), which the next `run --once` finishes instead.
Do not pick an older image:
it refuses and leaves the database untouched. On Compose, Kubernetes and native the
coordinator does not leave this to you once `timeout_seconds` (at most 24 hours) has
passed since the update began, the next `run` stops the target and restores the
encrypted backup the preparation took, then starts the earlier release again (see
[Automatic rollback](#automatic-rollback-to-the-fenced-snapshot)). A target that
stopped before writing the database (`preparing` in the unattended journal) is put
back at once by its source image, which lifts the freeze and serves.

### Native

Native support requires the Linux/systemd/SQLite service using the package's
`/usr/bin/hikyo`. Host PostgreSQL/HA are refused. This is the recipe for a **new**
installation, from nothing. The package installs only `/usr/bin/hikyo`: it creates no
user, key, configuration or service, so the steps below do. Run them from the source
tag checkout above, with `systemd`, `sudo`, `jq`, `openssl` and `curl` present.

1. Install the [signed package](https://hikyo.app/docs/installation/#native-packages)
   of a release that contains this capability.

2. Create the runtime user, its data directory and the root key. Generate the root
   key once and retain it, with your own off-host recovery copy: it wraps the vault
   in step 6 and decrypts the database.

   ```sh
   sudo useradd --system --user-group --home-dir /var/lib/hikyo --shell /usr/sbin/nologin hikyo
   sudo install -d -m 0755 -o root -g root /etc/hikyo /var/lib/hikyo-security-status
   sudo install -d -m 0750 -o hikyo -g hikyo /var/lib/hikyo
   openssl rand -hex 32 | sudo install -m 0400 -o root -g root /dev/stdin /etc/hikyo/root.key
   ```

3. Write `/etc/hikyo/hikyo.env`, owned by root and mode 0600. Set the HTTPS origin
   and the exact reverse-proxy CIDRs of your deployment. Do not set
   `HIKYO_UPGRADE_*` here: the ceremony writes them.

   ```sh
   sudo install -m 0600 -o root -g root /dev/null /etc/hikyo/hikyo.env
   sudo tee /etc/hikyo/hikyo.env >/dev/null <<'EOF'
   HIKYO_DB=sqlite:/var/lib/hikyo/hikyo.db
   HIKYO_EXTERNAL_ORIGIN=https://hikyo.example.com
   HIKYO_TRUSTED_PROXY_CIDRS=<exact-reverse-proxy-CIDRs>
   HIKYO_SECURITY_MAINTENANCE_STATUS_FILE=/var/lib/hikyo-security-status/status.json
   EOF
   ```

4. Install the server unit. The upgrade adapter refuses any other shape of unit, so
   keep listener and TLS settings in the environment file, not on the command line.

   ```sh
   sudo install -m 0644 install/security-maintenance/hikyo.service /etc/systemd/system/hikyo.service
   sudo systemctl daemon-reload
   ```

5. Install the recipe and the feed root, as in
   [Provision the local owner](#provision-the-local-owner):

   ```sh
   sudo install -m 0644 install/security-maintenance/native.json.example /etc/hikyo/security-maintenance.json
   sudo install -m 0644 install/security-maintenance/security-feed-root.json /etc/hikyo/security-feed-root.json
   ```

6. Run the ceremony. Do not start `hikyo.service` first: a production service
   refuses to boot without a signed bundle and a pinned operator key, which the
   ceremony supplies. It also enables the service, so it comes back after a reboot.
   With `policy.mode` set to `manual` the ceremony does neither, so use the
   [manual procedure](manual-upgrades.md) to run such a host instead.

   ```sh
   sudo sh install/security-maintenance/install-systemd.sh
   ```

   `enroll` creates `/etc/hikyo/upgrade.json` if absent. It refuses, changing
   nothing on the host, unless the database is empty: a populated database without
   upgrade control objects is a legacy installation, which this ceremony does not
   adopt, and it holds the same lock as `sudo hikyo upgrade`, so the two cannot run
   together. Because no database exists and no operator key is pinned elsewhere, it
   then, in this order and stopping at the first failure:

   1. creates the root-wrapped vault `/etc/hikyo/upgrade-keys/operator.age` (root,
      0600, in a 0700 directory), with the same code `sudo hikyo upgrade` uses,
      and publishes its public key as `/var/lib/hikyo-upgrade/operator.pub` (0644);
   2. downloads and verifies the signed release of the installed executable, and
      refuses unless `/usr/bin/hikyo` is byte for byte that release's archive
      binary. This needs GitHub: the installed release's signed bundle goes to
      `/var/lib/hikyo-upgrade/bundle-*` and its verified archive stays in
      `/var/lib/hikyo-upgrader/downloads`;
   3. writes the pin, the bundle and the installation state directory to
      `/var/lib/hikyo-upgrader/runtime.env` (0600) and a systemd drop-in that loads
      it, last, so nothing names a key or bundle that does not exist;
   4. runs `hikyo migrate` as the runtime user, starts `hikyo.service` and waits
      until that exact binary answers ready;
   5. binds the vault to the instance the database was given at that first boot,
      then enrolls and enables the timer.

   Running it again changes nothing that exists. A failure leaves a state it
   resumes from: before step 3 no pin exists, and after it the service is started
   again without migrating twice, and a `hikyo migrate` that was killed or ran out
   of space is run again. A vault is never replaced, so one left by a failed attempt
   keeps its key, and it binds only to the installation that this ceremony booted
   for the release it pinned, never to an existing database.

An installation whose operator key is held elsewhere stays refused, and `enroll`
changes nothing on it: it stops with the explanation and its remedy. That is the
answer for a host installed from 1.0.0 on the [manual procedure](manual-upgrades.md),
which pins a key kept outside the host: it is not adopted and stays on that
procedure. Without this refusal its first update would stop the service and then
fail on the differing pin. An installation whose pin and vault came from `sudo hikyo upgrade` can enroll,
once its signed bundle is set as `HIKYO_UPGRADE_BUNDLE` under `public_directory`
and its archive is cached by the exact `sudo hikyo upgrade --release-identity
'<json>'` command the refusal prints. Its route is empty, so it only verifies and
caches. A legacy passphrase vault needs its existing explicit migration ceremony.

The coordinator checks after boot and about five minutes after the previous
operation, with bounded jitter; this is scheduling behavior, not guaranteed
remediation latency. Manual mode keeps the timer disabled. The legacy binary
installer never runs the ceremony. A long-lived `run` process is also available
when an installation-owned scheduler is more appropriate.

### Status in the web interface

Instance administrators see a read-only **Security maintenance** tab under
Instance settings. The serving application holds no deployment credentials and
never reads the coordinator's configuration or private state. Instead the
coordinator writes a public subset of its status to `status_file` (mode 0644)
after every status change, and the server reads that file on each request through
`HIKYO_SECURITY_MAINTENANCE_STATUS_FILE`. A failure to write it is logged and never
fails maintenance.

The file is world-readable on a host and, in a cluster, readable by everyone who can
read ConfigMaps in the namespace, so treat everything in it as public. It omits the
operation record, recovery journals and the other fences. Two fields do carry
values on purpose: `action` is the exact command an administrator must run, and its
`--confirm` value is a manifest digest or a recovery incarnation, which only proves
that the person knew what they were abandoning and is not a credential; `detail` is
the failure text, passed through the same value scrubbing as command output (quoted
values, URL credentials, `key=value` secrets and long token-shaped strings become
`[redacted]`) and cut to 1 KiB. The unscrubbed text stays in the private state that
`status` prints.

The Compose and native examples use `/var/lib/hikyo-security-status/status.json`,
a root-owned world-readable directory kept apart from the private state
directory. The coordinator creates it if absent, but Compose needs it before the
server starts, which is why step 2 creates it. `compose.yaml` mounts it read-only
and sets the variable. The native recipe's `/etc/hikyo/hikyo.env` already has
`HIKYO_SECURITY_MAINTENANCE_STATUS_FILE=/var/lib/hikyo-security-status/status.json`;
an installation made another way adds it and restarts `hikyo.service`. Without the
variable the tab reports that nothing is reported. A file that exists but cannot be
read or parsed is shown as unreadable, never as healthy, and metadata past
`known_until` is shown as stale. The chart's in-cluster coordinator sets this up
itself: it copies the file into a ConfigMap the server mounts as a directory, so the
file is a symbolic link that the server follows (the size bound and regular-file check
still apply), and kubelet refreshes it within about a minute. The external-host
Kubernetes recipe has no WebUI status: nothing mounts a status into the server, the
published Role has no `configmaps` rule, and `status_config_map` is refused outside the
in-cluster mode. Read that coordinator with `status` on its host.
Add `status_file` to an existing configuration only after the installed coordinator
release supports it: older builds reject unknown members. It does not change
enrollment or the recorded policy.

### Commands

`check` reports verified eligibility and never stages or applies. `enroll` records
local authority only for the exact healthy installation/recovery incarnation.
`run --once` reports current, blocked, stale or the verified application outcome;
on Compose, Kubernetes and native it rolls an unfinished replacement back once its
`timeout_seconds` has elapsed, and other owners stay pinned
(see [Automatic rollback](#automatic-rollback-to-the-fenced-snapshot)). `status` reads
durable state without network access and includes a derived `stale` field.

A failed command's bounded, scrubbed stderr reaches the error and so the journal
(`journalctl -u hikyo-security-maintenance`). Scrubbing removes quoted, labeled,
URL-embedded and long token-shaped values, but it is a filter rather than a proof,
so keep journal access limited to administrators.

## Direct singleton Kubernetes/PostgreSQL

Keep the existing unattended enrollment, separate live/scratch PostgreSQL
databases, persistent root key and POSIX-durable upgrade PVC contract. Use one
replica, `Recreate`, no HA/config-rollout enrollment and no server API token.
Provide all stage/inspect configuration through container environment, including
`HIKYO_ROOT_KEY_FILE`, because the stage command replaces the server arguments.
The Helm chart sets it when `upgrade.unattended.enabled` is true.
The root-key init container and server must reference the same authenticated
image. All matching Hikyo image-bearing containers are updated together. Init
containers re-run in the stage Pod while the source serves, so a Deployment whose init
container mounts a PersistentVolumeClaim without `readOnly: true` is refused.
Hikyo sets `tcp_user_timeout = 30000`, `tcp_keepalives_idle = 60`, `tcp_keepalives_interval = 10`
and `tcp_keepalives_count = 3` on each of its PostgreSQL sessions that can outlive a dead server, so the server drops a session
left by a server Pod that died with its network within about 90 seconds. Set the same on the
PostgreSQL server as well: it covers sessions Hikyo does not open, and a pooler between Hikyo and
the database, where Hikyo's settings reach only the pooler's session. Otherwise such a session can
hold off an automatic rollback for up to about two hours (see the failure table).

The coordinator talks to the Kubernetes API directly: no `kubectl` is installed or
needed, on a host or in the image. Run it in the cluster from the chart, or on a
separate qualified Linux host with a kubeconfig. Use the same OS/architecture as the
workload; local inspection refuses a mismatched executor before replacement.

### In the cluster, from the chart

Security maintenance is on by default in the chart, but it takes effect only when
`upgrade.unattended.enabled` is true. Unattended upgrades need a scratch database and a
state volume only you can provide, so they stay your explicit choice. With them off the
chart renders exactly what it did before this feature: no Deployment label, no live-image
lookup, no coordinator objects and no failure. With them on, install or upgrade as usual:

```sh
helm upgrade --install hikyo <chart> --set upgrade.unattended.enabled=true ...
```

The feed trust root ships in the chart from the tag, as `files/security-feed-root.json`.
`securityMaintenance.coordinator.feedRoot` overrides it, for example
`--set-file securityMaintenance.coordinator.feedRoot=install/security-maintenance/security-feed-root.json`.
The chart never fetches a root from the feed it authorizes. Until the
[key ceremony](#publisher-and-offline-operation) commits a root, a chart release has
none: with unattended upgrades on and no `feedRoot`, the coordinator renders nothing,
the install or upgrade still succeeds, and the install notes say that automatic security
updates are off and why. Upgrade to a release that ships the root, or set `feedRoot`,
to turn them on. With a root this renders:

- a CronJob (`*/15` by default, `concurrencyPolicy: Forbid`) running
  `hikyo security-maintenance run --once` as the chart's non-root user, with its own
  ServiceAccount. The serving Deployment keeps `automountServiceAccountToken: false`
  and gains no RBAC;
- a Role and RoleBinding. `resourceNames` scopes the Deployment, the staging Pod's
  get, delete and log, the CronJob and the status ConfigMap. RBAC cannot scope Pod
  `list` and `create`, `replicasets` `list` or `pods/exec`, because the source Pod's
  name is not known in advance, so those are namespace-wide. Keep Hikyo in a
  dedicated namespace and use admission policy as described below;
- a PVC for private state, a ConfigMap holding the configuration and the feed root,
  and an empty status ConfigMap;
- under `networkPolicy.egress.restricted`, a separate NetworkPolicy for the
  coordinator pods: DNS, the API server and public 443. Set
  `securityMaintenance.coordinator.egress.apiServer.to` to the API server address
  (the render fails without it while the coordinator is in effect). The server's
  policy is unchanged.

**Installing is your consent.** Installing the chart with unattended upgrades on
authorizes automatic replacement of the server image, under the configured `policy`, by
signed releases. `securityMaintenance.coordinator.enroll` is `true` by default and
renders a Helm post-install hook Job that runs `enroll` and retries until the server is
healthy; use `helm install --wait --timeout 20m`. Set `enroll=false` to withhold the
consent: the CronJob then runs and reports `explicit_enrollment_required`, and
`coordinator.enabled=false` renders no coordinator at all. `enroll=false` only stops the
hook; it does not revoke an enrollment already made, so an enrolled installation keeps
updating. Set `coordinator.enabled=false` to stop automatic replacement there. That,
turning maintenance or unattended upgrades off, or removing the only feed root
(`feedRoot` emptied with none shipped in the chart) deletes the coordinator's state
volume with the enrollment and any rollback journal, so do it only while `status` shows
no operation in progress; re-enabling then needs a new enrollment. The hook runs on
install only, because a failed hook on upgrade fails the release and an upgraded release
did not consent through this default. Set `securityMaintenance.coordinator.enrollOnUpgrade=true`
to also run it as a post-upgrade hook, for example to enroll an installation that
predates the feed, and use `helm upgrade --wait --timeout 20m`. An installation that is
already enrolled succeeds at once, healthy or not, restored or not, so the hook does not
block the upgrade you need during a recovery. The exception is a removed pin, which it
records again and which needs the cluster. A restored installation stays
blocked until you run `reconcile` ([below](#running-coordinator-commands-in-the-cluster)).

The Deployment's UID cannot be known before the first install, so `enroll` reads the
live Deployment once and keeps its UID in the private state (`kubernetes-identity.json`).
A later run against a Deployment with another UID is refused, and a repeated `enroll`
never re-pins. To adopt a replaced Deployment on purpose, remove that file as
described [below](#running-coordinator-commands-in-the-cluster), then enroll again. An
explicit `deployment_uid` in the configuration still takes precedence, which is what
the external-host recipe uses.

Inside the pod the service account is the authority, so the host-root requirement and
the root-owned-file rules relax for this mode only, and only as far as needed:

- Kubernetes projects the ConfigMap files as root, and the unprivileged coordinator may
  read root-owned public files. A root coordinator (native, Compose) is unchanged. The
  files are mounted with `subPath` because the readers refuse a ConfigMap directory's
  symbolic links, so a config edit reaches the next run's Pod, not a running one.
- Kubelet applies `fsGroup` to the PVC, making its root group-writable. The
  configuration's `volume_root` names that one directory and only it skips the
  writable-ancestor test; the state directory beneath it must still be mode 0700 and
  owned by the coordinator, and every ancestor above the volume is still checked.
- A volume root that any user can write, without the sticky bit, is refused: another
  local user could replace the state directory in it. Some provisioners create such a
  root, such as k3s local-path at mode 0777. Remove its world write permission (for
  example `chmod o-w` from an initContainer that runs as the volume's owner), or use a
  storage class that leaves the root group-writable. If neither is possible, setting
  `accept_world_writable_volume_root` (chart value
  `securityMaintenance.coordinator.acceptWorldWritableVolumeRoot`) accepts that risk
  explicitly.

After a proven healthy `run`, and only then, the coordinator patches its own CronJob to
the image the deployment owner observed, once that image is bound to its release's
signed inventory and the release is not withdrawn (see
[The coordinator's own binary](#the-coordinators-own-binary)), so the next run is the
release that was just verified. `check`, `enroll` and `reconcile` never write the
CronJob. The enrollment hook that runs on every `helm upgrade` succeeds for an enrolled
installation from its stored status, without the cluster or the coordinator lock, unless
its pin was removed for a re-pin. It still mounts the state volume, so on a read-write-once
volume attached to another node it waits for a running coordinator's Pod, which can outlast
`--timeout`; Helm then marks the release failed although the change applied. A refused
image patch, for example a removed Role rule, is logged and retried on every later run; it
never marks the update failed.

#### Deliberate upgrades and the chart image

`helm upgrade` keeps the live server image once the coordinator has replaced it: the
chart reads the live Deployment with `lookup`, so `helm template` and a client-side dry
run render `image.digest` instead. The coordinator's CronJob keeps its own live image
the same way, because the live Deployment can run a target the coordinator has not
proven while an update is failing; a release without a CronJob yet takes the server
image. `helm upgrade --set image.digest=<newer>` therefore
succeeds and leaves the server where it was. The upgrade output says so: `NOTES.txt`
prints a `WARNING: image.digest was NOT applied` block with the running and requested
images whenever they differ.

To move the server on purpose, for example to a release the feed does not select, run
that one upgrade with an explicit override and drop it afterwards:

```sh
helm upgrade hikyo <chart> --reuse-values \
  --set image.digest=sha256:<signed-image-digest> \
  --set securityMaintenance.keepLiveImage=false
helm upgrade hikyo <chart> --reuse-values --set securityMaintenance.keepLiveImage=true
```

An override value is the smallest honest way to do this. Turning
`securityMaintenance.enabled` off would drop the Helm opt-in label while the coordinator
runs, and editing or removing the Deployment's
`hikyo.dev/security-release-manifest` annotation changes the pod template and rolls the
server. The override changes nothing else, and the server's own unattended upgrade
handles the new image. The coordinator never goes backwards by itself, so check the
direction of the move first and treat an older image as manual recovery under the
[failure rules](#failure-and-recovery). Pass the second command, or leave
`keepLiveImage` out of later values files, because `--reuse-values` would otherwise keep
the override.

#### Running coordinator commands in the cluster

The coordinator image has no shell and `kubectl create job --from=cronjob/...` cannot
change its arguments, so every command in this runbook that says `sudo hikyo
security-maintenance ...` (`status`, `reconcile`, `enroll`, `check`) runs as a one-off
Job derived from the CronJob: same ServiceAccount, state volume, configuration and
NetworkPolicy labels, different arguments. The Job must run where the PVC can attach, so
if the volume is read-write-once on another node it waits for the CronJob's pod to end.

```sh
export NS=hikyo CRON=<release>-hikyo-security-maintenance  # kubectl -n "$NS" get cronjob
hikyo_coordinator() {
  kubectl -n "$NS" get cronjob "$CRON" -o json |
    jq --arg args "$*" '{apiVersion: "batch/v1", kind: "Job",
      metadata: {generateName: "hikyo-security-maintenance-op-"},
      spec: (.spec.jobTemplate.spec | .backoffLimit = 0 | .ttlSecondsAfterFinished = 3600
        | .template.spec.containers[0].args = (["security-maintenance"] + ($args | split(" ")) + ["--config=/etc/hikyo/security-maintenance.json"]))}' |
    kubectl -n "$NS" create -f - -o name
}
```

The arguments must not contain spaces. Run a command and read its output, which is the
same JSON the host command prints:

```sh
job=$(hikyo_coordinator status)
kubectl -n "$NS" logs -f "$job"
job=$(hikyo_coordinator reconcile --confirm VALUE)   # VALUE is the action the status prints
kubectl -n "$NS" logs -f "$job"
```

Re-pinning the Deployment identity means deleting `kubernetes-identity.json` from the
state volume, and the coordinator image has no `rm`. Run the same derived Job with a stock
image that has one, pinned by digest, in place of the coordinator, then enroll again:

```sh
BUSYBOX=docker.io/library/busybox@sha256:<digest of an image you trust>
kubectl -n "$NS" get cronjob "$CRON" -o json |
  jq --arg image "$BUSYBOX" '{apiVersion: "batch/v1", kind: "Job",
    metadata: {generateName: "hikyo-security-maintenance-repin-"},
    spec: (.spec.jobTemplate.spec | .backoffLimit = 0 | .ttlSecondsAfterFinished = 3600
      | .template.spec.containers[0] |= (.image = $image | del(.args)
        | .command = ["rm", "-f", "/var/lib/hikyo-security-maintenance/state/deployment/kubernetes-identity.json"]))}' |
  kubectl -n "$NS" create -f -
hikyo_coordinator enroll   # pins the live Deployment again; already enrolled, so nothing else changes
```

After every run the coordinator copies its public status into the status ConfigMap,
which the server mounts read-only (see [Status in the web interface](#status-in-the-web-interface)).
A failure to copy is logged and never fails the run.

### On a separate host with a kubeconfig

Keep a separate protected kubeconfig with a fixed context, namespace and Deployment
and staging Pod name in the configuration. Do not put that
kubeconfig/token in the server container. This mode has no CronJob to follow and no
WebUI status: the Role below has no `configmaps` rule and `status_config_map` is refused
([Status in the web interface](#status-in-the-web-interface)). Bind its identity to
exactly this Role:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: hikyo-security-maintenance
  namespace: hikyo
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "update"]
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["list"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["list", "get", "create", "delete"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create", "get"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
```

RBAC cannot name-scope `create` and does not restrict fields. This identity can
therefore create any Pod in the namespace, including one that mounts its Secrets,
and change any field of a Deployment. Keep Hikyo in a dedicated namespace and treat
the kubeconfig as that namespace's administrator. Use admission policy to pin
created Pods to the configured staging name and to limit the Deployment `update`
to Hikyo image fields and the `hikyo.dev/security-release-manifest` template
annotation. The adapter uses `update` with the prepared resourceVersion, not patch.
The same applies to the in-cluster Role above.

The target staging Pod copies the admitted workload's required mounts and
configuration on the source node, without a controller token or published
service. It prepares every route artifact before the source is replaced.
Success requires Pod readiness, an independently derived runtime image digest,
and exact local applied-state/schema evidence. A Deployment update or `/readyz`
alone does not establish the outcome.

Flux (including HelmRelease), Argo and other GitOps resources refuse. Do not remove
ownership labels/annotations to evade this refusal. A dedicated GitOps adapter
remains work for the parent issue. Use direct installed manifests, or the chart
opt-in below, only when no other desired-state controller owns the workload.

To use a release installed by the Helm CLI, set `upgrade.unattended.enabled=true`;
`securityMaintenance.enabled` is true by default. This adds the Deployment label
`hikyo.dev/security-maintenance: helm`. The adapter accepts Helm ownership only
with that label and both `meta.helm.sh` release annotations, so a chart rendered
and applied by other tooling refuses. The example configuration's `container`
value matches the chart. Set `deployment` from the installed object; the chart names
it `<release>-hikyo`. Leave `deployment_uid` out and `enroll` pins it, or set it from
the installed object to pin it yourself.

The chart uses `lookup` to read the live Deployment. Once the coordinator has
replaced the image, marked by the `hikyo.dev/security-release-manifest` template
annotation, `helm upgrade` keeps the running image and ignores `image.digest`.
Before that first replacement, `image.digest` still applies. The upgrade prints a
warning when it kept an image other than the requested one, and
`securityMaintenance.keepLiveImage=false` applies `image.digest` on purpose; see
[Deliberate upgrades](#deliberate-upgrades-and-the-chart-image).

- `lookup` needs a live cluster. `helm template` and a client-side
  `helm upgrade --dry-run` render `image.digest`, not the running image. Preview
  with `helm upgrade --dry-run=server`.
- `helm rollback` restores the image stored in that revision, and an upgrade with
  `securityMaintenance.keepLiveImage=false` (or `securityMaintenance.enabled=false`
  when no coordinator is enabled) returns to `image.digest`. Both can start an
  older image, which the coordinator never does on its own. Treat them as manual
  recovery under the [failure rules](#failure-and-recovery).
- An upgrade between preparation and application changes the resourceVersion, so
  application refuses and the next run prepares again.

## ECS/Fargate singleton/PostgreSQL

The [ECS configuration](../../install/security-maintenance/ecs.json.example)
contains illustrative resource identifiers and a service-creation timestamp.
Replace every resource identity with the installation's verified values. The
configured AWS CLI must be a protected Linux AWS CLI v2 executable. The profile
and its credentials belong to the customer-side controller, never the server,
catalog or project maintainers.

This draft requires one directly owned Linux Fargate service/container with
PostgreSQL and a separate scratch database referenced through Secrets Manager,
durable EFS upgrade state, a separate read-only EFS root-key mount and writable
ephemeral `/tmp`. Keep the controller and workload architectures equal. Configure
the fixed `hikyo.dev/security-maintenance-owner` service tag. An existing ALB
target group must probe the configured operational port's `/readyz` and match
exactly HTTP 200. Existing service health grace must cover the operation budget.
Container liveness must use `security-maintenance health --liveness` so
maintenance does not trigger a health-based task replacement. The adapter
checks these settings and refuses unsupported layouts instead of changing them.

Prepare registers only a task definition whose accepted execution fields are
unchanged except the independently authenticated image. Owned isolated stage
and database-inspection tasks use the same persistent installation state. Their
single structured observation is read from their exact task-scoped CloudWatch
stream; those customer-owned logs are not a project update registry. The serving
task's actual image digest and ALB readiness remain separate evidence.

Application scales the owned service to zero, proves all source service tasks
stopped, and then selects the exact prepared definition with desired count one.
The startup gate still owns backup, scratch restore, migrations and recovery.
ECS stays pinned: no old task definition or database is restored automatically after uncertainty.
Only provably owned stage/inspection tasks and unused temporary definitions are
cleaned. A CLI failure that proves the request was never sent clears the
registration or task intent. Any other failure, including a refusal, keeps it,
because the CLI retries internally and its first attempt may have committed. The
next run reads the job task back by its `startedBy` value in both desired states, or
the newest task definition revision by its registration digest. It stops and clears
an owned job, retires a matching revision, and drops the intent only when those
reads come back empty. A missing task or an empty listing counts only five minutes
after the job began, so a refusal can delay the next run by that long. A failed or
truncated read keeps the intent and the run refuses until a later one succeeds.

AWS UpdateService has no conditional resource-version update. Enforce exclusive
service ownership with customer IAM and operating policy; the local lock and
snapshot recheck cannot prove that another principal will not race it. Existing
autoscaling, CloudFormation/other desired-state ownership, deployment rollback
or multiple replicas refuse. The draft does not alter IAM, EFS, ALB, networking
or health settings to make an installation qualify. Real AWS acceptance remains
required before advertising this provider contract.

## Publisher and offline operation

`hikyo security-maintenance feed --help` lists `init-root`, `advisory`, `publish`
and `refresh`. Private key bytes never belong in command arguments, catalog files
or logs. Store the root key apart from the online keys. The custody chosen is in the
[ADR](../adr/security-maintenance.md#key-custody): a 1-of-1 root with encrypted offline copies,
expiring one year after each ceremony. Changing the common feed
is not a customer outcome acknowledgement and does not give the project a fleet
census. Staged rollout through `--rollout-minutes` spreads adoption locally, with no
reporting, so a bad target can be withdrawn before most installations take it.
Project-controlled canaries and any optional telemetry require their own
explicit arrangements.

Clients read only `https://hikyo.app/security-feed/metadata/` and `.../targets/`,
which may serve cached files for up to ten minutes. Moving that site off GitHub Pages
changes no installation: serve the same files at the same paths, without redirects.

`.github/workflows/security-feed.yml` publishes there and stays inert until the
repository variable `HIKYO_SECURITY_FEED_ENABLED` is `true`. Its `security-feed`
branch is the publisher's private store, never read by a client. Each publish
re-serves the last built docs site with the new feed, and `docs.yml` includes the
store in every docs build, so neither deployment can drop the other. Before enabling
it, maintainers:

1. Run `feed init-root` offline, with `expires` one year ahead. Commit the result as `metadata/1.root.json` on a new
   `security-feed` branch that GitHub Actions may push to, with force pushes and
   deletion blocked, and as `install/security-maintenance/security-feed-root.json` on
   the release branch, so the next signed tag ships it. The ceremony also commits the
   same root to `chart/hikyo/files/security-feed-root.json`, byte-identical to that
   file; the chart tests refuse a difference.
2. Set `SECURITY_FEED_SNAPSHOT_KEY` and `SECURITY_FEED_TIMESTAMP_KEY` in environment
   `security-feed-online`. Set those two and `SECURITY_FEED_TARGETS_KEY` in
   `security-feed-publish`, with required reviewers. Allow only `main` on both
   environments: a workflow edited on another branch could otherwise read the online
   keys and freeze the feed.
3. Set the repository variable `HIKYO_SECURITY_FEED_PUBLISHER_VERSION` to a signed
   stable release that contains these commands. It is pinned, not the latest, so
   withdrawing or revoking a bad latest release cannot stop the withdrawal itself. If
   that pinned release is ever revoked, point the variable at another one.
4. Enable the feed variable and run the workflow with no inputs. This publishes the
   first empty catalog, and the daily job needs that repository. The job fails unless
   `hikyo.app` then serves the new version with no redirect.

### A critical flaw was found

An advisory applies to the release profile of its `--fixed-version`. Stable and
nightly are separate: a flaw affecting both needs a run for each, with the same ID,
or nightly installations will read `current`.

Cut the fixed release through the normal ceremony and wait until `release/trust` on
`main` authorizes it. Then run this, and have an environment reviewer approve it:

```sh
gh workflow run security-feed.yml --repo Hikyo-Org/Hikyo --ref main \
  -f advisory_id=CVE-YYYY-NNNN -f summary='One line' -f severity=critical \
  -f components=hikyo -f fixed_version=X.Y.Z -f platforms=linux/amd64,linux/arm64
```

Exact binary and image targets come from the signed release, and fixes are computed
as every active advisory the target is not affected by. Add `-f introduced=`, the
impact booleans `-f features=true`, `schema`, `api`, `configuration`, `maintenance`,
or `-f rollout_minutes=` when they apply. A repeated `advisory_id` replaces that
advisory, and that is how its fixed version moves past a bad release.

To pull a bad release, add `-f withdraw_target=X.Y.Z`, or send it alone. Withdrawals
need no release access, because publication verifies only targets that are new or
changed against the previous authenticated catalog. `-f withdraw_advisory=ID` retracts
an advisory.

Without the workflow, run the same steps from a checkout of the branch whose
directories are mode 0700, using a stamped binary. `DIGEST` is the catalog hash in the
current targets metadata, `NEXT` is one above the timestamp version, and the expiries
are RFC3339 with timestamp <= snapshot <= targets <= catalog expiry. `ROOT` is the
latest published root, as in the workflow. Omit `--previous-catalog` for the first catalog.

```sh
ROOT=$(find "$FEED/metadata" -maxdepth 1 -name '*.root.json' | sort -V | tail -n 1)
hikyo security-maintenance feed advisory --output "$WORK/catalog.json" --cache "$WORK/cache" \
  --previous-catalog "$FEED/targets/security/$DIGEST.catalog.json" \
  --id CVE-YYYY-NNNN --summary 'One line' --severity critical --component hikyo \
  --fixed-version X.Y.Z --platform linux/amd64 --platform linux/arm64
hikyo security-maintenance feed publish --catalog "$WORK/catalog.json" \
  --root "$ROOT" --output "$FEED" --cache "$WORK/cache" --version "$NEXT" \
  --targets-expires "$T" --snapshot-expires "$S" --timestamp-expires "$M" \
  --targets-key "$KEYS/targets.pem" --snapshot-key "$KEYS/snapshot.pem" --timestamp-key "$KEYS/timestamp.pem"
git -C "$FEED" add -- metadata targets
```

Upload only `metadata/` and `targets/`, and never delete a versioned file. Immutable
files must be visible before `metadata/timestamp.json`. `publish.lock` is a local lock
in the output directory and is not part of the repository. Publish refuses a lower or
equal catalog sequence with different bytes. Publish and refresh then run the real
client against the output, so a repository a client would reject fails before upload.
If a publication fails before writing `metadata/timestamp.json`, no client can
reach the versioned files it wrote (for example `metadata/N.targets.json` and
`metadata/N.snapshot.json`). Rebuilt files differ whenever the signature, expiry or
catalog changes, and publish refuses to replace them, so remove them before retrying
with `git -C "$FEED" clean -f -- metadata targets`, which deletes only files that
were never committed. The workflow starts from a fresh clone each run, so only manual
runs leave them.

If a run fails, start a fresh dispatch or use "Re-run all jobs". "Re-run failed jobs"
cannot find the earlier attempt's binary. A GitHub API rate limit is a known cause
during an incident, since release verification calls it anonymously from a shared runner.

To rotate an online key, commit the next root from `feed init-root` to the store branch as
`pending/N.root.json`, never under `metadata/`: every docs deployment serves `metadata/`,
and clients would adopt the root and refuse the feed the old keys signed. Replace the
environment secrets and dispatch with `-f rotating_root=true`. The daily refresh fails until
that run has published under the new root. Clients keep the old feed in between, for up to
its three-day timestamp expiry.

Renew the root ahead of its expiry, for example a month before, through the same steps: a new `feed init-root` with
the previous root as `previous_root_file` and a new one-year `expires`. Targets never outlast the
root, so the daily job starts failing 14 days before the root expires. An expired root stops the
feed: installations report stale and receive no fixes until the renewal is published.

### Keeping the feed alive

The daily job runs `feed refresh`, which re-signs snapshot and timestamp with online
keys around the existing targets metadata. It has no targets key input and refuses
any expiry beyond the targets expiry. Timestamps last three days, so one missed run
does not make installations stale. Catalog payloads last at most 90 days. The daily
job fails visibly when targets expire within 14 days. Renew by running the workflow
with no inputs, which carries every entry over at the next sequence.

A stopped publisher shows up only as every installation going stale, so
`security-feed-freshness` checks the published `timestamp.json` every six hours, fails under
36 hours left and opens one issue. It runs on the same GitHub as the publisher and would stop
with it, so also run `scripts/ci/check-security-feed-freshness.sh` from somewhere outside GitHub
before activating the feed. It needs only `curl` and `jq`, and exits non-zero when not fresh.

For offline mode, set `policy.mode` to `offline` and `feed.offline_directory` to
an imported repository with `metadata/` and `targets/`. Use the same approved
root and persistent local cache. Imports use the full TUF checks, including
expiry; a copied catalog alone is insufficient. Offline mode does not bypass
independent artifact authorization or install artifacts that were never staged.
It may still need release/artifact access during preparation; fully disconnected
end-to-end release-bundle import is not yet an accepted deployment contract.

## Failure and recovery

Feed loss, expiry, clock rollback, signature failure or a policy block leave an
already admitted healthy application running. Preserve the exact reason and
last verified/expiry fields. Do not label stale knowledge current.

After a deployment mutation or possible schema write, preserve the coordinator,
deployment and native/unattended journals, root key, custody and backup. Restore
or an installation/recovery-incarnation change invalidates enrollment. Do not
delete status to reacquire update authority, restart an older image or restore
the database automatically. The only older image that is safe to start is the
journal's own source while the unattended journal is still `preparing` and the
database has never been written; the source's boot check refuses it in every other
state. Finish the exact pending target through its owning
upgrade/deployment recovery procedure; the checker observes completion of that
original target and cannot replace uncertain work with a newer advisory target.

### Target stopped before writing the database

A target whose own preparation fails before `write-intent` marks its journal and
stays up without serving, so a restart cannot retry and cross into the write before
the source is back. Any later `run` of the coordinator that observes this report,
including under a manual policy or an unavailable feed, restores the source, not only the run that applied it, so an evicted CronJob pod, an
elapsed `timeout_seconds` or one failed API call is repaired by the next run. `run`
does this even when the feed cannot be reached or its metadata has expired, because
putting the source back is not an update; `check` never changes the workload. The restore writes only over the exact
state the update produced, and the source's own boot check refuses unless the
database is untouched.

These failures leave the instance down until a later `run` restores the source, which the rows
below say when it can. Where it cannot, the pin stays and a person must act, as the table lists. `status`
shows `operation_requires_reconciliation`, or `installation_unavailable` when the target or
service is not running to answer, `stale` while the feed is unavailable, or `manual_mode` under
a manual policy. The last three print no `action` unless a rollback failed, which always sets one; a wrong `--confirm` value prints the right one:

| Owner | Case | Your exit |
| --- | --- | --- |
| Compose, Kubernetes | The target never reports: its image cannot be pulled or its container cannot be created, it was killed (out of memory, node loss) before it wrote its report, its configuration is invalid so the server exits before preparation, or it has no journal yet. The coordinator cannot tell a dead target from a slow one, so it waits for `timeout_seconds`. After that the next `run` reads the control row, stops the target and restores the fenced backup, and no `sudo` or manual step is needed. It needs you only if that read or the restore is refused (see the next rows) or while the deadline has not passed. | Wait for the deadline and run `run --once`. To end it earlier, look at the container or Pod and its logs; if its journal never left `preparing` (`/srv/hikyo/upgrade/unattended/operation.json`), set the previous image back (`HIKYO_SERVER_IMAGE` in `install.env` and `up`; on Kubernetes the Deployment image, and with the chart a `helm upgrade` that also sets `securityMaintenance.keepLiveImage=false`, otherwise the live image is kept). Then `reconcile --confirm VALUE`. |
| Compose, Kubernetes | The restore keeps failing: another writer changed the Deployment or Compose file, or the source does not come back healthy. | Repair the cause, then as above. |
| Compose, Kubernetes | The deadline passed, but the rollback is refused: the live database no longer belongs to the journal's instance or incarnation, the control row is `healthy` at another release, the backup does not authenticate against its receipt, or operator custody holds an open journal. The pin stays because the target may have served. | Read the refusal in `detail`, then finish the target by selecting its staged image, or restore by hand from your own backup. Then `reconcile --confirm VALUE`. |
| Kubernetes (PostgreSQL) | The rollback is refused with `another session is connected to the database`. A session of a target that died with its network (a deleted Pod, a lost host) is still open on the database server, or a connection pooler between Hikyo and the database holds sessions of its own. The pin stays, every scheduled run tries again, and the instance stays down meanwhile. Hikyo's own sessions are dropped within about 90 seconds; with the operating system's TCP defaults the server drops any other dead session after about 15 minutes when it was sending, and after about two hours when it was idle. | Prevent it: in the database server's configuration also set `tcp_user_timeout = 30000`, `tcp_keepalives_idle = 60`, `tcp_keepalives_interval = 10` and `tcp_keepalives_count = 3`, which drop a dead session within about 90 seconds. To end the wait now: confirm no Hikyo server runs, find the session in `pg_stat_activity` (`client_addr` names the dead Pod or host), end it with `SELECT pg_terminate_backend(pid)`, then `run --once`. |
| Compose | The deadline passed and the control row cannot be read, because a hung target holds SQLite's lock (`detail` shows `locking protocol`). Nothing proves the target did not pass the final gate, so it is left running. | Look at the target's logs. If it is hung rather than serving, stop its container (`docker compose ... stop server`), then `run --once`: with the lock released the run reads the row and rolls back. Not rehearsed. |
| Compose, Kubernetes | No later coordinator run happens (CronJob or timer removed or suspended). | Run `run --once`, or set the image back yourself. |
| ECS | Every target that reports a stop before writing. ECS has no automatic restore. | Set the previous task definition back, then `reconcile --confirm VALUE`. |
| Native | The stop itself failed, or the source service does not restart healthy. | Finish with `sudo hikyo upgrade`, which reconciles the host journal, then `reconcile --confirm VALUE`. |
| Native | The rollback is refused: the live database no longer belongs to the journal's instance or incarnation, the control row is `healthy` at another release, the backup does not authenticate against its receipt, the cached source binary does not match the recorded digest, or the host has no source record for the journal. The pin stays because the target may have served. | Read the refusal in `detail`. At a journal before `rolling-back`, finish the target with `sudo hikyo upgrade`, then `reconcile --confirm VALUE`. A journal at `rolling-back` is finished by a coordinator run, and the service stays stopped and fenced until then; only a service a person already put back by hand, serving and unfenced, is released by `reconcile` instead. If the cached source release is missing or fails verification, run the `sudo hikyo upgrade --release-identity '...'` command that `detail` names: it fetches that release again and changes nothing else. Then run `run --once`. The other refusals at `rolling-back` (an authenticated source binary that differs from the journaled one, a backup that does not authenticate, a database of another instance or incarnation, no source record) do not clear by retrying: as with a `restore-required` journal, this needs explicit operator recovery. Keep the journal, the backup and the state directory, leave the service fenced, and restore the host from your own backup. No command lifts the fence for you. |
| Native | The coordinator died after stopping the service and before restarting it. The journal is `preparing` and the systemd fence is on. Until `timeout_seconds` has passed the next run refuses to adopt the unfinished host operation and cannot observe the stopped service. | Wait for the deadline and run `run --once`: the service is stopped under the fence with a `preparing` journal, so the run restores the source, restarts it and releases the journal without `sudo hikyo upgrade` (that command would repeat the proof and upgrade rather than restart the source). If the restore is refused, the service stays stopped until its cause is fixed (the error is in `detail` and the `runtime-*.log` files in the state directory); there is no supported way to lift the fence by hand while the journal is unfinished. |

A journal at `write-intent` or later, or a control row that has moved, is not released as
untouched in any owner. It ends only by the automatic rollback below, which restores the fenced
snapshot, or by a person when that rollback is refused (a target that is `healthy` at another
release, a backup that does not authenticate, or ECS, which has no rollback).

Native hosts restore their own source automatically in two cases. First, the
security coordinator stopped the service, and the backup, restore proof or evidence
failed while the journal was `preparing` and the database was still the untouched
source. It restarts the source service, proves it healthy, lifts the maintenance
fence, clears the journal, records the reason in `preparation-abandoned.json` and
reports a refusal that counts toward the limit below. If the restarted service is not
the untouched source, or its fence cannot be lifted, the service is stopped again and
stays under maintenance. The restore keeps the bundle the source boots from, which on
a host installed from nothing is named only in `runtime.env`. Second, a failure after
the database may have been written is rolled back to the fenced snapshot, as
described next.

### Automatic rollback to the fenced snapshot

On Compose, Kubernetes and native an update that does not finish is rolled back
instead of leaving the instance down. Three things start it, each only under `run`, never
`check`: the target's own `restore-required` report, any other report that the
target aborted, and the deadline, `timeout_seconds` after the update began (at most 24 hours). The deadline also applies to a pinned operation on
a later run, under a manual policy and while the feed is unreachable or expired,
because putting the source back is not an update. A target that is healthy at the
release the update chose completes, however late.

The coordinator stops the target and proves it gone. A one-shot of the target's own
executable then authenticates the encrypted backup taken under the maintenance
fence against its receipt, restores it, verifies it and deletes the attestation, and
the owner writes the earlier release back. The restore refuses unless the live
database is still the one the fence protected: no tenant write can have committed
while `maintenance` was set, so nothing is lost. A database that a target already
served from, or that belongs to another instance, stays pinned for you.

A rollback counts as a failure that reached the running service (see below).
`status` then reports that the earlier release is serving and the security fix is
not applied, in `retry_cause` and, where nothing else owns it, `detail`. ECS is not
rolled back automatically.

**Native.** The service is stopped by the host fence, so the backup taken before the
first write is the whole of the instance's data. A target that fails after the schema
write, for instance because its server never answers ready, is rolled back in the same
run: the coordinator authenticates the backup, restores it over the live database, puts
the earlier binary back at `/usr/bin/hikyo`, proves it is the recorded release, starts
it and lifts the fence. The instance, its recovery incarnation, its credential and
restore epochs and its data are those of the backup, and sessions that existed when the
backup was taken keep working. If the coordinator itself dies after the write, the next
timer run does the same once `timeout_seconds` has passed, under a manual policy too.
`timeout_seconds` is optional on native and defaults to 30 minutes (0 to 86400); set it
above the longest migration the installation needs. Before the host starts a rollback
it needs the earlier release's verified archive in `/var/lib/hikyo-upgrader/downloads`,
which `enroll` and every update keep until the operation finishes. A rollback never
runs from a feed or network lookup. It writes only over the exact state the update
produced: if the live row belongs to another backup or has passed the final gate at
the target, it is refused and the operation stays pinned. The deadline reads that row before
it stops anything: a target `healthy` at another release is left running and pinned, and
`status` reports that a person must decide. If the row cannot be read, a native host stops the
target only while its journal shows no gate was crossed. Compose reads the row through a one-off
container of the installed image and Kubernetes through a short stage Pod of the earlier release on the
node that served it; the chart's coordinator Role already allows both. A rollback that already
stopped the target leaves a durable `rolling_back` marker in the deployment journal, so an interrupted
rollback is finished on the next run even when no row can be read, as during a restore. Without the
marker, Compose and Kubernetes stay pinned until the row can be read, and ECS stays pinned. A row at
another installation or `healthy` at another release keeps the pin, marker or not. A rollback that restored the
snapshot reports `operation_rolled_back` and `run` exits non-zero under every policy, including `manual`
and a stale feed. A restore that found the source untouched is not reported as a rollback; only the
rollback one-shot's own report says `not-needed`, and `status` never does. Under a manual policy the
reason is `operation_rolled_back` and `run` returns the error, so the exit status is non-zero (the
command maps no distinct code), not `manual_mode`. A failed
rollback shows its `action` in `status` under every policy.

A rollback that ends a run ends it there: the same run selects and applies nothing, and the
disruptive-attempt count keeps the rollback, so the next run (after any retry delay) decides again.
A rollback that was interrupted and finished by a later run, including a native one whose SQLite file
was already gone when the crash happened (the host tolerates a missing database only while its journal
is at `rolling-back`), is counted and reported as a rollback too, as is a release that finds an
earlier run already restored the snapshot. A native journal that is still `preparing` while the
service is serving and the fence is off is released without `sudo`: at once by `reconcile --confirm`, or
by a `run` once the one-hour settle window after the operation began has passed.
The Compose and Kubernetes `rolling_back` marker is written only after the target is proven stopped,
so a target that might still be running never reads as stopped. A multi-hop row is read by the
installed release's `security-maintenance inspect --control-row`, which checks neither the binary nor
the schema.

### Repeated failure at one target

Each failure that reached the running service counts against the target, a rollback
to the fenced snapshot included. After the
third, the coordinator makes no further automatic attempt at it, whatever the
maintenance window or `urgent_severity` say, and `status` and the WebUI report a
protection gap:

```text
phase blocked, reason retry_exhausted
detail automatic attempts at 1.3.1 stopped after 3 failures that reached the running
       service; last failure: ...
action hikyo security-maintenance reconcile --confirm <manifest digest of the target>
```

Fix the cause shown in `detail`, then run the action. It clears the count and the
next run tries again, three more times at most. A different target selected by the
catalog starts a fresh count by itself. Failures before the workload is touched, such
as a registry outage during preparation, back off without stopping automation. A
refusal before any change to the service, such as an eligibility window that closed,
never counts. The limit is fixed, not configurable. The gap is reported once the
target is eligible: a target held back by a closed maintenance window still reports
`maintenance_window` until the window opens, and an urgent one is reported at once. The scratch proof is not run before the stop,
because it needs a restore of the backup and the credential lifecycle in that
restore, which cannot be done read-only against the live database.

If the source is still healthy at its original generation an hour after an attempt
began, the pin releases by itself and the target retries with backoff. Otherwise,
once the installation is healthy, `status` prints the exact way out in its `action`
field:

```sh
sudo hikyo security-maintenance reconcile --confirm VALUE
```

That is the host form. With the chart's in-cluster coordinator there is no host and no
`sudo`: run the same arguments through `hikyo_coordinator` as described in
[Running coordinator commands in the cluster](#running-coordinator-commands-in-the-cluster),
for example `hikyo_coordinator reconcile --confirm VALUE`. The `action` that `status` and
the WebUI print is the platform-neutral command without `sudo`.

For a pinned operation `VALUE` is the abandoned target's manifest digest. For a
restored installation it is the new recovery incarnation, and running it re-enrolls
the restored copy. It refuses an unhealthy installation and any other value. Native
hosts also need their upgrade journal finished through `hikyo upgrade`, except a journal at
`rolling-back`, which the next coordinator run finishes, or `reconcile` releases once a person has put the
source back by hand and it serves unfenced.

`run --once` exits nonzero while an applicable advisory stays blocked by anything
other than an expected wait (window, staged rollout, manual mode, or a retry backoff
before its third failed attempt), and while a pinned operation waits on a command for
a person, so the unit shows as failed in `systemctl --failed` and ordinary host monitoring. The timer keeps firing
after a failed run. Failed staging output reaches the journal after exact and
pattern redaction of secrets.

Follow [manual upgrades](manual-upgrades.md) and the canonical
[signed-upgrade contract](../adr/signed-upgrade-compatibility.md) for explicit
post-write recovery. Cloud/provider support and production acceptance remain
separate from this draft's local checks.
