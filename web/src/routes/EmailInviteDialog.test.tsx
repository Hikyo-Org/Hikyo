// @vitest-environment happy-dom
import { client } from '@hikyo/runtime';
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { MAILER_UNCONFIGURED_TEXT } from '../api/emailInvitations.ts';
import { renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { EmailInvitations } from './EmailInvitations.tsx';
import { EmailInviteDialog } from './EmailInviteDialog.tsx';

const mocks = vi.hoisted(() => ({ send: vi.fn() }));
vi.mock('../api/emailInvitations.ts', async (original) => ({
  ...(await original<typeof import('../api/emailInvitations.ts')>()),
  sendEmailInvitation: mocks.send,
}));

const org = 'org_11111111-1111-4111-8111-111111111111';
const inviter = 'prn_22222222-2222-4222-8222-222222222222';
const invitation = 'eiv_33333333-3333-4333-8333-333333333333';

afterEach(() => {
  mocks.send.mockReset();
  vi.unstubAllGlobals();
});

function mount() {
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const rendered = renderForm(<EmailInviteDialog org={org} scopeName="Acme" onDone={onDone} onCancel={onCancel} />);
  return { rendered, onDone, onCancel };
}

function control<T extends Element>(root: ParentNode, selector: string, kind: new () => T): T {
  const found = root.querySelector(selector);
  if (!(found instanceof kind)) throw new Error(`${selector} is missing`);
  return found;
}

async function fill(root: HTMLElement, email: string, template: string) {
  await act(async () => {
    if (email !== '') typeInto(control(root, 'input[name="email"]', HTMLInputElement), email);
    const select = control(root, 'select', HTMLSelectElement);
    select.value = template;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await act(async () => {
    control(root, 'button[type="submit"]', HTMLButtonElement).click();
    await Promise.resolve();
  });
  await settle();
}

describe('EmailInviteDialog', () => {
  it('requires a role template and offers the organisation-level choices', async () => {
    const { rendered } = mount();
    const { container, unmount } = await rendered;
    const options = [...container.querySelectorAll('option')].map((option) => option.value);
    expect(options).toContain('viewer');
    expect(options).toContain('admin');
    expect(options).not.toContain('operator');
    await fill(container, 'dana@example.com', '');
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('Choose a role template');
    expect(mocks.send).not.toHaveBeenCalled();
    await unmount();
  });

  it('shows the same neutral copy for any address, never implying a match', async () => {
    mocks.send.mockResolvedValue(undefined);
    const copies: string[] = [];
    for (const address of ['dana@example.com', 'nobody@nowhere.invalid']) {
      const { rendered, onDone } = mount();
      const { container, unmount } = await rendered;
      await fill(container, ` ${address} `, 'editor');
      expect(mocks.send).toHaveBeenLastCalledWith(org, { email: address, template: 'editor' });
      const text = container.querySelector('dialog')?.textContent ?? '';
      expect(text).toContain('is recorded');
      expect(text).toContain('If the address belongs to an account with a verified email');
      expect(text).not.toContain(address);
      expect(text).not.toMatch(/\b(found|sent to)\b/i);
      copies.push(text);
      const close = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Close');
      await act(async () => close?.click());
      expect(onDone).toHaveBeenCalledTimes(1);
      await unmount();
    }
    expect(copies[0]).toBe(copies[1]);
  });

  it('explains that email invitations need a configured mail server and names the alternatives', async () => {
    mocks.send.mockRejectedValue(new ApiError(400, 'bad', 'mailer-unconfigured'));
    const { rendered, onDone } = mount();
    const { container, unmount } = await rendered;
    await fill(container, 'dana@example.com', 'viewer');
    const alert = container.querySelector('[role="alert"]')?.textContent ?? '';
    expect(alert).toBe(MAILER_UNCONFIGURED_TEXT);
    for (const alternative of ['Invite', 'SCIM', 'administrator of both organisations']) expect(alert).toContain(alternative);
    expect(container.querySelector('form')).not.toBeNull();
    expect(onDone).not.toHaveBeenCalled();
    await unmount();
  });

  it('shows the uniform throttle answer on 429', async () => {
    mocks.send.mockRejectedValue(new ApiError(429, 'slow'));
    const { rendered } = mount();
    const { container, unmount } = await rendered;
    await fill(container, 'dana@example.com', 'viewer');
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('Too many requests right now');
    await unmount();
  });
});

describe('EmailInviteDialog list refresh', () => {
  const json = (data: object) =>
    new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } });

  beforeEach(() => client.setConfig({ baseUrl: 'https://hikyo.test' }));

  it('shows the new invitation in the pending list once it is recorded', async () => {
    let sent = false;
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => {
      const items = sent ? [{
        id: invitation, email: 'dana@example.com', template: 'editor', invited_by: inviter,
        created_at: '2026-11-06T10:00:00Z', expires_at: '2026-11-13T10:00:00Z',
      }] : [];
      return json({ items, count: items.length });
    }));
    mocks.send.mockImplementation(async () => { sent = true; });
    const view = await renderForm(<>
      <EmailInvitations org={org} invitedByName={() => 'Alex'} onRevoked={vi.fn()} />
      <EmailInviteDialog org={org} scopeName="Acme" onDone={vi.fn()} onCancel={vi.fn()} />
    </>);
    try {
      await settleTask();
      expect(view.container.textContent).toContain('No pending email invitations.');
      await fill(view.container, 'dana@example.com', 'editor');
      await settleTask();
      expect(view.container.querySelector('tbody tr')?.textContent).toContain('dana@example.com');
    } finally { await view.unmount(); }
  });
});

