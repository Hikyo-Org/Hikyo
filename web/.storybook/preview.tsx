import type { Preview } from '@storybook/react-vite'
import { themes } from 'storybook/theming'

import { installAppFetch, withApp } from './withApp.tsx'

import '@fontsource-variable/instrument-sans'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '../src/styles/tokens.css'
import '../src/styles/app.css'
import '../src/ui/ui.css'

// The app delivers its theme through this attribute (see src/app/theme.ts). A
// toolbar switch drives it so designers can flip light/dark; the initial global
// is 'dark', the app's default, so a11y contrast checks and the vitest browser
// run (which never touches the toolbar) stay on the real default. The vitest
// run is repeated with STORYBOOK_THEME=light (`pnpm run test-storybook:light`)
// so the a11y gate covers both palettes, not only the default.
const preview: Preview = {
  initialGlobals: {
    theme: import.meta.env['STORYBOOK_THEME'] === 'light' ? 'light' : 'dark',
    backgrounds: { value: import.meta.env['STORYBOOK_THEME'] === 'light' ? 'light' : 'dark' },
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
    (Story, { globals }) => {
      document.documentElement.dataset.theme = globals.theme
      return <Story />
    },
    withApp,
  ],
  // Install a story's stubbed API responses before its screen mounts.
  beforeEach: installAppFetch,
  // Give every component an autodocs page from its stories + arg types.
  tags: ['autodocs'],
  parameters: {
    // The docs page and the canvas sit on the app's own surface colour, so a
    // sticky strip painted `--bg` (the jump index) is invisible as in the app
    // rather than a dark bar with flush buttons on Storybook's grey.
    backgrounds: {
      options: {
        dark: { name: 'Dark', value: 'oklch(0.19 0.012 220)' },
        light: { name: 'Light', value: 'oklch(0.965 0.008 200)' },
      },
    },
    docs: { theme: themes.dark },
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
