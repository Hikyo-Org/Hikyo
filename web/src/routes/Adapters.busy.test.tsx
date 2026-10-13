// @vitest-environment happy-dom
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Adapters } from './Adapters.tsx';

const pending = vi.hoisted(() => ({ credential: false, move: false }));

// The side panel's credential replace and origin move are page-owned
// mutations; stand them in as already in flight.
vi.mock('../api/adapters.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/adapters.ts')>();
  return {
    ...actual,
    useSetAdapterCredential: () => ({ isPending: pending.credential, mutateAsync: vi.fn() }),
    useMoveAdapterOrigin: () => ({ isPending: pending.move, mutateAsync: vi.fn() }),
  };
});

afterEach(() => {
  vi.unstubAllGlobals();
  pending.credential = false;
  pending.move = false;
});

async function rowButtons(): Promise<{ disabled: boolean[]; unmount: () => Promise<void> }> {
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    const base = '/api/v1/orgs/acme/projects/app';
    if (path === `${base}/adapters`) {
      return Promise.resolve(Response.json({ items: [{
        id: 'adp_00000000-0000-0000-0000-000000000001', provider: 'forgejo',
        origin: 'https://ci.example', credential_present: true,
        authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
        state: 'active', created_at: '2026-09-01T00:00:00Z', targets: [],
      }] }));
    }
    if (path === `${base}/environments` || path === `${base}/keys`) {
      return Promise.resolve(Response.json({ items: [] }));
    }
    throw new Error(`unexpected ${path}`);
  }));
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/adapters']}>
      <Routes><Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} /></Routes>
    </MemoryRouter>,
  );
  await settleTask();
  const buttons = [...container.querySelectorAll<HTMLButtonElement>('.adapters__adapter .actions button')];
  expect(buttons.map((button) => button.textContent)).toContain('Delete adapter');
  return { disabled: buttons.map((button) => button.disabled), unmount };
}

it('keeps the adapter row idle when nothing is in flight', async () => {
  const { disabled, unmount } = await rowButtons();
  expect(disabled.every((value) => !value)).toBe(true);
  await unmount();
});

it.each(['credential', 'move'] as const)('disables every row action while a %s mutation is in flight', async (kind) => {
  pending[kind] = true;
  const { disabled, unmount } = await rowButtons();
  expect(disabled.every(Boolean)).toBe(true);
  await unmount();
});
