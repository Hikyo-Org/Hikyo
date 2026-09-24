import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Field } from './Field.tsx';

// The composed field is normally reached through Input, Select and Textarea;
// this page shows the layout those share with a bare control, so the label,
// hint and error rows can be designed once.
const meta = {
  component: Field,
  tags: ['ai-generated'],
  args: {
    label: 'Display name',
    children: (control) => <input {...control} defaultValue="Dana Jacobs" />,
  },
} satisfies Meta<typeof Field>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Display name')).toBeVisible();
  },
};

// A hint describes the control and sits under it in secondary ink.
export const WithHint: Story = {
  args: { hint: 'Shown to other members of the organisation.' },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Display name')).toHaveAccessibleDescription(
      'Shown to other members of the organisation.',
    );
  },
};

// An error marks the control invalid and is announced as an alert.
export const WithError: Story = {
  args: { hint: 'Shown to other members of the organisation.', error: 'A display name is required.' },
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Display name');
    await expect(control).toHaveAttribute('aria-invalid', 'true');
    await expect(canvas.getByRole('alert')).toHaveTextContent('A display name is required.');
    await expect(control).toHaveAccessibleDescription(/Shown to other members.*A display name is required/);
  },
};
