# Issue #519: precompressed SPA and route splitting

Issue: https://github.com/Hikyo-Org/Hikyo/issues/519. Base:
`f86f752634960abeb343d2781d93d39d4bf944fa`.

## Verdict

The ticket remains valid on its implementation base. The production build had
one 1.13 MB JavaScript entry chunk, every route was imported eagerly, and the
embedded UI served only identity representations. Runtime API compression was
not added: precompressed static files solve the stated first-paint cost without
putting buffering or compression middleware near SSE.

## Contract

- `vite build` writes Brotli and gzip sidecars for `index.html` and compressible
  hashed assets using Node's built-in zlib. A sidecar is kept only when smaller
  than its identity representation; already-compressed fonts are skipped.
- The SPA server negotiates existing `.br` and `.gz` sidecars from
  `Accept-Encoding`, prefers Brotli on equal client quality, retains identity
  for legacy clients, and emits `Vary: Accept-Encoding` whenever alternatives
  exist.
- Hashed assets retain `public, max-age=31536000, immutable`; `index.html`
  retains `no-cache`. Encoded assets retain the original media type.
- API and SSE routing are unchanged. No runtime compressor or response wrapper
  exists, so streaming behavior is unaffected.
- Top-level route modules load through three `React.lazy` groups: auth,
  workspace, and settings. Each route owns a Suspense boundary, preserving the
  authenticated shell while its content chunk loads.

## Bundle evidence

Production Vite output, same checkout and dependency graph:

| Initial JavaScript | Before | After | Change |
| --- | ---: | ---: | ---: |
| Raw | 1,132.94 kB | 502.99 kB | -55.6% |
| Gzip | 305.05 kB | 140.37 kB | -54.0% |

New route chunks: auth 16.39 kB, settings 184.00 kB, workspace 363.00 kB,
plus shared definitions 73.41 kB. The post-build step emitted 16 smaller
compressed representations; the initial JavaScript Brotli sidecar is 116.3
KiB on disk.

## Tests

- `internal/server/spa_test.go` drives the real public router and covers Brotli,
  gzip, q-value selection, disabled codings, identity fallback, `Vary`, media
  types, and unchanged cache policy for assets and SPA navigations.
- `web/e2e/precompress.test.ts` drives the post-build CLI against a temporary
  output tree, decompresses both formats, and proves fonts are not duplicated.

## Verification

- `pnpm --dir web run typecheck`: clean.
- `pnpm --dir web test`: 628 passed across 77 files on the rebased head.
- `pnpm --dir web run e2e`: desktop 186 passed / 3 expected skips; mobile
  188 passed / 1 expected skip. The advisory SSE lifecycle and fallback tests
  passed on both projects.
- `go test -count=1 ./...`: 4,063 passed across 69 packages.
- `go vet ./...` and `go build ./...`: clean.
- Two-axis review: Standards CLEAN; Spec CLEAN, including the post-fix recheck.
