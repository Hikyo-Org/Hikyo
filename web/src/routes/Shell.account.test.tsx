// @vitest-environment happy-dom
import { renderForm } from '../testkit/renderForm.tsx';
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { WhoAmI } from '../api/session.ts';
import { AccountEntry } from './Shell.tsx';

vi.mock('../api/session.ts', async (importOriginal) => {
  const session = await importOriginal<typeof import('../api/session.ts')>();
  return {
    ...session,
    useLogout: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

const session: WhoAmI = {
  session: {
    id: 'ses_123e4567-e89b-12d3-a456-426614174000',
    artifact: 'browser',
    created_at: '2026-08-24T08:00:00Z',
    idle_expires_at: '2026-08-24T08:30:00Z',
    absolute_expires_at: '2026-08-24T16:00:00Z',
    assurance: {
      method: 'local-password',
      factors: ['password'],
      authenticated_at: '2026-08-24T08:00:00Z',
    },
  },
  principal: {
    id: 'prn_123e4567-e89b-12d3-a456-426614174000',
    kind: 'human',
    display_name: 'Alice Example',
  },
  capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } },
};

function accountButton(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelector('button[aria-haspopup="menu"]');
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error('account entry has no menu trigger');
  }
  return button;
}

function firstMenuItem(container: HTMLElement): HTMLElement {
  const item = container.querySelector('[role="menuitem"]');
  if (!(item instanceof HTMLElement)) {
    throw new Error('account menu has no item');
  }
  return item;
}

async function renderAccountEntry(who: WhoAmI = session): Promise<{
  container: HTMLElement;
  trigger: HTMLButtonElement;
  unmount: () => Promise<void>;
}> {
  const { container, unmount } = await renderForm(
    <MemoryRouter>
      <AccountEntry session={who} updateVersions={[]} />
    </MemoryRouter>,
  );

  return {
    container,
    trigger: accountButton(container),
    unmount,
  };
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('account menu', () => {
  it('moves focus into the menu, then Escape closes it and restores the trigger', async () => {
    const { container, trigger, unmount } = await renderAccountEntry();
    await act(async () => trigger.click());

    const firstItem = firstMenuItem(container);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(firstItem.textContent).toBe('Account & security');
    expect(document.activeElement).toBe(firstItem);

    await act(async () => {
      firstItem.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }));
    });

    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[role="menu"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);

    await unmount();
  });

  it('closes when a pointer press starts outside the account entry', async () => {
    const outside = document.createElement('button');
    outside.textContent = 'Outside';
    document.body.append(outside);
    const { container, trigger, unmount } = await renderAccountEntry();
    await act(async () => trigger.click());
    expect(container.querySelector('[role="menu"]')).not.toBeNull();

    await act(async () => {
      outside.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    });

    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[role="menu"]')).toBeNull();

    await unmount();
  });

  it('closes when focus leaves the account entry without a pointer press', async () => {
    const outside = document.createElement('button');
    outside.textContent = 'Outside';
    document.body.append(outside);
    const { container, trigger, unmount } = await renderAccountEntry();
    await act(async () => trigger.click());
    expect(document.activeElement).toBe(firstMenuItem(container));

    await act(async () => outside.focus());

    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[role="menu"]')).toBeNull();

    await unmount();
  });

  it('uses a readable account label when the display name is absent or blank', async () => {
    const blank: WhoAmI = { ...session, principal: { ...session.principal, display_name: '' } };
    const { container, trigger, unmount } = await renderAccountEntry(blank);
    expect(trigger.getAttribute('aria-label')).toBe(
      'Account: Your account',
    );
    await act(async () => trigger.click());
    expect(container.querySelector('.menu__label .mono')?.textContent).toBe(
      'Your account',
    );
    await unmount();
  });
});
