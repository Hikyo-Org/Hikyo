# Hikyo Storybook local improvement

Local validation evidence snapshot at baseline HEAD `44f77a2eb8610b83b6405229f7c0759ec5f995cb`, collected against the uncommitted working tree. This report records the completed local-review phase. The user subsequently authorized commit, push, PR creation and merge; release, deployment and remote infrastructure changes remain outside that authorization. This is the single entry point; the offline HTML contains this report, full inventory, both sidebar trees and embedded screenshots.

The catalogue now has **642 stories across 131 modules, each with source-matched generated Docs**, versus 584 stories / 115 Docs. The accounting is `584 - 16 + 74 = 642`. All retained story IDs and all 115 original Docs IDs/import paths remain valid. The final ten required local checks and one full-light reference exit 0. A source relationship is coverage navigation, not proof that every conditional composition branch executed.

Review preview: http://192.168.0.30:6025/storybook/?path=/docs/ui-button--docs . The immutable final build is served on a LAN interface and kept running for review. Baseline comparison remains at http://192.168.0.30:6007/storybook/ . Exact HTTP/subpath checks and the final T3 screenshots are recorded in the artifact directory.

## Measured result

| Local owner | Before wall seconds | Final wall seconds | Final exit | Reproducible command from web/ |
| --- | --- | --- | --- | --- |
| typecheck | 1.7 | 1.76 | 0 | `node --run typecheck` |
| lint | not recorded separately | 0.21 | 0 | `node --run lint` |
| design | not recorded separately | 0.37 | 0 | `node --run design:check` |
| unit | 14.62 | 15.65 | 0 | `node --run test` |
| build-app | 1.61 | 1.33 | 0 | `node --run build` |
| build-storybook | 32.47 | 22.48 | 0 | `node --run build-storybook -- --output-dir .artifacts/storybook-improvement/after-theme-surface/storybook-static` |
| docs | not recorded separately | 0.32 | 0 | `node --run check:storybook-docs -- .artifacts/storybook-improvement/after-theme-surface/storybook-static` |
| storybook-dark | 31.96 | 54.72 | 0 | `node --run test-storybook` |
| storybook-light-full-reference | not recorded separately | 42.07 | 0 | `STORYBOOK_INTERACTION_PASS=full node --run test-storybook:light` |
| storybook-light | 31.16 | 46.24 | 0 | `node --run test-storybook:light` |
| storybook-theme | not recorded separately | 15.71 | 0 | `STORYBOOK_STATIC_DIR=.artifacts/storybook-improvement/after-theme-surface/storybook-static node --run test-storybook:theme` |

Final unit result: 183 passed (183); 1600 passed (1600). Both Storybook palettes exercise all 131 modules / 642 stories, with the existing accessibility error gate enabled. Required web typechecking, lint, design checks, application build, full Storybook build and full-index Docs guard passed. Graphify AST refresh completed with exit 0; its 17 pre-existing Astro parser warnings remain navigational evidence limits, separate from the Babel source inventory and required web checks.

| Measured output | Before files / bytes / gzip bytes | After files / bytes / gzip bytes | Delta raw / gzip bytes |
| --- | --- | --- | --- |
| App initial static JS closure | 2 / 644,818 / 175,252 | 2 / 644,855 / 175,265 | +37 / +13 |
| App initial CSS | 1 / 105,320 / 18,420 | 1 / 105,305 / 18,411 | -15 / -9 |
| All app JS chunks | 8 / 1,643,114 / 447,059 | 8 / 1,643,128 / 447,081 | +14 / +22 |
| All app CSS | 1 / 105,320 / 18,420 | 1 / 105,305 / 18,411 | -15 / -9 |
| Storybook JS | 287 / 10,629,875 / 2,821,043 | 345 / 10,789,965 / 2,888,343 | +160,090 / +67,300 |
| Storybook CSS | 1 / 105,188 / 18,413 | 1 / 105,250 / 18,436 | +62 / +23 |
| Storybook other assets, including index/fonts/design | 46 / 2,435,440 / 843,801 | 46 / 2,575,276 / 856,335 | +139,836 / +12,534 |

Startup uses the emitted HTML plus recursive static imports/modulepreloads, excluding dynamic imports. Gzip level 9 is measured per asset. The current app already includes the account module in its startup closure; older lazy-output claims are stale. There is **no application JS startup or lazy-chunk saving** to claim. CSS changes are tiny; Storybook grows because it contains 16 more modules and 74 new states. App and optional catalogue costs stay separate.

The docgen optimization scopes extraction away from generated API transport/schemas only. Baseline extraction was 14.6 seconds / 91% of preview work; earlier optimized runs were 5.8 and 5.4 seconds. The current final build profile: storybook:react-docgen-plugin transform (80%, 6.6s, 775 calls). All frontend owners remain eligible and the installed transform handler/context are preserved. One warm local run is not a CI performance guarantee: machine contention and caches vary, and final wall time includes design export plus the stronger guard. A separate same-source immutable repeat, after the suites finished, passed in 29.53 seconds (preview 7.67 seconds, docgen 6.0 seconds). The earlier catalogue final build (after-complete3) was 66.18 seconds with docgen 33.3 seconds; its aggregate slowdown is not attributed to the renderer helper, whose exact installed hook profile was 45.5 milliseconds first and 3.84 milliseconds warm. All profiles and both build logs are preserved, so no stable net wall-time or CI saving is asserted. No test matrix, runner, sharding or hosting redesign was introduced.

## Central theme contract and interaction accounting

The approved follow-up centralizes switching and lifetime mechanics in four built-Storybook tests, while every one of the 642 stories remains eligible for rendering and automatic accessibility in both palettes. Shared theme helpers and renderer regressions stay at their owner. No story is excluded, no Docs or export is removed, and the existing required dark/light CI jobs remain intact.

| Pass | Full interaction plays | Appearance plays | Render-only stories | Total stories with accessibility |
| --- | --- | --- | --- | --- |
| Dark/default | 516 | 0 | 126 | 642 |
| Light | 511 | 5 | 126 | 642 |
| Manual light Canvas | 516 | 0 | 126 | manual exploration; automatic CI is separate |

| Opt-in ID | Reason | Retained view IDs |
| --- | --- | --- |
| ui-auth-providerbutton--fires-on-click | The callback spy leaves the rendered provider button unchanged. | ui-auth-providerbutton--google |
| routes-providerdiscoveryalert--default | The retry callback spy leaves the rendered error alert unchanged. | routes-providerdiscoveryalert--default |
| ui-dialog--backdrop-click | Synthetic backdrop events verify dismissal callbacks without closing the controlled dialog. | ui-dialog--backdrop-click |
| ui-auth-loginform--provider-starts-from-step-one | The provider callback spy leaves the initial sign-in view unchanged. | ui-auth-loginform--provider-starts-from-step-one |
| pages-overview--create-project-journey | Route creation/cache assertions are theme-independent. The appearance pass seeds the same project before mounting, then waits for real navigation to the same populated Projects route with AppRoutes, providers and Shell. Standalone endpoint stories remain in both palettes. | pages-overview--create-project-journey, pages-overview--empty, pages-overview--populated, routes-projects--empty, routes-projects--populated |

Only four callbacks that leave their view unchanged and the real-route Overview creation journey use the shorter light appearance play. The callbacks explicitly assert visibility. The journey seeds the same project before mounting the actual providers and AppRoutes, then navigates to the matching populated Projects route. All state-producing plays, keyboard/focus, errors, permission, busy, selected/expanded and static visibility checks otherwise stay fully active in both palettes. Axe scans the final story state; it does not automatically scan every intermediate journey step.

Installed Storybook 10.6 supports initialGlobals and whole-story filters, but has no skip-play option. A shared typed wrapper uses a test-only light global and awaits the appearance play. Manual Canvas and Docs do not receive that global. The complete-index guard accounts for all stories and binds the five static tags to canonical helper imports, literal IDs, direct play properties and source exports. It rejects missing/stale tags, test opt-outs, unrelated retained IDs, duplicate play and later overriding spreads/computed properties. Generated Docs inheriting the first export tag do not count as test cases. Semantic equivalence still requires the source audit and browser proof.

Same-source sequential light reference: 42.07s full versus 46.24s with five shorter paths, a local difference of -4.17s. Central mechanics adds 15.71s. These single local runs have cache/order variance and are not a stable speedup claim or a net reduction in required validation time. A prior matched run measured 56.36s versus 52.32s. Mount/provider/accessibility costs remain.

The central contract covers Button Canvas and inline Docs color changes, live controls and navigation cleanup; four API Docs frames retaining independent fixtures and unsaved drafts through Light/Dark/Light; owned dialogs and manually explored keyboard popovers at desktop/phone widths; and production ThemeToggle persistence/fixture teardown. Offscreen Docs iframe readiness first failed because native lazy loading had not started; the helper now scrolls the real example into view before waiting for render/play/font completion. No sleep, timeout increase or accessibility relaxation was introduced.

T3 screenshot inspection then caught a real palette defect: the separate built-in background control injected an !important dark body even after App theme switched to Light. Light page headings became unreadable. App theme now owns foreground and surface together through the existing semantic tokens; the competing background toolbar is disabled and duplicated surface literals are removed. The central assertion compares computed body paint with the resolved --bg token, rather than accepting data-theme alone. Against the preserved faulty built artifact this stronger check fails with exit 1; the repaired current build passes. Production CSS and motion are unchanged by this repair.

Menu Docs sibling automatic keyboard plays could steal focus and leave an already-open popover. All six Menu interaction plays now recognize the same-origin manual Docs frame, leaving each example ready to explore. Canvas/Vitest keyboard behavior remains fully tested in both palettes. A readiness-aware check against the older immutable build fails on the open sibling; the repaired central suite passes. Adversarial review also preserved 25 proposed static assertions in both palettes, corrected the journey appearance to match the full route composition, and added guard regressions for overriding play properties. No confirmed finding remains unresolved.

Negative evidence: after-theme-policy/negative-surface-contract.log (exit 1 plus screenshot/trace), theme-contract/before-play-ready.log (old Menu built artifact exit 1), after-theme-policy/negative-interaction-policy.log (deliberately broken helper exit 1, restored exit 0), after-theme-policy/negative-missing-interaction-tag.log (copied index exit 1, real index restored exit 0). Original baseline artifacts were never regenerated. The earlier central failed lazy-frame check and shell readonly-status wrapper error are archived; the browser result itself passed and was re-recorded through the Node runner with exact exit 0.

Full per-entry theme disposition is included below. The authoritative generated Docs accounting prints 642 stories, 131 Docs, 516 full plays, 511 light full plays, five light appearance plays and 126 no-play entries. Current T3 records verify Menu ArrowDown selection, palette propagation without opening siblings, Escape focus return, and the full manual light creation journey ending on Billing platform in Projects. Matched original catalogue screenshots and follow-up screenshots are separately dated by their artifact phase.

## Exact changes and responsibility

- Normalized 115 existing modules to explicit Design system, Shared, Features/<domain> and Pages titles. Stories remain beside owners with recursive discovery. Explicit legacy meta.id preserves retained links. No custom storySort was added: the installed manager uses its default discovery/group order (Shared, Pages, Features, Design system), Docs first and CSF export order.
- Consolidated compatible Button and Badge variants into captioned galleries plus controls-driven examples. Removed 16 baseline cases only after retaining their visible state or moving the behavior assertion into Button/accessibility regressions. The invalid simultaneous Dialog gallery was replaced by its retained individual modal examples. The complete removal map is below.
- Added 16 owner modules covering missing distinct pages, PKI/certificate/admin panels, AWS mode fields, signup/refusal UI and shared composites. These add 74 published cases covering supported pending, empty, failure/retry, permission, busy, validation, long content and overlay states. Menu/native-popover keyboard stories remain because unit DOM emulation cannot prove their focus behavior.
- Exported the existing AppRoutes tree and rendered it through withApp and actual providers. Overview includes four named steps: overview to Projects, schema-valid creation with POST/201, return through the sidebar, revisit refreshed Projects. It awaits its real lazy module, not an arbitrary delay. Docs stays at the starting view through an explicit same-origin manual-frame check because installed Storybook 10.6 ignores iframe autoplay=false.
- Reused withApp/topLayerDocs. Fixtures install before mounts and match request methods, paths and required query values, reject duplicate query matches and unexpected requests, propagate Request init overrides and abort lifetimes, retire caches safely, and refuse inline app Docs. Deferred fixture body handling checks abort before storage mutation. Async handler, stale cleanup, pending request and manual-frame regressions cover the lifecycle.
- Added source-derived descriptions to 40 modules and accurate Button types/defaults/control descriptions. All 131 modules have effective documentation. React docgen finds 129 primary components; two are render-only modules. Forty-four parser-level empty descriptions are supplemented by explicit metadata. Native inherited props and controlled value/onChange callback contracts are documented separately from live automatic controls.
- Application and preview now share the font/token/app style barrel. Button.css owns nine button rule blocks; its import remains at the global owner for styled anchors, summaries and file-upload labels. Removed the unused .btn--disabled alias after dynamic-class search; native [disabled] remains. The normalized CSS rule/declaration ordering matches after that deliberate deletion. No token/reset/theme/utility purge occurred.
- Docs-only CSS contains wide generated prop tables at phone widths. The public Docs container subscribes to toolbar globals and publishes the theme to independent same-origin frames. Observers update CSS attributes without replacing iframe URLs, providers or form state. An owned facade retains the exported Storybook renderer for unchanged context/Docs parameters/element: the installed renderer otherwise resets ErrorBoundary key on every globals update. It delegates changed contexts, parameter identity, element, unmount and rejected-render retries, preserving navigation and HMR boundaries. Four owning regressions prove deduplication, reset and async failure handling. The canonical event is prebundled in the existing Storybook browser project to prevent initial optimizer reloads.
- The complete built-index guard uses source import paths and recursive modules, rejects malformed/empty indexes, stripped Docs, module opt-outs, unrelated same-title Docs, undiscovered nested modules and empty story files. The local build wrapper exports design assets once, builds once, and runs the guard. Existing CI invokes the guard after its build and retains both palette browser/design/subpath gates.
- Extended the existing design markup owner for demonstrated quoted/computed/template/cx/multiline shared-button bypasses, with regression cases. Native structural HTML stays allowed. CompactOrgRetention uses its existing Button owner. Production auth, secret handling, sensitivity inventory, design.pen exports and publishing boundaries remain intact.

## Baseline gaps and completed bounded plan

| Phase | Bounded outcome | Evidence |
| --- | --- | --- |
| Baseline/inventory | Archive current build and all-owner/module/route/state mappings before edits | before/ immutable builds, full inventory appendix, source line paths and exact extracted metadata |
| Responsibility/catalogue | Cover missing distinct owners, normalize titles with stable IDs, consolidate only proven redundant views | 131 module hashes; 30 routes; explicit removed-to-retained/test map; named real-route journey |
| Lifecycle/styles/gates | Repair shared owners and enforce full source-matched Docs without broad policy bans | withApp tests; shared style rule comparison; design bypass and negative Docs regressions |
| Measured optimization | Exclude generated transport-only docgen work and remove fake readiness delay | before/final profiles; docgen metadata parity; full palette tests |
| Visible verification | Compare matched Canvas/Docs, controls, keyboard, long content, phone, routes and theme/navigation lifetimes | T3 screenshots and structured browser evidence; final immutable build |

The exhaustive appendix separates baseline missing coverage, stale metadata/commands, redundant prop/behavior entries, retained meaningful states and justified exclusions. Final app inventory: 304 JSX-rendering symbols, 136 directly storied and 166 composed; App bootstrap and AuthProvider infrastructure are the two explicit unmapped exclusions. Thirty registered routes are mapped. Composite relationships do not assert every optional private branch or real backend integration was exercised.

## Browser proof and failure diagnosis

T3 compared the same Button gallery, API-driven Projects populated view and Dialog pinned-actions state in Canvas and Docs at 1280x800 and 390x844. Browser readiness uses render/play phase, fonts and observed content; lazy Docs frames are scrolled into view and awaited individually. Finite animations are finished for capture only, with production motion untouched. Phone means CSS viewport on desktop Chromium, not physical-device or authenticator proof.

The Projects PNG files were additionally checked for visible pixel content and matching hashes; apparent duplicate-image display blanks in the tool were not empty source captures. Intended visible changes are captioned galleries, descriptions/control tables, contained phone Docs overflow, the invalid modal gallery removal, meaningful new modules and correct light Docs surfaces. The baseline No Preview heading is hidden framework markup, not a visible Docs failure; actual screenshot content and .sbdocs-content heading are used. Computed button properties and normalized CSS precedence agree for unaffected Projects/Dialog states. Dynamic font/paint timing is excluded from visual claims.

Button live controls update variant and disabled state; sidebar hierarchy and Docs-first installed order were checked. Menu Enter/ArrowDown/Escape reaches Rename, Duplicate and restores Row actions focus. Long dialog footer actions stay reachable at both widths. Actual app prototype Projects and organization-settings consumers were exercised, including styled anchor/file-label siblings. Prototype instance-config returns an existing unsupported-fixture 501, so this is frontend route proof, not a real backend claim.

Overview Docs examples were manually explored and a project created in one frame while the other four retained their independent views. Repeated manager navigation Populated to LoadError to Button to Populated resets the right fixture state in the same preview frame. The final theme proof records all framed API examples following Light/Dark while an unsaved input and iframe/input identities survive. The initial CSS-only repair exposed a separate renderer remount: colors changed but drafts disappeared. The owned renderer facade repairs that visible lifecycle bug and its first failed proof remains archived. OpenPencil toolbar links and four PNG exports are retained and served under /storybook/design/; the external OS design application was not launched.

Negative proofs: deliberately disabling query matching made its representative regression fail with exit 1; restoring it passed. A copied built index with every Docs entry stripped fails the actual guard with exit 1; the immutable real build remains intact. The checker has regressions for actual shared-control bypass forms. No implementation is left deliberately broken.

Failures are preserved separately: initial missing installs/wrong local Node prevented bootstrap checks; a later cold Overview lazy-route load surfaced a real Loading fallback race, repaired through module readiness and aborted-body mutation coverage. The first theme-enabled cold dark run passed 564 tests but 11 suites failed import after Vite discovered storybook/internal/core-events and reloaded active setup modules. Narrow prebundling repairs that demonstrated reload. A later run stopped before tests with a Vitest WebSocket connection error after its optimizer completed, and was interrupted after 449 seconds. Its exact startup trigger remains unproved; installed Vitest waits for that connection before execution. The dependency cache was preserved and a fresh-cache full dark retry passed all 642 stories in 48.94 seconds, with no optimizer reload. Light passed all 642 in 56.25 seconds. This is a remaining runner-startup evidence limit, not a story assertion failure. An upstream related lifecycle issue is documented at https://github.com/vitest-dev/vitest/issues/10791 ; it is analogous evidence, not proof of the same trigger. Earlier draft type errors were repaired and final checks rerun. T3 invalid locator/readiness probes were operator/tool errors, not product test flakes. No assertion deadline or accessibility rule was weakened.

## Review and evidence limits

Local source review and targeted independent review findings were resolved. Current T3 Storybook verification completed before the browser host disconnected during the offline-report check. The self-contained report is verified from file:// with the installed Chromium after the explicit T3 unavailable-host result. No network assets are needed. Cross-provider review: SKIPPED, because WORKSTYLE disables unavailable Claude subscription and paid API review without authorization. Remote CI and delivery were not run at this authorized endpoint. Real WebAuthn prompts, popup identity authority, federation success, OS clipboard permission, production secret access/private-key downloads and full live-backend flows remain explicit human/integration boundaries. Automatic control metadata does not prove every controlled-composite callback scenario; source contracts and representative live controls are distinguished in the appendix.

## Reproduce and inspect

Installed Node is 26.7.0, matching .nvmrc. Client/web frozen installs were repaired without lockfile changes. From the repository root:

```sh
export PATH=/Users/developwent/.local/share/fnm/node-versions/v26.7.0/installation/bin:$PATH
cd web
node --run typecheck
node --run lint
node --run design:check
node --run test
node --run build
node --run build-storybook -- --output-dir .artifacts/storybook-improvement/after-theme-surface/storybook-static
node --run check:storybook-docs -- .artifacts/storybook-improvement/after-theme-surface/storybook-static
node --run test-storybook
STORYBOOK_INTERACTION_PASS=full node --run test-storybook:light
node --run test-storybook:light
STORYBOOK_STATIC_DIR=.artifacts/storybook-improvement/after-theme-surface/storybook-static node --run test-storybook:theme
node scripts/storybook/measure.mjs .artifacts/storybook-improvement/after-theme-surface/app-dist .artifacts/storybook-improvement/after-theme-surface/storybook-static
```

Run expensive suites sequentially. The final review server serves an immutable directory, so rebuilding another output cannot change its assets mid-check. To start a second review server on a free port, from web/:

```sh
node .artifacts/storybook-improvement/serve.mjs .artifacts/storybook-improvement/after-theme-surface/storybook-static 6021
```

Expected: serving on 0.0.0.0:6021, LAN /storybook/ responds 200, /storybook redirects 308, /storybook/index.json has 642 stories and 131 Docs. Use the current LAN IP if it changes. Stop only the process you started when the review window ends.

| Artifact owner | Path under web/.artifacts/storybook-improvement/ |
| --- | --- |
| Immutable baseline | before/storybook-static/; before/app-dist/; before/*logs; before/measurements-final.json |
| Final immutable build/checks/metrics | after-theme-surface/storybook-static/; after-theme-surface/app-dist/; after-theme-surface/checks.json; after-theme-surface/measurements.json; all eleven logs |
| Same-source cost repeats | after-build-repeat/build-storybook.log; after-build-repeat/checks.json; final docgen-helper-profile.json; after-theme-policy/checks.json; final full-light reference log |
| Theme policy and negative proofs | theme-play-audit.json; theme-interaction-policy.md/.html; after-theme-policy/negative-*.log and restored-*.log; theme-contract/before-play-ready.log; final t3-theme-followup.json |
| Exhaustive source/metadata maps | inventory/before.json; after.json; before-docgen.json; after-docgen.json; manifest-verification.log |
| Catalogue migrations | catalogue-title-migrations.json; removed-story-map.json; inventory/catalogue-delta.json |
| Before/after sidebar | inventory/before-sidebar.txt; inventory/after-sidebar.txt (full copies below) |
| CSS/style proof | after/css-rule-comparison.json; after-optimized/computed-style-comparison.json |
| Failure/negative proof | after/negative-query-match.log; after/restored-query-match.log; after-optimized/negative-no-docs.log; after-complete/storybook-dark.log |
| T3 snapshots/state records | after-theme-surface/http-proof.json; after-theme-surface/t3-theme-followup.json; after-complete3/http-proof.json; after-complete3/t3-final-manual-docs.json; before/t3-matched-*; after-optimized/t3-matched-*; after-reviewed/t3-manual-docs-journey.json, t3-phone-journey.json, t3-repeated-navigation.json; final t3-* files; after-complete2/t3-theme-remount-failure.json |
| Durable machine inventory | ../../../docs/handoff/storybook-improvement-inventory.json |

This evidence predates commit and PR delivery. Artifact builds/logs/screenshots are gitignored; the Markdown, offline HTML, authoring README and machine inventory are local review documents. Offline HTML includes the entire audit and images, with no server or network dependency.

## Matched screenshots

These matching captures preserve the original before and the after-complete3 catalogue-cleanup comparison. They are not relabeled as new theme-policy captures. Current follow-up Menu and real-route journey screenshots appear alongside them below; the new gate also verifies actual palette surface paint.

![Before: button canvas, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/before-canvas-button-390.png)

![After catalogue cleanup: button canvas, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/after-catalogue-cleanup-canvas-button-390.png)

![Before: button docs, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/before-docs-button-390.png)

![After catalogue cleanup: button docs, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/after-catalogue-cleanup-docs-button-390.png)

![Before: projects canvas, 1280px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/before-canvas-projects-1280.png)

![After catalogue cleanup: projects canvas, 1280px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/after-catalogue-cleanup-canvas-projects-1280.png)

![Before: dialog canvas, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/before-canvas-dialog-390.png)

![After catalogue cleanup: dialog canvas, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/after-catalogue-cleanup-canvas-dialog-390.png)

![Before: dialog docs, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/before-docs-dialog-390.png)

![After catalogue cleanup: dialog docs, 390px](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/after-catalogue-cleanup-docs-dialog-390.png)

## Current theme follow-up screenshots

![Current Menu Docs keyboard exploration, desktop](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/theme-current-menuDesktop.png)

![Current Menu Docs keyboard exploration, phone Light](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/theme-current-menuPhone.png)

![Current full manual Light real-route journey, phone](/Users/developwent/.t3/worktrees/wenv/t3-d7021d3b/web/.artifacts/storybook-improvement/screenshots/theme-current-overviewPhone.png)

## Full theme interaction disposition

Full 642-entry accounting:

| Story ID | Source | Disposition |
| --- | --- | --- |
| pages-adapters--empty | web/src/routes/Adapters.stories.tsx:29 | Full play both; readiness/state/style preserved |
| pages-adapters--failed | web/src/routes/Adapters.stories.tsx:30 | Full play both; readiness/state/style preserved |
| pages-adapters--adding | web/src/routes/Adapters.stories.tsx:31 | Full play both; readiness/state/style preserved |
| features-adapters-awsaccessfields--assume-role | web/src/routes/AwsAccessFields.stories.tsx:20 | Render and a11y both |
| features-adapters-awsaccessfields--web-identity | web/src/routes/AwsAccessFields.stories.tsx:21 | Render and a11y both |
| features-adapters-awsaccessfields--ambient | web/src/routes/AwsAccessFields.stories.tsx:22 | Render and a11y both |
| features-adapters-awsaccessfields--static | web/src/routes/AwsAccessFields.stories.tsx:23 | Render and a11y both |
| features-pki-certificatestab--populated | web/src/routes/CertificatesTab.stories.tsx:29 | Render and a11y both |
| features-pki-certificatestab--empty | web/src/routes/CertificatesTab.stories.tsx:30 | Render and a11y both |
| features-pki-certificatestab--loading | web/src/routes/CertificatesTab.stories.tsx:31 | Render and a11y both |
| features-pki-certificatestab--incomplete | web/src/routes/CertificatesTab.stories.tsx:32 | Render and a11y both |
| features-pki-certificatestab--issue-from-csr | web/src/routes/CertificatesTab.stories.tsx:33 | Full play both; readiness/state/style preserved |
| features-pki-certificatestab--revoke-confirmation | web/src/routes/CertificatesTab.stories.tsx:38 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--configured | web/src/routes/InstanceMailPanel.stories.tsx:16 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--unconfigured | web/src/routes/InstanceMailPanel.stories.tsx:17 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--loading | web/src/routes/InstanceMailPanel.stories.tsx:18 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--second-factor-required | web/src/routes/InstanceMailPanel.stories.tsx:19 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--failed | web/src/routes/InstanceMailPanel.stories.tsx:20 | Full play both; readiness/state/style preserved |
| features-instance-instancemailpanel--fresh-proof-required | web/src/routes/InstanceMailPanel.stories.tsx:21 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--populated | web/src/routes/OAuth2ProvidersPanel.stories.tsx:19 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--empty | web/src/routes/OAuth2ProvidersPanel.stories.tsx:20 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--loading | web/src/routes/OAuth2ProvidersPanel.stories.tsx:21 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--second-factor-required | web/src/routes/OAuth2ProvidersPanel.stories.tsx:22 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--failed | web/src/routes/OAuth2ProvidersPanel.stories.tsx:23 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--reconfiguring | web/src/routes/OAuth2ProvidersPanel.stories.tsx:24 | Full play both; readiness/state/style preserved |
| features-identity-oauth2providerspanel--delete-confirmation | web/src/routes/OAuth2ProvidersPanel.stories.tsx:29 | Full play both; readiness/state/style preserved |
| features-members-openregistration--closed | web/src/routes/OpenRegistration.stories.tsx:26 | Full play both; readiness/state/style preserved |
| features-members-openregistration--active | web/src/routes/OpenRegistration.stories.tsx:27 | Render and a11y both |
| features-members-openregistration--inactive-authority | web/src/routes/OpenRegistration.stories.tsx:28 | Full play both; readiness/state/style preserved |
| features-members-openregistration--loading | web/src/routes/OpenRegistration.stories.tsx:29 | Full play both; readiness/state/style preserved |
| features-members-openregistration--failed | web/src/routes/OpenRegistration.stories.tsx:30 | Full play both; readiness/state/style preserved |
| features-members-openregistration--editor-validation | web/src/routes/OpenRegistration.stories.tsx:31 | Full play both; readiness/state/style preserved |
| pages-overview--empty | web/src/routes/Overview.stories.tsx:42 | Full play both; readiness/state/style preserved |
| pages-overview--populated | web/src/routes/Overview.stories.tsx:51 | Full play both; readiness/state/style preserved |
| pages-overview--long-names | web/src/routes/Overview.stories.tsx:59 | Full play both; readiness/state/style preserved |
| pages-overview--create-project-journey | web/src/routes/Overview.stories.tsx:67 | Explicit visible appearance + behavior once |
| features-pki-pkiissuerspanel--populated | web/src/routes/PkiIssuersPanel.stories.tsx:16 | Full play both; readiness/state/style preserved |
| features-pki-pkiissuerspanel--empty | web/src/routes/PkiIssuersPanel.stories.tsx:21 | Full play both; readiness/state/style preserved |
| features-pki-pkiissuerspanel--loading | web/src/routes/PkiIssuersPanel.stories.tsx:25 | Full play both; readiness/state/style preserved |
| features-pki-pkiissuerspanel--second-factor-required | web/src/routes/PkiIssuersPanel.stories.tsx:29 | Full play both; readiness/state/style preserved |
| features-pki-pkiissuerspanel--failed | web/src/routes/PkiIssuersPanel.stories.tsx:33 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--populated | web/src/routes/PkiProfilesPanel.stories.tsx:16 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--empty | web/src/routes/PkiProfilesPanel.stories.tsx:21 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--loading | web/src/routes/PkiProfilesPanel.stories.tsx:25 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--second-factor-required | web/src/routes/PkiProfilesPanel.stories.tsx:29 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--failed | web/src/routes/PkiProfilesPanel.stories.tsx:33 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--narrowing | web/src/routes/PkiProfilesPanel.stories.tsx:37 | Full play both; readiness/state/style preserved |
| features-pki-pkiprofilespanel--invalid-policy | web/src/routes/PkiProfilesPanel.stories.tsx:44 | Full play both; readiness/state/style preserved |
| pages-remotes--empty | web/src/routes/Remotes.stories.tsx:26 | Full play both; readiness/state/style preserved |
| pages-remotes--long-content | web/src/routes/Remotes.stories.tsx:27 | Full play both; readiness/state/style preserved |
| pages-remotes--loading-directory | web/src/routes/Remotes.stories.tsx:28 | Full play both; readiness/state/style preserved |
| pages-remotes--permission-required | web/src/routes/Remotes.stories.tsx:29 | Full play both; readiness/state/style preserved |
| pages-samldone--missing-transaction | web/src/routes/SAMLDone.stories.tsx:22 | Full play both; readiness/state/style preserved |
| features-ssh-sshcertificatespanel--empty | web/src/routes/SSHCertificates.stories.tsx:24 | Full play both; readiness/state/style preserved |
| features-ssh-sshcertificatespanel--no-environments | web/src/routes/SSHCertificates.stories.tsx:25 | Render and a11y both |
| features-ssh-sshcertificatespanel--session-required | web/src/routes/SSHCertificates.stories.tsx:26 | Render and a11y both |
| features-ssh-sshcertificatespanel--populated | web/src/routes/SSHCertificates.stories.tsx:27 | Full play both; readiness/state/style preserved |
| features-ssh-sshcertificatespanel--refused | web/src/routes/SSHCertificates.stories.tsx:28 | Full play both; readiness/state/style preserved |
| features-ssh-sshcertificatespanel--create-ca | web/src/routes/SSHCertificates.stories.tsx:29 | Full play both; readiness/state/style preserved |
| pages-signupverify--entry | web/src/routes/SignupVerify.stories.tsx:20 | Render and a11y both |
| pages-signupverify--fresh-organisation | web/src/routes/SignupVerify.stories.tsx:21 | Render and a11y both |
| pages-signupverify--missing-link | web/src/routes/SignupVerify.stories.tsx:22 | Render and a11y both |
| pages-signupverify--validation | web/src/routes/SignupVerify.stories.tsx:23 | Full play both; readiness/state/style preserved |
| pages-signupverify--busy | web/src/routes/SignupVerify.stories.tsx:36 | Full play both; readiness/state/style preserved |
| pages-signupverify--refused | web/src/routes/SignupVerify.stories.tsx:46 | Full play both; readiness/state/style preserved |
| shared-workspacesettingslink--this-instance | web/src/routes/WorkspaceSettingsLink.stories.tsx:18 | Full play both; readiness/state/style preserved |
| shared-workspacesettingslink--destination-instance | web/src/routes/WorkspaceSettingsLink.stories.tsx:19 | Full play both; readiness/state/style preserved |
| design-system-ceremonynotice--default | web/src/ui/CeremonyNotice.stories.tsx:14 | Render and a11y both |
| design-system-ceremonynotice--long-content | web/src/ui/CeremonyNotice.stories.tsx:15 | Render and a11y both |
| design-system-auth-localsignupform--entry | web/src/ui/auth/LocalSignupForm.stories.tsx:20 | Render and a11y both |
| design-system-auth-localsignupform--no-organisation | web/src/ui/auth/LocalSignupForm.stories.tsx:21 | Render and a11y both |
| design-system-auth-localsignupform--request-and-resend | web/src/ui/auth/LocalSignupForm.stories.tsx:22 | Full play both; readiness/state/style preserved |
| design-system-auth-localsignupform--busy | web/src/ui/auth/LocalSignupForm.stories.tsx:36 | Full play both; readiness/state/style preserved |
| design-system-auth-localsignupform--refused | web/src/ui/auth/LocalSignupForm.stories.tsx:45 | Full play both; readiness/state/style preserved |
| app-runtimemaintenanceboundary--maintenance | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:37 | Full play both; readiness/state/style preserved |
| app-runtimemaintenanceboundary--recovery-required | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:49 | Full play both; readiness/state/style preserved |
| app-runtimemaintenanceboundary--reconnecting | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:63 | Full play both; readiness/state/style preserved |
| app-notifications--failure | web/src/app/notifications.stories.tsx:43 | Render and a11y both |
| app-notifications--success | web/src/app/notifications.stories.tsx:44 | Render and a11y both |
| app-notifications--info | web/src/app/notifications.stories.tsx:45 | Render and a11y both |
| app-notifications--dismisses | web/src/app/notifications.stories.tsx:47 | Full play both; readiness/state/style preserved |
| routes-accountprofile--editable | web/src/routes/AccountProfile.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-accountprofile--proof-required | web/src/routes/AccountProfile.stories.tsx:47 | Full play both; readiness/state/style preserved |
| routes-accountprofile--managed | web/src/routes/AccountProfile.stories.tsx:58 | Full play both; readiness/state/style preserved |
| routes-accountprofile--provider-sign-in | web/src/routes/AccountProfile.stories.tsx:70 | Full play both; readiness/state/style preserved |
| routes-accountprofile--loading | web/src/routes/AccountProfile.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-accountprofile--failed | web/src/routes/AccountProfile.stories.tsx:90 | Full play both; readiness/state/style preserved |
| routes-accountsecurity--populated | web/src/routes/AccountSecurity.stories.tsx:155 | Full play both; readiness/state/style preserved |
| routes-accountsecurity--empty | web/src/routes/AccountSecurity.stories.tsx:167 | Full play both; readiness/state/style preserved |
| routes-accountsecurity--loading | web/src/routes/AccountSecurity.stories.tsx:194 | Full play both; readiness/state/style preserved |
| routes-accountsecurity--failed | web/src/routes/AccountSecurity.stories.tsx:204 | Full play both; readiness/state/style preserved |
| routes-accountsecurity--proof-dialog | web/src/routes/AccountSecurity.stories.tsx:223 | Full play both; readiness/state/style preserved |
| routes-audit--populated | web/src/routes/Audit.stories.tsx:53 | Full play both; readiness/state/style preserved |
| routes-audit--empty | web/src/routes/Audit.stories.tsx:68 | Full play both; readiness/state/style preserved |
| routes-audit--loading | web/src/routes/Audit.stories.tsx:83 | Full play both; readiness/state/style preserved |
| routes-audit--failed | web/src/routes/Audit.stories.tsx:98 | Full play both; readiness/state/style preserved |
| routes-bindingcard--default | web/src/routes/BindingCard.stories.tsx:46 | Full play both; readiness/state/style preserved |
| routes-bindingcard--expiring-soon | web/src/routes/BindingCard.stories.tsx:56 | Full play both; readiness/state/style preserved |
| routes-bindingcard--quarantined | web/src/routes/BindingCard.stories.tsx:65 | Full play both; readiness/state/style preserved |
| routes-bindingcard--not-ready | web/src/routes/BindingCard.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--default | web/src/routes/BindingDialog.stories.tsx:81 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--git-hub-actions | web/src/routes/BindingDialog.stories.tsx:95 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--pull-request-event | web/src/routes/BindingDialog.stories.tsx:106 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--audience-required | web/src/routes/BindingDialog.stories.tsx:119 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--replace | web/src/routes/BindingDialog.stories.tsx:129 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--busy | web/src/routes/BindingDialog.stories.tsx:142 | Full play both; readiness/state/style preserved |
| routes-bindingdialog--failed | web/src/routes/BindingDialog.stories.tsx:154 | Full play both; readiness/state/style preserved |
| routes-clireauth--disclosure | web/src/routes/CLIReauth.stories.tsx:123 | Full play both; readiness/state/style preserved |
| routes-clireauth--disclosure-oidc | web/src/routes/CLIReauth.stories.tsx:141 | Full play both; readiness/state/style preserved |
| routes-clireauth--adapter | web/src/routes/CLIReauth.stories.tsx:154 | Full play both; readiness/state/style preserved |
| routes-clireauth--self-config | web/src/routes/CLIReauth.stories.tsx:168 | Full play both; readiness/state/style preserved |
| routes-clireauth--loading | web/src/routes/CLIReauth.stories.tsx:179 | Full play both; readiness/state/style preserved |
| routes-clireauth--failed | web/src/routes/CLIReauth.stories.tsx:188 | Full play both; readiness/state/style preserved |
| routes-clireauth--nothing-to-authorize | web/src/routes/CLIReauth.stories.tsx:201 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--populated | web/src/routes/CatalogueManageDialog.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--empty | web/src/routes/CatalogueManageDialog.stories.tsx:92 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--loading | web/src/routes/CatalogueManageDialog.stories.tsx:101 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--failed | web/src/routes/CatalogueManageDialog.stories.tsx:110 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--read-only | web/src/routes/CatalogueManageDialog.stories.tsx:119 | Full play both; readiness/state/style preserved |
| routes-cataloguemanagedialog--create-refused | web/src/routes/CatalogueManageDialog.stories.tsx:130 | Full play both; readiness/state/style preserved |
| routes-ceremony--protected | web/src/routes/Ceremony.stories.tsx:67 | Full play both; readiness/state/style preserved |
| routes-ceremony--code-offered | web/src/routes/Ceremony.stories.tsx:78 | Full play both; readiness/state/style preserved |
| routes-ceremony--oidc | web/src/routes/Ceremony.stories.tsx:88 | Full play both; readiness/state/style preserved |
| routes-ceremony--refused | web/src/routes/Ceremony.stories.tsx:101 | Full play both; readiness/state/style preserved |
| routes-changeapprovals--populated | web/src/routes/ChangeApprovals.stories.tsx:109 | Full play both; readiness/state/style preserved |
| routes-changeapprovals--empty | web/src/routes/ChangeApprovals.stories.tsx:126 | Full play both; readiness/state/style preserved |
| routes-changeapprovals--loading | web/src/routes/ChangeApprovals.stories.tsx:141 | Full play both; readiness/state/style preserved |
| routes-changeapprovals--failed | web/src/routes/ChangeApprovals.stories.tsx:156 | Full play both; readiness/state/style preserved |
| routes-chromeidentitycontrols--org-identity | web/src/routes/ChromeIdentityControls.stories.tsx:26 | Full play both; readiness/state/style preserved |
| routes-chromeidentitycontrols--project-identity | web/src/routes/ChromeIdentityControls.stories.tsx:35 | Full play both; readiness/state/style preserved |
| routes-compactorgretention--default | web/src/routes/CompactOrgRetention.stories.tsx:24 | Full play both; readiness/state/style preserved |
| routes-compactorgretention--busy | web/src/routes/CompactOrgRetention.stories.tsx:32 | Full play both; readiness/state/style preserved |
| routes-compactorgretention--refused | web/src/routes/CompactOrgRetention.stories.tsx:40 | Full play both; readiness/state/style preserved |
| routes-connectionmintdialog--default | web/src/routes/ConnectionMintDialog.stories.tsx:28 | Full play both; readiness/state/style preserved |
| routes-connectionmintdialog--clamped | web/src/routes/ConnectionMintDialog.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-connectionrow--live | web/src/routes/ConnectionRow.stories.tsx:45 | Full play both; readiness/state/style preserved |
| routes-connectionrow--expired | web/src/routes/ConnectionRow.stories.tsx:54 | Full play both; readiness/state/style preserved |
| routes-connectionrow--revoked | web/src/routes/ConnectionRow.stories.tsx:63 | Full play both; readiness/state/style preserved |
| routes-connectionrow--indefinite | web/src/routes/ConnectionRow.stories.tsx:81 | Full play both; readiness/state/style preserved |
| routes-createaccountdialog--default | web/src/routes/CreateAccountDialog.stories.tsx:33 | Full play both; readiness/state/style preserved |
| routes-createaccountdialog--name-required | web/src/routes/CreateAccountDialog.stories.tsx:42 | Full play both; readiness/state/style preserved |
| routes-createaccountdialog--busy | web/src/routes/CreateAccountDialog.stories.tsx:50 | Full play both; readiness/state/style preserved |
| routes-createaccountdialog--failed | web/src/routes/CreateAccountDialog.stories.tsx:62 | Full play both; readiness/state/style preserved |
| routes-createproviderdialog--default | web/src/routes/CreateProviderDialog.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-createproviderdialog--fields-required | web/src/routes/CreateProviderDialog.stories.tsx:53 | Full play both; readiness/state/style preserved |
| routes-createproviderdialog--busy | web/src/routes/CreateProviderDialog.stories.tsx:63 | Full play both; readiness/state/style preserved |
| routes-createproviderdialog--failed | web/src/routes/CreateProviderDialog.stories.tsx:75 | Full play both; readiness/state/style preserved |
| routes-credentialform--default | web/src/routes/CredentialForm.stories.tsx:22 | Full play both; readiness/state/style preserved |
| routes-credentialform--entered | web/src/routes/CredentialForm.stories.tsx:30 | Full play both; readiness/state/style preserved |
| routes-credentialform--busy | web/src/routes/CredentialForm.stories.tsx:41 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--row | web/src/routes/DefinitionsBundlePanel.stories.tsx:138 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--dialog-open | web/src/routes/DefinitionsBundlePanel.stories.tsx:146 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--git-read-only | web/src/routes/DefinitionsBundlePanel.stories.tsx:155 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--checked | web/src/routes/DefinitionsBundlePanel.stories.tsx:165 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--planned | web/src/routes/DefinitionsBundlePanel.stories.tsx:176 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--busy | web/src/routes/DefinitionsBundlePanel.stories.tsx:190 | Full play both; readiness/state/style preserved |
| routes-definitionsbundlepanel--refused | web/src/routes/DefinitionsBundlePanel.stories.tsx:202 | Full play both; readiness/state/style preserved |
| routes-deleteaccountdialog--default | web/src/routes/DeleteAccountDialog.stories.tsx:42 | Full play both; readiness/state/style preserved |
| routes-deleteaccountdialog--one-credential | web/src/routes/DeleteAccountDialog.stories.tsx:53 | Full play both; readiness/state/style preserved |
| routes-deleteaccountdialog--busy | web/src/routes/DeleteAccountDialog.stories.tsx:61 | Full play both; readiness/state/style preserved |
| routes-deleteaccountdialog--failed | web/src/routes/DeleteAccountDialog.stories.tsx:71 | Full play both; readiness/state/style preserved |
| routes-deleteaccountdialog--failed-may-have-committed | web/src/routes/DeleteAccountDialog.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-deleteadapterdialog--default | web/src/routes/DeleteAdapterDialog.stories.tsx:38 | Full play both; readiness/state/style preserved |
| routes-deleteadapterdialog--prune | web/src/routes/DeleteAdapterDialog.stories.tsx:46 | Full play both; readiness/state/style preserved |
| routes-deleteadapterdialog--busy | web/src/routes/DeleteAdapterDialog.stories.tsx:57 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--no-leases | web/src/routes/DeleteProviderDialog.stories.tsx:45 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--live-leases | web/src/routes/DeleteProviderDialog.stories.tsx:58 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--leases-unknown | web/src/routes/DeleteProviderDialog.stories.tsx:71 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--busy | web/src/routes/DeleteProviderDialog.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--conflict | web/src/routes/DeleteProviderDialog.stories.tsx:93 | Full play both; readiness/state/style preserved |
| routes-deleteproviderdialog--failed | web/src/routes/DeleteProviderDialog.stories.tsx:106 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--reported-healthy | web/src/routes/DeliveryTargets.stories.tsx:33 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--reported-degraded | web/src/routes/DeliveryTargets.stories.tsx:42 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--stale | web/src/routes/DeliveryTargets.stories.tsx:64 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--refused | web/src/routes/DeliveryTargets.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--reporter-revoked | web/src/routes/DeliveryTargets.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--quota-refused | web/src/routes/DeliveryTargets.stories.tsx:90 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--unknown | web/src/routes/DeliveryTargets.stories.tsx:100 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--empty | web/src/routes/DeliveryTargets.stories.tsx:109 | Full play both; readiness/state/style preserved |
| routes-deliverytargets--unsupported | web/src/routes/DeliveryTargets.stories.tsx:118 | Full play both; readiness/state/style preserved |
| routes-enrolmentgate--password | web/src/routes/EnrolmentGate.stories.tsx:46 | Full play both; readiness/state/style preserved |
| routes-enrolmentgate--password-refused | web/src/routes/EnrolmentGate.stories.tsx:56 | Full play both; readiness/state/style preserved |
| routes-establishcredential--initial | web/src/routes/EstablishCredential.stories.tsx:60 | Full play both; readiness/state/style preserved |
| routes-establishcredential--mismatch | web/src/routes/EstablishCredential.stories.tsx:69 | Full play both; readiness/state/style preserved |
| routes-establishcredential--refused | web/src/routes/EstablishCredential.stories.tsx:80 | Full play both; readiness/state/style preserved |
| routes-establishcredential--done | web/src/routes/EstablishCredential.stories.tsx:92 | Full play both; readiness/state/style preserved |
| routes-establishcredential--recover | web/src/routes/EstablishCredential.stories.tsx:102 | Full play both; readiness/state/style preserved |
| routes-establishcredential--recovered | web/src/routes/EstablishCredential.stories.tsx:112 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--populated | web/src/routes/FederationIssuersPanel.stories.tsx:75 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--empty | web/src/routes/FederationIssuersPanel.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--loading | web/src/routes/FederationIssuersPanel.stories.tsx:94 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--second-factor-required | web/src/routes/FederationIssuersPanel.stories.tsx:102 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--failed | web/src/routes/FederationIssuersPanel.stories.tsx:111 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--creating | web/src/routes/FederationIssuersPanel.stories.tsx:120 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--editing | web/src/routes/FederationIssuersPanel.stories.tsx:132 | Full play both; readiness/state/style preserved |
| routes-federationissuerspanel--delete-refused | web/src/routes/FederationIssuersPanel.stories.tsx:146 | Full play both; readiness/state/style preserved |
| routes-fleetupdatenotice--local-update | web/src/routes/FleetUpdateNotice.stories.tsx:59 | Full play both; readiness/state/style preserved |
| routes-fleetupdatenotice--remote-update | web/src/routes/FleetUpdateNotice.stories.tsx:88 | Full play both; readiness/state/style preserved |
| routes-fleetupdatenotice--multiple-updates | web/src/routes/FleetUpdateNotice.stories.tsx:120 | Full play both; readiness/state/style preserved |
| routes-fleetupdatenotice--no-updates | web/src/routes/FleetUpdateNotice.stories.tsx:156 | Full play both; readiness/state/style preserved |
| routes-foldercleanupdialog--default | web/src/routes/FolderCleanupDialog.stories.tsx:38 | Full play both; readiness/state/style preserved |
| routes-foldercleanupdialog--widening-refused | web/src/routes/FolderCleanupDialog.stories.tsx:51 | Full play both; readiness/state/style preserved |
| routes-foldercleanupdialog--busy | web/src/routes/FolderCleanupDialog.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-grantdialog--read-grant | web/src/routes/GrantDialog.stories.tsx:95 | Full play both; readiness/state/style preserved |
| routes-grantdialog--reveal-grant | web/src/routes/GrantDialog.stories.tsx:107 | Full play both; readiness/state/style preserved |
| routes-grantdialog--nothing-to-widen | web/src/routes/GrantDialog.stories.tsx:121 | Full play both; readiness/state/style preserved |
| routes-grantdialog--catalogue-failed | web/src/routes/GrantDialog.stories.tsx:132 | Full play both; readiness/state/style preserved |
| routes-grantdialog--catalogue-loading | web/src/routes/GrantDialog.stories.tsx:141 | Full play both; readiness/state/style preserved |
| routes-grantdialog--reporting-grantable | web/src/routes/GrantDialog.stories.tsx:153 | Full play both; readiness/state/style preserved |
| routes-grantdialog--reporting-not-grantable | web/src/routes/GrantDialog.stories.tsx:175 | Full play both; readiness/state/style preserved |
| routes-grantdialog--report-after-the-fact | web/src/routes/GrantDialog.stories.tsx:192 | Full play both; readiness/state/style preserved |
| routes-grantdialog--reporting-unsupported | web/src/routes/GrantDialog.stories.tsx:219 | Full play both; readiness/state/style preserved |
| routes-healthchip--never | web/src/routes/HealthChip.stories.tsx:57 | Full play both; readiness/state/style preserved |
| routes-healthchip--pending | web/src/routes/HealthChip.stories.tsx:64 | Full play both; readiness/state/style preserved |
| routes-healthchip--converging | web/src/routes/HealthChip.stories.tsx:71 | Full play both; readiness/state/style preserved |
| routes-healthchip--converged | web/src/routes/HealthChip.stories.tsx:78 | Full play both; readiness/state/style preserved |
| routes-healthchip--degraded | web/src/routes/HealthChip.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-healthchip--failed | web/src/routes/HealthChip.stories.tsx:92 | Full play both; readiness/state/style preserved |
| routes-healthchip--paused | web/src/routes/HealthChip.stories.tsx:99 | Full play both; readiness/state/style preserved |
| routes-healthchip--drift-attention | web/src/routes/HealthChip.stories.tsx:107 | Full play both; readiness/state/style preserved |
| routes-historydrawer--populated | web/src/routes/HistoryDrawer.stories.tsx:214 | Full play both; readiness/state/style preserved |
| routes-historydrawer--key-filter | web/src/routes/HistoryDrawer.stories.tsx:228 | Full play both; readiness/state/style preserved |
| routes-historydrawer--pin-sheet-open | web/src/routes/HistoryDrawer.stories.tsx:239 | Full play both; readiness/state/style preserved |
| routes-historydrawer--restore-sheet-open | web/src/routes/HistoryDrawer.stories.tsx:248 | Full play both; readiness/state/style preserved |
| routes-historydrawer--empty | web/src/routes/HistoryDrawer.stories.tsx:257 | Full play both; readiness/state/style preserved |
| routes-historydrawer--loading | web/src/routes/HistoryDrawer.stories.tsx:266 | Full play both; readiness/state/style preserved |
| routes-historydrawer--failed | web/src/routes/HistoryDrawer.stories.tsx:274 | Full play both; readiness/state/style preserved |
| routes-historydrawer--phone | web/src/routes/HistoryDrawer.stories.tsx:282 | Full play both; readiness/state/style preserved |
| routes-historydrawer--phone-detail | web/src/routes/HistoryDrawer.stories.tsx:292 | Full play both; readiness/state/style preserved |
| routes-importwizard--pick | web/src/routes/ImportWizard.stories.tsx:135 | Full play both; readiness/state/style preserved |
| routes-importwizard--dotenv-source | web/src/routes/ImportWizard.stories.tsx:143 | Full play both; readiness/state/style preserved |
| routes-importwizard--invalid-lines | web/src/routes/ImportWizard.stories.tsx:152 | Full play both; readiness/state/style preserved |
| routes-importwizard--connector-source | web/src/routes/ImportWizard.stories.tsx:162 | Full play both; readiness/state/style preserved |
| routes-importwizard--cli-guidance | web/src/routes/ImportWizard.stories.tsx:171 | Full play both; readiness/state/style preserved |
| routes-importwizard--classify | web/src/routes/ImportWizard.stories.tsx:179 | Full play both; readiness/state/style preserved |
| routes-importwizard--review | web/src/routes/ImportWizard.stories.tsx:188 | Full play both; readiness/state/style preserved |
| routes-importwizard--result | web/src/routes/ImportWizard.stories.tsx:197 | Full play both; readiness/state/style preserved |
| routes-importwizard--import-refused | web/src/routes/ImportWizard.stories.tsx:207 | Full play both; readiness/state/style preserved |
| routes-importwizard--review-failed | web/src/routes/ImportWizard.stories.tsx:219 | Full play both; readiness/state/style preserved |
| routes-importwizard--git-managed | web/src/routes/ImportWizard.stories.tsx:230 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--populated | web/src/routes/InstanceAdmin.stories.tsx:96 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--not-disclosed | web/src/routes/InstanceAdmin.stories.tsx:106 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--loading | web/src/routes/InstanceAdmin.stories.tsx:116 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--second-factor-required | web/src/routes/InstanceAdmin.stories.tsx:126 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--failed | web/src/routes/InstanceAdmin.stories.tsx:135 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--editing-credential-policy | web/src/routes/InstanceAdmin.stories.tsx:144 | Full play both; readiness/state/style preserved |
| routes-instanceadmin--rotate-dek-confirm | web/src/routes/InstanceAdmin.stories.tsx:155 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--active | web/src/routes/InstanceConfig.stories.tsx:110 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--unmanaged | web/src/routes/InstanceConfig.stories.tsx:121 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--pending | web/src/routes/InstanceConfig.stories.tsx:131 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--partial | web/src/routes/InstanceConfig.stories.tsx:142 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--recovery-required | web/src/routes/InstanceConfig.stories.tsx:153 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--loading | web/src/routes/InstanceConfig.stories.tsx:163 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--not-disclosed | web/src/routes/InstanceConfig.stories.tsx:171 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--failed | web/src/routes/InstanceConfig.stories.tsx:182 | Full play both; readiness/state/style preserved |
| routes-instanceconfig--test-mail-ceremony | web/src/routes/InstanceConfig.stories.tsx:193 | Full play both; readiness/state/style preserved |
| routes-invitedialog--org-scope | web/src/routes/InviteDialog.stories.tsx:28 | Full play both; readiness/state/style preserved |
| routes-invitedialog--instance-scope | web/src/routes/InviteDialog.stories.tsx:40 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--editable | web/src/routes/KeyDeclarationDetail.stories.tsx:130 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--deprecated | web/src/routes/KeyDeclarationDetail.stories.tsx:140 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--reclassify-confirm | web/src/routes/KeyDeclarationDetail.stories.tsx:151 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--git-read-only | web/src/routes/KeyDeclarationDetail.stories.tsx:165 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--source-failed | web/src/routes/KeyDeclarationDetail.stories.tsx:175 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--loading | web/src/routes/KeyDeclarationDetail.stories.tsx:184 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--gone | web/src/routes/KeyDeclarationDetail.stories.tsx:192 | Full play both; readiness/state/style preserved |
| routes-keydeclarationdetail--failed | web/src/routes/KeyDeclarationDetail.stories.tsx:201 | Full play both; readiness/state/style preserved |
| routes-lastapplyprovenance--labelled | web/src/routes/LastApplyProvenance.stories.tsx:27 | Full play both; readiness/state/style preserved |
| routes-lastapplyprovenance--bare | web/src/routes/LastApplyProvenance.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--renew | web/src/routes/LeaseActionDialog.stories.tsx:51 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--renew-ceiling-refused | web/src/routes/LeaseActionDialog.stories.tsx:62 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--revoke | web/src/routes/LeaseActionDialog.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--settle | web/src/routes/LeaseActionDialog.stories.tsx:83 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--busy | web/src/routes/LeaseActionDialog.stories.tsx:100 | Full play both; readiness/state/style preserved |
| routes-leaseactiondialog--failed | web/src/routes/LeaseActionDialog.stories.tsx:110 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--idle | web/src/routes/LeaseMintDialog.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--ceiling-refused | web/src/routes/LeaseMintDialog.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--no-session | web/src/routes/LeaseMintDialog.stories.tsx:98 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--submitting | web/src/routes/LeaseMintDialog.stories.tsx:107 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--failed | web/src/routes/LeaseMintDialog.stories.tsx:117 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--disclosed | web/src/routes/LeaseMintDialog.stories.tsx:133 | Full play both; readiness/state/style preserved |
| routes-leasemintdialog--held-back | web/src/routes/LeaseMintDialog.stories.tsx:150 | Full play both; readiness/state/style preserved |
| routes-login--with-providers | web/src/routes/Login.stories.tsx:49 | Full play both; readiness/state/style preserved |
| routes-login--password-step | web/src/routes/Login.stories.tsx:62 | Full play both; readiness/state/style preserved |
| routes-login--paused | web/src/routes/Login.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-login--local-only | web/src/routes/Login.stories.tsx:88 | Full play both; readiness/state/style preserved |
| routes-machineaccess--populated | web/src/routes/MachineAccess.stories.tsx:243 | Full play both; readiness/state/style preserved |
| routes-machineaccess--expanded-row | web/src/routes/MachineAccess.stories.tsx:253 | Full play both; readiness/state/style preserved |
| routes-machineaccess--federation-tab | web/src/routes/MachineAccess.stories.tsx:262 | Full play both; readiness/state/style preserved |
| routes-machineaccess--providers-tab | web/src/routes/MachineAccess.stories.tsx:273 | Full play both; readiness/state/style preserved |
| routes-machineaccess--kubernetes-tab | web/src/routes/MachineAccess.stories.tsx:283 | Full play both; readiness/state/style preserved |
| routes-machineaccess--kubernetes-tab-unsupported | web/src/routes/MachineAccess.stories.tsx:294 | Full play both; readiness/state/style preserved |
| routes-machineaccess--leases-tab | web/src/routes/MachineAccess.stories.tsx:310 | Full play both; readiness/state/style preserved |
| routes-machineaccess--empty | web/src/routes/MachineAccess.stories.tsx:320 | Full play both; readiness/state/style preserved |
| routes-machineaccess--loading | web/src/routes/MachineAccess.stories.tsx:337 | Full play both; readiness/state/style preserved |
| routes-machineaccess--refused | web/src/routes/MachineAccess.stories.tsx:357 | Full play both; readiness/state/style preserved |
| routes-machineaccess--failed | web/src/routes/MachineAccess.stories.tsx:367 | Full play both; readiness/state/style preserved |
| routes-machinerevealdialog--enable | web/src/routes/MachineRevealDialog.stories.tsx:24 | Full play both; readiness/state/style preserved |
| routes-machinerevealdialog--withdraw | web/src/routes/MachineRevealDialog.stories.tsx:35 | Full play both; readiness/state/style preserved |
| routes-machinerevealdialog--busy | web/src/routes/MachineRevealDialog.stories.tsx:44 | Full play both; readiness/state/style preserved |
| routes-machinerevealdialog--failed | web/src/routes/MachineRevealDialog.stories.tsx:54 | Full play both; readiness/state/style preserved |
| routes-machinerevokecredentialdialog--default | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:37 | Full play both; readiness/state/style preserved |
| routes-machinerevokecredentialdialog--busy | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:48 | Full play both; readiness/state/style preserved |
| routes-machinerevokecredentialdialog--failed | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:58 | Full play both; readiness/state/style preserved |
| routes-matrix--populated | web/src/routes/Matrix.stories.tsx:306 | Full play both; readiness/state/style preserved |
| routes-matrix--degraded | web/src/routes/Matrix.stories.tsx:320 | Full play both; readiness/state/style preserved |
| routes-matrix--git-managed | web/src/routes/Matrix.stories.tsx:337 | Full play both; readiness/state/style preserved |
| routes-matrix--empty | web/src/routes/Matrix.stories.tsx:348 | Full play both; readiness/state/style preserved |
| routes-matrix--no-environments | web/src/routes/Matrix.stories.tsx:363 | Full play both; readiness/state/style preserved |
| routes-matrix--loading | web/src/routes/Matrix.stories.tsx:372 | Full play both; readiness/state/style preserved |
| routes-matrix--forbidden | web/src/routes/Matrix.stories.tsx:380 | Full play both; readiness/state/style preserved |
| routes-matrix--failed | web/src/routes/Matrix.stories.tsx:390 | Full play both; readiness/state/style preserved |
| routes-matrixkeycreate--default | web/src/routes/MatrixKeyCreate.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-matrixpublishsheet--default | web/src/routes/MatrixPublishSheet.stories.tsx:56 | Full play both; readiness/state/style preserved |
| routes-matrixpublishsheet--blocked | web/src/routes/MatrixPublishSheet.stories.tsx:74 | Full play both; readiness/state/style preserved |
| routes-matrixpublishsheet--protected | web/src/routes/MatrixPublishSheet.stories.tsx:98 | Full play both; readiness/state/style preserved |
| routes-matrixroweditor--default | web/src/routes/MatrixRowEditor.stories.tsx:83 | Full play both; readiness/state/style preserved |
| routes-matrixroweditor--copy-disclosure | web/src/routes/MatrixRowEditor.stories.tsx:95 | Full play both; readiness/state/style preserved |
| routes-matrixroweditor--edits | web/src/routes/MatrixRowEditor.stories.tsx:109 | Full play both; readiness/state/style preserved |
| routes-matrixroweditor--long-value | web/src/routes/MatrixRowEditor.stories.tsx:140 | Full play both; readiness/state/style preserved |
| routes-matrixroweditor--long-secret-value | web/src/routes/MatrixRowEditor.stories.tsx:155 | Full play both; readiness/state/style preserved |
| routes-members--populated | web/src/routes/Members.stories.tsx:107 | Full play both; readiness/state/style preserved |
| routes-members--load-error | web/src/routes/Members.stories.tsx:126 | Full play both; readiness/state/style preserved |
| routes-members--with-access-rules | web/src/routes/Members.stories.tsx:203 | Full play both; readiness/state/style preserved |
| routes-members--project-with-access-rules | web/src/routes/Members.stories.tsx:253 | Full play both; readiness/state/style preserved |
| routes-mintconnectionform--default | web/src/routes/MintConnectionForm.stories.tsx:30 | Full play both; readiness/state/style preserved |
| routes-mintconnectionform--with-error | web/src/routes/MintConnectionForm.stories.tsx:46 | Full play both; readiness/state/style preserved |
| routes-mintdialog--reviewing | web/src/routes/MintDialog.stories.tsx:63 | Full play both; readiness/state/style preserved |
| routes-mintdialog--reviewing-no-reach | web/src/routes/MintDialog.stories.tsx:72 | Full play both; readiness/state/style preserved |
| routes-mintdialog--rotating | web/src/routes/MintDialog.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-mintdialog--submitting | web/src/routes/MintDialog.stories.tsx:91 | Full play both; readiness/state/style preserved |
| routes-mintdialog--failed | web/src/routes/MintDialog.stories.tsx:100 | Full play both; readiness/state/style preserved |
| routes-mintdialog--disclosed | web/src/routes/MintDialog.stories.tsx:115 | Full play both; readiness/state/style preserved |
| routes-mintdialog--held-back | web/src/routes/MintDialog.stories.tsx:128 | Full play both; readiness/state/style preserved |
| routes-oidcdone--no-transaction | web/src/routes/OIDCDone.stories.tsx:42 | Full play both; readiness/state/style preserved |
| routes-oidcdone--login-refused | web/src/routes/OIDCDone.stories.tsx:50 | Full play both; readiness/state/style preserved |
| routes-oidcdone--link-refused | web/src/routes/OIDCDone.stories.tsx:62 | Full play both; readiness/state/style preserved |
| routes-oidcdone--reauth-refused | web/src/routes/OIDCDone.stories.tsx:73 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--populated | web/src/routes/OidcProvidersPanel.stories.tsx:59 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--empty | web/src/routes/OidcProvidersPanel.stories.tsx:68 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--loading | web/src/routes/OidcProvidersPanel.stories.tsx:77 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--second-factor-required | web/src/routes/OidcProvidersPanel.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--failed | web/src/routes/OidcProvidersPanel.stories.tsx:94 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--reconfiguring | web/src/routes/OidcProvidersPanel.stories.tsx:103 | Full play both; readiness/state/style preserved |
| routes-oidcproviderspanel--delete-confirm | web/src/routes/OidcProvidersPanel.stories.tsx:115 | Full play both; readiness/state/style preserved |
| routes-opsdiagnosticbanners--error-severity | web/src/routes/OpsDiagnosticBanners.stories.tsx:20 | Render and a11y both |
| routes-opsdiagnosticbanners--warn | web/src/routes/OpsDiagnosticBanners.stories.tsx:22 | Render and a11y both |
| routes-opsdiagnosticbanners--unknown | web/src/routes/OpsDiagnosticBanners.stories.tsx:26 | Render and a11y both |
| routes-opsdiagnosticbanners--css-check | web/src/routes/OpsDiagnosticBanners.stories.tsx:33 | Full play both; readiness/state/style preserved |
| routes-orgsettings--populated | web/src/routes/OrgSettings.stories.tsx:112 | Full play both; readiness/state/style preserved |
| routes-orgsettings--empty | web/src/routes/OrgSettings.stories.tsx:129 | Full play both; readiness/state/style preserved |
| routes-orgsettings--loading | web/src/routes/OrgSettings.stories.tsx:145 | Full play both; readiness/state/style preserved |
| routes-orgsettings--failed | web/src/routes/OrgSettings.stories.tsx:158 | Full play both; readiness/state/style preserved |
| routes-pinreleaseoutcome--retained | web/src/routes/PinReleaseOutcome.stories.tsx:22 | Full play both; readiness/state/style preserved |
| routes-pinreleaseoutcome--collection-eligible | web/src/routes/PinReleaseOutcome.stories.tsx:30 | Full play both; readiness/state/style preserved |
| routes-pinreleaseoutcome--already-collected | web/src/routes/PinReleaseOutcome.stories.tsx:38 | Full play both; readiness/state/style preserved |
| routes-placeholder--default | web/src/routes/Placeholder.stories.tsx:17 | Full play both; readiness/state/style preserved |
| routes-profileupdatebadge--default | web/src/routes/ProfileUpdateBadge.stories.tsx:29 | Full play both; readiness/state/style preserved |
| routes-profileupdatebadge--multiple-versions | web/src/routes/ProfileUpdateBadge.stories.tsx:37 | Full play both; readiness/state/style preserved |
| routes-projectsettings--administrable | web/src/routes/ProjectSettings.stories.tsx:116 | Full play both; readiness/state/style preserved |
| routes-projectsettings--unlimited-retention-override | web/src/routes/ProjectSettings.stories.tsx:131 | Full play both; readiness/state/style preserved |
| routes-projectsettings--member-with-environments | web/src/routes/ProjectSettings.stories.tsx:150 | Full play both; readiness/state/style preserved |
| routes-projectsettings--load-error | web/src/routes/ProjectSettings.stories.tsx:176 | Full play both; readiness/state/style preserved |
| routes-projects--empty | web/src/routes/Projects.stories.tsx:57 | Full play both; readiness/state/style preserved |
| routes-projects--populated | web/src/routes/Projects.stories.tsx:66 | Full play both; readiness/state/style preserved |
| routes-projects--load-error | web/src/routes/Projects.stories.tsx:86 | Full play both; readiness/state/style preserved |
| routes-providerdiscoveryalert--default | web/src/routes/ProviderDiscoveryAlert.stories.tsx:18 | Explicit visible appearance + behavior once |
| routes-reconnect--contacting | web/src/routes/Reconnect.stories.tsx:47 | Full play both; readiness/state/style preserved |
| routes-reconnect--ready | web/src/routes/Reconnect.stories.tsx:55 | Full play both; readiness/state/style preserved |
| routes-reconnect--failed | web/src/routes/Reconnect.stories.tsx:63 | Full play both; readiness/state/style preserved |
| routes-remotecard--healthy | web/src/routes/RemoteCard.stories.tsx:68 | Full play both; readiness/state/style preserved |
| routes-remotecard--unreachable | web/src/routes/RemoteCard.stories.tsx:93 | Full play both; readiness/state/style preserved |
| routes-remotecard--credential-rejected | web/src/routes/RemoteCard.stories.tsx:111 | Full play both; readiness/state/style preserved |
| routes-remotecard--duplicate | web/src/routes/RemoteCard.stories.tsx:123 | Full play both; readiness/state/style preserved |
| routes-retentionboundsfields--days | web/src/routes/RetentionBoundsFields.stories.tsx:22 | Render and a11y both |
| routes-retentionboundsfields--exact | web/src/routes/RetentionBoundsFields.stories.tsx:24 | Full play both; readiness/state/style preserved |
| routes-retentionboundsfields--absent | web/src/routes/RetentionBoundsFields.stories.tsx:33 | Render and a11y both |
| routes-revisiondiff--populated | web/src/routes/RevisionDiff.stories.tsx:112 | Full play both; readiness/state/style preserved |
| routes-revisiondiff--revealed | web/src/routes/RevisionDiff.stories.tsx:125 | Full play both; readiness/state/style preserved |
| routes-revisiondiff--empty | web/src/routes/RevisionDiff.stories.tsx:145 | Full play both; readiness/state/style preserved |
| routes-revisiondiff--loading | web/src/routes/RevisionDiff.stories.tsx:153 | Full play both; readiness/state/style preserved |
| routes-revisiondiff--failed | web/src/routes/RevisionDiff.stories.tsx:161 | Full play both; readiness/state/style preserved |
| routes-revokeconnectiondialog--default | web/src/routes/RevokeConnectionDialog.stories.tsx:39 | Full play both; readiness/state/style preserved |
| routes-revokecredentialdialog--default | web/src/routes/RevokeCredentialDialog.stories.tsx:38 | Full play both; readiness/state/style preserved |
| routes-revokecredentialdialog--confirm | web/src/routes/RevokeCredentialDialog.stories.tsx:48 | Full play both; readiness/state/style preserved |
| routes-revokecredentialdialog--busy | web/src/routes/RevokeCredentialDialog.stories.tsx:58 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--populated | web/src/routes/SamlProvidersPanel.stories.tsx:140 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--empty | web/src/routes/SamlProvidersPanel.stories.tsx:149 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--loading | web/src/routes/SamlProvidersPanel.stories.tsx:157 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--second-factor-required | web/src/routes/SamlProvidersPanel.stories.tsx:165 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--failed | web/src/routes/SamlProvidersPanel.stories.tsx:173 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--creating | web/src/routes/SamlProvidersPanel.stories.tsx:181 | Full play both; readiness/state/style preserved |
| routes-samlproviderspanel--metadata-diff | web/src/routes/SamlProvidersPanel.stories.tsx:193 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--populated | web/src/routes/SamlSpKeysPanel.stories.tsx:42 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--loading | web/src/routes/SamlSpKeysPanel.stories.tsx:52 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--second-factor-required | web/src/routes/SamlSpKeysPanel.stories.tsx:61 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--failed | web/src/routes/SamlSpKeysPanel.stories.tsx:69 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--retire-confirm | web/src/routes/SamlSpKeysPanel.stories.tsx:78 | Full play both; readiness/state/style preserved |
| routes-samlspkeyspanel--compromise-retire-confirm | web/src/routes/SamlSpKeysPanel.stories.tsx:87 | Full play both; readiness/state/style preserved |
| routes-scanblockdialog--overridable | web/src/routes/ScanBlockDialog.stories.tsx:33 | Full play both; readiness/state/style preserved |
| routes-scanblockdialog--hard-block | web/src/routes/ScanBlockDialog.stories.tsx:46 | Full play both; readiness/state/style preserved |
| routes-scanwarndialog--default | web/src/routes/ScanWarnDialog.stories.tsx:52 | Full play both; readiness/state/style preserved |
| routes-scimprovisioning--administering | web/src/routes/ScimProvisioning.stories.tsx:218 | Full play both; readiness/state/style preserved |
| routes-scimprovisioning--unselected | web/src/routes/ScimProvisioning.stories.tsx:233 | Full play both; readiness/state/style preserved |
| routes-scimprovisioning--empty | web/src/routes/ScimProvisioning.stories.tsx:244 | Full play both; readiness/state/style preserved |
| routes-scimprovisioning--failed | web/src/routes/ScimProvisioning.stories.tsx:253 | Full play both; readiness/state/style preserved |
| routes-sections--default-panel | web/src/routes/Sections.stories.tsx:31 | Render and a11y both |
| routes-sections--danger-panel | web/src/routes/Sections.stories.tsx:33 | Render and a11y both |
| routes-sections--question-panel | web/src/routes/Sections.stories.tsx:35 | Render and a11y both |
| routes-sections--tight-panel | web/src/routes/Sections.stories.tsx:37 | Render and a11y both |
| routes-sections--jump | web/src/routes/Sections.stories.tsx:39 | Render and a11y both |
| routes-sections--disclosure | web/src/routes/Sections.stories.tsx:51 | Render and a11y both |
| routes-sections--copy-once | web/src/routes/Sections.stories.tsx:60 | Render and a11y both |
| routes-sections--consequences | web/src/routes/Sections.stories.tsx:68 | Full play both; readiness/state/style preserved |
| routes-sections--typed-confirm | web/src/routes/Sections.stories.tsx:90 | Full play both; readiness/state/style preserved |
| routes-setcredentialdialog--replace | web/src/routes/SetCredentialDialog.stories.tsx:40 | Full play both; readiness/state/style preserved |
| routes-setcredentialdialog--first-credential | web/src/routes/SetCredentialDialog.stories.tsx:51 | Full play both; readiness/state/style preserved |
| routes-setcredentialdialog--credential-required | web/src/routes/SetCredentialDialog.stories.tsx:61 | Full play both; readiness/state/style preserved |
| routes-setcredentialdialog--busy | web/src/routes/SetCredentialDialog.stories.tsx:69 | Full play both; readiness/state/style preserved |
| routes-setcredentialdialog--failed | web/src/routes/SetCredentialDialog.stories.tsx:80 | Full play both; readiness/state/style preserved |
| routes-sidebarlinkitem--default | web/src/routes/SidebarLinkItem.stories.tsx:36 | Full play both; readiness/state/style preserved |
| routes-sidebarlinkitem--active | web/src/routes/SidebarLinkItem.stories.tsx:45 | Full play both; readiness/state/style preserved |
| routes-sidebarlinkitem--disabled | web/src/routes/SidebarLinkItem.stories.tsx:54 | Full play both; readiness/state/style preserved |
| routes-sidebarlinkitem--members-active | web/src/routes/SidebarLinkItem.stories.tsx:71 | Full play both; readiness/state/style preserved |
| routes-sidebarversion--default | web/src/routes/SidebarVersion.stories.tsx:18 | Full play both; readiness/state/style preserved |
| routes-sidebarversion--absent | web/src/routes/SidebarVersion.stories.tsx:28 | Full play both; readiness/state/style preserved |
| routes-stepupbanner--authenticator-code | web/src/routes/StepUpBanner.stories.tsx:52 | Full play both; readiness/state/style preserved |
| routes-stepupbanner--passkey | web/src/routes/StepUpBanner.stories.tsx:64 | Full play both; readiness/state/style preserved |
| routes-systemprojectnotice--shown | web/src/routes/SystemProjectNotice.stories.tsx:43 | Full play both; readiness/state/style preserved |
| routes-systemscoperefusal--adapters | web/src/routes/SystemScopeRefusal.stories.tsx:22 | Full play both; readiness/state/style preserved |
| routes-systemscoperefusal--machine-access | web/src/routes/SystemScopeRefusal.stories.tsx:30 | Full play both; readiness/state/style preserved |
| routes-systemscoperefusal--scim | web/src/routes/SystemScopeRefusal.stories.tsx:37 | Full play both; readiness/state/style preserved |
| routes-targetform--default | web/src/routes/TargetForm.stories.tsx:92 | Full play both; readiness/state/style preserved |
| routes-targetform--locked-routing | web/src/routes/TargetForm.stories.tsx:101 | Full play both; readiness/state/style preserved |
| routes-targetform--organization-selected | web/src/routes/TargetForm.stories.tsx:109 | Full play both; readiness/state/style preserved |
| routes-targetform--empty-keys | web/src/routes/TargetForm.stories.tsx:128 | Full play both; readiness/state/style preserved |
| routes-targetform--busy | web/src/routes/TargetForm.stories.tsx:137 | Full play both; readiness/state/style preserved |
| routes-temporaryaccess--populated | web/src/routes/TemporaryAccess.stories.tsx:82 | Full play both; readiness/state/style preserved |
| routes-temporaryaccess--requester | web/src/routes/TemporaryAccess.stories.tsx:100 | Full play both; readiness/state/style preserved |
| routes-temporaryaccess--loading | web/src/routes/TemporaryAccess.stories.tsx:116 | Full play both; readiness/state/style preserved |
| routes-themetoggle--default | web/src/routes/ThemeToggle.stories.tsx:26 | Full play both; readiness/state/style preserved |
| routes-themetoggle--toggles | web/src/routes/ThemeToggle.stories.tsx:35 | Full play both; readiness/state/style preserved |
| routes-updatejobstatus--queued | web/src/routes/UpdateJobStatus.stories.tsx:33 | Full play both; readiness/state/style preserved |
| routes-updatejobstatus--running | web/src/routes/UpdateJobStatus.stories.tsx:41 | Full play both; readiness/state/style preserved |
| routes-updatejobstatus--succeeded | web/src/routes/UpdateJobStatus.stories.tsx:49 | Full play both; readiness/state/style preserved |
| routes-updatejobstatus--failed | web/src/routes/UpdateJobStatus.stories.tsx:57 | Full play both; readiness/state/style preserved |
| routes-values--populated | web/src/routes/Values.stories.tsx:100 | Full play both; readiness/state/style preserved |
| routes-values--window-live | web/src/routes/Values.stories.tsx:110 | Full play both; readiness/state/style preserved |
| routes-values--protected-locked | web/src/routes/Values.stories.tsx:125 | Full play both; readiness/state/style preserved |
| routes-values--write-only | web/src/routes/Values.stories.tsx:137 | Full play both; readiness/state/style preserved |
| routes-values--empty | web/src/routes/Values.stories.tsx:156 | Full play both; readiness/state/style preserved |
| routes-values--loading | web/src/routes/Values.stories.tsx:167 | Full play both; readiness/state/style preserved |
| routes-values--failed | web/src/routes/Values.stories.tsx:176 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--nothing-to-authorize | web/src/routes/WorkspaceApprove.stories.tsx:66 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--loading | web/src/routes/WorkspaceApprove.stories.tsx:76 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--establishment | web/src/routes/WorkspaceApprove.stories.tsx:85 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--step-up | web/src/routes/WorkspaceApprove.stories.tsx:99 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--failed | web/src/routes/WorkspaceApprove.stories.tsx:115 | Full play both; readiness/state/style preserved |
| routes-workspaceapprove--sign-in | web/src/routes/WorkspaceApprove.stories.tsx:130 | Full play both; readiness/state/style preserved |
| routes-workspacecallback--no-result | web/src/routes/WorkspaceCallback.stories.tsx:36 | Full play both; readiness/state/style preserved |
| routes-workspacecallback--could-not-close | web/src/routes/WorkspaceCallback.stories.tsx:44 | Full play both; readiness/state/style preserved |
| routes-workspacescope--loading | web/src/routes/WorkspaceScope.stories.tsx:79 | Full play both; readiness/state/style preserved |
| routes-workspacescope--unknown-remote | web/src/routes/WorkspaceScope.stories.tsx:87 | Full play both; readiness/state/style preserved |
| routes-workspacescope--reconnect-required | web/src/routes/WorkspaceScope.stories.tsx:97 | Full play both; readiness/state/style preserved |
| routes-workspacescope--checking | web/src/routes/WorkspaceScope.stories.tsx:109 | Full play both; readiness/state/style preserved |
| routes-workspacescope--failed | web/src/routes/WorkspaceScope.stories.tsx:123 | Full play both; readiness/state/style preserved |
| routes-workspacescope--connected | web/src/routes/WorkspaceScope.stories.tsx:145 | Full play both; readiness/state/style preserved |
| routes-workspacestepup--disconnected | web/src/routes/WorkspaceStepUp.stories.tsx:80 | Full play both; readiness/state/style preserved |
| routes-workspacestepup--contacting | web/src/routes/WorkspaceStepUp.stories.tsx:88 | Full play both; readiness/state/style preserved |
| routes-workspacestepup--ready | web/src/routes/WorkspaceStepUp.stories.tsx:97 | Full play both; readiness/state/style preserved |
| routes-workspacestepup--failed | web/src/routes/WorkspaceStepUp.stories.tsx:108 | Full play both; readiness/state/style preserved |
| members-access-rules--rules | web/src/routes/accessRules/AccessRules.stories.tsx:49 | Full play both; readiness/state/style preserved |
| members-access-rules--rules-on-a-project | web/src/routes/accessRules/AccessRules.stories.tsx:75 | Full play both; readiness/state/style preserved |
| members-access-rules--editor-folder-rule | web/src/routes/accessRules/AccessRules.stories.tsx:100 | Full play both; readiness/state/style preserved |
| members-access-rules--editor-new-rule | web/src/routes/accessRules/AccessRules.stories.tsx:157 | Full play both; readiness/state/style preserved |
| members-access-rules--editor-single-key-rule | web/src/routes/accessRules/AccessRules.stories.tsx:185 | Full play both; readiness/state/style preserved |
| members-access-rules--editor-without-key-names | web/src/routes/accessRules/AccessRules.stories.tsx:210 | Full play both; readiness/state/style preserved |
| members-access-rules--who-can | web/src/routes/accessRules/AccessRules.stories.tsx:223 | Full play both; readiness/state/style preserved |
| members-access-rules--who-can-without-key-names | web/src/routes/accessRules/AccessRules.stories.tsx:263 | Full play both; readiness/state/style preserved |
| members-access-rules--key-move-confirmation | web/src/routes/accessRules/AccessRules.stories.tsx:288 | Full play both; readiness/state/style preserved |
| members-access-rules--key-move-count-only | web/src/routes/accessRules/AccessRules.stories.tsx:314 | Full play both; readiness/state/style preserved |
| members-access-rules--glossary | web/src/routes/accessRules/AccessRules.stories.tsx:325 | Full play both; readiness/state/style preserved |
| ui-alert--danger | web/src/ui/Alert.stories.tsx:18 | Render and a11y both |
| ui-alert--done | web/src/ui/Alert.stories.tsx:19 | Render and a11y both |
| ui-alert--warn | web/src/ui/Alert.stories.tsx:20 | Render and a11y both |
| ui-alert--info | web/src/ui/Alert.stories.tsx:27 | Render and a11y both |
| ui-alert--with-action | web/src/ui/Alert.stories.tsx:34 | Render and a11y both |
| ui-alert--all-states | web/src/ui/Alert.stories.tsx:43 | Render and a11y both |
| ui-badge--neutral | web/src/ui/Badge.stories.tsx:18 | Render and a11y both |
| ui-badge--all-tones | web/src/ui/Badge.stories.tsx:21 | Render and a11y both |
| ui-button--secondary | web/src/ui/Button.stories.tsx:33 | Render and a11y both |
| ui-button--primary | web/src/ui/Button.stories.tsx:34 | Render and a11y both |
| ui-button--disabled | web/src/ui/Button.stories.tsx:35 | Render and a11y both |
| ui-button--icon | web/src/ui/Button.stories.tsx:36 | Render and a11y both |
| ui-button--all-variants | web/src/ui/Button.stories.tsx:44 | Render and a11y both |
| ui-checkbox--default | web/src/ui/Checkbox.stories.tsx:18 | Render and a11y both |
| ui-checkbox--checked | web/src/ui/Checkbox.stories.tsx:19 | Render and a11y both |
| ui-checkbox--disabled | web/src/ui/Checkbox.stories.tsx:20 | Render and a11y both |
| ui-checkbox--hit-box-is-the-input | web/src/ui/Checkbox.stories.tsx:25 | Full play both; readiness/state/style preserved |
| ui-checkbox--all-states | web/src/ui/Checkbox.stories.tsx:50 | Render and a11y both |
| ui-choicegroup--stack | web/src/ui/ChoiceGroup.stories.tsx:46 | Render and a11y both |
| ui-choicegroup--wrap | web/src/ui/ChoiceGroup.stories.tsx:47 | Render and a11y both |
| ui-choicegroup--nowrap | web/src/ui/ChoiceGroup.stories.tsx:48 | Render and a11y both |
| ui-choicegroup--two-columns | web/src/ui/ChoiceGroup.stories.tsx:52 | Render and a11y both |
| ui-choicegroup--three-columns | web/src/ui/ChoiceGroup.stories.tsx:53 | Render and a11y both |
| ui-choicegroup--chips | web/src/ui/ChoiceGroup.stories.tsx:54 | Render and a11y both |
| ui-choicegroup--with-hint | web/src/ui/ChoiceGroup.stories.tsx:57 | Render and a11y both |
| ui-choicegroup--radios | web/src/ui/ChoiceGroup.stories.tsx:60 | Render and a11y both |
| ui-choicegroup--all-states | web/src/ui/ChoiceGroup.stories.tsx:74 | Render and a11y both |
| ui-choicegroup--legend-names-the-group | web/src/ui/ChoiceGroup.stories.tsx:93 | Full play both; readiness/state/style preserved |
| ui-choicegroup--columns-are-a-grid | web/src/ui/ChoiceGroup.stories.tsx:102 | Full play both; readiness/state/style preserved |
| ui-choicegroup--nowrap-scrolls | web/src/ui/ChoiceGroup.stories.tsx:113 | Full play both; readiness/state/style preserved |
| ui-choicegroup--chips-mark-checked | web/src/ui/ChoiceGroup.stories.tsx:127 | Full play both; readiness/state/style preserved |
| ui-dialog--decision | web/src/ui/Dialog.stories.tsx:37 | Render and a11y both |
| ui-dialog--with-refusal | web/src/ui/Dialog.stories.tsx:39 | Render and a11y both |
| ui-dialog--wide | web/src/ui/Dialog.stories.tsx:45 | Render and a11y both |
| ui-dialog--must-acknowledge | web/src/ui/Dialog.stories.tsx:71 | Render and a11y both |
| ui-dialog--pinned-actions | web/src/ui/Dialog.stories.tsx:94 | Full play both; readiness/state/style preserved |
| ui-dialog--backdrop-click | web/src/ui/Dialog.stories.tsx:128 | Explicit visible appearance + behavior once |
| ui-dialog--is-modal-and-labelled | web/src/ui/Dialog.stories.tsx:158 | Full play both; readiness/state/style preserved |
| ui-dialog--mono-title | web/src/ui/Dialog.stories.tsx:176 | Full play both; readiness/state/style preserved |
| ui-disclosure--closed | web/src/ui/Disclosure.stories.tsx:20 | Render and a11y both |
| ui-disclosure--open | web/src/ui/Disclosure.stories.tsx:21 | Render and a11y both |
| ui-disclosure--toggles | web/src/ui/Disclosure.stories.tsx:24 | Full play both; readiness/state/style preserved |
| ui-field--default | web/src/ui/Field.stories.tsx:23 | Full play both; readiness/state/style preserved |
| ui-field--with-hint | web/src/ui/Field.stories.tsx:30 | Full play both; readiness/state/style preserved |
| ui-field--with-error | web/src/ui/Field.stories.tsx:40 | Full play both; readiness/state/style preserved |
| ui-glyph--decorative | web/src/ui/Glyph.stories.tsx:20 | Render and a11y both |
| ui-glyph--named | web/src/ui/Glyph.stories.tsx:21 | Render and a11y both |
| ui-glyph--all-states | web/src/ui/Glyph.stories.tsx:24 | Render and a11y both |
| ui-input--default | web/src/ui/Input.stories.tsx:18 | Render and a11y both |
| ui-input--password | web/src/ui/Input.stories.tsx:19 | Render and a11y both |
| ui-input--password-revealable | web/src/ui/Input.stories.tsx:20 | Full play both; readiness/state/style preserved |
| ui-input--disabled | web/src/ui/Input.stories.tsx:30 | Render and a11y both |
| ui-input--mono | web/src/ui/Input.stories.tsx:31 | Render and a11y both |
| ui-input--mono-targets-the-control | web/src/ui/Input.stories.tsx:34 | Full play both; readiness/state/style preserved |
| ui-input--with-hint | web/src/ui/Input.stories.tsx:44 | Render and a11y both |
| ui-input--with-error | web/src/ui/Input.stories.tsx:48 | Render and a11y both |
| ui-input--error-is-wired | web/src/ui/Input.stories.tsx:58 | Full play both; readiness/state/style preserved |
| ui-input--all-states | web/src/ui/Input.stories.tsx:68 | Render and a11y both |
| ui-input--external-description-is-merged | web/src/ui/Input.stories.tsx:84 | Full play both; readiness/state/style preserved |
| ui-input--external-invalid-is-kept | web/src/ui/Input.stories.tsx:99 | Full play both; readiness/state/style preserved |
| ui-menu--default | web/src/ui/Menu.stories.tsx:28 | Render and a11y both |
| ui-menu--opens | web/src/ui/Menu.stories.tsx:30 | Full play both; readiness/state/style preserved |
| ui-menu--selects | web/src/ui/Menu.stories.tsx:46 | Full play both; readiness/state/style preserved |
| ui-menu--keyboard | web/src/ui/Menu.stories.tsx:69 | Full play both; readiness/state/style preserved |
| ui-menu--tab-leaves | web/src/ui/Menu.stories.tsx:112 | Full play both; readiness/state/style preserved |
| ui-menu--selects-by-keyboard | web/src/ui/Menu.stories.tsx:134 | Full play both; readiness/state/style preserved |
| ui-menu--disabled-skipped | web/src/ui/Menu.stories.tsx:159 | Full play both; readiness/state/style preserved |
| ui-radio--default | web/src/ui/Radio.stories.tsx:21 | Render and a11y both |
| ui-radio--checked | web/src/ui/Radio.stories.tsx:22 | Render and a11y both |
| ui-radio--disabled | web/src/ui/Radio.stories.tsx:23 | Render and a11y both |
| ui-radio--all-states | web/src/ui/Radio.stories.tsx:35 | Render and a11y both |
| ui-select--default | web/src/ui/Select.stories.tsx:27 | Render and a11y both |
| ui-select--disabled | web/src/ui/Select.stories.tsx:28 | Render and a11y both |
| ui-select--all-states | web/src/ui/Select.stories.tsx:30 | Render and a11y both |
| ui-select--external-description-is-merged | web/src/ui/Select.stories.tsx:48 | Full play both; readiness/state/style preserved |
| ui-select--external-invalid-is-kept | web/src/ui/Select.stories.tsx:63 | Full play both; readiness/state/style preserved |
| ui-tabs--default | web/src/ui/Tabs.stories.tsx:41 | Render and a11y both |
| ui-tabs--second-selected | web/src/ui/Tabs.stories.tsx:42 | Render and a11y both |
| ui-tabs--keyboard-moves-and-selects | web/src/ui/Tabs.stories.tsx:45 | Full play both; readiness/state/style preserved |
| ui-tabs--two-demos-stay-independent | web/src/ui/Tabs.stories.tsx:88 | Full play both; readiness/state/style preserved |
| ui-textarea--default | web/src/ui/Textarea.stories.tsx:18 | Render and a11y both |
| ui-textarea--filled | web/src/ui/Textarea.stories.tsx:19 | Render and a11y both |
| ui-textarea--disabled | web/src/ui/Textarea.stories.tsx:20 | Render and a11y both |
| ui-textarea--all-states | web/src/ui/Textarea.stories.tsx:22 | Render and a11y both |
| ui-textarea--external-description-is-merged | web/src/ui/Textarea.stories.tsx:36 | Full play both; readiness/state/style preserved |
| ui-textarea--external-invalid-is-kept | web/src/ui/Textarea.stories.tsx:51 | Full play both; readiness/state/style preserved |
| ui-themeicon--sun | web/src/ui/ThemeIcon.stories.tsx:19 | Full play both; readiness/state/style preserved |
| ui-themeicon--moon | web/src/ui/ThemeIcon.stories.tsx:29 | Full play both; readiness/state/style preserved |
| ui-togglechip--not-included | web/src/ui/ToggleChip.stories.tsx:20 | Render and a11y both |
| ui-togglechip--included | web/src/ui/ToggleChip.stories.tsx:21 | Render and a11y both |
| ui-togglechip--implied | web/src/ui/ToggleChip.stories.tsx:23 | Render and a11y both |
| ui-togglechip--left-out | web/src/ui/ToggleChip.stories.tsx:24 | Render and a11y both |
| ui-togglechip--disabled | web/src/ui/ToggleChip.stories.tsx:25 | Render and a11y both |
| ui-togglechip--exclude-toggles | web/src/ui/ToggleChip.stories.tsx:41 | Full play both; readiness/state/style preserved |
| ui-tokens--design-system | web/src/ui/Tokens.stories.tsx:95 | Render and a11y both |
| ui-tokens--every-token-resolves | web/src/ui/Tokens.stories.tsx:99 | Full play both; readiness/state/style preserved |
| ui-typography--scale | web/src/ui/Typography.stories.tsx:56 | Render and a11y both |
| ui-auth-authenticatorcodefield--default | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:26 | Render and a11y both |
| ui-auth-authenticatorcodefield--checking | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:27 | Render and a11y both |
| ui-auth-authenticatorcodefield--submits-trimmed-and-clears | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:30 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--password-then-authenticator | web/src/ui/auth/LoginFlow.stories.tsx:232 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--authenticator-code-refused | web/src/ui/auth/LoginFlow.stories.tsx:245 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--unenrolled-is-gated-into-setup | web/src/ui/auth/LoginFlow.stories.tsx:260 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--unenrolled-allowed-by-policy | web/src/ui/auth/LoginFlow.stories.tsx:282 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--passkey-passwordless | web/src/ui/auth/LoginFlow.stories.tsx:292 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--identity-provider | web/src/ui/auth/LoginFlow.stories.tsx:302 | Full play both; readiness/state/style preserved |
| ui-auth-loginflow--sign-up-through-provider | web/src/ui/auth/LoginFlow.stories.tsx:313 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--with-providers | web/src/ui/auth/LoginForm.stories.tsx:55 | Render and a11y both |
| ui-auth-loginform--social-providers | web/src/ui/auth/LoginForm.stories.tsx:58 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--last-used-provider | web/src/ui/auth/LoginForm.stories.tsx:71 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--last-used-password | web/src/ui/auth/LoginForm.stories.tsx:83 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--last-used-passkey | web/src/ui/auth/LoginForm.stories.tsx:90 | Render and a11y both |
| ui-auth-loginform--local-only | web/src/ui/auth/LoginForm.stories.tsx:93 | Render and a11y both |
| ui-auth-loginform--password-and-passkey | web/src/ui/auth/LoginForm.stories.tsx:95 | Render and a11y both |
| ui-auth-loginform--password-step | web/src/ui/auth/LoginForm.stories.tsx:98 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--waiting-for-passkey | web/src/ui/auth/LoginForm.stories.tsx:112 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--contacting-provider | web/src/ui/auth/LoginForm.stories.tsx:121 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--refused | web/src/ui/auth/LoginForm.stories.tsx:131 | Render and a11y both |
| ui-auth-loginform--paused | web/src/ui/auth/LoginForm.stories.tsx:136 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--door-open | web/src/ui/auth/LoginForm.stories.tsx:145 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--sign-up-door | web/src/ui/auth/LoginForm.stories.tsx:156 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--opens-on-sign-up | web/src/ui/auth/LoginForm.stories.tsx:171 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--opens-on-sign-up-while-closed | web/src/ui/auth/LoginForm.stories.tsx:181 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--sign-up-confirmation | web/src/ui/auth/LoginForm.stories.tsx:190 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--sign-up-back-links | web/src/ui/auth/LoginForm.stories.tsx:203 | Full play both; readiness/state/style preserved |
| ui-auth-loginform--provider-starts-from-step-one | web/src/ui/auth/LoginForm.stories.tsx:216 | Explicit visible appearance + behavior once |
| ui-auth-loginform--submits-and-clears-password | web/src/ui/auth/LoginForm.stories.tsx:224 | Full play both; readiness/state/style preserved |
| ui-auth-proofdialog--reauth-gated | web/src/ui/auth/ProofDialog.stories.tsx:27 | Full play both; readiness/state/style preserved |
| ui-auth-proofdialog--authenticator-code | web/src/ui/auth/ProofDialog.stories.tsx:41 | Full play both; readiness/state/style preserved |
| ui-auth-proofdialog--refused | web/src/ui/auth/ProofDialog.stories.tsx:50 | Render and a11y both |
| ui-auth-providerbutton--google | web/src/ui/auth/ProviderButton.stories.tsx:30 | Render and a11y both |
| ui-auth-providerbutton--google-sign-up | web/src/ui/auth/ProviderButton.stories.tsx:33 | Render and a11y both |
| ui-auth-providerbutton--microsoft | web/src/ui/auth/ProviderButton.stories.tsx:36 | Render and a11y both |
| ui-auth-providerbutton--microsoft-sign-up | web/src/ui/auth/ProviderButton.stories.tsx:39 | Full play both; readiness/state/style preserved |
| ui-auth-providerbutton--git-hub | web/src/ui/auth/ProviderButton.stories.tsx:47 | Render and a11y both |
| ui-auth-providerbutton--git-hub-sign-up | web/src/ui/auth/ProviderButton.stories.tsx:49 | Render and a11y both |
| ui-auth-providerbutton--plain | web/src/ui/auth/ProviderButton.stories.tsx:52 | Render and a11y both |
| ui-auth-providerbutton--contacting | web/src/ui/auth/ProviderButton.stories.tsx:55 | Render and a11y both |
| ui-auth-providerbutton--last-used | web/src/ui/auth/ProviderButton.stories.tsx:58 | Render and a11y both |
| ui-auth-providerbutton--disabled | web/src/ui/auth/ProviderButton.stories.tsx:61 | Render and a11y both |
| ui-auth-providerbutton--fires-on-click | web/src/ui/auth/ProviderButton.stories.tsx:63 | Explicit visible appearance + behavior once |
| ui-auth-qrcode--default | web/src/ui/auth/QrCode.stories.tsx:20 | Render and a11y both |
| ui-auth-qrcode--named-and-scannable | web/src/ui/auth/QrCode.stories.tsx:24 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorchallenge--authenticator-and-passkey | web/src/ui/auth/SecondFactorChallenge.stories.tsx:32 | Render and a11y both |
| ui-auth-secondfactorchallenge--authenticator-only | web/src/ui/auth/SecondFactorChallenge.stories.tsx:34 | Render and a11y both |
| ui-auth-secondfactorchallenge--passkey-only | web/src/ui/auth/SecondFactorChallenge.stories.tsx:36 | Render and a11y both |
| ui-auth-secondfactorchallenge--no-factor-presentable | web/src/ui/auth/SecondFactorChallenge.stories.tsx:39 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorchallenge--checking-code | web/src/ui/auth/SecondFactorChallenge.stories.tsx:47 | Render and a11y both |
| ui-auth-secondfactorchallenge--waiting-for-passkey | web/src/ui/auth/SecondFactorChallenge.stories.tsx:49 | Render and a11y both |
| ui-auth-secondfactorchallenge--code-refused | web/src/ui/auth/SecondFactorChallenge.stories.tsx:51 | Render and a11y both |
| ui-auth-secondfactorchallenge--no-skip-control | web/src/ui/auth/SecondFactorChallenge.stories.tsx:59 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorchallenge--submits-trimmed-code | web/src/ui/auth/SecondFactorChallenge.stories.tsx:66 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorsetup--confirm-password | web/src/ui/auth/SecondFactorSetup.stories.tsx:37 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorsetup--confirm-password-refused | web/src/ui/auth/SecondFactorSetup.stories.tsx:48 | Render and a11y both |
| ui-auth-secondfactorsetup--choose | web/src/ui/auth/SecondFactorSetup.stories.tsx:52 | Render and a11y both |
| ui-auth-secondfactorsetup--choose-no-passkey-support | web/src/ui/auth/SecondFactorSetup.stories.tsx:54 | Render and a11y both |
| ui-auth-secondfactorsetup--authenticator | web/src/ui/auth/SecondFactorSetup.stories.tsx:56 | Render and a11y both |
| ui-auth-secondfactorsetup--authenticator-code-refused | web/src/ui/auth/SecondFactorSetup.stories.tsx:58 | Render and a11y both |
| ui-auth-secondfactorsetup--recovery-codes | web/src/ui/auth/SecondFactorSetup.stories.tsx:62 | Render and a11y both |
| ui-auth-secondfactorsetup--no-skip-on-any-step | web/src/ui/auth/SecondFactorSetup.stories.tsx:66 | Full play both; readiness/state/style preserved |
| ui-auth-secondfactorsetup--codes-need-acknowledgement | web/src/ui/auth/SecondFactorSetup.stories.tsx:82 | Full play both; readiness/state/style preserved |


## Before sidebar, source-sorted inventory

```text
app/notifications
  Docs app-notifications--docs
  Failure [app-notifications--failure]
  Success [app-notifications--success]
  Info [app-notifications--info]
  Dismisses [app-notifications--dismisses]

app/RuntimeMaintenanceBoundary
  Docs app-runtimemaintenanceboundary--docs
  Maintenance [app-runtimemaintenanceboundary--maintenance]
  Recovery Required [app-runtimemaintenanceboundary--recovery-required]
  Reconnecting [app-runtimemaintenanceboundary--reconnecting]

Members/Access rules
  Docs members-access-rules--docs
  Rules [members-access-rules--rules]
  Rules On A Project [members-access-rules--rules-on-a-project]
  Editor Folder Rule [members-access-rules--editor-folder-rule]
  Editor New Rule [members-access-rules--editor-new-rule]
  Editor Single Key Rule [members-access-rules--editor-single-key-rule]
  Editor Without Key Names [members-access-rules--editor-without-key-names]
  Who Can [members-access-rules--who-can]
  Who Can Without Key Names [members-access-rules--who-can-without-key-names]
  Key move confirmation [members-access-rules--key-move-confirmation]
  Key move, count only [members-access-rules--key-move-count-only]
  Glossary [members-access-rules--glossary]

routes/AccountProfile
  Docs routes-accountprofile--docs
  Editable [routes-accountprofile--editable]
  Proof Required [routes-accountprofile--proof-required]
  Managed [routes-accountprofile--managed]
  Provider Sign In [routes-accountprofile--provider-sign-in]
  Loading [routes-accountprofile--loading]
  Failed [routes-accountprofile--failed]

routes/AccountSecurity
  Docs routes-accountsecurity--docs
  Populated [routes-accountsecurity--populated]
  Empty [routes-accountsecurity--empty]
  Loading [routes-accountsecurity--loading]
  Failed [routes-accountsecurity--failed]
  Proof Dialog [routes-accountsecurity--proof-dialog]

routes/Audit
  Docs routes-audit--docs
  Populated [routes-audit--populated]
  Empty [routes-audit--empty]
  Loading [routes-audit--loading]
  Failed [routes-audit--failed]

routes/BindingCard
  Docs routes-bindingcard--docs
  Default [routes-bindingcard--default]
  Expiring Soon [routes-bindingcard--expiring-soon]
  Quarantined [routes-bindingcard--quarantined]
  Not Ready [routes-bindingcard--not-ready]

routes/BindingDialog
  Docs routes-bindingdialog--docs
  Default [routes-bindingdialog--default]
  Git Hub Actions [routes-bindingdialog--git-hub-actions]
  Pull Request Event [routes-bindingdialog--pull-request-event]
  Audience Required [routes-bindingdialog--audience-required]
  Replace [routes-bindingdialog--replace]
  Busy [routes-bindingdialog--busy]
  Failed [routes-bindingdialog--failed]

routes/CatalogueManageDialog
  Docs routes-cataloguemanagedialog--docs
  Populated [routes-cataloguemanagedialog--populated]
  Empty [routes-cataloguemanagedialog--empty]
  Loading [routes-cataloguemanagedialog--loading]
  Failed [routes-cataloguemanagedialog--failed]
  Read Only [routes-cataloguemanagedialog--read-only]
  Create Refused [routes-cataloguemanagedialog--create-refused]

routes/Ceremony
  Docs routes-ceremony--docs
  Protected [routes-ceremony--protected]
  Code Offered [routes-ceremony--code-offered]
  OIDC [routes-ceremony--oidc]
  Refused [routes-ceremony--refused]

routes/ChangeApprovals
  Docs routes-changeapprovals--docs
  Populated [routes-changeapprovals--populated]
  Empty [routes-changeapprovals--empty]
  Loading [routes-changeapprovals--loading]
  Failed [routes-changeapprovals--failed]

routes/ChromeIdentityControls
  Docs routes-chromeidentitycontrols--docs
  Org Identity [routes-chromeidentitycontrols--org-identity]
  Project Identity [routes-chromeidentitycontrols--project-identity]

routes/CLIReauth
  Docs routes-clireauth--docs
  Disclosure [routes-clireauth--disclosure]
  Disclosure OIDC [routes-clireauth--disclosure-oidc]
  Adapter [routes-clireauth--adapter]
  Self Config [routes-clireauth--self-config]
  Loading [routes-clireauth--loading]
  Failed [routes-clireauth--failed]
  Nothing To Authorize [routes-clireauth--nothing-to-authorize]

routes/CompactOrgRetention
  Docs routes-compactorgretention--docs
  Default [routes-compactorgretention--default]
  Busy [routes-compactorgretention--busy]
  Refused [routes-compactorgretention--refused]

routes/ConnectionMintDialog
  Docs routes-connectionmintdialog--docs
  Default [routes-connectionmintdialog--default]
  Clamped [routes-connectionmintdialog--clamped]

routes/ConnectionRow
  Docs routes-connectionrow--docs
  Live [routes-connectionrow--live]
  Expired [routes-connectionrow--expired]
  Revoked [routes-connectionrow--revoked]
  Indefinite [routes-connectionrow--indefinite]

routes/CreateAccountDialog
  Docs routes-createaccountdialog--docs
  Default [routes-createaccountdialog--default]
  Name Required [routes-createaccountdialog--name-required]
  Busy [routes-createaccountdialog--busy]
  Failed [routes-createaccountdialog--failed]

routes/CreateProviderDialog
  Docs routes-createproviderdialog--docs
  Default [routes-createproviderdialog--default]
  Fields Required [routes-createproviderdialog--fields-required]
  Busy [routes-createproviderdialog--busy]
  Failed [routes-createproviderdialog--failed]

routes/CredentialForm
  Docs routes-credentialform--docs
  Default [routes-credentialform--default]
  Entered [routes-credentialform--entered]
  Busy [routes-credentialform--busy]

routes/DefinitionsBundlePanel
  Docs routes-definitionsbundlepanel--docs
  Row [routes-definitionsbundlepanel--row]
  Dialog Open [routes-definitionsbundlepanel--dialog-open]
  Git Read Only [routes-definitionsbundlepanel--git-read-only]
  Checked [routes-definitionsbundlepanel--checked]
  Planned [routes-definitionsbundlepanel--planned]
  Busy [routes-definitionsbundlepanel--busy]
  Refused [routes-definitionsbundlepanel--refused]

routes/DeleteAccountDialog
  Docs routes-deleteaccountdialog--docs
  Default [routes-deleteaccountdialog--default]
  One Credential [routes-deleteaccountdialog--one-credential]
  Busy [routes-deleteaccountdialog--busy]
  Failed [routes-deleteaccountdialog--failed]
  Failed May Have Committed [routes-deleteaccountdialog--failed-may-have-committed]

routes/DeleteAdapterDialog
  Docs routes-deleteadapterdialog--docs
  Default [routes-deleteadapterdialog--default]
  Prune [routes-deleteadapterdialog--prune]
  Busy [routes-deleteadapterdialog--busy]

routes/DeleteProviderDialog
  Docs routes-deleteproviderdialog--docs
  No Leases [routes-deleteproviderdialog--no-leases]
  Live Leases [routes-deleteproviderdialog--live-leases]
  Leases Unknown [routes-deleteproviderdialog--leases-unknown]
  Busy [routes-deleteproviderdialog--busy]
  Conflict [routes-deleteproviderdialog--conflict]
  Failed [routes-deleteproviderdialog--failed]

routes/DeliveryTargets
  Docs routes-deliverytargets--docs
  Reported Healthy [routes-deliverytargets--reported-healthy]
  Reported Degraded [routes-deliverytargets--reported-degraded]
  Stale [routes-deliverytargets--stale]
  Refused [routes-deliverytargets--refused]
  Reporter Revoked [routes-deliverytargets--reporter-revoked]
  Quota Refused [routes-deliverytargets--quota-refused]
  Unknown [routes-deliverytargets--unknown]
  Empty [routes-deliverytargets--empty]
  Unsupported [routes-deliverytargets--unsupported]

routes/EnrolmentGate
  Docs routes-enrolmentgate--docs
  Password [routes-enrolmentgate--password]
  Password Refused [routes-enrolmentgate--password-refused]

routes/EstablishCredential
  Docs routes-establishcredential--docs
  Initial [routes-establishcredential--initial]
  Mismatch [routes-establishcredential--mismatch]
  Refused [routes-establishcredential--refused]
  Done [routes-establishcredential--done]
  Recover [routes-establishcredential--recover]
  Recovered [routes-establishcredential--recovered]

routes/FederationIssuersPanel
  Docs routes-federationissuerspanel--docs
  Populated [routes-federationissuerspanel--populated]
  Empty [routes-federationissuerspanel--empty]
  Loading [routes-federationissuerspanel--loading]
  Second Factor Required [routes-federationissuerspanel--second-factor-required]
  Failed [routes-federationissuerspanel--failed]
  Creating [routes-federationissuerspanel--creating]
  Editing [routes-federationissuerspanel--editing]
  Delete Refused [routes-federationissuerspanel--delete-refused]

routes/FleetUpdateNotice
  Docs routes-fleetupdatenotice--docs
  Local Update [routes-fleetupdatenotice--local-update]
  Remote Update [routes-fleetupdatenotice--remote-update]
  Multiple Updates [routes-fleetupdatenotice--multiple-updates]
  No Updates [routes-fleetupdatenotice--no-updates]

routes/FolderCleanupDialog
  Docs routes-foldercleanupdialog--docs
  Default [routes-foldercleanupdialog--default]
  Widening Refused [routes-foldercleanupdialog--widening-refused]
  Busy [routes-foldercleanupdialog--busy]

routes/GrantDialog
  Docs routes-grantdialog--docs
  Read Grant [routes-grantdialog--read-grant]
  Reveal Grant [routes-grantdialog--reveal-grant]
  Nothing To Widen [routes-grantdialog--nothing-to-widen]
  Catalogue Failed [routes-grantdialog--catalogue-failed]
  Catalogue Loading [routes-grantdialog--catalogue-loading]
  Reporting Grantable [routes-grantdialog--reporting-grantable]
  Reporting Not Grantable [routes-grantdialog--reporting-not-grantable]
  Report After The Fact [routes-grantdialog--report-after-the-fact]
  Reporting Unsupported [routes-grantdialog--reporting-unsupported]

routes/HealthChip
  Docs routes-healthchip--docs
  Never [routes-healthchip--never]
  Pending [routes-healthchip--pending]
  Converging [routes-healthchip--converging]
  Converged [routes-healthchip--converged]
  Degraded [routes-healthchip--degraded]
  Failed [routes-healthchip--failed]
  Paused [routes-healthchip--paused]
  Drift Attention [routes-healthchip--drift-attention]

routes/HistoryDrawer
  Docs routes-historydrawer--docs
  Populated [routes-historydrawer--populated]
  Key Filter [routes-historydrawer--key-filter]
  Pin Sheet Open [routes-historydrawer--pin-sheet-open]
  Restore Sheet Open [routes-historydrawer--restore-sheet-open]
  Empty [routes-historydrawer--empty]
  Loading [routes-historydrawer--loading]
  Failed [routes-historydrawer--failed]
  Phone [routes-historydrawer--phone]
  Phone Detail [routes-historydrawer--phone-detail]

routes/ImportWizard
  Docs routes-importwizard--docs
  Pick [routes-importwizard--pick]
  Dotenv Source [routes-importwizard--dotenv-source]
  Invalid Lines [routes-importwizard--invalid-lines]
  Connector Source [routes-importwizard--connector-source]
  Cli Guidance [routes-importwizard--cli-guidance]
  Classify [routes-importwizard--classify]
  Review [routes-importwizard--review]
  Result [routes-importwizard--result]
  Import Refused [routes-importwizard--import-refused]
  Review Failed [routes-importwizard--review-failed]
  Git Managed [routes-importwizard--git-managed]

routes/InstanceAdmin
  Docs routes-instanceadmin--docs
  Populated [routes-instanceadmin--populated]
  Not Disclosed [routes-instanceadmin--not-disclosed]
  Loading [routes-instanceadmin--loading]
  Second Factor Required [routes-instanceadmin--second-factor-required]
  Failed [routes-instanceadmin--failed]
  Editing Credential Policy [routes-instanceadmin--editing-credential-policy]
  Rotate Dek Confirm [routes-instanceadmin--rotate-dek-confirm]

routes/InstanceConfig
  Docs routes-instanceconfig--docs
  Active [routes-instanceconfig--active]
  Unmanaged [routes-instanceconfig--unmanaged]
  Pending [routes-instanceconfig--pending]
  Partial [routes-instanceconfig--partial]
  Recovery Required [routes-instanceconfig--recovery-required]
  Loading [routes-instanceconfig--loading]
  Not Disclosed [routes-instanceconfig--not-disclosed]
  Failed [routes-instanceconfig--failed]
  Test Mail Ceremony [routes-instanceconfig--test-mail-ceremony]

routes/InviteDialog
  Docs routes-invitedialog--docs
  Org Scope [routes-invitedialog--org-scope]
  Instance Scope [routes-invitedialog--instance-scope]

routes/KeyDeclarationDetail
  Docs routes-keydeclarationdetail--docs
  Editable [routes-keydeclarationdetail--editable]
  Deprecated [routes-keydeclarationdetail--deprecated]
  Reclassify Confirm [routes-keydeclarationdetail--reclassify-confirm]
  Git Read Only [routes-keydeclarationdetail--git-read-only]
  Source Failed [routes-keydeclarationdetail--source-failed]
  Loading [routes-keydeclarationdetail--loading]
  Gone [routes-keydeclarationdetail--gone]
  Failed [routes-keydeclarationdetail--failed]

routes/LastApplyProvenance
  Docs routes-lastapplyprovenance--docs
  Labelled [routes-lastapplyprovenance--labelled]
  Bare [routes-lastapplyprovenance--bare]

routes/LeaseActionDialog
  Docs routes-leaseactiondialog--docs
  Renew [routes-leaseactiondialog--renew]
  Renew Ceiling Refused [routes-leaseactiondialog--renew-ceiling-refused]
  Revoke [routes-leaseactiondialog--revoke]
  Settle [routes-leaseactiondialog--settle]
  Busy [routes-leaseactiondialog--busy]
  Failed [routes-leaseactiondialog--failed]

routes/LeaseMintDialog
  Docs routes-leasemintdialog--docs
  Idle [routes-leasemintdialog--idle]
  Ceiling Refused [routes-leasemintdialog--ceiling-refused]
  No Session [routes-leasemintdialog--no-session]
  Submitting [routes-leasemintdialog--submitting]
  Failed [routes-leasemintdialog--failed]
  Disclosed [routes-leasemintdialog--disclosed]
  Held Back [routes-leasemintdialog--held-back]

routes/Login
  Docs routes-login--docs
  With Providers [routes-login--with-providers]
  Password Step [routes-login--password-step]
  Paused [routes-login--paused]
  Local Only [routes-login--local-only]

routes/MachineAccess
  Docs routes-machineaccess--docs
  Populated [routes-machineaccess--populated]
  Expanded Row [routes-machineaccess--expanded-row]
  Federation Tab [routes-machineaccess--federation-tab]
  Providers Tab [routes-machineaccess--providers-tab]
  Kubernetes Tab [routes-machineaccess--kubernetes-tab]
  Kubernetes Tab Unsupported [routes-machineaccess--kubernetes-tab-unsupported]
  Leases Tab [routes-machineaccess--leases-tab]
  Empty [routes-machineaccess--empty]
  Loading [routes-machineaccess--loading]
  Refused [routes-machineaccess--refused]
  Failed [routes-machineaccess--failed]

routes/MachineRevealDialog
  Docs routes-machinerevealdialog--docs
  Enable [routes-machinerevealdialog--enable]
  Withdraw [routes-machinerevealdialog--withdraw]
  Busy [routes-machinerevealdialog--busy]
  Failed [routes-machinerevealdialog--failed]

routes/MachineRevokeCredentialDialog
  Docs routes-machinerevokecredentialdialog--docs
  Default [routes-machinerevokecredentialdialog--default]
  Busy [routes-machinerevokecredentialdialog--busy]
  Failed [routes-machinerevokecredentialdialog--failed]

routes/Matrix
  Docs routes-matrix--docs
  Populated [routes-matrix--populated]
  Degraded [routes-matrix--degraded]
  Git Managed [routes-matrix--git-managed]
  Empty [routes-matrix--empty]
  No Environments [routes-matrix--no-environments]
  Loading [routes-matrix--loading]
  Forbidden [routes-matrix--forbidden]
  Failed [routes-matrix--failed]

routes/MatrixKeyCreate
  Docs routes-matrixkeycreate--docs
  Default [routes-matrixkeycreate--default]

routes/MatrixPublishSheet
  Docs routes-matrixpublishsheet--docs
  Default [routes-matrixpublishsheet--default]
  Blocked [routes-matrixpublishsheet--blocked]
  Protected [routes-matrixpublishsheet--protected]

routes/MatrixRowEditor
  Docs routes-matrixroweditor--docs
  Default [routes-matrixroweditor--default]
  Copy Disclosure [routes-matrixroweditor--copy-disclosure]
  Edits [routes-matrixroweditor--edits]
  Long Value [routes-matrixroweditor--long-value]
  Long Secret Value [routes-matrixroweditor--long-secret-value]

routes/Members
  Docs routes-members--docs
  Populated [routes-members--populated]
  Load Error [routes-members--load-error]
  With Access Rules [routes-members--with-access-rules]
  Project With Access Rules [routes-members--project-with-access-rules]

routes/MintConnectionForm
  Docs routes-mintconnectionform--docs
  Default [routes-mintconnectionform--default]
  With Error [routes-mintconnectionform--with-error]

routes/MintDialog
  Docs routes-mintdialog--docs
  Reviewing [routes-mintdialog--reviewing]
  Reviewing No Reach [routes-mintdialog--reviewing-no-reach]
  Rotating [routes-mintdialog--rotating]
  Submitting [routes-mintdialog--submitting]
  Failed [routes-mintdialog--failed]
  Disclosed [routes-mintdialog--disclosed]
  Held Back [routes-mintdialog--held-back]

routes/OIDCDone
  Docs routes-oidcdone--docs
  No Transaction [routes-oidcdone--no-transaction]
  Login Refused [routes-oidcdone--login-refused]
  Link Refused [routes-oidcdone--link-refused]
  Reauth Refused [routes-oidcdone--reauth-refused]

routes/OidcProvidersPanel
  Docs routes-oidcproviderspanel--docs
  Populated [routes-oidcproviderspanel--populated]
  Empty [routes-oidcproviderspanel--empty]
  Loading [routes-oidcproviderspanel--loading]
  Second Factor Required [routes-oidcproviderspanel--second-factor-required]
  Failed [routes-oidcproviderspanel--failed]
  Reconfiguring [routes-oidcproviderspanel--reconfiguring]
  Delete Confirm [routes-oidcproviderspanel--delete-confirm]

routes/OpsDiagnosticBanners
  Docs routes-opsdiagnosticbanners--docs
  Error Severity [routes-opsdiagnosticbanners--error-severity]
  Warn [routes-opsdiagnosticbanners--warn]
  Unknown [routes-opsdiagnosticbanners--unknown]
  Css Check [routes-opsdiagnosticbanners--css-check]

routes/OrgSettings
  Docs routes-orgsettings--docs
  Populated [routes-orgsettings--populated]
  Empty [routes-orgsettings--empty]
  Loading [routes-orgsettings--loading]
  Failed [routes-orgsettings--failed]

routes/PinReleaseOutcome
  Docs routes-pinreleaseoutcome--docs
  Retained [routes-pinreleaseoutcome--retained]
  Collection Eligible [routes-pinreleaseoutcome--collection-eligible]
  Already Collected [routes-pinreleaseoutcome--already-collected]

routes/Placeholder
  Docs routes-placeholder--docs
  Default [routes-placeholder--default]

routes/ProfileUpdateBadge
  Docs routes-profileupdatebadge--docs
  Default [routes-profileupdatebadge--default]
  Multiple Versions [routes-profileupdatebadge--multiple-versions]

routes/Projects
  Docs routes-projects--docs
  Empty [routes-projects--empty]
  Populated [routes-projects--populated]
  Load Error [routes-projects--load-error]

routes/ProjectSettings
  Docs routes-projectsettings--docs
  Administrable [routes-projectsettings--administrable]
  Unlimited Retention Override [routes-projectsettings--unlimited-retention-override]
  Member With Environments [routes-projectsettings--member-with-environments]
  Load Error [routes-projectsettings--load-error]

routes/ProviderDiscoveryAlert
  Docs routes-providerdiscoveryalert--docs
  Default [routes-providerdiscoveryalert--default]

routes/Reconnect
  Docs routes-reconnect--docs
  Contacting [routes-reconnect--contacting]
  Ready [routes-reconnect--ready]
  Failed [routes-reconnect--failed]

routes/RemoteCard
  Docs routes-remotecard--docs
  Healthy [routes-remotecard--healthy]
  Unreachable [routes-remotecard--unreachable]
  Credential Rejected [routes-remotecard--credential-rejected]
  Duplicate [routes-remotecard--duplicate]

routes/RetentionBoundsFields
  Docs routes-retentionboundsfields--docs
  Days [routes-retentionboundsfields--days]
  Exact [routes-retentionboundsfields--exact]
  Absent [routes-retentionboundsfields--absent]

routes/RevisionDiff
  Docs routes-revisiondiff--docs
  Populated [routes-revisiondiff--populated]
  Revealed [routes-revisiondiff--revealed]
  Empty [routes-revisiondiff--empty]
  Loading [routes-revisiondiff--loading]
  Failed [routes-revisiondiff--failed]

routes/RevokeConnectionDialog
  Docs routes-revokeconnectiondialog--docs
  Default [routes-revokeconnectiondialog--default]

routes/RevokeCredentialDialog
  Docs routes-revokecredentialdialog--docs
  Default [routes-revokecredentialdialog--default]
  Confirm [routes-revokecredentialdialog--confirm]
  Busy [routes-revokecredentialdialog--busy]

routes/SamlProvidersPanel
  Docs routes-samlproviderspanel--docs
  Populated [routes-samlproviderspanel--populated]
  Empty [routes-samlproviderspanel--empty]
  Loading [routes-samlproviderspanel--loading]
  Second Factor Required [routes-samlproviderspanel--second-factor-required]
  Failed [routes-samlproviderspanel--failed]
  Creating [routes-samlproviderspanel--creating]
  Metadata Diff [routes-samlproviderspanel--metadata-diff]

routes/SamlSpKeysPanel
  Docs routes-samlspkeyspanel--docs
  Populated [routes-samlspkeyspanel--populated]
  Loading [routes-samlspkeyspanel--loading]
  Second Factor Required [routes-samlspkeyspanel--second-factor-required]
  Failed [routes-samlspkeyspanel--failed]
  Retire Confirm [routes-samlspkeyspanel--retire-confirm]
  Compromise Retire Confirm [routes-samlspkeyspanel--compromise-retire-confirm]

routes/ScanBlockDialog
  Docs routes-scanblockdialog--docs
  Overridable [routes-scanblockdialog--overridable]
  Hard Block [routes-scanblockdialog--hard-block]

routes/ScanWarnDialog
  Docs routes-scanwarndialog--docs
  Default [routes-scanwarndialog--default]

routes/ScimProvisioning
  Docs routes-scimprovisioning--docs
  Administering [routes-scimprovisioning--administering]
  Unselected [routes-scimprovisioning--unselected]
  Empty [routes-scimprovisioning--empty]
  Failed [routes-scimprovisioning--failed]

routes/Sections
  Docs routes-sections--docs
  Default Panel [routes-sections--default-panel]
  Danger Panel [routes-sections--danger-panel]
  Question Panel [routes-sections--question-panel]
  Tight Panel [routes-sections--tight-panel]
  Jump [routes-sections--jump]
  Disclosure [routes-sections--disclosure]
  Copy Once [routes-sections--copy-once]
  Consequences [routes-sections--consequences]
  Typed Confirm [routes-sections--typed-confirm]

routes/SetCredentialDialog
  Docs routes-setcredentialdialog--docs
  Replace [routes-setcredentialdialog--replace]
  First Credential [routes-setcredentialdialog--first-credential]
  Credential Required [routes-setcredentialdialog--credential-required]
  Busy [routes-setcredentialdialog--busy]
  Failed [routes-setcredentialdialog--failed]

routes/SidebarLinkItem
  Docs routes-sidebarlinkitem--docs
  Default [routes-sidebarlinkitem--default]
  Active [routes-sidebarlinkitem--active]
  Disabled [routes-sidebarlinkitem--disabled]
  Members Active [routes-sidebarlinkitem--members-active]

routes/SidebarVersion
  Docs routes-sidebarversion--docs
  Default [routes-sidebarversion--default]
  Absent [routes-sidebarversion--absent]

routes/StepUpBanner
  Docs routes-stepupbanner--docs
  Authenticator Code [routes-stepupbanner--authenticator-code]
  Passkey [routes-stepupbanner--passkey]

routes/SystemProjectNotice
  Docs routes-systemprojectnotice--docs
  Shown [routes-systemprojectnotice--shown]

routes/SystemScopeRefusal
  Docs routes-systemscoperefusal--docs
  Adapters [routes-systemscoperefusal--adapters]
  Machine Access [routes-systemscoperefusal--machine-access]
  Scim [routes-systemscoperefusal--scim]

routes/TargetForm
  Docs routes-targetform--docs
  Default [routes-targetform--default]
  Locked Routing [routes-targetform--locked-routing]
  Organization Selected [routes-targetform--organization-selected]
  Empty Keys [routes-targetform--empty-keys]
  Busy [routes-targetform--busy]

routes/TemporaryAccess
  Docs routes-temporaryaccess--docs
  Populated [routes-temporaryaccess--populated]
  Requester [routes-temporaryaccess--requester]
  Loading [routes-temporaryaccess--loading]

routes/ThemeToggle
  Docs routes-themetoggle--docs
  Default [routes-themetoggle--default]
  Toggles [routes-themetoggle--toggles]

routes/UpdateJobStatus
  Docs routes-updatejobstatus--docs
  Queued [routes-updatejobstatus--queued]
  Running [routes-updatejobstatus--running]
  Succeeded [routes-updatejobstatus--succeeded]
  Failed [routes-updatejobstatus--failed]

routes/Values
  Docs routes-values--docs
  Populated [routes-values--populated]
  Window Live [routes-values--window-live]
  Protected Locked [routes-values--protected-locked]
  Write Only [routes-values--write-only]
  Empty [routes-values--empty]
  Loading [routes-values--loading]
  Failed [routes-values--failed]

routes/WorkspaceApprove
  Docs routes-workspaceapprove--docs
  Nothing To Authorize [routes-workspaceapprove--nothing-to-authorize]
  Loading [routes-workspaceapprove--loading]
  Establishment [routes-workspaceapprove--establishment]
  Step Up [routes-workspaceapprove--step-up]
  Failed [routes-workspaceapprove--failed]
  Sign In [routes-workspaceapprove--sign-in]

routes/WorkspaceCallback
  Docs routes-workspacecallback--docs
  No Result [routes-workspacecallback--no-result]
  Could Not Close [routes-workspacecallback--could-not-close]

routes/WorkspaceScope
  Docs routes-workspacescope--docs
  Loading [routes-workspacescope--loading]
  Unknown Remote [routes-workspacescope--unknown-remote]
  Reconnect Required [routes-workspacescope--reconnect-required]
  Checking [routes-workspacescope--checking]
  Failed [routes-workspacescope--failed]
  Connected [routes-workspacescope--connected]

routes/WorkspaceStepUp
  Docs routes-workspacestepup--docs
  Disconnected [routes-workspacestepup--disconnected]
  Contacting [routes-workspacestepup--contacting]
  Ready [routes-workspacestepup--ready]
  Failed [routes-workspacestepup--failed]

ui/Alert
  Docs ui-alert--docs
  Danger [ui-alert--danger]
  Done [ui-alert--done]
  Warn [ui-alert--warn]
  Info [ui-alert--info]
  With Action [ui-alert--with-action]
  All States [ui-alert--all-states]
  Roles Match Tone [ui-alert--roles-match-tone]

ui/auth/AuthenticatorCodeField
  Docs ui-auth-authenticatorcodefield--docs
  Default [ui-auth-authenticatorcodefield--default]
  Checking [ui-auth-authenticatorcodefield--checking]
  Submits Trimmed And Clears [ui-auth-authenticatorcodefield--submits-trimmed-and-clears]

ui/auth/LoginFlow
  Docs ui-auth-loginflow--docs
  Password Then Authenticator [ui-auth-loginflow--password-then-authenticator]
  Authenticator Code Refused [ui-auth-loginflow--authenticator-code-refused]
  Unenrolled Is Gated Into Setup [ui-auth-loginflow--unenrolled-is-gated-into-setup]
  Unenrolled Allowed By Policy [ui-auth-loginflow--unenrolled-allowed-by-policy]
  Passkey Passwordless [ui-auth-loginflow--passkey-passwordless]
  Identity Provider [ui-auth-loginflow--identity-provider]
  Sign Up Through Provider [ui-auth-loginflow--sign-up-through-provider]

ui/auth/LoginForm
  Docs ui-auth-loginform--docs
  With Providers [ui-auth-loginform--with-providers]
  Social Providers [ui-auth-loginform--social-providers]
  Last Used Provider [ui-auth-loginform--last-used-provider]
  Last Used Password [ui-auth-loginform--last-used-password]
  Last Used Passkey [ui-auth-loginform--last-used-passkey]
  Local Only [ui-auth-loginform--local-only]
  Password And Passkey [ui-auth-loginform--password-and-passkey]
  Password Step [ui-auth-loginform--password-step]
  Waiting For Passkey [ui-auth-loginform--waiting-for-passkey]
  Contacting Provider [ui-auth-loginform--contacting-provider]
  Refused [ui-auth-loginform--refused]
  Paused [ui-auth-loginform--paused]
  Door Open [ui-auth-loginform--door-open]
  Sign Up Door [ui-auth-loginform--sign-up-door]
  Opens On Sign Up [ui-auth-loginform--opens-on-sign-up]
  Opens On Sign Up While Closed [ui-auth-loginform--opens-on-sign-up-while-closed]
  Sign Up Confirmation [ui-auth-loginform--sign-up-confirmation]
  Sign Up Back Links [ui-auth-loginform--sign-up-back-links]
  Provider Starts From Step One [ui-auth-loginform--provider-starts-from-step-one]
  Submits And Clears Password [ui-auth-loginform--submits-and-clears-password]

ui/auth/ProofDialog
  Docs ui-auth-proofdialog--docs
  Reauth Gated [ui-auth-proofdialog--reauth-gated]
  Authenticator Code [ui-auth-proofdialog--authenticator-code]
  Refused [ui-auth-proofdialog--refused]

ui/auth/ProviderButton
  Docs ui-auth-providerbutton--docs
  Google [ui-auth-providerbutton--google]
  Google Sign Up [ui-auth-providerbutton--google-sign-up]
  Microsoft [ui-auth-providerbutton--microsoft]
  Microsoft Sign Up [ui-auth-providerbutton--microsoft-sign-up]
  Git Hub [ui-auth-providerbutton--git-hub]
  Git Hub Sign Up [ui-auth-providerbutton--git-hub-sign-up]
  Plain [ui-auth-providerbutton--plain]
  Contacting [ui-auth-providerbutton--contacting]
  Last Used [ui-auth-providerbutton--last-used]
  Disabled [ui-auth-providerbutton--disabled]
  Fires On Click [ui-auth-providerbutton--fires-on-click]

ui/auth/QrCode
  Docs ui-auth-qrcode--docs
  Default [ui-auth-qrcode--default]
  Named And Scannable [ui-auth-qrcode--named-and-scannable]

ui/auth/SecondFactorChallenge
  Docs ui-auth-secondfactorchallenge--docs
  Authenticator And Passkey [ui-auth-secondfactorchallenge--authenticator-and-passkey]
  Authenticator Only [ui-auth-secondfactorchallenge--authenticator-only]
  Passkey Only [ui-auth-secondfactorchallenge--passkey-only]
  No Factor Presentable [ui-auth-secondfactorchallenge--no-factor-presentable]
  Checking Code [ui-auth-secondfactorchallenge--checking-code]
  Waiting For Passkey [ui-auth-secondfactorchallenge--waiting-for-passkey]
  Code Refused [ui-auth-secondfactorchallenge--code-refused]
  No Skip Control [ui-auth-secondfactorchallenge--no-skip-control]
  Submits Trimmed Code [ui-auth-secondfactorchallenge--submits-trimmed-code]

ui/auth/SecondFactorSetup
  Docs ui-auth-secondfactorsetup--docs
  Confirm Password [ui-auth-secondfactorsetup--confirm-password]
  Confirm Password Refused [ui-auth-secondfactorsetup--confirm-password-refused]
  Choose [ui-auth-secondfactorsetup--choose]
  Choose No Passkey Support [ui-auth-secondfactorsetup--choose-no-passkey-support]
  Authenticator [ui-auth-secondfactorsetup--authenticator]
  Authenticator Code Refused [ui-auth-secondfactorsetup--authenticator-code-refused]
  Recovery Codes [ui-auth-secondfactorsetup--recovery-codes]
  No Skip On Any Step [ui-auth-secondfactorsetup--no-skip-on-any-step]
  Codes Need Acknowledgement [ui-auth-secondfactorsetup--codes-need-acknowledgement]

ui/Badge
  Docs ui-badge--docs
  Neutral [ui-badge--neutral]
  Danger [ui-badge--danger]
  Changed [ui-badge--changed]
  Ok [ui-badge--ok]
  Mono [ui-badge--mono]
  All Tones [ui-badge--all-tones]

ui/Button
  Docs ui-button--docs
  Secondary [ui-button--secondary]
  Primary [ui-button--primary]
  Danger [ui-button--danger]
  Quiet [ui-button--quiet]
  Disabled [ui-button--disabled]
  Icon [ui-button--icon]
  Clicks [ui-button--clicks]
  All Variants [ui-button--all-variants]

ui/Checkbox
  Docs ui-checkbox--docs
  Default [ui-checkbox--default]
  Checked [ui-checkbox--checked]
  Disabled [ui-checkbox--disabled]
  Toggles [ui-checkbox--toggles]
  Label Toggles [ui-checkbox--label-toggles]
  Hit Box Is The Input [ui-checkbox--hit-box-is-the-input]
  All States [ui-checkbox--all-states]

ui/ChoiceGroup
  Docs ui-choicegroup--docs
  Stack [ui-choicegroup--stack]
  Wrap [ui-choicegroup--wrap]
  Nowrap [ui-choicegroup--nowrap]
  Two Columns [ui-choicegroup--two-columns]
  Three Columns [ui-choicegroup--three-columns]
  Chips [ui-choicegroup--chips]
  With Hint [ui-choicegroup--with-hint]
  Radios [ui-choicegroup--radios]
  All States [ui-choicegroup--all-states]
  Legend Names The Group [ui-choicegroup--legend-names-the-group]
  Columns Are A Grid [ui-choicegroup--columns-are-a-grid]
  Nowrap Scrolls [ui-choicegroup--nowrap-scrolls]
  Chips Mark Checked [ui-choicegroup--chips-mark-checked]

ui/Dialog
  Docs ui-dialog--docs
  Decision [ui-dialog--decision]
  With Refusal [ui-dialog--with-refusal]
  Wide [ui-dialog--wide]
  Must Acknowledge [ui-dialog--must-acknowledge]
  Pinned Actions [ui-dialog--pinned-actions]
  All States [ui-dialog--all-states]
  Backdrop Click [ui-dialog--backdrop-click]
  Is Modal And Labelled [ui-dialog--is-modal-and-labelled]
  Mono Title [ui-dialog--mono-title]

ui/Disclosure
  Docs ui-disclosure--docs
  Closed [ui-disclosure--closed]
  Open [ui-disclosure--open]
  Toggles [ui-disclosure--toggles]

ui/Field
  Docs ui-field--docs
  Default [ui-field--default]
  With Hint [ui-field--with-hint]
  With Error [ui-field--with-error]

ui/Glyph
  Docs ui-glyph--docs
  Decorative [ui-glyph--decorative]
  Named [ui-glyph--named]
  All States [ui-glyph--all-states]
  Decorative Is Hidden [ui-glyph--decorative-is-hidden]

ui/Input
  Docs ui-input--docs
  Default [ui-input--default]
  Password [ui-input--password]
  Password Revealable [ui-input--password-revealable]
  Disabled [ui-input--disabled]
  Mono [ui-input--mono]
  Mono Targets The Control [ui-input--mono-targets-the-control]
  With Hint [ui-input--with-hint]
  With Error [ui-input--with-error]
  Error Is Wired [ui-input--error-is-wired]
  Label Is Wired [ui-input--label-is-wired]
  All States [ui-input--all-states]
  External Description Is Merged [ui-input--external-description-is-merged]
  External Invalid Is Kept [ui-input--external-invalid-is-kept]

ui/Menu
  Docs ui-menu--docs
  Default [ui-menu--default]
  Opens [ui-menu--opens]
  Selects [ui-menu--selects]
  Keyboard [ui-menu--keyboard]
  Tab Leaves [ui-menu--tab-leaves]
  Selects By Keyboard [ui-menu--selects-by-keyboard]
  Disabled Skipped [ui-menu--disabled-skipped]

ui/Radio
  Docs ui-radio--docs
  Default [ui-radio--default]
  Checked [ui-radio--checked]
  Disabled [ui-radio--disabled]
  Selects [ui-radio--selects]
  All States [ui-radio--all-states]

ui/Select
  Docs ui-select--docs
  Default [ui-select--default]
  Disabled [ui-select--disabled]
  Label Is Wired [ui-select--label-is-wired]
  All States [ui-select--all-states]
  External Description Is Merged [ui-select--external-description-is-merged]
  External Invalid Is Kept [ui-select--external-invalid-is-kept]

ui/Tabs
  Docs ui-tabs--docs
  Default [ui-tabs--default]
  Second Selected [ui-tabs--second-selected]
  Keyboard Moves And Selects [ui-tabs--keyboard-moves-and-selects]
  Two Demos Stay Independent [ui-tabs--two-demos-stay-independent]

ui/Textarea
  Docs ui-textarea--docs
  Default [ui-textarea--default]
  Filled [ui-textarea--filled]
  Disabled [ui-textarea--disabled]
  Label Is Wired [ui-textarea--label-is-wired]
  All States [ui-textarea--all-states]
  External Description Is Merged [ui-textarea--external-description-is-merged]
  External Invalid Is Kept [ui-textarea--external-invalid-is-kept]

ui/ThemeIcon
  Docs ui-themeicon--docs
  Sun [ui-themeicon--sun]
  Moon [ui-themeicon--moon]

ui/ToggleChip
  Docs ui-togglechip--docs
  Not Included [ui-togglechip--not-included]
  Included [ui-togglechip--included]
  Implied [ui-togglechip--implied]
  Left Out [ui-togglechip--left-out]
  Disabled [ui-togglechip--disabled]
  Exclude Toggles [ui-togglechip--exclude-toggles]

ui/Tokens
  Docs ui-tokens--docs
  Design System [ui-tokens--design-system]
  Every Token Resolves [ui-tokens--every-token-resolves]

ui/Typography
  Docs ui-typography--docs
  Scale [ui-typography--scale]

```

## After sidebar, source-sorted inventory

```text
Design system/Alert
  Docs ui-alert--docs
  Danger [ui-alert--danger]
  Done [ui-alert--done]
  Warn [ui-alert--warn]
  Info [ui-alert--info]
  With Action [ui-alert--with-action]
  All States [ui-alert--all-states]

Design system/Auth/AuthenticatorCodeField
  Docs ui-auth-authenticatorcodefield--docs
  Default [ui-auth-authenticatorcodefield--default]
  Checking [ui-auth-authenticatorcodefield--checking]
  Submits Trimmed And Clears [ui-auth-authenticatorcodefield--submits-trimmed-and-clears]

Design system/Auth/LocalSignupForm
  Docs design-system-auth-localsignupform--docs
  Entry [design-system-auth-localsignupform--entry]
  No Organisation [design-system-auth-localsignupform--no-organisation]
  Request And Resend [design-system-auth-localsignupform--request-and-resend]
  Busy [design-system-auth-localsignupform--busy]
  Refused [design-system-auth-localsignupform--refused]

Design system/Auth/LoginFlow
  Docs ui-auth-loginflow--docs
  Password Then Authenticator [ui-auth-loginflow--password-then-authenticator]
  Authenticator Code Refused [ui-auth-loginflow--authenticator-code-refused]
  Unenrolled Is Gated Into Setup [ui-auth-loginflow--unenrolled-is-gated-into-setup]
  Unenrolled Allowed By Policy [ui-auth-loginflow--unenrolled-allowed-by-policy]
  Passkey Passwordless [ui-auth-loginflow--passkey-passwordless]
  Identity Provider [ui-auth-loginflow--identity-provider]
  Sign Up Through Provider [ui-auth-loginflow--sign-up-through-provider]

Design system/Auth/LoginForm
  Docs ui-auth-loginform--docs
  With Providers [ui-auth-loginform--with-providers]
  Social Providers [ui-auth-loginform--social-providers]
  Last Used Provider [ui-auth-loginform--last-used-provider]
  Last Used Password [ui-auth-loginform--last-used-password]
  Last Used Passkey [ui-auth-loginform--last-used-passkey]
  Local Only [ui-auth-loginform--local-only]
  Password And Passkey [ui-auth-loginform--password-and-passkey]
  Password Step [ui-auth-loginform--password-step]
  Waiting For Passkey [ui-auth-loginform--waiting-for-passkey]
  Contacting Provider [ui-auth-loginform--contacting-provider]
  Refused [ui-auth-loginform--refused]
  Paused [ui-auth-loginform--paused]
  Door Open [ui-auth-loginform--door-open]
  Sign Up Door [ui-auth-loginform--sign-up-door]
  Opens On Sign Up [ui-auth-loginform--opens-on-sign-up]
  Opens On Sign Up While Closed [ui-auth-loginform--opens-on-sign-up-while-closed]
  Sign Up Confirmation [ui-auth-loginform--sign-up-confirmation]
  Sign Up Back Links [ui-auth-loginform--sign-up-back-links]
  Provider Starts From Step One [ui-auth-loginform--provider-starts-from-step-one]
  Submits And Clears Password [ui-auth-loginform--submits-and-clears-password]

Design system/Auth/ProofDialog
  Docs ui-auth-proofdialog--docs
  Reauth Gated [ui-auth-proofdialog--reauth-gated]
  Authenticator Code [ui-auth-proofdialog--authenticator-code]
  Refused [ui-auth-proofdialog--refused]

Design system/Auth/ProviderButton
  Docs ui-auth-providerbutton--docs
  Google [ui-auth-providerbutton--google]
  Google Sign Up [ui-auth-providerbutton--google-sign-up]
  Microsoft [ui-auth-providerbutton--microsoft]
  Microsoft Sign Up [ui-auth-providerbutton--microsoft-sign-up]
  Git Hub [ui-auth-providerbutton--git-hub]
  Git Hub Sign Up [ui-auth-providerbutton--git-hub-sign-up]
  Plain [ui-auth-providerbutton--plain]
  Contacting [ui-auth-providerbutton--contacting]
  Last Used [ui-auth-providerbutton--last-used]
  Disabled [ui-auth-providerbutton--disabled]
  Fires On Click [ui-auth-providerbutton--fires-on-click]

Design system/Auth/QrCode
  Docs ui-auth-qrcode--docs
  Default [ui-auth-qrcode--default]
  Named And Scannable [ui-auth-qrcode--named-and-scannable]

Design system/Auth/SecondFactorChallenge
  Docs ui-auth-secondfactorchallenge--docs
  Authenticator And Passkey [ui-auth-secondfactorchallenge--authenticator-and-passkey]
  Authenticator Only [ui-auth-secondfactorchallenge--authenticator-only]
  Passkey Only [ui-auth-secondfactorchallenge--passkey-only]
  No Factor Presentable [ui-auth-secondfactorchallenge--no-factor-presentable]
  Checking Code [ui-auth-secondfactorchallenge--checking-code]
  Waiting For Passkey [ui-auth-secondfactorchallenge--waiting-for-passkey]
  Code Refused [ui-auth-secondfactorchallenge--code-refused]
  No Skip Control [ui-auth-secondfactorchallenge--no-skip-control]
  Submits Trimmed Code [ui-auth-secondfactorchallenge--submits-trimmed-code]

Design system/Auth/SecondFactorSetup
  Docs ui-auth-secondfactorsetup--docs
  Confirm Password [ui-auth-secondfactorsetup--confirm-password]
  Confirm Password Refused [ui-auth-secondfactorsetup--confirm-password-refused]
  Choose [ui-auth-secondfactorsetup--choose]
  Choose No Passkey Support [ui-auth-secondfactorsetup--choose-no-passkey-support]
  Authenticator [ui-auth-secondfactorsetup--authenticator]
  Authenticator Code Refused [ui-auth-secondfactorsetup--authenticator-code-refused]
  Recovery Codes [ui-auth-secondfactorsetup--recovery-codes]
  No Skip On Any Step [ui-auth-secondfactorsetup--no-skip-on-any-step]
  Codes Need Acknowledgement [ui-auth-secondfactorsetup--codes-need-acknowledgement]

Design system/Badge
  Docs ui-badge--docs
  Neutral [ui-badge--neutral]
  All Tones [ui-badge--all-tones]

Design system/Button
  Docs ui-button--docs
  Secondary [ui-button--secondary]
  Primary [ui-button--primary]
  Disabled [ui-button--disabled]
  Icon [ui-button--icon]
  All Variants [ui-button--all-variants]

Design system/CeremonyNotice
  Docs design-system-ceremonynotice--docs
  Default [design-system-ceremonynotice--default]
  Long Content [design-system-ceremonynotice--long-content]

Design system/Checkbox
  Docs ui-checkbox--docs
  Default [ui-checkbox--default]
  Checked [ui-checkbox--checked]
  Disabled [ui-checkbox--disabled]
  Hit Box Is The Input [ui-checkbox--hit-box-is-the-input]
  All States [ui-checkbox--all-states]

Design system/ChoiceGroup
  Docs ui-choicegroup--docs
  Stack [ui-choicegroup--stack]
  Wrap [ui-choicegroup--wrap]
  Nowrap [ui-choicegroup--nowrap]
  Two Columns [ui-choicegroup--two-columns]
  Three Columns [ui-choicegroup--three-columns]
  Chips [ui-choicegroup--chips]
  With Hint [ui-choicegroup--with-hint]
  Radios [ui-choicegroup--radios]
  All States [ui-choicegroup--all-states]
  Legend Names The Group [ui-choicegroup--legend-names-the-group]
  Columns Are A Grid [ui-choicegroup--columns-are-a-grid]
  Nowrap Scrolls [ui-choicegroup--nowrap-scrolls]
  Chips Mark Checked [ui-choicegroup--chips-mark-checked]

Design system/Dialog
  Docs ui-dialog--docs
  Decision [ui-dialog--decision]
  With Refusal [ui-dialog--with-refusal]
  Wide [ui-dialog--wide]
  Must Acknowledge [ui-dialog--must-acknowledge]
  Pinned Actions [ui-dialog--pinned-actions]
  Backdrop Click [ui-dialog--backdrop-click]
  Is Modal And Labelled [ui-dialog--is-modal-and-labelled]
  Mono Title [ui-dialog--mono-title]

Design system/Disclosure
  Docs ui-disclosure--docs
  Closed [ui-disclosure--closed]
  Open [ui-disclosure--open]
  Toggles [ui-disclosure--toggles]

Design system/Field
  Docs ui-field--docs
  Default [ui-field--default]
  With Hint [ui-field--with-hint]
  With Error [ui-field--with-error]

Design system/Glyph
  Docs ui-glyph--docs
  Decorative [ui-glyph--decorative]
  Named [ui-glyph--named]
  All States [ui-glyph--all-states]

Design system/Input
  Docs ui-input--docs
  Default [ui-input--default]
  Password [ui-input--password]
  Password Revealable [ui-input--password-revealable]
  Disabled [ui-input--disabled]
  Mono [ui-input--mono]
  Mono Targets The Control [ui-input--mono-targets-the-control]
  With Hint [ui-input--with-hint]
  With Error [ui-input--with-error]
  Error Is Wired [ui-input--error-is-wired]
  All States [ui-input--all-states]
  External Description Is Merged [ui-input--external-description-is-merged]
  External Invalid Is Kept [ui-input--external-invalid-is-kept]

Design system/Menu
  Docs ui-menu--docs
  Default [ui-menu--default]
  Opens [ui-menu--opens]
  Selects [ui-menu--selects]
  Keyboard [ui-menu--keyboard]
  Tab Leaves [ui-menu--tab-leaves]
  Selects By Keyboard [ui-menu--selects-by-keyboard]
  Disabled Skipped [ui-menu--disabled-skipped]

Design system/Radio
  Docs ui-radio--docs
  Default [ui-radio--default]
  Checked [ui-radio--checked]
  Disabled [ui-radio--disabled]
  All States [ui-radio--all-states]

Design system/Select
  Docs ui-select--docs
  Default [ui-select--default]
  Disabled [ui-select--disabled]
  All States [ui-select--all-states]
  External Description Is Merged [ui-select--external-description-is-merged]
  External Invalid Is Kept [ui-select--external-invalid-is-kept]

Design system/Tabs
  Docs ui-tabs--docs
  Default [ui-tabs--default]
  Second Selected [ui-tabs--second-selected]
  Keyboard Moves And Selects [ui-tabs--keyboard-moves-and-selects]
  Two Demos Stay Independent [ui-tabs--two-demos-stay-independent]

Design system/Textarea
  Docs ui-textarea--docs
  Default [ui-textarea--default]
  Filled [ui-textarea--filled]
  Disabled [ui-textarea--disabled]
  All States [ui-textarea--all-states]
  External Description Is Merged [ui-textarea--external-description-is-merged]
  External Invalid Is Kept [ui-textarea--external-invalid-is-kept]

Design system/ThemeIcon
  Docs ui-themeicon--docs
  Sun [ui-themeicon--sun]
  Moon [ui-themeicon--moon]

Design system/ToggleChip
  Docs ui-togglechip--docs
  Not Included [ui-togglechip--not-included]
  Included [ui-togglechip--included]
  Implied [ui-togglechip--implied]
  Left Out [ui-togglechip--left-out]
  Disabled [ui-togglechip--disabled]
  Exclude Toggles [ui-togglechip--exclude-toggles]

Design system/Tokens
  Docs ui-tokens--docs
  Design System [ui-tokens--design-system]
  Every Token Resolves [ui-tokens--every-token-resolves]

Design system/Typography
  Docs ui-typography--docs
  Scale [ui-typography--scale]

Features/Adapters/AwsAccessFields
  Docs features-adapters-awsaccessfields--docs
  Assume Role [features-adapters-awsaccessfields--assume-role]
  Web Identity [features-adapters-awsaccessfields--web-identity]
  Ambient [features-adapters-awsaccessfields--ambient]
  Static [features-adapters-awsaccessfields--static]

Features/Adapters/DeleteAdapterDialog
  Docs routes-deleteadapterdialog--docs
  Default [routes-deleteadapterdialog--default]
  Prune [routes-deleteadapterdialog--prune]
  Busy [routes-deleteadapterdialog--busy]

Features/Adapters/HealthChip
  Docs routes-healthchip--docs
  Never [routes-healthchip--never]
  Pending [routes-healthchip--pending]
  Converging [routes-healthchip--converging]
  Converged [routes-healthchip--converged]
  Degraded [routes-healthchip--degraded]
  Failed [routes-healthchip--failed]
  Paused [routes-healthchip--paused]
  Drift Attention [routes-healthchip--drift-attention]

Features/Adapters/TargetForm
  Docs routes-targetform--docs
  Default [routes-targetform--default]
  Locked Routing [routes-targetform--locked-routing]
  Organization Selected [routes-targetform--organization-selected]
  Empty Keys [routes-targetform--empty-keys]
  Busy [routes-targetform--busy]

Features/Identity/FederationIssuersPanel
  Docs routes-federationissuerspanel--docs
  Populated [routes-federationissuerspanel--populated]
  Empty [routes-federationissuerspanel--empty]
  Loading [routes-federationissuerspanel--loading]
  Second Factor Required [routes-federationissuerspanel--second-factor-required]
  Failed [routes-federationissuerspanel--failed]
  Creating [routes-federationissuerspanel--creating]
  Editing [routes-federationissuerspanel--editing]
  Delete Refused [routes-federationissuerspanel--delete-refused]

Features/Identity/OAuth2ProvidersPanel
  Docs features-identity-oauth2providerspanel--docs
  Populated [features-identity-oauth2providerspanel--populated]
  Empty [features-identity-oauth2providerspanel--empty]
  Loading [features-identity-oauth2providerspanel--loading]
  Second Factor Required [features-identity-oauth2providerspanel--second-factor-required]
  Failed [features-identity-oauth2providerspanel--failed]
  Reconfiguring [features-identity-oauth2providerspanel--reconfiguring]
  Delete Confirmation [features-identity-oauth2providerspanel--delete-confirmation]

Features/Identity/OidcProvidersPanel
  Docs routes-oidcproviderspanel--docs
  Populated [routes-oidcproviderspanel--populated]
  Empty [routes-oidcproviderspanel--empty]
  Loading [routes-oidcproviderspanel--loading]
  Second Factor Required [routes-oidcproviderspanel--second-factor-required]
  Failed [routes-oidcproviderspanel--failed]
  Reconfiguring [routes-oidcproviderspanel--reconfiguring]
  Delete Confirm [routes-oidcproviderspanel--delete-confirm]

Features/Identity/ProviderDiscoveryAlert
  Docs routes-providerdiscoveryalert--docs
  Default [routes-providerdiscoveryalert--default]

Features/Identity/SamlProvidersPanel
  Docs routes-samlproviderspanel--docs
  Populated [routes-samlproviderspanel--populated]
  Empty [routes-samlproviderspanel--empty]
  Loading [routes-samlproviderspanel--loading]
  Second Factor Required [routes-samlproviderspanel--second-factor-required]
  Failed [routes-samlproviderspanel--failed]
  Creating [routes-samlproviderspanel--creating]
  Metadata Diff [routes-samlproviderspanel--metadata-diff]

Features/Identity/SamlSpKeysPanel
  Docs routes-samlspkeyspanel--docs
  Populated [routes-samlspkeyspanel--populated]
  Loading [routes-samlspkeyspanel--loading]
  Second Factor Required [routes-samlspkeyspanel--second-factor-required]
  Failed [routes-samlspkeyspanel--failed]
  Retire Confirm [routes-samlspkeyspanel--retire-confirm]
  Compromise Retire Confirm [routes-samlspkeyspanel--compromise-retire-confirm]

Features/Instance/InstanceMailPanel
  Docs features-instance-instancemailpanel--docs
  Configured [features-instance-instancemailpanel--configured]
  Unconfigured [features-instance-instancemailpanel--unconfigured]
  Loading [features-instance-instancemailpanel--loading]
  Second Factor Required [features-instance-instancemailpanel--second-factor-required]
  Failed [features-instance-instancemailpanel--failed]
  Fresh Proof Required [features-instance-instancemailpanel--fresh-proof-required]

Features/Machine access/BindingCard
  Docs routes-bindingcard--docs
  Default [routes-bindingcard--default]
  Expiring Soon [routes-bindingcard--expiring-soon]
  Quarantined [routes-bindingcard--quarantined]
  Not Ready [routes-bindingcard--not-ready]

Features/Machine access/BindingDialog
  Docs routes-bindingdialog--docs
  Default [routes-bindingdialog--default]
  Git Hub Actions [routes-bindingdialog--git-hub-actions]
  Pull Request Event [routes-bindingdialog--pull-request-event]
  Audience Required [routes-bindingdialog--audience-required]
  Replace [routes-bindingdialog--replace]
  Busy [routes-bindingdialog--busy]
  Failed [routes-bindingdialog--failed]

Features/Machine access/CreateAccountDialog
  Docs routes-createaccountdialog--docs
  Default [routes-createaccountdialog--default]
  Name Required [routes-createaccountdialog--name-required]
  Busy [routes-createaccountdialog--busy]
  Failed [routes-createaccountdialog--failed]

Features/Machine access/CreateProviderDialog
  Docs routes-createproviderdialog--docs
  Default [routes-createproviderdialog--default]
  Fields Required [routes-createproviderdialog--fields-required]
  Busy [routes-createproviderdialog--busy]
  Failed [routes-createproviderdialog--failed]

Features/Machine access/DeleteAccountDialog
  Docs routes-deleteaccountdialog--docs
  Default [routes-deleteaccountdialog--default]
  One Credential [routes-deleteaccountdialog--one-credential]
  Busy [routes-deleteaccountdialog--busy]
  Failed [routes-deleteaccountdialog--failed]
  Failed May Have Committed [routes-deleteaccountdialog--failed-may-have-committed]

Features/Machine access/DeleteProviderDialog
  Docs routes-deleteproviderdialog--docs
  No Leases [routes-deleteproviderdialog--no-leases]
  Live Leases [routes-deleteproviderdialog--live-leases]
  Leases Unknown [routes-deleteproviderdialog--leases-unknown]
  Busy [routes-deleteproviderdialog--busy]
  Conflict [routes-deleteproviderdialog--conflict]
  Failed [routes-deleteproviderdialog--failed]

Features/Machine access/DeliveryTargets
  Docs routes-deliverytargets--docs
  Reported Healthy [routes-deliverytargets--reported-healthy]
  Reported Degraded [routes-deliverytargets--reported-degraded]
  Stale [routes-deliverytargets--stale]
  Refused [routes-deliverytargets--refused]
  Reporter Revoked [routes-deliverytargets--reporter-revoked]
  Quota Refused [routes-deliverytargets--quota-refused]
  Unknown [routes-deliverytargets--unknown]
  Empty [routes-deliverytargets--empty]
  Unsupported [routes-deliverytargets--unsupported]

Features/Machine access/GrantDialog
  Docs routes-grantdialog--docs
  Read Grant [routes-grantdialog--read-grant]
  Reveal Grant [routes-grantdialog--reveal-grant]
  Nothing To Widen [routes-grantdialog--nothing-to-widen]
  Catalogue Failed [routes-grantdialog--catalogue-failed]
  Catalogue Loading [routes-grantdialog--catalogue-loading]
  Reporting Grantable [routes-grantdialog--reporting-grantable]
  Reporting Not Grantable [routes-grantdialog--reporting-not-grantable]
  Report After The Fact [routes-grantdialog--report-after-the-fact]
  Reporting Unsupported [routes-grantdialog--reporting-unsupported]

Features/Machine access/LeaseActionDialog
  Docs routes-leaseactiondialog--docs
  Renew [routes-leaseactiondialog--renew]
  Renew Ceiling Refused [routes-leaseactiondialog--renew-ceiling-refused]
  Revoke [routes-leaseactiondialog--revoke]
  Settle [routes-leaseactiondialog--settle]
  Busy [routes-leaseactiondialog--busy]
  Failed [routes-leaseactiondialog--failed]

Features/Machine access/LeaseMintDialog
  Docs routes-leasemintdialog--docs
  Idle [routes-leasemintdialog--idle]
  Ceiling Refused [routes-leasemintdialog--ceiling-refused]
  No Session [routes-leasemintdialog--no-session]
  Submitting [routes-leasemintdialog--submitting]
  Failed [routes-leasemintdialog--failed]
  Disclosed [routes-leasemintdialog--disclosed]
  Held Back [routes-leasemintdialog--held-back]

Features/Machine access/MachineRevealDialog
  Docs routes-machinerevealdialog--docs
  Enable [routes-machinerevealdialog--enable]
  Withdraw [routes-machinerevealdialog--withdraw]
  Busy [routes-machinerevealdialog--busy]
  Failed [routes-machinerevealdialog--failed]

Features/Machine access/MachineRevokeCredentialDialog
  Docs routes-machinerevokecredentialdialog--docs
  Default [routes-machinerevokecredentialdialog--default]
  Busy [routes-machinerevokecredentialdialog--busy]
  Failed [routes-machinerevokecredentialdialog--failed]

Features/Machine access/MintDialog
  Docs routes-mintdialog--docs
  Reviewing [routes-mintdialog--reviewing]
  Reviewing No Reach [routes-mintdialog--reviewing-no-reach]
  Rotating [routes-mintdialog--rotating]
  Submitting [routes-mintdialog--submitting]
  Failed [routes-mintdialog--failed]
  Disclosed [routes-mintdialog--disclosed]
  Held Back [routes-mintdialog--held-back]

Features/Machine access/SetCredentialDialog
  Docs routes-setcredentialdialog--docs
  Replace [routes-setcredentialdialog--replace]
  First Credential [routes-setcredentialdialog--first-credential]
  Credential Required [routes-setcredentialdialog--credential-required]
  Busy [routes-setcredentialdialog--busy]
  Failed [routes-setcredentialdialog--failed]

Features/Members/Access rules
  Docs members-access-rules--docs
  Rules [members-access-rules--rules]
  Rules On A Project [members-access-rules--rules-on-a-project]
  Editor Folder Rule [members-access-rules--editor-folder-rule]
  Editor New Rule [members-access-rules--editor-new-rule]
  Editor Single Key Rule [members-access-rules--editor-single-key-rule]
  Editor Without Key Names [members-access-rules--editor-without-key-names]
  Who Can [members-access-rules--who-can]
  Who Can Without Key Names [members-access-rules--who-can-without-key-names]
  Key move confirmation [members-access-rules--key-move-confirmation]
  Key move, count only [members-access-rules--key-move-count-only]
  Glossary [members-access-rules--glossary]

Features/Members/InviteDialog
  Docs routes-invitedialog--docs
  Org Scope [routes-invitedialog--org-scope]
  Instance Scope [routes-invitedialog--instance-scope]

Features/Members/OpenRegistration
  Docs features-members-openregistration--docs
  Closed [features-members-openregistration--closed]
  Active [features-members-openregistration--active]
  Inactive Authority [features-members-openregistration--inactive-authority]
  Loading [features-members-openregistration--loading]
  Failed [features-members-openregistration--failed]
  Editor Validation [features-members-openregistration--editor-validation]

Features/PKI/CertificatesTab
  Docs features-pki-certificatestab--docs
  Populated [features-pki-certificatestab--populated]
  Empty [features-pki-certificatestab--empty]
  Loading [features-pki-certificatestab--loading]
  Incomplete [features-pki-certificatestab--incomplete]
  Issue From Csr [features-pki-certificatestab--issue-from-csr]
  Revoke Confirmation [features-pki-certificatestab--revoke-confirmation]

Features/PKI/PkiIssuersPanel
  Docs features-pki-pkiissuerspanel--docs
  Populated [features-pki-pkiissuerspanel--populated]
  Empty [features-pki-pkiissuerspanel--empty]
  Loading [features-pki-pkiissuerspanel--loading]
  Second Factor Required [features-pki-pkiissuerspanel--second-factor-required]
  Failed [features-pki-pkiissuerspanel--failed]

Features/PKI/PkiProfilesPanel
  Docs features-pki-pkiprofilespanel--docs
  Populated [features-pki-pkiprofilespanel--populated]
  Empty [features-pki-pkiprofilespanel--empty]
  Loading [features-pki-pkiprofilespanel--loading]
  Second Factor Required [features-pki-pkiprofilespanel--second-factor-required]
  Failed [features-pki-pkiprofilespanel--failed]
  Narrowing [features-pki-pkiprofilespanel--narrowing]
  Invalid Policy [features-pki-pkiprofilespanel--invalid-policy]

Features/Remotes/ConnectionMintDialog
  Docs routes-connectionmintdialog--docs
  Default [routes-connectionmintdialog--default]
  Clamped [routes-connectionmintdialog--clamped]

Features/Remotes/ConnectionRow
  Docs routes-connectionrow--docs
  Live [routes-connectionrow--live]
  Expired [routes-connectionrow--expired]
  Revoked [routes-connectionrow--revoked]
  Indefinite [routes-connectionrow--indefinite]

Features/Remotes/CredentialForm
  Docs routes-credentialform--docs
  Default [routes-credentialform--default]
  Entered [routes-credentialform--entered]
  Busy [routes-credentialform--busy]

Features/Remotes/MintConnectionForm
  Docs routes-mintconnectionform--docs
  Default [routes-mintconnectionform--default]
  With Error [routes-mintconnectionform--with-error]

Features/Remotes/RemoteCard
  Docs routes-remotecard--docs
  Healthy [routes-remotecard--healthy]
  Unreachable [routes-remotecard--unreachable]
  Credential Rejected [routes-remotecard--credential-rejected]
  Duplicate [routes-remotecard--duplicate]

Features/Remotes/RevokeConnectionDialog
  Docs routes-revokeconnectiondialog--docs
  Default [routes-revokeconnectiondialog--default]

Features/Remotes/RevokeCredentialDialog
  Docs routes-revokecredentialdialog--docs
  Default [routes-revokecredentialdialog--default]
  Confirm [routes-revokecredentialdialog--confirm]
  Busy [routes-revokecredentialdialog--busy]

Features/SSH/SSHCertificatesPanel
  Docs features-ssh-sshcertificatespanel--docs
  Empty [features-ssh-sshcertificatespanel--empty]
  No Environments [features-ssh-sshcertificatespanel--no-environments]
  Session Required [features-ssh-sshcertificatespanel--session-required]
  Populated [features-ssh-sshcertificatespanel--populated]
  Refused [features-ssh-sshcertificatespanel--refused]
  Create Ca [features-ssh-sshcertificatespanel--create-ca]

Features/Values/CatalogueManageDialog
  Docs routes-cataloguemanagedialog--docs
  Populated [routes-cataloguemanagedialog--populated]
  Empty [routes-cataloguemanagedialog--empty]
  Loading [routes-cataloguemanagedialog--loading]
  Failed [routes-cataloguemanagedialog--failed]
  Read Only [routes-cataloguemanagedialog--read-only]
  Create Refused [routes-cataloguemanagedialog--create-refused]

Features/Values/DefinitionsBundlePanel
  Docs routes-definitionsbundlepanel--docs
  Row [routes-definitionsbundlepanel--row]
  Dialog Open [routes-definitionsbundlepanel--dialog-open]
  Git Read Only [routes-definitionsbundlepanel--git-read-only]
  Checked [routes-definitionsbundlepanel--checked]
  Planned [routes-definitionsbundlepanel--planned]
  Busy [routes-definitionsbundlepanel--busy]
  Refused [routes-definitionsbundlepanel--refused]

Features/Values/FolderCleanupDialog
  Docs routes-foldercleanupdialog--docs
  Default [routes-foldercleanupdialog--default]
  Widening Refused [routes-foldercleanupdialog--widening-refused]
  Busy [routes-foldercleanupdialog--busy]

Features/Values/HistoryDrawer
  Docs routes-historydrawer--docs
  Populated [routes-historydrawer--populated]
  Key Filter [routes-historydrawer--key-filter]
  Pin Sheet Open [routes-historydrawer--pin-sheet-open]
  Restore Sheet Open [routes-historydrawer--restore-sheet-open]
  Empty [routes-historydrawer--empty]
  Loading [routes-historydrawer--loading]
  Failed [routes-historydrawer--failed]
  Phone [routes-historydrawer--phone]
  Phone Detail [routes-historydrawer--phone-detail]

Features/Values/ImportWizard
  Docs routes-importwizard--docs
  Pick [routes-importwizard--pick]
  Dotenv Source [routes-importwizard--dotenv-source]
  Invalid Lines [routes-importwizard--invalid-lines]
  Connector Source [routes-importwizard--connector-source]
  Cli Guidance [routes-importwizard--cli-guidance]
  Classify [routes-importwizard--classify]
  Review [routes-importwizard--review]
  Result [routes-importwizard--result]
  Import Refused [routes-importwizard--import-refused]
  Review Failed [routes-importwizard--review-failed]
  Git Managed [routes-importwizard--git-managed]

Features/Values/KeyDeclarationDetail
  Docs routes-keydeclarationdetail--docs
  Editable [routes-keydeclarationdetail--editable]
  Deprecated [routes-keydeclarationdetail--deprecated]
  Reclassify Confirm [routes-keydeclarationdetail--reclassify-confirm]
  Git Read Only [routes-keydeclarationdetail--git-read-only]
  Source Failed [routes-keydeclarationdetail--source-failed]
  Loading [routes-keydeclarationdetail--loading]
  Gone [routes-keydeclarationdetail--gone]
  Failed [routes-keydeclarationdetail--failed]

Features/Values/LastApplyProvenance
  Docs routes-lastapplyprovenance--docs
  Labelled [routes-lastapplyprovenance--labelled]
  Bare [routes-lastapplyprovenance--bare]

Features/Values/MatrixKeyCreate
  Docs routes-matrixkeycreate--docs
  Default [routes-matrixkeycreate--default]

Features/Values/MatrixPublishSheet
  Docs routes-matrixpublishsheet--docs
  Default [routes-matrixpublishsheet--default]
  Blocked [routes-matrixpublishsheet--blocked]
  Protected [routes-matrixpublishsheet--protected]

Features/Values/MatrixRowEditor
  Docs routes-matrixroweditor--docs
  Default [routes-matrixroweditor--default]
  Copy Disclosure [routes-matrixroweditor--copy-disclosure]
  Edits [routes-matrixroweditor--edits]
  Long Value [routes-matrixroweditor--long-value]
  Long Secret Value [routes-matrixroweditor--long-secret-value]

Features/Values/PinReleaseOutcome
  Docs routes-pinreleaseoutcome--docs
  Retained [routes-pinreleaseoutcome--retained]
  Collection Eligible [routes-pinreleaseoutcome--collection-eligible]
  Already Collected [routes-pinreleaseoutcome--already-collected]

Features/Values/RevisionDiff
  Docs routes-revisiondiff--docs
  Populated [routes-revisiondiff--populated]
  Revealed [routes-revisiondiff--revealed]
  Empty [routes-revisiondiff--empty]
  Loading [routes-revisiondiff--loading]
  Failed [routes-revisiondiff--failed]

Features/Values/ScanBlockDialog
  Docs routes-scanblockdialog--docs
  Overridable [routes-scanblockdialog--overridable]
  Hard Block [routes-scanblockdialog--hard-block]

Features/Values/ScanWarnDialog
  Docs routes-scanwarndialog--docs
  Default [routes-scanwarndialog--default]

Pages/AccountProfile
  Docs routes-accountprofile--docs
  Editable [routes-accountprofile--editable]
  Proof Required [routes-accountprofile--proof-required]
  Managed [routes-accountprofile--managed]
  Provider Sign In [routes-accountprofile--provider-sign-in]
  Loading [routes-accountprofile--loading]
  Failed [routes-accountprofile--failed]

Pages/AccountSecurity
  Docs routes-accountsecurity--docs
  Populated [routes-accountsecurity--populated]
  Empty [routes-accountsecurity--empty]
  Loading [routes-accountsecurity--loading]
  Failed [routes-accountsecurity--failed]
  Proof Dialog [routes-accountsecurity--proof-dialog]

Pages/Adapters
  Docs pages-adapters--docs
  Empty [pages-adapters--empty]
  Failed [pages-adapters--failed]
  Adding [pages-adapters--adding]

Pages/Audit
  Docs routes-audit--docs
  Populated [routes-audit--populated]
  Empty [routes-audit--empty]
  Loading [routes-audit--loading]
  Failed [routes-audit--failed]

Pages/Ceremony
  Docs routes-ceremony--docs
  Protected [routes-ceremony--protected]
  Code Offered [routes-ceremony--code-offered]
  OIDC [routes-ceremony--oidc]
  Refused [routes-ceremony--refused]

Pages/ChangeApprovals
  Docs routes-changeapprovals--docs
  Populated [routes-changeapprovals--populated]
  Empty [routes-changeapprovals--empty]
  Loading [routes-changeapprovals--loading]
  Failed [routes-changeapprovals--failed]

Pages/CLIReauth
  Docs routes-clireauth--docs
  Disclosure [routes-clireauth--disclosure]
  Disclosure OIDC [routes-clireauth--disclosure-oidc]
  Adapter [routes-clireauth--adapter]
  Self Config [routes-clireauth--self-config]
  Loading [routes-clireauth--loading]
  Failed [routes-clireauth--failed]
  Nothing To Authorize [routes-clireauth--nothing-to-authorize]

Pages/EnrolmentGate
  Docs routes-enrolmentgate--docs
  Password [routes-enrolmentgate--password]
  Password Refused [routes-enrolmentgate--password-refused]

Pages/EstablishCredential
  Docs routes-establishcredential--docs
  Initial [routes-establishcredential--initial]
  Mismatch [routes-establishcredential--mismatch]
  Refused [routes-establishcredential--refused]
  Done [routes-establishcredential--done]
  Recover [routes-establishcredential--recover]
  Recovered [routes-establishcredential--recovered]

Pages/InstanceAdmin
  Docs routes-instanceadmin--docs
  Populated [routes-instanceadmin--populated]
  Not Disclosed [routes-instanceadmin--not-disclosed]
  Loading [routes-instanceadmin--loading]
  Second Factor Required [routes-instanceadmin--second-factor-required]
  Failed [routes-instanceadmin--failed]
  Editing Credential Policy [routes-instanceadmin--editing-credential-policy]
  Rotate Dek Confirm [routes-instanceadmin--rotate-dek-confirm]

Pages/InstanceConfig
  Docs routes-instanceconfig--docs
  Active [routes-instanceconfig--active]
  Unmanaged [routes-instanceconfig--unmanaged]
  Pending [routes-instanceconfig--pending]
  Partial [routes-instanceconfig--partial]
  Recovery Required [routes-instanceconfig--recovery-required]
  Loading [routes-instanceconfig--loading]
  Not Disclosed [routes-instanceconfig--not-disclosed]
  Failed [routes-instanceconfig--failed]
  Test Mail Ceremony [routes-instanceconfig--test-mail-ceremony]

Pages/Login
  Docs routes-login--docs
  With Providers [routes-login--with-providers]
  Password Step [routes-login--password-step]
  Paused [routes-login--paused]
  Local Only [routes-login--local-only]

Pages/MachineAccess
  Docs routes-machineaccess--docs
  Populated [routes-machineaccess--populated]
  Expanded Row [routes-machineaccess--expanded-row]
  Federation Tab [routes-machineaccess--federation-tab]
  Providers Tab [routes-machineaccess--providers-tab]
  Kubernetes Tab [routes-machineaccess--kubernetes-tab]
  Kubernetes Tab Unsupported [routes-machineaccess--kubernetes-tab-unsupported]
  Leases Tab [routes-machineaccess--leases-tab]
  Empty [routes-machineaccess--empty]
  Loading [routes-machineaccess--loading]
  Refused [routes-machineaccess--refused]
  Failed [routes-machineaccess--failed]

Pages/Matrix
  Docs routes-matrix--docs
  Populated [routes-matrix--populated]
  Degraded [routes-matrix--degraded]
  Git Managed [routes-matrix--git-managed]
  Empty [routes-matrix--empty]
  No Environments [routes-matrix--no-environments]
  Loading [routes-matrix--loading]
  Forbidden [routes-matrix--forbidden]
  Failed [routes-matrix--failed]

Pages/Members
  Docs routes-members--docs
  Populated [routes-members--populated]
  Load Error [routes-members--load-error]
  With Access Rules [routes-members--with-access-rules]
  Project With Access Rules [routes-members--project-with-access-rules]

Pages/Not found
  Docs routes-placeholder--docs
  Default [routes-placeholder--default]

Pages/OIDCDone
  Docs routes-oidcdone--docs
  No Transaction [routes-oidcdone--no-transaction]
  Login Refused [routes-oidcdone--login-refused]
  Link Refused [routes-oidcdone--link-refused]
  Reauth Refused [routes-oidcdone--reauth-refused]

Pages/OrgSettings
  Docs routes-orgsettings--docs
  Populated [routes-orgsettings--populated]
  Empty [routes-orgsettings--empty]
  Loading [routes-orgsettings--loading]
  Failed [routes-orgsettings--failed]

Pages/Overview
  Docs pages-overview--docs
  Empty [pages-overview--empty]
  Populated [pages-overview--populated]
  Long Names [pages-overview--long-names]
  Create Project Journey [pages-overview--create-project-journey]

Pages/Projects
  Docs routes-projects--docs
  Empty [routes-projects--empty]
  Populated [routes-projects--populated]
  Load Error [routes-projects--load-error]

Pages/ProjectSettings
  Docs routes-projectsettings--docs
  Administrable [routes-projectsettings--administrable]
  Unlimited Retention Override [routes-projectsettings--unlimited-retention-override]
  Member With Environments [routes-projectsettings--member-with-environments]
  Load Error [routes-projectsettings--load-error]

Pages/Reconnect
  Docs routes-reconnect--docs
  Contacting [routes-reconnect--contacting]
  Ready [routes-reconnect--ready]
  Failed [routes-reconnect--failed]

Pages/Remotes
  Docs pages-remotes--docs
  Empty [pages-remotes--empty]
  Long Content [pages-remotes--long-content]
  Loading Directory [pages-remotes--loading-directory]
  Permission Required [pages-remotes--permission-required]

Pages/SAMLDone
  Docs pages-samldone--docs
  Missing Transaction [pages-samldone--missing-transaction]

Pages/ScimProvisioning
  Docs routes-scimprovisioning--docs
  Administering [routes-scimprovisioning--administering]
  Unselected [routes-scimprovisioning--unselected]
  Empty [routes-scimprovisioning--empty]
  Failed [routes-scimprovisioning--failed]

Pages/SignupVerify
  Docs pages-signupverify--docs
  Entry [pages-signupverify--entry]
  Fresh Organisation [pages-signupverify--fresh-organisation]
  Missing Link [pages-signupverify--missing-link]
  Validation [pages-signupverify--validation]
  Busy [pages-signupverify--busy]
  Refused [pages-signupverify--refused]

Pages/TemporaryAccess
  Docs routes-temporaryaccess--docs
  Populated [routes-temporaryaccess--populated]
  Requester [routes-temporaryaccess--requester]
  Loading [routes-temporaryaccess--loading]

Pages/Values
  Docs routes-values--docs
  Populated [routes-values--populated]
  Window Live [routes-values--window-live]
  Protected Locked [routes-values--protected-locked]
  Write Only [routes-values--write-only]
  Empty [routes-values--empty]
  Loading [routes-values--loading]
  Failed [routes-values--failed]

Pages/WorkspaceApprove
  Docs routes-workspaceapprove--docs
  Nothing To Authorize [routes-workspaceapprove--nothing-to-authorize]
  Loading [routes-workspaceapprove--loading]
  Establishment [routes-workspaceapprove--establishment]
  Step Up [routes-workspaceapprove--step-up]
  Failed [routes-workspaceapprove--failed]
  Sign In [routes-workspaceapprove--sign-in]

Pages/WorkspaceCallback
  Docs routes-workspacecallback--docs
  No Result [routes-workspacecallback--no-result]
  Could Not Close [routes-workspacecallback--could-not-close]

Pages/WorkspaceScope
  Docs routes-workspacescope--docs
  Loading [routes-workspacescope--loading]
  Unknown Remote [routes-workspacescope--unknown-remote]
  Reconnect Required [routes-workspacescope--reconnect-required]
  Checking [routes-workspacescope--checking]
  Failed [routes-workspacescope--failed]
  Connected [routes-workspacescope--connected]

Pages/WorkspaceStepUp
  Docs routes-workspacestepup--docs
  Disconnected [routes-workspacestepup--disconnected]
  Contacting [routes-workspacestepup--contacting]
  Ready [routes-workspacestepup--ready]
  Failed [routes-workspacestepup--failed]

Shared/ChromeIdentityControls
  Docs routes-chromeidentitycontrols--docs
  Org Identity [routes-chromeidentitycontrols--org-identity]
  Project Identity [routes-chromeidentitycontrols--project-identity]

Shared/CompactOrgRetention
  Docs routes-compactorgretention--docs
  Default [routes-compactorgretention--default]
  Busy [routes-compactorgretention--busy]
  Refused [routes-compactorgretention--refused]

Shared/FleetUpdateNotice
  Docs routes-fleetupdatenotice--docs
  Local Update [routes-fleetupdatenotice--local-update]
  Remote Update [routes-fleetupdatenotice--remote-update]
  Multiple Updates [routes-fleetupdatenotice--multiple-updates]
  No Updates [routes-fleetupdatenotice--no-updates]

Shared/Notifications
  Docs app-notifications--docs
  Failure [app-notifications--failure]
  Success [app-notifications--success]
  Info [app-notifications--info]
  Dismisses [app-notifications--dismisses]

Shared/OpsDiagnosticBanners
  Docs routes-opsdiagnosticbanners--docs
  Error Severity [routes-opsdiagnosticbanners--error-severity]
  Warn [routes-opsdiagnosticbanners--warn]
  Unknown [routes-opsdiagnosticbanners--unknown]
  Css Check [routes-opsdiagnosticbanners--css-check]

Shared/ProfileUpdateBadge
  Docs routes-profileupdatebadge--docs
  Default [routes-profileupdatebadge--default]
  Multiple Versions [routes-profileupdatebadge--multiple-versions]

Shared/RetentionBoundsFields
  Docs routes-retentionboundsfields--docs
  Days [routes-retentionboundsfields--days]
  Exact [routes-retentionboundsfields--exact]
  Absent [routes-retentionboundsfields--absent]

Shared/RuntimeMaintenanceBoundary
  Docs app-runtimemaintenanceboundary--docs
  Maintenance [app-runtimemaintenanceboundary--maintenance]
  Recovery Required [app-runtimemaintenanceboundary--recovery-required]
  Reconnecting [app-runtimemaintenanceboundary--reconnecting]

Shared/Sections
  Docs routes-sections--docs
  Default Panel [routes-sections--default-panel]
  Danger Panel [routes-sections--danger-panel]
  Question Panel [routes-sections--question-panel]
  Tight Panel [routes-sections--tight-panel]
  Jump [routes-sections--jump]
  Disclosure [routes-sections--disclosure]
  Copy Once [routes-sections--copy-once]
  Consequences [routes-sections--consequences]
  Typed Confirm [routes-sections--typed-confirm]

Shared/SidebarLinkItem
  Docs routes-sidebarlinkitem--docs
  Default [routes-sidebarlinkitem--default]
  Active [routes-sidebarlinkitem--active]
  Disabled [routes-sidebarlinkitem--disabled]
  Members Active [routes-sidebarlinkitem--members-active]

Shared/SidebarVersion
  Docs routes-sidebarversion--docs
  Default [routes-sidebarversion--default]
  Absent [routes-sidebarversion--absent]

Shared/StepUpBanner
  Docs routes-stepupbanner--docs
  Authenticator Code [routes-stepupbanner--authenticator-code]
  Passkey [routes-stepupbanner--passkey]

Shared/SystemProjectNotice
  Docs routes-systemprojectnotice--docs
  Shown [routes-systemprojectnotice--shown]

Shared/SystemScopeRefusal
  Docs routes-systemscoperefusal--docs
  Adapters [routes-systemscoperefusal--adapters]
  Machine Access [routes-systemscoperefusal--machine-access]
  Scim [routes-systemscoperefusal--scim]

Shared/ThemeToggle
  Docs routes-themetoggle--docs
  Default [routes-themetoggle--default]
  Toggles [routes-themetoggle--toggles]

Shared/UpdateJobStatus
  Docs routes-updatejobstatus--docs
  Queued [routes-updatejobstatus--queued]
  Running [routes-updatejobstatus--running]
  Succeeded [routes-updatejobstatus--succeeded]
  Failed [routes-updatejobstatus--failed]

Shared/WorkspaceSettingsLink
  Docs shared-workspacesettingslink--docs
  This Instance [shared-workspacesettingslink--this-instance]
  Destination Instance [shared-workspacesettingslink--destination-instance]

```

## Full source audit appendix

# Storybook source inventory and usefulness audit

Baseline source: 44f77a2eb8610b83b6405229f7c0759ec5f995cb. Baseline generated index: web/.artifacts/storybook-improvement/before/storybook-static/index.json. Generated Docs are matched by source import path, not title. The baseline build is preserved separately from browser-server files.

The Babel AST inventory enumerates every top-level JSX-rendering function/wrapper in application TypeScript sources, including private composites. Prop variants remain stories of one component. Native semantic elements are not invented component families. Direct coverage means a CSF primary component or explicitly rendered JSX; composition means a source render relationship and does not establish that every conditional branch executes. Source line numbers refer to the baseline revision.

| Metric | Baseline | Current local inventory |
| --- | --- | --- |
| Story modules | 115 | 131 |
| Story exports | 584 | 642 |
| Built stories | 584 | 642 |
| Built Docs | 115 | 131 |
| JSX-rendering symbols including infrastructure/private composites | 303 | 304 |
| Directly storied symbols | 119 | 136 |
| Composition-only symbols | 157 | 166 |
| Unmapped JSX symbols | 27 | 2 |
| Registered route surfaces | 30 | 30 |

## Current disposition and export accounting

74 published entries added; 16 baseline entries removed with explicit successors. 1 newly drafted behavior cases were moved to regression before publication and are accounted separately. 0 retained story IDs changed. Every original retained module keeps its generated Docs ID and source import path.

| Removed baseline ID | Retained visual successor | Regression owner | Evidence-based reason |
| --- | --- | --- | --- |
| ui-button--clicks | ui-button--secondary | web/src/ui/Button.test.tsx | Behavior-only activation now regression-tested while Secondary retains the same view. |
| ui-button--danger | ui-button--all-variants | visual consolidation | Captioned destructive action in gallery; Secondary exposes variant controls. |
| ui-button--quiet | ui-button--all-variants | visual consolidation | Captioned tertiary action in gallery; Secondary exposes variant controls. |
| ui-badge--danger | ui-badge--all-tones | visual consolidation | Captioned danger badge; Neutral exposes tone controls. |
| ui-badge--changed | ui-badge--all-tones | visual consolidation | Captioned changed badge; Neutral exposes tone controls. |
| ui-badge--ok | ui-badge--all-tones | visual consolidation | Captioned OK badge; Neutral exposes tone controls. |
| ui-badge--mono | ui-badge--all-tones | visual consolidation | Captioned monospace badge; Neutral exposes mono controls. |
| ui-input--label-is-wired | ui-input--default | web/src/ui/accessibility.test.tsx | Field label associations and distinct generated IDs regression-tested. |
| ui-select--label-is-wired | ui-select--default | web/src/ui/accessibility.test.tsx | Field label associations and distinct generated IDs regression-tested. |
| ui-textarea--label-is-wired | ui-textarea--default | web/src/ui/accessibility.test.tsx | Field label associations and distinct generated IDs regression-tested. |
| ui-checkbox--toggles | ui-checkbox--checked | web/src/ui/accessibility.test.tsx | Control activation regression-tested; checked view retained. |
| ui-checkbox--label-toggles | ui-checkbox--checked | web/src/ui/accessibility.test.tsx | Associated label activation regression-tested; checked view retained. |
| ui-radio--selects | ui-radio--checked | web/src/ui/accessibility.test.tsx | Selection and independence of separate Docs groups regression-tested. |
| ui-glyph--decorative-is-hidden | ui-glyph--decorative, ui-glyph--named | web/src/ui/accessibility.test.tsx | Decorative hiding and named image semantics regression-tested; both views retained. |
| ui-alert--roles-match-tone | ui-alert--all-states | web/src/ui/accessibility.test.tsx | Assertive/polite tone roles and decorative hiding regression-tested; all feedback states retained. |
| ui-dialog--all-states | ui-dialog--decision, ui-dialog--wide | visual consolidation | Invalid simultaneous-modal gallery removed; one modal made its sibling inert and unreachable. Both sizes remain independently reviewable. |

| Current module | Title | Meaningful current entries | Docs | Source SHA256 |
| --- | --- | --- | --- | --- |
| web/src/routes/Adapters.stories.tsx | Pages/Adapters | Empty, Failed, Adding | pages-adapters--docs | ddf52865a7d0845314565b41089e00883e8f87fa801fa2f19a43a07aa9a997f3 |
| web/src/routes/AwsAccessFields.stories.tsx | Features/Adapters/AwsAccessFields | AssumeRole, WebIdentity, Ambient, Static | features-adapters-awsaccessfields--docs | af98a68fac28810b903a3710a9dad49dea23297e3e832c17efc336cc30b050f4 |
| web/src/routes/CertificatesTab.stories.tsx | Features/PKI/CertificatesTab | Populated, Empty, Loading, Incomplete, IssueFromCsr, RevokeConfirmation | features-pki-certificatestab--docs | d8c170f3a5ab8ed294e053b3e13ec345df1071d676fcddad20c610b60623e960 |
| web/src/routes/InstanceMailPanel.stories.tsx | Features/Instance/InstanceMailPanel | Configured, Unconfigured, Loading, SecondFactorRequired, Failed, FreshProofRequired | features-instance-instancemailpanel--docs | 05d15220931288d70d24a9d466bc34e375b7c0a0d526af1cbb3fa75f2eb0eb30 |
| web/src/routes/OAuth2ProvidersPanel.stories.tsx | Features/Identity/OAuth2ProvidersPanel | Populated, Empty, Loading, SecondFactorRequired, Failed, Reconfiguring, DeleteConfirmation | features-identity-oauth2providerspanel--docs | fe8b8bb69eccc403b993bb3ea01d597604c9be6b53e2e300063c004fca407373 |
| web/src/routes/OpenRegistration.stories.tsx | Features/Members/OpenRegistration | Closed, Active, InactiveAuthority, Loading, Failed, EditorValidation | features-members-openregistration--docs | 8314145a6c107b8be2bd22374bfc3bd8990b3a8ef18819392b0022a0fbcdaf91 |
| web/src/routes/Overview.stories.tsx | Pages/Overview | Empty, Populated, LongNames, CreateProjectJourney | pages-overview--docs | 71a47205f5b9e2c118f9e581880bedeb17a3c444a31f0fe8666a720f3103034f |
| web/src/routes/PkiIssuersPanel.stories.tsx | Features/PKI/PkiIssuersPanel | Populated, Empty, Loading, SecondFactorRequired, Failed | features-pki-pkiissuerspanel--docs | a7500b453163efe6019398aa224433dc7f745a65cf0fcf9842ef5cb667a44267 |
| web/src/routes/PkiProfilesPanel.stories.tsx | Features/PKI/PkiProfilesPanel | Populated, Empty, Loading, SecondFactorRequired, Failed, Narrowing, InvalidPolicy | features-pki-pkiprofilespanel--docs | fe510eb4686be16db7fa7d69cab2532f60cc6e86ca79b6ddad8bffa70511b3c8 |
| web/src/routes/Remotes.stories.tsx | Pages/Remotes | Empty, LongContent, LoadingDirectory, PermissionRequired | pages-remotes--docs | ce5c7a13b54f3405d9b2c47d574e3d14f6774e5dbedf1dd96aee55246a93e90c |
| web/src/routes/SAMLDone.stories.tsx | Pages/SAMLDone | MissingTransaction | pages-samldone--docs | 624508e70c21e5403d24e141cca0264597aa190d2a2f16087ddb272144741c20 |
| web/src/routes/SSHCertificates.stories.tsx | Features/SSH/SSHCertificatesPanel | Empty, NoEnvironments, SessionRequired, Populated, Refused, CreateCa | features-ssh-sshcertificatespanel--docs | daf9059e2db933a211a3846900f6fa60cbf096228e00cda72a13f9b81a760c1d |
| web/src/routes/SignupVerify.stories.tsx | Pages/SignupVerify | Entry, FreshOrganisation, MissingLink, Validation, Busy, Refused | pages-signupverify--docs | c6ab34f5af11c9383a1d155cb90eff467ffc0c7b7e847d1e132f03f8ee56ab8e |
| web/src/routes/WorkspaceSettingsLink.stories.tsx | Shared/WorkspaceSettingsLink | ThisInstance, DestinationInstance | shared-workspacesettingslink--docs | 47b702212df83c6aeabc9ecf45b1bb2756c1c24aa0c60b50c717227bfb6aab0e |
| web/src/ui/CeremonyNotice.stories.tsx | Design system/CeremonyNotice | Default, LongContent | design-system-ceremonynotice--docs | 1569d5c92b570d54d4e550c295e68e0838dd26da834104cdcaadf50029396535 |
| web/src/ui/auth/LocalSignupForm.stories.tsx | Design system/Auth/LocalSignupForm | Entry, NoOrganisation, RequestAndResend, Busy, Refused | design-system-auth-localsignupform--docs | 5f07b92bdd2ebe1a1297ef1c0390575f1e75a6b3f8f2bf669b9b39030133072a |
| web/src/app/RuntimeMaintenanceBoundary.stories.tsx | Shared/RuntimeMaintenanceBoundary | Maintenance, RecoveryRequired, Reconnecting | app-runtimemaintenanceboundary--docs | 5990b880436441c51540ea90b634257e3c5a1fc12f5b50e12dc20ea135e43826 |
| web/src/app/notifications.stories.tsx | Shared/Notifications | Failure, Success, Info, Dismisses | app-notifications--docs | 32acd00c87fceaaad241c137e71749b8ec6db34f1f7e5b7ad6ecc73f3cae41cc |
| web/src/routes/AccountProfile.stories.tsx | Pages/AccountProfile | Editable, ProofRequired, Managed, ProviderSignIn, Loading, Failed | routes-accountprofile--docs | f7034ffbdfc9842535788f36fc84a2418518fd5a369ddfb33995c60470e79616 |
| web/src/routes/AccountSecurity.stories.tsx | Pages/AccountSecurity | Populated, Empty, Loading, Failed, ProofDialog | routes-accountsecurity--docs | f403ee57b06842dbb7e4fbbabcc7a024fe2b3335bfa5299dc660cd18386ad9e6 |
| web/src/routes/Audit.stories.tsx | Pages/Audit | Populated, Empty, Loading, Failed | routes-audit--docs | c8feb63359c49501e42d1a61e7f53f871a13a8a92306b844dbc993d9fd6a8c84 |
| web/src/routes/BindingCard.stories.tsx | Features/Machine access/BindingCard | Default, ExpiringSoon, Quarantined, NotReady | routes-bindingcard--docs | aae52c8a749c1505d37735e6721398535e90d57449d98d3ebeb64e19a2e13d07 |
| web/src/routes/BindingDialog.stories.tsx | Features/Machine access/BindingDialog | Default, GitHubActions, PullRequestEvent, AudienceRequired, Replace, Busy, Failed | routes-bindingdialog--docs | 1a95ccb10d735e6ca5ddf2e200eb4b5e5fc1e10d18b2d20b216fa0480915274b |
| web/src/routes/CLIReauth.stories.tsx | Pages/CLIReauth | Disclosure, DisclosureOIDC, Adapter, SelfConfig, Loading, Failed, NothingToAuthorize | routes-clireauth--docs | d39b71ea45b0e3b97344f78f5d161d0a65e691e3c2bd95954fd5e98c9110bc26 |
| web/src/routes/CatalogueManageDialog.stories.tsx | Features/Values/CatalogueManageDialog | Populated, Empty, Loading, Failed, ReadOnly, CreateRefused | routes-cataloguemanagedialog--docs | 25fc1ccf33a102f0739938ae2300880feaba8b02c347cb6b0dcb30c1b4b3ebc4 |
| web/src/routes/Ceremony.stories.tsx | Pages/Ceremony | Protected, CodeOffered, OIDC, Refused | routes-ceremony--docs | 11130b1f1fb4f43693ca90cfbefe76cf76432e3d8fd297bcd2c777d0ab59b8a7 |
| web/src/routes/ChangeApprovals.stories.tsx | Pages/ChangeApprovals | Populated, Empty, Loading, Failed | routes-changeapprovals--docs | c010ef3f6adde1e3f1b8b63dda31e90a6f58ef89e7eb3c860b13c1659ca7b6f5 |
| web/src/routes/ChromeIdentityControls.stories.tsx | Shared/ChromeIdentityControls | OrgIdentity, ProjectIdentity | routes-chromeidentitycontrols--docs | 68cc950cd270c59193504a2efe5f59f4fb1e1ffb0223d0dca1ee2ea1227316d7 |
| web/src/routes/CompactOrgRetention.stories.tsx | Shared/CompactOrgRetention | Default, Busy, Refused | routes-compactorgretention--docs | 50893e59de1f9a4ecbc734be2e127a44e79f7470855a5244c0cf88ba4dd05c93 |
| web/src/routes/ConnectionMintDialog.stories.tsx | Features/Remotes/ConnectionMintDialog | Default, Clamped | routes-connectionmintdialog--docs | 3b0bfe63b15437aab09c9cab0c88b5b9b667a4bf35432f6c7b4abe654c7d0a9b |
| web/src/routes/ConnectionRow.stories.tsx | Features/Remotes/ConnectionRow | Live, Expired, Revoked, Indefinite | routes-connectionrow--docs | bd66d96c26c59eb76e2c34d02746ccd9eb89eb0794705ba0313ce5b1ae80a660 |
| web/src/routes/CreateAccountDialog.stories.tsx | Features/Machine access/CreateAccountDialog | Default, NameRequired, Busy, Failed | routes-createaccountdialog--docs | 412f9f635cc68e42e13160830cb15d04a67402e0463d2fa08e7f40532af844ea |
| web/src/routes/CreateProviderDialog.stories.tsx | Features/Machine access/CreateProviderDialog | Default, FieldsRequired, Busy, Failed | routes-createproviderdialog--docs | 9b4c2f851f0268014d369f6574bee004c1278da09ce0ade95f06b2c467f07c2d |
| web/src/routes/CredentialForm.stories.tsx | Features/Remotes/CredentialForm | Default, Entered, Busy | routes-credentialform--docs | 9b43ada9f7d252d1ca8354878cb940780f1c9c9c68b11a9cb96b0ab3de81588d |
| web/src/routes/DefinitionsBundlePanel.stories.tsx | Features/Values/DefinitionsBundlePanel | Row, DialogOpen, GitReadOnly, Checked, Planned, Busy, Refused | routes-definitionsbundlepanel--docs | adf3fc1cf66e9d488213b9c2e176353fcc0f90dfe32f10a7518ee25767991c02 |
| web/src/routes/DeleteAccountDialog.stories.tsx | Features/Machine access/DeleteAccountDialog | Default, OneCredential, Busy, Failed, FailedMayHaveCommitted | routes-deleteaccountdialog--docs | f97b802f53bf242a1cdc3be5c464cbd0f4cedd8e366f5282a217dfcfc7c8a822 |
| web/src/routes/DeleteAdapterDialog.stories.tsx | Features/Adapters/DeleteAdapterDialog | Default, Prune, Busy | routes-deleteadapterdialog--docs | 375fc88509a3a5bc61e74851bdda084340ce5e4b3dd0493939ce707f3bea704f |
| web/src/routes/DeleteProviderDialog.stories.tsx | Features/Machine access/DeleteProviderDialog | NoLeases, LiveLeases, LeasesUnknown, Busy, Conflict, Failed | routes-deleteproviderdialog--docs | fca5fa5e9a726d1dee9b6b613c86fc682ec1571b16b41522f92fe5d014ec9691 |
| web/src/routes/DeliveryTargets.stories.tsx | Features/Machine access/DeliveryTargets | ReportedHealthy, ReportedDegraded, Stale, Refused, ReporterRevoked, QuotaRefused, Unknown, Empty, Unsupported | routes-deliverytargets--docs | 3427d37e2d54ce69b7f7c88b3dcc47946d041ad6b2211eec7f4f54aff94f904d |
| web/src/routes/EnrolmentGate.stories.tsx | Pages/EnrolmentGate | Password, PasswordRefused | routes-enrolmentgate--docs | 7e5b277c6f2098f895096477a3e47e5a1ad898cd62a83a4754eb01d655df8f83 |
| web/src/routes/EstablishCredential.stories.tsx | Pages/EstablishCredential | Initial, Mismatch, Refused, Done, Recover, Recovered | routes-establishcredential--docs | a95f73d477c76e00d077f1528446d12cf3f4a84759f363b6484b30d336a53149 |
| web/src/routes/FederationIssuersPanel.stories.tsx | Features/Identity/FederationIssuersPanel | Populated, Empty, Loading, SecondFactorRequired, Failed, Creating, Editing, DeleteRefused | routes-federationissuerspanel--docs | f440977c39a1f664171b5c35367e9b7427a1af02408d70cdc4eb0845711a8b38 |
| web/src/routes/FleetUpdateNotice.stories.tsx | Shared/FleetUpdateNotice | LocalUpdate, RemoteUpdate, MultipleUpdates, NoUpdates | routes-fleetupdatenotice--docs | 8d212740c78a8b6db6560b03739408947398d457a6f32cac5476d1f412b9a672 |
| web/src/routes/FolderCleanupDialog.stories.tsx | Features/Values/FolderCleanupDialog | Default, WideningRefused, Busy | routes-foldercleanupdialog--docs | bae977f9b5c0d15b6ffdd87c91ad1d1005919352b2b29c744426107946f09af2 |
| web/src/routes/GrantDialog.stories.tsx | Features/Machine access/GrantDialog | ReadGrant, RevealGrant, NothingToWiden, CatalogueFailed, CatalogueLoading, ReportingGrantable, ReportingNotGrantable, ReportAfterTheFact, ReportingUnsupported | routes-grantdialog--docs | 5178b26d29c147eabf8bc38b9d99563dd7ba1237b759354432357b7f99e7faa5 |
| web/src/routes/HealthChip.stories.tsx | Features/Adapters/HealthChip | Never, Pending, Converging, Converged, Degraded, Failed, Paused, DriftAttention | routes-healthchip--docs | 11a30ea8ad499dd3bbd9b22427e83844025028a511dea659e383eff45423e39f |
| web/src/routes/HistoryDrawer.stories.tsx | Features/Values/HistoryDrawer | Populated, KeyFilter, PinSheetOpen, RestoreSheetOpen, Empty, Loading, Failed, Phone, PhoneDetail | routes-historydrawer--docs | 968e42dc8273b2c57f1568a5360523cc52e9c65aa0550c6614d0aa0b88908c52 |
| web/src/routes/ImportWizard.stories.tsx | Features/Values/ImportWizard | Pick, DotenvSource, InvalidLines, ConnectorSource, CliGuidance, Classify, Review, Result, ImportRefused, ReviewFailed, GitManaged | routes-importwizard--docs | 333cc9f6059f1280f9278676de70a01e9bb8b234ddd2276a5da6f4f73e79476c |
| web/src/routes/InstanceAdmin.stories.tsx | Pages/InstanceAdmin | Populated, NotDisclosed, Loading, SecondFactorRequired, Failed, EditingCredentialPolicy, RotateDekConfirm | routes-instanceadmin--docs | c972de0564bcc01a04a87ba0c48a7c75ed262ba367e779fc31b869a54385108b |
| web/src/routes/InstanceConfig.stories.tsx | Pages/InstanceConfig | Active, Unmanaged, Pending, Partial, RecoveryRequired, Loading, NotDisclosed, Failed, TestMailCeremony | routes-instanceconfig--docs | 2f6d6115f060b53e80574cd5fe2c6818b943fa83c4af1c7f365e0ada787cee25 |
| web/src/routes/InviteDialog.stories.tsx | Features/Members/InviteDialog | OrgScope, InstanceScope | routes-invitedialog--docs | fef71eb806aaaa2449a11fdb4599a27ad92386f6ddce52d8c01f48f040026e11 |
| web/src/routes/KeyDeclarationDetail.stories.tsx | Features/Values/KeyDeclarationDetail | Editable, Deprecated, ReclassifyConfirm, GitReadOnly, SourceFailed, Loading, Gone, Failed | routes-keydeclarationdetail--docs | 1f535a21a0952af56b0306765cf08e5b57c2f41e9bfce1ee8009179f07d2cf6f |
| web/src/routes/LastApplyProvenance.stories.tsx | Features/Values/LastApplyProvenance | Labelled, Bare | routes-lastapplyprovenance--docs | a0f893c37cf403f1469b9eae506d232623422b0e0c656d82e9f6669bb730ad43 |
| web/src/routes/LeaseActionDialog.stories.tsx | Features/Machine access/LeaseActionDialog | Renew, RenewCeilingRefused, Revoke, Settle, Busy, Failed | routes-leaseactiondialog--docs | f2477e275c9757138cc4af324b2225bdc45b41e6c93729250706cf81ce79e5ea |
| web/src/routes/LeaseMintDialog.stories.tsx | Features/Machine access/LeaseMintDialog | Idle, CeilingRefused, NoSession, Submitting, Failed, Disclosed, HeldBack | routes-leasemintdialog--docs | 266c610e05bd1a425e5271aadfe61cc78b6d3f7703510c5ea8cb341b38d7730f |
| web/src/routes/Login.stories.tsx | Pages/Login | WithProviders, PasswordStep, Paused, LocalOnly | routes-login--docs | f94cf59a20ec259221df4c22710618fc09c5e791f07e6876c4a19c032a024679 |
| web/src/routes/MachineAccess.stories.tsx | Pages/MachineAccess | Populated, ExpandedRow, FederationTab, ProvidersTab, KubernetesTab, KubernetesTabUnsupported, LeasesTab, Empty, Loading, Refused, Failed | routes-machineaccess--docs | 21e98526991f34b24f68411cf194ae82b79dbd594d6172ddc55a8ecd203ebc7d |
| web/src/routes/MachineRevealDialog.stories.tsx | Features/Machine access/MachineRevealDialog | Enable, Withdraw, Busy, Failed | routes-machinerevealdialog--docs | d7b633a821fa8636c8d608a1f189bccc88bb4ba163ba8a13531d2f65c6d80ac9 |
| web/src/routes/MachineRevokeCredentialDialog.stories.tsx | Features/Machine access/MachineRevokeCredentialDialog | Default, Busy, Failed | routes-machinerevokecredentialdialog--docs | 701c60a5aa0f9c83dc5fc6c0789fdb4d6d8e0114f92233f560258e90e8405a51 |
| web/src/routes/Matrix.stories.tsx | Pages/Matrix | Populated, Degraded, GitManaged, Empty, NoEnvironments, Loading, Forbidden, Failed | routes-matrix--docs | 021064663fef1cae8f5eadde73f3b0443661496156ca238735d3ea138b9f2599 |
| web/src/routes/MatrixKeyCreate.stories.tsx | Features/Values/MatrixKeyCreate | Default | routes-matrixkeycreate--docs | d9b7cd6b7e06b5aff4cb37c11b02406feb70a094bb8c784de3be0b98abed8f60 |
| web/src/routes/MatrixPublishSheet.stories.tsx | Features/Values/MatrixPublishSheet | Default, Blocked, Protected | routes-matrixpublishsheet--docs | 09cb48ecaee080bac991954139e1092b5562e3d148ec1b0c5bd33f2978a14448 |
| web/src/routes/MatrixRowEditor.stories.tsx | Features/Values/MatrixRowEditor | Default, CopyDisclosure, Edits, LongValue, LongSecretValue | routes-matrixroweditor--docs | 663225b5753562c69996f7f7d84e805f415b62d73d694fac600170ba830b80b2 |
| web/src/routes/Members.stories.tsx | Pages/Members | Populated, LoadError, WithAccessRules, ProjectWithAccessRules | routes-members--docs | d96f4b5caeb05aa2c72d3548e75b026f6e2498c03978d1d0f3a9313b01a08337 |
| web/src/routes/MintConnectionForm.stories.tsx | Features/Remotes/MintConnectionForm | Default, WithError | routes-mintconnectionform--docs | 6753a128efea93b2a09c961f545fdce80adbb4fc9a546223d31dff8bc728c428 |
| web/src/routes/MintDialog.stories.tsx | Features/Machine access/MintDialog | Reviewing, ReviewingNoReach, Rotating, Submitting, Failed, Disclosed, HeldBack | routes-mintdialog--docs | 38dccc5a94e3cdbcd1ae8e7281d90abe0db048d9e61d81ede37d9bfb5adc7e91 |
| web/src/routes/OIDCDone.stories.tsx | Pages/OIDCDone | NoTransaction, LoginRefused, LinkRefused, ReauthRefused | routes-oidcdone--docs | 5d83909c5231b7bdcd61d033934438cb66b717cbee4e5f9b7defde25e93a2e21 |
| web/src/routes/OidcProvidersPanel.stories.tsx | Features/Identity/OidcProvidersPanel | Populated, Empty, Loading, SecondFactorRequired, Failed, Reconfiguring, DeleteConfirm | routes-oidcproviderspanel--docs | 7b7446e8df644d6d9fa83d952a033a3b05aa3372746831ebd247685770a425de |
| web/src/routes/OpsDiagnosticBanners.stories.tsx | Shared/OpsDiagnosticBanners | ErrorSeverity, Warn, Unknown, CssCheck | routes-opsdiagnosticbanners--docs | 3731c2c52caa1d85201b1c6f68cdb2bd94b086bd7e2281658830b2292e49da9c |
| web/src/routes/OrgSettings.stories.tsx | Pages/OrgSettings | Populated, Empty, Loading, Failed | routes-orgsettings--docs | 77de0fc40018a8a24e43ff48fab052c9163c7a049cd8af4be34ce3a7888b8ad6 |
| web/src/routes/PinReleaseOutcome.stories.tsx | Features/Values/PinReleaseOutcome | Retained, CollectionEligible, AlreadyCollected | routes-pinreleaseoutcome--docs | 55185b3402c9f6360a4683302f28c4b05d84904f7cf95ea1893874c269437c10 |
| web/src/routes/Placeholder.stories.tsx | Pages/Not found | Default | routes-placeholder--docs | 34fdc15bd7eec40c2f167a624ae7a344f7fb48b8666b8db98836214d6b3ed0c8 |
| web/src/routes/ProfileUpdateBadge.stories.tsx | Shared/ProfileUpdateBadge | Default, MultipleVersions | routes-profileupdatebadge--docs | 8a9c2d7ce291bad5db1acfdaa1c01aebfadd04ab5b59f07b93c37cb9d22c5620 |
| web/src/routes/ProjectSettings.stories.tsx | Pages/ProjectSettings | Administrable, UnlimitedRetentionOverride, MemberWithEnvironments, LoadError | routes-projectsettings--docs | 8633407740b1940528b0dd273ebd605cb774f6c0bf46e2d37931b567a99b116d |
| web/src/routes/Projects.stories.tsx | Pages/Projects | Empty, Populated, LoadError | routes-projects--docs | 0e926c5f2dba31f3ee0535a0fe4aa4d63877f7d9bb70bd387e4f522d941075f7 |
| web/src/routes/ProviderDiscoveryAlert.stories.tsx | Features/Identity/ProviderDiscoveryAlert | Default | routes-providerdiscoveryalert--docs | e6429000b94059dad524c629cbf2f82b43d8cd4190d100d7f4be187072210f69 |
| web/src/routes/Reconnect.stories.tsx | Pages/Reconnect | Contacting, Ready, Failed | routes-reconnect--docs | 16eeca6e9662447eb0412d9971bb57450c2aa37f55fbe8276efa1eff3227c98d |
| web/src/routes/RemoteCard.stories.tsx | Features/Remotes/RemoteCard | Healthy, Unreachable, CredentialRejected, Duplicate | routes-remotecard--docs | b5a7397db261d8cd07b85d3da877c65142cf9803b2c129dde89e27ec5e76d072 |
| web/src/routes/RetentionBoundsFields.stories.tsx | Shared/RetentionBoundsFields | Days, Exact, Absent | routes-retentionboundsfields--docs | 4292385725ba4138365a17f7b98a4909132d90a8e3fe4a8267a8e9185310fff0 |
| web/src/routes/RevisionDiff.stories.tsx | Features/Values/RevisionDiff | Populated, Revealed, Empty, Loading, Failed | routes-revisiondiff--docs | df72e83c59aab45b9f6a3cdf2b71bc6b60edc2d0ae105d0ecd6ff89c8756f79f |
| web/src/routes/RevokeConnectionDialog.stories.tsx | Features/Remotes/RevokeConnectionDialog | Default | routes-revokeconnectiondialog--docs | 0450fc8a9a765cf681da6060cbe60dfa1bc016aa1a141017b7696d583672d993 |
| web/src/routes/RevokeCredentialDialog.stories.tsx | Features/Remotes/RevokeCredentialDialog | Default, Confirm, Busy | routes-revokecredentialdialog--docs | 440cb2b00fc30482a88de448835f930358a83e4df42714e45a0fb1f76cb38295 |
| web/src/routes/SamlProvidersPanel.stories.tsx | Features/Identity/SamlProvidersPanel | Populated, Empty, Loading, SecondFactorRequired, Failed, Creating, MetadataDiff | routes-samlproviderspanel--docs | b979b0877b4f26d6bd273255d69852686088ed5807365d9f5444eb4ee1a54198 |
| web/src/routes/SamlSpKeysPanel.stories.tsx | Features/Identity/SamlSpKeysPanel | Populated, Loading, SecondFactorRequired, Failed, RetireConfirm, CompromiseRetireConfirm | routes-samlspkeyspanel--docs | 3f103f603d2be78e83980ec86fdda53955c447e3c0043ff76a587ebaa45de53f |
| web/src/routes/ScanBlockDialog.stories.tsx | Features/Values/ScanBlockDialog | Overridable, HardBlock | routes-scanblockdialog--docs | 6502ff00aa8b45be353cc4fb438b16ecace25f6346760adac11317f13de80a99 |
| web/src/routes/ScanWarnDialog.stories.tsx | Features/Values/ScanWarnDialog | Default | routes-scanwarndialog--docs | 84241e6776f7f4554b8cdeab23ede524602d3eb953662c47629dec2e13bbecd7 |
| web/src/routes/ScimProvisioning.stories.tsx | Pages/ScimProvisioning | Administering, Unselected, Empty, Failed | routes-scimprovisioning--docs | 846eec54fc8257f3d75b4514f4d990b97781bad2428d783754a9365112b8baef |
| web/src/routes/Sections.stories.tsx | Shared/Sections | DefaultPanel, DangerPanel, QuestionPanel, TightPanel, Jump, Disclosure, CopyOnce, Consequences, TypedConfirm | routes-sections--docs | 49158ba7f229080e13b008f77700f2c59ce3575f815db78572afc24229688ab8 |
| web/src/routes/SetCredentialDialog.stories.tsx | Features/Machine access/SetCredentialDialog | Replace, FirstCredential, CredentialRequired, Busy, Failed | routes-setcredentialdialog--docs | e7468b8a7e5e92a0330b1530ec56fd647ef4b2bda31f831fd895db95250e246b |
| web/src/routes/SidebarLinkItem.stories.tsx | Shared/SidebarLinkItem | Default, Active, Disabled, MembersActive | routes-sidebarlinkitem--docs | c61bd62fe08884544ce2267f023beb54efbce7b989afb20a11fbfb547b0c458e |
| web/src/routes/SidebarVersion.stories.tsx | Shared/SidebarVersion | Default, Absent | routes-sidebarversion--docs | 5ea0f8e9df6b6c3a8280ca52d27574d533aedff760821e74520bc1ae2dcab22f |
| web/src/routes/StepUpBanner.stories.tsx | Shared/StepUpBanner | AuthenticatorCode, Passkey | routes-stepupbanner--docs | f83886599469db5b83de37f3c31c6059de322cbfd4c02c488a42416d4d9f4cfc |
| web/src/routes/SystemProjectNotice.stories.tsx | Shared/SystemProjectNotice | Shown | routes-systemprojectnotice--docs | 46dd695f9ede4fe7d3054840071f8f24f01ec55f0b34ebf89faa08edac2fa218 |
| web/src/routes/SystemScopeRefusal.stories.tsx | Shared/SystemScopeRefusal | Adapters, MachineAccess, Scim | routes-systemscoperefusal--docs | fb39d01581b58ff725981aaffa482b32e0493e2e305b7f65f1b948cc07de34a3 |
| web/src/routes/TargetForm.stories.tsx | Features/Adapters/TargetForm | Default, LockedRouting, OrganizationSelected, EmptyKeys, Busy | routes-targetform--docs | f4013b8ed10fdbae223aab42342d0ee4bc43555bc9a6f37b7aa947235bc0f704 |
| web/src/routes/TemporaryAccess.stories.tsx | Pages/TemporaryAccess | Populated, Requester, Loading | routes-temporaryaccess--docs | e92d89ac4b90126fc3589ff108ddef5ac2a3a98fcefc9aecc2c7564475dd9637 |
| web/src/routes/ThemeToggle.stories.tsx | Shared/ThemeToggle | Default, Toggles | routes-themetoggle--docs | 51c60a6f057e8baae47fd005d17907eafa2796c018750d466099d16df7800494 |
| web/src/routes/UpdateJobStatus.stories.tsx | Shared/UpdateJobStatus | Queued, Running, Succeeded, Failed | routes-updatejobstatus--docs | 390aac5c56110b1cf93787b05c1d8faeaf07371eca5d1a76306147560e41e0bb |
| web/src/routes/Values.stories.tsx | Pages/Values | Populated, WindowLive, ProtectedLocked, WriteOnly, Empty, Loading, Failed | routes-values--docs | bea3d2ad3b9171564592f267d28abd0a999faf842742126181879f862128b9a8 |
| web/src/routes/WorkspaceApprove.stories.tsx | Pages/WorkspaceApprove | NothingToAuthorize, Loading, Establishment, StepUp, Failed, SignIn | routes-workspaceapprove--docs | a36b7152f805365880a0bc91b633e5ed3df822ba1fa259d370b5381625dacbc1 |
| web/src/routes/WorkspaceCallback.stories.tsx | Pages/WorkspaceCallback | NoResult, CouldNotClose | routes-workspacecallback--docs | db99089f88ee2800c5421866819f3acb382476ece5dcac8aca82528ca4a0c853 |
| web/src/routes/WorkspaceScope.stories.tsx | Pages/WorkspaceScope | Loading, UnknownRemote, ReconnectRequired, Checking, Failed, Connected | routes-workspacescope--docs | 4eae5d3897de160e46f8b2ca93e0f88374afa7091db8cacd859da468da812fd6 |
| web/src/routes/WorkspaceStepUp.stories.tsx | Pages/WorkspaceStepUp | Disconnected, Contacting, Ready, Failed | routes-workspacestepup--docs | 84690f09b6526541af2e41b5b9bec2e1ac7444d1b13f45956005403f55a316c2 |
| web/src/routes/accessRules/AccessRules.stories.tsx | Features/Members/Access rules | Rules, RulesOnAProject, EditorFolderRule, EditorNewRule, EditorSingleKeyRule, EditorWithoutKeyNames, WhoCan, WhoCanWithoutKeyNames, KeyMoveConfirmation, KeyMoveCountOnly, Glossary | members-access-rules--docs | 423ab5a1741d10f56f8ba8d1b9e0493c0f96efeaff2a48ca5343f23c9a341c4c |
| web/src/ui/Alert.stories.tsx | Design system/Alert | Danger, Done, Warn, Info, WithAction, AllStates | ui-alert--docs | c510ddfd878dbd501a3002e5fb5d46e7784d7eac4c3a085a4fdf5beed9b446f8 |
| web/src/ui/Badge.stories.tsx | Design system/Badge | Neutral, AllTones | ui-badge--docs | 5ed77cdce92a7ab25f99929a4377f98ac2b2585ff2a966ce524d1358c09bea1d |
| web/src/ui/Button.stories.tsx | Design system/Button | Secondary, Primary, Disabled, Icon, AllVariants | ui-button--docs | 8834b51b376a656964f92b6d43dcfa7bc3c60a62523a3c49ad9db17eb1858445 |
| web/src/ui/Checkbox.stories.tsx | Design system/Checkbox | Default, Checked, Disabled, HitBoxIsTheInput, AllStates | ui-checkbox--docs | 2d12db0854362b075534873898794f2466c68964c7de0ca06ab4b0350c0326aa |
| web/src/ui/ChoiceGroup.stories.tsx | Design system/ChoiceGroup | Stack, Wrap, Nowrap, TwoColumns, ThreeColumns, Chips, WithHint, Radios, AllStates, LegendNamesTheGroup, ColumnsAreAGrid, NowrapScrolls, ChipsMarkChecked | ui-choicegroup--docs | 60c7e9536d9b8a5860d658309a7527b41690b80e4b8bbdfaf8c770a28049656f |
| web/src/ui/Dialog.stories.tsx | Design system/Dialog | Decision, WithRefusal, Wide, MustAcknowledge, PinnedActions, BackdropClick, IsModalAndLabelled, MonoTitle | ui-dialog--docs | a21f6b1294367b5817ec894b14ca62a8c878ac8146c8b91c61109a2c733749eb |
| web/src/ui/Disclosure.stories.tsx | Design system/Disclosure | Closed, Open, Toggles | ui-disclosure--docs | ebcd6b1dca375eba9306a9488885cc363fb2a5be932cb82cae9ceecdff634ad5 |
| web/src/ui/Field.stories.tsx | Design system/Field | Default, WithHint, WithError | ui-field--docs | d44730b516c3062f6d0376020abcbf3d94c8a06d9a537dd769ee7a9707520338 |
| web/src/ui/Glyph.stories.tsx | Design system/Glyph | Decorative, Named, AllStates | ui-glyph--docs | f1330e845cb1414738734cce27406fc6813286aa889d178c0801e9919d1e2e51 |
| web/src/ui/Input.stories.tsx | Design system/Input | Default, Password, PasswordRevealable, Disabled, Mono, MonoTargetsTheControl, WithHint, WithError, ErrorIsWired, AllStates, ExternalDescriptionIsMerged, ExternalInvalidIsKept | ui-input--docs | 9b0e5b44490db6e046f67a0d103cbc18bcfbbfadb13d5f4ddb2bfed94556b0a1 |
| web/src/ui/Menu.stories.tsx | Design system/Menu | Default, Opens, Selects, Keyboard, TabLeaves, SelectsByKeyboard, DisabledSkipped | ui-menu--docs | 65a59ab4dbdfb5d9faebdbf142162b990bd677e478d09c5f1c3dc82bca6f68b5 |
| web/src/ui/Radio.stories.tsx | Design system/Radio | Default, Checked, Disabled, AllStates | ui-radio--docs | 68b220c62b15edd8a7fd212fa6105eda5b6cab6ee2ca03ea4db4702a8fec34ad |
| web/src/ui/Select.stories.tsx | Design system/Select | Default, Disabled, AllStates, ExternalDescriptionIsMerged, ExternalInvalidIsKept | ui-select--docs | e304c5c7ce781cdc1044676078fb545b0dfa4c0d4eb7b6251df911f815188b1b |
| web/src/ui/Tabs.stories.tsx | Design system/Tabs | Default, SecondSelected, KeyboardMovesAndSelects, TwoDemosStayIndependent | ui-tabs--docs | cd3b0f63d4b91c3d4aaff2746a74619efb90377dd3d0a984ae1cb36927d2713c |
| web/src/ui/Textarea.stories.tsx | Design system/Textarea | Default, Filled, Disabled, AllStates, ExternalDescriptionIsMerged, ExternalInvalidIsKept | ui-textarea--docs | 17b66a4d7da9440cb160c58d9fc55569169544c1036f0b1f26999aed417bbc89 |
| web/src/ui/ThemeIcon.stories.tsx | Design system/ThemeIcon | Sun, Moon | ui-themeicon--docs | 6be6078b88ec54be1eef1e3efacadb2f4a11e1fb485d306a83b481d7f2820780 |
| web/src/ui/ToggleChip.stories.tsx | Design system/ToggleChip | NotIncluded, Included, Implied, LeftOut, Disabled, ExcludeToggles | ui-togglechip--docs | 0c5668244327053e06dde638eb6975061de8c0cfe7710afbd6322c5ac874062e |
| web/src/ui/Tokens.stories.tsx | Design system/Tokens | DesignSystem, EveryTokenResolves | ui-tokens--docs | aab6690d6aecd4d96098dc7391b10e7fcfd33882f16b2e159a9b7e62106a7456 |
| web/src/ui/Typography.stories.tsx | Design system/Typography | Scale | ui-typography--docs | 81c5f250ed884439102fab3aa6cdf1e502ab5568e0402769b19efcf6d6f0761d |
| web/src/ui/auth/AuthenticatorCodeField.stories.tsx | Design system/Auth/AuthenticatorCodeField | Default, Checking, SubmitsTrimmedAndClears | ui-auth-authenticatorcodefield--docs | 3292514cd42b820f490491cc8905614599912f743fd70a1922eab9f171f96099 |
| web/src/ui/auth/LoginFlow.stories.tsx | Design system/Auth/LoginFlow | PasswordThenAuthenticator, AuthenticatorCodeRefused, UnenrolledIsGatedIntoSetup, UnenrolledAllowedByPolicy, PasskeyPasswordless, IdentityProvider, SignUpThroughProvider | ui-auth-loginflow--docs | 7dfeba07ca6e4da188bebec03125db6f314c2012e6b380ead6edf1a535414043 |
| web/src/ui/auth/LoginForm.stories.tsx | Design system/Auth/LoginForm | WithProviders, SocialProviders, LastUsedProvider, LastUsedPassword, LastUsedPasskey, LocalOnly, PasswordAndPasskey, PasswordStep, WaitingForPasskey, ContactingProvider, Refused, Paused, DoorOpen, SignUpDoor, OpensOnSignUp, OpensOnSignUpWhileClosed, SignUpConfirmation, SignUpBackLinks, ProviderStartsFromStepOne, SubmitsAndClearsPassword | ui-auth-loginform--docs | 49d01ef4ee123094824c0796a8bda45ceb1e0df846b3a6c52f2f6c93e8b80773 |
| web/src/ui/auth/ProofDialog.stories.tsx | Design system/Auth/ProofDialog | ReauthGated, AuthenticatorCode, Refused | ui-auth-proofdialog--docs | 5e78292e2f560f94a1d93a300badd400b20ec144bfeaa822e66fbb4f8c9de975 |
| web/src/ui/auth/ProviderButton.stories.tsx | Design system/Auth/ProviderButton | Google, GoogleSignUp, Microsoft, MicrosoftSignUp, GitHub, GitHubSignUp, Plain, Contacting, LastUsed, Disabled, FiresOnClick | ui-auth-providerbutton--docs | 07e231f6b2dd17d14c0f643e5292fc00f9c4e0edcf36b6dd4820817f37b6c3b3 |
| web/src/ui/auth/QrCode.stories.tsx | Design system/Auth/QrCode | Default, NamedAndScannable | ui-auth-qrcode--docs | 304294744c2133fc4aa9819d4f1fc7c593495220f535a4562d54e7d0d3e8c2d2 |
| web/src/ui/auth/SecondFactorChallenge.stories.tsx | Design system/Auth/SecondFactorChallenge | AuthenticatorAndPasskey, AuthenticatorOnly, PasskeyOnly, NoFactorPresentable, CheckingCode, WaitingForPasskey, CodeRefused, NoSkipControl, SubmitsTrimmedCode | ui-auth-secondfactorchallenge--docs | 450000be04bf3158fd5482626652878493e67887ee539a75e1543cd62663c60d |
| web/src/ui/auth/SecondFactorSetup.stories.tsx | Design system/Auth/SecondFactorSetup | ConfirmPassword, ConfirmPasswordRefused, Choose, ChooseNoPasskeySupport, Authenticator, AuthenticatorCodeRefused, RecoveryCodes, NoSkipOnAnyStep, CodesNeedAcknowledgement | ui-auth-secondfactorsetup--docs | 0f61b2fb76507c21c740b68de56d03ef3fc8c78ff2465b72368ef713774743e3 |

Final component descriptions absent from both docgen and explicit metadata: none.

The source-backed gaps below describe the baseline and their bounded implementation. All five originally unmapped page roots now have generated Docs and real component examples. The only final unmapped JSX-rendering symbols are the App BrowserRouter bootstrap wrapper and AuthProvider infrastructure, explicitly justified in the manifest. Composition coverage identifies every remaining visual owner but does not claim every private optional branch has an independent Canvas example.

| Current module | Primary description source | Owned prop types / defaults | Explicit controls / args |
| --- | --- | --- | --- |
| web/src/routes/Adapters.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/AwsAccessFields.stories.tsx | explicit source-backed CSF Docs | mode: 'ambient' \| 'assume-role' \| 'web-identity' \| 'static' = 'assume-role' | no explicit args; explicit argTypes |
| web/src/routes/CertificatesTab.stories.tsx | component documentation | project: { readonly org: string; readonly project: string }; environments: unknown; view: { rows: readonly CertificateRow[]; isPending: boolean; isError: boolean } | args present; docgen-only controls |
| web/src/routes/InstanceMailPanel.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/OAuth2ProvidersPanel.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/OpenRegistration.stories.tsx | explicit source-backed CSF Docs | scope: { readonly kind: 'org'; readonly org: string } \| { readonly kind: 'instance' }; scopeName: string; origin: string; authorityName: (principal: string) => string; onChanged: (text: string) => void | args present; docgen-only controls |
| web/src/routes/Overview.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/PkiIssuersPanel.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/PkiProfilesPanel.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/Remotes.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/SAMLDone.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/SSHCertificates.stories.tsx | explicit source-backed CSF Docs | org: string; project: string; sessionId: string \| null; environments: unknown; requesterOptions: unknown | args present; docgen-only controls |
| web/src/routes/SignupVerify.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceSettingsLink.stories.tsx | component documentation | path: string; className: string; children: ReactNode | args present; docgen-only controls |
| web/src/ui/CeremonyNotice.stories.tsx | component documentation | children: ReactNode; glyph: string = '!' | args present; docgen-only controls |
| web/src/ui/auth/LocalSignupForm.stories.tsx | component documentation | landing: string \| null; org: string; onBack: () => void | args present; docgen-only controls |
| web/src/app/RuntimeMaintenanceBoundary.stories.tsx | component documentation | children: ReactNode; failure: Error \| null; refreshSession: (signal?: AbortSignal) => Promise<void>; queries: QueryClient | args present; docgen-only controls |
| web/src/app/notifications.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/AccountProfile.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/AccountSecurity.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/Audit.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/BindingCard.stories.tsx | explicit source-backed CSF Docs | account: z.infer<typeof zServiceAccountList>['items'][number]; credential: z.infer<typeof zMachineCredentialList>['items'][number]; now: Date; ready: boolean; onReplace: (credential: MachineCredential) => void; onRevoke: (credential: MachineCredential) => void | args present; docgen-only controls |
| web/src/routes/BindingDialog.stories.tsx | component documentation | project: { org: string; project: string }; accounts: unknown; initial: z.infer<typeof zServiceAccountList>['items'][number]; replaces: z.infer<typeof zMachineCredentialList>['items'][number]; reachFor: (accountId: string) => readonly MachineDisclosureReach[]; onClose: () => void; onCreated: (message: string) => void | args present; docgen-only controls |
| web/src/routes/CLIReauth.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/CatalogueManageDialog.stories.tsx | component documentation | refData: { readonly org: string; readonly project: string }; onClose: () => void | args present; docgen-only controls |
| web/src/routes/Ceremony.stories.tsx | component documentation | request: {   purpose: CeremonyPurpose;   /** Each key's classification, so the list can mark the secrets. */   /** The environment the decision authorises, by id. */   environmentId: string;   /** The environment's human name, for the title. */   environmentName: string;   /** The enumerated unit: what the modal lists and what the challenge binds. */   keys: ReadonlyArray<{ id: string; name: string; classification?: 'secret' \| 'config' }>;   /** The guard's state, which decides whether TOTP is on the table. */   window: RevealWindow; }; onAuthorised: () => void; onCancel: () => void | args present; docgen-only controls |
| web/src/routes/ChangeApprovals.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/ChromeIdentityControls.stories.tsx | explicit source-backed CSF Docs | identityId: string; name: string; kind: 'org' \| 'project'; children: ReactNode | args present; docgen-only controls |
| web/src/routes/CompactOrgRetention.stories.tsx | explicit source-backed CSF Docs | policy: z.infer<typeof zRetentionPolicy>; busy: boolean; onSave: (next: RetentionPolicy) => void | args present; docgen-only controls |
| web/src/routes/ConnectionMintDialog.stories.tsx | component documentation | minted: MintedConnectionValue & { readonly label: string }; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ConnectionRow.stories.tsx | component documentation | connection: z.infer<typeof zInstanceConnection>; onRevoke: () => void | args present; docgen-only controls |
| web/src/routes/CreateAccountDialog.stories.tsx | component documentation | project: { org: string; project: string }; onClose: () => void; onCreated: (name: string, kind: ServiceAccount['kind']) => void | args present; docgen-only controls |
| web/src/routes/CreateProviderDialog.stories.tsx | component documentation | project: { org: string; project: string }; onClose: () => void; onCreated: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/CredentialForm.stories.tsx | component documentation | provider: string; busy: boolean; onCancel: () => void; onSubmit: (credential: string) => Promise<void> | args present; docgen-only controls |
| web/src/routes/DefinitionsBundlePanel.stories.tsx | explicit source-backed CSF Docs | org: string; project: string; settings: z.infer<typeof zDefinitionsSettings> | args present; docgen-only controls |
| web/src/routes/DeleteAccountDialog.stories.tsx | component documentation | project: { org: string; project: string }; account: z.infer<typeof zServiceAccountList>['items'][number]; onClose: () => void; onDeleted: (name: string) => void | args present; docgen-only controls |
| web/src/routes/DeleteAdapterDialog.stories.tsx | component documentation | adapter: z.infer<typeof zAdapter>; busy: boolean; onCancel: () => void; onDecide: (decision: 'prune' \| 'retain') => void | args present; docgen-only controls |
| web/src/routes/DeleteProviderDialog.stories.tsx | component documentation | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; liveLeaseCount: number; leasesKnown: boolean; onClose: () => void; onDeleted: (origin: string, revokedCount: number) => void | args present; docgen-only controls |
| web/src/routes/DeliveryTargets.stories.tsx | explicit source-backed CSF Docs | project: { org: string; project: string }; view: {   readonly support: ReportingSupport;   /** Readable environments only: an unreadable one is absent, never redacted (D7). */   readonly reports: readonly EnvironmentReports[];   /** Environments whose listing failed for any reason other than "not readable". */   readonly failures: readonly EnvRef[];   readonly isPending: boolean; }; known: boolean; accounts: unknown; now: Date | args present; docgen-only controls |
| web/src/routes/EnrolmentGate.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/EstablishCredential.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/FederationIssuersPanel.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/FleetUpdateNotice.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/FolderCleanupDialog.stories.tsx | component documentation | proposals: unknown; existingFolders: unknown; busy: boolean; envName: (id: string) => string; onApply: (moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/GrantDialog.stories.tsx | component documentation | project: { org: string; project: string }; account: z.infer<typeof zServiceAccountList>['items'][number]; scope: unknown; machineReveal: boolean; mayGrantReporting: boolean; liveCredentials: number; onClose: () => void; onGranted: (environment: string, results: readonly GrantResult[]) => void | args present; docgen-only controls |
| web/src/routes/HealthChip.stories.tsx | explicit source-backed CSF Docs | target: z.infer<typeof zAdapterTarget> | args present; explicit argTypes |
| web/src/routes/HistoryDrawer.stories.tsx | component documentation | refData: { readonly org: string; readonly project: string }; environments: unknown; keys: unknown; currentRevisions: ReadonlyMap<string, bigint>; protectedEnvironmentIds: unknown; cellsByEnvironment: ReadonlyMap<string, readonly HistoryCurrentCell[]>; pendingByEnvironment: ReadonlyMap<string, number>; pendingByOthersByEnvironment: ReadonlyMap<string, number>; currentValuesByEnvironment: ReadonlyMap<string, readonly ValueCell[]>; openerRef: RefObject<HTMLAnchorElement \| null> | args present; docgen-only controls |
| web/src/routes/ImportWizard.stories.tsx | component documentation | matrixRef: { readonly org: string; readonly project: string }; environments: unknown; gitManaged: boolean; canDeclareKeys: boolean; onClose: () => void | args present; docgen-only controls |
| web/src/routes/InstanceAdmin.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/InstanceConfig.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/InviteDialog.stories.tsx | component documentation | scope: { readonly kind: 'org'; readonly org: string } \| { readonly kind: 'instance' }; scopeName: string; origin: string; onDone: (text: string) => void; onCancel: () => void | args present; docgen-only controls |
| web/src/routes/KeyDeclarationDetail.stories.tsx | component documentation | refData: { readonly org: string; readonly project: string }; keyId: string; environments: unknown; impact: {   readonly setEnvironmentIds: readonly string[];   readonly pendingEnvironmentIds: readonly string[]; }; impactReady: boolean; openerRef: RefObject<HTMLAnchorElement \| null> | args present; docgen-only controls |
| web/src/routes/LastApplyProvenance.stories.tsx | component documentation | lastApply: NonNullable<DefinitionsSettings['last_apply']> | args present; docgen-only controls |
| web/src/routes/LeaseActionDialog.stories.tsx | component documentation | project: { org: string; project: string }; action: {   readonly verb: 'renew' \| 'revoke' \| 'settle';   readonly environmentId: string;   readonly environmentName: string;   readonly lease: DynamicLease; }; onClose: () => void; onDone: (message: string) => void | args present; docgen-only controls |
| web/src/routes/LeaseMintDialog.stories.tsx | component documentation | project: { org: string; project: string }; sessionId: string \| null; providers: unknown; environments: unknown; lifecycle: \| { readonly kind: 'idle' } \| { readonly kind: 'reviewing'; readonly request: Req } \| { readonly kind: 'submitting'; readonly request: Req } \| { readonly kind: 'failed'; readonly request: Req; readonly error: string } \| {     readonly kind: 'disclosed';     readonly request: Req;     readonly result: Res;     readonly stored: boolean;     readonly heldBack: boolean;     readonly copyStatus: string \| null;   }; move: (   event: MintLifecycleEvent<Req, Res>, ) => MintTransitionResult<Req, Res>; isSubmitting: (requestId: number) => boolean; nextRequestId: () => number; onClose: () => void | args present; docgen-only controls |
| web/src/routes/Login.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/MachineAccess.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/MachineRevealDialog.stories.tsx | explicit source-backed CSF Docs | enable: boolean; busy: boolean; failure: string \| null; onConfirm: () => void; onClose: () => void | args present; docgen-only controls |
| web/src/routes/MachineRevokeCredentialDialog.stories.tsx | component documentation | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; onClose: () => void; onRevoked: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/Matrix.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/MatrixKeyCreate.stories.tsx | component documentation | folders: unknown; environments: unknown; protectedEnvironmentIds: unknown; initialFolder: string \| null; existingKeyNames: unknown; gitManaged: boolean = false; busy: boolean; mutationError: string \| null; onClose: () => void; onCreate: (payload: MatrixKeyCreatePayload) => Promise<void> | args present; docgen-only controls |
| web/src/routes/MatrixPublishSheet.stories.tsx | component documentation | refData: { readonly org: string; readonly project: string }; environments: unknown; revisions: ReadonlyMap<string, bigint>; pendingByEnvironment: ReadonlyMap<string, readonly MatrixPendingEntry[]>; problems: unknown; protectedEnvironmentIds: unknown; busy: boolean; mutationError: string \| null; onPublish: (environmentIds: readonly string[]) => void; onClose: () => void | args present; docgen-only controls |
| web/src/routes/MatrixRowEditor.stories.tsx | component documentation | refData: { readonly org: string; readonly project: string }; keyRecord: MatrixKeyList['items'][number]; environmentId: string; rows: unknown; busy: boolean; mutationError: string \| null; onClose: () => void; onApply: (changes: readonly MatrixEditorChange[]) => Promise<void>; onCopy: (destinations: readonly string[], confirmProtected: boolean) => void | args present; docgen-only controls |
| web/src/routes/Members.stories.tsx | explicit source-backed CSF Docs | scope: { readonly kind: 'org' } \| { readonly kind: 'instance' } | args present; docgen-only controls |
| web/src/routes/MintConnectionForm.stories.tsx | component documentation | mint: ReturnType<typeof useMintConnection>; onMinted: (result: Disclosed) => void | args present; docgen-only controls |
| web/src/routes/MintDialog.stories.tsx | component documentation | lifecycle: Exclude<MintLifecycle, { readonly kind: 'idle' }>; move: (   event: MintLifecycleEvent<Req, Res>, ) => MintTransitionResult<Req, Res>; isSubmitting: (requestId: number) => boolean | args present; docgen-only controls |
| web/src/routes/OIDCDone.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/OidcProvidersPanel.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/OpsDiagnosticBanners.stories.tsx | component documentation | severity: 'error' \| 'warn' \| 'unknown'; children: ReactNode | args present; docgen-only controls |
| web/src/routes/OrgSettings.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/PinReleaseOutcome.stories.tsx | explicit source-backed CSF Docs | consequence: RetentionConsequence; revision: unknown | args present; docgen-only controls |
| web/src/routes/Placeholder.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/ProfileUpdateBadge.stories.tsx | explicit source-backed CSF Docs | version: string | no explicit args; docgen-only controls |
| web/src/routes/ProjectSettings.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/Projects.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/ProviderDiscoveryAlert.stories.tsx | explicit source-backed CSF Docs | onRetry: () => void | args present; docgen-only controls |
| web/src/routes/Reconnect.stories.tsx | component documentation | origin: string; name: string | args present; docgen-only controls |
| web/src/routes/RemoteCard.stories.tsx | explicit source-backed CSF Docs | remote: z.infer<typeof zRemote>; duplicateIdentity: boolean | args present; docgen-only controls |
| web/src/routes/RetentionBoundsFields.stories.tsx | component documentation | age: \| { readonly kind: 'days'; readonly days: string } \| { readonly kind: 'exact'; readonly seconds: number } \| { readonly kind: 'absent' }; count: string; onAgeChange: (next: RetentionDayState) => void; onCountChange: (next: string) => void | args present; docgen-only controls |
| web/src/routes/RevisionDiff.stories.tsx | explicit source-backed CSF Docs | env: { org: string; project: string; environment: string }; environmentName: string; left: unknown; right: unknown; onClose: () => void | args present; docgen-only controls |
| web/src/routes/RevokeConnectionDialog.stories.tsx | component documentation | connection: z.infer<typeof zInstanceConnection>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/RevokeCredentialDialog.stories.tsx | component documentation | adapter: z.infer<typeof zAdapter>; busy: boolean; onCancel: () => void; onConfirm: () => void | args present; docgen-only controls |
| web/src/routes/SamlProvidersPanel.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/SamlSpKeysPanel.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/ScanBlockDialog.stories.tsx | component documentation | title: string; intro: string; findings: unknown; onOverride: ((tokens: readonly string[]) => Promise<void>) \| null; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ScanWarnDialog.stories.tsx | component documentation | keyName: string; items: unknown; onDismiss: (item: ScanWarnItem) => Promise<readonly ScanWarnItem[]>; onReclassify: () => Promise<void>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ScimProvisioning.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/Sections.stories.tsx | component documentation | id: string; title: string; danger: boolean = false; tight: boolean = false; question: boolean = false; children: ReactNode | args present; docgen-only controls |
| web/src/routes/SetCredentialDialog.stories.tsx | component documentation | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; onClose: () => void; onSet: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/SidebarLinkItem.stories.tsx | explicit source-backed CSF Docs | link: {   /** The surface id, or `project-members` for the filtered members projection. */   readonly id: string;   readonly label: string;   readonly to: string;   /** Non-null renders the row as a disabled span carrying this title. */   readonly disabledReason: string \| null; }; onNavigate: () => void | args present; docgen-only controls |
| web/src/routes/SidebarVersion.stories.tsx | component documentation | version: string \| undefined | no explicit args; docgen-only controls |
| web/src/routes/StepUpBanner.stories.tsx | component documentation | session: z.infer<typeof zWhoAmI> | args present; docgen-only controls |
| web/src/routes/SystemProjectNotice.stories.tsx | explicit source-backed CSF Docs | org: string; project: string | args present; docgen-only controls |
| web/src/routes/SystemScopeRefusal.stories.tsx | component documentation | surface: (typeof SYSTEM_SCOPE_REFUSED_SURFACES)[number] | no explicit args; docgen-only controls |
| web/src/routes/TargetForm.stories.tsx | explicit source-backed CSF Docs | title: string; provider: string; environments: unknown; keys: unknown; busy: boolean; initial: z.infer<typeof zAdapterTarget>; lockRouting: boolean; onCancel: () => void; onSubmit: (input: AdapterTargetInput) => Promise<void> | args present; docgen-only controls |
| web/src/routes/TemporaryAccess.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/ThemeToggle.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/UpdateJobStatus.stories.tsx | component documentation | jobID: string; job: InstanceUpdateJob \| undefined | args present; docgen-only controls |
| web/src/routes/Values.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceApprove.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceCallback.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceScope.stories.tsx | component documentation | remote: string; children: ReactNode | args present; docgen-only controls |
| web/src/routes/WorkspaceStepUp.stories.tsx | component documentation |  | args present; docgen-only controls |
| web/src/routes/accessRules/AccessRules.stories.tsx | explicit source-backed CSF Docs |  | no explicit args; docgen-only controls |
| web/src/ui/Alert.stories.tsx | component documentation | tone: 'danger' \| 'done' \| 'warn' \| 'info' = 'danger'; action: ReactNode; children: ReactNode | args present; explicit argTypes |
| web/src/ui/Badge.stories.tsx | explicit source-backed CSF Docs | tone: 'neutral' \| 'danger' \| 'changed' \| 'ok' = 'neutral'; mono: boolean | args present; explicit argTypes |
| web/src/ui/Button.stories.tsx | explicit source-backed CSF Docs | variant: 'primary' \| 'secondary' \| 'danger' \| 'quiet' = 'secondary' | args present; explicit argTypes |
| web/src/ui/Checkbox.stories.tsx | explicit source-backed CSF Docs | label: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/ChoiceGroup.stories.tsx | component documentation | legend: string; hint: string; layout: 'stack' \| 'wrap' \| 'nowrap' = 'stack'; columns: number; variant: 'rows' \| 'chips' = 'rows'; className: string; children: ReactNode | args present; explicit argTypes |
| web/src/ui/Dialog.stories.tsx | explicit source-backed CSF Docs | title: string; mono: boolean; lede: ReactNode; size: 'narrow' \| 'wide' = 'narrow'; actions: ReactNode; onCancel: (event: SyntheticEvent<HTMLDialogElement>) => void; onBackdropClick: () => void; initialFocus: RefObject<HTMLElement \| null>; pinActions: boolean; className: string; children: ReactNode | args present; docgen-only controls |
| web/src/ui/Disclosure.stories.tsx | component documentation | label: ReactNode; defaultOpen: boolean = false; className: string; children: ReactNode | args present; docgen-only controls |
| web/src/ui/Field.stories.tsx | component documentation | label: string; hint: string; error: string; className: string; aria-describedby: string; aria-invalid: ComponentProps<'input'>['aria-invalid']; id: string; children: (control: FieldControlProps) => ReactNode | args present; docgen-only controls |
| web/src/ui/Glyph.stories.tsx | explicit source-backed CSF Docs | name: 'lock' \| 'link' \| 'check' \| 'cross' \| 'warn' \| 'delta' \| 'draft' \| 'ellipsis' \| 'chevron'; label: string | args present; explicit argTypes |
| web/src/ui/Input.stories.tsx | explicit source-backed CSF Docs | label: string; hint: string; error: string; className: string; mono: boolean; revealable: boolean | args present; docgen-only controls |
| web/src/ui/Menu.stories.tsx | component documentation | label: string; glyph: ReactNode = '⋯'; children: ReactNode; className: string | args present; docgen-only controls |
| web/src/ui/Radio.stories.tsx | explicit source-backed CSF Docs | label: string; name: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/Select.stories.tsx | explicit source-backed CSF Docs | label: string; hint: string; error: string; className: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/Tabs.stories.tsx | explicit source-backed CSF Docs | initial: 'accounts' \| 'connections' \| 'federation' = 'accounts'; label: string = 'Machine access sections' | no explicit args; docgen-only controls |
| web/src/ui/Textarea.stories.tsx | explicit source-backed CSF Docs | label: string; hint: string; error: string; className: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/ThemeIcon.stories.tsx | component documentation | dark: boolean | args present; docgen-only controls |
| web/src/ui/ToggleChip.stories.tsx | explicit source-backed CSF Docs | pressed: boolean; mode: 'include' \| 'exclude' = 'include'; mono: boolean | args present; explicit argTypes |
| web/src/ui/Tokens.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/ui/Typography.stories.tsx | component documentation |  | no explicit args; docgen-only controls |
| web/src/ui/auth/AuthenticatorCodeField.stories.tsx | component documentation | submitLabel: string; busy: string \| null; disabled: boolean; onSubmit: (code: string) => void; children: ReactNode | args present; docgen-only controls |
| web/src/ui/auth/LoginFlow.stories.tsx | explicit source-backed CSF Docs | scenario: 'password-enrolled' \| 'password-unenrolled' \| 'passkey' \| 'provider' \| 'sign-up'; policy: 'require-second-factor' \| 'allow-unenrolled' | args present; explicit argTypes |
| web/src/ui/auth/LoginForm.stories.tsx | component documentation | providers: unknown; passkeys: boolean; signup: SignupDoor \| null; paused: boolean; lastUsed: LastSignIn \| null; busy: 'password' \| 'passkey' \| { provider: ProviderIdentity \| null } \| null; error: string \| null; onPassword: (credentials: { username: string; password: string }) => void; onPasskey: () => void; onProvider: (provider: ProviderIdentity, intent: SignInIntent) => void; links: ReactNode; initialIntent: 'sign-in' \| 'sign-up' = 'sign-in' | args present; docgen-only controls |
| web/src/ui/auth/ProofDialog.stories.tsx | component documentation | title: string = "Confirm it's you"; lede: string; field: 'code' \| 'password' \| 'code-or-password'; label: string; reauth: boolean = false; pending: boolean = false; failure: string \| null = null; onCancel: () => void; onSubmit: (value: string, clear: () => void) => void | args present; docgen-only controls |
| web/src/ui/auth/ProviderButton.stories.tsx | component documentation | provider: z.infer<typeof zAuthMethodProvider>; intent: 'sign-in' \| 'sign-up'; busy: boolean; disabled: boolean; lastUsed: boolean = false; onClick: () => void | args present; docgen-only controls |
| web/src/ui/auth/QrCode.stories.tsx | component documentation | value: string; title: string | args present; docgen-only controls |
| web/src/ui/auth/SecondFactorChallenge.stories.tsx | component documentation | username: string; totp: boolean; passkey: boolean; busy: 'code' \| 'passkey' \| null; error: string \| null; onCode: (code: string) => void; onPasskey: () => void | args present; docgen-only controls |
| web/src/ui/auth/SecondFactorSetup.stories.tsx | component documentation | username: string; step: \| { kind: 'password' } \| { kind: 'codes'; codes: readonly string[] } \| { kind: 'choose' } \| { kind: 'totp'; otpauthUrl: string; secret: string }; passkeys: boolean; busy: 'password' \| 'totp' \| 'passkey' \| 'code' \| null; error: string \| null; onPassword: (password: string) => void; onCodesStored: () => void; onChooseTotp: () => void; onChoosePasskey: () => void; onConfirmCode: (code: string) => void | args present; docgen-only controls |

Installed react-docgen extracts 129 of 131 primary modules; the two render-only modules use explicit CSF documentation. Forty-four parser-level descriptions remain empty, supplemented by explicit module Docs descriptions. This is a metadata audit; source extraction cannot prove that every live control can express application callback contracts. Button controls and representative Docs interactions are proved separately in the browser handoff.

AWS Docs controls intentionally expose the local Example.mode fixture, default assume-role, rather than serializing the real AwsAccessFields value: AwsAccess and onChange(value: AwsAccess): void callback contract. The original source props declaration is preserved in the component inventory. Mode changes reset fixture fields and real Authentication selection edits the descriptor; secret clearing remains verified by owning unit regressions.

| Current route ID | Path | Component | Current story owners | Disposition |
| --- | --- | --- | --- | --- |
| login | /login | web/src/routes/Login.tsx#Login | web/src/routes/Login.stories.tsx | component stories exist; real route/chrome journey not yet established |
| signup | /signup | web/src/routes/Login.tsx#Login | web/src/routes/Login.stories.tsx | component stories exist; real route/chrome journey not yet established |
| signup-verify | /signup/verify | web/src/routes/SignupVerify.tsx#SignupVerify | web/src/routes/SignupVerify.stories.tsx | component stories exist; real route/chrome journey not yet established |
| establish-credential | /establish | web/src/routes/EstablishCredential.tsx#EstablishCredential | web/src/routes/EstablishCredential.stories.tsx | component stories exist; real route/chrome journey not yet established |
| overview | / | web/src/routes/Placeholder.tsx#Overview | web/src/routes/Overview.stories.tsx | component stories exist; real route/chrome journey not yet established |
| projects | /projects | web/src/routes/Projects.tsx#Projects | web/src/routes/Projects.stories.tsx | component stories exist; real route/chrome journey not yet established |
| remotes | /remotes | web/src/routes/Remotes.tsx#Remotes | web/src/routes/Remotes.stories.tsx | component stories exist; real route/chrome journey not yet established |
| members | /orgs/:org/members | web/src/routes/Members.tsx#Members | web/src/routes/Members.stories.tsx | component stories exist; real route/chrome journey not yet established |
| org-settings | /orgs/:org/settings | web/src/routes/OrgSettings.tsx#OrgSettings | web/src/routes/OrgSettings.stories.tsx | component stories exist; real route/chrome journey not yet established |
| scim | /orgs/:org/scim | web/src/routes/ScimProvisioning.tsx#ScimProvisioningPage | web/src/routes/ScimProvisioning.stories.tsx | component stories exist; real route/chrome journey not yet established |
| audit | /orgs/:org/audit | web/src/routes/Audit.tsx#Audit | web/src/routes/Audit.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-admin | /instance | web/src/routes/InstanceAdmin.tsx#InstanceAdmin | web/src/routes/InstanceAdmin.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-config | /instance/config | web/src/routes/InstanceConfig.tsx#InstanceConfig | web/src/routes/InstanceConfig.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-members | /instance/members | web/src/routes/Members.tsx#Members | web/src/routes/Members.stories.tsx | component stories exist; real route/chrome journey not yet established |
| settings | /settings | web/src/routes/AccountSecurity.tsx#AccountSecurity | web/src/routes/AccountSecurity.stories.tsx | component stories exist; real route/chrome journey not yet established |
| matrix | /orgs/:org/projects/:project/matrix | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| history | /orgs/:org/projects/:project/matrix/history | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| key-detail | /orgs/:org/projects/:project/matrix/keys/:key | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| values | /orgs/:org/projects/:project/environments/:environment/values | web/src/routes/Values.tsx#Values | web/src/routes/Values.stories.tsx | component stories exist; real route/chrome journey not yet established |
| machine-access | /orgs/:org/projects/:project/machine-access | web/src/routes/MachineAccess.tsx#MachineAccessPage | web/src/routes/MachineAccess.stories.tsx | component stories exist; real route/chrome journey not yet established |
| change-approvals | /orgs/:org/projects/:project/change-approvals | web/src/routes/ChangeApprovals.tsx#ChangeApprovals | web/src/routes/ChangeApprovals.stories.tsx | component stories exist; real route/chrome journey not yet established |
| temporary-access | /orgs/:org/projects/:project/temporary-access | web/src/routes/TemporaryAccess.tsx#TemporaryAccess | web/src/routes/TemporaryAccess.stories.tsx | component stories exist; real route/chrome journey not yet established |
| adapters | /orgs/:org/projects/:project/adapters | web/src/routes/Adapters.tsx#AdaptersPage | web/src/routes/Adapters.stories.tsx | component stories exist; real route/chrome journey not yet established |
| project-audit | /orgs/:org/projects/:project/audit | web/src/routes/Audit.tsx#Audit | web/src/routes/Audit.stories.tsx | component stories exist; real route/chrome journey not yet established |
| project-settings | /orgs/:org/projects/:project/settings | web/src/routes/ProjectSettings.tsx#ProjectSettings | web/src/routes/ProjectSettings.stories.tsx | component stories exist; real route/chrome journey not yet established |
| cli-reauth | /reauth/cli | web/src/routes/CLIReauth.tsx#CLIReauth | web/src/routes/CLIReauth.stories.tsx | component stories exist; real route/chrome journey not yet established |
| workspace-approve | /workspace/approve | web/src/routes/WorkspaceApprove.tsx#WorkspaceApprove | web/src/routes/WorkspaceApprove.stories.tsx | component stories exist; real route/chrome journey not yet established |
| workspace-callback | /workspace/callback | web/src/routes/WorkspaceCallback.tsx#WorkspaceCallback | web/src/routes/WorkspaceCallback.stories.tsx | component stories exist; real route/chrome journey not yet established |
| oidc-done | /auth/oidc/done | web/src/routes/OIDCDone.tsx#OIDCDone | web/src/routes/OIDCDone.stories.tsx | component stories exist; real route/chrome journey not yet established |
| saml-done | /auth/saml/done | web/src/routes/SAMLDone.tsx#SAMLDone | web/src/routes/SAMLDone.stories.tsx | component stories exist; real route/chrome journey not yet established |

| Current rendering symbol | Source line | Direct story owner | Nearest composite story owner | Disposition |
| --- | --- | --- | --- | --- |
| WorkspaceContextProvider | web/src/api/transport.tsx:49 | web/src/routes/WorkspaceSettingsLink.stories.tsx | web/src/routes/RemoteCard.stories.tsx (depth 2), web/src/routes/Remotes.stories.tsx (depth 3) | direct-story coverage |
| AppRoutes | web/src/app/App.tsx:175 | web/src/routes/Overview.stories.tsx |  | direct-story coverage |
| App | web/src/app/App.tsx:241 |  |  | justified infrastructure exclusion above |
| AuthProvider | web/src/app/AuthProvider.tsx:180 |  |  | justified infrastructure exclusion above |
| RuntimeMaintenanceBoundary | web/src/app/RuntimeMaintenanceBoundary.tsx:27 | web/src/app/RuntimeMaintenanceBoundary.stories.tsx |  | direct-story coverage |
| RuntimeInterruption | web/src/app/RuntimeMaintenanceBoundary.tsx:136 |  | web/src/app/RuntimeMaintenanceBoundary.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ToastViewport | web/src/app/notifications.tsx:75 | web/src/app/notifications.stories.tsx | web/src/routes/Overview.stories.tsx (depth 1), web/src/routes/FleetUpdateNotice.stories.tsx (depth 1) | direct-story coverage |
| AccountProfile | web/src/routes/AccountProfile.tsx:11 | web/src/routes/AccountProfile.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1) | direct-story coverage |
| ProfileForm | web/src/routes/AccountProfile.tsx:25 |  | web/src/routes/AccountProfile.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AccountSecurity | web/src/routes/AccountSecurity.tsx:63 | web/src/routes/AccountSecurity.stories.tsx |  | direct-story coverage |
| AccountProofDialog | web/src/routes/AccountSecurity.tsx:627 |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| RecoveryCodes | web/src/routes/AccountSecurity.tsx:655 |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| PrototypeSessions | web/src/routes/AccountSecurity.tsx:699 |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ThemePreference | web/src/routes/AccountSecurity.tsx:738 |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| AdaptersPage | web/src/routes/Adapters.tsx:150 | web/src/routes/Adapters.stories.tsx |  | direct-story coverage |
| HealthChip | web/src/routes/Adapters.tsx:294 | web/src/routes/HealthChip.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 2) | direct-story coverage |
| AdapterPanel | web/src/routes/Adapters.tsx:304 |  | web/src/routes/Adapters.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| OriginMoveForm | web/src/routes/Adapters.tsx:511 |  | web/src/routes/Adapters.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| CredentialForm | web/src/routes/Adapters.tsx:587 | web/src/routes/CredentialForm.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 2) | direct-story coverage |
| RevokeCredentialDialog | web/src/routes/Adapters.tsx:639 | web/src/routes/RevokeCredentialDialog.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 2) | direct-story coverage |
| DeleteAdapterDialog | web/src/routes/Adapters.tsx:675 | web/src/routes/DeleteAdapterDialog.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 2) | direct-story coverage |
| MoveDetail | web/src/routes/Adapters.tsx:752 |  | web/src/routes/Adapters.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CreateAdapterPanel | web/src/routes/Adapters.tsx:918 |  | web/src/routes/Adapters.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| TargetForm | web/src/routes/Adapters.tsx:1124 | web/src/routes/TargetForm.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 2) | direct-story coverage |
| TargetDetail | web/src/routes/Adapters.tsx:1506 |  | web/src/routes/Adapters.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ConnectionFacts | web/src/routes/Adapters.tsx:1811 |  | web/src/routes/Adapters.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PlanChanges | web/src/routes/Adapters.tsx:1827 |  | web/src/routes/Adapters.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ConflictArtifact | web/src/routes/Adapters.tsx:1856 |  | web/src/routes/Adapters.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| RemoveDialog | web/src/routes/Adapters.tsx:1895 |  | web/src/routes/Adapters.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AdapterCeremony | web/src/routes/Adapters.tsx:1972 |  | web/src/routes/Adapters.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| Outcome | web/src/routes/Audit.tsx:40 |  | web/src/routes/Audit.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| Audit | web/src/routes/Audit.tsx:83 | web/src/routes/Audit.stories.tsx |  | direct-story coverage |
| AuditTrail | web/src/routes/Audit.tsx:96 |  | web/src/routes/Audit.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| AuditFact | web/src/routes/Audit.tsx:413 |  | web/src/routes/Audit.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AuditScopeFacts | web/src/routes/Audit.tsx:424 |  | web/src/routes/Audit.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AwsAccessFields | web/src/routes/AwsAccessFields.tsx:78 |  | web/src/routes/AwsAccessFields.stories.tsx (depth 1), web/src/routes/CredentialForm.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CLIReauth | web/src/routes/CLIReauth.tsx:26 | web/src/routes/CLIReauth.stories.tsx |  | direct-story coverage |
| CLIReauthMessage | web/src/routes/CLIReauth.tsx:213 |  | web/src/routes/CLIReauth.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CatalogueManageDialog | web/src/routes/CatalogueManageDialog.tsx:54 | web/src/routes/CatalogueManageDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| CreateFolder | web/src/routes/CatalogueManageDialog.tsx:157 |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| FolderRow | web/src/routes/CatalogueManageDialog.tsx:210 |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| CreateKeyGroup | web/src/routes/CatalogueManageDialog.tsx:296 |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| GroupRow | web/src/routes/CatalogueManageDialog.tsx:348 |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| Ceremony | web/src/routes/Ceremony.tsx:165 | web/src/routes/Ceremony.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/ImportWizard.stories.tsx (depth 1) | direct-story coverage |
| WorkspaceStepUp | web/src/routes/Ceremony.tsx:397 |  | web/src/routes/Ceremony.stories.tsx (depth 1), web/src/routes/WorkspaceStepUp.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CertificatesTab | web/src/routes/CertificatesTab.tsx:64 | web/src/routes/CertificatesTab.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| IssueDialog | web/src/routes/CertificatesTab.tsx:263 |  | web/src/routes/CertificatesTab.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ChangeApprovals | web/src/routes/ChangeApprovals.tsx:84 | web/src/routes/ChangeApprovals.stories.tsx |  | direct-story coverage |
| ScopedChangeApprovals | web/src/routes/ChangeApprovals.tsx:92 |  | web/src/routes/ChangeApprovals.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ApprovalRequestRow | web/src/routes/ChangeApprovals.tsx:458 |  | web/src/routes/ChangeApprovals.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PolicyPeople | web/src/routes/ChangeApprovals.tsx:550 |  | web/src/routes/ChangeApprovals.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ChromeIdentityControls | web/src/routes/ChromeIdentityControls.tsx:19 | web/src/routes/ChromeIdentityControls.stories.tsx | web/src/routes/OrgSettings.stories.tsx (depth 1), web/src/routes/ProjectSettings.stories.tsx (depth 1) | direct-story coverage |
| DefinitionsBundlePanel | web/src/routes/DefinitionsBundlePanel.tsx:28 | web/src/routes/DefinitionsBundlePanel.stories.tsx | web/src/routes/ProjectSettings.stories.tsx (depth 1) | direct-story coverage |
| BundleDialog | web/src/routes/DefinitionsBundlePanel.tsx:71 |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 1), web/src/routes/ProjectSettings.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| WideningConfirm | web/src/routes/DefinitionsBundlePanel.tsx:417 |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| LastApplyProvenance | web/src/routes/DefinitionsBundlePanel.tsx:451 | web/src/routes/LastApplyProvenance.stories.tsx | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) | direct-story coverage |
| DifferenceList | web/src/routes/DefinitionsBundlePanel.tsx:490 |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| StateBadge | web/src/routes/DeliveryTargets.tsx:36 |  | web/src/routes/DeliveryTargets.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| DeliveryTargetsPanel | web/src/routes/DeliveryTargets.tsx:82 | web/src/routes/DeliveryTargets.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| EnrolmentGate | web/src/routes/EnrolmentGate.tsx:31 | web/src/routes/EnrolmentGate.stories.tsx |  | direct-story coverage |
| EstablishCredential | web/src/routes/EstablishCredential.tsx:33 | web/src/routes/EstablishCredential.stories.tsx |  | direct-story coverage |
| RecoveryForm | web/src/routes/EstablishCredential.tsx:216 |  | web/src/routes/EstablishCredential.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| FederationIssuersPanel | web/src/routes/FederationIssuersPanel.tsx:60 | web/src/routes/FederationIssuersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| IssuerForm | web/src/routes/FederationIssuersPanel.tsx:325 |  | web/src/routes/FederationIssuersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| FolderCleanupDialog | web/src/routes/FolderCleanupDialog.tsx:30 | web/src/routes/FolderCleanupDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| HistoryDrawer | web/src/routes/HistoryDrawer.tsx:122 | web/src/routes/HistoryDrawer.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| RevisionDetail | web/src/routes/HistoryDrawer.tsx:937 |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| RestoreSheet | web/src/routes/HistoryDrawer.tsx:1165 |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PinSheet | web/src/routes/HistoryDrawer.tsx:1320 |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ReleaseSheet | web/src/routes/HistoryDrawer.tsx:1528 |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PinReleaseOutcome | web/src/routes/HistoryDrawer.tsx:1591 | web/src/routes/PinReleaseOutcome.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | direct-story coverage |
| ImpactValue | web/src/routes/HistoryDrawer.tsx:1677 |  | web/src/routes/HistoryDrawer.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ImportWizard | web/src/routes/ImportWizard.tsx:146 | web/src/routes/ImportWizard.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| InstanceAdmin | web/src/routes/InstanceAdmin.tsx:109 | web/src/routes/InstanceAdmin.stories.tsx |  | direct-story coverage |
| CredentialPolicyPanel | web/src/routes/InstanceAdmin.tsx:246 |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CryptoMaintenance | web/src/routes/InstanceAdmin.tsx:318 |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| InstanceConfig | web/src/routes/InstanceConfig.tsx:18 | web/src/routes/InstanceConfig.stories.tsx |  | direct-story coverage |
| ConfigurationRoot | web/src/routes/InstanceConfig.tsx:31 |  | web/src/routes/InstanceConfig.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ConfigurationOwner | web/src/routes/InstanceConfig.tsx:52 |  | web/src/routes/InstanceConfig.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| SelfConfigCeremony | web/src/routes/InstanceConfig.tsx:209 |  | web/src/routes/InstanceConfig.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| SystemProjectNotice | web/src/routes/InstanceConfig.tsx:260 | web/src/routes/SystemProjectNotice.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| InstanceMailPanel | web/src/routes/InstanceMailPanel.tsx:26 | web/src/routes/InstanceMailPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| InviteDialog | web/src/routes/InviteDialog.tsx:32 | web/src/routes/InviteDialog.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| InviteForm | web/src/routes/InviteDialog.tsx:118 |  | web/src/routes/InviteDialog.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| IssuedAuthorityDialog | web/src/routes/InviteDialog.tsx:233 |  | web/src/routes/InviteDialog.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| KeyDeclarationDetail | web/src/routes/KeyDeclarationDetail.tsx:68 | web/src/routes/KeyDeclarationDetail.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| KeyLoadError | web/src/routes/KeyDeclarationDetail.tsx:236 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| KeyDeclarationBody | web/src/routes/KeyDeclarationDetail.tsx:255 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| Fact | web/src/routes/KeyDeclarationDetail.tsx:441 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ValueRules | web/src/routes/KeyDeclarationDetail.tsx:476 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RuleLine | web/src/routes/KeyDeclarationDetail.tsx:499 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| MetadataEditor | web/src/routes/KeyDeclarationDetail.tsx:535 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RenameKey | web/src/routes/KeyDeclarationDetail.tsx:788 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ReclassifyKey | web/src/routes/KeyDeclarationDetail.tsx:901 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| DeleteKey | web/src/routes/KeyDeclarationDetail.tsx:1057 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ImpactPreview | web/src/routes/KeyDeclarationDetail.tsx:1127 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| ConfirmDialog | web/src/routes/KeyDeclarationDetail.tsx:1167 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| Toggle | web/src/routes/KeyDeclarationDetail.tsx:1417 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 4), web/src/routes/Matrix.stories.tsx (depth 5) | composed coverage: specific branch still requires source/state review |
| DeclarationEditor | web/src/routes/KeyDeclarationDetail.tsx:1450 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RuleFields | web/src/routes/KeyDeclarationDetail.tsx:1769 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| PresenceControl | web/src/routes/KeyDeclarationDetail.tsx:1894 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| GroupEditor | web/src/routes/KeyDeclarationDetail.tsx:1950 |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| Login | web/src/routes/Login.tsx:106 | web/src/routes/Login.stories.tsx | web/src/routes/CLIReauth.stories.tsx (depth 1), web/src/routes/WorkspaceApprove.stories.tsx (depth 1) | direct-story coverage |
| MachineAccessPage | web/src/routes/MachineAccess.tsx:140 | web/src/routes/MachineAccess.stories.tsx |  | direct-story coverage |
| Matrix | web/src/routes/Matrix.tsx:110 | web/src/routes/Matrix.stories.tsx |  | direct-story coverage |
| MatrixCell | web/src/routes/Matrix.tsx:1612 |  | web/src/routes/Matrix.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| MatrixLegend | web/src/routes/Matrix.tsx:1736 |  | web/src/routes/Matrix.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| MatrixKeyCreate | web/src/routes/MatrixKeyCreate.tsx:104 | web/src/routes/MatrixKeyCreate.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| PresenceField | web/src/routes/MatrixKeyCreate.tsx:579 |  | web/src/routes/MatrixKeyCreate.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MatrixPublishSheet | web/src/routes/MatrixPublishSheet.tsx:60 | web/src/routes/MatrixPublishSheet.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| MatrixRowEditor | web/src/routes/MatrixRowEditor.tsx:67 | web/src/routes/MatrixRowEditor.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| Members | web/src/routes/Members.tsx:156 | web/src/routes/Members.stories.tsx |  | direct-story coverage |
| Inspect | web/src/routes/Members.tsx:771 |  | web/src/routes/Members.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| GrantModal | web/src/routes/Members.tsx:1046 |  | web/src/routes/Members.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| OAuth2ProvidersPanel | web/src/routes/OAuth2ProvidersPanel.tsx:21 | web/src/routes/OAuth2ProvidersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| OAuth2Editor | web/src/routes/OAuth2ProvidersPanel.tsx:149 |  | web/src/routes/OAuth2ProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| OIDCDone | web/src/routes/OIDCDone.tsx:32 | web/src/routes/OIDCDone.stories.tsx |  | direct-story coverage |
| OidcProvidersPanel | web/src/routes/OidcProvidersPanel.tsx:74 | web/src/routes/OidcProvidersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| ProviderEditor | web/src/routes/OidcProvidersPanel.tsx:231 |  | web/src/routes/OidcProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| DeleteProviderDialog | web/src/routes/OidcProvidersPanel.tsx:410 |  | web/src/routes/OidcProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| OpenRegistrationPanel | web/src/routes/OpenRegistration.tsx:58 | web/src/routes/OpenRegistration.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| ScopedOpenRegistrationPanel | web/src/routes/OpenRegistration.tsx:63 |  | web/src/routes/OpenRegistration.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PolicySummary | web/src/routes/OpenRegistration.tsx:220 |  | web/src/routes/OpenRegistration.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RegistrationEditor | web/src/routes/OpenRegistration.tsx:410 |  | web/src/routes/OpenRegistration.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ChromeDiagnostic | web/src/routes/OpsDiagnosticBanners.tsx:12 | web/src/routes/OpsDiagnosticBanners.stories.tsx | web/src/routes/Overview.stories.tsx (depth 2) | direct-story coverage |
| OpsDiagnosticBanners | web/src/routes/OpsDiagnosticBanners.tsx:30 |  | web/src/routes/Overview.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| OrgSettings | web/src/routes/OrgSettings.tsx:37 | web/src/routes/OrgSettings.stories.tsx |  | direct-story coverage |
| NameEditor | web/src/routes/OrgSettings.tsx:199 |  | web/src/routes/OrgSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ProjectRetentionList | web/src/routes/OrgSettings.tsx:235 |  | web/src/routes/OrgSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CompactOrgRetention | web/src/routes/OrgSettings.tsx:307 | web/src/routes/CompactOrgRetention.stories.tsx | web/src/routes/OrgSettings.stories.tsx (depth 1) | direct-story coverage |
| PkiIssuersPanel | web/src/routes/PkiIssuersPanel.tsx:41 | web/src/routes/PkiIssuersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| IssuerRow | web/src/routes/PkiIssuersPanel.tsx:111 |  | web/src/routes/PkiIssuersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| CreateIssuerForm | web/src/routes/PkiIssuersPanel.tsx:354 |  | web/src/routes/PkiIssuersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PkiProfilesPanel | web/src/routes/PkiProfilesPanel.tsx:53 | web/src/routes/PkiProfilesPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| ProfileRow | web/src/routes/PkiProfilesPanel.tsx:140 |  | web/src/routes/PkiProfilesPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| Placeholder | web/src/routes/Placeholder.tsx:17 |  | web/src/routes/Overview.stories.tsx (depth 1), web/src/routes/Placeholder.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| Overview | web/src/routes/Placeholder.tsx:36 | web/src/routes/Overview.stories.tsx |  | direct-story coverage |
| NotFound | web/src/routes/Placeholder.tsx:59 | web/src/routes/Placeholder.stories.tsx | web/src/routes/Overview.stories.tsx (depth 1) | direct-story coverage |
| ProjectSettings | web/src/routes/ProjectSettings.tsx:64 | web/src/routes/ProjectSettings.stories.tsx |  | direct-story coverage |
| NameEditor | web/src/routes/ProjectSettings.tsx:318 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ProjectCryptoMaintenance | web/src/routes/ProjectSettings.tsx:361 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| DefinitionsPolicy | web/src/routes/ProjectSettings.tsx:448 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| NewEnvironment | web/src/routes/ProjectSettings.tsx:501 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| EnvironmentLifecycleActions | web/src/routes/ProjectSettings.tsx:595 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| EnvironmentPolicy | web/src/routes/ProjectSettings.tsx:807 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CompactProjectRetention | web/src/routes/ProjectSettings.tsx:941 |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| Projects | web/src/routes/Projects.tsx:12 | web/src/routes/Projects.stories.tsx |  | direct-story coverage |
| ProjectList | web/src/routes/Projects.tsx:74 |  | web/src/routes/Overview.stories.tsx (depth 1), web/src/routes/Projects.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| NewProjectForm | web/src/routes/Projects.tsx:119 |  | web/src/routes/Projects.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ProviderDiscoveryAlert | web/src/routes/ProviderDiscoveryAlert.tsx:4 | web/src/routes/ProviderDiscoveryAlert.stories.tsx | web/src/routes/CLIReauth.stories.tsx (depth 1), web/src/routes/Ceremony.stories.tsx (depth 1) | direct-story coverage |
| Remotes | web/src/routes/Remotes.tsx:84 | web/src/routes/Remotes.stories.tsx |  | direct-story coverage |
| ThisInstance | web/src/routes/Remotes.tsx:135 |  | web/src/routes/Remotes.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| RemoteCard | web/src/routes/Remotes.tsx:172 | web/src/routes/RemoteCard.stories.tsx | web/src/routes/Remotes.stories.tsx (depth 1) | direct-story coverage |
| UpdateJobStatus | web/src/routes/Remotes.tsx:424 | web/src/routes/UpdateJobStatus.stories.tsx | web/src/routes/RemoteCard.stories.tsx (depth 1), web/src/routes/Remotes.stories.tsx (depth 2) | direct-story coverage |
| Absent | web/src/routes/Remotes.tsx:451 |  | web/src/routes/RemoteCard.stories.tsx (depth 1), web/src/routes/Remotes.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AddRemote | web/src/routes/Remotes.tsx:455 |  | web/src/routes/Remotes.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| OriginAllowlist | web/src/routes/Remotes.tsx:568 |  | web/src/routes/Remotes.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ConnectionCredentials | web/src/routes/Remotes.tsx:657 |  | web/src/routes/Remotes.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ConnectionRow | web/src/routes/Remotes.tsx:713 | web/src/routes/ConnectionRow.stories.tsx | web/src/routes/Remotes.stories.tsx (depth 2) | direct-story coverage |
| MintConnectionForm | web/src/routes/Remotes.tsx:779 | web/src/routes/MintConnectionForm.stories.tsx | web/src/routes/Remotes.stories.tsx (depth 2) | direct-story coverage |
| ConnectionMintDialog | web/src/routes/Remotes.tsx:897 | web/src/routes/ConnectionMintDialog.stories.tsx | web/src/routes/Remotes.stories.tsx (depth 2) | direct-story coverage |
| RevokeConnectionDialog | web/src/routes/Remotes.tsx:997 | web/src/routes/RevokeConnectionDialog.stories.tsx | web/src/routes/Remotes.stories.tsx (depth 2) | direct-story coverage |
| WorkspacePicker | web/src/routes/Remotes.tsx:1139 |  | web/src/routes/RemoteCard.stories.tsx (depth 1), web/src/routes/Remotes.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PickerBody | web/src/routes/Remotes.tsx:1155 |  | web/src/routes/RemoteCard.stories.tsx (depth 2), web/src/routes/Remotes.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| OrgProjects | web/src/routes/Remotes.tsx:1186 |  | web/src/routes/RemoteCard.stories.tsx (depth 3), web/src/routes/Remotes.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| RetentionBoundsFields | web/src/routes/RetentionBoundsFields.tsx:8 | web/src/routes/RetentionBoundsFields.stories.tsx |  | direct-story coverage |
| RevisionDiffDialog | web/src/routes/RevisionDiff.tsx:13 | web/src/routes/RevisionDiff.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | direct-story coverage |
| SAMLDone | web/src/routes/SAMLDone.tsx:8 | web/src/routes/SAMLDone.stories.tsx |  | direct-story coverage |
| SSHCertificatesPanel | web/src/routes/SSHCertificates.tsx:110 | web/src/routes/SSHCertificates.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| CADialog | web/src/routes/SSHCertificates.tsx:433 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| TrustDialog | web/src/routes/SSHCertificates.tsx:546 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| DeleteCADialog | web/src/routes/SSHCertificates.tsx:604 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ProfileDialog | web/src/routes/SSHCertificates.tsx:655 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| DeleteProfileDialog | web/src/routes/SSHCertificates.tsx:832 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| IssueDialog | web/src/routes/SSHCertificates.tsx:893 |  | web/src/routes/SSHCertificates.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| SamlProvidersPanel | web/src/routes/SamlProvidersPanel.tsx:42 | web/src/routes/SamlProvidersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| ProviderRow | web/src/routes/SamlProvidersPanel.tsx:128 |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ProviderPolicyForm | web/src/routes/SamlProvidersPanel.tsx:248 |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| MetadataDiff | web/src/routes/SamlProvidersPanel.tsx:323 |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RefreshMetadataForm | web/src/routes/SamlProvidersPanel.tsx:364 |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| ProviderCreateForm | web/src/routes/SamlProvidersPanel.tsx:475 |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| SamlSpKeysPanel | web/src/routes/SamlSpKeysPanel.tsx:30 | web/src/routes/SamlSpKeysPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| SpKeyRow | web/src/routes/SamlSpKeysPanel.tsx:104 |  | web/src/routes/SamlSpKeysPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ScanBlockDialog | web/src/routes/ScanBlockDialog.tsx:23 | web/src/routes/ScanBlockDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1), web/src/routes/CatalogueManageDialog.stories.tsx (depth 2) | direct-story coverage |
| ScanWarnDialog | web/src/routes/ScanWarnDialog.tsx:48 | web/src/routes/ScanWarnDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| ScimProvisioningPage | web/src/routes/ScimProvisioning.tsx:75 | web/src/routes/ScimProvisioning.stories.tsx |  | direct-story coverage |
| BindingsSection | web/src/routes/ScimProvisioning.tsx:150 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| BindingCard | web/src/routes/ScimProvisioning.tsx:199 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AttentionList | web/src/routes/ScimProvisioning.tsx:247 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| DeleteBinding | web/src/routes/ScimProvisioning.tsx:271 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| CreateBindingForm | web/src/routes/ScimProvisioning.tsx:306 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MappingsSection | web/src/routes/ScimProvisioning.tsx:398 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| MappingRow | web/src/routes/ScimProvisioning.tsx:478 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MappingWarnings | web/src/routes/ScimProvisioning.tsx:642 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| CreateMappingForm | web/src/routes/ScimProvisioning.tsx:664 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| CredentialsSection | web/src/routes/ScimProvisioning.tsx:804 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| CredentialRow | web/src/routes/ScimProvisioning.tsx:839 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MintCredentialForm | web/src/routes/ScimProvisioning.tsx:896 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MintDialog | web/src/routes/ScimProvisioning.tsx:968 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| DirectorySection | web/src/routes/ScimProvisioning.tsx:1056 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| DirectoryUserRow | web/src/routes/ScimProvisioning.tsx:1123 |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| JumpIndex | web/src/routes/Sections.tsx:21 | web/src/routes/Sections.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 1) | direct-story coverage |
| Panel | web/src/routes/Sections.tsx:44 | web/src/routes/Sections.stories.tsx, web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/InstanceMailPanel.stories.tsx (depth 1), web/src/routes/OAuth2ProvidersPanel.stories.tsx (depth 1) | direct-story coverage |
| TypedNameConfirm | web/src/routes/Sections.tsx:85 | web/src/routes/Sections.stories.tsx | web/src/routes/OAuth2ProvidersPanel.stories.tsx (depth 1), web/src/routes/DeleteAccountDialog.stories.tsx (depth 1) | direct-story coverage |
| ConsequencesDialog | web/src/routes/Sections.tsx:151 | web/src/routes/Sections.stories.tsx | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 2) | direct-story coverage |
| Explain | web/src/routes/Sections.tsx:206 | web/src/routes/Sections.stories.tsx | web/src/routes/Members.stories.tsx (depth 2), web/src/routes/ScimProvisioning.stories.tsx (depth 3) | direct-story coverage |
| DisplayOnceCopy | web/src/routes/Sections.tsx:227 | web/src/routes/Sections.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/InviteDialog.stories.tsx (depth 2) | direct-story coverage |
| Shell | web/src/routes/Shell.tsx:123 |  | web/src/routes/Overview.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| SidebarLinkItem | web/src/routes/Shell.tsx:652 | web/src/routes/SidebarLinkItem.stories.tsx | web/src/routes/Overview.stories.tsx (depth 3) | direct-story coverage |
| SidebarSection | web/src/routes/Shell.tsx:694 |  | web/src/routes/Overview.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| InstanceContext | web/src/routes/Shell.tsx:721 |  | web/src/routes/Overview.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ProjectContext | web/src/routes/Shell.tsx:740 |  | web/src/routes/Overview.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| SidebarVersion | web/src/routes/Shell.tsx:841 | web/src/routes/SidebarVersion.stories.tsx | web/src/routes/Overview.stories.tsx (depth 2) | direct-story coverage |
| AccountEntry | web/src/routes/Shell.tsx:853 |  | web/src/routes/Overview.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ProfileUpdateBadge | web/src/routes/Shell.tsx:1018 | web/src/routes/ProfileUpdateBadge.stories.tsx | web/src/routes/Overview.stories.tsx (depth 3) | direct-story coverage |
| ThemeToggle | web/src/routes/Shell.tsx:1038 | web/src/routes/ThemeToggle.stories.tsx | web/src/routes/Overview.stories.tsx (depth 2) | direct-story coverage |
| SignupVerify | web/src/routes/SignupVerify.tsx:13 | web/src/routes/SignupVerify.stories.tsx |  | direct-story coverage |
| StepUpBanner | web/src/routes/StepUpBanner.tsx:31 | web/src/routes/StepUpBanner.stories.tsx | web/src/routes/Overview.stories.tsx (depth 2) | direct-story coverage |
| SystemScopeRefusal | web/src/routes/SystemScope.tsx:47 | web/src/routes/SystemScopeRefusal.stories.tsx |  | direct-story coverage |
| TemporaryAccess | web/src/routes/TemporaryAccess.tsx:126 | web/src/routes/TemporaryAccess.stories.tsx |  | direct-story coverage |
| ScopedTemporaryAccess | web/src/routes/TemporaryAccess.tsx:134 |  | web/src/routes/TemporaryAccess.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| AccessRequestRow | web/src/routes/TemporaryAccess.tsx:634 |  | web/src/routes/TemporaryAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| Values | web/src/routes/Values.tsx:87 | web/src/routes/Values.stories.tsx |  | direct-story coverage |
| ValuesSurface | web/src/routes/Values.tsx:109 |  | web/src/routes/Values.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| RowEditor | web/src/routes/Values.tsx:805 |  | web/src/routes/Values.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| WorkspaceApprove | web/src/routes/WorkspaceApprove.tsx:96 | web/src/routes/WorkspaceApprove.stories.tsx |  | direct-story coverage |
| StepUpReauth | web/src/routes/WorkspaceApprove.tsx:322 |  | web/src/routes/WorkspaceApprove.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| WorkspaceCallback | web/src/routes/WorkspaceCallback.tsx:26 | web/src/routes/WorkspaceCallback.stories.tsx |  | direct-story coverage |
| WorkspaceScope | web/src/routes/WorkspaceScope.tsx:40 | web/src/routes/WorkspaceScope.stories.tsx |  | direct-story coverage |
| WorkspaceBoundary | web/src/routes/WorkspaceScope.tsx:62 |  | web/src/routes/WorkspaceScope.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ConnectedWorkspace | web/src/routes/WorkspaceScope.tsx:144 |  | web/src/routes/WorkspaceScope.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| WorkspaceBanner | web/src/routes/WorkspaceScope.tsx:216 |  | web/src/routes/WorkspaceScope.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| Reconnect | web/src/routes/WorkspaceScope.tsx:243 | web/src/routes/Reconnect.stories.tsx | web/src/routes/WorkspaceScope.stories.tsx (depth 2) | direct-story coverage |
| WorkspaceSettingsLink | web/src/routes/WorkspaceSettingsLink.tsx:6 | web/src/routes/WorkspaceSettingsLink.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 1) | direct-story coverage |
| Terms | web/src/routes/accessRules/AccessGlossary.tsx:7 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AccessGlossary | web/src/routes/accessRules/AccessGlossary.tsx:26 | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| ExistingAccessCard | web/src/routes/accessRules/ExistingAccess.tsx:12 |  | web/src/routes/Members.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| KeyMoveConfirmDialog | web/src/routes/accessRules/KeyMoveConfirmDialog.tsx:38 | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/FolderCleanupDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | direct-story coverage |
| PermissionList | web/src/routes/accessRules/PermissionList.tsx:20 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PermRow | web/src/routes/accessRules/PermissionList.tsx:51 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RuleEditorDialog | web/src/routes/accessRules/RuleEditorDialog.tsx:48 | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| AxisBox | web/src/routes/accessRules/RuleEditorDialog.tsx:202 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AxisChip | web/src/routes/accessRules/RuleEditorDialog.tsx:214 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| AxisEditor | web/src/routes/accessRules/RuleEditorDialog.tsx:222 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| RulesPanel | web/src/routes/accessRules/RulesPanel.tsx:18 | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| WhoCan | web/src/routes/accessRules/WhoCan.tsx:29 | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | direct-story coverage |
| AnswerTable | web/src/routes/accessRules/WhoCan.tsx:172 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| PermBadge | web/src/routes/accessRules/parts.tsx:8 |  | web/src/routes/Members.stories.tsx (depth 2), web/src/routes/accessRules/AccessRules.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| Lock | web/src/routes/accessRules/parts.tsx:22 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| EnvList | web/src/routes/accessRules/parts.tsx:34 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| KeyLabel | web/src/routes/accessRules/parts.tsx:53 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| KeyList | web/src/routes/accessRules/parts.tsx:63 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| Except | web/src/routes/accessRules/parts.tsx:85 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| WherePart | web/src/routes/accessRules/parts.tsx:95 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) | composed coverage: specific branch still requires source/state review |
| RuleWhere | web/src/routes/accessRules/parts.tsx:109 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RulePerms | web/src/routes/accessRules/parts.tsx:149 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) | composed coverage: specific branch still requires source/state review |
| RuleSummary | web/src/routes/accessRules/parts.tsx:165 |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| CreateAccountDialog | web/src/routes/machineAccess/AccountDialogs.tsx:29 | web/src/routes/CreateAccountDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| DeleteAccountDialog | web/src/routes/machineAccess/AccountDialogs.tsx:154 | web/src/routes/DeleteAccountDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| ExpandableRow | web/src/routes/machineAccess/AccountRows.tsx:16 |  | web/src/routes/MachineAccess.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| ExpansionBody | web/src/routes/machineAccess/AccountRows.tsx:103 |  | web/src/routes/MachineAccess.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| JourneyActionButton | web/src/routes/machineAccess/AccountRows.tsx:304 |  | web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| ExpiryBadge | web/src/routes/machineAccess/Credentials.tsx:23 |  | web/src/routes/BindingCard.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| MintDialog | web/src/routes/machineAccess/Credentials.tsx:44 | web/src/routes/MintDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| LeaseMintDialog | web/src/routes/machineAccess/DynamicLeases.tsx:64 | web/src/routes/LeaseMintDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| LeaseActionDialog | web/src/routes/machineAccess/DynamicLeases.tsx:358 | web/src/routes/LeaseActionDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| CreateProviderDialog | web/src/routes/machineAccess/DynamicProviders.tsx:60 | web/src/routes/CreateProviderDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| SetCredentialDialog | web/src/routes/machineAccess/DynamicProviders.tsx:209 | web/src/routes/SetCredentialDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| RevokeCredentialDialog | web/src/routes/machineAccess/DynamicProviders.tsx:310 | web/src/routes/MachineRevokeCredentialDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| DeleteProviderDialog | web/src/routes/machineAccess/DynamicProviders.tsx:388 | web/src/routes/DeleteProviderDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| GrantDialog | web/src/routes/machineAccess/EnvironmentGrants.tsx:50 | web/src/routes/GrantDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| GrantBody | web/src/routes/machineAccess/EnvironmentGrants.tsx:147 |  | web/src/routes/GrantDialog.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) | composed coverage: specific branch still requires source/state review |
| BindingCard | web/src/routes/machineAccess/FederationBindings.tsx:33 | web/src/routes/BindingCard.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| BindingDialog | web/src/routes/machineAccess/FederationBindings.tsx:227 | web/src/routes/BindingDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | direct-story coverage |
| PolicyStrip | web/src/routes/machineAccess/MachineRevealPolicy.tsx:22 |  | web/src/routes/MachineAccess.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| MachineRevealDialog | web/src/routes/machineAccess/MachineRevealPolicy.tsx:98 | web/src/routes/MachineRevealDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 2) | direct-story coverage |
| Alert | web/src/ui/Alert.tsx:36 | web/src/ui/Alert.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 1), web/src/routes/CertificatesTab.stories.tsx (depth 1) | direct-story coverage |
| Badge | web/src/ui/Badge.tsx:20 | web/src/ui/Badge.stories.tsx | web/src/routes/CertificatesTab.stories.tsx (depth 1), web/src/routes/InstanceMailPanel.stories.tsx (depth 1) | direct-story coverage |
| Button | web/src/ui/Button.tsx:27 | web/src/ui/Alert.stories.tsx, web/src/ui/Button.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/Adapters.stories.tsx (depth 1), web/src/routes/CertificatesTab.stories.tsx (depth 1) | direct-story coverage |
| CeremonyNotice | web/src/ui/CeremonyNotice.tsx:4 | web/src/ui/CeremonyNotice.stories.tsx | web/src/routes/Ceremony.stories.tsx (depth 1), web/src/routes/ConnectionMintDialog.stories.tsx (depth 1) | direct-story coverage |
| Checkbox | web/src/ui/Checkbox.tsx:18 | web/src/ui/Checkbox.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/BindingDialog.stories.tsx (depth 1) | direct-story coverage |
| ChoiceGroup | web/src/ui/ChoiceGroup.tsx:26 | web/src/ui/ChoiceGroup.stories.tsx | web/src/routes/MatrixPublishSheet.stories.tsx (depth 1), web/src/routes/MatrixRowEditor.stories.tsx (depth 1) | direct-story coverage |
| Dialog | web/src/ui/Dialog.tsx:44 | web/src/ui/Dialog.stories.tsx | web/src/routes/CertificatesTab.stories.tsx (depth 1), web/src/routes/OAuth2ProvidersPanel.stories.tsx (depth 1) | direct-story coverage |
| Disclosure | web/src/ui/Disclosure.tsx:18 | web/src/ui/Disclosure.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | direct-story coverage |
| Field | web/src/ui/Field.tsx:46 | web/src/ui/Field.stories.tsx | web/src/routes/MatrixRowEditor.stories.tsx (depth 1), web/src/ui/Dialog.stories.tsx (depth 1) | direct-story coverage |
| Glyph | web/src/ui/Glyph.tsx:32 | web/src/ui/Glyph.stories.tsx | web/src/app/notifications.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 1) | direct-story coverage |
| Input | web/src/ui/Input.tsx:23 | web/src/ui/Dialog.stories.tsx, web/src/ui/Input.stories.tsx | web/src/routes/InstanceMailPanel.stories.tsx (depth 1), web/src/routes/PkiProfilesPanel.stories.tsx (depth 1) | direct-story coverage |
| Menu | web/src/ui/Menu.tsx:29 | web/src/ui/Menu.stories.tsx |  | direct-story coverage |
| MenuItem | web/src/ui/Menu.tsx:162 | web/src/ui/Menu.stories.tsx |  | direct-story coverage |
| Radio | web/src/ui/Radio.tsx:17 | web/src/ui/ChoiceGroup.stories.tsx, web/src/ui/Radio.stories.tsx | web/src/routes/MintConnectionForm.stories.tsx (depth 1), web/src/routes/CertificatesTab.stories.tsx (depth 2) | direct-story coverage |
| Select | web/src/ui/Select.tsx:15 | web/src/ui/Dialog.stories.tsx, web/src/ui/Select.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/InviteDialog.stories.tsx (depth 2) | direct-story coverage |
| Tabs | web/src/ui/Tabs.tsx:16 |  | web/src/routes/MachineAccess.stories.tsx (depth 1), web/src/ui/Tabs.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| TabPanel | web/src/ui/Tabs.tsx:80 |  | web/src/routes/MachineAccess.stories.tsx (depth 1), web/src/ui/Tabs.stories.tsx (depth 1) | composed coverage: specific branch still requires source/state review |
| Textarea | web/src/ui/Textarea.tsx:16 | web/src/ui/Textarea.stories.tsx | web/src/routes/CertificatesTab.stories.tsx (depth 1), web/src/routes/PkiIssuersPanel.stories.tsx (depth 1) | direct-story coverage |
| ThemeIcon | web/src/ui/ThemeIcon.tsx:13 | web/src/ui/Button.stories.tsx, web/src/ui/ThemeIcon.stories.tsx | web/src/routes/ThemeToggle.stories.tsx (depth 1), web/src/routes/Overview.stories.tsx (depth 3) | direct-story coverage |
| ToggleChip | web/src/ui/ToggleChip.tsx:38 | web/src/ui/ToggleChip.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) | direct-story coverage |
| AuthenticatorCodeField | web/src/ui/auth/AuthenticatorCodeField.tsx:12 | web/src/ui/auth/AuthenticatorCodeField.stories.tsx | web/src/ui/auth/SecondFactorChallenge.stories.tsx (depth 1), web/src/ui/auth/SecondFactorSetup.stories.tsx (depth 1) | direct-story coverage |
| LocalSignupForm | web/src/ui/auth/LocalSignupForm.tsx:10 | web/src/ui/auth/LocalSignupForm.stories.tsx | web/src/ui/auth/LoginForm.stories.tsx (depth 1), web/src/routes/Login.stories.tsx (depth 2) | direct-story coverage |
| LoginForm | web/src/ui/auth/LoginForm.tsx:94 | web/src/ui/auth/LoginForm.stories.tsx | web/src/routes/Login.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) | direct-story coverage |
| ProofDialog | web/src/ui/auth/ProofDialog.tsx:28 | web/src/ui/auth/ProofDialog.stories.tsx | web/src/routes/InstanceMailPanel.stories.tsx (depth 1), web/src/routes/OpenRegistration.stories.tsx (depth 2) | direct-story coverage |
| ProviderButton | web/src/ui/auth/ProviderButton.tsx:84 | web/src/ui/auth/ProviderButton.stories.tsx | web/src/ui/auth/LoginForm.stories.tsx (depth 1), web/src/routes/Login.stories.tsx (depth 2) | direct-story coverage |
| QrCode | web/src/ui/auth/QrCode.tsx:15 | web/src/ui/auth/QrCode.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/ui/auth/SecondFactorSetup.stories.tsx (depth 1) | direct-story coverage |
| SecondFactorChallenge | web/src/ui/auth/SecondFactorChallenge.tsx:18 | web/src/ui/auth/SecondFactorChallenge.stories.tsx | web/src/routes/Login.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) | direct-story coverage |
| SecondFactorSetup | web/src/ui/auth/SecondFactorSetup.tsx:37 | web/src/ui/auth/SecondFactorSetup.stories.tsx | web/src/routes/EnrolmentGate.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) | direct-story coverage |

The Overview journey uses the actual AppRoutes tree and AuthProvider, validates create request/response contracts, submits POST with the required 201 response, observes query refresh in Projects, returns through the real sidebar, and revisits the updated list. Four named steps execute in Canvas; framed Docs disables autoplay for manual exploration. Real lazy workspace-module readiness is awaited before mount to keep cold transform contention independent from API/state assertions. Deferred body abort/reset regression prevents a retired handler from mutating a replacement story.

Installed Storybook 10.6 non-inline Docs IFrameStory ignores autoplay=false and uses viewMode=story. Overview CreateProjectJourney checks isManualDocsFrame before any named step so its Docs frame stays ready for manual exploration. Canvas and test frames still run the complete workflow. The narrow helper safely catches inaccessible parent windows/documents and has five pure window-fake regressions.

The public Docs context owns toolbar globals. HikyoDocsContainer subscribes to GLOBALS_UPDATED and publishes the current theme attribute. StoryTheme synchronizes each independent frame from the containing same-origin Docs document using a MutationObserver, without changing iframe URLs or React keys. Existing form state and providers survive light/dark changes; observers, channel subscriptions and prior attributes are restored on cleanup. Docs blocks remain lazily imported.

The built-in backgrounds addon applied an important sb-show-main surface that overrode Hikyo app tokens after a toolbar theme change. Preview disables that competing backgrounds feature so application --bg remains the single surface owner; the centralized browser checker compares computed body background against the resolved app token in both palettes. Current browser proof is reported separately.

The existing Storybook Vitest project explicitly prebundles storybook/internal/core-events. The first cold theme run discovered this newly used preview dependency during setup and reloaded active test modules. Preparing the dependency before suite startup removes that reload race without changing project coverage, test assertions, browser matrix or timeouts.

Live form preservation required the public Docs renderer extension point in addition to theme attributes. Installed Storybook 10.6 assigns a new root ErrorBoundary key on a Docs globals render, replacing independent frames and clearing manual drafts. The facade delegates to the exported original DocsRenderer and deduplicates only the identical context, parameters object and mount element. New/HMR context, changed parameters, separate mounts, explicit unmount and rejected-render retries delegate normally. Completion is shared while pending, and a failed older render cannot evict its newer replacement. Four owner regressions cover frame/input retention, lifecycle delegation and asynchronous failures; real browser form/theme proof remains separate.

Only explicitly registered behavior cases use interactionOnce in the light test project. Appearance callbacks preserve visible/readiness assertions and the same final view; static palette assertions remain full plays in both projects. The existing Docs guard also validates the complete interaction accounting against source-bound literal helper calls/static tags, retained story references and test eligibility. Generated Docs may inherit the first story tag and are not interaction cases. No blanket theme/play or accessibility opt-out is introduced.

| Coverage | Dark and manual Canvas | Light validation project |
| --- | --- | --- |
| Story render, fixtures and accessibility | 642 | 642 |
| Full play functions | 516 | 511 |
| Explicit appearance callbacks | not used | 5 |
| Render-only stories without play | 126 | 126 |
| Generated Docs | 131 | 131 |

Generated Docs is the shared built-index gate, not a claim that every Docs page runs in the browser projects. Actual manual Docs and controls proof is reported separately.

| Registered behavior case | Source/export | Retained appearance references | Reason |
| --- | --- | --- | --- |
| ui-auth-providerbutton--fires-on-click | ./src/ui/auth/ProviderButton.stories.tsx#FiresOnClick | ui-auth-providerbutton--google | The callback spy leaves the rendered provider button unchanged. |
| routes-providerdiscoveryalert--default | ./src/routes/ProviderDiscoveryAlert.stories.tsx#Default | routes-providerdiscoveryalert--default | The retry callback spy leaves the rendered error alert unchanged. |
| ui-dialog--backdrop-click | ./src/ui/Dialog.stories.tsx#BackdropClick | ui-dialog--backdrop-click | Synthetic backdrop events verify dismissal callbacks without closing the controlled dialog. |
| ui-auth-loginform--provider-starts-from-step-one | ./src/ui/auth/LoginForm.stories.tsx#ProviderStartsFromStepOne | ui-auth-loginform--provider-starts-from-step-one | The provider callback spy leaves the initial sign-in view unchanged. |
| pages-overview--create-project-journey | ./src/routes/Overview.stories.tsx#CreateProjectJourney | pages-overview--create-project-journey, pages-overview--empty, pages-overview--populated, routes-projects--empty, routes-projects--populated | Route creation/cache assertions are theme-independent. The appearance pass seeds the same project before mounting, then waits for real navigation to the same populated Projects route with AppRoutes, providers and Shell. Standalone endpoint stories remain in both palettes. |

Source binding checks named shared-helper imports, literal IDs, static tags and one direct play call without later spread/computed overrides. Generated Docs may inherit the first export tag and do not count as interaction cases. Appearance equivalence, readiness and actual project execution require the documented source audit and browser checks.

The 25 proposed static assertion opt-ins were rejected after source review: toBeVisible/getByRole can catch palette-specific hidden content that accessibility alone misses. They remain full plays in both palettes. Only the five behavior cases above opt in, with explicit visible/readiness assertions. Matched same-source timing, full/reference project execution and deliberately broken routing proof belong to the parent policy handoff.

## Verified Rewrite lessons

- PR 685 colocated stories and component styles, rendered real routes/providers, isolated Docs fixtures in frames, and checked CSS-rule multisets and matching screenshots. Those are source-backed techniques, not permission to copy its counts or broad migration.
- PR 687 final policy retained native text/structure/headings/lists/tables/forms and meaningful shared controls, accessibility, security and graphics. It removed ten pass-through modules after preserving every native tag/prop/class/ref/handler. Its editor pruning reduced total lazy output without changing initial app download; app and Storybook assets were counted separately.
- PR 720 usefulness cleanup consolidated compatible variants, moved behavior-only cases into equivalent tests, preserved meaningful final states, and retained 136 Docs modules. The recorded 1,021 to 876 cleanup was followed by a real-route journey bringing that historical catalogue to 877. The journey used named steps and Docs autoplay false.
- The inspected Rewrite Storybook configurations did not configure custom storySort. Any explicit order in Hikyo is a new local policy and requires its own evidence.

Evidence files: /Users/developwent/.t3/worktrees/rewrite/t3code-7743d2bf/docs/handoffs/pr-685.md, pr-687.md, pr-720.md and storybook-usefulness-cleanup.md; related ADR 0099. These files are historical evidence. No remote write or infrastructure action is authorized.

## Baseline gaps and bounded disposition

| Responsibility | Source | Evidence gap | Bounded action |
| --- | --- | --- | --- |
| App route tree and Shell chrome | web/src/app/App.tsx; web/src/routes/Shell.tsx | No full route-tree story; isolated sidebar/identity controls did not render shell state or route transitions. | Use exported real AppRoutes under withApp MemoryRouter. Preserve production BrowserRouter, auth gates, route registry and session policy. |
| Overview | web/src/routes/Placeholder.tsx#Overview | Placeholder module only stories NotFound; Overview is a different component, not a prop variant. | Add Pages/Overview real route examples and project creation/revisit journey. |
| Remotes and Adapters routes | web/src/routes/Remotes.tsx#Remotes; web/src/routes/Adapters.tsx#AdaptersPage | Existing RemoteCard/ConnectionRow/adapter dialog modules do not cover full page or data states. | Add page modules using retained real page exports/gates and contract-valid fixtures. |
| Signup verification and SAML completion | web/src/routes/SignupVerify.tsx; web/src/routes/SAMLDone.tsx | No stories; success navigation is human/integration-only, but missing-token/refusal and form validation are storyable. | Story safe refusal/form states without invoking real browser authority, passkeys, popups or remote login. |
| New identity/admin panels | PkiIssuersPanel; PkiProfilesPanel; OAuth2ProvidersPanel; InstanceMailPanel; OpenRegistrationPanel | Only composition relationships from other modules; their meaningful data/editor/refusal states lacked focused modules. | Add owner-adjacent module and Docs for supported pending, empty, populated, failed, permission and overlay states. |
| Certificate panels | CertificatesTab; SSHCertificatesPanel | MachineAccess default views do not prove every tab and dialog branch. | Add focused panels using real providers, contracts and non-secret illustrative data. |
| Shared missing composites | CeremonyNotice; LocalSignupForm; AwsAccessFields; WorkspaceSettingsLink | Composition exists but independently owned semantics/interaction lacked a catalogue module. | Add meaningful captions/controls plus supported busy/refusal/mode/link states. |
| Docs descriptions/props | before-docgen.json | 113 primary components extracted; 37 have empty generated component descriptions. Two modules have no primary component. Button union icon prop is omitted by docgen. | Add explicit source-derived docs descriptions and owned prop controls. Native inherited prop filtering is separate from omitted owned props. |
| Titles | before-sidebar.txt | 114 module titles depend on filesystem auto-title; only Members/Access rules is explicit. ui/routes/app source folders are sidebar groups rather than product responsibility. | Explicit Design system/Shared/feature/Pages titles while retaining each existing module id. New modules acquire deliberate IDs. |
| Fixture/Docs frame coupling | web/.storybook/withApp.tsx; topLayerDocs.ts | Inline Docs examples can replace fetch; loading promises and resources need owner cleanup. | Frame all app-backed Docs examples and top-layer overlays; abort/retire previous fixtures and query clients before replacement. |
| Redundant behavior entry | web/src/ui/Button.stories.tsx#Clicks | Same final button view as Secondary; only callback behavior differs. | Move click/type/signature checks into owning regression, retain Secondary and all design-linked variants. |
| Compatible Badge variants | web/src/ui/Badge.stories.tsx | Neutral/Danger/Changed/Ok/Mono differ by compatible props; AllTones already gives a gallery. | Caption gallery and preserve one controls-driven example after assertions and ID/export accounting. |

## Justified exclusions and retained states

- AuthProvider, transport/workspace providers, main entry and testkit renderer are infrastructure, not independently designed visual component modules. Their visible consumers and lifecycle are covered by real-provider stories and owning tests. App/Shell visual state is covered through the real route tree, rather than dismissed as invisible.
- Real WebAuthn prompts, passwordless authenticator ceremonies and external/popup identity-provider success remain human/integration flows. Safe refused/expired/incomplete/second-factor UI stays in the catalogue.
- Display-once secret retention, sensitivity inventory and passkey/popup authority are not changed to make stories easier. Illustrative fixtures stay within published response contracts. OS/browser clipboard permission remains a separate manual boundary.
- Repeated simple 404/refusal answers that render an already retained Alert remain historical justified exclusions only when source comparison proves identical view/controls. Instance pages with distinct 404 copy/actions stay retained.
- RuntimeMaintenanceBoundary ready seed is not a meaningful observable fixture transition; retained interruption/recovery/reconnecting examples cover its distinct designed answers.
- Loading, empty, failure/retry, busy/disabled, permission/refusal, selected/expanded, validation, keyboard/focus, phone, long-content and overlay entries are retained where supported. Menu browser contracts remain because happy-dom cannot prove native popover focus and dismissal equivalence.

## Every baseline module and generated Docs

| Title | Story owner | Primary component | Exports | Matched Docs | Disposition |
| --- | --- | --- | --- | --- | --- |
| app/RuntimeMaintenanceBoundary | web/src/app/RuntimeMaintenanceBoundary.stories.tsx | web/src/app/RuntimeMaintenanceBoundary.tsx#RuntimeMaintenanceBoundary | 3 | app-runtimemaintenanceboundary--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| app/notifications | web/src/app/notifications.stories.tsx | web/src/app/notifications.tsx#ToastViewport | 4 | app-notifications--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/AccountProfile | web/src/routes/AccountProfile.stories.tsx | web/src/routes/AccountProfile.tsx#AccountProfile | 6 | routes-accountprofile--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/AccountSecurity | web/src/routes/AccountSecurity.stories.tsx | web/src/routes/AccountSecurity.tsx#AccountSecurity | 5 | routes-accountsecurity--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Audit | web/src/routes/Audit.stories.tsx | web/src/routes/Audit.tsx#Audit | 4 | routes-audit--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/BindingCard | web/src/routes/BindingCard.stories.tsx | web/src/routes/machineAccess/FederationBindings.tsx#BindingCard | 4 | routes-bindingcard--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/BindingDialog | web/src/routes/BindingDialog.stories.tsx | web/src/routes/machineAccess/FederationBindings.tsx#BindingDialog | 7 | routes-bindingdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CLIReauth | web/src/routes/CLIReauth.stories.tsx | web/src/routes/CLIReauth.tsx#CLIReauth | 7 | routes-clireauth--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CatalogueManageDialog | web/src/routes/CatalogueManageDialog.stories.tsx | web/src/routes/CatalogueManageDialog.tsx#CatalogueManageDialog | 6 | routes-cataloguemanagedialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Ceremony | web/src/routes/Ceremony.stories.tsx | web/src/routes/Ceremony.tsx#Ceremony | 4 | routes-ceremony--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ChangeApprovals | web/src/routes/ChangeApprovals.stories.tsx | web/src/routes/ChangeApprovals.tsx#ChangeApprovals | 4 | routes-changeapprovals--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ChromeIdentityControls | web/src/routes/ChromeIdentityControls.stories.tsx | web/src/routes/ChromeIdentityControls.tsx#ChromeIdentityControls | 2 | routes-chromeidentitycontrols--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CompactOrgRetention | web/src/routes/CompactOrgRetention.stories.tsx | web/src/routes/OrgSettings.tsx#CompactOrgRetention | 3 | routes-compactorgretention--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ConnectionMintDialog | web/src/routes/ConnectionMintDialog.stories.tsx | web/src/routes/Remotes.tsx#ConnectionMintDialog | 2 | routes-connectionmintdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ConnectionRow | web/src/routes/ConnectionRow.stories.tsx | web/src/routes/Remotes.tsx#ConnectionRow | 4 | routes-connectionrow--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CreateAccountDialog | web/src/routes/CreateAccountDialog.stories.tsx | web/src/routes/machineAccess/AccountDialogs.tsx#CreateAccountDialog | 4 | routes-createaccountdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CreateProviderDialog | web/src/routes/CreateProviderDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#CreateProviderDialog | 4 | routes-createproviderdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/CredentialForm | web/src/routes/CredentialForm.stories.tsx | web/src/routes/Adapters.tsx#CredentialForm | 3 | routes-credentialform--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/DefinitionsBundlePanel | web/src/routes/DefinitionsBundlePanel.stories.tsx | web/src/routes/DefinitionsBundlePanel.tsx#DefinitionsBundlePanel | 7 | routes-definitionsbundlepanel--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/DeleteAccountDialog | web/src/routes/DeleteAccountDialog.stories.tsx | web/src/routes/machineAccess/AccountDialogs.tsx#DeleteAccountDialog | 5 | routes-deleteaccountdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/DeleteAdapterDialog | web/src/routes/DeleteAdapterDialog.stories.tsx | web/src/routes/Adapters.tsx#DeleteAdapterDialog | 3 | routes-deleteadapterdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/DeleteProviderDialog | web/src/routes/DeleteProviderDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#DeleteProviderDialog | 6 | routes-deleteproviderdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/DeliveryTargets | web/src/routes/DeliveryTargets.stories.tsx | web/src/routes/DeliveryTargets.tsx#DeliveryTargetsPanel | 9 | routes-deliverytargets--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/EnrolmentGate | web/src/routes/EnrolmentGate.stories.tsx | web/src/routes/EnrolmentGate.tsx#EnrolmentGate | 2 | routes-enrolmentgate--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/EstablishCredential | web/src/routes/EstablishCredential.stories.tsx | web/src/routes/EstablishCredential.tsx#EstablishCredential | 6 | routes-establishcredential--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/FederationIssuersPanel | web/src/routes/FederationIssuersPanel.stories.tsx | web/src/routes/FederationIssuersPanel.tsx#FederationIssuersPanel | 8 | routes-federationissuerspanel--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/FleetUpdateNotice | web/src/routes/FleetUpdateNotice.stories.tsx | render-only fixture/composite | 4 | routes-fleetupdatenotice--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/FolderCleanupDialog | web/src/routes/FolderCleanupDialog.stories.tsx | web/src/routes/FolderCleanupDialog.tsx#FolderCleanupDialog | 3 | routes-foldercleanupdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/GrantDialog | web/src/routes/GrantDialog.stories.tsx | web/src/routes/machineAccess/EnvironmentGrants.tsx#GrantDialog | 9 | routes-grantdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/HealthChip | web/src/routes/HealthChip.stories.tsx | web/src/routes/Adapters.tsx#HealthChip | 8 | routes-healthchip--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/HistoryDrawer | web/src/routes/HistoryDrawer.stories.tsx | web/src/routes/HistoryDrawer.tsx#HistoryDrawer | 9 | routes-historydrawer--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ImportWizard | web/src/routes/ImportWizard.stories.tsx | web/src/routes/ImportWizard.tsx#ImportWizard | 11 | routes-importwizard--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/InstanceAdmin | web/src/routes/InstanceAdmin.stories.tsx | web/src/routes/InstanceAdmin.tsx#InstanceAdmin | 7 | routes-instanceadmin--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/InstanceConfig | web/src/routes/InstanceConfig.stories.tsx | web/src/routes/InstanceConfig.tsx#InstanceConfig | 9 | routes-instanceconfig--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/InviteDialog | web/src/routes/InviteDialog.stories.tsx | web/src/routes/InviteDialog.tsx#InviteDialog | 2 | routes-invitedialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/KeyDeclarationDetail | web/src/routes/KeyDeclarationDetail.stories.tsx | web/src/routes/KeyDeclarationDetail.tsx#KeyDeclarationDetail | 8 | routes-keydeclarationdetail--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/LastApplyProvenance | web/src/routes/LastApplyProvenance.stories.tsx | web/src/routes/DefinitionsBundlePanel.tsx#LastApplyProvenance | 2 | routes-lastapplyprovenance--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/LeaseActionDialog | web/src/routes/LeaseActionDialog.stories.tsx | web/src/routes/machineAccess/DynamicLeases.tsx#LeaseActionDialog | 6 | routes-leaseactiondialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/LeaseMintDialog | web/src/routes/LeaseMintDialog.stories.tsx | web/src/routes/machineAccess/DynamicLeases.tsx#LeaseMintDialog | 7 | routes-leasemintdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Login | web/src/routes/Login.stories.tsx | web/src/routes/Login.tsx#Login | 4 | routes-login--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MachineAccess | web/src/routes/MachineAccess.stories.tsx | web/src/routes/MachineAccess.tsx#MachineAccessPage | 11 | routes-machineaccess--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MachineRevealDialog | web/src/routes/MachineRevealDialog.stories.tsx | web/src/routes/machineAccess/MachineRevealPolicy.tsx#MachineRevealDialog | 4 | routes-machinerevealdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MachineRevokeCredentialDialog | web/src/routes/MachineRevokeCredentialDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#RevokeCredentialDialog | 3 | routes-machinerevokecredentialdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Matrix | web/src/routes/Matrix.stories.tsx | web/src/routes/Matrix.tsx#Matrix | 8 | routes-matrix--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MatrixKeyCreate | web/src/routes/MatrixKeyCreate.stories.tsx | web/src/routes/MatrixKeyCreate.tsx#MatrixKeyCreate | 1 | routes-matrixkeycreate--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MatrixPublishSheet | web/src/routes/MatrixPublishSheet.stories.tsx | web/src/routes/MatrixPublishSheet.tsx#MatrixPublishSheet | 3 | routes-matrixpublishsheet--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MatrixRowEditor | web/src/routes/MatrixRowEditor.stories.tsx | web/src/routes/MatrixRowEditor.tsx#MatrixRowEditor | 5 | routes-matrixroweditor--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Members | web/src/routes/Members.stories.tsx | web/src/routes/Members.tsx#Members | 4 | routes-members--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MintConnectionForm | web/src/routes/MintConnectionForm.stories.tsx | web/src/routes/Remotes.tsx#MintConnectionForm | 2 | routes-mintconnectionform--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/MintDialog | web/src/routes/MintDialog.stories.tsx | web/src/routes/machineAccess/Credentials.tsx#MintDialog | 7 | routes-mintdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/OIDCDone | web/src/routes/OIDCDone.stories.tsx | web/src/routes/OIDCDone.tsx#OIDCDone | 4 | routes-oidcdone--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/OidcProvidersPanel | web/src/routes/OidcProvidersPanel.stories.tsx | web/src/routes/OidcProvidersPanel.tsx#OidcProvidersPanel | 7 | routes-oidcproviderspanel--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/OpsDiagnosticBanners | web/src/routes/OpsDiagnosticBanners.stories.tsx | web/src/routes/OpsDiagnosticBanners.tsx#ChromeDiagnostic | 4 | routes-opsdiagnosticbanners--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/OrgSettings | web/src/routes/OrgSettings.stories.tsx | web/src/routes/OrgSettings.tsx#OrgSettings | 4 | routes-orgsettings--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/PinReleaseOutcome | web/src/routes/PinReleaseOutcome.stories.tsx | web/src/routes/HistoryDrawer.tsx#PinReleaseOutcome | 3 | routes-pinreleaseoutcome--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Placeholder | web/src/routes/Placeholder.stories.tsx | web/src/routes/Placeholder.tsx#NotFound | 1 | routes-placeholder--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ProfileUpdateBadge | web/src/routes/ProfileUpdateBadge.stories.tsx | web/src/routes/Shell.tsx#ProfileUpdateBadge | 2 | routes-profileupdatebadge--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ProjectSettings | web/src/routes/ProjectSettings.stories.tsx | web/src/routes/ProjectSettings.tsx#ProjectSettings | 4 | routes-projectsettings--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Projects | web/src/routes/Projects.stories.tsx | web/src/routes/Projects.tsx#Projects | 3 | routes-projects--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ProviderDiscoveryAlert | web/src/routes/ProviderDiscoveryAlert.stories.tsx | web/src/routes/ProviderDiscoveryAlert.tsx#ProviderDiscoveryAlert | 1 | routes-providerdiscoveryalert--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Reconnect | web/src/routes/Reconnect.stories.tsx | web/src/routes/WorkspaceScope.tsx#Reconnect | 3 | routes-reconnect--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/RemoteCard | web/src/routes/RemoteCard.stories.tsx | web/src/routes/Remotes.tsx#RemoteCard | 4 | routes-remotecard--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/RetentionBoundsFields | web/src/routes/RetentionBoundsFields.stories.tsx | web/src/routes/RetentionBoundsFields.tsx#RetentionBoundsFields | 3 | routes-retentionboundsfields--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/RevisionDiff | web/src/routes/RevisionDiff.stories.tsx | web/src/routes/RevisionDiff.tsx#RevisionDiffDialog | 5 | routes-revisiondiff--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/RevokeConnectionDialog | web/src/routes/RevokeConnectionDialog.stories.tsx | web/src/routes/Remotes.tsx#RevokeConnectionDialog | 1 | routes-revokeconnectiondialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/RevokeCredentialDialog | web/src/routes/RevokeCredentialDialog.stories.tsx | web/src/routes/Adapters.tsx#RevokeCredentialDialog | 3 | routes-revokecredentialdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SamlProvidersPanel | web/src/routes/SamlProvidersPanel.stories.tsx | web/src/routes/SamlProvidersPanel.tsx#SamlProvidersPanel | 7 | routes-samlproviderspanel--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SamlSpKeysPanel | web/src/routes/SamlSpKeysPanel.stories.tsx | web/src/routes/SamlSpKeysPanel.tsx#SamlSpKeysPanel | 6 | routes-samlspkeyspanel--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ScanBlockDialog | web/src/routes/ScanBlockDialog.stories.tsx | web/src/routes/ScanBlockDialog.tsx#ScanBlockDialog | 2 | routes-scanblockdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ScanWarnDialog | web/src/routes/ScanWarnDialog.stories.tsx | web/src/routes/ScanWarnDialog.tsx#ScanWarnDialog | 1 | routes-scanwarndialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ScimProvisioning | web/src/routes/ScimProvisioning.stories.tsx | web/src/routes/ScimProvisioning.tsx#ScimProvisioningPage | 4 | routes-scimprovisioning--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Sections | web/src/routes/Sections.stories.tsx | web/src/routes/Sections.tsx#Panel | 9 | routes-sections--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SetCredentialDialog | web/src/routes/SetCredentialDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#SetCredentialDialog | 5 | routes-setcredentialdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SidebarLinkItem | web/src/routes/SidebarLinkItem.stories.tsx | web/src/routes/Shell.tsx#SidebarLinkItem | 4 | routes-sidebarlinkitem--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SidebarVersion | web/src/routes/SidebarVersion.stories.tsx | web/src/routes/Shell.tsx#SidebarVersion | 2 | routes-sidebarversion--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/StepUpBanner | web/src/routes/StepUpBanner.stories.tsx | web/src/routes/StepUpBanner.tsx#StepUpBanner | 2 | routes-stepupbanner--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SystemProjectNotice | web/src/routes/SystemProjectNotice.stories.tsx | web/src/routes/InstanceConfig.tsx#SystemProjectNotice | 1 | routes-systemprojectnotice--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/SystemScopeRefusal | web/src/routes/SystemScopeRefusal.stories.tsx | web/src/routes/SystemScope.tsx#SystemScopeRefusal | 3 | routes-systemscoperefusal--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/TargetForm | web/src/routes/TargetForm.stories.tsx | web/src/routes/Adapters.tsx#TargetForm | 5 | routes-targetform--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/TemporaryAccess | web/src/routes/TemporaryAccess.stories.tsx | web/src/routes/TemporaryAccess.tsx#TemporaryAccess | 3 | routes-temporaryaccess--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/ThemeToggle | web/src/routes/ThemeToggle.stories.tsx | web/src/routes/Shell.tsx#ThemeToggle | 2 | routes-themetoggle--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/UpdateJobStatus | web/src/routes/UpdateJobStatus.stories.tsx | web/src/routes/Remotes.tsx#UpdateJobStatus | 4 | routes-updatejobstatus--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/Values | web/src/routes/Values.stories.tsx | web/src/routes/Values.tsx#Values | 7 | routes-values--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/WorkspaceApprove | web/src/routes/WorkspaceApprove.stories.tsx | web/src/routes/WorkspaceApprove.tsx#WorkspaceApprove | 6 | routes-workspaceapprove--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/WorkspaceCallback | web/src/routes/WorkspaceCallback.stories.tsx | web/src/routes/WorkspaceCallback.tsx#WorkspaceCallback | 2 | routes-workspacecallback--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/WorkspaceScope | web/src/routes/WorkspaceScope.stories.tsx | web/src/routes/WorkspaceScope.tsx#WorkspaceScope | 6 | routes-workspacescope--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| routes/WorkspaceStepUp | web/src/routes/WorkspaceStepUp.stories.tsx | web/src/routes/WorkspaceStepUp.stories.tsx#StepUp | 4 | routes-workspacestepup--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| Members/Access rules | web/src/routes/accessRules/AccessRules.stories.tsx | render-only fixture/composite | 11 | members-access-rules--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Alert | web/src/ui/Alert.stories.tsx | web/src/ui/Alert.tsx#Alert | 7 | ui-alert--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Badge | web/src/ui/Badge.stories.tsx | web/src/ui/Badge.tsx#Badge | 6 | ui-badge--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Button | web/src/ui/Button.stories.tsx | web/src/ui/Button.tsx#Button | 8 | ui-button--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Checkbox | web/src/ui/Checkbox.stories.tsx | web/src/ui/Checkbox.tsx#Checkbox | 7 | ui-checkbox--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/ChoiceGroup | web/src/ui/ChoiceGroup.stories.tsx | web/src/ui/ChoiceGroup.tsx#ChoiceGroup | 13 | ui-choicegroup--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Dialog | web/src/ui/Dialog.stories.tsx | web/src/ui/Dialog.tsx#Dialog | 9 | ui-dialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Disclosure | web/src/ui/Disclosure.stories.tsx | web/src/ui/Disclosure.tsx#Disclosure | 3 | ui-disclosure--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Field | web/src/ui/Field.stories.tsx | web/src/ui/Field.tsx#Field | 3 | ui-field--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Glyph | web/src/ui/Glyph.stories.tsx | web/src/ui/Glyph.tsx#Glyph | 4 | ui-glyph--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Input | web/src/ui/Input.stories.tsx | web/src/ui/Input.tsx#Input | 13 | ui-input--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Menu | web/src/ui/Menu.stories.tsx | web/src/ui/Menu.tsx#Menu | 7 | ui-menu--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Radio | web/src/ui/Radio.stories.tsx | web/src/ui/Radio.tsx#Radio | 5 | ui-radio--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Select | web/src/ui/Select.stories.tsx | web/src/ui/Select.tsx#Select | 6 | ui-select--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Tabs | web/src/ui/Tabs.stories.tsx | web/src/ui/Tabs.stories.tsx#Demo | 4 | ui-tabs--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Textarea | web/src/ui/Textarea.stories.tsx | web/src/ui/Textarea.tsx#Textarea | 7 | ui-textarea--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/ThemeIcon | web/src/ui/ThemeIcon.stories.tsx | web/src/ui/ThemeIcon.tsx#ThemeIcon | 2 | ui-themeicon--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/ToggleChip | web/src/ui/ToggleChip.stories.tsx | web/src/ui/ToggleChip.tsx#ToggleChip | 6 | ui-togglechip--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Tokens | web/src/ui/Tokens.stories.tsx | web/src/ui/Tokens.stories.tsx#TokensPage | 2 | ui-tokens--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/Typography | web/src/ui/Typography.stories.tsx | web/src/ui/Typography.stories.tsx#Specimen | 1 | ui-typography--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/AuthenticatorCodeField | web/src/ui/auth/AuthenticatorCodeField.stories.tsx | web/src/ui/auth/AuthenticatorCodeField.tsx#AuthenticatorCodeField | 3 | ui-auth-authenticatorcodefield--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/LoginFlow | web/src/ui/auth/LoginFlow.stories.tsx | web/src/ui/auth/LoginFlow.stories.tsx#LoginFlow | 7 | ui-auth-loginflow--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/LoginForm | web/src/ui/auth/LoginForm.stories.tsx | web/src/ui/auth/LoginForm.tsx#LoginForm | 20 | ui-auth-loginform--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/ProofDialog | web/src/ui/auth/ProofDialog.stories.tsx | web/src/ui/auth/ProofDialog.tsx#ProofDialog | 3 | ui-auth-proofdialog--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/ProviderButton | web/src/ui/auth/ProviderButton.stories.tsx | web/src/ui/auth/ProviderButton.tsx#ProviderButton | 11 | ui-auth-providerbutton--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/QrCode | web/src/ui/auth/QrCode.stories.tsx | web/src/ui/auth/QrCode.tsx#QrCode | 2 | ui-auth-qrcode--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/SecondFactorChallenge | web/src/ui/auth/SecondFactorChallenge.stories.tsx | web/src/ui/auth/SecondFactorChallenge.tsx#SecondFactorChallenge | 9 | ui-auth-secondfactorchallenge--docs | Retain module and generated Docs; normalize explicit title with stable module id. |
| ui/auth/SecondFactorSetup | web/src/ui/auth/SecondFactorSetup.stories.tsx | web/src/ui/auth/SecondFactorSetup.tsx#SecondFactorSetup | 9 | ui-auth-secondfactorsetup--docs | Retain module and generated Docs; normalize explicit title with stable module id. |

## Every baseline entry usefulness disposition

| Title / export | Source line | ID | Meaningful state hints | Disposition |
| --- | --- | --- | --- | --- |
| app/RuntimeMaintenanceBoundary / Maintenance | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:35 | app-runtimemaintenanceboundary--maintenance | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/RuntimeMaintenanceBoundary / RecoveryRequired | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:47 | app-runtimemaintenanceboundary--recovery-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/RuntimeMaintenanceBoundary / Reconnecting | web/src/app/RuntimeMaintenanceBoundary.stories.tsx:61 | app-runtimemaintenanceboundary--reconnecting | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/notifications / Failure | web/src/app/notifications.stories.tsx:41 | app-notifications--failure | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/notifications / Success | web/src/app/notifications.stories.tsx:42 | app-notifications--success | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/notifications / Info | web/src/app/notifications.stories.tsx:43 | app-notifications--info | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| app/notifications / Dismisses | web/src/app/notifications.stories.tsx:45 | app-notifications--dismisses | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / Editable | web/src/routes/AccountProfile.stories.tsx:37 | routes-accountprofile--editable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / ProofRequired | web/src/routes/AccountProfile.stories.tsx:45 | routes-accountprofile--proof-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / Managed | web/src/routes/AccountProfile.stories.tsx:56 | routes-accountprofile--managed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / ProviderSignIn | web/src/routes/AccountProfile.stories.tsx:68 | routes-accountprofile--provider-sign-in | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / Loading | web/src/routes/AccountProfile.stories.tsx:80 | routes-accountprofile--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountProfile / Failed | web/src/routes/AccountProfile.stories.tsx:88 | routes-accountprofile--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountSecurity / Populated | web/src/routes/AccountSecurity.stories.tsx:153 | routes-accountsecurity--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountSecurity / Empty | web/src/routes/AccountSecurity.stories.tsx:165 | routes-accountsecurity--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountSecurity / Loading | web/src/routes/AccountSecurity.stories.tsx:192 | routes-accountsecurity--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountSecurity / Failed | web/src/routes/AccountSecurity.stories.tsx:202 | routes-accountsecurity--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/AccountSecurity / ProofDialog | web/src/routes/AccountSecurity.stories.tsx:221 | routes-accountsecurity--proof-dialog | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Audit / Populated | web/src/routes/Audit.stories.tsx:51 | routes-audit--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Audit / Empty | web/src/routes/Audit.stories.tsx:66 | routes-audit--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Audit / Loading | web/src/routes/Audit.stories.tsx:81 | routes-audit--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Audit / Failed | web/src/routes/Audit.stories.tsx:96 | routes-audit--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingCard / Default | web/src/routes/BindingCard.stories.tsx:43 | routes-bindingcard--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingCard / ExpiringSoon | web/src/routes/BindingCard.stories.tsx:53 | routes-bindingcard--expiring-soon | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingCard / Quarantined | web/src/routes/BindingCard.stories.tsx:62 | routes-bindingcard--quarantined | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingCard / NotReady | web/src/routes/BindingCard.stories.tsx:70 | routes-bindingcard--not-ready | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / Default | web/src/routes/BindingDialog.stories.tsx:79 | routes-bindingdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / GitHubActions | web/src/routes/BindingDialog.stories.tsx:93 | routes-bindingdialog--git-hub-actions | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / PullRequestEvent | web/src/routes/BindingDialog.stories.tsx:104 | routes-bindingdialog--pull-request-event | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / AudienceRequired | web/src/routes/BindingDialog.stories.tsx:117 | routes-bindingdialog--audience-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / Replace | web/src/routes/BindingDialog.stories.tsx:127 | routes-bindingdialog--replace | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / Busy | web/src/routes/BindingDialog.stories.tsx:140 | routes-bindingdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/BindingDialog / Failed | web/src/routes/BindingDialog.stories.tsx:152 | routes-bindingdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / Disclosure | web/src/routes/CLIReauth.stories.tsx:121 | routes-clireauth--disclosure | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / DisclosureOIDC | web/src/routes/CLIReauth.stories.tsx:139 | routes-clireauth--disclosure-oidc | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / Adapter | web/src/routes/CLIReauth.stories.tsx:152 | routes-clireauth--adapter | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / SelfConfig | web/src/routes/CLIReauth.stories.tsx:166 | routes-clireauth--self-config | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / Loading | web/src/routes/CLIReauth.stories.tsx:177 | routes-clireauth--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / Failed | web/src/routes/CLIReauth.stories.tsx:186 | routes-clireauth--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CLIReauth / NothingToAuthorize | web/src/routes/CLIReauth.stories.tsx:199 | routes-clireauth--nothing-to-authorize | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / Populated | web/src/routes/CatalogueManageDialog.stories.tsx:80 | routes-cataloguemanagedialog--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / Empty | web/src/routes/CatalogueManageDialog.stories.tsx:90 | routes-cataloguemanagedialog--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / Loading | web/src/routes/CatalogueManageDialog.stories.tsx:99 | routes-cataloguemanagedialog--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / Failed | web/src/routes/CatalogueManageDialog.stories.tsx:108 | routes-cataloguemanagedialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / ReadOnly | web/src/routes/CatalogueManageDialog.stories.tsx:117 | routes-cataloguemanagedialog--read-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CatalogueManageDialog / CreateRefused | web/src/routes/CatalogueManageDialog.stories.tsx:128 | routes-cataloguemanagedialog--create-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Ceremony / Protected | web/src/routes/Ceremony.stories.tsx:65 | routes-ceremony--protected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Ceremony / CodeOffered | web/src/routes/Ceremony.stories.tsx:76 | routes-ceremony--code-offered | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Ceremony / OIDC | web/src/routes/Ceremony.stories.tsx:86 | routes-ceremony--oidc | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Ceremony / Refused | web/src/routes/Ceremony.stories.tsx:99 | routes-ceremony--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChangeApprovals / Populated | web/src/routes/ChangeApprovals.stories.tsx:107 | routes-changeapprovals--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChangeApprovals / Empty | web/src/routes/ChangeApprovals.stories.tsx:124 | routes-changeapprovals--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChangeApprovals / Loading | web/src/routes/ChangeApprovals.stories.tsx:139 | routes-changeapprovals--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChangeApprovals / Failed | web/src/routes/ChangeApprovals.stories.tsx:154 | routes-changeapprovals--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChromeIdentityControls / OrgIdentity | web/src/routes/ChromeIdentityControls.stories.tsx:23 | routes-chromeidentitycontrols--org-identity | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ChromeIdentityControls / ProjectIdentity | web/src/routes/ChromeIdentityControls.stories.tsx:32 | routes-chromeidentitycontrols--project-identity | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CompactOrgRetention / Default | web/src/routes/CompactOrgRetention.stories.tsx:21 | routes-compactorgretention--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CompactOrgRetention / Busy | web/src/routes/CompactOrgRetention.stories.tsx:29 | routes-compactorgretention--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CompactOrgRetention / Refused | web/src/routes/CompactOrgRetention.stories.tsx:37 | routes-compactorgretention--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionMintDialog / Default | web/src/routes/ConnectionMintDialog.stories.tsx:26 | routes-connectionmintdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionMintDialog / Clamped | web/src/routes/ConnectionMintDialog.stories.tsx:37 | routes-connectionmintdialog--clamped | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionRow / Live | web/src/routes/ConnectionRow.stories.tsx:43 | routes-connectionrow--live | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionRow / Expired | web/src/routes/ConnectionRow.stories.tsx:52 | routes-connectionrow--expired | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionRow / Revoked | web/src/routes/ConnectionRow.stories.tsx:61 | routes-connectionrow--revoked | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ConnectionRow / Indefinite | web/src/routes/ConnectionRow.stories.tsx:79 | routes-connectionrow--indefinite | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateAccountDialog / Default | web/src/routes/CreateAccountDialog.stories.tsx:31 | routes-createaccountdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateAccountDialog / NameRequired | web/src/routes/CreateAccountDialog.stories.tsx:40 | routes-createaccountdialog--name-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateAccountDialog / Busy | web/src/routes/CreateAccountDialog.stories.tsx:48 | routes-createaccountdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateAccountDialog / Failed | web/src/routes/CreateAccountDialog.stories.tsx:60 | routes-createaccountdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateProviderDialog / Default | web/src/routes/CreateProviderDialog.stories.tsx:37 | routes-createproviderdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateProviderDialog / FieldsRequired | web/src/routes/CreateProviderDialog.stories.tsx:51 | routes-createproviderdialog--fields-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateProviderDialog / Busy | web/src/routes/CreateProviderDialog.stories.tsx:61 | routes-createproviderdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CreateProviderDialog / Failed | web/src/routes/CreateProviderDialog.stories.tsx:73 | routes-createproviderdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CredentialForm / Default | web/src/routes/CredentialForm.stories.tsx:20 | routes-credentialform--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CredentialForm / Entered | web/src/routes/CredentialForm.stories.tsx:28 | routes-credentialform--entered | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/CredentialForm / Busy | web/src/routes/CredentialForm.stories.tsx:39 | routes-credentialform--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / Row | web/src/routes/DefinitionsBundlePanel.stories.tsx:136 | routes-definitionsbundlepanel--row | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / DialogOpen | web/src/routes/DefinitionsBundlePanel.stories.tsx:144 | routes-definitionsbundlepanel--dialog-open | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / GitReadOnly | web/src/routes/DefinitionsBundlePanel.stories.tsx:153 | routes-definitionsbundlepanel--git-read-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / Checked | web/src/routes/DefinitionsBundlePanel.stories.tsx:163 | routes-definitionsbundlepanel--checked | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / Planned | web/src/routes/DefinitionsBundlePanel.stories.tsx:174 | routes-definitionsbundlepanel--planned | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / Busy | web/src/routes/DefinitionsBundlePanel.stories.tsx:188 | routes-definitionsbundlepanel--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DefinitionsBundlePanel / Refused | web/src/routes/DefinitionsBundlePanel.stories.tsx:200 | routes-definitionsbundlepanel--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAccountDialog / Default | web/src/routes/DeleteAccountDialog.stories.tsx:40 | routes-deleteaccountdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAccountDialog / OneCredential | web/src/routes/DeleteAccountDialog.stories.tsx:51 | routes-deleteaccountdialog--one-credential | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAccountDialog / Busy | web/src/routes/DeleteAccountDialog.stories.tsx:59 | routes-deleteaccountdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAccountDialog / Failed | web/src/routes/DeleteAccountDialog.stories.tsx:69 | routes-deleteaccountdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAccountDialog / FailedMayHaveCommitted | web/src/routes/DeleteAccountDialog.stories.tsx:83 | routes-deleteaccountdialog--failed-may-have-committed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAdapterDialog / Default | web/src/routes/DeleteAdapterDialog.stories.tsx:36 | routes-deleteadapterdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAdapterDialog / Prune | web/src/routes/DeleteAdapterDialog.stories.tsx:44 | routes-deleteadapterdialog--prune | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteAdapterDialog / Busy | web/src/routes/DeleteAdapterDialog.stories.tsx:55 | routes-deleteadapterdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / NoLeases | web/src/routes/DeleteProviderDialog.stories.tsx:43 | routes-deleteproviderdialog--no-leases | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / LiveLeases | web/src/routes/DeleteProviderDialog.stories.tsx:56 | routes-deleteproviderdialog--live-leases | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / LeasesUnknown | web/src/routes/DeleteProviderDialog.stories.tsx:69 | routes-deleteproviderdialog--leases-unknown | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / Busy | web/src/routes/DeleteProviderDialog.stories.tsx:80 | routes-deleteproviderdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / Conflict | web/src/routes/DeleteProviderDialog.stories.tsx:91 | routes-deleteproviderdialog--conflict | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeleteProviderDialog / Failed | web/src/routes/DeleteProviderDialog.stories.tsx:104 | routes-deleteproviderdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / ReportedHealthy | web/src/routes/DeliveryTargets.stories.tsx:29 | routes-deliverytargets--reported-healthy | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / ReportedDegraded | web/src/routes/DeliveryTargets.stories.tsx:38 | routes-deliverytargets--reported-degraded | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / Stale | web/src/routes/DeliveryTargets.stories.tsx:60 | routes-deliverytargets--stale | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / Refused | web/src/routes/DeliveryTargets.stories.tsx:69 | routes-deliverytargets--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / ReporterRevoked | web/src/routes/DeliveryTargets.stories.tsx:78 | routes-deliverytargets--reporter-revoked | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / QuotaRefused | web/src/routes/DeliveryTargets.stories.tsx:86 | routes-deliverytargets--quota-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / Unknown | web/src/routes/DeliveryTargets.stories.tsx:96 | routes-deliverytargets--unknown | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / Empty | web/src/routes/DeliveryTargets.stories.tsx:105 | routes-deliverytargets--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/DeliveryTargets / Unsupported | web/src/routes/DeliveryTargets.stories.tsx:114 | routes-deliverytargets--unsupported | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EnrolmentGate / Password | web/src/routes/EnrolmentGate.stories.tsx:44 | routes-enrolmentgate--password | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EnrolmentGate / PasswordRefused | web/src/routes/EnrolmentGate.stories.tsx:54 | routes-enrolmentgate--password-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Initial | web/src/routes/EstablishCredential.stories.tsx:58 | routes-establishcredential--initial | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Mismatch | web/src/routes/EstablishCredential.stories.tsx:67 | routes-establishcredential--mismatch | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Refused | web/src/routes/EstablishCredential.stories.tsx:78 | routes-establishcredential--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Done | web/src/routes/EstablishCredential.stories.tsx:90 | routes-establishcredential--done | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Recover | web/src/routes/EstablishCredential.stories.tsx:100 | routes-establishcredential--recover | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/EstablishCredential / Recovered | web/src/routes/EstablishCredential.stories.tsx:110 | routes-establishcredential--recovered | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Populated | web/src/routes/FederationIssuersPanel.stories.tsx:73 | routes-federationissuerspanel--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Empty | web/src/routes/FederationIssuersPanel.stories.tsx:83 | routes-federationissuerspanel--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Loading | web/src/routes/FederationIssuersPanel.stories.tsx:92 | routes-federationissuerspanel--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / SecondFactorRequired | web/src/routes/FederationIssuersPanel.stories.tsx:100 | routes-federationissuerspanel--second-factor-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Failed | web/src/routes/FederationIssuersPanel.stories.tsx:109 | routes-federationissuerspanel--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Creating | web/src/routes/FederationIssuersPanel.stories.tsx:118 | routes-federationissuerspanel--creating | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / Editing | web/src/routes/FederationIssuersPanel.stories.tsx:130 | routes-federationissuerspanel--editing | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FederationIssuersPanel / DeleteRefused | web/src/routes/FederationIssuersPanel.stories.tsx:144 | routes-federationissuerspanel--delete-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FleetUpdateNotice / LocalUpdate | web/src/routes/FleetUpdateNotice.stories.tsx:57 | routes-fleetupdatenotice--local-update | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FleetUpdateNotice / RemoteUpdate | web/src/routes/FleetUpdateNotice.stories.tsx:86 | routes-fleetupdatenotice--remote-update | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FleetUpdateNotice / MultipleUpdates | web/src/routes/FleetUpdateNotice.stories.tsx:118 | routes-fleetupdatenotice--multiple-updates | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FleetUpdateNotice / NoUpdates | web/src/routes/FleetUpdateNotice.stories.tsx:154 | routes-fleetupdatenotice--no-updates | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FolderCleanupDialog / Default | web/src/routes/FolderCleanupDialog.stories.tsx:36 | routes-foldercleanupdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FolderCleanupDialog / WideningRefused | web/src/routes/FolderCleanupDialog.stories.tsx:49 | routes-foldercleanupdialog--widening-refused | error/refusal, long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/FolderCleanupDialog / Busy | web/src/routes/FolderCleanupDialog.stories.tsx:80 | routes-foldercleanupdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / ReadGrant | web/src/routes/GrantDialog.stories.tsx:93 | routes-grantdialog--read-grant | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / RevealGrant | web/src/routes/GrantDialog.stories.tsx:105 | routes-grantdialog--reveal-grant | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / NothingToWiden | web/src/routes/GrantDialog.stories.tsx:119 | routes-grantdialog--nothing-to-widen | empty, long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / CatalogueFailed | web/src/routes/GrantDialog.stories.tsx:130 | routes-grantdialog--catalogue-failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / CatalogueLoading | web/src/routes/GrantDialog.stories.tsx:139 | routes-grantdialog--catalogue-loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / ReportingGrantable | web/src/routes/GrantDialog.stories.tsx:151 | routes-grantdialog--reporting-grantable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / ReportingNotGrantable | web/src/routes/GrantDialog.stories.tsx:173 | routes-grantdialog--reporting-not-grantable | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / ReportAfterTheFact | web/src/routes/GrantDialog.stories.tsx:190 | routes-grantdialog--report-after-the-fact | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/GrantDialog / ReportingUnsupported | web/src/routes/GrantDialog.stories.tsx:217 | routes-grantdialog--reporting-unsupported | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Never | web/src/routes/HealthChip.stories.tsx:55 | routes-healthchip--never | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Pending | web/src/routes/HealthChip.stories.tsx:62 | routes-healthchip--pending | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Converging | web/src/routes/HealthChip.stories.tsx:69 | routes-healthchip--converging | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Converged | web/src/routes/HealthChip.stories.tsx:76 | routes-healthchip--converged | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Degraded | web/src/routes/HealthChip.stories.tsx:83 | routes-healthchip--degraded | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Failed | web/src/routes/HealthChip.stories.tsx:90 | routes-healthchip--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / Paused | web/src/routes/HealthChip.stories.tsx:97 | routes-healthchip--paused | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HealthChip / DriftAttention | web/src/routes/HealthChip.stories.tsx:105 | routes-healthchip--drift-attention | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / Populated | web/src/routes/HistoryDrawer.stories.tsx:212 | routes-historydrawer--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / KeyFilter | web/src/routes/HistoryDrawer.stories.tsx:226 | routes-historydrawer--key-filter | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / PinSheetOpen | web/src/routes/HistoryDrawer.stories.tsx:237 | routes-historydrawer--pin-sheet-open | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / RestoreSheetOpen | web/src/routes/HistoryDrawer.stories.tsx:246 | routes-historydrawer--restore-sheet-open | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / Empty | web/src/routes/HistoryDrawer.stories.tsx:255 | routes-historydrawer--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / Loading | web/src/routes/HistoryDrawer.stories.tsx:264 | routes-historydrawer--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / Failed | web/src/routes/HistoryDrawer.stories.tsx:272 | routes-historydrawer--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / Phone | web/src/routes/HistoryDrawer.stories.tsx:280 | routes-historydrawer--phone | narrow | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/HistoryDrawer / PhoneDetail | web/src/routes/HistoryDrawer.stories.tsx:290 | routes-historydrawer--phone-detail | narrow | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / Pick | web/src/routes/ImportWizard.stories.tsx:133 | routes-importwizard--pick | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / DotenvSource | web/src/routes/ImportWizard.stories.tsx:141 | routes-importwizard--dotenv-source | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / InvalidLines | web/src/routes/ImportWizard.stories.tsx:150 | routes-importwizard--invalid-lines | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / ConnectorSource | web/src/routes/ImportWizard.stories.tsx:160 | routes-importwizard--connector-source | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / CliGuidance | web/src/routes/ImportWizard.stories.tsx:169 | routes-importwizard--cli-guidance | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / Classify | web/src/routes/ImportWizard.stories.tsx:177 | routes-importwizard--classify | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / Review | web/src/routes/ImportWizard.stories.tsx:186 | routes-importwizard--review | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / Result | web/src/routes/ImportWizard.stories.tsx:195 | routes-importwizard--result | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / ImportRefused | web/src/routes/ImportWizard.stories.tsx:205 | routes-importwizard--import-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / ReviewFailed | web/src/routes/ImportWizard.stories.tsx:217 | routes-importwizard--review-failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ImportWizard / GitManaged | web/src/routes/ImportWizard.stories.tsx:228 | routes-importwizard--git-managed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / Populated | web/src/routes/InstanceAdmin.stories.tsx:94 | routes-instanceadmin--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / NotDisclosed | web/src/routes/InstanceAdmin.stories.tsx:104 | routes-instanceadmin--not-disclosed | empty, error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / Loading | web/src/routes/InstanceAdmin.stories.tsx:114 | routes-instanceadmin--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / SecondFactorRequired | web/src/routes/InstanceAdmin.stories.tsx:124 | routes-instanceadmin--second-factor-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / Failed | web/src/routes/InstanceAdmin.stories.tsx:133 | routes-instanceadmin--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / EditingCredentialPolicy | web/src/routes/InstanceAdmin.stories.tsx:142 | routes-instanceadmin--editing-credential-policy | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceAdmin / RotateDekConfirm | web/src/routes/InstanceAdmin.stories.tsx:153 | routes-instanceadmin--rotate-dek-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Active | web/src/routes/InstanceConfig.stories.tsx:108 | routes-instanceconfig--active | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Unmanaged | web/src/routes/InstanceConfig.stories.tsx:119 | routes-instanceconfig--unmanaged | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Pending | web/src/routes/InstanceConfig.stories.tsx:129 | routes-instanceconfig--pending | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Partial | web/src/routes/InstanceConfig.stories.tsx:140 | routes-instanceconfig--partial | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / RecoveryRequired | web/src/routes/InstanceConfig.stories.tsx:151 | routes-instanceconfig--recovery-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Loading | web/src/routes/InstanceConfig.stories.tsx:161 | routes-instanceconfig--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / NotDisclosed | web/src/routes/InstanceConfig.stories.tsx:169 | routes-instanceconfig--not-disclosed | empty, error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / Failed | web/src/routes/InstanceConfig.stories.tsx:180 | routes-instanceconfig--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InstanceConfig / TestMailCeremony | web/src/routes/InstanceConfig.stories.tsx:191 | routes-instanceconfig--test-mail-ceremony | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InviteDialog / OrgScope | web/src/routes/InviteDialog.stories.tsx:26 | routes-invitedialog--org-scope | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/InviteDialog / InstanceScope | web/src/routes/InviteDialog.stories.tsx:38 | routes-invitedialog--instance-scope | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / Editable | web/src/routes/KeyDeclarationDetail.stories.tsx:128 | routes-keydeclarationdetail--editable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / Deprecated | web/src/routes/KeyDeclarationDetail.stories.tsx:138 | routes-keydeclarationdetail--deprecated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / ReclassifyConfirm | web/src/routes/KeyDeclarationDetail.stories.tsx:149 | routes-keydeclarationdetail--reclassify-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / GitReadOnly | web/src/routes/KeyDeclarationDetail.stories.tsx:163 | routes-keydeclarationdetail--git-read-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / SourceFailed | web/src/routes/KeyDeclarationDetail.stories.tsx:173 | routes-keydeclarationdetail--source-failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / Loading | web/src/routes/KeyDeclarationDetail.stories.tsx:182 | routes-keydeclarationdetail--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / Gone | web/src/routes/KeyDeclarationDetail.stories.tsx:190 | routes-keydeclarationdetail--gone | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/KeyDeclarationDetail / Failed | web/src/routes/KeyDeclarationDetail.stories.tsx:199 | routes-keydeclarationdetail--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LastApplyProvenance / Labelled | web/src/routes/LastApplyProvenance.stories.tsx:25 | routes-lastapplyprovenance--labelled | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LastApplyProvenance / Bare | web/src/routes/LastApplyProvenance.stories.tsx:37 | routes-lastapplyprovenance--bare | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / Renew | web/src/routes/LeaseActionDialog.stories.tsx:49 | routes-leaseactiondialog--renew | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / RenewCeilingRefused | web/src/routes/LeaseActionDialog.stories.tsx:60 | routes-leaseactiondialog--renew-ceiling-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / Revoke | web/src/routes/LeaseActionDialog.stories.tsx:71 | routes-leaseactiondialog--revoke | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / Settle | web/src/routes/LeaseActionDialog.stories.tsx:81 | routes-leaseactiondialog--settle | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / Busy | web/src/routes/LeaseActionDialog.stories.tsx:98 | routes-leaseactiondialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseActionDialog / Failed | web/src/routes/LeaseActionDialog.stories.tsx:108 | routes-leaseactiondialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / Idle | web/src/routes/LeaseMintDialog.stories.tsx:71 | routes-leasemintdialog--idle | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / CeilingRefused | web/src/routes/LeaseMintDialog.stories.tsx:83 | routes-leasemintdialog--ceiling-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / NoSession | web/src/routes/LeaseMintDialog.stories.tsx:96 | routes-leasemintdialog--no-session | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / Submitting | web/src/routes/LeaseMintDialog.stories.tsx:105 | routes-leasemintdialog--submitting | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / Failed | web/src/routes/LeaseMintDialog.stories.tsx:115 | routes-leasemintdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / Disclosed | web/src/routes/LeaseMintDialog.stories.tsx:131 | routes-leasemintdialog--disclosed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/LeaseMintDialog / HeldBack | web/src/routes/LeaseMintDialog.stories.tsx:148 | routes-leasemintdialog--held-back | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Login / WithProviders | web/src/routes/Login.stories.tsx:47 | routes-login--with-providers | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Login / PasswordStep | web/src/routes/Login.stories.tsx:60 | routes-login--password-step | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Login / Paused | web/src/routes/Login.stories.tsx:71 | routes-login--paused | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Login / LocalOnly | web/src/routes/Login.stories.tsx:86 | routes-login--local-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / Populated | web/src/routes/MachineAccess.stories.tsx:241 | routes-machineaccess--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / ExpandedRow | web/src/routes/MachineAccess.stories.tsx:251 | routes-machineaccess--expanded-row | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / FederationTab | web/src/routes/MachineAccess.stories.tsx:260 | routes-machineaccess--federation-tab | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / ProvidersTab | web/src/routes/MachineAccess.stories.tsx:271 | routes-machineaccess--providers-tab | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / KubernetesTab | web/src/routes/MachineAccess.stories.tsx:281 | routes-machineaccess--kubernetes-tab | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / KubernetesTabUnsupported | web/src/routes/MachineAccess.stories.tsx:292 | routes-machineaccess--kubernetes-tab-unsupported | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / LeasesTab | web/src/routes/MachineAccess.stories.tsx:308 | routes-machineaccess--leases-tab | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / Empty | web/src/routes/MachineAccess.stories.tsx:318 | routes-machineaccess--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / Loading | web/src/routes/MachineAccess.stories.tsx:335 | routes-machineaccess--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / Refused | web/src/routes/MachineAccess.stories.tsx:355 | routes-machineaccess--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineAccess / Failed | web/src/routes/MachineAccess.stories.tsx:365 | routes-machineaccess--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevealDialog / Enable | web/src/routes/MachineRevealDialog.stories.tsx:22 | routes-machinerevealdialog--enable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevealDialog / Withdraw | web/src/routes/MachineRevealDialog.stories.tsx:33 | routes-machinerevealdialog--withdraw | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevealDialog / Busy | web/src/routes/MachineRevealDialog.stories.tsx:42 | routes-machinerevealdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevealDialog / Failed | web/src/routes/MachineRevealDialog.stories.tsx:52 | routes-machinerevealdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevokeCredentialDialog / Default | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:35 | routes-machinerevokecredentialdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevokeCredentialDialog / Busy | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:46 | routes-machinerevokecredentialdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MachineRevokeCredentialDialog / Failed | web/src/routes/MachineRevokeCredentialDialog.stories.tsx:56 | routes-machinerevokecredentialdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Populated | web/src/routes/Matrix.stories.tsx:304 | routes-matrix--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Degraded | web/src/routes/Matrix.stories.tsx:318 | routes-matrix--degraded | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / GitManaged | web/src/routes/Matrix.stories.tsx:335 | routes-matrix--git-managed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Empty | web/src/routes/Matrix.stories.tsx:346 | routes-matrix--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / NoEnvironments | web/src/routes/Matrix.stories.tsx:361 | routes-matrix--no-environments | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Loading | web/src/routes/Matrix.stories.tsx:370 | routes-matrix--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Forbidden | web/src/routes/Matrix.stories.tsx:378 | routes-matrix--forbidden | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Matrix / Failed | web/src/routes/Matrix.stories.tsx:388 | routes-matrix--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixKeyCreate / Default | web/src/routes/MatrixKeyCreate.stories.tsx:37 | routes-matrixkeycreate--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixPublishSheet / Default | web/src/routes/MatrixPublishSheet.stories.tsx:54 | routes-matrixpublishsheet--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixPublishSheet / Blocked | web/src/routes/MatrixPublishSheet.stories.tsx:72 | routes-matrixpublishsheet--blocked | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixPublishSheet / Protected | web/src/routes/MatrixPublishSheet.stories.tsx:96 | routes-matrixpublishsheet--protected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixRowEditor / Default | web/src/routes/MatrixRowEditor.stories.tsx:81 | routes-matrixroweditor--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixRowEditor / CopyDisclosure | web/src/routes/MatrixRowEditor.stories.tsx:93 | routes-matrixroweditor--copy-disclosure | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixRowEditor / Edits | web/src/routes/MatrixRowEditor.stories.tsx:107 | routes-matrixroweditor--edits | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixRowEditor / LongValue | web/src/routes/MatrixRowEditor.stories.tsx:138 | routes-matrixroweditor--long-value | long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MatrixRowEditor / LongSecretValue | web/src/routes/MatrixRowEditor.stories.tsx:153 | routes-matrixroweditor--long-secret-value | long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Members / Populated | web/src/routes/Members.stories.tsx:105 | routes-members--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Members / LoadError | web/src/routes/Members.stories.tsx:124 | routes-members--load-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Members / WithAccessRules | web/src/routes/Members.stories.tsx:201 | routes-members--with-access-rules | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Members / ProjectWithAccessRules | web/src/routes/Members.stories.tsx:251 | routes-members--project-with-access-rules | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintConnectionForm / Default | web/src/routes/MintConnectionForm.stories.tsx:28 | routes-mintconnectionform--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintConnectionForm / WithError | web/src/routes/MintConnectionForm.stories.tsx:44 | routes-mintconnectionform--with-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / Reviewing | web/src/routes/MintDialog.stories.tsx:61 | routes-mintdialog--reviewing | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / ReviewingNoReach | web/src/routes/MintDialog.stories.tsx:70 | routes-mintdialog--reviewing-no-reach | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / Rotating | web/src/routes/MintDialog.stories.tsx:80 | routes-mintdialog--rotating | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / Submitting | web/src/routes/MintDialog.stories.tsx:89 | routes-mintdialog--submitting | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / Failed | web/src/routes/MintDialog.stories.tsx:98 | routes-mintdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / Disclosed | web/src/routes/MintDialog.stories.tsx:113 | routes-mintdialog--disclosed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/MintDialog / HeldBack | web/src/routes/MintDialog.stories.tsx:126 | routes-mintdialog--held-back | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OIDCDone / NoTransaction | web/src/routes/OIDCDone.stories.tsx:40 | routes-oidcdone--no-transaction | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OIDCDone / LoginRefused | web/src/routes/OIDCDone.stories.tsx:48 | routes-oidcdone--login-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OIDCDone / LinkRefused | web/src/routes/OIDCDone.stories.tsx:60 | routes-oidcdone--link-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OIDCDone / ReauthRefused | web/src/routes/OIDCDone.stories.tsx:71 | routes-oidcdone--reauth-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / Populated | web/src/routes/OidcProvidersPanel.stories.tsx:57 | routes-oidcproviderspanel--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / Empty | web/src/routes/OidcProvidersPanel.stories.tsx:66 | routes-oidcproviderspanel--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / Loading | web/src/routes/OidcProvidersPanel.stories.tsx:75 | routes-oidcproviderspanel--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / SecondFactorRequired | web/src/routes/OidcProvidersPanel.stories.tsx:83 | routes-oidcproviderspanel--second-factor-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / Failed | web/src/routes/OidcProvidersPanel.stories.tsx:92 | routes-oidcproviderspanel--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / Reconfiguring | web/src/routes/OidcProvidersPanel.stories.tsx:101 | routes-oidcproviderspanel--reconfiguring | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OidcProvidersPanel / DeleteConfirm | web/src/routes/OidcProvidersPanel.stories.tsx:113 | routes-oidcproviderspanel--delete-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OpsDiagnosticBanners / ErrorSeverity | web/src/routes/OpsDiagnosticBanners.stories.tsx:18 | routes-opsdiagnosticbanners--error-severity | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OpsDiagnosticBanners / Warn | web/src/routes/OpsDiagnosticBanners.stories.tsx:20 | routes-opsdiagnosticbanners--warn | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OpsDiagnosticBanners / Unknown | web/src/routes/OpsDiagnosticBanners.stories.tsx:24 | routes-opsdiagnosticbanners--unknown | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OpsDiagnosticBanners / CssCheck | web/src/routes/OpsDiagnosticBanners.stories.tsx:31 | routes-opsdiagnosticbanners--css-check | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OrgSettings / Populated | web/src/routes/OrgSettings.stories.tsx:110 | routes-orgsettings--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OrgSettings / Empty | web/src/routes/OrgSettings.stories.tsx:127 | routes-orgsettings--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OrgSettings / Loading | web/src/routes/OrgSettings.stories.tsx:143 | routes-orgsettings--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/OrgSettings / Failed | web/src/routes/OrgSettings.stories.tsx:156 | routes-orgsettings--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/PinReleaseOutcome / Retained | web/src/routes/PinReleaseOutcome.stories.tsx:19 | routes-pinreleaseoutcome--retained | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/PinReleaseOutcome / CollectionEligible | web/src/routes/PinReleaseOutcome.stories.tsx:27 | routes-pinreleaseoutcome--collection-eligible | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/PinReleaseOutcome / AlreadyCollected | web/src/routes/PinReleaseOutcome.stories.tsx:35 | routes-pinreleaseoutcome--already-collected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Placeholder / Default | web/src/routes/Placeholder.stories.tsx:14 | routes-placeholder--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProfileUpdateBadge / Default | web/src/routes/ProfileUpdateBadge.stories.tsx:26 | routes-profileupdatebadge--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProfileUpdateBadge / MultipleVersions | web/src/routes/ProfileUpdateBadge.stories.tsx:34 | routes-profileupdatebadge--multiple-versions | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProjectSettings / Administrable | web/src/routes/ProjectSettings.stories.tsx:114 | routes-projectsettings--administrable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProjectSettings / UnlimitedRetentionOverride | web/src/routes/ProjectSettings.stories.tsx:129 | routes-projectsettings--unlimited-retention-override | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProjectSettings / MemberWithEnvironments | web/src/routes/ProjectSettings.stories.tsx:148 | routes-projectsettings--member-with-environments | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProjectSettings / LoadError | web/src/routes/ProjectSettings.stories.tsx:174 | routes-projectsettings--load-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Projects / Empty | web/src/routes/Projects.stories.tsx:55 | routes-projects--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Projects / Populated | web/src/routes/Projects.stories.tsx:64 | routes-projects--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Projects / LoadError | web/src/routes/Projects.stories.tsx:84 | routes-projects--load-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ProviderDiscoveryAlert / Default | web/src/routes/ProviderDiscoveryAlert.stories.tsx:15 | routes-providerdiscoveryalert--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Reconnect / Contacting | web/src/routes/Reconnect.stories.tsx:45 | routes-reconnect--contacting | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Reconnect / Ready | web/src/routes/Reconnect.stories.tsx:53 | routes-reconnect--ready | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Reconnect / Failed | web/src/routes/Reconnect.stories.tsx:61 | routes-reconnect--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RemoteCard / Healthy | web/src/routes/RemoteCard.stories.tsx:66 | routes-remotecard--healthy | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RemoteCard / Unreachable | web/src/routes/RemoteCard.stories.tsx:91 | routes-remotecard--unreachable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RemoteCard / CredentialRejected | web/src/routes/RemoteCard.stories.tsx:109 | routes-remotecard--credential-rejected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RemoteCard / Duplicate | web/src/routes/RemoteCard.stories.tsx:121 | routes-remotecard--duplicate | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RetentionBoundsFields / Days | web/src/routes/RetentionBoundsFields.stories.tsx:20 | routes-retentionboundsfields--days | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RetentionBoundsFields / Exact | web/src/routes/RetentionBoundsFields.stories.tsx:22 | routes-retentionboundsfields--exact | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RetentionBoundsFields / Absent | web/src/routes/RetentionBoundsFields.stories.tsx:31 | routes-retentionboundsfields--absent | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevisionDiff / Populated | web/src/routes/RevisionDiff.stories.tsx:110 | routes-revisiondiff--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevisionDiff / Revealed | web/src/routes/RevisionDiff.stories.tsx:123 | routes-revisiondiff--revealed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevisionDiff / Empty | web/src/routes/RevisionDiff.stories.tsx:143 | routes-revisiondiff--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevisionDiff / Loading | web/src/routes/RevisionDiff.stories.tsx:151 | routes-revisiondiff--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevisionDiff / Failed | web/src/routes/RevisionDiff.stories.tsx:159 | routes-revisiondiff--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevokeConnectionDialog / Default | web/src/routes/RevokeConnectionDialog.stories.tsx:37 | routes-revokeconnectiondialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevokeCredentialDialog / Default | web/src/routes/RevokeCredentialDialog.stories.tsx:36 | routes-revokecredentialdialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevokeCredentialDialog / Confirm | web/src/routes/RevokeCredentialDialog.stories.tsx:46 | routes-revokecredentialdialog--confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/RevokeCredentialDialog / Busy | web/src/routes/RevokeCredentialDialog.stories.tsx:56 | routes-revokecredentialdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / Populated | web/src/routes/SamlProvidersPanel.stories.tsx:138 | routes-samlproviderspanel--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / Empty | web/src/routes/SamlProvidersPanel.stories.tsx:147 | routes-samlproviderspanel--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / Loading | web/src/routes/SamlProvidersPanel.stories.tsx:155 | routes-samlproviderspanel--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / SecondFactorRequired | web/src/routes/SamlProvidersPanel.stories.tsx:163 | routes-samlproviderspanel--second-factor-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / Failed | web/src/routes/SamlProvidersPanel.stories.tsx:171 | routes-samlproviderspanel--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / Creating | web/src/routes/SamlProvidersPanel.stories.tsx:179 | routes-samlproviderspanel--creating | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlProvidersPanel / MetadataDiff | web/src/routes/SamlProvidersPanel.stories.tsx:191 | routes-samlproviderspanel--metadata-diff | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / Populated | web/src/routes/SamlSpKeysPanel.stories.tsx:40 | routes-samlspkeyspanel--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / Loading | web/src/routes/SamlSpKeysPanel.stories.tsx:50 | routes-samlspkeyspanel--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / SecondFactorRequired | web/src/routes/SamlSpKeysPanel.stories.tsx:59 | routes-samlspkeyspanel--second-factor-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / Failed | web/src/routes/SamlSpKeysPanel.stories.tsx:67 | routes-samlspkeyspanel--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / RetireConfirm | web/src/routes/SamlSpKeysPanel.stories.tsx:76 | routes-samlspkeyspanel--retire-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SamlSpKeysPanel / CompromiseRetireConfirm | web/src/routes/SamlSpKeysPanel.stories.tsx:85 | routes-samlspkeyspanel--compromise-retire-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScanBlockDialog / Overridable | web/src/routes/ScanBlockDialog.stories.tsx:31 | routes-scanblockdialog--overridable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScanBlockDialog / HardBlock | web/src/routes/ScanBlockDialog.stories.tsx:44 | routes-scanblockdialog--hard-block | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScanWarnDialog / Default | web/src/routes/ScanWarnDialog.stories.tsx:50 | routes-scanwarndialog--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScimProvisioning / Administering | web/src/routes/ScimProvisioning.stories.tsx:216 | routes-scimprovisioning--administering | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScimProvisioning / Unselected | web/src/routes/ScimProvisioning.stories.tsx:231 | routes-scimprovisioning--unselected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScimProvisioning / Empty | web/src/routes/ScimProvisioning.stories.tsx:242 | routes-scimprovisioning--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ScimProvisioning / Failed | web/src/routes/ScimProvisioning.stories.tsx:251 | routes-scimprovisioning--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / DefaultPanel | web/src/routes/Sections.stories.tsx:29 | routes-sections--default-panel | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / DangerPanel | web/src/routes/Sections.stories.tsx:31 | routes-sections--danger-panel | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / QuestionPanel | web/src/routes/Sections.stories.tsx:33 | routes-sections--question-panel | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / TightPanel | web/src/routes/Sections.stories.tsx:35 | routes-sections--tight-panel | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / Jump | web/src/routes/Sections.stories.tsx:37 | routes-sections--jump | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / Disclosure | web/src/routes/Sections.stories.tsx:49 | routes-sections--disclosure | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / CopyOnce | web/src/routes/Sections.stories.tsx:58 | routes-sections--copy-once | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / Consequences | web/src/routes/Sections.stories.tsx:66 | routes-sections--consequences | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Sections / TypedConfirm | web/src/routes/Sections.stories.tsx:88 | routes-sections--typed-confirm | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SetCredentialDialog / Replace | web/src/routes/SetCredentialDialog.stories.tsx:38 | routes-setcredentialdialog--replace | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SetCredentialDialog / FirstCredential | web/src/routes/SetCredentialDialog.stories.tsx:49 | routes-setcredentialdialog--first-credential | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SetCredentialDialog / CredentialRequired | web/src/routes/SetCredentialDialog.stories.tsx:59 | routes-setcredentialdialog--credential-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SetCredentialDialog / Busy | web/src/routes/SetCredentialDialog.stories.tsx:67 | routes-setcredentialdialog--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SetCredentialDialog / Failed | web/src/routes/SetCredentialDialog.stories.tsx:78 | routes-setcredentialdialog--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarLinkItem / Default | web/src/routes/SidebarLinkItem.stories.tsx:34 | routes-sidebarlinkitem--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarLinkItem / Active | web/src/routes/SidebarLinkItem.stories.tsx:43 | routes-sidebarlinkitem--active | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarLinkItem / Disabled | web/src/routes/SidebarLinkItem.stories.tsx:52 | routes-sidebarlinkitem--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarLinkItem / MembersActive | web/src/routes/SidebarLinkItem.stories.tsx:69 | routes-sidebarlinkitem--members-active | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarVersion / Default | web/src/routes/SidebarVersion.stories.tsx:16 | routes-sidebarversion--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SidebarVersion / Absent | web/src/routes/SidebarVersion.stories.tsx:26 | routes-sidebarversion--absent | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/StepUpBanner / AuthenticatorCode | web/src/routes/StepUpBanner.stories.tsx:50 | routes-stepupbanner--authenticator-code | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/StepUpBanner / Passkey | web/src/routes/StepUpBanner.stories.tsx:62 | routes-stepupbanner--passkey | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SystemProjectNotice / Shown | web/src/routes/SystemProjectNotice.stories.tsx:41 | routes-systemprojectnotice--shown | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SystemScopeRefusal / Adapters | web/src/routes/SystemScopeRefusal.stories.tsx:20 | routes-systemscoperefusal--adapters | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SystemScopeRefusal / MachineAccess | web/src/routes/SystemScopeRefusal.stories.tsx:28 | routes-systemscoperefusal--machine-access | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/SystemScopeRefusal / Scim | web/src/routes/SystemScopeRefusal.stories.tsx:35 | routes-systemscoperefusal--scim | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TargetForm / Default | web/src/routes/TargetForm.stories.tsx:89 | routes-targetform--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TargetForm / LockedRouting | web/src/routes/TargetForm.stories.tsx:98 | routes-targetform--locked-routing | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TargetForm / OrganizationSelected | web/src/routes/TargetForm.stories.tsx:106 | routes-targetform--organization-selected | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TargetForm / EmptyKeys | web/src/routes/TargetForm.stories.tsx:125 | routes-targetform--empty-keys | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TargetForm / Busy | web/src/routes/TargetForm.stories.tsx:134 | routes-targetform--busy | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TemporaryAccess / Populated | web/src/routes/TemporaryAccess.stories.tsx:80 | routes-temporaryaccess--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TemporaryAccess / Requester | web/src/routes/TemporaryAccess.stories.tsx:98 | routes-temporaryaccess--requester | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/TemporaryAccess / Loading | web/src/routes/TemporaryAccess.stories.tsx:114 | routes-temporaryaccess--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ThemeToggle / Default | web/src/routes/ThemeToggle.stories.tsx:24 | routes-themetoggle--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/ThemeToggle / Toggles | web/src/routes/ThemeToggle.stories.tsx:33 | routes-themetoggle--toggles | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/UpdateJobStatus / Queued | web/src/routes/UpdateJobStatus.stories.tsx:31 | routes-updatejobstatus--queued | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/UpdateJobStatus / Running | web/src/routes/UpdateJobStatus.stories.tsx:39 | routes-updatejobstatus--running | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/UpdateJobStatus / Succeeded | web/src/routes/UpdateJobStatus.stories.tsx:47 | routes-updatejobstatus--succeeded | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/UpdateJobStatus / Failed | web/src/routes/UpdateJobStatus.stories.tsx:55 | routes-updatejobstatus--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / Populated | web/src/routes/Values.stories.tsx:98 | routes-values--populated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / WindowLive | web/src/routes/Values.stories.tsx:108 | routes-values--window-live | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / ProtectedLocked | web/src/routes/Values.stories.tsx:123 | routes-values--protected-locked | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / WriteOnly | web/src/routes/Values.stories.tsx:135 | routes-values--write-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / Empty | web/src/routes/Values.stories.tsx:154 | routes-values--empty | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / Loading | web/src/routes/Values.stories.tsx:165 | routes-values--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/Values / Failed | web/src/routes/Values.stories.tsx:174 | routes-values--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / NothingToAuthorize | web/src/routes/WorkspaceApprove.stories.tsx:64 | routes-workspaceapprove--nothing-to-authorize | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / Loading | web/src/routes/WorkspaceApprove.stories.tsx:74 | routes-workspaceapprove--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / Establishment | web/src/routes/WorkspaceApprove.stories.tsx:83 | routes-workspaceapprove--establishment | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / StepUp | web/src/routes/WorkspaceApprove.stories.tsx:97 | routes-workspaceapprove--step-up | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / Failed | web/src/routes/WorkspaceApprove.stories.tsx:113 | routes-workspaceapprove--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceApprove / SignIn | web/src/routes/WorkspaceApprove.stories.tsx:128 | routes-workspaceapprove--sign-in | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceCallback / NoResult | web/src/routes/WorkspaceCallback.stories.tsx:34 | routes-workspacecallback--no-result | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceCallback / CouldNotClose | web/src/routes/WorkspaceCallback.stories.tsx:42 | routes-workspacecallback--could-not-close | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / Loading | web/src/routes/WorkspaceScope.stories.tsx:77 | routes-workspacescope--loading | loading | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / UnknownRemote | web/src/routes/WorkspaceScope.stories.tsx:85 | routes-workspacescope--unknown-remote | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / ReconnectRequired | web/src/routes/WorkspaceScope.stories.tsx:95 | routes-workspacescope--reconnect-required | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / Checking | web/src/routes/WorkspaceScope.stories.tsx:107 | routes-workspacescope--checking | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / Failed | web/src/routes/WorkspaceScope.stories.tsx:121 | routes-workspacescope--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceScope / Connected | web/src/routes/WorkspaceScope.stories.tsx:143 | routes-workspacescope--connected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceStepUp / Disconnected | web/src/routes/WorkspaceStepUp.stories.tsx:78 | routes-workspacestepup--disconnected | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceStepUp / Contacting | web/src/routes/WorkspaceStepUp.stories.tsx:86 | routes-workspacestepup--contacting | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceStepUp / Ready | web/src/routes/WorkspaceStepUp.stories.tsx:95 | routes-workspacestepup--ready | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| routes/WorkspaceStepUp / Failed | web/src/routes/WorkspaceStepUp.stories.tsx:106 | routes-workspacestepup--failed | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / Rules | web/src/routes/accessRules/AccessRules.stories.tsx:48 | members-access-rules--rules | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / RulesOnAProject | web/src/routes/accessRules/AccessRules.stories.tsx:74 | members-access-rules--rules-on-a-project | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / EditorFolderRule | web/src/routes/accessRules/AccessRules.stories.tsx:99 | members-access-rules--editor-folder-rule | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / EditorNewRule | web/src/routes/accessRules/AccessRules.stories.tsx:156 | members-access-rules--editor-new-rule | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / EditorSingleKeyRule | web/src/routes/accessRules/AccessRules.stories.tsx:184 | members-access-rules--editor-single-key-rule | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / EditorWithoutKeyNames | web/src/routes/accessRules/AccessRules.stories.tsx:209 | members-access-rules--editor-without-key-names | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / WhoCan | web/src/routes/accessRules/AccessRules.stories.tsx:222 | members-access-rules--who-can | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / WhoCanWithoutKeyNames | web/src/routes/accessRules/AccessRules.stories.tsx:262 | members-access-rules--who-can-without-key-names | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / KeyMoveConfirmation | web/src/routes/accessRules/AccessRules.stories.tsx:287 | members-access-rules--key-move-confirmation | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / KeyMoveCountOnly | web/src/routes/accessRules/AccessRules.stories.tsx:313 | members-access-rules--key-move-count-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| Members/Access rules / Glossary | web/src/routes/accessRules/AccessRules.stories.tsx:324 | members-access-rules--glossary | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / Danger | web/src/ui/Alert.stories.tsx:17 | ui-alert--danger | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / Done | web/src/ui/Alert.stories.tsx:18 | ui-alert--done | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / Warn | web/src/ui/Alert.stories.tsx:19 | ui-alert--warn | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / Info | web/src/ui/Alert.stories.tsx:26 | ui-alert--info | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / WithAction | web/src/ui/Alert.stories.tsx:33 | ui-alert--with-action | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / AllStates | web/src/ui/Alert.stories.tsx:42 | ui-alert--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Alert / RolesMatchTone | web/src/ui/Alert.stories.tsx:61 | ui-alert--roles-match-tone | default/prop/composition; source review required | removed with explicit visual/test successor: Assertive/polite tone roles and decorative hiding regression-tested; all feedback states retained.; regression web/src/ui/accessibility.test.tsx |
| ui/Badge / Neutral | web/src/ui/Badge.stories.tsx:15 | ui-badge--neutral | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Badge / Danger | web/src/ui/Badge.stories.tsx:16 | ui-badge--danger | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned danger badge; Neutral exposes tone controls. |
| ui/Badge / Changed | web/src/ui/Badge.stories.tsx:17 | ui-badge--changed | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned changed badge; Neutral exposes tone controls. |
| ui/Badge / Ok | web/src/ui/Badge.stories.tsx:18 | ui-badge--ok | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned OK badge; Neutral exposes tone controls. |
| ui/Badge / Mono | web/src/ui/Badge.stories.tsx:19 | ui-badge--mono | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned monospace badge; Neutral exposes mono controls. |
| ui/Badge / AllTones | web/src/ui/Badge.stories.tsx:31 | ui-badge--all-tones | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Button / Secondary | web/src/ui/Button.stories.tsx:18 | ui-button--secondary | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal.; retain OpenPencil design link |
| ui/Button / Primary | web/src/ui/Button.stories.tsx:19 | ui-button--primary | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal.; retain OpenPencil design link |
| ui/Button / Danger | web/src/ui/Button.stories.tsx:20 | ui-button--danger | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned destructive action in gallery; Secondary exposes variant controls. |
| ui/Button / Quiet | web/src/ui/Button.stories.tsx:21 | ui-button--quiet | default/prop/composition; source review required | removed with explicit visual/test successor: Captioned tertiary action in gallery; Secondary exposes variant controls. |
| ui/Button / Disabled | web/src/ui/Button.stories.tsx:22 | ui-button--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal.; retain OpenPencil design link |
| ui/Button / Icon | web/src/ui/Button.stories.tsx:23 | ui-button--icon | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal.; retain OpenPencil design link |
| ui/Button / Clicks | web/src/ui/Button.stories.tsx:28 | ui-button--clicks | default/prop/composition; source review required | removed with explicit visual/test successor: Behavior-only activation now regression-tested while Secondary retains the same view.; regression web/src/ui/Button.test.tsx |
| ui/Button / AllVariants | web/src/ui/Button.stories.tsx:38 | ui-button--all-variants | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Checkbox / Default | web/src/ui/Checkbox.stories.tsx:15 | ui-checkbox--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Checkbox / Checked | web/src/ui/Checkbox.stories.tsx:16 | ui-checkbox--checked | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Checkbox / Disabled | web/src/ui/Checkbox.stories.tsx:17 | ui-checkbox--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Checkbox / Toggles | web/src/ui/Checkbox.stories.tsx:19 | ui-checkbox--toggles | default/prop/composition; source review required | removed with explicit visual/test successor: Control activation regression-tested; checked view retained.; regression web/src/ui/accessibility.test.tsx |
| ui/Checkbox / LabelToggles | web/src/ui/Checkbox.stories.tsx:30 | ui-checkbox--label-toggles | default/prop/composition; source review required | removed with explicit visual/test successor: Associated label activation regression-tested; checked view retained.; regression web/src/ui/accessibility.test.tsx |
| ui/Checkbox / HitBoxIsTheInput | web/src/ui/Checkbox.stories.tsx:40 | ui-checkbox--hit-box-is-the-input | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Checkbox / AllStates | web/src/ui/Checkbox.stories.tsx:65 | ui-checkbox--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / Stack | web/src/ui/ChoiceGroup.stories.tsx:44 | ui-choicegroup--stack | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / Wrap | web/src/ui/ChoiceGroup.stories.tsx:45 | ui-choicegroup--wrap | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / Nowrap | web/src/ui/ChoiceGroup.stories.tsx:46 | ui-choicegroup--nowrap | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / TwoColumns | web/src/ui/ChoiceGroup.stories.tsx:50 | ui-choicegroup--two-columns | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / ThreeColumns | web/src/ui/ChoiceGroup.stories.tsx:51 | ui-choicegroup--three-columns | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / Chips | web/src/ui/ChoiceGroup.stories.tsx:52 | ui-choicegroup--chips | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / WithHint | web/src/ui/ChoiceGroup.stories.tsx:55 | ui-choicegroup--with-hint | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / Radios | web/src/ui/ChoiceGroup.stories.tsx:58 | ui-choicegroup--radios | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / AllStates | web/src/ui/ChoiceGroup.stories.tsx:72 | ui-choicegroup--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / LegendNamesTheGroup | web/src/ui/ChoiceGroup.stories.tsx:91 | ui-choicegroup--legend-names-the-group | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / ColumnsAreAGrid | web/src/ui/ChoiceGroup.stories.tsx:100 | ui-choicegroup--columns-are-a-grid | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / NowrapScrolls | web/src/ui/ChoiceGroup.stories.tsx:111 | ui-choicegroup--nowrap-scrolls | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ChoiceGroup / ChipsMarkChecked | web/src/ui/ChoiceGroup.stories.tsx:125 | ui-choicegroup--chips-mark-checked | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / Decision | web/src/ui/Dialog.stories.tsx:35 | ui-dialog--decision | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / WithRefusal | web/src/ui/Dialog.stories.tsx:37 | ui-dialog--with-refusal | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / Wide | web/src/ui/Dialog.stories.tsx:43 | ui-dialog--wide | long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / MustAcknowledge | web/src/ui/Dialog.stories.tsx:69 | ui-dialog--must-acknowledge | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / PinnedActions | web/src/ui/Dialog.stories.tsx:92 | ui-dialog--pinned-actions | long content | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / AllStates | web/src/ui/Dialog.stories.tsx:126 | ui-dialog--all-states | default/prop/composition; source review required | removed with explicit visual/test successor: Invalid simultaneous-modal gallery removed; one modal made its sibling inert and unreachable. Both sizes remain independently reviewable. |
| ui/Dialog / BackdropClick | web/src/ui/Dialog.stories.tsx:136 | ui-dialog--backdrop-click | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / IsModalAndLabelled | web/src/ui/Dialog.stories.tsx:166 | ui-dialog--is-modal-and-labelled | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Dialog / MonoTitle | web/src/ui/Dialog.stories.tsx:184 | ui-dialog--mono-title | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Disclosure / Closed | web/src/ui/Disclosure.stories.tsx:18 | ui-disclosure--closed | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Disclosure / Open | web/src/ui/Disclosure.stories.tsx:19 | ui-disclosure--open | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Disclosure / Toggles | web/src/ui/Disclosure.stories.tsx:22 | ui-disclosure--toggles | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Field / Default | web/src/ui/Field.stories.tsx:21 | ui-field--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Field / WithHint | web/src/ui/Field.stories.tsx:28 | ui-field--with-hint | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Field / WithError | web/src/ui/Field.stories.tsx:38 | ui-field--with-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Glyph / Decorative | web/src/ui/Glyph.stories.tsx:18 | ui-glyph--decorative | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Glyph / Named | web/src/ui/Glyph.stories.tsx:19 | ui-glyph--named | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Glyph / AllStates | web/src/ui/Glyph.stories.tsx:22 | ui-glyph--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Glyph / DecorativeIsHidden | web/src/ui/Glyph.stories.tsx:53 | ui-glyph--decorative-is-hidden | default/prop/composition; source review required | removed with explicit visual/test successor: Decorative hiding and named image semantics regression-tested; both views retained.; regression web/src/ui/accessibility.test.tsx |
| ui/Input / Default | web/src/ui/Input.stories.tsx:15 | ui-input--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / Password | web/src/ui/Input.stories.tsx:16 | ui-input--password | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / PasswordRevealable | web/src/ui/Input.stories.tsx:17 | ui-input--password-revealable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / Disabled | web/src/ui/Input.stories.tsx:27 | ui-input--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / Mono | web/src/ui/Input.stories.tsx:28 | ui-input--mono | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / MonoTargetsTheControl | web/src/ui/Input.stories.tsx:31 | ui-input--mono-targets-the-control | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / WithHint | web/src/ui/Input.stories.tsx:41 | ui-input--with-hint | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / WithError | web/src/ui/Input.stories.tsx:45 | ui-input--with-error | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / ErrorIsWired | web/src/ui/Input.stories.tsx:55 | ui-input--error-is-wired | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / LabelIsWired | web/src/ui/Input.stories.tsx:65 | ui-input--label-is-wired | default/prop/composition; source review required | removed with explicit visual/test successor: Field label associations and distinct generated IDs regression-tested.; regression web/src/ui/accessibility.test.tsx |
| ui/Input / AllStates | web/src/ui/Input.stories.tsx:72 | ui-input--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / ExternalDescriptionIsMerged | web/src/ui/Input.stories.tsx:88 | ui-input--external-description-is-merged | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Input / ExternalInvalidIsKept | web/src/ui/Input.stories.tsx:103 | ui-input--external-invalid-is-kept | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / Default | web/src/ui/Menu.stories.tsx:24 | ui-menu--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / Opens | web/src/ui/Menu.stories.tsx:26 | ui-menu--opens | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / Selects | web/src/ui/Menu.stories.tsx:42 | ui-menu--selects | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / Keyboard | web/src/ui/Menu.stories.tsx:65 | ui-menu--keyboard | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / TabLeaves | web/src/ui/Menu.stories.tsx:108 | ui-menu--tab-leaves | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / SelectsByKeyboard | web/src/ui/Menu.stories.tsx:130 | ui-menu--selects-by-keyboard | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Menu / DisabledSkipped | web/src/ui/Menu.stories.tsx:155 | ui-menu--disabled-skipped | disabled/busy, keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Radio / Default | web/src/ui/Radio.stories.tsx:19 | ui-radio--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Radio / Checked | web/src/ui/Radio.stories.tsx:20 | ui-radio--checked | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Radio / Disabled | web/src/ui/Radio.stories.tsx:21 | ui-radio--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Radio / Selects | web/src/ui/Radio.stories.tsx:23 | ui-radio--selects | default/prop/composition; source review required | removed with explicit visual/test successor: Selection and independence of separate Docs groups regression-tested.; regression web/src/ui/accessibility.test.tsx |
| ui/Radio / AllStates | web/src/ui/Radio.stories.tsx:43 | ui-radio--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Select / Default | web/src/ui/Select.stories.tsx:24 | ui-select--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Select / Disabled | web/src/ui/Select.stories.tsx:25 | ui-select--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Select / LabelIsWired | web/src/ui/Select.stories.tsx:27 | ui-select--label-is-wired | default/prop/composition; source review required | removed with explicit visual/test successor: Field label associations and distinct generated IDs regression-tested.; regression web/src/ui/accessibility.test.tsx |
| ui/Select / AllStates | web/src/ui/Select.stories.tsx:33 | ui-select--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Select / ExternalDescriptionIsMerged | web/src/ui/Select.stories.tsx:51 | ui-select--external-description-is-merged | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Select / ExternalInvalidIsKept | web/src/ui/Select.stories.tsx:66 | ui-select--external-invalid-is-kept | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tabs / Default | web/src/ui/Tabs.stories.tsx:39 | ui-tabs--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tabs / SecondSelected | web/src/ui/Tabs.stories.tsx:40 | ui-tabs--second-selected | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tabs / KeyboardMovesAndSelects | web/src/ui/Tabs.stories.tsx:43 | ui-tabs--keyboard-moves-and-selects | keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tabs / TwoDemosStayIndependent | web/src/ui/Tabs.stories.tsx:86 | ui-tabs--two-demos-stay-independent | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / Default | web/src/ui/Textarea.stories.tsx:15 | ui-textarea--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / Filled | web/src/ui/Textarea.stories.tsx:16 | ui-textarea--filled | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / Disabled | web/src/ui/Textarea.stories.tsx:17 | ui-textarea--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / LabelIsWired | web/src/ui/Textarea.stories.tsx:19 | ui-textarea--label-is-wired | default/prop/composition; source review required | removed with explicit visual/test successor: Field label associations and distinct generated IDs regression-tested.; regression web/src/ui/accessibility.test.tsx |
| ui/Textarea / AllStates | web/src/ui/Textarea.stories.tsx:25 | ui-textarea--all-states | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / ExternalDescriptionIsMerged | web/src/ui/Textarea.stories.tsx:39 | ui-textarea--external-description-is-merged | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Textarea / ExternalInvalidIsKept | web/src/ui/Textarea.stories.tsx:54 | ui-textarea--external-invalid-is-kept | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ThemeIcon / Sun | web/src/ui/ThemeIcon.stories.tsx:17 | ui-themeicon--sun | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ThemeIcon / Moon | web/src/ui/ThemeIcon.stories.tsx:27 | ui-themeicon--moon | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / NotIncluded | web/src/ui/ToggleChip.stories.tsx:17 | ui-togglechip--not-included | empty, selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / Included | web/src/ui/ToggleChip.stories.tsx:18 | ui-togglechip--included | selected | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / Implied | web/src/ui/ToggleChip.stories.tsx:20 | ui-togglechip--implied | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / LeftOut | web/src/ui/ToggleChip.stories.tsx:21 | ui-togglechip--left-out | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / Disabled | web/src/ui/ToggleChip.stories.tsx:22 | ui-togglechip--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/ToggleChip / ExcludeToggles | web/src/ui/ToggleChip.stories.tsx:38 | ui-togglechip--exclude-toggles | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tokens / DesignSystem | web/src/ui/Tokens.stories.tsx:94 | ui-tokens--design-system | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Tokens / EveryTokenResolves | web/src/ui/Tokens.stories.tsx:98 | ui-tokens--every-token-resolves | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/Typography / Scale | web/src/ui/Typography.stories.tsx:55 | ui-typography--scale | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/AuthenticatorCodeField / Default | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:24 | ui-auth-authenticatorcodefield--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/AuthenticatorCodeField / Checking | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:25 | ui-auth-authenticatorcodefield--checking | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/AuthenticatorCodeField / SubmitsTrimmedAndClears | web/src/ui/auth/AuthenticatorCodeField.stories.tsx:28 | ui-auth-authenticatorcodefield--submits-trimmed-and-clears | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / PasswordThenAuthenticator | web/src/ui/auth/LoginFlow.stories.tsx:228 | ui-auth-loginflow--password-then-authenticator | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / AuthenticatorCodeRefused | web/src/ui/auth/LoginFlow.stories.tsx:241 | ui-auth-loginflow--authenticator-code-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / UnenrolledIsGatedIntoSetup | web/src/ui/auth/LoginFlow.stories.tsx:256 | ui-auth-loginflow--unenrolled-is-gated-into-setup | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / UnenrolledAllowedByPolicy | web/src/ui/auth/LoginFlow.stories.tsx:278 | ui-auth-loginflow--unenrolled-allowed-by-policy | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / PasskeyPasswordless | web/src/ui/auth/LoginFlow.stories.tsx:288 | ui-auth-loginflow--passkey-passwordless | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / IdentityProvider | web/src/ui/auth/LoginFlow.stories.tsx:298 | ui-auth-loginflow--identity-provider | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginFlow / SignUpThroughProvider | web/src/ui/auth/LoginFlow.stories.tsx:309 | ui-auth-loginflow--sign-up-through-provider | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / WithProviders | web/src/ui/auth/LoginForm.stories.tsx:53 | ui-auth-loginform--with-providers | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / SocialProviders | web/src/ui/auth/LoginForm.stories.tsx:56 | ui-auth-loginform--social-providers | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / LastUsedProvider | web/src/ui/auth/LoginForm.stories.tsx:69 | ui-auth-loginform--last-used-provider | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / LastUsedPassword | web/src/ui/auth/LoginForm.stories.tsx:81 | ui-auth-loginform--last-used-password | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / LastUsedPasskey | web/src/ui/auth/LoginForm.stories.tsx:88 | ui-auth-loginform--last-used-passkey | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / LocalOnly | web/src/ui/auth/LoginForm.stories.tsx:91 | ui-auth-loginform--local-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / PasswordAndPasskey | web/src/ui/auth/LoginForm.stories.tsx:93 | ui-auth-loginform--password-and-passkey | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / PasswordStep | web/src/ui/auth/LoginForm.stories.tsx:96 | ui-auth-loginform--password-step | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / WaitingForPasskey | web/src/ui/auth/LoginForm.stories.tsx:110 | ui-auth-loginform--waiting-for-passkey | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / ContactingProvider | web/src/ui/auth/LoginForm.stories.tsx:119 | ui-auth-loginform--contacting-provider | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / Refused | web/src/ui/auth/LoginForm.stories.tsx:129 | ui-auth-loginform--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / Paused | web/src/ui/auth/LoginForm.stories.tsx:134 | ui-auth-loginform--paused | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / DoorOpen | web/src/ui/auth/LoginForm.stories.tsx:143 | ui-auth-loginform--door-open | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / SignUpDoor | web/src/ui/auth/LoginForm.stories.tsx:154 | ui-auth-loginform--sign-up-door | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / OpensOnSignUp | web/src/ui/auth/LoginForm.stories.tsx:169 | ui-auth-loginform--opens-on-sign-up | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / OpensOnSignUpWhileClosed | web/src/ui/auth/LoginForm.stories.tsx:179 | ui-auth-loginform--opens-on-sign-up-while-closed | overlay/expanded | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / SignUpConfirmation | web/src/ui/auth/LoginForm.stories.tsx:188 | ui-auth-loginform--sign-up-confirmation | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / SignUpBackLinks | web/src/ui/auth/LoginForm.stories.tsx:201 | ui-auth-loginform--sign-up-back-links | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / ProviderStartsFromStepOne | web/src/ui/auth/LoginForm.stories.tsx:214 | ui-auth-loginform--provider-starts-from-step-one | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/LoginForm / SubmitsAndClearsPassword | web/src/ui/auth/LoginForm.stories.tsx:222 | ui-auth-loginform--submits-and-clears-password | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProofDialog / ReauthGated | web/src/ui/auth/ProofDialog.stories.tsx:25 | ui-auth-proofdialog--reauth-gated | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProofDialog / AuthenticatorCode | web/src/ui/auth/ProofDialog.stories.tsx:39 | ui-auth-proofdialog--authenticator-code | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProofDialog / Refused | web/src/ui/auth/ProofDialog.stories.tsx:48 | ui-auth-proofdialog--refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / Google | web/src/ui/auth/ProviderButton.stories.tsx:28 | ui-auth-providerbutton--google | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / GoogleSignUp | web/src/ui/auth/ProviderButton.stories.tsx:31 | ui-auth-providerbutton--google-sign-up | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / Microsoft | web/src/ui/auth/ProviderButton.stories.tsx:34 | ui-auth-providerbutton--microsoft | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / MicrosoftSignUp | web/src/ui/auth/ProviderButton.stories.tsx:37 | ui-auth-providerbutton--microsoft-sign-up | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / GitHub | web/src/ui/auth/ProviderButton.stories.tsx:45 | ui-auth-providerbutton--git-hub | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / GitHubSignUp | web/src/ui/auth/ProviderButton.stories.tsx:47 | ui-auth-providerbutton--git-hub-sign-up | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / Plain | web/src/ui/auth/ProviderButton.stories.tsx:50 | ui-auth-providerbutton--plain | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / Contacting | web/src/ui/auth/ProviderButton.stories.tsx:53 | ui-auth-providerbutton--contacting | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / LastUsed | web/src/ui/auth/ProviderButton.stories.tsx:56 | ui-auth-providerbutton--last-used | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / Disabled | web/src/ui/auth/ProviderButton.stories.tsx:59 | ui-auth-providerbutton--disabled | disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/ProviderButton / FiresOnClick | web/src/ui/auth/ProviderButton.stories.tsx:61 | ui-auth-providerbutton--fires-on-click | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/QrCode / Default | web/src/ui/auth/QrCode.stories.tsx:18 | ui-auth-qrcode--default | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/QrCode / NamedAndScannable | web/src/ui/auth/QrCode.stories.tsx:22 | ui-auth-qrcode--named-and-scannable | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / AuthenticatorAndPasskey | web/src/ui/auth/SecondFactorChallenge.stories.tsx:30 | ui-auth-secondfactorchallenge--authenticator-and-passkey | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / AuthenticatorOnly | web/src/ui/auth/SecondFactorChallenge.stories.tsx:32 | ui-auth-secondfactorchallenge--authenticator-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / PasskeyOnly | web/src/ui/auth/SecondFactorChallenge.stories.tsx:34 | ui-auth-secondfactorchallenge--passkey-only | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / NoFactorPresentable | web/src/ui/auth/SecondFactorChallenge.stories.tsx:37 | ui-auth-secondfactorchallenge--no-factor-presentable | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / CheckingCode | web/src/ui/auth/SecondFactorChallenge.stories.tsx:45 | ui-auth-secondfactorchallenge--checking-code | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / WaitingForPasskey | web/src/ui/auth/SecondFactorChallenge.stories.tsx:47 | ui-auth-secondfactorchallenge--waiting-for-passkey | loading, disabled/busy | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / CodeRefused | web/src/ui/auth/SecondFactorChallenge.stories.tsx:49 | ui-auth-secondfactorchallenge--code-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / NoSkipControl | web/src/ui/auth/SecondFactorChallenge.stories.tsx:57 | ui-auth-secondfactorchallenge--no-skip-control | empty, keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorChallenge / SubmitsTrimmedCode | web/src/ui/auth/SecondFactorChallenge.stories.tsx:64 | ui-auth-secondfactorchallenge--submits-trimmed-code | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / ConfirmPassword | web/src/ui/auth/SecondFactorSetup.stories.tsx:35 | ui-auth-secondfactorsetup--confirm-password | validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / ConfirmPasswordRefused | web/src/ui/auth/SecondFactorSetup.stories.tsx:46 | ui-auth-secondfactorsetup--confirm-password-refused | error/refusal, validation | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / Choose | web/src/ui/auth/SecondFactorSetup.stories.tsx:50 | ui-auth-secondfactorsetup--choose | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / ChooseNoPasskeySupport | web/src/ui/auth/SecondFactorSetup.stories.tsx:52 | ui-auth-secondfactorsetup--choose-no-passkey-support | empty | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / Authenticator | web/src/ui/auth/SecondFactorSetup.stories.tsx:54 | ui-auth-secondfactorsetup--authenticator | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / AuthenticatorCodeRefused | web/src/ui/auth/SecondFactorSetup.stories.tsx:56 | ui-auth-secondfactorsetup--authenticator-code-refused | error/refusal | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / RecoveryCodes | web/src/ui/auth/SecondFactorSetup.stories.tsx:60 | ui-auth-secondfactorsetup--recovery-codes | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / NoSkipOnAnyStep | web/src/ui/auth/SecondFactorSetup.stories.tsx:64 | ui-auth-secondfactorsetup--no-skip-on-any-step | empty, keyboard/focus | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |
| ui/auth/SecondFactorSetup / CodesNeedAcknowledgement | web/src/ui/auth/SecondFactorSetup.stories.tsx:80 | ui-auth-secondfactorsetup--codes-need-acknowledgement | default/prop/composition; source review required | retained: Keep meaningful visual, interaction or controls-driven coverage; no proven redundant successor warranted removal. |

Story-name state hints are navigation aids, not a test pass. The original play function, fixture table, imported component and source line remain available in before.json. Candidate behavior removals require an explicit successor test before they can be removed; all unproven candidates stay retained.

## Every registered route

| Route ID | Path | Mode/chrome | Source component | Baseline story modules | Disposition |
| --- | --- | --- | --- | --- | --- |
| login | /login | public/none | web/src/routes/Login.tsx#Login | web/src/routes/Login.stories.tsx | component stories exist; real route/chrome journey not yet established |
| signup | /signup | public/none | web/src/routes/Login.tsx#Login | web/src/routes/Login.stories.tsx | component stories exist; real route/chrome journey not yet established |
| signup-verify | /signup/verify | public/none | web/src/routes/SignupVerify.tsx#SignupVerify | none | missing page story coverage |
| establish-credential | /establish | public/none | web/src/routes/EstablishCredential.tsx#EstablishCredential | web/src/routes/EstablishCredential.stories.tsx | component stories exist; real route/chrome journey not yet established |
| overview | / | authenticated/shell | web/src/routes/Placeholder.tsx#Overview | none | missing page story coverage |
| projects | /projects | authenticated/shell | web/src/routes/Projects.tsx#Projects | web/src/routes/Projects.stories.tsx | component stories exist; real route/chrome journey not yet established |
| remotes | /remotes | authenticated/shell | web/src/routes/Remotes.tsx#Remotes | none | missing page story coverage |
| members | /orgs/:org/members | authenticated/shell | web/src/routes/Members.tsx#Members | web/src/routes/Members.stories.tsx | component stories exist; real route/chrome journey not yet established |
| org-settings | /orgs/:org/settings | authenticated/shell | web/src/routes/OrgSettings.tsx#OrgSettings | web/src/routes/OrgSettings.stories.tsx | component stories exist; real route/chrome journey not yet established |
| scim | /orgs/:org/scim | authenticated/shell | web/src/routes/ScimProvisioning.tsx#ScimProvisioningPage | web/src/routes/ScimProvisioning.stories.tsx | component stories exist; real route/chrome journey not yet established |
| audit | /orgs/:org/audit | authenticated/shell | web/src/routes/Audit.tsx#Audit | web/src/routes/Audit.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-admin | /instance | authenticated/shell | web/src/routes/InstanceAdmin.tsx#InstanceAdmin | web/src/routes/InstanceAdmin.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-config | /instance/config | authenticated/shell | web/src/routes/InstanceConfig.tsx#InstanceConfig | web/src/routes/InstanceConfig.stories.tsx | component stories exist; real route/chrome journey not yet established |
| instance-members | /instance/members | authenticated/shell | web/src/routes/Members.tsx#Members | web/src/routes/Members.stories.tsx | component stories exist; real route/chrome journey not yet established |
| settings | /settings | authenticated/shell | web/src/routes/AccountSecurity.tsx#AccountSecurity | web/src/routes/AccountSecurity.stories.tsx | component stories exist; real route/chrome journey not yet established |
| matrix | /orgs/:org/projects/:project/matrix | authenticated/shell | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| history | /orgs/:org/projects/:project/matrix/history | authenticated/shell | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| key-detail | /orgs/:org/projects/:project/matrix/keys/:key | authenticated/shell | web/src/routes/Matrix.tsx#Matrix | web/src/routes/Matrix.stories.tsx | component stories exist; real route/chrome journey not yet established |
| values | /orgs/:org/projects/:project/environments/:environment/values | authenticated/shell | web/src/routes/Values.tsx#Values | web/src/routes/Values.stories.tsx | component stories exist; real route/chrome journey not yet established |
| machine-access | /orgs/:org/projects/:project/machine-access | authenticated/shell | web/src/routes/MachineAccess.tsx#MachineAccessPage | web/src/routes/MachineAccess.stories.tsx | component stories exist; real route/chrome journey not yet established |
| change-approvals | /orgs/:org/projects/:project/change-approvals | authenticated/shell | web/src/routes/ChangeApprovals.tsx#ChangeApprovals | web/src/routes/ChangeApprovals.stories.tsx | component stories exist; real route/chrome journey not yet established |
| temporary-access | /orgs/:org/projects/:project/temporary-access | authenticated/shell | web/src/routes/TemporaryAccess.tsx#TemporaryAccess | web/src/routes/TemporaryAccess.stories.tsx | component stories exist; real route/chrome journey not yet established |
| adapters | /orgs/:org/projects/:project/adapters | authenticated/shell | web/src/routes/Adapters.tsx#AdaptersPage | none | missing page story coverage |
| project-audit | /orgs/:org/projects/:project/audit | authenticated/shell | web/src/routes/Audit.tsx#Audit | web/src/routes/Audit.stories.tsx | component stories exist; real route/chrome journey not yet established |
| project-settings | /orgs/:org/projects/:project/settings | authenticated/shell | web/src/routes/ProjectSettings.tsx#ProjectSettings | web/src/routes/ProjectSettings.stories.tsx | component stories exist; real route/chrome journey not yet established |
| cli-reauth | /reauth/cli | ceremony/none | web/src/routes/CLIReauth.tsx#CLIReauth | web/src/routes/CLIReauth.stories.tsx | component stories exist; real route/chrome journey not yet established |
| workspace-approve | /workspace/approve | ceremony/none | web/src/routes/WorkspaceApprove.tsx#WorkspaceApprove | web/src/routes/WorkspaceApprove.stories.tsx | component stories exist; real route/chrome journey not yet established |
| workspace-callback | /workspace/callback | public/none | web/src/routes/WorkspaceCallback.tsx#WorkspaceCallback | web/src/routes/WorkspaceCallback.stories.tsx | component stories exist; real route/chrome journey not yet established |
| oidc-done | /auth/oidc/done | public/none | web/src/routes/OIDCDone.tsx#OIDCDone | web/src/routes/OIDCDone.stories.tsx | component stories exist; real route/chrome journey not yet established |
| saml-done | /auth/saml/done | public/none | web/src/routes/SAMLDone.tsx#SAMLDone | none | missing page story coverage |

## Component and composite ownership appendix

| Symbol | Source line | Exported | Direct story modules | Nearest composed modules | Owning tests | Baseline disposition |
| --- | --- | --- | --- | --- | --- | --- |
| WorkspaceContextProvider | web/src/api/transport.tsx:49 | true |  | web/src/routes/RemoteCard.stories.tsx (depth 2), web/src/routes/WorkspaceScope.stories.tsx (depth 3) | web/src/routes/HistoryDrawer.test.tsx, web/src/routes/InstanceConfig.test.tsx, web/src/routes/WorkspaceSettingsLink.test.tsx | composed coverage: specific branch still requires source/state review |
| App | web/src/app/App.tsx:173 | true |  |  | web/src/app/App.gate.test.tsx | needs composition/exclusion audit |
| AuthProvider | web/src/app/AuthProvider.tsx:180 | true |  |  | web/src/app/AuthProvider.test.tsx, web/src/app/RuntimeMaintenanceBoundary.test.tsx, web/src/routes/InstanceAdmin.credential-policy.test.tsx, web/src/routes/InstanceAdmin.crypto.test.tsx, web/src/routes/InstanceConfig.test.tsx, web/src/routes/ProjectSettings.test.tsx, web/src/routes/Projects.test.tsx, web/src/routes/SystemScope.test.tsx, web/src/routes/WorkspaceApprove.lifetime.test.tsx | needs composition/exclusion audit |
| RuntimeMaintenanceBoundary | web/src/app/RuntimeMaintenanceBoundary.tsx:27 | true | web/src/app/RuntimeMaintenanceBoundary.stories.tsx |  | web/src/app/RuntimeMaintenanceBoundary.test.tsx | direct-story coverage |
| RuntimeInterruption | web/src/app/RuntimeMaintenanceBoundary.tsx:136 | false |  | web/src/app/RuntimeMaintenanceBoundary.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ToastViewport | web/src/app/notifications.tsx:75 | true | web/src/app/notifications.stories.tsx | web/src/routes/FleetUpdateNotice.stories.tsx (depth 1) | web/src/app/notifications.test.tsx, web/src/routes/AccountSecurity.test.tsx, web/src/routes/Shell.update.test.tsx | direct-story coverage |
| AccountProfile | web/src/routes/AccountProfile.tsx:11 | true | web/src/routes/AccountProfile.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1) | web/src/routes/AccountProfile.test.tsx | direct-story coverage |
| ProfileForm | web/src/routes/AccountProfile.tsx:25 | false |  | web/src/routes/AccountProfile.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AccountSecurity | web/src/routes/AccountSecurity.tsx:63 | true | web/src/routes/AccountSecurity.stories.tsx |  | web/src/routes/AccountSecurity.display-once-copy.test.tsx, web/src/routes/AccountSecurity.test.tsx | direct-story coverage |
| AccountProofDialog | web/src/routes/AccountSecurity.tsx:627 | false |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| RecoveryCodes | web/src/routes/AccountSecurity.tsx:655 | false |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| PrototypeSessions | web/src/routes/AccountSecurity.tsx:699 | false |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ThemePreference | web/src/routes/AccountSecurity.tsx:738 | false |  | web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| AdaptersPage | web/src/routes/Adapters.tsx:150 | false |  |  | web/src/routes/Adapters.test.tsx, web/src/routes/SystemScope.test.tsx | needs composition/exclusion audit |
| HealthChip | web/src/routes/Adapters.tsx:294 | true | web/src/routes/HealthChip.stories.tsx |  |  | direct-story coverage |
| AdapterPanel | web/src/routes/Adapters.tsx:304 | false |  |  |  | needs composition/exclusion audit |
| OriginMoveForm | web/src/routes/Adapters.tsx:511 | false |  |  |  | needs composition/exclusion audit |
| CredentialForm | web/src/routes/Adapters.tsx:587 | true | web/src/routes/CredentialForm.stories.tsx |  |  | direct-story coverage |
| RevokeCredentialDialog | web/src/routes/Adapters.tsx:639 | true | web/src/routes/RevokeCredentialDialog.stories.tsx |  |  | direct-story coverage |
| DeleteAdapterDialog | web/src/routes/Adapters.tsx:675 | true | web/src/routes/DeleteAdapterDialog.stories.tsx |  |  | direct-story coverage |
| MoveDetail | web/src/routes/Adapters.tsx:752 | false |  |  |  | needs composition/exclusion audit |
| CreateAdapterPanel | web/src/routes/Adapters.tsx:918 | false |  |  |  | needs composition/exclusion audit |
| TargetForm | web/src/routes/Adapters.tsx:1124 | true | web/src/routes/TargetForm.stories.tsx |  | web/src/routes/Adapters.test.tsx | direct-story coverage |
| TargetDetail | web/src/routes/Adapters.tsx:1506 | false |  |  |  | needs composition/exclusion audit |
| ConnectionFacts | web/src/routes/Adapters.tsx:1811 | false |  |  |  | needs composition/exclusion audit |
| PlanChanges | web/src/routes/Adapters.tsx:1827 | false |  |  |  | needs composition/exclusion audit |
| ConflictArtifact | web/src/routes/Adapters.tsx:1856 | false |  |  |  | needs composition/exclusion audit |
| RemoveDialog | web/src/routes/Adapters.tsx:1895 | false |  |  |  | needs composition/exclusion audit |
| AdapterCeremony | web/src/routes/Adapters.tsx:1972 | false |  |  |  | needs composition/exclusion audit |
| Outcome | web/src/routes/Audit.tsx:40 | false |  | web/src/routes/Audit.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| Audit | web/src/routes/Audit.tsx:83 | true | web/src/routes/Audit.stories.tsx |  | web/src/routes/Audit.test.tsx | direct-story coverage |
| AuditTrail | web/src/routes/Audit.tsx:96 | false |  | web/src/routes/Audit.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| AuditFact | web/src/routes/Audit.tsx:413 | false |  | web/src/routes/Audit.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AuditScopeFacts | web/src/routes/Audit.tsx:424 | false |  | web/src/routes/Audit.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AwsAccessFields | web/src/routes/AwsAccessFields.tsx:78 | true |  | web/src/routes/CredentialForm.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CLIReauth | web/src/routes/CLIReauth.tsx:26 | true | web/src/routes/CLIReauth.stories.tsx |  | web/src/routes/CLIReauth.oidc.test.tsx | direct-story coverage |
| CLIReauthMessage | web/src/routes/CLIReauth.tsx:213 | false |  | web/src/routes/CLIReauth.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CatalogueManageDialog | web/src/routes/CatalogueManageDialog.tsx:54 | true | web/src/routes/CatalogueManageDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/CatalogueManageDialog.test.tsx | direct-story coverage |
| CreateFolder | web/src/routes/CatalogueManageDialog.tsx:157 | false |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| FolderRow | web/src/routes/CatalogueManageDialog.tsx:210 | false |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| CreateKeyGroup | web/src/routes/CatalogueManageDialog.tsx:296 | false |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| GroupRow | web/src/routes/CatalogueManageDialog.tsx:348 | false |  | web/src/routes/CatalogueManageDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| Ceremony | web/src/routes/Ceremony.tsx:165 | true | web/src/routes/Ceremony.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/ImportWizard.stories.tsx (depth 1) | web/src/routes/Ceremony.task-key.test.tsx | direct-story coverage |
| WorkspaceStepUp | web/src/routes/Ceremony.tsx:397 | true |  | web/src/routes/Ceremony.stories.tsx (depth 1), web/src/routes/WorkspaceStepUp.stories.tsx (depth 1) | web/src/routes/WorkspaceHandoffConsumers.test.tsx | composed coverage: specific branch still requires source/state review |
| CertificatesTab | web/src/routes/CertificatesTab.tsx:64 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/CertificatesTab.test.tsx | composed coverage: specific branch still requires source/state review |
| IssueDialog | web/src/routes/CertificatesTab.tsx:263 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ChangeApprovals | web/src/routes/ChangeApprovals.tsx:84 | true | web/src/routes/ChangeApprovals.stories.tsx |  | web/src/routes/ChangeApprovals.test.tsx | direct-story coverage |
| ScopedChangeApprovals | web/src/routes/ChangeApprovals.tsx:92 | false |  | web/src/routes/ChangeApprovals.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ApprovalRequestRow | web/src/routes/ChangeApprovals.tsx:458 | false |  | web/src/routes/ChangeApprovals.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PolicyPeople | web/src/routes/ChangeApprovals.tsx:550 | false |  | web/src/routes/ChangeApprovals.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ChromeIdentityControls | web/src/routes/ChromeIdentityControls.tsx:19 | true | web/src/routes/ChromeIdentityControls.stories.tsx | web/src/routes/OrgSettings.stories.tsx (depth 1), web/src/routes/ProjectSettings.stories.tsx (depth 1) | web/src/routes/ChromeIdentityControls.test.tsx | direct-story coverage |
| DefinitionsBundlePanel | web/src/routes/DefinitionsBundlePanel.tsx:28 | true | web/src/routes/DefinitionsBundlePanel.stories.tsx | web/src/routes/ProjectSettings.stories.tsx (depth 1) | web/src/routes/DefinitionsBundlePanel.test.tsx | direct-story coverage |
| BundleDialog | web/src/routes/DefinitionsBundlePanel.tsx:71 | false |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 1), web/src/routes/ProjectSettings.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| WideningConfirm | web/src/routes/DefinitionsBundlePanel.tsx:417 | false |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| LastApplyProvenance | web/src/routes/DefinitionsBundlePanel.tsx:451 | true | web/src/routes/LastApplyProvenance.stories.tsx | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) | web/src/routes/DefinitionsBundlePanel.test.tsx | direct-story coverage |
| DifferenceList | web/src/routes/DefinitionsBundlePanel.tsx:490 | false |  | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/ProjectSettings.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| StateBadge | web/src/routes/DeliveryTargets.tsx:36 | true |  | web/src/routes/DeliveryTargets.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| DeliveryTargetsPanel | web/src/routes/DeliveryTargets.tsx:82 | true | web/src/routes/DeliveryTargets.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/DeliveryTargets.test.tsx | direct-story coverage |
| EnrolmentGate | web/src/routes/EnrolmentGate.tsx:31 | true | web/src/routes/EnrolmentGate.stories.tsx |  | web/src/routes/EnrolmentGate.test.tsx | direct-story coverage |
| EstablishCredential | web/src/routes/EstablishCredential.tsx:33 | true | web/src/routes/EstablishCredential.stories.tsx |  | web/src/routes/EstablishCredential.test.tsx | direct-story coverage |
| RecoveryForm | web/src/routes/EstablishCredential.tsx:216 | false |  | web/src/routes/EstablishCredential.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| FederationIssuersPanel | web/src/routes/FederationIssuersPanel.tsx:60 | true | web/src/routes/FederationIssuersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) |  | direct-story coverage |
| IssuerForm | web/src/routes/FederationIssuersPanel.tsx:325 | false |  | web/src/routes/FederationIssuersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| FolderCleanupDialog | web/src/routes/FolderCleanupDialog.tsx:30 | true | web/src/routes/FolderCleanupDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/FolderCleanupDialog.test.tsx | direct-story coverage |
| HistoryDrawer | web/src/routes/HistoryDrawer.tsx:122 | true | web/src/routes/HistoryDrawer.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/HistoryDrawer.test.tsx | direct-story coverage |
| RevisionDetail | web/src/routes/HistoryDrawer.tsx:937 | false |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| RestoreSheet | web/src/routes/HistoryDrawer.tsx:1165 | false |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PinSheet | web/src/routes/HistoryDrawer.tsx:1320 | false |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ReleaseSheet | web/src/routes/HistoryDrawer.tsx:1528 | false |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PinReleaseOutcome | web/src/routes/HistoryDrawer.tsx:1591 | true | web/src/routes/PinReleaseOutcome.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) | web/src/routes/HistoryDrawer.test.tsx | direct-story coverage |
| ImpactValue | web/src/routes/HistoryDrawer.tsx:1677 | false |  | web/src/routes/HistoryDrawer.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ImportWizard | web/src/routes/ImportWizard.tsx:146 | true | web/src/routes/ImportWizard.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) |  | direct-story coverage |
| InstanceAdmin | web/src/routes/InstanceAdmin.tsx:109 | true | web/src/routes/InstanceAdmin.stories.tsx |  | web/src/routes/InstanceAdmin.credential-policy.test.tsx, web/src/routes/InstanceAdmin.crypto.test.tsx | direct-story coverage |
| CredentialPolicyPanel | web/src/routes/InstanceAdmin.tsx:246 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CryptoMaintenance | web/src/routes/InstanceAdmin.tsx:318 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| InstanceConfig | web/src/routes/InstanceConfig.tsx:18 | true | web/src/routes/InstanceConfig.stories.tsx |  | web/src/routes/InstanceConfig.test.tsx | direct-story coverage |
| ConfigurationRoot | web/src/routes/InstanceConfig.tsx:31 | false |  | web/src/routes/InstanceConfig.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ConfigurationOwner | web/src/routes/InstanceConfig.tsx:52 | false |  | web/src/routes/InstanceConfig.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| SelfConfigCeremony | web/src/routes/InstanceConfig.tsx:209 | false |  | web/src/routes/InstanceConfig.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| SystemProjectNotice | web/src/routes/InstanceConfig.tsx:260 | true | web/src/routes/SystemProjectNotice.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) |  | direct-story coverage |
| InstanceMailPanel | web/src/routes/InstanceMailPanel.tsx:26 | true |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/InstanceMailPanel.test.tsx | composed coverage: specific branch still requires source/state review |
| InviteDialog | web/src/routes/InviteDialog.tsx:32 | true | web/src/routes/InviteDialog.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) | web/src/routes/InviteDialog.test.tsx | direct-story coverage |
| InviteForm | web/src/routes/InviteDialog.tsx:118 | false |  | web/src/routes/InviteDialog.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| IssuedAuthorityDialog | web/src/routes/InviteDialog.tsx:233 | true |  | web/src/routes/InviteDialog.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| KeyDeclarationDetail | web/src/routes/KeyDeclarationDetail.tsx:68 | true | web/src/routes/KeyDeclarationDetail.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/KeyDeclarationDetail.test.tsx | direct-story coverage |
| KeyLoadError | web/src/routes/KeyDeclarationDetail.tsx:236 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| KeyDeclarationBody | web/src/routes/KeyDeclarationDetail.tsx:255 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| Fact | web/src/routes/KeyDeclarationDetail.tsx:441 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ValueRules | web/src/routes/KeyDeclarationDetail.tsx:476 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RuleLine | web/src/routes/KeyDeclarationDetail.tsx:499 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| MetadataEditor | web/src/routes/KeyDeclarationDetail.tsx:535 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RenameKey | web/src/routes/KeyDeclarationDetail.tsx:788 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ReclassifyKey | web/src/routes/KeyDeclarationDetail.tsx:901 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| DeleteKey | web/src/routes/KeyDeclarationDetail.tsx:1057 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ImpactPreview | web/src/routes/KeyDeclarationDetail.tsx:1127 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| ConfirmDialog | web/src/routes/KeyDeclarationDetail.tsx:1167 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| Toggle | web/src/routes/KeyDeclarationDetail.tsx:1417 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 4), web/src/routes/Matrix.stories.tsx (depth 5) |  | composed coverage: specific branch still requires source/state review |
| DeclarationEditor | web/src/routes/KeyDeclarationDetail.tsx:1450 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RuleFields | web/src/routes/KeyDeclarationDetail.tsx:1769 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| PresenceControl | web/src/routes/KeyDeclarationDetail.tsx:1894 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 3), web/src/routes/Matrix.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| GroupEditor | web/src/routes/KeyDeclarationDetail.tsx:1950 | false |  | web/src/routes/KeyDeclarationDetail.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| Login | web/src/routes/Login.tsx:106 | true | web/src/routes/Login.stories.tsx | web/src/routes/CLIReauth.stories.tsx (depth 1), web/src/routes/WorkspaceApprove.stories.tsx (depth 1) | web/src/routes/Login.oidc.test.tsx | direct-story coverage |
| MachineAccessPage | web/src/routes/MachineAccess.tsx:140 | true | web/src/routes/MachineAccess.stories.tsx |  | web/src/routes/MachineAccess.dialog-reset.test.tsx, web/src/routes/SystemScope.test.tsx | direct-story coverage |
| Matrix | web/src/routes/Matrix.tsx:110 | true | web/src/routes/Matrix.stories.tsx |  | web/src/routes/Matrix.degraded.test.tsx, web/src/routes/Matrix.git-managed.test.tsx, web/src/routes/Matrix.mutation-error.test.tsx | direct-story coverage |
| MatrixCell | web/src/routes/Matrix.tsx:1612 | false |  | web/src/routes/Matrix.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| MatrixLegend | web/src/routes/Matrix.tsx:1736 | false |  | web/src/routes/Matrix.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| MatrixKeyCreate | web/src/routes/MatrixKeyCreate.tsx:104 | true | web/src/routes/MatrixKeyCreate.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/MatrixKeyCreate.test.tsx | direct-story coverage |
| PresenceField | web/src/routes/MatrixKeyCreate.tsx:579 | false |  | web/src/routes/MatrixKeyCreate.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| MatrixPublishSheet | web/src/routes/MatrixPublishSheet.tsx:60 | true | web/src/routes/MatrixPublishSheet.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/MatrixPublishSheet.validation.test.tsx | direct-story coverage |
| MatrixRowEditor | web/src/routes/MatrixRowEditor.tsx:67 | true | web/src/routes/MatrixRowEditor.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/MatrixRowEditor.test.tsx, web/src/routes/secret-focus.test.tsx | direct-story coverage |
| Members | web/src/routes/Members.tsx:156 | true | web/src/routes/Members.stories.tsx |  | web/src/routes/AccessibilityPolish.test.tsx, web/src/routes/Members.delegated.test.tsx, web/src/routes/Members.instance.test.tsx, web/src/routes/Members.rule-outcomes.test.tsx | direct-story coverage |
| Inspect | web/src/routes/Members.tsx:771 | false |  | web/src/routes/Members.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| GrantModal | web/src/routes/Members.tsx:1046 | true |  | web/src/routes/Members.stories.tsx (depth 1) | web/src/routes/Members.grant-lifetime.test.tsx | composed coverage: specific branch still requires source/state review |
| OAuth2ProvidersPanel | web/src/routes/OAuth2ProvidersPanel.tsx:21 | true |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/OAuth2ProvidersPanel.test.tsx | composed coverage: specific branch still requires source/state review |
| OAuth2Editor | web/src/routes/OAuth2ProvidersPanel.tsx:149 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| OIDCDone | web/src/routes/OIDCDone.tsx:32 | true | web/src/routes/OIDCDone.stories.tsx |  | web/src/routes/OIDCDone.test.tsx | direct-story coverage |
| OidcProvidersPanel | web/src/routes/OidcProvidersPanel.tsx:74 | true | web/src/routes/OidcProvidersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/OidcProvidersPanel.test.tsx | direct-story coverage |
| ProviderEditor | web/src/routes/OidcProvidersPanel.tsx:231 | false |  | web/src/routes/OidcProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| DeleteProviderDialog | web/src/routes/OidcProvidersPanel.tsx:410 | false |  | web/src/routes/OidcProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| OpenRegistrationPanel | web/src/routes/OpenRegistration.tsx:58 | true |  | web/src/routes/Members.stories.tsx (depth 1) | web/src/routes/OpenRegistration.test.tsx | composed coverage: specific branch still requires source/state review |
| ScopedOpenRegistrationPanel | web/src/routes/OpenRegistration.tsx:63 | false |  | web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PolicySummary | web/src/routes/OpenRegistration.tsx:220 | false |  | web/src/routes/Members.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RegistrationEditor | web/src/routes/OpenRegistration.tsx:410 | false |  | web/src/routes/Members.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ChromeDiagnostic | web/src/routes/OpsDiagnosticBanners.tsx:12 | true | web/src/routes/OpsDiagnosticBanners.stories.tsx |  |  | direct-story coverage |
| OpsDiagnosticBanners | web/src/routes/OpsDiagnosticBanners.tsx:30 | true |  |  | web/src/routes/OpsDiagnosticBanners.test.tsx | needs composition/exclusion audit |
| OrgSettings | web/src/routes/OrgSettings.tsx:36 | true | web/src/routes/OrgSettings.stories.tsx |  | web/src/routes/AccessibilityPolish.test.tsx | direct-story coverage |
| NameEditor | web/src/routes/OrgSettings.tsx:198 | false |  | web/src/routes/OrgSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ProjectRetentionList | web/src/routes/OrgSettings.tsx:234 | false |  | web/src/routes/OrgSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CompactOrgRetention | web/src/routes/OrgSettings.tsx:306 | true | web/src/routes/CompactOrgRetention.stories.tsx | web/src/routes/OrgSettings.stories.tsx (depth 1) | web/src/routes/OrgSettings.test.tsx | direct-story coverage |
| PkiIssuersPanel | web/src/routes/PkiIssuersPanel.tsx:41 | true |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/PkiIssuersPanel.test.tsx | composed coverage: specific branch still requires source/state review |
| IssuerRow | web/src/routes/PkiIssuersPanel.tsx:111 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| CreateIssuerForm | web/src/routes/PkiIssuersPanel.tsx:354 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PkiProfilesPanel | web/src/routes/PkiProfilesPanel.tsx:53 | true |  | web/src/routes/InstanceAdmin.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ProfileRow | web/src/routes/PkiProfilesPanel.tsx:140 | false |  | web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| Placeholder | web/src/routes/Placeholder.tsx:17 | false |  | web/src/routes/Placeholder.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| Overview | web/src/routes/Placeholder.tsx:36 | true |  |  | web/src/routes/Projects.test.tsx | needs composition/exclusion audit |
| NotFound | web/src/routes/Placeholder.tsx:59 | true | web/src/routes/Placeholder.stories.tsx |  |  | direct-story coverage |
| ProjectSettings | web/src/routes/ProjectSettings.tsx:64 | true | web/src/routes/ProjectSettings.stories.tsx |  | web/src/routes/ProjectSettings.test.tsx | direct-story coverage |
| NameEditor | web/src/routes/ProjectSettings.tsx:318 | false |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ProjectCryptoMaintenance | web/src/routes/ProjectSettings.tsx:361 | false |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| DefinitionsPolicy | web/src/routes/ProjectSettings.tsx:448 | false |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| NewEnvironment | web/src/routes/ProjectSettings.tsx:501 | false |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| EnvironmentLifecycleActions | web/src/routes/ProjectSettings.tsx:595 | true |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | web/src/routes/ProjectSettings.test.tsx | composed coverage: specific branch still requires source/state review |
| EnvironmentPolicy | web/src/routes/ProjectSettings.tsx:807 | false |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CompactProjectRetention | web/src/routes/ProjectSettings.tsx:941 | true |  | web/src/routes/ProjectSettings.stories.tsx (depth 1) | web/src/routes/ProjectSettings.retention.test.tsx, web/src/routes/ProjectSettings.test.tsx | composed coverage: specific branch still requires source/state review |
| Projects | web/src/routes/Projects.tsx:12 | true | web/src/routes/Projects.stories.tsx |  | web/src/routes/Projects.test.tsx | direct-story coverage |
| ProjectList | web/src/routes/Projects.tsx:74 | true |  | web/src/routes/Projects.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| NewProjectForm | web/src/routes/Projects.tsx:119 | true |  | web/src/routes/Projects.stories.tsx (depth 1) | web/src/routes/Projects.test.tsx | composed coverage: specific branch still requires source/state review |
| ProviderDiscoveryAlert | web/src/routes/ProviderDiscoveryAlert.tsx:4 | true | web/src/routes/ProviderDiscoveryAlert.stories.tsx | web/src/routes/CLIReauth.stories.tsx (depth 1), web/src/routes/Ceremony.stories.tsx (depth 1) |  | direct-story coverage |
| Remotes | web/src/routes/Remotes.tsx:84 | true |  |  | web/src/routes/Remotes.test.tsx | needs composition/exclusion audit |
| ThisInstance | web/src/routes/Remotes.tsx:135 | true |  |  | web/src/routes/Remotes.test.tsx | needs composition/exclusion audit |
| RemoteCard | web/src/routes/Remotes.tsx:172 | true | web/src/routes/RemoteCard.stories.tsx |  |  | direct-story coverage |
| UpdateJobStatus | web/src/routes/Remotes.tsx:424 | true | web/src/routes/UpdateJobStatus.stories.tsx | web/src/routes/RemoteCard.stories.tsx (depth 1) | web/src/routes/Remotes.test.tsx | direct-story coverage |
| Absent | web/src/routes/Remotes.tsx:451 | false |  | web/src/routes/RemoteCard.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| AddRemote | web/src/routes/Remotes.tsx:455 | true |  |  | web/src/routes/Remotes.test.tsx | needs composition/exclusion audit |
| OriginAllowlist | web/src/routes/Remotes.tsx:568 | false |  |  |  | needs composition/exclusion audit |
| ConnectionCredentials | web/src/routes/Remotes.tsx:657 | true |  |  | web/src/routes/Remotes.connections.test.tsx | needs composition/exclusion audit |
| ConnectionRow | web/src/routes/Remotes.tsx:713 | true | web/src/routes/ConnectionRow.stories.tsx |  |  | direct-story coverage |
| MintConnectionForm | web/src/routes/Remotes.tsx:779 | true | web/src/routes/MintConnectionForm.stories.tsx |  |  | direct-story coverage |
| ConnectionMintDialog | web/src/routes/Remotes.tsx:897 | true | web/src/routes/ConnectionMintDialog.stories.tsx |  |  | direct-story coverage |
| RevokeConnectionDialog | web/src/routes/Remotes.tsx:997 | true | web/src/routes/RevokeConnectionDialog.stories.tsx |  |  | direct-story coverage |
| WorkspacePicker | web/src/routes/Remotes.tsx:1139 | false |  | web/src/routes/RemoteCard.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| PickerBody | web/src/routes/Remotes.tsx:1155 | false |  | web/src/routes/RemoteCard.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| OrgProjects | web/src/routes/Remotes.tsx:1186 | false |  | web/src/routes/RemoteCard.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RetentionBoundsFields | web/src/routes/RetentionBoundsFields.tsx:8 | true | web/src/routes/RetentionBoundsFields.stories.tsx |  | web/src/routes/RetentionEditors.test.tsx | direct-story coverage |
| RevisionDiffDialog | web/src/routes/RevisionDiff.tsx:13 | true | web/src/routes/RevisionDiff.stories.tsx | web/src/routes/HistoryDrawer.stories.tsx (depth 2), web/src/routes/Matrix.stories.tsx (depth 3) | web/src/routes/RevisionDiff.test.tsx | direct-story coverage |
| SAMLDone | web/src/routes/SAMLDone.tsx:8 | true |  |  | web/src/routes/SAMLDone.test.tsx | needs composition/exclusion audit |
| SSHCertificatesPanel | web/src/routes/SSHCertificates.tsx:110 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CADialog | web/src/routes/SSHCertificates.tsx:433 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| TrustDialog | web/src/routes/SSHCertificates.tsx:546 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| DeleteCADialog | web/src/routes/SSHCertificates.tsx:604 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ProfileDialog | web/src/routes/SSHCertificates.tsx:655 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| DeleteProfileDialog | web/src/routes/SSHCertificates.tsx:832 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| IssueDialog | web/src/routes/SSHCertificates.tsx:893 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| SamlProvidersPanel | web/src/routes/SamlProvidersPanel.tsx:42 | true | web/src/routes/SamlProvidersPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/SamlAdmin.test.tsx | direct-story coverage |
| ProviderRow | web/src/routes/SamlProvidersPanel.tsx:128 | false |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ProviderPolicyForm | web/src/routes/SamlProvidersPanel.tsx:248 | false |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| MetadataDiff | web/src/routes/SamlProvidersPanel.tsx:323 | false |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RefreshMetadataForm | web/src/routes/SamlProvidersPanel.tsx:364 | false |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| ProviderCreateForm | web/src/routes/SamlProvidersPanel.tsx:475 | false |  | web/src/routes/SamlProvidersPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| SamlSpKeysPanel | web/src/routes/SamlSpKeysPanel.tsx:30 | true | web/src/routes/SamlSpKeysPanel.stories.tsx | web/src/routes/InstanceAdmin.stories.tsx (depth 1) | web/src/routes/SamlAdmin.test.tsx | direct-story coverage |
| SpKeyRow | web/src/routes/SamlSpKeysPanel.tsx:104 | false |  | web/src/routes/SamlSpKeysPanel.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ScanBlockDialog | web/src/routes/ScanBlockDialog.tsx:23 | true | web/src/routes/ScanBlockDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1), web/src/routes/CatalogueManageDialog.stories.tsx (depth 2) | web/src/routes/ScanBlockDialog.test.tsx | direct-story coverage |
| ScanWarnDialog | web/src/routes/ScanWarnDialog.tsx:48 | true | web/src/routes/ScanWarnDialog.stories.tsx | web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/ScanWarnDialog.test.tsx | direct-story coverage |
| ScimProvisioningPage | web/src/routes/ScimProvisioning.tsx:75 | true | web/src/routes/ScimProvisioning.stories.tsx |  | web/src/routes/ScimProvisioning.secret-lifetime.test.tsx, web/src/routes/SystemScope.test.tsx | direct-story coverage |
| BindingsSection | web/src/routes/ScimProvisioning.tsx:150 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| BindingCard | web/src/routes/ScimProvisioning.tsx:199 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AttentionList | web/src/routes/ScimProvisioning.tsx:247 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| DeleteBinding | web/src/routes/ScimProvisioning.tsx:271 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| CreateBindingForm | web/src/routes/ScimProvisioning.tsx:306 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| MappingsSection | web/src/routes/ScimProvisioning.tsx:398 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| MappingRow | web/src/routes/ScimProvisioning.tsx:478 | true |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | web/src/routes/ScimProvisioning.retarget.test.tsx | composed coverage: specific branch still requires source/state review |
| MappingWarnings | web/src/routes/ScimProvisioning.tsx:642 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| CreateMappingForm | web/src/routes/ScimProvisioning.tsx:664 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| CredentialsSection | web/src/routes/ScimProvisioning.tsx:804 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| CredentialRow | web/src/routes/ScimProvisioning.tsx:839 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| MintCredentialForm | web/src/routes/ScimProvisioning.tsx:896 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| MintDialog | web/src/routes/ScimProvisioning.tsx:968 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| DirectorySection | web/src/routes/ScimProvisioning.tsx:1056 | false |  | web/src/routes/ScimProvisioning.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| DirectoryUserRow | web/src/routes/ScimProvisioning.tsx:1123 | true |  | web/src/routes/ScimProvisioning.stories.tsx (depth 2) | web/src/routes/ScimProvisioning.directory.test.tsx | composed coverage: specific branch still requires source/state review |
| JumpIndex | web/src/routes/Sections.tsx:21 | true | web/src/routes/Sections.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/InstanceAdmin.stories.tsx (depth 1) |  | direct-story coverage |
| Panel | web/src/routes/Sections.tsx:44 | true | web/src/routes/Sections.stories.tsx, web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/AccountProfile.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | direct-story coverage |
| TypedNameConfirm | web/src/routes/Sections.tsx:85 | true | web/src/routes/Sections.stories.tsx | web/src/routes/DeleteAccountDialog.stories.tsx (depth 1), web/src/routes/DeleteProviderDialog.stories.tsx (depth 1) |  | direct-story coverage |
| ConsequencesDialog | web/src/routes/Sections.tsx:151 | true | web/src/routes/Sections.stories.tsx | web/src/routes/DefinitionsBundlePanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | direct-story coverage |
| Explain | web/src/routes/Sections.tsx:206 | true | web/src/routes/Sections.stories.tsx | web/src/routes/Members.stories.tsx (depth 2), web/src/routes/ScimProvisioning.stories.tsx (depth 3) |  | direct-story coverage |
| DisplayOnceCopy | web/src/routes/Sections.tsx:227 | true | web/src/routes/Sections.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/InviteDialog.stories.tsx (depth 2) |  | direct-story coverage |
| Shell | web/src/routes/Shell.tsx:123 | true |  |  |  | needs composition/exclusion audit |
| SidebarLinkItem | web/src/routes/Shell.tsx:652 | true | web/src/routes/SidebarLinkItem.stories.tsx |  |  | direct-story coverage |
| SidebarSection | web/src/routes/Shell.tsx:694 | false |  |  |  | needs composition/exclusion audit |
| InstanceContext | web/src/routes/Shell.tsx:721 | false |  |  |  | needs composition/exclusion audit |
| ProjectContext | web/src/routes/Shell.tsx:740 | true |  |  | web/src/routes/Shell.chrome.test.tsx | needs composition/exclusion audit |
| SidebarVersion | web/src/routes/Shell.tsx:841 | true | web/src/routes/SidebarVersion.stories.tsx |  |  | direct-story coverage |
| AccountEntry | web/src/routes/Shell.tsx:853 | true |  |  | web/src/routes/Shell.account.test.tsx, web/src/routes/Shell.test.tsx | needs composition/exclusion audit |
| ProfileUpdateBadge | web/src/routes/Shell.tsx:1018 | true | web/src/routes/ProfileUpdateBadge.stories.tsx |  | web/src/routes/Shell.update.test.tsx | direct-story coverage |
| ThemeToggle | web/src/routes/Shell.tsx:1038 | true | web/src/routes/ThemeToggle.stories.tsx |  |  | direct-story coverage |
| SignupVerify | web/src/routes/SignupVerify.tsx:13 | true |  |  | web/src/routes/SignupVerify.test.tsx | needs composition/exclusion audit |
| StepUpBanner | web/src/routes/StepUpBanner.tsx:31 | true | web/src/routes/StepUpBanner.stories.tsx |  |  | direct-story coverage |
| SystemScopeRefusal | web/src/routes/SystemScope.tsx:47 | true | web/src/routes/SystemScopeRefusal.stories.tsx |  |  | direct-story coverage |
| TemporaryAccess | web/src/routes/TemporaryAccess.tsx:126 | true | web/src/routes/TemporaryAccess.stories.tsx |  | web/src/routes/TemporaryAccess.test.tsx | direct-story coverage |
| ScopedTemporaryAccess | web/src/routes/TemporaryAccess.tsx:134 | false |  | web/src/routes/TemporaryAccess.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| AccessRequestRow | web/src/routes/TemporaryAccess.tsx:634 | false |  | web/src/routes/TemporaryAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| Values | web/src/routes/Values.tsx:87 | true | web/src/routes/Values.stories.tsx |  | web/src/routes/Values.ceremony-task.test.tsx, web/src/routes/Values.write-feedback.test.tsx, web/src/routes/secret-focus.test.tsx | direct-story coverage |
| ValuesSurface | web/src/routes/Values.tsx:109 | false |  | web/src/routes/Values.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| RowEditor | web/src/routes/Values.tsx:805 | false |  | web/src/routes/Values.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| WorkspaceApprove | web/src/routes/WorkspaceApprove.tsx:96 | true | web/src/routes/WorkspaceApprove.stories.tsx |  | web/src/routes/WorkspaceApprove.lifetime.test.tsx, web/src/routes/WorkspaceApprove.test.tsx | direct-story coverage |
| StepUpReauth | web/src/routes/WorkspaceApprove.tsx:322 | false |  | web/src/routes/WorkspaceApprove.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| WorkspaceCallback | web/src/routes/WorkspaceCallback.tsx:26 | true | web/src/routes/WorkspaceCallback.stories.tsx |  | web/src/routes/WorkspaceCallback.test.tsx | direct-story coverage |
| WorkspaceScope | web/src/routes/WorkspaceScope.tsx:40 | true | web/src/routes/WorkspaceScope.stories.tsx |  | web/src/routes/WorkspaceScope.test.tsx | direct-story coverage |
| WorkspaceBoundary | web/src/routes/WorkspaceScope.tsx:62 | false |  | web/src/routes/WorkspaceScope.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ConnectedWorkspace | web/src/routes/WorkspaceScope.tsx:144 | false |  | web/src/routes/WorkspaceScope.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| WorkspaceBanner | web/src/routes/WorkspaceScope.tsx:216 | false |  | web/src/routes/WorkspaceScope.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| Reconnect | web/src/routes/WorkspaceScope.tsx:243 | true | web/src/routes/Reconnect.stories.tsx | web/src/routes/WorkspaceScope.stories.tsx (depth 2) | web/src/routes/WorkspaceHandoffConsumers.test.tsx | direct-story coverage |
| WorkspaceSettingsLink | web/src/routes/WorkspaceSettingsLink.tsx:6 | true |  | web/src/routes/HistoryDrawer.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 1) | web/src/routes/WorkspaceSettingsLink.test.tsx | composed coverage: specific branch still requires source/state review |
| Terms | web/src/routes/accessRules/AccessGlossary.tsx:7 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AccessGlossary | web/src/routes/accessRules/AccessGlossary.tsx:26 | true | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) |  | direct-story coverage |
| ExistingAccessCard | web/src/routes/accessRules/ExistingAccess.tsx:12 | true |  | web/src/routes/Members.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| KeyMoveConfirmDialog | web/src/routes/accessRules/KeyMoveConfirmDialog.tsx:38 | true | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/FolderCleanupDialog.stories.tsx (depth 1), web/src/routes/Matrix.stories.tsx (depth 2) |  | direct-story coverage |
| PermissionList | web/src/routes/accessRules/PermissionList.tsx:20 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PermRow | web/src/routes/accessRules/PermissionList.tsx:51 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RuleEditorDialog | web/src/routes/accessRules/RuleEditorDialog.tsx:48 | true | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) |  | direct-story coverage |
| AxisBox | web/src/routes/accessRules/RuleEditorDialog.tsx:202 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AxisChip | web/src/routes/accessRules/RuleEditorDialog.tsx:214 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| AxisEditor | web/src/routes/accessRules/RuleEditorDialog.tsx:222 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| RulesPanel | web/src/routes/accessRules/RulesPanel.tsx:18 | true | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) |  | direct-story coverage |
| WhoCan | web/src/routes/accessRules/WhoCan.tsx:29 | true | web/src/routes/accessRules/AccessRules.stories.tsx | web/src/routes/Members.stories.tsx (depth 1) |  | direct-story coverage |
| AnswerTable | web/src/routes/accessRules/WhoCan.tsx:172 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| PermBadge | web/src/routes/accessRules/parts.tsx:8 | true |  | web/src/routes/Members.stories.tsx (depth 2), web/src/routes/accessRules/AccessRules.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| Lock | web/src/routes/accessRules/parts.tsx:22 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| EnvList | web/src/routes/accessRules/parts.tsx:34 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| KeyLabel | web/src/routes/accessRules/parts.tsx:53 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| KeyList | web/src/routes/accessRules/parts.tsx:63 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| Except | web/src/routes/accessRules/parts.tsx:85 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| WherePart | web/src/routes/accessRules/parts.tsx:95 | false |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 3), web/src/routes/Members.stories.tsx (depth 4) |  | composed coverage: specific branch still requires source/state review |
| RuleWhere | web/src/routes/accessRules/parts.tsx:109 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RulePerms | web/src/routes/accessRules/parts.tsx:149 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 2), web/src/routes/Members.stories.tsx (depth 3) |  | composed coverage: specific branch still requires source/state review |
| RuleSummary | web/src/routes/accessRules/parts.tsx:165 | true |  | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| CreateAccountDialog | web/src/routes/machineAccess/AccountDialogs.tsx:29 | true | web/src/routes/CreateAccountDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| DeleteAccountDialog | web/src/routes/machineAccess/AccountDialogs.tsx:154 | true | web/src/routes/DeleteAccountDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| ExpandableRow | web/src/routes/machineAccess/AccountRows.tsx:16 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| ExpansionBody | web/src/routes/machineAccess/AccountRows.tsx:103 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| JourneyActionButton | web/src/routes/machineAccess/AccountRows.tsx:304 | false |  | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| ExpiryBadge | web/src/routes/machineAccess/Credentials.tsx:23 | true |  | web/src/routes/BindingCard.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| MintDialog | web/src/routes/machineAccess/Credentials.tsx:44 | true | web/src/routes/MintDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/MintDialog.test.tsx | direct-story coverage |
| LeaseMintDialog | web/src/routes/machineAccess/DynamicLeases.tsx:64 | true | web/src/routes/LeaseMintDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| LeaseActionDialog | web/src/routes/machineAccess/DynamicLeases.tsx:358 | true | web/src/routes/LeaseActionDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| CreateProviderDialog | web/src/routes/machineAccess/DynamicProviders.tsx:60 | true | web/src/routes/CreateProviderDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/CreateProviderDialog.test.tsx | direct-story coverage |
| SetCredentialDialog | web/src/routes/machineAccess/DynamicProviders.tsx:209 | true | web/src/routes/SetCredentialDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| RevokeCredentialDialog | web/src/routes/machineAccess/DynamicProviders.tsx:310 | true | web/src/routes/MachineRevokeCredentialDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| DeleteProviderDialog | web/src/routes/machineAccess/DynamicProviders.tsx:388 | true | web/src/routes/DeleteProviderDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| GrantDialog | web/src/routes/machineAccess/EnvironmentGrants.tsx:50 | true | web/src/routes/GrantDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/GrantDialog.test.tsx | direct-story coverage |
| GrantBody | web/src/routes/machineAccess/EnvironmentGrants.tsx:147 | false |  | web/src/routes/GrantDialog.stories.tsx (depth 1), web/src/routes/MachineAccess.stories.tsx (depth 2) |  | composed coverage: specific branch still requires source/state review |
| BindingCard | web/src/routes/machineAccess/FederationBindings.tsx:33 | true | web/src/routes/BindingCard.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | direct-story coverage |
| BindingDialog | web/src/routes/machineAccess/FederationBindings.tsx:227 | true | web/src/routes/BindingDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 1) | web/src/routes/BindingDialog.test.tsx | direct-story coverage |
| PolicyStrip | web/src/routes/machineAccess/MachineRevealPolicy.tsx:22 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| MachineRevealDialog | web/src/routes/machineAccess/MachineRevealPolicy.tsx:98 | true | web/src/routes/MachineRevealDialog.stories.tsx | web/src/routes/MachineAccess.stories.tsx (depth 2) |  | direct-story coverage |
| Alert | web/src/ui/Alert.tsx:36 | true | web/src/ui/Alert.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/AccountProfile.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | direct-story coverage |
| Badge | web/src/ui/Badge.tsx:20 | true | web/src/ui/Badge.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/BindingCard.stories.tsx (depth 1) |  | direct-story coverage |
| Button | web/src/ui/Button.tsx:27 | true | web/src/ui/Alert.stories.tsx, web/src/ui/Button.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/AccountProfile.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | direct-story coverage |
| CeremonyNotice | web/src/ui/CeremonyNotice.tsx:4 | true |  | web/src/routes/Ceremony.stories.tsx (depth 1), web/src/routes/ConnectionMintDialog.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| Checkbox | web/src/ui/Checkbox.tsx:18 | true | web/src/ui/Checkbox.stories.tsx, web/src/ui/Dialog.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/routes/BindingDialog.stories.tsx (depth 1) |  | direct-story coverage |
| ChoiceGroup | web/src/ui/ChoiceGroup.tsx:26 | true | web/src/ui/ChoiceGroup.stories.tsx | web/src/routes/MatrixPublishSheet.stories.tsx (depth 1), web/src/routes/MatrixRowEditor.stories.tsx (depth 1) |  | direct-story coverage |
| Dialog | web/src/ui/Dialog.tsx:44 | true | web/src/ui/Dialog.stories.tsx | web/src/routes/BindingDialog.stories.tsx (depth 1), web/src/routes/CatalogueManageDialog.stories.tsx (depth 1) |  | direct-story coverage |
| Disclosure | web/src/ui/Disclosure.tsx:18 | true | web/src/ui/Disclosure.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | direct-story coverage |
| Field | web/src/ui/Field.tsx:46 | true | web/src/ui/Field.stories.tsx | web/src/routes/MatrixRowEditor.stories.tsx (depth 1), web/src/ui/Dialog.stories.tsx (depth 1) |  | direct-story coverage |
| Glyph | web/src/ui/Glyph.tsx:32 | true | web/src/ui/Glyph.stories.tsx | web/src/app/notifications.stories.tsx (depth 1), web/src/routes/AccountSecurity.stories.tsx (depth 1) |  | direct-story coverage |
| Input | web/src/ui/Input.tsx:23 | true | web/src/ui/Dialog.stories.tsx, web/src/ui/Input.stories.tsx | web/src/routes/Members.stories.tsx (depth 1), web/src/routes/MintConnectionForm.stories.tsx (depth 1) |  | direct-story coverage |
| Menu | web/src/ui/Menu.tsx:29 | true | web/src/ui/Menu.stories.tsx |  |  | direct-story coverage |
| MenuItem | web/src/ui/Menu.tsx:162 | true | web/src/ui/Menu.stories.tsx |  |  | direct-story coverage |
| Radio | web/src/ui/Radio.tsx:17 | true | web/src/ui/ChoiceGroup.stories.tsx, web/src/ui/Radio.stories.tsx | web/src/routes/MintConnectionForm.stories.tsx (depth 1), web/src/routes/MatrixKeyCreate.stories.tsx (depth 2) |  | direct-story coverage |
| Select | web/src/ui/Select.tsx:15 | true | web/src/ui/Dialog.stories.tsx, web/src/ui/Select.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/InviteDialog.stories.tsx (depth 2) |  | direct-story coverage |
| Tabs | web/src/ui/Tabs.tsx:16 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1), web/src/ui/Tabs.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| TabPanel | web/src/ui/Tabs.tsx:80 | true |  | web/src/routes/MachineAccess.stories.tsx (depth 1), web/src/ui/Tabs.stories.tsx (depth 1) |  | composed coverage: specific branch still requires source/state review |
| Textarea | web/src/ui/Textarea.tsx:16 | true | web/src/ui/Textarea.stories.tsx | web/src/routes/FederationIssuersPanel.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | direct-story coverage |
| ThemeIcon | web/src/ui/ThemeIcon.tsx:13 | true | web/src/ui/Button.stories.tsx, web/src/ui/ThemeIcon.stories.tsx | web/src/routes/ThemeToggle.stories.tsx (depth 1) |  | direct-story coverage |
| ToggleChip | web/src/ui/ToggleChip.tsx:38 | true | web/src/ui/ToggleChip.stories.tsx | web/src/routes/accessRules/AccessRules.stories.tsx (depth 1), web/src/routes/Members.stories.tsx (depth 2) |  | direct-story coverage |
| AuthenticatorCodeField | web/src/ui/auth/AuthenticatorCodeField.tsx:12 | true | web/src/ui/auth/AuthenticatorCodeField.stories.tsx | web/src/ui/auth/SecondFactorChallenge.stories.tsx (depth 1), web/src/ui/auth/SecondFactorSetup.stories.tsx (depth 1) |  | direct-story coverage |
| LocalSignupForm | web/src/ui/auth/LocalSignupForm.tsx:10 | true |  | web/src/ui/auth/LoginForm.stories.tsx (depth 1), web/src/routes/Login.stories.tsx (depth 2) | web/src/ui/auth/LocalSignupForm.test.tsx | composed coverage: specific branch still requires source/state review |
| LoginForm | web/src/ui/auth/LoginForm.tsx:94 | true | web/src/ui/auth/LoginForm.stories.tsx | web/src/routes/Login.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) |  | direct-story coverage |
| ProofDialog | web/src/ui/auth/ProofDialog.tsx:28 | true | web/src/ui/auth/ProofDialog.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 2), web/src/routes/InstanceAdmin.stories.tsx (depth 2) |  | direct-story coverage |
| ProviderButton | web/src/ui/auth/ProviderButton.tsx:84 | true | web/src/ui/auth/ProviderButton.stories.tsx | web/src/ui/auth/LoginForm.stories.tsx (depth 1), web/src/routes/Login.stories.tsx (depth 2) |  | direct-story coverage |
| QrCode | web/src/ui/auth/QrCode.tsx:15 | true | web/src/ui/auth/QrCode.stories.tsx | web/src/routes/AccountSecurity.stories.tsx (depth 1), web/src/ui/auth/SecondFactorSetup.stories.tsx (depth 1) |  | direct-story coverage |
| SecondFactorChallenge | web/src/ui/auth/SecondFactorChallenge.tsx:18 | true | web/src/ui/auth/SecondFactorChallenge.stories.tsx | web/src/routes/Login.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) |  | direct-story coverage |
| SecondFactorSetup | web/src/ui/auth/SecondFactorSetup.tsx:37 | true | web/src/ui/auth/SecondFactorSetup.stories.tsx | web/src/routes/EnrolmentGate.stories.tsx (depth 1), web/src/ui/auth/LoginFlow.stories.tsx (depth 1) |  | direct-story coverage |

## Baseline generated descriptions, prop types/defaults and controls

| Story module | Extracted component | Component description | Extracted owned props / defaults | Explicit args/controls |
| --- | --- | --- | --- | --- |
| web/src/app/RuntimeMaintenanceBoundary.stories.tsx | web/src/app/RuntimeMaintenanceBoundary.tsx#RuntimeMaintenanceBoundary | present | children: ReactNode; failure: Error \| null; refreshSession: (signal?: AbortSignal) => Promise<void>; queries: QueryClient | args present; docgen-only controls |
| web/src/app/notifications.stories.tsx | web/src/app/notifications.tsx#ToastViewport | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/AccountProfile.stories.tsx | web/src/routes/AccountProfile.tsx#AccountProfile | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/AccountSecurity.stories.tsx | web/src/routes/AccountSecurity.tsx#AccountSecurity | present |  | no explicit args; docgen-only controls |
| web/src/routes/Audit.stories.tsx | web/src/routes/Audit.tsx#Audit | present |  | no explicit args; docgen-only controls |
| web/src/routes/BindingCard.stories.tsx | web/src/routes/machineAccess/FederationBindings.tsx#BindingCard | empty; source-derived description needed | account: z.infer<typeof zServiceAccountList>['items'][number]; credential: z.infer<typeof zMachineCredentialList>['items'][number]; now: Date; ready: boolean; onReplace: (credential: MachineCredential) => void; onRevoke: (credential: MachineCredential) => void | args present; docgen-only controls |
| web/src/routes/BindingDialog.stories.tsx | web/src/routes/machineAccess/FederationBindings.tsx#BindingDialog | present | project: { org: string; project: string }; accounts: unknown; initial: z.infer<typeof zServiceAccountList>['items'][number]; replaces: z.infer<typeof zMachineCredentialList>['items'][number]; reachFor: (accountId: string) => readonly MachineDisclosureReach[]; onClose: () => void; onCreated: (message: string) => void | args present; docgen-only controls |
| web/src/routes/CLIReauth.stories.tsx | web/src/routes/CLIReauth.tsx#CLIReauth | present |  | no explicit args; docgen-only controls |
| web/src/routes/CatalogueManageDialog.stories.tsx | web/src/routes/CatalogueManageDialog.tsx#CatalogueManageDialog | present | refData: { readonly org: string; readonly project: string }; onClose: () => void | args present; docgen-only controls |
| web/src/routes/Ceremony.stories.tsx | web/src/routes/Ceremony.tsx#Ceremony | present | request: {   purpose: CeremonyPurpose;   /** Each key's classification, so the list can mark the secrets. */   /** The environment the decision authorises, by id. */   environmentId: string;   /** The environment's human name, for the title. */   environmentName: string;   /** The enumerated unit: what the modal lists and what the challenge binds. */   keys: ReadonlyArray<{ id: string; name: string; classification?: 'secret' \| 'config' }>;   /** The guard's state, which decides whether TOTP is on the table. */   window: RevealWindow; }; onAuthorised: () => void; onCancel: () => void | args present; docgen-only controls |
| web/src/routes/ChangeApprovals.stories.tsx | web/src/routes/ChangeApprovals.tsx#ChangeApprovals | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/ChromeIdentityControls.stories.tsx | web/src/routes/ChromeIdentityControls.tsx#ChromeIdentityControls | empty; source-derived description needed | identityId: string; name: string; kind: 'org' \| 'project'; children: ReactNode | args present; docgen-only controls |
| web/src/routes/CompactOrgRetention.stories.tsx | web/src/routes/OrgSettings.tsx#CompactOrgRetention | empty; source-derived description needed | policy: z.infer<typeof zRetentionPolicy>; busy: boolean; onSave: (next: RetentionPolicy) => void | args present; docgen-only controls |
| web/src/routes/ConnectionMintDialog.stories.tsx | web/src/routes/Remotes.tsx#ConnectionMintDialog | present | minted: MintedConnectionValue & { readonly label: string }; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ConnectionRow.stories.tsx | web/src/routes/Remotes.tsx#ConnectionRow | present | connection: z.infer<typeof zInstanceConnection>; onRevoke: () => void | args present; docgen-only controls |
| web/src/routes/CreateAccountDialog.stories.tsx | web/src/routes/machineAccess/AccountDialogs.tsx#CreateAccountDialog | present | project: { org: string; project: string }; onClose: () => void; onCreated: (name: string, kind: ServiceAccount['kind']) => void | args present; docgen-only controls |
| web/src/routes/CreateProviderDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#CreateProviderDialog | present | project: { org: string; project: string }; onClose: () => void; onCreated: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/CredentialForm.stories.tsx | web/src/routes/Adapters.tsx#CredentialForm | present | provider: string; busy: boolean; onCancel: () => void; onSubmit: (credential: string) => Promise<void> | args present; docgen-only controls |
| web/src/routes/DefinitionsBundlePanel.stories.tsx | web/src/routes/DefinitionsBundlePanel.tsx#DefinitionsBundlePanel | empty; source-derived description needed | org: string; project: string; settings: z.infer<typeof zDefinitionsSettings> | args present; docgen-only controls |
| web/src/routes/DeleteAccountDialog.stories.tsx | web/src/routes/machineAccess/AccountDialogs.tsx#DeleteAccountDialog | present | project: { org: string; project: string }; account: z.infer<typeof zServiceAccountList>['items'][number]; onClose: () => void; onDeleted: (name: string) => void | args present; docgen-only controls |
| web/src/routes/DeleteAdapterDialog.stories.tsx | web/src/routes/Adapters.tsx#DeleteAdapterDialog | present | adapter: z.infer<typeof zAdapter>; busy: boolean; onCancel: () => void; onDecide: (decision: 'prune' \| 'retain') => void | args present; docgen-only controls |
| web/src/routes/DeleteProviderDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#DeleteProviderDialog | present | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; liveLeaseCount: number; leasesKnown: boolean; onClose: () => void; onDeleted: (origin: string, revokedCount: number) => void | args present; docgen-only controls |
| web/src/routes/DeliveryTargets.stories.tsx | web/src/routes/DeliveryTargets.tsx#DeliveryTargetsPanel | empty; source-derived description needed | project: { org: string; project: string }; view: {   readonly support: ReportingSupport;   /** Readable environments only: an unreadable one is absent, never redacted (D7). */   readonly reports: readonly EnvironmentReports[];   /** Environments whose listing failed for any reason other than "not readable". */   readonly failures: readonly EnvRef[];   readonly isPending: boolean; }; known: boolean; accounts: unknown; now: Date | args present; docgen-only controls |
| web/src/routes/EnrolmentGate.stories.tsx | web/src/routes/EnrolmentGate.tsx#EnrolmentGate | present |  | no explicit args; docgen-only controls |
| web/src/routes/EstablishCredential.stories.tsx | web/src/routes/EstablishCredential.tsx#EstablishCredential | present |  | no explicit args; docgen-only controls |
| web/src/routes/FederationIssuersPanel.stories.tsx | web/src/routes/FederationIssuersPanel.tsx#FederationIssuersPanel | present |  | no explicit args; docgen-only controls |
| web/src/routes/FleetUpdateNotice.stories.tsx | render-only | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/FolderCleanupDialog.stories.tsx | web/src/routes/FolderCleanupDialog.tsx#FolderCleanupDialog | present | proposals: unknown; existingFolders: unknown; busy: boolean; envName: (id: string) => string; onApply: (moves: readonly FolderMove[]) => Promise<readonly FolderMoveOutcome[]>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/GrantDialog.stories.tsx | web/src/routes/machineAccess/EnvironmentGrants.tsx#GrantDialog | present | project: { org: string; project: string }; account: z.infer<typeof zServiceAccountList>['items'][number]; scope: unknown; machineReveal: boolean; mayGrantReporting: boolean; liveCredentials: number; onClose: () => void; onGranted: (environment: string, results: readonly GrantResult[]) => void | args present; docgen-only controls |
| web/src/routes/HealthChip.stories.tsx | web/src/routes/Adapters.tsx#HealthChip | empty; source-derived description needed | target: z.infer<typeof zAdapterTarget> | args present; argTypes present |
| web/src/routes/HistoryDrawer.stories.tsx | web/src/routes/HistoryDrawer.tsx#HistoryDrawer | present | refData: { readonly org: string; readonly project: string }; environments: unknown; keys: unknown; currentRevisions: ReadonlyMap<string, bigint>; protectedEnvironmentIds: unknown; cellsByEnvironment: ReadonlyMap<string, readonly HistoryCurrentCell[]>; pendingByEnvironment: ReadonlyMap<string, number>; pendingByOthersByEnvironment: ReadonlyMap<string, number>; currentValuesByEnvironment: ReadonlyMap<string, readonly ValueCell[]>; openerRef: RefObject<HTMLAnchorElement \| null> | args present; docgen-only controls |
| web/src/routes/ImportWizard.stories.tsx | web/src/routes/ImportWizard.tsx#ImportWizard | present | matrixRef: { readonly org: string; readonly project: string }; environments: unknown; gitManaged: boolean; canDeclareKeys: boolean; onClose: () => void | args present; docgen-only controls |
| web/src/routes/InstanceAdmin.stories.tsx | web/src/routes/InstanceAdmin.tsx#InstanceAdmin | present |  | no explicit args; docgen-only controls |
| web/src/routes/InstanceConfig.stories.tsx | web/src/routes/InstanceConfig.tsx#InstanceConfig | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/InviteDialog.stories.tsx | web/src/routes/InviteDialog.tsx#InviteDialog | present | scope: { readonly kind: 'org'; readonly org: string } \| { readonly kind: 'instance' }; scopeName: string; origin: string; onDone: (text: string) => void; onCancel: () => void | args present; docgen-only controls |
| web/src/routes/KeyDeclarationDetail.stories.tsx | web/src/routes/KeyDeclarationDetail.tsx#KeyDeclarationDetail | present | refData: { readonly org: string; readonly project: string }; keyId: string; environments: unknown; impact: {   readonly setEnvironmentIds: readonly string[];   readonly pendingEnvironmentIds: readonly string[]; }; impactReady: boolean; openerRef: RefObject<HTMLAnchorElement \| null> | args present; docgen-only controls |
| web/src/routes/LastApplyProvenance.stories.tsx | web/src/routes/DefinitionsBundlePanel.tsx#LastApplyProvenance | present | lastApply: NonNullable<DefinitionsSettings['last_apply']> | args present; docgen-only controls |
| web/src/routes/LeaseActionDialog.stories.tsx | web/src/routes/machineAccess/DynamicLeases.tsx#LeaseActionDialog | present | project: { org: string; project: string }; action: {   readonly verb: 'renew' \| 'revoke' \| 'settle';   readonly environmentId: string;   readonly environmentName: string;   readonly lease: DynamicLease; }; onClose: () => void; onDone: (message: string) => void | args present; docgen-only controls |
| web/src/routes/LeaseMintDialog.stories.tsx | web/src/routes/machineAccess/DynamicLeases.tsx#LeaseMintDialog | present | project: { org: string; project: string }; sessionId: string \| null; providers: unknown; environments: unknown; lifecycle: \| { readonly kind: 'idle' } \| { readonly kind: 'reviewing'; readonly request: Req } \| { readonly kind: 'submitting'; readonly request: Req } \| { readonly kind: 'failed'; readonly request: Req; readonly error: string } \| {     readonly kind: 'disclosed';     readonly request: Req;     readonly result: Res;     readonly stored: boolean;     readonly heldBack: boolean;     readonly copyStatus: string \| null;   }; move: (   event: MintLifecycleEvent<Req, Res>, ) => MintTransitionResult<Req, Res>; isSubmitting: (requestId: number) => boolean; nextRequestId: () => number; onClose: () => void | args present; docgen-only controls |
| web/src/routes/Login.stories.tsx | web/src/routes/Login.tsx#Login | present |  | no explicit args; docgen-only controls |
| web/src/routes/MachineAccess.stories.tsx | web/src/routes/MachineAccess.tsx#MachineAccessPage | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/MachineRevealDialog.stories.tsx | web/src/routes/machineAccess/MachineRevealPolicy.tsx#MachineRevealDialog | empty; source-derived description needed | enable: boolean; busy: boolean; failure: string \| null; onConfirm: () => void; onClose: () => void | args present; docgen-only controls |
| web/src/routes/MachineRevokeCredentialDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#RevokeCredentialDialog | present | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; onClose: () => void; onRevoked: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/Matrix.stories.tsx | web/src/routes/Matrix.tsx#Matrix | present |  | no explicit args; docgen-only controls |
| web/src/routes/MatrixKeyCreate.stories.tsx | web/src/routes/MatrixKeyCreate.tsx#MatrixKeyCreate | present | folders: unknown; environments: unknown; protectedEnvironmentIds: unknown; initialFolder: string \| null; existingKeyNames: unknown; gitManaged: boolean = false; busy: boolean; mutationError: string \| null; onClose: () => void; onCreate: (payload: MatrixKeyCreatePayload) => Promise<void> | args present; docgen-only controls |
| web/src/routes/MatrixPublishSheet.stories.tsx | web/src/routes/MatrixPublishSheet.tsx#MatrixPublishSheet | present | refData: { readonly org: string; readonly project: string }; environments: unknown; revisions: ReadonlyMap<string, bigint>; pendingByEnvironment: ReadonlyMap<string, readonly MatrixPendingEntry[]>; problems: unknown; protectedEnvironmentIds: unknown; busy: boolean; mutationError: string \| null; onPublish: (environmentIds: readonly string[]) => void; onClose: () => void | args present; docgen-only controls |
| web/src/routes/MatrixRowEditor.stories.tsx | web/src/routes/MatrixRowEditor.tsx#MatrixRowEditor | present | refData: { readonly org: string; readonly project: string }; keyRecord: MatrixKeyList['items'][number]; environmentId: string; rows: unknown; busy: boolean; mutationError: string \| null; onClose: () => void; onApply: (changes: readonly MatrixEditorChange[]) => Promise<void>; onCopy: (destinations: readonly string[], confirmProtected: boolean) => void | args present; docgen-only controls |
| web/src/routes/Members.stories.tsx | web/src/routes/Members.tsx#Members | empty; source-derived description needed | scope: { readonly kind: 'org' } \| { readonly kind: 'instance' } | args present; docgen-only controls |
| web/src/routes/MintConnectionForm.stories.tsx | web/src/routes/Remotes.tsx#MintConnectionForm | present | mint: ReturnType<typeof useMintConnection>; onMinted: (result: Disclosed) => void | args present; docgen-only controls |
| web/src/routes/MintDialog.stories.tsx | web/src/routes/machineAccess/Credentials.tsx#MintDialog | present | lifecycle: Exclude<MintLifecycle, { readonly kind: 'idle' }>; move: (   event: MintLifecycleEvent<Req, Res>, ) => MintTransitionResult<Req, Res>; isSubmitting: (requestId: number) => boolean | args present; docgen-only controls |
| web/src/routes/OIDCDone.stories.tsx | web/src/routes/OIDCDone.tsx#OIDCDone | present |  | no explicit args; docgen-only controls |
| web/src/routes/OidcProvidersPanel.stories.tsx | web/src/routes/OidcProvidersPanel.tsx#OidcProvidersPanel | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/OpsDiagnosticBanners.stories.tsx | web/src/routes/OpsDiagnosticBanners.tsx#ChromeDiagnostic | present | severity: 'error' \| 'warn' \| 'unknown'; children: ReactNode | args present; docgen-only controls |
| web/src/routes/OrgSettings.stories.tsx | web/src/routes/OrgSettings.tsx#OrgSettings | present |  | no explicit args; docgen-only controls |
| web/src/routes/PinReleaseOutcome.stories.tsx | web/src/routes/HistoryDrawer.tsx#PinReleaseOutcome | empty; source-derived description needed | consequence: RetentionConsequence; revision: unknown | args present; docgen-only controls |
| web/src/routes/Placeholder.stories.tsx | web/src/routes/Placeholder.tsx#NotFound | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/ProfileUpdateBadge.stories.tsx | web/src/routes/Shell.tsx#ProfileUpdateBadge | empty; source-derived description needed | version: string | no explicit args; docgen-only controls |
| web/src/routes/ProjectSettings.stories.tsx | web/src/routes/ProjectSettings.tsx#ProjectSettings | present |  | no explicit args; docgen-only controls |
| web/src/routes/Projects.stories.tsx | web/src/routes/Projects.tsx#Projects | present |  | no explicit args; docgen-only controls |
| web/src/routes/ProviderDiscoveryAlert.stories.tsx | web/src/routes/ProviderDiscoveryAlert.tsx#ProviderDiscoveryAlert | empty; source-derived description needed | onRetry: () => void | args present; docgen-only controls |
| web/src/routes/Reconnect.stories.tsx | web/src/routes/WorkspaceScope.tsx#Reconnect | present | origin: string; name: string | args present; docgen-only controls |
| web/src/routes/RemoteCard.stories.tsx | web/src/routes/Remotes.tsx#RemoteCard | empty; source-derived description needed | remote: z.infer<typeof zRemote>; duplicateIdentity: boolean | args present; docgen-only controls |
| web/src/routes/RetentionBoundsFields.stories.tsx | web/src/routes/RetentionBoundsFields.tsx#RetentionBoundsFields | present | age: \| { readonly kind: 'days'; readonly days: string } \| { readonly kind: 'exact'; readonly seconds: number } \| { readonly kind: 'absent' }; count: string; onAgeChange: (next: RetentionDayState) => void; onCountChange: (next: string) => void | args present; docgen-only controls |
| web/src/routes/RevisionDiff.stories.tsx | web/src/routes/RevisionDiff.tsx#RevisionDiffDialog | empty; source-derived description needed | env: { org: string; project: string; environment: string }; environmentName: string; left: unknown; right: unknown; onClose: () => void | args present; docgen-only controls |
| web/src/routes/RevokeConnectionDialog.stories.tsx | web/src/routes/Remotes.tsx#RevokeConnectionDialog | present | connection: z.infer<typeof zInstanceConnection>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/RevokeCredentialDialog.stories.tsx | web/src/routes/Adapters.tsx#RevokeCredentialDialog | present | adapter: z.infer<typeof zAdapter>; busy: boolean; onCancel: () => void; onConfirm: () => void | args present; docgen-only controls |
| web/src/routes/SamlProvidersPanel.stories.tsx | web/src/routes/SamlProvidersPanel.tsx#SamlProvidersPanel | present |  | no explicit args; docgen-only controls |
| web/src/routes/SamlSpKeysPanel.stories.tsx | web/src/routes/SamlSpKeysPanel.tsx#SamlSpKeysPanel | present |  | no explicit args; docgen-only controls |
| web/src/routes/ScanBlockDialog.stories.tsx | web/src/routes/ScanBlockDialog.tsx#ScanBlockDialog | present | title: string; intro: string; findings: unknown; onOverride: ((tokens: readonly string[]) => Promise<void>) \| null; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ScanWarnDialog.stories.tsx | web/src/routes/ScanWarnDialog.tsx#ScanWarnDialog | present | keyName: string; items: unknown; onDismiss: (item: ScanWarnItem) => Promise<readonly ScanWarnItem[]>; onReclassify: () => Promise<void>; onClose: () => void | args present; docgen-only controls |
| web/src/routes/ScimProvisioning.stories.tsx | web/src/routes/ScimProvisioning.tsx#ScimProvisioningPage | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/Sections.stories.tsx | web/src/routes/Sections.tsx#Panel | present | id: string; title: string; danger: boolean = false; tight: boolean = false; question: boolean = false; children: ReactNode | args present; docgen-only controls |
| web/src/routes/SetCredentialDialog.stories.tsx | web/src/routes/machineAccess/DynamicProviders.tsx#SetCredentialDialog | present | project: { org: string; project: string }; provider: z.infer<typeof zDynamicProvider>; onClose: () => void; onSet: (origin: string) => void | args present; docgen-only controls |
| web/src/routes/SidebarLinkItem.stories.tsx | web/src/routes/Shell.tsx#SidebarLinkItem | empty; source-derived description needed | link: {   /** The surface id, or `project-members` for the filtered members projection. */   readonly id: string;   readonly label: string;   readonly to: string;   /** Non-null renders the row as a disabled span carrying this title. */   readonly disabledReason: string \| null; }; onNavigate: () => void | args present; docgen-only controls |
| web/src/routes/SidebarVersion.stories.tsx | web/src/routes/Shell.tsx#SidebarVersion | present | version: string \| undefined | no explicit args; docgen-only controls |
| web/src/routes/StepUpBanner.stories.tsx | web/src/routes/StepUpBanner.tsx#StepUpBanner | present | session: z.infer<typeof zWhoAmI> | args present; docgen-only controls |
| web/src/routes/SystemProjectNotice.stories.tsx | web/src/routes/InstanceConfig.tsx#SystemProjectNotice | empty; source-derived description needed | org: string; project: string | args present; docgen-only controls |
| web/src/routes/SystemScopeRefusal.stories.tsx | web/src/routes/SystemScope.tsx#SystemScopeRefusal | present | surface: (typeof SYSTEM_SCOPE_REFUSED_SURFACES)[number] | no explicit args; docgen-only controls |
| web/src/routes/TargetForm.stories.tsx | web/src/routes/Adapters.tsx#TargetForm | empty; source-derived description needed | title: string; provider: string; environments: unknown; keys: unknown; busy: boolean; initial: z.infer<typeof zAdapterTarget>; lockRouting: boolean; onCancel: () => void; onSubmit: (input: AdapterTargetInput) => Promise<void> | args present; docgen-only controls |
| web/src/routes/TemporaryAccess.stories.tsx | web/src/routes/TemporaryAccess.tsx#TemporaryAccess | present |  | no explicit args; docgen-only controls |
| web/src/routes/ThemeToggle.stories.tsx | web/src/routes/Shell.tsx#ThemeToggle | present |  | no explicit args; docgen-only controls |
| web/src/routes/UpdateJobStatus.stories.tsx | web/src/routes/Remotes.tsx#UpdateJobStatus | present | jobID: string; job: InstanceUpdateJob \| undefined | args present; docgen-only controls |
| web/src/routes/Values.stories.tsx | web/src/routes/Values.tsx#Values | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceApprove.stories.tsx | web/src/routes/WorkspaceApprove.tsx#WorkspaceApprove | present |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceCallback.stories.tsx | web/src/routes/WorkspaceCallback.tsx#WorkspaceCallback | present |  | no explicit args; docgen-only controls |
| web/src/routes/WorkspaceScope.stories.tsx | web/src/routes/WorkspaceScope.tsx#WorkspaceScope | present | remote: string; children: ReactNode | args present; docgen-only controls |
| web/src/routes/WorkspaceStepUp.stories.tsx | web/src/routes/WorkspaceStepUp.stories.tsx#StepUp | present |  | args present; docgen-only controls |
| web/src/routes/accessRules/AccessRules.stories.tsx | render-only | empty; source-derived description needed |  | no explicit args; docgen-only controls |
| web/src/ui/Alert.stories.tsx | web/src/ui/Alert.tsx#Alert | present | tone: 'danger' \| 'done' \| 'warn' \| 'info' = 'danger'; action: ReactNode; children: ReactNode | args present; argTypes present |
| web/src/ui/Badge.stories.tsx | web/src/ui/Badge.tsx#Badge | empty; source-derived description needed | tone: 'neutral' \| 'danger' \| 'changed' \| 'ok' = 'neutral'; mono: boolean | args present; argTypes present |
| web/src/ui/Button.stories.tsx | web/src/ui/Button.tsx#Button | empty; source-derived description needed | variant: 'primary' \| 'secondary' \| 'danger' \| 'quiet' = 'secondary' | args present; argTypes present |
| web/src/ui/Checkbox.stories.tsx | web/src/ui/Checkbox.tsx#Checkbox | empty; source-derived description needed | label: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/ChoiceGroup.stories.tsx | web/src/ui/ChoiceGroup.tsx#ChoiceGroup | present | legend: string; hint: string; layout: 'stack' \| 'wrap' \| 'nowrap' = 'stack'; columns: number; variant: 'rows' \| 'chips' = 'rows'; className: string; children: ReactNode | args present; argTypes present |
| web/src/ui/Dialog.stories.tsx | web/src/ui/Dialog.tsx#Dialog | empty; source-derived description needed | title: string; mono: boolean; lede: ReactNode; size: 'narrow' \| 'wide' = 'narrow'; actions: ReactNode; onCancel: (event: SyntheticEvent<HTMLDialogElement>) => void; onBackdropClick: () => void; initialFocus: RefObject<HTMLElement \| null>; pinActions: boolean; className: string; children: ReactNode | args present; docgen-only controls |
| web/src/ui/Disclosure.stories.tsx | web/src/ui/Disclosure.tsx#Disclosure | present | label: ReactNode; defaultOpen: boolean = false; className: string; children: ReactNode | args present; docgen-only controls |
| web/src/ui/Field.stories.tsx | web/src/ui/Field.tsx#Field | present | label: string; hint: string; error: string; className: string; aria-describedby: string; aria-invalid: ComponentProps<'input'>['aria-invalid']; id: string; children: (control: FieldControlProps) => ReactNode | args present; docgen-only controls |
| web/src/ui/Glyph.stories.tsx | web/src/ui/Glyph.tsx#Glyph | empty; source-derived description needed | name: 'lock' \| 'link' \| 'check' \| 'cross' \| 'warn' \| 'delta' \| 'draft' \| 'ellipsis' \| 'chevron'; label: string | args present; argTypes present |
| web/src/ui/Input.stories.tsx | web/src/ui/Input.tsx#Input | empty; source-derived description needed | label: string; hint: string; error: string; className: string; mono: boolean; revealable: boolean | args present; docgen-only controls |
| web/src/ui/Menu.stories.tsx | web/src/ui/Menu.tsx#Menu | present | label: string; glyph: ReactNode = '⋯'; children: ReactNode; className: string | args present; docgen-only controls |
| web/src/ui/Radio.stories.tsx | web/src/ui/Radio.tsx#Radio | empty; source-derived description needed | label: string; name: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/Select.stories.tsx | web/src/ui/Select.tsx#Select | empty; source-derived description needed | label: string; hint: string; error: string; className: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/Tabs.stories.tsx | web/src/ui/Tabs.stories.tsx#Demo | empty; source-derived description needed | initial: 'accounts' \| 'connections' \| 'federation' = 'accounts'; label: string = 'Machine access sections' | no explicit args; docgen-only controls |
| web/src/ui/Textarea.stories.tsx | web/src/ui/Textarea.tsx#Textarea | empty; source-derived description needed | label: string; hint: string; error: string; className: string; mono: boolean | args present; docgen-only controls |
| web/src/ui/ThemeIcon.stories.tsx | web/src/ui/ThemeIcon.tsx#ThemeIcon | present | dark: boolean | args present; docgen-only controls |
| web/src/ui/ToggleChip.stories.tsx | web/src/ui/ToggleChip.tsx#ToggleChip | empty; source-derived description needed | pressed: boolean; mode: 'include' \| 'exclude' = 'include'; mono: boolean | args present; argTypes present |
| web/src/ui/Tokens.stories.tsx | web/src/ui/Tokens.stories.tsx#TokensPage | present |  | no explicit args; docgen-only controls |
| web/src/ui/Typography.stories.tsx | web/src/ui/Typography.stories.tsx#Specimen | present |  | no explicit args; docgen-only controls |
| web/src/ui/auth/AuthenticatorCodeField.stories.tsx | web/src/ui/auth/AuthenticatorCodeField.tsx#AuthenticatorCodeField | present | submitLabel: string; busy: string \| null; disabled: boolean; onSubmit: (code: string) => void; children: ReactNode | args present; docgen-only controls |
| web/src/ui/auth/LoginFlow.stories.tsx | web/src/ui/auth/LoginFlow.stories.tsx#LoginFlow | empty; source-derived description needed | scenario: 'password-enrolled' \| 'password-unenrolled' \| 'passkey' \| 'provider' \| 'sign-up'; policy: 'require-second-factor' \| 'allow-unenrolled' | args present; argTypes present |
| web/src/ui/auth/LoginForm.stories.tsx | web/src/ui/auth/LoginForm.tsx#LoginForm | present | providers: unknown; passkeys: boolean; signup: SignupDoor \| null; paused: boolean; lastUsed: LastSignIn \| null; busy: 'password' \| 'passkey' \| { provider: ProviderIdentity \| null } \| null; error: string \| null; onPassword: (credentials: { username: string; password: string }) => void; onPasskey: () => void; onProvider: (provider: ProviderIdentity, intent: SignInIntent) => void; links: ReactNode; initialIntent: 'sign-in' \| 'sign-up' = 'sign-in' | args present; docgen-only controls |
| web/src/ui/auth/ProofDialog.stories.tsx | web/src/ui/auth/ProofDialog.tsx#ProofDialog | present | title: string = "Confirm it's you"; lede: string; field: 'code' \| 'password' \| 'code-or-password'; label: string; reauth: boolean = false; pending: boolean = false; failure: string \| null = null; onCancel: () => void; onSubmit: (value: string, clear: () => void) => void | args present; docgen-only controls |
| web/src/ui/auth/ProviderButton.stories.tsx | web/src/ui/auth/ProviderButton.tsx#ProviderButton | present | provider: z.infer<typeof zAuthMethodProvider>; intent: 'sign-in' \| 'sign-up'; busy: boolean; disabled: boolean; lastUsed: boolean = false; onClick: () => void | args present; docgen-only controls |
| web/src/ui/auth/QrCode.stories.tsx | web/src/ui/auth/QrCode.tsx#QrCode | present | value: string; title: string | args present; docgen-only controls |
| web/src/ui/auth/SecondFactorChallenge.stories.tsx | web/src/ui/auth/SecondFactorChallenge.tsx#SecondFactorChallenge | present | username: string; totp: boolean; passkey: boolean; busy: 'code' \| 'passkey' \| null; error: string \| null; onCode: (code: string) => void; onPasskey: () => void | args present; docgen-only controls |
| web/src/ui/auth/SecondFactorSetup.stories.tsx | web/src/ui/auth/SecondFactorSetup.tsx#SecondFactorSetup | present | username: string; step: \| { kind: 'password' } \| { kind: 'codes'; codes: readonly string[] } \| { kind: 'choose' } \| { kind: 'totp'; otpauthUrl: string; secret: string }; passkeys: boolean; busy: 'password' \| 'totp' \| 'passkey' \| 'code' \| null; error: string \| null; onPassword: (password: string) => void; onCodesStored: () => void; onChooseTotp: () => void; onChoosePasskey: () => void; onConfirmCode: (code: string) => void | args present; docgen-only controls |

Exact extracted descriptions, property descriptions, union/type/default data and raw CSF args/argTypes live in before-docgen.json. Installed react-docgen is used; actual live Docs controls still require browser inspection.

## Artifact entry points and reproducibility

Run from the repository root:

```sh
node web/.artifacts/storybook-improvement/inventory/generate.cjs before web/.artifacts/storybook-improvement/before/storybook-static/index.json
node web/.artifacts/storybook-improvement/inventory/docgen.cjs before
node web/.artifacts/storybook-improvement/inventory/generate.cjs after web/.artifacts/storybook-improvement/after-theme-surface/storybook-static/index.json
node web/.artifacts/storybook-improvement/inventory/docgen.cjs after
node web/.artifacts/storybook-improvement/inventory/write-durable.cjs
node web/.artifacts/storybook-improvement/inventory/write-report.cjs
```

Outputs: before.json / after.json contain component, route, story and Docs mappings; before-sidebar.txt / after-sidebar.txt contain complete trees and IDs; before-docgen.json / after-docgen.json contain type/default/control extraction; proposed-source-coverage.json records baseline owner coverage and exclusions. Final logs, screenshot comparisons, catalogue delta and measured costs belong to the parent handoff.
