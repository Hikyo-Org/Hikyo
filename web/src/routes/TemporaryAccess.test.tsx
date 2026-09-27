// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { duration, TemporaryAccess } from './TemporaryAccess.tsx';

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'usr_00000000-0000-0000-0000-00000000000a' } } }),
}));

afterEach(() => vi.unstubAllGlobals());

const env = {
  id: 'env_00000000-0000-0000-0000-000000000001',
  org_id: 'org_00000000-0000-0000-0000-000000000001',
  project_id: 'prj_00000000-0000-0000-0000-000000000001',
  name: 'production',
  display_order: 0,
  created_at: '2026-09-01T00:00:00Z',
};

const offer = {
  policy_id: 'xpol_00000000-0000-0000-0000-000000000001',
  policy_version: 1,
  capabilities: ['read', 'reveal'],
  max_duration_seconds: 28800,
  min_approvals: 1,
  enabled: true,
  caller_may_bypass: false,
};

function request(overrides: Record<string, unknown>) {
  return {
    id: 'xreq_00000000-0000-0000-0000-000000000001',
    environment_id: env.id,
    policy_id: offer.policy_id,
    policy_version: 1,
    requester: 'usr_00000000-0000-0000-0000-00000000000b',
    requester_name: 'Dana Jacobs',
    capabilities: ['reveal'],
    duration_seconds: 3600,
    reason: 'incident 42',
    bypassed: false,
    state: 'open',
    invalidated_cause: '',
    min_approvals: 1,
    approvals: 0,
    votes: [],
    created_at: '2026-09-01T00:00:00Z',
    review_expires_at: '2026-09-02T00:00:00Z',
    ...overrides,
  };
}

function selectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === text);
  if (found === undefined) throw new Error(`missing button ${text}`);
  return found;
}

async function mount() {
  return renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/temporary-access']}>
      <Routes>
        <Route path="/orgs/:org/projects/:project/temporary-access" element={<TemporaryAccess />} />
      </Routes>
    </MemoryRouter>,
  );
}

it('renders durations in the largest whole unit', () => {
  expect(duration(28800)).toBe('8h');
  expect(duration(90)).toBe('90s');
  expect(duration(1800)).toBe('30m');
});

