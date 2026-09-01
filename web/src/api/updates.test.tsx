// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, test, vi } from 'vitest';

import { forgetWorkspace, rememberWorkspace } from './workspace.ts';
import {
  type InstanceUpdateJob,
  jobReadErrorVisible,
  updateJobOutcome,
  useRemoteUpdateJob,
  useServerVersion,
} from './updates.ts';

const origin = 'https://remote.example';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

afterEach(() => {
  forgetWorkspace(origin);
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

function job(overrides: Partial<InstanceUpdateJob>): InstanceUpdateJob {
  return {
    id: 'job_1',
    backend: 'flux',
    version: '1.4.0',
    state: 'queued',
    phase: 'queued',
    requested_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

test('updateJobOutcome collapses the six job states into three outcomes', () => {
  expect(updateJobOutcome(job({ state: 'queued' })).kind).toBe('running');
  expect(updateJobOutcome(job({ state: 'running' })).kind).toBe('running');
  expect(updateJobOutcome(job({ state: 'succeeded' })).kind).toBe('succeeded');
  expect(updateJobOutcome(job({ state: 'failed' })).kind).toBe('failed');
  expect(updateJobOutcome(job({ state: 'rolled-back' })).kind).toBe('failed');
  expect(updateJobOutcome(job({ state: 'rollback-failed' })).kind).toBe('failed');
});

test('updateJobOutcome passes failure_code through on a terminal failure', () => {
  expect(updateJobOutcome(job({ state: 'rollback-failed', failure_code: 'health_probe' }))).toEqual(
    { kind: 'failed', failureCode: 'health_probe' },
  );
  // A failure without a code carries no failureCode rather than an empty string.
  expect(updateJobOutcome(job({ state: 'failed' })).failureCode).toBeUndefined();
  // Success and in-flight outcomes never carry a code.
  expect(updateJobOutcome(job({ state: 'succeeded', failure_code: 'ignored' }))).toEqual({
    kind: 'succeeded',
  });
});

test('jobReadErrorVisible suppresses the read-error alert behind a terminal failure', () => {
  // No error → never shown, whatever is cached.
  expect(jobReadErrorVisible(false, undefined)).toBe(false);
  expect(jobReadErrorVisible(false, job({ state: 'failed' }))).toBe(false);
  // Error with no cached job, or a non-failure cached job → shown.
  expect(jobReadErrorVisible(true, undefined)).toBe(true);
  expect(jobReadErrorVisible(true, job({ state: 'succeeded' }))).toBe(true);
  // Error while a terminal-failure read is still cached → suppressed; the
  // failure alert already covers it, so the two must not double up.
  expect(jobReadErrorVisible(true, job({ state: 'rollback-failed' }))).toBe(false);
});

test('server version reads server_version from the contract meta endpoint', async () => {
  const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    const path = new URL(request.url, 'http://localhost').pathname;
    if (path === '/api/v1/meta') {
      return Promise.resolve(
        new Response(
          JSON.stringify({ server_version: '1.4.0', api_revision: 7, protocol_capabilities: [] }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      );
    }
    return Promise.resolve(new Response(null, { status: 404 }));
  });
  vi.stubGlobal('fetch', fetchMock);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  function Probe() {
    const version = useServerVersion();
    return <span>{version.data ?? ''}</span>;
  }

  try {
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <Probe />
        </QueryClientProvider>,
      );
    });
    // Wait on the query settling with a real-timer poll rather than a fixed
    // count of microtask flushes: react-query commits on its own schedule, so a
    // bounded flush loop can read '' before the value lands (it did, under CI
    // timing). vi.waitFor retries against the wall clock until the value arrives.
    await vi.waitFor(
      async () => {
        await act(async () => {
          await Promise.resolve();
        });
        expect(container.textContent).toBe('1.4.0');
      },
      { timeout: 2_000, interval: 20 },
    );
    const metaCall = fetchMock.mock.calls
      .map(([input]) => (input instanceof Request ? input : new Request(input)))
      .find((request) => new URL(request.url, 'http://localhost').pathname === '/api/v1/meta');
    expect(metaCall).toBeDefined();
  } finally {
    await act(async () => {
      root.unmount();
    });
    client.clear();
  }
});

test('remote update-job polling stops after the initial status read errors', async () => {
  vi.useFakeTimers();
  rememberWorkspace({
    origin,
    value: 'secret',
    session: 'ses_1',
    idleExpiresAt: '2099-01-01T00:00:00Z',
    absoluteExpiresAt: '2099-01-01T00:00:00Z',
  });
  const fetchMock = vi.fn(() =>
    Promise.resolve(
      new Response('{"message":"unavailable"}', {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  );
  vi.stubGlobal('fetch', fetchMock);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);

  function Probe() {
    useRemoteUpdateJob(origin, 'job_1');
    return null;
  }

  try {
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <Probe />
        </QueryClientProvider>,
      );
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
  } finally {
    await act(async () => {
      root.unmount();
    });
    client.clear();
  }
});
