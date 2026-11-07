# #1068 automatic security maintenance draft

Date: 2026-11-01, validated and repaired 2026-11-02, remaining pieces built 2026-11-04, automatic
rollback built and rehearsed on Compose, native and a real Kubernetes cluster 2026-11-05.
Scope: the requested draft implementation, for review. No merge, release,
production feed activation, installation enrollment or deployment is authorized
by this delivery. The parent issue remains open.

Starting base: `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334`.
Branch: `feat/security-maintenance-draft`.
The PR's head identifies the delivered candidate; check results must name that
head rather than treating this starting base as the implementation revision.

## Delivery split

On 2026-11-06 the draft was split into two stacked pull requests:

- #1241, part 1 (foundations): the signed TUF feed client and publisher, the
  catalog/policy engine, the upgrade primitives (`internal/store/upgrade`,
  `internal/hostupgrade`, `internal/upgradegate`) and the documentation.
- #1137, part 2, stacked on part 1: the coordinator command, the native, Compose,
  Kubernetes and ECS deployment owners, the installation recipes, the chart and the
  WebUI status panel.

This handoff describes the combined draft. Behavior, ownership entries and evidence
that need part 2 are marked "(part 2)"; part 1 alone does not deliver them. Every
rehearsal and real-flow result below ran on the combined branch, not on part 1.

## Implemented behavior

- A common TUF-authenticated public security catalog with durable freshness,
  rollback, equivocation, clock and root-rotation protection. Offline metadata
  imports use the same verification. Existing release trust independently
  authenticates executable/image and compatibility identity.
- Local applicability and policy: explicit enrollment, recovery incarnation,
  stable/nightly channel, application pins, patch/minor/compatible classes,
  severity and UTC windows. Defaults in the new recipe (part 2) permit compatible
  security remediation including disclosed features and maintenance. Existing
  manual installations are never silently enrolled.
- (part 2) Real artifact/route preparation before writers stop, current authorization
  revalidation, durable exact-target operation pins, and actual running identity,
  database generation/schema and readiness checks. Ambiguous post-write work
  cannot be replaced by a newer target or an automatic old-image rollback.
- (part 2) Separate installation-owned native, Compose, direct Kubernetes and narrowly
  contracted ECS/Fargate execution. Unsupported ownership/topology refuses.
  The serving application receives no deployment credentials or Docker socket.
- Local operator CLI/status with separate release, security coverage/component
  scope and upgrade-impact fields, a scheduled installation recipe, and explicit
  local root/publisher tooling. A read-only status endpoint and WebUI panel show its state.
  All but the root/publisher tooling is part 2.

The implementation retains the no-backports support policy. Independently
serviced modules and the optional same-container launcher were not selected.
The key ceremony, the compromise and root-recovery ceremonies and real platform
acceptance remain separate from the code.

## Code ownership map

| Owner | Main entry point | Pull request |
| --- | --- | --- |
| Public feed/client and publisher primitives | [securityfeed](../../internal/securityfeed/repository.go), [publisher](../../internal/securityfeed/publish.go) | #1241 |
| Catalog/policy engine | [securitymaintenance](../../internal/securitymaintenance/engine.go) | #1241 |
| Durable coordinator and local command/config | `internal/app/security_maintenance.go` | #1137 (part 2) |
| Feed publisher CLI | [feed CLI](../../internal/app/security_publish.go) | #1241 |
| Upgrade primitives | [store/upgrade](../../internal/store/upgrade/), [hostupgrade](../../internal/hostupgrade/host.go), [upgradegate](../../internal/upgradegate/gate.go) | #1241 |
| Native source/route/process adapter | `internal/app/security_native_driver.go`, `internal/app/native_security_upgrade.go` | #1137 (part 2) |
| External image verification and readiness wait | `internal/app/security_deployment_driver.go` | #1137 (part 2) |
| Preparation cache and exact-image startup | `internal/app/security_image.go`, [unattended owner](../../internal/app/unattended_upgrade.go) | #1137 (part 2) |
| Compose/Kubernetes/ECS replacement | `internal/securitydeployment/deployment.go`, `internal/securitydeployment/ecs.go` | #1137 (part 2) |
| Installation and policy examples | `install/security-maintenance/`, `install/systemd/hikyo-security-maintenance.service`, `install/systemd/hikyo-security-maintenance.timer` | #1137 (part 2) |

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

## Validation of the draft

