# #1040: pre-authentication source admission

The charge for live human sessions described here was narrowed later: see
[live-session source admission](./live-session-source-admission.md).

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/1040>.
Inspected and reproduced against main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
The acceptance audit below was rechecked on dev `748930f1f3d219b0daaf11df826c4e002cf9134a`.
The authorized endpoint is a reviewable PR. Maintainer security review and the
human merge gate remain required. No release, deployment or production load
exercise is authorized or claimed.

## Contract and repair

The production HTTP middleware installed
`operation.WithPreauthenticationAdmission`, but ordinary session and machine
resolution never invoked its receipt. A bounded router/service regression
reproduced attempt 61 returning 401 while still executing all three resolver
reads for both a CLI session and a workload credential.

Each applicable resolver now invokes `operation.AdmitPreauthentication` after
cheap artifact grammar validation and before its first credential datastore
read. This reuses the existing source allowance, not a new limiter. The receipt
memoizes both success and refusal across nested service reads and transaction
retries, without caching authentication or authorization. The developer leg's
existing call remains in its original owner.

The charge is independent of liveness: grammatical unknown, expired, revoked,
generation-stale and epoch-stale artifacts spend the same source allowance as
live ones. Within that allowance, ordinary session and machine negative paths
retain their three reads in their existing order, decoy decode and constant-time
verifier comparison. Engine-internal timing remains the previously accepted
residual; these tests do not claim wall-clock equivalence.

Missing, malformed and non-credential artifact types remain cheap refusals,
without credential lookup or source-counter allocation. Conflicting browser and
bearer channels and malformed DPoP transport envelopes retain their early
rejection. The existing paired resolver still checks the credential before
cryptographically verifying its DPoP proof; this ordering is unchanged.

## Route and artifact matrix

| Entry and artifact | Pre-auth charge and protected work | After resolution | Other bounds |
| --- | --- | --- | --- |
| Ordinary REST services, CLI `cli` and cookie `br` sessions | One deferred charge before session verifier, principal generation and credential epoch reads: the source allowance, or the session's own 300 per minute budget once this node has proven it live ([live-session source admission](./live-session-source-admission.md)) | Existing exact-operation admission, live grants, assurance and authenticated human quota | Public 512 in-flight cap; body and transport deadlines |
| Workspace `ws` session on the self-scoped surface | Same session charge and reads; requesting-origin predicate unchanged | Existing account-security exclusions, exact-operation admission and grants | Same public bounds; CORS and origin binding unchanged |
| Workload `wl` and automation `au` bearers | One source charge before credential, service-account and epoch reads | Existing artifact/capability admission and machine fetch budget | Same public bounds; machine fetch rate remains per principal |
| Developer `dev` bearer | Existing source hook before developer lookup retained | Existing refusal and delegated-delivery policy retained | Same public and delivery bounds; no second source charge |
| Instance `ic` bearer, including paired directory GET | One source charge before connection lookup; ordinary two-read discipline retained (the paired leg charges in `AuthenticatePairedConnection`); proof/replay checks still inside serve transaction | Directory-only artifact admission, current grants and DPoP replay limits unchanged | Same public cap; existing replay-store bounds |
| SCIM credential on dedicated SCIM routes or a generic REST route | One source charge after SCIM grammar and before credential lookup | Existing binding match, epoch, provider, per-binding serialization, audit and exact-operation rules | Public and SCIM body/page bounds |
| Browser mutation CSRF precheck and handler | Both resolutions reuse one source receipt; overload at the CSRF boundary now propagates as 429 | One authenticated quota receipt, even when CSRF and handler each resolve | Existing CSRF verifier and companion-cookie checks unchanged |
| MCP tools/call, modern and compatibility profiles | Transport attaches the same deferred hook; canonical machine resolver charges before lookup; nested admission/service reads share it | Existing machine-only artifact contract and shared principal/org/instance admission | Existing per-node 64 cap, 30-second deadline, body/response/cursor bounds and shared 4/8/64 leases |
| SPA document's authenticated remote-origin CSP read | Same deferred source receipt outside the API route group, before session resolution | Existing directory authorization; refusal retains baseline CSP and serves the document | Public in-flight and response bounds; no remote origins disclosed on refusal |
| Public ceremony routes: CLI login start, device start and poll, redeem, CLI reauth redeem, pairing start and finish, workspace handoff start and redeem, passkey login, SAML ACS and metadata, signup request and verify, recovery, login challenges, credential establishment | Not bearer resolution. Each takes a single-use handoff, code or ceremony value and is gated by `Limiter.Enter` (per-source 10 per minute plus the memory-derived semaphore) in its handler or service before its first read. `GET /meta`, `/runtime/status` and `/auth/methods` use `AdmitDiscovery` | Existing single-use consumption, binding and refusal events | Argon2id semaphore, queue depth 16, public 512 in-flight cap |
| Password login/reauth and federated machine JWTs | Existing dedicated `Limiter.Enter` path remains; no new bearer hook added to federation resolution | Existing account throttles, issuer/JWKS bounds, authorization and refusal events | Existing Argon2id memory-derived semaphore, maximum 8 workers and queue depth 16 |
| Trusted server-side ceremony session-ID revalidation | Existing ceremony admission and opaque binding, not a wire bearer entry | Existing live session/epoch checks retained | Ceremony lifetime and single-use bounds unchanged |
| Missing/malformed bearer, credential conflict, public meta/static discovery | No new resolver charge without a grammatical credential lookup; meta/static discovery keeps its own existing charge | Existing public and non-enumerating response policy | Existing parsing/body/global bounds remain |

