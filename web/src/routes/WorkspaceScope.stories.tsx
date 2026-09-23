import type { Meta, StoryObj } from '@storybook/react-vite';
import { zMeta, zRemoteList, zSessionList } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { forgetWorkspace, rememberWorkspace } from '../api/workspace.ts';
import { WorkspaceScope } from './WorkspaceScope.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The boundary between operating this instance and a remote. It reads the
// remotes directory (GET /api/v1/instance/remotes), looks the bearer up in the
// in-memory workspace store, and, once connected, gates the children on the
// remote's live meta read (GET <origin>/api/v1/meta). The liveness probe
// (GET <origin>/api/v1/me/sessions, every 5s) gets a well-formed answer so a
// connected story stays connected instead of striking out to Reconnect.
const ORIGIN = 'https://hikyo.example.com';
const NAME = 'production';

const withBearer = () => {
  rememberWorkspace({
    origin: ORIGIN,
    value: 'wsb_story_bearer',
    session: 'ses_01989abc-def0-7123-8123-000000000010',
    idleExpiresAt: '2099-08-22T10:30:00Z',
    absoluteExpiresAt: '2099-08-22T18:00:00Z',
  });
  return () => {
    forgetWorkspace(ORIGIN);
  };
};

const directory = {
  items: [
    {
      id: 'rem_01989abc-def0-7123-8123-000000000001',
      name: NAME,
      url: ORIGIN,
      spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
      created_at: '2026-08-01T09:00:00Z',
      created_by: 'prn_dana',
      state: 'ok',
      last_attempt_at: '2026-09-01T09:00:00Z',
      stale: false,
    },
  ],
  count: 1,
} satisfies z.infer<typeof zRemoteList>;

const remotes = (rest: Partial<MockRoute>): MockRoute => ({
  url: '/api/v1/instance/remotes',
  ...rest,
});
const remoteMeta = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/api\/v1\/meta$/, ...rest });
const metaBody = {
  server_version: '1.4.0',
  api_revision: 1,
  protocol_capabilities: [],
} satisfies z.infer<typeof zMeta>;
const liveness: MockRoute = {
  url: /\/api\/v1\/me\/sessions$/,
  body: { items: [], count: 0 } satisfies z.infer<typeof zSessionList>,
};

const meta = {
  component: WorkspaceScope,
  tags: ['ai-generated'],
  args: { remote: NAME, children: <p>The product surface, pointed at the remote.</p> },
  parameters: { ...topLayerDocs, app: { responses: [] } },
} satisfies Meta<typeof WorkspaceScope>;

export default meta;
type Story = StoryObj<typeof meta>;

// The directory has not answered: the loading status stands.
export const Loading: Story = {
  parameters: { app: { responses: [remotes({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('status')).toHaveTextContent(/loading/i));
  },
};

// No remote of this name is configured: a deep link to nowhere.
export const UnknownRemote: Story = {
  parameters: { app: { responses: [remotes({ body: { items: [], count: 0 } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: /unknown remote/i })).toBeVisible();
    await expect(canvas.getByText(NAME)).toBeVisible();
  },
};

// The remote is known but no bearer is held (every reload, every kill switch):
// the gate routes to Reconnect. Its own phases live in Reconnect.stories.
export const ReconnectRequired: Story = {
  parameters: {
    app: { responses: [remotes({ body: directory }), remoteMeta({ pending: true })] },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: `Session expired on ${ORIGIN}` }),
    ).toBeVisible();
  },
};

// A bearer is held and the live compatibility read is in flight.
export const Checking: Story = {
  beforeEach: withBearer,
  parameters: {
    app: { responses: [remotes({ body: directory }), remoteMeta({ pending: true }), liveness] },
  },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByRole('status')).toHaveTextContent(`Checking ${ORIGIN}`),
    );
  },
};

// The compatibility read failed on the remote: the gate closes with the
// failure named, no children mounted.
export const Failed: Story = {
  beforeEach: withBearer,
  parameters: {
    app: {
      responses: [
        remotes({ body: directory }),
        remoteMeta({ status: 500, body: { error: 'remote down' } }),
        liveness,
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /cannot operate this remote/i }),
    ).toBeVisible();
    await expect(canvas.getByRole('alert')).toHaveTextContent(/500/);
    await expect(canvas.getByRole('alert')).toHaveTextContent(`${ORIGIN} answered 500.`);
    await expect(canvas.queryByText(/the product surface/i)).toBeNull();
  },
};

// Connected: the persistent trust banner over the children.
export const Connected: Story = {
  beforeEach: withBearer,
  parameters: {
    app: { responses: [remotes({ body: directory }), remoteMeta({ body: metaBody }), liveness] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/the product surface/i)).toBeVisible();
    await expect(canvas.getByRole('status')).toHaveTextContent(`Operating ${ORIGIN}`);
    await expect(
      canvas.getByRole('button', { name: `Exit the workspace on ${ORIGIN}` }),
    ).toBeEnabled();
  },
};
