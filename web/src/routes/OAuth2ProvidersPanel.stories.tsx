import type { Meta, StoryObj } from '@storybook/react-vite';
import { zOauth2ProviderList } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
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
  const panel = within(canvas.getByRole('complementary', { name: 'Reconfigure GitHub' }));
  await expect(panel.getByRole('textbox', { name: 'Provider slug' })).toBeDisabled();
  await expect(panel.getByLabelText('Client secret')).toHaveValue('');
} };
// Adding opens the empty editor in the side panel; Save leads, Cancel follows.
export const Adding: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Add GitHub provider' }));
  const panel = within(canvas.getByRole('complementary', { name: 'Add GitHub provider' }));
  await expect(panel.getByRole('textbox', { name: 'Provider slug' })).toBeEnabled();
  await expect(panel.getByRole('button', { name: 'Save provider' }).nextElementSibling).toBe(panel.getByRole('button', { name: 'Cancel' }));
  await userEvent.click(panel.getByRole('button', { name: 'Cancel' }));
  await expect(canvas.queryByRole('complementary')).toBeNull();
} };
export const DeleteConfirmation: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: 'Delete GitHub' }));
  await expect(await canvas.findByRole('dialog')).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
} };
