// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
import { renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { pairingSample } from './RemotePairing.story-fixtures.ts';
import { CreatePairingInviteForm, PairRemoteForm } from './RemotePairing.tsx';

const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
const prepared = {
  phrase: 'abacus abacus abacus abacus abacus abacus',
  requester_state: 'opaque-local-continuation-fixture',
  expires_at: new Date(Date.now() + 3_600_000).toISOString(),
};
const invite = {
  id: 'invite-fixture', label: 'Approved peer', origin: 'https://remote.example',
  spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
  created_at: '2026-10-20T11:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(),
  credential_expires_at: new Date(Date.now() + 2_592_000_000).toISOString(),
};
function pairingConfiguration() {
  return Response.json({ items: [], count: 0, origin: invite.origin });
}
function input(container: HTMLElement, id: string) {
  const element = container.querySelector(`#${id}`);
  if (!(element instanceof HTMLInputElement)) throw new Error(`Missing input ${id}`);
  return element;
}
async function click(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find((node) => node.textContent === label);
  if (button === undefined) throw new Error(`Missing button ${label}`);
  await act(async () => button.click());
  await settleTask();
}
async function submit(container: HTMLElement) {
  const form = container.querySelector('form');
  if (form === null) throw new Error('Missing form');
  await act(async () => { form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
  await settleTask();
}

it('prepares locally, preserves hidden continuation across blur, and never caches sensitive data', async () => {
  const calls: Request[] = [];
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    calls.push(request);
    return Response.json(prepared, { status: 201 });
  }));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase');
  expect(calls.map((request) => new URL(request.url).pathname)).toEqual(['/api/v1/instance/pairing-requests']);
  expect(await calls[0]?.text()).toBe('');
  expect(view.container.textContent).toContain(prepared.phrase);
  expect(view.container.innerHTML).not.toContain(prepared.requester_state);
  await act(async () => window.dispatchEvent(new Event('blur')));
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.textContent).toContain('Your local continuation is still held');
  await click(view.container, 'Reveal pairing phrase');
  expect(view.container.textContent).toContain(prepared.phrase);
  expect(view.client.getMutationCache().getAll()).toEqual([]);
  expect(view.client.getQueryCache().getAll()).toEqual([]);
  await act(async () => retireSensitiveOperations(view.client));
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.querySelector('#pair-remote-invitation')).toBeNull();
});

it.each([{ label: 'four characters', secret: 'Ab4!' }, { label: 'five characters with spaces', secret: ' A4b ' }])('prepares an exact custom secret without a strength minimum: $label', async ({ secret }) => {
  let delivered = '';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    delivered = await request.text();
    return Response.json({ ...prepared, phrase: secret }, { status: 201 });
  }));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await act(async () => input(view.container, 'pairing-mode-custom').click());
  await act(async () => typeInto(input(view.container, 'pairing-custom-secret'), secret));
  expect(input(view.container, 'pairing-custom-secret').type).toBe('password');
  await click(view.container, 'Prepare with my password or phrase');
  expect(delivered).toBe(JSON.stringify({ phrase: secret }));
  expect(view.container.querySelector('#pairing-custom-secret')).toBeNull();
  expect(view.container.querySelector('[aria-label="Pairing phrase"]')?.textContent).toBe(secret);
  expect(view.client.getMutationCache().getAll()).toEqual([]);
  await act(async () => retireSensitiveOperations(view.client));
  expect(input(view.container, 'pairing-custom-secret').value).toBe('');
  expect(view.container.querySelector('[aria-label="Pairing phrase"]')).toBeNull();
});

it('clears custom input on mode changes and at dispatch before a pending preparation', async () => {
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await act(async () => input(view.container, 'pairing-mode-custom').click());
  await act(async () => typeInto(input(view.container, 'pairing-custom-secret'), 'Ab4!'));
  await act(async () => input(view.container, 'pairing-mode-generated').click());
  await act(async () => input(view.container, 'pairing-mode-custom').click());
  expect(input(view.container, 'pairing-custom-secret').value).toBe('');
  await act(async () => typeInto(input(view.container, 'pairing-custom-secret'), ' A4b '));
  await click(view.container, 'Prepare with my password or phrase');
  expect(input(view.container, 'pairing-custom-secret').value).toBe('');
  await act(async () => retireSensitiveOperations(view.client));
  expect(input(view.container, 'pairing-custom-secret').value).toBe('');
});

