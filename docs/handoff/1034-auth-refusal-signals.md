# #1034 authentication refusal signals: decision packet

Status: **HOLD for the specific audit amendment and its required review.**
This packet proposes a contract. It does not amend an accepted ADR, register an
event, claim an implemented security floor, or close #1034.

Source inspected: `24e198b32a3ffd8d4013207b2f974d76ec49be6f`, the live
`Hikyo-Org/Hikyo` main on 2026-10-28. Scope: refusal producer, vocabulary,
compatibility controls and integration requirements. Instance reader routes
belong to [#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035); admission
enforcement belongs to [#1040](https://github.com/Hikyo-Org/Hikyo/issues/1040).
No release ledger, deployment, production fixture or notification is changed.

## Decision that prevents activation

[Issue #1034](https://github.com/Hikyo-Org/Hikyo/issues/1034) explicitly requires
an amendment before replacing silent invalid bearer authentication with a
signal. The operative MCP amendment in [audit-model.md](../adr/audit-model.md)
preserves silence for missing, invalid, expired and revoked bearers. Its
aggregation licence does not select a storage schema or loss policy.

[oss-mechanics.md, Governance](../adr/oss-mechanics.md) requires
reopening the locked decision's ticket, the same adversarial cross-model review,
and recording the amendment in the ADR. The audit decision is
[#24](https://github.com/Hikyo-Org/Hikyo/issues/24), with MCP propagation to
[#627](https://github.com/Hikyo-Org/Hikyo/issues/627). No amendment is recorded
here. Approval of the overall stabilization recommendations is not this
decision or a waiver of its review requirement.

Cross-provider review is **SKIPPED**, because the approved Anthropic subscription
path is unavailable. No paid API or replacement provider was authorized. Ordinary
source inspection and local tests do not satisfy that gate. Maintainer security
review and the later human merge instruction also remain required.

## Real route and artifact matrix

The table separates current ownership from proposed coverage. Grammar is a
caller claim, never evidence of a principal. A refusal is not an authorization
denial merely because it occurred on a protected route.

| Entry and artifact | Current owner and result | Proposed generic producer responsibility |
| --- | --- | --- |
| `POST /api/v1/auth/local/login`, browser or CLI password | `service.Auth.LocalLogin`; durable `auth.login` failure, no attempted username payload | Preserve dedicated login event; no second refusal event |
| `GET /api/v1/auth/whoami`, ordinary `br` cookie or `cli` bearer; account-security siblings | `authz.authenticateSession` and `authenticateResolvedSession`; missing/malformed cheap rejection; unknown, expired, revoked, generation/epoch stale return unauthenticated | Generic artifact refusal after final resolution; retain identical unknown/dead response and read-work classes |
| Ordinary protected API with workload/automation bearer | `service.Actor.resolve` to `authz.AuthenticateCaller` to `authenticateMachine`; three reads for grammatical unknown and dead credentials, no generic durable refusal | Generic artifact refusal; no token fingerprint or artifact-state lookup added |
| Workspace session `ws` and directory/paired connection artifacts | `AuthenticateCaller`, `authenticateInstanceConnection`, `AuthenticatePairedConnection`; origin/proof and lifetime checks inside resolution | Generic final authentication miss; preserve workspace origin binding and paired proof semantics |
| `/mcp` and `/mcp/codex`, `tools/call` | MCP production admission and ordinary service authorization, outside the API middleware stack; current invalid bearer silence is explicit | Propagate the same request signal context through the MCP owner; an API-only middleware cannot cover this path |
| MCP static discovery and `tools/list` | Closed unauthenticated exemption, no bearer resolution | No authentication failure inferred from intentionally anonymous protocol metadata |
| Developer `dev` credential on delivery | `authz.authenticateDeveloper`, existing `auth.artifact_class_refused` rejected-use event and pre-auth hook | Dedicated owner only; check actual persisted receipt before suppressing a generic fallback; do not duplicate |
| Federation exchange and bound assertion delivery | `service.Federation` and `authz` federation resolution; `identity.federation_refused` with closed causes | Dedicated owner only where it emits; trace both exchange and use, not just one route |
| SCIM `/api/v1/orgs/{org}/scim/v2/{binding}/*`, including discovery | `AuthenticateSCIMCaller` returns unknown/dead artifacts without an event; `service.SCIM.wireTx` emits `scim.credential_refused` for a resolved binding mismatch | Preserve the dedicated mismatch event; generic coverage must also cover the currently silent unknown/dead cases while retaining SCIM wire errors and binding confinement |
| Valid identity, wrong artifact class for matched operation | `authz` operation admission, `auth.artifact_class_refused`, uniform nonexistent response | Preserve exactly one dedicated event, with resolved identity |
| Valid identity lacking authority, including nonexistent scope | `authz.Authorize` and its single denial writer, durable `grant.denied` | Preserve per-event durability, operation/formula and resolved/claimed scope semantics; never aggregate or duplicate |
| Conflicting cookie and bearer, malformed/disallowed DPoP | `API.extractBearer`, before contract validation or handler; uniform 401 | Generic `credential-conflict` or `proof` refusal, without credential/proof material |
| Cookie mutation with CSRF mismatch | `API.requireCSRF`, live-cookie verification before handler; uniform 401 | Generic `csrf` refusal; a dead cookie must still allow a new anonymous login |
| Source allowance exhaustion or shared backend failure | #1040 canonical `operation.AdmitPreauthentication`; existing `admission.ErrOverloaded` / `RateLimitedError`, uniform 429 and `Retry-After` | Generic `admission` summary; do not resolve credentials to enrich it or charge again |
| Public 512-request cap before route matching | `boundPublicRequests`, uniform 429; operational listener separate | Bounded `admission` summary using public transport context; API middleware alone is too late |
| Contract parsing, unknown routes, CORS preflight, body limits | Contract/CORS/transport owners; bounded existing RED classes | No invented authentication failure from arbitrary 400/404 or anonymous preflight; preserve RED telemetry |
| SPA authorized remote-origin lookup | `remoteOriginSource` can resolve a browser session outside the API group | Carry source/correlation and deduplicate nested resolution if the final request is covered; do not treat a successful document response as an auth refusal |

Source owners: `internal/server/{api,browser,server,http_limits,metrics}.go`,
`internal/service/{service,auth,federation}.go`, `internal/authz/{session,machine,developer}.go`,
`internal/store/authn/scim.go`, `internal/mcpserver/`,
`internal/audit/{registry,row}.go`. The operation and wire registries remain
canonical for accepted names; the table is not a second route registry.

Important inspection limit: an event name's presence in the SCIM wire registry
does not prove every SCIM authentication refusal emits it. The actual writer
here is `recordMismatch`, after rollback. Similarly, developer refusal capture
uses the transaction's capture/flush protocol; the returned nonexistent sentinel
from `refuseAdmission` is not itself the persistence receipt. Do not infer
success or failure of persistence merely from its ignored return value.

## Recommended amendment, proposed only

Choose a **durable bounded summary**, `auth.request_refused` schema v1, in the
instance security trail. This replaces only the generic silent authentication
and named early-refusal cases above. It is neither a mirror of existing audit
events to stdout nor a per-source Prometheus label.

1. Use a database-time 60-second instance window. Keep one current staging
   aggregate with fixed counters for the closed stage set `artifact`,
   `credential-conflict`, `proof`, `csrf`, `admission`. Keep at most 32 source
   samples across the entire window. Overflow increments an explicit unsampled
   count; it cannot allocate a new source bucket. No per-token map exists.
2. Every accepted summary update commits through one narrow enumerated writer
   before returning the existing refusal. Staging is a new, explicitly mutable
   bounded accumulator, not an UPDATE to either append-only audit table. On
   rotation, seal at most five summary events, reset the staging row and advance
   its window in the same transaction. A crash cannot count an update without
   retaining it or seal the same window twice. HA nodes share this singleton.
3. A registered system sealer flushes a quiet final window without requiring
   another rejected request. Startup recovers a pending window. Failed seals
   retain pending data and block its overwrite; they never discard an old
   accumulator to start a new one. The implementation must prove this protocol
   on both engines before it is operative.
4. Bound signal work separately: eight writers in flight, at most 64 waiting,
   and a 100 ms total deadline including queue acquisition. These are proposed
   signal-work bounds, not changes to #1040 source budgets. Request cancellation
   cannot create an unbounded background task. Numeric defaults require the
   same review as the contract; no implementation benchmark is claimed here.
5. Sampling is deliberately bounded: at most the first 32 accepted samples per
   window. Retain exact durable counters for accepted updates, not an exact
   record for every flood request. Samples retain canonical source IP, a
   server-generated request ID and a matched operation name, or the fixed
   `unmatched` sentinel. A sample never takes a token, username, raw URL/query,
   user-agent prose, body, DPoP proof, password or fingerprint. Known-dead and
   unknown artifacts both use the single `artifact` stage; no extra resolution
   is performed to distinguish them.
6. Envelope actor is unauthenticated with null principal/credential/authority,
   empty instance chain and no object. System sealing uses system provenance;
   request sources live in the typed samples, not a fabricated sealer source IP.
   Payload: window start/end, stage, count, unsampled count, and at most 32
   validated source/request/operation samples. This needs an explicitly reviewed
   bounded structured-sample schema at the existing audit validator. Counts
   use checked integer arithmetic; saturation is an explicit health failure,
   never a wrap to zero. Existing `auth.*`, SCIM and `grant.denied` schemas stay
   unchanged.
7. Retention is the existing instance `security` policy. The sealed event uses
   existing instance query/export under instance `audit-read`, through #1035.
   Staging is internal state, not a new reader endpoint. Document the normal
   one-window publication delay, and expose pending/unhealthy sealing as bounded
   operational health so empty results cannot imply no recent refusals.
8. Sink, queue or deadline failure preserves the original refusal status/body
   and cannot authenticate anyone. It increments a closed, label-bounded health
   counter and makes signal health degraded. Such requests are explicitly
   **not durably counted**. No claim of lossless recording during datastore
   outage is made. This exception to the no-silent-loss aspiration must be
   accepted explicitly, including the independent collector's health alert.
   Never emit arbitrary sink errors or request data to logs.
9. Set a trusted per-request ownership receipt only after a dedicated event or
   generic update commits. Nested service calls, CSRF checking and retries share
   it. HTTP status inspection alone cannot tell authentication from authorization
   or prove a dedicated event persisted. Preserve `grant.denied` independently
   and never aggregate it, a successful disclosure or a grant mutation.
10. Generate correlation before the public cap and transport/MCP split. Do not
    trust `X-Request-ID`, forwarding headers from untrusted peers or artifact
    claims. Reuse `API.sourceIP` trust policy, validate IP syntax and bound each
    sample. The current `audit.Context` carries source, user agent and origin,
    but no request correlation; new correlation must have one canonical owner.

The explicit loss boundary in item 8 is why this design cannot be silently
inferred from an existing aggregation licence. If exact durable accounting for
every network refusal, including sink failure, is required, this recommendation
does not meet it; that alternative requires a separately reviewed available
durable admission/spool design and its resource budget. Per-request append-only
events without aggregation would permit distributed sources to grow storage at
request rate and are not recommended.

## Integration requirements

- **#1040:** retain `operation.AdmitPreauthentication`, its memoized one-source
  charge, existing error types and uniform 429 plus `Retry-After`. Charge after
  cheap valid grammar and before resolver reads. Missing/malformed cheap rejects
  are signal candidates, not a new charge rule. The generic observer must not
  call this hook to obtain a signal or probe an artifact after admission fails.
  The lane reports adding the receipt to MCP tools/call and the SPA CSP lookup,
  preserving overload through CSRF and the declared SCIM 429 response wrappers.
  These are coordination reports, not validated changes in this packet's base.
  Generic observation must key the existing overload sentinel before any
  resolver state is known, including at those sibling transports.
- **#1035:** consume sealed instance security events through its existing
  instance proof, query and export owners. Add filter vocabulary only through
  canonical generated contracts if required. Operator tests must correlate an
  isolated revoked replay's server request ID and source to a sealed sample.
  During floods, operators get totals plus bounded samples, not per-token
  reconstruction. No project automation gains instance authority.
- **#1038:** collector health must distinguish pending, sampled/unsampled, and
  failed signal accounting from an absence of attacks. No external messages or
  new collector credentials are authorized by this packet.

## Evidence and required acceptance

`TestAuthenticationRefusalWireCompatibility` uses the production HTTP router,
real services and both datastore fixtures. It compares missing/malformed,
unknown/expired/revoked browser and CLI sessions, generation/epoch staleness and
unknown/expired/revoked machine credentials; checks live controls and conflicting
channels; preserves one dedicated wrong-artifact refusal; and inspects complete
captured response headers/bodies, Debug logs, metrics and both audit exports for
bearer and request-path/query canaries. It asserts that unauthenticated failures
do not become `grant.denied`. It does not pin silence as a permanent invariant.

This is compatibility coverage, **not implemented refusal signal acceptance**.
The following remain **NOTRUN** until the amendment is operative and a producer
exists:

- Real generic revoked-token replay to retained source/correlation record through
  the #1035 API, CLI and browser interfaces; MCP entry points and early-cap path.
- Missing/malformed DPoP, live CSRF mismatch, developer/federation/SCIM dedicated
  event receipt suppression and sink failure without duplicate events.
- Distributed-source floods exceeding sample capacity; constant staging size,
  at most five sealed events/window, bounded writers/waiters/deadline, exact
  accepted totals and explicit unsampled/failed-accounting counts.
- Two-node concurrent rotation, crash/restart, quiet-window flush, seal failure,
  unavailable sink and integer overflow on both engines. Preserve old pending
  windows rather than overwriting evidence under pressure.
- Complete-output canaries for request bodies, passwords, attempted usernames,
  forwarding/request-ID spoofing and proof contents through those producers.
- Existing `TestAuditCore`, dedicated-owner suites, admission shared-backend
  tests, closed registry/redaction checks, full lint and generated contracts
  after the final implementation.

## Executable continuation

1. Obtain the specific contract decision and satisfy the locked ADR amendment
   process, or record an explicitly bounded owner change to that process. Do not
   interpret general 1.0 approval as either. Reopen #24 and record approved
   propagation in the canonical audit ADR only after the gate is satisfied.
2. Implement through the existing audit/authentication/transport owners, with
   SQLite and PostgreSQL migrations and the structured-sample validator. Add
   generator inputs, then regenerate affected registries/contracts. Recheck
   final #1040 and #1035 heads before integrating; do not repair their readers
   or admission algorithms in this lane.
3. Add failing real-path signal tests, implement the producer and sealer, then
   prove the acceptance negatives above. Select local checks from the complete
   PR diff with `scripts/ci/fast-feedback.sh --working-tree --plan`; run them
   through the shared heavy queue where required. Use disposable databases.
4. Update this handoff with the tested revision, exact local/hosted proof and
   remaining review gates. Sign and DCO every commit, verify the full PR range
   before pushing and GitHub verification afterward. Keep the PR unmerged
   pending the later explicit maintainer merge instruction.
