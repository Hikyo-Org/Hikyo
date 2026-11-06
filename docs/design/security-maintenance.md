# Automatic security maintenance draft

Issue: [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).
Status: implementation draft for review, not production trust activation or
platform acceptance. Existing signed-upgrade and unattended-custody decisions
remain authoritative. This document records the draft choices needed to make
the code executable; it does not change support policy or close the issue.

## Product contract

An installation obtains the same signed public security catalog as other
installations, evaluates applicability locally, and uses its own deployment
authority. No registration, heartbeat, instance identifier, installed-version
upload, remote command channel or project-held deployment credential is needed.
HTTPS operators still see ordinary request/network metadata. Metadata requests
carry a fixed client identity, not an installation-specific token.

The new operator role is `hikyo security-maintenance`. It runs separately from
the serving application. Native coordination remains root-owned. Compose
coordination uses a host-owned Docker client. Kubernetes coordination uses a
separate installation-owned kubeconfig, or, from the Helm chart, a CronJob in the
cluster under its own ServiceAccount. The serving workload receives neither a
Docker socket nor a Kubernetes deployment token.

Whole signed binaries and images remain the delivery unit. Release identity,
advisory coverage, and restart/feature/schema/API/configuration impact are
separate fields. A security label cannot prove feature-free code. Existing
[single-version support](../../SUPPORT.md) and no-backports policy remain.
No component launcher, plugins, LTS branch or independent module distribution
is introduced.

## Draft policy decisions

The supplied recipe defaults to automatic security maintenance, stable channel,
all severities, authenticated compatible routes including necessary feature
upgrades, and permitted maintenance. This is disclosed by the explicit
installation ceremony. Existing installations are never enrolled by starting
the checker or changing a flag. Native custody and existing container
unattended-enrollment prerequisites remain independently required.

Operators can select manual or verified offline mode, an explicit application
pin, patch/minor/compatible upgrade classes, a severity threshold, and a daily
UTC maintenance window. A verified fix at or above `urgent_severity`, which the
recipes set to `critical`, skips the window only. Leaving it empty keeps the window
for every severity. Pins, upgrade classes, maintenance consent, signatures and
recovery fences bind regardless of severity. Windows can wrap midnight. Zero duration permits any time. A blocked pin or
feature upgrade is reported as a protection gap; it does not create a backport.
Local policy changes are reloaded between checks and a change during staging
refuses application until the operation is checked again. Final mutation gates
re-read the protected configuration, including across native executable handoff.
The first workload mutation must precede metadata expiry and, unless the fix is
urgent, window end. An operation admitted within the window can finish after it
closes.

New installation recipes enable the timer after successful explicit enrollment.
The legacy binary installer and existing manual configurations are unchanged.
The recipes read the project feed at `https://hikyo.app/security-feed/`, which serves
nothing until maintainers provision real feed trust and enable publication.

## Two independent trust decisions

`internal/securityfeed` uses the maintained `go-tuf/v2` client already pinned in
the dependency graph. A separately provisioned root authorizes only the fixed
`security/catalog.json` target. TUF supplies role thresholds, consecutive root
rotation, expiry and version checks. The wrapper retains authenticated version,
hash and time floors, refuses same-version equivocation, bounds responses and
requests, and serializes durable cache changes. Online HTTPS and offline
repository imports use the same verification workflow. Switching a mirror
retains the same cache/floors. A bootstrap root is never fetched from the feed
it would authorize.

The existing build-stamped Hikyo release root separately authenticates every
selected release, its exact manifest/compatibility identity and artifact bytes.
The catalog's target identity is only a locator. A bulletin signer cannot add an
executable to the release inventory, invent a migration edge, authorize a new
image repository or bypass a revoked release. OCI platform digests are derived
from the signed image/index bytes rather than trusted from advisory claims.

Publisher commands use maintained TUF/Sigstore signers, explicit protected key
files and explicit expiry/version choices. `feed advisory` derives exact targets
from the independently verified release and computes their fixes. `feed publish`
validates the closed catalog, refuses a non-advancing sequence with different
bytes and independently authenticates active targets that are new or changed
against the previous authenticated catalog, so an aged-out release or provider
outage cannot block a withdrawal. Root creation requires supplied public role keys
and thresholds; rotation is authenticated under both old and new roots. Publication
preserves immutable versioned metadata/targets, writes timestamp last, then runs the
real client against the output. `feed refresh` re-signs snapshot and timestamp around
the existing targets, so liveness needs only online keys. The inert
`security-feed.yml` workflow runs these from a signed release and serves the result
under `hikyo.app`, a host the project controls, so leaving GitHub Pages changes no
installation. Its private store is a git branch no client reads. A publish re-serves
the last built docs site instead of rebuilding it, so a broken docs build cannot
stale the feed. Release artifacts still come from GitHub through the existing
release route.

