// @vitest-environment happy-dom
import { act, useState } from 'react';
import { renderForm } from '../testkit/renderForm.tsx';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { WhoAmI } from '../api/session.ts';
import { AccountEntry, useRoutedChoice } from './Shell.tsx';

// useLogout wants an AuthProvider; AccountEntry only mounts it, never fires it.
vi.mock('../api/session.ts', async (importOriginal) => {
  const session = await importOriginal<typeof import('../api/session.ts')>();
  return { ...session, useLogout: () => ({ isPending: false, mutate: vi.fn() }) };
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

afterEach(() => {
  document.body.replaceChildren();
});

describe('useRoutedChoice', () => {
  // The regression: a scoped route latches its id, and a later UNSCOPED route
  // (routed === undefined) must KEEP that latch rather than clear it.
  it('latches a scoped route id and keeps it across an unscoped route', async () => {
    function Latch({ routed }: { routed: string | undefined }) {
      const [value, setValue] = useState('');
      useRoutedChoice(routed, setValue);
      return <output>{value}</output>;
    }

    const { container, rerender, unmount } = await renderForm(<Latch routed={undefined} />);
    const chosen = () => container.querySelector('output')?.textContent ?? '';
    try {
      expect(chosen()).toBe('');

      await rerender(<Latch routed="org-a" />);
      expect(chosen()).toBe('org-a');

      // Unscoped destination: the persisted tenant must survive.
      await rerender(<Latch routed={undefined} />);
      expect(chosen()).toBe('org-a');

      // A new scoped route replaces the latch.
      await rerender(<Latch routed="org-b" />);
      expect(chosen()).toBe('org-b');
    } finally {
      await unmount();
    }
  });

  // The warm-cache regression: MOUNTING on a scoped route (org listing already
  // in cache) must latch on the first render, not skip it like a reset effect.
  it('latches a scoped route id on mount', async () => {
    function Latch({ routed }: { routed: string | undefined }) {
      const [value, setValue] = useState('');
      useRoutedChoice(routed, setValue);
      return <output>{value}</output>;
    }

    const { container, unmount } = await renderForm(<Latch routed="org-a" />);
    const chosen = () => container.querySelector('output')?.textContent ?? '';
    try {
      expect(chosen()).toBe('org-a');
    } finally {
      await unmount();
    }
  });
});

describe('AccountEntry route-change close', () => {
  // Exercises the same `useResetOnChange(location.pathname, () => setOpen(false))`
  // line the collapsed nav sheet uses; AccountEntry is the mountable instance.
  it('closes the open menu when the route changes', async () => {
    const router = createMemoryRouter(
      [{ path: '*', element: <AccountEntry session={session} updateVersions={[]} /> }],
      { initialEntries: ['/a'] },
    );

    const { container, unmount } = await renderForm(<RouterProvider router={router} />);
    try {
      const trigger = container.querySelector('button[aria-haspopup="menu"]');
      if (!(trigger instanceof HTMLButtonElement)) {
        throw new Error('account entry has no menu trigger');
      }

      await act(async () => trigger.click());
      expect(trigger.getAttribute('aria-expanded')).toBe('true');
      expect(container.querySelector('[role="menu"]')).not.toBeNull();

      await act(async () => {
        await router.navigate('/b');
      });
      expect(trigger.getAttribute('aria-expanded')).toBe('false');
      expect(container.querySelector('[role="menu"]')).toBeNull();
    } finally {
      await unmount();
    }
  });
});
