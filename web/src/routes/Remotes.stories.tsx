import type { Meta, StoryObj } from '@storybook/react-vite';
import { zDirectoryListing, zInstanceConnectionList, zMeta, zRemoteList, zWorkspaceOriginList } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { AppRoutes } from '../app/App.tsx';
import { DemoSurface } from '../demo/DemoSurface.tsx';
import { createPairingFixture, pairingIdentity } from './RemotePairing.story-fixtures.ts';
import { Remotes } from './Remotes.tsx';

const url = '/api/v1/instance/remotes';
const directory = { identity: 'storybook-local-instance', version: '1.4.0', orgs: [{ name: 'Platform team with a long organisation name', projects: ['api', 'web', 'observability'] }], org_count: 1, project_count: 3 } satisfies z.input<typeof zDirectoryListing>;
const remote = { id: 'rem_00000000-0000-7000-8000-000000000001', name: 'Production service instance with a long name', url: 'https://peer.example', spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=', created_at: '2026-09-01T00:00:00Z', created_by: 'prn_operator', state: 'unreachable', last_attempt_at: '2026-09-01T00:00:00Z', stale: true, stale_for_seconds: 7200 } satisfies z.input<typeof zRemoteList>['items'][number];
const responses = [
  { url: '/api/v1/instance/pairing-invites', body: { items: [], count: 0, origin: 'https://remote.example' } },
  { url: '/api/v1/instance/directory', body: directory },
  { url: '/api/v1/meta', body: { server_version: '1.4.0', api_revision: 1, protocol_capabilities: [], instance_identity: 'storybook-local-instance' } satisfies z.input<typeof zMeta> },
  { url: '/api/v1/instance/connections', body: zInstanceConnectionList.parse({ items: [], count: 0 }) },
  { url: '/api/v1/instance/workspace-origins', body: zWorkspaceOriginList.parse({ items: [], count: 0 }) },
];
const meta = {
  title: 'Pages/Remotes',
  component: Remotes,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url, body: { items: [], count: 0 } satisfies z.input<typeof zRemoteList> }, ...responses] }, docs: { ...topLayerDocs.docs, description: { component: 'Real Remotes screen with local directory transport fixtures. Opening a remote workspace launches a human-owned popup and is deliberately excluded; the separate RemoteCard catalogue retains its handoff states.' } } },
} satisfies Meta<typeof Remotes>;
export default meta;
type Story = StoryObj<typeof meta>;

const pairingFixture = createPairingFixture();

/** Manual entry point: the product route, navigation and PanelForms are real. */
export const InteractivePairing: Story = {
  render: () => <AppRoutes />,
  beforeEach: async () => {
    pairingFixture.reset();
    await import('../app/route-groups/workspace.ts');
    return () => pairingFixture.reset();
  },
  parameters: {
    app: { auth: true, identity: pairingIdentity, path: '/remotes', routeTree: true, responses: pairingFixture.responses },
    docs: { description: { story: 'Interactive Remotes with sample data. Open Pair a remote, choose your own secret or generated word count, and import opaque-invitation-fixture to add a sample directory card. To explore administrator approval, open Create pairing invitation with any sample phrase, a label, https://remote.example and fingerprint AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=. Creation displays the same sample invitation and QR. All transport storage resets when the story remounts. No keys, PAKE proof, real remote connection or workspace authentication is performed. The separate human workspace handoff is explicitly refused. Dark and light appearance follow the Storybook toolbar.' } },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Remotes', level: 1 })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Pair a remote' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Create pairing invitation' })).toBeVisible();
  },
};

export const Empty: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText(/No remotes yet/)).toBeVisible(); await expect(canvas.getByRole('heading', { name: 'Connection credentials' })).toBeVisible(); } };
export const LongContent: Story = { parameters: { app: { responses: [{ url, body: { items: [remote], count: 1 } satisfies z.input<typeof zRemoteList> }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('heading', { name: remote.name })).toBeVisible(); } };
export const LoadingDirectory: Story = { parameters: { app: { responses: [{ url, pending: true }, { url: '/api/v1/instance/directory', pending: true }, ...responses.slice(1)] } }, play: async ({ canvas }) => { await waitFor(() => expect(canvas.getByText("Loading this instance's directory…")).toBeVisible()); } };
export const PermissionRequired: Story = { parameters: { app: { responses: [{ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent('may not hold'); } };

// Every input on the page opens from a button into the side panel.
export const AddRemotePanel: Story = {
  play: async ({ canvas }) => {
    const open = await canvas.findByRole('button', { name: 'Add a remote' });
    await expect(canvas.queryByRole('textbox', { name: 'URL' })).toBeNull();
    await userEvent.click(open);
    const panel = canvas.getByRole('complementary', { name: 'Add a remote' });
    await expect(within(panel).getByRole('form', { name: 'Add a remote' })).toBeVisible();
    await expect(within(panel).getByRole('textbox', { name: 'URL' })).toBeVisible();
  },
};

// Rename is a form in the side panel (it was a browser prompt), prefilled with
// the current name; Save stays off until the name changes.
export const RenamePanel: Story = {
  parameters: LongContent.parameters,
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Rename' }));
    const panel = canvas.getByRole('complementary', { name: 'Rename remote' });
    const field = within(panel).getByRole('textbox', { name: 'Display name' });
    await expect(field).toHaveValue(remote.name);
    await expect(within(panel).getByRole('button', { name: 'Save name' })).toBeDisabled();
    await userEvent.clear(field);
    await userEvent.type(field, 'Production');
    await expect(within(panel).getByRole('button', { name: 'Save name' })).toBeEnabled();
  },
};

export const AllowOriginPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Allow an origin' }));
    const panel = canvas.getByRole('complementary', { name: 'Allow an origin' });
    await expect(within(panel).getByRole('textbox', { name: 'Origin' })).toBeVisible();
    await expect(within(panel).getByRole('button', { name: 'Allow origin' })).toBeVisible();
  },
};

// The public demo mounts this page read-only via `ReadOnlyContext` ('actions'
// mode, owner decision M7, 2026-10-13), not a disabling `<fieldset>`: every
// opener into a side panel (Button reads the context itself) stays disabled.
export const DemoReadOnly: Story = {
  parameters: LongContent.parameters,
  decorators: [(Story) => <DemoSurface id="remotes"><Story /></DemoSurface>],
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText(/^Read-only sample data\./)).toBeVisible();
    await expect(canvasElement.querySelector('fieldset[disabled]')).toBeNull();
    await expect(await canvas.findByRole('button', { name: 'Add a remote' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Rename' })).toBeDisabled();
  },
};

export const MintPanel: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Mint a credential' }));
    const panel = canvas.getByRole('complementary', { name: 'Mint a credential' });
    await expect(within(panel).getByRole('textbox', { name: 'Label' })).toBeVisible();
    await expect(within(panel).getByRole('radio', { name: 'Instance default' })).toBeChecked();
  },
};
