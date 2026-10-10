import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { WorkspaceContextProvider } from '../api/transport.tsx';
import { createWorkspaceClient } from '../api/workspaceClient.ts';
import { WorkspaceSettingsLink } from './WorkspaceSettingsLink.tsx';

const meta = {
  title: 'Shared/WorkspaceSettingsLink',
  component: WorkspaceSettingsLink,
  tags: ['ai-generated'],
  args: { path: '/orgs/acme/settings', children: 'Organisation settings' },
  parameters: { ...topLayerDocs, app: { responses: [] } },
} satisfies Meta<typeof WorkspaceSettingsLink>;
export default meta;
type Story = StoryObj<typeof meta>;
export const ThisInstance: Story = { play: async ({ canvas }) => { await expect(canvas.getByRole('link')).toHaveAttribute('href', '/orgs/acme/settings'); } };
export const DestinationInstance: Story = {
  decorators: [(Story) => <WorkspaceContextProvider value={{ origin: 'https://peer.example', remote: 'peer', client: createWorkspaceClient('https://peer.example') }}><Story /></WorkspaceContextProvider>],
  play: async ({ canvas }) => { await expect(canvas.getByRole('link')).toHaveAttribute('href', 'https://peer.example/orgs/acme/settings'); },
};
