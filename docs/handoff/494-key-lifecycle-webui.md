# #494 Browser key lifecycle — rename, reclassify, delete — handoff

Status: FEATURE-COMPLETE for the ticket's scope. Web-only; no contract or
migration change. The `renameKey`, `reclassifyKey` and `deleteKey` operations
and their Zod already existed in the generated client — **nothing was
regenerated**. Blocked-by #491 (PR #535, detail + editor foundation) and #183
(PR #541, scan block dialog) are both merged; this extends that foundation.

## What shipped

The three remaining lifecycle actions on the catalogue declaration detail
(`web/src/routes/KeyDeclarationDetail.tsx`), each rendered only in the editable
(`definitions_source === 'db'`) branch beside the existing metadata editor:

- **Rename** (`RenameKey`, `useRenameKey`): identity is the immutable id, so no
  reference breaks; the name is Git-exported/public, so the same Surface-2
  scanning block the metadata editor uses attaches here (redacted findings +
  audited override with the same-content resubmit). Collisions and any other
  refusal surface inline via `keyLifecycleRefusalText`.
- **Reclassify** (`ReclassifyKey`, broadened `useReclassifyKey`): offers only the
  opposite direction (the server 400s a same-classification no-op anyway) behind
  a native-`<dialog>` confirm rendering that direction's distinct consequences.
  - Tightening (`config` → `secret`): re-secures occurrences, drops config
    scanning dismissals. No reveal.
  - Declassifying (`secret` → `config`): a disclosure. The server enforces it
    (CapReveal @ project + MFA session assurance; there is **no per-request
    reveal token** and no env reveal window — the CLI does nothing client-side
    either). The UI states the disclosure + second-factor requirement, attempts
    the ceremony, and maps refusals: **403 → reauthenticate**, **404 → the
    uniform missing-key sentence**. The 404 branch is deliberately identical to
    every other 404 because a distinguishable message would turn the reveal gate
    into the existence/permission oracle it exists to close. On success the
    response's Surface-1 warnings for re-materialised occurrences render redacted
    (rule id + locator).
- **Delete** (`DeleteKey`, `useDeleteKey`): an impact preview of the affected
  environments (delivered value / unpublished draft) plus the shared
  `<TypedNameConfirm>` danger-zone gate. On success it navigates back to the
  matrix (no stale key route). **No value is ever shown.**

### Impact preview

Assembled in `Matrix.tsx` (`keyDetailImpact`/`keyDetailImpactReady`) from the
cells the matrix already holds, via the pure `assembleKeyImpact`. Only
value-free environment-id lists cross into the detail surface — never a cell (a
config `ValueCell` can carry material). Destructive actions stay disabled until
the preview is ready (fail-closed), so a preview never understates its blast
radius.

## Load-bearing gotcha: delete invalidation must be `exact`

`matrixKeyKey(ref, key) === [...matrixKeysKey(ref), 'key', key]` — the single-key
query key is the list key plus a suffix. A **non-exact** `invalidateQueries` on
`matrixKeysKey` therefore ALSO matches the still-mounted single-key query and
re-fetches the just-deleted key: a guaranteed 404 that rejects the mutation's
`onSuccess` promise and, with it, the caller's navigate-to-matrix — the surface
gets stranded on the deleted-key error state. `useDeleteKey` invalidates the
list with `{ exact: true }` and never touches `matrixKeyKey`; the stale
single-key cache is dropped on unmount. Regression test:
`web/src/api/matrix-hook.test.tsx` ("refreshes the list without re-fetching the
deleted single key"). Rename/reclassify keep the detail open, so their non-exact
list invalidation cascading into the single key is harmless (returns 200).

## Registry / CI wiring

- E2E rides `web/e2e/flows/matrix.spec.ts` (describe "catalogue declaration
  lifecycle"), reusing the `key-detail` surface — a new spec file would never run
  on its own PR (the merge gate loads `ci.yml` group spec lists from the base
  branch; see #491 handoff and `docs/handoff/491-catalogue-declaration-detail.md`).
  The journey creates a throwaway config key, renames it, tightens it to secret,
  then deletes it. Declassification's reveal gate and Surface-1 warnings are
  covered by the component tests, which drive the refusal shapes deterministically.
- Prototype dev server (`web/prototype/mock-api.ts`) gained `PUT /keys/{id}/name`,
  `PUT /keys/{id}/classification`, and `DELETE /keys/{id}`.

## Verification

- `pnpm --dir web typecheck` — clean.
- `pnpm --dir web test` — 469 passed (new: rename, rename-scan-block,
  declassify+warnings, declassify-403-reauth, declassify-404-mask, tighten,
  delete+typed-confirm+nav, delete-fail-closed, and the delete-invalidation hook
  guard).
- `pnpm --dir web build` — clean.
- Empirical (prototype + Playwright): rename and delete verified end-to-end
  against the mock; the reclassify confirm dialog is exercised by vitest (the dev
  prototype's `StrictMode` double-invokes the layout effect and leaves any
  conditionally-mounted native `<dialog>` closed, affecting every dialog, not a
  bug in this change; production e2e has no `StrictMode`).

## Deliberately out of scope (later tickets, per #491 handoff)

- Rules/presence editing (`updateKeyDeclaration`) and deprecation toggling.
- `ScanWarnDialog`'s one-click reclassify-as-secret on the matrix is untouched —
  it already uses the dedicated ceremony endpoint.
