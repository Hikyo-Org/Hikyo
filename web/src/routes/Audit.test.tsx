// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import * as auditApi from '../api/audit.ts';
import { Audit, InstanceAudit } from './Audit.tsx';

// The page reads the current principal id (for the "Self" filter shortcut) from
// the auth context; stub it so the route renders without the full provider.
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'usr_self' } } }),
}));

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const event = {
  seq: 1,
  id: 'evt_1',
  type: 'grant.created',
  schema_version: 1,
  occurred_at: '2026-09-01T00:00:00Z',
  occurred_asserted: false,
  recorded_at: '2026-09-01T00:00:00Z',
  actor_class: 'user',
  scope_class: 'org',
  outcome: 'denied',
  origin: 'api',
  payload: {},
};

async function renderAudit(items: readonly (typeof event)[], instance = false) {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal('fetch', async () => {
    await gate;
    return Response.json({ items, count: items.length, next_after_seq: 0, upper_seq: 0, exhausted: true });
  });
  const rendered = await renderForm(
    <MemoryRouter initialEntries={[instance ? '/instance/audit' : '/orgs/acme/audit']}>
      <Routes><Route path="/orgs/:org/audit" element={<Audit />} /><Route path="/instance/audit" element={<InstanceAudit />} /></Routes>
    </MemoryRouter>,
  );
  return { ...rendered, release };
}

it('shows a loading status, then the empty state carrying the one Clear button', async () => {
  const { container, release, unmount } = await renderAudit([]);
  try {
    expect(container.querySelector('#audit-events [role="status"]')?.textContent).toBe('Loading events…');
    // Filter and Events stay on one page, no tabs and no jump row.
    expect(container.querySelector('[role="tablist"]')).toBeNull();
    await act(async () => release());
    await settleTask();
    const empty = container.querySelector('.audit__empty');
    expect(empty?.getAttribute('role')).toBe('status');
    expect(empty?.textContent).toContain('No events match this filter.');
    const clears = [...container.querySelectorAll('button')].filter((b) => b.textContent === 'Clear');
    expect(clears).toHaveLength(1);
    expect(empty?.contains(clears[0] ?? null)).toBe(true);
  } finally {
    await unmount();
  }
});

it('renders rows as audit__row buttons with an aria-hidden outcome glyph', async () => {
  const { container, release, unmount } = await renderAudit([event]);
  try {
    await act(async () => release());
    await settleTask();
    const row = container.querySelector('button.audit__row');
    expect(row?.querySelector('.audit__row-op')?.classList.contains('mono')).toBe(true);
    const glyph = row?.querySelector('.audit__outcome svg[aria-hidden="true"]');
    expect(glyph?.getAttribute('data-glyph')).toBe('denied');
    expect(row?.querySelector('.audit__outcome')?.textContent).toBe('denied');
    // No detail until an event is picked: it opens in the side panel.
    expect(container.querySelector('aside[aria-label="Event detail"]')).toBeNull();
  } finally {
    await unmount();
  }
});


it('shows the actor name in the row while retaining its ID in event details', async () => {
  const named = { ...event, actor_id: 'prn_dana', actor_name: 'Dana Jacobs' };
  const { container, release, unmount } = await renderAudit([named]);
  try {
    await act(async () => release());
    await settleTask();
    const row = container.querySelector<HTMLButtonElement>('button.audit__row');
    expect(row?.querySelector('.audit__row-actor')?.textContent).toBe('Dana Jacobs');
    expect(row?.textContent).not.toContain('prn_dana');
    await act(async () => row?.click());
    expect(container.querySelector('aside[aria-label="Event detail"]')?.textContent).toContain('Principal IDprn_dana');
  } finally { await unmount(); }
});

it('fills Self and carries applied outcomes to the verified export consumer, surfacing its failure', async () => {
  const download = vi.spyOn(auditApi, 'downloadAuditExport').mockRejectedValue(new Error('incomplete receipt'));
  const { container, release, unmount } = await renderAudit([]);
  try {
    await act(async () => release());
    await settleTask();

    const self = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Self');
    await act(async () => self?.click());
    const actor = container.querySelector<HTMLInputElement>('input[placeholder="usr_…"]');
    expect(actor?.value).toBe('usr_self');

    // Queried by its legend, the name the group already carries; no class exists only to be a selector.
    const outcomes = [...container.querySelectorAll('fieldset')].find((set) => set.querySelector('legend')?.textContent === 'Outcomes');
    const boxes = [...(outcomes?.querySelectorAll<HTMLInputElement>('input[type="checkbox"]') ?? [])];
    const denied = boxes.find((b) => b.parentElement?.textContent?.trim() === 'denied');
    const failure = boxes.find((b) => b.parentElement?.textContent?.trim() === 'failure');
    await act(async () => denied?.click());
    await act(async () => failure?.click());

    const apply = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Apply filter');
    await act(async () => apply?.click());
    await settleTask();

    const exportButton = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Export JSONL');
    await act(async () => exportButton?.click());
    await settleTask();
    const href = download.mock.calls[0]?.[0] ?? '';
    const params = new URL(href, 'https://x').searchParams;
    expect(params.get('actor')).toBe('usr_self');
    expect(params.getAll('outcomes')).toEqual(['denied', 'failure']);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('No download was saved');
  } finally {
    await unmount();
  }
});

