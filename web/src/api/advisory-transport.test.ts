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
    expect(input.headers.get('Accept')).toBe('text/event-stream; hikyo-topology=1');
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

test('opts in through the generated remote transport and dispatches project-only frames', async () => {
  const onEvent = vi.fn();
  let delivered: (() => void) | undefined;
  const received = new Promise<void>((resolve) => { delivered = resolve; });
  const fetch = vi.fn<typeof globalThis.fetch>((input) => {
    if (!(input instanceof Request)) throw new Error('Expected a generated Request');
    expect(input.url).toBe('https://remote.test/api/v1/orgs/org_a/projects/project_a/events');
    expect(input.headers.get('Accept')).toBe('text/event-stream; hikyo-topology=1');
    return Promise.resolve(new Response(
      'event: future.project.event\ndata: {"type":"future.project.event"}\n\n' +
      'event: environments.changed\ndata: {"type":"environments.changed","project_id":"project_a"}\n\n',
      { headers: { 'Content-Type': 'text/event-stream' } },
    ));
  });
  const client = createClient(createConfig({ baseUrl: 'https://remote.test', fetch }));
  const handle = watchProjectAdvisoryStream(
    { org: 'org_a', project: 'project_a' }, { client },
    { onState: vi.fn(), onEvent: (event) => { onEvent(event); delivered?.(); } },
  );
  await received;
  await handle.stop();
  expect(onEvent).toHaveBeenCalledExactlyOnceWith({ type: 'environments.changed', projectId: 'project_a' });
  expect(fetch).toHaveBeenCalledTimes(1);
});
