import { afterEach, describe, expect, it, vi } from 'vitest';

import { writeClipboard, writeExpiringClipboard } from './clipboard.ts';
import { notifyFailure } from './notifications.tsx';
vi.mock('./notifications.tsx', () => ({ notifyFailure: vi.fn() }));

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function backgroundClipboard() {
  let focused = false;
  let visible = false;
  const windowEvents = new EventTarget();
  const documentEvents = new EventTarget();
  const removeFocus = vi.fn(windowEvents.removeEventListener.bind(windowEvents));
  const removeVisibility = vi.fn(documentEvents.removeEventListener.bind(documentEvents));
  vi.stubGlobal('window', {
    addEventListener: windowEvents.addEventListener.bind(windowEvents),
    removeEventListener: removeFocus,
  });
  vi.stubGlobal('document', {
    hasFocus: () => focused,
    get visibilityState() { return visible ? 'visible' : 'hidden'; },
    addEventListener: documentEvents.addEventListener.bind(documentEvents),
    removeEventListener: removeVisibility,
  });
  return {
    focus(event: 'focus' | 'visibilitychange' = 'focus') {
      focused = true;
      visible = true;
      (event === 'focus' ? windowEvents : documentEvents).dispatchEvent(new Event(event));
    },
    blur() { focused = false; visible = false; },
    removeFocus,
    removeVisibility,
  };
}

describe('writeExpiringClipboard', () => {
  it.each<'focus' | 'visibilitychange'>(['focus', 'visibilitychange'])('clears after background expiry on %s and removes listeners', async (event) => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('secret'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);
    expect(readText).not.toHaveBeenCalled();
    tab.focus(event);
    await vi.advanceTimersByTimeAsync(0);
    expect(writeText.mock.calls).toEqual([['secret'], ['']]);
    expect(tab.removeFocus).toHaveBeenCalledOnce();
    expect(tab.removeVisibility).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('does not clear external changed content after background expiry', async () => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText, readText: vi.fn(() => Promise.resolve('external')) } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);
    tab.focus();
    await vi.advanceTimersByTimeAsync(0);
    expect(writeText).toHaveBeenCalledExactlyOnceWith('secret');
  });

  it('does not expire a newer identical app copy on returned focus', async () => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('same'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('same', true);
    await vi.advanceTimersByTimeAsync(45_000);
    await writeClipboard('same');
    tab.focus();
    await vi.advanceTimersByTimeAsync(0);
    expect(readText).not.toHaveBeenCalled();
    expect(writeText.mock.calls).toEqual([['same'], ['same']]);
  });

  it('stops after denied read without repeated permission attempts', async () => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.reject(new Error('permission denied')));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);
    tab.focus();
    await vi.advanceTimersByTimeAsync(0);
    tab.focus('visibilitychange');
    tab.focus();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(readText).toHaveBeenCalledOnce();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('secret');
    expect(tab.removeFocus).toHaveBeenCalledOnce();
    expect(tab.removeVisibility).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('expires retry listeners at the deadline and never clears on later focus', async () => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('secret'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(165_000);
    tab.focus();
    await vi.advanceTimersByTimeAsync(0);
    expect(readText).not.toHaveBeenCalled();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('secret');
    expect(tab.removeFocus).toHaveBeenCalledOnce();
    expect(tab.removeVisibility).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('never clears when a throttled expiry callback dispatches after the deadline', async () => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('secret'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    vi.setSystemTime(Date.now() + 165_000);
    await vi.advanceTimersByTimeAsync(45_000);
    tab.focus();
    expect(readText).not.toHaveBeenCalled();
    expect(tab.removeFocus).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each(['deadline', 'lost focus'])('never clears when a queued focus attempt dispatches after %s', async (reason) => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    let release: (() => void) | undefined;
    const writeText = vi.fn((text: string) => text === 'blocking'
      ? new Promise<void>((_resolve, reject) => { release = () => reject(new Error('blocking write refused')); }) : Promise.resolve());
    const readText = vi.fn(() => Promise.resolve('secret'));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);
    const pending = writeClipboard('blocking');
    tab.focus();
    if (reason === 'deadline') await vi.advanceTimersByTimeAsync(120_000);
    else tab.blur();
    release?.();
    await expect(pending).resolves.toBe('refused');
    await vi.advanceTimersByTimeAsync(0);
    expect(readText).not.toHaveBeenCalled();
    expect(writeText.mock.calls).toEqual([['secret'], ['blocking']]);
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each(['deadline', 'lost focus'])('never clears when async read completes after %s', async (reason) => {
    vi.useFakeTimers();
    const tab = backgroundClipboard();
    let release: ((text: string) => void) | undefined;
    const writeText = vi.fn(() => Promise.resolve());
    const readText = vi.fn(() => new Promise<string>((resolve) => { release = resolve; }));
    vi.stubGlobal('navigator', { clipboard: { writeText, readText } });
    await writeExpiringClipboard('secret', true);
    await vi.advanceTimersByTimeAsync(45_000);
    tab.focus();
    await vi.advanceTimersByTimeAsync(0);
    expect(readText).toHaveBeenCalledOnce();
    if (reason === 'deadline') await vi.advanceTimersByTimeAsync(120_000);
    else tab.blur();
    release?.('secret');
    await vi.advanceTimersByTimeAsync(0);
    expect(writeText).toHaveBeenCalledExactlyOnceWith('secret');
    expect(vi.getTimerCount()).toBe(0);
  });

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

    const confirmation = await writeExpiringClipboard('secret', true);
    expect(confirmation).toContain('recorded as a disclosure');
    expect(confirmation).toContain('Attempts to clear after 45s');
    expect(confirmation).toContain('If unfocused, retries once on return within 2 minutes.');
    expect(confirmation).not.toContain('Cleared in 45s');
    expect(confirmation).toContain('Clipboard managers may keep this browser copy.');
    expect(confirmation).toContain('hikyo values get KEY --reveal --clipboard');
    expect(confirmation).not.toContain('secret');
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
