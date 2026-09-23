import type { Meta, StoryObj } from '@storybook/react-vite';
import { zSamlSpKeyList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { SamlSpKeysPanel } from './SamlSpKeysPanel.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The panel's one read is the SP key list. An SP always holds an active key,
// so there is no empty state; mid-rotation it also holds a retiring one.
const LIST_URL = '/api/v1/instance/saml-sp-keys';

const ACTIVE = 'sha256:9c1e4b7a2f0d8e6c5b3a1f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c';
const RETIRING = 'sha256:3a7f1c9e5b2d8f4a6c0e2b4d6f8a1c3e5b7d9f1a3c5e7b9d1f3a5c7e9b1d3f5a';

const keys = {
  keys: [
    { fingerprint: ACTIVE, state: 'active', created_at: '2026-09-01T09:00:00Z' },
    { fingerprint: RETIRING, state: 'retiring', created_at: '2026-03-01T09:00:00Z' },
  ],
} satisfies z.input<typeof zSamlSpKeyList>;

const list = (rest: Partial<MockRoute>): MockRoute => ({ url: LIST_URL, ...rest });

const meta = {
  component: SamlSpKeysPanel,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [list({ body: keys })] },
  },
} satisfies Meta<typeof SamlSpKeysPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Mid-rotation: the active key offers compromise-retire, the retiring one retire.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(ACTIVE)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Compromise-retire' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Retire' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /rotate the active signing key/i })).toBeEnabled();
  },
};

// The list never settles: the loading status stands and rotate stays disabled.
export const Loading: Story = {
  parameters: { app: { responses: [list({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading sp signing keys/i)).toBeVisible());
    await expect(canvas.getByRole('button', { name: /rotate the active signing key/i })).toBeDisabled();
  },
};

// A 403 on the MFA-mandatory read: the second-factor state, no rows.
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [list({ status: 403, body: { code: 'forbidden' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/needs a second factor and this authority/i)).toBeVisible();
  },
};

// The read failed outright: the list refusal sentence.
export const Failed: Story = {
  parameters: { app: { responses: [list({ status: 500, body: { code: 'internal' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/the server failed/i)).toBeVisible();
  },
};

// The ordinary retirement ceremony opened on the retiring key: the danger
// zone spells out the consequence and demands the full fingerprint, typed.
export const RetireConfirm: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Retire' }));
    await expect(canvas.getByText(/retiring erases this key/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Retire key' })).toBeDisabled();
  },
};

// The compromise ceremony on the active key: no overlap window, spelled out.
export const CompromiseRetireConfirm: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Compromise-retire' }));
    await expect(canvas.getByText(/no overlap window/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Compromise-retire key' })).toBeDisabled();
  },
};
