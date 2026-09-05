// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { ConnectionCredentials } from './Remotes.tsx';

const SENTINEL = 'hik_1_cn_SENTINEL_PLAINTEXT';

const cleanups: Array<() => Promise<void>> = [];

afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) {
    await cleanup();
  }
  vi.unstubAllGlobals();
});

const live = {
  id: 'icn_11111111-1111-1111-1111-111111111111',
  principal: 'mch_live',
  label: 'production peer',
  kind: 'hikyo-token',
  prefix_hint: 'hik_1_cn_abc',
  lifetime: 'finite',
  expires_at: '2027-01-01T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  created_by: 'usr_root',
  last_used_at: '2026-02-01T00:00:00Z',
  live: true,
};

const revoked = {
  id: 'icn_22222222-2222-2222-2222-222222222222',
  principal: 'mch_dead',
  label: 'retired peer',
  kind: 'hikyo-token',
  prefix_hint: 'hik_1_cn_xyz',
  lifetime: 'finite',
  created_at: '2026-01-01T00:00:00Z',
  created_by: 'usr_root',
  revoked_at: '2026-03-01T00:00:00Z',
  live: false,
};

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

type Handlers = {
  list?: readonly unknown[];
  mint?: () => Response;
  revoke?: () => Response;
};

function stubFetch(handlers: Handlers) {
  const fetchMock = vi.fn((request: RequestInfo | URL) => {
    const url = request instanceof Request ? request.url : String(request);
    const method = request instanceof Request ? request.method : 'GET';
    if (url.endsWith('/api/v1/instance/connections') && method === 'GET') {
      const items = handlers.list ?? [];
      return Promise.resolve(jsonResponse({ items, count: items.length }, 200));
    }
    if (url.endsWith('/api/v1/instance/connections') && method === 'POST') {
      return Promise.resolve((handlers.mint ?? (() => jsonResponse(minted(), 201)))());
    }
    if (url.includes('/api/v1/instance/connections/') && method === 'DELETE') {
      return Promise.resolve((handlers.revoke ?? (() => new Response(null, { status: 204 })))());
    }
    throw new Error(`unexpected request: ${method} ${url}`);
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function minted(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return { value: SENTINEL, connection: live, clamped: false, ...overrides };
}

async function render(handlers: Handlers) {
  const fetchMock = stubFetch(handlers);
  const rendered = await renderForm(<ConnectionCredentials />);
  cleanups.push(rendered.unmount);
  await settleTask();
  return { ...rendered, fetchMock };
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const found = Array.from(container.querySelectorAll('button')).find(
    (candidate) => candidate.textContent === text,
  );
  if (found === undefined) {
    throw new Error(`no button labelled "${text}"`);
  }
  return found;
}

function postBody(fetchMock: ReturnType<typeof stubFetch>): Promise<unknown> {
  const request = fetchMock.mock.calls
    .map(([candidate]) => candidate)
    .find((candidate) => candidate instanceof Request && candidate.method === 'POST');
  if (!(request instanceof Request)) {
    throw new Error('no POST request was sent');
  }
  return request.json();
}

async function submitMintForm(container: HTMLElement, label: string): Promise<void> {
  const labelInput = container.querySelector<HTMLInputElement>('#connection-label');
  if (labelInput === null) {
    throw new Error('the mint form has no label input');
  }
  await act(async () => typeInto(labelInput, label));
  await act(async () => {
    button(container, 'Mint credential').click();
  });
  await settleTask();
}

describe('ConnectionCredentials inventory', () => {
  it('renders bounded metadata and the live/revoked state without plaintext', async () => {
    const { container } = await render({ list: [live, revoked] });

    expect(container.textContent).toContain('production peer');
    expect(container.textContent).toContain('retired peer');
    expect(container.querySelector('.connection__prefix')?.textContent).toContain('hik_1_cn_abc');
    expect(container.querySelector('[data-state="live"]')?.textContent).toBe('live');
    expect(container.querySelector('[data-state="revoked"]')?.textContent).toBe('revoked');
    // A revoked entry offers no revoke button; a live one does.
    const revokeButtons = Array.from(container.querySelectorAll('button')).filter(
      (candidate) => candidate.textContent === 'Revoke',
    );
    expect(revokeButtons).toHaveLength(1);
  });

  it('shows the empty state when nothing is minted', async () => {
    const { container } = await render({ list: [] });
    expect(container.textContent).toContain('None minted.');
  });
});

describe('ConnectionCredentials mint', () => {
  it('mints with the default lifetime and names no lifetime field', async () => {
    const { container, fetchMock } = await render({ list: [] });
    await submitMintForm(container, 'a new peer');

    expect(await postBody(fetchMock)).toEqual({ label: 'a new peer' });
    expect(container.querySelector('.machine__token')?.textContent).toBe(SENTINEL);
  });

  it('sends lifetime_seconds when a custom lifetime is chosen, and never indefinite too', async () => {
    const { container, fetchMock } = await render({ list: [] });
    await act(async () => {
      (container.querySelector('#lifetime-custom') as HTMLInputElement).click();
    });
    await act(async () => typeInto(container.querySelector('#lifetime-days')!, '7'));
    await submitMintForm(container, 'weekly peer');

    const body = (await postBody(fetchMock)) as Record<string, unknown>;
    expect(body).toEqual({ label: 'weekly peer', lifetime_seconds: 7 * 86_400 });
    expect('indefinite' in body).toBe(false);
  });

  it('sends indefinite alone when chosen', async () => {
    const { container, fetchMock } = await render({ list: [] });
    await act(async () => {
      (container.querySelector('#lifetime-indefinite') as HTMLInputElement).click();
    });
    await submitMintForm(container, 'forever peer');

    const body = (await postBody(fetchMock)) as Record<string, unknown>;
    expect(body).toEqual({ label: 'forever peer', indefinite: true });
    expect('lifetime_seconds' in body).toBe(false);
  });

  it('keeps the value out of the query and mutation caches, and gone from the DOM once stored', async () => {
    const { container, client } = await render({ list: [] });
    await submitMintForm(container, 'a new peer');

    // The value lives only in the dialog. It is reset out of the mutation cache
    // and never entered the query cache.
    const mutationData = JSON.stringify(
      client.getMutationCache().getAll().map((mutation) => mutation.state.data),
    );
    expect(mutationData).not.toContain(SENTINEL);
    expect(JSON.stringify(client.getQueryCache().getAll())).not.toContain(SENTINEL);

    // Done is refused until stored is confirmed, the value has no second look.
    await act(async () => button(container, 'Done').click());
    await settle();
    expect(container.querySelector('.machine__token')?.textContent).toBe(SENTINEL);
    expect(container.textContent).toContain('Confirm you have stored it');

    await act(async () => {
      (container.querySelector('#connection-stored') as HTMLInputElement).click();
    });
    await act(async () => button(container, 'Done').click());
    await settle();
    expect(container.querySelector('.machine__token')).toBeNull();
    expect(container.textContent).not.toContain(SENTINEL);
  });

  it('refuses a sub-second custom lifetime without sending it to the server', async () => {
    const { container, fetchMock } = await render({ list: [] });
    await act(async () => {
      (container.querySelector('#lifetime-custom') as HTMLInputElement).click();
    });
    await act(async () => typeInto(container.querySelector('#lifetime-days')!, '0.000001'));
    await act(async () => typeInto(container.querySelector('#connection-label')!, 'too small'));

    const mintButton = button(container, 'Mint credential');
    expect(mintButton.disabled).toBe(true);
    expect(container.querySelector('#lifetime-days')?.getAttribute('aria-invalid')).toBe('true');
    await act(async () => mintButton.click());
    await settleTask();
    // Only the initial inventory GET ran; no mint POST was sent.
    expect(fetchMock.mock.calls.every(([c]) => !(c instanceof Request) || c.method === 'GET')).toBe(
      true,
    );
  });

  it('surfaces the both-lifetime / indefinite-disallowed refusal from a 400', async () => {
    const { container } = await render({
      list: [],
      mint: () => new Response(null, { status: 400 }),
    });
    await submitMintForm(container, 'bad peer');
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('That mint was refused');
    expect(container.querySelector('.machine__token')).toBeNull();
  });
});

describe('ConnectionCredentials revoke', () => {
  it('states the active-session and remote-trust consequence before committing', async () => {
    const { container } = await render({ list: [live] });
    await act(async () => button(container, 'Revoke').click());
    await settle();

    const dialog = container.ownerDocument.querySelector('dialog');
    expect(dialog?.textContent).toContain('credential rejected');
    expect(dialog?.textContent).toContain('Active workspace sessions');
    expect(dialog?.textContent).toContain('unaffected');
  });

  it('reports a double revoke as a 409 rather than a silent success', async () => {
    const { container } = await render({
      list: [live],
      revoke: () => new Response(null, { status: 409 }),
    });
    await act(async () => button(container, 'Revoke').click());
    await settle();
    await act(async () => button(container, 'Revoke credential').click());
    await settleTask();

    expect(container.ownerDocument.querySelector('dialog')?.textContent).toContain(
      'already revoked',
    );
  });

  it('closes the dialog on a successful revoke', async () => {
    const { container } = await render({ list: [live] });
    await act(async () => button(container, 'Revoke').click());
    await settle();
    await act(async () => button(container, 'Revoke credential').click());
    await settleTask();

    expect(container.ownerDocument.querySelector('dialog')).toBeNull();
  });
});