## Allowance, source attribution and refusal semantics

The canonical hook already used `Limiter.AdmitDiscovery`: 60 attempts per source
per minute, shared with meta/static discovery. This change preserves that exact
allowance and charge point, including for live credentials. It does not apply
the 10/minute Argon2id allowance to ordinary bearer lookup, raise any limits, or
add a credential-keyed counter. Password/JWT verification retains its separate
existing admission class. Operators behind NAT share a source allowance.

Single-node source windows remain sliding in memory. HA uses the existing
shared fixed-minute datastore counters. Two independent router/limiter instances
sharing coordination therefore receive one installation-wide allowance, not one
allowance per node. A fixed-window boundary can still admit two adjacent
windows' allowances, as documented by the admission owner. The verification
semaphore remains per node; it is not an installation-wide bearer semaphore.

Source attribution still uses the socket peer unless the peer belongs to an
explicit trusted-proxy CIDR. For trusted peers, the rightmost untrusted
`X-Forwarded-For` hop is authoritative. A forged header cannot select a new
bucket, including by prepending addresses to a trusted proxy chain.

Within allowance, invalid credentials retain their uniform 401 response. Once
the source is exhausted, credential state is not consulted: REST returns the
existing uniform 429 and source-window `Retry-After`. Shared coordination failure
fails closed before credential lookup and advertises the existing 5-second
retry hint. MCP retains its existing uniform 429 and 60-second hint. SCIM's
already-declared common 429 JSON response is now rendered by all typed refusal
wrappers and the pre-handler syntax-error path; ordinary SCIM errors retain
their RFC 7644 envelope. No OpenAPI or SDK contract changed.

## Signal ownership and integration with #1034

This repair adds no audit event, catalog entry, permanent account lockout,
metric label or new refusal reason. #1034 owns the bounded authentication signal
contract and any required ADR amendment. Its integration seam is the existing
`admission.ErrOverloaded` / `RateLimitedError`, before the resolver knows the
credential's liveness. Middleware and protocol adapters must preserve that
sentinel rather than turning it into unauthenticated or internal error. Existing
developer, federation, SCIM and authenticated-denial events stay with their
current owners. No raw credentials or attacker-controlled labels are added.

## Acceptance criteria and evidence

Audited against dev `748930f1f`. Paths are relative to the repository root.
`isolation/` is `internal/isolation/`; `admission/` is `internal/admission/`.

