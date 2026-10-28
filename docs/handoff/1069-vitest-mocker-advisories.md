# #1069: Vitest mocker development-tool repair

Parent: [#79](https://github.com/Hikyo-Org/Hikyo/issues/79).
Owner: [#1069](https://github.com/Hikyo-Org/Hikyo/issues/1069).
PR: [#1078](https://github.com/Hikyo-Org/Hikyo/pull/1078).
Inspected baseline: `24e198b32a3ffd8d4013207b2f974d76ec49be6f`, 2026-10-28.

## Disposition and patch selection

Both GitHub Dependabot alerts were OPEN on inspection:

| Alert | Advisory | Affected 4.x range | First patched 4.x |
| --- | --- | --- | --- |
| [104](https://github.com/Hikyo-Org/Hikyo/security/dependabot/104) | [GHSA-xp26-3454-mjgp](https://github.com/advisories/GHSA-xp26-3454-mjgp), Windows redirect traversal | `>=4.1.11 <4.1.12` | `4.1.12` |
| [105](https://github.com/Hikyo-Org/Hikyo/security/dependabot/105) | [GHSA-qxg6-89mg-rqv8](https://github.com/advisories/GHSA-qxg6-89mg-rqv8), manual mock code injection | `>=2.1.0-beta.7 <4.1.12` | `4.1.12` |

The [official 4.1.12 release](https://github.com/vitest-dev/vitest/releases/tag/v4.1.12)
backports manual and redirect mock hardening in
[upstream PR #11580](https://github.com/vitest-dev/vitest/pull/11580).
Use the first patched 4.x release, rather than changing major versions or
forcing only a transitive dependency past its exact peers. Update `vitest`,
`@vitest/browser-playwright` and `@vitest/coverage-v8` together. Their published
metadata pins the Vitest peers, browser package and mocker to `4.1.12`.
Node `26.12.1` and Vite `8.2.4` satisfy its engine/peer ranges.
The Storybook Vitest addon `10.6.3` accepts Vitest/browser `^4.0.0 || ^5.0.0`.
Keep the existing Playwright `1.62.1` and `playwright-core` override together.
`pnpm peers check` exposed an existing mismatch: `@storybook/addon-mcp` and
`@storybook/addon-vitest` `10.6.3` require Storybook `^10.6.3`, but the core
was pinned to `10.6.2`. Raise only that core pin to `10.6.3`; the resulting
graph reports no peer dependency issues. Storybook's separate `3.2.4`
expect/spy utilities do not introduce another mocker dependency.
New lockfile package versions are limited to Vitest `4.1.12` and Storybook
`10.6.3`; regeneration also removes two duplicate helper versions in favor of
versions already present in the graph.

## Exposure boundary

GitHub classifies both findings as **medium, development, transitive**, with
`web/pnpm-lock.yaml` as the affected manifest. The vulnerable path is a Vite
development-server mock-registration handler. The Windows issue concerns a
redirect pathname crossing the configured file allowlist. The injection issue
requires a client able to reach the development-server WebSocket, then changes
the JavaScript served to the developer's browser. Upstream documents that Vite's
Origin check blocks the tested cross-origin browser clients; that does not
make a reachable development server safe from non-browser clients.

Hikyo uses Vitest for unit mocks and a separate browser project in
`web/vite.config.ts`, with `storybookTest` and `@vitest/browser-playwright`.
Storybook interactions use `storybook/test` and the synthetic fetch fixtures
in `web/.storybook/withApp.tsx`. These are development/test consumers worth
validating, even without a direct application call to `mockerPlugin`.
Vitest Browser Mode explicitly sets `registerWebSocketEvents: false` on the
interceptor plugin. The public unauthenticated registration route from these
advisories is therefore not enabled by that browser project. This is an
exposure distinction, not a reason to retain a vulnerable dependency.
A tracked-source search found no direct application configuration of
`mockerPlugin` or `interceptorPlugin`; dependency inspection found no production
dependency path to `@vitest/mocker`. Source inspection does not establish that
Hikyo enables the affected public registration endpoint in its supported setup.
The server embeds the static Vite output under `internal/webui/dist`; it does
not serve a Vite development server. The advisory's `configureServer` path does
not run in `vite build` or `vite preview`. These alerts are not evidence of a
production Hikyo server exploit. No live Windows exploit or production probing
was performed.

## Validation

Checks use the pinned Node `26.12.1` and pnpm `11.24.0`. Heavy setup, suites and
builds run through the shared resource queue. Unit runs use at most two workers;
browser runs use one worker with browser file parallelism disabled. No test,
threshold, timeout, or security check is removed or relaxed.

| Evidence | Result |
| --- | --- |
| Frozen client and web installation | PASS |
| `pnpm --dir web peers check` | PASS, no peer dependency issues |
| `pnpm --dir web why @vitest/mocker` | Exactly one version, `4.1.12`; production-only traversal returns no dependency path |
| Shared `fast-feedback.sh --working-tree`, complete PR diff from the inspected baseline | PASS: typecheck, lint, 217 unit files / 2,035 tests |
| Node script typecheck and unit consumer check | PASS: 217 files / 2,035 tests |
| Direct `node --run test` smoke check of Storybook docgen, interaction coverage and build options | PASS: 3 files / 10 tests |
| V8 coverage provider over the unit project | PASS: 217 files / 2,035 tests; coverage report generated |
| Chromium Storybook, dark | PASS: 143 files / 857 tests |
| Chromium Storybook, light | PASS: 143 files / 857 tests |
| Production SPA build and development-code isolation | PASS; emitted JavaScript contains neither advisory handler/accessor marker |
| Design export and production Storybook build with generated Docs inventory | PASS: 143 modules, 857 stories, 143 generated Docs |
| Built Storybook theme switching, Docs controls/fixtures and lifetime consumer | PASS: 4 tests, 1 worker |

A local synthetic probe also exercises the installed public mocker APIs:
manual-mock URLs and quoted export names remain data while exports resolve;
valid in-root redirect loading succeeds; slash and backslash traversal
registrations are rejected; disabling WebSocket registration installs no
registration handler. This is a macOS plugin-level check with isolated temporary
files and no network. It is not a native Windows or deployed-service acceptance
run. The temporary files are removed by the probe.

Reproduce the owner checks from the repository root after installing the pinned
client and web dependencies:

```sh
VITEST_MAX_WORKERS=2 scripts/ci/fast-feedback.sh --working-tree
pnpm --dir web exec vitest run --project unit --maxWorkers=2 --coverage
pnpm --dir web run test-storybook --maxWorkers=1 --browser.fileParallelism=false
pnpm --dir web run test-storybook:light --maxWorkers=1 --browser.fileParallelism=false
pnpm --dir web run build
pnpm --dir web run build-storybook
pnpm --dir web run test-storybook:theme
```

Use the shared queue for those heavy commands under the current contributor
policy. No persistence code changes are made, so a separate SQLite/PostgreSQL
run is NOTRUN for this dependency-only task. Native Windows exploit acceptance
is NOTRUN. Hosted CI results and the exact PR head must be checked separately;
local passes do not establish merge-queue/full-candidate or release acceptance.

The initial source head `bd43e070de2f5c51f97faa9750a7866f5edfe08a` passed all
three hosted feedback lanes in
[run 40670361677, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/40670361677).
The closeout changes only this handoff. Its final head still needs its own
hosted feedback and maintainer review; consult the PR for current results.
Validated manifest blob: `b18a7aa779d1d42026ffbefe24671dc681eb6982`.
Validated lockfile blob: `f94bb39360de36b27c479bd1460cbcfef5fabf7e`.

## Maintainer closeout

1. Review the exact dependency PR, its local evidence and hosted feedback.
   Maintainer review and merge queue/full-candidate proof remain required.
2. After merge, re-query both alerts on the default branch. A PR with patched
   dependencies is source repair evidence, not proof GitHub has closed an alert.
   Record the observed `state`, `fixed_at` and resolved dependency version.
3. If either alert stays open after dependency-graph ingestion, inspect that
   alert's manifest and dependency path. Do not dismiss it to simulate a fix.

```sh
gh api repos/Hikyo-Org/Hikyo/dependabot/alerts/104 \
  --jq '{number,state,fixed_at,dependency,security_vulnerability}'
gh api repos/Hikyo-Org/Hikyo/dependabot/alerts/105 \
  --jq '{number,state,fixed_at,dependency,security_vulnerability}'
```

Cross-provider review is SKIPPED because the configured Anthropic subscription
review path is unavailable. No paid fallback or scoped ADR waiver is inferred.
No release, publication, deployment or stable-policy activation is authorized.
