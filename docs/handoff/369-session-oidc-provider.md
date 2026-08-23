# Issue #369 — session OIDC provider owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/369 (parent #326; audit finding
`F-S30-2`). Implementation base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `useSessionOIDCProvider` is the single owner of deriving a configured OIDC
  provider from the current session assurance and authentication-method query.
- A non-OIDC session, pending method data, missing provider slug, removed
  provider, or non-OIDC provider with the same slug resolves to `null`.
- `Ceremony` and `CLIReauth` offer OIDC reauthentication only for the resolved
  provider. Their passkey paths remain available when resolution returns
  `null`.
- Zero-window OIDC sessions retain the existing explanation that their identity
  provider cannot satisfy a per-disclosure gate.

Generated outputs: none. Database migrations: none. Wire and audit contracts:
unchanged.

## Regression evidence

Before implementation, the new hook test failed with
`TypeError: useSessionOIDCProvider is not a function`. Both consumers then
failed against the migrated hook boundary until their duplicated derivations
were removed.

## Validation

```text
pnpm --dir web exec vitest run \
  src/api/account.session-oidc-provider.test.tsx \
  src/routes/Ceremony.task-key.test.tsx \
  src/routes/CLIReauth.oidc.test.tsx                         8 passed
pnpm --dir web run typecheck                                passed
pnpm --dir web run test                       313 passed / 38 files
pnpm --dir web run build                                    passed
git diff --check                                            passed
```

## Review

- Standards axis: one unnecessary test-only assertion removed; no remaining
  documented violation or baseline smell.
- Specification axis: `CLEAN`; no missing requirement, scope creep, or
  incorrect behavior.
