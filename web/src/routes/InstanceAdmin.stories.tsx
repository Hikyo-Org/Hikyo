import type { Meta, StoryObj } from '@storybook/react-vite';
import { zCredentialPolicy, zOrgList, zRetentionHealth } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { InstanceAdmin } from './InstanceAdmin.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The instance settings page owns three reads (the organisation directory, the
// credential policy and retention health) and mounts four child panels with a
// list read each. On this page an unmatched fetch does NOT fail loud: the
// harness 404 renders as the honest "not disclosed" status, so every state
// answers all seven rows explicitly. The route is `surfaceById('instance-admin')`.
const ORGS_URL = '/api/v1/orgs';
const POLICY_URL = '/api/v1/instance/credential-policy';
const HEALTH_URL = '/api/v1/instance/retention-health';
const PANEL_URLS: readonly string[] = [
  '/api/v1/instance/oidc-providers',
  '/api/v1/instance/federation-issuers',
  '/api/v1/instance/saml-providers',
  '/api/v1/instance/saml-sp-keys',
];

const orgs = {
  count: 3,
  items: [
    { id: 'org_123e4567-e89b-12d3-a456-426614174001', name: 'Acme', active: true, created_at: '2026-01-01T00:00:00Z', origin: 'manual' },
    { id: 'org_123e4567-e89b-12d3-a456-426614174002', name: 'Globex Industrial Holdings (EMEA)', active: true, created_at: '2026-03-01T00:00:00Z', origin: 'registration' },
    { id: 'org_123e4567-e89b-12d3-a456-426614174003', name: 'Initech', active: false, created_at: '2025-11-01T00:00:00Z', origin: 'manual' },
  ],
} satisfies z.input<typeof zOrgList>;

const policy = {
  max_finite_lifetime_seconds: 7_776_000,
  allow_indefinite: false,
  max_live_credentials: 5,
} satisfies z.input<typeof zCredentialPolicy>;

const health = {
  last_prune_success: '2026-09-22T03:00:00Z',
  stale: false,
  stale_after_seconds: 86400,
  peak_project_bytes: 12_345_678,
  storage_warn: false,
  backup: {
    scheduled: true,
    last_success_at: '2026-09-22T02:00:00Z',
    artifact_age_seconds: 3600,
    rpo_seconds: 86_400,
    rpo_exceeded: false,
    last_failure_at: null,
    last_failure_reason: '',
    last_prune_at: '2026-09-22T02:05:00Z',
    last_drill_at: '2026-09-01T02:00:00Z',
    last_drill_ok: true,
    drill_stale: false,
  },
  adapter_targets_failed: 0,
  adapter_targets_paused: 0,
  adapter_targets_attention: 0,
  adapter_jobs_queued: 0,
} satisfies z.input<typeof zRetentionHealth>;

// The child panels' populated bodies are storied in their own files; here they
// just need to resolve so the page reads as a whole.
const populated: readonly MockRoute[] = [
  { url: ORGS_URL, body: orgs },
  { url: POLICY_URL, body: policy },
  { url: HEALTH_URL, body: health },
  { url: '/api/v1/instance/oidc-providers', body: { providers: [] } },
  { url: '/api/v1/instance/federation-issuers', body: { count: 0, items: [] } },
  { url: '/api/v1/instance/saml-providers', body: { providers: [] } },
  { url: '/api/v1/instance/saml-sp-keys', body: { keys: [] } },
];

// Every read answered the same way: the page-wide states.
const uniformly = (rest: Partial<MockRoute>): readonly MockRoute[] =>
  [ORGS_URL, POLICY_URL, HEALTH_URL, ...PANEL_URLS].map((url) => ({ url, ...rest }));

const app = (responses: readonly MockRoute[]) => ({ auth: true, path: '/instance', responses });

const meta = {
  component: InstanceAdmin,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app(populated) },
} satisfies Meta<typeof InstanceAdmin>;

export default meta;
type Story = StoryObj<typeof meta>;

// Every read resolved: the directory rows, the policy ceiling and the prune status.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: 'Acme' })).toBeVisible();
    await expect(canvas.getByText('inactive')).toBeVisible();
    await expect(canvas.getByText('90 days')).toBeVisible();
    await expect(canvas.getByText(/payload pruning last succeeded/i)).toBeVisible();
  },
};

// Every read 404s: each panel says it is not disclosed, nothing is guessed empty.
export const NotDisclosed: Story = {
  parameters: { app: app(uniformly({ status: 404 })) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/organisation directory is not disclosed/i)).toBeVisible();
    await expect(canvas.getByText(/credential policy is not disclosed/i)).toBeVisible();
    await expect(canvas.getByText(/retention health is not disclosed/i)).toBeVisible();
  },
};

// Nothing settles: every panel stands in its loading status.
export const Loading: Story = {
  parameters: { app: app(uniformly({ pending: true })) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading organisations/i)).toBeVisible());
    await expect(canvas.getByText(/loading credential policy/i)).toBeVisible();
    await expect(canvas.getByText(/loading retention health/i)).toBeVisible();
  },
};

// Every read 403s: the session needs a second factor, said per panel.
export const SecondFactorRequired: Story = {
  parameters: { app: app(uniformly({ status: 403, body: { code: 'forbidden' } })) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/listing every organisation.*needs a second factor/i)).toBeVisible();
    await expect(canvas.getByText(/credential policy requires a second factor/i)).toBeVisible();
  },
};

// Every read failed outright: the server-failure alerts, no fabricated rows.
export const Failed: Story = {
  parameters: { app: app(uniformly({ status: 500, body: { code: 'internal' } })) },
  play: async ({ canvas }) => {
    // Three page-owned reads fail; each lands in its own alert as it settles.
    await waitFor(() => expect(canvas.getAllByText(/the server failed/i).length).toBeGreaterThanOrEqual(3));
  },
};

// The credential policy in edit mode: fields seeded from the fetched ceiling.
export const EditingCredentialPolicy: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'edit' }));
    await expect(canvas.getByLabelText('Maximum finite lifetime (seconds)')).toHaveValue('7776000');
    await expect(canvas.getByLabelText('Maximum live credentials per service account')).toHaveValue('5');
    await expect(canvas.getByRole('checkbox', { name: /no expiry/i })).not.toBeChecked();
    await expect(canvas.getByRole('button', { name: 'Save credential policy' })).toBeEnabled();
  },
};

// A crypto ceremony open: the consequences dialog for the instance DEK rotation.
export const RotateDekConfirm: Story = {
  play: async ({ canvas }) => {
    // Let the signed-in tree settle first: the rotate button exists before
    // whoami answers, and a click on that first tree is lost when it swaps.
    await expect(await canvas.findByRole('link', { name: 'Acme' })).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Rotate the instance DEK' }));
    const dialog = await canvas.findByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveTextContent(/incomplete until you run that re-encryption/i);
    await expect(canvas.getByRole('button', { name: 'Rotate the DEK' })).toBeEnabled();
  },
};
