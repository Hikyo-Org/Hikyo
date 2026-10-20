import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { AppRoutes } from '../app/App.tsx';
import { createPairingFixture, pairingIdentity, pairingSample } from './RemotePairing.story-fixtures.ts';
import { CreatePairingInviteForm } from './RemotePairing.tsx';

const interactiveFixture = createPairingFixture();

const invite = {
  id: 'invite-fixture', label: 'Approved peer', origin: 'https://remote.example',
  spki_pin: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
  created_at: '2026-10-20T11:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(),
  credential_expires_at: new Date(Date.now() + 2_592_000_000).toISOString(),
};
const configurationResponse = { url: '/api/v1/instance/pairing-invites', body: { items: [], count: 0, origin: invite.origin } };
const meta = {
  title: 'Pages/Remotes/Pairing invitations',
  component: CreatePairingInviteForm,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [configurationResponse, { url: '/api/v1/instance/pairing-invites', method: 'POST', status: 201, body: { invite, invite_bundle: pairingSample.bundle } }] },
    docs: { ...topLayerDocs.docs, description: { component: 'Administrator approval by manually entering a privately received phrase. Invitation copy and QR share only the invitation, never the phrase or local continuation.' } },
  },
} satisfies Meta<typeof CreatePairingInviteForm>;
export default meta;
type Story = StoryObj<typeof meta>;

export const InteractivePreview: Story = {
  render: () => <AppRoutes />,
  beforeEach: async () => {
    interactiveFixture.reset();
    await import('../app/route-groups/workspace.ts');
    return () => interactiveFixture.reset();
  },
  parameters: {
    app: { auth: true, identity: pairingIdentity, path: '/remotes', routeTree: true, responses: interactiveFixture.responses },
    docs: { description: { story: 'The real Remotes route and approval panel with sample API data. Enter a sample phrase and label. The instance supplies its configured HTTPS address and certificate fingerprint. Creation displays a sample invitation and its QR code. The preview performs no cryptography and makes no real remote connection. Workspace popup authentication is excluded.' } },
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Create pairing invitation' }));
    await expect(canvas.getByRole('complementary', { name: 'Create pairing invitation' })).toBeVisible();
  },
};

export const ManualApproval: Story = {
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Phrase received privately'), 'abacus abacus abacus abacus abacus abacus');
    await userEvent.type(canvas.getByLabelText('Invitation label'), invite.label);
    await expect(await canvas.findByText(invite.origin)).toBeVisible();
    await expect(canvas.queryByLabelText("This instance's HTTPS origin")).toBeNull();
    await expect(canvas.queryByLabelText("This instance's certificate fingerprint")).toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: 'Approve and create invitation' }));
    await expect(await canvas.findByRole('img', { name: 'Pairing invitation QR code' })).toBeVisible();
    await expect(canvas.getByLabelText('Pairing invitation')).toHaveValue(pairingSample.bundle);
    await userEvent.click(canvas.getByRole('button', { name: 'Hide invitation' }));
    await expect(canvas.queryByRole('img', { name: 'Pairing invitation QR code' })).toBeNull();
    await expect(canvas.queryByLabelText('Pairing invitation')).toBeNull();
  },
};

export const PhoneApproval: Story = {
  ...ManualApproval,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

export const OversizedInvitation: Story = {
  parameters: { app: { responses: [configurationResponse, { url: '/api/v1/instance/pairing-invites', method: 'POST', status: 201, body: { invite, invite_bundle: 'x'.repeat(2_001) } }] } },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Phrase received privately'), 'abacus abacus abacus abacus abacus abacus');
    await userEvent.type(canvas.getByLabelText('Invitation label'), invite.label);
    await expect(await canvas.findByText(invite.origin)).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Approve and create invitation' }));
    await expect(await canvas.findByText('This invitation is too long for a QR code. Copy the invitation instead.')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Copy invitation' })).toBeEnabled();
  },
};

export const LoadingConfiguration: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-invites', pending: true }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText("Loading this instance's pairing address…")).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Approve and create invitation' })).toBeDisabled();
    await expect(canvas.queryByLabelText("This instance's HTTPS origin")).toBeNull();
    await expect(canvas.queryByLabelText("This instance's certificate fingerprint")).toBeNull();
  },
};

export const ConfigurationRequired: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-invites', body: { items: [], count: 0, origin: 'http://local.example', configuration_error: 'A public HTTPS origin is required.' } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: 'Open instance configuration' })).toHaveAttribute('href', '/instance/config');
    await expect(canvas.getByRole('button', { name: 'Approve and create invitation' })).toBeDisabled();
    await expect(canvas.queryByText('http://local.example')).toBeNull();
  },
};

export const ConfigurationReadFailed: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-invites', status: 503, body: { error: { code: 'service_unavailable', message: 'Unavailable' } } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: 'Retry pairing configuration' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Approve and create invitation' })).toBeDisabled();
  },
};

export const CertificateDiscoveryFailed: Story = {
  parameters: { app: { responses: [configurationResponse, { url: '/api/v1/instance/pairing-invites', method: 'POST', status: 400, body: { error: { code: 'bad_request', message: 'Invalid request', detail: 'The public HTTPS certificate could not be verified. Ask the instance operator to check HTTPS or the advanced HIKYO_PAIRING_SPKI_PIN setting.' } } }] } },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Phrase received privately'), ' A4b ');
    await userEvent.type(canvas.getByLabelText('Invitation label'), invite.label);
    await expect(await canvas.findByText(invite.origin)).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Approve and create invitation' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent('The public HTTPS certificate could not be verified.');
    await expect(canvas.getByLabelText('Invitation label')).toHaveValue(invite.label);
    await expect(canvas.getByLabelText('Phrase received privately')).toBeEnabled();
    await expect(canvas.queryByLabelText("This instance's certificate fingerprint")).toBeNull();
  },
};
