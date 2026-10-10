import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { SAMLDone } from './SAMLDone.tsx';

const meta = {
  title: 'Pages/SAMLDone',
  component: SAMLDone,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: 'SAML callback refusal when no initiating transaction exists. A successful callback navigates the frame away and remains covered by SAMLDone.test.tsx; the identity-provider flow is human-only.' } } },
  beforeEach: () => {
    const previous = globalThis.location.href;
    const path = new URL(previous);
    path.search = '?state=storybook-missing-transaction';
    globalThis.history.replaceState(null, '', path);
    return () => globalThis.history.replaceState(null, '', previous);
  },
} satisfies Meta<typeof SAMLDone>;
export default meta;
type Story = StoryObj<typeof meta>;
export const MissingTransaction: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent('transaction is missing or already completed');
    await expect(canvas.getByRole('link', { name: 'Return to sign in' })).toHaveAttribute('href', '/login');
  },
};
