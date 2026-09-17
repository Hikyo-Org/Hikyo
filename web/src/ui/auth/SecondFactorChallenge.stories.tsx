import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { SecondFactorChallenge } from './SecondFactorChallenge.tsx';

const meta = {
  component: SecondFactorChallenge,
  tags: ['ai-generated'],
  args: {
    username: 'alex',
    totp: true,
    passkey: true,
    busy: null,
    error: null,
    onCode: fn(),
    onPasskey: fn(),
  },
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof SecondFactorChallenge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const AuthenticatorAndPasskey: Story = {};

export const AuthenticatorOnly: Story = { args: { passkey: false } };

export const PasskeyOnly: Story = { args: { totp: false } };

/** The enrolled factor cannot be presented from this device: named, with the way out. */
export const NoFactorPresentable: Story = {
  args: { totp: false, passkey: false },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/cannot be presented/i);
    await expect(canvas.queryByRole('button')).toBeNull();
  },
};

export const CheckingCode: Story = { args: { busy: 'code' } };

export const WaitingForPasskey: Story = { args: { busy: 'passkey' } };

export const CodeRefused: Story = {
  args: {
    error:
      'That code was not accepted. A code is valid for one time step and is used once: wait for the next code and try again.',
  },
};

// The step has no way past it: no skip, no "later", no link out.
export const NoSkipControl: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.queryByRole('button', { name: /continue|skip|later|without/i })).toBeNull();
    await expect(canvas.queryByRole('link')).toBeNull();
  },
};

export const SubmitsTrimmedCode: Story = {
  play: async ({ canvas, args }) => {
    const field = canvas.getByLabelText('Authenticator code');
    await expect(canvas.getByRole('button', { name: 'Present code' })).toBeDisabled();
    await userEvent.type(field, ' 123456 ');
    await userEvent.click(canvas.getByRole('button', { name: 'Present code' }));
    await expect(args.onCode).toHaveBeenCalledWith('123456');
    await expect(field).toHaveValue('');
  },
};
