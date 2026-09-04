# Issue 627 handoff: MCP phase-1 ADR

## Delivered

- Locked [MCP phase-1 ADR](../adr/mcp-server.md): protocol/SDK pin,
  compatibility, endpoint, stateless JSON transport, closed five-tool surface,
  exact service/formula/audit mappings, pagination, secret boundary, and
  explicit in/out decisions.
- Amended API/CLI, audit, operations, and threat-model ADRs through operative
  banners; indexed the new ADR in both document maps.
- No runtime, dependency, OpenAPI, migration, status-ledger, or configuration
  change is included in this documentation ticket.
- Removed the unreliable batch-loopback GPG preflight from `AGENTS.md`; actual
  signed commits remain mandatory and signing may never be bypassed.

## Locked implementation order

1. [#628](https://github.com/Hikyo-Org/Hikyo/issues/628) adds the feature-gated
   stateless transport, closed registry, security middleware, audit-origin
   migration, and protocol conformance.
2. [#629](https://github.com/Hikyo-Org/Hikyo/issues/629) adds datastore-bounded
   pagination and the five read tools without post-materialization slicing.
3. [#630](https://github.com/Hikyo-Org/Hikyo/issues/630) proves deployment,
   alternating-replica behavior, named-client interoperability, and operator
   documentation.
4. [#631](https://github.com/Hikyo-Org/Hikyo/issues/631) decides whether OAuth
   human delegation is necessary and, only if selected, evaluates currently
   maintained protocol packages. Fosite is not presumed.

## Review anchors

- Every advertised domain tool maps to exactly one existing operation:
  `key.list`, `environment.list`, `value.list`, `value.pending-list`, or
  `revision.list`.
- Phase 1 never requests `reveal` or `reveal-history`; service-account grants
  and current in-transaction authorization remain authoritative.
- Result bounds are byte limits as well as item limits. Each cursor page
  reauthorizes and consumes shared budgets; one cursor chain has fixed page,
  item, byte, and non-renewing time bounds.
- `server/discover` and `tools/list` are static tenant-free protocol metadata,
  not domain tools or unaudited tenant reads. Their exact unauthenticated-class
  audit exemptions are locked explicitly.

## Delivery and validation

- Branch: `docs/627-mcp-adr`, based on `origin/main` after research PR
  [#626](https://github.com/Hikyo-Org/Hikyo/pull/626) merged as `1c9baf03`.
- The mandatory Standards and Spec reviews found seven valid defects, all
  fixed: metadata audit and admission, amendment navigation, legacy lifecycle
  compatibility, transport-independent artifact admission, cumulative
  traversal bounds, and handoff evidence. Parent review also corrected the
  input-schema and exact-formula wording.
- `scripts/ci/verify-docs.sh` passed after the fixes: 32 ledger entries
  verified; Astro reported 0 errors, warnings, or hints; the 38-page build,
  OSS policy and fixtures, PWA and offline browser, PostHog, live-docs, and
  fallback-channel gates all passed.
- `git diff --check`, local-link inspection, and no-new-em-dash checks passed.
- Follow-up Standards and Spec reviews reported no remaining actionable
  findings.
- Pull-request state is recorded by the delivery workflow after the signed
  commit is pushed; this handoff contains no runtime implementation.
