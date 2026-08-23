# Handoff: #333 workspace handoff transaction contract

Issue: https://github.com/Hikyo-Org/Hikyo/issues/333 (parent #326; audit finding
`F-S26-1`). Final reviewed base: `6106485a02b07cf6e03050075be0217bd0268ad7`.

## Contract

- `WorkspaceHandoffTransaction` is a closed union of establishment and step-up
  shapes. A step-up requires `operation` and `environment`; an establishment
  cannot carry either field.
- The server maps the service purpose through generated `FromX` union methods.
  Unknown purposes, incomplete step-up bindings, and contradictory
  establishment bindings fail with an internal error before serialization.
- The approval URL carries only opaque state. After authentication, the page
  loads the server-owned transaction and derives its prompt and ceremony from
  the parsed `purpose`; no URL purpose or synthetic operation/environment
  default can select a security ceremony.
- Establishment reads expose only purpose and expiry to an authenticated human
  holding the opaque state. Step-up reads retain bound-session ownership before
  exposing operation, environment, or key ids.

## Coverage

- The OpenAPI pin rejects both missing step-up fields and establishment fields
  that belong only to step-up. The HTTP contract test validates the real
  step-up response and proves unknown service purposes return `500`.
- The SQLite and PostgreSQL isolation cases cover the real service producer:
  both transaction variants are readable, while step-up ownership and uniform
  invalid-state refusals remain intact.
- Generated outputs: `api/apigen/apigen.gen.go` and
  `clients/ts/src/generated/{index.ts,sdk.gen.ts,types.gen.ts,zod.gen.ts}`.
  Both owning generators were run; the TypeScript client verify gate passed
  13/13 on the combined #333/#334 contract.
- Final exact-head validation: API and server focused tests passed; web
  TypeScript passed; web Vitest passed 310/310; generated client tests passed
  13/13; full Go passed 3,476 tests across 61 packages. The focused Playwright
  regression passed all four Authorize-workspace combinations across desktop,
  mobile, dark, and light. Two-axis review found the redundant URL-purpose
  owner, this missing handoff, and two stale/incomplete documentation details;
  every finding was fixed. Spec review returned CLEAN; the final standards
  comment was fixed at the three-round cap and rechecked by repository search
  plus the final gates above.
