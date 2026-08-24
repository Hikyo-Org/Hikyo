// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, test, vi } from 'vitest';

import { forgetWorkspace, rememberWorkspace } from './workspace.ts';
import { useRemoteUpdateJob } from './updates.ts';

const origin = 'https://remote.example';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

afterEach(() => {
  forgetWorkspace(origin);
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
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
