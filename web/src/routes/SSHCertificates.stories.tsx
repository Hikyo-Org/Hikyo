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
const populated = [{ url: `${base}/ssh-cas`, body: { items: [ca] } }, ...empty.slice(1)];
// One card per subsection, each holding its own action and its table, the
// table inside its own sideways scroller.
export const Populated: Story = { parameters: { app: { responses: populated } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('service-access', { exact: true })).toBeVisible();
  for (const name of ['Certificate authorities', 'Profiles', 'Certificates']) {
    const card = canvas.getByRole('heading', { name, level: 3 }).closest('.card');
    await expect(card).not.toBeNull();
    await expect(card?.querySelector('.table-scroll > table')).not.toBeNull();
  }
  // A row's actions sit in one `.actions` row, so the mobile grid reaches them.
  const rotate = canvas.getByRole('button', { name: 'Rotate' });
  await expect(rotate.parentElement).toHaveClass('actions');
} };
// At a phone width the CA row's four actions form an equal-width two-per-row
// grid, the table drops to the compact size, and only the table's own
// container scrolls sideways: the page never does.
// KNOWN APP BUG (reported, not fixed here): the shared mobile grid rule
// (`web/src/ui/Button.css` ~line 146, `.actions:has(> .btn ~ .btn) { ...
// grid-template-columns: 1fr 1fr; }`) omits `minmax(0, ...)`, so a bare `1fr`
// track's minimum is its content's width, not 0. When a row's two columns'
// content widths differ enough (here "Trust bundle"/"Rotate" vs "Download
// KRL"/"Delete") and the container is narrow, the columns render unequal
// (measured 79px vs 99px) instead of the equal split every other `1fr`
// grid-template-columns in this codebase writes as `minmax(0, 1fr)` (see
// `.matrix-editor__provenance`, `.adapters__form--two`). This story keeps the
// correct expectation and is left failing until that rule is fixed.
export const PhoneActionGrid: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  parameters: { app: { responses: populated } },
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByText('service-access', { exact: true });
    const row = canvas.getByRole('button', { name: 'Rotate' }).closest('.actions');
    await expect(row).not.toBeNull();
    await expect(getComputedStyle(row!).display).toBe('grid');
    const [a, b, c, d] = [...row!.querySelectorAll(':scope > .btn')].map((button) => button.getBoundingClientRect());
    await expect(d).toBeDefined();
    await expect(Math.round(a!.top)).toBe(Math.round(b!.top));
    await expect(Math.round(a!.width)).toBe(Math.round(b!.width));
    await expect(c!.top).toBeGreaterThanOrEqual(a!.bottom);
    await expect(Math.round(c!.top)).toBe(Math.round(d!.top));
    const table = row!.closest('table')!;
    await expect(getComputedStyle(table).fontSize).toBe(getComputedStyle(document.documentElement).getPropertyValue('--fs-xs').trim());
    await expect(getComputedStyle(table.parentElement!).overflowX).toBe('auto');
    // The story's own mounted root, not `document.documentElement` (the
    // Vitest browser-mode runner's page is wider than the story).
    await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
  },
};
// Without a session nothing that acts is offered: the primary, secondary and
// danger buttons all wear the one unavailable look, alike across variants.
export const ActionsUnavailable: Story = {
  args: { sessionId: null },
  parameters: { app: { responses: populated } },
  play: async ({ canvas }) => {
    await canvas.findByText('service-access', { exact: true });
    const held = [
      canvas.getByRole('button', { name: 'Create CA' }),
      canvas.getByRole('button', { name: 'Rotate' }),
      canvas.getByRole('button', { name: 'Delete' }),
    ];
    const live = getComputedStyle(canvas.getByRole('button', { name: 'Trust bundle' }));
    const looks = held.map((button) => getComputedStyle(button));
    for (const [index, look] of looks.entries()) {
      await expect(held[index]).toBeDisabled();
      await expect(look.cursor).toBe('not-allowed');
      await expect(look.backgroundColor).toBe(looks[0]!.backgroundColor);
      await expect(look.color).toBe(looks[0]!.color);
      await expect(look.color).not.toBe(live.color);
    }
  },
};
export const Refused: Story = { parameters: { app: { responses: empty.map(({ url }) => ({ url, status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } })) } }, play: async ({ canvas }) => { await expect(await canvas.findByText('The SSH CAs could not be listed. Listing them needs read on this environment.')).toBeVisible(); } };
export const CreateCa: Story = { play: async ({ canvas }) => {
  await canvas.findByText('No SSH CA in this environment yet. Create one to issue certificates.');
  await userEvent.click(canvas.getByRole('button', { name: 'Create CA' }));
  await expect(await canvas.findByRole('dialog', { name: 'Create SSH CA' })).toBeVisible();
  await expect(canvas.getByRole('textbox', { name: 'Name' })).toBeVisible();
  await expect(canvas.getByRole('button', { name: 'Create' })).toBeDisabled();
} };