Custody is chosen in the [ADR](../adr/security-maintenance.md#key-custody). Enabling that
workflow and the compromise and root-recovery ceremonies need maintainer review before production. The draft does not generate or
install production signing keys.

## Catalog and local state

`hikyo.security-catalog/v1` has a monotonic sequence, issue/expiry times,
advisories and exact targets. The payload lifetime is at most 90 days; freshness
comes from the short timestamp expiry, because clients bound eligibility by the
earliest role and payload expiry. Advisories name severity,
components and half-open SemVer intervals per trust profile/platform. An
affected range can set `delivery` to `binary` or `image`; omission covers both.
Targets
name the full release identity, platforms, immutable image when applicable,
fixed advisory IDs, optional coverage date and reviewed upgrade impact.
Separate native and OCI target records may share a release identity/platform;
an empty image selects the binary delivery record and a pinned image selects
the OCI record. Installation configuration chooses the executor/delivery kind.
A target can name `available_at` and `rollout_minutes`. A withdrawn target grants
nothing, so it may stay inside a range whose fixed version moved past it.
Unknown/duplicate/trailing fields, ambiguous identities, still-affected active
targets, mutable image tags and oversized inventories refuse.

The engine prefers the newest forward target covering all locally applicable
advisories. If none covers them all, it selects the permitted target fixing the
most, so an unfixed advisory cannot hold back a critical fix; the remainder stays
applicable and visible. A target carrying an advisory the installation does not
have is never selected. A target's optional `available_at` and `rollout_minutes`
stagger installations by a local hash of instance and manifest, so a publisher can
stage a rollout and withdraw it without any cohort list or telemetry. Withdrawal
reaches installations still waiting for their slot. Successive failures on one
target back off from 5 minutes to 6 hours; a different target resets this. After
three, the wait counts as a protection gap and fails the scheduled unit.

The durable status keeps the exact running release,
covered advisory/component scope, selected target and impact, last verified
check, expiry, current applicability, enrollment/recovery incarnation and any
pending operation. A coverage date never claims that external host, database,
proxy or every installed dependency is patched.
Current coverage is matched to the actual running artifact, including its exact
image digest, so binary and image records cannot borrow one another's claims.

Status phases include unenrolled, enrolled, applicable, staged, applying,
healthy/current, blocked, stale and failed. `current` means no outstanding
applicable advisory within the verified catalog and local policy scope; it is
not an assertion that no unknown vulnerability exists. `status` adds a derived
stale flag without contacting the publisher. Missing or expired metadata never
stops a healthy admitted application.

The serving application shows status to instance administrators, read-only,
through a one-way file channel: with `status_file` set, the coordinator writes a
public subset (no operation record or recovery journal) after every save, and the server reads it on each request. The server never
loads the coordinator's configuration or private state and gains no deployment
authority. A failed write is logged and never fails maintenance; an absent file
is "not reported" and an unparseable one is "unreadable", never healthy. The file is
world-readable on a host and namespace-readable as a ConfigMap, so it is public: it
carries the exact `action` command, whose `--confirm` value (a manifest digest or a
recovery incarnation) proves intent and is not a credential, and the failure `detail`
after value scrubbing and a 1 KiB bound. The external-host Kubernetes recipe has no WebUI
status.

The coordinator and its journal are private, installation-owned and protected
by one persistent lock. Enrollment binds the observed installation and recovery
incarnation. Restore or an installation change invalidates that authority.
Authenticated catalog floors persist even if later platform inspection fails.
Malformed local state is not discarded or reset to get green.

## Execution ordering and recovery

1. Refresh/authenticate metadata, inspect the real installation, evaluate local
   applicability/channel/pins/window/capabilities and retain an exact target.
2. Independently authenticate the release and stage all route executables and
   evidence before stopping writers. Measure schema/maintenance impact from the
   signed route and apply local policy again.
3. Refresh feed authorization after staging. Recheck the exact source,
   generation, installation and recovery incarnation. Persist the operation.
4. Revalidate release/route authorization immediately before mutation, preserving
   the original source identity, generation and recovery tuple across repeated
   preparation. Enforce current local policy, window end and metadata expiry
   before the first stop or desired-state change. Once that exact operation
   begins, expiry alone does not cancel a migration or strand the stopped
   application. A definite Kubernetes resource-version conflict preserves a
   retryable prepared operation; an ambiguous provider reply requires recovery.
   Native handoff also preserves the original source binary digest and a hash
   of resolved host configuration/environment. The child rechecks these against
   current local authority, database control state and the running source before
   the first fence. Environment contents never enter the guard's arguments.
5. Use the existing authenticated migration gate, writer exclusion, encrypted
   backup, real scratch restore, durable journal and bounded health checks.
6. Confirm the exact running binary/image and database release, advanced
   generation, unchanged recovery incarnation and live readiness. A pull, API
   success, container restart or healthy supervisor alone cannot satisfy this.

Image inspection reports the executing binary's OS/architecture. A different
controller/workload platform refuses before replacement. Kubernetes and ECS
prove the runtime manifest digest derived from the signed image index. Compose
uses Docker's local image identity and exact RepoDigests binding plus applied
payload/schema evidence; Docker's image configuration digest is not a manifest
digest. An optional catalog runtime digest is checked against the authenticated
index during preparation, while completion uses the adapter's applicable proof.

An ambiguous application failure pins the original operation. The next checker
can observe completion of that exact target but cannot select a newer operation
while the outcome is uncertain. The pin releases without an operator only when the
exact source is still healthy at its original generation an hour after the attempt
began, which shows no replacement took effect. Otherwise `reconcile --confirm`,
printed by `status`, is the explicit exit once the installation is healthy; the
confirmation names what is abandoned. It also accepts a restored installation. It
never passes an owner's recovery fence, because it refuses an unhealthy installation.
Proven pre-mutation refusals are retried with backoff, but only a bounded number
of times: a refusal that reached the running service costs an outage, so after
`maxDisruptiveAttempts` (3) at one target the engine stops attempting it, reports
`retry_exhausted` as a protection gap whatever the window or urgency, and names the
`reconcile --confirm` action that clears the count. A new target resets it; a repeated
attempt, a new catalog sequence or a successful preparation does not. Failures that
never touched the workload only back off. The scratch proof cannot run before the
stop: it needs the backup restored and its credential lifecycle run, a write
transaction that is rolled back on scratch, not a read-only query of the live database.
The native coordinator reserves exit status 23 for that definite refusal across
verified executable handoff. Other child failures remain ambiguous. An unstarted
native attempt restores its prior host journal before returning the refusal. When the
security coordinator's proof fails after the stop but with the journal still
`preparing` and the control row the untouched source, the native attempt also
restarts the source service, proves it healthy, lifts the fence and clears the
journal with a recorded reason, then returns the refusal; the classification survives
because the child exits 23 only after that proof. A stop that fails, a source that does
not restart healthy or a failure from the `proved` phase on stays stopped and pinned.
Its running source archive remains retained through preparation and failed
attempt cleanup; only successful replacement can retire that active proof.
A target that stops while its journal is still `preparing` has written nothing
but the `backup-preparing` freeze, so the signed-upgrade rule allowing rollback
on a proven pre-schema-write abort applies. The target marks its journal and its
`inspect` reports the untouched source with `aborted_before_mutation`; the Compose
executor then starts the source from its prepared snapshot, the Kubernetes executor
writes the prepared Deployment template back (only over the exact state Apply wrote,
so another writer's change refuses), and the source image,
the only one the journal names, lifts the freeze and clears the journal. The driver
releases its pin only after observing that exact source healthy at its original
generation, then returns `ErrNotStarted`, so the retry backoff applies. The report
travels in the observation, so a later run restores as well: the process that applied
may be gone, the wait may have ended, or the first restore may have failed on a
transient error, which the next run repeats. A target that finds its own marker idles
instead of retrying. A journal
at `write-intent` or later is never released, and the marker alone is never read
as permission: the control row decides. ECS reports the same observation but
keeps the replacement pinned. A target that cannot report (its image cannot start,
it is killed before writing the marker, or its configuration fails first) is handled by
the deadline below: an inspection by the source image alone would race a restarting
target into `write-intent` and kill a migration, so the owner stops the target first.
Each failed attempt removes its own backup, evidence and scratch
directories, and the next preparation sweeps any a killed attempt left, never those
the current journal references. A source process admitted before the
freeze and still alive would be admitted again, as no generation moves; the
coordinator stops the source first.
### Automatic rollback to the fenced snapshot

Owner decision, 2026-11-04: an update failure must not leave an instance down, so a
failed update rolls back to a snapshot. The snapshot is the encrypted backup the
preparation already takes and proves by scratch restore. Its archive holds the whole
database as it stood under the backup fence, including the control row. No second copy
is kept.

**The lossless window.** On container owners the fence sets `maintenance` durably before the
export, and every writer transaction refuses under it. It stays set through every hop until the
final boot gate commits `healthy` (`maintenance = hop+1 < route_length`). On native the host
fence and its stop proof hold the same window: the service cannot start, and a candidate can
serve only after that same gate. So while the control row has not reached `healthy` at a release
other than the source, no tenant write can have committed since the snapshot, and restoring it
loses nothing and returns no revoked credential. This is why a rollback skips the recovery
semantics of `restore run` (credential epoch advance, new incarnation, reconciliation): they
exist because a restored backup can be older than what it replaces.

The restore moves the control row back to the snapshot's generation. `prepare` had moved it
forward, to fence out processes admitted before. Rolling it back re-admits such a process, the
same accepted limit as the pre-write exit: the owner stops the source and proves it gone first.
The restore keeps the restore epoch and incarnation, so sessions and enrollment stay valid. It
also un-spends the attestation's one-use nonce, so the rollback deletes the attestation and its
signature before anything can serve. The ciphertext and receipt stay. Floors in the database
return to the snapshot's, which the source's own boot requires; floors retained outside it stay.

**When it may run.** All of these, with the target stopped:

1. The journal names a ciphertext and receipt. The ciphertext must authenticate against the
   receipt, and the receipt must name this journal's instance, source and route. Attestation
   expiry and the live generation are not checked here, so a later run can still roll back.
2. The live control row is read under the upgrade lock after the stop. It must belong to the
   journal's instance and to the receipt's incarnation and restore epoch, and operator custody
   must have no open journal and a floor not above the restored epoch.
   - An untouched source needs no restore, and the exit applies directly: `backup-preparing` for
     this route on container owners, or the source `healthy` without `maintenance` under the host
     fence on native.
   - Any other readable row needs `maintenance` set, `Pending.BackupID` equal to the receipt's
     backup ID, and a generation above the receipt's `SourceGeneration`. `healthy` at a release
     other than the source stays pinned, because the target may have served.
   - A failed read is accepted only at `rolling-back`, only when it returns "absent" rather than
     an error, and only when the database identity matches what the journal recorded on entering
     `rolling-back`. A connection failure is never read as permission. Everything else stays
     pinned.
3. Before publishing, the restored control row must be this journal's source at the receipt's
   `SourceGeneration`: `backup-preparing` naming this journal's target and route on container
   owners, `healthy` without `maintenance` on native. The backup ID is what identifies the archive:
   generation alone cannot, because a fence that is released and re-taken repeats it.

**The sequence.** One restore function, used by every owner:

1. The owner stops the target and proves it stopped, counting terminating pods: Compose removes
   the container, Kubernetes scales the Deployment to zero and waits for no pod, native fences and
   stops the unit.
2. The rollback runs as a one-shot of the **target** executable with the server's mounts and
   environment, built the same way as the stage run, on the node recorded at apply. Only that
   release is known to read the archive it wrote, and the same code restored this archive to
   scratch minutes earlier. The one-shot holds the coordinator lock for its whole run and refuses
   if it is unavailable. The journal moves to `rolling-back`, recording the database identity, only
   after a readable row passed rule 2 in this run. A run that cannot read the row changes nothing
   and stays pinned. A crash resumes the rollback, and a target that restarts refuses at
   `rolling-back`. The command does nothing when the journal is absent or still at `preparing`. An untouched source under a
   later journal phase restores nothing but leaves through the same exit: it deletes the attestation and
   rewrites the journal to `preparing` with `aborted` set.
3. It restores and verifies, deletes the attestation and its signature, then rewrites the journal
   to `preparing` with `aborted` set, no backup references and only fields the source already knows.
   A source image refusing a `rolling-back` journal is intended. From here the existing exit does the
   rest: the owner writes the source back, trusting the one-shot's report because the target is gone,
   `preparationNeverWrote` passes, the source lifts the fence with its compare-and-swap, and the
   coordinator proves the exact source healthy at the generation it observed before. No new way to
   lift a fence exists.
4. If the one-shot cannot start or fails, the owner still writes the source back. The source image's
   gate is the authority: if the journal is past `preparing` it refuses, and the instance stays
   pinned, as a protection gap with the exact command.

**Restoring live.** SQLite: the archive's database is staged beside the live file. Holding only the
lock file, with no connection open, the live file, then its `-wal` and `-shm`, are removed in that
order, the directory is synced, and the staged file is published and synced, so a stale WAL can never meet the restored file.
A crash repeats the rollback from `rolling-back`. PostgreSQL: the owned session refuses if any other
session, of any role, is connected to the database (a role that cannot see another role's sessions counts
them as clients), waits briefly for a backend that has just closed, then in one transaction with a
transaction-local `lock_timeout` drops every object in `public`, leaving the schema and its grants. It
checks that `public` is empty before committing, then runs the source migrations and the existing COPY
import. The reset drops exactly the objects the preflight counts, from one list: relations, functions,
procedures, aggregates, types including composite types, operators, collations, conversions and text search
configurations and dictionaries. The database identity recorded at `rolling-back` is the database name with
the cluster's system identifier, so an absent database on a repointed DSN is not mistaken for the one that was
being restored. This is not one transaction with the import, so `rolling-back` covers the gap and repeats it.
Preparation refuses before the fence unless the role can read `pg_control_system()` and `current_schema()` is `public` and the role owns every object there, because that failure
must not first appear during a rollback.

**What triggers it.** The driver's wait for the target ends in `restore-required`, in a target report,
or at its deadline (`timeout_seconds`, at most 24 hours). A later run does the same for a pinned
operation whose deadline has passed, so a coordinator that died or a host that rebooted does not need
a person. A slow migration that outlasts the deadline is stopped and rolled back, which loses nothing
but counts as a failed attempt, so the deadline must fit the installation's largest migration. A
rollback is a disruptive failure: backoff applies and the third at one target stops as
`retry_exhausted`. Status says plainly that the earlier release is serving and the fix is not applied.

**The deadline never stops a target past the final gate.** Before any stop, on every owner, the
coordinator reads the control row. A row `healthy` at a release other than the source means the target
passed the final gate and may have served: nothing is stopped, the operation stays pinned, and status
reports that it needs a person. When the row cannot be read, the target is stopped only if the owner proves
no gate was crossed. Native proves it from the host journal: a phase of `preparing`, `proved`,
`write-intent` or `rolling-back` holds no gate, any later phase does not. Compose and Kubernetes write a
durable `rolling_back` marker to the deployment journal once the target is proven stopped and before the
one-shot starts. They read the row first: Compose through a one-off container of the installed image, Kubernetes through a short stage Pod
of the source image on the journaled node (it reads with `inspect --control-row` and is deleted by its
UID), and they prove no gate was crossed unless the row is `healthy` at another release or belongs to another
installation. A row that cannot be read proves it only under the marker, which is what a crash while the
database is absent leaves, only while the journal is still `applying`, and only while the workload does not run
the target, whatever reference started it (Kubernetes compares the Pods' runtime image IDs by digest; Compose
refuses unless every live container's image resolves through its RepoDigests to the journaled source, as
observation resolves it, since its reference may be a tag and its image ID differs between storage drivers): the marker vouches for
the stop it recorded, and a target running again was started by a person who may have taken it through the
gate. Otherwise it stays pinned, and so does ECS, which has no rollback. A target that crosses the gate between the read and the stop is stopped,
and rule 2 then refuses the restore with the target down. That race is accepted and marked in the code.
A restore that stops nothing, because the source is serving untouched, releases the pin without
counting as a disruption.

The deadline's own read gets one more read when the wait ends: the executor observes the target again on a
fresh window and stops only if the row is not healthy at another release. The one-shot that restores the
snapshot and the write-back of the source each get their own bounded window, so a hung one-shot cannot
starve the write-back.

A rollback that fails is reported, not hidden: status sets the `action` and the protection gap even under
a manual policy or a stale feed. A rollback that restored the snapshot reports `operation_rolled_back` and exits non-zero, in the same run,
under a manual policy or a stale feed, or for an unobservable target on a later run. The reason under a
manual policy is `operation_rolled_back`, not `manual_mode`. A restore that found the source untouched is
not a rollback; `not-needed` appears only in the rollback one-shot's report, never in `status`. A run that
rolled back with the source observed ends there, selecting and applying nothing, and keeps the retry count.
The executors set `rolling_back` only after the target is proven stopped; a resumed rollback reports the
snapshot restored even when its own one-shot finds nothing to do, and `Reconcile` returns `ErrRolledBack`
when the release follows an earlier rollback, which the engine counts and reports. That account outlives the
journal, because the coordinator can die after the release and before it saves: Compose and Kubernetes leave
the deployment journal at `rolled-back`, native leaves `operation-source.json` with `rolled_back`, and the next
`Prepare` removes either, as does a manual `hikyo upgrade` before it writes its own journal. Native tolerates a
missing SQLite file only at a `rolling-back` journal, and releases a `preparing` journal under a serving,
unfenced source, at once for the operator's `reconcile` and for a `run` only after `operationSettle` (one hour).
A `rolling-back` journal under a serving, unfenced source was put back by hand and is released the same way.
At a `rolling-back` journal `hikyo upgrade` does one thing: given the source's `--release-identity`, it fetches
that release into the verified cache, so that a rollback refused for a missing or damaged source release
finishes on the next run. With the file absent `Observe` cannot read the row, so the resume goes through the deadline path and the `NoFinalGate` journal proof. `inspect --control-row` reads the row without binary or schema checks. `preapply_refused` means the target stopped before any write.

A native run reinstalls the source binary and restarts it through the existing native exit. The source
binary's digest and runtime evidence are not journal fields, because the source must still read the
journal strictly after a rollback. They sit in `operation-source.json` beside it, written before the
journal and bound to its instance, target, route and source. The record is read only by a rollback;
without it an unfinished journal stays pinned. It is removed only after its journal has been cleared, never before, and not at all when clearing the journal fails. The reinstall runs after the database is restored and
before the journal is released, from the verified release cache, and refuses unless the cached binary's
digest equals the recorded one. A later run does the same through the host journal, by handing off to
the verified target executable with `--security-rollback`. Native `timeout_seconds` defaults to 30
minutes.

**Sorting the failures.**

| Failure | Result |
| --- | --- |
| Image cannot be pulled | Staging already ran it on the same host or node. If it still happens, the deadline stops the target and the source restarts. No restore. Native has no image |
| Target cannot start, or its configuration fails first | Source untouched. Target stopped, source restarts. No restore |
| Killed before its marker | Same |
| Failed or killed during migration, or failed health before the final gate | Automatic rollback to the snapshot |
| Failed or killed during the rollback, or database down at the deadline | Next run tries again from the durable state |
| Rollback cannot finish, such as a lost archive or a full disk | Pinned, protection gap, exact command. **Down until someone acts** |
| Source does not boot after a good restore | Pinned with `maintenance` set. **Down until someone acts** |
| Target passed the final gate and then fails or misbehaves, even if it never served | **No automatic rollback.** It may have served, and going back could drop accepted writes and return revoked credentials. Fixed forward: withdraw it and publish a fix, or an operator's `restore run`. Staged rollout limits how many installations take it |
| Legacy-genesis archive, which has no control row | Pinned. The recipes do not create these |
| ECS | Stays pinned. ECS has no restore and is not advertised until real AWS acceptance |

The explicit recovery contract remains for everything outside this window.

## Deployment contracts and draft acceptance

| Contract | Implemented path | Required acceptance before advertising support |
| --- | --- | --- |
| Linux/systemd, root, local SQLite | Existing native upgrade coordinator, exact stable/nightly identity and route pin, noninteractive root-wrapped custody, real process/binary/readiness inspection | Real systemd stable security-upgrade and interruption/recovery rehearsal |
| Enrolled singleton Compose/SQLite | External host selector, protected resolved desired-state snapshot, isolated target-image preparation, digest override, actual stop/recreation and Docker health plus local applied-state proof | Real Compose fixture and restart/recovery rehearsal on a qualified Linux host |
| Enrolled direct singleton Kubernetes/PostgreSQL | Fixed namespace/Deployment, UID pinned at explicit enrollment or configured, `Recreate`, protected kubeconfig or the chart's in-cluster CronJob service account, isolated same-node preparation Pod, resource-version checked image mutation, actual runtime platform digest and applied-state proof | Real cluster/PVC/scratch-DB rehearsal, admission/RBAC and interruption/recovery checks |
| Native cloud VM using that exact systemd contract | Same local executor; no provider credential held by the project | Each advertised VM recipe must be verified in its actual provider |
| ECS/Fargate singleton/PostgreSQL, EFS and existing ALB readiness | Customer-owned AWS CLI/profile, strict existing service/task/storage/health contract, isolated stage/inspection tasks and task-scoped logs, full source stop before exact target task definition | Real AWS service/EFS/ALB/IAM/logging, interruption, cleanup and recovery acceptance; exclusive desired-state ownership enforced outside this process |
| GitOps, Helm releases that did not opt in, HA, native PostgreSQL or other managed containers | Explicit capability refusal | Dedicated desired-state/provider and writer/recovery contracts, implementation and provider acceptance |

A chart release qualifies when `securityMaintenance.enabled` (the default) and
`upgrade.unattended.enabled` are both set, which marks
the Deployment for the adapter and keeps a coordinator-updated image across `helm
upgrade`. `helm rollback` reverts it. Manifests rendered with `helm template` and
applied directly use the direct-owner contract. The adapter refuses other Helm
releases and Flux, Argo or other GitOps owners. The configuration rollout controller
remains configuration-only.

The Kubernetes adapter uses the cluster API only: `pods/exec` and logs go through
client-go, and no `kubectl` exists in the image. With `securityMaintenance.coordinator`
the chart runs the coordinator as a CronJob and enrolls only when its separate
`enroll` value says so. The Deployment's UID is unknown before the first install, so
enrollment reads it once and pins it in private state; a later mismatch refuses like a
configured UID does, and unenrolled observation is read-only. In-cluster mode drops
the host-root requirement, accepts root-owned public files because Kubernetes projects
ConfigMaps as root, and exempts only the PVC's kubelet-managed volume root from the
writable-ancestor test. After a proven update, and only in an applying `run`, the
coordinator patches its own CronJob to the image the deployment owner observed, under
the same checks as a host coordinator's binary below, and copies its public status into
a ConfigMap the server mounts. Neither step can fail an update, and a refusal is
retried on later runs. `enroll` succeeds without acting on an enrolled installation, so
the chart's hook never blocks an upgrade during recovery.

A host coordinator, meaning Compose or a Kubernetes or ECS coordinator outside the
cluster, runs the packaged `/usr/bin/hikyo`, which a server image update never
replaces. After a run leaves the installation healthy with no operation pending, the
coordinator compares its own release with the running image's and, if older, prepares
that exact release through the same build-stamped root and floors as an update and
atomically replaces its own file with the signed binary for its platform. It never
downgrades, orders by version rather than channel, and replaces only a root-installed
regular file it resolved itself. The running release is reported by the serving
container, so it is trusted only after the latest authenticated catalog is found not to
withdraw it and the image the deployment owner observed is found in that release's
independently signed inventory. It is on by default because the coordinator is part
of what must be patched. A failure is logged and retried on the next run, and leaves
the engine's phase, `Detail` and later maintenance alone: `Detail` explains the latest
update failure, which this is not. Native updates its own binary and an in-cluster
coordinator follows through its CronJob image under the same rules.

The last row is a real remaining gap in the parent issue. This draft does not
claim all #1068 platforms accepted, turn local fakes into provider proof, or
close the parent ticket. The optional same-container launcher remains outside
this implementation. See the [operator runbook](../operations/security-maintenance.md)
and [delivery handoff](../handoff/1068-security-maintenance-draft.md).

ECS UpdateService provides no resource-version precondition. Rechecking the
service snapshot is not CAS. That draft contract requires customer IAM and
operating policy to make this controller the exclusive desired-state writer.
Autoscaling, IaC ownership, rollback deployment controls, multiple replicas or
unproven persistence/health layouts refuse. Registering/stopping only provably
owned temporary resources is separate from updating the serving service; lost
provider replies retain explicit recovery records rather than guessed cleanup.
