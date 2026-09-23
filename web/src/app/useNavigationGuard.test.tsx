// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useNavigationGuard } from './useNavigationGuard.ts';

function Guarded({ active, onAttempt }: { active: boolean; onAttempt: () => void }) {
  useNavigationGuard(active, onAttempt);
  return null;
}

let root: Root | null = null;

afterEach(async () => {
  if (root !== null) {
    await act(async () => root?.unmount());
    root = null;
  }
  vi.restoreAllMocks();
});

async function mount(active: boolean, onAttempt: () => void): Promise<Root> {
  root ??= createRoot(document.createElement('div'));
  const mounted = root;
  await act(async () => {
    mounted.render(<Guarded active={active} onAttempt={onAttempt} />);
  });
  return mounted;
}

describe('useNavigationGuard', () => {
  it('pushes a history sentinel and registers beforeunload while active', async () => {
    const pushState = vi.spyOn(history, 'pushState').mockImplementation(() => {});
    const addListener = vi.spyOn(window, 'addEventListener');

    await mount(false, () => {});
    expect(pushState).not.toHaveBeenCalled();

    await mount(true, () => {});
    expect(pushState).toHaveBeenCalledTimes(1);
    expect(pushState).toHaveBeenCalledWith({ hikyoNavigationGuard: true }, '', window.location.href);
    expect(addListener.mock.calls.map(([type]) => type)).toEqual(
      expect.arrayContaining(['beforeunload', 'popstate']),
    );
  });

  it('re-pushes the sentinel on popstate and routes the attempt to the latest callback', async () => {
    const pushState = vi.spyOn(history, 'pushState').mockImplementation(() => {});
    const first = vi.fn();
    const latest = vi.fn();

    await mount(true, first);
    await mount(true, latest);
    pushState.mockClear();

    await act(async () => {
      window.dispatchEvent(new PopStateEvent('popstate'));
    });

    expect(pushState).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();
    expect(latest).toHaveBeenCalledTimes(1);
  });

  it('adopts a stale sentinel left by another guard instead of surfacing an attempt', async () => {
    const pushState = vi.spyOn(history, 'pushState').mockImplementation(() => {});
    const replaceState = vi.spyOn(history, 'replaceState').mockImplementation(() => {});
    const onAttempt = vi.fn();

    await mount(true, onAttempt);
    pushState.mockClear();

    // The previous guard's deactivation `history.back()` settling after this
    // one mounted: the pop lands on that guard's sentinel, not on the route.
    await act(async () => {
      window.dispatchEvent(new PopStateEvent('popstate', { state: { hikyoNavigationGuard: true } }));
    });

    expect(onAttempt).not.toHaveBeenCalled();
    expect(pushState).not.toHaveBeenCalled();
    expect(replaceState).toHaveBeenCalledWith({ hikyoNavigationGuard: true }, '', window.location.href);
  });

  it('removes its listeners and consumes the sentinel when deactivated', async () => {
    vi.spyOn(history, 'pushState').mockImplementation(() => {});
    const back = vi.spyOn(history, 'back').mockImplementation(() => {});
    const removeListener = vi.spyOn(window, 'removeEventListener');
    const onAttempt = vi.fn();

    await mount(true, onAttempt);
    await mount(false, onAttempt);

    expect(back).toHaveBeenCalledTimes(1);
    expect(removeListener.mock.calls.map(([type]) => type)).toEqual(
      expect.arrayContaining(['beforeunload', 'popstate']),
    );
    await act(async () => {
      window.dispatchEvent(new PopStateEvent('popstate'));
    });
    expect(onAttempt).not.toHaveBeenCalled();
  });
});
