import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAuthMethods, zRegistrationPolicy } from '@hikyo/zod';
import { expect, fn, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { OpenRegistrationPanel } from './OpenRegistration.tsx';

const url = '/api/v1/instance/registration-policy';
const policy = {
  id: 'reg_storybook', external: [], local: { domains: ['example.com'] }, landing: { kind: 'none' },
  authority_principal_id: 'prn_operator', state: 'active', row_version: 1,
  created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
} satisfies z.input<typeof zRegistrationPolicy>;
const methods = { url: '/api/v1/auth/methods', body: zAuthMethods.parse({ providers: [], local_login_enabled: true, signup_open: false, signup_paused: false, signup_methods: [] }) };
const closed = { url, status: 404, body: { error: { code: 'not-found', message: 'No policy' } } };
const meta = {
  title: 'Features/Members/OpenRegistration',
  component: OpenRegistrationPanel,
  tags: ['ai-generated'],
  args: { scope: { kind: 'instance' }, scopeName: 'This instance', origin: 'https://hikyo.example', authorityName: () => 'Test operator', onChanged: fn() },
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "Instance or organisation registration policy and its editor. No policy means closed; an inactive policy explains its actual authority or precondition failure. Every save, re-save and closure requires fresh proof." } },  app: { responses: [closed, methods] } },
} satisfies Meta<typeof OpenRegistrationPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Closed: Story = { play: async ({ canvas }) => { await expect(await canvas.findByRole('button', { name: 'Open registration…' })).toBeVisible(); } };
export const Active: Story = { parameters: { app: { responses: [{ url, body: policy }, methods] } } };
export const InactiveAuthority: Story = { parameters: { app: { responses: [{ url, body: { ...policy, state: 'inactive', inactive_cause: 'authority-lost' } }, methods] } }, play: async ({ canvas }) => { await expect(await canvas.findByText(/no longer holds the grant/)).toBeVisible(); } };
export const Loading: Story = { parameters: { app: { responses: [{ url, pending: true }] } }, play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading the registration policy…')).toBeVisible()); } };
export const Failed: Story = { parameters: { app: { responses: [{ url, status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('server error 500'); } };
export const EditorValidation: Story = { play: async ({ canvas, step }) => {
  await step('Open the registration policy editor', async () => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Open registration…' }));
    await expect(await canvas.findByRole('dialog')).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: 'Email + password' })).not.toBeChecked();
  });
  await step('Reject a policy that admits nobody', async () => {
    await userEvent.click(canvas.getByRole('button', { name: 'Save' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent('Admit at least one way to sign up');
  });
} };
