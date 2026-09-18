import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { RemoteCard } from './Remotes.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

type Props = Parameters<typeof RemoteCard>[0];
type Remote = Props['remote'];

const base: Remote = {
  id: 'rem_01989abc-def0-7123-8123-000000000001',
  name: 'peer-one',
  url: 'https://peer-one.example',
  spki_pin: 'sha256:AAAABBBBCCCCDDDDEEEEFFFF',
  created_at: '2026-07-01T09:00:00Z',
  created_by: 'operator@hikyo.example',
  state: 'ok',
  last_attempt_at: '2026-08-23T08:00:00Z',
  observed_at: '2026-08-23T08:00:00Z',
  stale: false,
};

// The card stages a workspace handoff on mount: a live meta read then a start
// POST, both cross-origin to the remote. The harness matches by path, so these
// two rows let the launcher settle to its ready "Continue to …" label. Widened
// (populated directory facts) so the story shows the dense card, not a bare one.
const handoffResponses: readonly MockRoute[] = [
  {
    url: '/api/v1/meta',
    body: { server_version: '1.4.0', api_revision: 1, protocol_capabilities: [] },
  },
  {
    url: '/api/v1/auth/workspace/start',
    method: 'POST',
    body: {
      handoff: 'hnd_01989abc-def0-7123-8123-000000000009',
      state: 'state-token-0001',
      expires_at: '2026-08-23T08:05:00Z',
    },
  },
];

const meta = {
  component: RemoteCard,
  tags: ['ai-generated'],
  // The card renders a top-level <li>; the screen owns a <ul className="remotes">
  // around the list, so the story supplies the same context (axe listitem rule).
  decorators: [
    (Story) => (
      <ul className="remotes">
        <Story />
      </ul>
    ),
  ],
  parameters: { ...topLayerDocs, app: { responses: handoffResponses } },
  args: { remote: base, duplicateIdentity: false },
} satisfies Meta<typeof RemoteCard>;

export default meta;
type Story = StoryObj<typeof meta>;

// A reachable remote with a populated directory: facts, org list, and a ready
// launcher once the handoff settles.
export const Healthy: Story = {
  args: {
    remote: {
      ...base,
      identity: 'peer-one-identity',
      version: '1.4.0',
      org_count: 2,
      project_count: 5,
      orgs: [
        { name: 'platform', projects: ['api', 'web'] },
        { name: 'data', projects: ['warehouse'] },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('button', { name: /continue to https:\/\/peer-one\.example to sign in/i }),
    ).toBeEnabled();
    await expect(canvas.getByRole('heading', { name: 'peer-one' })).toBeVisible();
    await expect(canvas.getByText('Reachable')).toBeVisible();
  },
};

// Unreachable and stale: directory facts fall back to "absent", the staleness
// line explains how long, and the last-known snapshot still renders.
export const Unreachable: Story = {
  args: {
    remote: {
      ...base,
      name: 'peer-down',
      state: 'unreachable',
      stale: true,
      stale_for_seconds: 7200,
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'peer-down' })).toBeVisible();
    await expect(canvas.getByText(/unreachable for/i)).toBeVisible();
  },
};

// A rejected credential is its own loud recovery line with a link to fix it on
// the peer.
export const CredentialRejected: Story = {
  args: {
    remote: { ...base, name: 'peer-rejected', state: 'credential-rejected' },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'peer-rejected' })).toBeVisible();
    await expect(canvas.getByText(/pinned credential was rejected/i)).toBeVisible();
  },
};

// Two entries share one identity: neither is served. The launcher is disabled
// and no handoff is staged.
export const Duplicate: Story = {
  args: {
    remote: { ...base, name: 'peer-dup', identity: 'shared-identity' },
    duplicateIdentity: true,
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'peer-dup' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /open workspace/i })).toBeDisabled();
    await expect(canvas.getByText(/another entry resolves to the same instance identity/i)).toBeVisible();
  },
};
