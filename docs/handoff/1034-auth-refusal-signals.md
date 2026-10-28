# #1034 authentication refusal signals: decision packet

Status: **Exact contract approved by the owner on 2026-10-28; producer implemented, final validation in progress.**
The owner explicitly waived cross-provider review for #1034 only. Maintainer
security review and the human merge gate remain. This does not close #1034.

Source inspected: `24e198b32a3ffd8d4013207b2f974d76ec49be6f`, the live
`Hikyo-Org/Hikyo` main on 2026-10-28. Scope: refusal producer, vocabulary,
compatibility controls and integration requirements. Instance reader routes
belong to [#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035); admission
enforcement belongs to [#1040](https://github.com/Hikyo-Org/Hikyo/issues/1040).
No release ledger, deployment, production fixture or notification is changed.

## Recorded owner decision

[Issue #1034](https://github.com/Hikyo-Org/Hikyo/issues/1034) explicitly requires
an amendment before replacing silent invalid bearer authentication with a
signal. The operative MCP amendment in [audit-model.md](../adr/audit-model.md)
preserves silence for missing, invalid, expired and revoked bearers. Its
aggregation licence does not select a storage schema or loss policy.

[oss-mechanics.md, Governance](../adr/oss-mechanics.md) requires
reopening the locked decision's ticket, the same adversarial cross-model review,
and recording the amendment in the ADR. The audit decision is
[#24](https://github.com/Hikyo-Org/Hikyo/issues/24), with MCP propagation to
[#627](https://github.com/Hikyo-Org/Hikyo/issues/627). The specific owner approval now records this amendment in the canonical ADR.
The earlier overall stabilization approval alone did not authorize it.

The owner subsequently approved this exact packet and explicitly waived
cross-provider review for #1034 only. No cross-provider review was performed;
no paid fallback was authorized. The approval is recorded in the canonical
[audit amendment](../adr/auth-refusal-signals.md), operative on implementation
merge. Maintainer security review and the later human merge instruction remain.

## Real route and artifact matrix

The table separates existing ownership from generic producer coverage. Grammar is a
caller claim, never evidence of a principal. A refusal is not an authorization
denial merely because it occurred on a protected route.

| Entry and artifact | Baseline owner and result | Implemented generic producer responsibility |
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

## Approved summary contract

The owner approved a **durable bounded summary**, `auth.request_refused` schema v1, in the
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
   and a 100 ms total deadline including queue acquisition. These are approved
   signal-work bounds, not changes to #1040 source budgets. Request cancellation
   cannot create an unbounded background task. The owner approval includes these numeric defaults; no implementation
   benchmark is claimed here.
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
   counter and makes signal health degraded. Such calls have **no confirmed durability receipt**; an interrupted commit can
   have an uncertain outcome. The failure counter counts unconfirmed accounting
   calls, including seals, rather than an exact number of lost network requests. No claim of lossless recording during datastore
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

The owner explicitly accepted the loss boundary in item 8 as part of the
exact contract approval. It was not inferred from the aggregation licence. If exact durable accounting for
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
  The dependency is PR #1084 at `f5faf05fcb61e69327db3bf492c5a0c9ca0d40cc`.
  Its owner reports exact-head hosted feedback, CodeQL and full repository
  invariants passing; local lint hit its unchanged aggregate deadline. It is
  unmerged and still requires maintainer security review. Combined-source
  runtime proof will be recorded separately from this lane's own head.
- **#1035:** consume sealed instance security events through its existing
  instance proof, query and export owners. Add filter vocabulary only through
  canonical generated contracts if required. Operator tests must correlate an
  isolated revoked replay's server request ID and source to a sealed sample.
  During floods, operators get totals plus bounded samples, not per-token
  reconstruction. No project automation gains instance authority.
- **#1038:** collector health must distinguish pending, sampled/unsampled, and
  failed signal accounting from an absence of attacks. No external messages or
  new collector credentials are authorized by this packet.

## Implementation and validation

The source producer observes explicit outcomes from authentication, transport and
admission owners. It settles the pending signal before final response headers.
Successful SPA documents and incidental dead-cookie CSRF probes do not invent a
failure. The shared receipt suppresses a generic signal only after the dedicated
transaction or rollback-settlement flush commits. A failed dedicated write keeps
its existing fail-closed error and cannot suppress generic fallback accounting.

The dedicated singleton writer and system sealer use the ordinary transaction
owner. PostgreSQL row locking and SERIALIZABLE retries, or SQLite's write owner,
coordinate nodes. Audit inserts, staging reset and the next increment commit
atomically. Migrations 80 add only the bounded mutable row on both engines. The
normal sqlc/authz generators and development schema-identity generator own the
checked-in outputs. Backup inventory includes staging automatically; PostgreSQL
restore may replace only the untouched seed, never pending refusal evidence.

The operator guide is
[authentication-refusal-signals.md](../operations/authentication-refusal-signals.md).
It explains publication delay, retention, sample correlation, unsampled counts,
fixed health series and commit-outcome uncertainty. No reader route or
admission-enforcement repair is included in this PR.

The prior compatibility-only head `bc7ea467520c89c956e755197a2cb3202db3741c`
reproduced generic silence on main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Its local/hosted results do not qualify the producer added afterward. Canonical
main was rechecked at `970ac0db7c6024f91dedcefe34db7ca560b247e3`; the intervening
changes were documentation only.

Current producer checks exercise the real router and services on SQLite and
PostgreSQL, including modern/Codex MCP. They cover browser/CLI/machine dead and
unknown artifacts, stale generation/epoch, connection/workspace/SCIM siblings,
CSRF/DPoP/channel refusals, dedicated developer/federation/SCIM/login ownership,
retained revoked replay correlation, bounded source floods and concurrent nodes,
startup recovery, sink and seal failures, public cap observation and complete
wire/log/metrics/audit canaries. Exact final source and results will be recorded
below after validation completes. No hosted producer CI result is claimed yet.

Cross-provider review is **SKIPPED**, under the explicit #1034-only waiver.
Maintainer security review, protected merge-group proof and a later explicit
human merge instruction remain **HOLD**. Hosted/local source checks do not prove
publication, deployment, live-provider acceptance or real collector delivery.

## Executable handoff

1. Validate this PR's complete diff using
   `scripts/ci/fast-feedback.sh --working-tree --plan`, then the selected shared
   driver and affected owner/consumer suites. Persistence proof requires both
   datastores. Keep unavailable or failed checks explicit.
2. Integrate with #1040 PR #1084 and #1035 PR #1045 at pinned heads. Exercise an
   isolated revoked replay through the supported reader after the window seals;
   match `X-Hikyo-Request-ID` to the payload sample rather than the summary's
   envelope correlation. Prove canonical overload without a second source charge.
3. Obtain maintainer security review on the final signed/DCO PR head. Fresh
   hosted feedback/invariants and protected current-base merge proof remain
   separate requirements. Do not merge without a later explicit instruction.
4. Before release acceptance, #1037/#1038 owners must collect these sealed
   records and the five health series, and provide their own resource-specific
   external collection/alert receipts. This source PR does not authorize external
   messages, production mutation, paid usage or publication.
