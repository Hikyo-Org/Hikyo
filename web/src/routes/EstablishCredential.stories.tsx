import type { Meta, StoryContext, StoryObj } from '@storybook/react-vite';
import { zRecoveryBeginResult } from '@hikyo/zod';
import { expect, userEvent } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { EstablishCredential } from './EstablishCredential.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The public, sessionless page where a display-once authority becomes a
// password (registry surface `establish-credential`, path /establish), and its
// `?mode=recover` entry that spends one recovery code first. The two POSTs
// (establish, recovery/begin) are the only transport; the play fills the form
// and submits so the refused and finished states are the page's own.
const PASSWORD = 'a first password long enough';

const establish = (rest: Partial<MockRoute>): MockRoute => ({
  url: '/api/v1/auth/credential/establish',
  method: 'POST',
  ...rest,
});

const recovered: MockRoute = {
  url: '/api/v1/auth/recovery/begin',
  method: 'POST',
  body: {
    authority: 'hik_cea_recovered_authority_value',
    expires_at: '2099-08-23T12:00:00Z',
  } satisfies z.infer<typeof zRecoveryBeginResult>,
};

const app = (rest: { path?: string; responses?: MockRoute[] } = {}) => ({
  path: '/establish',
  routePath: '/establish',
  responses: [],
  ...rest,
});

/** Paste an authority and type the password twice, then submit. */
async function establishWith(canvas: StoryContext['canvas']) {
  await userEvent.type(canvas.getByLabelText('Setup authority'), 'hik_cea_authority_value_1234');
  await userEvent.type(canvas.getByLabelText('New password'), PASSWORD);
  await userEvent.type(canvas.getByLabelText('Repeat the password'), PASSWORD);
  await userEvent.click(canvas.getByRole('button', { name: 'Establish credential' }));
}

const meta = {
  component: EstablishCredential,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app() },
} satisfies Meta<typeof EstablishCredential>;

export default meta;
type Story = StoryObj<typeof meta>;

// The authority form: a masked authority, a password twice, the recovery link.
export const Initial: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'Establish your credential' })).toBeVisible();
    await expect(canvas.getByLabelText('Setup authority')).toHaveAttribute('type', 'password');
    await expect(canvas.getByRole('link', { name: /recover with a code/i })).toBeVisible();
  },
};

// The two passwords differ: refused on the page, before any request.
export const Mismatch: Story = {
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Setup authority'), 'hik_cea_authority_value_1234');
    await userEvent.type(canvas.getByLabelText('New password'), PASSWORD);
    await userEvent.type(canvas.getByLabelText('Repeat the password'), `${PASSWORD} but different`);
    await userEvent.click(canvas.getByRole('button', { name: 'Establish credential' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/the two passwords differ/i);
  },
};

// The server refused the authority: one uniform sentence, the form stays.
export const Refused: Story = {
  parameters: {
    app: app({ responses: [establish({ status: 401, body: { error: 'unauthenticated' } })] }),
  },
  play: async ({ canvas }) => {
    await establishWith(canvas);
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/the authority was not accepted/i);
    await expect(canvas.getByRole('button', { name: 'Establish credential' })).toBeEnabled();
  },
};

// The credential is set (204) and the authority spent: on to sign-in.
export const Done: Story = {
  parameters: { app: app({ responses: [establish({ status: 204 })] }) },
  play: async ({ canvas }) => {
    await establishWith(canvas);
    await expect(await canvas.findByRole('heading', { name: 'Credential established' })).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
  },
};

// `?mode=recover`: username plus one recovery code.
export const Recover: Story = {
  parameters: { app: app({ path: '/establish?mode=recover' }) },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'Recover your account' })).toBeVisible();
    await expect(canvas.getByLabelText('Recovery code')).toHaveAttribute('type', 'password');
    await expect(canvas.getByRole('button', { name: /have a setup authority instead/i })).toBeVisible();
  },
};

// The recovery code was accepted: the authority is held, only the password is asked.
export const Recovered: Story = {
  parameters: { app: app({ path: '/establish?mode=recover', responses: [recovered] }) },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Username'), 'alice');
    await userEvent.type(canvas.getByLabelText('Recovery code'), 'aaaa-bbbb');
    await userEvent.click(canvas.getByRole('button', { name: 'Continue' }));
    await expect(await canvas.findByText(/your recovery code was accepted/i)).toBeVisible();
    await expect(canvas.queryByLabelText('Setup authority')).toBeNull();
    await expect(canvas.getByLabelText('New password')).toBeVisible();
  },
};
