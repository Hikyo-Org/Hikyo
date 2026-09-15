import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { RefusalFinding } from '../api/client.ts';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';

const acknowledgedFinding: RefusalFinding = {
  rule_id: 'aws-access-key',
  surface: 'value_write',
  locator: 'app/API_KEY',
  acknowledgement: 'ack-token-1',
};

const meta = {
  component: ScanBlockDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    title: 'Declaration blocked by secret scanning',
    intro: 'Declaring API_KEY was refused: the value looks like credential material.',
    findings: [acknowledgedFinding],
    onOverride: fn(async () => undefined),
    onClose: fn(),
  },
} satisfies Meta<typeof ScanBlockDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Overridable: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /declaration blocked by secret scanning/i }),
    ).toBeVisible();
    await expect(canvas.getByText('aws-access-key')).toBeVisible();
    await expect(canvas.getByText('app/API_KEY')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /acknowledge and continue/i })).toBeVisible();
  },
};

// A finding with no acknowledgement token is a hard block: the browser cannot
// override it, so no override button is offered at all.
export const HardBlock: Story = {
  args: {
    findings: [
      {
        rule_id: 'high-entropy',
        surface: 'edit',
        locator: 'app/SECRET',
      },
    ],
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('high-entropy')).toBeVisible();
    await expect(
      canvas.queryByRole('button', { name: /acknowledge and continue/i }),
    ).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: /^close$/i })).toBeVisible();
  },
};
