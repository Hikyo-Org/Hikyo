// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import type { AdapterTargetInput } from '../api/adapters.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Adapters, TargetForm } from './Adapters.tsx';

function selectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

afterEach(() => vi.unstubAllGlobals());

it.each([
  ['forgejo', 'Forgejo'],
  ['github-actions', 'GitHub Actions'],
  ['future-provider', 'future-provider'],
])('renders the %s provider through the response decoder', async (provider, label) => {
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    if (request.method !== 'GET') throw new Error(`unexpected ${request.method}`);
    const base = '/api/v1/orgs/acme/projects/app';
    if (path === `${base}/adapters`) {
      return Promise.resolve(Response.json({ items: [{
        id: 'adp_00000000-0000-0000-0000-000000000001', provider,
        origin: 'https://ci.example', credential_present: false,
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
  try {
    await settleTask();
    expect(container.querySelector('.adapters__adapter h2')?.textContent).toBe(label);
    expect(container.querySelector('[role="alert"]')).toBeNull();
  } finally {
    await unmount();
  }
});

it('TargetForm keeps the typed prefix, previews the stored form, and parses selected repository ids', async () => {
  const submitted: AdapterTargetInput[] = [];
  const onSubmit = (input: AdapterTargetInput) => {
    submitted.push(input);
    return Promise.resolve();
  };
  const { container, unmount } = await renderForm(
    <TargetForm
      title="Add target"
      environments={[{ id: 'env_1', name: 'prod' }]}
      keys={[]}
      busy={false}
      onCancel={() => undefined}
      onSubmit={onSubmit}
    />,
  );
  try {
    const select = (label: string) =>
      [...container.querySelectorAll('label')].find((l) => l.textContent?.startsWith(label))?.querySelector('select, input');
    const kind = select('Destination kind');
    if (!(kind instanceof HTMLSelectElement)) throw new Error('kind select missing');
    expect([...kind.options].map((o) => o.textContent)).toContain('GitHub organization');
    await act(async () => selectValue(kind, 'organization'));
    const visibility = select('Visibility');
    if (!(visibility instanceof HTMLSelectElement)) throw new Error('visibility select missing');
    await act(async () => selectValue(visibility, 'selected'));
    const ids = select('Repository ids');
    if (!(ids instanceof HTMLInputElement)) throw new Error('repository ids input missing');
    await act(async () => typeInto(ids, '12, x'));
    const prefix = select('Name prefix');
    if (!(prefix instanceof HTMLInputElement)) throw new Error('prefix input missing');
    await act(async () => typeInto(prefix, 'prod_'));
    expect(prefix.value).toBe('prod_');
    expect(container.textContent).toContain('Will be stored as PROD_');

    const form = container.querySelector('form');
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted).toHaveLength(0);
    expect(container.textContent).toContain('Repository ids are whole numbers.');

    await act(async () => typeInto(ids, '12, 34'));
    await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(submitted[0]).toMatchObject({ visibility: 'selected', selected_repository_ids: [12, 34], name_prefix: 'PROD_' });
  } finally {
    await unmount();
  }
});

it('lists each failed name on its own line and every warning separately', async () => {
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url).pathname;
    const base = '/api/v1/orgs/acme/projects/app';
    const target = {
      id: 'tgt_00000000-0000-0000-0000-000000000001', adapter_id: 'adp_00000000-0000-0000-0000-000000000001',
      environment_id: 'env_00000000-0000-0000-0000-000000000001', destination_kind: 'repository',
      destination_owner: 'acme', destination_name: 'app', destination_environment: '', visibility: '',
      destination_id: 1, repository_id: 0,
      selected_repository_ids: [], name_prefix: '', generation: 1, state: 'active', sync_status: 'degraded',
      converged_revision: null, last_attempted_revision: null, last_attempted_at: null,
      last_error_class: '', retry_at: null, paused_at: null, drift_attention: false,
      failure_names: ['DB_URL', 'API_KEY'], warnings: ['rate limited', 'name truncated'],
      keys: [], conflicts: [],
    };
    if (path === `${base}/adapters`) {
      return Promise.resolve(Response.json({ items: [{
        id: 'adp_00000000-0000-0000-0000-000000000001', provider: 'forgejo',
        origin: 'https://ci.example', credential_present: true,
        authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
        state: 'active', created_at: '2026-09-01T00:00:00Z', targets: [target],
      }] }));
    }
    if (path === `${base}/adapter-targets/${target.id}`) {
      return Promise.resolve(Response.json({ target, conflicts: [], mapping: [] }));
    }
    if (path === `${base}/environments` || path === `${base}/keys`) {
      return Promise.resolve(Response.json({ items: [] }));
    }
    throw new Error(`unexpected ${path}`);
  }));
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/adapters?target=tgt_00000000-0000-0000-0000-000000000001']}>
      <Routes><Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} /></Routes>
    </MemoryRouter>,
  );
  try {
    // Two settles: the adapter list lands first, the target detail second.
    await settleTask();
    await settleTask();
    const failures = [...container.querySelectorAll('.adapters__failures li')].map((li) => li.textContent?.trim());
    expect(failures).toEqual(['! DB_URL: failed', '! API_KEY: failed']);
    const warnings = [...container.querySelectorAll('.adapters__warnings li')].map((li) => li.textContent);
    expect(warnings).toEqual(['rate limited', 'name truncated']);
  } finally {
    await unmount();
  }
});
