# Issue 628 handoff: stateless MCP transport

## Delivered

- Added the feature-gated `POST /mcp` endpoint using the official Go MCP SDK
  pinned at `v1.7.0`, with stateless JSON responses and protocol `2026-07-28`.
- Added a deterministic closed registry whose typed rows bind tool name,
  input and output schemas, service operation, authorization operation,
  formula, audit disposition, machine-only artifact policy, read-only
  annotations, result bound, and secret policy. Registration verifies the
  formula and audit disposition against the authorization registry.
- Added a transport-independent operation contract consumed by the existing
  in-transaction artifact-admission chokepoint. Raw service-account bearers
  stay redacted and are available only to registered adapter callbacks.
- Added exact Host, trusted-proxy authority, Origin, protocol mirror header,
  content type, Accept, request size, bearer size, deadline, concurrency,
  cancellation, admission, authenticated rate-limit signaling, and safe-error
  enforcement.
- Added `mcp` to the audit origin vocabulary with forward and rollback
  migrations for SQLite and PostgreSQL. The static discovery and catalogue
  methods are explicitly classified and audit-exempted.

## Boundary held

- The production registry is intentionally empty in this ticket. Issue #629
  owns the five datastore-bounded read tools and their service mappings.
- `internal/mcpserver` imports no store, generated SQL, or crypto packages.
  It has no generic operation proxy, REST loopback, session state, or outbound
  bearer forwarding.
- Browser CORS remains disabled for `/mcp`, even when an Origin is approved for
  the separate cross-instance workspace API.

## Validation anchors

- Conformance tests pin discovery capabilities, mirror headers, explicit
  legacy refusal, validated static notifications, JSON responses, deterministic
  catalogues, unknown-tool refusal, and the exact SDK version.
- Security tests cover body and result bounds, Host and Origin rejection,
  trusted proxies, bearer multiplicity and redaction, safe errors, deadlines,
  cancellation, discovery admission, and instance concurrency.
- Two independent handlers with separate registries accept alternating
  requests without a session id.
  An import-boundary test prevents store, generated SQL, or crypto imports.
- The existing cross-engine upgrade test now proves that an `mcp` audit origin
  is accepted after migration while pre-migration data survives.

## Next ticket

Issue #629 should register only the five ADR-pinned read tools through this
registry, call existing services with the callback's raw bearer, and add the
dual-engine authorization, pagination, cursor, denial, rate, and secret-canary
evidence that depends on those concrete operations.

## Verification

- `go test ./... -count=1`: 4,545 tests passed across 74 packages.
- `go test ./internal/isolation -count=1 -timeout 30m`: 1,419 tests passed.
- `go test -race ./internal/mcpserver -count=1`: 30 tests passed.
- `go build ./...`, `go vet ./...`, and the complete docs/PWA verification
  passed.
- Independent spec and standards review rounds returned clean after their
  findings were fixed.
