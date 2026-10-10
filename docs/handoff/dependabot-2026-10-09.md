# Dependabot vulnerability remediation, 2026-10-09

Endpoint: one PR for review. Merge, release, and deployment are separate.
Base: d9f7999645d8fc47f1bd7d22918b3fb900b97a7e.

## Changes and alert coverage

All 18 open alerts returned by the GitHub API on 2026-10-09 are covered.

| Package scope | Package | Updated version | Alert numbers |
| --- | --- | --- | --- |
| web | source-map-js | 1.2.2 | 75 |
| docs/site | sharp, including platform/libvips packages | 0.35.5 | 67, 70 |
| docs/site | smol-toml | 1.9.0 | 69 |
| docs/site | source-map-js | 1.2.2 | 68 |
| docs/site | devalue, including its existing override | 5.9.3 | 60, 61, 62, 63, 64, 65 |
| scripts/mcp-conformance | @modelcontextprotocol/client | 2.2.0 | 74 |
| scripts/mcp-conformance | @modelcontextprotocol/sdk | 1.32.1 | 73 |
| scripts/mcp-conformance | source-map-js | 1.2.2 | 72 |
| scripts/mcp-conformance | proxy-addr | 2.0.8 | 71 |
| scripts/mcp-conformance | hono | 4.13.13 | 58 |
| scripts/mcp-conformance | ip-address | 10.7.3 | 52, 53 |

Inspector moves from 2.5.0 to 2.10.1, using its upstream compatible client/core/server 2.2.0 graph rather than overriding its exact SDK pins. Conformance remains 0.2.0-alpha.11, with the existing protocol version and failure baseline.

The OAuth advisory also discusses persisted credentials and bundled providers. The repository smoke test is explicitly unauthenticated and tenant-free, with no OAuth provider or persisted production bearer. This PR does not migrate external Inspector users' credential stores. See https://github.com/advisories/GHSA-6qxp-vccf-f47h.

## Local evidence

- Frozen installs pass in web, docs/site, and scripts/mcp-conformance. The existing clients/ts package was installed to satisfy the web typecheck imports.
- Each affected package's pnpm audit --json reports zero vulnerabilities at every severity.
- Alert-by-alert manifest/lockfile check: 18/18 resolve to versions at or above their first patched version.
- Web: typecheck, lint, 167 test files / 1,511 tests, production build and precompression pass.
- Docs: pnpm run verify passes, including Astro diagnostics, 76-page build, OSS/PWA/CSP/LLMS gates and unvisited-route offline browser navigation. Analytics is disabled in this local build because deploy configuration is not required.
- MCP: scripts/ci/check-mcp-conformance.sh passes Inspector tools/list and server-stateless, tools-list and caching scenarios against the existing baseline.
- Standards and specification reviews found no issues. Cross-provider review was not run.

## Delivery and follow-up

Remote CI is separate from these local results. After merge, query the repository's open Dependabot alerts and verify that these 18 numbers have closed. No alerts were dismissed. Deployment is outside this PR's scope.
