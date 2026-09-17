# #773 adversarial review findings handoff

## Outcome

All twenty findings of the #773 adversarial review (PRs #745 through #770)
are addressed in one PR, root cause each, no deferrals. Nothing was closed by
deleting a test or relaxing a gate; where an existing test asserted the
defective behaviour (the MCP release-failure test) it was rewritten to assert
the corrected contract and the reason is in its comment.

## What changed, by finding

| Finding | Fix | Regression evidence |
|---|---|---|
| F01 checkbox glyphs under CSP | `app.css`: the tick is two rotated borders, the dash a bar; no `data:` mask. | `web/src/styles/csp.test.ts` fails on any `url(data:` in `app.css` or `tokens.css`. |
| F02 MCP set accepts absent/null value | `valueInput` records presence; literal `null` is refused by name; `set` requires a present string, `unset` refuses any value, `""` stays a legitimate proposal. | `TestStageValuePresenceIsExplicit` (8 boundary cases per write tool). |
| F03 release failure reported as call failure | `withAdmission`: the call's outcome wins; a release failure after success is recorded on the call state and handed to `Options.OnReleaseFailure` (a plain callback, since the MCP package must own no telemetry sink; the app routes it to its logger), never joined into the result. | `TestReleaseFailureKeepsCommittedStageResult` (fake success + failed release: committed result returned, staging invoked once, the callback receives the operation and error once). `TestSharedAdmissionReleaseFailureKeepsTheResult` replaces the fail-closed read test; acquisition still fails closed (`TestSharedAdmissionLimitUsesUniformHTTP429`). |
| F04 design images outside `/storybook/` | `design()` emits a relative `design/<slug>.png`; the addon renders it in an `<img>`, so it resolves under `/storybook/` and at the dev root alike. | `web/.storybook/design.test.ts` resolves the URL against both bases. CI check below asserts every linked image ships in `storybook-static/design/`. |
| F05 inline Docs stories share one fetch | Every `parameters.app` story spreads `topLayerDocs` (framed on Docs). `installAppFetch` throws when it runs with `viewMode === 'docs'`, so forgetting the spread fails loud. | Guard in `withApp.tsx`; `topLayerDocs.ts` documents both reasons. |
| F06 HA exactly-once window | Per-node counters; the killed leader's `Run` is joined before the failover wait; every worker is cancelled and joined before the final per-node assertions (first leader 1, failover leader 1, third node 0). No sleeps, no timeout changes. | `go test -race -count=3 -run TestSchedulerHAThreeNodesOnePostgres ./internal/app` against a local PostgreSQL 18: 3/3 pass. |
| F07 font substitution silenced | `export.ts` passes `--font-policy strict` and forwards the child's stderr; a failed export names the node and the CLI message. | Run in this worktree: `design:export` exits 0 with the pinned fonts present; no missing-font fixture was fabricated. |
| F08 formatting bypasses collection | The literal regex tolerates whitespace, line breaks and a trailing comma; every `design(` call that the literal form cannot see fails by file name. Sources now carry their path. | `lib.test.ts`: same node set for four formattings; dynamic argument throws naming the file. |
| F09 theme resolution | `parsePenVariables` requires `themes` to be exactly `{"Mode":["Dark","Light"]}` and refuses an explicit Dark entry, a second untheme'd entry, or a second Light entry. | `lib.test.ts` theme resolution block (reversed, missing, extra, second axis, no themes, explicit Dark, duplicates). |
| F10 non-colour light overrides | `compareTokens` checks each mode against its own CSS value; `cssToPenVariables` refuses to seed a non-colour that differs between modes rather than dropping the light value. | `lib.test.ts` per-mode non-colours block. |
| F11 copy-and-rename ids | Skill text now requires fresh ids (prefer `clone_node`); `export.ts` refuses a document with a duplicate node id before exporting. | `findDuplicateNodeIds` tests. |
| F12 menu semantics without keyboard model | `Menu`: open focuses the first enabled item; Up/Down wrap, Home/End jump, disabled rows skipped; Escape closes and returns focus (native popover). | `Menu.stories.tsx` `KeyboardModel` play test in real Chromium. |
| F13 Tabs duplicate ids | Focus moves within the owning tablist by position; the Demo uses a `useId` prefix. | `Tabs.stories.tsx` `TwoDemosStayIndependent`: unique ids, second demo keeps focus and selection, first untouched. |
| F14 probe accepts incomplete catalogue | `checkCatalog` requires exactly the closed read set or the closed read-plus-write set; the reported count is the number of read tools that answered. | `scripts/mcp-production-client/main_test.go` (complete sets pass; mixed, duplicate, unknown, write-only, empty fail). |
| F15 machine-credential publish claim | Threat-model banner and the stage tool description state the real boundary: no MCP tool exposes publish; REST publish authorization is unchanged. | Doc review; `grep` for the retired wording is empty. |
| F16 field controls overwrite caller aria | `Field` merges an external `aria-describedby` in front of hint/error ids and keeps an external `aria-invalid` unless the field's own error marks the control; Input, Select and Textarea pass theirs through. | `ExternalDescriptionIsMerged` and `ExternalInvalidIsKept` stories for all three, asserting accessible description and DOM attributes. |
| F17 radio examples share a group | Each Radio story carries its own `name`. | Story args; comment explains the Docs composition. |
| F18 no production Storybook build on PRs | The `storybook` CI job runs `pnpm exec storybook build` after the browser suites, then checks every exported design image is in `storybook-static/design/`. | Workflow step; run locally in this worktree (see below). |
| F19 bench artifact provenance | `bench.Result` gains additive `passes` and `selection` (`lowest-p99-pass`); harness stays 2 and an artifact without them is single-pass evidence, stated in the schema comment. `SelectPass` is the pinned estimator. | `bench_test.go` pins whole-pass selection and tie order; committed Pi artifact still validates. |
| F20 `as CSSProperties` | `web/src/css-custom-properties.d.ts` widens `CSSProperties` for `--*` keys; `ChoiceGroup` is checked by assignment. | Typecheck. |

