# Complete Hikyo configuration and remote Apply

Status: selected implementation design, 2026-09-06. **Implementation is in progress. Local tests now cover the 26-key owner catalogue and application-generation replacement. Node overlays and deployment integration remain unfinished.** This document does not claim deployment support, merge approval or production changes.

## Authority and delivery boundary

The user revised D11: “every variable, with ability to remote apply. But only instance admin with 2fa auth (passkey, totp) can do it”. This supersedes the earlier nine-key scope. The user previously approved applying from Hikyo without restarting the container, separate projects for independent remotes, configuration sharing only within HA, and delegated subsequent recommendations. The user subsequently explicitly approved controlled bootstrap rollouts when required by startup-injected settings, while ordinary settings reload live.

Selected recommendation: cover the complete server configuration surface, retain one explicit authority per setting, and add a narrow managed deployment integration for settings whose durable authority lives outside the running application. Bootstrap transitions are part of completion, not exclusions hidden behind a read-only inventory. Unsupported or unconnected deployment integrations must produce an explicit unavailable operation, never a successful Apply.

PR #686 at `152373212c6d78a3bcae91e3300097ff0d893acf` implements protected project setup/adoption and activation of nine values: `HIKYO_MAIL_ADDR`, `HIKYO_MAIL_TLS`, `HIKYO_MAIL_USER`, `HIKYO_MAIL_PASSWORD`, `HIKYO_MAIL_FROM`, `HIKYO_MAIL_EHLO`, `HIKYO_MAIL_ALLOWED_CIDRS`, `HIKYO_MAIL_CA_PEM`, and `HIKYO_UPDATE_CHANNEL`. Its validation proves that scope only. See the [existing proposal](./self-configuration-proposal.md) and [validation record](../reports/self-configuration/validation.md). Subsequent authentication fixes need their own evidence.

## Catalogue and ownership

Use a single versioned descriptor registry, replacing duplicated key lists in `internal/config/config.go`, `internal/config/managed.go` and `internal/runtimeconfig/catalogue.go`. Each descriptor declares its canonical value name, startup aliases, type/default/absence rules, secrecy, validation dependencies, owning component, owner or node scope, activation class and required deployment capabilities. Preserve strict production validation, including development-only restrictions and authentication minimums.

“Every variable” means every setting consumed by the server, with explicit mappings for startup flags and file aliases. It does not mean importing arbitrary ambient process environment. CLI context and retired inputs remain classified and explained, not incorrectly activated as server settings. Unknown server keys are a schema mismatch. A registry coverage check must identify newly added parser inputs that lack an activation descriptor.

The logical instance owns its protected organization/project, desired revision and application generation. Independent remote instances retain separate values, identities, schemas and history. The root management view references those projects; it neither stores remote plaintext nor confers remote authority.

HA replicas share owner settings. Node settings require explicit overlays keyed by stable admitted node identity. An overlay is part of the published effective configuration and must never silently inherit a different node's value. Initial node identity and bootstrap discovery still originate from the deployment authority. Changing node identity is an explicit membership transition, not an ordinary shared string update. The overlay storage/schema format is an unresolved implementation interface below.

## Complete activation classification

The following classifications describe required behavior, not implemented support. A valid candidate can span classes; its apply plan must include every changed setting and dependency.

