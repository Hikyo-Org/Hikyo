import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { profiles } from '../testkit/storybookPki.ts';
import { PkiProfilesPanel } from './PkiProfilesPanel.tsx';

const meta = {
  title: 'Features/PKI/PkiProfilesPanel',
  component: PkiProfilesPanel,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: profiles }] } },
} satisfies Meta<typeof PkiProfilesPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('web-services', { exact: true })).toBeVisible();
  },
};
export const Empty: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: { profiles: [] } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByText(/No certificate profiles yet/)).toBeVisible(); },
};
export const Loading: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', pending: true }] } },
  play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText('Loading certificate profiles…')).toBeVisible()); },
};
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('needs instance-config and a second factor'); },
};
export const Failed: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', status: 500, body: { error: { code: 'internal', message: 'Internal' } } }] } },
  play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent(/server error 500|could not|failed/i); },
};
export const Narrowing: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Narrow' }));
    await expect(canvas.getByRole('textbox', { name: 'Policy of web-services (JSON)' })).toBeVisible();
    await expect(canvas.getByText(/Only a narrowing is accepted/)).toBeVisible();
  },
};
export const InvalidPolicy: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pki/profiles', body: { profiles: [] } }] } },
  play: async ({ canvas }) => {
    await canvas.findByText(/No certificate profiles yet/);
    await userEvent.type(canvas.getByRole('textbox', { name: 'Name' }), 'web');
    await userEvent.clear(canvas.getByRole('textbox', { name: 'Policy (JSON)' }));
    await userEvent.type(canvas.getByRole('textbox', { name: 'Policy (JSON)' }), 'invalid JSON');
    await userEvent.click(canvas.getByRole('button', { name: 'Create' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent('not valid JSON');
  },
};
