# Go 1.27 and dependency refresh handoff

Completed 2026-08-23 on `t3code/upgrade-node-26`.

## Delivered

- `go.mod` now selects Go 1.27.0; CI continues to read the exact version from
  that file. Source-build, release-signing, architecture, and CI-fixture text
  now agree with the pin.
- All direct Go modules are current. Fourteen modules moved, including
  `modernc.org/sqlite` 1.56.0 to 1.57.0; generated Go output remains fresh.
- Database stack audited: `pgx` 5.10.0, `goose` 3.27.3, and `sqlc` 1.31.1 were
  already current. CI now pins PostgreSQL 18.6 by manifest digest.
- Compatible current-major JavaScript dependencies moved to their latest
  policy-eligible releases.
- Demo Alpine moved from 3.21 to 3.24; release distroless image digest moved to
  the current Debian 13 nonroot manifest.

No database schema or migration changed.

## Deliberate holds

- Docs remain on TypeScript 6.0.3 because Astro still uses the JavaScript
  compiler API. Web uses TypeScript 7.0.2 directly; the generated client keeps
  its side-by-side TypeScript 7 CLI and TypeScript 6 API arrangement.
- Same-hour npm releases were not exempted from pnpm's minimum-release-age
  policy. No `minimumReleaseAgeExclude` entries were added.
- `@hey-api/openapi-ts` remains at 0.97.3. Version 0.99.0 generates `as`
  assertions and an ESLint suppression, both forbidden by repository standards.

## Verification

- Go 1.27.0: `go build ./...`, `go vet ./...`, generated-code refresh, gofmt,
  actionlint, and ShellCheck passed.
- `go test -count=1 ./...` passed across 61 packages against PostgreSQL 18.6;
  the SQLite/PostgreSQL conformance, migration, transaction, backup, and
  isolation legs ran. `internal/isolation` completed in 347.397 seconds.
- Binary-mode `govulncheck` found zero reachable vulnerabilities. The scanner
  benchmark completed at 351097 ns/op with 10 allocations/op.
- TypeScript client verification passed 14 tests. Web TypeScript 7 typecheck,
  323 tests, and production build passed.
- Documentation verification passed Astro diagnostics, 35-page build, OSS
  policy, PWA/offline, live-site, and fallback-channel gates.
- Frozen pnpm installs passed for client, web, and docs lockfiles.
- pnpm audits found no known vulnerabilities in client, web, or docs; all Go
  module checksums verified.

## Local operations

The requested Docker prune removed all unused containers, images, networks,
build cache, and volumes. Docker reported 58.78 GB reclaimed. The temporary
PostgreSQL validation container used a bind-mounted disposable data directory
so validation did not recreate a Docker volume.
