// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { SamlProvidersPanel } from './SamlProvidersPanel.tsx';
import { SamlSpKeysPanel } from './SamlSpKeysPanel.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function pathOf(input: Parameters<typeof fetch>[0]): { method: string; path: string; request: Request } {
  const request = input instanceof Request ? input : new Request(input);
  return { method: request.method, path: new URL(request.url, 'http://localhost').pathname, request };
}

function setNativeValue(element: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const proto = element instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
  if (setter === undefined) throw new Error('no value setter');
  setter.call(element, value);
  element.dispatchEvent(new Event('input', { bubbles: true }));
}

function button(container: HTMLElement, label: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === label);
  if (found === undefined) throw new Error(`button "${label}" is missing`);
  return found;
}

const provider = {
  slug: 'acme',
  display_name: 'Acme',
  kind: 'saml',
  entity_id: 'https://idp.example/acme',
  acs_url: 'https://sp.example/acs',
  sso_redirect_url: 'https://idp.example/acme/sso',
  signing_certificate_fingerprints: ['sha256:AAA'],
  assurance_policy: null,
  allow_email_nameid: false,
  force_sign_requests: false,
  metadata_source: 'file',
  metadata_url: null,
  metadata_signed: false,
  metadata_signing_fingerprint: null,
  metadata_valid_until: null,
  warnings: [],
  enabled: true,
  row_version: 1,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
} as const;

describe('SamlProvidersPanel metadata ceremony', () => {
  it('previews the trust diff, then applies with the confirmed fingerprints and clears the document', async () => {
    const bodies: string[] = [];
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path, request } = pathOf(args[0]);
      if (method === 'GET' && path === '/api/v1/instance/saml-providers') {
        return Promise.resolve(json({ providers: [] }));
      }
      if (method === 'PUT' && path === '/api/v1/instance/saml-providers/acme') {
        return request.text().then((text) => {
          bodies.push(text);
          const parsed: unknown = JSON.parse(text);
          const confirmed =
            typeof parsed === 'object' && parsed !== null && 'confirmed_fingerprints' in parsed;
          if (!confirmed) {
            return json({
              applied: false,
              provider: null,
              diff: { endpoints_added: ['https://idp.example/acme/sso'], endpoints_removed: [], certs_added_fps: ['sha256:AAA'], certs_removed_fps: [] },
              required_fingerprints: ['sha256:AAA'],
              required_endpoints: ['https://idp.example/acme/sso'],
            });
          }
          return json({
            applied: true,
            provider,
            diff: { endpoints_added: [], endpoints_removed: [], certs_added_fps: [], certs_removed_fps: [] },
            required_fingerprints: [],
            required_endpoints: [],
          });
        });
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    // The CSRF token the client echoes on mutations.
    document.cookie = '__Host-hikyo-csrf=token';

    const { container, client, unmount } = await renderForm(<SamlProvidersPanel />);
    await settleTask();

    await act(async () => button(container, '+ configure SAML provider').click());
    const inputs = container.querySelectorAll<HTMLInputElement>('.saml-editor input');
    const slug = inputs[0];
    const name = inputs[1];
    const entity = inputs[2];
    const textarea = container.querySelector<HTMLTextAreaElement>('.saml-editor textarea');
    if (slug === undefined || name === undefined || entity === undefined || textarea === null) {
      throw new Error('provider create fields missing');
    }
    await act(async () => setNativeValue(slug, 'acme'));
    await act(async () => setNativeValue(name, 'Acme'));
    await act(async () => setNativeValue(entity, 'https://idp.example/acme'));
    await act(async () => setNativeValue(textarea, '<md:EntityDescriptor/>'));

    await act(async () => button(container, 'Preview and configure').click());
    await settleTask();
    // The diff is shown and nothing is applied yet.
    expect(container.textContent).toContain('changes trust state');
    expect(container.textContent).toContain('sha256:AAA');

    await act(async () => button(container, 'Confirm trust and configure provider').click());
    await settleTask();

    // The second request carried the confirmed material copied from required_*.
    const secondBody = bodies[1];
    if (secondBody === undefined) throw new Error('confirm request was never sent');
    const confirmBody: unknown = JSON.parse(secondBody);
    expect(confirmBody).toMatchObject({
      confirmed_fingerprints: ['sha256:AAA'],
      confirmed_endpoints: ['https://idp.example/acme/sso'],
    });
    expect(container.textContent).toContain('Configured SAML provider acme');
    // The write-only metadata never survives a successful apply in the DOM…
    expect(container.querySelector('.saml-editor')).toBeNull();
    // …nor in React Query's mutation cache: the ceremony resets the mutation so
    // the document is not recoverable from the client afterwards.
    const leaked = client
      .getMutationCache()
      .getAll()
      .some((mutation) => JSON.stringify(mutation.state.variables ?? {}).includes('<md:EntityDescriptor/>'));
    expect(leaked).toBe(false);
    await unmount();
  });

  it('confirms the exact previewed document even if the form is edited while the preview is in flight', async () => {
    const bodies: string[] = [];
    let releasePreview: (() => void) | undefined;
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path, request } = pathOf(args[0]);
      if (method === 'GET' && path === '/api/v1/instance/saml-providers') {
        return Promise.resolve(json({ providers: [] }));
      }
      if (method === 'PUT' && path === '/api/v1/instance/saml-providers/acme') {
        return request.text().then((text) => {
          bodies.push(text);
          const parsedBody: unknown = JSON.parse(text);
          const confirmed =
            typeof parsedBody === 'object' && parsedBody !== null && 'confirmed_fingerprints' in parsedBody;
          if (confirmed) {
            return json({ applied: true, provider, diff: { endpoints_added: [], endpoints_removed: [], certs_added_fps: [], certs_removed_fps: [] }, required_fingerprints: [], required_endpoints: [] });
          }
          // Hold the preview response so the form can be edited before it lands.
          return new Promise<Response>((resolve) => {
            releasePreview = () =>
              resolve(
                json({
                  applied: false,
                  provider: null,
                  diff: { endpoints_added: [], endpoints_removed: [], certs_added_fps: ['sha256:AAA'], certs_removed_fps: [] },
                  required_fingerprints: ['sha256:AAA'],
                  required_endpoints: [],
                }),
              );
          });
        });
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    document.cookie = '__Host-hikyo-csrf=token';

    const { container, unmount } = await renderForm(<SamlProvidersPanel />);
    await settleTask();
    await act(async () => button(container, '+ configure SAML provider').click());
    const inputs = container.querySelectorAll<HTMLInputElement>('.saml-editor input');
    const slug = inputs[0];
    const name = inputs[1];
    const entity = inputs[2];
    const textarea = container.querySelector<HTMLTextAreaElement>('.saml-editor textarea');
    if (slug === undefined || name === undefined || entity === undefined || textarea === null) {
      throw new Error('provider create fields missing');
    }
    await act(async () => setNativeValue(slug, 'acme'));
    await act(async () => setNativeValue(name, 'Acme'));
    await act(async () => setNativeValue(entity, 'https://idp.example/acme'));
    await act(async () => setNativeValue(textarea, 'DOCUMENT_A'));
    await act(async () => button(container, 'Preview and configure').click());

    // Cancel is disabled while the request is in flight, so a still-pending
    // mutation can never be reset (and left to settle) with the document cached.
    expect(button(container, 'Cancel').disabled).toBe(true);

    // Edit to a different document while the preview response is still pending.
    const liveTextarea = container.querySelector<HTMLTextAreaElement>('.saml-editor textarea');
    if (liveTextarea === null) throw new Error('textarea vanished mid-preview');
    await act(async () => setNativeValue(liveTextarea, 'DOCUMENT_B'));

    // Now let the preview land; the pending diff is restored despite the edit.
    await act(async () => {
      releasePreview?.();
      await Promise.resolve();
    });
    await settleTask();
    await act(async () => button(container, 'Confirm trust and configure provider').click());
    await settleTask();

    // The confirm request must carry the previewed document A, never the edit B.
    const confirmBody = bodies.find((body) => body.includes('confirmed_fingerprints'));
    if (confirmBody === undefined) throw new Error('confirm request was never sent');
    expect(confirmBody).toContain('DOCUMENT_A');
    expect(confirmBody).not.toContain('DOCUMENT_B');
    await unmount();
  });
});

