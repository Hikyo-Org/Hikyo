# #79 response enum compatibility follow-up

Base: `b7e8f3219ad232f929a2f7c8e650c0ff54a1affa` (#617 implementation). Branch `fix/one-zero-response-enums`; worktree `/tmp/hikyo-response-enums`. Codex authored; no commit or push. Parent owns review, exact-head CI, merge and release acceptance.

`api/openapi.yaml` now opens only `AdapterProvider` and `SamlProviderWarning.properties.code`, preserving known values through `x-extensible-enum`. Both generated Go and TypeScript/Zod artifacts were regenerated using canonical commands. `IssuerType` has source-level `x-enum-varnames` to keep its previous Go identifiers despite generator collision changes. Dynamic provider kind remains closed `postgres`; warning severity remains closed `warning | error`.

The web adapter label now displays an unknown provider name, instead of incorrectly naming it GitHub Actions. SAML diagnostics use the existing server-supplied message and severity. CLI doctor and inventory preserve unknown warning codes. Server adapter creation still rejects unsupported providers before credential/factory use; the CLI/web create selectors remain bounded to known supported providers.

Unknown fixture values `future-provider` and `future-warning` are hypothetical future response strings only. They are not new integrations, warnings emitted by the service, or invented wire fields. Tests cover runtime OpenAPI, generated Go round trip, actual Go HTTP decoder, generated TypeScript assignment and Zod parsing, actual web fetch/decode/render, and CLI severity behavior. Negative fixtures pin PostgreSQL-only dynamic kinds and warning/error severity. `TestSAMLProviderWarningsAreRequiredAndClosed` became `TestSAMLProviderWarningsAreRequiredAndCodesAreOpen`, preserving required warnings and exact known values while adding the closed severity assertion.

Validation passed:

- Full Go API, CLI, server and adapter packages; service unknown-provider/ceremony rejection fixtures. `GOMAXPROCS=2 go test -p 1` used; no PostgreSQL migration surface touched.
- `clients/ts`: `pnpm run verify`, canonical regeneration/typecheck/all 20 tests.
- `web`: typecheck, all 83 files / 670 tests, production build/precompression. Focused adapter/SAML tests 8/8 passed. Full-run Matrix tests emit localhost/act diagnostics while passing; new focused tests do not. Existing build chunk-size advisory remains.
- Dependency lockfiles unchanged; diff whitespace check passed. Neither touched package has ESLint configured.

Logs: `/tmp/hikyo-response-enums-{go,service,ts,web}.log`. Decision report: [response-enums.html](../reports/1.0/response-enums.html). Parent must bind final review and CI URLs to the exact candidate; no release acceptance, merge or tag is claimed here.
