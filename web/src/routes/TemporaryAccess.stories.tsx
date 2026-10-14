import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { TemporaryAccess } from './TemporaryAccess.tsx';

import { openTab } from '../../.storybook/openTab.ts';
import { expectLeftAlignedPage } from '../../.storybook/layout.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page mounts the environment list and the access-policy list at once; the
// request queue stays disabled until an environment is chosen, so these two
// endpoints are the whole initial state surface.
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
  ],
  count: 1,
};

// A project-wide reveal rule and a production rule offering edit and publish,
// one of them disabled, so the table shows every column's range.
const policies = {
  items: [
    {
      id: 'xpol_00000000-0000-0000-0000-000000000001',
      environment_id: '',
      capabilities: ['reveal'],
      max_duration_seconds: 28800,
      min_approvals: 1,
      allow_self_approval: false,
      request_ttl_seconds: 86400,
      enabled: true,
      version: 1,
      approvers: [{ kind: 'principal', subject_id: 'usr_00000000-0000-0000-0000-000000000001' }],
      bypassers: [],
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-01T00:00:00Z',
    },
    {
      id: 'xpol_00000000-0000-0000-0000-000000000002',
      environment_id: 'env_00000000-0000-0000-0000-000000000001',
      capabilities: ['edit', 'publish'],
      max_duration_seconds: 3600,
      min_approvals: 2,
      allow_self_approval: false,
      request_ttl_seconds: 14400,
      enabled: false,
      version: 3,
      approvers: [{ kind: 'principal', subject_id: 'usr_00000000-0000-0000-0000-000000000002' }],
      bypassers: ['usr_00000000-0000-0000-0000-000000000001'],
      created_at: '2026-09-02T00:00:00Z',
      updated_at: '2026-09-03T00:00:00Z',
    },
  ],
};

const envRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/environments(\?|$)/, ...rest });
const policyRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/access-policies(\?|$)/, ...rest });

const path = '/orgs/acme/projects/app/temporary-access';
const routePath = '/orgs/:org/projects/:project/temporary-access';

const meta = {
  title: 'Pages/TemporaryAccess',
  id: 'routes-temporaryaccess',
  component: TemporaryAccess,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { auth: true, path, routePath, responses: [] } },
} satisfies Meta<typeof TemporaryAccess>;

export default meta;
type Story = StoryObj<typeof meta>;

// A member manager sees the project's access policies with their capabilities,
// longest duration and state.
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
    await openTab(canvas, 'Policies');
    await expect(await canvas.findByRole('cell', { name: 'All environments' })).toBeVisible();
    // Capabilities are tags, not a joined string: each capability is its own
    // CapabilityTag, keeping the plain word as its text.
    await expect(canvas.getByText('edit')).toBeVisible();
    await expect(canvas.getByText('publish')).toBeVisible();
    await expect(canvas.getByRole('cell', { name: 'disabled' })).toBeVisible();
    // The longest duration is spelled out, never "8h".
    await expect(canvas.getAllByRole('cell', { name: /^\d+ (day|hour|minute|second)s?$/ }).length).toBeGreaterThan(0);
    await expectLeftAlignedPage(canvas.getByRole('heading', { level: 1 }));
  },
};

// The policy form and the delete confirmation open in the side panel.
export const PolicyPanels: Story = {
  parameters: Populated.parameters,
  play: async ({ canvas }) => {
    await openTab(canvas, 'Policies');
    await canvas.findByRole('cell', { name: 'All environments' });
    await userEvent.click(canvas.getByRole('button', { name: 'New access policy' }));
    const form = within(canvas.getByRole('complementary', { name: 'New access policy' }));
    await expect(form.getByRole('group', { name: 'Requestable capabilities' })).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await userEvent.click(canvas.getAllByRole('button', { name: 'Delete' })[0] ?? canvas.getByRole('table'));
    const confirm = within(canvas.getByRole('complementary', { name: 'Delete access policy' }));
    await expect(confirm.getByRole('button', { name: 'Delete policy' })).toBeDisabled();
    await userEvent.click(confirm.getByRole('button', { name: 'Cancel deletion' }));
    await expect(canvas.queryByRole('complementary', { name: 'Delete access policy' })).toBeNull();
  },
};

// Someone who does not manage members sees the request surface and a sentence
// about who administers policies, never a raw refusal.
export const Requester: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [envRoute({ body: environments }), policyRoute({ status: 404, body: { error: 'not found' } })],
    },
  },
  play: async ({ canvas }) => {
    await openTab(canvas, 'Requests');
    await waitFor(() => expect(canvas.getByText(/choose an environment to request access/i)).toBeVisible());
    await openTab(canvas, 'Policies');
    await waitFor(() => expect(canvas.getByText(/administered by project member managers/i)).toBeVisible());
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
    await openTab(canvas, 'Policies');
    await waitFor(() => expect(canvas.getByText(/loading policies/i)).toBeVisible());
  },
};
