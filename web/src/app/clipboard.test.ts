import { afterEach, describe, expect, it, vi } from 'vitest';

import { writeClipboard, writeExpiringClipboard } from './clipboard.ts';
import { notifyFailure } from './notifications.tsx';
vi.mock('./notifications.tsx', () => ({ notifyFailure: vi.fn() }));

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('writeExpiringClipboard', () => {
  it('announces cleanup refusal globally after the disclosure owner retires', async () => {
    let release: (() => void) | undefined;
    let current = true;
    const writeText = vi.fn((text: string) => text === 'secret'
      ? new Promise<void>((resolve) => { release = resolve; })
      : Promise.reject(new Error('cleanup denied')));
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const pending = writeExpiringClipboard('secret', true, () => current);
    current = false;
    release?.();
    await pending;
    expect(notifyFailure).toHaveBeenCalledExactlyOnceWith('The browser refused to clear a canceled copy. Clear your clipboard manually.');
  });
  it.each([true, false])('does not expire a newer identical copy (audited: %s)', async (audited) => {
    vi.useFakeTimers();
    const writeText = vi.fn<(value: string) => Promise<void>>(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText, readText: vi.fn(() => Promise.resolve('same')) } });
    vi.stubGlobal('document', { hasFocus: () => true });
    await writeExpiringClipboard('same', true);
    await vi.advanceTimersByTimeAsync(44_000);
    await writeExpiringClipboard('same', audited);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(writeText.mock.calls.map(([value]) => value)).toEqual(['same', 'same']);
    await vi.advanceTimersByTimeAsync(44_000);
    expect(writeText.mock.calls.map(([value]) => value)).toEqual(audited ? ['same', 'same', ''] : ['same', 'same']);
  });
  it('retires an in-flight secret write without read permission before a newer app copy', async () => {
    let release: (() => void) | undefined;
    let current = true;
    const writeText = vi.fn((text: string) => text === 'secret'
      ? new Promise<void>((resolve) => { release = resolve; })
      : Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const secret = writeExpiringClipboard('secret', true, () => current);
    current = false;
    const newer = writeClipboard('newer');
    release?.();
    await Promise.all([secret, newer]);
    expect(writeText.mock.calls.map(([text]) => text)).toEqual(['secret', '', 'newer']);
  });

  it('never dispatches a queued secret whose task retired while another write was pending', async () => {
    let release: (() => void) | undefined;
    let current = true;
    const writeText = vi.fn((text: string) => text === 'first'
      ? new Promise<void>((resolve) => { release = resolve; })
      : Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const first = writeClipboard('first');
    const secret = writeExpiringClipboard('secret', true, () => current);
    current = false;
    release?.();
    await Promise.all([first, secret]);
    expect(writeText).toHaveBeenCalledExactlyOnceWith('first');
  });

  it('reports audited copy honestly and clears while the tab remains focused', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('secret'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    vi.stubGlobal('document', { hasFocus: () => true });

    await expect(writeExpiringClipboard('secret', true)).resolves.toContain(
      'recorded as a disclosure',
    );
    await vi.advanceTimersByTimeAsync(45_000);

    expect(writeText).toHaveBeenNthCalledWith(1, 'secret');
    expect(writeText).toHaveBeenNthCalledWith(2, '');
  });

  it('never clears an ordinary (non-audited) copy, matching its microcopy', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('LOG_LEVEL=debug'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    vi.stubGlobal('document', { hasFocus: () => true });

    await expect(writeExpiringClipboard('LOG_LEVEL=debug', false)).resolves.toBe(
      'Copied. This value is not a secret, so no disclosure was recorded.',
    );
    await vi.advanceTimersByTimeAsync(45_000);

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(readText).not.toHaveBeenCalled();
  });

  it.each([
    ['the clipboard now holds something else', () => Promise.resolve('other')],
    ['the clipboard cannot be read', () => Promise.reject(new Error('denied'))],
  ])('does not clear when %s', async (_case, readText) => {
    vi.useFakeTimers();
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText, readText: vi.fn(readText) } });
    vi.stubGlobal('document', { hasFocus: () => true });

    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);

    expect(writeText).toHaveBeenCalledTimes(1);
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
