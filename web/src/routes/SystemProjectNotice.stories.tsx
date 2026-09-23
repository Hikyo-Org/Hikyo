import type { InstanceConfigStatus } from '@hikyo/client';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { SystemProjectNotice } from './InstanceConfig.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The notice the matrix shows above Hikyo's own configuration project. It
// reads the self-configuration status and renders nothing unless the binding
// names exactly this org and project, so the only storyable state is the match.
const ORG = 'org_system';
const PROJECT = 'prj_system';

const status: InstanceConfigStatus = {
  owner_instance_id: 'instance_local',
  managed: true,
  binding: { org_id: ORG, project_id: PROJECT, environment_id: 'env_system', schema_version: 1 },
  generation: 7,
  desired_revision: 2,
  latest_revision: 3,
  state: 'pending',
  nodes: [],
  job: null,
};

const meta = {
  component: SystemProjectNotice,
  tags: ['ai-generated'],
  args: { org: ORG, project: PROJECT },
  parameters: {
    ...topLayerDocs,
    app: { responses: [{ url: '/api/v1/instance/config', body: status }] },
  },
} satisfies Meta<typeof SystemProjectNotice>;

export default meta;
type Story = StoryObj<typeof meta>;

// The binding matches: the notice names the state and links to the apply surface.
export const Shown: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Hikyo system configuration')).toBeVisible();
    await expect(canvas.getByText(/apply pending/i)).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Review and apply' })).toHaveAttribute('href', '/instance/config');
  },
};