describe('EmailInvitations', () => {
  const pending = (email: string, template: string, id: string) => ({
    id, email, template, invited_by: inviter, created_at: '2026-11-06T10:00:00Z', expires_at: '2026-11-13T10:00:00Z',
  });

  it('filters the pending list by email and says so when nothing matches', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(JSON.stringify({
      count: 2,
      items: [pending('dana@example.com', 'editor', invitation), pending('lee@example.com', 'viewer', 'eiv_44444444-4444-4444-8444-444444444444')],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })));
    const view = await renderForm(<EmailInvitations org={org} invitedByName={() => 'Alex'} onRevoked={vi.fn()} />);
    try {
      await settleTask();
      const rows = () => [...view.container.querySelectorAll('tbody tr')].map((row) => row.textContent ?? '');
      expect(rows()).toHaveLength(2);
      const input = control(view.container, 'input[type="search"]', HTMLInputElement);
      await act(async () => typeInto(input, 'LEE'));
      expect(rows()).toHaveLength(1);
      expect(rows()[0]).toContain('lee@example.com');
      await act(async () => typeInto(input, 'nobody'));
      expect(rows()).toHaveLength(0);
      expect(view.container.textContent).toContain('No invitation matches “nobody”.');
    } finally { await view.unmount(); }
  });
});

describe('EmailInvitations list', () => {
  const json = (data: object, status = 200) =>
    new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });

  beforeEach(() => client.setConfig({ baseUrl: 'https://hikyo.test' }));

  it('lists pending invitations and revokes one', async () => {
    let revoked = false;
    const requests: string[] = [];
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (input, init) => {
      const request = input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      requests.push(`${request.method} ${path}`);
      if (request.method === 'DELETE') {
        revoked = true;
        return new Response(null, { status: 204 });
      }
      const items = revoked ? [] : [{
        id: invitation, email: 'dana@example.com', template: 'editor', invited_by: inviter,
        created_at: '2026-11-06T10:00:00Z', expires_at: '2026-11-13T10:00:00Z',
      }];
      return json({ items, count: items.length });
    }));
    const onRevoked = vi.fn();
    const view = await renderForm(<EmailInvitations org={org} invitedByName={() => 'Alex'} onRevoked={onRevoked} />);
    try {
      await settleTask();
      const row = view.container.querySelector('tbody tr')?.textContent ?? '';
      for (const cell of ['dana@example.com', 'editor', 'Alex']) expect(row).toContain(cell);
      const revoke = view.container.querySelector<HTMLButtonElement>('button[aria-label="Revoke the invitation for dana@example.com"]');
      await act(async () => revoke?.click());
      await settleTask();
      expect(requests).toContain(`DELETE /api/v1/orgs/${org}/email-invitations/${invitation}`);
      expect(onRevoked).toHaveBeenCalledWith('dana@example.com');
      expect(view.container.textContent).toContain('No pending email invitations.');
    } finally { await view.unmount(); }
  });
});