it.each([1, 128])('sends the selected generated word count %i without a custom secret', async (wordCount) => {
  let delivered = '';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    delivered = await request.text();
    return Response.json(prepared, { status: 201 });
  }));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await act(async () => typeInto(input(view.container, 'pairing-word-count'), String(wordCount)));
  await click(view.container, 'Generate pairing phrase');
  expect(delivered).toBe(JSON.stringify({ word_count: wordCount }));
});

it('administrator submits custom case and surrounding spaces exactly as received', async () => {
  let delivered = '';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    if (request.method === 'GET') return pairingConfiguration();
    delivered = await request.text();
    return Response.json({ invite, invite_bundle: pairingSample.bundle }, { status: 201 });
  }));
  const view = await renderForm(<CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  await settleTask();
  await act(async () => {
    typeInto(input(view.container, 'invite-pairing-phrase'), ' A4b ');
    typeInto(input(view.container, 'invite-pairing-label'), invite.label);
  });
  await submit(view.container);
  expect(delivered).toContain('"phrase":" A4b "');
});

it.each(['requester', 'administrator'])('refuses multiline paste before native password input sanitation: %s', async (owner) => {
  if (owner === 'administrator') vi.stubGlobal('fetch', vi.fn(async () => pairingConfiguration()));
  const view = await renderForm(owner === 'requester' ? <PairRemoteForm /> : <CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  if (owner === 'requester') await act(async () => input(view.container, 'pairing-mode-custom').click());
  const field = input(view.container, owner === 'requester' ? 'pairing-custom-secret' : 'invite-pairing-phrase');
  await act(async () => typeInto(field, 'Keep'));
  const paste = new Event('paste', { bubbles: true, cancelable: true });
  Object.defineProperty(paste, 'clipboardData', { value: { getData: () => 'ab\r\ncd' } });
  await act(async () => field.dispatchEvent(paste));
  expect(paste.defaultPrevented).toBe(true);
  expect(field.value).toBe('Keep');
  expect(view.container.textContent).toContain('This field accepts a single line');
  expect(view.container.textContent).not.toContain('ab\r\ncd');
});

it('imports the invitation with only locally-held continuation and clears pasted input before response', async () => {
  let delivered = '';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/pairing-requests')) return Response.json(prepared, { status: 201 });
    delivered = await request.text();
    return Response.json({ error: { code: 'conflict', message: 'Refused' } }, { status: 409 });
  }));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase');
  await act(async () => {
    typeInto(input(view.container, 'pair-remote-name'), 'Remote');
    typeInto(input(view.container, 'pair-remote-invitation'), pairingSample.bundle);
  });
  expect(delivered).toBe('');
  expect(view.container.querySelector('[aria-label="Invitation destination review"]')?.textContent).toContain(pairingSample.origin);
  await submit(view.container);
  expect(delivered).toBe(JSON.stringify({ name: 'Remote', requester_state: prepared.requester_state, invite_bundle: pairingSample.bundle }));
  expect(input(view.container, 'pair-remote-invitation').value).toBe('');
  expect(view.container.textContent).toContain('Pairing was refused');
  expect(view.client.getMutationCache().getAll()).toEqual([]);
});

it('does not reveal a preparation that arrives after focus loss', async () => {
  let resolveResponse: ((response: Response) => void) | undefined;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { resolveResponse = resolve; })));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase');
  await act(async () => {
    window.dispatchEvent(new Event('blur'));
    resolveResponse?.(Response.json(prepared, { status: 201 }));
  });
  await settleTask();
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.textContent).toContain('Your local continuation is still held');
});

it('retires the held continuation and import form at its absolute expiry', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('fetch', vi.fn(async () => Response.json({ ...prepared, expires_at: new Date(Date.now() + 60_000).toISOString() }, { status: 201 })));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  const generate = view.container.querySelector('button');
  if (generate === null) throw new Error('Missing generator');
  await act(async () => generate.click());
  await settle();
  expect(view.container.textContent).toContain(prepared.phrase);
  await act(async () => vi.advanceTimersByTimeAsync(60_001));
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.querySelector('#pair-remote-invitation')).toBeNull();
  expect(view.container.textContent).toContain('local continuation expired');
});

