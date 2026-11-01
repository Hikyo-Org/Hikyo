# Security maintenance operator runbook

This is a draft capability for [#1068](https://github.com/Hikyo-Org/Hikyo/issues/1068).
Do not enroll production installations until the proposed feed trust/custody
decisions and the relevant deployment acceptance have been reviewed. The
[design](../design/security-maintenance.md) names the exact contracts and gaps.
Nothing here authorizes a project release, publication or deployment.

## Provision the local owner

Use a signed Hikyo release containing this capability. Unstamped source builds
cannot replace production release trust with a downloaded key. Keep the checker
outside the serving process with persistent private state. On Linux provision
the example paths under `/etc/hikyo` and `/var/lib/hikyo-security-maintenance` as
root, without symlink ancestry or runtime-user write access. Configuration and
public root files may be mode 0644; private keys and journal files are 0600,
private directories 0700. Use the actual installed Docker/kubectl paths.

Choose the [native](../../install/security-maintenance/native.json.example),
[Compose](../../install/security-maintenance/compose.json.example) or
[Kubernetes](../../install/security-maintenance/kubernetes.json.example)
configuration. Save the selected, fully populated configuration as
`/etc/hikyo/security-maintenance.json`. The example feed origin is a placeholder;
replace it with an independently approved publisher/mirror and provision its
approved root as `/etc/hikyo/security-feed-root.json`. Do not trust a root merely
because that same endpoint offers it.

The defaults allow necessary feature upgrades and maintenance through signed
compatible routes. Set `policy.mode` to `manual` to disable automatic execution,
or choose `patch`/`minor` upgrade classes, an explicit `pin`, severity threshold
and UTC window. A restricted choice can leave an advisory blocked. No backport
is implied. A critical advisory never overrides local policy or safety gates.

## Native and Compose

Native support requires the existing Linux/systemd/SQLite installation and its
root-owned `/etc/hikyo/upgrade.json`, local signed source evidence and
noninteractive root-wrapped recovery custody. A legacy passphrase vault needs
its existing explicit migration ceremony. Host PostgreSQL/HA are refused.

For a new Compose installation, use the complete
[maintenance recipe](../../install/security-maintenance/compose.yaml). Preserve
separate data, upgrade custody and root-key mounts as described in the
[unattended container runbook](unattended-container-upgrades.md). Its environment
includes the production inputs needed by stage/inspect as well as the server.
The real operational healthcheck is required. The serving container has no
Docker socket. Existing manually managed populated databases cannot be adopted
by adding `HIKYO_UPGRADE_UNATTENDED`.

Use the fixed project/service and protected environment/manifest files named in
the coordinator configuration. The controller retains a resolved desired-state
snapshot and `compose-active.json` image override in its private deployment
directory. Later manual Compose operations must include that active override;
running only the original image setting can request an obsolete image. Keep
this installation under one deployment owner. Do not run an unrelated image
watcher against it.

On the qualified Linux host, with the configuration and runtime already ready:

```sh
sudo /usr/bin/hikyo security-maintenance check
sudo /usr/bin/hikyo security-maintenance enroll
sudo /usr/bin/hikyo security-maintenance run --once
sudo /usr/bin/hikyo security-maintenance status
```

`check` reports verified eligibility and never stages or applies. Before
enrollment it reports `explicit_enrollment_required`. `enroll` records local
authority only for the exact healthy installation/recovery incarnation.
`run --once` reports current, blocked, stale or the verified application outcome;
it never automatically restores an old image after uncertainty. `status` reads
durable state without network access and includes a derived `stale` field.

For the new recipe's scheduled installation, run from the verified source tree:

```sh
sudo sh install/security-maintenance/install-systemd.sh
systemctl list-timers hikyo-security-maintenance.timer
sudo /usr/bin/hikyo security-maintenance status
```

The helper discloses restart/replacement authority, validates the configuration,
enrolls automatic/offline mode and enables the timer. Manual mode keeps it
disabled. It is never run by the legacy binary installer. The timer checks after
boot and about five minutes after the previous operation, with bounded jitter;
this is scheduling behavior, not guaranteed remediation latency. A long-lived
`run` process is also available when an installation-owned scheduler is more
appropriate.

## Direct singleton Kubernetes/PostgreSQL

Keep the existing unattended enrollment, separate live/scratch PostgreSQL
databases, persistent root key and POSIX-durable upgrade PVC contract. Use one
replica, `Recreate`, no HA/config-rollout enrollment and no server API token.
Provide all stage/inspect configuration through container environment, including
`HIKYO_ROOT_KEY_FILE`, because the stage command replaces the server arguments.
The root-key init container and server must reference the same authenticated
image. All matching Hikyo image-bearing containers are updated together.

Run the controller on a qualified Linux host using a separate protected
kubeconfig with a fixed context, namespace, Deployment UID and staging Pod name.
Use the same OS/architecture as the workload; local inspection refuses a
mismatched executor before replacement. Give it only the required
Deployment, ReplicaSet and Pod reads, exact Deployment update, exact staging Pod
create/delete, log reads and Pod exec needed by this draft. Use admission policy
to restrict the permitted target objects and patch shape; ordinary RBAC does
not restrict fields. Do not put that kubeconfig/token in the server container.

The target staging Pod copies the admitted workload's required mounts and
configuration on the source node, without a controller token or published
service. It prepares every route artifact before the source is replaced.
Success requires Pod readiness, an independently derived runtime image digest,
and exact local applied-state/schema evidence. A Deployment update or `/readyz`
alone does not establish the outcome.

Actively Helm-managed, Flux/Argo and other GitOps resources refuse. Do not remove
ownership labels/annotations to evade this refusal. Use direct installed
manifests only when no other desired-state controller owns the workload; a
dedicated GitOps adapter remains work for the parent issue.

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
No old task definition/database is automatically restored after uncertainty.
Only provably owned stage/inspection tasks and unused temporary definitions are
cleaned. A lost API reply retains its intent/identity for operator reconciliation.

AWS UpdateService has no conditional resource-version update. Enforce exclusive
service ownership with customer IAM and operating policy; the local lock and
snapshot recheck cannot prove that another principal will not race it. Existing
autoscaling, CloudFormation/other desired-state ownership, deployment rollback
or multiple replicas refuse. The draft does not alter IAM, EFS, ALB, networking
or health settings to make an installation qualify. Real AWS acceptance remains
required before advertising this provider contract.

## Publisher and offline operation

`hikyo security-maintenance feed init-root --help` describes explicit public role
keys, thresholds and root signers. `feed publish --help` describes catalog/root,
protected role keys, cache/output paths, increasing versions and explicit role
expiries. Publisher input must be a reviewed `hikyo.security-catalog/v1` with
exact release identities and advisory/component scope. Publication independently
verifies active release/image targets before preparing the local TUF repository.
Private key bytes never belong in command arguments, catalog files or logs.

Keep root and online role custody separate; choose thresholds and root-history
retention through the maintainer process. Publish immutable metadata/target
files before timestamp. Changing the common feed is not a customer outcome
acknowledgement and does not give the project a fleet census. Project-controlled
canaries and any optional telemetry require their own explicit arrangements.

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
the database automatically. Reconcile the exact pending target with its owning
upgrade/deployment recovery procedure. The checker can observe completion of
that original healthy target; it cannot replace uncertain work with a newer
advisory target.

Follow [manual upgrades](manual-upgrades.md) and the canonical
[signed-upgrade contract](../adr/signed-upgrade-compatibility.md) for explicit
post-write recovery. Cloud/provider support and production acceptance remain
separate from this draft's local checks.
