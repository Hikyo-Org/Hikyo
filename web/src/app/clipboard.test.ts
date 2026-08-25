import { afterEach, describe, expect, it, vi } from 'vitest';

import { writeClipboard, writeExpiringClipboard } from './clipboard.ts';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('writeExpiringClipboard', () => {
  it('reports audited copy honestly and clears while the tab remains focused', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    vi.stubGlobal('document', { hasFocus: () => true });

    await expect(writeExpiringClipboard('secret', true)).resolves.toContain(
      'recorded as a disclosure',
    );
    await vi.advanceTimersByTimeAsync(45_000);

    expect(writeText).toHaveBeenNthCalledWith(1, 'secret');
    expect(writeText).toHaveBeenNthCalledWith(2, '');
  });
});

describe('writeClipboard', () => {
  it('reports ok when the browser writes the value', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });

    await expect(writeClipboard('display-once')).resolves.toBe('ok');
    expect(writeText).toHaveBeenCalledWith('display-once');
  });

  it('reports refused when the browser rejects the write', async () => {
    vi.stubGlobal('navigator', {
      clipboard: { writeText: vi.fn(() => Promise.reject(new Error('permission denied'))) },
    });

    await expect(writeClipboard('display-once')).resolves.toBe('refused');
  });

  it('reports refused when clipboard access throws synchronously', async () => {
    vi.stubGlobal('navigator', {
      clipboard: {
        writeText: vi.fn(() => {
          throw new TypeError('clipboard unavailable');
        }),
      },
    });

    await expect(writeClipboard('display-once')).resolves.toBe('refused');
  });

  it('reports refused when the clipboard API is absent', async () => {
    vi.stubGlobal('navigator', {});

    await expect(writeClipboard('display-once')).resolves.toBe('refused');
  });
});