it('rejects a pending preparation after session retirement', async () => {
  let resolveResponse: ((response: Response) => void) | undefined;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { resolveResponse = resolve; })));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase');
  await act(async () => {
    retireSensitiveOperations(view.client);
    resolveResponse?.(Response.json(prepared, { status: 201 }));
  });
  await settleTask();
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.querySelector('#pair-remote-invitation')).toBeNull();
});

it('expiry invalidates an in-flight pairing result on the still-mounted surface', async () => {
  vi.useFakeTimers();
  let resolveResponse: ((response: Response) => void) | undefined;
  const onClose = vi.fn();
  vi.stubGlobal('fetch', vi.fn((request: Request) => {
    if (new URL(request.url).pathname.endsWith('/pairing-requests')) {
      return Promise.resolve(Response.json({ ...prepared, expires_at: new Date(Date.now() + 60_000).toISOString() }, { status: 201 }));
    }
    return new Promise<Response>((resolve) => { resolveResponse = resolve; });
  }));
  const view = await renderForm(<PairRemoteForm onClose={onClose} />);
  cleanups.push(view.unmount);
  const generate = view.container.querySelector('button');
  if (generate === null) throw new Error('Missing generator');
  await act(async () => generate.click());
  await settle();
  await act(async () => {
    typeInto(input(view.container, 'pair-remote-name'), 'Remote');
    typeInto(input(view.container, 'pair-remote-invitation'), pairingSample.bundle);
  });
  const form = view.container.querySelector('form');
  if (form === null) throw new Error('Missing import form');
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await settle();
  await act(async () => vi.advanceTimersByTimeAsync(60_001));
  await act(async () => resolveResponse?.(Response.json({
    id: 'remote-fixture', name: 'Remote', url: invite.origin, spki_pin: invite.spki_pin,
    created_at: invite.created_at, created_by: 'principal-fixture', state: 'unreachable',
    stale: true,
  }, { status: 201 })));
  await settle();
  expect(onClose).not.toHaveBeenCalled();
  expect(view.container.textContent).toContain('local continuation expired');
  expect(view.container.textContent).not.toContain('Remote paired.');
});

it('administrator creates an invitation from the phrase without caching either value', async () => {
  let delivered = '';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    if (request.method === 'GET') return pairingConfiguration();
    delivered = await request.text();
    return Response.json({ invite, invite_bundle: pairingSample.bundle }, { status: 201 });
  }));
  const view = await renderForm(<CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  await settleTask();
  await act(async () => {
    typeInto(input(view.container, 'invite-pairing-phrase'), prepared.phrase);
    typeInto(input(view.container, 'invite-pairing-label'), invite.label);
  });
  await submit(view.container);
  expect(delivered).toContain('"phrase":"abacus abacus abacus abacus abacus abacus"');
  expect(delivered).toBe(JSON.stringify({ phrase: prepared.phrase, label: invite.label, invite_lifetime_seconds: 600, credential_lifetime_seconds: 2_592_000 }));
  expect(view.container.textContent).not.toContain(prepared.phrase);
  expect(view.container.querySelector('svg[aria-label="Pairing invitation QR code"]')).not.toBeNull();
  await act(async () => window.dispatchEvent(new Event('blur')));
  expect(view.container.querySelector('svg[aria-label="Pairing invitation QR code"]')).toBeNull();
  expect(view.container.innerHTML).not.toContain(pairingSample.bundle);
  expect(view.client.getMutationCache().getAll()).toEqual([]);
});

it('loads a read-only instance destination without losing entered phrase or offering URL and pin inputs', async () => {
  let resolveConfiguration: ((response: Response) => void) | undefined;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { resolveConfiguration = resolve; })));
  const view = await renderForm(<CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  await act(async () => {
    typeInto(input(view.container, 'invite-pairing-phrase'), ' A4b ');
    typeInto(input(view.container, 'invite-pairing-label'), invite.label);
  });
  expect(view.container.textContent).toContain("Loading this instance's pairing address");
  expect(view.container.querySelector('button[type="submit"]')?.hasAttribute('disabled')).toBe(true);
  expect(view.container.querySelector('#invite-pairing-origin')).toBeNull();
  expect(view.container.querySelector('#invite-pairing-pin')).toBeNull();
  await act(async () => resolveConfiguration?.(pairingConfiguration()));
  await settleTask();
  expect(view.container.textContent).toContain(`Connects to ${invite.origin}`);
  expect(input(view.container, 'invite-pairing-phrase').value).toBe(' A4b ');
  expect(view.container.querySelector('button[type="submit"]')?.hasAttribute('disabled')).toBe(false);
});

