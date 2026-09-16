import { fileURLToPath } from 'node:url';

import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: [
    '@storybook/addon-vitest',
    '@storybook/addon-a11y',
    '@storybook/addon-docs',
    '@storybook/addon-mcp',
    '@storybook/addon-designs',
  ],
  framework: '@storybook/react-vite',
  // Design exports rendered by scripts/design/export.ts; gitignored.
  staticDirs: [{ from: '../design/exports', to: '/design' }],
  // Zero-telemetry ADR: no phone-home from local or CI builds.
  core: { disableTelemetry: true },
  managerEntries: (entries = []) => [...entries, fileURLToPath(new URL('./openpencil-addon.tsx', import.meta.url))],
  // The toolbar button ships in every build; the Node side that holds the RPC
  // token is mounted only by the dev server.
  viteFinal: async (config, { configType, port }) => {
    if (configType !== 'DEVELOPMENT') return config;
    const { openPencilPlugin } = await import('./openpencil-middleware.ts');
    const penPath = fileURLToPath(new URL('../design/hikyo.pen', import.meta.url));
    return { ...config, plugins: [...(config.plugins ?? []), openPencilPlugin(penPath, port)] };
  },
};
export default config;
