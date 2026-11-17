# Bring-your-own user directory (OIDC providers and lifecycle events)

Branch `dunky13/bring-your-own-auth-provider`, based on `dev`. The pull request
description records the owner decisions and the validation evidence.

## Result

- Sign-in through any OpenID Connect issuer already worked through **Instance
  settings → Identity providers**. This change adds no new sign-in mechanism.
- New: **directory lifecycle events**. An OIDC provider row may carry a
  `lifecycle_profile` (closed enum, v1 `clerk`) and a sealed, write-only signing
  secret. `POST /api/v1/auth/oidc/{provider}/lifecycle` verifies Standard
  Webhooks signatures (Clerk's `svix-*` headers are mapped) and acts on:
  - `user.deleted`: unlinks the identity, ends the sessions that came through
    that issuer, and revokes developer credentials minted from those sessions or
    through that issuer's provider rows. The account, its grants and its other
    credentials stay.
  - `user.updated` with `banned: true`: the same sweep and revocation, and the
    link stays.
  - `locked` and every other event: ignored, answered `204`.
- The ADR amendment is the 2026-11-17 banner at the top of
  [human-auth.md](../adr/human-auth.md).
- New docs page [Other OIDC providers](../site/src/content/docs/docs/oidc-providers.mdx)
  has step-by-step guides for Clerk, Auth0, Okta, Amazon Cognito, Keycloak,
  authentik, ZITADEL and Authelia, plus troubleshooting.
- The OIDC, SAML provider and SAML SP key panels now show CLI hints that exist.

## Owner decisions (2026-11-17)

1. Clerk deletion unlinks and ends sessions. It does not remove grants or
   disable the account.
2. A Clerk ban ends sessions and keeps the link. `locked` is ignored, because
   an outsider can trigger it with failed sign-ins.
3. Signatures are verified by the Standard Webhooks reference Go library, not
   the Svix SDK and not a hand-rolled check.

## Known gaps

- No live Clerk instance was used. The Svix signature mapping is proven with
  Svix's published test vector, and the event payloads in the tests are
  synthetic. Before relying on it, send a test `user.deleted` and a ban from a
  Clerk development instance and confirm the `204` and the audit events.
- The provider guides rest on primary-source research, not on live tenants.
  Values the research could not confirm are phrased as "check a decoded test
  token". Confirm each provider against a real tenant as users report back.
- The retryable `503` for a provider changed between verification and the write
  is covered by the handler mapping test only. A deterministic race test needs
  a production hook that does not exist.
- The ADR's local-credential floor for instance-capability holders is not
  enforced in code. An administrator who signs in only through Clerk is
  recovered through `credential-reset` or break-glass after a Clerk deletion.
- A restore can bring back a link that a deletion event removed. The operator
  must revalidate the directory against Hikyo after a restore or an outage.
- Lifecycle profiles for other directories (for example Auth0 or WorkOS
  webhooks) are not built. Okta and authentik push SCIM, and Auth0 can, through
  its Outbound SCIM event stream. The guides point there.

## Next steps

1. Merge after CI and review. Merge needs the owner's approval.
2. On a Clerk development instance, follow the Clerk section of the new docs
   page end to end, including the lifecycle steps, and record the result on the
   pull request.
