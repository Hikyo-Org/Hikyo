import type { Meta, StoryObj } from '@storybook/react-vite';
import { zImportValuesResult, zKey, zValueOccurrenceList } from '@hikyo/zod';
import { expect, fn, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ImportWizard } from './ImportWizard.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The browser import wizard. A file is parsed on this device and every step
// after the source is reached by driving the real flow in `play`: the
// occurrence read (phase 1a, POST per environment) opens the classify step,
// the declare (POST keys) and import (POST per environment) writes produce the
// result step. Each write has a canned row; a refused row is the failed state.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const DEV = 'env_123e4567-e89b-12d3-a456-426614174010';
const PRD = 'env_123e4567-e89b-12d3-a456-426614174012';

const environments = [
  { id: DEV, name: 'development' },
  { id: PRD, name: 'production' },
];

// One already-declared key (set only in production, so the review shows an
// overwrite decision there), and three new ones with a type suggestion each.
const FILE = 'EXISTING=1\nNEW_DATABASE_URL=postgres://db.internal/app\nDEBUG=true\nPORT=8080\n';
const NAMES = ['EXISTING', 'NEW_DATABASE_URL', 'DEBUG', 'PORT'];

const occurrences = (environment: string): z.input<typeof zValueOccurrenceList> => ({
  environment_id: environment,
  definitions_revision: 3,
  items: NAMES.map((name) => ({
    name,
    declared: name === 'EXISTING',
    set: name === 'EXISTING' && environment === PRD,
    token: `tok-${name}-${environment}`,
  })),
});

const declared = {
  id: 'key_123e4567-e89b-12d3-a456-426614174099',
  org_id: ORG,
  project_id: PRJ,
  name: 'NEW_DATABASE_URL',
  folder_path: '',
  classification: 'secret',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string' } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-01-01T00:00:00Z',
} satisfies z.input<typeof zKey>;

const imported = (names: readonly string[]): z.input<typeof zImportValuesResult> => ({
  imported: [...names],
  skipped: [],
  findings: [{ rule_id: 'postgres-uri', surface: 'import_value', locator: 'NEW_DATABASE_URL' }],
});

const occurrenceRows = (rest: Omit<MockRoute, 'url' | 'method'> = {}): readonly MockRoute[] =>
  [DEV, PRD].map((environment) => ({
    url: `${PROJECT_URL}/environments/${environment}/values/occurrences`,
    method: 'POST',
    body: occurrences(environment),
    ...rest,
  }));

const importRows = (rest: Omit<MockRoute, 'url' | 'method'> = {}): readonly MockRoute[] => [
  { url: `${PROJECT_URL}/environments/${DEV}/values/import`, method: 'POST', body: imported(NAMES), ...rest },
  { url: `${PROJECT_URL}/environments/${PRD}/values/import`, method: 'POST', body: imported(NAMES.slice(1)), ...rest },
];

const app = (responses: readonly MockRoute[] = []) => ({
  app: {
    responses: [
      ...responses,
      ...occurrenceRows(),
      // The declare contract answers 201; any other status is a refusal.
      { url: `${PROJECT_URL}/keys`, method: 'POST', status: 201, body: declared },
      ...importRows(),
    ],
  },
});

const forbidden = { status: 403, body: { error: { code: 'forbidden', message: 'forbidden' } } };

type Canvas = ReturnType<typeof within>;

const pickDotenv = async (canvas: Canvas) => {
  await userEvent.click(await canvas.findByRole('button', { name: /\.env file/ }));
};

const chooseFile = async (canvas: Canvas, text: string, summary: RegExp) => {
  await userEvent.upload(canvas.getByLabelText('Dotenv file'), new File([text], 'app.env', { type: 'text/plain' }));
  await expect(await canvas.findByText(summary)).toBeVisible();
};

const reachClassify = async (canvas: Canvas) => {
  await pickDotenv(canvas);
  await chooseFile(canvas, FILE, /app\.env: 4 values read/);
  await userEvent.click(canvas.getByRole('button', { name: 'Review' }));
  await expect(await canvas.findByText('Classify new keys')).toBeVisible();
};

const reachReview = async (canvas: Canvas) => {
  await reachClassify(canvas);
  await userEvent.click(canvas.getByRole('button', { name: 'Review changes' }));
  await expect(await canvas.findByText('Overwrite already-set values:')).toBeVisible();
};

