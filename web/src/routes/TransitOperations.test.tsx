// @vitest-environment happy-dom
import { zTransitKey } from '@hikyo/zod';
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { parseTransitCallers, transitWholeNumber, transitCallerWidening, TransitKeyManagement } from './TransitKeyManagement.tsx';
import { TransitOperations } from './TransitOperations.tsx';

const env = { org: 'acme', project: 'app', environment: 'env_00000000-0000-7000-8000-000000000001' };
export const transitKeyFixture = zTransitKey.parse({
  id: 'trk_00000000-0000-7000-8000-000000000001', environment_id: env.environment,
  name: 'app-key', algorithm: 'xchacha20-poly1305', custody: 'software', allowed_operations: ['encrypt', 'decrypt', 'rewrap', 'datakey', 'datakey-plaintext'],
  exportable: false, state: 'active', latest_version: 1, min_encrypt_version: 1, min_decrypt_version: 1,
  compromised_through_version: 0, rotation_period_seconds: 0, rotation_due: false,
  created_by: 'prn_operator', created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
  versions: [{ version: 1, material: 'sealed', created_at: '2026-10-01T00:00:00Z' }], callers: [],
});
const key = transitKeyFixture;
function button(container: HTMLElement, label: string) {
  const found = [...container.querySelectorAll('button')].find((item) => item.textContent === label);
  if (found === undefined) throw new Error(`Missing button ${label}`);
  return found;
}
function textarea(container: HTMLElement, label: string) {
  const found = [...container.querySelectorAll('label')].find((item) => item.textContent === label);
  const control = found === undefined ? null : container.querySelector(`#${CSS.escape(found.htmlFor)}`);
  if (!(control instanceof HTMLTextAreaElement)) throw new Error(`Missing textarea ${label}`);
  return control;
}
async function write(container: HTMLElement, label: string, value: string) {
  const control = textarea(container, label);
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(control, value);
    control.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
async function choose(container: HTMLElement, operation: string) {
  const select = container.querySelector('select');
  if (select === null) throw new Error('Missing operation select');
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set?.call(select, operation);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
}
async function submit(container: HTMLElement) {
  await act(async () => { container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
  await settleTask();
}
afterEach(() => vi.unstubAllGlobals());
it('shows decrypted plaintext once without entering the query or mutation cache', async () => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json({ plaintext: 'cHVibGljLXRlc3QtZml4dHVyZQ==', key_version: 1 }))));
  const view = await renderForm(<TransitOperations env={env} transitKey={key} canAct />);
  try {
    await choose(view.container, 'decrypt');
    await write(view.container, 'Ciphertext', 'fixture-ciphertext');
    await submit(view.container);
    expect(textarea(view.container, 'Ciphertext').value).toBe('');
    expect(textarea(view.container, 'Plaintext output (base64)').value.length > 0).toBe(true);
    expect(button(view.container, 'Clear result').disabled).toBe(true);
    expect(view.client.getMutationCache().getAll()).toHaveLength(0);
    expect(view.client.getQueryCache().getAll()).toHaveLength(0);
    const stored = view.container.querySelector('input[type="checkbox"]');
    if (!(stored instanceof HTMLInputElement)) throw new Error('Missing stored confirmation');
    await act(async () => stored.click());
    await act(async () => button(view.container, 'Clear result').click());
    expect(view.container.querySelector('[aria-label="Operation result"]')).toBeNull();
  } finally { await view.unmount(); }
});
it.each(['blur', 'pagehide', 'session'])('clears sensitive input and drops late plaintext after %s', async (boundary) => {
  let complete: (response: Response) => void = () => { throw new Error('No request started'); };
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { complete = resolve; })));
  const view = await renderForm(<TransitOperations env={env} transitKey={key} canAct />);
  try {
    await choose(view.container, 'decrypt');
    await write(view.container, 'Ciphertext', 'fixture-ciphertext');
    await write(view.container, 'Context (base64)', 'Zml4dHVyZQ==');
    await submit(view.container);
    await act(async () => {
      if (boundary === 'session') retireSensitiveOperations(view.client);
      else window.dispatchEvent(new Event(boundary));
      complete(Response.json({ plaintext: 'cHVibGljLXRlc3QtZml4dHVyZQ==', key_version: 1 }));
    });
    await settleTask();
    expect(view.container.querySelector('[aria-label="Operation result"]')).toBeNull();
    expect(textarea(view.container, 'Ciphertext').value).toBe('');
    expect(textarea(view.container, 'Context (base64)').value).toBe('');
  } finally { await view.unmount(); }
});
it('drops late delivery to a replaced key surface', async () => {
  let complete: (response: Response) => void = () => { throw new Error('No request started'); };
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { complete = resolve; })));
  const view = await renderForm(<TransitOperations key="old" env={env} transitKey={key} canAct />);
  try {
    await choose(view.container, 'decrypt'); await write(view.container, 'Ciphertext', 'fixture-ciphertext'); await submit(view.container);
    await view.rerender(<TransitOperations key="new" env={{ ...env, environment: 'env_other' }} transitKey={{ ...key, name: 'other-key' }} canAct />);
    await act(async () => complete(Response.json({ plaintext: 'cHVibGljLXRlc3QtZml4dHVyZQ==', key_version: 1 })));
    await settleTask();
    expect(view.container.querySelector('[aria-label="Operation result"]')).toBeNull();
  } finally { await view.unmount(); }
});
it('refuses raw server details and retains safe policy refusal copy', async () => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json({ error: { code: 'conflict', message: 'SENSITIVE_SERVER_DETAIL' } }, { status: 409 }))));
  const view = await renderForm(<TransitOperations env={env} transitKey={{ ...key, state: 'disabled' }} canAct />);
  try {
    await write(view.container, 'Plaintext (base64)', 'Zml4dHVyZQ=='); await submit(view.container);
    expect(view.container.textContent).toContain('This key is disabled. Cryptographic operations are refused.');
    expect(view.container.textContent).toContain('The key state or policy refused this operation.');
    expect(view.container.textContent).not.toContain('SENSITIVE_SERVER_DETAIL');
    expect(textarea(view.container, 'Plaintext (base64)').value).toBe('');
  } finally { await view.unmount(); }
});
it('parses caller restrictions and refuses unsafe integer inputs', () => {
  expect(parseTransitCallers('prn_00000000-0000-7000-8000-000000000002 encrypt,decrypt\nprn_00000000-0000-7000-8000-000000000003 sign')).toEqual([{ principal_id: 'prn_00000000-0000-7000-8000-000000000002', operations: ['encrypt', 'decrypt'] }, { principal_id: 'prn_00000000-0000-7000-8000-000000000003', operations: ['sign'] }]);
  expect(parseTransitCallers('')).toEqual([]);
  expect(() => parseTransitCallers('prn_00000000-0000-7000-8000-000000000002 export')).toThrow();
  for (const raw of ['', '1.2', '-1', '9007199254740992']) expect(() => transitWholeNumber(raw, 0)).toThrow();
});

