# Hikyo Storybook PR #881 delivery handoff

Delivery phase: user approved commit, push, PR and merge. Release, deployment and manual remote infrastructure changes remain excluded. PR #881: https://github.com/Hikyo-Org/Hikyo/pull/881 . Linked to the T3 thread. Implementation and build-output fix commits are cryptographically verified by GitHub; required exact-head CI is pending. Implementation commit: `d267e8b0b07588be323e1c565b14a158d44ce00d`; current-main base: `9a5d678a553ef469819530669044da062059e22e`.

The authoritative catalogue is **643 stories / 131 Docs / 131 modules**. The approved local snapshot remains 642; main added one retained `routes-clireauth--developer-credential` case. The original improvement accounted for `584 - 16 + 74 = 642`. Current inventory covers 304 visual symbols and 30 surfaces. There are no unexplained removals or changed retained IDs. Original sidebar trees, removal mappings, controls/defaults, exceptions and matched desktop/phone evidence remain in [the full local handoff](storybook-improvement.md), its [self-contained offline copy](storybook-improvement.html), and [the refreshed machine inventory](storybook-improvement-inventory.json).

Theme mechanics live in four representative central browser checks. All 643 stories still render and receive accessibility checks in both palettes. Dark/manual execute 517 full plays; light executes 512 full plays plus five audited appearance callbacks; 126 stories have no play. Selected, expanded, error, busy, permission and keyboard states keep full coverage. Manual Canvas keeps full interaction, while Docs journeys/overlays preserve manual exploration.

## Changes and review

Normalized slash-separated titles with explicit retained IDs; added missing owner stories, real-route named journeys and captioned galleries; replaced 16 redundant cases with retained views or owner regressions. Added recursive source-matched generated Docs and interaction-accounting gates. Strengthened meaningful shared Button ownership checks while retaining native semantic HTML. Moved Button styling/fonts to their owners, preserved cascade and production motion, isolated fixtures/Docs caches and top-layer frames, and fixed Docs theme rendering plus the preview surface token.

Current-base Standards and Spec reviews passed. Standards found attached `-oPATH` output parsing could validate a stale default build; its new regression first failed (exit 1), then passed with the owner fix. A full installed-script build using attached output also passed its mandatory Docs gate. Cross-provider review was **SKIPPED** because the configured Anthropic route is unavailable. This is not a clean cross-provider verdict. Jev and GitHub required gates will be recorded in the delivery receipt after the PR exists.

Auth, WebAuthn/popup human-only exclusions, secret handling, sensitivity inventory, OpenPencil assets/design exports and publishing boundaries are preserved. Three newer main PRs were incorporated cleanly; their security changes were retained.

## Reproducible local checks

From `web/`, use pinned Node 26.7.0 and installed pnpm 11.24.0. Browser commands set `STORYBOOK_STATIC_DIR=.artifacts/storybook-improvement/after-main-refresh/storybook-static`. Suites ran sequentially against an immutable build. All exit 0; unit results: 183 files / 1,612 tests; dark/light: 131 modules / 643 stories each; central theme: four tests.

| Command | Exit | Wall time |
| --- | --- | --- |
| `node --run typecheck` | 0 | 1.69s |
| `node --run lint` | 0 | 0.21s |
| `node --run design:check` | 0 | 0.39s |
| `node --run test` | 0 | 15.76s |
| `node --run build` | 0 | 1.37s |
| `node --run build-storybook -- --output-dir .artifacts/storybook-improvement/after-main-refresh/storybook-static` | 0 | 30.48s |
| `node --run check:storybook-docs -- .artifacts/storybook-improvement/after-main-refresh/storybook-static` | 0 | 0.33s |
| `node --run test-storybook` | 0 | 43.00s |
| `node --run test-storybook:light` | 0 | 37.79s |
| `node --run test-storybook:theme` | 0 | 15.46s |

The build-output-only correction subsequently reran the affected guards and full units, without duplicating unchanged browser suites:

