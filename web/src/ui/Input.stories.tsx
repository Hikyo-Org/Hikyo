import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { Input } from './Input.tsx';

const meta = {
  component: Input,
  tags: ['ai-generated'],
  args: { label: 'Username', placeholder: 'you@example.com' },
} satisfies Meta<typeof Input>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Password: Story = { args: { label: 'Password', type: 'password', placeholder: '' } };
export const PasswordRevealable: Story = {
  args: { label: 'Password', type: 'password', placeholder: '', revealable: true, defaultValue: 'hunter2' },
  play: async ({ canvas }) => {
    const field = canvas.getByLabelText('Password');
    await expect(field).toHaveAttribute('type', 'password');
    await userEvent.click(canvas.getByRole('button', { name: 'Show password' }));
    await expect(field).toHaveAttribute('type', 'text');
    await expect(canvas.getByRole('button', { name: 'Hide password' })).toHaveAttribute('aria-pressed', 'true');
  },
};
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };
export const Mono: Story = { args: { label: 'Key name', mono: true, defaultValue: 'DATABASE_URL' } };

// `mono` reaches the control, not the wrapper: the label and hint stay in the UI face.
export const MonoTargetsTheControl: Story = {
  args: { ...Mono.args, hint: 'Uppercase, digits and underscores.' },
  play: async ({ canvas }) => {
    const input = canvas.getByLabelText('Key name');
    await expect(input).toHaveClass('mono');
    await expect(getComputedStyle(input).fontFamily).toMatch(/Plex Mono/);
    await expect(getComputedStyle(canvas.getByText(/underscores/)).fontFamily).not.toMatch(/Plex Mono/);
  },
};

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
      <Input label="Password" type="password" revealable defaultValue="hunter2" />
      <Input label="Remote URL" hint="The origin only; paths and query strings are refused." />
      <Input label="Fingerprint" defaultValue="sha256:ab12" error="That fingerprint does not match the certificate the remote presented." />
      <Input label="Disabled" defaultValue="locked" disabled />
    </div>
  ),
};


// The caller's own accessibility wiring survives the field's: an external
// description is merged in front of the generated hint and error, and an
// external invalid state is kept when the field has no error of its own.
export const ExternalDescriptionIsMerged: Story = {
  args: { label: 'Remote URL', hint: 'The hint.', error: 'The error.' },
  render: (args) => (
    <>
      <p id="external-note">An external note.</p>
      <Input {...args} aria-describedby="external-note" />
    </>
  ),
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Remote URL');
    await expect(control).toHaveAccessibleDescription(/an external note.*the hint.*the error/i);
    await expect(control).toHaveAttribute('aria-invalid', 'true');
  },
};

export const ExternalInvalidIsKept: Story = {
  args: { label: 'Remote URL', hint: 'The hint.', 'aria-invalid': true },
  play: async ({ canvas }) => {
    const control = canvas.getByLabelText('Remote URL');
    await expect(control).toHaveAttribute('aria-invalid', 'true');
    await expect(control).toHaveAccessibleDescription('The hint.');
  },
};