it('distinguishes caller narrowing from new principals, new operations and unrestricted access', () => {
  const old = [{ principal_id: 'prn_00000000-0000-7000-8000-000000000002', operations: key.allowed_operations.slice(0, 1) }];
  expect(transitCallerWidening([], old)).toEqual([]);
  expect(transitCallerWidening(old, old)).toEqual([]);
  expect(transitCallerWidening(old, [])).toEqual(['Every principal with crypto-use in this environment']);
  expect(transitCallerWidening(old, [{ principal_id: 'prn_00000000-0000-7000-8000-000000000003', operations: ['decrypt'] }])).toEqual(['prn_00000000-0000-7000-8000-000000000003: decrypt']);
  expect(transitCallerWidening(old, [{ principal_id: 'prn_00000000-0000-7000-8000-000000000002', operations: ['encrypt', 'decrypt'] }])).toEqual(['prn_00000000-0000-7000-8000-000000000002: decrypt']);
});
it('requires an explicit resettable acknowledgment before widening caller access', async () => {
  const fetch = vi.fn(() => Promise.resolve(Response.json({ error: { code: 'conflict', message: 'refused' } }, { status: 409 })));
  vi.stubGlobal('fetch', fetch);
  const restricted = { ...key, callers: [{ principal_id: 'prn_00000000-0000-7000-8000-000000000002', operations: key.allowed_operations.slice(0, 1) }] };
  const view = await renderForm(<TransitKeyManagement env={env} transitKey={restricted} canAct />);
  try {
    await act(async () => button(view.container, 'Configure key').click());
    const dialog = view.container.querySelector('dialog');
    if (dialog === null) throw new Error('Missing configuration dialog');
    await write(dialog, 'Caller entries', '');
    expect(dialog.textContent).toContain('Every principal with crypto-use in this environment');
    expect(button(dialog, 'Configure key').disabled).toBe(true);
    await submit(dialog);
    expect(fetch).not.toHaveBeenCalled();
    const acknowledgment = dialog.querySelector('input[type="checkbox"]');
    if (!(acknowledgment instanceof HTMLInputElement)) throw new Error('Missing expanded access acknowledgment');
    await act(async () => acknowledgment.click());
    expect(button(dialog, 'Configure key').disabled).toBe(false);
    await write(dialog, 'Caller entries', 'prn_00000000-0000-7000-8000-000000000003 encrypt');
    expect(button(dialog, 'Configure key').disabled).toBe(true);
    const updatedAcknowledgment = dialog.querySelector('input[type="checkbox"]');
    if (!(updatedAcknowledgment instanceof HTMLInputElement)) throw new Error('Missing expanded access acknowledgment');
    await act(async () => updatedAcknowledgment.click());
    await submit(dialog);
    expect(fetch).toHaveBeenCalledTimes(1);
  } finally { await view.unmount(); }
});

it('clears committed sensitive DOM synchronously before a pagehide handler returns', async () => {
  const view = await renderForm(<TransitOperations env={env} transitKey={key} canAct />);
  try {
    await write(view.container, 'Plaintext (base64)', 'Zml4dHVyZQ==');
    // Deliberately outside act: no test-owned React flush may hide a BFCache
    // capture that occurs before the pagehide callback returns.
    window.dispatchEvent(new Event('pagehide'));
    expect(textarea(view.container, 'Plaintext (base64)').value).toBe('');
  } finally { await view.unmount(); }
});
