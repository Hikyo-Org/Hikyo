// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { CreateProviderDialog, providerOriginRefusal } from './machineAccess/DynamicProviders.tsx';

const mocks = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock('../api/dynamic.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/dynamic.ts')>()),
  createDynamicProvider: mocks.create,
  useRefreshProviders: () => vi.fn(),
}));
beforeEach(() => mocks.create.mockReset().mockResolvedValue(undefined));
afterEach(() => vi.restoreAllMocks());

describe('password-free provider origin', () => {
  it.each(['postgres://admin@db.example.com:5432/app', 'postgresql://admin@db.example.com/app'])('accepts %s', (origin) => {
    expect(providerOriginRefusal(origin)).toBeNull();
  });
  it.each([
    'db.internal:5432/app', 'https://admin@db.example.com/app',
    'postgres://db.example.com/app', 'postgres://admin@db.example.com/',
    'postgres://admin:secret@db.example.com/app', 'postgres://admin:@db.example.com/app',
    'postgres://admin@db.example.com/app?sslmode=disable', 'postgres://admin@db.example.com/app#fragment',
  ])('refuses malformed or secret-bearing origin before any dial: %s', (origin) => {
    expect(providerOriginRefusal(origin)).not.toBeNull();
  });
  it.each([
    { origin: 'postgres://admin@db.example.com/app', accepted: true },
    { origin: 'db.internal:5432/app', accepted: false },
    { origin: 'postgres://admin:do-not-store@db.example.com/app', accepted: false },
  ])('the real form only submits a valid password-free origin', async ({ origin, accepted }) => {
    const onCreated = vi.fn();
    const view = await renderForm(<CreateProviderDialog project={{ org: 'acme', project: 'payments' }} onClose={() => {}} onCreated={onCreated} />);
    try {
      for (const [selector, value] of [
        ['#create-provider-origin', origin], ['#create-provider-grant-role', 'lease_parent'], ['#create-provider-credential', 'admin-secret'],
      ]) {
        if (selector === undefined || value === undefined) throw new Error('fixture input missing');
        const input = view.container.querySelector(selector);
        if (!(input instanceof HTMLInputElement)) throw new Error(`missing input ${selector}`);
        await act(async () => typeInto(input, value));
      }
      const submit = Array.from(view.container.querySelectorAll('button')).find((button) => button.textContent === 'Configure provider');
      if (submit === undefined) throw new Error('submit missing');
      await act(async () => submit.click());
      await settle();
      if (accepted) {
        expect(mocks.create).toHaveBeenCalledWith({ org: 'acme', project: 'payments' }, { kind: 'postgres', origin, grant_role: 'lease_parent', credential: 'admin-secret' });
        expect(onCreated).toHaveBeenCalledWith(origin);
        expect(view.container.querySelector('#create-provider-credential')).toHaveProperty('value', '');
      } else {
        expect(mocks.create).not.toHaveBeenCalled();
        expect(onCreated).not.toHaveBeenCalled();
        expect(view.container.querySelector('[role="alert"]')?.textContent).not.toContain('do-not-store');
      }
    } finally { await view.unmount(); }
  });
});
