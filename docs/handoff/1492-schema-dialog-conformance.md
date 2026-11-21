# #1492: key-declaration and schema-editing dialog conformance (Part A)

Snapshot: 2026-11-21. Ledger obligation `OBL-UI-SCHEMA-POLISH` stays `open`: this
PR delivers Part A of [#1492](https://github.com/Hikyo-Org/Hikyo/issues/1492) only.
Part B (dialog visual polish) is still owed.

## What changed

The UI-only gaps between the dialogs and the locked
[ui-spec requirements](../spec/ui-spec.md#key-declaration--schema-editing-requirements)
and [schema-model.md](../adr/schema-model.md) are closed. No server or API change.

| Requirement | Change |
| --- | --- |
| Post-tightening advisory | `TIGHTEN_ADVISORY` in `web/src/api/matrix.ts` covers what the ADR requires: tightening cannot un-disclose, treat the values as exposed and rotate them, and old values stay readable in history. The key-detail confirm and a `warn` notice after commit both state it. So does the scan-warn dialog, before its "Reclassify as secret" action. |
| Visible trim | Key create refuses a whitespace-only first value instead of silently dropping it. The live hint says trimming leaves it empty. The row-save trim notice is now tested. |
| `any_of` hints | The row editor names every alternative as the field hint before typing. A live error replaces the hint. |
| Deprecation warning | The row editor warns when the key is deprecated, with its note and the live-value count from the cells it can read. No server count was added, because that is a disclosure question. |
| Near-miss warning | The alert tone is now `warn`, per `ui/Alert.tsx`. |
| Multiline `string` | A matrix test shows that interior newlines survive the stage path while edge whitespace is trimmed. |

There are new stories:

- `KeyDeclarationDetail/TightenConfirm`
- `MatrixKeyCreate/NearMiss` and `WhitespaceOnlyFirstValue`
- `MatrixRowEditor/AnyOfHint`, `DeprecatedKey` and `MultilineValue`

## Evidence

- `pnpm exec tsc --noEmit -p .` is clean.
- `oxlint` on `src/routes` and `src/api/matrix.ts`: exit 0.
- `pnpm run design:check` passes.
- Unit tests pass, run with `--maxWorkers=2`: 103 tests in the 6 affected files.
- The Storybook tests for the four affected story files pass (41 tests), run through the shared heavy queue.
- Before/after screenshots of 7 stories were captured from static Storybook builds of the base and this branch, at 1280×900 and 390×844. They are kept locally and are not hosted.
- An independent Standards and Spec review raised one blocking finding: the scan-warn path tightened without the advisory. It also raised low and nit findings, all fixed. Cross-provider review was not run.

## Remaining

1. **Part B of #1492:**
   - Move the raw controls in `MatrixKeyCreate`, `KeyDeclarationDetail` and `CatalogueManageDialog` onto the `Field`, `Input`, `Select` and `Textarea` atoms.
   - Replace the raw px values with tokens.
   - Add phone layout (below 700px) and stories for key create, catalogue manage, the schema builder, the `any_of` editor and the reclassify confirm.
   - Mark `OBL-UI-SCHEMA-POLISH` implemented only after Part B merges.
2. **CLI advisory:** the CLI prints no tighten advisory. That is tracked in [#1493](https://github.com/Hikyo-Org/Hikyo/issues/1493).
3. **Owner decision needed:** `Matrix.tsx` exempts `HIKYO_MAIL_PASSWORD` from trimming, mirroring the server's protected SMTP profile. No ADR permits that, and schema-model.md states one unconditional trim rule. This PR leaves the exemption unchanged.
4. **Not checked:** this PR does not verify at runtime that the history UI shows old plaintexts under plain read. The advisory copy follows the ADR.
