import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Textarea } from './Textarea.tsx';

const meta = {
  component: Textarea,
  tags: ['ai-generated'],
  args: { label: 'Description', placeholder: 'What this project is for' },
} satisfies Meta<typeof Textarea>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Filled: Story = { args: { defaultValue: 'Customer-facing API and its workers.' } };
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Description')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 360 }}>
      <Textarea label="Description" placeholder="What this project is for" />
      <Textarea label="Filled" defaultValue="Customer-facing API and its workers." hint="Shown on the projects list." />
      <Textarea label="Refused" defaultValue="x" error="At least three characters." />
      <Textarea label="Disabled" defaultValue="locked" disabled />
    </div>
  ),
};

// The caller's own accessibility wiring survives the field's: an external
// description is merged in front of the generated hint and error, and an
// external invalid state is kept when the field has no error of its own.
export const ExternalDescriptionIsMerged: Story = {
  args: { label: 'Description', hint: 'The hint.', error: 'The error.' },
  render: (args) => (
    <>
      <p id="external-note">An external note.</p>
      <Textarea {...args} aria-describedby="external-note" />
    </>
  ),
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Description');
    await expect(control).toHaveAccessibleDescription(/an external note.*the hint.*the error/i);
    await expect(control).toHaveAttribute('aria-invalid', 'true');
  },
};

export const ExternalInvalidIsKept: Story = {
  args: { label: 'Description', hint: 'The hint.', 'aria-invalid': true },
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Description');
    await expect(control).toHaveAttribute('aria-invalid', 'true');
    await expect(control).toHaveAccessibleDescription('The hint.');
  },
};
