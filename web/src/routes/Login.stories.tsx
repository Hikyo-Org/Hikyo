import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAuthMethods } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import { Login } from './Login.tsx';

// Logged-out screen. `useLogin` reads `useAuth`, so it needs AuthProvider even
// though it is the sign-in page — a whoami 401 gives the honest anonymous state.
// Load-time calls: GET /api/v1/auth/whoami (401) and GET /api/v1/auth/methods.
// The login/passkey/OIDC/SAML actions are mutations the play never fires (they
// would hit the network or the WebAuthn prompt). See .storybook/withApp.tsx.
// The methods body is `useAuthMethods`'s parsed payload, so it is typed against
// its schema — tsc catches contract drift before the browser run.
const withProviders = {
  local_login_enabled: true,
  providers: [
    { slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' },
    { slug: 'sso', display_name: 'SAML SSO', kind: 'saml' },
  ],
} satisfies z.infer<typeof zAuthMethods>;
const localOnly = { local_login_enabled: true, providers: [] } satisfies z.infer<typeof zAuthMethods>;

const meta = {
  component: Login,
  tags: ['ai-generated'],
  parameters: {
    app: {
      auth: true,
      responses: [
        { url: '/api/v1/auth/whoami', status: 401, body: { error: 'unauthenticated' } },
        { url: '/api/v1/auth/methods', body: withProviders },
      ],
    },
  },
} satisfies Meta<typeof Login>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithProviders: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /sign in to hikyo/i })).toBeVisible();
    await expect(canvas.getByLabelText('Username')).toBeVisible();
    await expect(canvas.getByLabelText('Password')).toBeVisible();
    await expect(await canvas.findByRole('button', { name: /continue with corporate idp/i })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /continue with saml sso/i })).toBeVisible();
  },
};

export const LocalOnly: Story = {
  parameters: {
    app: {
      auth: true,
      responses: [
        { url: '/api/v1/auth/whoami', status: 401, body: { error: 'unauthenticated' } },
        { url: '/api/v1/auth/methods', body: localOnly },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: /^sign in$/i })).toBeVisible();
    // The Sign in button renders unconditionally, so it is no proof the methods
    // query settled. Wait for the loading status to clear first, or the provider
    // absence would pass vacuously during the pending window.
    await waitFor(() => expect(canvas.queryByText(/loading sign-in methods/i)).toBeNull());
    await expect(canvas.queryByRole('button', { name: /continue with/i })).toBeNull();
  },
};
