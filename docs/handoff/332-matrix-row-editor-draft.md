# Handoff: #332 matrix row editor draft ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/332 (parent #326; audit
finding `F-S29-1`). Implementation is based on `origin/main` commit
`94da1b003467cfe0ee7e1e87c98f061abe8a5b9b`.

## Contract

- One `Map<environmentId, MatrixDraftEdit>` owns every staged row edit.
  Absence means untouched; `{ op: 'set', value }` includes an explicit empty
  string; `{ op: 'unset' }` means clear to absent.
- `matrixDraftChanges` preserves environment-row ordering while converting
  tagged edits to the existing apply contract.
- Choosing **Keep current state** removes the row edit, restoring its exact
  initial visible value and disabling Save when no other edits remain.
- Fill-all, per-row validation, textarea edits, clear/keep toggles, rendering,
  and apply changes read or write the same edit map.
- Wire values and API operations are unchanged. Generated outputs: none.
  Database migrations: none.

## Regression evidence

- Typing a replacement, choosing Clear, then Keep restores the published value
  and leaves Save disabled.
- Touching a field to `''` submits one explicit `set` change with an empty
  value; it is not confused with unset or untouched.
- The state helper keeps untouched, explicit-empty, and unset rows distinct in
  input environment order.

## Validation

```text
rtk pnpm --dir web exec vitest run src/routes/MatrixRowEditor.test.tsx src/routes/matrix-state.test.ts
2 files / 14 tests passed

rtk pnpm --dir web run typecheck
passed

rtk pnpm --dir web run test
37 files / 310 tests passed

rtk pnpm --dir web run build
passed

rtk go test -count=1 -timeout=5m ./...
3,461 tests passed in 61 packages

rtk git diff --check
passed
```

## Review

- Standards axis round 1 found a stale base and duplicated component-test
  setup. The branch was fast-forwarded to preserve #329 and the setup was
  extracted into `renderEditor`; round 2: `CLEAN`.
- Spec axis round 1 found only the stale-base blocker; round 2: `CLEAN`.
