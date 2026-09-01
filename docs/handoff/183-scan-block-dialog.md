# Handoff — #183 Surface-2 scan block dialog in the SPA

Closes #183 (parent #74, secret scanning; the deferred browser residual from
PR #182). Delivers the Surface-2 blocking presentation on the catalogue
declaration editor introduced by #491.

## What shipped

- **Block dialog mounted on the declaration editor.** `MetadataEditor`
  (`web/src/routes/KeyDeclarationDetail.tsx`) now routes a scanner-refused
  metadata write — a credential-shaped `description` or `folder` — to the
  existing `ScanBlockDialog` instead of an inline alert. The dialog states the
  exported-as-public consequence, lists only each finding's rule id + locator,
  and offers one audited override that resubmits the **same** content with every
  finding's content-bound acknowledgement token. The operator's typed value is
  never passed to the dialog.
- **Named refusal surfaced.** `ScanBlockDialog` (`web/src/routes/ScanBlockDialog.tsx`)
  now shows the server's own caller-safe `detail` verbatim when an override is
  rejected (stale / version-skew / surplus / expired, named by the server),
  falling back to a generic line only when the refusal carried no safe detail.
  This improves the Matrix key-create/value-write block path (#492) too — the
  component is shared.
- **Criteria flipped.** `internal/isolation/scanning_criteria_test.go`: `SS3.ui`
  loses its `Blocked` marker and binds the new Playwright title; `blockedClauses`
  is now `0` — every ADR §9 leg is proven.
- **E2E.** `web/e2e/flows/scanning.spec.ts` gains
  `blocks a public declaration field, acknowledges, and resubmits (SS3 [UI])`:
  create a config key → open its declaration detail → a canary description is
  refused → the dialog states the consequence, shows only the redacted finding,
  offers no ignore-all, and the canary reaches neither the dialog DOM nor the
  console → Escape closes without submitting → override acknowledges and the
  resubmit is saved.

## Decisions / deviations from the 2026-08-21 handoff comment

- **The env-create mount is dead design.** That comment predates #491 (which
  added the declaration editor) and #492 (which shipped `ScanBlockDialog`). The
  ticket body's target — "the declaration editor introduced by #491" — governs.
  The block dialog is mounted on `MetadataEditor`, not the env-create form.
- **Single audited override, not per-finding checkboxes.** The presentation
  reuses #492's already-merged, already-reviewed `ScanBlockDialog`.
  Acknowledgement remains **one content-bound token per finding** (the button
  submits exactly the listed findings' tokens); there is no select-all / blanket
  ignore-all input on any surface.
- **Stale/skew/surplus is covered at the unit + server layers, not e2e.** The
  native `<dialog>` is modal, so content is frozen at submit and the resubmit
  always carries token-bound content — a stale-by-name refusal is not reachable
  through this UI. The *surfacing* of the named refusal is proven in vitest
  (`ScanBlockDialog.test.tsx`, `KeyDeclarationDetail.test.tsx`); the server-side
  stale/skew/surplus-by-name behaviour is proven in Go (#74).

## Verification

- `pnpm --dir web typecheck` — clean.
- `pnpm --dir web test` — 433 passed.
- `pnpm --dir web build` — ok.
- `go test ./internal/isolation/ -run TestScanningCriteriaMatrixIsComplete` — ok.
- Targeted Playwright: the new SS3 [UI] journey (desktop + mobile in CI).

## Fresh-worktree gotcha

`web` typecheck/build resolves the generated client's `zod` from
`clients/ts/node_modules`. A fresh checkout must
`pnpm --dir clients/ts install --frozen-lockfile` first, or every SPA file
fails with TS2307 cascading to `implicit any`. CI's `build-spa.sh` does this;
a local run must too.
