# #1097: Hono repair and deepmerge development exposure

Owner: [#1097](https://github.com/Hikyo-Org/Hikyo/issues/1097).
Parent: [#79](https://github.com/Hikyo-Org/Hikyo/issues/79).
Source baseline: `ce5e85835383b6428676ff595e2a0c1a72f6a088`, inspected 2026-10-29.
The final candidate and release acceptance remain the parent's responsibility.

## Scope and live disposition

| Dependabot alert | Package and owner | Disposition |
| --- | --- | --- |
| [151](https://github.com/Hikyo-Org/Hikyo/security/dependabot/151), medium | Web development dependency: `@open-pencil/cli@0.15.0` -> `@open-pencil/mcp@0.15.0` -> `@hono/node-server@1.19.17` | Repair with `2.1.3`, the first patched release. Default-branch alert closure requires merge and dependency-graph ingestion. |
| [148](https://github.com/Hikyo-Org/Hikyo/security/dependabot/148), low | Docs development dependency: `workbox-build@7.4.1` -> `@rollup/plugin-node-resolve@16.0.3` -> `deepmerge@4.3.1` | Explicit source-based non-reachability disposition for this pinned consumer; no patched release exists. Keep the alert OPEN, with the limits and recheck trigger below. |

Dependabot alert 148 is unrelated to deferred feature issue #148. This task does
not reopen that feature. Vitest alerts 104/105 and merged PR #1078 are completed
work and are not part of this PR's diff or queue.

## Hono: smallest compatible repair

[GHSA-rmxm-3fg6-px4f](https://github.com/advisories/GHSA-rmxm-3fg6-px4f)
affects `>=1.19.10 <2.1.3`. Its `serveStatic` double decoding can skip middleware
on a static-file prefix. Upstream identifies `2.1.3` as the first fixed release;
there is no patched 1.x release in the published advisory.

The sole pinned owner is OpenPencil's development MCP package. Its
[`createAppServer`](https://github.com/open-pencil/open-pencil/blob/v0.15.0/packages/mcp/src/server/lifecycle.ts#L30)
uses `getRequestListener(app.fetch)` with `node:http.createServer`. Inspection of
its published runtime bundle found no `serveStatic`, `serve-static`, or Vercel
adapter import. This is an exposure limit for the shipped development consumer,
not a declaration that the dependency itself is fixed at 1.x.

A newer OpenPencil MCP release still requests `^1.19.17`, and upgrading that
owner would also change the broader OpenPencil graph. Instead, scope the
compatibility override to the current owner:

```yaml
'@open-pencil/mcp@0.15.0>@hono/node-server': 2.1.3
```

Only that package version changes in the regenerated web lockfile. Hono remains
`4.13.12`, which satisfies the adapter's `hono: ^4` peer. The
[2.x breaking changes](https://github.com/honojs/node-server/releases/tag/v2.0.0)
are Node 18 removal and removal of the Vercel adapter. The pinned Node `26.12.1`
satisfies `>=20`, and OpenPencil uses the retained `getRequestListener` API.
The focused regression tests resolve the adapter from OpenPencil's actual
package context, rather than testing an unrelated root-level copy.

Hikyo's Go server and built SPA do not use this Node development adapter. No
production exploit, deployed-provider proof, or real OpenPencil session is
claimed; the import check does not start OpenPencil's server or access its token.

## Deepmerge: bounded disposition, not a patched-version claim

[GHSA-gcqx-w48m-w2h4](https://github.com/advisories/GHSA-gcqx-w48m-w2h4)
affects `>=4.2.0 <=4.3.1`; the npm latest version is `4.3.1` and the official
advisory lists no fixed release. The library can poison the prototype of its
returned object when attacker-controlled keys meet a susceptible target shape.
A synthetic positive control reproduced that behavior with a null placeholder,
without modifying global `Object.prototype`. The advisory is not dismissed as
a false positive.

The complete docs lockfile contains one deepmerge version and one consumer.
The relevant pinned call chain is:

```text
scripts/build-pwa.mjs: buildPwa
  workbox-build@7.4.1: generateSW -> bundle
    @rollup/plugin-node-resolve@16.0.3: nodeResolve()
      module initialization: deepFreeze(deepMerge({}, defaults))
```

[Workbox's bundler](https://github.com/GoogleChrome/workbox/blob/v7.4.1/packages/workbox-build/src/lib/bundle.ts#L45)
constructs `nodeResolve()` without caller options. More decisively, the
[resolver source](https://github.com/rollup/plugins/blob/node-resolve-v16.0.3/packages/node-resolve/src/index.js#L48)
contains exactly one deepmerge call, cloning its fixed internal `defaults` into
an ordinary empty object. Those defaults contain fixed arrays and booleans.
Caller options are combined separately with `{ ...defaults, ...opts }`, without
deepmerge. Document text, request bodies, package metadata, and visitor input
are not arguments to the deepmerge call. The plugin runs in the build process,
not the emitted service-worker request handler.

Published source archives for OpenPencil MCP, Workbox and the resolver were
checked against their committed lockfile SHA-512 integrity values. A local
probe also loaded the actual resolver, checked its fixed defaults and sole
callsite, and contrasted that use with the vulnerable-library positive control.
The docs manifest, lockfile and overrides are unchanged. No downgrade, unreviewed
replacement merge library, copied third-party security patch, or automatic
alert dismissal is introduced.

**Recheck trigger:** any Workbox/resolver/deepmerge version or owner change, new
consumer, or change to the deepmerge callsite/input provenance invalidates this
disposition until reviewed again. Also recheck for an upstream patched release
before final candidate qualification. Parent #79 owns acceptance of this
recorded disposition; this lane does not edit the canonical acceptance ledger.

## Validation and remaining gates

Node `26.12.1`, pnpm `11.24.0`:

- PASS: frozen web, client and docs installations; web peer check.
- PASS: dependency traversal shows one patched Hono adapter, exclusively under
  OpenPencil MCP, and one deepmerge copy under the pinned Workbox resolver.
- PASS: OpenPencil MCP ESM import and `startServer` export, without server startup.
- PASS: two new real HTTP adapter tests (MCP POST body/header forwarding and
  streamed response bytes); with focused Storybook configuration owners,
  3 files / 10 tests pass with one worker.
- PASS: web typecheck and lint, with no suppressed errors.
- PASS: six docs PWA asset-graph tests, plus the source/disposition probe.
- PASS: complete-PR `fast-feedback.sh --working-tree --plan` selects the web
  domain. Full shared feedback, Storybook suites and documentation builds are
  NOTRUN locally to avoid overlapping the active host campaigns. Exact-head
  hosted feedback and protected merge-group checks remain required.
- Independent adversarial Standards and Spec review outcomes are recorded in
  the PR. Cross-provider review is not requested and is not substituted.

Standards review identified one asynchronous-test gap: an immediately closed
producer could hide buffering until EOF. The test now receives the first event
while the producer remains open, then releases the second event, with reader
and server cleanup in `finally`. An isolated buffering mutation fails this
test while the pinned adapter passes. Spec review reported zero findings.

Focused reproduction from the repository root, after frozen installations:

```sh
pnpm --dir web peers check
pnpm --dir web why @hono/node-server
pnpm --dir docs/site why deepmerge
pnpm --dir web exec vitest run --project unit scripts/hono-node-server.test.ts .storybook/docgen.test.ts .storybook/interactionCoverage.test.ts --maxWorkers=1
pnpm --dir web run typecheck
pnpm --dir web run lint
node --test --test-concurrency=1 docs/site/scripts/test-pwa-assets.mjs
scripts/ci/fast-feedback.sh --working-tree --plan
```

After the repair merges, query Dependabot 151 for its actual `state` and
`fixed_at`. Alert 148 remains an explicit open disposition, not a fixed alert.
No release, deployment, stable-policy activation or production mutation is
part of this task.
