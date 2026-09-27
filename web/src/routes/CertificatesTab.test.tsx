// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { CertificatesTab } from './CertificatesTab.tsx';

const ceremonies = vi.hoisted(() => [] as unknown[]);
vi.mock('../api/values.ts', () => ({
  runPasskeyCeremony: (input: unknown) => {
    ceremonies.push(input);
    return Promise.resolve();
  },
}));

afterEach(() => {
  vi.unstubAllGlobals();
  ceremonies.length = 0;
});

const project = { org: 'org_acme', project: 'prj_app' };
const environments = [{ id: 'env_prod', name: 'prod' }];
const base = '/api/v1/orgs/org_acme/projects/prj_app/environments/env_prod';
const certificate = {
  id: 'cert_00000000-0000-7000-8000-000000000001',
  environment_id: 'env_prod',
  profile: 'web',
  issuer_id: 'pkii_00000000-0000-7000-8000-000000000001',
  issuer_name: 'issuing',
  issuer_version: 1,
  serial: 'abc123',
  state: 'issued',
  key_source: 'generated',
  key_algorithm: 'ecdsa-p256',
  key_fingerprint: 'sha256:x',
  dns_names: ['api.svc.example.com'],
  ip_addresses: [],
  uris: [],
  not_before: '2026-09-26T00:00:00Z',
  not_after: '2026-09-27T00:00:00Z',
  certificate_pem: '-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n',
  principal_id: 'usr_x',
  principal_class: 'human',
  created_at: '2026-09-26T00:00:00Z',
  updated_at: '2026-09-26T00:00:00Z',
};
const profile = {
  name: 'web',
  policy: {
    allowed_issuers: ['issuing'], dns_patterns: ['*.svc.example.com'], ip_ranges: [], uri_patterns: [],
    allow_wildcard_names: false, key_algorithms: ['ecdsa-p256'], key_usages: ['digital-signature'],
    ext_key_usages: ['server-auth'], max_ttl_seconds: 259200, default_ttl_seconds: 86400,
    renew_window_seconds: 28800, allow_csr: true, allow_generated_key: true, machine_issuance: false, organization: '',
  },
};

function stubServer(bodies: unknown[]) {
  vi.stubGlobal('fetch', vi.fn(async (...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path === `${base}/certificate-profiles`) {
      return Response.json({ profiles: [profile] });
    }
    if (request.method === 'GET' && path === `${base}/certificates`) {
      return Response.json({ certificates: [] });
    }
    if (request.method === 'POST' && path === `${base}/certificates`) {
      const body = (await request.json()) as Record<string, unknown>;
      bodies.push(body);
      return Response.json(body.generate_key === true
        ? { certificate, private_key_pem: '-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----\n' }
        : { certificate: { ...certificate, key_source: 'csr' } });
    }
    throw new Error(`unexpected ${request.method} ${path}`);
  }));
}

function textareaLabelled(container: HTMLElement, label: string): HTMLTextAreaElement {
  const found = [...container.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label));
  const target = found === undefined ? null : container.querySelector(`#${CSS.escape(found.htmlFor)}`);
  if (!(target instanceof HTMLTextAreaElement)) throw new Error(`no textarea ${label}`);
  return target;
}

function typeArea(area: HTMLTextAreaElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
  setter?.call(area, value);
  area.dispatchEvent(new Event('input', { bubbles: true }));
}

function button(container: ParentNode, text: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === text);
  if (found === undefined) throw new Error(`no button ${text}`);
  return found;
}

it('issues from a CSR with the names from the fields, not the CSR', async () => {
  const bodies: unknown[] = [];
  stubServer(bodies);
  const view = { rows: [], isPending: false, isError: false };
  const { container, unmount } = await renderForm(<CertificatesTab project={project} environments={environments} view={view} />);
  try {
    await act(async () => button(container, 'Issue certificate').click());
    await settleTask();
    typeArea(textareaLabelled(document.body, 'Certificate signing request'), '-----BEGIN CERTIFICATE REQUEST-----\nCCCC\n-----END CERTIFICATE REQUEST-----');
    typeArea(textareaLabelled(document.body, 'DNS names'), 'api.svc.example.com\n\n  ');
    await act(async () => button(document.body, 'Issue').click());
    await settleTask();
    expect(bodies).toEqual([{
      profile: 'web', dns_names: ['api.svc.example.com'],
      csr_pem: '-----BEGIN CERTIFICATE REQUEST-----\nCCCC\n-----END CERTIFICATE REQUEST-----',
    }]);
    expect(ceremonies).toEqual([]);
  } finally {
    await unmount();
  }
});

