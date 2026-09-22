import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import type { StorybookConfig } from '@storybook/react-vite';

const require = createRequire(import.meta.url);

/**
 * Resolve a Storybook package to its directory from THIS config file.
 *
 * pnpm 11's global virtual store (`virtualStoreType: global` in
 * pnpm-workspace.yaml) links `storybook` from a store outside the project, so
 * a bare `import('@storybook/react-vite')` issued from inside storybook's own
 * chunks has nothing to resolve against. `pnpm exec` hides that by injecting
 * NODE_PATH plus an ESM loader hook through NODE_OPTIONS; plain `node --run
 * test` (the AGENTS.md check) does not, and the vitest config, which loads
 * this file through the storybook vitest plugin, failed with
 * "Cannot find package '@storybook/react-vite'". Absolute paths are
 * Storybook's own answer for strict package layouts.
 */
function getAbsolutePath(value: string): string {
  return dirname(require.resolve(join(value, 'package.json')));
}

// `managerEntries` is a real preset -- Storybook applies it as
// `presets.apply('managerEntries', [], options)` and expects `string[]` back --
// but it is not on the public `StorybookConfig` surface. The intersection types
// it; it is a type, not a cast.
const config: StorybookConfig & { managerEntries: (entries: string[]) => string[] } = {
  stories: ['../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: [
    getAbsolutePath('@storybook/addon-vitest'),
    getAbsolutePath('@storybook/addon-a11y'),
    getAbsolutePath('@storybook/addon-docs'),
    getAbsolutePath('@storybook/addon-mcp'),
    getAbsolutePath('@storybook/addon-designs'),
  ],
  framework: { name: getAbsolutePath('@storybook/react-vite'), options: {} },
  // Design exports rendered by scripts/design/export.ts; gitignored.
  staticDirs: [{ from: '../design/exports', to: '/design' }],
  // Zero-telemetry ADR: no phone-home from local or CI builds.
  core: { disableTelemetry: true },
  managerEntries: (entries) => [...entries, fileURLToPath(new URL('./openpencil-addon.tsx', import.meta.url))],
};
export default config;
