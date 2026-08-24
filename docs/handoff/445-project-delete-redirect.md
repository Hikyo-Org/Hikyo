# #445 — Project-delete redirect

Issue: https://github.com/Hikyo-Org/Hikyo/issues/445. Pull request:
https://github.com/Hikyo-Org/Hikyo/pull/469.

## Contract

- A successful project delete navigates to the canonical `/projects` surface.
- Navigation is the mutation hook's success callback, so it runs before the
  session refresh invalidates the deleted project's query.
- Delete refusals remain inline on the project settings page.

## Regression evidence

- The routed `ProjectSettings` test submits a real mocked `204` delete.
- Its mocked session refresh never settles; reaching `/projects` therefore
  proves navigation does not wait for refresh or a deleted-resource refetch.
- The destination cannot render the stale project-read refusal.

## Validation

- `git diff --check` passed.
- Standards and issue-spec review both returned `CLEAN` after the callback
  ordering was corrected.
- Exact-head local and CI results are recorded on PR #469.
