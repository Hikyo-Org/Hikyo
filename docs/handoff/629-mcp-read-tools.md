# Issue 629 handoff: bounded read-only MCP configuration tools

## Delivered

- Registered the five ADR-approved read tools on the stateless MCP transport
  from #628: `hikyo_list_definitions`, `hikyo_list_environments`,
  `hikyo_inspect_configuration`, `hikyo_list_pending_changes`, and
  `hikyo_list_revisions`. Each tool maps to exactly one existing read service
  operation, carries stable snake_case JSON Schema inputs, typed structured
  outputs, read-only annotations, and the pinned authorization operation,
  formula, audit disposition, machine-only artifact policy, and secret policy.
- Added a bounded keyset page method at the service and store layers for every
  mapped list operation. Each page method verifies the SAME store authorization
  its unbounded sibling verifies, under its own registered store operation, and
  fetches strictly past the cursor with a `LIMIT`. No handler materializes a
  whole collection to slice a limit afterwards.
- Repeated current identity resolution, grants, capability formulas, protected
  scope, and audit behavior on every call: the transport passes the raw
  service-account bearer straight to the service as `service.Bearer(raw)`, and
  the service resolves the principal and authorizes inside its own transaction.
- Added an encrypted, authenticated pagination cursor. It binds the tool, the exact
  scope ids, the stable keyset position, the page and item and byte chain
  counters, and one 15-minute chain expiry that continuation never renews. The
  AEAD is XChaCha20-Poly1305 under an instance-wide key derived from the keyring's
  root token key. Each cursor operation refreshes the authoritative shared token
  key version, so cursors remain portable when another replica rotates it. The
  transport holds only a narrow Seal/Open sealer because the crypto chokepoint
  confines the primitive to `internal/crypto`.
- Enforced the phase-1 output bounds in the transport: per-page byte fit against
  the 256 KiB structured-content bound with a named `result_item_too_large`
  refusal, and the chain bounds (10 pages, 1000 items, 1 MiB) with a named
  `traversal_limit_reached` refusal and no continuation cursor.
- Added datastore-coordinated MCP admission after authorization and before
  tenant-shaped work: a 60-call/minute token bucket with capacity 20 per
  service-account principal, plus 4/principal, 8/organization, and 64/instance
  concurrency claims shared across replicas. Coordinator failure refuses the
  call. Every successful call releases its expiring claim through a bounded
  cleanup context that survives request cancellation.

## Boundary held

- `internal/mcpserver` still imports no `internal/store`, generated SQL, or
  `internal/crypto` package. It depends on narrow service interfaces plus
  `service.Bearer`, and it uses no cryptographic primitive directly.
- No mutation, no reveal, no secret plaintext. `Values.List` is always called
  with `reveal=false`; a `secret` cell and a `secret` or unset pending draft
  carry only classification and set/absent presence. The output mapping drops
  any value on a non-`config` cell as defense in depth.
- Unauthorized reads collapse to one `SafeOperationError`, indistinguishable
  from a nonexistent resource. Only the cursor, bound, and argument errors
  surface their own tenant-safe token.

## Validation anchors

- Service and store dual-engine pagination equivalence for all five operations:
  concatenated pages reproduce the unbounded read in order, on sqlite and
  postgres (`internal/isolation/mcp_pagination_test.go`).
- Transport tool tests with fakes: the closed five-tool catalog, cursor paging,
  a final page that omits the cursor, cursor tamper rejection, cursor scope
  binding, page-size range rejection, unknown-argument rejection, and the
  secret-plaintext drop (`internal/mcpserver/tools_test.go`).
- End-to-end over the real transport, services, and datastore: a seeded secret
  canary is absent from every tool result and from the denial error, the config
  plaintext is present, an ungranted service account receives the one safe
  error, and a cursor minted by one handler is accepted by an independent
  handler over the same datastore (`internal/isolation/mcp_e2e_test.go`).
- The closed registry, formula, audit disposition, store-operation completeness,
  read-only, and cross-engine query-contract invariants stay green.
- Cursor crypto tests load two independent keyrings over one shared store,
  rotate through one replica, and prove the other immediately accepts cursors
  under the new authoritative key.

## Verification

- `go test ./... -count=1` passes: 4604 tests across 74 packages, including
  local PostgreSQL and SQLite legs.
- `go build ./...` and `go vet ./...` are clean.
- `go test -race ./internal/mcpserver -count=1` passes all 50 transport tests.
- `go tool sqlc generate` is stable and `git diff --check HEAD` is clean.
- Standards and spec reviews were completed, followed by three blocking native
  Codex review rounds. Every valid finding was fixed with regression coverage.