## Verification run for this PR

- `go test ./internal/mcpserver ./scripts/mcp-production-client ./scripts/mcp-public-smoke ./internal/scanning/... ./internal/bench ./cmd/bench-scan`: pass.
- `go test -race -count=3 -run TestSchedulerHAThreeNodesOnePostgres ./internal/app` with `HIKYO_TEST_POSTGRES_DSN` pointing at a local PostgreSQL 18: 3/3 pass (the test creates and drops its own scratch database).
- `web`: `node --run typecheck` clean with `clients/ts` and `web` dependencies installed under Node 26.7.0; `node --run test`: 120 files, 1049 tests pass.
- `web`: `pnpm run design:export` (token check + strict-font render of the four Button nodes) exit 0; `pnpm run test-storybook` (dark) and `pnpm run test-storybook:light`: 59 files, 240 tests pass each; `pnpm exec storybook build` succeeds and all four exported design images are present in `storybook-static/design/`.
- Static build served under `/storybook/` with `Content-Security-Policy: img-src 'self'` (Playwright, both themes): unchecked shows no mark, checked shows the rotated tick, indeterminate shows the dash, no `data:` mask, Space toggles, focus indication present, zero CSP console violations; the Button design image decodes from `/storybook/design/Button--Primary.png` at 242 px; the Projects, Audit and Login Docs pages render every story framed with no inline-fetch refusal.

## Review status

- Native Codex cross-provider pass: **skipped**. The originating review recorded Codex quota as exhausted, and the standing quota gate says a known-low balance skips immediately. This PR is therefore NOT labelled CLEAN by a cross-provider reviewer.
- A same-provider (Claude) delta review of the full diff against the twenty acceptance criteria returned zero findings. Weight it as a second read, not as the cross-provider gate #773 asked for: it confirmed each fix against its criterion rather than producing new attack angles. Re-run the native Codex pass when quota returns before treating the review as closed.

## Follow-ups that are NOT deferred work

- #762 (route-to-atom migration) can now rely on the Field aria contract (F16).
- #651 release evidence: the probe now reports the true read-call count (F14).
