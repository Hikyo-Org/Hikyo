import { useSyncExternalStore } from 'react';

import { isDemo } from '../demo/config.ts';
import { Glyph } from '../ui/Glyph.tsx';
import { isAppMode, knownShellOrigin } from './shell.ts';

type Notification = {
  id: number;
  message: string;
  tone: 'error' | 'info' | 'success';
  action?: { href: string; label: string };
  onDismiss?: () => void;
};

let current: Notification | null = null;
let nextId = 1;
const listeners = new Set<() => void>();

function publish(
  tone: Notification['tone'],
  message: string,
  action?: Notification['action'],
  onDismiss?: () => void,
): void {
  current?.onDismiss?.();
  current = { id: nextId, message, tone, action, onDismiss };
  nextId += 1;
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function snapshot(): Notification | null {
  return current;
}

export function notifyFailure(message: string): void {
  publish('error', message);
}

export function notifySuccess(message: string): void {
  publish('success', message);
}

export function notifyUpdate(
  message: string,
  href: string,
  onDismiss: () => void,
  label = 'View release',
): void {
  publish('info', message, { href, label }, onDismiss);
}

export function clearNotification(): void {
  if (current === null) {
    return;
  }
  current.onDismiss?.();
  current = null;
  for (const listener of listeners) {
    listener();
  }
}

function dismiss(id: number): void {
  if (current?.id !== id) {
    return;
  }
  clearNotification();
}

/**
 * A toast link opens in a new window. The desktop app's window origin is not
 * the instance's, so a relative link resolves against the real origin and the
 * shell opens it in the system browser.
 */
function toastHref(href: string): string {
  if (isDemo && href.startsWith('/')) return `#${href}`;
  const origin = isAppMode() ? knownShellOrigin() : undefined;
  return origin !== undefined && href.startsWith('/') && !href.startsWith('//') ? `${origin}${href}` : href;
}

export function ToastViewport() {
  const notification = useSyncExternalStore(subscribe, snapshot, snapshot);
  const assertive = notification?.tone === 'error' ? notification : null;
  const polite = notification !== null && notification.tone !== 'error' ? notification : null;
  return (
    <>
      <div className="visually-hidden" role="alert" aria-live="assertive" aria-atomic="true">
        {assertive === null ? null : <span key={assertive.id}>{assertive.message}</span>}
      </div>
      <div className="visually-hidden" role="status" aria-live="polite" aria-atomic="true">
        {polite === null ? null : <span key={polite.id}>{polite.message}</span>}
      </div>
      {notification === null ? null : (
        <div key={notification.id} className={`toast toast--${notification.tone}`}>
          <Glyph
            name={notification.tone === 'error' ? 'alert' : notification.tone === 'success' ? 'check' : 'arrow-up'}
            className="alert__glyph"
          />
          <span>{notification.message}</span>
          {notification.action === undefined ? null : (
            <a
              className="toast__action"
              href={toastHref(notification.action.href)}
              target="_blank"
              rel="noreferrer"
            >
              {notification.action.label}
            </a>
          )}
          <button
            type="button"
            className="toast__dismiss"
            aria-label="Dismiss notification"
            onClick={() => dismiss(notification.id)}
          >
            <Glyph name="cross" />
          </button>
        </div>
      )}
    </>
  );
}
