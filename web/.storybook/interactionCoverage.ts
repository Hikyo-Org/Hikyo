import type { ReactRenderer } from '@storybook/react-vite';
import type { Args, PlayFunction } from 'storybook/internal/types';

/** Every opt-in has an explicit appearance replacement, never a theme-wide play switch. */
export const INTERACTION_ONCE = {
  'ui-auth-providerbutton--fires-on-click': {
    reason: 'The callback spy leaves the rendered provider button unchanged.',
    retained: ['ui-auth-providerbutton--google'],
  },
  'routes-providerdiscoveryalert--default': {
    reason: 'The retry callback spy leaves the rendered error alert unchanged.',
    retained: ['routes-providerdiscoveryalert--default'],
  },
  'ui-dialog--backdrop-click': {
    reason: 'Synthetic backdrop events verify dismissal callbacks without closing the controlled dialog.',
    retained: ['ui-dialog--backdrop-click'],
  },
  'ui-auth-loginform--provider-starts-from-step-one': {
    reason: 'The provider callback spy leaves the initial sign-in view unchanged.',
    retained: ['ui-auth-loginform--provider-starts-from-step-one'],
  },
  'pages-overview--create-project-journey': {
    reason: 'Route creation/cache assertions are theme-independent. The appearance pass seeds the same project before mounting, then waits for real navigation to the same populated Projects route with AppRoutes, providers and Shell. Standalone endpoint stories remain in both palettes.',
    retained: ['pages-overview--create-project-journey', 'pages-overview--empty', 'pages-overview--populated', 'routes-projects--empty', 'routes-projects--populated'],
  },
};

export type InteractionOnceId = keyof typeof INTERACTION_ONCE;

/** Only the existing light Vitest project opts in. Manual light Canvas still plays fully. */
export function appearancePass(globals: { hikyoInteractionPass?: string; theme?: string }): boolean {
  if (globals.hikyoInteractionPass === undefined || globals.hikyoInteractionPass === 'full') return false;
  if (globals.hikyoInteractionPass === 'appearance' && globals.theme === 'light') return true;
  throw new Error('Invalid Storybook interaction pass: appearance is restricted to the light test project');
}

/** Retain render, fixtures, before/after hooks and a11y in both palettes. */
export function interactionOnce<TArgs extends Args>(
  id: InteractionOnceId,
  play: PlayFunction<ReactRenderer, TArgs>,
  appearance: PlayFunction<ReactRenderer, TArgs>,
) {
  if (!Object.hasOwn(INTERACTION_ONCE, id)) throw new Error(`Unregistered interaction coverage: ${id}`);
  return (context: Parameters<PlayFunction<ReactRenderer, TArgs>>[0]) => {
    return appearancePass(context.globals) ? appearance(context) : play(context);
  };
}
