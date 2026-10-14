import type { Meta, StoryObj } from '@storybook/react-vite';
import { zCertificate } from '@hikyo/zod';
import { expect, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { profiles } from '../testkit/storybookPki.ts';
import { CertificatesTab } from './CertificatesTab.tsx';

const certificate = zCertificate.parse({
  id: 'cert_00000000-0000-7000-8000-000000000001', environment_id: 'env_prod', profile: 'web-services',
  issuer_id: 'pkii_00000000-0000-7000-8000-000000000001', issuer_name: 'issuing', issuer_version: 1,
  serial: 'public-fixture-serial', state: 'issued', key_source: 'csr', key_algorithm: 'ecdsa-p256', key_fingerprint: 'sha256:public-fixture',
  dns_names: ['api.svc.example.com', 'long-service-name-for-observability.svc.example.com'], ip_addresses: [], uris: [],
  not_before: '2026-09-01T00:00:00Z', not_after: '2099-09-01T00:00:00Z', certificate_pem: 'PUBLIC CERTIFICATE FIXTURE',
  principal_id: 'prn_alex', principal_class: 'human', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
});
const meta = {
  title: 'Features/PKI/CertificatesTab',
  component: CertificatesTab,
  tags: ['ai-generated'],
  args: {
    project: { org: 'org_acme', project: 'prj_app' }, environments: [{ id: 'env_prod', name: 'production' }],
    view: { rows: [{ environmentId: 'env_prod', environmentName: 'production', certificate }], isPending: false, isError: false },
  },
  parameters: { ...topLayerDocs, app: { responses: [{ url: '/api/v1/orgs/org_acme/projects/prj_app/environments/env_prod/certificate-profiles', body: { profiles: profiles.profiles.map(({ name, policy }) => ({ name, policy })) } }] }, docs: { ...topLayerDocs.docs, description: { component: 'Certificate metadata and public material across project environments. Generated private-key issuance requires a human-owned passkey prompt and is deliberately excluded.' } } },
} satisfies Meta<typeof CertificatesTab>;
export default meta;
type Story = StoryObj<typeof meta>;
// The tab's module is one card: its action above a table that scrolls
// sideways inside its own container.
export const Populated: Story = { play: async ({ canvas }) => {
  const card = canvas.getByRole('heading', { name: 'Certificates', level: 2 }).closest('.card');
  await expect(card).not.toBeNull();
  await expect(card?.querySelector('.table-scroll > table')).not.toBeNull();
  await expect(card).toContainElement(canvas.getByRole('button', { name: 'Issue certificate' }));
} };
// At a phone width the table drops to the compact type size and tighter
// cell padding; the long name list scrolls inside the table's container
// while the page itself never scrolls sideways.
export const PhoneCompactTable: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const table = canvas.getByRole('table');
    const root = getComputedStyle(document.documentElement);
    await expect(getComputedStyle(table).fontSize).toBe(root.getPropertyValue('--fs-xs').trim());
    await expect(getComputedStyle(table.querySelector('td')!).paddingTop).toBe(root.getPropertyValue('--space-1').trim());
    await expect(getComputedStyle(table.parentElement!).overflowX).toBe('auto');
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};
export const Empty: Story = { args: { view: { rows: [], isPending: false, isError: false } } };
export const Loading: Story = { args: { view: { rows: [], isPending: true, isError: false } } };
export const Incomplete: Story = { args: { view: { rows: [], isPending: false, isError: true } } };
export const IssueFromCsr: Story = { play: async ({ canvas }) => {
  await userEvent.click(canvas.getByRole('button', { name: 'Issue certificate' }));
  await expect(await canvas.findByRole('dialog', { name: 'Issue certificate' })).toBeVisible();
  await expect(await canvas.findByRole('textbox', { name: 'Certificate signing request (PEM)' })).toBeVisible();
} };
export const RevokeConfirmation: Story = { play: async ({ canvas }) => {
  await userEvent.click(canvas.getByRole('button', { name: 'Revoke' }));
  await expect(await canvas.findByRole('dialog', { name: 'Revoke certificate' })).toBeVisible();
  await expect(canvas.getByRole('combobox', { name: 'Reason' })).toBeVisible();
} };
