# Hikyo MCP write capability (ADR, decision locked 2026-09-14)

> **Status: decision locked, not yet operative.** The owner locked this decision
> on 2026-09-14 via grilling ([#742](https://github.com/Hikyo-Org/Hikyo/issues/742)).
> Per the [oss-mechanics.md](./oss-mechanics.md) amendment procedure it becomes
> **operative** only after a cross-provider adversarial review of this ADR
> concludes SOUND and the governance PR merges. It is the separate ADR that
> [mcp-server.md](./mcp-server.md) requires before any mutating MCP tool can
> exist. Until operative, MCP stays read-only.

## Context

Phase 1 ([mcp-server.md](./mcp-server.md)) locked a read-only MCP adapter. Its
phase-boundary table puts "any mutation, validation-as-dry-run, stage, publish,
approval, or adapter action" out of scope and states a separate ADR is
required. This ADR delivers the smallest coherent write surface: staging a
pending change and validating a proposed change, both mapped 1:1 to existing or
net-new audited authorization operations, gated behind a second default-off
operator flag.

Publish, secret reveal, and every other out-of-scope path from the phase-1
threat-model closing clause remain out; each needs its own later amendment.

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
- `registry.go:138` refuses any artifact other than a machine credential; a
  write tool inherits that refusal of human, SCIM, and session artifacts
  unchanged.
- The Host, Origin, bearer-redaction, and uniform-401 transport controls in
  `handler.go` are operation-agnostic and are reused unchanged.

So the only structural blocker is the registration gate in `mcpserver`.

## Decision

### 1. Two tools, mapped 1:1 to audited operations

- `hikyo_stage_change` maps to the existing `value.stage` operation
  (`internal/authz/registry.go:2465`): formula `edit@env`, it writes a pending
  draft (`StorePendingStage`) and emits `EventValueStaged`. It performs no
  publish and no downstream delivery.
- `hikyo_validate_change` maps to a **net-new** `value.validate` operation:
  formula `edit@env`, zero mutating store operations, emitting a new
  `EventValueChangeValidated`. It validates a caller-supplied proposed change
  and returns findings. It is a genuine non-mutating operation, not a write
  performed as a dry run (`research/hikyo-mcp-server.md:205`).

No generic dispatch: one tool, one operation, as in phase 1.

### 2. Scope boundary: stage never publishes

The "non-secret only" framing is dropped. The secret/non-secret distinction is
a property of the key, not of `value.stage` (its `edit@env` formula covers
both, and the secret scanner it runs is a detector, not an authorization gate).
The enforceable and reviewable boundary is instead: **the MCP write surface can
stage and validate, never publish or deliver.** A staged change is an inert
pending draft; nothing reaches an adapter or downstream target until a separate
`value.publish`, which stays out of scope and which a machine credential cannot
perform (see § Protected environments). The existing secret scanner still runs
at the stage ingress.

### 3. Alignment with the audit model

[audit-model.md:105](./audit-model.md) forbids `audited:none` for any operation
whose formula is beyond bare `read` or that mutates state, and permits it only
for tenant-class proof-scoped pure reads. Both write-surface operations have an
`edit@env` formula, so both are refused `audited:none` and must emit events:
`value.stage` already emits `EventValueStaged`; `value.validate` emits the new
`EventValueChangeValidated`. `origin=mcp` already marks events emitted while a
service operation is reached through `POST /mcp` (audit-model.md amendment
2026-09-04), so both tools are already audit-shaped. Note that
`value.validate` mutates nothing yet is still audited, because it is an
authority-bearing action, not a pure read whose result the trail would
duplicate.

### 4. Relax the registration gate, precisely, keyed off a declared tool class

`registry.go` gains an explicit tool class on `ToolSpec` (read vs
write-surface) and the `AuditDisposition` type is extended to admit an
events-emitting disposition alongside `audited:none`. The gate then asserts:

- **read tool** requires `policy.ReadOnly && policy.AuditedNone` (unchanged).
- **write-surface tool** requires `!policy.AuditedNone`: it must emit events.
  `policy.ReadOnly` may be true (`value.validate`) or false (`value.stage`);
  the registry row and tool annotations are derived from `policy.ReadOnly`, not
  hand-set. `ReadOnly=false` means `ReadOnlyHint: false`, `IdempotentHint:
  false`, and destructive-hint from the operation.

The one hard, fail-closed rule: **a write-surface tool is never
`audited:none`.** An unaudited mutation or authority-bearing action can never
register. As in phase 1 the tool's declared formula must still match the authz
registry's derived policy for the mapped operation, so a tool cannot lie about
its class.

### 5. Gate behind a second operator flag

Registration-time exclusion behind `HIKYO_MCP_WRITE_ENABLED`, a
self-configuration catalogue entry alongside `HIKYO_MCP_ENABLED` and
`HIKYO_MCP_ALLOWED_ORIGINS` (`internal/config/variables.go:115-116`,
`internal/runtimeconfig/catalogue.go:58-59`). Two independent flags:

- Write requires the read transport enabled first (`HIKYO_MCP_ENABLED`).
- Default off. The flag controls availability, not permission.
- Write-surface tools are installed into the frozen registry only when the flag
  is set. An unregistered tool cannot appear in `tools/list` or be called,
  which is strictly stronger than a runtime per-call check.

The real authorization gate stays the per-operation authz formula plus the
audit event, unchanged. The flag prevents accidental enablement; it grants no
authority on its own.

### 6. Cancellation rolls back open work

`value.stage` mutates, so its handler must roll store work back on cancellation
or client disconnect, per the phase-1 cancellation semantics in
[mcp-server.md](./mcp-server.md) ("cancellation reaches store work and rolls it
back"). No partial stage may survive a cancelled `tools/call`.
`value.validate` mutates nothing, so cancellation is trivially safe for it.

## Protected environments

`value.stage` carries no `postGrantForbidden` (only `value.publish` does,
`internal/authz/registry.go:2492`), so a machine credential holding `edit@env`
can stage pending drafts into any environment it is authorized for, including
protected ones. This is accepted: staging produces only an inert pending draft;
the protected-environment ceremony and the blocked-environment veto bite at
publish, which is out of scope and which a machine credential cannot satisfy. No
MCP-layer authorization refusal is added, because authorization belongs in the
operation, never in the transport ([mcp-server.md:243](./mcp-server.md)).

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
  enablement guard and couples two distinct trust decisions.
- Runtime per-call write check instead of registration-time exclusion:
  rejected. A registered-but-refused write tool still appears in `tools/list`
  and leaks the surface; registration-time exclusion does not.
- `audited:none` for a write-surface op with a compensating log: rejected.
  Directly violates audit-model.md:105 and defeats the trail the action
  requires.
- Enforcing "non-secret only" at the tool: rejected. Not expressible at the op
  level; the enforceable boundary is "stage never publishes" instead.
- `value.validate` as a read-shaped `read@env` audited-none tool: rejected. It
  would let any read-capable caller drive the scanner and schema engine against
  arbitrary input and drifts from the change-workflow intent; the authority
  should match "you can validate a change where you could make one."
- MCP-layer refusal of staging into protected environments: rejected. It would
  place an authorization decision in the transport.

## Consequences

- Two new operator-facing tools, `hikyo_stage_change` and
  `hikyo_validate_change`, behind `HIKYO_MCP_WRITE_ENABLED`.
- One net-new authorization operation `value.validate` and one net-new audit
  event `EventValueChangeValidated`, with forward and rollback closed-enum
  migrations on SQLite and PostgreSQL, landing with the emitter.
- One new operator flag and its catalogue entry.
- A relaxed but still fail-closed registration gate in `mcpserver`, keyed off a
  declared tool class.
- Publish, secret entry, and secret reveal remain out; each needs a further
  amendment.
