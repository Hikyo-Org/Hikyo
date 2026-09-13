# Unattended container upgrades

Status: implemented; deployment acceptance in progress, 2026-09-13.
Encrypted local container custody is explicitly owner-approved; the execution
design below is not a declaration of shipped support or a newly locked runtime
contract. The governing [signed-upgrade ADR](../adr/signed-upgrade-compatibility.md)
retains its other release, migration, fencing and recovery requirements.

## Intended behavior and scope

An operator enables unattended upgrades when first deploying Hikyo. After an
external updater replaces the image, the new container verifies its own exact
release, prepares recoverable backup evidence, migrates through the authenticated
route and serves only after admission and health succeed. No terminal, manual
evidence assembly or per-upgrade approval is required on the successful path.

The initial scope is Docker with SQLite and non-HA, single-replica Kubernetes
with PostgreSQL. PostgreSQL requires a separately provisioned empty scratch
database of the same engine, configured using protected credentials. HA,
rolling schema migration and arbitrary deployment orchestration are outside
this initial implementation.

Reject HA configuration before custody enrollment, backup preparation or any
live-state mutation. Also reject simultaneous activation of an existing rollout
controller with a different upgrade authority; two independent coordinators
must not compete for target selection or pending-operation ownership.

The option defaults off and is persisted as installation enrollment. Disabling
or omitting it retains manual upgrade behavior; it never clears a pending fence
or authorizes serving an incompatible release. Existing populated installations
need an explicit migration/enrollment procedure; a new flag must not silently
bootstrap unknown databases, retire an existing operator pin or fabricate a
legacy-writers-stopped assertion.

The first-install opt-in is `HIKYO_UPGRADE_UNATTENDED=true` or
`--upgrade-unattended`. PostgreSQL also requires
`HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN`. Published images predating this change
do not implement these settings.

## Explicit custody exception

The owner approved encrypted local custody after disclosure that the enrolled
container obtains recovery authority. Reuse the existing root-key-wrapped
encrypted custody implementation. Persist its backup identity, attestation key
and root escrow in a private vault; persist installation state and the upgrade
journal independently of disposable container filesystems. Persist encrypted
backups and public evidence needed to resume the exact operation.

Vault encryption protects stored material but does not separate it from a
compromised runtime that can read the installation root key. Locally generated
attestations prove completion within that approved boundary; they do not prove
independence from the runtime. Manual deployments retain their separate operator
custody. No private key belongs in logs, command arguments, environment values
or the public evidence directory. Local custody is not an off-host recovery copy.

The server receives no Docker socket, systemd control, Git credential or
Kubernetes deployment credential. An external updater remains responsible for
selecting and replacing the image. The startup procedure controls admission to
Hikyo's database and serving, not the container platform.

## Target and artifact authority

Treat the signed release identity of the running candidate image as the final
target. Fetch only evidence and intermediate artifacts needed for that target's
verified route. Do not follow `latest`, `nightly`, a newer release discovered
during boot or SemVer inference to select another target. Mutable tags are
external update inputs, never internal compatibility authority.

Verify the candidate and each route hop using existing pinned release trust,
manifest binding, exact source identity and migration digests. Preserve channel,
sequence and recovery-bridge rules. Missing metadata, unsupported historical
executables or absent compatible routes fail closed. Each intermediate executable
independently enforces the gate, with maintenance retained until the final target
is admitted. A changed target cannot steal a pending operation or reuse its proof.

## Startup sequence and the backup boundary

1. Validate enrollment, persistent state, custody, candidate proof, verified
   route and engine-specific scratch configuration before changing live state.
   A genuinely fresh database follows existing genesis rules; it needs no source
   backup. Healthy same-release restarts need no new backup proof.
2. Acquire the database's exclusive upgrade authority, drain already admitted
   guarded transactions and persist a `backup-preparing` maintenance fence bound
   to the exact source, target and generation. Invalidate old process admission.
   Release no path that lets old writers resume while backup is prepared.
3. Under that durable fence, export encrypted source state, restore to a separate
   empty same-engine scratch target and verify root/credential recovery. Produce
   the existing receipt and attestation bound to the exact backup and route.
   This uses real restored data, not a successful upload or a schema-only probe.
4. After proof is complete, atomically transition the reservation into the
   existing prepared-operation representation, preserving its identity and fence.
   Only then hand control to historical route candidates, which understand the
   established prepared/evidence contract. No schema change precedes valid proof.
5. Run the authenticated route with existing durable schema-write boundaries.
   Admit serving only after the final target's schema, keys and bounded health
   checks pass. Record completion durably before ordinary restart behavior resumes.

The runtime foundation implements phase `backup-preparing` with a preparation
record carrying the target, route digest and operator key identity while keeping
the completed source operation intact. Conversion must remove that new
preparation field before a historical candidate reads the established operation
format. SQLite and PostgreSQL integration tests verify these transitions. Full
container/pod deployment acceptance remains a separate requirement.
The existing backup receipt does not bind a data cursor. Consequently, a backup
taken while ordinary writes continue is not a sufficient basis for this flow:
checking release identity alone cannot detect writes committed after export.
The fence must precede backup, drain existing guarded transactions and persist
through proof generation and migration.

