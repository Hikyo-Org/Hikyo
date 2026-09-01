# #500 — Administer SAML providers and SP keys in the WebUI

Parent: instance-administration parity (#25). **WebUI-only.** The backend
(`internal/server/saml.go`, `internal/service/saml_providers.go`,
`internal/service/saml_sp_keys.go`) and the generated TS operations already
existed on `main`; this change adds no API, no codegen, no server code, and no
new route.

## Contract decisions (no server change)

The locked contract is preserved verbatim. Two points the delivery notes ask to
state explicitly:

- **Endpoint-to-action mapping.** Each lifecycle act has exactly one surface,
  following the contract's own intent so nothing is UI-special:
  - **create** → `PUT /saml-providers/{slug}` (new slug).
  - **replace metadata** → `POST /saml-providers/{slug}/refresh-metadata`
    (file-backed supplies the replacement document; URL-backed re-fetches). PUT
    is used for create only — refresh-metadata is the contract's own
    reconfigure-metadata path, so there is no ambiguous second way to change
    trust material.
  - **policy / disable** → `PATCH /saml-providers/{slug}` (never demands
    unreadable metadata, exactly why the endpoint exists).
  - **delete** → `DELETE /saml-providers/{slug}`.
- **The delete "impact preview" is ceremony copy, not a computed preview.** No
  impact-preview API exists (matches OIDC delete). The removal ceremony states
  the consequence in the product's words — every session through the provider is
  swept, the provider stops advertising for sign-in, linked identities stop
  resolving — and it names the invariant that local-floor, locally-authenticated
  access is unaffected. Read this before flagging "preview" as unimplemented.

## Write-only discipline

`SamlProvider` never returns `metadata_document`, so metadata XML is only ever an
input. The create form clears the textarea on a successful apply; no mutation
echoes the document into feedback, a toast, or the URL. The e2e asserts the
editor is gone (with its textarea) after a successful create.

## Metadata diff-and-confirm ceremony

Modelled on `CredentialPolicyPanel`'s preview machine. A first PUT/refresh with
no `confirmed_*` returns `applied=false` + a `diff`; the panel holds the diff and
the `required_fingerprints`/`required_endpoints`, and the confirm button reruns
the same request with those copied into `confirmed_*`. One state machine serves
both create (PUT) and refresh-metadata.

## SP-key retirement: two distinct ceremonies

- **Ordinary retire** is offered ONLY on a `retiring` key (the server 409s the
  active key; `samlFailureText` surfaces that as "rotate first"). Typed-name
  gate on the fingerprint; copy: the cert leaves SP metadata now.
- **Compromise-retire** is offered ONLY on the `active` key. Typed-name gate;
  copy: no overlap window, immediate erase + replacement.

`retired` is not a listed state — retirement erases the key, so it leaves the
inventory; the copy explains that rather than drawing a dead row.

## Files

- `web/src/api/samlProviders.ts` — query/mutation hooks, client-side validation
  (slug pattern is the exact `ProviderSlugPath` one), and the per-action failure
  mapper. `web/src/api/samlProviders.test.ts` covers validation + failure text.
- `web/src/routes/SamlProvidersPanel.tsx`, `web/src/routes/SamlSpKeysPanel.tsx` —
  the two panels, mounted (prototype-gated, like `instance-grants`) into
  `web/src/routes/InstanceAdmin.tsx` with JumpIndex entries. No new registry
  surface → no CI-grouping trap.
- `web/src/routes/SamlAdmin.test.tsx` — component tests for the ceremony state
  machine and the state-gated retirement buttons.
- `web/e2e/flows/instance-admin.spec.ts` — the end-to-end journey (create via
  ceremony → refresh metadata → login-availability probe → rotate → retire →
  compromise-retire → delete → availability gone), plus the two new panels added
  to the password-only second-factor refusal array.

## Login availability

SAML login stays a protocol-specific path (out of scope; `Login.tsx` renders
OIDC buttons only). The e2e's availability check queries the public
`/api/v1/auth/methods` from a fresh unauthenticated context — the same endpoint
`AuthMethods` builds, which advertises enabled SAML providers by slug. Enabling
makes the slug appear; deleting makes it vanish.

## Validation

- `pnpm --dir web typecheck` — clean.
- `pnpm --dir web test` — 70 files, all green (adds `samlProviders.test.ts` +
  `SamlAdmin.test.tsx`).
- `pnpm --dir web build` — clean.
- Targeted Playwright: the new instance-admin journey, desktop + mobile
  (separate invocations — one shared backend, per the e2e serial-instance rule).

## Fresh-worktree gotcha

`clients/ts` needs its own `pnpm install --frozen-lockfile` before the web
typecheck resolves `zod` from the generated sources; otherwise every `parsed()`
return is `any` repo-wide (TS2307-adjacent). CI does this in a dedicated step.
