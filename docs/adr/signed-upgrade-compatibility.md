# Signed upgrade compatibility and platform orchestration

Status: locked upon merge of the #638 governance and legacy-retirement PR, 2026-09-05.
Native Codex high-effort review completed in three rounds, final verdict SOUND.
The accompanying retirement code satisfies the prerequisite to make this decision operative.
Implementation of the compatibility foundations remains a mandatory 1.0 gate.

## Decision and release boundary

Use a signed, directed release-compatibility graph. An edge is permission to
upgrade from one exact release identity and schema set to one exact target.
Direct skips are allowed only when the target declares that edge safe. Otherwise
choose the shortest verified route; deterministic ascending release sequence
breaks ties. Never infer compatibility from SemVer, a higher schema number,
GitHub's latest release, or the presence of migration files.

Before 1.0, implement compatibility metadata, signed release binding, the
applied-release ledger, an explicit legacy genesis edge and a migration gate on
SQLite and PostgreSQL. Automated platform application is disabled until its
backup, drain, health, rollback and restart-recovery acceptance passes. Manual
deployment remains supported, subject to the same target-binary migration gate.

The current remote updater is unsafe legacy: its rollback can restore a database
after schema writes and its Flux script uses ambient Git authority. The governance PR must
hard-disable remote application in configuration,
service admission and the standalone helper, including previously queued jobs.
Retire the existing Compose, systemd and host-helper Flux apply/rollback paths.
Preserve metadata, update checks and verified manual downloads. Replacement
platform orchestration is a separate implementation, not a flag that re-enables
those scripts.

## Release identity and trust

A stable release identity includes the version, monotonically increasing release
sequence, source commit, compatibility artifact digest and release manifest
digest. The existing offline-root-authorized release manifest binds the exact
compatibility artifact bytes alongside binary provenance, image and chart
digests. The compatibility artifact cannot contain its own or its containing
manifest's digest; those identities come from the verified enclosing manifest.
The binary embeds the source-owned declaration and its digest, avoiding a
self-referential binary/manifest hash. Runtime callers pass the verified release
envelope separately; the target verifies it against the pinned release trust.

Each target declaration is a closed, versioned schema naming the target version,
sequence, exact source release identities, exact source and target ordered
migration manifests (version plus SHA-256 of every migration), supported engines,
and edge mode: `restart` or `maintenance`. Unknown schema versions, fields,
engines, modes, duplicate or inconsistent nodes, missing source proof, cycles,
and descending sequences refuse. Maximum graph: 256 releases, 1024 edges,
32 hops; oversize graphs refuse without truncation. Same-release restarts require
identical release and migration digests and are not upgrade edges.

Every traversed node must be authenticated by its target's verified manifest.
Ordinary edges require that target-manifest authorization; a recovery-root-signed
bridge is the sole exceptional edge authority and may authorize an otherwise
absent edge after compromise. It never substitutes for normal independent
authentication of the target artifact and exact migration identity. A discovery response is only a locator; downloaded metadata is verified
before planning. Cache trust is revalidated on use against current authorized
revocations. Revoked, withdrawn or unavailable bridge artifacts refuse the route;
no substitution with a similarly named version. An installed revoked source is
not executed or downloaded as a route hop: emergency exit is allowed only through
a recovery-root-signed bridge declaration naming its exact stored source identity
and an independently trusted target. Revoked artifacts are never reinstalled.
Plans pin all manifests and
artifacts before the first stop, and revalidate immediately before application.

Nightly uses a separate, explicitly weaker `nightly/v1` keyless trust profile.
Its versioned policy pins the Fulcio trust roots, Rekor public keys/checkpoint
identity, OIDC issuer, canonical repository identity, exact workflow path and
protected main ref. A Sigstore bundle binds the compatibility declaration and
the exact version, tag, source commit and a closed inventory of every published
asset (name, type, platform and SHA-256 digest), including all archives, native
packages, compatibility declaration, binary provenance, checksums and metadata.
When OCI artifacts exist it binds their exact repository and manifest/index
digests too. Missing, extra, duplicate or substituted assets refuse; publication
cannot add an unbound executable later. The bundle also binds certificate
identity and validity, signed integrated time,
and verified log inclusion proof/checkpoint. Verification is offline from the
complete bundle and pinned policy, without trusting local wall time as evidence
that an expired certificate was valid at signing. Missing material, wrong issuer,
workflow/ref/repository, invalid inclusion, or a changed digest refuses. Policy
rotation is recovery-root-authorized. Use the existing maintained Cosign/Sigstore
verification tools; no new cryptographic verifier. Unsigned current nightlies
cannot claim this profile. The gated server refuses an unsigned nightly upgrade
of a populated database until a valid profile and bridge exist.

