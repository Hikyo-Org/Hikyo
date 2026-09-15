import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, waitFor } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ChangeApprovals } from './ChangeApprovals.tsx';

// The page mounts two queries at once: the environment list (whose names label
// the policy rows) and the approval-policy list (the panel that is loaded,
// empty, or failed). The review queue stays disabled until an environment is
// chosen, so these two endpoints are the whole state surface.
const environments = {
  items: [
    {
      id: 'env_00000000-0000-0000-0000-000000000001',
      org_id: 'org_00000000-0000-0000-0000-000000000001',
      project_id: 'prj_00000000-0000-0000-0000-000000000001',
      name: 'production',
      display_order: 0,
      created_at: '2026-09-01T00:00:00Z',
    },
    {
      id: 'env_00000000-0000-0000-0000-000000000002',
      org_id: 'org_00000000-0000-0000-0000-000000000001',
      project_id: 'prj_00000000-0000-0000-0000-000000000001',
      name: 'staging',
      display_order: 1,
      created_at: '2026-09-01T00:00:00Z',
    },
  ],
  count: 2,
};

// A widened policy set for design density: an all-environments rule, a covered
// production rule, and a disabled staging rule, spanning min-approval counts,
// self-approval on and off, and enabled/disabled state.
const policies = {
  items: [
    {
      id: 'pol_00000000-0000-0000-0000-000000000001',
      environment_id: '',
      min_approvals: 2,
      allow_self_approval: false,
      request_ttl_seconds: 86400,
      enabled: true,
      version: 1,
      approvers: [{ kind: 'principal', subject_id: 'prn_00000000-0000-0000-0000-000000000001' }],
      bypassers: [],
      principal_names: { 'prn_00000000-0000-0000-0000-000000000001': 'Dana Jacobs' },
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-01T00:00:00Z',
    },
    {
      id: 'pol_00000000-0000-0000-0000-000000000002',
      environment_id: 'env_00000000-0000-0000-0000-000000000001',
      min_approvals: 1,
      allow_self_approval: true,
      request_ttl_seconds: 14400,
      enabled: true,
      version: 1,
      approvers: [{ kind: 'principal', subject_id: 'prn_00000000-0000-0000-0000-000000000002' }],
      bypassers: ['prn_00000000-0000-0000-0000-000000000001'],
      principal_names: { 'prn_00000000-0000-0000-0000-000000000002': 'Rey Okafor' },
      created_at: '2026-09-02T00:00:00Z',
      updated_at: '2026-09-02T00:00:00Z',
    },
    {
      id: 'pol_00000000-0000-0000-0000-000000000003',
      environment_id: 'env_00000000-0000-0000-0000-000000000002',
      min_approvals: 3,
      allow_self_approval: false,
      request_ttl_seconds: 43200,
      enabled: false,
      version: 1,
      approvers: [
        {
          kind: 'scim_group',
          subject_id: 'grp_00000000-0000-0000-0000-000000000001',
          binding_id: 'bnd_00000000-0000-0000-0000-000000000001',
        },
      ],
      bypassers: [],
      created_at: '2026-09-03T00:00:00Z',
      updated_at: '2026-09-03T00:00:00Z',
    },
  ],
};

const envRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/environments(\?|$)/, ...rest });
const policyRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/approval-policies(\?|$)/, ...rest });

const path = '/orgs/acme/projects/app/approvals';
const routePath = '/orgs/:org/projects/:project/approvals';

const meta = {
  component: ChangeApprovals,
  tags: ['ai-generated'],
  parameters: { app: { auth: true, path, routePath, responses: [] } },
} satisfies Meta<typeof ChangeApprovals>;

export default meta;
type Story = StoryObj<typeof meta>;

// Policies resolved to a set of rules: the table renders each with its
// environment, quorum, and enabled state.
export const Populated: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [envRoute({ body: environments }), policyRoute({ body: policies })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('cell', { name: 'All environments' })).toBeVisible();
    await expect(canvas.getByRole('cell', { name: 'production' })).toBeVisible();
  },
};

// Policies resolved empty: the panel names what the absence means rather than
// showing a blank table.
export const Empty: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [envRoute({ body: environments }), policyRoute({ body: { items: [] } })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no approval policies/i)).toBeVisible();
  },
};

// The policy read never settles: the loading status stands.
export const Loading: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [envRoute({ body: environments }), policyRoute({ pending: true })],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading policies/i)).toBeVisible());
  },
};

// The policy read failed: the panel surfaces its refusal path, not a blank.
export const Failed: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [envRoute({ body: environments }), policyRoute({ status: 500, body: { error: 'policy read failed' } })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/could not be completed/i)).toBeVisible();
  },
};
