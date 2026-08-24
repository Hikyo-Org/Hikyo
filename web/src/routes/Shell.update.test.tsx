// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { clearNotification, ToastViewport } from '../app/notifications.tsx';
import type { UpdateStatus } from '../api/updates.ts';
import { FleetUpdateNotice, ProfileUpdateBadge } from './Shell.tsx';

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
    const version = '1.1.0-nightly.20260824.42.g176e6e67';
    const status: UpdateStatus = {
      available: true,
      apply_supported: false,
      channel: 'nightly',
      current_version: '1.0.0',
      latest_version: version,
      prerelease: true,
      release_url:
        'https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.g176e6e67',
    };
    const updateUI = (principalId: string) => (
      <>
        <FleetUpdateNotice local={status} remotes={[]} principalId={principalId} />
        <ProfileUpdateBadge version={version} />
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
    expect(container.querySelector('.toast')?.textContent).toContain(version);

    await act(async () => root.unmount());
  });

  it('notifies for an administered remote and keeps the aggregate profile badge after dismissal', async () => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    const updates: Array<{ origin: string; status: UpdateStatus }> = [
      {
        origin: 'https://remote.example',
        status: {
          apply_supported: true,
          apply_backend: 'flux',
          available: true,
          channel: 'stable',
          current_version: '1.0.0',
          latest_version: '1.1.0',
          prerelease: false,
          release_url: 'https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0',
        },
      },
    ];

    await act(async () => {
      root.render(
        <>
          <FleetUpdateNotice local={null} remotes={updates} principalId="usr_admin" />
          <ProfileUpdateBadge version="https://remote.example: 1.1.0" />
          <ToastViewport />
        </>,
      );
    });
    expect(container.querySelector('.toast')?.textContent).toContain('remote.example');
    const dismiss = container.querySelector('button[aria-label="Dismiss notification"]');
    if (!(dismiss instanceof HTMLButtonElement)) {
      throw new Error('remote update toast has no dismiss button');
    }
    await act(async () => dismiss.click());
    expect(container.querySelector('.toast')).toBeNull();
    expect(container.querySelector('.account-update-badge')?.getAttribute('aria-label')).toContain(
      'remote.example',
    );
    expect(window.localStorage.length).toBe(1);
    await act(async () => root.unmount());
  });

  it('combines local and remote updates into one fleet toast', async () => {
    const container = document.createElement('div');
    document.body.append(container);
    const root = createRoot(container);
    const local: UpdateStatus = {
      apply_supported: true,
      apply_backend: 'compose',
      available: true,
      channel: 'stable',
      current_version: '1.0.0',
      latest_version: '1.1.0',
      prerelease: false,
      release_url: 'https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0',
    };
    const remote: UpdateStatus = {
      ...local,
      apply_backend: 'flux',
      current_version: '1.0.1',
    };

    await act(async () => {
      root.render(
        <>
          <FleetUpdateNotice
            local={local}
            remotes={[{ origin: 'https://remote.example', status: remote }]}
            principalId="usr_admin"
          />
          <ToastViewport />
        </>,
      );
    });

    expect(container.querySelectorAll('.toast')).toHaveLength(1);
    expect(container.querySelector('.toast')?.textContent).toContain(
      '2 Hikyo environments have updates available',
    );
    await act(async () => root.unmount());
  });
});
