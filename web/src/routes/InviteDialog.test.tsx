// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { InviteDialog } from './InviteDialog.tsx';

const mocks = vi.hoisted(() => ({
  inviteMember: vi.fn(),
  writeClipboard: vi.fn(),
}));

vi.mock('../api/access.ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/access.ts')>()),
  inviteMember: mocks.inviteMember,
}));
vi.mock('../app/clipboard.ts', () => ({ writeClipboard: mocks.writeClipboard }));

const AUTHORITY = 'hik_cea_SENTINEL_AUTHORITY_VALUE';

afterEach(() => {
  mocks.inviteMember.mockReset();
});

function mount(scope: { kind: 'org'; org: string } | { kind: 'instance' }) {
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const rendered = renderForm(
    <MemoryRouter>
      <InviteDialog
        scope={scope}
        scopeName="Acme"
        origin="https://hikyo.example"
        onDone={onDone}
        onCancel={onCancel}
      />
    </MemoryRouter>,
  );
  return { rendered, onDone, onCancel };
}

function input(root: ParentNode, label: string): HTMLInputElement {
  const match = [...root.querySelectorAll('label')].find((candidate) => candidate.textContent === label);
  const control = match === undefined ? null : root.querySelector(`#${CSS.escape(match.htmlFor)}`);
  if (!(control instanceof HTMLInputElement)) {
    throw new Error(`${label} input is missing`);
  }
  return control;
}

function submit(root: ParentNode): Promise<void> {
  const form = root.querySelector('form');
  if (!(form instanceof HTMLFormElement)) {
    throw new Error('the invite form is missing');
  }
  return act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    // The submit handler is async: let its awaited call and the state it sets
    // resolve inside the same act.
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe('InviteDialog', () => {
  it('offers no initial grants first, then the templates admitted at the scope', async () => {
    const { rendered } = mount({ kind: 'org', org: 'org_a' });
    const { container, unmount } = await rendered;
    const options = [...container.querySelectorAll('option')].map((option) => option.value);
    expect(options[0]).toBe('');
    expect(options).toContain('viewer');
    expect(options).toContain('admin');
    expect(options).not.toContain('operator');
    await unmount();

    const instance = mount({ kind: 'instance' });
    const mounted = await instance.rendered;
    expect([...mounted.container.querySelectorAll('option')].map((option) => option.value)).toEqual([
      '',
      'operator',
    ]);
    await mounted.unmount();
  });

  it('refuses an empty username locally and sends nothing', async () => {
    const { rendered } = mount({ kind: 'org', org: 'org_a' });
    const { container, unmount } = await rendered;
    await submit(container);
    await settle();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('login handle');
    expect(mocks.inviteMember).not.toHaveBeenCalled();
    await unmount();
  });

  it('invites and shows the authority once, outside every cache', async () => {
    mocks.inviteMember.mockResolvedValue({
      principal_id: 'prn_new',
      authority: AUTHORITY,
      expires_at: '2026-09-03T10:00:00Z',
    });
    const { rendered, onDone } = mount({ kind: 'org', org: 'org_a' });
    const { container, client, unmount } = await rendered;
    typeInto(input(container, 'Username'), ' dana ');
    const select = container.querySelector('select');
    if (!(select instanceof HTMLSelectElement)) throw new Error('no template select');
    await act(async () => {
      select.value = 'editor';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await submit(container);
    await settle();

    expect(mocks.inviteMember).toHaveBeenCalledWith(
      { kind: 'org', org: 'org_a' },
      { username: 'dana', displayName: '', template: 'editor' },
    );
    expect(container.querySelector('[data-testid="issued-authority"]')?.textContent).toBe(AUTHORITY);
    expect(container.textContent).toContain('--as dana');
    expect(container.querySelector('a[href="/establish"]')).not.toBeNull();
    expect(client.getQueryCache().getAll()).toEqual([]);
    expect(client.getMutationCache().getAll()).toEqual([]);

    const close = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Close');
    if (close === undefined) throw new Error('no Close button');
    await act(async () => close.click());
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onDone.mock.calls[0]?.[0]).toContain('Invited dana at Acme as editor');
    await unmount();
  });

  it('keeps the form on a username collision and says so', async () => {
    mocks.inviteMember.mockRejectedValue(new ApiError(409, 'conflict'));
    const { rendered, onDone } = mount({ kind: 'instance' });
    const { container, unmount } = await rendered;
    typeInto(input(container, 'Username'), 'dana');
    await submit(container);
    await settle();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'That username is already taken.',
    );
    expect(container.querySelector('form')).not.toBeNull();
    expect(container.querySelector('[data-testid="issued-authority"]')).toBeNull();
    expect(onDone).not.toHaveBeenCalled();
    await unmount();
  });

  it('treats Escape as cancel so the page never keeps a closed ceremony open', async () => {
    const { rendered, onCancel } = mount({ kind: 'org', org: 'org_a' });
    const { container, unmount } = await rendered;
    const dialog = container.querySelector('dialog');
    if (!(dialog instanceof HTMLDialogElement)) throw new Error('no dialog');
    await act(async () => {
      dialog.dispatchEvent(new Event('cancel', { bubbles: false, cancelable: true }));
    });
    expect(onCancel).toHaveBeenCalledTimes(1);
    await unmount();
  });
});