it('files an immutable request within the offer and keeps policy administration to managers', async () => {
  const bodies: unknown[] = [];
  let queue = { offer, items: [] as unknown[] };
  vi.stubGlobal('fetch', async (req: Request) => {
    const path = new URL(req.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/access-policies')) return Response.json({ code: 'not_found', message: 'not found' }, { status: 404 });
    if (path.endsWith('/access-requests') && req.method === 'GET') return Response.json(queue);
    if (path.endsWith('/access-requests') && req.method === 'POST') {
      const body: unknown = await req.json();
      bodies.push(body);
      const filed = request({ requester: 'usr_00000000-0000-0000-0000-00000000000a', requester_name: 'Me' });
      queue = { offer, items: [filed] };
      return Response.json(filed);
    }
    throw new Error(`unexpected ${req.method} ${path}`);
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    expect(container.querySelector('.page.page--chrome')).not.toBeNull();
    expect(container.textContent).toContain('Access policies are administered by project member managers.');
    const select = container.querySelector('#ta-env');
    if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
    await act(async () => selectValue(select, env.id));
    await settleTask();
    // Only the offered capabilities are requestable.
    const labels = [...container.querySelectorAll('fieldset label')].map((l) => l.textContent);
    expect(labels).toEqual(['read', 'reveal']);
    expect(button(container, 'Request access').disabled).toBe(true);
    const revealLabel = [...container.querySelectorAll('fieldset label')].find((l) => l.textContent === 'reveal');
    const reveal = container.querySelector<HTMLInputElement>(`#${CSS.escape(revealLabel?.getAttribute('for') ?? '')}`);
    await act(async () => reveal?.click());
    const reason = container.querySelector('#ta-reason');
    if (!(reason instanceof HTMLTextAreaElement)) throw new Error('reason missing');
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
      setter?.call(reason, 'incident 42');
      reason.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const hours = container.querySelector('#ta-hours');
    if (!(hours instanceof HTMLInputElement)) throw new Error('hours missing');
    await act(async () => typeInto(hours, '2'));
    await act(async () => button(container, 'Request access').click());
    await settleTask();
    expect(bodies).toEqual([{ capabilities: ['reveal'], reason: 'incident 42', duration_seconds: 7200 }]);
    expect(container.textContent).toContain('Request submitted.');
    // The requester withdraws, never approves, their own request.
    expect(button(container, 'Withdraw')).toBeDefined();
    expect([...container.querySelectorAll('button')].some((b) => b.textContent === 'Approve')).toBe(false);
    expect(container.querySelector('.temporary-access__request-state')?.textContent).toBe('open · 0/1 approvals');
  } finally {
    await unmount();
  }
});

it('lets an approver decide another person\'s request and revoke granted access', async () => {
  const posts: string[] = [];
  let items: unknown[] = [request({})];
  vi.stubGlobal('fetch', async (req: Request) => {
    const path = new URL(req.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/access-policies')) return Response.json({ items: [] });
    if (path.endsWith('/access-requests')) return Response.json({ offer, items });
    if (req.method === 'POST') {
      posts.push(path.slice(path.lastIndexOf('/') + 1));
      const granted = request({ state: 'granted', granted_at: '2026-09-01T01:00:00Z', expires_at: '2026-09-01T02:00:00Z' });
      items = [path.endsWith('/revoke') ? request({ state: 'revoked', granted_at: '2026-09-01T01:00:00Z', expires_at: '2026-09-01T02:00:00Z', resolved_at: '2026-09-01T01:30:00Z' }) : granted];
      return Response.json(items[0]);
    }
    throw new Error(`unexpected ${req.method} ${path}`);
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    expect(container.textContent).toContain('No access policies. Nothing is requestable in this project yet.');
    const select = container.querySelector('#ta-env');
    if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
    await act(async () => selectValue(select, env.id));
    await settleTask();
    await act(async () => button(container, 'Approve').click());
    await settleTask();
    expect(posts).toEqual(['vote']);
    expect(container.querySelector('.temporary-access__request-state')?.textContent).toBe('granted');
    await act(async () => button(container, 'Revoke').click());
    await settleTask();
    expect(posts).toEqual(['vote', 'revoke']);
    expect(container.textContent).toContain('Access revoked. It ended immediately.');
    expect(container.querySelector('.temporary-access__request-state')?.textContent).toBe('revoked');
    // A revoked grant ended when it was revoked, not at its scheduled expiry.
    expect(container.textContent).toContain(new Date('2026-09-01T01:30:00Z').toLocaleString());
    expect(container.textContent).not.toContain(new Date('2026-09-01T02:00:00Z').toLocaleString());
  } finally {
    await unmount();
  }
});

it('sends only the capabilities the current environment offers', async () => {
  const staging = { ...env, id: 'env_00000000-0000-0000-0000-000000000002', name: 'staging', display_order: 1 };
  const bodies: unknown[] = [];
  vi.stubGlobal('fetch', async (req: Request) => {
    const path = new URL(req.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env, staging], count: 2 });
    if (path.endsWith('/access-policies')) return Response.json({ code: 'not_found', message: 'not found' }, { status: 404 });
    if (path.endsWith('/access-requests') && req.method === 'GET') {
      const wide = path.includes(env.id);
      return Response.json({ offer: { ...offer, capabilities: wide ? ['edit', 'read', 'reveal'] : ['read', 'reveal'] }, items: [] });
    }
    if (path.endsWith('/access-requests') && req.method === 'POST') {
      bodies.push(await req.json());
      return Response.json(request({ environment_id: staging.id }));
    }
    throw new Error(`unexpected ${req.method} ${path}`);
  });
  const tick = async (container: HTMLElement, name: string) => {
    const label = [...container.querySelectorAll('fieldset label')].find((l) => l.textContent === name);
    const box = container.querySelector<HTMLInputElement>(`#${CSS.escape(label?.getAttribute('for') ?? '')}`);
    if (box === null) throw new Error(`missing capability ${name}`);
    await act(async () => box.click());
  };
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const select = container.querySelector('#ta-env');
    if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
    await act(async () => selectValue(select, env.id));
    await settleTask();
    await tick(container, 'edit');
    await tick(container, 'reveal');
    // Switching to an environment that does not offer `edit` hides it; the
    // hidden tick must not ride along in the request.
    await act(async () => selectValue(select, staging.id));
    await settleTask();
    expect([...container.querySelectorAll('fieldset label')].map((l) => l.textContent)).toEqual(['read', 'reveal']);
    const reason = container.querySelector('#ta-reason');
    if (!(reason instanceof HTMLTextAreaElement)) throw new Error('reason missing');
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
      setter?.call(reason, 'incident 43');
      reason.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button(container, 'Request access').click());
    await settleTask();
    expect(bodies).toEqual([{ capabilities: ['reveal'], reason: 'incident 43', duration_seconds: 3600 }]);
  } finally {
    await unmount();
  }
});

