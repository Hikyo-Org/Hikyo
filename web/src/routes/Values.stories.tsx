import type { Meta, StoryObj } from '@storybook/react-vite';
import { zEnvironmentList, zRevealWindow, zValueList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { Values } from './Values.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The reveal / copy / write-only-edit surface for ONE environment. On mount it
// reads three things: the masked value list, the project's environments (the
// publish destinations and the ceremony's environment name) and the reveal
// window, whose `can_reveal` decides whether a secret gets Reveal/Copy controls
// or the write-only editor. Reveal/copy/publish run the ceremony and are not
// driven here; the stories cover what the page looks like before any of that.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const ENV = 'env_123e4567-e89b-12d3-a456-426614174010';
const PATH = `/orgs/${ORG}/projects/${PRJ}/environments/${ENV}/values`;
const ROUTE = '/orgs/:org/projects/:project/environments/:environment/values';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const VALUES_URL = `${PROJECT_URL}/environments/${ENV}/values`;
const WINDOW_URL = `${PROJECT_URL}/environments/${ENV}/reveal-window`;
const ENVIRONMENTS_URL = `${PROJECT_URL}/environments`;

type ValueList = z.infer<typeof zValueList>;
type RevealWindow = z.infer<typeof zRevealWindow>;

const environments = {
  count: 3,
  items: [
    { id: ENV, name: 'production', display_order: 0 },
    { id: 'env_123e4567-e89b-12d3-a456-426614174011', name: 'staging', display_order: 1 },
    { id: 'env_123e4567-e89b-12d3-a456-426614174012', name: 'development', display_order: 2 },
  ].map((item) => ({ ...item, org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' })),
} satisfies z.infer<typeof zEnvironmentList>;

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const cell = (
  n: number,
  name: string,
  classification: 'secret' | 'config',
  value?: string,
): ValueList['items'][number] => ({
  key_id: keyId(n),
  name,
  classification,
  set: value !== undefined,
  revealed: classification === 'config' && value !== undefined,
  ...(classification === 'config' && value !== undefined ? { value } : {}),
});

// Dense enough to design against: secrets set and absent, config short and
// long, and a key name near the 128-character ceiling to test the mono column.
const populated: ValueList = {
  count: 9,
  items: [
    cell(1, 'DATABASE_URL', 'secret', 'x'),
    cell(2, 'STRIPE_SECRET_KEY', 'secret', 'x'),
    cell(3, 'SMTP_PASSWORD', 'secret'),
    cell(4, 'LOG_LEVEL', 'config', 'info'),
    cell(5, 'FEATURE_FLAGS', 'config', 'checkout-v2,search-rerank,beta-dashboard,new-onboarding'),
    cell(6, 'PUBLIC_BASE_URL', 'config', 'https://app.example.com'),
    cell(7, 'CACHE_TTL_SECONDS', 'config'),
    cell(8, 'OIDC_CLIENT_SECRET', 'secret', 'x'),
    cell(
      9,
      'ANALYTICS_PIPELINE_WAREHOUSE_CONNECTION_STRING_FOR_THE_NIGHTLY_EXPORT_JOB_IN_EUROPE_WEST',
      'config',
      'warehouse://eu-west-4/nightly',
    ),
  ],
};

const locked: RevealWindow = {
  effective_window_seconds: 600,
  protected: false,
  totp_offered: false,
  live: false,
  single_decision: false,
  can_reveal: true,
};

const rows = (values: MockRoute, window: MockRoute = { url: WINDOW_URL, body: locked }) => ({
  app: { path: PATH, routePath: ROUTE, responses: [values, window, { url: ENVIRONMENTS_URL, body: environments }] },
});

const meta = {
  component: Values,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, ...rows({ url: VALUES_URL, body: populated }) },
} satisfies Meta<typeof Values>;

export default meta;
type Story = StoryObj<typeof meta>;

// No window is live: every secret is masked with Reveal and Copy on offer, the
// chip says each disclosure will ask first, config values read in the clear.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL' })).toBeVisible();
    await expect(canvas.getByText('Locked · a passkey per disclosure')).toBeVisible();
    await expect(canvas.getByText('info')).toBeVisible();
    await expect(canvas.getAllByText('absent')).toHaveLength(2);
  },
};

// A window is live: the chip counts it down and "Reveal every secret" is enabled.
export const WindowLive: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    {
      url: WINDOW_URL,
      body: { ...locked, live: true, expires_at: new Date(Date.now() + 9 * 60_000).toISOString() },
    },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/^Reveal window · \d+s$/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeEnabled();
  },
};

// A protected environment with no live window: the chip names the passkey rule.
export const ProtectedLocked: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    { url: WINDOW_URL, body: { ...locked, protected: true, effective_window_seconds: 0 } },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Protected · a passkey per disclosure')).toBeVisible();
  },
};

// `edit` without `reveal`: secrets carry no disclosure control, and the editor
// says it replaces without seeing. The play opens the editor (no request).
export const WriteOnly: Story = {
  parameters: rows(
    { url: VALUES_URL, body: populated },
    { url: WINDOW_URL, body: { ...locked, can_reveal: false } },
  ),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('DATABASE_URL')).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Reveal DATABASE_URL' })).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
    await userEvent.click(canvas.getByRole('button', { name: 'DATABASE_URL' }));
    await expect(
      canvas.getByPlaceholderText('Replace without seeing the current value'),
    ).toBeVisible();
    await expect(canvas.getByText(/You may replace this value but not read it/)).toBeVisible();
  },
};

// An environment with no keys yet: the bar's actions are disabled and the
// table has only its header.
export const Empty: Story = {
  parameters: rows({ url: VALUES_URL, body: { count: 0, items: [] } satisfies ValueList }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Locked · a passkey per disclosure')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
    await expect(canvas.queryByRole('row', { name: /DATABASE_URL/ })).not.toBeInTheDocument();
  },
};

// Nothing has resolved: the heading stands with no chip, no rows, no controls
// beyond the disabled bar.
export const Loading: Story = {
  parameters: rows({ url: VALUES_URL, pending: true }, { url: WINDOW_URL, pending: true }),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Values' })).toBeVisible());
    await expect(canvas.getByRole('button', { name: 'Reveal every secret' })).toBeDisabled();
  },
};

// The value read failed: the page keeps its chip and bar and says so.
export const Failed: Story = {
  parameters: rows({ url: VALUES_URL, status: 500, body: { error: 'values read failed' } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/The values could not be loaded/)).toBeVisible();
  },
};
