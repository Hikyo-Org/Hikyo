import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { MintConnectionForm } from './Remotes.tsx';

type Props = Parameters<typeof MintConnectionForm>[0];

// The form drives a `useMintConnection` result. It is a plain object, not a
// hook, so a story can hand it a resolved-or-erroring stand-in directly.
const idleMint: Props['mint'] = {
  mint: fn(async () => ({ value: 'hk_minted_value', clamped: false })),
  pending: false,
  error: null,
};

const meta = {
  component: MintConnectionForm,
  tags: ['ai-generated'],
  args: { mint: idleMint, onMinted: fn() },
} satisfies Meta<typeof MintConnectionForm>;

export default meta;
type Story = StoryObj<typeof meta>;

// The idle form: heading, label field, lifetime radios, submit.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /mint a credential/i })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /mint credential/i })).toBeVisible();
  },
};

// A failed mint surfaces the reason as an alert above the fields.
export const WithError: Story = {
  args: {
    mint: { ...idleMint, error: new Error('The mint was refused.') },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toBeVisible();
  },
};
