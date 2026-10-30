# #1040: pre-authentication source admission

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/1040>.
Inspected and reproduced against main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
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
| Ordinary REST services, CLI `cli` and cookie `br` sessions | One deferred source charge before session verifier, principal generation and credential epoch reads | Existing exact-operation admission, live grants, assurance and authenticated human quota | Public 512 in-flight cap; body and transport deadlines |
| Workspace `ws` session on ordinary REST | Same session charge and reads; requesting-origin predicate unchanged | Existing account-security exclusions, exact-operation admission and grants | Same public bounds; CORS and origin binding unchanged |
| Workload `wl` and automation `au` bearers | One source charge before credential, service-account and epoch reads | Existing artifact/capability admission and machine fetch budget | Same public bounds; machine fetch rate remains per principal |
| Developer `dev` bearer | Existing source hook before developer lookup retained | Existing refusal and delegated-delivery policy retained | Same public and delivery bounds; no second source charge |
| Instance `ic` bearer, including paired directory GET | One source charge before connection lookup; ordinary two-read discipline retained; paired proof/replay checks still inside serve transaction | Directory-only artifact admission, current grants and DPoP replay limits unchanged | Same public cap; existing replay-store bounds |
| SCIM credential on dedicated SCIM routes or a generic REST route | One source charge after SCIM grammar and before credential lookup | Existing binding match, epoch, provider, per-binding serialization, audit and exact-operation rules | Public and SCIM body/page bounds |
| Browser mutation CSRF precheck and handler | Both resolutions reuse one source receipt; overload at the CSRF boundary now propagates as 429 | One authenticated quota receipt, even when CSRF and handler each resolve | Existing CSRF verifier and companion-cookie checks unchanged |
| MCP tools/call, modern and compatibility profiles | Transport attaches the same deferred hook; canonical machine resolver charges before lookup; nested admission/service reads share it | Existing machine-only artifact contract and shared principal/org/instance admission | Existing per-node 64 cap, 30-second deadline, body/response/cursor bounds and shared 4/8/64 leases |
| SPA document's authenticated remote-origin CSP read | Same deferred source receipt outside the API route group, before session resolution | Existing directory authorization; refusal retains baseline CSP and serves the document | Public in-flight and response bounds; no remote origins disclosed on refusal |
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
