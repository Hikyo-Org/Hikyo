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
separate installation-owned kubeconfig. The serving workload receives neither a
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
UTC maintenance window. Security urgency does not override these choices.
Windows can wrap midnight. Zero duration permits any time. A blocked pin or
feature upgrade is reported as a protection gap; it does not create a backport.
Local policy changes are reloaded between checks and a change during staging
refuses application until the operation is checked again. Final mutation gates
re-read the protected configuration, including across native executable handoff.
The first workload mutation must precede both metadata expiry and window end;
an operation admitted within the window can finish after it closes.

New installation recipes enable the timer after successful explicit enrollment.
The legacy binary installer and existing manual configurations are unchanged.
The example feed host is deliberately not a live project endpoint. Maintainers
must approve and provision real feed trust and publication before activating
these recipes for an official release.

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
files and explicit expiry/version choices. They validate the closed catalog
and independently authenticate all active release/image targets before writing
anything. Root creation requires supplied public role keys and thresholds;
rotation is authenticated under both old and new roots. Publication preserves
immutable versioned metadata/targets and writes timestamp last. It prepares a
local repository; uploading/activating it remains a separate maintainer action.

Root/targets threshold custody, real publisher hosting, compromise/root recovery
ceremonies and operational expiry rotation need maintainer review before
production. The draft does not generate or install production signing keys.

## Catalog and local state

`hikyo.security-catalog/v1` has a monotonic sequence, issue/expiry times,
advisories and exact targets. The payload lifetime is at most seven days; TUF's
earliest role expiry can shorten eligibility. Advisories name severity,
components and half-open SemVer intervals per trust profile/platform. An
affected range can set `delivery` to `binary` or `image`; omission covers both.
Targets
name the full release identity, platforms, immutable image when applicable,
fixed advisory IDs, optional coverage date and reviewed upgrade impact.
Separate native and OCI target records may share a release identity/platform;
an empty image selects the binary delivery record and a pinned image selects
the OCI record. Installation configuration chooses the executor/delivery kind.
Unknown/duplicate/trailing fields, ambiguous identities, still-affected targets,
mutable image tags and oversized inventories refuse.

The engine selects a forward target covering all locally applicable advisories
at the configured severity. The durable status keeps the exact running release,
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
or retry old code to clear the failure. Explicit deployment/upgrade-owner
reconciliation is required. Proven pre-mutation refusals remain retryable.
The native coordinator reserves exit status 23 for that definite refusal across
verified executable handoff. Other child failures remain ambiguous. An unstarted
native attempt restores its prior host journal before returning the refusal.
Its running source archive remains retained through preparation and failed
attempt cleanup; only successful replacement can retire that active proof.
After possible schema writes, the existing explicit recovery contract remains;
there is no automatic database restore or old-image rollback.

## Deployment contracts and draft acceptance

| Contract | Implemented path | Required acceptance before advertising support |
| --- | --- | --- |
| Linux/systemd, root, local SQLite | Existing native upgrade coordinator, exact stable/nightly identity and route pin, noninteractive root-wrapped custody, real process/binary/readiness inspection | Real systemd stable security-upgrade and interruption/recovery rehearsal |
| Enrolled singleton Compose/SQLite | External host selector, protected resolved desired-state snapshot, isolated target-image preparation, digest override, actual stop/recreation and Docker health plus local applied-state proof | Real Compose fixture and restart/recovery rehearsal on a qualified Linux host |
| Enrolled direct singleton Kubernetes/PostgreSQL | Fixed namespace/Deployment UID, `Recreate`, protected kubeconfig, isolated same-node preparation Pod, resource-version checked image mutation, actual runtime platform digest and applied-state proof | Real cluster/PVC/scratch-DB rehearsal, admission/RBAC and interruption/recovery checks |
| Native cloud VM using that exact systemd contract | Same local executor; no provider credential held by the project | Each advertised VM recipe must be verified in its actual provider |
| ECS/Fargate singleton/PostgreSQL, EFS and existing ALB readiness | Customer-owned AWS CLI/profile, strict existing service/task/storage/health contract, isolated stage/inspection tasks and task-scoped logs, full source stop before exact target task definition | Real AWS service/EFS/ALB/IAM/logging, interruption, cleanup and recovery acceptance; exclusive desired-state ownership enforced outside this process |
| Helm-managed/GitOps, HA, native PostgreSQL or other managed containers | Explicit capability refusal | Dedicated desired-state/provider and writer/recovery contracts, implementation and provider acceptance |

Helm-rendered manifests installed directly through `kubectl apply` can use the
direct-owner contract if no Helm/Flux/Argo controller owns them. The adapter
refuses an actively Helm-managed release or GitOps owner. The configuration
rollout controller remains configuration-only.

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
