import { fileURLToPath } from 'node:url';

import type { StorybookConfig } from '@storybook/react-vite';

// `managerEntries` is a real preset -- Storybook applies it as
// `presets.apply('managerEntries', [], options)` and expects `string[]` back --
// but it is not on the public `StorybookConfig` surface. The intersection types
// it; it is a type, not a cast.
const config: StorybookConfig & { managerEntries: (entries: string[]) => string[] } = {
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
  managerEntries: (entries) => [...entries, fileURLToPath(new URL('./openpencil-addon.tsx', import.meta.url))],
  // The toolbar button ships in every build; the Node side that holds the RPC
  // token is mounted only by the dev server.
  viteFinal: async (config, { configType, port }) => {
    if (configType !== 'DEVELOPMENT') return config;
    // `@storybook/addon-vitest` applies viteFinal too, with configType
    // DEVELOPMENT and no port: that is the Vitest browser server, not the
    // Storybook dev server. There is no origin to allow there, so nothing mounts.
    if (typeof port !== 'number') return config;
    const { openPencilPlugin } = await import('./openpencil-middleware.ts');
    const penPath = fileURLToPath(new URL('../design/hikyo.pen', import.meta.url));
    return { ...config, plugins: [...(config.plugins ?? []), openPencilPlugin(penPath, port)] };
  },
};
export default config;