| Command | Exit | Wall time |
| --- | --- | --- |
| `node --run typecheck` | 0 | 1.92s |
| `node --run lint` | 0 | 0.22s |
| `node --run design:check` | 0 | 0.38s |
| `node --run test` | 0 | 17.99s |
| `node --run build-storybook -- -o.artifacts/storybook-improvement/after-output-fix/storybook-static` | 0 | 36.19s |
| `node --run check:storybook-docs -- .artifacts/storybook-improvement/after-output-fix/storybook-static` | 0 | 0.34s |

Logs and exact command JSON: `web/.artifacts/storybook-improvement/after-main-refresh/` and `after-output-fix/`. The attached-output build is a separate immutable directory. AST/source/Docs inventory verification and `git diff --check` passed. Graphify AST refresh exited 0; pre-existing Astro parser warnings remain navigational limits, separate from runtime tests.

## Measurements and browser evidence

The original before/after measurements and baseline artifacts remain intact in the full handoff. Startup and total lazy chunks are reported separately. Five light shortcuts are a coverage policy, not a proven speed gain: matched timings varied, including one light reference faster than the optimized run. No reliable light-suite speedup or app-startup reduction is claimed.

Current-main integration footprint below uses gzip level 9, mtime 0, and counts source assets separately from compressed siblings; these numbers include upstream changes and are not attributed to this improvement alone.

| Asset group | Files | Bytes | Gzip bytes |
| --- | --- | --- | --- |
| appInitialJS | 2 | 647475 | 175697 |
| appInitialCSS | 1 | 105305 | 18406 |
| appTotalJS | 8 | 1647033 | 447347 |
| appTotalCSS | 1 | 105305 | 18406 |
| storybookJS | 345 | 10795555 | 2880520 |
| storybookCSS | 1 | 105250 | 18431 |
| storybookAssets | 46 | 2576214 | 856728 |

The approved matched Canvas/Docs desktop/phone comparisons, Button controls, API screen, modal/popover, long content, keyboard, theme switching and repeated navigation are archived in the full offline handoff. Current T3 browser rechecked the upstream developer-credential Canvas and light Button generated Docs (description, defaults, controls and sidebar) on the refreshed immutable build. Early Docs `No Preview` was a loading sentinel; completed Docs rendered correctly before capture. Current preview: http://192.168.0.30:6027/storybook/?path=/docs/ui-button--docs . HTTP 200 verified; server session `80216` remains running through review.

Negative proofs include broken fixture query matching, stripped Docs, missing interaction-policy bindings and the old faulty surface theme, all failing before restoration. No timeout increase, accessibility-rule disabling, arbitrary readiness sleeps or production-motion reduction was used. Remote CI and merge are not established by these local results; follow the linked PR and delivery receipt.

## Qodo review corrections, 10 October 2026

Both Qodo findings were verified and fixed: discussion `4238000062` (unrelated sibling frame accepted) and `4238000065` (pnpm forwards a bare separator). The former's owning regression failed before the correction; the actual installed `pnpm run check:storybook-docs -- <immutable-dir>` command failed with `web/--/index.json` before correction. Both now pass.

The per-export guard uses effective metadata/story parameters, canonical frame import aliases, shallow JavaScript spread order and Storybook parameter inheritance. It rejects unrelated constants/comments/siblings, inline overrides, unresolved isolation fields, shadowed helper isolation parameters, invalid `app` scalars, evaluated declaration assignments/updates and unsupported top-level control flow. It never executes story code; the runtime refusal remains independent. Static conventions and their limits are documented beside the preview.

Validation artifacts: `web/.artifacts/storybook-improvement/after-qodo-fix/`; before-failure proofs: `delivery/qodo-frames-before.log` and `delivery/qodo-argv-before.log`. Typecheck, lint, design checks, full units, actual attached-output full Storybook build and complete Docs/accounting guard exited 0. After the final mutation regressions, typecheck/lint, full units (184 files, **1,624 tests**) and the installed pnpm complete-index command passed again. The final guard validates 67 app modules, 643 stories and 131 source-matched Docs. Follow-up Standards review is CLEAN; cross-provider remains SKIPPED. Browser/application source and stories are unchanged by these guard-only corrections, so the prior full dark/light and central theme evidence remains applicable; browser suites were not duplicated locally.

The machine inventory includes current source hashes and both finding dispositions. GitHub required exact-head CI remains pending; merge and its receipt will be verified separately.
