// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  clearNotification,
  notifyFailure,
  notifySuccess,
  notifyUpdate,
  ToastViewport,
} from './notifications.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

afterEach(() => {
  clearNotification();
  document.body.replaceChildren();
});

function mount() {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  return { container, root };
}

describe('toast store', () => {
  it('renders both live regions empty before any toast appears', async () => {
    const { container, root } = mount();
    await act(async () => root.render(<ToastViewport />));

    const assertive = container.querySelector('[role="alert"]');
    const polite = container.querySelector('[role="status"]');
    expect(assertive?.getAttribute('aria-live')).toBe('assertive');
    expect(polite?.getAttribute('aria-live')).toBe('polite');
    expect(assertive?.textContent).toBe('');
    expect(polite?.textContent).toBe('');
    expect(container.querySelector('.toast')).toBeNull();

    await act(async () => root.unmount());
  });

  it('announces a success toast in the polite region', async () => {
    const { container, root } = mount();
    await act(async () => root.render(<ToastViewport />));
    await act(async () => notifySuccess('Saved'));

    expect(container.querySelector('[role="status"]')?.textContent).toBe('Saved');
    expect(container.querySelector('[role="alert"]')?.textContent).toBe('');
    expect(container.querySelector('.toast--success')?.textContent).toContain('Saved');

    await act(async () => root.unmount());
  });

  it('announces a failure toast in the assertive region', async () => {
    const { container, root } = mount();
    await act(async () => root.render(<ToastViewport />));
    await act(async () => notifyFailure('Broke'));

    expect(container.querySelector('[role="alert"]')?.textContent).toBe('Broke');
    expect(container.querySelector('[role="status"]')?.textContent).toBe('');

    await act(async () => root.unmount());
  });

  it('runs the outgoing onDismiss when another toast overwrites it', () => {
    const onDismiss = vi.fn();
    notifyUpdate('Update available', '/release', onDismiss);
    expect(onDismiss).not.toHaveBeenCalled();

    notifySuccess('Saved');
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('runs the outgoing onDismiss when the store is cleared', () => {
    const onDismiss = vi.fn();
    notifyUpdate('Update available', '/release', onDismiss);

    clearNotification();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