| Current inputs | Scope and representation | Activation responsibility |
| --- | --- | --- |
| Nine managed values listed above; startup aliases `HIKYO_MAIL_PASSWORD_FILE`, `HIKYO_MAIL_CA_FILE` | Owner; import file contents once into canonical encrypted values | Existing component replacement; preserve exact password bytes and explicit SMTP test behavior |
| `HIKYO_ARGON2_MEMORY_KIB`, `HIKYO_ARGON2_TIME`, `HIKYO_ARGON2_PARALLELISM`, `HIKYO_ADMISSION_BUDGET_MIB`, `HIKYO_REAUTH_WINDOW_SECONDS` | Owner policy; validate resource fit on every node | Replace authentication/admission graph; preserve shared counters and enforce floors |
| `HIKYO_DIRECTORY_PROXY`, `HIKYO_ADAPTER_EGRESS_POLICY_FILE`, `HIKYO_OIDC_EGRESS_POLICY_FILE`, `HIKYO_DYNAMIC_EGRESS_POLICY_FILE` | Owner; policy-file aliases import contents into typed canonical documents | Replace outbound clients/workers; cancel or drain old work under explicit policy-transition rules |
| `HIKYO_EXTERNAL_ORIGIN`, `HIKYO_TRUSTED_PROXY_CIDRS`, `HIKYO_MCP_ENABLED`, `HIKYO_MCP_ALLOWED_ORIGINS` | Owner | Replace HTTP/auth/federation/MCP graph; validate endpoint, RP and session consequences before cutover |
| `HIKYO_BACKUP_RECIPIENTS`, `HIKYO_BACKUP_INTERVAL`, `HIKYO_BACKUP_RPO`, `HIKYO_BACKUP_RETAIN_COUNT`, `HIKYO_BACKUP_RETAIN_DAYS`, `HIKYO_BACKUP_RTO_TARGET`, `HIKYO_AUDIT_ACCESS_RETAIN_DAYS`, `HIKYO_AUDIT_SECURITY_RETAIN_DAYS` | Owner | Replace backup/retention configuration and scheduler jobs under existing singleton fencing |
| `HIKYO_BACKUP_DIR` | Explicit node destination, with shared-storage requirements when applicable | Validate permitted destination and access before activation; deployment integration supplies missing mounts/permissions |
| `HIKYO_LISTEN`, `HIKYO_OPERATIONAL_LISTEN`; flags `--listen`, `--operational-listen` | Node | Reserve changed listeners, validate reachability, transition serving; deployment integration handles Service/Ingress/network changes where needed |
| `HIKYO_TLS_CERT_FILE`, `HIKYO_TLS_KEY_FILE`; flags `--tls-cert-file`, `--tls-key-file` | Node delivery; certificate/private-key contents encrypted through canonical configuration, not arbitrary remote file reads | Validate pair and endpoint coverage, replace TLS configuration; deployment integration handles externally owned sources |
| `HIKYO_PG_POOL_MAX` | Explicit node capacity setting | Replace database pools against the same verified datastore after draining users |
| `HIKYO_HA`, `HIKYO_NODE_ID` | Owner topology policy plus explicit node identity | Coordinated membership/admission/scheduler transition; require PostgreSQL, shared root authority and unique node identities |
| `HIKYO_DB` | Node bootstrap locator, normally referencing the shared datastore for HA; secret credentials stay protected | Managed deployment integration persists locator; distinguish verified reconnect from fenced data migration |
| `HIKYO_ROOT_KEY_FILE`, `HIKYO_ROOT_KEY`, `HIKYO_NEW_ROOT_KEY_FILE`; flag `--root-key-file` | External custody descriptors and dedicated rotation input; raw root keys never become project cells | Managed deployment integration and existing dual-wrap root rotation; verify durable replacement boot before retiring old wrapper |
| `HIKYO_UPGRADE_BUNDLE`, `HIKYO_UPGRADE_STATE_DIR`, `HIKYO_UPGRADE_EVIDENCE`, `HIKYO_UPGRADE_BACKUP`, `HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY`, `HIKYO_UPGRADE_OPERATOR_INSTANCE`, `HIKYO_UPGRADE_TARGET_MANIFEST`, `HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED`; flag `--auto-migrate` | Node installation/upgrade inputs and signed evidence | Manage through deployment/upgrade plan; changing references cannot manufacture signed evidence, assert writers stopped, or bypass the upgrade gate |
| `HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE`, `HIKYO_DEV_SERVICE_BUDGETS_DISABLED`, `HIKYO_DEV_ADAPTER_FAKE_PROVIDER`; flag `--dev` | Explicit deployment mode and test-only settings | Never enable production bypasses through remote Apply; require a validated development deployment transition before test-only settings are applicable |
| `HIKYO_STATE_DIR`, `HIKYO_TRUST_BUNDLE`, `HIKYO_CONTEXT`, `HIKYO_INSTANCE`, `HIKYO_ORG`, `HIKYO_PROJECT`, `HIKYO_ENV`, `HIKYO_TOKEN`, `HIKYO_COMPOSE_DOCKER`, `XDG_STATE_HOME` | Client-only inputs | Not consumed as server configuration; do not import client tokens into the server project |
| `HIKYO_UPDATER_SOCKET` | Retired software updater | Continue refusing enablement. Remote configuration Apply does not revive unsafe binary upgrades |

Canonical names for new content-based TLS and egress settings must be fixed in the registry contract before implementation. Never remotely dereference arbitrary paths supplied as normal project values. Root/private backup/signing custody is separate from ordinary project secret handling; private backup identities and operator signing keys are not server configuration.

## Application generation replacement

`internal/app/app.go` currently opens the datastore/keyring, constructs long-lived services and listeners, and only then calls `SelfConfig.LoadRuntime`. For expanded settings, load and validate the applied snapshot immediately after `openKeyed`, before constructing configuration-dependent services. Bootstrap discovery remains available before the datastore can be read.

