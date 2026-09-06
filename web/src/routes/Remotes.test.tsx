// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { InstanceUpdateJob } from '../api/updates.ts';
import { MemoryRouter } from 'react-router';
import { forgetWorkspace, rememberWorkspace } from '../api/workspace.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { AddRemote, Remotes, ThisInstance, UpdateJobStatus } from './Remotes.tsx';

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

async function renderAddRemote(items: readonly (typeof peer)[], refusal?: string) {
  const fetchMock = vi.fn((request: RequestInfo | URL) => {
    const method = request instanceof Request ? request.method : 'GET';
    if (method === 'GET') {
      const url = request instanceof Request ? request.url : String(request);
      if (new URL(url).pathname === '/api/v1/meta') return Promise.resolve(Response.json({ instance_identity: 'local-instance' }));
      return Promise.resolve(remoteList(items));
    }
    if (refusal !== undefined) return Promise.resolve(Response.json({ error: { code: 'conflict', message: 'Conflict', detail: refusal } }, { status: 409 }));
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
        job={updateJob({ state: 'rollback-failed', phase: 'rollback', failure_code: 'health_probe' })}
      />,
    );
    cleanups.push(rendered.unmount);

    const alert = rendered.container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain('job_9');
    expect(alert?.textContent).toContain('rollback-failed');
    expect(alert?.textContent).toContain('(rollback)');
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
  it('names the identity refusal when another URL resolves to this instance', async () => {
    const { container } = await renderAddRemote([], 'self_connected');
    expect(container.textContent).toContain('local-instance');
    await act(async () => {
      typeInto(input(container, 'remote-name'), 'alias');
      typeInto(input(container, 'remote-url'), 'https://alias.example');
      typeInto(input(container, 'remote-pin'), 'pin');
      typeInto(input(container, 'remote-credential'), 'credential');
    });
    await submit(container);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('That is this instance. A remote must be another instance.');
  });

  it('blocks an existing origin before starting the mutation', async () => {
    const { container, fetchMock } = await renderAddRemote([peer]);
    const url = input(container, 'remote-url');

    expect(url.type).toBe('url');
    await act(async () => typeInto(url, '  https://peer.example  '));
    await submit(container);

    expect(fetchMock.mock.calls.filter(([request]) => request instanceof Request && request.method === 'POST')).toHaveLength(0);
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

      expect(fetchMock.mock.calls.filter(([request]) => request instanceof Request && request.method === 'POST')).toHaveLength(0);
      expect(container.querySelector('[role="alert"]')?.textContent).toContain(
        'Enter a bare HTTPS origin',
      );
    },
  );

  it('refuses this instance as its own remote before starting the mutation', async () => {
    const { container, fetchMock } = await renderAddRemote([]);
    vi.stubGlobal('location', new URL('https://self.example/remotes'));
    await act(async () => typeInto(input(container, 'remote-url'), 'https://self.example'));
    await submit(container);

    expect(fetchMock.mock.calls.filter(([request]) => request instanceof Request && request.method === 'POST')).toHaveLength(0);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'That is this instance. A remote must be another instance.',
    );
  });

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


describe('ThisInstance', () => {
  it('uses the own-directory endpoint and discards visible stale metadata after a forbidden refresh', async () => {
    let forbidden = false;
    const paths: string[] = [];
    vi.stubGlobal('fetch', async (request: Request) => {
      paths.push(new URL(request.url).pathname);
      return new Response(JSON.stringify(forbidden ? { error: { code: 'forbidden', message: 'forbidden' } } : {
        identity: 'opaque-instance', version: 'v1.0.0', org_count: 1, project_count: 2,
        orgs: [{ name: 'Example', projects: ['Billing', 'Portal'] }],
      }), { status: forbidden ? 403 : 200, headers: { "Content-Type": "application/json" } });
    });
    const rendered = await renderForm(<ThisInstance />);
    cleanups.push(rendered.unmount);
    await settleTask();
    expect(rendered.container.textContent).toContain('opaque-instance');
    expect(rendered.container.textContent).toContain('Billing, Portal');
    expect(paths).toEqual(['/api/v1/instance/directory']);
    forbidden = true;
    await act(async () => { await rendered.client.invalidateQueries({ queryKey: ['instance-directory'] }); });
    await settleTask();
    expect(rendered.container.querySelector('[role="alert"]')?.textContent).toContain('You do not hold instance-directory');
    expect(rendered.container.textContent).not.toContain('opaque-instance');
    expect(rendered.container.querySelector('dl')).toBeNull();
  });
});

describe('RemoteCard', () => {
  async function renderRemotes(items: readonly Record<string, unknown>[]) {
    vi.stubGlobal('fetch', (request: Request) => {
      const path = new URL(request.url).pathname;
      const body = path === '/api/v1/instance/remotes' ? { items, count: items.length } : { items: [], count: 0 };
      return Promise.resolve(
        new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }),
      );
    });
    const rendered = await renderForm(<MemoryRouter><Remotes /></MemoryRouter>);
    cleanups.push(rendered.unmount);
    await settleTask();
    return rendered.container;
  }

  it('renders the state as a badge label and folds staleness into one sentence', async () => {
    const container = await renderRemotes([
      { ...peer, state: 'unreachable', stale: true, stale_for_seconds: 7200, identity: 'inst-a' },
    ]);
    const card = container.querySelector('.remote');
    expect(card?.querySelector('.badge[data-state="unreachable"]')?.textContent).toBe('Unreachable');
    expect(card?.querySelector('.remote__stale')?.textContent).toBe(
      'Unreachable for 2 hours. Showing the last known directory.',
    );
    for (const alert of card?.querySelectorAll('[role="alert"]') ?? []) {
      expect(alert.textContent).not.toContain('Unreachable');
    }
    expect(card?.querySelectorAll('.values__absent')).toHaveLength(3);
  });

  it('gives a rejected credential its own recovery sentence naming the peer', async () => {
    const container = await renderRemotes([{ ...peer, state: 'credential-rejected' }]);
    expect(container.querySelector('.badge[data-state="credential-rejected"]')?.textContent).toBe(
      'Credential rejected',
    );
    expect(container.querySelector('.remote [role="alert"]')?.textContent).toContain(
      'The pinned credential was rejected. Mint a new connection credential on production and replace it here.',
    );
  });

  it('marks both entries sharing an identity and refuses to open either', async () => {
    const container = await renderRemotes([
      { ...peer, identity: 'inst-a' },
      { ...peer, id: 'rmt_223e4567-e89b-12d3-a456-426614174000', name: 'mirror', url: 'https://mirror.example/', identity: 'inst-a' },
      { ...peer, id: 'rmt_323e4567-e89b-12d3-a456-426614174000', name: 'other', url: 'https://other.example/', identity: 'inst-b' },
    ]);
    const badges = [...container.querySelectorAll('.badge[data-state="duplicate-identity"]')];
    expect(badges.map((badge) => badge.textContent)).toEqual(['Duplicate identity', 'Duplicate identity']);
    const cards = [...container.querySelectorAll('.remote')];
    const launcher = (card: Element) =>
      [...card.querySelectorAll('button')].find((button) => !['Rename', 'Remove'].includes(button.textContent ?? ''));
    expect(launcher(cards[0]!)?.disabled).toBe(true);
    expect(launcher(cards[1]!)?.disabled).toBe(true);
    expect(launcher(cards[2]!)?.disabled).toBe(false);
  });

  it('withdraws the open badge and picker from a duplicated entry that still holds a bearer', async () => {
    rememberWorkspace({
      origin: 'https://peer.example',
      value: 'bearer-1',
      session: 'session-1',
      idleExpiresAt: '2099-01-01T00:00:00Z',
      absoluteExpiresAt: '2099-01-01T01:00:00Z',
    });
    cleanups.push(() => {
      forgetWorkspace('https://peer.example');
      return Promise.resolve();
    });
    const container = await renderRemotes([
      { ...peer, identity: 'inst-a' },
      { ...peer, id: 'rmt_223e4567-e89b-12d3-a456-426614174000', name: 'mirror', url: 'https://mirror.example/', identity: 'inst-a' },
    ]);
    const card = container.querySelector('.remote');
    expect(card?.querySelector('.remote__actions .badge')).toBeNull();
    expect(card?.querySelector('.remote__picker')).toBeNull();
    expect([...(card?.querySelectorAll('button') ?? [])].map((b) => b.textContent)).toContain('Close workspace');
  });
});
