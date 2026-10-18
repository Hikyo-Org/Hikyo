import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { TransitKeys } from './TransitKeys.tsx';

const org = 'org_00000000-0000-7000-8000-000000000001';
const project = 'prj_00000000-0000-7000-8000-000000000001';
const environment = 'env_00000000-0000-7000-8000-000000000001';
const base = `/api/v1/orgs/${org}/projects/${project}/environments`;
const keysPath = `${base}/${environment}/transit-keys`;
const path = `/orgs/${org}/projects/${project}/transit-keys?env=${environment}`;
const at = '2026-10-01T00:00:00Z';
const key = {
  id: 'trk_00000000-0000-7000-8000-000000000001', environment_id: environment,
  name: 'database-column', algorithm: 'xchacha20-poly1305', custody: 'software',
  allowed_operations: ['encrypt', 'decrypt', 'rewrap', 'datakey'], exportable: false,
  state: 'active', latest_version: 2, min_encrypt_version: 2, min_decrypt_version: 1,
  compromised_through_version: 0, rotation_period_seconds: 86400, rotation_due: true,
  created_by: 'prn_storybook', created_at: at, updated_at: at,
  versions: [{ version: 1, material: 'sealed', created_at: at }, { version: 2, material: 'sealed', created_at: at }], callers: [],
};
const environments = { url: base, body: { items: [{ id: environment, org_id: org, project_id: project, name: 'production', display_order: 0, created_at: at }], count: 1 } };
const populated = [environments, { url: keysPath, body: { items: [key] } }, { url: `${keysPath}/${key.name}`, body: key }];
const selected = `${path}&key=${key.name}`;
const meta = {
  title: 'Features/Transit/TransitKeys', component: TransitKeys, tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { auth: true, path, routePath: '/orgs/:org/projects/:project/transit-keys', responses: populated },
    docs: { ...topLayerDocs.docs, description: { component: 'Environment-scoped public key metadata, lifecycle management and ephemeral cryptographic operations. Managed key material is never returned. Fixture responses contain public metadata only.' } },
  },
} satisfies Meta<typeof TransitKeys>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Populated: Story = { play: async ({ canvas }) => {
  await expect(await canvas.findByRole('button', { name: 'Inspect database-column' })).toBeVisible();
  await expect(canvas.getByText('Rotation due', { exact: true })).toBeVisible();
  await userEvent.click(canvas.getByRole('button', { name: 'Inspect database-column' }));
  const detail = await canvas.findByRole('region', { name: 'Selected Transit key' });
  await expect(await within(detail).findByRole('heading', { name: 'Key versions' })).toBeVisible();
  await expect(within(detail).getByRole('region', { name: 'Cryptographic operations' })).toBeVisible();
} };
export const Empty: Story = { parameters: { app: { responses: [environments, { url: keysPath, body: { items: [] } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('No Transit keys in this environment.')).toBeVisible();
} };
export const Disabled: Story = { parameters: { app: { path: selected, responses: [environments, { url: keysPath, body: { items: [{ ...key, state: 'disabled' }] } }, { url: `${keysPath}/${key.name}`, body: { ...key, state: 'disabled' } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('This key is disabled. Cryptographic operations are refused.')).toBeVisible();
} };
export const Compromised: Story = { parameters: { app: { path: selected, responses: [environments, { url: keysPath, body: { items: [{ ...key, compromised_through_version: 2 }] } }, { url: `${keysPath}/${key.name}`, body: { ...key, compromised_through_version: 2 } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText(/Versions through 2 are compromised/)).toBeVisible();
} };
export const PendingDeletion: Story = { parameters: { app: { path: selected, responses: [environments, { url: keysPath, body: { items: [{ ...key, state: 'pending-deletion', deletion_after: '2099-10-01T00:00:00Z' }] } }, { url: `${keysPath}/${key.name}`, body: { ...key, state: 'pending-deletion', deletion_after: '2099-10-01T00:00:00Z' } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('This key is pending-deletion. Cryptographic operations are refused.')).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Cancel deletion' })).toBeVisible();
} };
export const Phone: Story = { globals: { viewport: { value: 'phone', isRotated: false } }, parameters: { app: { path: selected } }, play: async ({ canvas, canvasElement }) => {
  await expect(await canvas.findByRole('heading', { name: 'Key versions' })).toBeVisible();
  await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
} };
export const CreateKey: Story = { play: async ({ canvas }) => {
  await canvas.findByRole('button', { name: 'Inspect database-column' });
  await userEvent.click(canvas.getByRole('button', { name: 'Create key' }));
  await expect(await canvas.findByRole('dialog', { name: 'Create transit key' })).toBeVisible();
  await expect(canvas.getByRole('group', { name: 'Allowed operations' })).toBeVisible();
} };
export const ReadRefused: Story = { parameters: { app: { responses: [environments, { url: keysPath, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('Transit access was refused. Check your environment permissions, caller grant and second factor.')).toBeVisible();
} };