Nightly never authorizes a stable edge or stable identity. Nightly application
is explicit CLI only; no nightly WebUI apply. A trust-profile transition or exit
from a revoked stable source requires two proofs: a recovery-root-signed bridge
statement authorizes the exact source/target release identities and trust-policy
digests; an operator attestation binds that bridge digest to the instance,
restore epoch, generation, backup receipt and one-use nonce. The target verifies
both and consumes the nonce with its pending-operation transaction. This avoids
requiring the project release signer to sign each customer's instance while
preventing instance-local replay. Neither proof relaxes the target's normal
stable signature checks. A revoked source remains inspectable as installed
state, never executable as an intermediate bridge. No silent stable-to-nightly
fallback exists.

## Applied-release and migration state

Keep one instance-wide durable applied-release record outside tenant resources.
It stores release identity, migration-set digest, instance identity, restore
epoch and one monotonically increasing upgrade generation. It is not an audit
replacement. A pending operation stores the immutable source/target, route and
hop, generation, maintenance state, pre-upgrade backup identity and phase.
The phases are `prepared`, `schema-write-started`, `schema-applied`, `healthy`
and `restore-required`. Record the pending operation durably before any schema
write. Record `schema-write-started` durably before handing control to goose.
If a crash makes the write boundary ambiguous, assume schema writes happened.

Migration, server startup and explicit `hikyo migrate` share one gate. Its lock
covers state inspection, pending-state commit, migration, exact schema
verification and applied-state commit, with the existing SQLite host lock or
PostgreSQL advisory lock. The gate checks release proof and source identity
independently of discovery and any helper/controller. A client-supplied flag,
environment variable, manifest path or claimed previous version is not authority.
The exact source schema and identity must match the durable database state.
Only the same verified target and upgrade generation may resume an interrupted
operation; a different target, lower version, unknown applied migration or
changed migration digest refuses serving and migration.

The bootstrap operation must be atomic and fail closed even before the ledger
table exists. While holding the migration lock, inspect the full existing schema
and goose applied set read-only, validate the explicit genesis declaration, then
create the control tables and durable pending row in one database transaction
before ordinary schema migrations. The control-table transaction's crash
outcomes are either absent or complete. Fresh empty installs have a distinct
genesis identity. A populated pre-ledger database is accepted only when its exact
schema fingerprint and migration manifest match a documented legacy genesis;
never label an arbitrary old database as the current release.

Legacy binaries cannot enforce a new gate. The first hop therefore requires
operator-enforced full stop of every pre-gate writer and a verified backup before
installing the first gated binary. This limit is documented, tested as an
operational precondition, and never presented as retroactive protection against
an old binary or an administrator with direct datastore access.

## Maintenance and HA

Any schema-changing, bridge or rolling-incompatible route uses durable
maintenance across the complete route. Public tenant operations and all
background writers refuse while maintenance is active; operational health and
local recovery inspection remain available without secrets. Gates check the
maintenance generation inside each write transaction. Fences invalidate old
singleton leases. In-flight work is drained before migration, and already
admitted writes must finish or abort before the gate obtains exclusive authority.

Default is full stop on PostgreSQL HA. A rolling `restart` edge is permitted
only for an unchanged exact schema and an explicitly tested binary-skew pair;
it grants no general major-version or N-1 window. The helper/controller verifies
all server writers have stopped for maintenance, then runs one target migrator.
The maintenance marker survives process, pod, controller and host restarts.
Loss of coordination, observation or proof leaves maintenance enabled.

The structural fence is one upgrade-control row. Each process is admitted with
an exact release identity and generation at boot. Every domain transaction,
including reads which can acquire a write path, holds a shared row lock and
compares both identities before accessing tenant state. Direct adapter, dynamic,
crypto, retention, backup and scheduler writer transactions carry the same
check; migrations alone use the exclusive gate. Maintenance activation obtains
an exclusive row lock, which waits for already-admitted transactions to finish,
and increments the generation atomically. No new domain transaction is admitted
while maintenance is active. On SQLite BEGIN IMMEDIATE serializes writers; the
same persisted identity check still applies. Clearing maintenance does not renew
old process identities: every stale or partitioned process must restart and pass
boot admission. HA heartbeat observations alone never count as exclusion. Tests
cover a paused transaction, an old process returning after completion, direct
runtime writers, and crashes after each phase and each migration commit.

