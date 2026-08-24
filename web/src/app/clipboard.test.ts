import { afterEach, describe, expect, it, vi } from 'vitest';

import { writeClipboard } from './clipboard.ts';

afterEach(() => {
  vi.unstubAllGlobals();
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
