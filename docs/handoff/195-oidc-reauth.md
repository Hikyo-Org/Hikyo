# #195 — OIDC disclosure reauthentication

## Landed contract

- Browser-started OIDC transactions persist an explicit `browser` bit. Their
  callback returns `303 /auth/oidc/done` with rotated cookies on success and a
  closed wire error code on refusal; non-browser callers retain the JSON path.
  A short-lived HTTP-only `__Host-` marker preserves that redirect shape when
  callback admission returns 429, without querying transaction storage after
  the limiter has refused work.
- `whoami.session.assurance.provider` carries the configured slug only for an
  OIDC-backed session. The issuer remains private to the assurance method and
  the public provider listing exposes no new trust material.
- Login lists configured OIDC providers. Disclosure ceremonies and CLI
  handoffs offer `Re-authenticate with <provider>` only for an OIDC session and
  an effective window above zero. Passkey remains available beside it.
- Effective window zero remains passkey-only. The UI states that the identity
  provider cannot satisfy a per-disclosure gate, and the server still refuses
  an OIDC re-run for that environment.

## Browser return path

The SPA opens a blank popup synchronously from the click, severs its opener,
then navigates it after the start request returns. The callback lands on
`/auth/oidc/done`, which publishes the transaction result on
`hikyo-oidc:<state>`. The ceremony owns that channel before navigation, rejects
refusals and a ten-minute timeout, refreshes the rotated session, then resumes
the protected operation. A blocked popup falls back to same-tab navigation and
the done page returns to the remembered location.
Identity linking uses the same done page; a refused link remains visible there
with an explicit return to account security instead of disappearing in a
same-tab redirect.

Playwright starts `internal/oidctest/cmd`, the release-excluded wrapper around
the existing fake provider. Global setup configures it with an assurance
policy, links the fixture administrator through its real authorization flow,
and the reveal flow proves popup, callback, done-page, channel, and disclosure
end to end.

## Storage and generated surfaces

Migration `00033_oidc_browser_callback.sql` adds the non-null browser flag on
SQLite and Postgres. SQLC, strict-server OpenAPI, TypeScript client, and Zod
outputs are regenerated from their checked-in sources.

## Explicit non-goals

OIDC at an effective zero window would amend the locked human-auth ADR and is
not implemented. SAML reauthentication parity remains separate work; this
ticket adds no SAML browser ceremony.
