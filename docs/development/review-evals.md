# Review eval corpus

Regressions that reached `dev` and turned integration red. Each entry pairs the
change that introduced the failure with the change that fixed it, so a reviewer
(human or agent) can be replayed against the introducing diff to check whether
it would have caught the problem. Rerun the corpus when a review prompt, skill
or model changes, and keep the change only if it catches more without adding
noise on clean PRs.

Add an entry when a `Dev integration is red` issue closes. Record only facts
you verified from the run, the failing check and the fix; write `not traced`
for anything you did not establish.

| Red dev run | Failing check | Introduced by | Fixed by | Class |
| --- | --- | --- | --- | --- |
| [42310710033](https://github.com/Hikyo-Org/Hikyo/actions/runs/42310710033) at `526156110` | `e2e/demo/demo.spec.ts:351` "read-only details and filters work while management actions stay disabled" (desktop, mobile) | #1222 (`6ea1258bb`, renamed the credential hint "Developer principal:" to "Developer:") | #1249 (`0bdc360c8`) | Copy change without updating the e2e expectation that reads it |
| [42349026951](https://github.com/Hikyo-Org/Hikyo/actions/runs/42349026951) at `4c1e13e53` | `e2e/flows/matrix.spec.ts:1125` "meets the pinned assertion set on Key declaration": touch height of "Your own personal values" (mobile) | #1167 (`8cb364488`, own-values link in `PersonalMembers.tsx`) | #1254 (`4562b9308`) | New interactive element below the mobile touch-target size |
| [42349026951](https://github.com/Hikyo-Org/Hikyo/actions/runs/42349026951) at `4c1e13e53` | `e2e/flows/matrix.spec.ts:180` "keeps values on their environment IDs after display reorder" read count (desktop, mobile) | #1167, commit not traced | #1254 (`4562b9308`, expected reads 3+4×2 to 3+5×2) | New read path without updating the pinned request-count expectation |
