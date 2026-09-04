# Hikyo remote MCP server: implementation research

**Date:** 2026-09-04  
**Status:** research and implementation recommendation, not an ADR  
**Scope:** a remote, authenticated MCP server served by `hikyo server`; optional
local stdio is a separate developer convenience.  
**Method:** primary sources only for MCP, OAuth, and SDK claims. Repository
conclusions come from Hikyo's locked ADRs and current code. MCP citations target
the dated `2026-07-28` specification so a later live-doc edit cannot silently
change this design.

**Owner direction (2026-09-04):** ship MCP in the current codebase with the
existing service-account bearer first. Reassess embedded OAuth after the current
social-auth and CLI-login work lands. Do not require an external authorization
server. Do not presume Fosite or any other protocol package. The later OAuth
ADR must first confirm that OAuth human delegation is necessary. If it is,
evaluate maintained Go implementations using current evidence. Hikyo must not
hand-write OAuth protocol machinery.

## 1. Executive answer

Hikyo can add a production-quality MCP server without adding a service or a
second runtime. The recommended shape is a thin Go adapter at `POST /mcp` in the
existing `hikyo server` process. It uses the official Go SDK v1.7.0 in stateless
mode, passes the raw presented artifact to the existing service layer, and lets
the operation resolve identity and authorization inside its transaction. It
must not call Hikyo's REST API over localhost or access stores and cryptography
directly.

The recommended delivery is phased:

1. Ship a controlled, read-only MCP using existing Hikyo service-account
   bearer credentials. This is suitable for operator-configured Codex, Claude,
   and API clients. It is a production profile, but not automatic MCP OAuth.
2. Add OAuth human delegation only when one-click client sign-in or broad
   ChatGPT/plugin distribution is a product requirement. Under Hikyo's
   self-contained operating model, implement the authorization-server role in
   `hikyo server`; do not make another auth service a production dependency.
   This still needs a separate ADR, a current package evaluation if an embedded
   implementation is required, and independently reviewed security work.

The work is not mainly JSON-RPC plumbing. The two hard design problems are:

1. **A model-safe product surface.** Existing list operations can be too large
   for model context, and secret plaintext returned by a tool may leave the
   Hikyo deployment for the AI provider. Tool boundaries, pagination, and
   disclosure policy are therefore product and security work, not wire work.
2. **OAuth human delegation, if required.** Hikyo currently consumes OIDC for
   login and workload federation, but is not an OAuth authorization server.
   Existing service-account bearers work for managed clients; automatic sign-in
   does not.

### Recommended decisions

| Question | Recommendation | Reason |
|---|---|---|
| Production SDK | Pin official Go SDK `v1.7.0` initially | It supports MCP `2026-07-28`, preserves legacy behavior, and fits Hikyo's single Go binary. |
| Transport | Streamable HTTP on `POST /mcp`; `Stateless: true`; JSON responses first | The current protocol has no sessions or GET stream, and this preserves Hikyo's stateless HA model. |
| Initial authentication | Existing Hikyo service-account bearer | It already has narrow grants, immediate database-backed revocation, audit attribution, and support in managed AI clients. |
| OAuth follow-up | Reconfirm the product need, then decide the implementation boundary in a separate ADR | If OAuth remains necessary, keep the AS embedded in `hikyo server`, compare maintained Go packages using evidence current at implementation time, and never hand-write the protocol. No external auth service is required. |
| Compatibility | Normative `2026-07-28`; intentionally support and test `2025-11-25` during migration | Current clients will not all move at once. The Go SDK supports both eras, but Hikyo must prove the exact configuration. |
| Initial surface | Read-only metadata, definitions, config, secret presence, pending state, and revision history; no secret plaintext | This proves useful AI workflows without weakening Hikyo's disclosure and protected-publish invariants. |

