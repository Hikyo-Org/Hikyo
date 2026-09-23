import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, screen, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../../.storybook/topLayerDocs.ts';
import { ProofDialog } from './ProofDialog.tsx';

const meta = {
  component: ProofDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    lede: 'To save the registration policy, enter the code from your authenticator, or your password if you have none. Fresh proof, every time.',
    field: 'code-or-password',
    reauth: true,
    onCancel: fn(),
    onSubmit: fn(),
  },
} satisfies Meta<typeof ProofDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The reauth-gated shape: a password field (never autofilled with a one-time
// code), Confirm in the CHANGED slate, disabled until something is typed.
export const ReauthGated: Story = {
  play: async ({ args }) => {
    const field = await screen.findByLabelText('Authenticator code or password');
    await expect(field).toHaveAttribute('type', 'password');
    await expect(field).toHaveAttribute('autocomplete', 'current-password');
    const confirm = screen.getByRole('button', { name: 'Confirm' });
    await expect(confirm).toHaveClass('btn--reauth');
    await expect(confirm).toBeDisabled();
    await userEvent.type(field, '123456');
    await userEvent.click(confirm);
    await expect(args.onSubmit).toHaveBeenCalledWith('123456', expect.any(Function));
  },
};

export const AuthenticatorCode: Story = {
  args: { field: 'code', reauth: false, lede: 'Removing a passkey is proved by a credential you already hold.' },
  play: async () => {
    const field = await screen.findByLabelText('Authenticator code');
    await expect(field).toHaveAttribute('autocomplete', 'one-time-code');
    await expect(screen.getByRole('button', { name: 'Confirm' })).not.toHaveClass('btn--reauth');
  },
};

export const Refused: Story = {
  args: { failure: 'That proof was not accepted. Enter a fresh authenticator code, or your password if you have no authenticator.' },
};
