import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { DeveloperCredentialPolicy } from './DeveloperCredentialPolicy.tsx';

const url = '/api/v1/instance/developer-credential-policy';
const meta = {
  title: 'Features/Instance/DeveloperCredentialPolicy',
  component: DeveloperCredentialPolicy,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: 'Instance-config lifetime ceiling, read from the server. Whole seconds from 1 to 28,800; every change requires confirmation of permanent expiry clamping. Raising the ceiling cannot renew existing credentials.' } },
    app: { responses: [{ url, body: { max_lifetime_seconds: 28800 } }] },
  },
} satisfies Meta<typeof DeveloperCredentialPolicy>;
export default meta;
type Story = StoryObj<typeof meta>;

export const CurrentPolicy: Story = { play: async ({ canvas }) => {
  await expect(await canvas.findByRole('button', { name: 'Edit developer credential policy' })).toBeVisible();
  await expect(canvas.getByText(/28,?800 seconds/)).toBeVisible();
} };
export const Phone: Story = { ...CurrentPolicy, globals: { viewport: { value: 'phone', isRotated: false } } };
export const Loading: Story = { parameters: { app: { responses: [{ url, pending: true }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('status')).toHaveTextContent('Loading developer credential policy');
} };
export const PermissionOrSecondFactorRequired: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('alert')).toHaveTextContent('second-factor assurance');
  await expect(canvas.queryByRole('button', { name: 'Edit developer credential policy' })).toBeNull();
} };
export const ConfirmLowerCeiling: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Edit developer credential policy' }));
  const input = canvas.getByRole('spinbutton', { name: 'Maximum developer credential lifetime (seconds)' });
  await userEvent.clear(input);
  await userEvent.type(input, '3600');
  await userEvent.click(canvas.getByRole('button', { name: 'Review developer credential policy' }));
  await expect(await canvas.findByRole('dialog')).toHaveTextContent('may expire immediately');
  await expect(canvas.getByRole('dialog')).toHaveTextContent('3600 seconds');
} };
