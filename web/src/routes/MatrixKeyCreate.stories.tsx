import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { MatrixKeyCreate } from './MatrixKeyCreate.tsx';

const development = {
  id: 'env_01989abc-def0-7123-8123-000000000001',
  org_id: 'org_01989abc-def0-7123-8123-000000000000',
  project_id: 'prj_01989abc-def0-7123-8123-000000000000',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const production = { ...development, id: 'env_01989abc-def0-7123-8123-000000000002', name: 'production', display_order: 1 };

const meta = {
  component: MatrixKeyCreate,
  tags: ['ai-generated'],
  args: {
    folders: ['app'],
    environments: [development, production],
    protectedEnvironmentIds: [production.id],
    initialFolder: 'app',
    existingKeyNames: ['EXISTING_KEY'],
    busy: false,
    mutationError: null,
    onClose: fn(),
    onCreate: fn(async () => undefined),
  },
} satisfies Meta<typeof MatrixKeyCreate>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /new key/i })).toBeVisible();
    await expect(canvas.getByLabelText('Group')).toHaveValue('app');
    await expect(canvas.getByLabelText('Key name')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^declare$/i })).toBeEnabled();
  },
};
