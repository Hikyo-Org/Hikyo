import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { AppRoutes } from '../app/App.tsx';
import { createPairingFixture, pairingIdentity, pairingSample } from './RemotePairing.story-fixtures.ts';
import { PairRemoteForm } from './RemotePairing.tsx';
import { PanelForm } from './Sections.tsx';

const prepared = { phrase: 'abacus abacus abacus abacus abacus abacus', requester_state: 'opaque-local-continuation-fixture', expires_at: new Date(Date.now() + 3_600_000).toISOString() };
const interactiveFixture = createPairingFixture();
const meta = {
  title: 'Pages/Remotes/Pairing',
  component: PairRemoteForm,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [{ url: '/api/v1/instance/pairing-requests', method: 'POST', status: 201, body: prepared }] },
    docs: { ...topLayerDocs.docs, description: { component: 'Requester pairing with memory-only continuation. Preparation contacts only the local instance; the administrator invitation is exchanged outside Hikyo.' } },
  },
} satisfies Meta<typeof PairRemoteForm>;
export default meta;
type Story = StoryObj<typeof meta>;

// Manual design preview. These deterministic samples are not pairing authority.
export const InteractivePreview: Story = {
  render: () => <AppRoutes />,
  beforeEach: async () => {
    interactiveFixture.reset();
    await import('../app/route-groups/workspace.ts');
    return () => interactiveFixture.reset();
  },
  parameters: {
    app: { auth: true, identity: pairingIdentity, path: '/remotes', routeTree: true, responses: interactiveFixture.responses },
    docs: { description: { story: 'The real Remotes route and requester panel with sample API data. Choose any password or generated word count, then create a sample invitation in the approval panel and copy or scan its QR code. No keys, cryptographic exchange or real remote connection is created. Workspace popup sign-in is excluded and explicitly refused by the fixture. Use the Storybook theme toolbar for dark or light appearance.' } },
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Pair a remote' }));
    await expect(canvas.getByRole('complementary', { name: 'Pair a remote' })).toBeVisible();
  },
};

export const LocalPreparation: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Generate pairing phrase' }));
    await expect(await canvas.findByLabelText('Pairing phrase')).toHaveTextContent(prepared.phrase);
    await expect(canvas.getByLabelText('Invitation from the administrator')).toHaveAttribute('type', 'password');
    await expect(canvas.getByRole('button', { name: 'Connect remote' })).toBeDisabled();
    await userEvent.click(canvas.getByRole('button', { name: 'Hide pairing phrase' }));
    await expect(canvas.queryByLabelText('Pairing phrase')).toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: 'Reveal pairing phrase' }));
    await expect(canvas.getByLabelText('Pairing phrase')).toBeVisible();
  },
};

export const PhonePreparation: Story = {
  ...LocalPreparation,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

export const PermissionRefusal: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-requests', method: 'POST', status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Generate pairing phrase' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent('instance-config');
  },
};

export const CustomShortSecret: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-requests', method: 'POST', status: 201, body: { ...prepared, phrase: ' A4b ' } }] } },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('radio', { name: 'Use my own password or phrase' }));
    const secret = canvas.getByLabelText('Your password or phrase');
    await expect(secret).toHaveAttribute('type', 'password');
    await userEvent.type(secret, ' A4b ');
    await userEvent.click(canvas.getByRole('button', { name: 'Prepare with my password or phrase' }));
    const disclosed = await canvas.findByLabelText('Pairing phrase');
    await expect(disclosed.textContent).toBe(' A4b ');
    await expect(getComputedStyle(disclosed).whiteSpace).toBe('break-spaces');
    await expect(canvas.queryByLabelText('Your password or phrase')).toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: 'Hide pairing phrase' }));
    await expect(canvas.queryByLabelText('Pairing phrase')).toBeNull();
  },
};

export const PhoneCustomShortSecret: Story = {
  ...CustomShortSecret,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

const publicLongSecret = 'X'.repeat(4_096);

export const LongSecretDisclosure: Story = {
  render: () => <main><h1>Pairing disclosure</h1><PanelForm opener="Pair a remote">{() => <PairRemoteForm />}</PanelForm></main>,
  parameters: { app: { responses: [{ url: '/api/v1/instance/pairing-requests', method: 'POST', status: 201, body: { ...prepared, phrase: publicLongSecret } }] } },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Pair a remote' }));
    const panel = canvas.getByRole('complementary', { name: 'Pair a remote' });
    await userEvent.click(within(panel).getByRole('button', { name: 'Generate pairing phrase' }));
    const disclosed = await within(panel).findByLabelText('Pairing phrase');
    await expect(disclosed.textContent).toBe(publicLongSecret);
    await expect(getComputedStyle(disclosed).whiteSpace).toBe('break-spaces');
    await expect(getComputedStyle(disclosed).overflowWrap).toBe('anywhere');
    await expect(disclosed.scrollWidth <= disclosed.clientWidth + 1).toBe(true);
    const body = panel.querySelector('.side-panel__body');
    if (!(body instanceof HTMLElement)) throw new Error('Missing pairing panel body');
    await expect(body.scrollWidth <= body.clientWidth + 1).toBe(true);
    await userEvent.click(within(panel).getByRole('button', { name: 'Hide pairing phrase' }));
    await expect(within(panel).queryByLabelText('Pairing phrase')).toBeNull();
  },
};

export const PhoneLongSecretDisclosure: Story = {
  ...LongSecretDisclosure,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

export const InvitationReview: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Generate pairing phrase' }));
    await userEvent.type(await canvas.findByLabelText('Remote name'), 'Platform');
    await userEvent.type(canvas.getByLabelText('Invitation from the administrator'), pairingSample.bundle);
    await expect(canvas.getByLabelText('Invitation destination review')).toHaveTextContent(pairingSample.origin);
    await expect(canvas.getByRole('button', { name: 'Connect remote' })).toBeEnabled();
  },
};

export const PhoneInvitationReview: Story = {
  ...InvitationReview,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

export const UnrelatedQrFallback: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Generate pairing phrase' }));
    await userEvent.type(await canvas.findByLabelText('Invitation from the administrator'), 'https://unrelated.example');
    await expect(canvas.getByRole('alert')).toHaveTextContent('valid Hikyo pairing invitation');
    await expect(canvas.getByRole('button', { name: 'Connect remote' })).toBeDisabled();
    await expect(canvas.getByLabelText('Import QR image')).toBeEnabled();
  },
};
