# Handoff: MCP write capability ADR (#742)

Issue key: [#742](https://github.com/Hikyo-Org/Hikyo/issues/742). ADR draft:
[docs/adr/mcp-write.md](../adr/mcp-write.md). Banners: mcp-server.md,
threat-model.md. This doc lets a fresh context pick up without re-deriving.

## Status

Decision LOCKED 2026-09-14 via grilling. Not yet operative. Operative still
requires: a cross-provider adversarial review of the ADR text concluding SOUND,
and the governance PR merging. Per [oss-mechanics.md](../adr/oss-mechanics.md)
§ Governance, a locked ADR is amended only by reopening its ticket, running the
same adversarial cross-model review that locks decisions, and recording the
amendment in the ADR itself.

## Locked decisions (grilling 2026-09-14)

1. First write op is `value.stage` only; "non-secret only" framing dropped; the
   enforceable boundary is stage-and-validate never publish or deliver.
2. Two flags: `HIKYO_MCP_ENABLED` (read) + `HIKYO_MCP_WRITE_ENABLED` (write);
   write requires read; both default off; registration-time exclusion.
3. `hikyo_validate_change` is in scope for this amendment.
4. `value.validate` is a net-new op: `edit@env`, zero mutating store ops, emits
   net-new `EventValueChangeValidated`, behind the write flag; closed-enum
   migration on SQLite and PostgreSQL.
5. Gate invariant: declared tool class on `ToolSpec`; read tool requires
   `ReadOnly && AuditedNone`; write-surface tool requires `!AuditedNone` (must
   emit events), `ReadOnly` per-op, annotations derived. A write-surface tool is
   never `audited:none`.
6. Protected environments: machine staging into protected envs is accepted;
   protection bites at publish (out of scope); no MCP-layer authz refusal.

## What the code review established (verified, not asserted)

The only structural blocker to a write-capable MCP tool is the `mcpserver`
registration gate. Every layer below it is already write-agnostic:

- `internal/mcpserver/registry.go:145` refuses a tool unless its authorization
  operation is both `ReadOnly` and `AuditedNone`.
- `registry.go:128` accepts only `AuditDispositionNone`.
- `registry.go:178,186-191` hardcode `ReadOnly: true` and read-only annotations.
- `internal/mcpserver/tools.go:69` (`AdmissionService.Acquire`) authorizes any
  operation; `registry.go:138` refuses non-machine artifacts; `handler.go`
  transport controls are operation-agnostic.

Required to enable write (four points, in the ADR): extend the
`AuditDisposition` enum, relax the `:145` gate to admit an events-emitting
mutating op, map 1:1 to a real mutating authz op (`value.stage`/`value.publish`,
`internal/authz/registry.go:204,206`, which already emit events so
`AuditedNone=false`), accurate per-tool annotations, cancellation rollback, and
the `HIKYO_MCP_WRITE_ENABLED` flag as a self-configuration catalogue entry.

## Cross-provider review round 1 (Astra, OpenAI, low effort)

Target authored by Claude (Opus 4.8); reviewer OpenAI per the cross-model-review
provider-routing rule. Verdict: OBJECTIONS. Findings reconciled as follows.

| Finding | Astra verdict | Disposition |
|---|---|---|
| A1 "structurally impossible" | too broad | Reworded: the registration gate is the sole structural blocker; not "impossible in principle". |
| A6 read-only gate "cannot be faked" | WRONG | Accepted. `readOnlyStoreOps` (`authz/registry.go:1063-1068`) is a hand-maintained map. It is fail-closed by omission (unclassified store op derives `ReadOnly=false`), so the residual risk is a wrongful *addition*, not a fake. Downgraded to a review obligation recorded in the ADR, not a test. |
| C4 line cite `IdempotentHint` at :187 | WRONG line | Accepted. It is at `registry.go:190`. |
| C1/D1/D3 "MCP is the only blocker", write flag "necessary" | overclaim | Accepted. These are the proposed design, not established requirements. The ADR frames the flag and mapping as proposal, open for grilling. |
| B3/B5/C2/C3/D2 rendered as `<<ccr:...>>` blobs | unreadable | Reconstructed from primary source (stronger than the compressed text). B3: write ops are `AuditedNone=false` and emit events, so audited:none is forbidden for them (audit-model.md:105) and the tool complies by mapping to an emitting op. B5: secret entry/reveal separately out, non-secret config write is smallest viable first. C2: machine-credential-only admission inherited unchanged. C3: transport controls reused unchanged. D2: registration-time exclusion beats runtime per-call check. |

## Wrinkle resolved during grilling

`value.stage` is the generic value-write ingress; the secret/non-secret split
is a property of the key, not the op (`edit@env` covers both, and the secret
scanner is a detector, not an authorization gate). So "non-secret only" is not
expressible at the op level. Resolved by dropping that framing and adopting the
checkable boundary "stage-and-validate never publish or deliver": a staged
change is an inert pending draft, nothing is delivered until a separate
`value.publish` that a machine credential cannot perform.

## Not done

No implementation. No PR. No cross-model review of the ADR text yet (the R1
above reviewed the findings, not this ADR text; that review is the remaining
gate to operative). Not operative.
