# Member access rules: owner decisions of 2026-11-20

## Result

The owner settled the four open points that blocked the remaining
`CAP-MEMBER-ACCESS-RULES` scope. `docs/adr/member-access-rules.md` records them
under "Owner decisions, 2026-11-20"; they change no D1 to D11 clause.

| Point | Decision | Ticket |
| --- | --- | --- |
| Key narrowing on all-projects rules | Folder selectors only; single-key items refused; project creation needs no notice | #1446 |
| Environment rename/create under name binding | Confirmation naming who gains through the name match, as for folder moves; audited. Inclusion in an *all*/*all, except* rule is intended reach | #1447 |
| Environment delete an *all, except* rule names (2026-11-27) | Fails closed: refused like a widening rename until the gainers are confirmed; audited. The rule keeps the name, so a later environment of it is excepted again | #1447 |
| Machine rules | Environment axis only, no key narrowing, inside the owning project and the class's scope depth (workload: explicit environments) | #1473 (new) |
| Server-side resolver | Additive `/api/v1` operation and CLI spelling, declared as an api-cli-surface amendment in the implementing PR | #1474 (new) |

The ledger's remaining text cites all four tickets.

## Evidence

- Machine rules are refused today by `ErrRuleMachine`
  (`internal/service/rules.go`), because the machine widening gate diffs reach
  per environment; without key narrowing that count stays exact.
- Machine grants are confined to the owning project (`machine-identities.md`)
  and to each class's scope depth (`machineDepths`, `checkMachineScope`), so
  machine rules inherit both bounds.
- Additive operations after the `/api/v1` freeze are admitted by declared
  amendment; the 2026-11-16 `rotation` amendment in `api-cli-surface.md` is the
  precedent.

## Remaining

- Set `ready-for-agent` on #1446, #1447, #1473 and #1474 once this record merges.
- Still open in the ADR and not decided here: whether folder selectors bind by
  path or by folder id.
- Each implementing PR needs independent Standards and Spec review with a
  security focus.
