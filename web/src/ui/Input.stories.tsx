import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Input } from './Input.tsx';

const meta = {
  component: Input,
  tags: ['ai-generated'],
  args: { label: 'Username', placeholder: 'you@example.com' },
} satisfies Meta<typeof Input>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Password: Story = { args: { label: 'Password', type: 'password' } };
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };

export const WithHint: Story = {
  args: { label: 'Remote URL', hint: 'The origin only; paths and query strings are refused.' },
};

export const WithError: Story = {
  args: {
    label: 'Remote URL',
    defaultValue: 'hikyo.example/api',
    error: 'Enter an https origin, such as https://hikyo.example.',
  },
};

// The hint and the error describe the control, and an error marks it invalid;
// that is what a screen reader reads out and what the border colour echoes.
export const ErrorIsWired: Story = {
  args: { ...WithError.args, hint: 'The origin only.' },
  play: async ({ canvas }) => {
    const input = canvas.getByLabelText('Remote URL');
    await expect(input).toHaveAttribute('aria-invalid', 'true');
    await expect(input).toHaveAccessibleDescription(/origin only.*https origin/i);
    await expect(canvas.getByRole('alert')).toBeVisible();
  },
};

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    // The label must resolve the control, or the field is a swatch with no name.
    await expect(canvas.getByLabelText('Username')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 320 }}>
      <Input label="Username" placeholder="you@example.com" />
      <Input label="Password" type="password" />
      <Input label="Remote URL" hint="The origin only; paths and query strings are refused." />
      <Input label="Fingerprint" defaultValue="sha256:ab12" error="That fingerprint does not match the certificate the remote presented." />
      <Input label="Disabled" defaultValue="locked" disabled />
    </div>
  ),
};

