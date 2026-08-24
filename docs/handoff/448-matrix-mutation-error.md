# Issue #448 — matrix mutation refusal ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/448. Delivery PR:
https://github.com/Hikyo-Org/Hikyo/pull/475.

**State: implemented.** Stage, clear, and copy refusals are owned by the row
editor instance that initiated them. They no longer duplicate above the grid
or survive editor close, key changes, and project changes.

## Contract

- An editor-originated mutation refusal renders only inside its row editor.
- Closing the editor or resetting the matrix scope clears its visible refusal.
- A late asynchronous rejection remains bound to the original selection object,
  so reopening the same cell cannot surface stale feedback.
- Copy refusals remain visible while their originating editor stays open.
- No API, generated contract, or migration changed.

## Coverage

`web/src/routes/Matrix.mutation-error.test.tsx` exercises the Matrix route and
proves both the settled-rejection path and the close-before-rejection race. The
assertion matches alert text by containment so the removed grid glyph cannot
hide a duplicate announcement.

## Review and validation

Two-axis review completed in three bounded rounds. Spec review found and then
verified the late-rejection ownership fix; Standards review found and then
verified the test helper's narrowed `Error` contract. Final verdicts are
Standards `CLEAN` and Spec `SOUND`. PR #475 records exact-head CI results.
