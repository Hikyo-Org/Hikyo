# Handoff: editable chrome identity (org and project hue, content, image)

## Result

The Identity panels of Organisation settings and Project settings now edit and
persist the badge instead of showing "Monogram PO, hue 195" read-only. Scope is
org and project.

## Contract (additive, revision 14)

- `PUT /api/v1/orgs/{org}/identity` and the project twin take `{hue 0..359, mark string|null}` and return `Org` or `Project`.
- `PUT|DELETE|GET .../identity/image`: PUT takes a raw `image/*` body, DELETE returns the entity, GET returns the stored bytes with the exact validated Content-Type.
- Optional `identity {hue, mark, image_version}` on `Org`, `MyOrg`, `Project`. `image_version` is the lowercase hex SHA-256 of the bytes. The SPA appends it as `?v=` so a new image changes the style's URL and redraws; `/api` responses stay `no-store`, so images are refetched on remount.
- `RenameRequest` and `metadata` are untouched.

## Rules

- Stored formats are PNG, JPEG, WebP. SVG is converted to PNG in the browser and the server refuses it (400).
- Image: 256 KiB body cap (per-route, enforced before buffering), magic bytes must match the declared type, at most 1024 x 1024, then a full decode, because a CSS background gives no load-error signal. The image is inspected only after authorization, and a JPEG may have at most 100 scans, because decode cost grows with scans rather than bytes or pixels.
- Mark: no control or invisible format characters except the emoji joiners (ZWJ, tag characters), at least one visible character, no surrounding whitespace, at most 2 grapheme clusters (`rivo/uniseg`). Null or empty falls back to the name monogram.

## Authorization and audit

- Org writes: `manage-members@org`. Project writes: `manage-projects@org`. Refusal is the uniform 404/403.
- Project image read: `read@project` plus project navigation, so a member reaching the project only through one environment still sees its tile. Audited none.
- Org image read has no formula: it follows `listMyOrgs` visibility (`resolveSelf` + `OrgsForPrincipal` projection, no machine credentials), so a project-scoped member sees it. A caller outside that projection, such as an instance operator on the org's settings page, needs `org.get`. Otherwise, and with no image, the same 404. Declared as an amendment in `docs/adr/tenant-isolation.md`, pending owner confirmation.
- Events `settings.org_identity_changed` and `settings.project_identity_changed` record previous and new hue, mark (sanitised free text) and image version, read in the same transaction as the write. Never bytes.

## Storage

Migration 00083 (both dialects) adds `identity_hue` (default 195, 0..359 check), `identity_mark`, `identity_image`, `identity_image_type`, `identity_image_version` to `orgs` and `projects`. Get and list queries never select the bytes; a dedicated query reads them. Deleting the org or project deletes the row, and the image with it.

## SVG and CSP (decision with evidence)

`createImageBitmap(svgBlob)` fails in Chromium 151 and WebKit 26.5 ("The source image could not be decoded") even with `blob:` allowed. An `<img>` from a blob URL is blocked by `img-src 'self'` in both. With `img-src 'self' blob:` the blob `<img>` loads, draws to canvas and `toBlob('image/png')` succeeds in both, without taint. So the server CSP baseline is now `img-src 'self' blob:` (`internal/server/spa.go`, asserted in `internal/server/spa_test.go`, recorded in `docs/adr/ops-spec.md`). The docs-site demo keeps `img-src 'self'` because it never uploads. `data:` stays out. Firefox was not installed and is untested.

## Web

- `web/src/routes/chrome-identity.ts`: adapter from the API identity; the image is a same-origin `url("...")` with `?v=`, path segments encoded, never `data:`. In remote workspace mode (`?remote=`) a project falls back to mark plus hue; the rail's orgs are this server's, so their images still load.
- `ChromeIdentityControls.tsx`: editable outside prototype mode when the page supplies actions; read-only under `ReadOnlyContext`. Preset hue saves at once, the slider previews live and saves on its native `change`, content saves on blur. Saves are serialised, latest wins, and each carries both fields, so a slow response cannot restore a replaced value. A draft clears once the server holds it. A failed save reverts only what it carried and surfaces through the page's `useFeedback`. The pages key the controls per entity and the identity mutations take their target in their variables, so a queued save cannot follow an org switch.
- `chrome-image.ts`: SVG validation (DOMParser, viewBox size), rasterisation and the client-side size refusals. `chrome-mark.ts`: the 2-grapheme limit via `Intl.Segmenter`.
- Raw upload goes through the generated operation, so the CSRF interceptor in `web/src/api/client.ts` applies.

## Remaining and follow-ups

- Migration 00083 is the next free number on `dev`; renumber it, and regenerate `internal/buildcompat/development.json`, if another change claims it first.
- Owner decision: a sole org's badge appears only in settings, because the rail and mobile switcher need at least two orgs.
- Firefox SVG conversion is untested.
- `Intl.Segmenter` and `rivo/uniseg` v0.4.7 count some Indic conjuncts differently: the input can accept a mark the server refuses with a visible error, after which it reverts.
- Maintainer security review: the `api/settings.ts` inventory pin and the `blob:` CSP change.
