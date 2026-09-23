import type { Meta, StoryObj } from '@storybook/react-vite';
import { zDefinitionsSettings, zFolderList, zKeyGroupList } from '@hikyo/zod';
import { expect, fn, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { CatalogueManageDialog } from './CatalogueManageDialog.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The folder and linked-key lifecycle dialog. It reads the project's folders,
// key groups and definitions source; a Git-managed source makes every write
// read-only behind the standing notice.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;

const stamp = { org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' };

const folders = {
  count: 3,
  items: [
    { id: 'fld_123e4567-e89b-12d3-a456-426614174001', path: 'database', ...stamp },
    { id: 'fld_123e4567-e89b-12d3-a456-426614174002', path: 'payments', ...stamp },
    { id: 'fld_123e4567-e89b-12d3-a456-426614174003', path: 'observability/exporters', ...stamp },
  ],
} satisfies z.input<typeof zFolderList>;

// One working set and one inert set (a single member cannot link anything).
const groups = {
  count: 2,
  items: [
    {
      id: 'kgr_123e4567-e89b-12d3-a456-426614174050',
      name: 'stripe',
      members: ['STRIPE_SECRET_KEY', 'STRIPE_WEBHOOK_SECRET'],
      inert: false,
      ...stamp,
    },
    {
      id: 'kgr_123e4567-e89b-12d3-a456-426614174051',
      name: 'warehouse',
      members: ['WAREHOUSE_DSN'],
      inert: true,
      ...stamp,
    },
  ],
} satisfies z.input<typeof zKeyGroupList>;

const empty = { items: [], count: 0 };

const definitions = (source: 'db' | 'git'): MockRoute => ({
  url: `${PROJECT_URL}/definitions/settings`,
  body: { definitions_source: source } satisfies z.input<typeof zDefinitionsSettings>,
});

type Answer = Omit<MockRoute, 'url'>;

const app = (rest: { folders?: Answer; groups?: Answer; definitions?: MockRoute; writes?: readonly MockRoute[] }) => ({
  app: {
    responses: [
      ...(rest.writes ?? []),
      { url: `${PROJECT_URL}/folders`, body: folders, ...rest.folders },
      { url: `${PROJECT_URL}/key-groups`, body: groups, ...rest.groups },
      rest.definitions ?? definitions('db'),
    ],
  },
});

const meta = {
  component: CatalogueManageDialog,
  tags: ['ai-generated'],
  args: { refData: { org: ORG, project: PRJ }, onClose: fn() },
  parameters: { ...topLayerDocs, ...app({}) },
} satisfies Meta<typeof CatalogueManageDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Folders and linked-key sets loaded: editable rows, the inert marker, and both create forms.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText('Folder path for database')).toBeVisible();
    await expect(canvas.getByLabelText('Name for linked-key set warehouse')).toBeVisible();
    await expect(canvas.getByText(/needs at least two keys/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Add folder' })).toBeDisabled();
  },
};

// A fresh project: both sections say so and the create forms are the only rows.
export const Empty: Story = {
  parameters: app({ folders: { body: empty }, groups: { body: empty } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('No folders yet.')).toBeVisible();
    await expect(canvas.getByText(/No linked keys yet/)).toBeVisible();
  },
};

// Both reads still in flight.
export const Loading: Story = {
  parameters: app({ folders: { pending: true }, groups: { pending: true } }),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText('Loading folders…')).toBeVisible());
    await expect(canvas.getByText('Loading linked keys…')).toBeVisible();
  },
};

// Both reads refused: each section names its own failure.
export const Failed: Story = {
  parameters: app({ folders: { status: 500 }, groups: { status: 500 } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Folders could not be read.')).toBeVisible();
    await expect(canvas.getByText('Linked keys could not be read.')).toBeVisible();
  },
};

// A Git-managed project: the notice, disabled rows, and no create forms.
export const ReadOnly: Story = {
  parameters: app({ definitions: definitions('git') }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/managed in Git/)).toBeVisible();
    await expect(canvas.getByLabelText('Folder path for database')).toBeDisabled();
    await expect(canvas.queryByRole('button', { name: 'Add folder' })).not.toBeInTheDocument();
  },
};

// A folder create the server refused for permission: the form keeps the path
// and shows the refusal beside it.
export const CreateRefused: Story = {
  parameters: app({
    writes: [{ url: `${PROJECT_URL}/folders`, method: 'POST', status: 403, body: { error: { code: 'forbidden', message: 'forbidden' } } }],
  }),
  play: async ({ canvas }) => {
    await userEvent.type(await canvas.findByLabelText('New folder path'), 'app/cache');
    await userEvent.click(canvas.getByRole('button', { name: 'Add folder' }));
    await expect(
      await canvas.findByText('You do not have permission to create the folder in this project.'),
    ).toBeVisible();
  },
};
