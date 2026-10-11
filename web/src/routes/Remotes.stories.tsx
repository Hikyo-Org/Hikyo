import type { Meta, StoryObj } from '@storybook/react-vite';
import { zDirectoryListing, zInstanceConnectionList, zMeta, zRemoteList, zWorkspaceOriginList } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { Remotes } from './Remotes.tsx';

const url = '/api/v1/instance/remotes';
const directory = { identity: 'storybook-local-instance', version: '1.4.0', orgs: [{ name: 'Platform team with a long organisation name', projects: ['api', 'web', 'observability'] }], org_count: 1, project_count: 3 } satisfies z.input<typeof zDirectoryListing>;
const remote = { id: 'rem_00000000-0000-7000-8000-000000000001', name: 'Production service instance with a long name', url: 'https://peer.example', spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=', created_at: '2026-09-01T00:00:00Z', created_by: 'prn_operator', state: 'unreachable', last_attempt_at: '2026-09-01T00:00:00Z', stale: true, stale_for_seconds: 7200 } satisfies z.input<typeof zRemoteList>['items'][number];
const responses = [
  { url: '/api/v1/instance/directory', body: directory },
  { url: '/api/v1/meta', body: { server_version: '1.4.0', api_revision: 1, protocol_capabilities: [], instance_identity: 'storybook-local-instance' } satisfies z.input<typeof zMeta> },
  { url: '/api/v1/instance/connections', body: zInstanceConnectionList.parse({ items: [], count: 0 }) },
  { url: '/api/v1/instance/workspace-origins', body: zWorkspaceOriginList.parse({ items: [], count: 0 }) },
];
const meta = {
  title: 'Pages/Remotes',
  component: Remotes,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url, body: { items: [], count: 0 } satisfies z.input<typeof zRemoteList> }, ...responses] }, docs: { ...topLayerDocs.docs, description: { component: 'Real Remotes screen with local directory transport fixtures. Opening a remote workspace launches a human-owned popup and is deliberately excluded; the separate RemoteCard catalogue retains its handoff states.' } } },
} satisfies Meta<typeof Remotes>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Empty: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText(/No remotes yet/)).toBeVisible(); await expect(canvas.getByRole('heading', { name: 'Connection credentials' })).toBeVisible(); } };
export const LongContent: Story = { parameters: { app: { responses: [{ url, body: { items: [remote], count: 1 } satisfies z.input<typeof zRemoteList> }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('heading', { name: remote.name })).toBeVisible(); } };
export const LoadingDirectory: Story = { parameters: { app: { responses: [{ url, pending: true }, { url: '/api/v1/instance/directory', pending: true }, ...responses.slice(1)] } }, play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText("Loading this instance's directory…")).toBeVisible()); } };
export const PermissionRequired: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('may not hold'); } };
