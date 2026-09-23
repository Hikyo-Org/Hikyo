import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAccountProfile } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { AccountProfile } from './AccountProfile.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The profile panel reads GET /api/v1/me/profile under the real AuthProvider
// (the form remounts on the signed-in principal). The save is a PATCH the play
// never fires. Bodies are typed against the response schema, so contract drift
// fails in tsc before the browser run.
const local = {
  username: 'alice',
  display_name: 'Alice Example',
  email: 'alice@example.com',
  managed: false,
  username_editable: true,
} satisfies z.infer<typeof zAccountProfile>;

const profile = (rest: Partial<MockRoute>): MockRoute => ({ url: '/api/v1/me/profile', ...rest });

const meta = {
  component: AccountProfile,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { auth: true, responses: [profile({ body: local })] } },
} satisfies Meta<typeof AccountProfile>;

export default meta;
type Story = StoryObj<typeof meta>;

// A local account: username, display name and email are all editable, and the
// save stays disabled until something changes.
export const Editable: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText('Username')).toHaveValue('alice');
    await expect(canvas.getByRole('button', { name: 'Save profile' })).toBeDisabled();
  },
};

// A changed username asks for the existing proof before it can be saved.
export const ProofRequired: Story = {
  play: async ({ canvas }) => {
    const username = await canvas.findByLabelText('Username');
    await userEvent.clear(username);
    await userEvent.type(username, 'alice-new');
    await expect(canvas.getByLabelText('Code or password')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Save profile' })).toBeDisabled();
  },
};

// An identity provider owns the handle: no username field, read-only name.
export const Managed: Story = {
  parameters: {
    app: { auth: true, responses: [profile({ body: { ...local, managed: true } })] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText('Display name')).toHaveAttribute('readonly');
    await expect(canvas.queryByLabelText('Username')).toBeNull();
    await expect(canvas.getByText(/your identity provider manages your username/i)).toBeVisible();
  },
};

// An SSO account with a local display name: no handle to edit, name and email are.
export const ProviderSignIn: Story = {
  parameters: {
    app: { auth: true, responses: [profile({ body: { ...local, username_editable: false } })] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText('Display name')).not.toHaveAttribute('readonly');
    await expect(canvas.queryByLabelText('Username')).toBeNull();
    await expect(canvas.getByText(/you sign in through your identity provider/i)).toBeVisible();
  },
};

// The profile read never settles: the loading status stands.
export const Loading: Story = {
  parameters: { app: { auth: true, responses: [profile({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('status')).toHaveTextContent(/loading your profile/i));
  },
};

// The profile read failed: no empty editable form, an alert with a retry.
export const Failed: Story = {
  parameters: {
    app: { auth: true, responses: [profile({ status: 500, body: { error: 'profile read failed' } })] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/your profile could not be loaded/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Try again' })).toBeVisible();
  },
};
