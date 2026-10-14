import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ChangeApprovals } from './ChangeApprovals.tsx';

import { expectLeftAlignedPage } from '../../.storybook/layout.ts';
import { openTab } from '../../.storybook/openTab.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

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
  title: 'Pages/ChangeApprovals',
  id: 'routes-changeapprovals',
  component: ChangeApprovals,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "Environment change-approval policies and their approver sets. The real screen distinguishes pending, empty, populated and failed reads instead of treating an unread policy list as empty." } },  app: { auth: true, path, routePath, responses: [] } },
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
    // Request lifetimes are spelled out: 86400 seconds reads "1 day".
    await expect(canvas.getByRole('cell', { name: '1 day' })).toBeVisible();
    await expectLeftAlignedPage(canvas.getByRole('heading', { name: 'Change approvals', level: 1 }));
    // Policies and the review queue are tabs.
    await expect(canvas.getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['Policies', 'Review queue']);
    await openTab(canvas, 'Review queue');
    await expect(await canvas.findByRole('heading', { name: 'Review queue' })).toBeVisible();
  },
};

// New policy opens its form in the side panel, not under the table.
export const PolicyFormPanel: Story = {
  parameters: Populated.parameters,
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('cell', { name: 'production' });
    const open = canvas.getByRole('button', { name: 'New policy' });
    await userEvent.click(open);
    const panel = within(canvas.getByRole('complementary', { name: 'New policy' }));
    await expect(panel.getByRole('form', { name: 'New policy' })).toBeVisible();
    await expect(panel.getByLabelText('Approvals required')).toBeVisible();
    // "Advanced" wears the button skin, not a bare label with the native
    // triangle (ProjectSettings' environment-lifecycle pattern): the chevron,
    // not a native marker, carries the open state.
    const summary = canvasElement.querySelector('.change-approvals__advanced > summary');
    if (!(summary instanceof HTMLElement)) throw new Error('advanced summary missing');
    await expect(summary).toHaveClass('btn');
    await expect(getComputedStyle(summary).listStyle).toContain('none');
    const chevron = summary.querySelector('.glyph[data-glyph="chevron"]');
    if (chevron === null) throw new Error('chevron missing');
    await userEvent.click(summary);
    await expect(summary.closest('details')).toHaveAttribute('open');
    await expect(getComputedStyle(chevron).transform).not.toBe('none');
    await userEvent.click(summary);
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

// A refused policy delete shows on the Policies tab, where the Delete was pressed.
export const DeleteRefused: Story = {
  parameters: {
    app: {
      auth: true,
      path,
      routePath,
      responses: [
        envRoute({ body: environments }),
        policyRoute({ body: policies }),
        { url: /\/approval-policies\/pol_/, method: 'DELETE', status: 500, body: { code: 'internal' } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('cell', { name: 'All environments' })).toBeVisible();
    const [remove] = canvas.getAllByRole('button', { name: 'Delete' });
    if (remove === undefined) throw new Error('Delete button missing');
    await userEvent.click(remove);
    const alert = await canvas.findByRole('alert');
    await expect(alert).toBeVisible();
    await expect(canvas.getByRole('tab', { name: 'Policies' })).toHaveAttribute('aria-selected', 'true');
    // The refusal sits above the tab bar, so it stays in view on the Review
    // queue tab as well.
    const tabs = canvas.getByRole('tablist');
    await expect(alert.compareDocumentPosition(tabs) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    await openTab(canvas, 'Review queue');
    await expect(canvas.getByRole('alert')).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Review queue', level: 2 })).toBeVisible();
  },
};
