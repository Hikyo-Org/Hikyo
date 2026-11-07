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
| [42349026951](https://github.com/Hikyo-Org/Hikyo/actions/runs/42349026951) at `4c1e13e53` | `web/e2e` personal values: "touch height of Your own personal values" | #1167 (`35b1062b6`, My personal values page) | #1254 (`4562b9308`) | New interactive element below the mobile touch-target size |
| [42402195198](https://github.com/Hikyo-Org/Hikyo/actions/runs/42402195198) at `0c6f26bce` | `e2e/demo/demo.spec.ts` "members: read-only for every demo account"; `e2e/flows/machine-access.spec.ts` strict-mode `Invite` button resolved to 2 elements | #1250 (`111e98b15`, invite by email from Members, #1207) | #1263 (`43a4c76bf`) | New control duplicated an existing accessible name and changed the read-only demo surface |
| not traced | `e2e/demo` developer credential label | not traced (label rename) | #1249 (`0bdc360c8`) | Copy change without updating the demo e2e expectation |
