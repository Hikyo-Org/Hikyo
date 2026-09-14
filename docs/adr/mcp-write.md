# Hikyo MCP write capability (ADR, DRAFT, proposed 2026-09-14)

> **Status: DRAFT, not operative.** This ADR becomes operative only on owner
> decision, cross-model review concluding SOUND, and the governance PR merging,
> per the [oss-mechanics.md](./oss-mechanics.md) amendment procedure. It is the
> separate ADR that [mcp-server.md](./mcp-server.md) requires before any
> mutating MCP tool can exist. Issue key:
> [#742](https://github.com/Hikyo-Org/Hikyo/issues/742). The design below is a
> proposal for grilling, not a locked decision.

## Context

Phase 1 ([mcp-server.md](./mcp-server.md)) locked a read-only MCP adapter. Its
phase-boundary table puts "any mutation, validation-as-dry-run, stage, publish,
approval, or adapter action" out of scope and states a separate ADR is
required. This ADR scopes the smallest viable write: a non-secret configuration
mutation mapped 1:1 to an existing audited authorization operation, gated behind
a second operator flag.

Secret entry and secret reveal remain out of scope for this ADR. They carry
their own disclosure surface and require their own amendment.

## What blocks a write tool today

Verified against source at this revision:

- `internal/mcpserver/registry.go:145` refuses any tool whose authorization
  operation is not both `ReadOnly` and `AuditedNone`.
- `registry.go:128` accepts only `AuditDispositionNone`.
- `registry.go:178` hardcodes `ReadOnly: true` in the registry row;
  `registry.go:186-191` hardcodes read-only tool annotations
  (`ReadOnlyHint: true`, `IdempotentHint: true`, non-destructive, closed-world).

Everything below the `mcpserver` registration gate is already write-agnostic:

- `internal/mcpserver/tools.go:69` acquires admission for any authorization
  operation before claiming rate and concurrency capacity.
- `registry.go:138` refuses any artifact other than a machine credential.
- The Host, Origin, bearer-redaction, and uniform-401 transport controls in
  `handler.go` are operation-agnostic and are reused unchanged.

So the only structural blocker is the registration gate in `mcpserver`.

## Decision (proposed)

### 1. Map to a real audited mutating operation, 1:1

A write tool maps to an existing mutating authorization operation, with no
generic dispatch. `value.stage` and `value.publish`
(`internal/authz/registry.go:204,206`) are the natural first targets. They
already declare audit events, so their derived `AuditedNone` is false and their
`ReadOnly` is false.

This is the alignment point with the audit model:
[audit-model.md:105](./audit-model.md) forbids `audited:none` for any operation
whose formula is beyond bare `read` or that mutates state. A write tool
therefore complies by mapping to an events-emitting operation, never by
relaxing the audit disposition. `origin=mcp` already marks events emitted while
a service operation is reached through `POST /mcp` (audit-model.md amendment
2026-09-04), so a mutation performed over MCP is already audit-shaped; this ADR
adds the emitting operations, not a new envelope.

### 2. Relax the registration gate, precisely

`registry.go` changes:

- Extend the `AuditDisposition` type to admit an events-emitting disposition
  alongside `audited:none`, and relax the `:128` check accordingly.
- Relax the `:145` gate: a write tool requires its authorization operation to
  emit events (not `AuditedNone`) and to be non-read-only. The gate stays
  fail-closed: a tool must declare its class explicitly and it must match the
  authz registry's derived policy for the mapped operation.
- Make the registry row and tool annotations accurate per tool. A publishing or
  staging tool is `ReadOnly: false`, `ReadOnlyHint: false`,
  `IdempotentHint: false`, and destructive-hint set from the operation.
  Annotations remain defense in depth, never authorization
  ([mcp-server.md:243](./mcp-server.md)).

### 3. Gate behind a second operator flag

Registration-time exclusion behind `HIKYO_MCP_WRITE_ENABLED`, a
self-configuration catalogue entry alongside `HIKYO_MCP_ENABLED` and
`HIKYO_MCP_ALLOWED_ORIGINS` (`internal/config/variables.go:115-116`,
`internal/runtimeconfig/catalogue.go:58-59`). Two independent flags:

- Write requires the read transport enabled first (`HIKYO_MCP_ENABLED`).
- Default off. The flag controls availability, not permission.
- Write tools are installed into the frozen registry only when the flag is set.
  An unregistered tool cannot appear in `tools/list` or be called, which is
  strictly stronger than a runtime per-call check.

The real authorization gate stays the per-operation authz formula plus the
audit event, unchanged. The flag prevents accidental enablement; it grants no
authority on its own.

### 4. Cancellation rolls back open work

A write handler must roll store work back on cancellation or client
disconnect, per the phase-1 cancellation semantics in
[mcp-server.md](./mcp-server.md). No partial mutation may survive a cancelled
`tools/call`.

## Review obligation: the read-only store-op classification map

`readOnlyStoreOps` (`internal/authz/registry.go:1063-1068`) is a
hand-maintained map that drives the derived `ReadOnly` policy. It is fail-closed
by omission: an unclassified store operation derives `ReadOnly=false` and is
treated as mutating. The only failure that would matter here is a wrongful
addition marking a genuinely mutating store operation read-only. That is a
standing review obligation on any change to that map, recorded here so the
write path does not silently depend on its correctness. It is not a mechanically
testable invariant, because the map's fail-closed default already covers the
omission case.

## Alternatives considered

- Single flag reused for read and write: rejected. It removes the accidental-
  enablement guard the owner asked for and couples two different trust
  decisions.
- Runtime per-call write check instead of registration-time exclusion:
  rejected. A registered-but-refused write tool still appears in `tools/list`
  and leaks the surface; registration-time exclusion does not.
- `audited:none` for a write op with a compensating log: rejected. Directly
  violates audit-model.md:105 and defeats the trail the mutation requires.

## Consequences

- One new operator flag and its catalogue entry.
- A relaxed but still fail-closed registration gate in `mcpserver`.
- The first mutating MCP tool, mapped to an existing audited operation.
- Secret entry and reveal remain out; a further amendment is required for them.