const meta = {
  component: ImportWizard,
  tags: ['ai-generated'],
  args: { matrixRef: { org: ORG, project: PRJ }, environments, gitManaged: false, onClose: fn() },
  parameters: { ...topLayerDocs, ...app() },
} satisfies Meta<typeof ImportWizard>;

export default meta;
type Story = StoryObj<typeof meta>;

// Step 1: the source picker, every journey as one row.
export const Pick: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('list', { name: 'Import sources' })).toBeVisible();
    await expect(canvas.getAllByRole('listitem')).toHaveLength(7);
  },
};

// Step 2 for `.env`: a parsed file, its summary, and the target environments.
export const DotenvSource: Story = {
  play: async ({ canvas }) => {
    await pickDotenv(canvas);
    await chooseFile(canvas, FILE, /app\.env: 4 values read/);
    await expect(canvas.getByRole('button', { name: 'Review' })).toBeEnabled();
  },
};

// A file with an invalid line: the all-or-nothing gate lists it and blocks Review.
export const InvalidLines: Story = {
  play: async ({ canvas }) => {
    await pickDotenv(canvas);
    await chooseFile(canvas, 'GOOD=1\nlower=2\n', /1 invalid line skipped/);
    await expect(canvas.getByRole('list', { name: 'Invalid lines' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Review' })).toBeDisabled();
  },
};

// A connector that needs a source slug before its file input unlocks.
export const ConnectorSource: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /Infisical export/ }));
    await expect(await canvas.findByText(/Name the source environment slug/)).toBeVisible();
    await expect(canvas.getByLabelText('Infisical export')).toBeDisabled();
  },
};

// A journey the browser cannot run: the CLI guidance and its command.
export const CliGuidance: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /SOPS file/ }));
    await expect(await canvas.findByText(/hikyo import --from sops/)).toBeVisible();
  },
};

// Step 3: the new keys, each secret by default with a type suggestion shown but not chosen.
export const Classify: Story = {
  play: async ({ canvas }) => {
    await reachClassify(canvas);
    await expect(canvas.getByText('Suggested: boolean')).toBeVisible();
    await expect(canvas.getByText('Suggested: integer')).toBeVisible();
  },
};

// Step 4: per-environment plan with the overwrite decision for an already-set key.
export const Review: Story = {
  play: async ({ canvas }) => {
    await reachReview(canvas);
    await expect(canvas.getByText(/3 to import, 3 new keys declared, 1 skipped \(already set\)/)).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: 'EXISTING' })).not.toBeChecked();
  },
};

// Step 5: what landed per environment, with the redacted scanning warnings.
export const Result: Story = {
  play: async ({ canvas }) => {
    await reachReview(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Import' }));
    await expect(await canvas.findByText(/Declared 3 new keys/)).toBeVisible();
    await expect(canvas.getByText(/skipped 1 \(EXISTING\); secret-scanning warnings: postgres-uri/)).toBeVisible();
  },
};

// Phase 2 refused per environment: the result names each refusal in place.
export const ImportRefused: Story = {
  parameters: app(importRows(forbidden)),
  play: async ({ canvas }) => {
    await reachReview(canvas);
    await userEvent.click(canvas.getByRole('button', { name: 'Import' }));
    await expect(
      (await canvas.findAllByText('You do not have permission to import values into this environment.')),
    ).toHaveLength(2);
  },
};

// The occurrence read failed: the source step stays, with the error above it.
export const ReviewFailed: Story = {
  parameters: app(occurrenceRows({ status: 500, body: { error: { code: 'internal', message: 'boom' } } })),
  play: async ({ canvas }) => {
    await pickDotenv(canvas);
    await chooseFile(canvas, FILE, /app\.env: 4 values read/);
    await userEvent.click(canvas.getByRole('button', { name: 'Review' }));
    await waitFor(() => expect(canvas.getByText('The server could not import these values (error 500).')).toBeVisible());
  },
};

// A Git-managed project: new keys cannot be declared here and are named as skipped.
export const GitManaged: Story = {
  args: { gitManaged: true },
  play: async ({ canvas }) => {
    await pickDotenv(canvas);
    await chooseFile(canvas, FILE, /app\.env: 4 values read/);
    await userEvent.click(canvas.getByRole('button', { name: 'Review' }));
    await expect(await canvas.findByText(/managed in Git/)).toBeVisible();
    await expect(canvas.getByText(/cannot be declared here and will be skipped/)).toBeVisible();
  },
};
