# Hikyo delivery-target condition reporting (ADR, PROPOSED 2026-09-23)

> **Status: PROPOSED, not locked.** Drafted for grilling on
> [#683](https://github.com/Hikyo-Org/Hikyo/issues/683). Every decision below
> carries lettered options and a recommendation; nothing here is operative
> until the owner locks it and the [oss-mechanics.md](./oss-mechanics.md)
> § Governance review has run (or been explicitly waived by the owner). **No
> reporting implementation is authorized by this ADR alone.** Until operative,
> the web UI keeps its current statement that Kubernetes conditions live only
> in the cluster.

## Context

The UI audit ([ui-audit-2026-09-05](../reports/ui-audit-2026-09-05/README.md),
[#680](https://github.com/Hikyo-Org/Hikyo/issues/680)) found no source for live
Kubernetes target conditions. Today:

- The operator only reads from the server. Per CR it sends
  `GET /api/v1/orgs/{org}/projects/{project}/environments/{env}/delivery` with
  `cursor`, `projection`, `acknowledged_keys` and `parameters`, under the CR's
  own workload credential (bootstrap token or TokenRequest federation). It
  sends no CR, namespace, cluster or `HikyoInstance` identity; only
  `User-Agent: hikyo-operator/<version>`.
- `HikyoSecret.status` carries a closed condition vocabulary
  (`internal/operator/api/v1alpha1/conditions.go`): `Synced`, `Designation`,
  `Conflict`, `Delivery`, `Scrubbed`, `Rollout`, `CredentialExpiry`,
  `PinExpired`, `Unreconciled`, `Ready`, each with a closed reason set, plus
  `lifecycle` (Synced/Retained/Scrubbed/Refused/Unreconciled) and
  `observedGeneration`.
- The server stores nothing about delivery targets. It does record one
  `identity.delivery_fetched` event per fetch (disposition full/current,
  credential, projection, cursor presented), which is the only thing the
  server **observes** about a target.
- The web UI's Kubernetes tab (`web/src/routes/MachineAccess.tsx`) states that
  no targets are reported and points at `kubectl`.

The server cannot infer reconciliation health from successful delivery: a
target can be `Conflict=True/ManagedSecretNotOwned`,
`Designation=False/SecretNotDesignated` or `Rollout=False/Stalled` without any
fetch failing, and some of those states never fetch at all. A live status view
therefore needs a deliberate controller-to-server channel, which the
[k8s-integration ADR](./k8s-integration.md) deferred as "new attack surface and
new availability coupling". This ADR designs that channel.

## Locked constraints this design must keep

- **The operator holds no Hikyo credential** (k8s-integration § Identity). No
  operator-wide reporter principal, ever.
- **Write ordering** (k8s-integration § Write ordering): Secret, workload
  patches, cursor. Reporting may never enter that sequence or gate it.
- **Integration-neutral server surface** (k8s-integration § Beside and beyond;
  api-cli-surface): no operator-special endpoint. The report endpoint is a
  delivery-target report any integration (Compose, a future ESO provider) could
  use.
- **Workload allowlist is closed** (permission-model § Machine principals;
  threat-model: "read-only, the only v1 workload capability"). Any machine
  write is an allowlist amendment and must be declared as one.
- **Unauthorized is indistinguishable from nonexistent** (permission-model).
- **Missing or stale must never read as healthy** (#683 acceptance).

## Decisions

### D1. Reporting authority

- **(a) Implied by `read`.** The fetch credential may report on the
  environment it reads. No allowlist change, no extra grant. But it turns the
  threat model's "read-only" into "read plus telemetry write" silently, and a
  stolen read credential can paint the UI green.
- **(b) New atom `report-delivery-status` at `(project, environment)` scope,
  added to the workload allowlist only.** Declared amendment to
  permission-model and threat-model. Explicit grant; revocable independently
  of `read`; the grant API still refuses every other write. The machine-access
  setup journey offers it as one checkbox beside `read`.
- **(c) Operator-wide reporter credential.** Rejected by the locked
  no-operator-principal rule; listed only so the rejection is recorded.

**Recommendation: (b).** The authority boundary stays legible in RBAC terms: a
grant list that says `read` means read. The cost is one extra grant per
reporting service account, carried by the setup journey.

### D2. What the server shows

- **(a) Ledger only.** Derive "last authenticated contact" from existing
  `identity.delivery_fetched` events. No new channel, but cannot show
  `Conflict`, `Designation`, `Rollout` or `Unreconciled`, and per-environment
  fetches are not per-target.
- **(b) Controller reports only.**
- **(c) Both, as two labelled layers.** *Observed by server*: last
  authenticated contact per principal and environment, from the ledger.
  *Reported by controller*: the conditions below. The UI never merges them into
  one health bit.

**Recommendation: (c).** The server's own observation is free and verified;
controller conditions are assertions and are shown as such.

### D3. Target identity and ownership

A report row is keyed by
`(service-account principal, environment, cluster id, HikyoInstance UID, CR UID)`:

- **Principal, never credential id**, so overlap rotation (mint, then revoke)
  keeps one row.
- **Cluster id** is the `kube-system` namespace UID, the conventional stable
  cluster identity; the operator reads it once at start (needs `get` on that
  one Namespace object, `resourceNames`-restricted).
- **CR UID** distinguishes a recreated CR from its predecessor; a new UID is a
  new row, the old one ages out (D6).
- Tenant ownership is the principal's project. A report naming an environment
  the principal holds no `report-delivery-status` on is a uniform 404.

Display labels: **(a)** namespace and CR name sent and shown; **(b)** opaque
UIDs only. **Recommendation: (a)**; namespace and CR name are what an operator
types into `kubectl`, they are already readable by anyone who can list CRs,
and hiding them makes the view unusable. They are bounded strings validated
against Kubernetes name grammar (DNS-1123, 63/253 chars) and are never
interpreted.

### D4. Payload and vocabulary

The report body is **closed and value-free**:

| Field | Content |
| --- | --- |
| `vocabulary` | vocabulary version the server advertised (D10) |
| `target` | cluster id, `HikyoInstance` UID, namespace, name, CR UID |
| `generation`, `observed_generation` | from CR metadata and status |
| `reported_at` | operator clock, RFC 3339 |
| `resync_interval_seconds` | the CR's effective interval, so staleness is per target |
| `lifecycle` | closed enum from `status.lifecycle` |
| `conditions[]` | `{type, status, reason, observed_generation}` from the closed set |
| `reporter` | `hikyo-operator/<version>` |

**Excluded, normatively:** condition `message` text, Event text, secret values,
key names (including undelivered-key lists), mapping, cursor, cursor binding,
stamp, managed Secret UID/resourceVersion, credential material, credential
expiry (the server already knows it), and any free text. A `type` or `reason`
outside the advertised vocabulary refuses the **whole** report (422, field
named); the server never stores an unrecognized string.

Key names are excluded even though a condition like
`Delivery=False/UndeliveredSecrets` is less useful without them: they are
already visible to readers of the environment, but a report is a machine write
and restating them adds a second copy the server must then authorize. The UI
links to the environment's key view instead.

### D5. Freshness and ordering

Derived UI state per row, computed at read time, never stored as "healthy":

| State | Rule |
| --- | --- |
| `unknown` | no row, or server/operator lacks the capability |
| `stale` | `now - received_at > 2 * resync_interval + 5 min` (5 min = max backoff), `resync_interval` clamped server-side to [30 s, 24 h] |
| `reporter-revoked` | the principal is deleted or holds no `report-delivery-status` now |
| `reported` | fresh; shows the conditions exactly as asserted, with "reported by controller, `<age>` ago" |

Ordering, per row:

- **(a) Generation monotonic, then operator timestamp monotonic within a
  generation.** Reject `observed_generation` lower than stored; within equal
  generation reject `reported_at` not later than stored. `reported_at` more
  than 5 min in the future is refused. Reconciles are already serialized per
  CR and the operator is a leader-elected singleton, so this only guards
  retries and leader handover.
- **(b) Last received wins.**

**Recommendation: (a).** Out-of-order refusals are answered 409 and are not
retried.

### D6. Deletion, retention and bounds

- **No finalizer.** A finalizer would make CR deletion depend on server
  reachability. On CR deletion the operator sends one best-effort tombstone;
  failure is an Event, nothing more.
- A row with no report for **7 days** is `stale` (already, by D5) and is
  **purged at 30 days**; tombstoned rows are purged immediately after the
  tombstone audit event. Principal deletion deletes its rows in the same
  transaction.
- **Latest-state table, not a log.** One row per key; history lives in the
  cluster.
- **Quota: 100 rows per service-account principal** (pins precedent), named
  409 refusal beyond it. Report size capped at 8 KiB.

### D7. Human read capability

- **(a) `read` on the environment.** Status is metadata, not values.
- **(b) `manage-identities` on the project.**

**Recommendation: (a)**, with the Kubernetes tab gated per environment the
viewer can read; rows for environments the viewer cannot read are absent, not
redacted.

### D8. Audit and budget

- **No audit event per report**: at a 5 min resync that doubles the
  conditional-fetch record volume for no accountability gain.
- Events: first report for a new row, tombstone, purge, and every refusal
  (authorization, vocabulary, ordering, quota), per audit-model conventions.
  Grant and revoke of `report-delivery-status` are grant-mutation events like
  any other.
- **Separate budget bucket**: 12/min per principal, 300/min per org, charged
  after authorization, so a report storm cannot starve fetches.

### D9. Operator behavior and failure

- Report **after** the cursor is persisted, once per reconcile outcome, and on
  CR deletion (tombstone). Every reconcile reports, so the periodic resync is
  the heartbeat.
- **Fire-and-forget.** One attempt per reconcile, 5 s timeout, no requeue, no
  faster retry. Failure emits a rate-limited Event; **no CR condition**
  (reporting about reporting is recursive and would perturb `Ready`).
- Reports use the same credential as the fetch; a federation token is reused
  within its 600 s life rather than minted twice.
- Helm value `operator.statusReporting`: **(a) default on**, capability-probed;
  **(b) default off**. **Recommendation: (a)**; it sends no values, uses no new
  cluster RBAC beyond the one Namespace `get`, and refuses cleanly without the
  grant.

### D10. Compatibility and rollout

- Server advertises `delivery-target-report` with a vocabulary version in
  `/api/v1/meta` `protocol_capabilities`. The operator reads `/meta` once per
  `HikyoInstance` (cached 10 min) and reports only when advertised, sending
  only the advertised vocabulary.
- **Old operator, new server**: no rows, UI `unknown`. **New operator, old
  server**: capability absent, no reports, no errors. A 404 on the report path
  disables reporting for that instance until the next `/meta` refresh.
- Vocabulary grows additively; a new condition type needs a new vocabulary
  version, and servers keep accepting older versions.
- Migrations: new closed capability atom, report table, audit event kinds,
  all roll-forward goose migrations on SQLite and PostgreSQL.

### D11. UI and remote transport

- Kubernetes tab lists rows grouped by cluster and namespace, each with the
  D5 state, the server-observed last contact (D2), and the asserted
  conditions. Copy says "reported by the controller"; nothing reads as
  independently verified cluster health. Empty list copy stays "no reports",
  never "healthy".
- Remote workspaces read the remote instance directly (existing workspace
  transport, no proxy) and map unreachable / credential-rejected to the
  multi-instance vocabulary. Remote directory snapshots carry **no** target
  status.

## Rejected

- **Operator-wide reporter credential** (D1c): the locked confused deputy.
- **Status piggybacked on the fetch request**: status after the write is only
  known after the fetch, fetches are per environment not per target, and it
  would make status a term of the audited disclosure request.
- **Finalizer-backed deletion**: couples CR deletion to server reachability.
- **Free-text messages**: Kubernetes condition messages can echo key names,
  paths and server error text; the closed vocabulary is the only safe payload.
- **Server-side verification of cluster health**: Hikyo has no cluster
  credential and must not gain one (signed-upgrade-compatibility).

## Amendments this ADR declares when locked

| ADR | Amendment |
| --- | --- |
| permission-model | closed atom set gains `report-delivery-status`; workload allowlist gains it at `(project, environment)` |
| threat-model | workload capability "read-only" becomes "read, plus value-free status report" |
| k8s-integration | deferred push channel stays deferred; status reporting is a separate, non-delivery channel; RBAC gains `get` on the `kube-system` Namespace |
| machine-identities | reports attributed to principal, not credential; rotation keeps rows |
| audit-model | new event kinds per D8 |
| ops-spec | quota, budget, staleness and retention values per D5, D6, D8 |
| api-cli-surface | integration-neutral report and list operations |

## Implementation tickets (filed only after lock)

1. **Governance**: amendment banners on the ADRs above, ops-catalogue rows with
   `x-hikyo-formula`, ADR index. No code. Depends on lock.
2. **Server**: atom and allowlist, report table and purge sweep, report and
   list endpoints, budget bucket, audit events, `/meta` capability. Depends on
   1. Acceptance: cross-tenant and revoked-principal reports are uniform 404;
   unknown vocabulary 422; out-of-order 409; quota 409; value-free schema
   pinned by a test that fails on any string field outside the closed enums
   and name grammar.
3. **Operator**: capability probe, reporter after cursor persist, tombstone
   on delete, Helm value, Namespace `get`. Depends on 2's OpenAPI contract.
   Acceptance: reporting failure never changes conditions, Secret or cursor;
   no report contains message text.
4. **Web UI**: Kubernetes tab rows and states, remote workspace mapping.
   Depends on 2. Acceptance: missing, stale and reporter-revoked never render
   as healthy; Storybook states for each.
5. **Validation**: end-to-end from a real operator (kind/k3d) to the browser:
   cross-tenant refusal, revoked credential, out-of-order update, stale after
   stop, deletion tombstone, secret-safe payload capture. Depends on 2, 3, 4.

## Open questions for grilling

D1, D2, D3 display labels, D5 ordering, D7 and D9 default are the choices; the
rest are consequences. Recommendations: D1 b, D2 c, D3 a, D5 a, D7 a, D9 a.
