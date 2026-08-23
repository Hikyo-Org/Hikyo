# Handoff: #372 browser API helper ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/372 (parent #326; audit finding
`F-S32-2`). Implementation base: `428dd6a5`.

## Contract

- `web/e2e/fixtures/api.ts` owns the only page-session API helper.
- Successful response bodies cross the caller-supplied Zod schema; `204` maps
  to `null`, matching existing `browserApi` behavior.
- Non-2xx responses throw `BrowserApiError` with typed `status` and byte-exact
  text `body`. Callers inspect `status`, never parse error-message text.
- The helper scopes CSRF-cookie lookup and requests to the primary e2e instance
  so the second-instance cookie jar cannot supply the wrong token.
- Fixture setup and repair use generated endpoint response schemas; the
  response-discarding `zFixtureIgnored` schema no longer exists.

## Coverage

- `api.test.ts` pins `BrowserApiError` construction and typed fields.
- `registry.test.ts` passes with all registered flows still closed.
- Web typecheck passes after installing both locked web and generated-client
  dependencies.
- Full web Vitest suite passes: 38 files, 311 tests.
- Generated files are unaffected.
