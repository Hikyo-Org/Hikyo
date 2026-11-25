# #1492: key-declaration and schema-editing dialog conformance (Parts A and B)

Snapshot: 2026-11-25. Part A merged in #1496. Part B (dialog visual polish) is
the follow-up PR described under [Part B](#part-b-dialog-visual-polish), which
marks ledger obligation `OBL-UI-SCHEMA-POLISH` implemented.

## Part A: what changed

The UI-only gaps between the dialogs and the locked
[ui-spec requirements](../spec/ui-spec.md#key-declaration--schema-editing-requirements)
and [schema-model.md](../adr/schema-model.md) are closed. No server or API behaviour change.

| Requirement | Change |
| --- | --- |
| Post-tightening advisory | `TIGHTEN_ADVISORY` in `web/src/api/matrix.ts` covers what the ADR requires: tightening cannot un-disclose, treat the values as exposed and rotate them, and old values stay readable in history. The key-detail confirm and a `warn` notice after commit both state it. So does the scan-warn dialog, before its "Reclassify as secret" action, and the matrix shows the same `warn` notice after that reclassify commits. |
| Visible trim | Key create refuses a whitespace-only first value instead of silently dropping it. The live hint says trimming leaves it empty. The row-save trim notice is now tested. |
| `any_of` hints | The row editor names every alternative as the field hint before typing. A live error replaces the hint. |
| Deprecation warning | The row editor warns when the key is deprecated, with its note and the live-value count from the cells it can read. No server count was added, because that is a disclosure question. |
| Near-miss warning | The alert tone is now `warn`, per `ui/Alert.tsx`. |
| Multiline `string` | A matrix test shows that interior newlines survive the stage path while edge whitespace is trimmed. |
| Trim on every write | Owner decision (2026-11-23): the matrix no longer exempts `HIKYO_MAIL_PASSWORD` from the trim. It is trimmed like every value, and the row-save notice reports it. |

There are new stories:

- `KeyDeclarationDetail/TightenConfirm`
- `MatrixKeyCreate/NearMiss` and `WhitespaceOnlyFirstValue`
- `MatrixRowEditor/AnyOfHint`, `DeprecatedKey` and `MultilineValue`

## Part A evidence

- `pnpm exec tsc --noEmit -p .` is clean.
- `oxlint` on `src/routes` and `src/api/matrix.ts`: exit 0.
- `pnpm run design:check` passes.
- Unit tests pass, run with `--maxWorkers=2`: 103 tests in the 6 affected files.
- The Storybook tests for the four affected story files pass (41 tests), run through the shared heavy queue.
- Before/after screenshots of 7 stories were captured from static Storybook builds of the base and this branch, at 1280×900 and 390×844. They are kept locally and are not hosted.
- `sensitiveInventory.json`: the `api/matrix.ts` and `routes/MatrixRowEditor.tsx` pins are refreshed after a sensitivity review, recorded in its `review` field. Neither change touches plaintext state, mutation inputs or results, reveal, or focus remasking.
- History claim verified: snapshot entries keep their write-time classification (`internal/service/revision_diff.go`), and the revision diff needs only `read` (`OpValueExport`). The `revision_diff_is_per_key_and_secret_safe` conformance scenario now tightens `CONFIG` to `secret` and asserts a plain reader still diffs the pre-tighten plaintexts. The advisory text matches that behaviour and is unchanged. This is service-level evidence; the history UI was not exercised at runtime.
- An independent Standards and Spec review raised one blocking finding: the scan-warn path tightened without the advisory. It also raised low and nit findings, all fixed. Cross-provider review was not run.

## Part B: dialog visual polish

| Item | Change |
| --- | --- |
| Shared atoms | `MatrixKeyCreate`, `KeyDeclarationDetail` and `CatalogueManageDialog` render every text control through `ui/Field`, `Input`, `Select` and `Textarea`; the two folder comboboxes go through `Field`'s render function. `Field` gained `labelHidden` (catalogue rows, whose context names the control) and `Textarea` gained `controlClassName` (the masked first value). The name preview is now the field hint, `matrix-create-name-hint`. |
| Tokens | The `.matrix-key-create*` rules carry no raw px beyond hairlines. The `app.css` adherence budget fell from 206 to 198. |
| Phone layout | Below 700px the catalogue rows put the name, the key count and the actions on separate lines instead of pushing Rename and Delete past the sheet (the dialog was 400px wide in a 368px box). The desktop rows also lost a three-column grid that did not fit four children. The `any_of` add button aligns with its select, and the reclassify confirm drops paragraph margins stacked on the dialog gap. |
| Stories | `MatrixKeyCreate`: `PhoneStringConstraints`, `PhoneSchemaBuilder`. `CatalogueManageDialog`: `PhonePopulated`. `KeyDeclarationDetail`: `AnyOfEditor`, `PhoneAnyOfEditor`, `PhoneReclassifyConfirm`. `JsonSchemaBuilder` already had `PhoneRules` and `PhoneNested`. The phone stories assert that nothing scrolls sideways. |

Evidence for Part B (before/after screenshots, checks, Playwright flows and review) is in its PR. Screenshots are kept locally, not hosted.

## Remaining

1. **CLI advisory:** the CLI prints no tighten advisory. That is tracked in [#1493](https://github.com/Hikyo-Org/Hikyo/issues/1493).
2. **`HIKYO_MAIL_PASSWORD` trimming:** resolved after Part A. The server trims it on every write path (#1522) and every bootstrap value is trimmed (#1523). Upgraded installs may still show the old catalogue description, tracked in [#1562](https://github.com/Hikyo-Org/Hikyo/issues/1562).
3. **`FolderCleanupDialog`** still uses the `catalogue-manage__row-main` three-column grid and a bare checkbox and combobox. It was outside #1492's named dialogs and is unchanged.
