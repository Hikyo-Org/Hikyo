import type { Meta, StoryObj } from '@storybook/react-vite';
import { zDefinitionsBundle, zDefinitionsCheckResult, zDefinitionsPlan } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import type { DefinitionsSettings } from '../api/definitions.ts';
import { ORG, PRJ } from '../testkit/ids.ts';
import { DefinitionsBundlePanel } from './DefinitionsBundlePanel.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The definitions-bundle settings row and its dialog. The dialog's later
// states are reached by driving the real flow in `play`: a bundle file is
// chosen, checked (POST check) and planned (POST plans, then GET the stored
// plan). A refused or never-settling check row is the failed / busy state.
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const PLAN = 'pln_123e4567-e89b-12d3-a456-426614174000';

const db: DefinitionsSettings = { definitions_source: 'db' };
const git: DefinitionsSettings = {
  definitions_source: 'git',
  last_apply: {
    plan_id: PLAN,
    applied_at: '2026-09-01T10:00:00Z',
    applied_by: 'prn_123e4567-e89b-12d3-a456-426614174000',
    commit: 'abc1234',
    ref: 'refs/heads/main',
    revision: 7n,
  },
};

// A based bundle (it names the revision it was exported from), as the file
// reader's strict schema accepts it.
const bundle = {
  format_version: 1,
  base_revision: 11,
  environments: [{ name: 'development' }, { name: 'production' }],
  key_groups: [{ name: 'stripe' }],
  keys: [
    {
      name: 'DATABASE_URL',
      folder_path: 'database',
      classification: 'secret',
      description: 'Primary datastore connection string',
      deprecated: false,
      deprecation_note: '',
      group: '',
      declaration: { rule: { type: 'url', schemes: ['postgres'] } },
      required_in: { mode: 'all', environments: [] },
      forbidden_in: { mode: 'none', environments: [] },
    },
  ],
} satisfies z.input<typeof zDefinitionsBundle>;

const kinds = {
  environments: { creates: ['staging'], updates: [], renames: [], deletes: [] },
  key_groups: {
    creates: [],
    updates: [],
    renames: [{ id: 'kgr_123e4567-e89b-12d3-a456-426614174050', from: 'stripe', to: 'payments' }],
    deletes: [],
  },
  keys: { creates: ['SENTRY_DSN'], updates: ['DATABASE_URL'], renames: [], deletes: ['LEGACY_WAREHOUSE_DSN'] },
};

const checked = {
  state: 'file_ahead',
  base_revision: 11,
  current_revision: 11,
  differences: { ...kinds, reveal_required: [] },
} satisfies z.input<typeof zDefinitionsCheckResult>;

const plan = {
  id: PLAN,
  digest: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
  base_revision: 11,
  current_revision: 11,
  additive: false,
  expires_at: '2026-09-23T12:00:00Z',
  protected_environments: ['production'],
  diff: {
    ...kinds,
    key_deletions: [{ name: 'LEGACY_WAREHOUSE_DSN', live_in: ['production'] }],
    env_deletions: [],
    reveal_required: ['DATABASE_URL'],
  },
  deletions_present: true,
  reveal_required: ['DATABASE_URL'],
} satisfies z.input<typeof zDefinitionsPlan>;

const check = (rest: Omit<MockRoute, 'url' | 'method'> = {}): MockRoute => ({
  url: `${PROJECT_URL}/definitions/check`,
  method: 'POST',
  body: checked,
  ...rest,
});

const app = (rest: Omit<MockRoute, 'url' | 'method'> = {}) => ({
  app: {
    responses: [
      check(rest),
      // The plan contract answers 201 on create, then the stored plan is read back.
      { url: `${PROJECT_URL}/definitions/plans`, method: 'POST', status: 201, body: { plan } },
      { url: `${PROJECT_URL}/definitions/plans/${PLAN}`, body: { plan } },
    ],
  },
});

type Canvas = ReturnType<typeof within>;

const openDialog = async (canvas: Canvas) => {
  await userEvent.click(canvas.getByRole('button', { name: 'Check a bundle' }));
  await expect(await canvas.findByRole('dialog', { name: 'Definitions bundle' })).toBeVisible();
};

const chooseBundle = async (canvas: Canvas) => {
  await userEvent.upload(
    canvas.getByLabelText(/Definitions bundle file/),
    new File([JSON.stringify(bundle)], 'definitions.json', { type: 'application/json' }),
  );
  await waitFor(() => expect(canvas.getByRole('button', { name: 'Check bundle' })).toBeEnabled());
};

const meta = {
  component: DefinitionsBundlePanel,
  tags: ['ai-generated'],
  args: { org: ORG, project: PRJ, settings: db },
  parameters: { ...topLayerDocs, ...app() },
} satisfies Meta<typeof DefinitionsBundlePanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// The settings row: both bundle downloads and the way into the dialog.
export const Row: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('link', { name: 'Download definitions bundle' })).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Download portable bundle' })).toBeVisible();
  },
};

// The dialog freshly opened: nothing chosen, both actions waiting on a file.
export const DialogOpen: Story = {
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await expect(canvas.getByRole('button', { name: 'Check bundle' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Create impact plan' })).toBeDisabled();
  },
};

// A Git-managed project: the notice, the last-applied provenance, apply refused.
export const GitReadOnly: Story = {
  args: { settings: git },
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await expect(canvas.getByText(/browser apply is refused/)).toBeVisible();
    await expect(canvas.getByText('abc1234')).toBeVisible();
  },
};

// A checked bundle: the drift verdict and the per-kind difference lists.
export const Checked: Story = {
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await chooseBundle(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Check bundle' }));
    await expect(await canvas.findByText(/File ahead: the file contains changes/)).toBeVisible();
    await expect(canvas.getByText('LEGACY_WAREHOUSE_DSN')).toBeVisible();
  },
};

// The immutable impact plan: digest, deletions to allow, and a reveal requirement.
export const Planned: Story = {
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await chooseBundle(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Check bundle' }));
    await userEvent.click(await canvas.findByRole('button', { name: 'Create impact plan' }));
    await expect(await canvas.findByRole('heading', { name: 'Immutable impact plan' })).toBeVisible();
    await expect(canvas.getByText(/Reveal authorization is required for: DATABASE_URL/)).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: /allow the listed deletions/ })).not.toBeChecked();
    await expect(canvas.getByRole('button', { name: 'Review and apply' })).toBeDisabled();
  },
};

// The check never settles: the busy status with the actions held.
export const Busy: Story = {
  parameters: app({ pending: true }),
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await chooseBundle(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Check bundle' }));
    await expect(await canvas.findByText('Checking the instance…')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Check bundle' })).toBeDisabled();
  },
};

// The check refused as stale: the recovery text above the file field.
export const Refused: Story = {
  parameters: app({ status: 409, body: { error: { code: 'conflict', message: 'stale' } } }),
  play: async ({ canvas }) => {
    await openDialog(canvas);
    await chooseBundle(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Check bundle' }));
    await expect(await canvas.findByText(/stale, expired, or conflicts with current state/)).toBeVisible();
  },
};
