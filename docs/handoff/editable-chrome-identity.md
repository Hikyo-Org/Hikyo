# Handoff: editable chrome identity (org and project hue, content, image)

## Result

The Identity panels of Organisation settings and Project settings now edit and
persist the badge instead of showing "Monogram PO, hue 195" read-only. Scope is
org and project. Nothing is committed: the change sits in the working tree.

## Contract (additive, revision 13)

- `PUT /api/v1/orgs/{org}/identity` and the project twin take `{hue 0..359, mark string|null}` and return `Org` or `Project`.
- `PUT|DELETE|GET .../identity/image`: PUT takes a raw `image/*` body, DELETE returns the entity, GET returns the stored bytes with the exact validated Content-Type.
- Optional `identity {hue, mark, image_version}` on `Org`, `MyOrg`, `Project`. `image_version` is the lowercase hex SHA-256 of the bytes; the SPA appends it as `?v=` so a change busts the cache.
- `RenameRequest` and `metadata` are untouched.

## Rules

- Stored formats are PNG, JPEG, WebP. SVG is converted to PNG in the browser and the server refuses it (400).
- Image: 256 KiB body cap (per-route, enforced before buffering), magic bytes must match the declared type, at most 1024 x 1024, header decode only.
- Mark: non-empty, no control characters, no surrounding whitespace, at most 2 grapheme clusters (`rivo/uniseg`). Null or empty falls back to the name monogram.

## Authorization and audit

- Org writes: `manage-members@org`. Project writes: `manage-projects@org`. Refusal is the uniform 404/403.
- Project image read: `read@project`, audited none.
- Org image read has no formula: it follows `listMyOrgs` visibility (`resolveSelf` + `OrgsForPrincipal` projection, no machine credentials), so a project-scoped member sees it. Not listed and no image are the same 404.
- Events `settings.org_identity_changed` and `settings.project_identity_changed` record previous and new hue, mark (sanitised free text) and image version, read in the same transaction as the write. Never bytes.

## Storage

Migration 00083 (both dialects) adds `identity_hue` (default 195, 0..359 check), `identity_mark`, `identity_image`, `identity_image_type`, `identity_image_version` to `orgs` and `projects`. Get and list queries never select the bytes; a dedicated query reads them. Deleting the org or project deletes the row, and the image with it.

## SVG and CSP (decision with evidence)

`createImageBitmap(svgBlob)` fails in Chromium 151 and WebKit 26.5 ("The source image could not be decoded") even with `blob:` allowed. An `<img>` from a blob URL is blocked by `img-src 'self'` in both. With `img-src 'self' blob:` the blob `<img>` loads, draws to canvas and `toBlob('image/png')` succeeds in both, without taint. So the CSP baseline is now `img-src 'self' blob:` (`internal/server/spa.go`, the `web/vite.config.ts` meta CSP, asserted in `internal/server/spa_test.go`, recorded in `docs/adr/ops-spec.md`). `data:` stays out. Firefox was not installed and is untested.

## Web

- `web/src/routes/chrome-identity.ts`: adapter from the API identity; the image is a same-origin `url("...")` with `?v=`, path segments encoded, never `data:`. Remote workspace mode (`?remote=`) falls back to mark plus hue.
- `ChromeIdentityControls.tsx`: editable outside prototype mode when the page supplies actions; read-only under `ReadOnlyContext`. Preset hue saves at once, the slider previews live and saves on release, content saves on blur, failures revert to server state and surface through the page's `useFeedback`.
- `chrome-image.ts`: SVG validation (DOMParser, viewBox size), rasterisation and the client-side size refusals. `chrome-mark.ts`: the 2-grapheme limit via `Intl.Segmenter`.
- Raw upload goes through the generated operation, so the CSRF interceptor in `web/src/api/client.ts` applies.

## Remaining and follow-ups

- Two in-flight changes may also claim migration 00083; renumber on merge.
- Regenerate `internal/buildcompat/development.json` after any migration renumbering (needs a scratch Postgres).
- A rapid second edit sent while the first is saving is blocked by disabling the buttons; the slider is not disabled and the last write wins.
