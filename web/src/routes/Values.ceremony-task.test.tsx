// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { RevealWindow } from '../api/values.ts';
import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import type { CeremonyRequest } from './Ceremony.tsx';
import { Values } from './Values.tsx';

const mocks = vi.hoisted(() => ({
  copy: vi.fn(),
  fetchRevealWindow: vi.fn(),
  revealAll: vi.fn(),
  revealOne: vi.fn(),
  /** The cached reveal guard the surface renders from; `undefined` = pending or failed. */
  guard: undefined as RevealWindow | undefined,
}));

vi.mock('../api/values.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/values.ts')>();
  return {
    ...actual,
    fetchRevealWindow: mocks.fetchRevealWindow,
    useCopyValues: () => ({ mutateAsync: mocks.copy }),
    useEnvironments: () => ({
      data: {
        items: [
          { id: 'env-a', name: 'Alpha' },
          { id: 'env-b', name: 'Beta' },
          { id: 'env-c', name: 'Gamma' },
        ],
      },
    }),
    useRevealAll: () => ({ mutateAsync: mocks.revealAll }),
    useRevealOne: () => ({ mutateAsync: mocks.revealOne }),
    useRevealWindow: () => ({
      data: mocks.guard,
    }),
    useSetValue: () => ({ isPending: false, mutateAsync: vi.fn() }),
    useValues: () => ({
      data: {
        items: [
          {
            key_id: 'key-a',
            name: 'KEY_A',
            classification: 'secret',
            set: true,
          },
          {
            key_id: 'key-b',
            name: 'KEY_B',
            classification: 'secret',
            set: true,
          },
        ],
      },
      isError: false,
    }),
  };
});

vi.mock('../api/transport.tsx', () => ({
  useTransport: () => ({ client: undefined }),
}));

vi.mock('./Ceremony.tsx', async (importActual) => {
  const actual = await importActual<typeof import('./Ceremony.tsx')>();
  return {
    ...actual,
    Ceremony: ({ request }: { request: CeremonyRequest }) => (
      <output data-testid="ceremony">{request.environmentId}</output>
    ),
  };
});

function App() {
  const navigate = useNavigate();
  return (
    <>
      <button
        type="button"
        onClick={() => navigate('/orgs/org-a/projects/project-a/environments/env-c/values')}
      >
        Navigate
      </button>
      <Routes>
        <Route
          path="/orgs/:org/projects/:project/environments/:environment/values"
          element={<Values />}
        />
      </Routes>
    </>
  );
}

async function renderValues() {
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/orgs/org-a/projects/project-a/environments/env-a/values']}>
      <App />
    </MemoryRouter>,
  );
  return { container, unmount };
}

function button(container: HTMLElement, name: string): HTMLButtonElement {
  const match = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === name || candidate.getAttribute('aria-label') === name,
  );
  if (match === undefined) throw new Error(`button ${name} is missing`);
  return match;
}