Extract application graph construction from resource acquisition and serving. Keep an app-owned supervisor responsible for the datastore/keyring identity, configuration coordinator, listener ownership and recovery control. A generation owns immutable configuration, request handlers, outbound clients and worker contexts. No global `os.Setenv` mutation is an activation mechanism.

Implemented lifecycle seam, covered by both-engine installer tests:

```go
type PreparedActivation interface {
    Activate(context.Context) error
    Close() error
}

type RuntimeInstaller interface {
    Prepare(context.Context, *runtimeconfig.Bundle) (PreparedActivation, error)
}
```

Preparation parses values, checks local capabilities and reserves required resources without starting outbound jobs or acquiring active scheduler ownership. Unchanged listeners may retain their sockets while swapping the graph. Changed listeners must be reserved before commitment where possible. Candidate disposal releases exactly the resources it acquired. The supervisor must own activation so retiring a worker cannot cancel or deadlock its own installer.

Activation drains or cancels old requests and workers according to documented component policy, installs the prepared graph, then starts new workers with the existing fenced scheduler semantics. `SelfConfig.ReconcileRuntime` may acknowledge a generation only after real installation succeeds. Updating the mail bundle pointer is insufficient evidence that authentication, backup or listener settings changed.

Origin/RP changes require a recovery route and proof that an administrator can authenticate at the target origin. Existing passkeys may not be valid under a new RP ID. Preserve a bounded old-origin recovery path or equivalent independent operational recovery until target access is proved; do not infer successful access from an HTTP health response. Bind the exact transition and target to reauthentication.

## Managed deployment integration

The selected integration is a narrow authenticated capability provider, not a shell-command runner. It is bound to one owner and an explicit set of deployment/node resources. It advertises versioned capabilities and returns verifiable preparation/commit/observation receipts. No directory credential, viewing-server session or ordinary project machine identity can use it to apply changes.

Hikyo cannot persist new root sources or DB locators across replacement when Kubernetes controls immutable Secrets, environment and read-only mounts. A supervisor or sidecar alone does not solve that problem. A durable external authority and an authorized writer are required. Keeping a hidden second unlock root or a redirect in the old database merely relocates the dependency and is not selected.

| Deployment target | Required provider responsibilities | Current support claim |
| --- | --- | --- |
| Host/service installation | Own designated durable bootstrap configuration and root custody locations; atomic write, fsync, permissions, old/new generation journal, startup discovery and observation; access limited to declared installation resources | Required design; no implemented host provider claimed |
| Kubernetes managed resources | Narrow RBAC over declared workload/bootstrap resources; source-version CAS, staged resource creation, Secret/custody integration, safe delivery to nodes, replacement-pod observation and retained recovery material | Required design; no implemented Kubernetes provider claimed |
| GitOps-managed Kubernetes | Change the canonical GitOps source through its authorized workflow and observe controller reconciliation; never rely on a live patch that reconciliation will undo | Required design; no implemented GitOps provider claimed |

Active in-process configuration changes should not restart containers. Some immutable Kubernetes delivery mechanisms require pod replacement to receive changed external inputs. The provider must report that requirement before an apply plan is committed. The user explicitly approved that controlled rollout exception for bootstrap settings. Show the rollout impact before exact MFA authorization, persist the desired external source, observe replacement pods and report actual convergence. Ordinary setting activation must remain in-process. Never label a pod rollout an in-process reload.

Initial provider enrollment, resource authority and credential custody must be implemented and validated explicitly. Delegated product-design recommendations are not evidence that a provider is installed or that a deployment grants access. No current deployment credentials, endpoint, resource names or permissions are assumed by this design.

## Exact request, acknowledgements and recovery

The owner-side plan binds the published revision and overlay digests, schema, current generation and recovery incarnation, target owner, fixed participant set, affected capabilities, external source versions and any endpoint transition. Reauthentication binds the immutable plan digest as well as the existing exact revision target. Only a human instance administrator on that owner may authorize commitment using a fresh passkey or TOTP ceremony. Password-only authentication, recovery codes, cached disclosure windows and machine sessions are not substitutes.

Preparation must not perform a root cutover, change a DB locator or create an irreversible deployment effect before the exact decision is authorized. The final authorization transaction rechecks current owner authority and spends the ceremony once. Persist an idempotent intent before effectful external work. An authenticated provider executes only that recorded plan, within its resource/capability bounds. Repeated requests return the same operation; changed plans require new authorization.

