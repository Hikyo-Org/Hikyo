import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { CredentialForm } from './Adapters.tsx';

const meta = {
  component: CredentialForm,
  tags: ['ai-generated'],
  args: {
    busy: false,
    onCancel: fn(),
    onSubmit: fn(async () => undefined),
  },
} satisfies Meta<typeof CredentialForm>;

export default meta;
type Story = StoryObj<typeof meta>;

// Empty on open: Replace is disabled until a credential is entered.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('form', { name: 'Replace credential' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Replace' })).toBeDisabled();
  },
};

// Typing a credential enables Replace and submits the plaintext through onSubmit.
export const Entered: Story = {
  play: async ({ args, canvas }) => {
    await userEvent.type(canvas.getByLabelText('Credential'), 'ghp_secret');
    const replace = canvas.getByRole('button', { name: 'Replace' });
    await expect(replace).toBeEnabled();
    await userEvent.click(replace);
    await waitFor(() => expect(args.onSubmit).toHaveBeenCalledWith('ghp_secret'));
  },
};

// While the mutation runs the button reads "Replacing…" and is disabled.
export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Replacing…' })).toBeDisabled();
  },
};