it('blocks creation for missing HTTPS configuration and links to the canonical instance settings', async () => {
  const fetchMock = vi.fn(async () => Response.json({ items: [], count: 0, origin: '', configuration_error: 'A public HTTPS origin is required.' }));
  vi.stubGlobal('fetch', fetchMock);
  const view = await renderForm(<MemoryRouter><CreatePairingInviteForm /></MemoryRouter>);
  cleanups.push(view.unmount);
  await settleTask();
  await act(async () => {
    typeInto(input(view.container, 'invite-pairing-phrase'), ' A4b ');
    typeInto(input(view.container, 'invite-pairing-label'), invite.label);
  });
  expect(view.container.querySelector('a')?.getAttribute('href')).toBe('/instance/config');
  expect(view.container.querySelector('button[type="submit"]')?.hasAttribute('disabled')).toBe(true);
  await submit(view.container);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(input(view.container, 'invite-pairing-phrase').value).toBe(' A4b ');
});

it('hides stale destination on configuration read failure and keeps the phrase through retry', async () => {
  let unavailable = false;
  vi.stubGlobal('fetch', vi.fn(async () => unavailable
    ? Response.json({ error: { code: 'service_unavailable', message: 'Unavailable' } }, { status: 503 })
    : pairingConfiguration()));
  const view = await renderForm(<CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  await settleTask();
  await act(async () => typeInto(input(view.container, 'invite-pairing-phrase'), ' A4b '));
  unavailable = true;
  await act(async () => view.client.invalidateQueries({ queryKey: ['remote-pairing-invites'] }));
  await settleTask();
  expect(view.container.textContent).not.toContain(invite.origin);
  expect(view.container.textContent).toContain('The pairing address could not be read');
  expect(input(view.container, 'invite-pairing-phrase').value).toBe(' A4b ');
  unavailable = false;
  await click(view.container, 'Retry pairing configuration');
  expect(view.container.textContent).toContain(`Connects to ${invite.origin}`);
  expect(input(view.container, 'invite-pairing-phrase').value).toBe(' A4b ');
});

it('shows caller-safe certificate discovery guidance and keeps approval fields usable', async () => {
  const detail = 'The public HTTPS certificate could not be verified. Ask the operator to check the advanced pairing fingerprint setting.';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => request.method === 'GET'
    ? pairingConfiguration()
    : Response.json({ error: { code: 'bad_request', message: 'Invalid request', detail } }, { status: 400 })));
  const view = await renderForm(<CreatePairingInviteForm />);
  cleanups.push(view.unmount);
  await settleTask();
  await act(async () => {
    typeInto(input(view.container, 'invite-pairing-phrase'), ' A4b ');
    typeInto(input(view.container, 'invite-pairing-label'), invite.label);
  });
  await submit(view.container);
  expect(view.container.querySelector('[role="alert"]')?.textContent).toBe(detail);
  expect(input(view.container, 'invite-pairing-label').value).toBe(invite.label);
  expect(input(view.container, 'invite-pairing-phrase').disabled).toBe(false);
  expect(input(view.container, 'invite-pairing-phrase').value).toBe('');
  expect(view.container.querySelector('#invite-pairing-pin')).toBeNull();
});

it('rejects malformed invitations locally and never sends a pairing request', async () => {
  const requests: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    requests.push(new URL(request.url).pathname);
    return Response.json(prepared, { status: 201 });
  }));
  const view = await renderForm(<PairRemoteForm />);
  cleanups.push(view.unmount);
  await click(view.container, 'Generate pairing phrase');
  await act(async () => {
    typeInto(input(view.container, 'pair-remote-name'), 'Remote');
    typeInto(input(view.container, 'pair-remote-invitation'), 'https://unrelated.example');
  });
  await submit(view.container);
  expect(requests).toEqual(['/api/v1/instance/pairing-requests']);
  expect(view.container.textContent).toContain('valid Hikyo pairing invitation');
});
