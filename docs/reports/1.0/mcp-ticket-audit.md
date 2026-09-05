# MCP ticket audit and published decisions

Published decision record, 5 September 2026. [#631](https://github.com/Hikyo-Org/Hikyo/issues/631) was revised to an evidence-led human-delegation decision; [#651](https://github.com/Hikyo-Org/Hikyo/issues/651) was filed for the separate machine-MCP release proof. No OAuth implementation, public deployment or live named-client execution is claimed by these ticket changes. The implementation audit inspected `54c58bbdbf7abb90228482718a8bf4f0844f6ea4`; #627, #628, #629 and #630 are closed, and #637/#639 are merged. The new #651 acceptance gate remains open. Human identity integration must still respect #613/#617's owning semantics.


## Release judgment

The supported machine surface remains a valid 1.0 scope: opt-in stateless MCP, managed workload/automation bearers, five bounded read-only tools, configuration plaintext only, no secret material or mutation. Human OAuth, browser-cookie authentication, dynamic registration, secret reveal/entry and writes are explicitly outside the locked phase. Enterprise self-hosting does not inherently require an embedded OAuth authorization server.

The remaining actual release concern is evidence for advertised deployment/client claims, not absent human delegation. #639 explicitly delivered a checker/runbook rather than recorded exact-image public HTTPS or live Codex/Claude/OpenAI results. The official Go test uses a one-tool echo fixture; Inspector CI checks unauthenticated `tools/list` against a conformance fixture. These prove useful protocol behavior, but do not establish each named client's authenticated production-tool path.

Recorded disposition: #631 remains a post-1.0 evidence/decision issue. #651 tracks exact-image machine-MCP release acceptance separately from the delivered #630 implementation. Do not reopen #629 as unfinished OAuth work. Do not turn unavailable provider credentials into a fake test pass. A named client can be marked unverified or unsupported until proven, but narrowing any mandatory ADR acceptance requires an explicit recorded disposition rather than silently rewriting the gate.

## Revised [#631](https://github.com/Hikyo-Org/Hikyo/issues/631)

**Title:** MCP human delegation: decide the enterprise workflow and authorization-server necessity

### Problem and decision boundary

Hikyo already supports machine MCP with managed service-account bearers. Determine whether a concrete human-delegated workflow requires a different authorization model: per-user consent and attribution, user-scoped access, centrally terminated employee access, or a target client that cannot safely consume managed credentials. A protocol-version mismatch, failed proxy configuration or absent client execution evidence is not proof that OAuth is required.

This issue decides necessity and architecture. It does not implement OAuth, select Fosite by precedent, introduce an external authorization service, or expand the five-tool read-only surface. Existing machine authentication remains supported regardless of the decision.

### Acceptance

- [ ] Record an evidence matrix for exact client versions/protocol profiles and one real scoped configuration-read workflow. Distinguish managed-bearer support, automatic human sign-in, local desktop/CLI execution, and provider-hosted remote execution. Each row links reproducible redacted evidence or states the precise unverified/unsupported reason. Record token custody, operator workload, user attribution and termination behavior. Never treat an OAuth button or configuration example as a successful test.
- [ ] Identify the minimum unmet human workflow and compare: retain machine credentials; use a dedicated bounded machine identity managed by an operator; introduce embedded human delegation. State where a service-account identity is insufficient for human audit/consent or offboarding. Recommend no OAuth if the supported workflows do not require it; otherwise explain the product need and exact authority being delegated. A no-OAuth decision adds no protocol code and records revisit triggers.
- [ ] If human delegation is necessary, write a reviewed ADR covering existing Hikyo identity/IdP integration, per-request current authorization, exact resource/audience and issuer binding, consent, scope attenuation, client registration policy, exact redirect matching, mandatory PKCE, code/refresh token lifecycle, token theft/replay, revocation, audit attribution and multi-replica transactional storage. Specify account disable/removal, membership/grant change and IdP-session consequences; upstream login alone must not confer MCP authority. Discovery and registration must not create SSRF or token-passthrough paths.
- [ ] Only after a positive necessity decision, compare then-maintained embedded Go packages using pinned release/source, standards coverage, security response, supported storage atomicity, API stability, dependency advisories, license and real target-client behavior. Use maintained implementation and signature/protocol libraries. Spike only shortlisted packages with discovery, RFC 8707 resource binding, RFC 9207 issuer behavior, redirect/PKCE negatives, code redemption and refresh races, revoke-before-use, crash/restart and alternating replicas. Report failures without weakening required controls or silently substituting a different protocol profile.
- [ ] Publish bounded implementation tickets after the ADR locks, with dependencies, threat cases and both-engine acceptance. Separate provider selection, shared durable token/consent storage, authorization admission, client interoperability and operator lifecycle as appropriate. Secret reveal, secret entry, writes, approvals and publish remain separate decision lanes. Mark this issue complete when the evidence and decision are accepted, not when speculative OAuth code exists.

### Dependencies and ordering

- #627 locks the current machine boundary; #628/#629/#630 implementation is delivered through #637/#639 and predecessors. These are context, not still-open blockers.
- Missing exact-image/client evidence from #630 is a concrete input to the client-needs decision. Link the separate release acceptance record below; it must distinguish authentication from protocol compatibility.
- #613 and #617 are prerequisites for locking or implementing a human identity/consent integration whose semantics they affect. Read-only client evidence collection and a no-OAuth disposition can proceed now. Do not wait for them to inspect current clients, and do not call human semantics stable solely because code exists in a worktree.

### Release classification

Post-1.0 decision lane under the present supported product boundary. Becomes a release blocker only if an explicitly approved 1.0 human-delegation claim is added. Its absence does not make existing machine MCP unsupported.

## Filed [#651](https://github.com/Hikyo-Org/Hikyo/issues/651)

**Title:** MCP release acceptance: record exact-image HTTPS and named-client proof

### Problem and outcome

#630/#639 delivered deployment configuration, local conformance/isolation coverage and a fail-closed public checker. Their handoff explicitly leaves the public deployment and live named-client record to a later run. Complete that evidence for the candidate release and make the support matrix match demonstrated behavior. This ticket adds no OAuth or new tools.

### Acceptance

- [ ] Record exact server commit/image digest, deployment revision, public HTTPS endpoint, engine and replica configuration, UTC execution time and client/tool versions. Use a dedicated nonproduction fixture organization/project with synthetic configuration and a seeded secret canary. Keep credentials outside configuration files, arguments, logs, reports and provider response dumps; provider-hosted calls require authorized recipient/custody.
- [ ] Execute the existing `scripts/mcp-public-smoke` against the deployed candidate: unauthenticated tenant-free discovery and closed catalog, authorized production `hikyo_list_definitions`, invalid-token denial and a token proven live before committed revocation. Record revocation completion and the first subsequent request outcome without response bodies. The checker's two-second polling observation does not by itself prove zero propagation delay; the existing transactional next-call test plus a known post-commit probe establishes that assertion. Prove replacement credentials still work and denied credentials disclose no tenant facts.
- [ ] Exercise the official Go client and Inspector with a real scoped production tool, then the documented exact Codex CLI, Claude Code and OpenAI Responses profiles where those profiles are claimed supported. Record negotiated wire version and authenticated tool result separately from static discovery. A raw Go JSON-RPC probe does not stand in for another client's execution. Classify failures as authentication, transport/protocol, deployment or unavailable execution evidence. Keep legacy protocol refusal intact; adding OAuth does not fix it.
- [ ] Exercise successive authenticated calls and cursor continuation through two actual candidate server processes/replicas over PostgreSQL without session IDs or affinity. Observe immediate credential/grant changes, bounded cancellation/draining and released shared concurrency claims. Existing independently constructed in-process replicas remain regression evidence; report them honestly rather than labeling them a deployed multi-process test.
- [ ] Commit a redacted result artifact and update the operator support table to supported, unsupported with reason, or not yet verified for each pinned profile. Preserve conformance, closed registry, anti-oracle, canary, bounds, rate coordination and shutdown checks. Unknown or failed mandatory profiles remain open evidence; any narrower release claim requires explicit release/ADR disposition. No provider response bodies or tenant identifiers are necessary in public evidence.

### Dependencies and release classification

Depends on the #639 implementation and a reviewed candidate image plus an isolated HTTPS deployment and appropriate test-client credentials. Does not depend on #631, an embedded AS, or unfinished human-auth capabilities. Required before claiming the full advertised named-client/deployment matrix for 1.0. Use isolated synthetic fixtures and record any external prerequisite that prevents execution. No missing credential or client runtime may be reported as a passing result.

## Implementation evidence inspected

- `docs/adr/mcp-server.md`: locked five-tool machine surface, `2026-07-28`, explicit legacy refusal, per-call current authorization, bounded traversal, secret boundary and required client evidence.
- `docs/handoff/629-mcp-read-tools.md`, `internal/isolation/mcp_e2e_test.go`, `internal/isolation/mcp_pagination_test.go`: real services/datastores, dual-engine authorization and keyset pagination, canary absence, wrong-class/denial audit, independently loaded replica keyrings and immediate revoked-token refusal.
- `internal/service/mcp_admission.go`: current authorization before shared rate/concurrency charging, then independent current authorization at the page service; unavailable admission refuses and bounded cleanup survives cancellation.
- `internal/mcpserver/client_interop_test.go`, `scripts/ci/check-mcp-conformance.sh`: official Go echo fixture and Inspector unauthenticated catalog scope; pinned conformance remains a protocol gate, not proof of production authorization.
- `docs/handoff/630-mcp-operations.md`, `docs/site/src/content/docs/docs/mcp.mdx`, `scripts/mcp-public-smoke/main.go`: explicit managed credential custody, deployment settings, client examples, external proof boundary and checker behavior.

This was a bounded ticket and evidence audit, not a fresh full security review or test execution. No new implementation vulnerability was established by this audit. The supported machine scope is preserved.