A later validation asked one question: can a published fix reach an installation
with no person involved? As first delivered, the answer was no. Reading the real
execution paths and running real Docker found these defects, now repaired:

- Nothing authored, published or renewed the feed, so every installation would go
  stale within a week. `feed advisory`, `feed refresh`, real-client verification of
  output and an inert [workflow](../../.github/workflows/security-feed.yml) now exist.
- A failed attempt or a restore could block every later update, with no command to
  leave that state. `reconcile` now does, and an untouched source releases by itself.
- Critical fixes waited for maintenance windows. `urgent_severity` skips the window.
- One unfixed advisory could hold back a critical fix. Selection now takes the best
  partial fix, and refuses a target carrying an advisory the installation lacks.
- Staging left the running source unable to restart, found only by real Docker. Each
  staged target now has its own cache.
- A published release reached every installation within about five minutes. A target
  can now stage its rollout through a local hash, with no reporting.
- Blocked protection gaps exited 0. They now fail the scheduled unit.
- Chart installs failed, and were refused. Opt-in, root-key environment and index
  digest proof now work. Kubernetes refusals no longer pin once a read shows the
  target absent, so a reply to a retried, committed write cannot pass as a refusal.
  ECS register and run-task refusals no longer pin. A refused `update-service` does.

## Evidence added by validation

- [Real Compose rehearsal](../../scripts/ci/security-compose-rehearsal.sh): replacement
  through the real executor, real registry, real signed fixtures under ephemeral trust.
  Plain `docker compose up` keeps the target, data and root key survive, kill during
  boot recovers, an older image refuses with the database unchanged, the source
  survives a restart after staging, a stop failure pins and `Reconcile` releases.
  It bypasses the feed, driver and engine and fakes GitHub. It is not wired into CI.
