# Handoff: #364 typed SCIM PATCH payloads

Issue: https://github.com/Hikyo-Org/Hikyo/issues/364 (parent #326; audit ID
`F-S24-1`).

## Contract

- `scimproto.ParsePatch` returns one sealed, typed payload variant per accepted
  matrix cell; `ParsedPatch` never exposes undecoded operation bytes.
- Operation values are decoded into typed payloads by `scimproto`; the server
  consumes them without re-marshalling, re-decoding, or owning PATCH value
  decode helpers.
- Direct and pathless Group member payloads run `CheckMembers` before
  `ParsePatch` succeeds. Nested groups, empty references, and bounded-reference
  refusals retain their existing `invalidValue` codes.
- Matrix cells, operation order, atomic conversion, active normalization, and
  service command semantics are unchanged.

## Regression evidence

`TestPatchMembersAreValidatedByParser` pins direct and pathless member
refusals. `TestPatchReturnsTypedPayloadPerKind` covers all five payload kinds,
including value-free removals. `FuzzParsePatch` now asserts that every
successful operation has a non-nil typed payload.

Generated outputs: none.

## Validation

```text
go test -count=1 ./internal/scimproto ./internal/server ./internal/service
                                                     566 passed
go vet ./internal/scimproto ./internal/server ./internal/service
                                                     passed
git diff --check                                   passed
```

Two-axis code review round 2: clean. SCIM isolation, bounded fuzz, and full
suite remain to be rerun on the committed head when host memory pressure
subsides.
