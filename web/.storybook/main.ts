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
};
export default config;
