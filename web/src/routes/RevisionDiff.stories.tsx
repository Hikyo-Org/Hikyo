import type { Meta, StoryObj } from '@storybook/react-vite';
import { zRevealWindow, zRevisionDiff } from '@hikyo/zod';
import { expect, fn, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { RevisionDiffDialog } from './RevisionDiff.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The revision diff, opened from the history drawer's detail pane. On mount it
// POSTs the comparison; config rows arrive in the clear, secret rows as
// write-presence only. Revealing one secret key first reads the reveal window
// (a live one skips the ceremony) then POSTs the per-key disclosure, which the
// dialog holds for 30 seconds and drops on blur. The screen calls `useAuth`
// for the session id its ceremony task is keyed on, so the harness runs it
// with `auth: true`.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const ENV = 'env_123e4567-e89b-12d3-a456-426614174012';
const ENV_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/environments/${ENV}`;
const DIFF_URL = `${ENV_URL}/revisions/diff`;
const REVEAL_URL = `${ENV_URL}/revisions/diff/reveal`;
const WINDOW_URL = `${ENV_URL}/reveal-window`;

type Diff = z.input<typeof zRevisionDiff>;
type Row = Diff['items'][number];

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const config = (n: number, name: string, status: Row['status'], before?: string, after?: string): Row => ({
  key_id: keyId(n),
  name,
  classification: 'config',
  status,
  revealed: true,
  ...(before === undefined ? {} : { before }),
  ...(after === undefined ? {} : { after }),
});
const secret = (n: number, name: string, status: 'added' | 'removed' | 'edited' | 'not_edited'): Row => ({
  key_id: keyId(n),
  name,
  classification: 'secret',
  status,
  revealed: false,
});

// Every row status the wire allows: config in the clear with before/after,
// secrets as write-presence only, and one long value to wrap.
const populated: Diff = {
  left_revision: 3,
  right_revision: 7,
  items: [
    config(1, 'LOG_LEVEL', 'changed', 'info', 'warn'),
    config(2, 'PUBLIC_BASE_URL', 'unchanged', 'https://app.example.com', 'https://app.example.com'),
    config(3, 'FEATURE_FLAGS', 'changed', 'checkout-v2', 'checkout-v2,search-rerank,beta-dashboard,new-onboarding,eu-data-residency'),
    config(4, 'CACHE_TTL_SECONDS', 'added', undefined, '300'),
    config(5, 'LEGACY_REPORTING_ENDPOINT', 'removed', 'https://reports.legacy.example.com', undefined),
    secret(6, 'DATABASE_URL', 'edited'),
    secret(7, 'STRIPE_SECRET_KEY', 'not_edited'),
    secret(8, 'OIDC_CLIENT_SECRET', 'added'),
    secret(9, 'SMTP_PASSWORD', 'removed'),
  ],
};

const liveWindow = {
  effective_window_seconds: 600,
  protected: false,
  totp_offered: false,
  live: true,
  single_decision: false,
  can_reveal: true,
  expires_at: new Date(Date.now() + 9 * 60_000).toISOString(),
} satisfies z.input<typeof zRevealWindow>;

const disclosed: Diff = {
  left_revision: 3,
  right_revision: 7,
  items: [
    {
      key_id: keyId(6),
      name: 'DATABASE_URL',
      classification: 'secret',
      status: 'changed',
      revealed: true,
      before: 'postgres://app:hunter2@db-old.internal:5432/app',
      after: 'postgres://app:s3cr3t@db.internal:5432/app',
    },
  ],
};

const diff = (rest: Omit<MockRoute, 'url' | 'method'>): MockRoute => ({ url: DIFF_URL, method: 'POST', ...rest });

const meta = {
  component: RevisionDiffDialog,
  tags: ['ai-generated'],
  args: {
    env: { org: ORG, project: PRJ, environment: ENV },
    environmentName: 'production',
    left: 3n,
    right: 7n,
    onClose: fn(),
  },
  parameters: { ...topLayerDocs, app: { auth: true, responses: [diff({ body: populated })] } },
} satisfies Meta<typeof RevisionDiffDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The comparison resolved: config rows with their before → after, secret rows
// masked with a per-key Reveal.
export const Populated: Story = {
  play: async ({ canvas }) => {
    // A fresh query per retry: AuthProvider swaps the tree once whoami settles.
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Diff r3 → r7 · production' })).toBeVisible());
    await waitFor(() => expect(canvas.getByText('info → warn')).toBeVisible());
    await expect(canvas.getByText('absent → 300')).toBeVisible();
    await expect(canvas.getAllByText('masked · write-presence only')).toHaveLength(4);
    await expect(canvas.getByRole('button', { name: 'Reveal DATABASE_URL in diff' })).toBeVisible();
  },
};

// One secret disclosed under a live window: both retained values show with the
// re-mask countdown and a Mask control.
export const Revealed: Story = {
  parameters: {
    app: {
      auth: true,
      responses: [
        diff({ body: populated }),
        { url: WINDOW_URL, body: liveWindow },
        { url: REVEAL_URL, method: 'POST', body: disclosed },
      ],
    },
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Reveal DATABASE_URL in diff' }));
    await expect(await canvas.findByText(/postgres:\/\/app:hunter2@db-old\.internal:5432\/app → postgres/)).toBeVisible();
    await expect(canvas.getByText(/^Re-masks in \d+s\. Switching away masks immediately\.$/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Mask DATABASE_URL' })).toBeVisible();
  },
};

// Neither revision set a key.
export const Empty: Story = {
  parameters: { app: { auth: true, responses: [diff({ body: { left_revision: 3, right_revision: 7, items: [] } satisfies Diff })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('No set keys in these revisions.')).toBeVisible();
  },
};

// The comparison never settles.
export const Loading: Story = {
  parameters: { app: { auth: true, responses: [diff({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText('Reading revision diff…')).toBeVisible());
  },
};

// The comparison was refused: the dialog stays open and says so.
export const Failed: Story = {
  parameters: { app: { auth: true, responses: [diff({ status: 403, body: { error: 'revision diff refused' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toBeVisible();
  },
};
