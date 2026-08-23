# TypeScript 7 upgrade handoff

Completed 2026-08-23 on `t3code/upgrade-node-26`, after the Node 26.7.0
toolchain commit `22af0677`.

## Changed

- `web` pins `typescript` 7.0.2. Its existing `tsc --noEmit` command now invokes
  the stable Go-native compiler; `tsgo` was only the preview command name.
- `clients/ts` installs TypeScript 7.0.2 as the `@typescript/native` npm alias,
  which supplies `tsc`, and installs `@typescript/typescript6` 6.0.2 through the
  `typescript` name for `@hey-api/openapi-ts`'s programmatic compiler API.
- `docs/site` intentionally remains on TypeScript 6.0.3. Astro/Volar embeds the
  compiler API, which TypeScript 7.0 does not expose.

## Compatibility finding

A direct `clients/ts` replacement with `typescript` 7.0.2 made generation fail
at `ts.NewLineKind.LineFeed`. The side-by-side package layout recommended by the
TypeScript team keeps generation on the TS6 API while the package's typecheck
uses TS7's native `tsc`.

## Verification

- `clients/ts`: `tsc` reports 7.0.2, the imported compiler API reports 6.0.3,
  generation is deterministic, typecheck passes, and 14 tests pass.
- `web`: `tsc` reports 7.0.2, typecheck passes, 323 tests pass, and Vite builds.
- Local warm typecheck time fell from 4.17s to 0.94s for `web` and from 1.54s
  to 0.68s for `clients/ts`.
- `./scripts/ci/verify-docs.sh`: Astro typecheck, build, PWA, policy, and live-docs
  gates pass while retaining TypeScript 6.0.3.
- `go test ./... -count=1`: 3,891 tests pass across 61 packages.
