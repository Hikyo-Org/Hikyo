import type { Meta, StoryObj } from '@storybook/react-vite';
import { zSamlProviderList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { SamlProvidersPanel } from './SamlProvidersPanel.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The panel's one read is the instance SAML provider list. The create form
// and the per-row editors open from state; the metadata ceremony's preview is
// the one mutation a story drives, answered by a POST row.
const LIST_URL = '/api/v1/instance/saml-providers';

const providers = {
  providers: [
    {
      slug: 'acme-adfs',
      display_name: 'Acme ADFS',
      kind: 'saml',
      entity_id: 'https://adfs.acme.example/adfs/services/trust',
      acs_url: 'https://hikyo.example/api/v1/auth/saml/acme-adfs/acs',
      sso_redirect_url: 'https://adfs.acme.example/adfs/ls/',
      signing_certificate_fingerprints: ['sha256:8f3a…c1d2', 'sha256:0b7e…9a44'],
      assurance_policy: ['urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport'],
      allow_email_nameid: false,
      force_sign_requests: true,
      metadata_source: 'file',
      metadata_url: null,
      metadata_signed: false,
      metadata_signing_fingerprint: null,
      metadata_valid_until: null,
      warnings: [
        {
          code: 'certificate-expiring',
          severity: 'warning',
          message: 'Signing certificate sha256:0b7e…9a44 expires in 12 days.',
          effective_at: '2026-09-01T00:00:00Z',
          fingerprint: 'sha256:0b7e…9a44',
        },
        {
          code: 'metadata-expired',
          severity: 'error',
          message: 'The pinned metadata document is past its validUntil; refresh it.',
          effective_at: '2026-09-10T00:00:00Z',
        },
      ],
      enabled: true,
      row_version: 4,
      created_at: '2026-06-01T00:00:00Z',
      updated_at: '2026-09-01T00:00:00Z',
    },
    {
      slug: 'okta-saml',
      display_name: 'Okta',
      kind: 'saml',
      entity_id: 'http://www.okta.com/exk1a2b3c4d5e6f7g8h9',
      acs_url: 'https://hikyo.example/api/v1/auth/saml/okta-saml/acs',
      sso_redirect_url: 'https://acme.okta.com/app/hikyo/exk1a2b3c4d5e6f7g8h9/sso/saml',
      signing_certificate_fingerprints: ['sha256:5e21…77af'],
      assurance_policy: null,
      allow_email_nameid: true,
      force_sign_requests: false,
      metadata_source: 'url',
      metadata_url: 'https://acme.okta.com/app/exk1a2b3c4d5e6f7g8h9/sso/saml/metadata',
      metadata_signed: true,
      metadata_signing_fingerprint: 'sha256:5e21…77af',
      metadata_valid_until: '2027-03-01T00:00:00Z',
      warnings: [],
      enabled: true,
      row_version: 1,
      created_at: '2026-08-15T00:00:00Z',
      updated_at: '2026-08-15T00:00:00Z',
    },
    {
      slug: 'retired-idp',
      display_name: 'Retired IdP',
      kind: 'saml',
      entity_id: 'https://idp.retired.example/saml',
      acs_url: 'https://hikyo.example/api/v1/auth/saml/retired-idp/acs',
      sso_redirect_url: 'https://idp.retired.example/saml/sso',
      signing_certificate_fingerprints: [],
      assurance_policy: null,
      allow_email_nameid: false,
      force_sign_requests: false,
      metadata_source: 'file',
      metadata_url: null,
      metadata_signed: false,
      metadata_signing_fingerprint: null,
      metadata_valid_until: null,
      warnings: [],
      enabled: false,
      row_version: 9,
      created_at: '2025-11-01T00:00:00Z',
      updated_at: '2026-07-01T00:00:00Z',
    },
  ],
} satisfies z.input<typeof zSamlProviderList>;

// A preview answer: the fetched metadata rotates a certificate and adds an
// endpoint, so nothing applies until the operator confirms exactly this diff.
const previewDiff = {
  url: `${LIST_URL}/okta-saml/refresh-metadata`,
  method: 'POST',
  body: {
    applied: false,
    provider: null,
    diff: {
      endpoints_added: ['https://acme.okta.com/app/hikyo/exk1a2b3c4d5e6f7g8h9/sso/saml-post'],
      endpoints_removed: [],
      certs_added_fps: ['sha256:c0ff…ee00'],
      certs_removed_fps: ['sha256:5e21…77af'],
    },
    required_fingerprints: ['sha256:c0ff…ee00'],
    required_endpoints: ['https://acme.okta.com/app/hikyo/exk1a2b3c4d5e6f7g8h9/sso/saml-post'],
  },
} satisfies MockRoute;

const list = (rest: Partial<MockRoute>): MockRoute => ({ url: LIST_URL, ...rest });

const meta = {
  component: SamlProvidersPanel,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [list({ body: providers })] },
  },
} satisfies Meta<typeof SamlProvidersPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Three providers: a file-backed one carrying a warning and an error
// diagnostic, a signed url-backed one with a validity window, and a disabled one.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Acme ADFS')).toBeVisible();
    await expect(canvas.getByRole('alert')).toHaveTextContent(/past its validUntil/i);
    await expect(canvas.getByText('disabled')).toBeVisible();
  },
};

// Nothing configured: the status line and the configure affordance.
export const Empty: Story = {
  parameters: { app: { responses: [list({ body: { providers: [] } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no saml providers are configured/i)).toBeVisible();
  },
};

// The list never settles: the loading status stands.
export const Loading: Story = {
  parameters: { app: { responses: [list({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading saml providers/i)).toBeVisible());
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

// The create form, file-backed by default: slug, entity ID, pasted XML.
export const Creating: Story = {
  parameters: { app: { responses: [list({ body: { providers: [] } })] } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: /configure a new saml provider/i }));
    await expect(canvas.getByRole('heading', { name: /configure a saml provider/i })).toBeVisible();
    await expect(canvas.getByLabelText('Metadata XML')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /preview and configure/i })).toBeVisible();
  },
};

// The metadata ceremony mid-way: the fetched diff is shown and nothing has
// applied; the danger button confirms exactly the listed trust change.
export const MetadataDiff: Story = {
  parameters: { app: { responses: [list({ body: providers }), previewDiff] } },
  play: async ({ canvas }) => {
    const row = (await canvas.findByText('Okta')).closest('[data-saml-provider]');
    if (!(row instanceof HTMLElement)) throw new Error('Okta row missing');
    const buttons = [...row.querySelectorAll('button')];
    const refresh = buttons.find((button) => button.textContent === 'Refresh metadata');
    if (refresh === undefined) throw new Error('Refresh metadata button missing');
    await userEvent.click(refresh);
    await userEvent.click(canvas.getByRole('button', { name: /fetch and preview metadata/i }));
    await expect(await canvas.findByText(/this metadata changes trust state/i)).toBeVisible();
    await expect(canvas.getByText('sha256:c0ff…ee00')).toBeVisible();
    await expect(
      canvas.getByRole('button', { name: /confirm and apply the trust change/i }),
    ).toBeVisible();
  },
};
