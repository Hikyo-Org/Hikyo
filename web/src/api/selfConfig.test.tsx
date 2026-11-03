// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { selfConfigPollMs, useSelfConfig, type SelfConfigStatus } from './selfConfig.ts';

const active: SelfConfigStatus = {
  owner_instance_id: 'instance_local', managed: true,
  binding: { org_id: 'org_system', project_id: 'prj_system', environment_id: 'env_system', schema_version: 1 },
  generation: 7n, desired_revision: 2n, latest_revision: 3n, state: 'active', nodes: [], job: null,
};
const wire = { ...active, generation: 7, desired_revision: 2, latest_revision: 3 };
const jobBase = { id: 'job', revision: 3n, generation: 8n, created_at: '2026-09-06T11:00:00Z' };

let cleanup: (() => Promise<void>) | undefined;
beforeEach(() => vi.useFakeTimers());
afterEach(async () => {
  await cleanup?.();
  cleanup = undefined;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function Binding() {
  useSelfConfig();
  return null;
}

function Live() {
  useSelfConfig({ live: true });
  return null;
}

/** Mounts the surfaces and returns a counter of the reads the network actually saw. */
async function mount(ui: React.JSX.Element, respond: () => Response = () => json({ ...wire })) {
  const fetcher = vi.fn(async () => respond());
  vi.stubGlobal('fetch', fetcher);
  const view = await renderForm(ui);
  cleanup = view.unmount;
  await settle();
  return () => fetcher.mock.calls.length;
}

function json(body: object, status = 200, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

async function advance(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
  await settle();
}

describe('self-config cadence', () => {
  it('polls quickly only while a rollout is unsettled', () => {
    expect(selfConfigPollMs(active)).toBe(30_000);
    expect(selfConfigPollMs(undefined)).toBe(30_000);
    expect(selfConfigPollMs({ ...active, state: 'recovery_required' })).toBe(30_000);
    expect(selfConfigPollMs({ ...active, state: 'pending' })).toBe(5_000);
    expect(selfConfigPollMs({ ...active, state: 'partial' })).toBe(5_000);
    expect(selfConfigPollMs({ ...active, job: { ...jobBase, state: 'preparing' } })).toBe(5_000);
    expect(selfConfigPollMs({ ...active, job: { ...jobBase, state: 'pending' } })).toBe(5_000);
    expect(selfConfigPollMs({ ...active, job: { ...jobBase, state: 'completed' } })).toBe(30_000);
  });

  it('reads once a minute by default, however many surfaces share the answer', async () => {
    const reads = await mount(<><Binding /><Binding /></>);
    expect(reads()).toBe(1);
    await advance(59_000);
    expect(reads()).toBe(1);
    await advance(1_000);
    expect(reads()).toBe(2);
  });

  it('keeps the live page current while a rollout settles, and that sets the shared cadence', async () => {
    const reads = await mount(<><Live /><Binding /></>, () => json({ ...wire, state: 'pending' }));
    expect(reads()).toBe(1);
    await advance(5_000);
    expect(reads()).toBe(2);
  });

  it('slows the live page once nothing is settling', async () => {
    const reads = await mount(<Live />);
    await advance(29_000);
    expect(reads()).toBe(1);
    await advance(1_000);
    expect(reads()).toBe(2);
  });

  it('resumes after a throttled read when told to, but stops at a refusal', async () => {
    const throttled = await mount(<Live />, () => json({ error: { code: 'too_many_requests', message: 'too many requests' } }, 429, { 'Retry-After': '8' }));
    expect(throttled()).toBe(1);
    await advance(8_000);
    expect(throttled()).toBe(2);
    await cleanup?.();
    cleanup = undefined;
    const denied = await mount(<Live />, () => json({ error: { code: 'forbidden', message: 'forbidden' } }, 403));
    expect(denied()).toBe(1);
    await advance(120_000);
    expect(denied()).toBe(1);
  });

  it('keeps its own cadence after a throttled read that names no wait', async () => {
    const reads = await mount(<Live />, () => json({ error: { code: 'too_many_requests', message: 'too many requests' } }, 429));
    expect(reads()).toBe(1);
    await advance(30_000);
    expect(reads()).toBe(2);
  });

  it('keeps polling after a throttled read that says to retry at once', async () => {
    const reads = await mount(<Live />, () => json({ error: { code: 'too_many_requests', message: 'too many requests' } }, 429, { 'Retry-After': '0' }));
    expect(reads()).toBe(1);
    await advance(30_000);
    expect(reads()).toBe(2);
  });

  it('refetches a fresh answer only when the live page mounts over it', async () => {
    const client = new QueryClient();
    client.setQueryData(['self-config'], active);
    const binding = await mount(<QueryClientProvider client={client}><Binding /></QueryClientProvider>);
    expect(binding()).toBe(0);
    await cleanup?.();
    cleanup = undefined;
    client.setQueryData(['self-config'], active);
    const live = await mount(<QueryClientProvider client={client}><Live /></QueryClientProvider>);
    expect(live()).toBe(1);
  });
});
