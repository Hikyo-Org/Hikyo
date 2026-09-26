# Hikyo workload revision webhooks for non-Kubernetes hosts (ADR, proposed 2026-09-25)

> **Status: Proposed. Nothing here is locked and no implementation is
> authorized.** Every decision below lists its options with verdicts and one
> recommendation for the owner to grill on
> [#805](https://github.com/Hikyo-Org/Hikyo/issues/805). Locking follows the
> [oss-mechanics.md](./oss-mechanics.md) § Governance procedure; the
> amendments table at the end is a declaration of what locking would amend,
> not an edit to those ADRs.

## Context

A Kubernetes workload learns of a new effective revision because the operator
polls every 5 min (ops-spec § 7) and restarts on a changed change token. A
Compose or systemd host has the same pull path, `hikyo compose sync` on a
timer (compose-integration § Change propagation, ops-spec § 6: every 5 min),
but many hosts run no timer and re-export only when a CI deploy job runs.
Concrete case: DBugIT's VU production stack (Compose on a VM, machine
credential with `reveal`, workload pinned to a `production` revision). Changing
one value needs a human to re-run the deploy job
(DBugIT_V3/Rewrite#599).

#805 asks for an outbound, signed, value-free notification when a workload's
effective revision changes, so the host (or a CI system) can run its own
authorized fetch without polling.

Verified facts in source this design builds on:

- **A pin is per workload principal and environment.** `revision_pins` is
  unique on `(org_id, project_id, environment_id, workload_principal_id)`
  (`internal/store/migrations/postgres/00022_rollback_pins.sql:39-57`); the
  workload principal is the `mch_` machine principal, the service-account row
  is `sa_`. Workload deletion cascades its pins.
- **A same-revision pin is already a distinct action.** `Pins.Set` computes
  `renewing := existing.Revision == target.Revision` and emits `pin.renewed`
  instead of `pin.created` / `pin.reassigned`
  (`internal/service/pins.go:234`, `internal/audit/registry.go:444-448`).
  Pin expiry never changes delivery (ops-spec § 8, Pins & grants).
- **Publish already enqueues outbound work inside its own transaction.**
  After `revision.published` is inserted, `r.Adapters().EnqueuePublished`
  runs in the same transaction and emits `adapter.sync_requested` per job
  (`internal/service/publish.go:734-775`); the only post-commit effect is the
  advisory SSE (`publish.go:556-559`).
- **The adapter outbox is the claim pattern.** `adapter_outbox` carries
  authority principal, dedup key, attempt count, `next_attempt_at`,
  `lease_owner`, `lease_expires_at`, terminal states including `superseded`
  (`00024_adapter_outbox.sql:212-235`), claimed with
  `FOR UPDATE OF j SKIP LOCKED` under a per-target single-flight and a
  per-org cap of 4 (`internal/store/adapter_runtime.go:465-468`).
- **One egress policy implementation exists.** `netpolicy.PublicDialer`
  resolves, validates the exact dialed address against `IsNonPublic`, and
  admits only operator-configured `AllowedCIDRs`
  (`internal/netpolicy/public.go:35`, `:154`). Each integration gets its own
  operator policy file: `HIKYO_ADAPTER_EGRESS_POLICY_FILE`,
  `HIKYO_OIDC_EGRESS_POLICY_FILE`, `HIKYO_DYNAMIC_EGRESS_POLICY_FILE`
  (`internal/config/config.go:297-299`), plus `HIKYO_MAIL_ALLOWED_CIDRS`.
  Adapter clients refuse redirects (`internal/adapter/forgejo/client.go:116`).
- **No tenant-usable runtime signing key exists.** Upgrade statements are
  release-time, offline-root or Sigstore signed
  (signed-upgrade-compatibility § Release identity and trust). The only
  runtime ed25519 key is the self-configuration rollout signer, loaded from an
  operator file and bound to `configrollout` commands
  (`internal/app/self_config_deployment.go:445`, `:633`).
- **Adapter outbound credentials are envelope-encrypted and write-only**
  (`adapters.credential_ciphertext`; encryption-model: project DEK,
  `project_field`; permission-model § Adapters).

## Locked constraints this design must keep

- **No resident client agent.** compose-integration § Deferred and
  § Propagations ("MUST NOT introduce a resident client-side agent for
  Compose"), mvp-boundary row "Local agent daemon / resident watcher: Out",
  re-affirmed at #147. `compose sync` stays one-shot.
- **A notification is advisory, never delivery.** revision-model § Live
  updates: metadata only, never values; correctness never depends on
  delivery; clients refetch under their own authorization. The receiver's
  fetch is the only disclosure path and keeps its full formula, cursor and
  per-key audit (compose-integration § Authorization).
- **Outbound destinations are enumerated.** threat-model § Telemetry: the only
  outbound connections are enumerated user-configured functional
  destinations; multi-instance and SMTP each joined by declared amendment.
  Zero configured webhooks must mean zero webhook egress (mvp-boundary O7).
- **Tenant-configured destinations are an SSRF surface** (threat-model trust
  boundary 4): https only; default-deny of non-global ranges with exceptions
  **only by the instance operator**; rebinding-safe dialing; proxies enforce
  the same policy or are refused; no redirects; response caps.
- **External effects are audited INTENT then OUTCOME** (threat-model § Audit
  integrity; audit-model envelope `intent`/`unknown` restricted by registry).
- **No generic job framework** (system-architecture § Jobs, #147 amendment):
  a new durable queue is a separate domain outbox of the same shape.
- **Standing delegations record an authority principal**, re-checked before
  every external effect, reassigned atomically on any routing mutation
  (permission-model § Adapters and the registration-policy amendment).
- **No secrets on argv**; credentials shown once, never retrievable
  (api-cli-surface § Output grammar; machine-identities § Delivery).
- **UI and CLI parity** for every server-mediated capability
  (api-cli-surface § Parity).
- **Unauthorized is indistinguishable from nonexistent** (permission-model).

## Decisions

### D1. Trigger semantics

The trigger is a change of the **effective revision** of a
`(workload principal, environment)` pair, computed inside the transaction that
causes it:

| Cause | Fires when |
| --- | --- |
| `publish` (incl. rollback-publish, `apply`) | the workload has no pin in that environment and the environment's latest revision advanced |
| `pin.created` | the pinned revision differs from the workload's previous effective revision (latest) |
| `pin.reassigned` | the new pinned revision differs from the old one (always, by construction) |
| `pin.released` | the released pin's revision differs from the environment's latest |
| `pin.renewed`, pin expiry, publish while pinned | never |

A pin created at the current latest, or released while equal to latest,
changes nothing and fires nothing. A pinned payload later collected makes
delivery fail loud (ops-spec § 8); that is not a revision change and does not
fire.

**Guarantee:**

- **(a) Exactly-once delivery.** Unachievable over HTTP: a timeout after the
  receiver processed the request is indistinguishable from a lost request.
  Promising it is a false claim.
- **(b) Exactly one delivery record per transition, at-least-once attempts,
  stable idempotency key.** Each transition creates one delivery row with a
  `delivery_id` (UUIDv7) sent as `webhook-id`; retries resend the same id and
  body. Receivers dedupe on it. Security: neutral. Operability: standard
  webhook contract. Complexity: low. Fit: matches the adapter dedup key.

**Recommended: (b).** The #805 acceptance line "exactly one delivery" is
restated as "exactly one delivery record, and at most one successful
acknowledgement is required".

**Coalescing:**

- **(a) FIFO, every transition delivered in order.** Receivers see full
  history; a down receiver accumulates a queue; history is already in the
  revision lineage.
- **(b) Supersede to latest** (the adapter rule): a new transition for the
  same webhook marks any queued or retrying delivery `superseded`; one
  delivery in flight per webhook. The superseded row stays in the log. The
  new body's `previous_revision` is the effective revision before this
  transition, so nothing is lost for a receiver that only needs "current".
  Security: bounds queue growth. Operability: a returning receiver gets one
  delivery, not a backlog. Complexity: low (existing pattern). Fit: identical
  to deployment-adapter § Supersede.

**Recommended: (b).** Ordering guarantee: per webhook, deliveries are created
in commit order and at most one is in flight; no ordering across webhooks.

### D2. Scope

- **(a) Per `(workload principal, environment)`.** Fires exactly when that
  workload's effective revision changes; pins are respected. Security:
  narrowest disclosure. Operability: one hook per deployed stack, the #805
  case. Complexity: effective-revision computation per pair at publish, over
  webhooks only (not all workloads). Fit: pins are keyed the same way.
- **(b) Per environment.** Fires on every publish; pin changes need a second
  rule; pinned workloads receive spurious notifications. Simpler, wrong for
  the pinned production case that motivated the issue.
- **(c) Per delivery target** (`delivery_target_reports`, #788). Only the
  Kubernetes operator reports targets; Compose hosts have no row. Rejected
  for this use case.

**Recommended: (a).** One environment per webhook; a workload reading several
environments gets several webhooks. Webhooks are keyed on the principal, never
a credential id, so credential rotation keeps them (k8s-condition-reporting D3
precedent). Principal, environment or project deletion deletes the affected webhooks
(and their queued deliveries) in the same transaction, so no webhook names a
dead `(principal, environment)` pair. The issue's `--workload` flag gains
`--env` accordingly.

### D3. Payload

Closed, value-free JSON, at most 2 KiB, served as `application/json`:

| Field | Content |
| --- | --- |
| `schema` | `hikyo.webhook/v1` |
| `event` | closed enum: `revision.changed`, `test` |
| `delivery_id` | UUIDv7, equal to the `webhook-id` header |
| `webhook_id` | immutable webhook id |
| `instance` | the instance's configured public origin |
| `org_id`, `project_id`, `environment_id` | immutable ids, never mutable names |
| `workload_principal_id` | the `mch_` id |
| `cause` | closed enum: `publish`, `pin.created`, `pin.reassigned`, `pin.released`, `test` |
| `previous_revision`, `revision` | integers; `previous_revision` is absent (never null) on the first revision, per the OpenAPI 3.1 profile's nullable prohibition |
| `occurred_at` | commit time of the causing transaction, RFC 3339 UTC |

Excluded normatively: values, key names, changed-key counts, publisher
identity, pin authority, and free text. A `test` body carries the same fields
with the current effective revision in both revision fields and `cause`
`test`.

**`change_token`, which #805 lists:**

- **(a) Include it.** The token is designed as non-secret metadata (keyed per
  scope, safe in pod annotations). Lets a receiver skip a restart when a
  republish delivered identical content. But revision-model § Live updates
  says the metadata channel "never carries change tokens", so this needs a
  revision-model amendment, and it hands a per-environment equality signal to
  a destination outside Hikyo's authorization.
- **(b) Omit it.** The receiver's next action is `compose sync`, whose
  conditional fetch already answers "current" and delivers nothing when
  content is unchanged. No amendment. Security: strictly less disclosure.
  Operability: an identical-content republish costs one conditional fetch.

**Recommended: (b).** The revision numbers are enough to trigger; the fetch is
the authority on content.

### D4. Signing and secret custody

- **(a) HMAC-SHA256 with a per-webhook secret, Standard Webhooks format.**
  Headers `webhook-id`, `webhook-timestamp`, `webhook-signature: v1,<base64>`
  over `"{id}.{timestamp}.{raw body}"`. Security: authenticity plus replay
  bound; the server must hold the secret recoverably. Operability: verifiers
  exist in every language, and the format is an open spec. Complexity: low.
  Fit: custody reuses the adapter credential pattern.
- **(b) Hikyo-signed ed25519 statement.** The receiver needs only a public
  key. But there is no tenant-usable runtime key: release keys are offline,
  and the self-configuration signer is purpose-bound to rollout commands.
  This needs a new runtime key class in encryption-model (custody, rotation,
  restore behaviour, domain separation), a public-key discovery endpoint and
  a receiver-side pinning story. Complexity: high for no receiver benefit.
- **(c) Both.** Two verification procedures to document and test.

**Recommended: (a).**

Custody and lifecycle under (a):

- The secret is 256 bits, server-generated by default and displayed exactly
  once at create or rotate as `whsec_<base64>`. `--secret-file PATH`
  alternatively supplies one (at least 32 bytes after decoding; refused
  otherwise). Never on argv, never returned by list or show.
- Stored under the project DEK as a `project_field` envelope, like
  `adapters.credential_ciphertext`; plaintext exists only in the delivery
  attempt's operation scope (system-architecture § Crypto boundary).
- **Rotation with overlap:** `webhook rotate-secret` keeps the previous
  secret for 24 h and sends both signatures, space-separated, as the format
  allows; then the old one is destroyed. `--now` destroys it immediately.
- **Restore:** threat-model restore invalidates every adapter credential;
  webhook secrets follow the same rule. Restored webhooks come up `disabled`
  until the secret is rotated.
- The `whsec_` grammar joins the audit-model free-text redaction filter and
  the secret-scanning ruleset, so a pasted secret never persists in the trail.

### D5. Delivery machinery

- **(a) A third domain outbox, `webhook_deliveries`, enqueued in the causing
  transaction.** The publish and pin transactions insert delivery rows exactly
  where `EnqueuePublished` runs today; the worker in `hikyo server` claims
  with `FOR UPDATE SKIP LOCKED` on PostgreSQL (single writer on SQLite), one
  in flight per webhook, 4 per org. The row doubles as the delivery log.
  Security: no effect escapes before commit; crash-safe. Operability: works
  in HA with no new component. Complexity: one table, one worker, copied
  shape. Fit: system-architecture § Jobs and the #147 precedent (separate,
  not generalized).
- **(b) Model a webhook as an adapter provider.** Adapter configure carries
  `manage-adapters ∧ reveal(E)` plus reauthentication, a ledger, sentinels and
  a converge model built for secret-bearing pushes. Wrong shape and wrong
  authority for a value-free notification.
- **(c) Post-commit in-process send.** Lost on crash, no INTENT record,
  violates the threat-model external-effect rule.

**Recommended: (a).**

Retry and dead-letter:

- **(a) Adapter curve, never give up** (30 s to 1 h, jittered). With
  supersede this is one row per webhook, but a permanently dead receiver is
  hit hourly forever.
- **(b) Same curve, bounded:** give up after 24 h and set the delivery
  `dead`; the webhook shows `failing` until a later delivery succeeds;
  `webhook redeliver <delivery-id>` re-enqueues a dead delivery with the same
  id and body, and is refused by name when a newer delivery exists for that
  webhook (the newer one already carries the current state).

**Recommended: (b).**

Attempt rules: 10 s total deadline; `2xx` success; `3xx` failure, never
followed; `408`, `429`, `5xx` and network errors retry; other `4xx` end the
delivery `rejected` (receiver misconfiguration needs a human, not retries).
The response body is read up to 4 KiB and discarded; the log stores status
code, latency, attempt count and a closed error class, never response bytes
or headers. Before every attempt the worker re-checks the authority formula
(D7) and that the webhook is still enabled; failure ends the delivery
`aborted` with an audit event.

### D6. SSRF and egress

- **(a) Reuse `netpolicy.PublicDialer` with a new operator-only
  `HIKYO_WEBHOOK_EGRESS_POLICY_FILE`.** URL grammar: `https` only, no
  userinfo, no fragment, port allowed. Dial the validated resolved address
  (rebinding-safe by construction); `Proxy: nil` unless an explicit
  operator-configured proxy that applies the same policy; no redirects; TLS
  via WebPKI or an operator CA file (the SMTP precedent), never skip-verify.
  Security: identical to adapters. Operability: an RFC1918 receiver such as
  the VU host needs the operator to list its CIDR once, deliberately.
  Complexity: low. Fit: threat-model boundary 4 as written.
- **(b) Let tenants mark a webhook as private-network.** Violates the
  operator-only exception rule.
- **(c) Allow plain `http` for LAN receivers.** The signature protects
  integrity, but the body discloses publish timing in clear and the rule is
  https-only.

**Recommended: (a).**

URL sensitivity: receivers commonly embed a bearer secret in the path or query
(Home Assistant webhook ids, relay tokens). The full URL is therefore stored
under the project DEK beside the secret, and list, show, the delivery log and
audit payloads carry only the origin plus a SHA-256 fingerprint of the full
URL.

### D7. Permissions and audit

Who may create:

- **(a) `manage-identities(project)` ∧ `read(E)`.** A webhook attaches to a
  workload principal, which `manage-identities` already administers; `read(E)`
  covers the metadata disclosed (when E changed, to which revision). No new
  atom, no allowlist change: no machine principal holds `manage-identities`.
  Security: adequate for value-free metadata. Complexity: none added. Fit:
  existing templates.
- **(b) New atom `manage-webhooks(project)`.** Separately grantable, but it is
  a closed-set amendment for a capability whose risk (metadata egress to an
  operator-policed destination) sits below credential minting.
- **(c) `manage-adapters(project)`.** Conflates a value-free notification
  with secret-bearing adapter authority.

**Recommended: (a).** No reauthentication (nothing disclosed is value-bearing).
The webhook is a **fifth standing delegation**: create records the acting
principal as authority; any change of URL, environment or secret atomically
reassigns authority to the actor; every attempt re-checks the authority's
`manage-identities(project) ∧ read(E)`. `delete` and `list` need
`manage-identities(project)` alone; `test`, `redeliver` and `deliveries` need
the full formula.

Audit:

- **(a) Per-attempt INTENT and OUTCOME events**, as the threat model states
  for external effects, plus `webhook.created`, `webhook.updated` (with
  authority reassignment), `webhook.secret_rotated`, `webhook.deleted`,
  `webhook.delivery_requested` (in the causing transaction, like
  `adapter.sync_requested`), `webhook.superseded`, `webhook.dead`,
  `webhook.aborted`. Volume is bounded by supersede and the 24 h curve
  (about 30 attempts per dead delivery).
- **(b) Delivery log as the only per-attempt record**, audit for lifecycle
  only. Lighter, but departs from the locked INTENT/OUTCOME rule and needs
  an argument that a value-free effect is exempt.

**Recommended: (a).** New `webhook` audit category, correlation id = delivery
id.

### D8. CLI, API and web UI

Verbs (human session only in the artifact matrix; JSON output per
api-cli-surface):

```
hikyo webhook create --workload mch_… --env <env> --url-file PATH [--secret-file PATH]
hikyo webhook list [--workload mch_…] [--env <env>]
hikyo webhook show <webhook-id>
hikyo webhook delete <webhook-id>
hikyo webhook test <webhook-id>
hikyo webhook rotate-secret <webhook-id> [--now]
hikyo webhook enable|disable <webhook-id>
hikyo webhook deliveries <webhook-id> [--state …]
hikyo webhook redeliver <delivery-id>
hikyo webhook verify --secret-file PATH        # client-local, see D9
```

`--url-file` (or stdin) replaces a `--url` flag because receiver URLs often
embed a bearer token (D6) and argv carries no secrets. `test` enqueues a `test` event through the same outbox, egress policy and
signing, and appears in the delivery log. Endpoints live under
`/api/v1/orgs/{org}/projects/{project}/webhooks`, with the delivery log paged
per api-cli-surface limits.

Web UI parity is mandatory, not optional: the machine-access workload view
gains a webhooks panel (create with one-time secret display, enable/disable,
rotate, test, delivery log with state and error class, redeliver). The UI copy
says "notification", never "synced" or "delivered values".

### D9. Receiver verification and the client-local verifier

Verification procedure (normative, published with test vectors):

1. Read the raw body bytes; do not parse JSON first.
2. Reject if `webhook-timestamp` (Unix seconds) differs from local time by more than 5 min.
3. Compute `HMAC-SHA256(secret, "{webhook-id}.{webhook-timestamp}.{body}")`,
   base64-encode, and compare in constant time against each `v1,` entry of
   `webhook-signature`; accept if any matches (rotation overlap).
4. Reject a `webhook-id` already processed (keep ids for at least 24 h).
5. Parse the body strictly against `hikyo.webhook/v1`; unknown `schema`
   refuses.
6. Treat the body as a hint: trigger an authorized fetch; never act on
   `revision` as if it were content.

Receiver shape for Compose/systemd:

- **(a) Documentation only**, with Go and Python snippets. Every host then
  writes HTTP parsing and HMAC code in shell-adjacent glue.
- **(b) `hikyo webhook verify`, a client-local verb.** Reads one HTTP/1.1
  request on stdin, verifies per the procedure (id cache in the state
  directory), writes `204` or `401` to stdout and exits 0 or non-zero. It
  holds only the webhook secret, never a Hikyo credential, contacts no server
  and runs once per connection under socket activation, so it is not a
  resident agent. Complexity: one small verb and a shared verifier package
  also used by the server tests.

**Recommended: (b).**

Reference systemd recipe (TLS terminated by the host's reverse proxy, which
forwards to loopback):

```ini
# hikyo-webhook.socket
[Socket]
ListenStream=127.0.0.1:9180
Accept=yes

# hikyo-webhook@.service
[Service]
Type=oneshot
LoadCredentialEncrypted=webhook-secret
StandardInput=socket
StandardOutput=socket
ExecStart=/usr/local/bin/hikyo webhook verify --secret-file ${CREDENTIALS_DIRECTORY}/webhook-secret
ExecStartPost=/usr/bin/systemctl start --no-block hikyo-sync.service
```

`Type=oneshot` is load-bearing: `ExecStartPost` then runs only after
`verify` exits 0, so an unverified connection never starts a sync.
`hikyo-sync.service` is the existing one-shot `hikyo compose sync` unit with
its own `LoadCredentialEncrypted=hikyo-token`: the receiving unit never holds
the reveal-capable credential, and the 5 min timer stays as the fallback.

Forgejo/GitHub `workflow_dispatch` needs an `Authorization` header Hikyo
cannot send without holding a forge token:

- **(a) Relay on the receiving side.** The same socket unit, whose
  `ExecStartPost` calls
  `POST {forge}/repos/{owner}/{repo}/actions/workflows/{file}/dispatches`
  with a forge token from its own systemd credential and
  `{"ref":"main","inputs":{"revision":"…"}}`. Hikyo holds no third-party
  credential.
- **(b) One optional static header per webhook**, stored write-only under
  adapter credential custody. Hikyo gains a third-party credential per
  webhook, with restore, rotation and redaction obligations.
- **(c) A native forge-dispatch kind.** That is an adapter, with adapter
  authority; out of scope.

**Recommended: (a)** for v1; (b) deferred until a receiver cannot run a relay.
The Forgejo endpoint's version floor is pinned at implementation.

### D10. Alternatives to a server-side webhook

- **Host agent** (`hikyo agent --workload … --exec`, long-poll). Simplest
  receiver, but a resident process holding a reveal-capable credential, which
  compose-integration, system-architecture propagation and mvp-boundary
  (re-affirmed at #147) all rule out. **Verdict: rejected unless those ADRs are
  reopened.** Security: worst (standing decryption capability in a daemon).
  Fit: direct conflict.
- **Polling timer** (`compose sync` every 5 min, available today). No Hikyo
  change, no inbound port on the host, correctness identical; latency up to
  the timer interval; the server records each conditional fetch
  (`fetch` events) but nothing links a restart to a publish. **Verdict:
  remains the documented baseline and the fallback under every receiver
  recipe.** The webhook only shortens latency and removes the CI hop.
- **Server-sent events to a CLI subscriber.** Same resident-process objection
  as the agent.

## Proposed operational values

| Value | Proposed default |
| --- | --- |
| Webhooks per project | 50; per workload principal 5 |
| Attempt deadline | 10 s total, response read cap 4 KiB (discarded) |
| Retry curve | exponential 30 s to 1 h cap, jittered; `dead` after 24 h |
| Concurrency | 1 in flight per webhook, 4 per org, claimed by lease |
| Lease | 60 s, renewed per attempt |
| Timestamp tolerance (receiver) | 5 min |
| Secret overlap on rotate | 24 h |
| Delivery log retention | 30 d or last 200 rows per webhook, whichever is fewer |
| `test` / `redeliver` budget | 10/min per principal, inside the adapter trigger bucket |
| Body size | at most 2 KiB |

## Rejected

- **Exactly-once promise** (D1a): not achievable over HTTP.
- **Values or key names in the body**: turns a hint into a delivery channel
  outside the fetch formula and its per-key audit.
- **Webhook as adapter** (D5b) and **post-commit send** (D5c).
- **Tenant private-network exception and plain http** (D6b, D6c).
- **Resident host agent and SSE subscriber** (D10).

## Amendments this ADR declares when locked

| ADR | Amendment |
| --- | --- |
| threat-model | enumerated outbound destinations gain **webhook receivers**, under the D6 control set |
| permission-model | fifth standing delegation (webhook); formula rows for create/update/rotate/test/redeliver/deliveries/delete/list per D7 |
| audit-model | new `webhook` category and event types per D7; `intent`/`unknown` outcomes permitted on the attempt pair; `whsec_` in the redaction filter |
| system-architecture | a third domain-specific outbox (`webhook_deliveries`), not a generalization |
| encryption-model | webhook secret and full URL are `project_field` envelopes; restore disables webhooks until rotation |
| api-cli-surface | `webhook` verb family joins the closed set; human-session only; `webhook verify` joins the client-local class |
| compose-integration | the "workload refresh (fog)" item narrows: Hikyo notifies, never restarts; socket-activated verifier recipe; no agent |
| k8s-integration | push delivery stays deferred; webhooks are a notification channel, not delivery |
| mvp-boundary | new row: revision webhooks In (post-lock); agent row unchanged |
| ops-spec | values table above |
| secret-scanning | `whsec_` pattern |
| revision-model | only if D3 (a) is chosen: live-update rule excludes change tokens except in webhook bodies |

## Acceptance criteria

1. Publishing to an environment creates exactly one delivery record per
   enabled webhook whose workload is unpinned there, and none for pinned
   workloads.
2. `pin create` to a revision different from the current effective revision,
   `pin reassign`, and `pin release` to a different latest each create exactly
   one record; a same-revision pin (`pin.renewed`), a no-op create or release
   at latest, and pin expiry create none.
3. A rolled-back publish or pin transaction leaves no delivery row.
4. Two server replicas never send the same delivery concurrently; a killed
   worker's delivery is retried under the same `webhook-id`.
5. Requests to non-public addresses outside the operator policy, redirects,
   `http` URLs and DNS rebinding to a private address are refused and logged
   by closed error class.
6. The published test vectors verify with `hikyo webhook verify`, the Go
   verifier and one third-party Standard Webhooks library; a tampered body,
   stale timestamp or replayed id fails.
7. No request body, log row or audit payload contains a value, key name,
   secret or full URL (canary test).
8. The Compose/systemd recipe and the `workflow_dispatch` relay are
   documented and exercised end to end on a real host.

## Implementation tickets (after lock)

1. **Governance**: amendment banners on the ADRs above, ops-catalogue rows,
   mvp-boundary row, handoff doc. No code.
2. **Server**: migrations (SQLite and PostgreSQL, goose roll-forward) for
   `webhooks` and `webhook_deliveries`; enqueue in the publish and pin
   transactions; worker with lease claim, egress policy file, signing,
   retry, supersede, dead; authorization registry rows; audit registry
   entries; OpenAPI endpoints and contract tests. Depends on 1.
3. **Shared verifier and CLI**: `internal/webhooksig` used by server tests and
   the CLI; `hikyo webhook` verbs including `verify`; test vectors.
   Depends on 2's OpenAPI contract.
4. **Web UI**: workload webhooks panel, one-time secret dialog, delivery log,
   Storybook states for `failing`, `dead`, `disabled`, `superseded`.
   Depends on 2.
5. **Docs**: verification procedure, systemd socket recipe, forge relay
   recipe, operator egress policy for private receivers, DBugIT VU walkthrough.
   Depends on 3.
6. **Validation**: end-to-end acceptance 1 to 8 on a two-replica PostgreSQL
   harness and a SQLite single node. Depends on 2 to 5.

## Recommendations at a glance

D1 (b) one record per transition, at-least-once, `webhook-id` dedup; (b)
supersede to latest. D2 (a) per workload principal and environment. D3 value-free
closed body; (b) omit `change_token`. D4 (a) HMAC, Standard Webhooks,
project-DEK custody, 24 h rotation overlap. D5 (a) third domain outbox in the
causing transaction; (b) bounded 24 h then `dead`. D6 (a) `PublicDialer` plus
operator-only webhook egress policy; URL stored encrypted. D7 (a)
`manage-identities ∧ read(E)`, standing delegation; (a) INTENT/OUTCOME per
attempt. D8 full `webhook` family with UI parity. D9 (b) client-local
`webhook verify`; (a) receiver-side forge relay. D10 agent rejected, polling
kept as baseline.
