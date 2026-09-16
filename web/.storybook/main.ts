import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';

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

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: [
    getAbsolutePath('@storybook/addon-vitest'),
    getAbsolutePath('@storybook/addon-a11y'),
    getAbsolutePath('@storybook/addon-docs'),
    getAbsolutePath('@storybook/addon-mcp'),
  ],
  framework: { name: getAbsolutePath('@storybook/react-vite'), options: {} },
  // Zero-telemetry ADR: no phone-home from local or CI builds.
  core: { disableTelemetry: true },
};
export default config;
