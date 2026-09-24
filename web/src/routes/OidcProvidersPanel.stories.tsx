import type { Meta, StoryObj } from '@storybook/react-vite';
import { zOidcProviderList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { OidcProvidersPanel } from './OidcProvidersPanel.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The panel's one read is the instance provider list; the editor and the
// delete dialog open from state, so the play functions click them open.
const LIST_URL = '/api/v1/instance/oidc-providers';

const providers = {
  providers: [
    {
      slug: 'okta',
      display_name: 'Okta',
      issuer: 'https://acme.okta.com',
      client_id: '0oa1b2c3d4e5f6g7h8i9',
      scopes: 'openid profile email',
      redirect_uri: 'https://hikyo.example/api/v1/auth/oidc/okta/callback',
      assurance_policy: '{"acr":["phr"]}',
      enabled: true,
    },
    {
      slug: 'legacy-adfs',
      display_name: 'Legacy ADFS',
      issuer: 'https://adfs.acme.example/adfs',
      client_id: 'hikyo-web',
      scopes: 'openid',
      redirect_uri: 'https://hikyo.example/api/v1/auth/oidc/legacy-adfs/callback',
      assurance_policy: null,
      enabled: false,
    },
  ],
} satisfies z.input<typeof zOidcProviderList>;

const list = (rest: Partial<MockRoute>): MockRoute => ({ url: LIST_URL, ...rest });

const meta = {
  component: OidcProvidersPanel,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [list({ body: providers })] },
  },
} satisfies Meta<typeof OidcProvidersPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Two providers, one advertised and one disabled, each with its action pair.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Okta')).toBeVisible();
    await expect(canvas.getByText('disabled')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Reconfigure Legacy ADFS' })).toBeVisible();
  },
};

// Nothing configured: the status line and the add affordance.
export const Empty: Story = {
  parameters: { app: { responses: [list({ body: { providers: [] } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no identity providers are configured/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: /add identity provider/i })).toBeVisible();
  },
};

// The list never settles: the loading status stands and no controls render.
export const Loading: Story = {
  parameters: { app: { responses: [list({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading identity providers/i)).toBeVisible());
  },
};

// A 403 on the MFA-mandatory read: the honest second-factor state, no list.
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [list({ status: 403, body: { code: 'forbidden' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/needs a second factor/i)).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /add identity provider/i })).toBeNull();
  },
};

// The read failed outright: the refusal sentence for the list operation.
export const Failed: Story = {
  parameters: { app: { responses: [list({ status: 500, body: { code: 'internal' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toBeVisible();
  },
};

// Reconfiguring an advertised provider and unticking Enabled: the slug and
// issuer are read-only, the secret is blank, and the disable impact is spelled out.
export const Reconfiguring: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Reconfigure Okta' }));
    await expect(canvas.getByRole('heading', { name: 'Reconfigure Okta' })).toBeVisible();
    await expect(canvas.getByLabelText('Issuer URL')).toBeDisabled();
    await expect(canvas.getByLabelText('Client secret')).toHaveValue('');
    await userEvent.click(canvas.getByRole('checkbox', { name: /enabled/i }));
    await expect(canvas.getByRole('alert')).toHaveTextContent(/disabling removes/i);
  },
};

// The delete ceremony: a modal that demands the immutable slug, typed.
export const DeleteConfirm: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Delete Legacy ADFS' }));
    const dialog = await canvas.findByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveTextContent(/delete legacy adfs\?/i);
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
  },
};
