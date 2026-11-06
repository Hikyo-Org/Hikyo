import type { Meta, StoryObj } from '@storybook/react-vite';
import { zPersonalOffboardingReport } from '@hikyo/zod';
import { expect, within } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { OffboardingReport } from './OffboardingReport.tsx';

// The offboarding report for one member of a project: the shared keys they are
// recorded as receiving, whether each changed since, the service accounts they
// created, and what the report cannot show. Names only, never values.
const ROUTE = '/orgs/:org/projects/:project/offboarding-report';
const PATH = `/orgs/${ORG}/projects/${PRJ}/offboarding-report`;
const REPORT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/personal-offboarding-report/alex`;

const report = {
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174101',
  username: 'alex',
  evidence_from: '2026-04-01T00:00:00Z',
  generated_at: '2026-11-06T10:00:00Z',
  received: [
    { environment_id: STAGING, environment_name: 'staging', key_name: 'DATABASE_URL', last_received_at: '2026-10-30T16:00:00Z', changed_since: false },
    { environment_id: PROD, environment_name: 'production', key_name: 'STRIPE_SECRET_KEY', last_received_at: '2026-09-12T09:00:00Z', changed_since: true },
  ],
  service_accounts: [{ id: 'prn_123e4567-e89b-12d3-a456-426614174201', name: 'alex-ci', created_at: '2026-05-02T08:00:00Z' }],
  limits: [
    'Disclosures before the evidence window, or never audited, do not appear.',
    'A value the person copied elsewhere cannot be recalled; rotate it.',
  ],
} satisfies z.input<typeof zPersonalOffboardingReport>;

const meta = {
  title: 'Pages/OffboardingReport',
  id: 'routes-offboardingreport',
  component: OffboardingReport,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { path: `${PATH}?member=alex`, routePath: ROUTE, responses: [{ url: REPORT_URL, body: report }] },
  },
} satisfies Meta<typeof OffboardingReport>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Report: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'alex', level: 2 })).toBeVisible();
    await expect(canvas.getByText(/^Evidence from /)).toBeVisible();
    const table = within(canvas.getByRole('table'));
    await expect(table.getByText('DATABASE_URL')).toBeVisible();
    await expect(table.getByText('unchanged, rotate')).toBeVisible();
    await expect(table.getByText('changed since')).toBeVisible();
    await expect(canvas.getByText('alex-ci')).toBeVisible();
    await expect(canvas.getByText(/cannot be recalled/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Print' })).toBeVisible();
  },
};

export const Phone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'alex', level: 2 })).toBeVisible();
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};

// No member chosen yet: only the form.
export const ChooseMember: Story = {
  parameters: { app: { path: PATH, routePath: ROUTE, responses: [] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText(/^Member/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Show report' })).toBeDisabled();
  },
};

// The caller lacks manage-members or audit-read.
export const Refused: Story = {
  parameters: {
    app: { path: `${PATH}?member=alex`, routePath: ROUTE, responses: [{ url: REPORT_URL, status: 403, body: { error: { code: 'forbidden', message: 'not permitted' } } }] },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('The report needs manage-members and audit-read on this project.')).toBeVisible();
  },
};
