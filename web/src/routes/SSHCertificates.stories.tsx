import type { Meta, StoryObj } from '@storybook/react-vite';
import { zSshca, zSshcaList, zSshProfileList, zSshCertificateList } from '@hikyo/zod';
import { expect, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { SSHCertificatesPanel } from './SSHCertificates.tsx';

const base = '/api/v1/orgs/org_acme/projects/prj_app/environments/env_prod';
const ca = zSshca.parse({ id: 'sshca_00000000-0000-7000-8000-000000000001', name: 'service-access', authority_principal_id: 'prn_00000000-0000-7000-8000-000000000001', created_at: '2026-09-01T00:00:00Z', keys: [{ id: 'sshkey_00000000-0000-7000-8000-000000000001', algorithm: 'ed25519', public_key: 'ssh-ed25519 PUBLIC-FIXTURE', fingerprint: 'SHA256:public-fixture', origin: 'generated', state: 'active', trusted: true, created_at: '2026-09-01T00:00:00Z' }] });
const empty = [
  { url: `${base}/ssh-cas`, body: zSshcaList.parse({ items: [] }) },
  { url: `${base}/ssh-profiles`, body: zSshProfileList.parse({ items: [] }) },
  { url: `${base}/ssh-certificates`, body: zSshCertificateList.parse({ items: [] }) },
];
const meta = {
  title: 'Features/SSH/SSHCertificatesPanel',
  component: SSHCertificatesPanel,
  tags: ['ai-generated'],
  args: { org: 'org_acme', project: 'prj_app', sessionId: 'ses_storybook', environments: [{ id: 'env_prod', name: 'production' }], requesterOptions: [] },
  parameters: { ...topLayerDocs, app: { responses: empty }, docs: { ...topLayerDocs.docs, description: { component: 'Environment-scoped SSH CA, profile and certificate lifecycle. CA and generated user private keys remain excluded from fixture responses. Browser passkey issuance is human-only.' } } },
} satisfies Meta<typeof SSHCertificatesPanel>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Empty: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText('No SSH CA in this environment yet. Create one to issue certificates.')).toBeVisible(); } };
export const NoEnvironments: Story = { args: { environments: [] } };
export const SessionRequired: Story = { args: { sessionId: null } };
export const Populated: Story = { parameters: { app: { responses: [{ url: `${base}/ssh-cas`, body: { items: [ca] } }, ...empty.slice(1)] } }, play: async ({ canvas }) => { await expect(await canvas.findByText('service-access', { exact: true })).toBeVisible(); } };
export const Refused: Story = { parameters: { app: { responses: empty.map(({ url }) => ({ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } })) } }, play: async ({ canvas }) => { await expect(await canvas.findByText('The SSH CAs could not be listed. Listing them needs read on this environment.')).toBeVisible(); } };
export const CreateCa: Story = { play: async ({ canvas }) => {
  await canvas.findByText('No SSH CA in this environment yet. Create one to issue certificates.');
  await userEvent.click(canvas.getByRole('button', { name: 'Create CA' }));
  await expect(await canvas.findByRole('dialog', { name: 'Create SSH CA' })).toBeVisible();
  await expect(canvas.getByRole('textbox', { name: 'Name' })).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Create' })).toBeDisabled();
} };
