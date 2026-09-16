import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { AuthenticatorCodeField } from './AuthenticatorCodeField.tsx';

const meta = {
  component: AuthenticatorCodeField,
  tags: ['ai-generated'],
  args: { submitLabel: 'Present code', busy: null, disabled: false, onSubmit: fn() },
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <div className="login__card">
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof AuthenticatorCodeField>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Checking: Story = { args: { busy: 'Checking…', disabled: true } };

// Disabled until a code is typed; submits trimmed; clears after.
export const SubmitsTrimmedAndClears: Story = {
  play: async ({ canvas, args }) => {
    const field = canvas.getByLabelText('Authenticator code');
    const submit = canvas.getByRole('button', { name: 'Present code' });
    await expect(submit).toBeDisabled();
    await userEvent.type(field, ' 123456 ');
    await userEvent.click(submit);
    await expect(args.onSubmit).toHaveBeenCalledWith('123456');
    await expect(field).toHaveValue('');
  },
};
