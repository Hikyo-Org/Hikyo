# Node 26 upgrade handoff

Completed 2026-08-23 on `t3code/upgrade-node-26` from base `2d915d6f`.

## Changed

- `.nvmrc` now selects Node 26.7.0; every Node-using CI job reads this file.
- The source-build docs require Node.js 26.7.0 and Corepack 0.35.0.
- Node 26 no longer bundles Corepack, so every pnpm-using CI path installs the
  exact Corepack release through the shared `scripts/ci/install-corepack.sh`.
- `web` and `clients/ts` use `@types/node` 26.2.0, matching the existing docs package.
- Two compose-stack test fixtures now carry the auth kinds required by the
  parse-time auth-rule refactor in `15a020dd`; this repaired two failures already
  present at the branch base.

## Verification

All commands ran with Node v26.7.0 and pnpm 11.10.0 where applicable.

- `pnpm --dir clients/ts run verify`: 14 tests passed.
- `pnpm --dir web run typecheck`, `test`, and `build`: 323 tests passed.
- `./scripts/ci/verify-docs.sh`: typecheck, build, PWA, policy, and live-docs gates passed.
- `scripts/ci/install-corepack.sh`: installed and verified Corepack 0.35.0 from a
  Node 26 installation that initially contained only `node`, `npm`, and `npx`.
- `actionlint` plus build-reuse, cache-policy, trusted-script, and changed-path
  fixtures passed.
- `go test ./... -count=1`: 3,891 tests passed across 61 packages.
