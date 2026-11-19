# Dependency major-version sweep handoff

Completed 2026-11-20 on `dunky13/dependency-major-version-audit`, based on `dev`.
Every direct Go, npm, PyPI, GitHub Action, pinned CI tool, runtime and container
dependency was compared against its registry's latest release.

## Changed

- pnpm 11.24.0 → 12.21.0 in every `packageManager`, the setup docs and the CI
  version fixtures. Corepack 0.35.0 provisions it unchanged. Lockfiles keep
  format 9.0 and gain a `packageManagerDependencies` document; no resolved
  dependency changed. `export-pnpm-cache.sh` now requires `12.*` (the store path
  is still `v11`).
- pnpm 12 injects an ESM resolve hook through `NODE_OPTIONS` into `pnpm run`
  scripts. `clients/ts/src/installJsonParser.test.ts` spawned a child node outside
  any package and failed with `ERR_REQUIRE_CYCLE_MODULE`; the child now runs
  without the inherited `NODE_OPTIONS`.
- Actions: `setup-node` v7.1.0, `configure-pages` v6.0.0, `deploy-pages` v5.0.1.
  The release notes list only the Node 24 runtime and removal of the dummy
  `NODE_AUTH_TOKEN`; no workflow sets `registry-url`. The frozen coverage digest
  gained the matching setup-node pair.
- kind v0.32.0 → v0.33.0. The `kindest/node` image moves within Kubernetes 1.36
  (v1.36.1 → v1.36.4, the digest from the v0.33.0 release notes); the default
  1.37 image was not adopted.
- Go: cel-go v0.30.0, x/net v0.63.0, x/tools v0.53.0 (from a pseudo-version, also
  in `go-tool-modules.txt` and the goimports pin), x/vuln v1.10.0,
  controller-tools v0.23.0 (CRDs differ only in the generator annotation) and
  standard-webhooks v1.3.0 (source compatible; it now rejects empty secrets,
  which the OIDC call sites already guard).

## Deliberately not changed

- `@openai/codex` stays 0.160.1. `scripts/acceptance/mcp-fixture/clients.go`,
  its receipts and `docs/site/.../mcp.mdx` bind the recorded authenticated Codex
  proof to that exact version; moving it needs a new authenticated run.
- go-jose v5 has no tagged release, only a pseudo-version.
- Node stays on 26.x (27 is alpha); Postgres stays 18 (19 is a release candidate).
- `docs/site` stays on TypeScript 6.0.3 and `clients/ts` keeps the
  `@typescript/typescript6` alias. Both were retested from scratch on TS 7.0.2:
  - `astro check` refuses TS 7 by design ("astro check does not currently
    support TypeScript 7.0"); `@astrojs/check` 0.9.12 and Volar 2.4.31 peer on
    `<7`, and `verify-docs.sh` runs `pnpm peers check`. The successor,
    `@astrojs/ts-content-mapper` 0.2.0, is experimental and needs TS 7.1.
  - `@hey-api/openapi-ts` 0.99.0 (latest stable) crashes at `ts.SyntaxKind`
    because TS 7.0 exposes no compiler API (hey-api/hey-api#4235). Only the `next`
    prerelease drops the dependency, and it changes generated output: `baseUrl`
    becomes `'/'` and 459 schemas become `z.strictObject`.

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

1. When TypeScript 7.1 is stable: move `web` to it, then trial `docs/site` with
   `@astrojs/ts-content-mapper` and `tsc --runExternalCode`, and recheck whether
   the hey-api stable release still needs the TS 6 alias.
2. Optional local trial of tsrs for editor/`--watch` feedback; do not gate CI on
   it while it is a beta tracking 7.1.
3. Decide whether Zod should enforce `additionalProperties: false`; if so, fix
   the `contract.test.ts:176` fixture first.
4. Re-prove the MCP fixture on a newer Codex only with an authenticated client.
