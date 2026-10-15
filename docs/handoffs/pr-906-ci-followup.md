# PR #906 CI follow-up

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

This fix is stacked on `ui-review-pr` at `0df9a17950d6e859f81ced93704792e2ceb840c5`. Keep it separate and unmerged for review. The parent is [PR #906](https://github.com/Hikyo-Org/Hikyo/pull/906).

## Failure and repair

[Full CI attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/38520702126) tested merge candidate `f064dd1baeae20244c81cdc424cd66110c2fcd73`. Storybook's 779 stories passed in both themes; its later demo step failed eight cases. Mobile groups 1 and 2 failed in matrix navigation and revision history respectively, leaving later serial tests unexecuted.

On compact screens, a matrix key's first tap should open its name preview and its second tap should navigate. Linux Chromium emitted compatibility mouse events after touch, so the unconditional mouse-leave handler immediately closed the preview. Each tap therefore behaved like a first tap. `MatrixKeyLink` now dismisses on pointer leave only for non-touch pointers, preserving mouse and pen hover dismissal. Outside press, Escape, focus loss, desktop activation and keyboard activation retain their behavior.

The demo checks still assumed the previous page layouts. They now select Policy, Sign-in factors, Review queue and SCIM Directory before inspecting their contents, and close the service-account detail dialog before switching tabs. Existing assertions remain intact. No retries, timeout increases or workflow changes were added.

A complete mobile rerun also exposed an existing organisation-switch test race. It sampled menu visibility while a fresh mobile context was still on the session loading screen, then incorrectly selected desktop assertions. The test now branches on Playwright's configured `isMobile` fixture and lets menu clicks wait for readiness. The original trace records the mobile context, premature visibility result and subsequent mobile shell.

## Evidence

- Reproduced the original touch failure three times in the pinned Linux Playwright image. The new first-tap regression also failed against the original build.
- Independent review found a pen-hover regression in an intermediate mouse-only predicate. A new pen test failed against that version and passed after preserving all non-touch pointer exits. Final independent source review found no remaining issues.
- Final typechecking and lint passed. Production SPA build, demo isolation and CSP checks passed.
- Final Linux demo suite: 76 passed across desktop and mobile, including six new input and dismissal cases.
- Final unit suite: 195 files and 1,678 tests passed. Matrix Storybook stories: 14 passed in dark mode and 14 in light mode.
- Final live mobile group 1: 63 passed, 2 existing desktop-only skips; the original matrix failure and all previously blocked serial cases passed.
- Final live mobile group 2: all 59 passed, including the repaired organisation-switch test and the registration case whose browser crashed during the earlier OOM event.

Linux verification used Node 26.7.0, pnpm 11.24.0 and the workflow's pinned Playwright image `mcr.microsoft.com/playwright@sha256:dcc5531e97840b9b5e794f2814476b21571c5124a3fca2267d73041f56e7580e`, with locally built Linux ARM64 server binaries. Hosted CI uses x86_64; these local results do not establish hosted exact-candidate success.

An earlier demo run, concurrent with heavy builds, passed 74 cases and timed out on two loading expectations. Its evidence was retained. The final complete run passed all 76 with the original timeouts. Resource contention is a hypothesis for the earlier loading delays, not a proven root cause. A later parallel live-suite attempt exhausted the local Docker memory budget: Docker recorded an OOM event and Chromium reported `Target crashed`. Those artifacts were retained, and live verification was restarted sequentially.

## Resuming verification

From `web/`, run `node --run typecheck`, `node --run lint`, `node --run test`, then `node --run build:demo` and `node --run test:demo`. After building the production SPA, run the affected live mobile groups separately against fresh fixtures:

```sh
pnpm exec playwright test --project=mobile e2e/flows/matrix.spec.ts e2e/flows/shell.spec.ts e2e/flows/scanning.spec.ts
pnpm exec playwright test --project=mobile e2e/flows/members.spec.ts e2e/flows/account.spec.ts e2e/flows/history.spec.ts
```

The local evidence is retained under `hikyo-906-investigation/` and `hikyo-906-*.log` on the originating machine. These temporary artifacts are not portable PR evidence; the run link and commands above are the durable entry points.

The full-candidate workflow requires a default-branch base. This stacked PR intentionally targets `ui-review-pr`, so normal feedback and local verification must be distinguished from full merge-candidate validation. Keep the existing gate policy and obtain fresh candidate proof when the stack is integrated. No merge or deployment is authorized by this handoff.