Old processes must stop serving and writing before export. The gate must exclude
guarded old processes transactionally, including paused writers that later
resume; heartbeat observations alone are insufficient. A pre-gate binary cannot
be fenced retroactively. First legacy transition requires operator-confirmed
full stop through the existing manual/bootstrap path and cannot be inferred
from the unattended flag or a single-replica declaration.

## Docker and Kubernetes integration

Docker must preserve the SQLite data, root key, private custody, journal and
encrypted backup across image replacement. Watchtower or another updater may
replace the image, but startup still refuses an unsupported target or failed
proof. No hook is allowed to bypass admission, and an older image must not regain
serving after the new generation is fenced. Do not configure automatic old-image
rollback after possible schema writes.

Kubernetes initially supports one non-HA replica with PostgreSQL. Use a
replacement strategy that avoids intentionally overlapping old and new server
pods; replica count alone does not prevent rollout overlap. The database fence
remains the actual exclusion boundary if a process is delayed or partitioned.
Keep readiness false during preparation and migration, and configure startup
probe allowance for bounded backup/restore and migration work. Pod restart must
resume or refuse from durable state, never clear it.

The scratch PostgreSQL DSN must identify an operator-provisioned empty database
separate from production. Refuse aliases of the live target, nonempty databases,
missing credentials and unsupported engines. Persist enough scratch-operation
identity to distinguish an interrupted owned drill from an unrelated nonempty
database. Define and test safe cleanup/reuse without deleting operator data.
The DSN grants scratch database access, not Kubernetes workload-control access.
Kubernetes can inject it from `secretKeyRef`; a literal credential must not enter
checked-in Helm values, rendered examples or logs.

## Failure and recovery

Missing prerequisites before reservation refuse startup without modifying
production state. After reservation, loss of custody, proof or coordination
retains maintenance. Restart can resume only the same authenticated operation
using its durable source, target and generation; a partially created backup or
drill cannot become successful evidence merely because the process restarted.

Once schema writes begin or their outcome is uncertain, preserve the existing
`restore-required` behavior. Never restore a database, run reverse migrations or
restart an older image automatically. Failure may require operator recovery;
unattended successful upgrades do not imply unattended recovery from corruption
or an ambiguous schema write. Disabling the option or rolling back the manifest
must not evade this state.

## Acceptance required before support is advertised

### Browser maintenance experience

An already loaded browser checks a public, nonsecret HOME-instance runtime
status endpoint on its own origin. Confirmed maintenance blocks editing, names
only the reported preparation/backup/restore/migration/health phase, and offers
no invented countdown. A failed connection or unstructured proxy 503 is shown
as reconnecting, not claimed as proof that an upgrade is running. Older servers
without the endpoint remain usable.

Retain the current URL and ordinary form drafts while interaction is blocked.
Existing session expiry/replacement and sensitive-state retirement still apply;
do not persist plaintext disclosures to preserve a draft. Revalidate the root
session and cached queries before resuming editing. Never replay a mutation or
submit a draft automatically. Announce phase changes accessibly, keep keyboard
focus inside the maintenance dialog, and require no motion or manual reload.

This reconnect experience applies to an already loaded SPA. A fresh navigation
while Kubernetes has no ready backend can receive the ingress's unavailable
page before Hikyo's JavaScript loads; the client cannot turn that response into
an application maintenance screen. A separate serving path would be needed to
promise a Hikyo page during that interval. Remote workspace status is outside
this first HOME-instance status authority.

### Runtime acceptance

| Area | Required evidence |
| --- | --- |
| Enrollment and defaults | Fresh explicit enrollment persists; omitted/false preserves manual behavior; reenrollment cannot replace custody or release a pending fence. |
| Exact image target | The running signed release is the target even when a newer release appears; wrong identity, unsupported route, downgrade and changed pending target refuse. |
| Backup consistency | A paused guarded transaction drains before export; a writer attempting to return after fencing refuses; no write commits between source export and migration. |
| Proof and historical hops | Real SQLite and PostgreSQL scratch restore verifies recovery; missing/stale/wrong proof refuses; conversion to established prepared state preserves source, generation, route and exclusion. |
| Replacement and recovery | Container/pod replacement preserves custody and journal; crashes during reservation, export, drill, prepared conversion and schema writes resume safely or remain fenced; no post-write rollback occurs. |

Exercise actual Docker SQLite replacement and single-replica Kubernetes
PostgreSQL replacement, not only unit tests. Include repeated successful
upgrades, ordinary same-release reboot, concurrent candidate starts, disk full,
scratch/live alias rejection, nonempty scratch refusal and failed health checks.
Record exact artifacts, environment and results in implementation evidence before
changing operator-facing docs from proposed to supported.
