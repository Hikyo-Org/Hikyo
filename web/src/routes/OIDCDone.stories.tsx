import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { rememberOIDCReturn, takeOIDCReturn } from '../api/oidcChannel.ts';
import { OIDCDone } from './OIDCDone.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The same-origin return page for browser-started OIDC flows. Everything it
// shows is a pure function of the real location (`purpose`, `state`, `error`)
// and the stored return target. Only the refused and invalid states are
// storied: every success navigates the frame away (login replaces to `/`,
// link replaces to the return target, reauth closes then assigns it). The
// refused link/reauth broadcasts go to a channel nobody listens on.
const STATE = 'state-195';
const RETURN_TO = '/settings#account-security';

const withReturn = (params: Record<string, string>) => () => {
  const original = globalThis.location.href;
  const next = new URL(original);
  for (const [key, value] of Object.entries(params)) next.searchParams.set(key, value);
  globalThis.history.replaceState(null, '', next);
  rememberOIDCReturn(STATE, RETURN_TO);
  return () => {
    takeOIDCReturn(STATE);
    globalThis.history.replaceState(null, '', original);
  };
};

const meta = {
  component: OIDCDone,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
} satisfies Meta<typeof OIDCDone>;

export default meta;
type Story = StoryObj<typeof meta>;

// Opened without a transaction: nothing to return from.
export const NoTransaction: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/without an oidc transaction/i);
    await expect(canvas.getByRole('button', { name: /close this window/i })).toBeEnabled();
  },
};

// The provider refused a sign-in: the way back is the login page.
export const LoginRefused: Story = {
  beforeEach: withReturn({ purpose: 'login', state: STATE, error: 'access_denied' }),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/refused this sign-in/i);
    await expect(canvas.getByRole('link', { name: /return to sign in/i })).toHaveAttribute(
      'href',
      '/login',
    );
  },
};

// The provider refused an identity link: back to account security.
export const LinkRefused: Story = {
  beforeEach: withReturn({ purpose: 'link', state: STATE, error: 'access_denied' }),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/refused this link/i);
    await expect(
      canvas.getByRole('link', { name: /return to account security/i }),
    ).toHaveAttribute('href', RETURN_TO);
  },
};

// The provider refused a reauthentication: back to where it started.
export const ReauthRefused: Story = {
  beforeEach: withReturn({ purpose: 'reauth', state: STATE, error: 'access_denied' }),
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/refused this reauthentication/i);
    await expect(canvas.getByRole('link', { name: 'Back' })).toHaveAttribute('href', RETURN_TO);
  },
};
