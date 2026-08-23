# Issue #360 — resolve CLI leaf authentication rules at parse time

Issue: https://github.com/Hikyo-Org/Hikyo/issues/360 (parent #326; audit
finding `F-S19-1`). Pull request: https://github.com/Hikyo-Org/Hikyo/pull/412.
Implementation base: `21254750377527a7291eaf2c99a84fc07277bf66`.

## Contract

- `parseCommon` resolves the closed authentication-kind rule before flag
  parsing or client-state access and carries both operation and eligible kinds
  in `commonFlags`.
- Authenticated human/machine and compose-machine target resolution consume the
  carried kinds; neither can repeat or defer the registry lookup.
- The phase-one `import` parser, which intentionally cannot use the shared
  `--env` grammar, resolves and carries the same rule before its custom flags.
- Missing rules remain fail-closed. Existing authentication eligibility,
  command output, wire/audit bytes, ordering, and dialect behavior are
  unchanged.
- Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `TestEveryAuthOperationReachesItsRule` drives every authentication-table leaf
  through `Run`; authenticated leaves must reach an exact missing-rule sentinel
  before state or command validation, while local/refusal-only leaves must reach
  their declared dispatcher spelling.
- `TestParseCommonRefusesUnknownOperationBeforeStateRead` proves an unknown leaf
  returns `ExitInternal` without reading the state environment.
- Focused leaf/rule regression set: 163 passed.
- Scoped `go test -count=1 ./internal/cli`: 463 passed.
- Full `go test -count=1 ./...`: 3,707 passed in 61 packages.
- Standards/spec review: CLEAN in round 2/3 after leaf inventory and helper
  ownership findings were fixed.
- Exact-head CI results are recorded on PR #412.
