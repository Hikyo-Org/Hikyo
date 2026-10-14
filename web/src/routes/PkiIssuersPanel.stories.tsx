import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import { expectStackedRow } from '../../.storybook/layout.ts';
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
    await expect(await canvas.findByText('issuing v1', { exact: true })).toBeVisible();
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
// Every form opens in the side panel: creating an issuer, and a version's
// typed-name revoke.
export const FormsInPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Create a certificate authority' }));
    const create = within(canvas.getByRole('complementary', { name: 'Create a certificate authority' }));
    await expect(create.getByLabelText('Common name')).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary')).toBeNull();
    await userEvent.click(canvas.getAllByRole('button', { name: 'Revoke (compromise)' })[0] as HTMLElement);
    const revoke = within(canvas.getByRole('complementary', { name: /^Revoke / }));
    await expect(revoke.getByRole('button', { name: 'Revoke' })).toBeVisible();
  },
};

// The pending version's own form (install a signed certificate) opens in the
// side panel too.
export const InstallPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Install certificate' }));
    const panel = within(canvas.getByRole('complementary', { name: /^Install certificate of/ }));
    await expect(panel.getByLabelText('Signed CA certificate (PEM)')).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Verify and install' })).toBeDisabled();
  },
};

// At a phone width a row's actions (here the newest active version's five:
// Rotate, Publish CRL, View CRL, Retire, Revoke) sit on the equal-width
// two-column grid, wrapping to a third row rather than overflowing.
export const PhoneStackedActions: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const row = (await canvas.findByText('issuing v1')).closest<HTMLElement>('.settings-row');
    if (row === null) throw new Error('issuer row missing');
    await expectStackedRow(row, 5);
  },
};