| Criterion | Evidence | State |
| --- | --- | --- |
| Route and artifact matrix: charge, expensive work, authenticated charge, global limits | The matrix above. Rows now match the live-session decision and list the public ceremony routes with their `Limiter.Enter` gate | Closed |
| Real HTTP unknown artifacts charge before database work | `isolation/preauthentication_admission_test.go:43` drives eight classes through the production router and asserts 429 with zero resolver queries past the allowance | Closed |
| Real HTTP expired, revoked and stale artifacts charge before database work | `isolation/preauthentication_admission_test.go:399` (`cli`, `br`, `wl`) and `isolation/preauthentication_acceptance_test.go:83` (`ws`, `au`, `ic`): uniform 401 and the unchanged read count at the last slot, then 429 with zero reads | Closed (gap filled here) |
| Malformed and missing artifacts take the cheap path | `isolation/preauthentication_admission_test.go:197`: empty, malformed and wrong-class values do zero reads on an exhausted source | Closed |
| Threshold-crossing source refused, another source progresses | `isolation/preauthentication_admission_test.go:43` (second source at line 102) and `:490` (shared nodes, second source) | Closed |
| Spoofed forwarded addresses cannot evade the limit | Untrusted peer rotating `X-Forwarded-For`: `isolation/preauthentication_admission_test.go:68`. Trusted proxy, rightmost untrusted hop, including a forged first header field line sent as a separate line: `isolation/preauthentication_transports_test.go:148` and `server/sourceip_internal_test.go`. That regression exposed a real bypass, fixed here in `API.sourceIP` and in the MCP transport's own `sourceIP` (`mcpserver/sourceip_internal_test.go`). `API.sourceIP` read only the first `X-Forwarded-For` field line, so an attacker line placed before the proxy's own line chose the source (RFC 9110 section 5.3 makes the lines one list), and the MCP reader fell back to the socket peer on any multi-line header, which shared one bucket among every client behind a proxy | Closed (bug fixed here) |
| HA: bound holds across nodes, shared backend failure fails closed | `isolation/preauthentication_admission_test.go:490` (two routers, one allowance, zero reads on injected failure); `admission/admission_shared_test.go:92` and `:161` kept | Closed |
| Global and bounded queue and concurrency controls remain | `server/http_limits_test.go:10` (512 in flight); `admission/admission_test.go:57` and `:87` (semaphore, queue depth 16) | Closed |
| Legitimate traffic is not charged twice | `isolation/preauthentication_acceptance_test.go` (`TestPreauthenticationLegitimateTrafficIsChargedOnce`) counts charges at the shared store for a live workload and for live `cli` and `br` sessions (gap filled here); `isolation/preauthentication_admission_test.go:219` (CSRF and handler resolve twice, one charge); `operation/preauthentication_admission_test.go:34` | Closed |
| Developer, SCIM and federation keep their semantics | Developer: `isolation/developer_admission_test.go:20`, `isolation/auth_refusal_siblings_test.go:207`. SCIM: `isolation/preauthentication_transports_test.go:23`. Federation: `isolation/preauthentication_acceptance_test.go:306`, which presents a malformed bearer to the federation service and covers only the dedicated-admission ownership; no federation code changed, and a valid-JWT path is not re-tested here | Closed |
| Authenticated capability and denial behavior unchanged | `isolation/artifact_admission_wire_test.go:14`; logout and CSRF flow at `isolation/preauthentication_admission_test.go:219`; no authorization decision changed | Closed |
| Real production resolver chain, bounded work asserted | The acceptance tests in `isolation/` use `server.New`, the production services and the in-transaction authorizer with the `authn.SetQueryObserver` read counter; the federation test calls the service directly with the production limiter. `server/preauthentication_admission_test.go:14` is the retained synthetic seam test, not acceptance | Closed |
| Anti-enumeration negative controls remain | Identical 401 bodies and fixed read counts across unknown, expired, revoked and stale states: `isolation/preauthentication_admission_test.go:399` and `TestPreauthenticationDeadSiblingArtifacts`. Each `ws`, `au` and `ic` class also has a live control on the same route and request shape (200, 404, 200), so a dead state is refused for its state | Closed |
| #1034 coordination, no secrets, no high-cardinality labels | One admission sample per refusal, no other stage, no durable audit row, no presented value or verifier retained: `isolation/preauthentication_acceptance_test.go:234`. Canonical seam: `isolation/auth_refusal_consumer_test.go:107` and `:151`. Closed sample shape: `audit/refusal_test.go:22` and `:53`. Metric labels: the closed-stage refusal collector, `server/refusal_metrics.go` and `server/refusal_internal_test.go`, plus `server/metrics_scrape_test.go:151` | Closed (gap filled here) |

The new tests fail when the hook is removed from the machine or instance-connection
resolver, and when live-session memory is bypassed. Each mutation was reverted and
none is committed.

### Remaining boundaries

