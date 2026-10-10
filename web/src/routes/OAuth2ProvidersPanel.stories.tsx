import type { Meta, StoryObj } from '@storybook/react-vite';
import { zOauth2ProviderList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { OAuth2ProvidersPanel } from './OAuth2ProvidersPanel.tsx';

const url = '/api/v1/instance/oauth2-providers';
const body = { providers: [{ profile: 'github', slug: 'github', display_name: 'GitHub', issuer: 'https://github.com', client_id: 'public-client-fixture', redirect_uri: 'https://hikyo.example/api/v1/auth/oauth2/github/callback', enabled: true, row_version: 1 }] } satisfies z.input<typeof zOauth2ProviderList>;
const meta = {
  title: 'Features/Identity/OAuth2ProvidersPanel',
  component: OAuth2ProvidersPanel,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "GitHub sign-in provider administration. Lists enabled and disabled providers, opens a write-only client-secret editor, and requires the immutable provider slug before deletion. GitHub sign-in itself contributes one factor." } },  app: { responses: [{ url, body }] } },
} satisfies Meta<typeof OAuth2ProvidersPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = { play: async ({ canvas }) => { await expect(await canvas.findByRole('button', { name: 'Reconfigure GitHub' })).toBeVisible(); } };
export const Empty: Story = { parameters: { app: { responses: [{ url, body: { providers: [] } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByText('No GitHub provider configured.')).toBeVisible(); } };
export const Loading: Story = { parameters: { app: { responses: [{ url, pending: true }] } }, play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading GitHub providers…')).toBeVisible()); } };
export const SecondFactorRequired: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('requires a second factor'); } };
export const Failed: Story = { parameters: { app: { responses: [{ url, status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('Providers could not be loaded'); } };
export const Reconfiguring: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Reconfigure GitHub' }));
  await expect(canvas.getByRole('textbox', { name: 'Provider slug' })).toBeDisabled();
  await expect(canvas.getByLabelText('Client secret')).toHaveValue('');
} };
export const DeleteConfirmation: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Delete GitHub' }));
  await expect(await canvas.findByRole('dialog')).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
} };
