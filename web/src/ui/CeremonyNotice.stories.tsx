import type { Meta, StoryObj } from '@storybook/react-vite';

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
export const Default: Story = {};
export const LongContent: Story = {
  args: { glyph: '⚿', children: 'This credential is shown once. Store it in the destination before closing this dialog. Your browser will ask for a passkey to authorize issuance; canceling leaves the current credentials unchanged.' },
  decorators: [(Story) => <div style={{ maxWidth: 320 }}><Story /></div>],
};