it.each([
  ['/orgs/acme/audit', '/orgs/:org/audit', 'this organisation'],
  ['/orgs/acme/projects/app/audit', '/orgs/:org/projects/:project/audit', 'this project'],
])('names the audit-read grant and its scope when %s refuses', async (path, pattern, where) => {
  vi.stubGlobal('fetch', async () =>
    Response.json({ error: { code: 'not_found', message: 'not found' } }, { status: 404 }),
  );
  const rendered = await renderForm(
    <MemoryRouter initialEntries={[path]}>
      <Routes><Route path={pattern} element={<Audit />} /></Routes>
    </MemoryRouter>,
  );
  await settleTask();
  const alert = rendered.container.querySelector('[role="alert"]')?.textContent ?? '';
  expect(alert).toContain('This trail is not available, or you may not read it.');
  expect(alert).toContain(`audit-read grant on ${where}`);
  expect(alert).toContain('A member manager can grant it under Members.');
  await rendered.unmount();
});

it('loads the instance trail independently of tenant selection and shows source attribution', async () => {
  const sourced = { ...event, scope_class: 'instance', actor_id: 'usr_self', source_ip: '192.0.2.7', user_agent: 'audit-fixture', actor_credential_id: 'cred_fixture' };
  const { container, release, unmount } = await renderAudit([sourced], true);
  try {
    expect(container.querySelector('h1')?.textContent).toBe('Instance audit');
    expect(container.querySelector('#audit-events [role="status"]')?.textContent).toBe('Loading events…');
    await act(async () => release());
    await settleTask();
    const row = container.querySelector<HTMLButtonElement>('button.audit__row');
    await act(async () => row?.click());
    const detail = container.querySelector('aside[aria-label="Event detail"]')?.textContent ?? '';
    expect(detail).toContain('Source IP192.0.2.7');
    expect(detail).toContain('User agentaudit-fixture');
    expect(detail).toContain('Credentialcred_fixture');
    expect(detail).toContain('Actor classuser');
  } finally { await unmount(); }
});

it('names instance authority on denial and disables export', async () => {
  vi.stubGlobal('fetch', async () => Response.json({ error: { code: 'forbidden', message: 'forbidden' } }, { status: 403 }));
  const { container, unmount } = await renderForm(<MemoryRouter><InstanceAudit /></MemoryRouter>);
  try {
    await settleTask();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('audit-read grant on this instance');
    const button = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Export JSONL');
    expect(button?.disabled).toBe(true);
    expect(container.querySelector('.audit__empty')).toBeNull();
  } finally { await unmount(); }
});

it('pins the instance query ceiling across browser pages', async () => {
  const calls: string[] = [];
  vi.stubGlobal('fetch', async (request: Request) => {
    calls.push(request.url);
    return Response.json({ items: [], count: 0, next_after_seq: 7, upper_seq: 19, exhausted: calls.length > 1 });
  });
  const { container, unmount } = await renderForm(<MemoryRouter><InstanceAudit /></MemoryRouter>);
  try {
    await settleTask();
    const more = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Load more');
    await act(async () => more?.click());
    await settleTask();
    expect(calls).toHaveLength(2);
    const url = new URL(calls[1] ?? '', 'https://hikyo.example');
    expect(url.pathname).toBe('/api/v1/instance/audit');
    expect(url.searchParams.get('after_seq')).toBe('7');
    expect(url.searchParams.get('to_seq')).toBe('19');
    expect(container.querySelector('.audit__empty')?.textContent).toContain('No events match this filter.');
  } finally { await unmount(); }
});

it('does not announce the end of the trail when a later page loses authority', async () => {
  let calls = 0;
  vi.stubGlobal('fetch', async () => {
    calls++;
    return calls === 1
      ? Response.json({ items: [event], count: 1, next_after_seq: 1, upper_seq: 19, exhausted: false })
      : Response.json({ error: { code: 'forbidden', message: 'forbidden' } }, { status: 403 });
  });
  const { container, unmount } = await renderForm(<MemoryRouter><InstanceAudit /></MemoryRouter>);
  try {
    await settleTask();
    const more = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Load more');
    await act(async () => more?.click());
    await settleTask();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('audit-read grant on this instance');
    expect(container.querySelector('.audit__end')).toBeNull();
  } finally { await unmount(); }
});
