# Folder combobox and input review

Folder assignment now offers an explicit picker for existing folders while
retaining free-text entry for new folders. This handoff records local
implementation and validation evidence. The host records subsequent PR,
CI and merge state; deployment remains outside this change's scope.

## Implemented

- Shared `web/src/ui/Combobox.tsx` and component-owned CSS, using existing
  design tokens and glyphs without adding a dependency.
- Applied to key declaration (`MatrixKeyCreate`), metadata editing
  (`KeyDeclarationDetail`) and bulk folder cleanup (`FolderCleanupDialog`).
- The chevron opens all suggestions; typing filters them without replacing
  the entered text. Arrow keys explore, Enter selects without submitting the
  form, Escape dismisses the picker before the surrounding modal, and Tab
  preserves typed text. Suggestions use a labelled listbox and active descendant.
- The popover stays above dialogs and scroll containers, flips above the
  field near the viewport bottom, and follows scrolling and viewport changes.
- Folder options combine paths already used by keys with registered empty
  folders. Failed catalogue reads do not contribute retained stale suggestions.
  The merged list is memoized in `Matrix.tsx`.
- Existing and new folder paths preserve case, spaces and UTF-8. Only outer
  whitespace is trimmed; the server remains the authority on path validity.
  Creation still occurs on the existing Declare, Save or Move action.

## Other input fields

These recommendations are based on source inspection. Only the three folder
assignment surfaces above were changed.

| Field | Recommendation | Reason and source |
| --- | --- | --- |
| Member grant principal | Useful next candidate: searchable suggestions plus exact-ID entry | `Members.tsx` currently separates existing-member selection and manual principal IDs. Selection must preserve identity IDs and must not imply creating an identity. |
| PKI binding organization, project and environment IDs | Use dependent searchable selectors for existing resources | `PkiProfilesPanel.tsx` requires manual IDs. Load permitted resources in scope order; binding does not create them. |
| Remote adapter owner, repository and GitHub environment | Conditional candidate | `Adapters.tsx` needs provider discovery before it can offer real remote choices. Local project or environment names are not substitutes. Preserve the explicit remote-creation checkbox. |
| Audit operation and resource filters | Optional suggestions with free text | `Audit.tsx` accepts wildcards. A closed select would remove valid filter expressions. |
| Temporary-access approver and bypasser IDs | Keep exact-ID entry with current capabilities | `TemporaryAccess.tsx` explicitly lacks directory search. Do not invent directory choices. |
| Change-approval people | Keep the current known-person picker and advanced ID entry | `ChangeApprovals.tsx` already supports both available suggestions and exact IDs. |
| Target keys | Keep existing key choices and exact-name/pattern inputs | `TargetKeySelection.tsx` already exposes existing keys. Patterns need free text. |
| Folder creation and rename paths | Keep text | `CatalogueManageDialog.tsx` creates or renames a unique folder; choosing an existing path would conflict rather than assign it. |
| Algorithms, classification, account kinds and short environment lists | Keep current select, radio or checkbox controls | Small finite sets do not need an editable suggestion field. |
| Secret values, descriptions, URLs, patterns, JSON and numeric settings | Keep their current input types | These fields do not represent reusable folder-like choices. Secret suggestions would also introduce unnecessary value handling. |

## Verification

All commands below ran from `web/` with the pinned Node version and frozen
client/web dependencies.

| Check | Result |
| --- | --- |
| `node --run typecheck` | Passed |
| `node --run lint` | Passed |
| `node --run design:check` | Passed, including the new CSS owner's zero literal-size budget |
| `node --run test` | 209 files, 1,912 tests passed |
| Affected Storybook browser tests, dark | 5 files, 41 stories passed |
| Affected Storybook browser tests, light | 5 files, 41 stories passed |
| `node --run build` | Passed; bundle-size warning remains informational |
| `node --run build-storybook` with an ignored artifact output directory | Passed; documentation guard found 139 modules, 809 stories and 139 generated Docs |
| `git diff --check` | Passed |

Browser stories covered `Combobox`, `MatrixKeyCreate`, `FolderCleanupDialog`,
`KeyDeclarationDetail` and `Matrix`. Regressions cover registered empty folders,
failed catalogue reads and recovery, exact existing paths, new UTF-8 paths,
filtering, selection, custom text, disabled controls and keyboard behavior.
Manual collaborative-browser checks covered desktop and 390px layouts, both
themes, focus retention, upward placement and repositioning on scroll. The
390px page and popup had no horizontal overflow.

Independent review read `Matrix.tsx` and `KeyDeclarationDetail.tsx` in full and
checked the shared component and integrations. Its three findings were fixed:
listbox ID wiring, stale catalogue suggestions and lossy folder normalization.
No actionable findings remained. Existing mutation custody, authorization,
scanner acknowledgements and access-widening confirmation remain in their
existing owners. Neither module required a sensitivity-inventory pin update.

These browser checks use real components with Storybook API fixtures. A full
authenticated application journey, the complete Storybook browser suite,
remote CI and production runtime were not exercised. Unit runs emitted
instance-config 404 and Happy DOM teardown diagnostics while passing.

The structural graph was refreshed locally. HTML companions and screenshots
are local review artifacts outside Git; their temporary locations may not persist.

Keyboard behavior follows the
[WAI-ARIA editable combobox pattern](https://www.w3.org/WAI/ARIA/apg/patterns/combobox/).
