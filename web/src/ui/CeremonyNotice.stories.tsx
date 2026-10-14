import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { CeremonyNotice } from './CeremonyNotice.tsx';

const meta = {
  title: 'Design system/CeremonyNotice',
  component: CeremonyNotice,
  tags: ['ai-generated'],
  args: { children: 'Confirm this action with your passkey. Your browser will ask you to approve it.' },
  parameters: { docs: { description: { component: 'Shared ceremony notice. The browser-owned WebAuthn prompt remains a human-only flow; this module covers its explanatory surface.' } } },
} satisfies Meta<typeof CeremonyNotice>;
export default meta;
type Story = StoryObj<typeof meta>;
// The default mark is the alert glyph, decorative beside the words.
export const Default: Story = {
  play: async ({ canvas }) => {
    const glyph = canvas.getByRole('status').querySelector('svg.alert__glyph');
    await expect(glyph).toHaveAttribute('data-glyph', 'alert');
    await expect(glyph).toHaveAttribute('aria-hidden', 'true');
  },
};
export const LongContent: Story = {
  args: { glyph: 'passkey', children: 'This credential is shown once. Store it in the destination before closing this dialog. Your browser will ask for a passkey to authorize issuance; canceling leaves the current credentials unchanged.' },
  decorators: [(Story) => <div style={{ maxWidth: 320 }}><Story /></div>],
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('status').querySelector('svg.alert__glyph')).toHaveAttribute('data-glyph', 'passkey');
  },
};
