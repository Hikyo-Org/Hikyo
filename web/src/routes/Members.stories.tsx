import type { Meta, StoryObj } from '@storybook/react-vite';
import { zGrantList, zOrg, zProjectList } from '@hikyo/zod';
import { expect } from 'storybook/test';
import type { z } from 'zod';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { Members } from './Members.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The org-scope members surface reads `useParams().org`, `useSearchParams()` and
// `useAuth` (no outlet context, no workspace context), so the harness runs it
// with `auth: true` at `/orgs/:org/members`. Its load-time reads are three GETs:
// the org (`useOrg`), the org's grant list (`useOrgGrants`), and the org's
// projects (`useOrgTopology` → `useProjects`). An EMPTY project list keeps the
// topology `ready` while firing no per-environment/per-setting fan-out, so the
// list, the inspector and the "New grant" affordance all render from one page.
// `useInstanceGrants(false)` is disabled at org scope and needs no row. See
// .storybook/withApp.tsx. Revoke/Reset/New grant/Invite are mutations or dialogs
// the read-only play never fires.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const ORG_URL = `/api/v1/orgs/${ORG}`;
const GRANTS_URL = `/api/v1/orgs/${ORG}/grants`;
const PROJECTS_URL = `/api/v1/orgs/${ORG}/projects`;

const org = {
  id: ORG,
  name: 'Acme',
  active: true,
  created_at: '2026-01-01T00:00:00Z',
} satisfies z.infer<typeof zOrg>;

const noProjects = { count: 0, items: [] } satisfies z.infer<typeof zProjectList>;

// Two members at organisation scope. Principal ids differ from the operator's
// (prn_…174010), so the "you" badge and the self-reset guard stay out of the way.
// No grant carries `reveal`, the inspector's default capability, so the answer
// reads "Nobody" and the only "Dana"/"Ravi" text on the page is the member cell.
const DANA = 'prn_123e4567-e89b-12d3-a456-426614174020';
const RAVI = 'prn_123e4567-e89b-12d3-a456-426614174021';
const origins = [{ kind: 'manual', subject: 'operator' }];
const grants = {
  count: 3,
  items: [
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174030',
      principal_id: DANA,
      principal_name: 'Dana',
      capability: 'manage-members',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174031',
      principal_id: DANA,
      principal_name: 'Dana',
      capability: 'read',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174032',
      principal_id: RAVI,
      principal_name: 'Ravi',
      capability: 'read',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
} satisfies z.infer<typeof zGrantList>;

const meta = {
  component: Members,
  tags: ['ai-generated'],
  args: { scope: { kind: 'org' } },
  parameters: {
    ...topLayerDocs,
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members`,
      routePath: '/orgs/:org/members',
      responses: [
        { url: ORG_URL, body: org },
        { url: GRANTS_URL, body: grants },
        { url: PROJECTS_URL, body: noProjects },
      ],
    },
  },
} satisfies Meta<typeof Members>;

export default meta;
type Story = StoryObj<typeof meta>;

// Populated organisation: the membership table lists each principal per scope,
// one revocable line per capability, under the "Who can…?" inspector.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /members/i, level: 1 })).toBeVisible();
    // The table mounts only once the grants query resolves, so await the first
    // header; its siblings and rows are in the DOM together by then.
    await expect(await canvas.findByRole('columnheader', { name: 'Member' })).toBeVisible();
    await expect(canvas.getByRole('columnheader', { name: 'Scope' })).toBeVisible();
    await expect(canvas.getByRole('columnheader', { name: 'Capabilities' })).toBeVisible();
    await expect(await canvas.findByText('Dana')).toBeVisible();
    await expect(canvas.getByText('Ravi')).toBeVisible();
    await expect(
      canvas.getByRole('button', { name: /revoke manage-members on .* for Dana/i }),
    ).toBeVisible();
  },
};

// Load failure: the grant listing 500s, so the page blanks the derived rows and
// shows the degraded alert instead — the state a real screen shows when the
// memberships read is down. The org and projects reads still fire.
export const LoadError: Story = {
  parameters: {
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members`,
      routePath: '/orgs/:org/members',
      responses: [
        { url: ORG_URL, body: org },
        { url: GRANTS_URL, status: 500, body: { error: 'boom' } },
        { url: PROJECTS_URL, body: noProjects },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText(/server failed while reading memberships/i),
    ).toBeVisible();
  },
};
