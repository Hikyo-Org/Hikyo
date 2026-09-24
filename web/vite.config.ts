/// <reference types="vitest/config" />
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

import { storybookTest } from '@storybook/addon-vitest/vitest-plugin';
import { playwright } from '@vitest/browser-playwright';

import { prototypeMockApi } from './prototype/mock-api.ts';

const here = (p: string) => fileURLToPath(new URL(p, import.meta.url));

// The generated client (aliased below) imports `zod` from clients/ts, which has
// its own lockfile and node_modules. Without that install the bundler treats
// `zod` as an unresolved external and only warns, so the SPA would ship an
// import it cannot load. Refuse up front with the fix in the message instead.
if (!existsSync(here('../clients/ts/node_modules/zod'))) {
  throw new Error('clients/ts is not installed (zod unresolved): run `pnpm --dir clients/ts install --frozen-lockfile` first');
}

// The build is constrained by the server's CSP baseline, not by taste
// (internal/server/spa.go asserts the header; internal/server/spa_test.go
// asserts it has no `unsafe-inline`). Two settings below exist only to keep
// the emitted document legal under it — they are load-bearing, not tuning.
export default defineConfig(({ mode }) => ({
  // Root-only, per the system-architecture ADR: subpath mounting is deferred,
  // which keeps RP ID/origin and asset resolution trivial.
  base: '/',
  plugins: [react(), ...(mode === 'prototype' ? [prototypeMockApi()] : [])],
  resolve: {
    alias: {
      // The generated client is consumed as source rather than as a published
      // package: `clients/ts` emits TypeScript and has no build step, and
      // adding one would put a compiled copy of a generated artifact in the
      // tree for no reader. A pnpm workspace was the alternative and was
      // rejected: it relocates the lockfile and breaks the `client` CI job's
      // frozen-lockfile install.
      '@hikyo/client': here('../clients/ts/src/generated/index.ts'),
      // The operation-bound descriptors (#213): one value per operation binding
      // its call, success status and response parser, so `parsed`/`ok` select an
      // operation instead of pairing a promise and a schema by hand.
      '@hikyo/operations': here('../clients/ts/src/generated/operations.gen.ts'),
      '@hikyo/zod': here('../clients/ts/src/generated/zod.gen.ts'),
      '@hikyo/runtime': here('../clients/ts/src/generated/client.gen.ts'),
      // The client FACTORY, not the shared singleton. `@hikyo/runtime` is the
      // one same-origin `client` the SPA talks to itself through; the workspace
      // tier (#71) needs a SECOND, origin-scoped client per remote, which means
      // `createClient`/`createConfig` — they live in the generated core, not in
      // client.gen.ts, so the alias points one level in.
      '@hikyo/runtime-core': here('../clients/ts/src/generated/client/index.ts'),
    },
  },
  build: {
    outDir: here('../internal/webui/dist'),
    emptyOutDir: true,
    assetsDir: 'assets',
    // No data: URIs. Inlining would need `img-src data:` in the CSP, which is
    // a relaxation bought for a few hundred bytes.
    assetsInlineLimit: 0,
    modulePreload: {
      // The polyfill is injected as an INLINE script, which the CSP forbids.
      // Every browser this ships to supports modulepreload natively.
      polyfill: false,
    },
  },
  // Two vitest projects. `unit` is the original build-config suite — it lives
  // here so it resolves `@hikyo/*` through the same aliases the bundle does; a
  // second config file would be a second place for that mapping to be wrong.
  // `storybook` runs the stories in a headless Chromium and needs a browser, so
  // it is opt-in (`pnpm run test-storybook`); `pnpm run test` stays `unit`-only
  // to keep the browserless CI SPA-verify job (scripts/ci/build-spa.sh) green.
  test: {
    projects: [
      {
        extends: true,
        test: {
          name: 'unit',
          // Playwright owns `.spec.ts` under e2e/flows. Picking those up here
          // would fail on a missing browser, or — worse — skip and look like
          // coverage.
          include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'e2e/**/*.test.ts', 'scripts/**/*.test.ts', '.storybook/**/*.test.ts'],
          environment: 'node',
        },
      },
      {
        extends: true,
        plugins: [storybookTest({ configDir: here('.storybook') })],
        test: {
          name: 'storybook',
          browser: {
            enabled: true,
            headless: true,
            provider: playwright({}),
            instances: [{ browser: 'chromium' }],
          },
        },
      },
    ],
  },
}));