# Handoff: #343 SCIM positional grammar

Issue: https://github.com/Hikyo-Org/Hikyo/issues/343 (parent #326; audit
finding `F-S20-2`). Pull request:
https://github.com/Hikyo-Org/Hikyo/pull/399. Implementation base:
`21267cccd4dd977fb23a6d9d7c8fe38934db1b05`.

## Contract

- SCIM binding, mapping, credential, user, and group commands parse all flags
  and positional arguments through `parseCommon` and its canonical
  interspersed grammar.
- `scimPositionals` owns SCIM positional arity. Missing identifiers retain the
  existing command-specific usage text; trailing identifiers retain the
  existing `takes no positional arguments` diagnostic.
- A literal `-` is a positional identifier. Flags may appear before, between,
  or after required identifiers.
- `account reset-credential` uses the same canonical parser and requires
  exactly one principal. Syntax failures occur before disclosure preparation,
  target resolution, or HTTP work.
- Wire, audit, output-format, ordering, database, and generated contracts are
  unchanged. Generated outputs: none.

## Regression evidence

- `TestSCIMPositionalGrammar` covers all four migrated SCIM command shapes,
  including the two-identifier credential path, literal `-`, missing and extra
  arguments, preserved diagnostics, and zero HTTP requests.
- `TestResetCredentialPositionalGrammar` covers a principal after flags and a
  refused trailing positional, with zero HTTP requests.
- The named SCIM regression failed against the branch-point implementation:
  `hikyo scim binding show -o json scb_1` returned `ExitUsage` before target
  resolution. It passes on the pull-request implementation.

## Validation

```text
go test -count=1 ./internal/cli -run
  'Test(SCIMPositionalGrammar|ResetCredentialPositionalGrammar|DisclosurePreparationFailureMakesNoRequest)$'
                                                         22 passed / 1 package
go test -count=1 ./...                              3,532 passed / 61 packages
gofmt -w internal/cli/scim.go internal/cli/verbs.go
  internal/cli/identities_test.go                             clean
git diff --check                                             clean
```

## Review

- Standards round 1 found missing positive coverage for the two-identifier
  credential path and repeated exact-arity logic. Both were fixed; round 2:
  `CLEAN`.
- Spec round 1 found missing/extra diagnostic drift and one remaining
  zero-arity callsite. Both were fixed and pinned by message assertions; round
  2: `CLEAN`.
