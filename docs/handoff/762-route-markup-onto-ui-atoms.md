# 762 route markup onto the ui/ atoms (layer 2)

Branch `feat/762-route-markup-atoms`. Layer 2 of the Storybook migration
(`docs/handoff/storybook-ui-consistency.md` section 5): the routes stop
hand-writing what a `web/src/ui` atom already emits, the route copy findings
land, and `design:check` gains a gate so the markup cannot drift back.

Layer 1 (#761, CSS and tokens into the app) is already on main. What stays
open after this branch is #760 (backend second factor) and the sites that are
raw by ruling, both listed below.

## What moved, task by task

Commits in `git log --oneline origin/main..HEAD` order (oldest first).

| Task | What | Commits |
| --- | --- | --- |
| 1 | Routes A-K take `ui/Button`; 21 non-button `.btn` sites (anchors, `<summary>`) left raw | `04a91e41` |
| 2 | Routes L-Z take `ui/Button`; the row micro controls fold onto the quiet variant and lose their private colour rules | `94136552`, `6126e85b`, `f5c12af0` |
| 3 | Routes take `ui/Alert`; `Sections.Alert` and `Sections.Done` deleted; `ui/Alert` gains `warn` and `info`; six dead alert ids dropped | `40f06477`, `dc6f6e77`, `21c0576e` (plan doc `ecd324cd`) |
| 4 | Hinted and invalid fields take `ui/Input`, `ui/Select`, `ui/Textarea`; OIDC and remote-URL refusals become the control's `error`; a field refusal clears the form-level one | `fa9e7bdf`, `479f4aaf`, `b3785289` |
| 5 | Routes take `ui/Checkbox`, `ui/Radio` and `ui/ChoiceGroup`; adapter keys become a chips ChoiceGroup; no route rule reaches an atom's label | `3f071f6d`, `f91163ca`, `cec86a5f` |
| 6 | The route dialogs outside MachineAccess take `ui/Dialog`; the route hook file keeps only `useFeedback`; the wide dialog keeps its phone treatment | `f46009da`, `024cbc2a`, `63c87019`, `7724b8d7` |
| 7a | MachineAccess takes `ui/Dialog` and `ui/Tabs`; the `nextTab` helper is deleted, the atom story covers its keyboard model | `f183509a`, `90f1cfff` |
| 7b | The last 19 `.ceremony` / `.matrix-editor` shells take `ui/Dialog`; both shells and their CSS are deleted; the atom owns the backdrop click | `c08ad494`, `1574ec90`, `646b5e78` |
| 8 | Emoji and text glyphs become `ui/Glyph`; `AccountSecurity`'s QR becomes `ui/auth/QrCode`; routes take `ui/Badge`; the settings tags become quiet buttons with `aria-pressed` | `0abcdae3`, `af7710e5`, `d9853eb3` |
| 9 | `/login` renders `ui/auth/LoginForm`; the password stays in sensitive state; every sign-in leg retires when an attempt starts | `5b03bb9c`, `50bff042`, `a52737e9` |
| 10 | Route copy: the StepUpBanner title covers provider sessions, two ledes say what the page does rather than which colour it is | `d00813e1`, `73d9093d` |
| 11 | The design calls: row editor on the dialog anatomy, publish sheet legend and eyebrow and badges, credential lifetime as a choice group with its own Days field | `d813489b`, `8d700073`, `3da0c246`, `1e7b9cb9`, `6b28299d`, `913b81ea` |
| 11 fix | `Audit.tsx`'s pane refusal takes `ui/Alert`, the row editor's toggles sit beside the panels they open | `d4604925` |
| 12 | The `markup-check` gate, this handoff, section 5 closed | `f4ddb83c`, `bc3bbc2f`, and this one |

The `app.css` adherence budget ratcheted down through the series, 284 to 266,
as each task's CSS was retired.

## Atoms extended, and why

Six atom edits were authorised during the series; each was a gap the routes
proved, not a convenience.

- **`ui/Alert` gains `warn` and `info` tones** (`dc6f6e77`). The routes carried
  three severities where the atom had two, so without them a caution would have
  rendered as a failure.
- **`ui/Dialog` gains `onBackdropClick`** (`646b5e78`). Deleting the
  `.matrix-editor` shell would have removed click-to-dismiss from the two matrix
  editors; the prop restores it (fires only when the click target is the dialog
  itself) and a story covers both halves.
- **`ui/auth/LoginForm` holds the password in `useSensitiveState`** (`50bff042`).
  The route had the retirement wipe, the mount clear and the stale-generation
  refusal; plain `useState` in the atom would have dropped all three. Security
  fix, found in review.
- **`ui/ChoiceGroup` lost an `as` cast** (`d813489b`): it types its column custom
  property instead, so the group toggle reads as a row heading without the
  escape hatch.
- **`ui/Tabs.stories` covers the keyboard model** (`90f1cfff`) that the deleted
  `nextTab` helper's tests used to cover.
- **`ui/auth/QrCode`'s docstring** was corrected (`d813489b`) once
  `AccountSecurity` stopped keeping its own copy.

## Sites left raw, and why

Every such site carries a `markup-check:` comment, which is what the gate
reads. Grep: `grep -rn "markup-check" web/src/routes web/src/app`. Cited by the
comment's own words, since line numbers move.

**Not an alert** (a status paragraph, or a `role="note"`, that borrows the
alert or notice skin but is not a refusal), marker `not an alert`:
`Values.tsx`, `Sections.tsx`, `MachineAccess.tsx` (three of them, the reveal,
the mint and the grant status lines), `Remotes.tsx`, `ImportWizard.tsx`,
`ScimProvisioning.tsx` (the severity-keyed `<li>` and one status paragraph).

**The `machine__policy` skin**, marker `machine__policy skin`:
`MachineAccess.tsx` x3. The policy paragraphs combine `alert` or `notice` with
a route class that restyles them; they are alerts, but not the atom's shape.

**Inline refusal against a block atom**, marker `inline refusal, atom is
block-level`: `OrgSettings.tsx`, a `<span className="alert" role="alert">`
inside `.settings-row__copy`. It is a refusal, but `ui/Alert` renders a block
div, which would break the row's inline copy. The issue's literal
`className="alert"` grep over the routes is clean apart from this one span.

**Rich label, so the atom's `label: string` cannot carry it**, marker `rich
label`: `Matrix.tsx` (the PROTECTED marker is its own span, pushed right),
`Adapters.tsx` x2 (each option leads with a `<strong>` verb).

**No visible label**, marker `no visible label`: `FolderCleanupDialog.tsx`, a
bare checkbox in a three-column grid whose name is the column beside it; it
keeps its `aria-label`.

**An attribute the atom cannot pass through**, marker `the row's \`title\`
disambiguates`: `ChangeApprovals.tsx`, whose row `title` separates two people
with the same display name (`ui/Checkbox` spreads rest onto the input, which
would shrink the tooltip to the box).

**A control the atom does not express**, marker `a raw textarea inside the
atom's field`: `MatrixRowEditor.tsx` x2. The secret face is
`-webkit-text-security` on a textarea so pasted newlines survive, which no
Input type expresses; Field still owns the label and the wiring.

Not gated at all, by ruling: the 37 standalone `.field__hint` captions (they
are captions, not fields) and the non-button `.btn` sites from Task 1
(`<a>`, `<Link>`, `<summary>`), which are not buttons.

## The gate is green at HEAD

`Audit.tsx`'s events-pane refusal (`<p className="audit__empty alert"
role="alert">`) was the one site the gate caught that no task had ruled: the
Alert task's commits never opened the file. Task 11's fix round converted it to
`<Alert>` in `d4604925`, so `pnpm run design:check` passes on the branch, and
with it `design:export`, `storybook` and `build-storybook`, which chain
through it.

## Visual deltas the tasks accepted

Summarised from the task reports in
`.superpowers/sdd/2026-09-17-762-route-markup-onto-ui-atoms/`.

- **Alerts (Task 3).** `ui/Alert` renders a `div`, so alerts inside cards regain
  their declared ink (the `.card p` dim override was a leak) and lose the UA
  paragraph margin in dialogs and forms, where gap governs spacing. Accepted as
  the approved design.
- **Choice rows (Task 5).** Rows drop from a private 36px to the foundation's
  24px on a fine pointer (coarse stays 44px) at the publish confirmation, the
  import-wizard rows, the definitions-bundle delete row and the adapters
  conflict rows. Three captions move from 13px dim to the foundation's 14px
  full-ink option text. The adapter keys render as chips, the variant the atom
  documents for short key names.
- **Buttons (Task 2).** The add-key and revoke micro controls lose their private
  colour and hover rules and take the plain quiet look; revoke is a 36x36 quiet
  icon button with no negative pull-in.
- **Dialogs (Tasks 6 and 7).** The atom's anatomy is the approved one: narrow
  dialogs 24px padding, 32px gutter, 760px cap; wide dialogs keep the phone
  treatment (16px 12px padding, stretched action buttons) via
  `.dialog.dialog--wide`. GrantDialog's no-Cancel branch gained a Close.
- **Badges and glyphs (Task 8).** The problem count loses its danger tint; the
  armed countdown keeps `tone="ok"` as an on-state; the org-wide scope badge is
  neutral; the SAML warning changes tone to match the mapping; the publish
  sheet's lock keeps assistive text and says "secret".
- **Task 11 design calls.** The row editor takes the dialog anatomy and its
  fields go through `ui/Field`, which adds a `.field > label` on `--tx-dim` to a
  row the e2e contrast pin reads; the publish sheet gains a legend, an eyebrow
  and badge rows inside an indent rule written for a key list; the lifetime Days
  field inherits an `8ch` width from a deleted rule and now sits on its own
  line. All four want an eye in the preview (checklist below).

## Sensitivity inventory

Task 9's fix round widened the inventory regex to `useSensitiveState`, so
`ui/auth/LoginForm.tsx` is pinned. That widening newly pinned 16 further routes
**at their unchanged content**: they were pinned by hash, not re-read line by
line for sensitivity. A fresh sensitivity read of those 16 files is not part of
this branch.

## Dependency: #760 for the `/login` gate

Section 3's sign-in direction needs backend enforcement of the second factor.
Until #760 lands, `/login` renders `ui/auth/LoginForm` against today's API and
the challenge and setup gates stay mocked in Storybook. Wiring the route to
those gates is #760's work, not this branch's.

## How to run the checks

```sh
export PATH="$HOME/.local/share/fnm:$PATH"; eval "$(fnm env)"; fnm use 26
cd web
pnpm run design:check      # tokens, app.css adherence budget, markup-check
node --run typecheck
node --run test            # unit
node --run test-storybook  # story tests, a11y addon on error
pnpm run e2e               # build + playwright desktop and mobile
```

`design:check` is also the first leg of `design:export`, so Storybook builds
run it too.

### The markup gate

`web/scripts/design/markup-check.ts` walks `web/src/routes` and `web/src/app`
(`.tsx`, excluding `.test.` and `.stories.`) and fails on markup an atom
already emits: `alert`, `notice`, `chk`, `ceremony` and `matrix-editor` as
class tokens, `type="checkbox"`, `type="radio"`, a single-line `<button>` with
`className="btn`, `settings-tag`, `chip`, and the glyphs the atoms own. The
scan itself is `web/scripts/design/markup.ts`, pinned by
`web/scripts/design/markup.test.ts`. Add a pattern when an atom lands.

A site that stays raw by ruling carries a `markup-check:` comment, and the
comment rules exactly one thing. On a line that also holds markup it rules that
line. Alone on its line it rules the single element that starts on the next
non-blank line, followed to that element's close, which is how one ruling
covers a radiogroup's two inputs. It never reaches a sibling, and a marker at
column 0 is itself a gate failure (it would rule a whole module). Any wording
counts: the gate reads the marker, the reviewer reads the reason, and there is
no allowlist hidden in the script.

## Preview verification (controller)

Local checks cannot see any of these. Fill in on the preview, 1280 and 390.

- [ ] Row editor density: the dialog anatomy at 24px padding, the fields
      through `ui/Field`, nothing cramped.
- [ ] Publish sheet: legend, eyebrow and badge rows read as a hierarchy, and the
      lede above the legend does not read as a duplicate.
- [ ] Credential lifetime: the radio group reads as a group, the Days field on
      its own line is wide enough at `8ch`.
- [ ] Dialogs: 24px padding on the narrow family, and the wide family keeps its
      phone treatment at 390.
- [ ] Invite dialog: the primary action is last in the action row.
- [ ] Revoke: 36x36 quiet icon buttons, no pull-in, aligned with their row.
- [ ] Settings toggles: quiet buttons carrying `aria-pressed`, at control
      height inside the settings rows.
- [ ] `.field > label` contrast in the row editor (`--tx-dim`), which the e2e
      contrast pin reads.
- [ ] 390px: no horizontal scroll on the matrix, the dialogs or the settings
      rows.
