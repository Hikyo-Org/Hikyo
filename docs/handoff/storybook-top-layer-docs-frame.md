# PR #752 — keep top-layer story overlays inside their Storybook docs frame

## Problem

Several Storybook autodocs pages opened as a popup/modal with no way to close
them, covering the documentation. Native `<dialog>.showModal()`, `popover`, and
`position: fixed` overlays render into the browser **top layer / viewport**, not
the story's flow. On an inline autodocs Docs page (global `tags: ['autodocs']`)
each such story painted its overlay over the whole document with no dismiss path.

## Fix

`web/.storybook/topLayerDocs.ts` — single shared parameter:

```ts
export const topLayerDocs = {
  docs: { story: { inline: false, height: '720px' } },
} satisfies Parameters;
```

`inline: false` renders the story in its own iframe, which re-roots the top
layer to that frame so the overlay stays boxed and the surrounding docs stay
readable. `720px` sizes the story viewport only — each modal caps at ~82vh of the
frame and scrolls internally, so its close control (top-anchored ✕) and footer
stay reachable. It is not a per-modal fit; raising the height would not remove
internal scroll, it would only enlarge the empty frame.

Applied on `meta.parameters` for: notifications, FolderCleanupDialog,
InviteDialog, MatrixKeyCreate, MatrixRowEditor (merged with existing `app`
param), ScanBlockDialog, ScanWarnDialog. `Sections` applies it at **story
level** on `Consequences` only — the sole story there that opens a dialog; the
inline primitives (Panel/Alert/Done/Explain/JumpIndex/…) stay inline.

`ui/Menu.stories.tsx` is intentionally untouched: its `popover="auto"` is closed
by default, so it never escapes.

## Verification

- Local `storybook build`; same-origin iframe sweep of all 8 pages → overlay
  inside a nested story iframe, zero shown dialogs / open popovers at the
  docs-document level.
- MatrixKeyCreate measured empirically: `dialog[open]` in the 720px frame,
  `overflow-y: auto`, ✕ visible top-right, Declare/Cancel reachable via internal
  dialog scroll. Screenshot: `web/storybook-static/after-matrixkeycreate.png`
  (gitignored build dir).
- `pnpm run typecheck` — pass.
- `pnpm run build-storybook` — pass.
- `pnpm run test-storybook` — 24 files / 76 tests pass (a11y at error level).

## Local-env traps (cost time this session)

- `build-storybook` fails with `Rolldown failed to resolve import "zod"` unless
  `clients/ts` is installed **first**: `pnpm --dir clients/ts install
  --frozen-lockfile`. `web` links `clients/ts`; CI installs it before web.
- `storybook dev` via the direct `node_modules/.bin/storybook` hits a
  `CriticalPresetLoadError` on this machine's pnpm-11 links store. Run via
  `pnpm run storybook` / `pnpm run build-storybook`, which resolve correctly.
- `test-storybook` (vitest browser mode) fails to fetch the `addon-vitest`
  setup file if `node_modules` is on a store outside the project (e.g. `/tmp`).
  Use the default pnpm store: `rm -rf node_modules && pnpm install
  --frozen-lockfile`.

## Post-merge

Eyeball `hikyo.app/storybook` for the 8 pages once the docs deploy runs.