describe('SamlSpKeysPanel retirement gating', () => {
  const activeKey = { fingerprint: 'sha256:ACTIVE', state: 'active', created_at: '2026-09-01T00:00:00Z' } as const;
  const retiringKey = { fingerprint: 'sha256:RETIRING', state: 'retiring', created_at: '2026-08-01T00:00:00Z' } as const;

  it('offers compromise-retire only for the active key and ordinary retire only for the retiring key', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path } = pathOf(args[0]);
      if (method === 'GET' && path === '/api/v1/instance/saml-sp-keys') {
        return Promise.resolve(json({ keys: [activeKey, retiringKey] }));
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container, unmount } = await renderForm(<SamlSpKeysPanel />);
    await settleTask();

    const activeRow = container.querySelector<HTMLElement>('[data-sp-key="sha256:ACTIVE"]');
    const retiringRow = container.querySelector<HTMLElement>('[data-sp-key="sha256:RETIRING"]');
    if (activeRow === null || retiringRow === null) throw new Error('key rows missing');

    expect(button(activeRow, 'Compromise-retire')).toBeTruthy();
    expect([...activeRow.querySelectorAll('button')].some((b) => b.textContent === 'Retire')).toBe(false);
    expect(button(retiringRow, 'Retire')).toBeTruthy();
    expect([...retiringRow.querySelectorAll('button')].some((b) => b.textContent === 'Compromise-retire')).toBe(false);

    await unmount();
  });

  it('surfaces the active-key conflict when the server refuses an ordinary retire', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path } = pathOf(args[0]);
      if (method === 'GET' && path === '/api/v1/instance/saml-sp-keys') {
        return Promise.resolve(json({ keys: [retiringKey] }));
      }
      if (method === 'DELETE' && path === '/api/v1/instance/saml-sp-keys/sha256:RETIRING') {
        return Promise.resolve(json({ code: 'conflict' }, 409));
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    document.cookie = '__Host-hikyo-csrf=token';

    const { container, unmount } = await renderForm(<SamlSpKeysPanel />);
    await settleTask();

    await act(async () => button(container, 'Retire').click());
    const confirmInput = container.querySelector<HTMLInputElement>('.danger-zone input');
    if (confirmInput === null) throw new Error('retire confirm input missing');
    await act(async () => setNativeValue(confirmInput, 'sha256:RETIRING'));
    await act(async () => button(container, 'Retire key').click());
    await settleTask();

    expect(container.textContent).toContain('active signing key');
    await unmount();
  });
});
