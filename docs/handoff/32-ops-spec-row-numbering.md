# #32: ops-spec row numbering and supersession obligations

## Result

- `OBL-OPS-ROW-NUMBERING` is implemented. The `docs/adr/ops-spec.md` decision
  inventory had two rows numbered 15 (*Secret scanning* and
  *Plans/pins/grants*). By owner decision the inventory is renumbered:
  *Secret scanning* stays 15 and *Local repository scanning* stays 15a, and
  the second 15 and every later row move down by one (old 15 → 16, 16 → 17,
  17 → 18, 18 → 19, 19 → 20, 20 → 21, 21 → 22). No value, default or row name
  changed.
- Live citations are updated:
  - `ops-spec.md`: the #930 banner and the historical-inventory note (row 18 →
    19) and the #589 banner (row 21 → 22). Each states the old number.
  - `docs/spec/ops-catalogue.md`: the adapter-outbox citation (row 19 → 20) and
    a dated correction under "Row numbering collision".
  - Code comments and one test citation string:
    `internal/definitions/bundle.go` (row 15 → 16),
    `internal/remotefetch/bounds.go`, `internal/freetext/sanitize.go` and
    `internal/conformance/boundregistry_test.go`.
- Handoffs and reports dated before 2026-11-20 keep the old numbers; the
  amendment banner maps them.
- `OBL-OPS-SUPERSESSION` is implemented without a reissue, by owner decision.
  The *Signing re-key/revocation* clause in `ops-spec.md` already states "no
  overlapping signing window" and that it supersedes the earlier
  one-release-overlap sketch. No other overlap wording remains.

## Governance

- The owner authorized reopening #32 for this scope on 2026-11-20
  ([comment](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-7041663042)).
- The owner accepted independent Claude Standards and Spec review in place of
  cross-model review, which is unavailable because the Codex subscription has
  ended. Cross-model review did not run and is not CLEAN.
- `docs/spec/open-items.md` keeps both immutable obligation rows; the ledger
  carries their status.

## Verification

- `node scripts/ci/check-doc-status.mjs --check` and
  `scripts/ci/check-doc-status_test.sh`.
- The inventory has one row per number: 15, 15a, then 16 to 22.
- `rg -n -i '\brows? ?(1[5-9]|2[0-2])'` outside handoffs and reports finds only
  updated citations.

## Remaining

- Close #32 again once this pull request merges.
