import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { SystemScopeRefusal } from './SystemScope.tsx';

// A deep link into the Hikyo system scope for a surface the scope refuses
// outright (machine consumers, adapters, SCIM). The gate renders this reason
// instead of the page; a Router is all it needs (the app decorator supplies one).
const meta = {
  component: SystemScopeRefusal,
  tags: ['ai-generated'],
  parameters: { app: { responses: [] } },
} satisfies Meta<typeof SystemScopeRefusal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Adapters: Story = {
  args: { surface: 'adapters' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/deployment adapters are not available here/i)).toBeVisible();
    await expect(canvas.getByRole('link')).toBeVisible();
  },
};

export const MachineAccess: Story = {
  args: { surface: 'machine-access' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/machine access is not available here/i)).toBeVisible();
  },
};

export const Scim: Story = {
  args: { surface: 'scim' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/scim provisioning is not available here/i)).toBeVisible();
  },
};
