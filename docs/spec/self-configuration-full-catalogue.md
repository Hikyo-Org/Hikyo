# Complete Hikyo configuration and remote Apply

Status, 2026-09-06: **expanded implementation is uncommitted and locally tested; full acceptance and exact-head CI remain open.** The current catalogue has 27 top-level keys: nine original mail/channel values, 16 owner settings, one secret node-overlay document and one bootstrap-alias document. This is not a claim that every server transition or deployment provider is finished.

## Authority and delivery boundary

The user's D11 correction requires every supported server variable and remote Apply only by the target instance administrator using fresh passkey or TOTP. The user delegated subsequent recommendations and explicitly approved controlled bootstrap pod rollouts when deployment-owned inputs cannot change in place. Ordinary settings still reload without a container restart. These decisions do not need renewed approval.

Historical PR #686 validation at `152373212c6d78a3bcae91e3300097ff0d893acf` covers the nine-key implementation. Current expansion evidence belongs in the [validation record](../reports/self-configuration/validation.md). No merge, deployment, cluster enrollment or exact-head CI result is implied by local tests.

Each logical instance owns its protected organization, project, environment, schema, secrets and generation. The root management view groups references. It does not confer remote authority or centralize secrets. Only HA replicas share one owner configuration. The owning instance enforces normal scoped access together with instance-admin MFA; machine credentials, directory credentials and administration of a different instance cannot Apply.

## Catalogue and activation coverage

`internal/config/variables.go` inventories 65 recognized inputs: 53 server, 10 client, one command-only and one retired. `internal/runtimeconfig/catalogue.go` defines the editable catalogue. Inventory metadata and required activation classes do not prove that an activation consumer exists. Canonical file-content values replace filesystem aliases after one-time import; project values never request arbitrary remote filesystem reads.

| Family | Scope and current implementation | Remaining boundary |
| --- | --- | --- |
| Nine mail/channel values | Owner; immutable mail component and notification channel replacement | Historical CI applies only to this slice; explicit test-email remains separate from Apply |
| Argon2 memory/time/parallelism, reauthentication window | Owner; replace auth/admission graph with production floors and each node's actual capacity | Node budget stays node-local; preserve rate/backoff counters across replacement |
| External origin, directory proxy, MCP enabled/origins | Owner; replace HTTP/auth/federation/MCP/outbound captures | RP hostname change needs exact TOTP plus current password and confirmed TOTP on the initiating admin; target DNS/TLS/login reachability is not proved |
| Backup interval/recipients/retention/RPO/RTO, audit retention | Owner; replace scheduler and retention configuration | Existing irreversible pruning consequences remain; no fabricated rollback of deleted data |
| Listeners, PostgreSQL pool limit, admission budget, backup directory, trusted proxy CIDRs | Node; explicit overlay and live consumers | External Service/Ingress/firewall/mount changes are not managed by a successful local socket or directory check |
| TLS certificate/key PEM, adapter/OIDC/dynamic egress JSON | Node; encrypted content import and live TLS/client replacement | Import private key files only with mode 0400/0600; never reread stale imported paths after adoption |
| Database locator and root-key source | Alias-only `HIKYO_BOOTSTRAP_SOURCES`; controlled external deployment protocol | Concrete provider supports an enrolled singleton non-HA Recreate Kubernetes 1.36 deployment only; same-database reconnect and root dual-wrap, not data migration |
| HA mode and stable node identity | Deployment topology/membership inputs | Remote topology/identity transitions remain unimplemented; ordinary owner/node reload supports existing HA membership |
| Upgrade bundle/state/evidence/backup/operator key/target manifest/legacy-writer gate | Server startup and signed-upgrade inputs | Existing upgrade gate remains authoritative; remotely changing these controls is not implemented and cannot manufacture signed evidence |
| Development admission/budget/fake-provider controls and development mode | Deployment-only safety context | No production remote bypass; development deployment transitions remain unimplemented |
| Client context/token/state/trust inputs; command-only operator-instance selector | Not server settings | Explicitly classified; never imported as server project secrets |
| Retired updater socket | Retired | Refused; configuration Apply does not revive the binary updater |

The 16 added owner keys are Argon2's three settings; two audit-retention settings; six backup settings; directory proxy; external origin; two MCP settings; and the reauthentication window. `HIKYO_TRUSTED_PROXY_CIDRS` belongs exclusively to a node overlay. It must not inherit a shared owner or stale process trust policy.

`HIKYO_NODE_OVERRIDES` is a secret, strict version-1 JSON object with a `nodes` map, limited to 64 KiB. Stable node IDs select only their own entry. Listener addresses and admission budget must be explicit. TLS uses `HIKYO_TLS_CERT_PEM` and `HIKYO_TLS_KEY_PEM`; egress policies use the three canonical `*_EGRESS_POLICY_JSON` keys. All changed members are validated against the fixed participant set. A missing HA node starts only a validated, fenced repair graph with readiness false; it does not inherit a different node's settings or acknowledge business readiness.