## Backup and restore custody

### Operator-approved local custody for nightly CLI upgrades (2026-09-06)

The operator explicitly approved running a single `sudo hikyo upgrade` command
on the server, with encrypted operator-only recovery keys stored locally. For
this nightly CLI flow, the operator process may hold an encrypted age identity,
an independent attestation key and root-key escrow in a root-owned directory
outside the runtime user's writable paths. Unlocking requires an interactive
operator passphrase. Private material is decrypted only in operator-process
memory and is never placed in server environment variables, command arguments,
public evidence directories or the database. The unprivileged server and host
adapter receive public evidence only. This explicit exception supersedes the
off-host custody requirement below for this flow; it does not establish an
off-host disaster-recovery copy or authorize unattended key unlocking.

The CLI automates authenticated release discovery, complete bundle assembly,
encrypted export, real scratch restore and credential proof, installation,
migration and bounded health checks. Build trust pins and migration policy are
embedded. The final release signature remains detached because its signed
manifest covers the final executable bytes; the CLI fetches and verifies it.
No manual evidence assembly is required. Existing unsigned installations enter
through their exact recovery-signed bridge. Intermediate hops remain in
maintenance, and every executable independently enforces the runtime gate.

The operator CLI owns a durable host journal and persistent systemd startup
fence. Before invoking migration it records an uncertain-write boundary. A
failed or interrupted post-boundary operation never automatically restarts an
older binary or restores a database. A later invocation reconciles the journal
with authenticated database state before continuing. The retired remote apply
protocol remains disabled; this exception grants no service-control privilege
to the Hikyo server, browser or remote client.

Populated-database upgrades require a verified pre-upgrade backup; this
explicitly supersedes the existing loud no-recipient skip as a successful
upgrade outcome. A fresh empty database needs no source-data backup. Ordinary
same-release restarts with no pending schema change are not backup-triggering
upgrades.

The encrypted backup manifest and a public `backup-receipt/v1` bind backup ID,
SHA-256 ciphertext digest, instance ID, exact source release and migration-set
identity, restore epoch, route generation and recipient fingerprints. Before
stopping workloads, an operator-controlled drill process verifies decryption,
root-key escrow and restore against a scratch target; it issues an
`upgrade-attestation/v1` naming the receipt digest, verified bridge/route digest,
instance, epoch, generation, target, issue/expiry times and one-use nonce.
Attestations expire after 24 hours and are consumed with pending-state creation.
The signing key is an instance-local operator recovery/attestation key held
outside the server and deployment controller. Its public key is pinned during
local installation; rotation requires the prior key or local break-glass custody
and increments the epoch, invalidating pending attestations. Use the configured
standard signature tooling, not custom cryptographic signing code.

Helpers and target binaries verify these public receipts and signatures without
receiving age identities, root-key escrow secrets or attestation private keys.
They compare the ciphertext digest against the existing encrypted backup and
match all receipt fields to live source state. An unsigned filename, timestamp,
successful upload, stale drill or bare claim of backup success is insufficient.
The public receipt contains no account, tenant, secret or bearer data. This is
operator attestation of a drill, not a claim that a helper can prove decryptability
without custody. A malicious operator with datastore/root authority remains the
existing threat-model residual.

Automatic binary rollback is allowed only on a proven pre-schema-write abort.
Once `schema-write-started` is durable, or the outcome is uncertain, restoration
is an explicit operator-held recovery flow, not a reverse migration or automatic
old-container restart. Restore preserves Hikyo's recovery-mode semantics:
credential epochs change, restored bearer authority is inert and grants require
reconciliation. Upgrade-control state is reconciled against the restored instance
and epoch; restoring an older backup cannot silently resume a newer pending hop.
Applied identity advances only after exact migration verification and bounded
health checks. A failed post-write health check leaves `restore-required`.

## Platform ownership

