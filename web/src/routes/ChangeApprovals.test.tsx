// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { ChangeApprovals } from './ChangeApprovals.tsx';

afterEach(() => vi.unstubAllGlobals());

const env = {
  id: 'env_00000000-0000-0000-0000-000000000001',
  org_id: 'org_00000000-0000-0000-0000-000000000001',
  project_id: 'prj_00000000-0000-0000-0000-000000000001',
  name: 'production',
  display_order: 0,
  created_at: '2026-09-01T00:00:00Z',
};

function selectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

it('uses the chrome anatomy, loads, and names the next action in each empty state', async () => {
  let releasePolicies: () => void = () => undefined;
  const policiesGate = new Promise<void>((resolve) => {
    releasePolicies = resolve;
  });
  vi.stubGlobal('fetch', async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/approval-policies')) {
      await policiesGate;
      return Response.json({ items: [] });
    }
    if (path.endsWith('/approval-requests')) return Response.json({ items: [] });
    throw new Error(`unexpected ${path}`);
  });
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/approvals']}>
      <Routes><Route path="/orgs/:org/projects/:project/approvals" element={<ChangeApprovals />} /></Routes>
    </MemoryRouter>,
  );
  try {
    expect(container.querySelector('main')).toBeNull();
    expect(container.querySelector('.page.page--chrome')).not.toBeNull();
    expect(container.querySelector('.page__lede')).not.toBeNull();
    expect([...container.querySelectorAll('nav.jump a')].map((a) => a.getAttribute('href'))).toEqual([
      '#ca-policies',
      '#ca-requests',
    ]);
    expect(container.querySelector('#ca-policies [role="status"]')?.textContent).toBe('Loading policies…');
    expect(container.querySelector('#ca-requests [role="status"]')?.textContent).toBe(
      'Choose an environment to see its queue.',
    );
    await act(async () => releasePolicies());
    await settleTask();
    expect(container.textContent).not.toContain('Loading policies');

    const select = container.querySelector('#ca-review-env');
    if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
    await act(async () => selectValue(select, env.id));
    await settleTask();
    expect(container.querySelector('#ca-requests [role="status"]')?.textContent).toBe(
      'No requests in this environment. A policy on this environment creates one when a protected change is staged.',
    );
  } finally {
    await unmount();
  }
});


it('edits disclosed approvers by name while preserving their IDs and keeping other IDs advanced', async () => {
  const principal = 'prn_00000000-0000-0000-0000-000000000001';
  const policy = {
    id: 'pol_00000000-0000-0000-0000-000000000001', environment_id: '', min_approvals: 1,
    allow_self_approval: false, request_ttl_seconds: 3600, enabled: true, version: 1,
    approvers: [{ kind: 'principal', subject_id: principal }], bypassers: [],
    principal_names: { [principal]: 'Dana Jacobs' },
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  };
  vi.stubGlobal('fetch', async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/approval-policies')) return Response.json({ items: [policy] });
    throw new Error(`unexpected directory read ${path}`);
  });
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/approvals']}>
      <Routes><Route path="/orgs/:org/projects/:project/approvals" element={<ChangeApprovals />} /></Routes>
    </MemoryRouter>,
  );
  try {
    await settleTask();
    const edit = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Edit');
    if (edit === undefined) throw new Error('missing Edit');
    await act(async () => edit.click());
    const label = [...container.querySelectorAll('fieldset label')].find((item) => item.textContent === 'Dana Jacobs');
    const checkbox = label?.querySelector('input');
    expect(checkbox?.checked).toBe(true);
    expect(container.querySelector<HTMLTextAreaElement>('#ca-approvers')?.value).toBe(principal);
    await act(async () => checkbox?.click());
    expect(container.querySelector<HTMLTextAreaElement>('#ca-approvers')?.value).toBe('');
    expect(container.querySelector('details')?.open).toBe(false);
  } finally { await unmount(); }
});
