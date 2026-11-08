# Automatic security maintenance (ADR, proposed 2026-11-04)

> **Status: proposed with [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).**
> It becomes operative when its reviewed PR merges. It authorizes no release,
> deployment, key generation or feed activation. The items under
> [Still open](#still-open) remain the maintainer's.

Goal, from the owner: push security updates without user intervention, and patch a
critical flaw easily. The [design](../design/security-maintenance.md) holds the detail
and the [runbook](../operations/security-maintenance.md) the procedures. This file
records only the decisions.

## Decisions

### Delivery unit and support policy

Whole signed binaries and images are what gets delivered. The
[single-version support policy](../../SUPPORT.md) stands, with no backports. A security
label does not claim feature-free code, so restart, feature, schema, API, configuration
and maintenance impact are separate reviewed fields. Independently updatable modules
and a same-container launcher are not selected.

### Two independent trust decisions

A TUF feed, using `go-tuf/v2`, says what is vulnerable and which target fixes it. It
never authorizes code. The existing build-stamped release root separately authenticates
every selected release, its compatibility identity and its bytes. A compromised feed
key can therefore name a target but cannot add an executable, invent a migration edge
or bypass a revoked release.

Online keys (snapshot, timestamp) keep the feed alive. The targets key publishes
advisories, behind a reviewed environment. The root stays offline. A root is never
fetched from the feed it authorizes.

### Hosting

Owner decision, 2026-11-04: the feed is a page under `hikyo.app`, so that leaving GitHub
does not leave a GitHub path to maintain for the feed.

- Clients read only `https://hikyo.app/security-feed/metadata/` and `.../targets/`.
  Moving the site means serving the same files at the same paths, without redirects.
  The URLs are installation configuration, so a mirror keeps the same root and floors.
- A publish re-serves the last built docs site with the new feed, because a broken docs
  build must not stale the feed. Every docs build carries the feed, so neither
  deployment can drop the other.
- The publisher keeps its history in a protected `security-feed` branch. No client reads
  it and no client knows of it (owner decision, 2026-11-04). It exists because signing needs
  the exact prior versions, which a cached page cannot give, and it moves with the publishing
  workflow.

**Remaining GitHub coupling, outside the feed:** every binary still hard-codes
`api.github.com` for signed release artifacts and reads release trust from a GitHub
branch, and images come from `ghcr.io`. Moving those needs a release that carries the
new route, delivered through the old one. Owner decision, 2026-11-04: this is tracked separately.

### Consent and who gets this

Owner decision, 2026-11-04: only installations on the release containing this feature,
or newer, receive automatic updates, and that release is acceptable as a required normal
upgrade. More exactly, installations created through the new recipes from that release
onward can enroll. No adoption path is built for native hosts whose operator key is held
elsewhere or for populated Compose databases, and they stay refused with an explanation.
A new native host enrolls from nothing: the ceremony creates its vault and pin with the
existing custody code.

Enrollment is explicit and local: a ceremony on a host, or
`securityMaintenance.coordinator.enroll` in the chart. Starting a checker or changing
an image never enrolls. There is no registration, heartbeat, instance identifier or
project-held deployment credential.

Owner decision, 2026-11-04: **automatic maintenance is on by default in the chart** for an
installation that has chosen unattended upgrades, and the recipes enable it too. The chart cannot
turn on unattended upgrades itself, because they need a scratch database and a state volume only
the operator can provide. Owner decision, 2026-11-06: this stays so, which means a Kubernetes operator
sets `upgrade.unattended.enabled` once before fixes apply. Default enrollment happens on `helm install`. An upgrading release
enrolls only when asked, because a failed hook would fail that release.

Rolling the policy back is local: `policy.mode: manual` stops automatic updates, and the
ceremony disables the timer. A scheduled `run`, such as the chart's CronJob, still restores a
source after a failed update (owner decision, 2026-11-04). Removing the policy changes nothing already applied. A restore
or a changed recovery incarnation invalidates enrollment, so consent is never carried
across an installation's recovery.

### Upgrade classes and policy

The recipes default to automatic, stable, compatible upgrades including necessary
features and maintenance. Operators can choose manual or verified offline mode, an
explicit pin, patch or minor classes, a severity threshold and a UTC window. A blocked
fix is reported as a protection gap. It never produces a backport.

### Emergency window

`urgent_severity`, set to `critical` in the recipes, skips the maintenance window and
nothing else. Pins, upgrade classes, maintenance consent, signatures and recovery fences
bind at every severity. A target can stage its rollout through a local hash with no
reporting, so a bad release can be withdrawn before most installations take it.

### Stale knowledge and offline operation

- A feed outage, expired metadata or no metadata never stops a healthy admitted
  application. It reports `stale` and starts no update. The one action it still takes is
  restoring a source whose target reported a stop before writing, which is not an update.
- At cold start with no fresh metadata, nothing is selected and the service starts under
  its normal gates. Known revocation and migration fences stay binding.
- Offline mode imports a signed repository through the same TUF verification as online,
  from a local directory. It does not skip any check.
- A root can rotate past an expired root through its chain while its keys exist. If root
  keys are lost, an installation must be re-provisioned by hand: a changed bootstrap root
  refuses against retained floors, and the feed state has to be cleared deliberately. The
  rebuilt feed must carry the catalog sequence forward with `--previous-catalog`, or
  clients refuse it as a rollback even then.
- Persistent floors refuse older metadata. Restoring the whole state directory from a
  snapshot cannot be detected by that same state: this residual is accepted, and an
  attacker able to do so already holds the coordinator's authority.

### Recovery

Owner decision, 2026-11-04: an update failure must not leave an instance down, so a failed
update rolls back. This amends the [signed-upgrade ADR](./signed-upgrade-compatibility.md),
which allowed automatic rollback only on a proven pre-schema-write abort. The
[design](../design/security-maintenance.md#automatic-rollback-to-the-fenced-snapshot) holds the
conditions.

- The snapshot is the encrypted backup the preparation already takes and proves by scratch
  restore. It holds the database as it stood under the backup fence.
- A rollback may run only while durable state proves that no tenant write can have committed
  since: the control row has not reached `healthy` at a release other than the source. It keeps
  the restore epoch and incarnation, so sessions and enrollment stay valid and it needs none of
  the recovery semantics of `restore run`. The archive is identified by its backup ID, so no
  other backup can pass as this one.
- The owner stops the target first. The target executable performs the restore, then the
  existing pre-write exit restores the source image, which alone may lift the fence. A crash
  during the rollback resumes the rollback, and this also holds on a later run after a reboot.
- A failure before write intent needs no restore, and the same source restart applies.
- Three failures that reached the service on one target stop automatic attempts until the target
  changes or an operator acts, as a protection gap that fails the scheduled unit. Owner decision,
  2026-11-04: a critical fix may wait for a person at that point.
- A refusal reply counts as no effect only when a read proves no effect, since a client may have
  retried a request that already committed.

**What still needs a person.** A target that passed the final gate may have served, so going back
would drop accepted writes and could return revoked credentials. It is fixed forward by
withdrawal and a new release, or by an operator's `restore run`, even when it crashed before any
client reached it, because that cannot be told apart. A rollback that cannot finish and a source
that does not boot after one stay pinned. ECS has no restore and stays pinned.

The scratch drill records `authoritatively-no-credential-scope` only when no
organisation exists other than the protected configuration organisation (a first
administrator's installation may have none), so nothing could hold credential
management. A tenant organisation with no eligible principal refuses, with or
without a stored secret.

### Deployment owners

Each owner holds its own authority, and the serving application holds none: no Docker
socket, no Kubernetes token. The owners built are native systemd with SQLite, Compose with
SQLite, and direct singleton Kubernetes with PostgreSQL (in the cluster or from a host).
ECS/Fargate is contracted narrowly and is not advertised until real AWS acceptance. Support
for any of them is not claimed until its acceptance in the
[handoff](../handoff/1068-security-maintenance-draft.md) has passed.
GitOps-managed workloads stay refused, because their controller owns the desired state.

The coordinator is part of what must be patched, so it follows the release it has proven
healthy: a host coordinator replaces its executable and an in-cluster one patches its
own CronJob image. A failure there never changes the update's result.

Status reaches the web interface through a read-only public file, or a named ConfigMap
on Kubernetes. A missing file reads as not reported, and an unreadable one as unknown,
never healthy.

### Catalog format

`hikyo.security-catalog/v1` is closed and strictly decoded, so a new field breaks older
clients. A format that needs one is published beside v1.

## Key custody

Owner decision, 2026-11-06: the maintainer is the only custodian.

- The root is one key, threshold 1 of 1, with encrypted offline copies. Anyone holding a copy and
  its passphrase holds the whole root, which this accepts for one custodian.
- The root expires one year after each ceremony and is renewed yearly through the rotation steps,
  with the same or new keys. The renewal also proves the copies still open.
- The targets key stays a secret in the reviewed `security-feed-publish` environment, so a critical
  fix is one approved workflow run.
- Snapshot and timestamp are online with one key each. That is the workflow's existing assumption,
  not an owner decision.

## PostgreSQL sessions left by a dead server

Owner decision, 2026-11-06: Hikyo sets `tcp_user_timeout` and TCP keepalives on each of its
PostgreSQL sessions that can outlive a dead server (the store pool and the operator lock session), so the server drops a session whose Pod died within about 90 seconds and the
rollback is not held off by it. The runbook still recommends the same settings on the server, for
sessions Hikyo does not open and for poolers. The rollback keeps refusing while another session is
connected: ending sessions itself could reset under a server still writing.

## Not built

Owner decision, 2026-11-06: no one-command rollback for a target past the final gate. That case stays
fixed forward, or recovered by an operator's `restore run`, until real use shows a need.

## Still open

These need the maintainer before any installation can receive a fix:

1. The compromise and root-recovery ceremonies.
2. The key ceremony, the environments, the `security-feed` branch and enabling the workflow, as
   listed in the runbook. The owner has chosen a monitor outside GitHub for the feed freshness
   check (owner decision, 2026-11-06); it is set up once the feed is live. The
   `security-feed-freshness` workflow covers a stopped publisher but shares GitHub with it.
3. A stable release containing this feature, including the real root.
4. Real platform acceptance, tracked in the
   [handoff](../handoff/1068-security-maintenance-draft.md).

## Review record

This amends the [signed-upgrade ADR](./signed-upgrade-compatibility.md) by adding the
automatic source restore and the no-credential-scope drill value, under a banner there.
Independent Standards and Spec review applies per the owner's 2026-10-29 policy, and
cross-provider review is not required. The outcome is recorded on the PR.