`HIKYO_BOOTSTRAP_SOURCES` is strict version-1 JSON with optional `database_source` and `root_source` aliases. Aliases refer to enrolled external custody, never raw DSNs or root keys in project cells. The database/root needed to unlock configuration cannot be discovered solely from that encrypted configuration.

## Application generation replacement

The app-owned supervisor retains DB, keyring, coordination, configuration coordinator and listener ownership. `internal/app/generation.go`, `owner_runtime.go`, `runtime_listener.go` and `runtime_serve.go` build, prepare and replace configuration-dependent service graphs. `runtimeconfig.RuntimeInstaller.Prepare` returns a `PreparedActivation` with `Activate` and `Close` methods. Preparation does not start active workers or acknowledge a generation.

Activation reserves changed sockets, fences new business operations, drains requests including response flush and old workers, inherits admission counters, replaces a PostgreSQL pool through the stable DB facade when needed, installs handlers/TLS/clients and starts the new graph. Unchanged sockets remain owned by the supervisor. The original nine-key component changes do not rebuild unrelated services. Memory TLS supports certificate rotation and plain/TLS transitions on an existing address; failed preparation releases candidate resources.

REST, SCIM, MCP and scheduled work refuse stale generation admission. Auth and narrowly authorized configuration-repair routes remain usable on the last usable graph after an activation failure. A handler ignoring cancellation can prevent a complete drain indefinitely; that safety limit is not a bounded availability promise. Runtime acknowledgements occur only after actual installation and source verification. A pointer update, prepared graph or controller receipt alone never means Applied.

An RP hostname transition requires the same initiating admin's live confirmed TOTP and current-epoch password credential, plus an exact fresh TOTP Apply decision. Same-host port changes may use a passkey. This preserves an origin-independent local login route; it does not establish target-origin network reachability or successful authentication. Host recovery remains independent. Complete target-origin access proof is an acceptance gap.

## Controlled deployment provider

A durable external writer is required for immutable/read-only Kubernetes mounts and environment to survive pod replacement. No in-process generation swap or hidden second root can replace that authority. `internal/configrollout` provides a signed, constrained mailbox protocol; app enrollment and installed-source checks live in `internal/app/self_config_deployment.go`. Service coordination lives in `internal/service/self_config_deployment.go` and durable store rollout rows/sequences.

The concrete provider requires explicit enrollment of one non-HA, stable-node, singleton **Recreate** Deployment on Kubernetes 1.36. Fixed resource identities, source versions, signer custody, narrow RBAC and admission enforcement constrain the controller. It is not a shell executor or general cluster administrator. The application does not infer enrollment from a URL or project alias. Real cluster installation, admission refusal and pod replacement evidence remain required.

| Target | Current responsibility and limit |
| --- | --- |
| Enrolled Kubernetes singleton | Stage only declared source references, compare admitted workload/source identities, execute signed command, observe exact replacement template/source and return a verified receipt; actual cluster proof remains open |
| HA bootstrap rollout | Not implemented; provider and candidate preparation refuse this topology |
| Host/service manager | No implemented durable-write/custody/recovery provider |
| GitOps-managed workload | No implemented canonical-source provider; a live patch that reconciliation can overwrite is not durable support |

Database aliases must prove the same admitted datastore, owner/schema/recovery identity and a fresh challenge. No cross-database or cross-engine migration is claimed. Root transitions prepare a new wrapper without changing external custody before authorization, then atomically persist authorized wrapper/target/command. Raw roots remain external. The old wrapper is retained for recovery. **There is no automatic root finalization**: verified source and replacement boot evidence do not silently retire the recovery wrapper.

## Prepare, exact MFA, commit and acknowledgement

The browser first sends `prepare_only: true` with a stable idempotency key and exact selected revision/generation. The job becomes ready only when all fixed participants have current matching preparation evidence, and a deployment plan exists when required. Prepared review lasts five minutes; individual synchronous requests wait at most 30 seconds. Fresh worker/heartbeat evidence is at most 30 seconds old. Preparation does not consume the final Apply ceremony.

The reviewed plan digest binds owner, recovery incarnation, revision/snapshot, schema, fixed participants, current generation, external source versions and deployment effect. The UI labels the action **Reload live** or **Controlled rollout**. Final Apply reuses the request identity and includes the exact digest in fresh passkey/TOTP evidence. The final transaction rechecks authority, epoch, factor freshness, plan/job version and database clock before spending the proof once and recording the durable intent. Replayed or changed decisions cannot authorize another effect.

External calls happen outside the final authorization transaction. Signed commands and sequence numbers are durably journaled so lost replies and worker restarts retry the authorized command. Journals and status contain identifiers/digests/receipts, not root keys or credential contents. Uncommitted private preparation lost on restart must be prepared again. Replacement nodes must verify installed aliases and template stamp before a runtime acknowledgement can establish convergence.

## Failure and deployment restoration

Pre-commit refusal leaves the old target unchanged. Post-commit refusal remains pending/partial with business operations fenced; it never silently reinstates obsolete policy. Ordinary failed targets can prepare a new published repair while staying fenced. A bootstrap job with a nonterminal external handoff cannot be superseded until the controller outcome is known.

