// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { retireSensitiveOperations } from '../api/sensitiveMutation.ts';
import type { RevealWindow } from '../api/values.ts';
import { RevisionDiffDialog } from './RevisionDiff.tsx';

const mocks = vi.hoisted(() => ({ window: vi.fn(), disclose: vi.fn() }));
vi.mock('../app/AuthProvider.tsx', () => ({ useAuth: () => ({ identity: { session: { id: 'session' } } }) }));
vi.mock('../api/transport.tsx', () => ({ useTransport: () => ({ client: undefined }) }));
vi.mock('../api/values.ts', () => ({ fetchRevealWindow: mocks.window }));
vi.mock('./Ceremony.tsx', () => ({ Ceremony: () => <output>Ceremony requested</output> }));
const masked = { left_revision: 1, right_revision: 2, items: [{ key_id: 'key_0198aaaa-bbbb-7ccc-8ddd-eeeeffff0002', name: 'TOKEN', classification: 'secret', status: 'edited', revealed: false }] };
const disclosed = { ...masked, items: [{ ...masked.items[0], status: 'changed', revealed: true, before: 'before-secret', after: 'after-secret' }] };
function button(container: HTMLElement, name: string) {
  const found = [...container.querySelectorAll('button')].find((element) => element.textContent === name);
  if (found === undefined) throw new Error(`Missing ${name}`);
  return found;
}
beforeEach(() => {
  mocks.window.mockReset().mockResolvedValue(revealWindow(true));
  mocks.disclose.mockReset().mockResolvedValue(disclosed);
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const body = request.url.endsWith('/reveal') ? await mocks.disclose() : masked;
    return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
  }));
});
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });
async function mount() {
  const form = await renderForm(<RevisionDiffDialog env={{ org: 'org', project: 'project', environment: 'env' }} environmentName="Development" left={1n} right={2n} onClose={() => {}} />);
  await settle();
  return form;
}
it('keeps per-key disclosure out of query/mutation caches and masks on blur', async () => {
  const form = await mount();
  try {
    await act(async () => button(form.container, 'Reveal TOKEN in diff').click());
    await settle();
    expect(form.container.textContent).toContain('before-secret');
    expect(form.client.getMutationCache().getAll()).toEqual([]);
    expect(form.client.getQueryCache().getAll()).toEqual([]);
    await act(async () => window.dispatchEvent(new Event('blur')));
    expect(form.container.textContent).not.toContain('before-secret');
  } finally { await form.unmount(); }
});
it('does not resume a guard preflight after blur', async () => {
  const pending = deferred<RevealWindow>();
  mocks.window.mockReturnValue(pending.promise);
  const form = await mount();
  try {
    await act(async () => button(form.container, 'Reveal TOKEN in diff').click());
    await act(async () => window.dispatchEvent(new Event('blur')));
    await act(async () => pending.resolve(revealWindow(true)));
    await settle();
    expect(mocks.disclose).not.toHaveBeenCalled();
    expect(form.container.textContent).not.toContain('before-secret');
  } finally { await form.unmount(); }
});
it('discards an in-flight disclosure when the session retires', async () => {
  const pending = deferred<typeof disclosed>();
  mocks.disclose.mockReturnValue(pending.promise);
  const form = await mount();
  try {
    await act(async () => button(form.container, 'Reveal TOKEN in diff').click());
    await settle();
    await act(async () => retireSensitiveOperations(form.client));
    await act(async () => pending.resolve(disclosed));
    await settle();
    expect(form.container.textContent).not.toContain('before-secret');
    expect(form.client.getMutationCache().getAll()).toEqual([]);
  } finally { await form.unmount(); }
});
