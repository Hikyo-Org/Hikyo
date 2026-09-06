// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Audit } from './Audit.tsx';

afterEach(() => vi.unstubAllGlobals());

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

async function renderAudit(items: readonly (typeof event)[]) {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  vi.stubGlobal('fetch', async () => {
    await gate;
    return Response.json({ items, count: items.length, next_after_seq: 0, upper_seq: 0, exhausted: true });
  });
  const rendered = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/audit']}>
      <Routes><Route path="/orgs/:org/audit" element={<Audit />} /></Routes>
    </MemoryRouter>,
  );
  return { ...rendered, release };
}

it('shows a loading status, then the empty state carrying the one Clear button', async () => {
  const { container, release, unmount } = await renderAudit([]);
  try {
    expect(container.querySelector('#audit-events [role="status"]')?.textContent).toBe('Loading events…');
    expect(container.querySelector('nav.jump')).not.toBeNull();
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
    const glyph = row?.querySelector('.audit__outcome [aria-hidden="true"]');
    expect(glyph?.textContent).toBe('⊘ ');
    expect(row?.querySelector('.audit__outcome')?.textContent).toBe('⊘ denied');
    expect(container.querySelector('#audit-detail [role="status"]')?.textContent).toBe(
      'Select an event to see its detail.',
    );
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
    expect(container.querySelector('#audit-detail')?.textContent).toContain('Principal IDprn_dana');
  } finally { await unmount(); }
});
