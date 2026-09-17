# Handoff: MCP write capability ADR (#742)

Issue key: [#742](https://github.com/Hikyo-Org/Hikyo/issues/742). ADR draft:
[docs/adr/mcp-write.md](../adr/mcp-write.md). Banners: mcp-server.md,
threat-model.md. This doc lets a fresh context pick up without re-deriving.

## Status

Decision LOCKED 2026-09-14 via grilling. Cross-provider review WAIVED by the
owner 2026-09-17 (quota exhausted; "skip adversarial review"). Operative upon
the governance PR merging. Per [oss-mechanics.md](../adr/oss-mechanics.md)
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
`value.publish`, which no MCP tool exposes (an automation may still hold
`publish` over REST; see ADR § Protected environments).

## Cross-provider review of the ADR text

Required by [oss-mechanics.md](../adr/oss-mechanics.md) before this amendment
is operative. Author provider: Anthropic (Claude), so the reviewer must be the
native OpenAI CLI (cached `gpt-6-astra`, effort `low`).

| Date | Round | Result |
|---|---|---|
| 2026-09-14 | attempt on `24ddbba` (pre-lock draft) | no verdict file produced; stale, superseded by the locked text |
| 2026-09-17 | R1 on the locked text | SKIPPED, quota gate: owner reported Codex out of limits |

A skipped pass is never SOUND. On 2026-09-17 the owner waived this review for
the amendment ("skip adversarial review"); the ADR status records the waiver.
If a later reopen wants the review, `.xreview/mcp-write-adr-brief.txt` in the
old review worktree is stale (pre-lock shape); write a fresh brief from the six
locked decisions above.

## Owner waiver and implementation (2026-09-17)

Owner instruction "Continue without review" (recorded on #742). Implementation
proceeds ahead of the SOUND verdict as a PR stacked on #766; the review is
still owed before #766 is undrafted, and the ADR stays non-operative until it
merges. Implementation ticket: [#767](https://github.com/Hikyo-Org/Hikyo/issues/767).

What landed (see the ADR § Corrections for the three source facts):

- `internal/mcpserver/registry.go`: `ToolClass` on `ToolSpec`, `AuditDispositionEvents`,
  gate keyed off the class, `ReadOnly` and annotations derived from the authz
  policy. Empty class is read (strictest default).
- `internal/mcpserver/write_tools.go`: `hikyo_stage_change` (→ `Values.Set`/`Unset`,
  `value.stage`) and `hikyo_validate_change` (→ `Values.ValidateSet`/`ValidateUnset`,
  `value.validate`); `RegisterWriteTools`, `WriteToolNames`, `AllToolNames`.
  Wire-safe `SafeDetail()` refusals cross verbatim (`cursor.go`); everything
  else still collapses to the one safe error.
- `internal/authz/registry.go`: `OpValueValidate` (`edit@env`, four store ops,
  emits `value.change_validated`). `internal/audit/registry.go`: the event.
- `internal/service/values_validate.go`, `scan.go`: the validate operation and
  its non-persisting scan. `checkNotForbidden` now returns a wire-safe detail,
  matching the `required_in` veto beside it.
- `internal/service/mcp_admission.go`: admits the two operations.
- `HIKYO_MCP_WRITE_ENABLED`: `config.go` (requires `HIKYO_MCP_ENABLED`),
  `variables.go`, `managed_owner.go`, `runtimeconfig/catalogue.go`, boot log,
  chart `mcp.writeEnabled` (+ helper refusal, CI check), docs
  (`configuration.mdx`, `mcp.mdx`), regenerated `variable-inventory.json`.
- `internal/app/generation.go`: registration-time exclusion; metrics label set
  is the full catalog regardless of the flag (`conformance/metrics_test.go`
  series pins raised accordingly).
- Scripts: `mcp-public-smoke` accepts read-only or read+write catalog;
  `mcp-production-client` never calls a write tool.
- Pins updated: `isolation/testdata/operation_formulas.json`,
  `service/budget_classification.go`.

Cancellation (decision 6): the transport propagates request cancellation into
the handler context (`TestCancellationReachesRegisteredOperation`), and
`tx.WriteResult` commits only a completed attempt while `retryLoop` honours a
cancelled context (`TestRetryLoopRespectsCancelledContext`), so a cancelled
stage never commits a partial draft. No new mid-transaction cancel test was
added; it would flake without proving more than those two.

Tests: `mcpserver/registry_test.go` (gate matrix), `write_tools_test.go`
(catalog, pinned rows, mapping, bounds, error policy),
`isolation/mcp_write_e2e_test.go` (real datastore, both engines, canary,
audit origin, denial), `config_test.go` (flag parse and refusal).

## Same-provider review (Standards + Spec, 2026-09-17)

Two parallel Claude sub-agents (code-review skill), explicitly NOT the
cross-provider gate. Both returned CHANGES; every finding folded in:

- Schema `maxLength` on `value` echoed the oversized proposal in the SDK's
  validation error (reproduced with a canary). Removed; the service byte
  budget bounds it and `TestSchemaRefusalsNeverEchoTheProposedValue` pins it.
- Cancellation now proven end to end: `cancellingStager` cancels the request
  context as the stage reaches the service; no draft and no `value.staged`
  row survive (`TestMCPWriteSurfaceEndToEnd`).
- Registry rows pin the real service pair (`Set/Unset`,
  `ValidateSet/ValidateUnset`); read tools declare `ToolClassRead` explicitly;
  shared `runChange` helper; `service.MaxRequestFindings` exported and reused;
  SafeDetail contract documented; serverInfo no longer says read-only.
- ADR § Corrections gained item 4 (wire-safe refusals cross the transport;
  § 3 "never the proposed material" holds for the trail, not the response).
- Fallout fixed: `MaxSelfConfigSeedInputBytes` raised to 32 MiB (the owner
  catalogue grew by one key); `TestAuditCore` now exercises the
  `value.change_validated` emitter.

## Not done

Owner wording calls: ADR § Corrections items 3 (secret entry) and 5 (a machine
credential with `publish` CAN publish over REST; the MCP boundary holds because
no publish tool is registered, not because machines cannot publish).

Cross-provider review of the ADR text (still the operative gate). Governance PR
[#766](https://github.com/Hikyo-Org/Hikyo/pull/766) stays draft until it
concludes SOUND; the implementation PR merges only after #766. The owner has
not yet answered the banner-wording question (ADR § Corrections item 3).
