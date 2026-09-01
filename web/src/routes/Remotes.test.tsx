// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { InstanceUpdateJob } from '../api/updates.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { AddRemote, UpdateJobStatus } from './Remotes.tsx';

const cleanups: Array<() => Promise<void>> = [];

afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) {
    await cleanup();
  }
  vi.unstubAllGlobals();
});

const peer = {
  id: 'rmt_123e4567-e89b-12d3-a456-426614174000',
  name: 'production',
  url: 'https://peer.example/',
  spki_pin: 'peer-pin',
  created_at: '2026-01-01T00:00:00Z',
  created_by: 'principal-1',
  state: 'ok',
  last_attempt_at: '2026-01-01T00:00:00Z',
  stale: false,
};

function remoteList(items: readonly (typeof peer)[]): Response {
  return new Response(JSON.stringify({ items, count: items.length }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

async function renderAddRemote(items: readonly (typeof peer)[]) {
  const fetchMock = vi.fn((request: RequestInfo | URL) => {
    const method = request instanceof Request ? request.method : 'GET';
    if (method === 'GET') {
      return Promise.resolve(remoteList(items));
    }
    return Promise.resolve(
      new Response(JSON.stringify({ ...peer, url: 'https://new.example' }), {
        status: 201,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
  });
  vi.stubGlobal('fetch', fetchMock);

  const rendered = await renderForm(<AddRemote />);
  cleanups.push(rendered.unmount);
  await settleTask();
  return { container: rendered.container, fetchMock };
}

function input(container: HTMLElement, id: string): HTMLInputElement {
  const found = container.querySelector(`#${id}`);
  if (!(found instanceof HTMLInputElement)) {
    throw new Error(`input ${id} is missing`);
  }
  return found;
}

async function submit(container: HTMLElement): Promise<void> {
  const form = container.querySelector('form');
  if (!(form instanceof HTMLFormElement)) {
    throw new Error('the add-remote form is missing');
  }
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await settleTask();
}

describe('UpdateJobStatus', () => {
  function updateJob(overrides: Partial<InstanceUpdateJob>): InstanceUpdateJob {
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

  it('renders a rollback-failed job as an alert carrying the failure code', async () => {
    const rendered = await renderForm(
      <UpdateJobStatus
        jobID="job_9"
        job={updateJob({ state: 'rollback-failed', failure_code: 'health_probe' })}
      />,
    );
    cleanups.push(rendered.unmount);

    const alert = rendered.container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain('job_9');
    expect(alert?.textContent).toContain('rollback-failed');
    expect(alert?.textContent).toContain('health_probe');
  });

  it('renders a succeeded job as a plain status line, not an alert', async () => {
    const rendered = await renderForm(
      <UpdateJobStatus jobID="job_9" job={updateJob({ state: 'succeeded', phase: 'complete' })} />,
    );
    cleanups.push(rendered.unmount);

    expect(rendered.container.querySelector('[role="alert"]')).toBeNull();
    expect(rendered.container.textContent).toContain('succeeded');
    expect(rendered.container.textContent).toContain('(complete)');
  });
});

describe('AddRemote', () => {
  it('blocks an existing origin before starting the mutation', async () => {
    const { container, fetchMock } = await renderAddRemote([peer]);
    const url = input(container, 'remote-url');

    expect(url.type).toBe('url');
    await act(async () => typeInto(url, '  https://peer.example  '));
    await submit(container);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'This origin is already added as production.',
    );
  });

  it.each([
    'hikyo.example',
    'http://hikyo.example',
    'https://hikyo.example/a/..',
    'https://@hikyo.example',
  ])(
    'blocks invalid remote URL %s before starting the mutation',
    async (rawURL) => {
      const { container, fetchMock } = await renderAddRemote([]);

      await act(async () => typeInto(input(container, 'remote-url'), rawURL));
      await submit(container);

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(container.querySelector('[role="alert"]')?.textContent).toContain(
        'Enter a bare HTTPS origin',
      );
    },
  );

  it('trims a valid origin before posting it', async () => {
    const { container, fetchMock } = await renderAddRemote([]);
    await act(async () => {
      typeInto(input(container, 'remote-name'), 'new-peer');
      typeInto(input(container, 'remote-url'), '  https://new.example/  ');
      typeInto(input(container, 'remote-pin'), 'new-pin');
      typeInto(input(container, 'remote-credential'), 'new-credential');
    });
    await submit(container);

    const request = fetchMock.mock.calls
      .map(([candidate]) => candidate)
      .find((candidate) => candidate instanceof Request && candidate.method === 'POST');
    if (!(request instanceof Request)) {
      throw new Error('the add mutation did not send a Request');
    }
    expect(await request.json()).toMatchObject({ url: 'https://new.example' });
  });
});
