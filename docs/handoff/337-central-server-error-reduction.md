# Handoff: #337 central server error reduction

Issue: https://github.com/Hikyo-Org/Hikyo/issues/337 (parent #326; audit finding
`F-S14-1`, including `F-S13-1`). Implementation base:
`3b342e5a657f8a9d75cfe5d973a89eb23d28f0a2`.

## Contract

- Service errors from the migrated TOTP, recovery, CLI reauthentication,
  WebAuthn, OIDC, and SAML handlers return to the strict-server response error
  hook. `writeHandlerError` is their sole error-to-wire reducer and sole fault
  logger.
- Intentional operation-specific behavior remains local: reset uniformity;
  SAML/OIDC pre-auth and callback handling; WebAuthn login and precondition
  collapse; reauth-TOTP's not-found-to-unauthenticated rule; and workspace
  handoff lookup policy.
- Password-policy sentinels carry caller-safe `password` detail, preserving the
  credential-establishment response bytes without handler classification.
- OIDC provider update races are central conflicts. The `putOidcProvider`
  contract now declares `409`; no database migration is required.

## Coverage

- HTTP regression: a machine credential whose account lookup is not found gets
  contract-valid `404` from `listPasskeys`, with no fault log.
- Central policy tests validate migrated TOTP/recovery and OIDC sentinel
  responses against OpenAPI, pin empty TOTP detail, compare SAML conflict bytes,
  and preserve password detail.
- Generated outputs: `api/apigen/apigen.gen.go` and
  `clients/ts/src/generated/types.gen.ts`, regenerated from
  `api/openapi.yaml` through their owning generators.
- Local validation: focused server/service/API tests passed; web TypeScript and
  310 Vitest tests passed; generated client verification passed 13/13; `go vet
  ./...` passed; full Go passed 3,501 tests across 61 packages.
