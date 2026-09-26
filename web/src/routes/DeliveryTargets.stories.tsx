import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ORG, PRJ } from '../testkit/ids.ts';
import { listing, NOW, refusedTarget, target, viewOf } from '../testkit/deliveryTargets.ts';
import { serviceAccount } from '../testkit/machineAccess.ts';
import { DeliveryTargetsPanel } from './DeliveryTargets.tsx';

// The Kubernetes tab's two layers, one story per derived state (#790). The
// panel is presentational: each story hands it a parsed listing. The empty
// `app` parameter mounts the router its key-view links need, and no fetch.
const meta = {
  component: DeliveryTargetsPanel,
  tags: ['ai-generated'],
  parameters: { app: { responses: [] } },
  args: {
    project: { org: ORG, project: PRJ },
    accounts: [serviceAccount],
    now: NOW,
    known: true,
    view: viewOf(listing({ targets: [target(0, 'reported')] })),
  },
} satisfies Meta<typeof DeliveryTargetsPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// A fresh report whose conditions all assert True: still only "reported".
export const ReportedHealthy: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText('reported')).toBeVisible();
    await expect(canvas.getByText('reported by the controller, 5 minutes ago')).toBeVisible();
    await expect(canvas.getByText('Ready=True/Reconciled')).toBeVisible();
  },
};

// A fresh report asserting a failure: the reason links to the key view.
export const ReportedDegraded: Story = {
  args: {
    view: viewOf(
      listing({
        targets: [
          target(0, 'reported', [
            { type: 'Ready', status: 'False', reason: 'Blocked', observed_generation: 3 },
            { type: 'Delivery', status: 'False', reason: 'UndeliveredSecrets', observed_generation: 3 },
          ]),
        ],
      }),
    ),
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Ready=False/Blocked')).toBeVisible();
    await expect(
      canvas.getByRole('link', { name: 'UndeliveredSecrets: open the production keys' }),
    ).toBeVisible();
  },
};

// No report within twice the heartbeat plus backoff.
export const Stale: Story = {
  args: { view: viewOf(listing({ targets: [target(0, 'stale')] })) },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('stale')).toBeVisible();
    await expect(canvas.getByText(/the last was received 4 hours ago/)).toBeVisible();
  },
};

// The latest report named a condition outside the vocabulary.
export const Refused: Story = {
  args: { view: viewOf(listing({ targets: [refusedTarget(0)] })) },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('refused')).toBeVisible();
    await expect(canvas.getByText(/refused \(vocabulary\) 10 minutes ago/)).toBeVisible();
  },
};

// The account lost report-delivery-status or every live credential.
export const ReporterRevoked: Story = {
  args: { view: viewOf(listing({ targets: [target(0, 'reporter-revoked')] })) },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('reporter revoked')).toBeVisible();
  },
};

// The account's reports were refused at its row quota: a principal notice.
export const QuotaRefused: Story = {
  args: {
    view: viewOf(listing({ targets: [target(0, 'reported')], quotaRefused: true })),
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('quota-refused')).toBeVisible();
  },
};

// Observed fetching, but no report received: unknown, never healthy.
export const Unknown: Story = {
  args: { view: viewOf(listing({})) },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('? unknown')).toBeVisible();
    await expect(canvas.getByText('no report received')).toBeVisible();
  },
};

// Nothing observed, nothing reported: "no reports", never healthy.
export const Empty: Story = {
  args: { view: viewOf(listing({ observed: false })) },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('No reports.')).toBeVisible();
  },
};

// A server that does not advertise delivery-target-report: no list is read,
// and the panel says reporting is unsupported rather than "no reports".
export const Unsupported: Story = {
  args: { view: { support: 'unsupported', reports: [], failures: [], isPending: false } },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByText(/this server does not support delivery-target reporting/i),
    ).toBeVisible();
    await expect(canvas.queryByText('No reports.')).not.toBeInTheDocument();
  },
};
