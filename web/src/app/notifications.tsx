import { useSyncExternalStore } from 'react';

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

export function ToastViewport() {
  const notification = useSyncExternalStore(subscribe, snapshot, snapshot);
  const assertive = notification?.tone === 'error' ? notification : null;
  const polite = notification !== null && notification.tone !== 'error' ? notification : null;
  return (
    <>
      <div className="visually-hidden" role="alert" aria-live="assertive">
        {assertive === null ? null : <span key={assertive.id}>{assertive.message}</span>}
      </div>
      <div className="visually-hidden" role="status" aria-live="polite">
        {polite === null ? null : <span key={polite.id}>{polite.message}</span>}
      </div>
      {notification === null ? null : (
        <div key={notification.id} className={`toast toast--${notification.tone}`}>
          <span className="alert__glyph" aria-hidden="true">
            {notification.tone === 'error' ? '!' : notification.tone === 'success' ? '✓' : '↑'}
          </span>
          <span>{notification.message}</span>
          {notification.action === undefined ? null : (
            <a
              className="toast__action"
              href={notification.action.href}
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
            ×
          </button>
        </div>
      )}
    </>
  );
}
