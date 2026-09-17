# #762 Route markup onto the ui/ atoms (layer 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-written markup in `web/src/routes` and `web/src/app` with the `web/src/ui` atoms, delete the superseded route-local helpers and CSS, apply the route copy findings and the three design calls, and close `docs/handoff/storybook-ui-consistency.md` §5.

**Architecture:** Layer 1 (#761, the base branch this plan builds on) already moved every `ui.css` rule into `app.css`, so every atom renders correctly in the app and the dedupe tasks are zero-visual-change tag swaps. Tasks are batched by atom kind, not by route, so each commit drives one grep to zero and is reviewable as one shape of change. The final task adds the "Done when" greps to `scripts/design/adherence-check.ts` so the migration cannot regress.

**Tech Stack:** React 19, TypeScript (`node --run typecheck`), Vitest unit (`node --run test`), Vitest Storybook browser project (`pnpm run test-storybook`), Playwright e2e (`pnpm e2e`).

**Spec:** GitHub issue #762 (body reproduced in the Global Constraints below where it binds), `docs/handoff/storybook-ui-consistency.md` §4 and §5, `web/DESIGN.md`. The atoms' own doc comments in `web/src/ui/*.tsx` are the interface contract; read the atom file before using it.

## Global Constraints

- **Zero visual change** in every dedupe task (Tasks 1 through 9): the atoms emit the same classes the routes hand-write today. A story or e2e snapshot that changes in those tasks is a defect, not a design update. Only Task 11 (design calls) and Task 10 (copy) are allowed to change what the user sees.
- **Never edit `web/src/ui/*` atoms** unless a task names a gap explicitly. If an atom cannot express a site, report it under Concerns and leave that site raw; do not fork the atom.
- **No `as` casts. No `any`.** Parse, don't cast. Existing code style: single quotes, 2-space indent, `.tsx` imports with explicit extensions (`import { Button } from '../ui/Button.tsx'`).
- **No em-dash** anywhere: code, comments, commits, docs.
- **Commits:** Conventional Commits, `git commit -s` (DCO sign-off), signing stays enabled, never `--no-verify`. One commit per task unless the task says otherwise.
- **Per-task checks before reporting DONE:** `cd web && node --run typecheck && node --run test`. Tasks that touch a route with a `*.stories.tsx` also run `pnpm run test-storybook` (browser project; if Chromium is missing run `pnpm run e2e:install` once).
- **Tests are the contract:** unit tests that query `role`/`name` keep passing unchanged. A test that asserts on a class name or a text glyph that the task removes is updated in the same commit to assert the new markup (never deleted, never weakened to `toBeTruthy`).
- **e2e selectors:** `web/e2e/flows/*.ts` select `.notice` (kept: `ui/Alert tone="done"` emits `.notice`) and by role. Before finishing a task, grep `web/e2e` for every class name the task deletes and retarget the selector to the atom's markup.
- **Dead code:** when the last user of a route-local rule, helper, or component goes, delete it in the same commit (`app.css` rules included).
- **Done when (issue text, verbatim):** `grep` finds no `className="alert"`, `className="chk"`, raw `type="checkbox"` outside `ui/`, or emoji glyphs in `web/src/routes`; `routes/Sections.tsx` no longer exports `Alert`/`Done`; unit and e2e green; `docs/handoff/storybook-ui-consistency.md` §5 closed.
- **Out of scope (ruled):** the `/login`-while-authenticated challenge/setup gate waits for #760 (backend); only the `LoginForm` swap ships here.

---

### Task 1: Buttons, routes A through K

**Files:**
- Modify: every file under `web/src/routes/` whose name sorts before `L` (case-insensitive) and contains `className="btn` (non-test, non-story). At the time of writing: `AccountProfile.tsx`, `AccountSecurity.tsx`, `Adapters.tsx`, `Audit.tsx`, `CLIReauth.tsx`, `CatalogueManageDialog.tsx`, `Ceremony.tsx`, `ChangeApprovals.tsx`, `ChromeIdentityControls.tsx`, `DefinitionsBundlePanel.tsx`, `EstablishCredential.tsx`, `FederationIssuersPanel.tsx`, `FolderCleanupDialog.tsx`, `HistoryDrawer.tsx`, `ImportWizard.tsx`, `InstanceAdmin.tsx`, `InstanceConfig.tsx`, `InviteDialog.tsx`, `KeyDeclarationDetail.tsx`. Confirm with: `grep -l 'className="btn' web/src/routes/[A-Ka-k]*.tsx | grep -v -e '\.test\.' -e '\.stories\.'`.
- Read first: `web/src/ui/Button.tsx` (variants `primary | secondary | danger | quiet`, `icon` requires `aria-label`, `type` is NOT defaulted).

**Interfaces:**
- Consumes: `Button` from `web/src/ui/Button.tsx`.
- Produces: nothing new; Task 2 repeats the same mapping on the remaining files.

- [ ] **Step 1: Mapping rules (apply mechanically)**

| Today | Becomes |
|---|---|
| `<button className="btn" ...>` | `<Button ...>` |
| `<button className="btn btn--primary" ...>` | `<Button variant="primary" ...>` |
| `<button className="btn btn--danger" ...>` | `<Button variant="danger" ...>` |
| `<button className="btn btn--quiet" ...>` | `<Button variant="quiet" ...>` |
| `<button className="btn btn--icon" aria-label=... ...>` | `<Button icon aria-label=... ...>` |
| `<button className="btn btn--icon btn--quiet" ...>` | `<Button icon variant="quiet" ...>` |
| `<button className="btn some-other-class" ...>` | `<Button className="some-other-class" ...>` (Button prepends `btn`) |
| `<button className={cond ? 'btn btn--primary' : 'btn'}>` | `<Button variant={cond ? 'primary' : 'secondary'}>` |
| `<Link className="btn ...">` / `<a className="btn ...">` | **leave as is** (Button renders `<button>`; an anchor stays an anchor) |

Keep every other attribute (`type`, `disabled`, `onClick`, `aria-*`, `data-*`, `ref`) exactly as written. `type="submit"` / `type="button"` stay explicit because `Button` does not default `type`.

- [ ] **Step 2: Apply the mapping to every file in scope**

Add `import { Button } from '../ui/Button.tsx';` (or `'../ui/Button.tsx'` relative to the file) once per file, in the existing import block, alphabetically among the relative imports.

- [ ] **Step 3: Verify the grep is zero for the batch**

Run: `grep -n '<button[^>]*className="btn' web/src/routes/[A-Ka-k]*.tsx | grep -v -e '\.test\.' -e '\.stories\.'`
Expected: no output.

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: all green. Fix any test that asserted `.btn--primary` on a `<button>` by asserting by role and name instead.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes
git commit -s -m "refactor(web): routes A-K use ui/Button"
```

---

### Task 2: Buttons, routes L through Z, app/, and the micro controls

**Files:**
- Modify: every file under `web/src/routes/` whose name sorts at or after `L` and every file under `web/src/app/` that contains `className="btn` (non-test, non-story). At the time of writing: `Login.tsx` (only if it still exists; Task 9 replaces it, so skip it here), `MachineAccess.tsx`, `Matrix.tsx`, `MatrixKeyCreate.tsx`, `MatrixPublishSheet.tsx`, `MatrixRowEditor.tsx`, `Members.tsx`, `OidcProvidersPanel.tsx`, `OrgSettings.tsx`, `ProjectSettings.tsx`, `Projects.tsx`, `Remotes.tsx`, `RevisionDiff.tsx`, `SamlProvidersPanel.tsx`, `SamlSpKeysPanel.tsx`, `ScanBlockDialog.tsx`, `ScanWarnDialog.tsx`, `ScimProvisioning.tsx`, `Sections.tsx`, `Shell.tsx`, `StepUpBanner.tsx`, `SystemScope.tsx`, `Values.tsx`, `WorkspaceApprove.tsx`, plus `web/src/app/*.tsx`. Confirm with grep as in Task 1.
- Modify: `web/src/routes/Matrix.tsx:1226` (`matrix__add-key`), `web/src/routes/Members.tsx:484` and `web/src/routes/AccountSecurity.tsx:222,437,803` (`capability__revoke`), `web/src/routes/Matrix.tsx:1150` (`matrix__history-link`, a `<Link>`).
- Modify: `web/src/styles/app.css` rules `.matrix__group-row .matrix__add-key` (around lines 2013 and 2029 and the media-query copy around 2718), `.capability__revoke` (around 4758, 4774, and the media copy around 5156), `.matrix__history-link` (around 3983). Line numbers drift after #761; grep.
- Modify: `web/src/routes/HistoryDrawer.tsx:266` queries `document.querySelectorAll('.matrix__history-link')`.

**Interfaces:**
- Consumes: `Button` from `web/src/ui/Button.tsx`, mapping table from Task 1 (repeated below so this task stands alone).

- [ ] **Step 1: Apply the Task 1 mapping table**

| Today | Becomes |
|---|---|
| `<button className="btn" ...>` | `<Button ...>` |
| `<button className="btn btn--primary" ...>` | `<Button variant="primary" ...>` |
| `<button className="btn btn--danger" ...>` | `<Button variant="danger" ...>` |
| `<button className="btn btn--quiet" ...>` | `<Button variant="quiet" ...>` |
| `<button className="btn btn--icon" aria-label=... ...>` | `<Button icon aria-label=... ...>` |
| `<button className="btn some-other-class" ...>` | `<Button className="some-other-class" ...>` |
| `<Link className="btn ...">` / `<a className="btn ...">` | leave as is |

- [ ] **Step 2: Micro controls**

- `matrix__add-key` (a `<button>` inside a group row): `<Button variant="quiet" className="matrix__add-key" ...>` first, then read the `.matrix__group-row .matrix__add-key` rules. If they only set size, colour, and hover (which the quiet variant now supplies), delete the rules and the `className`. If they set layout (position, margin) keep only those declarations under the same selector.
- `capability__revoke` (icon-only buttons with `aria-label`): `<Button icon variant="quiet" aria-label={...} ...>`; apply the same keep-only-layout rule to `.capability__revoke`. In `Members.tsx:484` the class is chosen by a ternary; map the ternary to the `variant` prop.
- `matrix__history-link` is a React Router `<Link>`, so it cannot become `Button`. Change its class to `className="btn btn--quiet matrix__history-link"`, delete the size and colour declarations from `.matrix__history-link` (keep layout ones), and keep the class name because `HistoryDrawer.tsx:266` selects on it. Add a one-line comment above the `Link`: `// Anchor, not Button: the class is what HistoryDrawer uses to find the opener.`

- [ ] **Step 3: Grep zero across routes and app**

Run: `grep -rn '<button[^>]*className="btn' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.' -e 'routes/Login.tsx'`
Expected: no output.

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes web/src/app web/src/styles/app.css
git commit -s -m "refactor(web): routes L-Z and app use ui/Button; fold the row micro controls onto the quiet variant"
```

---

### Task 3: Alerts and notices onto ui/Alert; delete Sections.Alert and Sections.Done

**Files:**
- Modify: `web/src/routes/Sections.tsx:73-100` (delete `Alert` and `Done`, keep everything else; one internal use at line 215).
- Modify: every non-test file importing `Alert` or `Done` from `./Sections.tsx` (21 files at the time of writing: `Projects.tsx`, `SystemScope.tsx`, `FederationIssuersPanel.tsx`, `RetentionBoundsFields.tsx`, `InstanceAdmin.tsx`, `ProjectSettings.tsx`, `OidcProvidersPanel.tsx`, `InviteDialog.tsx`, `DefinitionsBundlePanel.tsx`, `InstanceConfig.tsx`, `SamlSpKeysPanel.tsx`, `Members.tsx`, `AccountSecurity.tsx`, `SamlProvidersPanel.tsx`, `OrgSettings.tsx`, `KeyDeclarationDetail.tsx`, `CatalogueManageDialog.tsx`, `ImportWizard.tsx`, `ScimProvisioning.tsx`, `AccountProfile.tsx`).
- Modify: every non-test file under `web/src/routes` and `web/src/app` with a raw `className="alert"` (28 files, 100 sites) or `className="notice"` (11 files, 25 sites) element.
- Read first: `web/src/ui/Alert.tsx` (`tone: 'danger' | 'done'`, optional `action`, emits `.alert`/`role=alert` or `.notice`/`role=status` with the glyph span).

**Interfaces:**
- Consumes: `Alert` from `web/src/ui/Alert.tsx`.
- Produces: `Sections.tsx` no longer exports `Alert` or `Done` (the "Done when" gate).

- [ ] **Step 1: Swap the Sections helpers**

`import { Alert, Done } from './Sections.tsx'` becomes `import { Alert } from '../ui/Alert.tsx'`. `<Alert>{x}</Alert>` stays as is (danger is the default tone). `<Done>{x}</Done>` becomes `<Alert tone="done">{x}</Alert>`. Files that import other names from `Sections.tsx` keep that import minus the two names.

- [ ] **Step 2: Swap the raw markup**

A raw refusal has this shape today:
```tsx
<p className="alert" role="alert">
  <span className="alert__glyph" aria-hidden="true">!</span>
  <span>{text}</span>
</p>
```
It becomes `<Alert>{text}</Alert>`. A raw `<p className="notice" role="status">...` becomes `<Alert tone="done">...</Alert>`. Where the raw element carries an inline action (a retry or replace `<Button>` inside it), pass it as `action={<Button ...>}`. Where the raw element carries a class in addition to `alert`/`notice` (for example `alert retention-warning`), keep the site raw and list it under Concerns; do not extend the atom.

- [ ] **Step 3: Delete `Alert` and `Done` from Sections.tsx**

Remove both functions and their doc comments. Sections.tsx's own use at line 215 imports from `../ui/Alert.tsx`.

- [ ] **Step 4: Grep zero**

Run:
```bash
grep -rn 'className="alert"' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'
grep -rn 'className="notice"' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'
grep -n 'export function \(Alert\|Done\)' web/src/routes/Sections.tsx
```
Expected: no output from any.

- [ ] **Step 5: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green. Tests that query `getByRole('alert')` / `getByRole('status')` keep passing because the atom emits the same roles.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -s -m "refactor(web): routes use ui/Alert; delete Sections.Alert and Sections.Done"
```

---

### Task 4: Field hints and errors onto Input, Select, Textarea

**Files:**
- Modify: the files with `field__hint` or `aria-invalid` (non-test): `FederationIssuersPanel.tsx`, `ProjectSettings.tsx`, `InstanceAdmin.tsx`, `ChangeApprovals.tsx`, `InviteDialog.tsx`, `OidcProvidersPanel.tsx`, `Adapters.tsx`, `AccountSecurity.tsx`, `InstanceConfig.tsx`, `Members.tsx`, `SamlSpKeysPanel.tsx`, `Remotes.tsx`, `SamlProvidersPanel.tsx`, `KeyDeclarationDetail.tsx`, `MatrixRowEditor.tsx`.
- Read first: `web/src/ui/Field.tsx`, `web/src/ui/Input.tsx`, `web/src/ui/Select.tsx`, `web/src/ui/Textarea.tsx`. `FieldProps = { label, hint?, error?, className? }`; the control props omit `className`; `mono` is a prop; `Input` has `revealable` for password reveal.

**Interfaces:**
- Consumes: `Input`, `Select`, `Textarea` from `web/src/ui/`.

- [ ] **Step 1: Migrate each field that has a hint or an error**

Today:
```tsx
<div className="field">
  <label htmlFor="x">Label</label>
  <input id="x" value={v} onChange={...} aria-describedby="x-hint" aria-invalid={bad ? true : undefined} />
  <p className="field__hint" id="x-hint">Hint text</p>
  {bad ? <p className="alert" role="alert">...why...</p> : null}
</div>
```
Becomes:
```tsx
<Input id="x" label="Label" hint="Hint text" error={bad ? 'why' : undefined} value={v} onChange={...} />
```
Rules: keep `id` when a test or a label elsewhere targets it, otherwise drop it (the atom generates one). `className="mono"` on the input becomes `mono`. A `<select>` becomes `Select` with the same `<option>` children. A `<textarea>` becomes `Textarea`. A hint that contains JSX (a link) cannot pass through `hint: string`; keep that field raw and list it under Concerns. Fields without hint or error are left raw in this task (not in scope).

- [ ] **Step 2: Grep zero**

Run: `grep -rn 'field__hint\|aria-invalid' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'`
Expected: only the sites listed under Concerns (JSX hints), ideally none.

- [ ] **Step 3: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green. Tests that used `getByLabelText('Label')` keep passing; tests that asserted on the hint id are updated to `toHaveAccessibleDescription('Hint text')`.

- [ ] **Step 4: Commit**

```bash
git add web/src/routes
git commit -s -m "refactor(web): hinted and invalid fields use ui/Input, Select, Textarea"
```

---

### Task 5: Checkboxes and radios onto Checkbox, Radio, ChoiceGroup

**Files:**
- Modify: the 20 route files with raw `type="checkbox"` or `type="radio"`: `InstanceAdmin.tsx`, `ChangeApprovals.tsx`, `OidcProvidersPanel.tsx`, `MatrixKeyCreate.tsx`, `Audit.tsx`, `MatrixPublishSheet.tsx`, `Adapters.tsx`, `Matrix.tsx`, `DefinitionsBundlePanel.tsx`, `Members.tsx`, `AccountSecurity.tsx`, `MachineAccess.tsx`, `InstanceConfig.tsx`, `Remotes.tsx`, `SamlProvidersPanel.tsx`, `HistoryDrawer.tsx`, `MatrixRowEditor.tsx`, `FolderCleanupDialog.tsx`, `ImportWizard.tsx`, `ScimProvisioning.tsx`. Start with the unclassed ones the issue names: `InstanceConfig.tsx:178`, `Adapters.tsx:1161/1185/1607`.
- Modify: `web/src/styles/app.css`: delete `.chk`, `.chk input[type='checkbox']`, `.chk label` (around line 3273) once no route writes `.chk` by hand, the four local checkbox size rules, and the local fieldset resets for the audit filter, the SAML panel, and the SCIM panel (grep `fieldset` in app.css near `.audit`, `.saml`, `.scim`; the #761 handoff §2 lists the checkbox rule line ranges).
- Read first: `web/src/ui/Checkbox.tsx` (`label`, `mono`, spreads input props, emits `.field.chk`), `web/src/ui/Radio.tsx` (`label`, `name`, `mono`), `web/src/ui/ChoiceGroup.tsx` (`legend`, `hint?`, `layout`, `columns?`, `variant`, children).

**Interfaces:**
- Consumes: the three atoms above.

- [ ] **Step 1: Migrate every raw input**

A single labelled checkbox:
```tsx
<div className="field chk">
  <input id="x" type="checkbox" checked={c} onChange={...} />
  <label htmlFor="x">Text</label>
</div>
```
becomes `<Checkbox id="x" label="Text" checked={c} onChange={...} />`. Drop `id` unless a test or another label targets it. A label with `className="mono"` becomes `mono`.

A set of related checkboxes or radios inside a `<fieldset><legend>` becomes:
```tsx
<ChoiceGroup legend="Legend text" layout="stack">
  <Radio name="mode" label="A" value="a" checked={m === 'a'} onChange={...} />
  <Radio name="mode" label="B" value="b" checked={m === 'b'} onChange={...} />
</ChoiceGroup>
```
Use `layout="wrap"` where the fieldset today lays options in a row, `columns={n}` where it is a grid. A checkbox whose label is not plain text (contains a `<Badge>` or a link) cannot pass `label: string`; keep it raw and list under Concerns.

- [ ] **Step 2: Delete the superseded CSS**

Delete `.chk` and its two child rules in `app.css` (the atom's rules, moved by #761, cover them), the four local size rules on `input[type='checkbox']` / `input[type='radio']` (grep `type='checkbox'` and `type='radio'` in app.css and delete every width/height/inline-size declaration on them outside the atom's block), and the fieldset resets under the audit filter, SAML and SCIM panels (the `ChoiceGroup` block already resets `fieldset`).

- [ ] **Step 3: Grep zero**

Run: `grep -rn 'type="checkbox"\|type="radio"\|className="chk"' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'`
Expected: only Concerns sites, ideally none.

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green. Tests using `getByRole('checkbox', { name })` keep passing.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -s -m "refactor(web): routes use ui/Checkbox, Radio and ChoiceGroup; delete the local checkbox rules"
```

---

### Task 6: Dialogs outside MachineAccess onto ui/Dialog; retire routes/useModalDialog.useModalDialog

**Files:**
- Modify (dialog markup): `Ceremony.tsx`, `OidcProvidersPanel.tsx`, `InviteDialog.tsx` (2), `InstanceConfig.tsx`, `Sections.tsx`, `AccountSecurity.tsx` (2), `Remotes.tsx` (2), `KeyDeclarationDetail.tsx` (`.matrix-editor`), `ScimProvisioning.tsx`.
- Modify (import only): every other importer of `useModalDialog` from `./useModalDialog.ts` or `../routes/useModalDialog.ts`: `InstanceAdmin.tsx`, `ScanBlockDialog.tsx`, `ProjectSettings.tsx`, `MatrixKeyCreate.tsx`, `DefinitionsBundlePanel.tsx`, `ImportWizard.tsx`, `Adapters.tsx`, `app/RuntimeMaintenanceBoundary.tsx`, `Members.tsx`, `CatalogueManageDialog.tsx`, `MatrixRowEditor.tsx`, `OrgSettings.tsx`, `HistoryDrawer.tsx`, `ScanWarnDialog.tsx`, `RevisionDiff.tsx`, `FolderCleanupDialog.tsx`, and `MachineAccess.tsx` (import only here; its dialogs are Task 7).
- Modify: `web/src/routes/useModalDialog.ts`: delete `useModalDialog`, keep `useFeedback`, rename the file to `web/src/routes/useFeedback.ts` and update every importer.
- Modify: `web/src/styles/app.css`: after this task and Task 7, delete every `.ceremony*` and `.matrix-editor*` rule (they were restyled to match `.dialog` in #761 and now have no users). Keep any `.ceremony__keys`, `.ceremony__scope`, `.ceremony__cap`, `.ceremony__window`, `.ceremony__totp` rule that styles CONTENT rather than the shell, renaming it to a route-scoped class (`.ceremony-keys` etc.) so it survives; the shell rules (`.ceremony`, `.ceremony::backdrop`, `.ceremony__title`, `.ceremony__lede`, `.ceremony__actions`, and the same five for `.matrix-editor`) are the ones the atom supersedes.
- Read first: `web/src/ui/Dialog.tsx` (`title`, `lede?`, `size: 'narrow' | 'wide'`, `actions?`, `onCancel?`, `initialFocus?`, `className?`; owns the `<dialog>` ref and the `showModal()`; primary action LAST), `web/src/ui/useModalDialog.ts`.

**Interfaces:**
- Consumes: `Dialog` from `web/src/ui/Dialog.tsx`, `useModalDialog` from `web/src/ui/useModalDialog.ts`.
- Produces: `web/src/routes/useFeedback.ts` exporting `useFeedback` (Task 7 imports it).

- [ ] **Step 1: Migrate each `.ceremony` / `.matrix-editor` dialog**

Today:
```tsx
const dialog = useModalDialog(firstButton);
return (
  <dialog ref={dialog} className="ceremony" aria-labelledby="t" onCancel={...}>
    <h2 className="ceremony__title" id="t">Title</h2>
    <p className="ceremony__lede">Lede</p>
    ...body...
    <div className="ceremony__actions">
      <button className="btn btn--primary">Do it</button>
      <button className="btn">Cancel</button>
    </div>
  </dialog>
);
```
Becomes:
```tsx
return (
  <Dialog title="Title" lede="Lede" initialFocus={firstButton} onCancel={...}
    actions={<><Button onClick={cancel}>Cancel</Button><Button variant="primary" onClick={doIt}>Do it</Button></>}>
    ...body...
  </Dialog>
);
```
`.matrix-editor` dialogs take `size="wide"`. Reorder actions so the primary is last. A dialog whose `aria-describedby` points at a scope paragraph keeps that paragraph in the body; drop the attribute only if the atom cannot carry it (it cannot today: list it under Concerns if the description is load-bearing for a test). A dialog wrapping a `<form method="dialog">` keeps the form inside `children` and passes the submit button in `actions` with `form="<id>"`.

- [ ] **Step 2: Move the hook import everywhere**

`import { useModalDialog, useFeedback } from './useModalDialog.ts'` becomes two imports: `useModalDialog` from `'../ui/useModalDialog.ts'`, `useFeedback` from `'./useFeedback.ts'`. `git mv web/src/routes/useModalDialog.ts web/src/routes/useFeedback.ts` and delete the hook from it.

- [ ] **Step 3: Grep**

Run:
```bash
grep -rn 'className="ceremony"\|className="matrix-editor"' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.' -e MachineAccess
grep -rn "useModalDialog.ts'" web/src | grep -v 'ui/useModalDialog.ts'
grep -rn '\.ceremony\b\|\.matrix-editor\b' web/e2e
```
Expected: first two empty; third lists e2e selectors you must retarget to `.dialog` (or better, to `getByRole('dialog', { name })`).

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src web/e2e
git commit -s -m "refactor(web): route dialogs use ui/Dialog; routes/useModalDialog keeps only useFeedback"
```

---

### Task 7: MachineAccess dialogs onto ui/Dialog and its tabs onto ui/Tabs

**Files:**
- Modify: `web/src/routes/MachineAccess.tsx` (12 `.ceremony` dialogs, one hand-rolled tablist at lines 486-522).
- Modify: `web/src/styles/app.css`: delete the `.ceremony*` and `.matrix-editor*` shell rules now that MachineAccess was their last user (see Task 6 for which rules are shell vs content).
- Modify: `web/e2e/flows/machine-access.spec.ts` selectors on `.ceremony` or the tab ids, if any.
- Read first: `web/src/ui/Dialog.tsx`, `web/src/ui/Tabs.tsx` (`Tabs<Id>` with `label`, `idPrefix`, `tabs: readonly { id, label }[]`, `selected`, `onSelect`; `TabPanel` with `idPrefix`, `selected`, children).

**Interfaces:**
- Consumes: `Dialog`, `Button`, `Tabs`, `TabPanel`, `useFeedback` from `./useFeedback.ts` (Task 6).

- [ ] **Step 1: Dialogs**

Apply exactly the Task 6 Step 1 transformation to each of the 12 dialogs (title, lede, body, `actions` with primary last, `initialFocus`, `onCancel`).

- [ ] **Step 2: Tabs**

Today the route renders a `<div role="tablist">` with `<button role="tab" id={`machine-tab-${id}`} aria-selected ...>` children and a `<div className="tabpanel" role="tabpanel" id="machine-panel" aria-labelledby=...>`. Replace with:
```tsx
<Tabs label="Machine access" idPrefix="machine" tabs={TABS} selected={tab} onSelect={setTab} />
<TabPanel idPrefix="machine" selected={tab}>...</TabPanel>
```
where `TABS` is a module-level `const TABS = [{ id: 'connections', label: 'Connections' }, ...] as const satisfies readonly TabItem<MachineTab>[];` (use the existing tab id union). Keep the keyboard behaviour the atom provides (arrow keys); delete the route's own `onKeyDown` handler for the tablist if it has one.

- [ ] **Step 3: Grep zero and delete the dead CSS**

Run: `grep -rn 'className="ceremony\|className="matrix-editor\|role="tab' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'`
Expected: no output. Then delete the `.ceremony` and `.matrix-editor` shell rules from `app.css` and run `grep -n 'ceremony\|matrix-editor' web/src/styles/app.css` to confirm only content-renamed rules remain.

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src web/e2e
git commit -s -m "refactor(web): MachineAccess uses ui/Dialog and ui/Tabs; delete the ceremony and matrix-editor shells"
```

---

### Task 8: Glyphs onto ui/Glyph; QrCode onto ui/auth/QrCode

**Files:**
- Modify (emoji): `Ceremony.tsx:254`, `ProjectSettings.tsx:838`, `MatrixKeyCreate.tsx:365`, `MatrixPublishSheet.tsx:182,189`, `Matrix.tsx:1269,1277,1723`, `HistoryDrawer.tsx:108,1040,1266`, `KeyDeclarationDetail.tsx:279,864`, `MatrixRowEditor.tsx:252`.
- Modify (text glyphs ✓ ✕ Δ ◌ ⋯): `Shell.tsx`, `ScanBlockDialog.tsx`, `MatrixKeyCreate.tsx`, `Audit.tsx`, `MatrixPublishSheet.tsx`, `Matrix.tsx`, `DefinitionsBundlePanel.tsx`, `Members.tsx`, `AccountSecurity.tsx`, `MachineAccess.tsx`, `HistoryDrawer.tsx`, `ImportWizard.tsx`, `KeyDeclarationDetail.tsx`, `CatalogueManageDialog.tsx`, `MatrixRowEditor.tsx`, `FolderCleanupDialog.tsx`, `ScanWarnDialog.tsx`, `ChromeIdentityControls.tsx`, `app/notifications.tsx`. Confirm with `grep -rln '✓\|✕\|Δ\|◌\|⋯' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'`.
- Modify: `web/src/routes/AccountSecurity.tsx:300,731-780` (delete the local `QrCode`, import from `../ui/auth/QrCode.tsx`).
- Read first: `web/src/ui/Glyph.tsx` (`name: 'lock' | 'link' | 'check' | 'cross' | 'warn' | 'delta' | 'draft' | 'ellipsis'`, `label?` makes it an image; without it `aria-hidden`).

**Interfaces:**
- Consumes: `Glyph`, `QrCode`.

- [ ] **Step 1: Mapping**

| Today | Becomes |
|---|---|
| `<span aria-hidden="true">🔒 </span>` | `<Glyph name="lock" /> ` (keep the trailing space text node if the layout relied on it) |
| `'🔒 ' + text` inside JSX text | `<><Glyph name="lock" /> {text}</>` |
| `🔗` | `<Glyph name="link" />` |
| `✓` | `<Glyph name="check" />` |
| `✕` | `<Glyph name="cross" />` |
| `Δ` | `<Glyph name="delta" />` |
| `◌` | `<Glyph name="draft" />` |
| `⋯` | `<Glyph name="ellipsis" />` |
| glyph inside a `value=` / `aria-label=` string attribute | replace the glyph with the word it stood for (`secret`, `linked`, `done`, `removed`, `changed`, `draft`, `more`), never an SVG |
| glyph inside a doc comment | leave the comment alone (comments are not markup); the "Done when" grep is for `web/src/routes` markup, so also reword the comment at `HistoryDrawer.tsx:108` to "marks the key as secret" so the grep is truly clean |
| glyph in prose that names it (`Matrix.tsx:1723` "🔒 marks the key") | `<Glyph name="lock" label="lock" /> marks the key` |

Where the glyph was the ONLY content of a button (`⋯` menu, `✕` close), the button must already carry `aria-label`; if it does not, add one that names the action.

- [ ] **Step 2: QrCode**

Delete the local `QrCode` function and its comment from `AccountSecurity.tsx`; `import { QrCode } from '../ui/auth/QrCode.tsx';` and keep the call site unchanged (same `value`/`title` props).

- [ ] **Step 2b: Badges (carried from #761 Task 3)**

Layer 1 deleted the `.chip--armed`, `.chip--wide`, `.settings-tag--on` and `.settings-tag--danger` rules (dead under the foundation badge block) and left `.history__current` and `.matrix__problem-count` styled by their own rules because their markup lacks `.badge`. Finish the fold here:
- Every `<span className="chip ...">` / `<span className="badge ...">` / `.settings-tag` that is NOT a button becomes `<Badge tone={...}>`: `--danger` and `data-state="danger"` map to `tone="danger"`, `--on` / `ok` states to `tone="ok"`, `changed` / `warn` to `tone="changed"`, everything else `neutral`. The armed countdown chip (`chip--armed`) and the wide chip carry their state in TEXT already (verify; if a state was colour-only, add the word).
- `.settings-tag` sites that are `<button>` elements (toggle tags) become `<Button variant="quiet" aria-pressed={on}>`; delete `.settings-tag*` rules from app.css when no site remains. This also restores the coarse-pointer touch floor the tag lost in layer 1.
- `.history__current` and `.matrix__problem-count` become `<Badge tone="neutral">` / `<Badge tone="danger">`; delete their rules (both `.matrix__problem-count` definitions; split `.matrix__count` out of the shared list and keep it).
- `.count` pill stays (DESIGN.md exception).
Run `grep -rn 'className="chip\|className="badge\|settings-tag\|history__current\|matrix__problem-count' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'` and expect no output.

- [ ] **Step 3: Grep zero**

Run: `grep -rn '🔒\|🔗\|✓\|✕\|Δ\|◌\|⋯' web/src/routes web/src/app | grep -v -e '\.test\.' -e '\.stories\.'`
Expected: no output. Then `grep -rn '🔒\|🔗\|✓\|✕\|Δ\|◌\|⋯' web/src/routes/*.test.tsx web/src/routes/*.stories.tsx` and update each assertion to query by role/name or `toHaveTextContent` without the glyph.

- [ ] **Step 4: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -s -m "refactor(web): routes use ui/Glyph and ui/auth/QrCode"
```

---

### Task 9: Sign-in onto ui/auth/LoginForm

**Files:**
- Modify: `web/src/routes/Login.tsx` (keep the hooks `useLogin`, `usePasskeyLogin`, `useOIDCLogin`, `useSAMLLogin`, `useAuthMethods`; replace the JSX with `LoginForm`).
- Test: `web/src/routes/Login.test.tsx` if present; `web/e2e/flows/login.spec.ts` (role-based, should pass unchanged).
- Read first: `web/src/ui/auth/LoginForm.tsx` (`providers`, `passkeys`, `busy: SignInBusy`, `error: string | null`, `onPassword({ username, password })`, `onPasskey()`, `onProvider(slug)`, `links?`), `web/src/ui/auth/fixtures.ts`.

**Interfaces:**
- Consumes: `LoginForm`, `SignInBusy`.

- [ ] **Step 1: Wire the form**

```tsx
export function Login() {
  const login = useLogin();
  const passkey = usePasskeyLogin();
  const oidc = useOIDCLogin();
  const saml = useSAMLLogin();
  const methods = useAuthMethods();
  const [contacting, setContacting] = useState<string | null>(null);

  const busy: SignInBusy = login.isPending
    ? 'password'
    : passkey.isPending
      ? 'passkey'
      : contacting !== null && (oidc.isPending || saml.isPending)
        ? { provider: contacting }
        : null;
  const error = login.isError
    ? loginFailureText(login.error)
    : passkey.isError
      ? stepUpFailureText(passkey.error)
      : null;

  return (
    <main className="login">
      <ProviderDiscoveryAlert ... />   // keep whatever the current file renders around the card
      <LoginForm
        providers={methods.data?.providers ?? []}
        passkeys={passkeysAvailable() && (methods.data?.passkeys ?? false)}
        busy={busy}
        error={error}
        onPassword={(credentials) => login.mutate(credentials)}
        onPasskey={() => passkey.mutate()}
        onProvider={(slug) => {
          setContacting(slug);
          const provider = methods.data?.providers.find((p) => p.slug === slug);
          if (provider?.kind === 'saml') saml.mutate(slug); else oidc.mutate(slug);
        }}
        links={<Link to={surfaceById('setup')}>...existing links...</Link>}
      />
    </main>
  );
}
```
Read the current `Login.tsx` fully first and keep every behaviour it has (the `useSensitiveState` password clearing moves inside `LoginForm` if the atom already does it; if it does not, report under Concerns rather than editing the atom). Match the exact provider shape from `useAuthMethods` against `SignInProvider`; if they differ, map in `Login.tsx`, do not cast.

- [ ] **Step 2: Run checks**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add web/src/routes/Login.tsx web/src/routes/Login.test.tsx
git commit -s -m "refactor(web): sign-in route renders ui/auth/LoginForm"
```

---

### Task 10: Route copy findings

**Files:**
- Modify: `web/src/routes/StepUpBanner.tsx:61` and `web/src/routes/StepUpBanner.stories.tsx:51`, plus any test asserting the old title.
- Modify: `web/src/routes/Projects.tsx` (ids shown where a count would do).
- Modify: every route whose lede restates its heading (find them: for each `<h1>`/`<h2>` followed by a `<p className="...lede">`, compare the two strings; a lede that only rephrases the heading is deleted, one that adds a fact is kept).

**Interfaces:** none.

- [ ] **Step 1: StepUpBanner title**

The banner shows for any session that lacks the second factor, including identity-provider sessions. Replace the title "This session is password-only" with "This session has no second factor" and keep the body. Update the story's `findByRole('heading', { name: /.../ })` and any unit test to the new text.

- [ ] **Step 2: Projects list**

Where a row prints a raw id (`project.id`, `org.id`) as visible text, replace it with the count that a user reads (for example `{n} environments`, `{n} keys`) taken from the data the row already has; if the row has no count available, remove the id from the visible text (keep it in `data-*` or `key`). Do not add an API call.

- [ ] **Step 3: Ledes**

For each page: keep the heading; delete a lede whose only content restates it; rewrite a lede to one concrete sentence when it can say what the page is for (what the user can do here, or what the list is filtered by). Keep every lede a test asserts on, updating the test to the new sentence in the same commit.

- [ ] **Step 4: Run checks and commit**

Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook`
```bash
git add web/src/routes
git commit -s -m "fix(web): route copy: banner title covers provider sessions, counts over ids, ledes that add a fact"
```

---

### Task 11: Design calls: MatrixRowEditor density, publish sheet copy, MintConnectionForm lifetime

**Files:**
- Modify: `web/src/routes/MatrixRowEditor.tsx` and its story, `web/src/routes/MatrixPublishSheet.tsx` and its story, the `MintConnectionForm` component in `web/src/routes/MachineAccess.tsx` (or its own file if it has one; grep `MintConnectionForm`) and `ConnectionMintDialog.stories.tsx`.
- Modify: `web/src/styles/app.css`: delete the `.matrix-editor__*` row/gap/caption rules the editor no longer uses.
- Read first: `web/src/ui/Dialog.tsx`, `web/src/ui/ChoiceGroup.tsx`, `web/src/ui/Badge.tsx` (`tone: 'neutral' | 'danger' | 'changed' | 'ok'`), `web/src/ui/Input.tsx`.

**Interfaces:** none new.

- [ ] **Step 1: MatrixRowEditor**

The editor renders through `Dialog size="wide"`; every field goes through `Input`/`Select` with the hint that today sits as a caption under it; one `actions` row (Cancel, then the primary save); rows are separated by `gap: var(--space-3)` (add one `.row-editor__rows { display: grid; gap: var(--space-3); }` rule in app.css and delete the 8px/12px rules it replaces); where the environment columns are choices (include/exclude per environment), render them as `<ChoiceGroup legend="Environments" columns={environments.length}>` of `Checkbox`.

- [ ] **Step 2: Publish sheet**

Wrap the environment rows in `<ChoiceGroup legend="Environments to publish" variant="rows">`. Each row: the `Checkbox` (label = environment name) meaning "include this environment"; then the revision step; then an eyebrow `<span className="eyebrow">changes</span>` above the key list; then the readiness as `<Badge tone="ok">ready</Badge>` or `<Badge tone="danger">blocked</Badge>` (never coloured text). Delete the coloured-text rule from app.css.

- [ ] **Step 3: MintConnectionForm lifetime**

Replace the inline sentence "Expires after [n] days (clamped to the instance ceiling)" with:
```tsx
<ChoiceGroup legend="Lifetime" layout="stack">
  <Radio name="lifetime" label="Instance default" value="default" checked={mode === 'default'} onChange={...} />
  <Radio name="lifetime" label="Expires after" value="days" checked={mode === 'days'} onChange={...} />
  {mode === 'days' ? (
    <Input type="number" label="Days" min={1} value={days} onChange={...} hint="Clamped to the instance ceiling." />
  ) : null}
</ChoiceGroup>
```
Keep the existing state and the mutation payload identical (same field names, same clamping on submit). Read the current form to learn the real option set (there may be a "never expires" option); map every existing option to a radio.

- [ ] **Step 4: Stories and checks**

Update the three stories' interactions to the new roles (`getByRole('radio', { name: 'Expires after' })`, `getByRole('checkbox', { name: <env> })`, `getByText('ready')`).
Run: `cd web && node --run typecheck && node --run test && pnpm run test-storybook && pnpm run design:check`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -s -m "feat(web): row editor on the dialog anatomy, publish sheet says what its rows are, lifetime as a choice group"
```

---

### Task 12: Regression gate, handoff, and the §5 close

**Files:**
- Create: `web/scripts/design/markup-check.ts`
- Modify: `web/package.json` (`design:check` runs it), `web/scripts/design/adherence-budget.json` (lower `app.css` budget to the new count the check prints).
- Modify: `docs/handoff/storybook-ui-consistency.md` §5 (mark each item done with the commit that did it).
- Create: `docs/handoff/762-route-markup-onto-ui-atoms.md`.

**Interfaces:** none.

- [ ] **Step 1: Write the check**

```ts
// Fails the build when route markup hand-writes what a web/src/ui atom emits.
// The list is the "Done when" of issue #762; add a pattern when an atom lands.
import { readdir, readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../../src');
const dirs = ['routes', 'app'];
const banned: readonly { pattern: RegExp; atom: string }[] = [
  { pattern: /className="alert"/, atom: 'ui/Alert' },
  { pattern: /className="notice"/, atom: 'ui/Alert tone="done"' },
  { pattern: /className="chk"/, atom: 'ui/Checkbox' },
  { pattern: /type="checkbox"/, atom: 'ui/Checkbox' },
  { pattern: /type="radio"/, atom: 'ui/Radio' },
  { pattern: /<button[^>]*className="btn/, atom: 'ui/Button' },
  { pattern: /className="ceremony"|className="matrix-editor"/, atom: 'ui/Dialog' },
  { pattern: /[🔒🔗✓✕Δ◌⋯]/u, atom: 'ui/Glyph' },
];

let failed = false;
for (const dir of dirs) {
  for (const name of await readdir(join(root, dir))) {
    if (!name.endsWith('.tsx') || name.includes('.test.') || name.includes('.stories.')) continue;
    const text = await readFile(join(root, dir, name), 'utf8');
    text.split('\n').forEach((line, i) => {
      for (const { pattern, atom } of banned) {
        if (pattern.test(line)) {
          failed = true;
          console.error(`src/${dir}/${name}:${i + 1}: hand-written markup, use ${atom}: ${line.trim()}`);
        }
      }
    });
  }
}
if (failed) process.exit(1);
console.log('markup-check: routes and app use the ui/ atoms');
```

- [ ] **Step 2: Wire it**

In `web/package.json`, `"design:check": "node scripts/design/tokens-check.ts && node scripts/design/adherence-check.ts && node scripts/design/markup-check.ts"`. Run `pnpm run design:check`; if `adherence-check` reports app.css under budget, lower the number in `adherence-budget.json` to the printed count.

- [ ] **Step 3: Handoff**

Write `docs/handoff/762-route-markup-onto-ui-atoms.md`: what moved (one line per task with its commit), what stayed raw and why (every Concerns item from the task reports), the #760 dependency for the `/login` gate, and how to run the checks. In `docs/handoff/storybook-ui-consistency.md` §5, mark the layer-2 items done and point at the new handoff.

- [ ] **Step 4: Full checks and commit**

Run: `cd web && pnpm run design:check && node --run typecheck && node --run test && pnpm run test-storybook`
```bash
git add web/scripts/design web/package.json docs/handoff
git commit -s -m "chore(web): markup-check gate for the ui/ atoms; close handoff section 5 for #762"
```
