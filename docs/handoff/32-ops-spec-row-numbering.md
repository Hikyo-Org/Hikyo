# #32: ops-spec row numbering and supersession obligations

## Result

- `OBL-OPS-ROW-NUMBERING` is implemented. In the `docs/adr/ops-spec.md`
  decision inventory, *Plans/pins/grants* shared number 15 with *Secret
  scanning*; it is now **15b**. No value, default or row name changed.
- The relabel avoids renumbering: every row cited by number elsewhere keeps it
  (row 15 and 15a in `secret-scanning.md`, row 18 in `ops-spec.md`, row 19 in
  `ops-catalogue.md`, row 21 in the registration amendment). No document cites
  the Plans/pins/grants row by number.
- `OBL-OPS-SUPERSESSION` is implemented without a reissue, by owner decision on
  2026-11-20. The *Signing re-key/revocation* clause in `ops-spec.md` already
  states "no overlapping signing window" and that it supersedes the earlier
  one-release-overlap sketch; no other overlap wording remains.
- An editorial amendment banner in `ops-spec.md` records both.

## Governance

- The owner authorized reopening #32 for this scope on 2026-11-20
  ([comment](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-7041663042)).
- Cross-provider review is unavailable (the Codex subscription has ended) and
  is not CLEAN. Independent Claude Standards and Spec review is recorded on the
  pull request.
- `docs/spec/open-items.md` keeps both immutable obligation rows; the ledger
  carries their status.

## Verification

- `node scripts/ci/check-doc-status.mjs --check` and
  `scripts/ci/check-doc-status_test.sh`.
- `git grep -n -E '\| 15 \|'` on `docs/adr/ops-spec.md` finds one row.

## Remaining

- Close #32 again once this pull request merges.