- No automated check proves that every `security: []` operation takes a gate. The
  gates in the matrix were verified by reading the handler or service of each of
  the 24 public (`security: []`) operations. A new public route can still ship without one.
- Login challenge TOTP and WebAuthn finish read the challenge before `enterFactorBudget`
  charges the per-account factor budget. The read is bounded by the single-use
  challenge id, not a source charge, and is left as is.
- A cross-origin request, such as a workspace bearer's `Origin` header, costs one
  CORS allowlist read (`Workspace.OriginAllowed`) before the source charge. It is
  cached when the allowlist is empty and is not a credential read.
- Each #1034 admission refusal performs its own refusal-window write. It carries
  no credential and is bounded, but it is a write behind a refused source.
- `enterWorkspaceAdmission` treats a nil limiter as unlimited. Tests that build a
  router without one rely on that.
- A datastore fault during session resolution leaves the live-session memory
  alone by design. It has no regression test, as the live-session handoff notes.
- The live-session memory is per node, so N nodes allow N times the session
  budget. This is the accepted tradeoff recorded in the live-session handoff.
- The Postgres leg ran on a disposable local container. Hosted CI, maintainer
  security review and merge remain separate gates.

## Validation and remaining gates

The new isolation tests drive actual HTTP routers, production services and
in-transaction resolvers, with the existing test-only query observer:

- `TestPreauthenticationRealBearerResolution`: eight ordinary artifact classes,
  full source allowance, threshold crossing, spoofed forwarded addresses and a
  second source, with zero resolver queries after refusal.
- `TestPreauthenticationDeadArtifacts`: grammatical unknown, expired, revoked,
  epoch-stale and session-generation-stale rows through the ordinary service
  actor path; identical 401 bodies and three reads before overload.
- `TestPreauthenticationCheapRejectsAndBrowserMutation`: missing/malformed/wrong
  types do no resolver work; a real browser logout uses the final source and
  authenticated quota slots despite two session resolutions; CSRF overload is
  429 with no lookup.
- `TestPreauthenticationSharedNodesAndFailure`: two routers use real shared
  datastore counters and one allowance; a second source progresses; injected
  coordination failure refuses before resolver queries on both nodes.
- `TestPreauthenticationDedicatedTransports`: dedicated SCIM, paired directory
  and MCP entry paths, including the SCIM pre-handler refusal.
- `TestPreauthenticationDeadSiblingArtifacts`: unknown, expired, revoked,
  epoch-stale (and generation-stale for `ws`) workspace, automation and instance
  artifacts on their own routes, each at the last allowance slot, then a 429 with
  zero credential reads, after a live control per class.
- `TestPreauthenticationLegitimateTrafficIsChargedOnce`: a live workload
  credential spends one source charge per request, and a live `cli` or `br`
  session spends one on its first presentation and none afterwards, counted at
  the shared store.
- `TestPreauthenticationRefusalsAreBoundedAndSecretFree`: refusals on five
  artifact classes add exactly one #1034 admission sample each, no other stage
  count, no durable audit row, no credential read, and no presented value or
  verifier in the retained state.
- `TestPreauthenticationFederationKeepsItsDedicatedAdmission`: a malformed
  federated bearer is bounded by `Limiter.Enter` and neither spends nor is refused by the
  bearer source allowance.
- `TestPreauthenticationTrustedProxyAndDocument`: trusted-proxy chain attribution
  and the otherwise separate SPA document policy lookup.

All fixtures are isolated synthetic data. PostgreSQL is disposable; SQLite uses
the existing temporary-directory fixture. No production DoS experiment, live
provider acceptance, release publication or deployment was performed.

Run with a disposable PostgreSQL DSN configured through
`HIKYO_TEST_POSTGRES_DSN`:

```sh
agent-heavy -- go test -p 2 -parallel 2 ./internal/isolation -run '^TestPreauthentication' -count=1
agent-heavy -- go test -p 2 -parallel 2 ./internal/authz ./internal/operation ./internal/admission ./internal/server ./internal/mcpserver ./internal/service ./internal/boundary
agent-heavy -- scripts/ci/fast-feedback.sh --working-tree
```

Local final validation and hosted exact-head status are recorded in the PR.
Hosted full-candidate checks, maintainer security review and merge remain
separate gates. Cross-provider review is SKIPPED due to unavailable authorized
review access; no exception or CLEAN review is inferred.
