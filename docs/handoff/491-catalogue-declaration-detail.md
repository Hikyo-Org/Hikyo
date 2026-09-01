# #491 Catalogue declaration detail + editor foundation — handoff

Status: FEATURE-COMPLETE for the ticket's scope. Web-only; no contract or
migration change (the `getKey` / `updateKeyMetadata` operations and their Zod
schemas already existed in the generated client — **nothing was regenerated**).

## What shipped

A routable, reload-safe declaration detail surface a key name opens onto, plus
the shared editor foundation later create/edit/lifecycle tickets extend.

- **Route** `key-detail` at `/orgs/:org/projects/:project/matrix/keys/:key`
  (`web/src/app/navigation.ts`). Addressed by the key's **immutable id**, never
  its mutable name, so a bookmark survives a rename. Rendered as
  `<Matrix keyDetailOpen />` inside `WorkspaceScope` (`web/src/app/App.tsx`),
  the same layering the `history` surface uses — the panel rides over the
  matrix, which supplies the loaded environment list for presence names.
- **`web/src/routes/KeyDeclarationDetail.tsx`** — inspects every declaration and
  organisation field (name, classification, folder, group, description,
  deprecation, value rules incl. `any_of`, presence with symbolic `all`, and
  recorded scan findings) and hosts the metadata editor. **No secret value is
  renderable** — the key record carries only declaration metadata.
- **Key-name click repointed** from history to this surface
  (`web/src/routes/Matrix.tsx`). Per-key revision history stays reachable one
  gesture deeper, from a link inside the detail.
- **Source-mode gating**: `db` shows the editor; `git` is read-only behind
  `GIT_DEFINITIONS_NOTICE` plus the last-applied provenance labels (display-only,
  never trusted) — `useDefinitionsSettings`.
- **One live write**: `useUpdateKeyMetadata` (folder + description) — the
  smallest complete journey. Carries `acknowledgements`, so it is the ingress a
  Surface-2 scanning block attaches to; a refused write renders the server's
  caller-safe detail (rule id + locator, never matched text) through the shared
  refusal path (`keyMetadataRefusalText`).
- **Recoverable states**: loading, 404/deleted (stale link → "Back to the
  matrix"), 403 refusal, and 409 reload guidance.

## Contract decision (per the ticket's delivery notes)

The key **name** click now opens the declaration detail (AC #1), superseding the
revision-history prototype's earlier "name opens history filtered to that key".
History-by-key is preserved as a link inside the detail, so no capability is
lost. `matrix.spec.ts` / `history.spec.ts` assertions that assumed name→history
were updated accordingly.

## Deliberately out of scope (later tickets)

- The Surface-2 **block dialog** itself (structured findings + ack tokens) —
  named as its own residual in `docs/handoff/74-secret-scanning.md`. The seam is
  here (findings render; refusal path surfaces the detail).
- Rules/presence editing, rename, delete, and deprecation **toggling**
  (deprecation is displayed, not yet editable). Reclassification already has its
  own ceremony (`useReclassifyKey`) and was not duplicated here.

## Registry / CI wiring

- Flow `key-detail` (`web/e2e/registry.ts`) rides **`flows/matrix.spec.ts`**, not
  a standalone spec. Why: the merge gate loads `ci.yml` from the base branch
  (`ci-control.yml` is `pull_request_target`), so the per-group spec lists a
  Playwright leg runs are the base branch's. A spec a PR *adds* to a group never
  executes on that PR, so its pinned claims would be forever unmet and
  `web-closure` would fail. A new surface's flow must therefore live in a spec
  file already in a group on `main`; the file's *content* comes from the PR
  checkout, so the surface's journey + pinned set run today. The key-detail
  tests are a self-contained `test.describe` appended to `matrix.spec.ts`
  (dedicated per-viewport config key, created/deleted via the fixture bearer).
- Prototype dev server (`web/prototype/mock-api.ts`) gained `GET`/`PATCH`
  `/keys/{id}` so the surface works under `--mode prototype`.

## Validation

- `pnpm --dir web typecheck` — clean.
- `pnpm --dir web test` — 406 passed (5 new in `KeyDeclarationDetail.test.tsx`).
- `pnpm --dir web build` — clean.
- Playwright `key-detail` flow (desktop + mobile), incl. the pinned assertion
  set on the new surface.
