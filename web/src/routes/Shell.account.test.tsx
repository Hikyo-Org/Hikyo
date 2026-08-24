// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
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

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

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
};

function accountButton(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelector('button[aria-haspopup="menu"]');
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error('account entry has no menu trigger');
  }
  return button;
}

function signOutButton(container: HTMLElement): HTMLButtonElement {
  const button = container.querySelector('[role="menuitem"]');
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error('account menu has no sign-out item');
  }
  return button;
}

async function renderAccountEntry(): Promise<{
  container: HTMLDivElement;
  trigger: HTMLButtonElement;
  unmount: () => Promise<void>;
}> {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);

  await act(async () => {
    root.render(
      <MemoryRouter>
        <AccountEntry session={session} updateVersions={[]} />
      </MemoryRouter>,
    );
  });

  return {
    container,
    trigger: accountButton(container),
    unmount: async () => act(async () => root.unmount()),
  };
}

afterEach(() => {
  document.body.replaceChildren();
});

describe('account menu', () => {
  it('moves focus into the menu, then Escape closes it and restores the trigger', async () => {
    const { container, trigger, unmount } = await renderAccountEntry();
    await act(async () => trigger.click());

    const signOut = signOutButton(container);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(document.activeElement).toBe(signOut);

    await act(async () => {
      signOut.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }));
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
    expect(document.activeElement).toBe(signOutButton(container));

    await act(async () => outside.focus());

    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[role="menu"]')).toBeNull();

    await unmount();
  });
});
