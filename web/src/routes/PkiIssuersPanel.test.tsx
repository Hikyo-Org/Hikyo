// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { PkiIssuersPanel } from './PkiIssuersPanel.tsx';

afterEach(() => vi.unstubAllGlobals());

const issuer = (over: Record<string, unknown>) => ({
  id: 'pkii_00000000-0000-7000-8000-000000000001',
  name: 'issuing',
  version: 1,
  kind: 'intermediate',
  origin: 'generated',
  state: 'active',
  key_algorithm: 'ecdsa-p256',
  key_fingerprint: 'sha256:abc',
  certificate_pem: '-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n',
  subject_cn: 'Issuing CA',
  restore_hold: false,
  issued_count: 3,
  crl_number: 0,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...over,
});

it('lists issuer versions with public material only, shows a pending CSR, and rotates the newest version', async () => {
  const posted: string[] = [];
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path === '/api/v1/instance/pki/issuers') {
      return Promise.resolve(Response.json({ issuers: [
        issuer({}),
        issuer({
          id: 'pkii_00000000-0000-7000-8000-000000000002', name: 'offline', state: 'pending',
          certificate_pem: undefined, csr_pem: '-----BEGIN CERTIFICATE REQUEST-----\nBBBB\n-----END CERTIFICATE REQUEST-----\n',
          restore_hold: true,
        }),
      ] }));
    }
    if (request.method === 'POST' && path === '/api/v1/instance/pki/issuers/issuing/rotate') {
      posted.push(path);
      return Promise.resolve(Response.json(issuer({ id: 'pkii_00000000-0000-7000-8000-000000000003', version: 2 })));
    }
    throw new Error(`unexpected ${request.method} ${path}`);
  }));
  const { container, unmount } = await renderForm(<PkiIssuersPanel />);
  try {
    await settleTask();
    const rows = container.querySelectorAll('[data-pki-issuer]');
    expect([...rows].map((row) => row.getAttribute('data-pki-issuer'))).toEqual(['issuing v1', 'offline v1']);
    expect(container.textContent).toContain('CERTIFICATE REQUEST');
    expect(container.textContent).toContain('held');
    // A CA private key never reaches the page: there is no field for it.
    expect(container.innerHTML).not.toMatch(/PRIVATE KEY/);

    const rotate = [...rows[0]!.querySelectorAll('button')].find((b) => b.textContent === 'Rotate');
    expect(rotate).toBeDefined();
    await act(async () => {
      rotate!.click();
    });
    await settleTask();
    expect(posted).toEqual(['/api/v1/instance/pki/issuers/issuing/rotate']);
    expect(container.textContent).toContain('v2 is active');
  } finally {
    await unmount();
  }
});

it('offers rotation only for the numerically newest string version', async () => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json({ issuers: [
    issuer({ version: '2', state: 'retiring' }),
    issuer({ id: 'pkii_00000000-0000-7000-8000-000000000010', version: '10' }),
  ] }))));
  const { container, unmount } = await renderForm(<PkiIssuersPanel />);
  try {
    await settleTask();
    const rows = container.querySelectorAll('[data-pki-issuer]');
    expect([...rows[0]!.querySelectorAll('button')].some((button) => button.textContent === 'Rotate')).toBe(false);
    expect([...rows[1]!.querySelectorAll('button')].some((button) => button.textContent === 'Rotate')).toBe(true);
  } finally {
    await unmount();
  }
});
