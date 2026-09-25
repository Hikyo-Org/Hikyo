import type { Meta, StoryObj } from '@storybook/react-vite';
import { zProjectList } from '@hikyo/zod';
import { expect } from 'storybook/test';
import type { z } from 'zod';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG, PRJ } from '../testkit/ids.ts';
import { Projects } from './Projects.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The projects surface reads `useOutletContext({ activeOrgId })` and, through
// `useInSystemScope`, `useInstanceOperator` from the real AuthProvider — so the
// harness runs it with `auth: true`. The identity is a *non-operator*: that
// disables the system-scope query (`useSystemScope(operator === true)`), leaving
// one load-time call, GET /api/v1/orgs/{org}/projects, and shows the create
// form (an ordinary scope). See .storybook/withApp.tsx.
const PROJECTS_URL = `/api/v1/orgs/${ORG}/projects`;

const member: z.input<typeof zProjectList>['items'][number] = {
  id: PRJ,
  org_id: ORG,
  name: 'billing',
  created_at: '2026-01-01T00:00:00Z',
};
const populated = {
  count: 3,
  items: [
    member,
    { ...member, id: 'prj_123e4567-e89b-12d3-a456-426614174002', name: 'web' },
    { ...member, id: 'prj_123e4567-e89b-12d3-a456-426614174003', name: 'analytics' },
  ],
} satisfies z.input<typeof zProjectList>;

const meta = {
  component: Projects,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: {
      auth: true,
      identity: { ...authenticatedIdentity, capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } } },
      path: '/projects',
      outlet: { activeOrgId: ORG },
      responses: [{ url: PROJECTS_URL, body: { count: 0, items: [] } }],
    },
  },
} satisfies Meta<typeof Projects>;

export default meta;
type Story = StoryObj<typeof meta>;

// Empty organisation: the list announces there is nothing yet and the create
// form is offered below it.
export const Empty: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
    await expect(await canvas.findByText(/no projects yet/i)).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'New project' })).toBeVisible();
    await expect(canvas.getByLabelText('Project name')).toBeVisible();
  },
};

export const Populated: Story = {
  parameters: {
    app: {
      auth: true,
      identity: { ...authenticatedIdentity, capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } } },
      path: '/projects',
      outlet: { activeOrgId: ORG },
      responses: [{ url: PROJECTS_URL, body: populated }],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('billing')).toBeVisible();
    await expect(canvas.getByText('web')).toBeVisible();
    await expect(canvas.getByText('analytics')).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Settings for billing' })).toBeVisible();
  },
};

// Load failure: the panel degrades to an alert (the design keeps meaning off
// colour), the exact state a real screen shows when the API is down.
export const LoadError: Story = {
  parameters: {
    app: {
      auth: true,
      identity: { ...authenticatedIdentity, capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } } },
      path: '/projects',
      outlet: { activeOrgId: ORG },
      responses: [{ url: PROJECTS_URL, status: 500, body: { error: 'boom' } }],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/projects could not be loaded/i)).toBeVisible();
  },
};
