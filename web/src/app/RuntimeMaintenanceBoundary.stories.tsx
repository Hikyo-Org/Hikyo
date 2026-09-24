import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { makeQueryClient } from './queryClient.ts';
import { RuntimeMaintenanceBoundary } from './RuntimeMaintenanceBoundary.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The fence around the whole app during an upgrade or an outage. It polls
// GET /api/v1/runtime/status (every 2s while blocked) and, when blocked,
// makes the children inert under a non-dismissable dialog. The ready state is
// not storied: it is the app underneath, and the component seeds it before the
// first poll settles, so a play could not tell a fixture from the seed. The
// child is a real input so the fence is visible: focus cannot reach it while
// blocked.
const status = (rest: Partial<MockRoute>): MockRoute => ({ url: '/api/v1/runtime/status', ...rest });

const meta = {
  component: RuntimeMaintenanceBoundary,
  tags: ['ai-generated'],
  args: {
    failure: null,
    refreshSession: fn<(signal?: AbortSignal) => Promise<void>>(),
    queries: makeQueryClient(),
    children: <input aria-label="Draft" defaultValue="Keep this draft" />,
  },
  parameters: { ...topLayerDocs, app: { responses: [] } },
} satisfies Meta<typeof RuntimeMaintenanceBoundary>;

export default meta;
type Story = StoryObj<typeof meta>;

// A confirmed upgrade in progress: the phase is announced and editing paused.
export const Maintenance: Story = {
  parameters: {
    app: { responses: [status({ status: 503, body: { state: 'maintenance', phase: 'migration' } })] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Hikyo is upgrading' })).toBeVisible();
    await expect(canvas.getByRole('status')).toHaveTextContent(/updating the database/i);
    await expect(canvas.getByLabelText('Draft').closest('[inert]')).not.toBeNull();
  },
};

// The upgrade could not complete safely: an operator must step in.
export const RecoveryRequired: Story = {
  parameters: {
    app: { responses: [status({ body: { state: 'recovery-required', phase: null } })] },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: 'Recovery needs an operator' }),
    ).toBeVisible();
    await expect(canvas.getByRole('status')).toHaveTextContent(/an operator must recover it/i);
  },
};

// The status endpoint answered something that is not a status (a proxy's HTML
// 503, an offline socket): the cause is unconfirmed, so it only reconnects.
export const Reconnecting: Story = {
  parameters: { app: { responses: [status({ status: 500, body: { error: 'gateway' } })] } },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: 'Reconnecting to Hikyo' }),
    ).toBeVisible();
    await expect(canvas.getByRole('status')).toHaveTextContent(/cannot reach its home instance/i);
  },
};