Keep external receipts separate from node runtime acknowledgements. Receipts bind operation/owner/incarnation/plan digest/source versions and identify the observed stage. A node acknowledgement binds its stable membership identity, effective configuration digest and actually installed generation. A preparation receipt, successful API request or deployment rollout is not proof of active configuration or future bootability.

Pre-commit failure leaves the current target unchanged and releases preparation resources. Post-commit failure remains pending/partial and follows the recorded plan; never silently fall back to obsolete values or start an unrelated plan. Ordinary reversible components can retain the last usable graph while reporting the failed target, subject to explicit stale-generation fencing. Security-policy transitions must not continue operations under a policy that has been durably revoked. The transition policy must be specified and tested before those components are enabled.

Root rotation reuses `service.Rotation.RotateRootKey` and the keyring's dual-wrap protocol: prepare the new wrapper, durably install the new external source, verify participants and replacement boot, then retire the old wrapper. Raw root material never enters the project, logs or general configuration API. Root finalization must not occur on evidence from a stale/read-only mount.

A DB reconnect must prove the same owner, schema and recovery identity. Actual data migration needs a writer fence, authenticated destination, bounded cutover journal and durable locator transition before retiring the old source. Existing restore changes credential/recovery identity and suspends configuration; it cannot be reused as a transparent migration without an explicit new protocol. Cross-engine migration capability is not implied.

Crash recovery reconciles both the datastore job and external receipt journal before serving affected operations. Backup restore invalidates old incarnation-bound plans and receipts; resuming restores retains the existing exact reauthentication and credential-confirmation requirement. Host recovery must work without the application UI or a reachable old origin. External custody and locator recovery instructions must remain usable when the application datastore is unavailable.

## Required implementation seams and unresolved dependencies

These interfaces require implementation decisions and evidence. They are not completed infrastructure or a reason to call the requested scope complete.

| Area | Required seam or decision |
| --- | --- |
| Catalogue/storage | Versioned descriptor source, canonical content names, immutable owner/node overlay model, catalogue upgrade from the existing nine-key project, and retention of every effective activation payload |
| Application lifecycle | Graph factory and supervisor ownership, prepare/activate/dispose integration, request/worker draining, generation fence coverage and transactional node acknowledgements |
| Provider protocol | Transport and authentication library, enrollment/trust/revocation, operation schema and receipt verification, resource allowlists, idempotency/CAS, and deadlines for disconnected providers |
| Deployment implementations | Host durable-write/recovery implementation; Kubernetes versus GitOps authority selection; root custody adapter; running-process delivery and replacement-boot observation |
| Transition protocols | Origin authentication proof, per-node identity changes, irreversible retention consequences, datastore migration support matrix, root-finalization proof, mixed runtime/bootstrap plan ordering and recovery |

Applicable ownership boundaries and proof sites include `internal/service/self_config_apply.go`, `self_config_runtime.go`, `reauth_self_config.go`, `internal/authz/self_config.go`, `internal/app/app.go`, `ha.go`, `tls.go`, `internal/service/rotation.go`, and `internal/store/upgrade`. Update owning ADRs and transport/audit contracts before introducing new authority. Existing schemas or successful preparation must not be used to mint generic network, filesystem or deployment authority.

## Acceptance gates

1. **Coverage and authority:** every server parser input maps to a descriptor and tested activation path; unsupported deployments report exact missing capabilities. Direct and remote owner calls refuse non-admin humans and all machines, password/recovery substitutes, stale/replayed ceremonies, changed revisions, changed plan digests and cross-owner requests.
2. **Runtime and HA:** exercise every activation class, real component behavior, race/fault injection, listener collisions, origin changes, worker draining and fixed membership. Verify unchanged container/process identity for supported in-process transitions. Two independent remotes retain different variables while HA nodes converge only within their owner.
3. **Bootstrap durability:** test the selected host and Kubernetes/GitOps providers against their actual authority boundaries. A fresh process or replacement pod boots from the committed locator/root source with the old source unavailable. Read-only mounts, controller rollback, missing permissions, lost replies and stale resource versions never produce false success.
4. **Recovery and custody:** interrupt each root/datastore transition stage, restore backups, revoke provider credentials, lose a node and disconnect the viewer. Prove recoverability, writer fencing, root retirement ordering, no hidden second root, no arbitrary file reads and no plaintext root/project-secret exposure in receipts or logs.
5. **Delivery proof:** complete browser workflows on desktop/mobile, generated contracts and audit/isolation checks, appropriate both-engine/race suites, independent review and exact-head CI. Update the LAN report with per-capability implementation status and evidence. A design file, read-only catalogue, provider stub or green tests for the earlier nine keys does not satisfy this expansion.
