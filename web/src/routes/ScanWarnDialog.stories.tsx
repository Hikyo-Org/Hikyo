import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ScanWarnDialog, type ScanWarnItem } from './ScanWarnDialog.tsx';

// Two rows: one carries an acknowledgement token (offers "Keep as config"),
// the other does not (the finding is still shown, but with nothing to dismiss
// it with beyond reclassifying the key).
const items: readonly ScanWarnItem[] = [
  {
    environmentId: 'env_01989abc-def0-7123-8123-000000000001',
    environmentName: 'production',
    value: 'postgres://user:pass@host/db',
    finding: {
      rule_id: 'high-entropy',
      surface: 'value_write',
      locator: 'app/DATABASE_URL',
      acknowledgement: 'ack-token-1',
    },
  },
  {
    environmentId: 'env_01989abc-def0-7123-8123-000000000002',
    environmentName: 'staging',
    value: 'postgres://user:pass@host/db',
    finding: {
      rule_id: 'high-entropy',
      surface: 'value_write',
      locator: 'app/DATABASE_URL',
    },
  },
];

const meta = {
  component: ScanWarnDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    keyName: 'DATABASE_URL',
    items,
    onDismiss: fn(async () => []),
    onReclassify: fn(async () => undefined),
    onClose: fn(),
  },
} satisfies Meta<typeof ScanWarnDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /possible secret in a config value/i }),
    ).toBeVisible();
    await expect(canvas.getAllByText('high-entropy')).toHaveLength(2);
    await expect(canvas.getByText('production')).toBeVisible();
    await expect(canvas.getByText('staging')).toBeVisible();
    // Only the acknowledged row offers "keep as config".
    await expect(canvas.getAllByRole('button', { name: /keep as config/i })).toHaveLength(1);
    await expect(
      canvas.getByRole('button', { name: /reclassify database_url as secret/i }),
    ).toBeVisible();
  },
};