- Whole path on Compose, one command (`sh scripts/ci/security-compose-rehearsal.sh feed`, about
  four minutes, rerun twice with no defect): real `feed init-root`, `advisory`, `publish` and
  `refresh` under ephemeral Ed25519 keys that never left the container; the shipped
  `compose.yaml` and `compose.json.example`; real `enroll`; then a real `security-maintenance
  run --once` as root from the stamped release A binary. No executor call, no test seam. Observed:
  an empty catalog left the installation `current`; a moderate advisory fixed by B stayed
  `blocked: maintenance_window` with the window closed; a critical advisory fixed by B, with
  `urgent_severity: critical`, replaced the server by itself (exit 0, `healthy`, the server
  reports B, the container and its image are B's, `install.env` names B). The first
  administrator, an organisation, a project, a key, a TOTP factor and a session survived: the
  old session still answered and the key listed. `status` showed `covered`, healthy, no
  operation; the status file reached an authenticated browser-session call to `GET
  /api/v1/instance/security-maintenance` byte for byte; the second run changed nothing; the
  coordinator binary moved to release B's bytes. Withdrawing a target before any run made
  `check` stop selecting it (`no_complete_security_target`, run exit 1), and a feed whose
  timestamp lapsed reported `stale: feed_unavailable` and changed nothing.
- Substitutions in that run: the engine is Docker Engine 29.9.2 with Compose 5.6.1 inside a
  privileged container on a rootful Podman 5.4 machine (arm64, 2 GiB), not the Podman API, so
  Podman-specific Compose behavior was not exercised. GitHub, ghcr.io and hikyo.app are one local
  server under a private CA, reached through `/etc/hosts` and the container's trust store, with the
  production feed paths. The fixture releases are nightly builds with a signed image artifact
  (Dockerfile.release's distroless base plus the binary and the CA), stripped, built with a
  placeholder web UI. The configuration changes only the repository, Docker path, nightly channel and
  a closed five-minute window. `compose.yaml` gains `extra_hosts` and `GOMEMLIMIT`, and the tools
  run under `GOMEMLIMIT`, because the VM OOM-killed 600 MB release verifications. The real
  `hikyo.app` was never written.
- Found by that run and since fixed: an installation holding only its first administrator failed
  its first update after the source had stopped, and could not recover. It now takes the update,
  with the normal restart. A forced failure before write intent, using a fixture secret whose
  ciphertext cannot be read, restored the source by itself in 21 seconds, stopped at three
  attempts as a protection gap, and left no backup copies. The refusal for a tenant with nobody
  managing identities is tested in `internal/app`, not rehearsed. A coordinator killed mid-wait
  recovered on the next run. The retry wait was rewound by editing `status.json`, not the clock.
- Native on real systemd 257 as PID 1, privileged container on the same Podman machine
  (`sh scripts/ci/security-compose-rehearsal.sh native`). Stage 1: the recipe from nothing through
  `install-systemd.sh` to enrolled, rerun with nothing changed. A container restart of PID 1, which
  is not a host reboot, brought the service, timer and vault back, and the real timer found the host
  current. Stage 2: the first update of that host, forced to fail by killing `backup upgrade-export`
  before write intent, restored the source on the real unit: `preparation-abandoned.json` written,
  journal gone, operation cleared, the bundle `runtime.env` names kept. A second restart booted the
  source. Stage 3: after the backoff the timer's retry took A to B through backup, scratch restore and
  apply, with data surviving and `/usr/bin/hikyo` ending as B's bytes. This found that the real server
  could not start under the mandated unit, because systemd delivers the root key with an ACL; fixed.
  Only the export kill is scripted, so a proof or evidence failure on native is not rehearsed.
  Substituted: a fixture release stands in for the package, on arm64, with the local feed and fake
  release route.
- Real workflow commands, without running the workflow: the store steps of a first publish, a rotation
  and the publish after it, against real git clones; docs run selection against the repository's 512
  real successful runs.
- Real systemd: the timer keeps firing after a failed run, with a stub binary.
- Real kind: index and child digest reporting, chart upgrade keeping the coordinator
  image, the published Role, and real 403 and 422 replies. No complete update on
  Kubernetes has run.
- Real kind, in-cluster coordinator: a CronJob-created Job under its own service account
  loaded its root-owned subPath ConfigMap files, passed the state checks on a 0777 local-path
  volume root as UID 65532 (such a root is now refused unless `accept_world_writable_volume_root`
  is set), reached the feed and stopped at its 404, wrote the status
  ConfigMap that the server pod's volume projected as a symlink, and a chart upgrade kept
  both the status data and a coordinator-replaced image. client-go exec and logs returned
  stdout, exit codes and the output bound. The Role answered `no` for other names. The
  server did not boot on that cluster (its fixture release was refused), so enrollment
  stopped at inspecting it and no update ran.
- Real aws-cli exit codes for 400 and 500 replies. No AWS resource was used.

## Automatic rollback (owner decision 2026-11-04: "a failure shouldn't occur")

A target that fails after it wrote is now rolled back to the fenced snapshot by itself, on
Compose, Kubernetes and native, in the same run or a later one. A target past the final gate is
never stopped by the deadline. The [runbook's failure table](../operations/security-maintenance.md)
lists what still needs a person. Rehearsals, none of them in CI:

- Compose (`sh scripts/ci/security-compose-rehearsal.sh rollback`, real Docker Engine and Compose
  inside a privileged `docker:dind` container in a Lima VM, 140 checks and 0 defects at `54756975a`): a post-write health failure; a target hung in migration
  past the deadline; a refusal before any write; a coordinator killed mid-operation with a later
  run under manual policy and with the feed down; a third rollback reaching `retry_exhausted`; a
  corrupted archive making the rollback itself fail and pin; a target that passed the final gate
  and then died, left alone. Data, the session and identity survive each restore.
- Native (`... native`, real systemd 257 from Debian trixie as PID 1 under rootful Podman in the same VM, 0 defects at
  `1f0ededa1`): the coordinator dies after the target binary is installed and a later timer run
  rolls back after the deadline, also with the SQLite file absent at rolling-back.
- Kubernetes (`scripts/ci/security-pi-rehearsal.sh`, the chart on a real arm64 k3s node in its own
  namespace, 107 checks and 0 defects at `fae19d5ac`): S1 chart install with unattended upgrades on; S2 a real critical advisory
  through the feed carries A to B; S3 C fails after write intent and is rolled back in the same
  run; S4 the coordinator is killed and a later run rolls back after the deadline; S4b the target
  crash-loops, so a later run reads the control row through a stage Pod; S4c the coordinator dies
  while the rollback one-shot runs and a later run resumes it; S5 a target past the final gate is
  not stopped. Each rollback is checked against the API server's Pod record: the Deployment at
  zero, no live server Pod when the one-shot was created, the one-shot on the recorded node.
- Found by the Kubernetes runs: a session left on PostgreSQL by a deleted server Pod makes the
  reset refuse ("another session is connected") until the server drops it. The pin stays and every
  run retries; at `23a0af5fe` a later run restored the instance once the session ended, between 4
  and 17 minutes later. That was watched by hand after the run ended; the retained log stops at
  the assertions, which recorded the instance still down. The rehearsal runs with server-side `tcp_user_timeout` and keepalives. By owner
  decision Hikyo now also sets them on its own sessions, including the operator lock session that
  runs a target's migrations. That is tested against PostgreSQL but not rehearsed on the cluster.
- Found by the Compose runs: a target frozen while it holds SQLite's lock leaves the control row
  unreadable, so the deadline cannot prove it did not pass the final gate and leaves it running.
  The runbook gives the exit; it is not rehearsed.

## Remaining gates

**No installation in the field can receive a fix yet.** Items 1 to 4 are gates, in order. Item 5
lists limits left in place.

1. The compromise and root-recovery ceremonies, which the owner has not yet reviewed. The other
   decisions are made (ADR, 2026-11-06): custody, the chart default, PostgreSQL sessions and no late
   rollback command. The freshness monitor outside GitHub is set up once the feed is live.
2. The key ceremony and a stable release: carrying out the 2026-11-06 custody decision, the
   `security-feed` store branch and the two environments. Merge this to `main`, because the workflow
   runs only there, with a successful `docs.yml` run whose `github-pages` artifact has not expired.
   Commit the real root as `install/security-maintenance/security-feed-root.json`, then cut a stable
   release containing it, tagged on an ancestor of `main`. Version 1.0.0 lacks all of this. Then set
   `HIKYO_SECURITY_FEED_PUBLISHER_VERSION` to that release, enable the workflow and run the first
   publish, per the [runbook](../operations/security-maintenance.md#publisher-and-offline-operation).
   Each chart operator sets `feedRoot` themselves. Until then enrollment fails closed, and
   `https://hikyo.app/security-feed/` serves 404. By owner decision, installations whose operator key
   is held elsewhere and populated Compose databases stay refused with an explanation.
3. Real acceptance of what has not run. A failed stable check here means a second release:
   - The publishing workflow and its deployment to `hikyo.app` have never executed.
   - Every rehearsal uses nightly fixtures, so the stable channel the recipes default to has not run
     end to end. Check that the stable image's binary matches the tarball's, which `stage` and the
     follow both require.
   - A real package install on a qualified Linux host, and amd64. A native proof or evidence failure,
     and a real host reboot. An end-to-end key rotation. Populated PostgreSQL.
   - Kubernetes on amd64, on a multi-node cluster, and with a populated database. The Pi runs used one
     arm64 node and a node-local state volume.
   - Real AWS. ECS exits and the 254 reclassification are tested against a fake runner. Confirm with one
     call each: Fargate digest shape from `describe-tasks`, platform version `LATEST` from
     `describe-services`, registration normalization from `describe-task-definition`, tags from
     `update-service`, read-after-write of `desiredCount`, `list-tasks --started-by --desired-status
     STOPPED` returning a just-finished job, `describe-tasks` reporting `MISSING` for a reaped ARN, and
     `list-task-definitions --sort DESC` ordering among families sharing a prefix. Do not advertise ECS
     before this.
4. Publication: an advisory through the workflow, reaching an installation for real.
5. Known limits left in place:
   - Some failures still have no automatic exit. See the table in the runbook's failure section.
   - A failed credential proof is discovered only after the source stops. A preflight would need the
     backup restored with custody, which `stage` does not have.
   - Selection does not know route existence: a best-coverage target with no route keeps retrying and a
     routable lesser fix is not tried. Withdraw the unroutable target.
   - The release client ignores proxy settings, so proxy-only hosts cannot update.
   - A publish makes anonymous GitHub API calls from a shared runner, so a rate limit can fail it during
     an incident. Rerun with a fresh dispatch: "Re-run failed jobs" cannot find the attempt's artifact.
   - External-host Kubernetes has no WebUI status. ECS has none either.
   - A new catalog field breaks older strict clients: add a new format beside v1.
   - A cold Go feedback cache plus the 220-second budget can fail hosted `feedback (go)` for this branch,
     as run 41266633784 did with no compiler diagnostic: it reported `Cache not found`.

This draft does not close #1068, add a release milestone or activate anything.

All reports/evidence remain local unless explicitly included as Markdown in the
PR. HTML companions are offline-only and are not committed. Task-owned test
processes, listeners and scratch files must be cleaned before final delivery;
retain only source, required failure evidence and review artifacts.
