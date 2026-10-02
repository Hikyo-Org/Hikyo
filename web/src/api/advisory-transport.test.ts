import { createClient, createConfig } from '@hikyo/runtime-core';
import { afterEach, expect, test, vi } from 'vitest';

import { watchProjectAdvisoryStream } from './advisory.ts';

afterEach(() => {
  vi.useRealTimers();
});

test('disposal settles immediately when fetch aborts before the generated retry sleep starts', async () => {
  vi.useFakeTimers();
  let started: (() => void) | undefined;
  const pendingFetch = new Promise<void>((resolve) => { started = resolve; });
  const fetch = vi.fn<typeof globalThis.fetch>((input) => {
    if (!(input instanceof Request)) throw new Error('Expected a generated Request');
    started?.();
    return new Promise<Response>((_resolve, reject) => {
      input.signal.addEventListener('abort', () => {
        reject(new DOMException('Request aborted', 'AbortError'));
      }, { once: true });
    });
  });
  const client = createClient(createConfig({ baseUrl: 'https://hikyo.test', fetch }));
  const handle = watchProjectAdvisoryStream(
    { org: 'org_a', project: 'project_a' },
    { client },
    { onEvent: vi.fn(), onState: vi.fn() },
  );
  await pendingFetch;

  // The real generated transport catches the rejected fetch AFTER this abort
  // event has fired, then enters its internal retry sleep with an aborted signal.
  let settled = false;
  const stopped = handle.stop().then(() => { settled = true; });
  await vi.advanceTimersByTimeAsync(0);
  expect(settled).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
  expect(fetch).toHaveBeenCalledTimes(1);
  await stopped;
});