async function selectDestination(container: HTMLElement, environmentId: string): Promise<void> {
  const select = container.querySelector<HTMLSelectElement>('#publish-destination');
  if (select === null) throw new Error('publish destination is missing');
  await act(async () => {
    select.value = environmentId;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

beforeEach(() => {
  mocks.guard = revealWindow(true);
  mocks.copy.mockReset();
  mocks.fetchRevealWindow.mockReset();
  mocks.revealAll.mockReset();
  mocks.revealOne.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('Values reveal gating', () => {
  for (const [label, guard] of [
    ['pending or failed', undefined],
    ['refused', { ...revealWindow(true), can_reveal: false }],
  ] as const) {
    it(`offers no disclosure control while the guard is ${label}`, async () => {
      mocks.guard = guard;
      const { container, unmount } = await renderValues();
      const labels = [...container.querySelectorAll('button')].map(
        (b) => b.getAttribute('aria-label') ?? b.textContent,
      );
      expect(labels.some((l) => l?.startsWith('Reveal KEY'))).toBe(false);
      expect(labels.some((l) => l?.startsWith('Copy KEY'))).toBe(false);
      const revealAll = [...container.querySelectorAll('button')].find(
        (b) => b.textContent === 'Reveal every secret',
      );
      expect(revealAll?.disabled).toBe(true);
      await unmount();
    });
  }

  it('stops a disclosure whose freshly fetched window says no, even with a stale cache', async () => {
    mocks.guard = revealWindow(true);
    mocks.fetchRevealWindow.mockResolvedValueOnce({ ...revealWindow(true), can_reveal: false });
    const { container, unmount } = await renderValues();
    await act(async () => button(container, 'Reveal KEY_A').click());
    await settle();
    expect(mocks.revealOne).not.toHaveBeenCalled();
    expect(container.textContent).toContain('Reveal is not granted here, so nothing was disclosed.');
    await unmount();
  });
});

describe('Values reveal accessibility', () => {
  it('announces one reveal once and keeps its ticking countdown visual-only', async () => {
    mocks.fetchRevealWindow.mockResolvedValueOnce(revealWindow(true));
    mocks.revealOne.mockResolvedValueOnce({
      key_id: 'key-a',
      name: 'KEY_A',
      value: 'revealed-value',
    });
    const { container, unmount } = await renderValues();

    await act(async () => button(container, 'Reveal KEY_A').click());
    await settle();

    expect(container.querySelectorAll('.values__reveal-announcement')).toHaveLength(1);
    const announcement = container.querySelector('.values__reveal-announcement');
    expect(announcement?.getAttribute('role')).toBe('status');
    expect(announcement?.textContent).toBe('KEY_A revealed: re-masks in 10 seconds');
    // The plaintext IS the accessible content: an aria-label would hide it.
    expect(container.querySelector('.values__plain')?.hasAttribute('aria-label')).toBe(false);
    expect(container.querySelector('.values__plain')?.textContent).toContain('revealed-value');
    expect(container.querySelector('.values__countdown')?.getAttribute('aria-hidden')).toBe(
      'true',
    );
    expect(container.querySelector('.values__countdown')?.hasAttribute('role')).toBe(false);
    await unmount();
  });

  it('uses one reveal announcement when every secret is disclosed', async () => {
    mocks.fetchRevealWindow.mockResolvedValueOnce(revealWindow(true));
    mocks.revealAll.mockResolvedValueOnce({
      items: [
        {
          key_id: 'key-a',
          name: 'KEY_A',
          classification: 'secret',
          value: 'revealed-a',
        },
        {
          key_id: 'key-b',
          name: 'KEY_B',
          classification: 'secret',
          value: 'revealed-b',
        },
      ],
    });
    const { container, unmount } = await renderValues();
    const liveRegionCount = container.querySelectorAll(
      '[role="status"], [aria-live="polite"]',
    ).length;

    await act(async () => button(container, 'Reveal every secret').click());
    await settle();

    expect(container.querySelectorAll('.values__reveal-announcement')).toHaveLength(1);
    expect(container.querySelector('.values__reveal-announcement')?.textContent).toBe(
      '2 secrets revealed: re-mask in 10 seconds',
    );
    expect(container.querySelectorAll('.values__countdown')).toHaveLength(2);
    expect(container.querySelectorAll('.values__countdown[aria-hidden="true"]')).toHaveLength(2);
    expect(container.querySelectorAll('.values__countdown[role]')).toHaveLength(0);
    expect(container.querySelectorAll('[role="status"], [aria-live="polite"]')).toHaveLength(
      liveRegionCount,
    );
    await unmount();
  });
});

describe('Values ceremony task ownership', () => {
  it('clears a pending native clipboard disclosure completed after navigation, without read permission', async () => {
    const pending = deferred<void>();
    const writeText = vi.fn((value: string) => value === 'pending-secret' ? pending.promise : Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    mocks.fetchRevealWindow.mockResolvedValueOnce(revealWindow(true));
    mocks.revealOne.mockResolvedValueOnce({ key_id: 'key-a', name: 'KEY_A', value: 'pending-secret' });
    const { container, unmount } = await renderValues();
    await act(async () => button(container, 'Copy KEY_A (audited disclosure)').click());
    await settle();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('pending-secret');
    await act(async () => button(container, 'Navigate').click());
    await act(async () => pending.resolve());
    await settle();
    expect(writeText.mock.calls.map(([value]) => value)).toEqual(['pending-secret', '']);
    expect(container.textContent).not.toContain('Copied, and recorded');
    await unmount();
  });
  it('ignores a guard completion from the environment visited before navigation', async () => {
    const pending = deferred<RevealWindow>();
    mocks.fetchRevealWindow.mockImplementationOnce(() => pending.promise);
    const { container, unmount } = await renderValues();

    await act(async () => button(container, 'Reveal KEY_A').click());
    await act(async () => button(container, 'Navigate').click());
    await act(async () => pending.resolve(revealWindow(false)));
    await settle();

    expect(container.querySelector('[data-testid="ceremony"]')).toBeNull();
    await unmount();
  });

  it('ignores a guard completion for a publish destination that changed', async () => {
    const pending = deferred<RevealWindow>();
    mocks.fetchRevealWindow.mockImplementationOnce(() => pending.promise);
    const { container, unmount } = await renderValues();
    await selectDestination(container, 'env-b');

    await act(async () => button(container, 'Publish into environment').click());
    await selectDestination(container, 'env-c');
    await act(async () => pending.resolve(revealWindow(false)));
    await settle();

    expect(container.querySelector('[data-testid="ceremony"]')).toBeNull();
    await unmount();
  });

  it('does not disclose a value whose request completes after navigation', async () => {
    const disclosure = deferred<{
      key_id: string;
      name: string;
      value: string;
    }>();
    mocks.fetchRevealWindow.mockResolvedValueOnce(revealWindow(true));
    mocks.revealOne.mockImplementationOnce(() => disclosure.promise);
    const { container, unmount } = await renderValues();

    await act(async () => button(container, 'Reveal KEY_A').click());
    await settle();
    await act(async () => button(container, 'Navigate').click());
    await act(async () => disclosure.resolve({
      key_id: 'key-a',
      name: 'KEY_A',
      value: 'must-not-cross-environments',
    }));
    await settle();

    expect(container.textContent).not.toContain('must-not-cross-environments');
    expect(container.textContent).toContain('No disclosures yet.');
    await unmount();
  });
});

describe('Values countdown lifecycle (#972)', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);
    mocks.guard = revealWindow(false);
    mocks.fetchRevealWindow.mockResolvedValue(revealWindow(true));
    mocks.revealOne.mockResolvedValue({ key_id: 'key-a', name: 'KEY_A', value: 'clock-secret' });
    mocks.revealAll.mockResolvedValue({ items: [
      { key_id: 'key-a', name: 'KEY_A', classification: 'secret', value: 'clock-secret' },
      { key_id: 'key-b', name: 'KEY_B', classification: 'secret', value: 'other-secret' },
    ] });
  });

  for (const state of ['locked', 'single-decision']) {
    it(`schedules no clock updates while fully masked and ${state}`, async () => {
      if (state === 'single-decision') mocks.guard = { ...revealWindow(true), single_decision: true };
      const form = await renderValues();
      try {
        expect(vi.getTimerCount()).toBe(0);
        await act(async () => vi.advanceTimersByTime(5_000));
        expect(vi.getTimerCount()).toBe(0);
      } finally { await form.unmount(); }
    });
  }

  for (const action of ['Reveal KEY_A', 'Reveal every secret']) {
    it(`uses one clock for ${action} and stops at the absolute deadline`, async () => {
      const form = await renderValues();
      try {
        // An idle clock sample must not inflate the next disclosure countdown.
        vi.setSystemTime(Date.now() + 60_000);
        await act(async () => button(form.container, action).click());
        await settle();
        expect(vi.getTimerCount()).toBe(1);
        expect(form.container.querySelector('.values__countdown')?.textContent).toContain('10 seconds');
        const announcement = form.container.querySelector('.values__reveal-announcement')?.textContent;
        await act(async () => vi.advanceTimersByTime(1_000));
        expect(form.container.querySelector('.values__countdown')?.textContent).toContain('9 seconds');
        expect(form.container.querySelector('.values__reveal-announcement')?.textContent).toBe(announcement);
        await act(async () => vi.advanceTimersByTime(9_000));
        expect(form.container.textContent).not.toContain('clock-secret');
        expect(form.container.textContent).not.toContain('other-secret');
        expect(vi.getTimerCount()).toBe(0);
      } finally { await form.unmount(); }
    });
  }

  for (const event of ['blur', 'visibilitychange']) {
    it(`stops the disclosure clock on ${event} and stays masked on return`, async () => {
      const form = await renderValues();
      try {
        await act(async () => button(form.container, 'Reveal KEY_A').click());
        await settle();
        expect(vi.getTimerCount()).toBe(1);
        await act(async () => (event === 'blur' ? window : document).dispatchEvent(new Event(event)));
        expect(form.container.textContent).not.toContain('clock-secret');
        expect(vi.getTimerCount()).toBe(0);
        vi.setSystemTime(Date.now() + 60_000);
        await act(async () => window.dispatchEvent(new Event('focus')));
        expect(form.container.textContent).not.toContain('clock-secret');
        expect(vi.getTimerCount()).toBe(0);
      } finally { await form.unmount(); }
    });
  }

  it('expires a disclosure by wall time after delayed callbacks', async () => {
    const form = await renderValues();
    try {
      await act(async () => button(form.container, 'Reveal KEY_A').click());
      await settle();
      vi.setSystemTime(Date.now() + 60_000);
      await act(async () => vi.advanceTimersByTime(250));
      expect(form.container.textContent).not.toContain('clock-secret');
      expect(vi.getTimerCount()).toBe(0);
    } finally { await form.unmount(); }
  });

  it('keeps the shared clock while a guard outlives focus-remasked plaintext', async () => {
    mocks.guard = { ...revealWindow(true), expires_at: new Date(Date.now() + 12_000).toISOString() };
    const form = await renderValues();
    try {
      await act(async () => button(form.container, 'Reveal KEY_A').click());
      await settle();
      expect(vi.getTimerCount()).toBe(1);
      await act(async () => window.dispatchEvent(new Event('blur')));
      expect(form.container.textContent).not.toContain('clock-secret');
      expect(vi.getTimerCount()).toBe(1);
      await act(async () => vi.advanceTimersByTime(12_000));
      expect(vi.getTimerCount()).toBe(0);
    } finally { await form.unmount(); }
  });

  it('ticks a timed guard without plaintext, then stops when its window expires', async () => {
    mocks.guard = { ...revealWindow(true), expires_at: new Date(Date.now() + 3_000).toISOString() };
    const form = await renderValues();
    try {
      expect(vi.getTimerCount()).toBe(1);
      expect(form.container.textContent).toMatch(/Reveal window.*3 seconds/);
      await act(async () => vi.advanceTimersByTime(1_000));
      expect(form.container.textContent).toMatch(/Reveal window.*2 seconds/);
      await act(async () => vi.advanceTimersByTime(2_000));
      expect(form.container.textContent).toMatch(/Reveal window.*0 seconds/);
      expect(vi.getTimerCount()).toBe(0);
    } finally { await form.unmount(); }
  });

  it('cleans up an active disclosure clock on unmount', async () => {
    const form = await renderValues();
    await act(async () => button(form.container, 'Reveal KEY_A').click());
    await settle();
    expect(vi.getTimerCount()).toBe(1);
    await form.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
