import type { Meta, StoryObj } from '@storybook/react-vite';
import type { zSecurityMaintenancePublicStatus } from '@hikyo/zod';
import { expect } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { MockRoute } from '../../.storybook/withApp.tsx';
import { SecurityMaintenancePanel } from './SecurityMaintenancePanel.tsx';

// One read: the status the separate maintenance coordinator published. The
// clock is an argument so "stale" is decided against a fixed instant.
const URL = '/api/v1/instance/security-maintenance';
const NOW = new Date('2026-11-04T12:00:00Z');

const current = {
  mode: 'automatic',
  channel: 'stable',
  enrolled: true,
  phase: 'current',
  protection_gap: false,
  applicable: [],
  coverage: ['HK-SA-2026-0007'],
  covered_components: ['hikyo'],
  coverage_through: '2026-11-01',
  catalog_sequence: 42,
  last_attempt: '2026-11-04T11:30:00Z',
  last_verified: '2026-11-04T11:30:00Z',
  known_until: '2026-11-06T11:30:00Z',
  running_version: '1.4.2',
} satisfies z.input<typeof zSecurityMaintenancePublicStatus>;

const reported = (status: z.input<typeof zSecurityMaintenancePublicStatus>): readonly MockRoute[] => [
  { url: URL, body: { reported: true, unreadable: false, status } },
];

const meta = {
  title: 'Features/Instance/SecurityMaintenancePanel',
  component: SecurityMaintenancePanel,
  tags: ['ai-generated'],
  args: { now: NOW },
  parameters: { ...topLayerDocs, app: { responses: reported(current) } },
} satisfies Meta<typeof SecurityMaintenancePanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Enrolled, verified and nothing outstanding.
export const EnrolledAndCurrent: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/enrolled and current/i)).toBeVisible();
    await expect(canvas.queryByText(/stale/i)).toBeNull();
  },
};

// An update is being applied: the target is named.
export const UpdateInProgress: Story = {
  parameters: {
    app: {
      responses: reported({
        ...current,
        phase: 'applying',
        applicable: ['HK-SA-2026-0008'],
        coverage: [],
        covered_components: [],
        target_version: '1.4.3',
      }),
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/update in progress: moving to 1\.4\.3/i)).toBeVisible();
  },
};

// A known advisory stays unremediated and the coordinator is not just waiting:
// the exact command and the cause are shown.
export const ProtectionGap: Story = {
  parameters: {
    app: {
      responses: reported({
        ...current,
        phase: 'blocked',
        reason: 'operation_requires_reconciliation',
        protection_gap: true,
        applicable: ['HK-SA-2026-0008'],
        coverage: [],
        covered_components: [],
        target_version: '1.4.3',
        action: 'hikyo security-maintenance reconcile --confirm 4e1f0c7a9b2d',
        detail: 'unfinished security operation requires exact-target operator reconciliation',
      }),
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/protection gap: 1 applicable advisory remains/i);
    await expect(canvas.getByText('hikyo security-maintenance reconcile --confirm 4e1f0c7a9b2d')).toBeVisible();
    await expect(canvas.getByText(/requires exact-target operator reconciliation/)).toBeVisible();
  },
};

// Metadata expired before the clock: "current" is withheld and the warning stands.
export const Stale: Story = {
  parameters: { app: { responses: reported({ ...current, known_until: '2026-11-03T11:30:00Z' }) } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/security metadata is stale/i)).toBeVisible();
    await expect(canvas.queryByText(/enrolled and current/i)).toBeNull();
  },
};

// No file configured, or none written yet.
export const NotReported: Story = {
  parameters: { app: { responses: [{ url: URL, body: { reported: false, unreadable: false } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no status has been reported/i)).toBeVisible();
  },
};

// The file exists but cannot be trusted: an alert, never a healthy state.
export const Unreadable: Story = {
  parameters: { app: { responses: [{ url: URL, body: { reported: false, unreadable: true } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/could not be read or parsed/i);
  },
};
