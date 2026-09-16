import type { Preview } from '@storybook/react-vite'

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
// run (which never touches the toolbar) stay on the real default.
const preview: Preview = {
  initialGlobals: { theme: 'dark' },
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
