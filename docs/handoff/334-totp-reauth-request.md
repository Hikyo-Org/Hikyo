# Handoff: #334 canonical TOTP reauthentication requests

Issue: https://github.com/Hikyo-Org/Hikyo/issues/334 (parent #326; audit
finding `F-S26-2`). Integrated base:
`ac8a2b6160ebb853db740d5b40599ba674135bed`.

## Contract

- `TotpReauthRequest` is now exactly one of two closed wire variants:
  `TotpEnvironmentReauthRequest` carries `environment_id` plus `code`, while
  `TotpAdapterReauthRequest` carries the literal `adapter` purpose, one adapter
  operation, a non-empty unique environment set, and `code`.
- Code-only, mixed environment/adapter, and incomplete adapter bodies are
  refused by OpenAPI admission before service dispatch. Both existing valid
  JSON bodies remain byte-for-byte compatible.
- The server unwraps the generated union before constructing the existing
  service intent. The CLI constructs the environment variant through the
  generated `FromTotpEnvironmentReauthRequest` method. Browser request literals
  are unchanged and typecheck against the generated union.
- `ReauthPurpose`, service intent validation, wire response bytes, audit data,
  ordering, and database state are unchanged. Database migrations: none.

## Generated outputs

- `api/apigen/apigen.gen.go`, owned by `api/openapi.yaml` through
  `api/oapi-codegen.yaml`.
- `clients/ts/src/generated/{index.ts,types.gen.ts,zod.gen.ts}`, owned by the
  same OpenAPI source through `clients/ts/openapi-ts.config.ts`.
- `x-hikyo-zod-strict` marks only the two XOR variants for strict-object Zod
  generation, because the pinned generator otherwise strips unknown fields
  before evaluating the union.

## Regression evidence

- The new request-validation table was red for code-only, mixed, and incomplete
  adapter bodies before the schema change; all five invalid/valid cases now
  pass.
- Existing server boundary coverage still refuses mixed TOTP intent variants.
- Existing CLI coverage confirms the canonical environment request opens the
  window and persists the rotated bearer.
- Reveal browser flows pass on both desktop and mobile projects, including the
  valid TOTP request and protected-environment refusal.

## Validation

```text
go test -count=1 ./api ./internal/server ./internal/cli     617 passed
go test -count=1 ./internal/isolation -run 'Reauth|reveal|Reveal'
                                                            27 passed
go test -count=1 ./...                                     3469 passed / 61 packages
go vet ./...                                               passed
pnpm --dir clients/ts run verify                            13 passed
pnpm --dir web run typecheck                               passed
pnpm --dir web run test                                    310 passed
playwright test e2e/flows/reveal.spec.ts --project=desktop  16 passed
playwright test e2e/flows/reveal.spec.ts --project=mobile   16 passed
gofmt -l <changed Go files>                                clean
git diff --check                                           clean
```

## Review

- Standards round 1 found one discarded generated-constructor error. The CLI
  now wraps and returns it; round 2: `CLEAN`.
- Spec round 1 found that non-strict generated Zod objects still accepted a
  mixed variant. Source-owned strict generation plus the client regression
  fixed it; round 2: `CLEAN`.
