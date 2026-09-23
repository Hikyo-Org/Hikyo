# Handoff: delivery-target condition reporting ADR (#683)

Issue key: [#683](https://github.com/Hikyo-Org/Hikyo/issues/683). ADR:
[docs/adr/k8s-condition-reporting.md](../adr/k8s-condition-reporting.md). This
doc lets a fresh context pick up without re-deriving.

## Status

Decision LOCKED 2026-09-23 via grilling; the owner chose the recommended
option on every question. Cross-provider review WAIVED by the owner on
2026-09-23 ("Waive" on the governance-review question). Operative upon the
governance PR merging. Per [oss-mechanics.md](../adr/oss-mechanics.md)
§ Governance, the ADR is amended only by reopening #683, running the same
adversarial cross-model review that locks decisions, and recording the
amendment in the ADR. That review was waived here, not satisfied.

No code changed. This design ticket authorizes no implementation; each
follow-up ticket lands under its own review.

## Locked decisions

| Question | Choice |
| --- | --- |
| D1 authority | new `report-delivery-status` atom, workload allowlist only |
| D2 layers | server-observed contact and controller-reported conditions, labelled, never merged |
| D3 labels | namespace and CR name shown, DNS-1123 validated |
| D4 key names | excluded from the payload |
| D5 ordering | generation monotonic, then `reported_at` monotonic within a generation |
| D7 readers | environment `read` |
| D9 default | `operator.statusReporting` on, capability probed |
| D6, D8, D10, D11 | no open alternative; locked with the above |
| Review | cross-provider review waived |

## Same-provider review fixes (2026-09-23)

Standards and Spec review (same provider, not a cross-provider pass) found and
this change fixed: a 404 disabling reporting instance-wide (now per-CR
suppression; the capability probe owns route existence), refusal audit volume
(bounded by suppression), a budget below the row quota (60/min, 5 min
heartbeat floor), deleted-principal contradiction (rows are deleted), a free
`reporter` string (closed enum plus SemVer), missing `refused` state (422/413 only; 409 ordering
is dropped, never shown) and 401 behavior, report timing after the status
write, the org budget ceiling (about 1500 reporting CRs), unstated CRD impact (no CRD change), operator-newer vocabulary
(skip report, never drop a condition), a threat-model misquote, and gaps in
the validation plan (revoked credential, same-generation and future-skew
ordering).

## What landed in this change

- The new ADR, with context drawn from source (operator conditions, fetch
  client, `identity.delivery_fetched`, `MachineAccess.tsx`).
- Amendment banners (pointers only) on permission-model, threat-model,
  k8s-integration, machine-identities, audit-model, ops-spec and
  api-cli-surface.
- Ops-catalogue value rows; ADR index row; the Kubernetes row in the UI audit
  README now links the ADR.

## Implementation tickets

| Ticket | Scope | Depends on |
| --- | --- | --- |
| [#788](https://github.com/Hikyo-Org/Hikyo/issues/788) | server: atom, table, report/list ops, budget, audit, `/meta` | none |
| [#789](https://github.com/Hikyo-Org/Hikyo/issues/789) | operator: probe, reporter, tombstone, Helm value | #788 contract |
| [#790](https://github.com/Hikyo-Org/Hikyo/issues/790) | web UI: Kubernetes tab states, setup-journey grant, remote mapping | #788 |
| [#791](https://github.com/Hikyo-Org/Hikyo/issues/791) | real controller-to-browser validation | #788, #789, #790 |

## Things a fresh context should not re-derive

- The operator never holds a Hikyo credential; reports use the CR's own
  credential. Operator-wide reporter credentials are rejected.
- Reporting sits after cursor persistence and can never alter conditions, the
  Secret or the cursor. No finalizer.
- The server stores only the closed vocabulary; any unknown `type`/`reason`
  refuses the whole report.
- `x-hikyo-formula` entries for the two new operations land with #788, not
  here, because the formula annotates the OpenAPI operation itself.
