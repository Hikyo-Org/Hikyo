# Handoff: #334 TOTP reauthentication request union

Issue: https://github.com/Hikyo-Org/Hikyo/issues/334 (parent #326; audit
finding `F-S26-2`).

## Contract

- `TotpReauthRequest` is the closed union of
  `TotpEnvironmentReauthRequest` and `TotpAdapterReauthRequest`.
- Environment reauthentication requires exactly `environment_id` and `code`.
- Adapter reauthentication requires exactly adapter `purpose`, `operation`,
  non-empty unique `environment_ids`, and `code`.
- `ReauthPurpose` remains the shared purpose owner; the adapter branch narrows
  it with `const: adapter`.
- Go consumers use generated `AsX`/`FromX` union methods. Existing web request
  literals and wire bytes are unchanged.
- Generated Zod branches are strict because Zod otherwise strips unknown keys
  before evaluating the union, which would admit mixed-variant payloads.

Generated outputs: `api/apigen/apigen.gen.go` and
`clients/ts/src/generated/{index.ts,types.gen.ts,zod.gen.ts}`.

## Validation

```text
go test -count=1 ./api ./internal/server ./internal/cli            passed
go test -count=1 ./internal/isolation -run 'Reauth|RevealCeremony' passed
go test -count=1 ./...                         3,468 tests passed
go vet ./...                                                        passed
pnpm --dir clients/ts run typecheck                                 passed
pnpm --dir clients/ts run test                             13 tests passed
pnpm --dir web run typecheck                                        passed
pnpm --dir web run test                                   308 tests passed
playwright reveal zero-window TOTP flow                    2 tests passed
```

The browser flow passed for desktop and mobile against real UI-tagged server
instances on isolated ports. The first attempt never reached a test because a
separate flow run already owned the default secondary port.

## Review

Standards review returned `CLEAN`. Spec review found one high-severity source
ownership issue in the initial inline adapter-only purpose enum. Reusing
`ReauthPurpose` and narrowing it with `const: adapter` resolved that finding;
round 2 returned `SOUND` with no critical new regressions.
