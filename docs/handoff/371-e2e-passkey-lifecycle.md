# #371 — e2e passkey lifecycle ownership

## Outcome

Passkey-bearing Playwright flows now request `passkeyPage`. Its teardown always
persists the shared credential's advanced signature counter, then probes the
file-backed shared session and re-mints it only after a `401`.

## Decisions

- `web/e2e/fixtures/passkey.ts` owns passkey page setup, persistence, and shared-session repair.
- Setup and flow authenticators share one `VIRTUAL_AUTHENTICATOR` definition.
- A missing shared credential fails loud before the credential file can be overwritten.
- Unexpected session-probe statuses fail the suite; only `401` authorizes re-minting.
- Settings drill cleanup uses the same lifecycle owner so filtered runs do not leak resources.
- The scanning flow deletes its temporary key, keeping desktop and mobile projects isolated in one run.

## Generated outputs

None.

## Validation

```text
pnpm --dir web run typecheck
  passed

pnpm --dir web run test
  38 files passed; 315 tests passed

pnpm --dir web run e2e
  297 passed; 1 expected desktop skip; desktop and mobile projects

code-review
  Standards: CLEAN
  Spec: CLEAN
```
