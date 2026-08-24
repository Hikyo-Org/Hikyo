# Issue #442 — stale deleted-key history filter

Issue: https://github.com/Hikyo-Org/Hikyo/issues/442

## Contract

- Revision-history browsing remains render-safe when `?key=` names a deleted
  key that is absent from both the current catalogue and the selected
  environment's revision lineage.
- The filter banner and empty state identify that value as an unknown key.
- Restore remains fail-closed: `restoreKeyName` still throws when a key is
  absent from the current catalogue.
- Routes, API wires, persistence, and generated outputs are unchanged.

## Regression evidence

- `historyKeyDisplay` returns the neutral deleted-key display when neither the
  catalogue nor selected revision can name the key.
- `HistoryDrawer` renders its empty state for a stale deleted-key query instead
  of throwing during render.

## Validation

Pending while local memory pressure is high. Pull-request CI is the first test
run; local focused and package checks follow when pressure subsides.

## Review

Pending two-axis standards and issue-spec review.
