import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { CLILogin } from './CLILogin.tsx';

const state = 'A'.repeat(43);
const transaction = (reference: string, transport: 'loopback' | 'device') => ({
  url: `/api/v1/auth/cli-login/transactions/${reference}`,
  body: {
    reference,
    transport,
    instance: 'https://vault.example',
    redirect_uri: transport === 'loopback' ? 'http://127.0.0.1:12345/callback' : '',
    expires_at: '2099-08-23T12:00:00Z',
  },
});
const meta = {
  title: 'Pages/CLILogin',
  id: 'routes-clilogin',
  component: CLILogin,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { auth: true, routePath: '/login/cli', responses: [] } },
} satisfies Meta<typeof CLILogin>;
export default meta;
type Story = StoryObj<typeof meta>;
export const LoopbackApproval: Story = {
  parameters: {
    app: {
      auth: true,
      path: `/login/cli?state=${state}`,
      responses: [transaction(state, 'loopback')],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: 'Authorize CLI sign-in' }),
    ).toBeVisible();
    await expect(canvas.getByText('Separate CLI session')).toBeVisible();
  },
};
export const DeviceApproval: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/login/cli?user_code=ABCDEFGH',
      responses: [transaction('ABCDEFGH', 'device')],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('ABCDEFGH')).toBeVisible();
  },
};
export const EnterDeviceCode: Story = {
  parameters: { app: { auth: true, path: '/login/cli?device=1' } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByLabelText('Device code')).toBeVisible());
  },
};
export const SignIn: Story = {
  parameters: {
    app: {
      auth: true,
      path: `/login/cli?state=${state}`,
      responses: [
        { url: '/api/v1/auth/whoami', status: 401, body: { error: 'unauthenticated' } },
        {
          url: '/api/v1/auth/methods',
          body: {
            local_login_enabled: true,
            providers: [],
            signup_open: true,
            signup_paused: false,
            signup_methods: ['local'],
          },
        },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Sign in to Hikyo' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Create an account' })).toBeNull();
  },
};
