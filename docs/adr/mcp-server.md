# Hikyo MCP phase 1 (ADR, locked 2026-09-04)

Context: the implementation research in
[hikyo-mcp-server.md](../research/hikyo-mcp-server.md) established that Hikyo
can expose a useful remote Model Context Protocol surface without another
service or an OAuth authorization server. This ADR locks phase 1: a controlled,
read-only MCP adapter in `hikyo server`, authenticated by existing
service-account bearers and confined to existing service and authorization
operations. OAuth human delegation remains a separate necessity decision under
[#631](https://github.com/Hikyo-Org/Hikyo/issues/631).

This ADR was locked by [#627](https://github.com/Hikyo-Org/Hikyo/issues/627).
It amends four locked decisions through banners committed with it:

- [api-cli-surface.md](./api-cli-surface.md) gains `/mcp` as one separately
  versioned protocol endpoint. It is not a REST domain endpoint, an OpenAPI
  operation, or a UI/CLI parity obligation.
- [audit-model.md](./audit-model.md) gains `mcp` in the closed audit-origin
  enum. Event types and successful-read audit dispositions do not change.
- [ops-spec.md](./ops-spec.md) gains the concrete phase-1 request, traversal,
  response, deadline, rate, and concurrency bounds.
- [threat-model.md](./threat-model.md) gains the MCP ingress and model-context
  boundary defined below.

No capability, principal class, credential class, audit event type, datastore
authority, or secret-disclosure path is added.

## Decision

Hikyo serves MCP from the existing public listener at the one canonical URI
`HIKYO_EXTERNAL_ORIGIN + /mcp`. Production use requires the configured external
origin to be HTTPS. Plain HTTP is accepted only when the external host is a
loopback literal or `localhost` and development mode is explicitly enabled.
The route is disabled unless MCP is explicitly enabled.

The server uses `github.com/modelcontextprotocol/go-sdk` at exactly `v1.7.0`.
Dependency upgrades require a reviewed changelog and the same protocol,
security, and interoperability tests as the initial pin. A floating version or
an SDK default is not policy.

The normative and only accepted protocol is `2026-07-28`. The migration policy
is explicit refusal, not accidental partial compatibility: `2025-11-25` and
older clients receive the pinned unsupported-version error and operator-facing
upgrade guidance. That legacy lifecycle requires `initialize`,
`notifications/initialized`, and negotiated connection state, which conflicts
with this phase's stateless, independently routable contract. A newer version,
or a stateful legacy profile, is rejected until intentionally added by an ADR
amendment with pinned conformance and named-client tests.

Transport is Streamable HTTP with these fixed properties:

- `POST /mcp` only. `GET` and `DELETE` return `405` with `Allow: POST`.
- `Stateless: true`. No protocol session id is created, accepted as authority,
  echoed, or required. Requests need no replica affinity.
- `JSONResponse: true`. Ordinary replies are JSON, not SSE. Request-scoped SSE,
  resumability, and server-initiated streams are out.
- One JSON-RPC request or notification per POST. Protocol headers and body
  metadata must match exactly as required by the pinned protocol.
- Notifications return `202` without a body. Unsupported versions, missing or
  mismatched headers, and unknown methods use the pinned MCP error contract.
- Client disconnect and request cancellation cancel service work and the
  transaction. No handler starts detached work.

`server/discover`, `tools/list`, and `tools/call` are the only implemented MCP
control methods. The server advertises tools with `listChanged: false` and no
other server capability. `server/discover` and `tools/list` are
unauthenticated-class public metadata: they return one static, tenant-free
catalog, never resolve a presented bearer, and require no Hikyo service
operation. Like `/api/v1/meta`, each is name-pinned in the audit-exemption
fixture with that exact rationale. They remain under pre-auth per-IP admission.
Tenant facts are available only through an authenticated tool call.

## Authentication and adapter boundary

Phase-1 `tools/call` accepts only an existing Hikyo `hikyo-token` belonging to
a `workload` or `automation` service account. It arrives in exactly one
`Authorization: Bearer <token>` header. Query parameters, cookies, tool
arguments, `_meta`, client information, and forwarding headers never carry or
replace it. Browser, CLI-session, workspace-session, provisioning, and instance
connection artifacts are refused.

The MCP transport performs syntax and size checks, then projects its selected
closed registry row into a shared, transport-independent operation contract in
the request context. That contract contains the stable authorization operation
and the closed artifact allowlist; it is constructed only from the compiled
OpenAPI or MCP registry, never request data. `AdmitOperation` must consume this
shared contract instead of depending only on `api.OperationFromContext`, and a
network operation that reaches a service method without a contract fails
closed. In-process callers retain their separately typed local path. The MCP
row admits only the existing machine-credential artifact class.

The adapter then passes the raw artifact as `service.Bearer(raw)` to the mapped
service method. The service's in-transaction resolution and artifact-admission
chokepoint enforce the contract before domain authorization, as an OpenAPI row
does today. The transport does not resolve a principal, cache grants, inspect
token prefixes as authority, or make a second authorization decision.
Revocation and grant changes therefore take effect on the next page or call.

`internal/mcpserver` is a transport adapter. It may depend on narrow service
interfaces and MCP wire types. It must not import `internal/store`, generated
SQL packages, or `internal/crypto`; call the REST API over loopback; forward the
bearer to any other service; or expose a generic operation-name/API proxy.

## Closed phase-1 tool registry

Tool names and schemas are stable MCP surface once released. Each tool calls
one existing service operation. A bounded service or store pagination method
may be added under the same named authorization operation, but the MCP handler
must not materialize an existing whole-list result and slice it afterward.

`O`, `P`, and `E` below mean the explicitly supplied organization, project, and
environment immutable ids. There is no ambient context, name resolution, or
default scope. Successful `audited: none` is the existing audit registry's
reviewed disposition for a proof-scoped pure read. An authenticated
authorization refusal emits the existing `grant.denied` event with the exact
operation and formula, now carrying `origin=mcp`. Invalid or missing bearer
presentations, including expired or revoked service-account credentials, retain
the existing silent, non-enumerating authentication-failure disposition. A
valid bearer of a disallowed class emits existing
`auth.artifact_class_refused` with `origin=mcp`. Thus authentication failure is
silent, artifact-class refusal is `auth.artifact_class_refused`, and capability
denial is `grant.denied`.

| Advertised tool | Existing service call and operation | Complete formula | Successful audit behavior | Returned data |
| --- | --- | --- | --- | --- |
| `hikyo_list_definitions` | `service.Keys.List`, `key.list` | `read@project` at `P` | `audited: none`; denial is `grant.denied` | Schema revision plus bounded key declarations, descriptions, classifications, rules, presence policy, and group ids. No values. |
| `hikyo_list_environments` | `service.Environments.List`, `environment.list` | `read@project` at `P` | `audited: none`; denial is `grant.denied` | Bounded environment identity, name, note, order, and creation metadata. |
| `hikyo_inspect_configuration` | `service.Values.List(..., reveal=false)`, `value.list` | `read@env` at `E` | `audited: none`; denial is `grant.denied` | Bounded cells. `config` plaintext may appear because classification is Hikyo's locked sensitivity boundary. A `secret` carries only classification and set/absent presence, never plaintext. |
| `hikyo_list_pending_changes` | `service.Revisions.PendingDrafts`, `value.pending-list` | `read@env` at `E` | `audited: none`; denial is `grant.denied` | Bounded caller-owned drafts. `config` draft plaintext may appear; secret and unset drafts never carry material. |
| `hikyo_list_revisions` | `service.Revisions.History`, `revision.list` | `read@env` at `E` | `audited: none`; denial is `grant.denied` | Bounded revision metadata, changed key names, publisher, time, schema revision, and payload-presence state. No historical values or change token. |

Every input schema requires the full immutable id chain appropriate to its
operation and accepts optional `page_size` and `cursor`. `page_size` is 25 by
default, minimum 1, maximum 100. `cursor` is at most 2 KiB. Unknown fields are rejected.
The response repeats the addressed ids and exposes `next_cursor` only when
another page exists. Empty `cursor` means the first page; an empty identifier
never means all scopes.

The MCP registry is closed in code and CI. It pins tool name, input and output
schema, service operation, authorization operation, formula, audit disposition,
read-only annotation, result bound, and secret policy in one row. CI fails if a
handler is unregistered, a registered row is unadvertised, a tool maps to zero
or multiple authorization operations, or the pinned formula/audit disposition
drifts from the authorization registry.

## Pagination and output bounds

All five tools page before domain objects are materialized. Store queries use a
stable keyset order plus `limit + 1`; they never fetch the whole collection to
apply a transport limit. Each page resolves the bearer and authorizes again in
a fresh transaction. A client can continue only through `next_cursor`; an
offset, caller-selected sort, include-all flag, batch array, or empty-list-means-
all convention is not accepted.

Cursors are opaque, authenticated, and portable across replicas. They bind the
tool, exact scope ids, filters, stable sort position, schema version, page
count, cumulative item and encoded-byte counts, and one 15-minute chain expiry
that continuation never renews. They contain no bearer, principal id, tenant names, secret
material, or authorization claim. Changing any bound input or failing cursor
authentication returns one safe invalid-cursor error. A cursor never grants
access and never suppresses current authorization.

Hard limits, applied after JSON encoding and before a byte crosses the wire:

| Boundary | Fixed phase-1 limit |
| --- | --- |
| MCP request body | 256 KiB |
| Items per page | 25 default, 100 maximum |
| One tool's `structuredContent` | 256 KiB |
| Compatibility text summary | 4 KiB; summary only, never a duplicate row set |
| Static discovery and tool catalog response | 64 KiB |
| Cursor | 2 KiB input, 15-minute validity |
| Tool execution | 30-second wall-clock deadline |
| One cursor chain | 10 pages, 1 000 items, and 1 MiB encoded structured content |

If the next single valid item cannot fit within 256 KiB, the operation refuses
with a named `result_item_too_large` error. Otherwise it returns only the items
that fit and a cursor for the first unreturned item. There is no silent field
truncation. Reaching a chain bound returns a named `traversal_limit_reached`
error and no continuation cursor. A fresh traversal starts at the first item;
there is no caller-supplied offset that skips the bound. Pagination calls count
as ordinary calls for every rate and concurrency budget, so small pages cannot
multiply permitted bulk-read work.

MCP has a datastore-coordinated token bucket per service-account principal. It
refills at 60 calls per minute and has capacity 20, so no burst exceeds 20
calls. Concurrency caps are 4 per principal, 8 per organization, and 64 per
instance. The 64-instance cap is inside the
server's existing global 512-request cap. Every replica shares the same budget
state; an unavailable coordinator refuses rather than falling back to a local
limiter. Overflow is uniform `429` with `Retry-After`. Protocol metadata calls
remain under the existing pre-auth per-IP admission and the 64 KiB response cap.

## Secret and model-context boundary

Phase 1 has no operation that asks for `reveal` or `reveal-history`. The MCP
adapter always calls `Values.List` with `reveal=false`. Secret ciphertext,
current secret plaintext, historical secret plaintext, equality signals,
change tokens, token material, provider credentials, MFA material, and root or
keyring material are absent from tool inputs, results, errors, logs, traces,
metrics, resources, prompts, and server instructions.

`config` plaintext, names, descriptions, notes, rules, authors, and history are
not secrets under Hikyo's locked model, but sending them to an MCP client moves
them into the client's model-context trust boundary. Enabling MCP is therefore
an operator decision with this consequence stated in configuration and user
documentation. Tenant-controlled descriptions and notes are untrusted data:
they stay typed fields in `structuredContent`, are never concatenated into
server instructions, and confer no authority. A canary secret fixture must be
absent from every phase-1 transport, error, log, trace, and metric test.

Future secret reveal requires a separate ADR and explicit feature gate. It must
retain `read@env` and `reveal@env` at `E`, the project machine-reveal opt-in, exact key
selection, and one existing disclosure event per key while stating that
plaintext enters the AI provider boundary. No such path is reserved in phase 1.

## Explicit phase boundary

| Feature | Phase-1 disposition |
| --- | --- |
| Static service-account bearer and five bounded read tools | **In** |
| `server/discover`, `tools/list`, `tools/call` | **In** |
| Secret plaintext reveal or historical reveal | **Out; separate ADR required** |
| Secret entry, even through elicitation or URL mode | **Out; separate ADR required** |
| Any mutation, validation-as-dry-run, stage, publish, approval, or adapter action | **Out; separate ADR required** |
| Resources, resource templates, prompts, subscriptions, `listChanged`, tasks, sampling, logging, roots | **Out and unadvertised** |
| OAuth, browser cookies, human delegation, dynamic client registration | **Out; necessity and maintained-package decision belongs to #631** |
| Local stdio server, generic REST/OpenAPI proxy, deprecated HTTP+SSE transport | **Rejected** |

Adding an out item is an amendment, not an implementation convenience. Tool
annotations and client approval UI are defense in depth, never authorization.

## Threat-model controls

**Host and Origin.** Host validation compares the effective request authority
to `HIKYO_EXTERNAL_ORIGIN`; it is never learned from the request. Proxy mode
uses only the existing trusted-proxy CIDRs and rejects ambiguous or multiple
forwarded authorities. A present `Origin` must exactly match a normalized,
explicit allowlist entry. `null`, wildcard, suffix, and reflected origins are
refused. A missing Origin is accepted for non-browser MCP clients. Browser CORS
is out in phase 1, so no cross-origin CORS grant is emitted.

**Bearer handling.** Exactly one bearer header is accepted. The value is
length-bounded, wrapped in the existing redacting artifact type, never copied
into structured telemetry, never returned, and never forwarded. Transport
metadata and tool annotations cannot select a principal or capability. Missing,
invalid, expired, revoked, wrong-class, and unauthorized artifacts disclose no
tenant fact. Cookies never authenticate MCP. Token passthrough to a downstream
service, another Hikyo endpoint, a callback, a tool result, or an error is
forbidden.

**Model-context disclosure and confused deputy.** Every tool takes explicit
scope ids, maps to one registered operation, and repeats current authorization.
There is no ambient context, caller-chosen operation, token exchange, outbound
network request, resource URI dereference, or result-field instruction. Client
name, version, `_meta`, annotations, cursor, and model prose are untrusted and
never authority. Authorization errors preserve unauthorized-equivalent-to-
nonexistent behavior.

**Cancellation and availability.** Request context reaches each service and
store call. Cancellation rolls back open work and writes no partial response;
handlers do not continue after disconnect. Body, header, catalog, item, page,
result, deadline, rate, and concurrency limits all compose with the existing
server caps. Limits are enforced before expensive parsing where possible and
after authorization before tenant-shaped work, so probes cannot occupy tenant
budgets by guessed ids.

**Multiple replicas.** Every request and every page is independent. Bearer
state, grants, cursor authentication keys, rate state, and audit state come from
shared authoritative configuration or the datastore. No in-memory session,
authorization cache, pagination snapshot, queue, or sticky route is required.
Successive requests must pass while deliberately alternating replicas.

## Required implementation evidence

Phase 1 is not complete until CI proves:

1. pinned `2026-07-28` conformance and explicit legacy-version refusal against
   stateless JSON transport;
2. closed registry parity with service operation, formula, audit disposition,
   shared artifact-admission contract, schemas, bounds, and no store or crypto
   imports;
3. dual-engine authorization, pagination, cursor tamper/expiry, bound, denial,
   cancellation, rate, and seeded-secret canary scenarios;
4. alternating-replica calls without `Mcp-Session-Id` or stickiness; and
5. named-client discovery and authorized call smoke tests using token references
   rather than literal tokens in configuration.

Conformance proves protocol shape only. It does not replace Hikyo's registry,
authorization, isolation, audit, model-context, or secret-canary evidence.