The [MCP 2026-07-28 release](https://blog.modelcontextprotocol.io/posts/2026-07-28/)
is the current general-availability specification. The [official Go SDK v1.7.0
release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0)
adds that revision, and its
[`StreamableHTTPOptions`](https://github.com/modelcontextprotocol/go-sdk/blob/v1.7.0/mcp/streamable.go#L118-L206)
documents the required stateless configuration and request limits.

## 2. Fit with Hikyo's current architecture

| Existing Hikyo property | Reuse | New work or constraint |
|---|---|---|
| Locked Go modular monolith and multicall binary | Mount MCP beside the API/UI in `hikyo server` | No sidecar, Node service, or MCP microservice. |
| Service methods accept `service.Actor` | Every MCP handler calls the same service operation as REST/CLI | Pass `service.Bearer(raw)` initially; add a validated OAuth access-token actor path only with OAuth. Do not duplicate authorization in MCP handlers. |
| Authorization is evaluated in the operation transaction | Existing grants, capabilities, current bindings, revocation, protected-environment rules, and audits remain authoritative | OAuth scope is additional attenuation only. It never grants a Hikyo capability. |
| OpenAPI 3.1 contract with operation/formula metadata | Reuse operation names and service semantics where they represent the same domain action | MCP is not an OpenAPI route. Add an MCP operation registry and parity test instead of bypassing the closed authorization registry. |
| Machine token and OIDC federation support | Reuse issuer/JWKS hardening and the `FederatedActor` pattern | Existing Hikyo opaque credentials and social-login ID tokens are not automatically valid OAuth access tokens for `/mcp`. |
| Stateless HA and shared database state | Current MCP requests can land on any replica | Do not enable MCP protocol sessions, in-memory user state, or load-balancer stickiness. |

This follows the locked
[system architecture](../adr/system-architecture.md),
[tenant-isolation boundary](../adr/tenant-isolation.md),
[permission model](../adr/permission-model.md), and
[machine identity model](../adr/machine-identities.md). The service-layer
precedent is visible in [`service.Actor`](../../internal/service/service.go) and
the already validated-claims pattern in the same package.

### Target request path

```text
MCP client
  -> TLS ingress
  -> Host, Origin, body-size, rate, and protocol-header checks
  -> raw Hikyo bearer extraction, or OAuth validation in the OAuth profile
  -> MCP transport and schema validation
  -> MCP method-to-Hikyo operation registry
  -> existing service method with raw service.Actor or validated federated claims
  -> authorize inside transaction
  -> store, audit, and response redaction
```

In the service-account profile, transport carries only the raw opaque bearer;
the service resolves it in the operation transaction. In the OAuth profile,
the authentication layer may verify signatures or introspect opaque tokens
outside the database transaction. It carries validated claims, not an already
trusted Hikyo principal, across the boundary. Inside the transaction, Hikyo
resolves `(issuer, subject, token class)` to a current human or service-account
principal and checks the current binding and grants. This mirrors
`FederatedActor` and avoids holding a database transaction open during network
discovery, JWKS refresh, or introspection.

## 3. Protocol contract Hikyo must implement

### 3.1 Transport and lifecycle

The current [Streamable HTTP
transport](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)
is JSON-RPC over one HTTP endpoint. For `2026-07-28`, Hikyo must implement this
wire behavior:

| Requirement | Hikyo consequence |
|---|---|
| One JSON-RPC request or notification per HTTP POST | Route only `POST /mcp`; return `405` with `Allow: POST` for GET/DELETE. |
| Client `Accept` contains both `application/json` and `text/event-stream` | Reject nonconforming requests. Hikyo may return JSON for ordinary responses. |
| Every request carries protocol and client metadata in `_meta` | Let the SDK validate required `io.modelcontextprotocol/protocolVersion` and client capabilities. Treat client info as display metadata, never authority. |
| Required `MCP-Protocol-Version`, `Mcp-Method`, and method-specific `Mcp-Name` headers mirror the body | Reject missing or mismatched fields with HTTP 400 and `HeaderMismatch` code `-32020`. Do not let a proxy authorize on unchecked mirror headers. |
| Unsupported protocol version | Return HTTP 400 and `UnsupportedProtocolVersionError` code `-32022` with supported versions. |
| Accepted notification | Return HTTP 202 with no body. |
| Unknown method | Return HTTP 404 plus JSON-RPC `-32601`, which distinguishes a modern server from a missing endpoint. |
| No protocol session, GET stream, DELETE session, or `Mcp-Session-Id` | Set `Stateless: true`; do not generate, echo, or require session IDs. |
| Request-scoped SSE is allowed | If later needed, final response ends the stream; disconnect cancels work; there is no `Last-Event-ID` resumption. |

Every modern request is independently routable. Long-lived list/resource change
notifications require the separate `subscriptions/listen` request. Phase 1
should not advertise `listChanged` or subscriptions, so the reverse proxy needs
no long-lived MCP stream tuning yet. If enabled later, disable proxy buffering,
send keepalive comments, bound concurrency, and make cancellation propagate.

MCP `2026-07-28` removed the initialization/session lifecycle used through
`2025-11-25`. A modern client may probe first, then fall back to `initialize`
only when the response is not a recognized modern protocol error. Hikyo should
support exactly these two tested versions initially:

| Version | Promise |
|---|---|
| `2026-07-28` | Normative implementation, stateless, per-request metadata, `server/discover`, MRTR, and current cache hints. |
| `2025-11-25` | Compatibility profile using the official Go SDK's legacy handling, without depending on server-side session state. |

Do not add the deprecated 2024 HTTP+SSE transport. The current transport spec
explicitly says new implementations should not adopt it. Pin the supported
versions in documentation and tests; do not promise arbitrary older clients.

### 3.2 Discovery and capabilities

Implement
[`server/discover`](https://modelcontextprotocol.io/specification/2026-07-28/server/discover)
because the current specification requires it. Its server information and
capabilities must truthfully describe only implemented features. In phase 1:

| Capability | Advertise? | Rule |
|---|---|---|
| Tools | Yes | Stable catalog; no `listChanged`. |
| Resources | No initially | Tools have the broadest host support and make every context fetch explicit. Add non-secret resources only after a concrete client workflow needs them. |
| Prompts | No initially | Add only after useful, reviewed templates exist. |
| Tasks extension | No | It is optional and adds durable execution state. |
| Deprecated roots, sampling, logging features | No | New Hikyo work should not build on deprecated features. |

List and read results in the current protocol carry caching hints such as
`ttlMs` and `cacheScope`. Stable schemas may be public-cacheable only if truly
identical for every caller. Authorization-filtered resource or tool lists are
private. Never let a shared cache expose one principal's catalog or content to
another.

### 3.3 Tools

The [tools
specification](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)
makes tools model-controlled operations. Hikyo must publish deterministic,
well-described JSON Schema 2020-12 input schemas, validate inputs, optionally
publish output schemas, validate outputs, and return typed `structuredContent`.
Text content may accompany it for compatibility. Protocol/validation failures
are JSON-RPC errors; domain failures are successful `tools/call` responses with
`isError: true` and a safe explanation.

Tool annotations such as read-only, destructive, idempotent, and open-world are
hints to clients. They do not enforce Hikyo authorization or confirmation. A
tool description must state its scope, side effects, draft/publish behavior,
required capability, and whether user interaction may be required.

Do not convert all current OpenAPI operations one for one, and do not expose a
generic `call_api(operationId, args)` escape hatch. Those designs create a
huge, unstable model surface and weaken the proof that each MCP action maps to
one existing domain operation and authorization formula.

An initial catalog should contain at most these five task-level operations:

| Candidate tool | Effect and guardrail |
|---|---|
| `hikyo_discover_scope` | Lists organizations, projects, environments, and the caller's usable non-secret actions; all results are authorization filtered. |
| `hikyo_inspect_configuration` | Reads definitions, non-secret config values, provenance, and secret presence only. Never returns secret material. |
| `hikyo_list_pending_changes` | Reads the caller-visible draft state without returning another principal's pending material. |
| `hikyo_list_revisions` | Reads bounded revision metadata; historical secret values remain absent. |
| `hikyo_validate_change` | Optional final v1 tool. Validates a proposed non-secret change and returns findings without mutation. It needs an explicit service operation rather than pretending a write is a dry run. |

This list is a candidate product surface, not a protocol requirement. A surface
ADR should lock names, schemas, retry/idempotency semantics, and exact mappings
before implementation. Existing `List` methods return whole collections in
several places. MCP needs service-layer cursor pagination or bounded `GetMany`
operations, with a hard result limit, so a transport wrapper cannot allocate or
inject an entire large project into model context.

### 3.4 Resources

The [resources
specification](https://modelcontextprotocol.io/specification/2026-07-28/server/resources)
defines application-controlled context, with absolute URIs, list/read methods,
templates, optional subscriptions, pagination, and cache hints. If a later
client workflow justifies resources, useful templates are:

| Example URI | Contents |
|---|---|
| `hikyo://organizations` | Organizations visible to the caller. |
| `hikyo://org/{org}/project/{project}/environments` | Visible environment topology. |
| `hikyo://org/{org}/project/{project}/definitions` | Key declarations and classifications. |
| `hikyo://org/{org}/project/{project}/matrix` | Config values, provenance, and secret presence, authorization filtered. |
| `hikyo://org/{org}/project/{project}/draft` | Current draft/plan state visible to the caller. |

Resource identity does not confer access. Every list and read repeats current
Hikyo authorization. Secret resources must not exist. Returned text and
metadata must be treated as untrusted model input and encoded so stored content
cannot alter the protocol envelope.

### 3.5 Prompts and multi-round interaction

The [prompts
specification](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/prompts.mdx)
defines user-controlled templates. Prompts are not necessary for a useful MCP
server and should be phase 2. Reasonable future prompts are `diagnose_drift`,
`prepare_change_plan`, and `review_deployment_config`; their arguments and
rendered messages must exclude secret plaintext.

The current protocol carries server-requested input through Multi Round-Trip
Requests (MRTR): a result says input is required, the client gathers the input,
then retries the original operation with matching input responses. Do not use
the older server-initiated request model on SSE.

## 4. Authorization design

### 4.1 Exact option verdict

| Option | Verdict | What it entails |
|---|---|---|
| Existing Hikyo service-account bearer | **Recommended for the first controlled production release** | Managed clients already support a configured bearer. Hikyo keeps its existing machine allowlists, immediate revocation, audit identity, and in-transaction checks. It does not provide automatic sign-in. |
| External OAuth authorization server | **Viable OAuth option when operators already run one** | Hikyo validates resource-bound access tokens and maps subjects to principals. The external server owns authorization, consent, client registration, code/token endpoints, signing keys, refresh/revocation, and policy. This adds a hard deployment dependency. |
| Embedded authorization server in Hikyo | **Best self-contained OAuth UX, but defer to a security project** | Use a maintained OAuth server library, never hand-roll it. It still requires metadata, PKCE S256, exact redirects, consent, codes, opaque access/refresh tokens, registration, rotation, revocation, storage, audit, and UI. |
| Reuse Hikyo browser session or an OIDC ID token as the MCP bearer | **Reject** | Neither is an audience-bound OAuth access token for `/mcp`; cookies also create a second CSRF/session transport. |

The service-account profile is deliberately managed rather than discoverable:
the operator mints a workload token, grants only the required environments, and
configures the token as an HTTP Authorization bearer in the MCP host. The MCP
authorization specification is optional, and current Codex, Claude, and OpenAI
API clients can send an explicitly configured bearer. The profile must be
feature-gated, use a stable public tool catalog with authorization at call time,
and preserve bounded pagination and existing bulk-read limits. Invalid or
missing credentials must yield one safe, non-enumerating tool failure. Hikyo
must not claim OAuth interoperability until protected-resource metadata and a
compatible authorization flow exist.

For this controlled profile, `server/discover` and the deterministic
`tools/list` catalog may be public because they contain no tenant facts.
`tools/call` passes the raw bearer to the mapped service operation, which
resolves it and authorizes in-transaction. If the product instead requires
HTTP-level 401 for an invalid Hikyo opaque token before JSON-RPC dispatch, that
requires a declared architecture amendment: a short transport-validation
transaction may answer only valid/invalid and must discard the principal, while
the domain operation still resolves and authorizes the raw artifact again. Do
not cache or place a resolved principal in request context.

An existing OIDC provider configured for human sign-in is not automatically an
MCP authorization server. Hikyo is currently that provider's relying party. A
remote MCP integration needs an authorization server that will issue access
tokens specifically for Hikyo's MCP resource, expose the required OAuth
metadata, and support an acceptable client-registration path.

### 4.2 Hikyo is the protected resource

The [MCP authorization
specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
defines OAuth for HTTP transports but makes authorization optional at the
protocol level. Hikyo must always authorize every domain call. The controlled
profile does that with an operator-configured service-account bearer and does
not advertise MCP OAuth. An OAuth-enabled profile adds the discovery and HTTP
challenge behavior below. Neither profile accepts Hikyo browser cookies.

For the OAuth-enabled profile, publish [RFC 9728 protected resource
metadata](https://www.rfc-editor.org/rfc/rfc9728.html) for the path-bearing MCP
resource. For a canonical endpoint `https://hikyo.example/mcp`, the discovery
URL is:

```text
https://hikyo.example/.well-known/oauth-protected-resource/mcp
```

A minimal response is:

```json
{
  "resource": "https://hikyo.example/mcp",
  "authorization_servers": ["https://auth.example"],
  "scopes_supported": ["hikyo:mcp"],
  "bearer_methods_supported": ["header"],
  "resource_name": "Hikyo MCP"
}
```

The `resource` value must exactly match Hikyo's configured canonical MCP URI.
The MCP profile requires at least one authorization server. An unauthenticated
or invalid request returns HTTP 401 and points to the metadata:

```http
WWW-Authenticate: Bearer resource_metadata="https://hikyo.example/.well-known/oauth-protected-resource/mcp", scope="hikyo:mcp"
```

Insufficient OAuth scope returns HTTP 403 with `error="insufficient_scope"`,
the required `scope`, and the resource metadata URL. Bearer tokens travel only
in `Authorization: Bearer ...`, never query parameters or cookies. MCP clients
include the canonical resource as the [RFC 8707 resource
indicator](https://www.rfc-editor.org/rfc/rfc8707.html) in authorization and
token requests. Hikyo rejects a token whose audience is not this MCP resource
and never passes the token onward to another service.

### 4.3 Authorization-server discovery and client registration

The client discovers the authorization server through protected-resource
metadata, then reads [RFC 8414 authorization-server
metadata](https://www.rfc-editor.org/rfc/rfc8414.html) or OpenID Connect
Discovery. The authorization server should include the [RFC 9207 issuer
identifier](https://www.rfc-editor.org/rfc/rfc9207.html) in authorization
responses so the client can detect mix-up. Authorization-code clients require
PKCE S256 and exact redirect-URI validation.

Current MCP [client
registration](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration)
uses this preference order:

| Priority | Mechanism | Hikyo consequence |
|---|---|---|
| 1 | Client ID Metadata Documents (CIMD) | Preferred for clients with no pre-existing relationship. Hikyo's embedded authorization-server component, not `/mcp`, must support and secure it. |
| 2 | Pre-registration | Fully valid and simplest for a controlled deployment, but each supported AI client needs an operator-created client record. |
| 3 | RFC 7591 Dynamic Client Registration | Deprecated in current MCP, retained only for backward compatibility. Do not make it the only long-term path. |

The current MCP specification normatively pins
[`draft-ietf-oauth-client-id-metadata-document-00`](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-00),
not an unversioned future draft. Pin that interpretation in tests and re-review
when MCP changes its normative reference. If Hikyo initially supports only
pre-registration, document the supported clients and the operator setup rather
than claiming open-ended MCP-host compatibility.

### 4.4 Token validation and Hikyo principal mapping

Hikyo must deliberately choose the accepted access-token profile. MCP does not
require JWT access tokens.

There is an architectural tension to resolve explicitly: OAuth transport
failures require an HTTP 401/403 before JSON-RPC dispatch, while Hikyo's locked
domain-authorization rule evaluates authority in the database transaction.
The safe split is authentication and coarse OAuth-scope attenuation at the HTTP
boundary, then principal binding and all Hikyo capability authorization inside
the operation transaction. The outer layer must not infer domain permission
from a token scope.

| Token type | Validation outside transaction | State checked inside transaction |
|---|---|---|
| JWT access token | HTTPS issuer discovery, allowed algorithm, signature/JWKS, exact issuer, MCP audience, expiry/not-before, and scope | `(issuer, sub, token class)` binding, principal active state, current grants, and domain authorization |
| Opaque access token | Authenticated introspection with bounded timeout and safe cache semantics | Same binding, active state, grants, and authorization |

Never use an ID token as an API bearer token. Never trust `clientInfo`, an email,
or an unverified token claim as a principal identifier. Record the validated
OAuth `client_id` or equivalent token claim for audit, with client-supplied
metadata marked untrusted.

OAuth scopes attenuate the request before domain authorization. They never
replace Hikyo's capability formula and never widen a principal. A single
baseline `hikyo:mcp` scope is the least disruptive first design. If separate
`read`, `write`, or `publish` scopes are wanted, that is a real second
authorization dimension and requires an ADR amendment, explicit denial/error
semantics, and tests for the Cartesian product of OAuth scope and Hikyo grants.

### 4.5 Relationship to the currently scoped OAuth and auth tickets

Live issue state was checked on 2026-09-04. No open Hikyo issue currently
implements an OAuth authorization server. The social-sign-in umbrella
[#615](https://github.com/Hikyo-Org/Hikyo/issues/615) and its implementation
tickets [#605 through #613](https://github.com/Hikyo-Org/Hikyo/issues/605) are
open, but they use OAuth in a different protocol role from MCP delegation.

| Current work | Protocol role and reusable part | What it does not provide for MCP |
|---|---|---|
| [#605](https://github.com/Hikyo-Org/Hikyo/issues/605) and [#609](https://github.com/Hikyo-Org/Hikyo/issues/609), both open | Hikyo is an OAuth client to GitHub. Reuse the state, PKCE S256, exact callback, mix-up defence, sealed upstream client secret, and audit patterns. | It neither registers MCP clients nor issues access tokens. `oauth2_providers` means upstream identity providers and must not be reused for MCP client registrations. |
| [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) and umbrella [#615](https://github.com/Hikyo-Org/Hikyo/issues/615), both open | Hikyo is an OIDC relying party for human sign-in and registration. Reuse the resulting account, external-identity resolution, browser session, provider selection, and login UI. | An OIDC ID token received from Google or Entra is not an MCP access token, and the login callback does not authorize a third-party client. |
| [#611](https://github.com/Hikyo-Org/Hikyo/issues/611), open | Adds local-factor establishment and purpose-bound proof for social-only accounts. Reuse it when MCP consent or a sensitive operation requires stronger proof. | It creates no client delegation, OAuth scope, token endpoint, or MCP audience. It is supporting account security, not an AS. |
| [#612](https://github.com/Hikyo-Org/Hikyo/issues/612), open | Closest architectural precursor: first-party authorization code, state, PKCE S256, an approval page, separate CLI session artifact, and RFC 8628 device flow. Its transaction and approval machinery should be factored for reuse. | It is deliberately a Hikyo CLI login transport. It does not expose standard AS metadata, `/authorize`, `/token`, MCP resource indicators, general redirect-client registration, consent grants, OAuth access/refresh tokens, or revocation. |
| Closed foundations [#54](https://github.com/Hikyo-Org/Hikyo/issues/54), [#61](https://github.com/Hikyo-Org/Hikyo/issues/61), and [#62](https://github.com/Hikyo-Org/Hikyo/issues/62) | Human sessions, service-account opaque bearers, and workload OIDC validation already supply identity resolution, immediate revocation patterns, and the no-external-AS phase-1 credential. | Browser cookies, `hik_` service-account tokens, and workload ID tokens remain distinct artifact classes. None should be relabeled as an OAuth access token. |

The other open slices are supporting or orthogonal. [#606](https://github.com/Hikyo-Org/Hikyo/issues/606)
controls account-registration admission, not OAuth client consent;
[#608](https://github.com/Hikyo-Org/Hikyo/issues/608) supplies mail and local
sign-up; [#610](https://github.com/Hikyo-Org/Hikyo/issues/610) claims an
invitation through an external identity; and
[#613](https://github.com/Hikyo-Org/Hikyo/issues/613) closes the social-sign-in
test, UI, documentation, and ledger evidence. They can create or secure the
human account behind a delegation, but none issues or delegates MCP tokens.

Ticket [#617](https://github.com/Hikyo-Org/Hikyo/issues/617) is orthogonal. It
opens enums for the future upstream `oauth2` identity kind and removes JIT
before the `/api/v1` freeze; it adds no authorization-server surface.

The naming distinction is load-bearing:

```text
GitHub / Google / Entra
  -> #607 / #609: Hikyo is the client and authenticates the human
  -> existing Hikyo browser session
  -> embedded Hikyo AS: AI host is the client and requests delegation
  -> OAuth access token restricted to the canonical Hikyo /mcp resource
  -> /mcp repeats current Hikyo authorization for every operation
```

The embedded-AS design should therefore be a new MCP-auth ADR and ticket,
not an expansion of #609. It should coordinate with #612 before that ticket is
implemented so both flows share reviewed transaction, PKCE, approval, expiry,
single-use, and audit primitives without conflating their artifact types or
wire contracts. #612 can remain independently deliverable.

For a self-contained Hikyo, the ADR should lock these decisions:

1. The authorization-server role runs inside the existing `hikyo server`
   process and database. Existing local, passkey, OIDC, OAuth2, and SAML login
   methods authenticate the human into an ordinary Hikyo browser session.
2. `/authorize` uses that session for login and explicit client consent, then
   issues a short-lived, single-use authorization code bound to PKCE S256,
   exact redirect URI, client, MCP resource indicator, principal, and expiry.
3. `/token` issues new opaque, hashed, database-backed OAuth artifacts, with
   refresh rotation and revocation. They are neither browser/CLI sessions nor
   service-account tokens. This preserves Hikyo's immediate-revocation model
   and avoids adding stateless JWT authority.
4. Start with pre-registered clients plus the MCP-required metadata and exact
   compatibility tests. Add CIMD only with a reviewed trust and redirect-URI
   policy; keep deprecated dynamic registration compatibility optional.
5. Use OAuth scope only as attenuation, initially one `hikyo:mcp` scope.
   Every tool still resolves the current Hikyo principal and grant inside the
   service transaction. Select a maintained OAuth server library during the
   ADR; the existing no-hand-rolled-auth invariant applies.

This makes an external authorization server unnecessary. It does not make the
OAuth work small: Hikyo still owns client trust, consent, codes, access and
refresh token lifecycle, discovery, revocation, mix-up defence, and conformance.
The controlled service-account bearer phase remains useful and can ship before
this work without creating an external dependency.

### 4.6 Embedded OAuth implementation decision and Fosite evidence

#### Verdict: decision deferred; Fosite is not presumed

[Ory Fosite](https://github.com/ory/fosite) is a Go framework for building an
OAuth 2.0 authorization server. It is not an authentication product and it is
not a standalone server. In Hikyo it would run as a library inside the existing
`hikyo server` process. That shape fits the no-external-AS decision, but the
released project state documented below is too stale and unresolved to make
Fosite the presumed or recommended engine.

The future OAuth ADR must start one level earlier: confirm that MCP OAuth human
delegation is still necessary. If it is, compare maintained Go implementations
and their current security, standards, release, and interoperability evidence
at implementation time. This report deliberately does not recommend an
alternative package now. OAuth protocol machinery must not be hand-written to
avoid a package decision. If no maintained implementation clears Hikyo's bar,
defer the OAuth profile or reduce its scope rather than weakening the security
rule or adding an external authorization service.

The current-ticket conclusion does not depend on a future package choice:

- Fosite replaces no ticket in #605 through #613, #615, or #617.
- It is irrelevant to most social-registration product work.
- It complements #609, but serves the opposite OAuth role and must not be used
  in `internal/oauth2rp`.
- Any embedded package supplies only protocol machinery. Hikyo continues to
  own identity, login, consent, persistence, policy, audit, deployment, and the
  `/mcp` resource server.
- Fosite may enter the future comparison only on the strength of its then-current
  maintained release and closed security findings, not because it was assessed
  in this report.

The protocol-role split is:

```text
GitHub / Google / Entra
  -> #607 / #609: Hikyo is an OAuth/OIDC client
  -> Hikyo account and browser session
  -> Hikyo consent and product policy
  -> selected embedded implementation: Hikyo is the OAuth authorization server
  -> AI host is the OAuth client
  -> resource-bound access token
  -> /mcp: Hikyo is the protected resource
```

#### Exact current-ticket impact

Issue state and bodies were checked from the live tracker on 2026-09-04. The
current social-auth lane remains independently deliverable and must not acquire
an embedded authorization-server package dependency.

| Ticket | Embedded-AS package impact | Reason |
|---|---|---|
| [#605](https://github.com/Hikyo-Org/Hikyo/issues/605) | **No replacement; coordinate migrations only** | Its `oauth2_providers` and `oauth2_transactions` model upstream identity providers. Embedded-AS clients, grants, codes, and tokens need separate tables and names. |
| [#606](https://github.com/Hikyo-Org/Hikyo/issues/606) | **Irrelevant** | Human registration admission is not OAuth client registration or consent. Fosite supplies neither product policy nor UI. |
| [#607](https://github.com/Hikyo-Org/Hikyo/issues/607) | **Provides a later sign-in choice** | Federated sign-up creates the Hikyo account and browser session that can later reach OAuth consent. An AS package does not validate Google or Entra as Hikyo's upstream relying-party flow. |
| [#608](https://github.com/Hikyo-Org/Hikyo/issues/608) | **Irrelevant** | SMTP and local email registration are account-establishment concerns, not authorization-server protocol mechanics. |
| [#609](https://github.com/Hikyo-Org/Hikyo/issues/609) | **Complementary; never replaced** | #609 correctly uses `golang.org/x/oauth2` because Hikyo is GitHub's OAuth client. Fosite is for the inverse role, where Hikyo issues tokens to AI clients. |
| [#610](https://github.com/Hikyo-Org/Hikyo/issues/610) | **Irrelevant to its wire flow** | Invitation claim establishes an external identity on a Hikyo account. It may create the account later granting consent, but it is not OAuth client delegation. |
| [#611](https://github.com/Hikyo-Org/Hikyo/issues/611) | **Reused by Hikyo around the AS** | Local factors and purpose-bound proof may gate consent or sensitive client administration. A protocol package does not authenticate or reauthenticate the human. |
| [#612](https://github.com/Hikyo-Org/Hikyo/issues/612) | **Patterns reused; ticket not replaced** | Its login handoff mints `ArtifactCLISession`, not an OAuth access token. Browser login, approval, provider preselection, and CLI-session semantics remain Hikyo work. Stable Fosite v0.49.0 does not contain the RFC 8628 device-flow implementation now present only on unreleased `master`; that fact is evidence against presuming Fosite, not a reason to couple #612 to another package. |
| [#613](https://github.com/Hikyo-Org/Hikyo/issues/613) | **No replacement** | Its A7 tests, bounds, audit closure, docs, and ledger evidence remain required for social authentication. Later AS tests form a separate acceptance lane. |
| [#615](https://github.com/Hikyo-Org/Hikyo/issues/615) | **No replacement** | The umbrella delivers social sign-in and open registration. It does not turn Hikyo into an authorization server. |
| [#617](https://github.com/Hikyo-Org/Hikyo/issues/617) | **Irrelevant except naming discipline** | Pre-freeze JIT removal and open identity enums do not cover OAuth clients or tokens. The embedded AS must add new, distinct contract names rather than overload identity-provider enums. |

#612 and the embedded AS may share Hikyo-owned browser authentication,
approval-page components, purpose binding, expiry helpers, single-use storage
patterns, and audit vocabulary. They should not share artifact tables or wire
contracts. #612 remains a first-party session transport; the AS remains
standards-based delegation to a third-party client.

#### What the evaluated Fosite release could supply

The [v0.49.0 README](https://github.com/ory/fosite/blob/v0.49.0/README.md)
documents authorization code, PKCE, opaque access and refresh tokens,
authorization-code/client/redirect binding, refresh rotation, and token
revocation. Its composable handlers include authorization code, refresh,
revocation, introspection, PKCE, and PAR. Its
[`Transactional` storage interface](https://github.com/ory/fosite/blob/v0.49.0/storage/transactional.go)
allows a Hikyo adapter to make code invalidation plus access/refresh creation
atomic on both supported databases.

If Fosite remains in the future comparison, its spike must use an explicit
minimal composition, never
[`ComposeAllEnabled`](https://github.com/ory/fosite/blob/v0.49.0/compose/compose.go).
The all-enabled helper also installs implicit, resource-owner-password,
client-credentials, JWT assertion, and OpenID Connect handlers that the MCP
profile neither needs nor should expose. The intended set is:

| Evaluated Fosite component | Possible Hikyo use |
|---|---|
| `OAuth2AuthorizeExplicitFactory` | Authorization-code issue and redemption only. |
| `OAuth2PKCEFactory` | Set `EnforcePKCE=true`; leave the plain method disabled so every client uses S256. |
| `OAuth2RefreshTokenGrantFactory` | Rotation-backed continuation, with explicit Hikyo lifetimes and revocation policy. |
| `OAuth2TokenRevocationFactory` | RFC 7009-style access/refresh revocation. |
| `OAuth2TokenIntrospectionFactory` | Stateful opaque-token validation; do not use stateless JWT introspection because it bypasses built-in revocation. |

A Fosite spike must also configure `ExactScopeStrategy` and
`ExactAudienceMatchingStrategy`.
Fosite's defaults are broader wildcard-scope and path-prefix URI audience
matching. Hikyo's one `hikyo:mcp` scope and canonical MCP URI require exact
matches. Configure every lifespan explicitly; Fosite defaults authorization
codes to 15 minutes, access tokens to one hour, and refresh tokens to 30 days,
which are library defaults rather than Hikyo decisions.

#### What the evaluated Fosite release does not supply

Fosite deliberately validates and executes parts of OAuth; its own README says
the embedding application still owns application/client security, CSRF,
database security, TLS, user authentication, and session management. Hikyo must
therefore implement and test:

| Missing layer | Hikyo responsibility |
|---|---|
| Human and product UX | Browser-session authentication from #607/#609, consent UI, reauthentication, denial/cancellation behavior, and client-administration UI. |
| Persistence and audit | Dual-engine migrations; `ClientManager`, code, access, refresh, PKCE, and revocation stores; transactional concurrency; expiry cleanup; audit identity and lifecycle events. Never serialize Fosite's concrete Go request objects as the durable schema. |
| MCP discovery | RFC 9728 protected-resource metadata and RFC 8414 authorization-server metadata. Fosite v0.49.0 exposes no metadata endpoints. |
| MCP client registration | Pre-registration administration first; later CIMD validation if required. Fosite does not implement CIMD or RFC 7591 Dynamic Client Registration. |
| MCP authorization extensions | RFC 8707 `resource` processing, exact `/mcp` binding, RFC 9207 `iss` authorization-response parameter, bearer challenges, and current MCP interoperability/conformance. |
| Domain authorization | Map the OAuth subject to a current Hikyo principal, then repeat live Hikyo grant checks in every service-operation transaction. OAuth scope only attenuates and never grants a capability. |

If Fosite remains a candidate, its
[`GetAudiences`](https://github.com/ory/fosite/blob/v0.49.0/audience_strategy.go)
parses a non-RFC-8707 `audience` form parameter. MCP clients send the RFC 8707
`resource` parameter in both authorization and token requests. The Hikyo
adapter must therefore reject caller-supplied `audience`, validate exactly one
canonical `resource`, bind it to the authorization transaction, translate it
into Fosite's requested/granted audience field, and verify the same binding on
code redemption and refresh. Missing, changed, duplicate, off-origin, or
non-canonical values fail closed. `ExactAudienceMatchingStrategy` is mandatory;
the default path-prefix matcher is too broad for the canonical `/mcp` resource.

The evaluated Fosite release also does not add RFC 9207's `iss` value. A Fosite
authorize wrapper would have to add
the configured Hikyo issuer to successful and error authorization responses in
the standard-defined cases, then pin it in interoperability tests. Static
RFC 8414 and RFC 9728 handlers remain Hikyo-owned. Start with pre-registered
public clients; CIMD needs a separate URL-fetch and redirect trust model with
SSRF controls and must not be improvised inside `ClientManager`.

#### Storage and trust boundaries

The future implementation should preserve these boundaries:

1. `internal/oauth2rp` remains the upstream GitHub client for #609 and imports
   `golang.org/x/oauth2`, not Fosite.
2. A new authorization-server module owns the selected package composition,
   protocol request adapters, MCP `resource` extension, issuer metadata, and
   OAuth artifact stores. It does not call Hikyo's REST API.
3. The existing auth service owns browser authentication, account/session
   resolution, reauthentication, and consent decisions. Only after explicit
   consent does it grant the exact scope and audience on the protocol request.
4. `/mcp` validates the opaque access artifact and scope, carries only a
   validated token binding into the operation, then resolves current principal
   state and authorization inside the normal service transaction.
5. OAuth client records and delegated artifacts are distinct from
   `oauth2_providers`, `oauth2_transactions`, browser/CLI sessions, workload
   identities, and service-account credentials. Names, prefixes, audit types,
   retention, and revocation remain disjoint.

The evaluated Fosite opaque HMAC strategy stores token signatures rather than
bearer plaintext and supports rotated verification secrets. Any chosen package
and token strategy must use a dedicated sealed OAuth key family, not Hikyo's
browser-session or service-account key. Database revocation,
session-generation changes, principal disablement, client disable, and consent
withdrawal must all invalidate access at the next MCP request. The selected
storage adapter must satisfy its package's atomicity contract and pass
cross-node, replay, code-race, refresh-race, and rollback tests on SQLite and
PostgreSQL.

#### Version and security posture

Fosite is recorded as an assessed option, not a candidate selected for a later
spike. Its current released state does not clear Hikyo's security bar:

| Evidence checked 2026-09-04 | Consequence |
|---|---|
| [v0.49.0](https://github.com/ory/fosite/releases/tag/v0.49.0) is the latest stable release and was published 2024-12-12. Its public API is described as only "almost stable" and its [`go.mod`](https://github.com/ory/fosite/blob/v0.49.0/go.mod) requires Go 1.22. | Go 1.27 compatibility should be proven, APIs wrapped behind a narrow Hikyo-owned interface, and the exact version pinned. No floating `master`. |
| v0.49.0 pins `github.com/go-jose/go-jose/v3` v3.0.3. The fix for [CVE-2025-27144](https://github.com/advisories/GHSA-c6gw-w398-hv78) is v3.0.4, but Fosite [PR #858](https://github.com/ory/fosite/pull/858) is still open and the current default branch still pins v3.0.3. | The spike must force a safe module version, run `govulncheck`, and prove no affected call path. Hikyo already uses go-jose v4, so adding Fosite introduces a second major line. |
| Open [issue #882](https://github.com/ory/fosite/issues/882) reports unauthenticated memory exhaustion in the opaque HMAC token parser used by the evaluated opaque-token profile. | Do not ship the released implementation unchanged. Require a released upstream fix or pin an independently reviewed fixed commit/fork, plus adversarial bearer-size and separator-count tests at the HTTP boundary. |
| Open [PR #883](https://github.com/ory/fosite/pull/883) fixes deletion of PKCE session state before verifier validation. Mandatory `EnforcePKCE=true` prevents the documented verifier-less downgrade, but the current behavior can still destroy a valid exchange after a bad attempt. | Require the fix and lifecycle regression tests before release; do not treat configuration as a complete repair. |
| Device authorization exists under [unreleased `master`](https://github.com/ory/fosite/tree/master/handler/rfc8628), not the [v0.49.0 handler tree](https://github.com/ory/fosite/tree/v0.49.0/handler). Ory's [security policy](https://github.com/ory/fosite/security) gives Apache-2.0 users no formal SLA and patches only the latest release. | Do not make #612 depend on Fosite device code or pin unreleased `master`. Maintain an explicit patch/update strategy before adopting the library. |

These findings do not justify hand-writing OAuth. They mean the future ADR must
evaluate the maintained Go package landscape from fresh primary-source evidence
instead of carrying this report's package assumptions forward. Fosite can be
included only if its then-current maintained release merits inclusion. Do not
default to an unreleased commit or private fork merely because no maintained
release clears the bar. The no-external-AS decision stays intact.

#### Decision and adoption sequencing

The approved sequence is:

1. Implement current-codebase MCP with existing service-account bearers and no
   embedded OAuth package dependency.
2. Land #605 through #613, #615, and #617 under their existing scopes. Coordinate
   the future AS schema names with #605 and reuse the browser-auth result and UI
   patterns from #607, #609, #611, and #612.
3. In the OAuth ADR, reconfirm that human delegation is necessary. Keep the
   no-hand-written-OAuth and no-external-AS constraints explicit.
4. If OAuth proceeds, compare currently maintained Go implementations
   using fresh release cadence, security response, transitive vulnerability,
   standards coverage, storage/atomicity, API stability, and target-client
   interoperability evidence. Do not preselect Fosite or invent a replacement
   package in this research.
5. Spike only the shortlisted implementation against RFC 8707, RFC 9207,
   metadata, registration, exact redirects, mandatory PKCE, consent,
   dual-engine atomicity, refresh rotation, revocation, cross-node use, and
   target MCP clients. Adopt it only if its findings close, then implement the
   embedded AS as a new ticket lane without removing service-account access.

Bottom line: Fosite replaces none of Hikyo's current OAuth, social-auth,
registration, invitation, factor, or CLI-session tickets. Its assessment is
useful evidence about package-selection risks, not a recommendation. The later
ADR owns both the OAuth necessity decision and any current package choice.

## 5. Default secret boundary

The current [elicitation
specification](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation)
states that form mode must not request sensitive information such as passwords,
API keys, or access tokens. Sensitive interactions use URL mode so the data is
entered into the server's HTTPS application, outside the MCP client and model
context.

Hikyo should make the safe initial invariant:

> The initial MCP surface never accepts secret plaintext and never returns
> secret plaintext.

That means no initial reveal tool, secret value resource, secret argument,
secret-bearing prompt, form elicitation for secrets, or diagnostic error that
echoes one. Existing machine/API delivery remains the right automation path
when a workload must receive secret values.

A later explicit reveal profile is possible without inventing a new authority
model: require an instance-level MCP reveal feature gate, the existing
per-project machine-reveal opt-in, a workload credential holding `read` and
`reveal` at the exact environment, an explicit bounded list of key names, and
one existing disclosure audit event per key. The tool must never expose all
secrets by default or expose secrets through resources. This option means the
plaintext is intentionally entering the MCP host and usually the AI provider's
model context. That consequence must be shown to the operator when the feature
is enabled. Client annotations and approval prompts are useful defense in depth,
not server-side authorization.

To set or change a secret from an AI-assisted workflow:

1. The tool validates the non-secret target and current authorization, then
   returns URL-mode input required with an opaque, short-lived transaction
   handle.
2. The user opens Hikyo's HTTPS page and authenticates there. The URL contains
   no secret and is not pre-authenticated.
3. Hikyo binds the transaction to the same validated MCP principal, client,
   exact target, intended operation, expiry, and one-use nonce.
4. The user enters the secret directly into Hikyo; existing reauthentication,
   scanning, staging, preview, approval, and publish rules run unchanged.
5. The retried MCP request can learn only a safe status and resulting revision,
   never the entered value.

If a client cannot perform URL elicitation, the operation fails safely and
directs the user to WebUI or CLI. The transaction handle must be unpredictable
or authenticated, single use, TTL-bound, replay-resistant, and useless as
authorization by itself. Cancellation, changed arguments, a changed principal,
or changed authorization invalidates it.

Publishing and protected changes need the same care even when no secret value
is disclosed. MCP annotations and model-written prose are not human
confirmation. The v1 publish tool should remain disabled until an ADR defines
how existing impact preview, approval, protected-environment confirmation, and
reauth ceremonies appear through MRTR or URL mode.

## 6. Security and operational requirements

The transport's own [security
requirements](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)
require Origin validation, and the MCP [security best-practices
guide](https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices)
covers confused-deputy, token-passthrough, SSRF, session, and local-server
threats. Hikyo needs controls at five boundaries:

| Boundary | Required controls |
|---|---|
| HTTP ingress | TLS, canonical Host allowlist, explicit Origin policy, strict content type, bounded body, request timeout, concurrency/rate limits by principal/client/tool, and safe CORS only when browser MCP clients are required. |
| OAuth network fetches | HTTPS, issuer allowlist, bounded redirects, no private/loopback/link-local destinations except explicit development, DNS rebinding resistance, egress policy, response-size/time limits, and cache/key-rotation tests. |
| Token processing | Exact issuer/audience, algorithm allowlist, expiry/not-before, resource indicator, scope, no token passthrough, and no bearer token in logs. Follow OAuth security BCP [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html). |
| MCP/domain adapter | Schema validation both directions, fixed operation registry, existing in-transaction authorization, idempotency/replay policy, safe errors, no generic API escape hatch, and no trust in annotations or clientInfo. |
| Data and observability | No full arguments/results in logs, secret-aware structured fields, current audit actor/client/method/tool/target/outcome, trace correlation without trusting trace metadata, and retention under the existing audit model. |

If the audit envelope gains `origin = mcp`, its closed origin/check-constraint
catalog needs forward and rollback migrations plus SQLite and PostgreSQL tests.
Do not encode MCP origin as an arbitrary string that escapes the audit
catalog-completeness invariant.

Go SDK v1.7.0 applies localhost Host protection and a default 4 MiB body limit,
but its current `CrossOriginProtection` zero value applies no protection unless a
compatibility flag is set. Hikyo must wrap the handler with explicit Go HTTP
cross-origin protection and configure allowed origins. Do not rely on an SDK
default that changes in v1.8.

If browser MCP clients are supported, CORS must enumerate trusted origins and
allow `Authorization`, `Content-Type`, and required MCP headers. Do not combine
wildcard origins with credentials. Cookie authentication remains unavailable on
`/mcp`, which avoids a second CSRF/session authorization path.

## 7. SDK and packaging choice

| Choice | Current state | Fit for Hikyo |
|---|---|---|
| [Official Go SDK v1.7.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0), tag commit `bc72835` | Stable release with MCP `2026-07-28`, stateless Streamable HTTP, legacy support, typed server helpers, auth package, and Go HTTP integration | **Choose.** One binary, direct service calls, shared logging/metrics/shutdown, and no IPC. |
| [Official TypeScript SDK v2](https://ts.sdk.modelcontextprotocol.io/v2/) | Stable v2 with current protocol support, server/client packages, schema adapters, and HTTP middleware | Good for an independent interoperability client or prototype, not Hikyo's production server. It introduces Node, a second process, deployment coordination, and an internal API/auth boundary. |

Pin the Go module version and review changelogs before upgrades. The SDK solves
wire protocol mechanics, not Hikyo's authorization, domain mapping, catalog
design, secret policy, audit completeness, or deployment configuration.

The likely code ownership is one deep adapter module, for example
`internal/mcpserver`, with a small OAuth-resource validation module if existing
OIDC/federation code cannot own access tokens cleanly. `cmd/hikyo` wires it into
the server. Handler closures receive only service interfaces and validated actor
claims. MCP packages must not import stores, SQLC, or crypto implementations.

### 7.1 AI client fit

| Client shape | Controlled bearer release | OAuth follow-up | Additional requirement |
|---|---|---|---|
| [Codex CLI, desktop, and IDE](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) | Yes. Streamable HTTP supports `bearer_token_env_var`. | Yes, including CIMD and DCR compatibility. | Put the most important safety instructions in the first 512 characters of server instructions. Configure reveal, if ever enabled, for per-tool approval. |
| [Claude Code](https://code.claude.com/docs/en/mcp) | Yes, through a static Authorization header sourced from environment. | Yes, with browser login and CIMD/DCR or pre-registered clients. | Smoke-test the exact supported client version and callback behavior. |
| [OpenAI Responses API](https://developers.openai.com/api/docs/guides/tools-connectors-mcp) | Yes, with the request's `authorization` token. | The application can supply a resulting access token. | The MCP endpoint must be publicly reachable from OpenAI, or use the documented Secure MCP Tunnel for a private deployment. Use `allowed_tools` and approval policy. |
| ChatGPT web | Not from a local Codex configuration. | Expected for a user-facing connection. | Package and review a ChatGPT plugin that exposes the remote MCP tools; this is separate from implementing the server. |
| Generic local MCP host | Usually, if it supports Streamable HTTP and static headers. | Only if its OAuth client profile overlaps Hikyo's AS. | Prove with conformance plus named-client smoke tests rather than claiming every MCP host. |

An operator-managed Codex configuration can look like:

```toml
[mcp_servers.hikyo]
url = "https://hikyo.example/mcp"
bearer_token_env_var = "HIKYO_MCP_TOKEN"
default_tools_approval_mode = "approve"
```

This keeps the token out of the configuration file. The environment variable
still belongs in the host's secret store, not a shell profile committed to a
repository.

### 7.2 Exact repository change map

| Area | Likely files | Change |
|---|---|---|
| Dependency and adapter | `go.mod`, `go.sum`, new `internal/mcpserver/*.go` | Pin module `github.com/modelcontextprotocol/go-sdk` v1.7.0 and import its `mcp` package; register the closed tool catalog; use `StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: ..., PropagateRequestCancellation: true}`; wrap explicit cross-origin protection. |
| Composition and routing | `internal/app/app.go`, `internal/server/server.go`, `internal/server/spa.go` | Construct shared concrete services once, inject narrow interfaces into REST and MCP, mount exact `/mcp` before SPA fallback, and reserve `/mcp` plus OAuth well-known roots from the SPA. |
| Configuration | `internal/config/config.go` and tests, chart/Compose docs | Add an enable flag, origin and request bounds; add issuer/resource/OAuth settings only for the OAuth profile; fail closed at boot. |
| Domain query shape | `internal/service`, `internal/store`, both SQL dialects | Add bounded cursor reads or `GetMany` where current `List` methods would return an entire catalog. Keep MCP out of persistence packages. |
| Audit and verification | `internal/audit/audit.go`, both audit migrations if `origin=mcp` is chosen, `internal/server` tests, `internal/isolation`, CI | Add transport provenance, redaction/canary tests, dual-engine scenarios, HA routing, client cancellation, and pinned MCP conformance. |

`api/openapi.yaml` does not need to describe `/mcp`: it is a separately
versioned JSON-RPC protocol, not a REST endpoint. Any new REST administration
surface still obeys `api/freeze.go`; the MCP surface needs its own compatibility
and deprecation policy in the new ADR.

## 8. Deployment shape

The production endpoint belongs on the existing public listener and image:

| Area | Required change |
|---|---|
| Server/router | Feature-gated `/mcp` POST handler. Add path-aware RFC 9728 metadata only with OAuth. Preserve existing health/readiness and graceful shutdown. |
| Configuration | Explicit enable flag, canonical public MCP URI, allowed Origins, request/concurrency limits, and compatibility versions. Add issuer, audience/resource, JWKS or introspection policy, and baseline scope only with OAuth. |
| Validation | Fail startup on an invalid/non-HTTPS canonical URI or unsafe Origin configuration. With OAuth, also fail on a missing/unsafe issuer or inconsistent audience. Development exceptions must be explicit. |
| Helm/Compose/proxy | Route `/mcp` to the same service and add the well-known path when OAuth is enabled. No stickiness. Add SSE proxy settings only when a streamed capability is enabled. |
| Operations/docs | Client setup, supported protocol versions, token grant/revocation, metrics/runbook, optional AS registration, and the explicit initial no-secret boundary. |

With the embedded AS, authorization and token endpoints share Hikyo's existing
readiness and database availability. There is no separate AS readiness or
introspection dependency. An upstream OIDC or OAuth2 provider outage can still
prevent login through that provider, but local-account authentication and
already valid Hikyo-issued MCP access tokens remain under Hikyo's own control.
Document and test that separation.

Useful metrics include request count/duration by MCP method and stable tool name,
protocol version, HTTP/JSON-RPC outcome, OAuth failure class, domain denial,
rate-limit rejection, input-required count, and active streams. Never use raw
resource URIs, arguments, tokens, or unbounded client strings as metric labels.

## 9. Verification and conformance

The official [MCP conformance
repository](https://github.com/modelcontextprotocol/conformance) runs a server
suite against a URL and freezes requirements by protocol revision. CI should
pin an exact release and action/commit SHA, then run the current requirement
set. Add the legacy command only if Hikyo declares that compatibility profile:

```sh
npx @modelcontextprotocol/conformance server \
  --url http://127.0.0.1:8080/mcp \
  --requirements 2026-07-28

npx @modelcontextprotocol/conformance server \
  --url http://127.0.0.1:8080/mcp \
  --requirements 2025-11-25
```

Do not waive a required phase-1 conformance scenario. Conformance proves the
wire contract, not Hikyo security or semantics. Add these project-owned layers:

| Layer | Minimum evidence |
|---|---|
| Protocol unit/contract | Header/body mismatch, missing capability, version negotiation, 202 notification, 404 method, schema input/output, pagination/cache hints, deterministic discovery/catalog, safe error shapes. |
| Controlled authentication | Bearer appears only in the Authorization header; every domain call receives only the raw artifact and resolves it in-transaction; missing, invalid, revoked, and unauthorized callers receive safe non-enumerating failures. |
| OAuth integration, when enabled | Correct protected-resource path/body; 401/403 challenges; AS discovery; resource indicator; issuer/audience/expiry/scope; JWT and/or introspection profile; token passthrough refusal; binding revocation effective on the next request. |
| Domain and secret policy | Every MCP handler maps to a registered operation and existing service authorization; secret-like fixtures never appear in the initial arguments/results/resources/prompts/logs. Add URL elicitation expiry, replay, cancellation, changed-target, and changed-principal tests with sensitive mutations. |
| Cross-engine and HA | Relevant service scenarios on SQLite and PostgreSQL; successive requests across replicas without stickiness; cancellation and bounded concurrency; proxy/TLS route tests. |
| Independent interoperability | Official Go client, official TypeScript v2 client, named Codex/Claude/OpenAI clients, and manual MCP Inspector exercise discovery, authentication, catalogs, calls, and business errors. Add URL elicitation only when shipped. |

Add a CI boundary rule equivalent in spirit to OpenAPI parity: the MCP registry
is closed, every advertised tool/resource/prompt has one explicit service
operation and authorization formula, and no handler imports persistence or
crypto packages. Snapshot only stable schemas, not authorization-filtered data.

## 10. Practical ticket sequence and acceptance criteria

The sequence below is designed as five independently reviewable tickets. Each
ticket declares the previous one as its blocker.

### Ticket 1: lock the MCP surface and secret-boundary ADR

**Deliver:** protocol versions; Go SDK pin; controlled-bearer profile; canonical
endpoint; initial tool schemas; exact service/capability/audit mappings; result
bounds; no-secret default; optional reveal requirements; mutation deferrals;
threat-model and compatibility amendments.

**Accept when:** an operation matrix maps every proposed primitive to an
existing Hikyo capability/formula and audit event; a seeded secret is formally
excluded from the initial surface; unsupported client cases are explicit; and
OAuth, secret reveal, resources, prompts, tasks, and mutations each have an
explicit in/out decision rather than an accidental implementation.

### Ticket 2: add the stateless MCP transport and closed registry

**Deliver:** Go SDK v1.7.0; `/mcp`; `Stateless: true`; explicit Origin/Host/body
protections; `server/discover`; version policy; operation registry; service-only
adapter boundary; metrics; JSON response mode.

**Accept when:** protocol-focused conformance passes for `2026-07-28`; requests
work across alternating replicas without a session ID; required headers/errors
are exact; cancellation reaches service work; and static analysis rejects
store/crypto imports or an unregistered MCP handler. The 2025 compatibility
profile is added only after its exact stateless behavior passes a separate test.

### Ticket 3: ship the safe read-only product surface

**Deliver:** reviewed read tools, typed schemas/results, bounded cursor
pagination or `GetMany`, input/output validation, stable server instructions,
safe domain-error translation, and redaction tests. Keep secret reveal, secret
entry, write/publish, resources, subscriptions, prompts, and tasks unadvertised
unless the ADR explicitly included them.

**Accept when:** catalogs are deterministic; every call repeats current Hikyo
authorization; unauthorized resources are indistinguishable from unavailable
ones under existing oracle rules; domain errors are model-actionable but safe;
and a seeded canary secret is absent from every transport body, catalog,
resource, error, log, trace, and metric.

### Ticket 4: deployment, client interoperability, and documentation

**Deliver:** feature configuration; chart/Compose/proxy documentation; CI
conformance pin; Codex, Claude, OpenAI API, official Go-client, and Inspector
smoke tests; operator token-mint/grant instructions; metrics and runbook.

**Accept when:** two replicas serve successive calls without stickiness;
public HTTPS checks prove `/mcp` and one authorized safe tool call; invalid and
revoked credentials reveal no tenant fact; client configurations contain no
literal tokens; and the docs distinguish local/private clients, public API
reachability, ChatGPT plugin packaging, and OAuth availability.

### Ticket 5: decide whether MCP needs OAuth human delegation

**Deliver:** a separate OAuth ADR that starts from named-client evidence and
decides whether human delegation is still necessary after the controlled-bearer
release. Compare retaining only managed service-account access with adding an
embedded authorization server. If OAuth is selected, compare then-maintained Go
implementations using section 4.6's release, security, standards, storage, and
interoperability gates; spike only the shortlist; keep the AS in `hikyo server`;
and define the follow-up tracer tickets. Never hand-write OAuth protocol
machinery and do not add an external authorization service. Keep secret reveal,
sensitive entry, and mutation ceremonies in separate decision and implementation
tickets rather than coupling them to OAuth.

**Accept when:** the ADR records the necessity decision from current
primary-source and named-client evidence. A no-OAuth decision closes the ticket
without adding protocol code. An OAuth decision records the selected package or
an explicit no-viable-package outcome; protected-resource and AS metadata;
client registration; access-token profile; principal mapping; scope
attenuation; release and vulnerability evidence; spike results for discovery,
resource indicator, PKCE, issuer, redirects, storage races, and target clients;
and independently grabbable implementation tickets with exact blocking edges.

### Planning range

For one engineer familiar with Hikyo, the controlled-bearer, read-only release
is roughly **10 to 18 engineer-days**, including ADR, bounded query work,
dual-engine tests, conformance, packaging, named-client smoke tests, docs, and
deployment proof. A narrowly gated reveal tool is another **4 to 8
engineer-days**. URL elicitation plus protected mutation/publish can add **8 to
15 engineer-days**. The embedded authorization server is a separate **25 to 45
engineer-day** security project before independent review. These are planning
ranges, not commitments; pagination depth, AS interoperability, and ceremony
decisions are the largest uncertainties.

## 11. Primary-source ledger

| Topic | Primary source |
|---|---|
| Current release and changes | [MCP 2026-07-28 announcement](https://blog.modelcontextprotocol.io/posts/2026-07-28/), [dated changelog](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/changelog.mdx), [versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning) |
| Transport and discovery | [transport overview](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports), [Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http), [`server/discover`](https://modelcontextprotocol.io/specification/2026-07-28/server/discover) |
| Server primitives | [tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools), [resources](https://modelcontextprotocol.io/specification/2026-07-28/server/resources), [prompts](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/prompts.mdx), [elicitation](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation) |
| MCP OAuth profile | [authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization), [authorization-server discovery](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/authorization-server-discovery), [client registration](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration), [security considerations](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations) |
| OAuth standards | [RFC 6750 Bearer](https://www.rfc-editor.org/rfc/rfc6750.html), [RFC 8414 AS metadata](https://www.rfc-editor.org/rfc/rfc8414.html), [RFC 8707 resource indicators](https://www.rfc-editor.org/rfc/rfc8707.html), [RFC 9207 authorization response issuer](https://www.rfc-editor.org/rfc/rfc9207.html), [RFC 9728 protected-resource metadata](https://www.rfc-editor.org/rfc/rfc9728.html), [OAuth 2.1 draft 13](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1-13), [CIMD draft 00](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-00), [RFC 7591 dynamic registration](https://www.rfc-editor.org/rfc/rfc7591.html), [RFC 9700 security BCP](https://www.rfc-editor.org/rfc/rfc9700.html) |
| Fosite assessment, not a selection | [v0.49.0 release](https://github.com/ory/fosite/releases/tag/v0.49.0), [README and security boundary](https://github.com/ory/fosite/blob/v0.49.0/README.md), [explicit composer](https://github.com/ory/fosite/blob/v0.49.0/compose/compose.go), [transaction interface](https://github.com/ory/fosite/blob/v0.49.0/storage/transactional.go), [audience parsing](https://github.com/ory/fosite/blob/v0.49.0/audience_strategy.go), [security policy](https://github.com/ory/fosite/security), [open memory-exhaustion report](https://github.com/ory/fosite/issues/882), [open PKCE lifecycle fix](https://github.com/ory/fosite/pull/883), [open go-jose update](https://github.com/ory/fosite/pull/858). These sources explain why a future ADR must refresh the maintained-package landscape rather than inherit Fosite as a recommendation. |
| Official SDKs | [Go SDK repository](https://github.com/modelcontextprotocol/go-sdk), [Go v1.7.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0), [Go server guide](https://github.com/modelcontextprotocol/go-sdk/blob/v1.7.0/docs/server.md), [TypeScript v2](https://ts.sdk.modelcontextprotocol.io/v2/), [TypeScript 2026 migration support](https://ts.sdk.modelcontextprotocol.io/v2/migration/support-2026-07-28) |
| Verification | [MCP conformance suite](https://github.com/modelcontextprotocol/conformance), [MCP Inspector](https://github.com/modelcontextprotocol/inspector) |
