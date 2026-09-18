import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Select } from './Select.tsx';

const meta = {
  component: Select,
  tags: ['ai-generated'],
  args: {
    label: 'Environment',
    children: (
      <>
        <option value="prod">Production</option>
        <option value="staging">Staging</option>
        <option value="dev">Development</option>
      </>
    ),
  },
} satisfies Meta<typeof Select>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Disabled: Story = { args: { disabled: true } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Environment')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 320 }}>
      <Select label="Environment" defaultValue="staging">
        <option value="prod">Production</option>
        <option value="staging">Staging</option>
        <option value="dev">Development</option>
      </Select>
      <Select label="Disabled" disabled>
        <option>Production</option>
      </Select>
    </div>
  ),
};

// The caller's own accessibility wiring survives the field's: an external
// description is merged in front of the generated hint and error, and an
// external invalid state is kept when the field has no error of its own.
export const ExternalDescriptionIsMerged: Story = {
  args: { label: 'Environment', hint: 'The hint.', error: 'The error.' },
  render: (args) => (
    <>
      <p id="external-note">An external note.</p>
      <Select {...args} aria-describedby="external-note" />
    </>
  ),
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Environment');
    await expect(control).toHaveAccessibleDescription(/an external note.*the hint.*the error/i);
    await expect(control).toHaveAttribute('aria-invalid', 'true');
  },
};

export const ExternalInvalidIsKept: Story = {
  args: { label: 'Environment', hint: 'The hint.', 'aria-invalid': true },
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Environment');
    await expect(control).toHaveAttribute('aria-invalid', 'true');
    await expect(control).toHaveAccessibleDescription('The hint.');
  },
};
