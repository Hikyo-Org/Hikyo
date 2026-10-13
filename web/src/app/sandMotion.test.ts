// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { hasGpu, installSandMotion } from './sandMotion.ts';

const root = () => document.documentElement.classList;

function stubWebgl(context: unknown) {
  const loseContext = vi.fn();
  const getContext = vi.fn(() => (context === 'gl' ? { getExtension: () => ({ loseContext }) } : context));
  vi.spyOn(document, 'createElement').mockReturnValue({ getContext } as unknown as HTMLElement);
  return { getContext, loseContext };
}

function setView({ hidden, focused }: { hidden: boolean; focused: boolean }) {
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(hidden);
  vi.spyOn(document, 'hasFocus').mockReturnValue(focused);
}

let uninstall: (() => void) | undefined;
afterEach(() => {
  uninstall?.();
  uninstall = undefined;
  vi.restoreAllMocks();
});

describe('hasGpu', () => {
  it('asks for an accelerated context and releases it at once', () => {
    const { getContext, loseContext } = stubWebgl('gl');
    expect(hasGpu(document)).toBe(true);
    expect(getContext).toHaveBeenCalledWith('webgl', { failIfMajorPerformanceCaveat: true });
    expect(loseContext).toHaveBeenCalledOnce();
  });

  it('is false for software rendering (null context) and when WebGL throws', () => {
    stubWebgl(null);
    expect(hasGpu(document)).toBe(false);
    vi.restoreAllMocks();
    vi.spyOn(document, 'createElement').mockImplementation(() => {
      throw new Error('no canvas');
    });
    expect(hasGpu(document)).toBe(false);
  });
});

describe('installSandMotion', () => {
  it('keeps the sand static without a GPU and never pauses it', () => {
    stubWebgl(null);
    setView({ hidden: true, focused: false });
    uninstall = installSandMotion();
    expect(root().contains('sand-static')).toBe(true);
    expect(root().contains('sand-paused')).toBe(false);
  });

  it('pauses while hidden or unfocused and resumes on focus and visibility', () => {
    stubWebgl('gl');
    setView({ hidden: false, focused: true });
    uninstall = installSandMotion();
    expect(root().contains('sand-static')).toBe(false);
    expect(root().contains('sand-paused')).toBe(false);

    setView({ hidden: false, focused: false });
    window.dispatchEvent(new Event('blur'));
    expect(root().contains('sand-paused')).toBe(true);

    setView({ hidden: false, focused: true });
    window.dispatchEvent(new Event('focus'));
    expect(root().contains('sand-paused')).toBe(false);

    setView({ hidden: true, focused: true });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(root().contains('sand-paused')).toBe(true);

    setView({ hidden: false, focused: true });
    window.dispatchEvent(new Event('pageshow'));
    expect(root().contains('sand-paused')).toBe(false);

    uninstall();
    uninstall = undefined;
    setView({ hidden: true, focused: false });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(root().contains('sand-paused')).toBe(false);
  });
});
