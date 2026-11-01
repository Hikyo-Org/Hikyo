# #1068 automatic security maintenance draft

Date: 2026-11-01.
Scope: the requested draft implementation, for review. No merge, release,
production feed activation, installation enrollment or deployment is authorized
by this delivery. The parent issue remains open.

Starting base: `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334`.
Branch: `feat/security-maintenance-draft`.
The PR's head identifies the delivered candidate; check results must name that
head rather than treating this starting base as the implementation revision.

## Implemented behavior

- A common TUF-authenticated public security catalog with durable freshness,
  rollback, equivocation, clock and root-rotation protection. Offline metadata
  imports use the same verification. Existing release trust independently
  authenticates executable/image and compatibility identity.
- Local applicability and policy: explicit enrollment, recovery incarnation,
  stable/nightly channel, application pins, patch/minor/compatible classes,
  severity and UTC windows. Defaults in the new recipe permit compatible
  security remediation including disclosed features and maintenance. Existing
  manual installations are never silently enrolled.
- Real artifact/route preparation before writers stop, current authorization
  revalidation, durable exact-target operation pins, and actual running identity,
  database generation/schema and readiness checks. Ambiguous post-write work
  cannot be replaced by a newer target or an automatic old-image rollback.
- Separate installation-owned native, Compose, direct Kubernetes and narrowly
  contracted ECS/Fargate execution. Unsupported ownership/topology refuses.
  The serving application receives no deployment credentials or Docker socket.
- Local operator CLI/status with separate release, security coverage/component
  scope and upgrade-impact fields, a scheduled installation recipe, and explicit
  local root/publisher tooling. No public API revision or frontend changed.

The implementation retains the no-backports support policy. Independently
serviced modules and the optional same-container launcher were not selected.
Maintainer approval of draft trust/custody/operational choices and real platform
acceptance remain separate from the code.

## Code ownership map

| Owner | Main entry point |
| --- | --- |
| Public feed/client and publisher primitives | [securityfeed](../../internal/securityfeed/repository.go), [publisher](../../internal/securityfeed/publish.go) |
| Catalog/policy/durable coordinator | [securitymaintenance](../../internal/securitymaintenance/engine.go) |
| Local command/config/publisher integration | [operator role](../../internal/app/security_maintenance.go), [feed CLI](../../internal/app/security_publish.go) |
| Native source/route/process adapter | [native driver](../../internal/app/security_native_driver.go), [exact upgrade bridge](../../internal/app/native_security_upgrade.go) |
| External image verification and readiness wait | [image driver](../../internal/app/security_deployment_driver.go) |
| Preparation cache and exact-image startup | [image staging](../../internal/app/security_image.go), [unattended owner](../../internal/app/unattended_upgrade.go) |
| Compose/Kubernetes/ECS replacement | [deployment contract](../../internal/securitydeployment/deployment.go), [ECS](../../internal/securitydeployment/ecs.go) |
| Installation and policy examples | [recipe directory](../../install/security-maintenance/), [service](../../install/systemd/hikyo-security-maintenance.service), [timer](../../install/systemd/hikyo-security-maintenance.timer) |

Use the [design](../design/security-maintenance.md) for decisions and support
boundaries, and the [operator runbook](../operations/security-maintenance.md) for
setup, publication, status and recovery. Example endpoints and resource IDs are
placeholders; no project feed or cloud resources were provisioned.

## Validation evidence

The real TUF-to-engine-to-packaged-upgrade fixture passed locally. Its success
case creates an actually admitted release, discovers the authenticated security
target, performs real encrypted backup/scratch restore/migration and starts the
compiled target with HTTP readiness and durable state/value checks. The failure
case injects actual target startup failure after writes, preserves recovery
fences, and verifies that reopening the checker does not retry old code.
Process management substitutes for systemd; this is not real systemd or provider
acceptance. Fixture artifacts use only temporary test trust and credentials.

Focused feed/policy/deployment tests exercise signatures, threshold/root
rotation, expiry/replay/equivocation, wrong source/platform/target, policy blocks,
staging/authorization ordering, source/recovery/desired-state drift, ambiguous
provider responses, no rollback and cleanup ownership. ECS tests use a fake AWS
runner. Installed Docker Compose v5 validated the complete recipe's resolved
configuration, health command, required environment and absence of a Docker
socket. That render is not a running Compose upgrade.

Review repairs add regression coverage for source drift during final
re-preparation, local policy revocation at the mutation gate, maintenance-window
closure during observation, platform mismatch, Compose's equivalent image proof,
Kubernetes service-account admission defaults and a retryable API conflict.
Native handoff carries the original route, admission deadline and protected
configuration fingerprint to the executable that actually stops writers. It
also binds the original source/recovery tuple, source binary and resolved host
configuration/environment hash. A definite pre-mutation child refusal preserves
its classification across re-execution and restores the unstarted host journal.
Initial and failed-attempt pruning retain the still-running source proof.
Deadline checks follow long final authorization work, and binary/image advisory
applicability and current coverage remain scoped to the actual artifact.

The new local CLI and verified public metadata/staging caches are registered in
the existing closed wire/audit/cache registries, with explicit key scope and
proof-gating descriptions. No broad cache-package exemption was added.

Final aggregate tests, independent Standards/Spec reviews and hosted CI are
recorded in the delivered PR. Pending or skipped gates must remain explicit.
No test-count claim substitutes for the real-flow distinctions above.

## Remaining gates

1. Approve the proposed protocol/root roles, custody, expiry/publication and
   recovery ceremonies through the maintainer process. Activate real trust and
   publication only in an independently authorized release lane.
2. Qualify the exact signed stable candidate on a real Linux/systemd host and
   actual Compose and Kubernetes/PostgreSQL installations, including persistent
   storage, restart, interruption, restore and network-loss behavior.
3. Rehearse the ECS/Fargate/EFS/PostgreSQL/ALB contract in AWS with customer-side
   IAM, logs, cleanup and interruption evidence. UpdateService has no CAS;
   enforce the documented exclusive desired-state owner outside the local lock.
   Native cloud-VM support similarly requires actual provider acceptance.
4. Split further rollout work into verified vertical slices. Helm-managed/GitOps,
   HA, native PostgreSQL, other managed services and fully disconnected artifact
   import remain outside the qualified contracts. Do not remove owner checks,
   recovery fences or expiry to claim their support.
5. Preserve the distinction between a draft PR, merged source, artifact
   publication, deployment and runtime acceptance. This handoff is none of the
   latter four and does not close #1068 or add a release milestone.

All reports/evidence remain local unless explicitly included as Markdown in the
PR. HTML companions are offline-only and are not committed. Task-owned test
processes, listeners and scratch files must be cleaned before final delivery;
retain only source, required failure evidence and review artifacts.
