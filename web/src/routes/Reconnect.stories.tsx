import type { Meta, StoryObj } from '@storybook/react-vite';
import { zMeta, zWorkspaceHandoffStarted } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { Reconnect } from './WorkspaceScope.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The state a deep-linked workspace lands in with no bearer. On mount it
// prepares the handoff on the REMOTE origin (GET <origin>/api/v1/meta, then
// POST <origin>/api/v1/auth/workspace/start) so the Reconnect click can open
// the popup synchronously. The click is never fired here: it would open a
// real window on the remote.
const ORIGIN = 'https://hikyo.example.com';

const remoteMeta = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/api\/v1\/meta$/, ...rest });
const metaBody = {
  server_version: '1.4.0',
  api_revision: 1,
  protocol_capabilities: [],
} satisfies z.input<typeof zMeta>;
const started: MockRoute = {
  url: /\/api\/v1\/auth\/workspace\/start$/,
  method: 'POST',
  body: {
    handoff: 'wsh_01989abc-def0-7123-8123-000000000001',
    state: 'state-195',
    expires_at: '2099-08-23T12:00:00Z',
  } satisfies z.input<typeof zWorkspaceHandoffStarted>,
};

const meta = {
  component: Reconnect,
  tags: ['ai-generated'],
  args: { origin: ORIGIN, name: 'production' },
  parameters: { ...topLayerDocs, app: { responses: [] } },
} satisfies Meta<typeof Reconnect>;

export default meta;
type Story = StoryObj<typeof meta>;

// The remote has not answered the meta read yet.
export const Contacting: Story = {
  parameters: { app: { responses: [remoteMeta({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Contacting…' })).toBeDisabled());
  },
};

// The transaction is open on the remote: one click away from its popup.
export const Ready: Story = {
  parameters: { app: { responses: [remoteMeta({ body: metaBody }), started] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Reconnect' })).toBeEnabled();
  },
};

// The remote answered an error: the message names it, with a retry.
export const Failed: Story = {
  parameters: { app: { responses: [remoteMeta({ status: 500, body: { error: 'remote down' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent(`${ORIGIN} answered 500.`);
    await expect(canvas.getByRole('button', { name: 'Try again' })).toBeEnabled();
  },
};
