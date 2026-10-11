import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { InstanceMailPanel } from './InstanceMailPanel.tsx';

const url = '/api/v1/instance/mail';
const meta = {
  title: 'Features/Instance/InstanceMailPanel',
  component: InstanceMailPanel,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "Instance mailer status and an explicit test-send form. Configured reports valid settings; delivery is only checked by a requested test. Sending asks for fresh proof and refuses unconfigured, busy or unauthorized requests." } },  app: { responses: [{ url, body: { configured: true } }] } },
} satisfies Meta<typeof InstanceMailPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Configured: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText('configured', { exact: true })).toBeVisible(); } };
export const Unconfigured: Story = { parameters: { app: { responses: [{ url, body: { configured: false } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('button', { name: 'Send test…' })).toBeDisabled(); } };
export const Loading: Story = { parameters: { app: { responses: [{ url, pending: true }] } }, play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading mailer status…')).toBeVisible()); } };
export const SecondFactorRequired: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('second factor'); } };
export const Failed: Story = { parameters: { app: { responses: [{ url, status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('server error 500'); } };
export const FreshProofRequired: Story = { play: async ({ canvas, step }) => {
  await step('Address the test message', async () => {
    await userEvent.type(await canvas.findByRole('textbox', { name: 'Test recipient' }), 'operator@example.com');
    await userEvent.click(canvas.getByRole('button', { name: 'Send test…' }));
  });
  await step('Inspect the fresh-proof dialog', async () => {
    await expect(await canvas.findByRole('dialog')).toBeVisible();
    await expect(canvas.getByText(/enter the code from your authenticator/)).toBeVisible();
  });
} };
