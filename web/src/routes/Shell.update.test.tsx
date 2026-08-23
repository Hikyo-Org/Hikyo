// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { clearNotification, ToastViewport } from '../app/notifications.tsx';
import { ProfileUpdateBadge, UpdateNotice } from './Shell.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

beforeEach(() => {
  const values = new Map<string, string>();
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    value: {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (key: string) => values.get(key) ?? null,
      key: (index: number) => [...values.keys()][index] ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    } satisfies Storage,
  });
});

afterEach(() => {
  clearNotification();
  window.localStorage.clear();
  document.body.replaceChildren();
});

describe('update notification', () => {
  it('leaves the profile badge visible after the update toast is dismissed', async () => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    const status = {
      available: true,
      channel: 'nightly' as const,
      current_version: '1.0.0',
      latest_version: '1.1.0-nightly.20260824.42.g176e6e67',
      prerelease: true,
      release_url:
        'https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.g176e6e67',
    };
    const updateUI = (principalId: string) => (
      <>
        <UpdateNotice status={status} principalId={principalId} />
        <ProfileUpdateBadge version={status.latest_version} />
        <ToastViewport />
      </>
    );

    await act(async () => {
      root.render(updateUI('usr_alice'));
    });

    expect(container.querySelector('[role="status"]')?.textContent).toContain(
      '1.1.0-nightly.20260824.42.g176e6e67',
    );
    const dismiss = container.querySelector('button[aria-label="Dismiss notification"]');
    if (!(dismiss instanceof HTMLButtonElement)) {
      throw new Error('update toast has no dismiss button');
    }
    await act(async () => dismiss.click());

    expect(container.querySelector('.toast')).toBeNull();
    expect(container.querySelector('.account-update-badge')?.getAttribute('aria-label')).toContain(
      '1.1.0-nightly.20260824.42.g176e6e67',
    );
    expect(window.localStorage.length).toBe(1);
    await act(async () => root.render(null));
    await act(async () => root.render(updateUI('usr_alice')));
    expect(container.querySelector('.toast')).toBeNull();
    expect(container.querySelector('.account-update-badge')).not.toBeNull();

    await act(async () => root.render(null));
    await act(async () => root.render(updateUI('usr_bob')));
    expect(container.querySelector('.toast')?.textContent).toContain(status.latest_version);

    await act(async () => root.unmount());
  });
});
