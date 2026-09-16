import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { codesStep, totpStep } from './fixtures.ts';
import { SecondFactorSetup } from './SecondFactorSetup.tsx';

const meta = {
  component: SecondFactorSetup,
  tags: ['ai-generated'],
  args: {
    username: 'alex',
    step: { kind: 'choose' },
    passkeys: true,
    busy: null,
    error: null,
    onChooseTotp: fn(),
    onChoosePasskey: fn(),
    onConfirmCode: fn(),
    onDone: fn(),
  },
  decorators: [
    (Story) => (
      <div className="login" style={{ minHeight: 'auto' }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof SecondFactorSetup>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Choose: Story = {};

export const ChooseNoPasskeySupport: Story = { args: { passkeys: false } };

export const Authenticator: Story = { args: { step: totpStep } };

export const AuthenticatorCodeRefused: Story = {
  args: { step: totpStep, error: 'That code did not match. Wait for the next one and try again.' },
};

export const RecoveryCodes: Story = { args: { step: codesStep } };

// No skip on any step: the gate holds until a factor stands and the codes are
// acknowledged. All three steps are mounted so the assertion covers each.
export const NoSkipOnAnyStep: Story = {
  render: (args) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <SecondFactorSetup {...args} step={{ kind: 'choose' }} />
      <SecondFactorSetup {...args} step={totpStep} />
      <SecondFactorSetup {...args} step={codesStep} />
    </div>
  ),
  play: async ({ canvas }) => {
    await expect(canvas.getAllByRole('heading', { level: 1 })).toHaveLength(3);
    await expect(canvas.queryByRole('button', { name: /skip|later|not now|without/i })).toBeNull();
    await expect(canvas.queryByRole('link')).toBeNull();
  },
};

export const CodesNeedAcknowledgement: Story = {
  args: { step: codesStep },
  play: async ({ canvas, args }) => {
    const proceed = canvas.getByRole('button', { name: 'Continue to Hikyo' });
    await expect(proceed).toBeDisabled();
    await userEvent.click(canvas.getByRole('checkbox'));
    await expect(proceed).toBeEnabled();
    await userEvent.click(proceed);
    await expect(args.onDone).toHaveBeenCalled();
  },
};