it('takes one emergency grant however often the button is clicked during the window check', async () => {
  const pending = deferred<Response>();
  let windowChecks = 0;
  const emergencies: unknown[] = [];
  vi.stubGlobal('fetch', async (req: Request) => {
    const path = new URL(req.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/access-policies')) return Response.json({ code: 'not_found', message: 'not found' }, { status: 404 });
    if (path.endsWith('/access-requests')) return Response.json({ offer: { ...offer, caller_may_bypass: true }, items: [] });
    if (path.endsWith('/reveal-window')) {
      windowChecks += 1;
      return pending.promise;
    }
    if (path.endsWith('/emergency')) {
      emergencies.push(await req.json());
      return Response.json(request({ state: 'granted', bypassed: true, requester: 'usr_00000000-0000-0000-0000-00000000000a' }));
    }
    throw new Error(`unexpected ${req.method} ${path}`);
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    const select = container.querySelector('#ta-env');
    if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
    await act(async () => selectValue(select, env.id));
    await settleTask();
    const revealLabel = [...container.querySelectorAll('fieldset label')].find((l) => l.textContent === 'reveal');
    const reveal = container.querySelector<HTMLInputElement>(`#${CSS.escape(revealLabel?.getAttribute('for') ?? '')}`);
    await act(async () => reveal?.click());
    const reason = container.querySelector('#ta-emergency-reason');
    if (!(reason instanceof HTMLInputElement)) throw new Error('emergency reason missing');
    await act(async () => typeInto(reason, 'outage'));
    await act(async () => button(container, 'Take emergency access').click());
    expect(button(container, 'Take emergency access').disabled).toBe(true);
    await act(async () => button(container, 'Take emergency access').click());
    await act(async () => pending.resolve(Response.json(revealWindow(true))));
    await settleTask();
    expect(windowChecks).toBe(1);
    expect(emergencies).toHaveLength(1);
  } finally {
    await unmount();
  }
});

it('disables policy deletion until the pending request settles', async () => {
  const deletion = deferred<Response>();
  let deletes = 0;
  const policy = {
    id: offer.policy_id, environment_id: env.id, capabilities: ['reveal'],
    max_duration_seconds: 3600, min_approvals: 1, allow_self_approval: false,
    request_ttl_seconds: 3600, enabled: true, version: 1, approvers: [], bypassers: [],
    created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
  };
  vi.stubGlobal('fetch', async (req: Request) => {
    const path = new URL(req.url).pathname;
    if (path.endsWith('/environments')) return Response.json({ items: [env], count: 1 });
    if (path.endsWith('/access-policies')) return Response.json({ items: [policy], count: 1 });
    if (path.endsWith(`/access-policies/${policy.id}`) && req.method === 'DELETE') {
      deletes++;
      return deletion.promise;
    }
    if (path.endsWith('/access-requests')) return Response.json({ offer, items: [] });
    throw new Error(`unexpected ${req.method} ${path}`);
  });
  const { container, unmount } = await mount();
  try {
    await settleTask();
    await act(async () => button(container, 'Delete').click());
    expect(deletes).toBe(0);
    expect(button(container, 'Delete policy').disabled).toBe(true);
    const confirmation = container.querySelector('.danger-zone input');
    if (!(confirmation instanceof HTMLInputElement)) throw new Error('confirmation input missing');
    await act(async () => typeInto(confirmation, env.name));
    await act(async () => button(container, 'Delete policy').click());
    await settleTask();
    expect(button(container, 'Delete').disabled).toBe(true);
    expect(button(container, 'Delete policy').disabled).toBe(true);
    await act(async () => button(container, 'Delete').click());
    expect(deletes).toBe(1);
    await act(async () => deletion.resolve(Response.json({ code: 'conflict', message: 'conflict' }, { status: 409 })));
    await settleTask();
    expect(button(container, 'Delete').disabled).toBe(false);
  } finally {
    await unmount();
  }
});
