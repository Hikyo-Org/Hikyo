import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { issuers } from '../testkit/storybookPki.ts';
import { PkiIssuersPanel } from './PkiIssuersPanel.tsx';

const meta = {
  title: 'Features/PKI/PkiIssuersPanel',
  component: PkiIssuersPanel,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url: '/api/v1/instance/pki/issuers', body: issuers }] } },
} satisfies Meta<typeof PkiIssuersPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('issuing', { exact: true })).toBeVisible();
  },
};
export const Empty: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/issuers', body: { issuers: [] } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/No certificate authorities yet/)).toBeVisible(); },
};
export const Loading: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/issuers', pending: true }] } },
  play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading certificate authorities…')).toBeVisible()); },
};
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/issuers', status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('needs instance-config and a second factor'); },
};
export const Failed: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/issuers', status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent(/server error 500|could not|failed/i); },
};