For a partial controlled rollout, **Restore deployment inputs** is a separate exact `rollout-restore` MFA action. It binds the original job revision, current generation and plan digest. The service reserves a sequence and signs outside the final transaction, then rechecks job/row version/incarnation and consumes fresh proof while persisting the Restore command and dedicated audit event atomically. Cancel, changed-plan and stale/replayed proof paths do not authorize a restore. Durable retry returns the same operation.

Only a verified controller `Restored` receipt terminates this external handoff. Restore does not change the desired revision and never means runtime Applied. Business remains fenced until an administrator publishes and separately applies a repaired revision using a new exact MFA ceremony. Previous snapshots and root wrappers remain available for this path; required retention roots are not garbage-collected while still needed.

Backup restoration invalidates incarnation-bound plans and requires existing access/credential reconciliation. Host CLI recovery remains available without the old UI origin, using external DB/root custody. When those sources are unavailable, the enrolled provider's external recovery instructions must be usable independently of the app.

## Acceptance still required

1. Freeze implementation and pass full affected both-engine, race, isolation, API/SDK/web/docs and independent review gates on that revision; then obtain exact-head CI. Expanded desktop and mobile passkey/TOTP channel-Apply journeys pass; actual controlled-rollout acceptance remains unfinished.
2. Exercise an actually enrolled Kubernetes 1.36 singleton: negative admission/RBAC tests, source CAS, lost reply/restart, replacement boot/stamp, root dual-wrap, same-DB challenge, explicit Restore and separately authorized repair. Controller probes alone do not prove deployment.
3. Complete or explicitly deliver the missing host/GitOps and HA bootstrap providers, topology/identity transitions and relevant upgrade/development startup controls before claiming the user's complete-server-variable objective.
4. Establish target-origin DNS/TLS/admin authentication and external network/mount reachability semantics. Local parser/socket proof does not establish them.
5. Define and verify true datastore migration and explicit root-finalization recovery obligations wherever those capabilities are promised. The current same-datastore alias protocol and retained wrapper are narrower.

## Integration correction evidence

Host administrator creation must consume the running server’s fresh sealed owner/node seed, rather than evaluating the CLI process environment or changing a live listener to its default. Missing or stale evidence must refuse before principal creation and explain that the server must be started before retrying. Local regression proof now passes: service race 35.027 s, app 16.236 s, including both-engine maximum-size/MFA authority checks. Both R1 corrections now preserve discovery mode and read a fresh clock after the membership lock; both-engine regressions passed in 5.308 s and R2 review is clean for those fixes. Durable exact-authorized Submit/Restore/Observe renewal and no-effect restoration passed R2 review, race checks (module 4.877 s, service 111.817 s, app 17.108 s) and full lint (78.122 s). Renewal changes transport sequence/timestamps only and cannot grant an uncommitted preparation authority. Real Kubernetes outage/recovery proof remains open.

Root finalization now locks the configuration binding before the key hierarchy, preserving both wrappers while the current generation/incarnation has an unresolved applying, partial, applied or superseded deployment. Restore and repair preparation retain the guard until the replacement generation is committed; finalization racing Apply invalidates its stale dual-wrap proof. SQLite/PostgreSQL rollout regressions passed in 15.885 s and existing key-rotation invariants passed in 2.166 s; R3 review is CLEAN. Logs: `/tmp/hikyo-root-guard-r3-focused.log` and `/tmp/hikyo-root-guard-r3-store.log`. Formatting and diff checks passed. No automatic root finalization is performed.

Latest serial Go package checks passed: config 0.695 s, runtime configuration 0.360 s, crypto 0.383 s, store 37.031 s, service 114.142 s, app 90.759 s, server 63.187 s, configuration rollout 0.890 s and lint 35.491 s. The earlier isolation failure identified outdated fixture defaults, pins and audit expectations. Those corrections now pass focused checks; the complete isolation rerun is still in progress. Log: `/tmp/hikyo-selfconfig-final-sequential.log`.

Compose and retention CLI isolation fixtures now use production audit defaults (90/365 days) and a 30-minute RTO. All TestComposeCLI and TestRetentionCLIStartupSweep cases passed on both engines in 59.640 s (`/tmp/hikyo-selfconfig-isolation-cli-final.log`). Full TestAuditCore and invariants 06/11/13 passed on SQLite/PostgreSQL in 35.458 s (`/tmp/hikyo-isolation-pins-audit-full-core.log`). Independent R1 review is CLEAN: every shared site is checked against explicit pins, seed authority rejects network/generic minting, and Restore audit rows come from real service authorization with refusal/idempotency coverage. All 369 isolation cases are now running through three planner shards sequentially (`/tmp/hikyo-selfconfig-isolation-shard-{0,1,2}.log`); completion is not yet claimed.

Additional affected package checks passed: command 1.291 s, admission 0.427 s, authorization 0.415 s, store upgrade 40.423 s and upgrade gate 75.608 s (`/tmp/hikyo-selfconfig-final-boundaries.log`). Final affected-package vet passed (`/tmp/hikyo-selfconfig-final-vet.log`).