Bare systemd and Compose use a separately installed host helper with a fixed
deployment identity, root-owned configuration, authenticated local IPC and a
bounded operation vocabulary. It receives no arbitrary command, path, image,
Compose project or unit from the browser. It verifies the pinned plan and release
proof itself, retains a durable journal, drains the configured deployment,
applies digest-pinned artifacts, checks health and reconciles restart state.
The server receives no Docker socket or systemd control authority.

A failed platform candidate health check first retains the service fence and
records a terminal host `restore-required` journal; automatic retries are refused.
If the exact runtime operation is still `schema-applied`, the coordinator also
advances that database operation to `restore-required` through its existing CAS.
If runtime-local health already established `healthy` before platform readiness
failed, its applied identity remains intact behind the terminal host fence;
operator recovery is required without rewriting successful database history.

Flux uses a separate pull-based upgrade controller with narrowly scoped access
to one declared deployment and Git path. Credentials can update only a dedicated single-application repository and protected ref. Ordinary Git
credentials are not treated as path-scoped. A shared repository is permitted only
with a tested server-side pre-receive policy restricting the exact object/path
and diff; client-side checks do not qualify. Credentials cannot force-push,
delete refs, create tags or write another branch. Flux uses a namespace-scoped
service account with fixed object-kind/name admission and signed-image policy,
so compromising Git credentials cannot submit arbitrary cluster objects.
Every repository layout additionally requires server-side pre-receive or cluster
admission enforcement of the exact permitted patch shape: only the verified
approved image digest and controller-owned upgrade-generation fields may change.
All other fields are immutable to this credential, including service accounts,
secret/config mounts, commands, arguments, init containers, security context,
RBAC and labels/annotations affecting admission. Object-name restrictions and
signed images alone do not satisfy this requirement. Configuration changes use
separate operator custody and review. Adversarial fixtures attempt each forbidden
pod-spec/RBAC change through a compromised repository credential. The controller does not accept a server-
supplied remote URL, branch, path, shell command or arbitrary Kubernetes object.
It coordinates suspension and full stop of writers, a single migration job,
digest-pinned deployment, health and resume under a durable generation. It must
prove Flux cannot race the maintenance sequence or roll the deployment back
automatically after schema writes. Server pods have no Git, Flux, Helm or
Kubernetes deployment credentials. Direct Helm application is a future adapter;
the documented manual maintenance procedure remains available.

WebUI application, when enabled, is stable-only and instance-admin-only with
fresh existing proof. It displays the exact route, bridge stops, maintenance and
backup identity, and creates a bounded request for external orchestration.
Ordinary account, org-admin, service-account and MCP credentials cannot trigger
it. No auto-apply endpoint or button is enabled by the compatibility foundation.

## Review record

Native Codex R1 identified five blockers: legacy rollback, structural writer
fencing, backup proof/custody, nightly/recovery trust and Flux credential scope.
The revisions above address each as explicit requirements. Runtime legacy
retirement is an independent prerequisite to making amendment banners operative.
R2 verified legacy retirement, writer fencing and backup custody, and requested
explicit recovery-edge precedence, a closed nightly asset inventory and exact
Flux patch admission. Those three requirements are now explicit above.
No implementation ticket claims a completed foundation from this proposal alone.

## Acceptance and implementation decomposition

1. Compatibility artifact and trust binding: positive direct and bridge routes;
   forged/unknown/duplicate/revoked/oversize/cyclic inputs; unavailable bridges;
   deterministic route; stable/nightly separation; tampered release artifacts.
2. Both-engine ledger and migration gate: fresh and supported legacy genesis;
   wrong schema; unknown migration digest; concurrent migrators; crash before
   and after each durable phase; explicit migrate and boot take the same gate;
   interrupted target resume and downgrade refusal.
3. Release acceptance: maintenance prevents all writers, source backups bind
   correctly, restore cannot resurrect upgrade authority, full-stop HA and
   unmodified-schema restart skew fixtures pass. Core foundations gate 1.0.
4. Host helper and Flux controller: separate implementation tickets only after
   this ADR locks; each remains disabled until independent backup, health,
   rollback-boundary and crash/restart tests pass on the real platform.

Owning amendment scopes: threat-model (#8) trust/custody; Compose (#18) external
helper; Kubernetes (#19) separate controller; architecture (#22) gate/ledger;
MVP (#26) pre-1.0 foundations; operations (#32) maintenance/restore; mechanics
(#33) compatibility manifest binding. Existing API freeze, tenant authorization,
cryptographic algorithms and no-enterprise-tier commitments are unchanged.
