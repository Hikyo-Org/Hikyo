import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { UpdateJobStatus } from './Remotes.tsx';

type Props = Parameters<typeof UpdateJobStatus>[0];
type Job = NonNullable<Props['job']>;

const jobID = 'upd_01989abc-def0-7123-8123-000000000001';

const base: Job = {
  id: jobID,
  backend: 'compose',
  version: '1.4.0',
  state: 'running',
  phase: 'apply',
  requested_at: '2026-08-23T08:00:00Z',
  started_at: '2026-08-23T08:00:05Z',
};

const meta = {
  component: UpdateJobStatus,
  tags: ['ai-generated'],
  args: { jobID },
} satisfies Meta<typeof UpdateJobStatus>;

export default meta;
type Story = StoryObj<typeof meta>;

// No job yet: the caller has an id but no data, so the line reads "queued".
export const Queued: Story = {
  args: { job: undefined },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('status')).toHaveTextContent(/queued/i);
  },
};

// A running job stays a plain status line, phase in parentheses.
export const Running: Story = {
  args: { job: base },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('status')).toHaveTextContent(/running \(apply\)/i);
  },
};

// A succeeded terminal outcome is still a status line, not an alert.
export const Succeeded: Story = {
  args: { job: { ...base, state: 'succeeded', phase: 'complete', finished_at: '2026-08-23T08:02:00Z' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('status')).toHaveTextContent(/succeeded/i);
  },
};

// A failed terminal outcome escalates to an alert carrying the failure code.
export const Failed: Story = {
  args: {
    job: {
      ...base,
      state: 'rollback-failed',
      phase: 'rollback',
      failure_code: 'health-check-timeout',
      finished_at: '2026-08-23T08:03:00Z',
    },
  },
  play: async ({ canvas }) => {
    const alert = canvas.getByRole('alert');
    await expect(alert).toHaveTextContent(/rollback-failed/i);
    await expect(alert).toHaveTextContent(/health-check-timeout/i);
  },
};
