// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { SecurityMaintenancePanel } from './SecurityMaintenancePanel.tsx';

const cleanups: Array<() => Promise<void>> = [];

afterEach(async () => {
  for (const unmount of cleanups.splice(0)) await unmount();
  vi.unstubAllGlobals();
});

const now = new Date('2026-11-04T12:00:00Z');
const status = {
  mode: 'automatic',
  channel: 'stable',
  enrolled: true,
  phase: 'current',
  protection_gap: false,
  applicable: [],
  coverage: [],
  covered_components: [],
  catalog_sequence: 42,
  last_attempt: '2026-11-04T11:30:00Z',
  last_verified: '2026-11-04T11:30:00Z',
  known_until: '2026-11-06T11:30:00Z',
  running_version: '1.4.2',
};

async function render(body: unknown, init?: ResponseInit, at: Date | null = now): Promise<HTMLElement> {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json(body, init))));
  const { container, unmount } = await renderForm(<SecurityMaintenancePanel now={at ?? undefined} />);
  cleanups.push(unmount);
  await settleTask();
  return container;
}

const reported = (overrides: object) => ({ reported: true, unreadable: false, status: { ...status, ...overrides } });

it('says nothing is reported when no status exists', async () => {
  const container = await render({ reported: false, unreadable: false });
  expect(container.textContent).toContain('No status has been reported');
  expect(container.textContent).not.toContain('Enrolled and current');
});

it('shows an unreadable file as an alert, never as healthy', async () => {
  const container = await render({ reported: false, unreadable: true });
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('could not be read or parsed');
  expect(container.textContent).not.toContain('No status has been reported');
  expect(container.textContent).not.toContain('Enrolled and current');
});

it('reports an enrolled, verified installation as current', async () => {
  const container = await render(reported({}));
  expect(container.textContent).toContain('Enrolled and current');
  expect(container.textContent).not.toContain('stale');
});

it('names the target of an update in progress', async () => {
  const container = await render(reported({ phase: 'staged', applicable: ['HK-SA-1'], target_version: '1.4.3' }));
  expect(container.textContent).toContain('Update in progress: moving to 1.4.3');
});

it('shows the protection gap with its exact command and cause', async () => {
  const container = await render(reported({
    phase: 'blocked',
    reason: 'retry_backoff',
    protection_gap: true,
    applicable: ['HK-SA-1', 'HK-SA-2'],
    action: 'hikyo security-maintenance reconcile --confirm abc',
    detail: 'preparation failed',
  }));
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('Protection gap: 2 applicable advisories remain unremediated');
  expect(container.querySelector('code')?.textContent).toBe('hikyo security-maintenance reconcile --confirm abc');
  expect(container.textContent).toContain('preparation failed');
});

it('does not assume the coordinator runs on a host', async () => {
  const container = await render(reported({ phase: 'blocked', action: 'hikyo security-maintenance reconcile --confirm abc' }));
  expect(container.textContent).not.toMatch(/on the host/i);
  expect(container.textContent).toContain('Kubernetes install');
});

it('does not tell a Kubernetes install to look for a host file', async () => {
  const container = await render({ reported: false, unreadable: true });
  expect(container.textContent).not.toMatch(/on the host/i);
  expect(container.textContent).toContain('status ConfigMap');
});

it.each([
  ['expired metadata', { known_until: '2026-11-04T11:59:59Z' }],
  ['never verified', { last_verified: undefined }],
  ['the coordinator reporting stale', { phase: 'stale', reason: 'feed_unavailable' }],
])('withholds "current" and warns when status is stale: %s', async (_name, overrides) => {
  const container = await render(reported(overrides));
  expect(container.textContent).toContain('Security metadata is stale');
  expect(container.textContent).not.toContain('Enrolled and current');
});

it.each([403, 404])('treats a %i refusal as nondisclosure, never as a status', async (status) => {
  const container = await render({ error: { code: 'not_found', message: 'Not found' } }, { status });
  expect(container.textContent).toContain('not disclosed to this session');
  expect(container.textContent).not.toContain('Enrolled and current');
});

it('withholds "current" while advisories remain applicable', async () => {
  const container = await render(reported({ applicable: ['HK-SA-1'] }));
  expect(container.textContent).not.toContain('Enrolled and current');
  expect(container.textContent).toContain('HK-SA-1');
});

it('turns stale when the metadata expires while the panel is open', async () => {
  const knownUntil = new Date(Date.now() + 200).toISOString();
  const container = await render(reported({ last_verified: new Date().toISOString(), known_until: knownUntil }), undefined, null);
  expect(container.textContent).toContain('Enrolled and current');
  await vi.waitFor(() => expect(container.textContent).toContain('Security metadata is stale'), { timeout: 2_000 });
  expect(container.textContent).not.toContain('Enrolled and current');
});

it('hides the last report when a refresh fails', async () => {
  let fail = false;
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(
    fail ? Response.json({ error: { code: 'internal', message: 'boom' } }, { status: 500 }) : Response.json(reported({ action: 'hikyo security-maintenance reconcile --confirm abc' })),
  )));
  const { container, unmount, client } = await renderForm(<SecurityMaintenancePanel now={now} />);
  cleanups.push(unmount);
  await settleTask();
  expect(container.textContent).toContain('Enrolled and current');
  fail = true;
  await client.refetchQueries({ queryKey: ['security-maintenance'] });
  await settleTask();
  expect(container.textContent).toContain('could not be read');
  expect(container.textContent).not.toContain('Enrolled and current');
  expect(container.textContent).not.toContain('reconcile --confirm abc');
});
