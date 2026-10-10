import type { Preview } from '@storybook/react-vite'
import { HikyoDocsContainer, StoryTheme } from './DocsContainer.tsx'
import { appTheme } from './docsTheme.ts'
import { createDocsRenderer } from './docsRenderer.ts'

import { installAppFetch, withApp } from './withApp.tsx'

import '../src/styles/index.ts'
import './docs.css'

// The app delivers its theme through this attribute (see src/app/theme.ts). A
// toolbar switch drives it so designers can flip light/dark; the initial global
// is 'dark', the app's default, so a11y contrast checks and the vitest browser
// run (which never touches the toolbar) stay on the real default. The vitest
// run is repeated with STORYBOOK_THEME=light (`pnpm run test-storybook:light`)
// so the a11y gate covers both palettes, not only the default.
const preview: Preview = {
  initialGlobals: {
    theme: import.meta.env['STORYBOOK_THEME'] === 'light' ? 'light' : 'dark',
  },
  globalTypes: {
    theme: {
      description: 'App theme',
      toolbar: {
        title: 'Theme',
        icon: 'circlehollow',
        items: [
          { value: 'dark', title: 'Dark' },
          { value: 'light', title: 'Light' },
        ],
        dynamicTitle: true,
      },
    },
  },
  decorators: [
    (Story, { globals }) => <StoryTheme theme={appTheme(globals)}><Story /></StoryTheme>,
    withApp,
  ],
  // Install a story's stubbed API responses before its screen mounts.
  beforeEach: installAppFetch,
  // Give every component an autodocs page from its stories + arg types.
  tags: ['autodocs'],
  parameters: {
    // App theme owns foreground and surface together through semantic tokens.
    // The separate built-in background toolbar otherwise keeps an !important
    // dark body after a light theme switch, making route text unreadable.
    backgrounds: { disable: true },
    docs: { container: HikyoDocsContainer, renderer: createDocsRenderer },
    controls: {
      matchers: {
       color: /(background|color)$/i,
       date: /Date$/i,
      },
    },

    a11y: {
      // 'todo' - show a11y violations in the test UI only
      // 'error' - fail CI on a11y violations
      // 'off' - skip a11y checks entirely
      test: 'error'
    }
  },
};

export default preview;