it('shows a generated key exactly once, after the mint ceremony, and holds Done until it is stored', async () => {
  const bodies: unknown[] = [];
  stubServer(bodies);
  const view = { rows: [], isPending: false, isError: false };
  const { container, unmount } = await renderForm(<CertificatesTab project={project} environments={environments} view={view} />);
  try {
    await act(async () => button(container, 'Issue certificate').click());
    await settleTask();
    const generate = [...document.body.querySelectorAll('input[type="radio"]')][1];
    if (!(generate instanceof HTMLInputElement)) throw new Error('no generate radio');
    await act(async () => generate.click());
    typeArea(textareaLabelled(document.body, 'DNS names'), 'worker.svc.example.com');
    await act(async () => button(document.body, 'Use a passkey and issue').click());
    await settleTask();
    expect(ceremonies).toEqual([{ operation: 'mint', environmentId: 'env_prod', keyIds: [] }]);
    expect(bodies).toEqual([{ profile: 'web', dns_names: ['worker.svc.example.com'], generate_key: true, key_algorithm: 'ecdsa-p256' }]);
    expect(textareaLabelled(document.body, 'Private key').value).toContain('SECRET');
    const done = button(document.body, 'Done');
    expect(done.disabled).toBe(true);
    const stored = document.body.querySelector('input[type="checkbox"]');
    if (!(stored instanceof HTMLInputElement)) throw new Error('no stored confirmation');
    await act(async () => stored.click());
    expect(button(document.body, 'Done').disabled).toBe(false);
  } finally {
    await unmount();
  }
});

it('counts certificates only once every listing settled', async () => {
  const { certificateCount } = await import('./CertificatesTab.tsx');
  expect(certificateCount({ rows: [], isPending: true, isError: false })).toBe('unknown');
  expect(certificateCount({ rows: [], isPending: false, isError: true })).toBe('unknown');
  expect(certificateCount({ rows: [], isPending: false, isError: false })).toBe(0);
});


it('announces certificate listing progress instead of an empty result', async () => {
  const { container, unmount } = await renderForm(<CertificatesTab project={project} environments={environments} view={{ rows: [], isPending: true, isError: false }} />);
  try {
    expect(container.querySelector('[role="status"]')?.textContent).toBe('Loading certificates…');
    expect(container.textContent).not.toContain('No certificates in this project yet.');
  } finally { await unmount(); }
});

it('blocks issuance while profiles load and after their request fails', async () => {
  let finish: ((response: Response) => void) | undefined;
  const fetchMock = vi.fn(() => new Promise<Response>((resolve) => { finish = resolve; }));
  vi.stubGlobal('fetch', fetchMock);
  const { container, unmount } = await renderForm(<CertificatesTab project={project} environments={environments} view={{ rows: [], isPending: false, isError: false }} />);
  try {
    await act(async () => button(container, 'Issue certificate').click());
    await settleTask();
    expect(document.body.textContent).toContain('Loading certificate profiles…');
    expect(button(document.body, 'Issue').disabled).toBe(true);
    await act(async () => finish?.(Response.json({ error: { code: 'forbidden', message: 'not permitted' } }, { status: 403 })));
    await settleTask();
    expect(document.body.querySelector('[role="alert"]')?.textContent).toContain('Certificate profiles could not be loaded');
    expect(button(document.body, 'Issue').disabled).toBe(true);
    await act(async () => button(document.body, 'Issue').click());
    expect(fetchMock).toHaveBeenCalledTimes(1);
  } finally { await unmount(); }
});
