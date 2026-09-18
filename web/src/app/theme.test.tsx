// @vitest-environment happy-dom
import { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

/**
 * The module holds the document's choice in memory, so every test gets a
 * fresh copy through `vi.resetModules` and a dynamic import.
 */
type ThemeModule = typeof import('./theme.ts');

async function loadTheme(): Promise<ThemeModule> {
  vi.resetModules();
  return import('./theme.ts');
}

function throwingStorage(overrides: Partial<Storage> = {}): Partial<Storage> {
  const denied = () => {
    throw new DOMException('The operation is insecure.', 'SecurityError');
  };
  return { getItem: denied, setItem: denied, removeItem: denied, ...overrides };
}

let root: Root | null = null;
let container: HTMLDivElement | null = null;

beforeEach(() => {
  document.documentElement.removeAttribute('data-theme');
});

afterEach(async () => {
  if (root !== null) {
    await act(async () => root?.unmount());
    root = null;
  }
  container?.remove();
  container = null;
  vi.unstubAllGlobals();
});

async function mountChoice(
  theme: ThemeModule,
): Promise<{ current: () => string; set: (choice: 'dark' | 'light' | 'system') => void }> {
  let setter: ((choice: 'dark' | 'light' | 'system') => void) | null = null;
  function Probe() {
    const [choice, setChoice] = theme.useThemeChoice();
    useEffect(() => {
      setter = setChoice;
    }, [setChoice]);
    return <output>{choice}</output>;
  }
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
  const mounted = root;
  await act(async () => {
    mounted.render(<Probe />);
  });
  return {
    current: () => container?.querySelector('output')?.textContent ?? '',
    set: (choice) => {
      setter?.(choice);
    },
  };
}

describe('theme storage is best-effort', () => {
  it('starts on the system theme, without throwing, when storage refuses reads', async () => {
    vi.stubGlobal('localStorage', throwingStorage());
    const theme = await loadTheme();

    expect(() => theme.initTheme()).not.toThrow();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);

    const probe = await mountChoice(theme);
    expect(probe.current()).toBe('system');
  });

  it('still paints and notifies subscribers when storage refuses writes', async () => {
    vi.stubGlobal('localStorage', throwingStorage({ getItem: () => null }));
    const theme = await loadTheme();
    theme.initTheme();
    const probe = await mountChoice(theme);

    await act(async () => probe.set('light'));

    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(probe.current()).toBe('light');
  });

  it('survives a cross-tab storage event whose read throws', async () => {
    let reads = 0;
    vi.stubGlobal(
      'localStorage',
      throwingStorage({
        getItem: () => {
          reads += 1;
          if (reads > 1) {
            throw new DOMException('The operation is insecure.', 'SecurityError');
          }
          return 'dark';
        },
        setItem: () => {},
        removeItem: () => {},
      }),
    );
    const theme = await loadTheme();
    theme.initTheme();
    const probe = await mountChoice(theme);
    expect(probe.current()).toBe('dark');

    await act(async () => {
      globalThis.dispatchEvent(new StorageEvent('storage', { key: 'hikyo.theme' }));
    });

    expect(probe.current()).toBe('system');
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('persists the choice when storage works', async () => {
    const setItem = vi.fn();
    const removeItem = vi.fn();
    vi.stubGlobal('localStorage', { getItem: () => 'light', setItem, removeItem });
    const theme = await loadTheme();
    theme.initTheme();
    const probe = await mountChoice(theme);
    expect(probe.current()).toBe('light');

    await act(async () => probe.set('system'));

    expect(removeItem).toHaveBeenCalledWith('hikyo.theme');
    expect(probe.current()).toBe('system');
  });
});
