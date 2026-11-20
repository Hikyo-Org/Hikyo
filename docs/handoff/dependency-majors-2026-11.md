# Dependency major-version sweep handoff

Completed 2026-11-20 on `dunky13/dependency-major-version-audit`, based on `dev`.
Every direct Go, npm, PyPI, GitHub Action, pinned CI tool, runtime and container
dependency was compared against its registry's latest release.

## Changed

- pnpm 11.24.0 → 12.21.0 in every `packageManager`, the setup docs and the CI
  version fixtures. Corepack 0.35.0 provisions it unchanged. Lockfiles keep
  format 9.0 and gain a `packageManagerDependencies` document; no resolved
  dependency changed. The private root package now has a `pnpm-lock.yaml`:
  pnpm 12 records the `packageManager` pin there on any root invocation, and
  `--frozen-lockfile` fails without it. `export-pnpm-cache.sh` now requires `12.*` (the store path
  is still `v11`).
- Feedback CI runs `install-corepack.sh` from the merge commit but runs pnpm in
  the PR head. The script now pins from the checkout it runs in, so heads still
  on pnpm 11 pass until rebased, and `export-pnpm-cache.sh` accepts pnpm 11 and
  12 (same `v11` store, separate cache keys). Drop 11 once no open PR needs it.
- With the global virtual store (`virtualStoreType: global`), pnpm 12 injects an
  ESM resolve hook through `NODE_OPTIONS` into `pnpm run` scripts. `clients/ts/src/installJsonParser.test.ts` spawned a child node outside
  any package and failed with `ERR_REQUIRE_CYCLE_MODULE`; the child now runs
  without the inherited `NODE_OPTIONS`.
- Actions: `configure-pages` v6.0.0 and `deploy-pages` v5.0.1; their release
  notes list only the Node 24 runtime.
- `setup-node` v7.1.0 lands separately through a `hotfix/` PR to `main`:
  `ci.yml` is a CI definition that dev admission refuses, and the storybook and
  feedback workflows must mirror its setup-node step. v7 only drops the dummy
  `NODE_AUTH_TOKEN`, and no workflow sets `registry-url`. That PR also adds the
  setup-node pair to the frozen coverage digest. A main-to-dev backmerge brings it here.
- kind v0.32.0 → v0.33.0. The `kindest/node` image moves within Kubernetes 1.36
  (v1.36.1 → v1.36.4, the digest from the v0.33.0 release notes); the default
  1.37 image was not adopted.
- Go: cel-go v0.31.0 (the last release under `github.com/google/cel-go`),
  x/net v0.63.0, x/tools v0.53.0 (from a pseudo-version, also in
  `go-tool-modules.txt` and the goimports pin), x/vuln v1.10.0,
  controller-tools v0.23.0 (CRDs differ only in the generator annotation) and
  standard-webhooks v1.3.0 (source compatible; it now rejects empty secrets,
  which the OIDC call sites already guard).

## Deliberately not changed

- `@openai/codex` stays 0.160.1. `scripts/acceptance/mcp-fixture/clients.go`,
  its receipts and `docs/site/.../mcp.mdx` bind the recorded authenticated Codex
  proof to that exact version; moving it needs a new authenticated run.
- go-jose v5 has no tagged release, only a pseudo-version.
- cel-go v0.32.0+ moved to `cel.dev/cel-go`. `k8s.io/apiserver` still imports
  `github.com/google/cel-go`, so moving the admission tests would evaluate CEL
  with a different library than the API server they model.
- Node stays on 26.x (27 is alpha); Postgres stays 18 (19 is a release candidate).
- `clients/ts` keeps the `@typescript/typescript6` alias for `@hey-api/openapi-ts`
  0.99.0, which crashes at `ts.SyntaxKind` without the TS 6 compiler API
  (hey-api/hey-api#4235). Only the `next` prerelease drops the dependency, and it
  changes generated output: `baseUrl` becomes `'/'` and 459 schemas become
  `z.strictObject`.

## TypeScript 7.1 and tsrs

Adopted 2026-11-20 on `dunky13/ts71-tsrs-trial`, deliberately ahead of 7.1.0
stable at the owner's request:

- `web`, `docs/site` and `clients/ts` (`@typescript/native`) pin
  `typescript` 7.1.0-beta exactly.
- `docs/site` replaces `astro check`, which refuses TS 7, with `astro sync` and
  `tsc --noEmit --runExternalCode` through `@astrojs/ts-content-mapper` 0.2.0.
  `@astrojs/check` and TypeScript 6.0.3 are gone. On four seeded errors
  (frontmatter, template expression, component prop, `<script>`), both checkers
  reported the same codes and positions; the check takes 3.3 s instead of 6.6–7.8 s.
- `web` adds tsrs 7.1.0-beta.tsrs3 for `node --run typecheck:fast` and the
  VS Code language server (see CONTRIBUTING.md). `tsc` stays the CI gate: tsrs
  `--fast` can, rarely, differ from the pinned compiler. tsrs packages carry
  SLSA provenance from `oxc-project/tsrs` and have no install scripts.
- Semver ranges exclude prereleases, so Storybook's and react-docgen-typescript's
  `typescript >= 4.x` peers reject 7.1.0-beta. `web/pnpm-workspace.yaml` allows
  exactly 7.1.0-beta through `peerDependencyRules`; `pnpm peers check` passes.
- Dependabot ignores the nightly `7.1.0-dev.*` builds (`>= 7.1.0-dev.0 < 7.1.0-rc`),
  which semver ranks above the beta. RCs and stable still update.

## Findings

- With strict objects, `clients/ts/src/contract.test.ts:176` fails: the test
  reuses a `key_ids` fixture for `DeveloperCredentialReauthIntent`, whose spec
  sets `additionalProperties: false`. Today's generator emits plain `z.object`
  for about 450 such schemas, so extra keys are not rejected in Zod.
- `tsrs` is `oxc-project/tsrs`, an experimental Rust port of the TS 7.1 compiler
  (first release October 2026, `7.1.0-beta.tsrs3` on 2026-11-18, no outside PRs).
  Measured locally (M2 Pro, median of 3): `web` typecheck 9.68 s on tsc 7.0.2,
  6.99 s on 7.1 beta, 0.51 s on tsrs, all with identical diagnostics on seeded
  errors. In CI, web `tsc` is 5.5 s of a 296 s job, so the gain is local and
  editor feedback, not CI wall time. tsc 7.0.2 `--singleThreaded` beats its
  default (7.99 s), which suggests a codebase-specific hot spot worth a
  `--generateTrace` profile.

## Next steps

1. When TypeScript 7.1.0 stable publishes (planned 2026-11-24; the RC slipped),
   pin it in all three projects, move tsrs to `7.1.0-tsrs1`, and drop the
   `peerDependencyRules` entry and the Dependabot `7.1.0-dev` ignore. Then
   recheck whether a hey-api stable release still needs the TS 6 alias.
2. Optional: enable oxlint type-aware rules with tsrs as the backend (oxlint
   1.92+, rules in `.oxlintrc.json`, not CLI flags). A trial over `web` took
   0.66 s and found 70 errors, including 39 `no-misused-promises` and 7
   `no-floating-promises`. oxlint 1.94 also adds `react(purity)`, which flags
   `src/routes/MachineAccess.tsx:243`.
3. Decide whether Zod should enforce `additionalProperties: false`; if so, fix
   the `contract.test.ts:176` fixture first.
4. Re-prove the MCP fixture on a newer Codex only with an authenticated client.
