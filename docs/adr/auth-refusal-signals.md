# Bounded authentication refusal summaries (#1034)

Owner-approved 2026-10-28. Operative when the implementation PR merges.
The owner approved the exact contract in the
[decision packet](../handoff/1034-auth-refusal-signals.md) and explicitly waived
cross-provider review for #1034 only. No qualifying cross-provider review was
performed. Maintainer security review and the human merge gate remain required.
This is an amendment of #24, propagated to #627, not a general governance waiver.

The approved packet's ten-point summary contract is normative: database-time
60-second instance windows; a single bounded mutable staging row separate from
append-only audit tables; at most five `auth.request_refused` v1 instance security
events per window; 32 source/request/operation samples across the whole window;
exact committed counters with explicit unsampled counts; eight writers, 64
waiters and a 100 ms total deadline; atomic seal/reset/update; startup and quiet
window sealing; no overwrite of unsealed evidence; and visible, explicitly
non-durable accounting failure when the sink, queue or deadline fails.

Authentication responses remain non-enumerating. Generic summaries do not name
the reason a grammatical artifact died or resolve it again. Source attribution
uses the existing explicit proxy trust policy and canonical IP addresses.
Correlation is server-generated before public admission, independent of client
headers and JSON-RPC ids. Operation samples come only from validated registries;
early refusals use `unmatched`. Credentials, attempted usernames, passwords,
proofs, user-agent prose, bodies, raw paths/queries and token fingerprints are
excluded. Summary actors are unauthenticated with null identity, credential and
authority; system sealing has no fabricated source IP.

Dedicated refusal suppression requires a durable commit receipt, including
rollback-settlement events. It is not inferred from a credential prefix, HTTP
status, captured insert or tentative write. Existing dedicated refusal events
and `grant.denied` retain their own cardinality, trail, retention and durability.
Successful disclosure and authenticated authorization denials are never
aggregated by this amendment.

Sealed summaries use the existing instance `audit-read` query/export owners
through #1035. Staging is not a new reader endpoint or instance authority.
Normal publication can lag by one window plus the sealer tick. Operational
health exposes accounting failures and pending window age with fixed series,
without source/principal/token labels. Empty audit results cannot establish an
absence of recent attempts while accounting is pending or unhealthy.

#1040 owns pre-auth charging and its existing overload sentinels, uniform 429 and
`Retry-After`. Observing a refusal does not invoke admission again or repair its
resolver paths. Anonymous discovery, arbitrary bad requests and successful SPA
documents do not become authentication failures merely from their HTTP status.
